package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sync/errgroup"

	cli "github.com/urfave/cli/v3"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
	"github.com/krait-go/krait/pkg/architecture"
	"github.com/krait-go/krait/pkg/churn"
	"github.com/krait-go/krait/pkg/circular"
	"github.com/krait-go/krait/pkg/complexity"
	"github.com/krait-go/krait/pkg/config"
	"github.com/krait-go/krait/pkg/deadcode"
	"github.com/krait-go/krait/pkg/deps"
	"github.com/krait-go/krait/pkg/duplication"
	"github.com/krait-go/krait/pkg/health"
	"github.com/krait-go/krait/pkg/reporter"
	"github.com/krait-go/krait/pkg/suppression"
	"github.com/krait-go/krait/pkg/unusedfiles"
)

var version = "dev"

func allAnalyzers() []analyzer.Analyzer {
	return []analyzer.Analyzer{
		deadcode.New(),
		duplication.New(),
		complexity.New(),
		architecture.New(),
		deps.New(),
		unusedfiles.New(),
		circular.New(),
	}
}

func allPostAnalyzers() []analyzer.PostAnalyzer {
	return []analyzer.PostAnalyzer{
		churn.New(),
		health.New(),
	}
}

func globalFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "format",
			Aliases: []string{"f"},
			Value:   "text",
			Usage:   "Output format: text, json, sarif, markdown",
		},
		&cli.StringFlag{
			Name:    "dir",
			Aliases: []string{"d"},
			Value:   ".",
			Usage:   "Project root directory",
		},
		&cli.IntFlag{
			Name:    "threshold",
			Aliases: []string{"t"},
			Value:   0,
			Usage:   "Exit 1 if total findings exceed N",
		},
		&cli.BoolFlag{
			Name:  "tests",
			Usage: "Include _test.go files in analysis",
		},
		&cli.BoolFlag{
			Name:  "ci",
			Usage: "CI mode: JSON output, exit 1 on any error-severity finding",
		},
		&cli.BoolFlag{
			Name:  "score",
			Usage: "Include health score in output",
		},
	}
}

func runAnalysis(cmd *cli.Command, analyzers []analyzer.Analyzer, postAnalyzers []analyzer.PostAnalyzer, includeScore bool) error {
	startTime := time.Now().UTC()

	project, cfg, err := loadProjectConfig(cmd)
	if err != nil {
		return err
	}

	results, err := executeAnalyzers(project, cfg, analyzers, postAnalyzers, includeScore)
	if err != nil {
		return err
	}

	return applySuppressionsAndReport(cmd, project, results, startTime)
}

// loadProjectConfig resolves the project directory, loads and validates config,
// applies flag overrides, and parses the project AST.
func loadProjectConfig(cmd *cli.Command) (*analyzer.Project, *analyzer.Config, error) {
	dir := cmd.String("dir")
	if cmd.Args().Len() > 0 {
		dir = cmd.Args().First()
	}

	cfg, err := config.Load(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("loading config: %w", err)
	}
	if cmd.Bool("tests") {
		cfg.IncludeTests = true
	}
	if err := config.Validate(cfg); err != nil {
		return nil, nil, fmt.Errorf("invalid config: %w", err)
	}

	project, err := parser.Parse(dir, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing project: %w", err)
	}

	return project, cfg, nil
}

// executeAnalyzers runs the parallel analyzers, then the sequential
// post-analyzers, and optionally appends a health score result.
func executeAnalyzers(
	project *analyzer.Project,
	cfg *analyzer.Config,
	analyzers []analyzer.Analyzer,
	postAnalyzers []analyzer.PostAnalyzer,
	includeScore bool,
) ([]*analyzer.Result, error) {
	// Run analyzers in parallel.
	results := make([]*analyzer.Result, len(analyzers))
	g, _ := errgroup.WithContext(context.Background())
	for i, a := range analyzers {
		g.Go(func() error {
			result, err := a.Analyze(project, cfg)
			if err != nil {
				return fmt.Errorf("analyzer %s: %w", a.Name(), err)
			}
			result.DurationMs = result.Duration.Milliseconds()
			results[i] = result
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Run post-analyzers sequentially, collecting into a separate slice
	// to avoid appending to the pre-allocated results slice (makezero).
	var postResults []*analyzer.Result
	for _, pa := range postAnalyzers {
		result, err := pa.PostAnalyze(project, cfg, results)
		if err != nil {
			return nil, fmt.Errorf("post-analyzer %s: %w", pa.Name(), err)
		}
		result.DurationMs = result.Duration.Milliseconds()
		postResults = append(postResults, result)
	}

	// Merge results for post-analyzers and optional health score.
	all := make([]*analyzer.Result, 0, len(results)+len(postResults)+1)
	all = append(all, results...)
	all = append(all, postResults...)

	// If --score and health not already included.
	if includeScore && !hasPostAnalyzer(postAnalyzers, "health") {
		h := health.New()
		result, err := h.PostAnalyze(project, cfg, all)
		if err != nil {
			return nil, fmt.Errorf("health score: %w", err)
		}
		result.DurationMs = result.Duration.Milliseconds()
		all = append(all, result)
	}

	return all, nil
}

// applySuppressionsAndReport applies suppression filters, formats the report,
// and enforces CI and threshold exit conditions.
func applySuppressionsAndReport(cmd *cli.Command, project *analyzer.Project, results []*analyzer.Result, startTime time.Time) error {
	// Suppression filtering.
	suppMap := suppression.BuildMapFromProject(project)
	for _, r := range results {
		filtered, staleFindings := suppMap.Filter(r.Findings)
		r.Findings = filtered
		if len(staleFindings) > 0 {
			r.Findings = append(r.Findings, staleFindings...)
		}
	}

	dir := cmd.String("dir")
	if cmd.Args().Len() > 0 {
		dir = cmd.Args().First()
	}
	report := buildReport(dir, results, time.Since(startTime), startTime)

	format := cmd.String("format")
	if cmd.Bool("ci") {
		format = "json"
	}
	if err := reporter.Format(os.Stdout, report, format); err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	if cmd.Bool("ci") && report.Summary.BySeverity[analyzer.SeverityError] > 0 {
		return fmt.Errorf("CI check failed: %d error-severity findings", report.Summary.BySeverity[analyzer.SeverityError])
	}
	threshold := cmd.Int("threshold")
	if threshold > 0 && report.Summary.TotalFindings > threshold {
		return fmt.Errorf("threshold exceeded: %d findings (threshold: %d)", report.Summary.TotalFindings, threshold)
	}

	return nil
}

func hasPostAnalyzer(postAnalyzers []analyzer.PostAnalyzer, name string) bool {
	for _, pa := range postAnalyzers {
		if pa.Name() == name {
			return true
		}
	}
	return false
}

func buildReport(rootDir string, results []*analyzer.Result, totalDuration time.Duration, startTime time.Time) *analyzer.Report {
	summary := analyzer.ReportSummary{
		BySeverity: make(map[analyzer.Severity]int),
		ByCategory: make(map[analyzer.Category]int),
	}

	for _, r := range results {
		for _, f := range r.Findings {
			summary.TotalFindings++
			summary.BySeverity[f.Severity]++
			summary.ByCategory[f.Category]++
		}
	}

	absDir, err := filepath.Abs(rootDir)
	if err != nil {
		absDir = rootDir
	}

	return &analyzer.Report{
		Version:       version,
		Timestamp:     startTime.Format(time.RFC3339),
		RootDir:       absDir,
		TotalDuration: totalDuration.Round(time.Millisecond).String(),
		Summary:       summary,
		Results:       results,
	}
}

func initCmd() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Create .krait.json config file with defaults",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			dir := cmd.String("dir")
			path := filepath.Join(dir, ".krait.json")

			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}

			cfg := analyzer.DefaultConfig()
			data, err := json.MarshalIndent(cfg, "", "  ")
			if err != nil {
				return err
			}

			if err := os.WriteFile(path, data, 0o600); err != nil {
				return err
			}

			_, _ = fmt.Fprintf(os.Stdout, "Created %s\n", path)
			return nil
		},
	}
}

func main() {
	cmd := &cli.Command{
		Name:           "krait",
		Usage:          "Unified codebase health analyzer for Go",
		Version:        version,
		DefaultCommand: "check",
		Flags:          globalFlags(),
		Commands: []*cli.Command{
			{
				Name:  "check",
				Usage: "Run all analyzers",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, allAnalyzers(), allPostAnalyzers(), true)
				},
			},
			{
				Name:  "dead",
				Usage: "Run dead code analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{deadcode.New()}, nil, cmd.Bool("score"))
				},
			},
			{
				Name:  "dupes",
				Usage: "Run code duplication analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{duplication.New()}, nil, cmd.Bool("score"))
				},
			},
			{
				Name:  "complexity",
				Usage: "Run complexity analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{complexity.New()}, nil, cmd.Bool("score"))
				},
			},
			{
				Name:  "arch",
				Usage: "Run architecture analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{architecture.New()}, nil, cmd.Bool("score"))
				},
			},
			{
				Name:  "deps",
				Usage: "Run dependency analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{deps.New()}, nil, cmd.Bool("score"))
				},
			},
			{
				Name:  "unused",
				Usage: "Run unused files/packages analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{unusedfiles.New()}, nil, cmd.Bool("score"))
				},
			},
			{
				Name:  "circular",
				Usage: "Run circular dependency analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{circular.New()}, nil, cmd.Bool("score"))
				},
			},
			{
				Name:  "hotspots",
				Usage: "Run git churn hotspot analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, []analyzer.Analyzer{complexity.New()}, []analyzer.PostAnalyzer{churn.New()}, cmd.Bool("score"))
				},
			},
			{
				Name:  "health",
				Usage: "Run all analyzers and show health score",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalysis(cmd, allAnalyzers(), allPostAnalyzers(), true)
				},
			},
			initCmd(),
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
