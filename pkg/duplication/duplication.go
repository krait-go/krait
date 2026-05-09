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

var _ analyzer.Analyzer = (*duplicanalyzer)(nil)

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

	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].StmtCount != groups[j].StmtCount {
			return groups[i].StmtCount > groups[j].StmtCount
		}
		return groups[i].Hash < groups[j].Hash
	})
	if len(groups) > maxCloneGroups {
		groups = groups[:maxCloneGroups]
	}
	return groups
}

// collectWindows slides a window of minStmts across every function body and
// returns all windows up to the maxWindows cap.
func collectWindows(project *analyzer.Project, minStmts int) []*stmtWindow {
	// Sort file paths so windows are collected in deterministic order.
	filePaths := make([]string, 0, len(project.Files))
	for fp := range project.Files {
		filePaths = append(filePaths, fp)
	}
	sort.Strings(filePaths)

	var allWindows []*stmtWindow
	for _, filePath := range filePaths {
		file := project.Files[filePath]
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

	// Collect hash keys in sorted order so group construction is deterministic.
	hashes := make([]uint64, 0, len(grouped))
	for h := range grouped {
		hashes = append(hashes, h)
	}
	sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })

	var groups []*cloneGroup
	for _, h := range hashes {
		windows := grouped[h]
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
			_, _ = h.Write([]byte{0xFF}) // delimiter between statements
		}
		fingerprintNode(h, stmt)
	}
	return h.Sum64()
}

// nodeTagMap maps AST node types to unique byte tags for structural fingerprinting.
// Using reflect.Type as key avoids a 38-case type switch while keeping O(1) lookup.
var nodeTagMap = func() map[reflect.Type]byte {
	m := map[reflect.Type]byte{
		reflect.TypeOf((*ast.File)(nil)):           1,
		reflect.TypeOf((*ast.FuncDecl)(nil)):       2,
		reflect.TypeOf((*ast.BlockStmt)(nil)):      3,
		reflect.TypeOf((*ast.IfStmt)(nil)):         4,
		reflect.TypeOf((*ast.ForStmt)(nil)):        5,
		reflect.TypeOf((*ast.RangeStmt)(nil)):      6,
		reflect.TypeOf((*ast.SwitchStmt)(nil)):     7,
		reflect.TypeOf((*ast.TypeSwitchStmt)(nil)): 8,
		reflect.TypeOf((*ast.SelectStmt)(nil)):     9,
		reflect.TypeOf((*ast.CaseClause)(nil)):     10,
		reflect.TypeOf((*ast.CommClause)(nil)):     11,
		reflect.TypeOf((*ast.ReturnStmt)(nil)):     12,
		reflect.TypeOf((*ast.AssignStmt)(nil)):     13,
		reflect.TypeOf((*ast.ExprStmt)(nil)):       14,
		reflect.TypeOf((*ast.DeclStmt)(nil)):       15,
		reflect.TypeOf((*ast.DeferStmt)(nil)):      16,
		reflect.TypeOf((*ast.GoStmt)(nil)):         17,
		reflect.TypeOf((*ast.SendStmt)(nil)):       18,
		reflect.TypeOf((*ast.IncDecStmt)(nil)):     19,
		reflect.TypeOf((*ast.BranchStmt)(nil)):     20,
		reflect.TypeOf((*ast.LabeledStmt)(nil)):    21,
		reflect.TypeOf((*ast.CallExpr)(nil)):       22,
		reflect.TypeOf((*ast.BinaryExpr)(nil)):     23,
		reflect.TypeOf((*ast.UnaryExpr)(nil)):      24,
		reflect.TypeOf((*ast.SelectorExpr)(nil)):   25,
		reflect.TypeOf((*ast.IndexExpr)(nil)):      26,
		reflect.TypeOf((*ast.SliceExpr)(nil)):      27,
		reflect.TypeOf((*ast.TypeAssertExpr)(nil)): 28,
		reflect.TypeOf((*ast.StarExpr)(nil)):       29,
		reflect.TypeOf((*ast.ParenExpr)(nil)):      30,
		reflect.TypeOf((*ast.CompositeLit)(nil)):   31,
		reflect.TypeOf((*ast.FuncLit)(nil)):        32,
		reflect.TypeOf((*ast.BasicLit)(nil)):       33,
		reflect.TypeOf((*ast.Ident)(nil)):          34,
		reflect.TypeOf((*ast.KeyValueExpr)(nil)):   35,
		reflect.TypeOf((*ast.ArrayType)(nil)):      36,
		reflect.TypeOf((*ast.MapType)(nil)):        37,
		reflect.TypeOf((*ast.Ellipsis)(nil)):       38,
	}
	return m
}()

// writeNodeTag writes a single byte tag identifying the AST node type into h.
func writeNodeTag(h hash.Hash64, n ast.Node) {
	tag := nodeTagMap[reflect.TypeOf(n)]
	_, _ = h.Write([]byte{tag})
}

// fingerprintNode walks all AST nodes and writes structural markers into h.
// Identifier names are intentionally excluded to match renamed clones.
func fingerprintNode(h hash.Hash64, node ast.Node) {
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			return false
		}
		// Write node type tag.
		writeNodeTag(h, n)

		switch x := n.(type) {
		case *ast.BinaryExpr:
			_, _ = h.Write([]byte(x.Op.String()))
		case *ast.UnaryExpr:
			_, _ = h.Write([]byte(x.Op.String()))
		case *ast.AssignStmt:
			_, _ = h.Write([]byte(x.Tok.String()))
		case *ast.SelectorExpr:
			_, _ = h.Write([]byte(x.Sel.Name))
		case *ast.BasicLit:
			_, _ = h.Write([]byte(x.Kind.String()))
		case *ast.Ident:
			// Excluded intentionally — catches renamed clones.
		}
		return true
	})
}
