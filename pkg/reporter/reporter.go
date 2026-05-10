package reporter

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/krait-go/krait/pkg/analyzer"
)

// ErrUnknownFormat is returned when an unregistered format is requested.
var ErrUnknownFormat = errors.New("unknown format")

// FormatFunc is the signature for a report formatter.
type FormatFunc func(w io.Writer, report *analyzer.Report) error

var registry = map[string]FormatFunc{}

func init() {
	Register("json", formatJSON)
	Register("sarif", formatSARIF)
	Register("text", formatText)
	Register("markdown", formatMarkdown)
}

// Register adds a format to the registry.
func Register(name string, fn FormatFunc) {
	registry[name] = fn
}

// Format writes the report in the specified format to the writer.
func Format(w io.Writer, report *analyzer.Report, format string) error {
	fn, ok := registry[format]
	if !ok {
		names := make([]string, 0, len(registry))
		for k := range registry {
			names = append(names, k)
		}
		sort.Strings(names)
		return fmt.Errorf("%w: %q (valid: %s)", ErrUnknownFormat, format, strings.Join(names, ", "))
	}
	return fn(w, report)
}

// Formats returns the list of registered format names.
func Formats() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
