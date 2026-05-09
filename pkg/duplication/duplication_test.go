package duplication

import (
	"testing"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

func dupCfg() *analyzer.Config {
	cfg := analyzer.DefaultConfig()
	// Lower threshold so the 4-statement function bodies in testdata/simple are detected.
	cfg.MinDuplicateLines = 4
	return cfg
}

func TestDuplication_FindsAtLeastOneCloneGroup(t *testing.T) {
	cfg := dupCfg()
	project, err := parser.Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Findings) < 1 {
		t.Errorf("expected at least 1 duplication finding; got 0")
	}
}

func TestDuplication_CloneCountAtLeastTwo(t *testing.T) {
	cfg := dupCfg()
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
		if f.Rule != "code-duplication" {
			continue
		}
		cloneCount, ok := f.Meta["clone_count"]
		if !ok {
			t.Errorf("finding missing clone_count in meta: %+v", f)
			continue
		}
		count, ok := cloneCount.(int)
		if !ok {
			t.Errorf("clone_count is not int, got %T", cloneCount)
			continue
		}
		if count < 2 {
			t.Errorf("clone_count = %d, want >= 2", count)
		}
	}
}

func TestDuplication_FindingReferencesBothFiles(t *testing.T) {
	cfg := dupCfg()
	project, err := parser.Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	// helpers.go and other.go have the same structure — look for a finding
	// that references both files (primary location + related location).
	foundBothFiles := false
	for _, f := range result.Findings {
		if f.Rule != "code-duplication" {
			continue
		}
		files := map[string]bool{f.Location.File: true}
		for _, rel := range f.RelatedLocations {
			files[rel.File] = true
		}
		if len(files) >= 2 {
			foundBothFiles = true
			break
		}
	}
	if !foundBothFiles {
		t.Errorf("expected a duplication finding referencing at least 2 different files; findings: %+v", result.Findings)
	}
}

func TestDuplication_Name(t *testing.T) {
	a := New()
	if a.Name() != "duplication" {
		t.Errorf("Name() = %q, want %q", a.Name(), "duplication")
	}
}

func TestDuplication_Description(t *testing.T) {
	a := New()
	if a.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestDeduplicateOverlapping_RemovesOverlaps(t *testing.T) {
	windows := []*stmtWindow{
		{File: "a.go", StartLine: 1, EndLine: 10},
		{File: "a.go", StartLine: 5, EndLine: 15},
		{File: "a.go", StartLine: 20, EndLine: 25},
	}
	result := deduplicateOverlapping(windows)
	if len(result) != 2 {
		t.Errorf("deduplicateOverlapping: got %d windows, want 2", len(result))
	}
	// First merged window should span 1-15.
	if result[0].StartLine != 1 || result[0].EndLine != 15 {
		t.Errorf("merged window: start=%d end=%d, want start=1 end=15", result[0].StartLine, result[0].EndLine)
	}
}

func TestFilterSelfSimilar_RemovesSameFunction(t *testing.T) {
	groups := []*cloneGroup{
		{
			Windows: []*stmtWindow{
				{File: "a.go", FuncName: "Foo"},
				{File: "a.go", FuncName: "Foo"},
			},
		},
		{
			Windows: []*stmtWindow{
				{File: "a.go", FuncName: "Foo"},
				{File: "b.go", FuncName: "Bar"},
			},
		},
	}
	result := filterSelfSimilar(groups)
	if len(result) != 1 {
		t.Errorf("filterSelfSimilar: got %d groups, want 1", len(result))
	}
}
