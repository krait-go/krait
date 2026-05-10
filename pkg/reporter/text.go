package reporter

import (
	"fmt"
	"io"

	"github.com/krait-go/krait/pkg/analyzer"
)

const maxFindingsPerSection = 20

func severityIcon(sev analyzer.Severity) string {
	switch sev {
	case analyzer.SeverityError:
		return "[ERR]"
	case analyzer.SeverityWarning:
		return "[WRN]"
	default:
		return "[INF]"
	}
}

func formatText(w io.Writer, report *analyzer.Report) error {
	if err := writeTextHeader(w, report); err != nil {
		return err
	}
	for _, res := range report.Results {
		if res.Analyzer == "health" {
			continue
		}
		if err := writeTextSection(w, res); err != nil {
			return err
		}
	}
	for _, res := range report.Results {
		if res.Analyzer == "health" {
			return writeHealthSection(w, res)
		}
	}
	return nil
}

// writeTextHeader writes the report title and overall summary line.
func writeTextHeader(w io.Writer, report *analyzer.Report) error {
	s := report.Summary
	errors := s.BySeverity[analyzer.SeverityError]
	warnings := s.BySeverity[analyzer.SeverityWarning]
	info := s.BySeverity[analyzer.SeverityInfo]

	if _, err := fmt.Fprintf(w, "krait v%s — %s\n\n", report.Version, report.TotalDuration); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}
	if _, err := fmt.Fprintf(w, "Found %d issues (%d errors, %d warnings, %d info)\n\n",
		s.TotalFindings, errors, warnings, info); err != nil {
		return fmt.Errorf("writing summary: %w", err)
	}
	return nil
}

// writeHealthSection writes the health score summary block at the end of the report.
func writeHealthSection(w io.Writer, res *analyzer.Result) error {
	score, _ := res.Stats["health_score"].(int)
	grade, _ := res.Stats["health_grade"].(string)
	if _, err := fmt.Fprintf(w, "Health Score: %d/100 (%s)\n", score, grade); err != nil {
		return err
	}
	signals, ok := res.Stats["signal_scores"].(map[string]any)
	if !ok {
		return nil
	}
	order := []struct{ key, label string }{
		{"dead_code", "Dead code"},
		{"duplication", "Duplication"},
		{"complexity", "Complexity"},
		{"architecture", "Architecture"},
		{"dependencies", "Dependencies"},
	}
	for _, o := range order {
		sig, ok := signals[o.key].(map[string]any)
		if !ok {
			continue
		}
		sigScore := toInt(sig["score"])
		raw := sig["raw"]
		var rawStr string
		switch v := raw.(type) {
		case float64:
			rawStr = fmt.Sprintf("%.1f", v)
		default:
			rawStr = fmt.Sprintf("%v", v)
		}
		if _, err := fmt.Fprintf(w, "  %-14s %3d  (%s)\n", o.label+":", sigScore, rawStr); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	default:
		return 0
	}
}

// writeTextSection writes a single analyzer result block, including all findings.
func writeTextSection(w io.Writer, res *analyzer.Result) error {
	if len(res.Findings) == 0 {
		if _, err := fmt.Fprintf(w, "=== %s (no issues found) ===\n\n", res.Analyzer); err != nil {
			return fmt.Errorf("writing section header for %s: %w", res.Analyzer, err)
		}
		return nil
	}

	if _, err := fmt.Fprintf(w, "=== %s (%d findings, %dms) ===\n\n",
		res.Analyzer, len(res.Findings), res.DurationMs); err != nil {
		return fmt.Errorf("writing section header for %s: %w", res.Analyzer, err)
	}

	limit := len(res.Findings)
	truncated := 0
	if limit > maxFindingsPerSection {
		truncated = limit - maxFindingsPerSection
		limit = maxFindingsPerSection
	}

	for _, f := range res.Findings[:limit] {
		if _, err := fmt.Fprintf(w, "  %s %s:%d: %s\n",
			severityIcon(f.Severity), f.Location.File, f.Location.Line, f.Message); err != nil {
			return fmt.Errorf("writing finding: %w", err)
		}
	}

	if truncated > 0 {
		if _, err := fmt.Fprintf(w, "  ... and %d more\n", truncated); err != nil {
			return fmt.Errorf("writing truncation notice: %w", err)
		}
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("writing section separator: %w", err)
	}
	return nil
}
