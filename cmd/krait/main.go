package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	cli "github.com/urfave/cli/v3"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
	"github.com/krait-go/krait/pkg/architecture"
	"github.com/krait-go/krait/pkg/complexity"
	"github.com/krait-go/krait/pkg/config"
	"github.com/krait-go/krait/pkg/deadcode"
	"github.com/krait-go/krait/pkg/deps"
	"github.com/krait-go/krait/pkg/duplication"
	"github.com/krait-go/krait/pkg/reporter"
)

var version = "dev"

func allAnalyzers() []analyzer.Analyzer {
	return []analyzer.Analyzer{
		deadcode.New(),
		duplication.New(),
		complexity.New(),
		architecture.New(),
		deps.New(),
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
	}
}

func runAnalyzers(cmd *cli.Command, analyzers []analyzer.Analyzer) error {
	totalStart := time.Now()

	// Resolve directory — positional arg overrides --dir flag
	dir := cmd.String("dir")
	if cmd.Args().Len() > 0 {
		dir = cmd.Args().First()
	}

	// Load config
	cfg, err := config.Load(dir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Apply flag overrides
	if cmd.Bool("tests") {
		cfg.IncludeTests = true
	}

	// Validate config
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	// Parse project
	project, err := parser.Parse(dir, cfg)
	if err != nil {
		return fmt.Errorf("parsing project: %w", err)
	}

	// Run analyzers
	var results []*analyzer.Result
	for _, a := range analyzers {
		result, err := a.Analyze(project, cfg)
		if err != nil {
			return fmt.Errorf("analyzer %s: %w", a.Name(), err)
		}
		result.DurationMs = result.Duration.Milliseconds()
		results = append(results, result)
	}

	// Build report
	report := buildReport(dir, results, time.Since(totalStart))

	// Determine format
	format := cmd.String("format")
	if cmd.Bool("ci") {
		format = "json"
	}

	// Output
	if err := reporter.Format(os.Stdout, report, format); err != nil {
		return fmt.Errorf("formatting output: %w", err)
	}

	// Exit code logic
	if cmd.Bool("ci") && report.Summary.BySeverity[analyzer.SeverityError] > 0 {
		return fmt.Errorf("CI check failed: %d error-severity findings", report.Summary.BySeverity[analyzer.SeverityError])
	}
	threshold := cmd.Int("threshold")
	if threshold > 0 && report.Summary.TotalFindings > threshold {
		return fmt.Errorf("threshold exceeded: %d findings (threshold: %d)", report.Summary.TotalFindings, threshold)
	}

	return nil
}

func buildReport(rootDir string, results []*analyzer.Result, totalDuration time.Duration) *analyzer.Report {
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
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
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

			if err := os.WriteFile(path, data, 0o644); err != nil {
				return err
			}

			fmt.Fprintf(os.Stdout, "Created %s\n", path)
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
					return runAnalyzers(cmd, allAnalyzers())
				},
			},
			{
				Name:  "dead",
				Usage: "Run dead code analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalyzers(cmd, []analyzer.Analyzer{deadcode.New()})
				},
			},
			{
				Name:  "dupes",
				Usage: "Run code duplication analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalyzers(cmd, []analyzer.Analyzer{duplication.New()})
				},
			},
			{
				Name:  "complexity",
				Usage: "Run complexity analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalyzers(cmd, []analyzer.Analyzer{complexity.New()})
				},
			},
			{
				Name:  "arch",
				Usage: "Run architecture analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalyzers(cmd, []analyzer.Analyzer{architecture.New()})
				},
			},
			{
				Name:  "deps",
				Usage: "Run dependency analyzer",
				Action: func(ctx context.Context, cmd *cli.Command) error {
					return runAnalyzers(cmd, []analyzer.Analyzer{deps.New()})
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
