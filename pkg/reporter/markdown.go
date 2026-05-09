package reporter

import (
	"fmt"
	"io"
	"strings"

	"github.com/krait-go/krait/pkg/analyzer"
)

func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "`", "\\`")
	return s
}

func formatMarkdown(w io.Writer, report *analyzer.Report) error {
	if err := writeMarkdownHeader(w, report); err != nil {
		return err
	}
	for _, res := range report.Results {
		if err := writeMarkdownSection(w, res); err != nil {
			return err
		}
	}
	return nil
}

// writeMarkdownHeader writes the report title, summary line, and severity table.
func writeMarkdownHeader(w io.Writer, report *analyzer.Report) error {
	s := report.Summary
	errors := s.BySeverity[analyzer.SeverityError]
	warnings := s.BySeverity[analyzer.SeverityWarning]
	info := s.BySeverity[analyzer.SeverityInfo]

	if _, err := fmt.Fprintln(w, "# Krait Report"); err != nil {
		return fmt.Errorf("writing title: %w", err)
	}
	if _, err := fmt.Fprintf(w, "\n**%d findings** | %s\n\n", s.TotalFindings, report.TotalDuration); err != nil {
		return fmt.Errorf("writing summary line: %w", err)
	}
	if _, err := fmt.Fprint(w, "| Severity | Count |\n|---|---|\n"); err != nil {
		return fmt.Errorf("writing severity table header: %w", err)
	}
	if _, err := fmt.Fprintf(w, "| error | %d |\n", errors); err != nil {
		return fmt.Errorf("writing error count: %w", err)
	}
	if _, err := fmt.Fprintf(w, "| warning | %d |\n", warnings); err != nil {
		return fmt.Errorf("writing warning count: %w", err)
	}
	if _, err := fmt.Fprintf(w, "| info | %d |\n\n", info); err != nil {
		return fmt.Errorf("writing info count: %w", err)
	}
	return nil
}

// writeMarkdownSection writes one analyzer's heading and findings table.
func writeMarkdownSection(w io.Writer, res *analyzer.Result) error {
	if _, err := fmt.Fprintf(w, "## %s\n\n", res.Analyzer); err != nil {
		return fmt.Errorf("writing section heading for %s: %w", res.Analyzer, err)
	}
	if len(res.Findings) == 0 {
		if _, err := fmt.Fprintf(w, "No issues found.\n\n"); err != nil {
			return fmt.Errorf("writing no issues for %s: %w", res.Analyzer, err)
		}
		return nil
	}
	if _, err := fmt.Fprint(w, "| Severity | Location | Message |\n|---|---|---|\n"); err != nil {
		return fmt.Errorf("writing findings table header for %s: %w", res.Analyzer, err)
	}
	for _, f := range res.Findings {
		if _, err := fmt.Fprintf(w, "| %s | `%s:%d` | %s |\n",
			string(f.Severity), escapeMarkdown(f.Location.File), f.Location.Line, escapeMarkdown(f.Message)); err != nil {
			return fmt.Errorf("writing finding row: %w", err)
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("writing section separator: %w", err)
	}
	return nil
}
