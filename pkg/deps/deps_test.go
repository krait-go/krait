package deps

import (
	"testing"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

func TestDepsAnalyzer_UnusedDependency(t *testing.T) {
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

	found := false
	for _, f := range result.Findings {
		if f.Rule == "unused-dependency" {
			if dep, ok := f.Meta["dependency"]; ok && dep == "github.com/unused/dep" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("expected unused-dependency finding for github.com/unused/dep; got findings: %+v", result.Findings)
	}
}

func TestDepsAnalyzer_FindingsHaveCorrectRule(t *testing.T) {
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

	for _, f := range result.Findings {
		if f.Rule != "unused-dependency" && f.Rule != "unlisted-dependency" {
			t.Errorf("unexpected rule %q in findings", f.Rule)
		}
	}
}

func TestDepsAnalyzer_TotalDependenciesStat(t *testing.T) {
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

	total, ok := result.Stats["total_dependencies"]
	if !ok {
		t.Fatal("stats missing total_dependencies key")
	}
	totalInt, ok := total.(int)
	if !ok {
		t.Fatalf("total_dependencies is not int, got %T", total)
	}
	if totalInt != 1 {
		t.Errorf("total_dependencies = %d, want 1", totalInt)
	}
}

func TestDepsAnalyzer_Name(t *testing.T) {
	a := New()
	if a.Name() != "dependency" {
		t.Errorf("Name() = %q, want %q", a.Name(), "dependency")
	}
}

func TestDepsAnalyzer_Description(t *testing.T) {
	a := New()
	if a.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestIsStdlib(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"fmt", true},
		{"net/http", true},
		{"encoding/json", true},
		{"github.com/foo/bar", false},
		{"golang.org/x/tools", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got := isStdlib(tc.path)
			if got != tc.want {
				t.Errorf("isStdlib(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}
