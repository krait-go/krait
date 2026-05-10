package unusedfiles

import (
	"testing"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

// TestUnusedFiles_DetectsOrphan verifies that a package with no importers is
// reported as unused. testdata/unused-pkg contains an "orphan" package that is
// never imported by the main package.
func TestUnusedFiles_DetectsOrphan(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project, err := parser.Parse("../../testdata/unused-pkg", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Findings) == 0 {
		t.Fatal("expected at least one finding for the orphan package, got none")
	}

	// Every finding must reference the orphan package.
	for _, f := range result.Findings {
		if f.Rule != "unused-file" {
			t.Errorf("unexpected rule %q; want %q", f.Rule, "unused-file")
		}
		pkgVal, ok := f.Meta["rel_path"]
		if !ok {
			t.Errorf("finding missing rel_path meta: %+v", f)
			continue
		}
		relPath, ok := pkgVal.(string)
		if !ok {
			t.Errorf("rel_path meta is not a string: %+v", f.Meta)
			continue
		}
		// The orphan package lives at pkg/orphan relative to the testdata root.
		if relPath != "pkg/orphan" {
			t.Errorf("expected finding for pkg/orphan, got rel_path=%q", relPath)
		}
	}

	// Verify stats are populated.
	unusedPkgs, ok := result.Stats["unused_packages"].(int)
	if !ok || unusedPkgs == 0 {
		t.Errorf("expected unused_packages > 0 in stats, got %v", result.Stats["unused_packages"])
	}
}

// TestUnusedFiles_CleanProject verifies that a project where all packages are
// reachable from an entry point produces zero findings.
func TestUnusedFiles_CleanProject(t *testing.T) {
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
		t.Errorf("unexpected finding in clean project: rule=%q file=%q message=%q",
			f.Rule, f.Location.File, f.Message)
	}
}

func TestUnusedFiles_Name(t *testing.T) {
	a := New()
	if a.Name() != "unused-files" {
		t.Errorf("Name() = %q, want %q", a.Name(), "unused-files")
	}
}

func TestUnusedFiles_Description(t *testing.T) {
	a := New()
	if a.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestUnusedFiles_RuleOff_NoFindings(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.Rules["unused-file"] = analyzer.SeverityOff
	project, err := parser.Parse("../../testdata/unused-pkg", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if len(result.Findings) != 0 {
		t.Errorf("expected no findings when rule is off, got %d: %+v", len(result.Findings), result.Findings)
	}

	// Stats should still reflect the unused packages even when rule is off.
	unusedPkgs, ok := result.Stats["unused_packages"].(int)
	if !ok || unusedPkgs == 0 {
		t.Errorf("expected unused_packages > 0 in stats even when rule is off, got %v", result.Stats["unused_packages"])
	}
}
