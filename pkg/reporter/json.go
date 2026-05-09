package reporter

import (
	"encoding/json"
	"io"

	"github.com/krait-go/krait/pkg/analyzer"
)

func formatJSON(w io.Writer, report *analyzer.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
