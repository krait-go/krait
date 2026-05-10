// Package health implements a post-analyzer that aggregates results from all
// other analyzers into a single weighted health score.
package health

import (
	"fmt"
	"math"
	"time"

	"github.com/krait-go/krait/pkg/analyzer"
)

type healthAnalyzer struct{}

// New returns a new health post-analyzer.
func New() analyzer.PostAnalyzer {
	return &healthAnalyzer{}
}

var _ analyzer.PostAnalyzer = (*healthAnalyzer)(nil)

func (h *healthAnalyzer) Name() string {
	return "health"
}

func (h *healthAnalyzer) Description() string {
	return "Computes a weighted health score from all analyzer results"
}

// signalInput holds the raw signal values extracted from each analyzer result.
type signalInput struct {
	unusedPct      float64 // dead-code: unused_percentage
	dupPct         float64 // duplication: duplication_percentage
	overThreshPct  float64 // complexity: (over_threshold / total_functions) * 100
	archViolations float64 // architecture: count of layer-violation findings
	depsIssues     float64 // dependency: unused_dependencies + unlisted_imports
}

// PostAnalyze computes a weighted health score and emits a finding when the
// score falls below cfg.MinHealthScore.
func (h *healthAnalyzer) PostAnalyze(
	project *analyzer.Project,
	cfg *analyzer.Config,
	results []*analyzer.Result,
) (*analyzer.Result, error) {
	start := time.Now()

	weights := cfg.HealthWeights
	if weights == nil {
		weights = analyzer.DefaultHealthWeights()
	}
	normWeights := normalizeWeights(weights)

	signals := extractSignals(results)
	perSignal := computeSignalScores(signals)

	composite := weightedScore(perSignal, normWeights)
	score := int(math.Round(composite))
	grade := letterGrade(score)

	stats := buildStats(score, grade, perSignal, normWeights)

	var findings []*analyzer.Finding
	if cfg.MinHealthScore > 0 && score < cfg.MinHealthScore {
		sev, ok := analyzer.ResolveSeverity("low-health-score", analyzer.SeverityError, cfg)
		if ok {
			findings = append(findings, &analyzer.Finding{
				Rule:     "low-health-score",
				Category: analyzer.CategoryHealth,
				Severity: sev,
				Message: fmt.Sprintf(
					"health score %d is below minimum threshold of %d (grade: %s)",
					score, cfg.MinHealthScore, grade,
				),
				Location: analyzer.Location{File: ""},
				Meta: map[string]any{
					"score":     score,
					"grade":     grade,
					"threshold": cfg.MinHealthScore,
				},
			})
		}
	}

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   h.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats:      stats,
	}, nil
}

// normalizedWeights holds each weight as a fraction of the total.
type normalizedWeights struct {
	deadCode     float64
	duplication  float64
	complexity   float64
	architecture float64
	dependencies float64
}

// normalizeWeights converts integer weights to fractions that sum to 1.0.
// If the total is zero, equal weights are used.
func normalizeWeights(w *analyzer.HealthWeights) normalizedWeights {
	total := w.DeadCode + w.Duplication + w.Complexity + w.Architecture + w.Dependencies
	if total == 0 {
		return normalizedWeights{0.2, 0.2, 0.2, 0.2, 0.2}
	}
	t := float64(total)
	return normalizedWeights{
		deadCode:     float64(w.DeadCode) / t,
		duplication:  float64(w.Duplication) / t,
		complexity:   float64(w.Complexity) / t,
		architecture: float64(w.Architecture) / t,
		dependencies: float64(w.Dependencies) / t,
	}
}

// perSignalScore holds the raw input value and clamped [0,100] score for one signal.
type perSignalScore struct {
	raw   float64
	score float64
}

// computeSignalScores converts raw signal values to [0,100] scores.
func computeSignalScores(s signalInput) map[string]perSignalScore {
	return map[string]perSignalScore{
		"dead_code":    {raw: s.unusedPct, score: clamp(100 - s.unusedPct*5)},
		"duplication":  {raw: s.dupPct, score: clamp(100 - s.dupPct*5)},
		"complexity":   {raw: s.overThreshPct, score: clamp(100 - s.overThreshPct*10)},
		"architecture": {raw: s.archViolations, score: clamp(100 - s.archViolations*5)},
		"dependencies": {raw: s.depsIssues, score: clamp(100 - s.depsIssues*10)},
	}
}

// weightedScore computes the composite score as the sum of signal_score × weight.
func weightedScore(scores map[string]perSignalScore, w normalizedWeights) float64 {
	return scores["dead_code"].score*w.deadCode +
		scores["duplication"].score*w.duplication +
		scores["complexity"].score*w.complexity +
		scores["architecture"].score*w.architecture +
		scores["dependencies"].score*w.dependencies
}

// letterGrade maps a numeric score to a letter grade.
func letterGrade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 80:
		return "B"
	case score >= 70:
		return "C"
	case score >= 60:
		return "D"
	default:
		return "F"
	}
}

// clamp constrains v to the range [0, 100].
func clamp(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// buildStats constructs the Stats map returned in the Result.
func buildStats(
	score int,
	grade string,
	perSignal map[string]perSignalScore,
	w normalizedWeights,
) map[string]any {
	weightByName := map[string]float64{
		"dead_code":    w.deadCode,
		"duplication":  w.duplication,
		"complexity":   w.complexity,
		"architecture": w.architecture,
		"dependencies": w.dependencies,
	}

	signalStats := make(map[string]any, len(perSignal))
	for name, ps := range perSignal {
		signalStats[name] = map[string]any{
			"raw":    ps.raw,
			"score":  ps.score,
			"weight": weightByName[name],
		}
	}

	return map[string]any{
		"health_score":  score,
		"health_grade":  grade,
		"signal_scores": signalStats,
	}
}

// extractSignals walks the results slice and pulls the relevant stats from each
// named analyzer. Unknown analyzer names are silently skipped.
func extractSignals(results []*analyzer.Result) signalInput {
	var s signalInput
	for _, r := range results {
		if r == nil {
			continue
		}
		switch r.Analyzer {
		case "dead-code":
			s.unusedPct = statFloat(r.Stats, "unused_percentage")

		case "duplication":
			s.dupPct = statFloat(r.Stats, "duplication_percentage")

		case "complexity":
			total := statFloat(r.Stats, "total_functions")
			over := statFloat(r.Stats, "over_threshold")
			if total > 0 {
				s.overThreshPct = (over / total) * 100
			}

		case "architecture":
			s.archViolations = float64(countRuleFindings(r.Findings, "layer-violation"))

		case "dependency":
			unused := statFloat(r.Stats, "unused_dependencies")
			unlisted := statFloat(r.Stats, "unlisted_imports")
			s.depsIssues = unused + unlisted
		}
	}
	return s
}

// countRuleFindings counts findings whose Rule field matches the given rule name.
func countRuleFindings(findings []*analyzer.Finding, rule string) int {
	count := 0
	for _, f := range findings {
		if f != nil && f.Rule == rule {
			count++
		}
	}
	return count
}

// statFloat retrieves a numeric value from a stats map, handling int, int64,
// and float64 type assertions. Returns 0.0 if the key is absent or the type
// is not numeric.
func statFloat(stats map[string]any, key string) float64 {
	if stats == nil {
		return 0
	}
	v, ok := stats[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case int32:
		return float64(n)
	}
	return 0
}
