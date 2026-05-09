package duplication

import (
	"fmt"
	"go/ast"
	"hash"
	"hash/fnv"
	"reflect"
	"sort"
	"time"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

const (
	maxCloneGroups = 50
	maxWindows     = 100000
	ruleName       = "code-duplication"
)

type duplicanalyzer struct{}

// New returns a new duplication analyzer.
func New() analyzer.Analyzer {
	return &duplicanalyzer{}
}

func (d *duplicanalyzer) Name() string {
	return "duplication"
}

func (d *duplicanalyzer) Description() string {
	return "Detects duplicated code blocks using AST fingerprinting"
}

// stmtWindow represents a sliding window of consecutive statements within a function.
type stmtWindow struct {
	File      string
	FuncName  string
	StartLine int
	EndLine   int
	StmtCount int
	Hash      uint64
}

// cloneGroup is a set of windows sharing the same structural fingerprint.
type cloneGroup struct {
	Hash      uint64
	Windows   []*stmtWindow
	StmtCount int
}

func (d *duplicanalyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
	start := time.Now()

	minStmts := cfg.MinDuplicateLines
	if minStmts < 2 {
		minStmts = 6
	}

	// Compute total lines across all files for duplication_percentage.
	totalLines := 0
	for _, file := range project.Files {
		totalLines += project.Fset.Position(file.End()).Line
	}

	// Collect all sliding windows from every function body.
	var allWindows []*stmtWindow

	for filePath, file := range project.Files {
		relFile := parser.RelPath(project.RootDir, filePath)

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}

			stmts := fn.Body.List
			if len(stmts) < minStmts {
				continue
			}

			funcName := fn.Name.Name

			// Slide a window of minStmts across the function body.
			for i := 0; i <= len(stmts)-minStmts; i++ {
				if len(allWindows) >= maxWindows {
					break
				}

				window := stmts[i : i+minStmts]
				h := fingerprintStatements(window)

				startPos := project.Fset.Position(window[0].Pos())
				endPos := project.Fset.Position(window[len(window)-1].End())

				allWindows = append(allWindows, &stmtWindow{
					File:      relFile,
					FuncName:  funcName,
					StartLine: startPos.Line,
					EndLine:   endPos.Line,
					StmtCount: minStmts,
					Hash:      h,
				})
			}
		}
	}

	// Group windows by hash.
	grouped := make(map[uint64][]*stmtWindow)
	for _, w := range allWindows {
		grouped[w.Hash] = append(grouped[w.Hash], w)
	}

	// Build clone groups: only keep those with 2+ windows.
	var groups []*cloneGroup
	for h, windows := range grouped {
		if len(windows) < 2 {
			continue
		}
		groups = append(groups, &cloneGroup{
			Hash:      h,
			Windows:   windows,
			StmtCount: windows[0].StmtCount,
		})
	}

	// Deduplicate overlapping ranges within the same file per group.
	for _, g := range groups {
		g.Windows = deduplicateOverlapping(g.Windows)
	}

	// Filter out groups where every window is in the same function
	// (self-similarity artefacts from the sliding window).
	groups = filterSelfSimilar(groups)

	// Sort by StmtCount descending, cap at maxCloneGroups.
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].StmtCount > groups[j].StmtCount
	})
	if len(groups) > maxCloneGroups {
		groups = groups[:maxCloneGroups]
	}

	// Build findings and accumulate stats.
	sev, include := analyzer.ResolveSeverity(ruleName, analyzer.SeverityWarning, cfg)

	var findings []*analyzer.Finding
	totalDuplicateLines := 0

	for _, g := range groups {
		m := len(g.Windows)

		// For stats: all windows except the "original" count as duplicate lines.
		for i, w := range g.Windows {
			if i == 0 {
				continue // treat first as original
			}
			totalDuplicateLines += w.EndLine - w.StartLine + 1
		}

		if !include {
			continue
		}

		first := g.Windows[0]
		lines := first.EndLine - first.StartLine + 1

		var related []analyzer.Location
		for _, w := range g.Windows[1:] {
			related = append(related, analyzer.Location{
				File:    w.File,
				Line:    w.StartLine,
				EndLine: w.EndLine,
			})
		}

		findings = append(findings, &analyzer.Finding{
			Rule:     ruleName,
			Category: analyzer.CategoryDuplication,
			Severity: sev,
			Message:  fmt.Sprintf("Duplicated code block (%d statements) found in %d locations", g.StmtCount, m),
			Location: analyzer.Location{
				File:    first.File,
				Line:    first.StartLine,
				EndLine: first.EndLine,
			},
			RelatedLocations: related,
			Meta: map[string]any{
				"clone_count": m,
				"lines":       lines,
				"statements":  g.StmtCount,
			},
		})
	}

	var dupPct float64
	if totalLines > 0 {
		dupPct = float64(totalDuplicateLines) / float64(totalLines) * 100
	}

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   d.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats: map[string]any{
			"clone_groups":             len(groups),
			"total_duplicate_lines":    totalDuplicateLines,
			"duplication_percentage":   dupPct,
		},
	}, nil
}

// deduplicateOverlapping sorts windows by (file, startLine) and merges
// overlapping ranges within the same file, keeping the earliest start.
func deduplicateOverlapping(windows []*stmtWindow) []*stmtWindow {
	if len(windows) <= 1 {
		return windows
	}

	sort.Slice(windows, func(i, j int) bool {
		if windows[i].File != windows[j].File {
			return windows[i].File < windows[j].File
		}
		return windows[i].StartLine < windows[j].StartLine
	})

	result := []*stmtWindow{windows[0]}
	for _, w := range windows[1:] {
		last := result[len(result)-1]
		if w.File == last.File && w.StartLine <= last.EndLine {
			// Overlapping in the same file — extend the range if needed, keep earliest.
			if w.EndLine > last.EndLine {
				last.EndLine = w.EndLine
			}
			continue
		}
		result = append(result, w)
	}
	return result
}

// filterSelfSimilar removes groups where all windows belong to the same function.
// These arise naturally from the sliding window and are not true cross-location clones.
func filterSelfSimilar(groups []*cloneGroup) []*cloneGroup {
	result := make([]*cloneGroup, 0, len(groups))
	for _, g := range groups {
		if allSameFunction(g.Windows) {
			continue
		}
		result = append(result, g)
	}
	return result
}

// allSameFunction reports whether every window in the group shares the same
// file and function name.
func allSameFunction(windows []*stmtWindow) bool {
	if len(windows) == 0 {
		return true
	}
	first := windows[0]
	for _, w := range windows[1:] {
		if w.File != first.File || w.FuncName != first.FuncName {
			return false
		}
	}
	return true
}

// fingerprintStatements hashes a slice of statements using structural AST features,
// excluding identifier names so renamed clones are still detected.
func fingerprintStatements(stmts []ast.Stmt) uint64 {
	h := fnv.New64a()
	for i, stmt := range stmts {
		if i > 0 {
			h.Write([]byte{0xFF}) // delimiter between statements
		}
		fingerprintNode(h, stmt)
	}
	return h.Sum64()
}

// fingerprintNode walks all AST nodes and writes structural markers into h.
// Identifier names are intentionally excluded to match renamed clones.
func fingerprintNode(h hash.Hash64, node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		// Write node type.
		h.Write([]byte(reflect.TypeOf(n).String()))

		switch x := n.(type) {
		case *ast.BinaryExpr:
			h.Write([]byte(x.Op.String()))
		case *ast.UnaryExpr:
			h.Write([]byte(x.Op.String()))
		case *ast.AssignStmt:
			h.Write([]byte(x.Tok.String()))
		case *ast.SelectorExpr:
			h.Write([]byte(x.Sel.Name))
		case *ast.BasicLit:
			h.Write([]byte(x.Kind.String()))
		case *ast.Ident:
			// Excluded intentionally — catches renamed clones.
		}
		return true
	})
}
