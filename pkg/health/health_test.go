package health

import (
	"testing"

	"github.com/krait-go/krait/pkg/analyzer"
)

// makeResult is a helper that builds an analyzer.Result with the given stats
// and findings. Pass nil for findings when only stats are relevant.
func makeResult(name string, stats map[string]any, findings []*analyzer.Finding) *analyzer.Result {
	if findings == nil {
		findings = []*analyzer.Finding{}
	}
	return &analyzer.Result{
		Analyzer: name,
		Stats:    stats,
		Findings: findings,
	}
}

// TestHealth_PerfectScore verifies that all-zero signal inputs produce a score
// of 100 and a grade of "A".
func TestHealth_PerfectScore(t *testing.T) {
	results := []*analyzer.Result{
		makeResult("dead-code", map[string]any{
			"unused_percentage": float64(0),
		}, nil),
		makeResult("duplication", map[string]any{
			"duplication_percentage": float64(0),
		}, nil),
		makeResult("complexity", map[string]any{
			"total_functions": float64(10),
			"over_threshold":  float64(0),
		}, nil),
		makeResult("architecture", nil, []*analyzer.Finding{}),
		makeResult("dependency", map[string]any{
			"unused_dependencies": float64(0),
			"unlisted_imports":    float64(0),
		}, nil),
	}

	cfg := analyzer.DefaultConfig()
	h := New()
	result, err := h.PostAnalyze(nil, cfg, results)
	if err != nil {
		t.Fatalf("PostAnalyze returned error: %v", err)
	}

	score, ok := result.Stats["health_score"].(int)
	if !ok {
		t.Fatalf("health_score stat is not int, got %T: %v", result.Stats["health_score"], result.Stats["health_score"])
	}
	if score != 100 {
		t.Errorf("expected score 100, got %d", score)
	}

	grade, ok := result.Stats["health_grade"].(string)
	if !ok {
		t.Fatalf("health_grade stat is not string")
	}
	if grade != "A" {
		t.Errorf("expected grade A, got %q", grade)
	}

	if len(result.Findings) != 0 {
		t.Errorf("expected no findings for perfect score, got %d", len(result.Findings))
	}
}

// TestHealth_DegradedScore verifies that mixed non-zero signals produce a score
// strictly between 1 and 99, and that signal_scores are populated.
func TestHealth_DegradedScore(t *testing.T) {
	archFinding := &analyzer.Finding{
		Rule:     "layer-violation",
		Category: analyzer.CategoryArchitecture,
		Severity: analyzer.SeverityError,
		Message:  "layer violation",
	}

	results := []*analyzer.Result{
		makeResult("dead-code", map[string]any{
			"unused_percentage": float64(5), // score = 100 - 5*5 = 75
		}, nil),
		makeResult("duplication", map[string]any{
			"duplication_percentage": float64(8), // score = 100 - 8*5 = 60
		}, nil),
		makeResult("complexity", map[string]any{
			"total_functions": float64(20),
			"over_threshold":  float64(4), // 20% → score = 100 - 20*10 = -100 → clamped 0
		}, nil),
		makeResult("architecture", nil, []*analyzer.Finding{archFinding}), // 1 violation → score = 100 - 1*5 = 95
		makeResult("dependency", map[string]any{
			"unused_dependencies": float64(1),
			"unlisted_imports":    float64(1), // 2 issues → score = 100 - 2*10 = 80
		}, nil),
	}

	cfg := analyzer.DefaultConfig()
	h := New()
	result, err := h.PostAnalyze(nil, cfg, results)
	if err != nil {
		t.Fatalf("PostAnalyze returned error: %v", err)
	}

	score, ok := result.Stats["health_score"].(int)
	if !ok {
		t.Fatalf("health_score stat is not int, got %T", result.Stats["health_score"])
	}
	if score < 1 || score > 99 {
		t.Errorf("expected degraded score in [1,99], got %d", score)
	}

	signalScores, ok := result.Stats["signal_scores"].(map[string]any)
	if !ok {
		t.Fatalf("signal_scores is not map[string]any")
	}
	for _, key := range []string{"dead_code", "duplication", "complexity", "architecture", "dependencies"} {
		if _, present := signalScores[key]; !present {
			t.Errorf("signal_scores missing key %q", key)
		}
	}
}

// TestHealth_MinScoreThreshold verifies that a score below cfg.MinHealthScore
// causes exactly one "low-health-score" finding with error severity.
func TestHealth_MinScoreThreshold(t *testing.T) {
	// Drive all signals to worst case so score ≈ 0.
	results := []*analyzer.Result{
		makeResult("dead-code", map[string]any{
			"unused_percentage": float64(100), // score = 100 - 500 → clamped 0
		}, nil),
		makeResult("duplication", map[string]any{
			"duplication_percentage": float64(100), // score 0
		}, nil),
		makeResult("complexity", map[string]any{
			"total_functions": float64(10),
			"over_threshold":  float64(10), // 100% → score 0
		}, nil),
		makeResult("architecture", nil, []*analyzer.Finding{
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
			{Rule: "layer-violation", Category: analyzer.CategoryArchitecture, Severity: analyzer.SeverityError},
		}),
		makeResult("dependency", map[string]any{
			"unused_dependencies": float64(10),
			"unlisted_imports":    float64(10), // score 0
		}, nil),
	}

	cfg := analyzer.DefaultConfig()
	cfg.MinHealthScore = 80

	h := New()
	result, err := h.PostAnalyze(nil, cfg, results)
	if err != nil {
		t.Fatalf("PostAnalyze returned error: %v", err)
	}

	score := result.Stats["health_score"].(int)
	if score >= 80 {
		t.Fatalf("expected score below 80 to trigger finding, got %d", score)
	}

	var lowHealthFinding *analyzer.Finding
	for _, f := range result.Findings {
		if f.Rule == "low-health-score" {
			lowHealthFinding = f
			break
		}
	}
	if lowHealthFinding == nil {
		t.Fatalf("expected a low-health-score finding, got none (score=%d)", score)
	}
	if lowHealthFinding.Severity != analyzer.SeverityError {
		t.Errorf("expected severity error, got %q", lowHealthFinding.Severity)
	}
	if lowHealthFinding.Category != analyzer.CategoryHealth {
		t.Errorf("expected category health, got %q", lowHealthFinding.Category)
	}
}

// TestHealth_WeightNormalization verifies that equal weights summing to 50
// (not 100) still produce a perfect score of 100 when all signals are zero.
// This confirms weights are normalized before scoring, not used raw.
func TestHealth_WeightNormalization(t *testing.T) {
	results := []*analyzer.Result{
		makeResult("dead-code", map[string]any{
			"unused_percentage": float64(0),
		}, nil),
		makeResult("duplication", map[string]any{
			"duplication_percentage": float64(0),
		}, nil),
		makeResult("complexity", map[string]any{
			"total_functions": float64(5),
			"over_threshold":  float64(0),
		}, nil),
		makeResult("architecture", nil, []*analyzer.Finding{}),
		makeResult("dependency", map[string]any{
			"unused_dependencies": float64(0),
			"unlisted_imports":    float64(0),
		}, nil),
	}

	cfg := analyzer.DefaultConfig()
	// Equal weights summing to 50 (not the default 100).
	cfg.HealthWeights = &analyzer.HealthWeights{
		DeadCode:     10,
		Duplication:  10,
		Complexity:   10,
		Architecture: 10,
		Dependencies: 10,
	}

	h := New()
	result, err := h.PostAnalyze(nil, cfg, results)
	if err != nil {
		t.Fatalf("PostAnalyze returned error: %v", err)
	}

	score, ok := result.Stats["health_score"].(int)
	if !ok {
		t.Fatalf("health_score is not int")
	}
	if score != 100 {
		t.Errorf("expected score 100 with equal normalized weights and perfect inputs, got %d", score)
	}

	grade := result.Stats["health_grade"].(string)
	if grade != "A" {
		t.Errorf("expected grade A, got %q", grade)
	}
}
