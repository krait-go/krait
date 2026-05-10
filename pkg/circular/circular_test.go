package circular

import (
	"testing"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

func TestCircular_DetectsCycle(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project, err := parser.Parse("../../testdata/circular", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Findings) == 0 {
		t.Fatal("expected at least one circular-dependency finding; got none")
	}

	// Verify the finding has the expected rule and category.
	for _, f := range result.Findings {
		if f.Rule != "circular-dependency" {
			t.Errorf("unexpected rule %q; want circular-dependency", f.Rule)
		}
		if f.Category != analyzer.CategoryCircular {
			t.Errorf("unexpected category %q; want %q", f.Category, analyzer.CategoryCircular)
		}
	}

	// Find a finding with a 3-node cycle (a -> b -> c -> a).
	found3 := false
	for _, f := range result.Findings {
		cycleLen, ok := f.Meta["cycle_length"].(int)
		if !ok {
			continue
		}
		if cycleLen == 3 {
			found3 = true
			break
		}
	}
	if !found3 {
		t.Errorf("expected a cycle of length 3; findings: %+v", result.Findings)
	}
}

func TestCircular_NoCycle(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project, err := parser.Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Findings) != 0 {
		t.Errorf("expected no circular-dependency findings; got %d: %+v", len(result.Findings), result.Findings)
	}
}

func TestCircular_Name(t *testing.T) {
	a := New()
	if a.Name() != "circular" {
		t.Errorf("Name() = %q, want %q", a.Name(), "circular")
	}
}

func TestCircular_Description(t *testing.T) {
	a := New()
	if a.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestCircular_Stats(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project, err := parser.Parse("../../testdata/circular", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	totalCycles, ok := result.Stats["total_cycles"].(int)
	if !ok {
		t.Fatal("stats missing total_cycles key or wrong type")
	}
	if totalCycles < 1 {
		t.Errorf("total_cycles = %d, want >= 1", totalCycles)
	}

	longestCycle, ok := result.Stats["longest_cycle_length"].(int)
	if !ok {
		t.Fatal("stats missing longest_cycle_length key or wrong type")
	}
	if longestCycle < 3 {
		t.Errorf("longest_cycle_length = %d, want >= 3", longestCycle)
	}

	packagesInCycles, ok := result.Stats["packages_in_cycles"].(int)
	if !ok {
		t.Fatal("stats missing packages_in_cycles key or wrong type")
	}
	if packagesInCycles < 3 {
		t.Errorf("packages_in_cycles = %d, want >= 3", packagesInCycles)
	}
}

func TestCircular_RuleOff_NoFindings(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.Rules["circular-dependency"] = analyzer.SeverityOff

	project, err := parser.Parse("../../testdata/circular", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Findings) != 0 {
		t.Errorf("expected no findings when rule is off; got %d", len(result.Findings))
	}
}

func TestCircular_FindingLocation(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project, err := parser.Parse("../../testdata/circular", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	for _, f := range result.Findings {
		if f.Location.File != "go.mod" {
			t.Errorf("expected location file to be go.mod; got %q", f.Location.File)
		}
		if f.Location.Line != 1 {
			t.Errorf("expected location line to be 1; got %d", f.Location.Line)
		}
	}
}
