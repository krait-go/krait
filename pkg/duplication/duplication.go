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

	totalLines := countProjectLines(project)
	groups := collectAndGroupWindows(project, minStmts)
	findings, totalDuplicateLines := cloneGroupsToFindings(groups, cfg)

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
			"clone_groups":           len(groups),
			"total_duplicate_lines":  totalDuplicateLines,
			"duplication_percentage": dupPct,
		},
	}, nil
}

// countProjectLines sums the last line number of every file in the project,
// used as the denominator for duplication_percentage.
func countProjectLines(project *analyzer.Project) int {
	total := 0
	for _, file := range project.Files {
		total += project.Fset.Position(file.End()).Line
	}
	return total
}

// collectAndGroupWindows builds sliding windows for every function body,
// groups them by hash, deduplicates, removes self-similar groups, and caps.
func collectAndGroupWindows(project *analyzer.Project, minStmts int) []*cloneGroup {
	allWindows := collectWindows(project, minStmts)
	groups := buildCloneGroups(allWindows)

	for _, g := range groups {
		g.Windows = deduplicateOverlapping(g.Windows)
	}
	groups = filterSelfSimilar(groups)

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].StmtCount > groups[j].StmtCount
	})
	if len(groups) > maxCloneGroups {
		groups = groups[:maxCloneGroups]
	}
	return groups
}

// collectWindows slides a window of minStmts across every function body and
// returns all windows up to the maxWindows cap.
func collectWindows(project *analyzer.Project, minStmts int) []*stmtWindow {
	var allWindows []*stmtWindow
	for filePath, file := range project.Files {
		relFile := parser.RelPath(project.RootDir, filePath)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || len(fn.Body.List) < minStmts {
				continue
			}
			allWindows = appendFuncWindows(allWindows, project, relFile, fn.Name.Name, fn.Body.List, minStmts)
			if len(allWindows) >= maxWindows {
				return allWindows
			}
		}
	}
	return allWindows
}

// appendFuncWindows adds sliding windows for a single function body.
func appendFuncWindows(allWindows []*stmtWindow, project *analyzer.Project, relFile, funcName string, stmts []ast.Stmt, minStmts int) []*stmtWindow {
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
	return allWindows
}

// buildCloneGroups groups windows by hash and returns only those with 2+ members.
func buildCloneGroups(allWindows []*stmtWindow) []*cloneGroup {
	grouped := make(map[uint64][]*stmtWindow)
	for _, w := range allWindows {
		grouped[w.Hash] = append(grouped[w.Hash], w)
	}
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
	return groups
}

// cloneGroupsToFindings converts clone groups to analyzer findings and returns
// the total number of duplicate lines.
func cloneGroupsToFindings(groups []*cloneGroup, cfg *analyzer.Config) ([]*analyzer.Finding, int) {
	sev, include := analyzer.ResolveSeverity(ruleName, analyzer.SeverityWarning, cfg)
	var findings []*analyzer.Finding
	totalDuplicateLines := 0

	for _, g := range groups {
		for i, w := range g.Windows {
			if i == 0 {
				continue // treat first occurrence as original
			}
			totalDuplicateLines += w.EndLine - w.StartLine + 1
		}
		if include {
			findings = append(findings, cloneGroupFinding(g, sev))
		}
	}
	return findings, totalDuplicateLines
}

// cloneGroupFinding builds a single Finding for a clone group.
func cloneGroupFinding(g *cloneGroup, sev analyzer.Severity) *analyzer.Finding {
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

	return &analyzer.Finding{
		Rule:     ruleName,
		Category: analyzer.CategoryDuplication,
		Severity: sev,
		Message:  fmt.Sprintf("Duplicated code block (%d statements) found in %d locations", g.StmtCount, len(g.Windows)),
		Location: analyzer.Location{
			File:    first.File,
			Line:    first.StartLine,
			EndLine: first.EndLine,
		},
		RelatedLocations: related,
		Meta: map[string]any{
			"clone_count": len(g.Windows),
			"lines":       lines,
			"statements":  g.StmtCount,
		},
	}
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
