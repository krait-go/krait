package reporter

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/krait-go/krait/pkg/analyzer"
)

func minimalReport() *analyzer.Report {
	return &analyzer.Report{
		Version:       "0.1.0",
		Timestamp:     "2024-01-01T00:00:00Z",
		RootDir:       "/test",
		TotalDuration: "100ms",
		Summary: analyzer.ReportSummary{
			TotalFindings: 1,
			BySeverity: map[analyzer.Severity]int{
				analyzer.SeverityWarning: 1,
			},
			ByCategory: map[analyzer.Category]int{
				analyzer.CategoryDeadCode: 1,
			},
		},
		Results: []*analyzer.Result{
			{
				Analyzer:   "dead-code",
				DurationMs: 5,
				Findings: []*analyzer.Finding{
					{
						Rule:     "unused-export-func",
						Category: analyzer.CategoryDeadCode,
						Severity: analyzer.SeverityWarning,
						Message:  "Exported func UnusedFunc is never referenced outside its package",
						Location: analyzer.Location{
							File: "pkg/exported.go",
							Line: 11,
						},
					},
				},
			},
		},
	}
}

func TestFormat_JSON_NonEmpty(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "json"); err != nil {
		t.Fatalf("Format json returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("json output is empty")
	}
}

func TestFormat_JSON_ValidUnmarshal(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "json"); err != nil {
		t.Fatalf("Format json returned error: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json output is not valid JSON: %v\nOutput:\n%s", err, buf.String())
	}
}

func TestFormat_SARIF_NonEmpty(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "sarif"); err != nil {
		t.Fatalf("Format sarif returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("sarif output is empty")
	}
}

func TestFormat_SARIF_CorrectVersion(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "sarif"); err != nil {
		t.Fatalf("Format sarif returned error: %v", err)
	}

	var sarifOut map[string]any
	if err := json.Unmarshal(buf.Bytes(), &sarifOut); err != nil {
		t.Fatalf("sarif output is not valid JSON: %v", err)
	}

	version, ok := sarifOut["version"]
	if !ok {
		t.Fatal("sarif output missing 'version' field")
	}
	if version != "2.1.0" {
		t.Errorf("sarif version = %q, want %q", version, "2.1.0")
	}
}

func TestFormat_Text_NonEmpty(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "text"); err != nil {
		t.Fatalf("Format text returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("text output is empty")
	}
}

func TestFormat_Text_ContainsFindings(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "text"); err != nil {
		t.Fatalf("Format text returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "UnusedFunc") {
		t.Errorf("text output does not mention UnusedFunc; output:\n%s", out)
	}
}

func TestFormat_Markdown_NonEmpty(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "markdown"); err != nil {
		t.Fatalf("Format markdown returned error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("markdown output is empty")
	}
}

func TestFormat_Markdown_ContainsHeader(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	if err := Format(&buf, report, "markdown"); err != nil {
		t.Fatalf("Format markdown returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "# Krait Report") {
		t.Errorf("markdown output missing '# Krait Report'; output:\n%s", out)
	}
}

func TestFormat_UnknownFormat_ReturnsError(t *testing.T) {
	var buf bytes.Buffer
	report := minimalReport()
	err := Format(&buf, report, "xml")
	if err == nil {
		t.Error("expected error for unknown format 'xml', got nil")
	}
}
