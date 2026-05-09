// Package reporter formats analysis reports into various output formats.
package reporter

import (
	"fmt"
	"io"

	"github.com/krait-go/krait/pkg/analyzer"
)

// Format writes the report in the specified format to the writer.
func Format(w io.Writer, report *analyzer.Report, format string) error {
	switch format {
	case "json":
		return formatJSON(w, report)
	case "sarif":
		return formatSARIF(w, report)
	case "text":
		return formatText(w, report)
	case "markdown":
		return formatMarkdown(w, report)
	default:
		return fmt.Errorf("unknown format: %q (valid: json, sarif, text, markdown)", format)
	}
}
