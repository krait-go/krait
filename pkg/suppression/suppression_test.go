package suppression

import (
	"testing"

	"github.com/krait-go/krait/pkg/analyzer"
)

// helpers

func makeFinding(file, rule string, line int) *analyzer.Finding {
	return &analyzer.Finding{
		Rule:     rule,
		Category: analyzer.CategoryDeadCode,
		Severity: analyzer.SeverityWarning,
		Message:  "test finding",
		Location: analyzer.Location{File: file, Line: line},
	}
}

func hasRule(findings []*analyzer.Finding, rule string) bool {
	for _, f := range findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// TestFilter_SuppressesMatchingRule verifies that a line-level suppression for a
// specific rule removes only findings for that rule on the covered lines.
func TestFilter_SuppressesMatchingRule(t *testing.T) {
	m := NewMap()
	// Directive on line 10; finding on line 11 — within the window.
	m.Add(Suppression{File: "foo.go", Rule: "unused-export-func", Line: 10})

	findings := []*analyzer.Finding{
		makeFinding("foo.go", "unused-export-func", 11),
		makeFinding("foo.go", "high-cyclomatic-complexity", 11),
	}

	filtered, stale := m.Filter(findings)

	if hasRule(filtered, "unused-export-func") {
		t.Error("unused-export-func should be suppressed but was not filtered out")
	}
	if !hasRule(filtered, "high-cyclomatic-complexity") {
		t.Error("high-cyclomatic-complexity should not be suppressed but was filtered out")
	}
	if len(stale) != 0 {
		t.Errorf("expected no stale suppressions, got %d", len(stale))
	}
}

// TestFilter_WildcardSuppressesAll verifies that a wildcard ("*") suppression
// removes all findings whose line falls within the window.
func TestFilter_WildcardSuppressesAll(t *testing.T) {
	m := NewMap()
	// Wildcard — no rule token in the directive.
	m.Add(Suppression{File: "bar.go", Rule: "*", Line: 5})

	findings := []*analyzer.Finding{
		makeFinding("bar.go", "unused-export-func", 6),
		makeFinding("bar.go", "code-duplication", 8),
		// Finding on line 26 is outside the 20-line window (5+20=25, so line 26 is out).
		makeFinding("bar.go", "unused-export-type", 26),
	}

	filtered, _ := m.Filter(findings)

	for _, f := range filtered {
		if f.Location.Line <= 25 {
			t.Errorf("finding %q on line %d should be suppressed by wildcard", f.Rule, f.Location.Line)
		}
	}
	if !hasRule(filtered, "unused-export-type") {
		t.Error("finding on line 26 (outside window) should not be suppressed")
	}
}

// TestFilter_FileLevelSuppression verifies that //krait:ignore-file suppressions
// remove all matching-rule findings regardless of line number.
func TestFilter_FileLevelSuppression(t *testing.T) {
	m := NewMap()
	m.AddFileLevel(Suppression{File: "legacy.go", Rule: "unused-export-func", Line: 1})

	findings := []*analyzer.Finding{
		makeFinding("legacy.go", "unused-export-func", 100),
		makeFinding("legacy.go", "unused-export-func", 500),
		// Different rule — must not be suppressed.
		makeFinding("legacy.go", "code-duplication", 200),
		// Different file — must not be suppressed.
		makeFinding("other.go", "unused-export-func", 10),
	}

	filtered, stale := m.Filter(findings)

	for _, f := range filtered {
		if f.Location.File == "legacy.go" && f.Rule == "unused-export-func" {
			t.Errorf("legacy.go unused-export-func on line %d should be suppressed by file-level directive", f.Location.Line)
		}
	}
	if !hasRule(filtered, "code-duplication") {
		t.Error("code-duplication in legacy.go should not be suppressed")
	}
	if !hasRule(filtered, "unused-export-func") {
		// other.go's finding should still be present.
		t.Error("unused-export-func in other.go should not be suppressed")
	}
	if len(stale) != 0 {
		t.Errorf("expected no stale suppressions, got %d", len(stale))
	}
}

// TestFilter_DetectsStaleSuppression verifies that suppressions that never
// match any finding produce stale-suppression findings.
func TestFilter_DetectsStaleSuppression(t *testing.T) {
	m := NewMap()
	// This suppression will never match because there are no findings for baz.go.
	m.Add(Suppression{File: "baz.go", Rule: "unused-export-func", Line: 7})

	findings := []*analyzer.Finding{
		makeFinding("other.go", "unused-export-func", 8),
	}

	filtered, stale := m.Filter(findings)

	if len(filtered) != 1 {
		t.Errorf("expected 1 filtered finding, got %d", len(filtered))
	}
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale-suppression finding, got %d", len(stale))
	}
	if stale[0].Rule != "stale-suppression" {
		t.Errorf("stale finding rule = %q, want %q", stale[0].Rule, "stale-suppression")
	}
	if stale[0].Category != analyzer.CategorySuppression {
		t.Errorf("stale finding category = %q, want %q", stale[0].Category, analyzer.CategorySuppression)
	}
	if stale[0].Location.File != "baz.go" {
		t.Errorf("stale finding file = %q, want %q", stale[0].Location.File, "baz.go")
	}
	if stale[0].Location.Line != 7 {
		t.Errorf("stale finding line = %d, want 7", stale[0].Location.Line)
	}
}

// TestBuildMap_ParsesComments verifies that BuildMapFromComments correctly
// parses all three directive forms.
func TestBuildMap_ParsesComments(t *testing.T) {
	comments := map[string][]RawComment{
		"a.go": {
			// Specific rule with reason.
			{Line: 3, Text: "//krait:ignore unused-export-func -- Used via reflection"},
			// Wildcard (no rule).
			{Line: 9, Text: "//krait:ignore"},
			// File-level directive.
			{Line: 1, Text: "//krait:ignore-file unused-export-type -- Legacy API"},
			// Non-directive comment — must be ignored.
			{Line: 5, Text: "// normal comment"},
		},
	}

	m := BuildMapFromComments(comments)

	if m.LineCount() != 2 {
		t.Errorf("LineCount() = %d, want 2", m.LineCount())
	}
	if m.FileCount() != 1 {
		t.Errorf("FileCount() = %d, want 1", m.FileCount())
	}

	// Verify the specific-rule line suppression.
	lineSupps := m.byFileLine["a.go"]
	var found3 bool
	for _, s := range lineSupps {
		if s.Line == 3 {
			found3 = true
			if s.Rule != "unused-export-func" {
				t.Errorf("line 3 rule = %q, want %q", s.Rule, "unused-export-func")
			}
			if s.Reason != "Used via reflection" {
				t.Errorf("line 3 reason = %q, want %q", s.Reason, "Used via reflection")
			}
		}
	}
	if !found3 {
		t.Error("expected line-level suppression at line 3")
	}

	// Verify the wildcard line suppression.
	var found9 bool
	for _, s := range lineSupps {
		if s.Line == 9 {
			found9 = true
			if s.Rule != "*" {
				t.Errorf("line 9 rule = %q, want %q", s.Rule, "*")
			}
		}
	}
	if !found9 {
		t.Error("expected wildcard line-level suppression at line 9")
	}

	// Verify the file-level suppression.
	fileSupps := m.byFile["a.go"]
	if len(fileSupps) != 1 {
		t.Fatalf("expected 1 file-level suppression, got %d", len(fileSupps))
	}
	fs := fileSupps[0]
	if fs.Rule != "unused-export-type" {
		t.Errorf("file suppression rule = %q, want %q", fs.Rule, "unused-export-type")
	}
	if fs.Reason != "Legacy API" {
		t.Errorf("file suppression reason = %q, want %q", fs.Reason, "Legacy API")
	}
}
