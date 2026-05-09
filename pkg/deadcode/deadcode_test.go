package deadcode

import (
	"testing"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

func parseSimple(t *testing.T) *analyzer.Project {
	t.Helper()
	cfg := analyzer.DefaultConfig()
	project, err := parser.Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	return project
}

func hasFindingForSymbol(findings []*analyzer.Finding, rule, symbol string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			if sym, ok := f.Meta["symbol"]; ok && sym == symbol {
				return true
			}
		}
	}
	return false
}

func TestDeadCode_FindsUnusedFunc(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project := parseSimple(t)

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if !hasFindingForSymbol(result.Findings, "unused-export-func", "UnusedFunc") {
		t.Errorf("expected finding for UnusedFunc; findings: %+v", result.Findings)
	}
}

func TestDeadCode_FindsUnusedMethod(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project := parseSimple(t)

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if !hasFindingForSymbol(result.Findings, "unused-export-method", "UnusedMethod") {
		t.Errorf("expected finding for UnusedMethod; findings: %+v", result.Findings)
	}
}

func TestDeadCode_DoesNotFlagUsedFunc(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project := parseSimple(t)

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if hasFindingForSymbol(result.Findings, "unused-export-func", "UsedFunc") {
		t.Error("UsedFunc should not be flagged as unused (it is called from main)")
	}
}

func TestDeadCode_DoesNotFlagNewUser(t *testing.T) {
	// NewUser matches the IgnoreExports "New*" pattern — must not be flagged.
	cfg := analyzer.DefaultConfig()
	project := parseSimple(t)

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	if hasFindingForSymbol(result.Findings, "unused-export-func", "NewUser") {
		t.Error("NewUser should not be flagged due to IgnoreExports 'New*' pattern")
	}
}

func TestDeadCode_RuleOff_NoUnusedFuncFindings(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.Rules["unused-export-func"] = analyzer.SeverityOff
	project := parseSimple(t)

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	for _, f := range result.Findings {
		if f.Rule == "unused-export-func" {
			t.Errorf("expected no unused-export-func findings when rule is off, got: %+v", f)
		}
	}
}

func TestDeadCode_CustomIgnorePattern_UnusedStar(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	// Adding "Unused*" to IgnoreExports should suppress UnusedFunc and UnusedMethod findings.
	cfg.IgnoreExports = append(cfg.IgnoreExports, "Unused*")
	project := parseSimple(t)

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	for _, f := range result.Findings {
		if sym, ok := f.Meta["symbol"]; ok {
			symStr, _ := sym.(string)
			if symStr == "UnusedFunc" || symStr == "UnusedMethod" {
				t.Errorf("symbol %q should be suppressed by Unused* pattern, but got finding: %+v", symStr, f)
			}
		}
	}
}

func TestDeadCode_Name(t *testing.T) {
	a := New()
	if a.Name() != "dead-code" {
		t.Errorf("Name() = %q, want %q", a.Name(), "dead-code")
	}
}

func TestDeadCode_Description(t *testing.T) {
	a := New()
	if a.Description() == "" {
		t.Error("Description() returned empty string")
	}
}
