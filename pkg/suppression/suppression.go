// Package suppression provides inline comment suppression for krait findings.
// It parses //krait:ignore and //krait:ignore-file directives from Go source
// comments, builds a suppression map, and filters findings against it.
package suppression

import (
	"fmt"
	"strings"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

const (
	prefixIgnore     = "//krait:ignore"
	prefixIgnoreFile = "//krait:ignore-file"

	// windowLines is the number of lines after the suppression comment within
	// which a finding's line must fall to be considered suppressed. A value of
	// 20 covers multi-line function signatures and doc-comment blocks.
	windowLines = 20
)

// Suppression records one parsed suppression directive.
type Suppression struct {
	File   string // absolute or relative file path (matches Finding.Location.File)
	Rule   string // rule name, or "*" for wildcard
	Reason string // optional free-text reason after " -- "
	Line   int    // line of the directive comment (1-based)
	used   bool   // set to true when the suppression matches a finding
}

// RawComment is a comment line extracted from a source file before AST parsing,
// or produced directly from AST comment groups.
type RawComment struct {
	Line int
	Text string // trimmed comment text including the "//" prefix
}

// Map is an indexed collection of suppressions, split by scope.
type Map struct {
	// byFileLine holds line-level suppressions keyed by file path.
	byFileLine map[string][]Suppression
	// byFile holds file-level suppressions keyed by file path.
	byFile map[string][]Suppression
}

// NewMap returns an empty suppression map.
func NewMap() *Map {
	return &Map{
		byFileLine: make(map[string][]Suppression),
		byFile:     make(map[string][]Suppression),
	}
}

// Add appends a line-level suppression.
func (m *Map) Add(s Suppression) {
	m.byFileLine[s.File] = append(m.byFileLine[s.File], s)
}

// AddFileLevel appends a file-level suppression.
func (m *Map) AddFileLevel(s Suppression) {
	m.byFile[s.File] = append(m.byFile[s.File], s)
}

// LineCount returns the total number of line-level suppressions across all files.
func (m *Map) LineCount() int {
	n := 0
	for _, ss := range m.byFileLine {
		n += len(ss)
	}
	return n
}

// FileCount returns the total number of file-level suppressions across all files.
func (m *Map) FileCount() int {
	n := 0
	for _, ss := range m.byFile {
		n += len(ss)
	}
	return n
}

// Filter partitions findings into those that should be kept and stale-suppression
// diagnostics for suppressions that never matched any finding.
//
// A finding is suppressed when:
//  1. A file-level suppression exists for the finding's file with a matching
//     rule (or wildcard "*").
//  2. A line-level suppression exists for the finding's file whose directive
//     line is within [finding.Line-windowLines, finding.Line-1], i.e. the
//     comment appears up to windowLines lines before the finding's line, with a
//     matching rule (or wildcard "*").
//
// After filtering, any suppression that was never used generates a
// stale-suppression finding pointing back at the directive's location.
func (m *Map) Filter(findings []*analyzer.Finding) (filtered, stale []*analyzer.Finding) {
	for _, f := range findings {
		if m.isSuppressed(f) {
			continue
		}
		filtered = append(filtered, f)
	}

	stale = m.collectStale()
	return filtered, stale
}

// isSuppressed returns true and marks the matching suppression as used when the
// finding should be suppressed.
func (m *Map) isSuppressed(f *analyzer.Finding) bool {
	file := f.Location.File
	line := f.Location.Line

	// Check file-level suppressions first.
	for i := range m.byFile[file] {
		s := &m.byFile[file][i]
		if ruleMatches(s.Rule, f.Rule) {
			s.used = true
			return true
		}
	}

	// Check line-level suppressions: the directive must appear on a line
	// strictly before the finding's line and within the look-ahead window.
	for i := range m.byFileLine[file] {
		s := &m.byFileLine[file][i]
		if s.Line >= line {
			continue
		}
		if line-s.Line > windowLines {
			continue
		}
		if ruleMatches(s.Rule, f.Rule) {
			s.used = true
			return true
		}
	}

	return false
}

// collectStale returns stale-suppression findings for every suppression that
// was never matched against a finding.
func (m *Map) collectStale() []*analyzer.Finding {
	var out []*analyzer.Finding

	emit := func(s Suppression) {
		msg := fmt.Sprintf("suppression for rule %q is unused", s.Rule)
		if s.Reason != "" {
			msg = fmt.Sprintf("suppression for rule %q is unused (reason: %s)", s.Rule, s.Reason)
		}
		out = append(out, &analyzer.Finding{
			Rule:     "stale-suppression",
			Category: analyzer.CategorySuppression,
			Severity: analyzer.SeverityWarning,
			Message:  msg,
			Location: analyzer.Location{
				File: s.File,
				Line: s.Line,
			},
		})
	}

	for _, ss := range m.byFileLine {
		for _, s := range ss {
			if !s.used {
				emit(s)
			}
		}
	}
	for _, ss := range m.byFile {
		for _, s := range ss {
			if !s.used {
				emit(s)
			}
		}
	}

	return out
}

// ruleMatches reports whether the suppression's rule applies to the finding rule.
func ruleMatches(suppressionRule, findingRule string) bool {
	return suppressionRule == "*" || suppressionRule == findingRule
}

// BuildMapFromComments parses a map of file path -> raw comment lines into a
// suppression Map. Each RawComment.Text must include the "//" prefix.
func BuildMapFromComments(commentsByFile map[string][]RawComment) *Map {
	m := NewMap()
	for file, comments := range commentsByFile {
		for _, rc := range comments {
			s, ok := parseDirective(file, rc.Line, rc.Text)
			if !ok {
				continue
			}
			if strings.HasPrefix(rc.Text, prefixIgnoreFile) {
				m.AddFileLevel(s)
			} else {
				m.Add(s)
			}
		}
	}
	return m
}

// BuildMapFromProject scans all AST comment groups in the project and builds a
// suppression map. File paths are stored as relative paths (matching the form
// used in Finding.Location.File) by relativising against project.RootDir.
func BuildMapFromProject(project *analyzer.Project) *Map {
	commentsByFile := make(map[string][]RawComment)

	for absPath, file := range project.Files {
		relPath := parser.RelPath(project.RootDir, absPath)
		var rcs []RawComment
		for _, cg := range file.Comments {
			for _, c := range cg.List {
				line := project.Fset.Position(c.Slash).Line
				rcs = append(rcs, RawComment{Line: line, Text: c.Text})
			}
		}
		if len(rcs) > 0 {
			commentsByFile[relPath] = rcs
		}
	}

	return BuildMapFromComments(commentsByFile)
}

// parseDirective attempts to parse a single comment line as a krait suppression
// directive. Returns the Suppression and true on success, zero value and false
// otherwise.
func parseDirective(file string, line int, text string) (Suppression, bool) {
	var (
		isFileLine bool
		rest       string
	)

	switch {
	case strings.HasPrefix(text, prefixIgnoreFile+" ") || text == prefixIgnoreFile:
		isFileLine = true
		rest = strings.TrimPrefix(text, prefixIgnoreFile)
		rest = strings.TrimSpace(rest)
	case strings.HasPrefix(text, prefixIgnore+" ") || text == prefixIgnore:
		// Must not be the longer "ignore-file" prefix.
		if strings.HasPrefix(text, prefixIgnoreFile) {
			return Suppression{}, false
		}
		isFileLine = false
		rest = strings.TrimPrefix(text, prefixIgnore)
		rest = strings.TrimSpace(rest)
	default:
		return Suppression{}, false
	}

	_ = isFileLine // used by caller to route to Add vs AddFileLevel

	rule, reason := splitRuleReason(rest)
	return Suppression{
		File:   file,
		Rule:   rule,
		Reason: reason,
		Line:   line,
	}, true
}

// splitRuleReason splits the trailing part of a directive (after the prefix) on
// the " -- " separator. If no rule token is present, the wildcard "*" is used.
func splitRuleReason(s string) (rule, reason string) {
	const sep = " -- "
	rule = s
	if idx := strings.Index(s, sep); idx >= 0 {
		rule = strings.TrimSpace(s[:idx])
		reason = strings.TrimSpace(s[idx+len(sep):])
	}
	rule = strings.TrimSpace(rule)
	if rule == "" {
		rule = "*"
	}
	return rule, reason
}
