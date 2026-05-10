// Package churn implements a post-analyzer that identifies git churn hotspots
// by correlating commit frequency with cyclomatic complexity.
package churn

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/krait-go/krait/pkg/analyzer"
)

// PostAnalyzer identifies files that are both frequently changed (high churn)
// and structurally complex, making them high-risk maintenance targets.
type PostAnalyzer struct{}

var _ analyzer.PostAnalyzer = (*PostAnalyzer)(nil)

// New returns a new churn post-analyzer.
func New() *PostAnalyzer {
	return &PostAnalyzer{}
}

// Name returns the analyzer name.
func (a *PostAnalyzer) Name() string {
	return "churn"
}

// Description returns a human-readable description.
func (a *PostAnalyzer) Description() string {
	return "Identifies files with high git churn combined with high complexity (maintenance hotspots)"
}

// ParsePeriod converts a short period string to a git --since argument.
// Accepted formats: "<N>m" for months, "<N>y" for years (e.g. "6m", "1y").
// N must be a positive integer. Returns an error for any other input.
func ParsePeriod(s string) (string, error) {
	if len(s) < 2 {
		return "", fmt.Errorf("invalid period %q: must be at least 2 characters (e.g. \"6m\", \"1y\")", s)
	}

	suffix := s[len(s)-1]
	numStr := s[:len(s)-1]

	n, err := strconv.Atoi(numStr)
	if err != nil {
		return "", fmt.Errorf("invalid period %q: numeric part %q is not an integer", s, numStr)
	}
	if n <= 0 {
		return "", fmt.Errorf("invalid period %q: number must be greater than 0", s)
	}

	switch suffix {
	case 'm':
		return fmt.Sprintf("%d.months.ago", n), nil
	case 'y':
		return fmt.Sprintf("%d.years.ago", n), nil
	default:
		return "", fmt.Errorf("invalid period %q: suffix %q is not supported (use 'm' for months or 'y' for years)", s, string(suffix))
	}
}

// ComputeRiskScores multiplies max-normalized churn and complexity scores.
// Files with zero churn always get a risk score of zero.
func ComputeRiskScores(churnCounts map[string]int, complexities map[string]float64) map[string]float64 {
	scores := make(map[string]float64, len(churnCounts))

	// Find max churn across all files that have churn.
	maxChurn := 0
	for _, c := range churnCounts {
		if c > maxChurn {
			maxChurn = c
		}
	}
	if maxChurn == 0 {
		// No churn at all — every file gets zero risk.
		for file := range churnCounts {
			scores[file] = 0
		}
		return scores
	}

	// Find max complexity across all files with known complexity.
	maxComplexity := 0.0
	for _, c := range complexities {
		if c > maxComplexity {
			maxComplexity = c
		}
	}

	for file, churn := range churnCounts {
		normalizedChurn := float64(churn) / float64(maxChurn)

		normalizedComplexity := 0.0
		if maxComplexity > 0 {
			if c, ok := complexities[file]; ok {
				normalizedComplexity = c / maxComplexity
			}
		} else {
			// No complexity data at all — treat all files as equally complex.
			normalizedComplexity = 1.0
		}

		scores[file] = normalizedChurn * normalizedComplexity
	}

	return scores
}

// hotspotEntry holds the computed data for a single churn hotspot.
type hotspotEntry struct {
	file       string
	churn      int
	complexity float64
	risk       float64
}

// PostAnalyze runs the churn post-analysis pass.
func (a *PostAnalyzer) PostAnalyze(project *analyzer.Project, cfg *analyzer.Config, results []*analyzer.Result) (*analyzer.Result, error) {
	start := time.Now()

	period := cfg.ChurnPeriod
	if period == "" {
		period = "6m"
	}

	sinceArg, err := ParsePeriod(period)
	if err != nil {
		return emptyResult(a.Name(), start, fmt.Sprintf("invalid churn_period: %v", err)), nil
	}

	churnCounts, gitErr := collectChurnCounts(project.RootDir, sinceArg)
	if gitErr != nil {
		// Git is unavailable or not a git repo — degrade gracefully.
		return emptyResult(a.Name(), start, fmt.Sprintf("git unavailable: %v", gitErr)), nil
	}

	complexities := extractComplexities(results)
	riskScores := ComputeRiskScores(churnCounts, complexities)

	hotspots := buildHotspots(churnCounts, complexities, riskScores)

	findings := buildFindings(hotspots, cfg)
	stats := buildStats(hotspots)

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   a.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats:      stats,
	}, nil
}

// collectChurnCounts shells out to git and counts how many times each .go file
// was modified during the given period.
func collectChurnCounts(rootDir, sinceArg string) (map[string]int, error) {
	cmd := exec.Command(
		"git", "log",
		"--format=",
		"--name-only",
		"--diff-filter=M",
		"--since="+sinceArg,
		"--",
		"*.go",
	)
	cmd.Dir = rootDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git log: %w (stderr: %s)", err, strings.TrimSpace(stderr.String()))
	}

	counts := make(map[string]int)
	scanner := bufio.NewScanner(&stdout)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		counts[line]++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading git output: %w", err)
	}

	return counts, nil
}

// fileAcc accumulates cyclomatic complexity sum and sample count for a file.
type fileAcc struct {
	sum   float64
	count int
}

// extractComplexities builds a per-file average cyclomatic complexity map from
// prior analyzer results. It reads findings from the complexity analyzer whose
// Meta["cyclomatic"] is set, and falls back to the stats["complexity_hotspots"]
// slice if no per-finding data is available.
func extractComplexities(results []*analyzer.Result) map[string]float64 {
	byFile := make(map[string]*fileAcc)

	for _, r := range results {
		if r.Analyzer != "complexity" {
			continue
		}

		// Primary: scan individual findings.
		for _, f := range r.Findings {
			cycloRaw, ok := f.Meta["cyclomatic"]
			if !ok {
				continue
			}
			file := f.Location.File
			if file == "" {
				continue
			}
			cyclo := toFloat64(cycloRaw)
			if _, exists := byFile[file]; !exists {
				byFile[file] = &fileAcc{}
			}
			byFile[file].sum += cyclo
			byFile[file].count++
		}

		// Fallback: use complexity_hotspots from stats if no findings populated the map.
		if len(byFile) == 0 {
			if hotspots, ok := r.Stats["complexity_hotspots"]; ok {
				extractFromHotspots(hotspots, byFile)
			}
		}
	}

	result := make(map[string]float64, len(byFile))
	for file, a := range byFile {
		if a.count > 0 {
			result[file] = a.sum / float64(a.count)
		}
	}
	return result
}

// extractFromHotspots reads the []map[string]any hotspots slice from the
// complexity stats and adds per-file cyclomatic values to byFile.
func extractFromHotspots(hotspots any, byFile map[string]*fileAcc) {
	slice, ok := hotspots.([]map[string]any)
	if !ok {
		return
	}
	for _, entry := range slice {
		file, _ := entry["file"].(string)
		if file == "" {
			continue
		}
		cyclo := toFloat64(entry["cyclomatic"])
		if _, exists := byFile[file]; !exists {
			byFile[file] = &fileAcc{}
		}
		byFile[file].sum += cyclo
		byFile[file].count++
	}
}

// toFloat64 converts common numeric types to float64.
func toFloat64(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case float64:
		return n
	case float32:
		return float64(n)
	}
	return 0
}

// buildHotspots assembles, sorts, and caps the hotspot list.
func buildHotspots(churnCounts map[string]int, complexities map[string]float64, riskScores map[string]float64) []hotspotEntry {
	entries := make([]hotspotEntry, 0, len(churnCounts))
	for file, churn := range churnCounts {
		entries = append(entries, hotspotEntry{
			file:       file,
			churn:      churn,
			complexity: complexities[file],
			risk:       riskScores[file],
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].risk != entries[j].risk {
			return entries[i].risk > entries[j].risk
		}
		if entries[i].churn != entries[j].churn {
			return entries[i].churn > entries[j].churn
		}
		return entries[i].file < entries[j].file
	})

	if len(entries) > 10 {
		entries = entries[:10]
	}
	return entries
}

// buildFindings converts hotspot entries into analyzer findings.
func buildFindings(hotspots []hotspotEntry, cfg *analyzer.Config) []*analyzer.Finding {
	sev, ok := analyzer.ResolveSeverity("churn-hotspot", analyzer.SeverityInfo, cfg)
	if !ok {
		return nil
	}

	findings := make([]*analyzer.Finding, 0, len(hotspots))
	for _, h := range hotspots {
		findings = append(findings, &analyzer.Finding{
			Rule:     "churn-hotspot",
			Category: analyzer.CategoryComplexity,
			Severity: sev,
			Message: fmt.Sprintf(
				"%s is a maintenance hotspot: modified %d times with avg cyclomatic complexity %.1f (risk score: %.2f)",
				h.file, h.churn, h.complexity, h.risk,
			),
			Location: analyzer.Location{File: h.file},
			Meta: map[string]any{
				"churn":      h.churn,
				"complexity": h.complexity,
				"risk":       h.risk,
			},
		})
	}
	return findings
}

// buildStats constructs the stats map for the result.
func buildStats(hotspots []hotspotEntry) map[string]any {
	hotspotFiles := make([]map[string]any, 0, len(hotspots))
	for _, h := range hotspots {
		hotspotFiles = append(hotspotFiles, map[string]any{
			"file":       h.file,
			"churn":      h.churn,
			"complexity": h.complexity,
			"risk":       h.risk,
		})
	}
	return map[string]any{
		"total_hotspots": len(hotspots),
		"hotspot_files":  hotspotFiles,
	}
}

// emptyResult returns a result with no findings and a warning stat.
func emptyResult(name string, start time.Time, warning string) *analyzer.Result {
	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   name,
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   nil,
		Stats: map[string]any{
			"total_hotspots": 0,
			"hotspot_files":  []map[string]any{},
			"warning":        warning,
		},
	}
}
