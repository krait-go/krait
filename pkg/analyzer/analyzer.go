// Package analyzer defines the core types and interfaces for krait's static analysis framework.
package analyzer

import (
	"path/filepath"
	"time"
)

// Severity represents the severity level of a finding.
type Severity string

// Severity constants define finding severity levels.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
	SeverityOff     Severity = "off"
)

// Category represents the category of analysis.
type Category string

// Category constants define analysis categories.
const (
	CategoryDeadCode     Category = "dead-code"
	CategoryDuplication  Category = "duplication"
	CategoryComplexity   Category = "complexity"
	CategoryArchitecture Category = "architecture"
	CategoryDependency   Category = "dependency"
	CategoryCircular     Category = "circular"
	CategoryHealth       Category = "health"
	CategorySuppression  Category = "suppression"
)

// Location identifies a position in source code.
type Location struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column,omitempty"`
	EndLine int    `json:"end_line,omitempty"`
}

// Finding represents a single issue found by an analyzer.
type Finding struct {
	Rule             string         `json:"rule"`
	Category         Category       `json:"category"`
	Severity         Severity       `json:"severity"`
	Message          string         `json:"message"`
	Location         Location       `json:"location"`
	RelatedLocations []Location     `json:"related_locations,omitempty"`
	Meta             map[string]any `json:"meta,omitempty"`
}

// Result is the output of a single analyzer run.
type Result struct {
	Analyzer   string         `json:"analyzer"`
	Duration   time.Duration  `json:"-"`
	DurationMs int64          `json:"duration_ms"`
	Findings   []*Finding     `json:"findings"`
	Stats      map[string]any `json:"stats,omitempty"`
}

// ReportSummary holds aggregate counts for a report.
type ReportSummary struct {
	TotalFindings int              `json:"total_findings"`
	BySeverity    map[Severity]int `json:"by_severity"`
	ByCategory    map[Category]int `json:"by_category"`
}

// Report is the aggregate output of all analyzers.
type Report struct {
	Version       string        `json:"version"`
	Timestamp     string        `json:"timestamp"`
	RootDir       string        `json:"root_dir"`
	TotalDuration string        `json:"total_duration"`
	Summary       ReportSummary `json:"summary"`
	Results       []*Result     `json:"results"`
}

// LayerConfig defines an architecture layer for violation checking.
type LayerConfig struct {
	Name      string   `json:"name"`
	Packages  []string `json:"packages"`
	CanImport []string `json:"can_import"`
}

// Config holds all configuration for analyzer execution.
type Config struct {
	IgnorePatterns      []string            `json:"ignore_patterns"`
	IncludeTests        bool                `json:"include_tests"`
	Rules               map[string]Severity `json:"rules"`
	CyclomaticThreshold int                 `json:"cyclomatic_threshold"`
	CognitiveThreshold  int                 `json:"cognitive_threshold"`
	MinDuplicateLines   int                 `json:"min_duplicate_lines"`
	MinDuplicateTokens  int                 `json:"min_duplicate_tokens"`
	ArchitectureLayers  []LayerConfig       `json:"architecture_layers"`
	IgnoreExports       []string            `json:"ignore_exports"`
	GodPackageThreshold int                 `json:"god_package_threshold"`
	ChurnPeriod         string              `json:"churn_period"`
	MinHealthScore      int                 `json:"min_health_score"`
	HealthWeights       *HealthWeights      `json:"health_weights"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		IgnorePatterns: []string{
			"vendor/**",
			"**/testdata/**",
			"**/.git/**",
			"**/generated/**",
			"**/*.pb.go",
			"**/*_gen.go",
		},
		IncludeTests:        false,
		Rules:               defaultRules(),
		CyclomaticThreshold: 15,
		CognitiveThreshold:  20,
		MinDuplicateLines:   6,
		MinDuplicateTokens:  50,
		GodPackageThreshold: 10,
		IgnoreExports:       []string{"New*", "Must*", "Register*"},
		ChurnPeriod:         "6m",
		MinHealthScore:      0,
		HealthWeights:       DefaultHealthWeights(),
	}
}

func defaultRules() map[string]Severity {
	return map[string]Severity{
		"unused-export-func":         SeverityWarning,
		"unused-export-method":       SeverityWarning,
		"unused-export-type":         SeverityWarning,
		"unused-export-var":          SeverityWarning,
		"unused-export-const":        SeverityWarning,
		"unused-dependency":          SeverityWarning,
		"unlisted-dependency":        SeverityError,
		"high-cyclomatic-complexity": SeverityWarning,
		"high-cognitive-complexity":  SeverityWarning,
		"code-duplication":           SeverityWarning,
		"god-package":                SeverityWarning,
		"layer-violation":            SeverityError,
		"unused-file":                SeverityWarning,
		"circular-dependency":        SeverityWarning,
		"churn-hotspot":              SeverityInfo,
		"low-health-score":           SeverityError,
		"stale-suppression":          SeverityWarning,
	}
}

// ResolveSeverity applies the config's severity override for a rule.
// Returns the resolved severity and whether the finding should be included.
func ResolveSeverity(rule string, defaultSev Severity, cfg *Config) (Severity, bool) {
	if configured, ok := cfg.Rules[rule]; ok {
		if configured == SeverityOff {
			return "", false
		}
		return configured, true
	}
	return defaultSev, true
}

// MatchesIgnoreExport checks if a symbol name matches any ignore export pattern.
func MatchesIgnoreExport(name string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, name); matched {
			return true
		}
	}
	return false
}

// Analyzer is the interface all analyzers implement.
type Analyzer interface {
	Name() string
	Description() string
	Analyze(project *Project, cfg *Config) (*Result, error)
}
