package complexity

import (
	"fmt"
	"go/ast"
	"go/token"
	"sort"
	"time"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

type complexityAnalyzer struct{}

// New returns a new complexity analyzer.
func New() analyzer.Analyzer {
	return &complexityAnalyzer{}
}

func (a *complexityAnalyzer) Name() string {
	return "complexity"
}

func (a *complexityAnalyzer) Description() string {
	return "Detects functions with high cyclomatic or cognitive complexity"
}

// computeCyclomatic computes the cyclomatic complexity of a function.
// It starts at 1 and increments for each decision point.
func computeCyclomatic(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 1
	}
	count := 1
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.IfStmt:
			count++
		case *ast.ForStmt:
			count++
		case *ast.RangeStmt:
			count++
		case *ast.CaseClause:
			// non-default case
			if x.List != nil {
				count++
			}
		case *ast.CommClause:
			// non-default comm clause
			if x.Comm != nil {
				count++
			}
		case *ast.BinaryExpr:
			if x.Op == token.LAND || x.Op == token.LOR {
				count++
			}
		case *ast.TypeSwitchStmt:
			count++
		case *ast.SelectStmt:
			count++
		}
		return true
	})
	return count
}

// computeCognitive computes the cognitive complexity of a function.
// It uses a custom walker that tracks nesting depth.
func computeCognitive(fn *ast.FuncDecl) int {
	if fn.Body == nil {
		return 0
	}
	w := &cognitiveWalker{}
	w.walkStmtList(fn.Body.List, 0)
	return w.score
}

type cognitiveWalker struct {
	score int
}

func (w *cognitiveWalker) walkStmtList(stmts []ast.Stmt, depth int) {
	for _, stmt := range stmts {
		w.walkStmt(stmt, depth)
	}
}

func (w *cognitiveWalker) walkStmt(stmt ast.Stmt, depth int) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.IfStmt:
		w.walkIfStmt(s, depth, false)

	case *ast.ForStmt:
		w.score += 1 + depth
		var body []ast.Stmt
		if s.Body != nil {
			body = s.Body.List
		}
		w.walkStmtList(body, depth+1)

	case *ast.RangeStmt:
		w.score += 1 + depth
		var body []ast.Stmt
		if s.Body != nil {
			body = s.Body.List
		}
		w.walkStmtList(body, depth+1)

	case *ast.SwitchStmt:
		w.score += 1 + depth
		if s.Body != nil {
			for _, cc := range s.Body.List {
				if clause, ok := cc.(*ast.CaseClause); ok {
					w.walkStmtList(clause.Body, depth+1)
				}
			}
		}

	case *ast.TypeSwitchStmt:
		w.score += 1 + depth
		if s.Body != nil {
			for _, cc := range s.Body.List {
				if clause, ok := cc.(*ast.CaseClause); ok {
					w.walkStmtList(clause.Body, depth+1)
				}
			}
		}

	case *ast.SelectStmt:
		w.score += 1 + depth
		if s.Body != nil {
			for _, cc := range s.Body.List {
				if clause, ok := cc.(*ast.CommClause); ok {
					w.walkStmtList(clause.Body, depth+1)
				}
			}
		}

	case *ast.BranchStmt:
		// goto, labeled break/continue each add +1
		if s.Label != nil && (s.Tok == token.BREAK || s.Tok == token.CONTINUE) {
			w.score++
		}
		if s.Tok == token.GOTO {
			w.score++
		}

	case *ast.BlockStmt:
		w.walkStmtList(s.List, depth)

	case *ast.ExprStmt:
		w.walkExprForFuncLit(s.X, depth)

	case *ast.AssignStmt:
		for _, rhs := range s.Rhs {
			w.walkExprForFuncLit(rhs, depth)
		}

	case *ast.ReturnStmt:
		for _, result := range s.Results {
			w.walkExprForFuncLit(result, depth)
		}

	case *ast.DeferStmt:
		if s.Call != nil {
			w.walkExprForFuncLit(s.Call.Fun, depth)
		}

	case *ast.GoStmt:
		if s.Call != nil {
			w.walkExprForFuncLit(s.Call.Fun, depth)
		}

	case *ast.SendStmt:
		w.walkExprForFuncLit(s.Value, depth)

	case *ast.IncDecStmt:
		// no structural complexity

	case *ast.LabeledStmt:
		w.walkStmt(s.Stmt, depth)
	}
}

// walkIfStmt handles if/else-if/else chains.
// isElseIf=true means this is an "else if" branch: +1 but no added nesting for the chain.
func (w *cognitiveWalker) walkIfStmt(s *ast.IfStmt, depth int, isElseIf bool) {
	// Each if (and each else-if, and each else) gets +1.
	// Only the top-level if adds nesting; else-if branches don't increase nesting.
	if isElseIf {
		w.score += 1
	} else {
		w.score += 1 + depth
	}

	// Account for logical operators in the condition.
	if s.Cond != nil {
		w.score += countLogicalOps(s.Cond)
	}

	// Walk the then-branch at depth+1 (unless this is else-if, then same depth for nesting).
	innerDepth := depth + 1
	if isElseIf {
		innerDepth = depth
	}
	if s.Body != nil {
		w.walkStmtList(s.Body.List, innerDepth)
	}

	// Handle else branch.
	if s.Else != nil {
		switch el := s.Else.(type) {
		case *ast.IfStmt:
			// else if: +1 but no nesting increment for the chain
			w.walkIfStmt(el, depth, true)
		case *ast.BlockStmt:
			// else: +1, walk at same depth as the if
			w.score += 1
			w.walkStmtList(el.List, depth+1)
		}
	}
}

// walkExprForFuncLit walks an expression, increasing nesting for any function literals found.
func (w *cognitiveWalker) walkExprForFuncLit(expr ast.Expr, depth int) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.FuncLit:
		// nested function literal: +1 (structural increment), then walk body at increased depth
		w.score++
		if e.Body != nil {
			w.walkStmtList(e.Body.List, depth+1)
		}
	case *ast.CallExpr:
		w.walkExprForFuncLit(e.Fun, depth)
		for _, arg := range e.Args {
			w.walkExprForFuncLit(arg, depth)
		}
	case *ast.BinaryExpr:
		w.walkExprForFuncLit(e.X, depth)
		w.walkExprForFuncLit(e.Y, depth)
	case *ast.UnaryExpr:
		w.walkExprForFuncLit(e.X, depth)
	case *ast.CompositeLit:
		for _, elt := range e.Elts {
			w.walkExprForFuncLit(elt, depth)
		}
	case *ast.KeyValueExpr:
		w.walkExprForFuncLit(e.Value, depth)
	}
}

// countLogicalOps counts the penalty for logical operators in a condition expression.
// +1 for the first operator found, +1 for each operator change (e.g., && to ||).
func countLogicalOps(expr ast.Expr) int {
	ops := flattenLogicalOps(expr)
	if len(ops) == 0 {
		return 0
	}
	score := 1 // first operator
	for i := 1; i < len(ops); i++ {
		if ops[i] != ops[i-1] {
			score++ // operator change
		}
	}
	return score
}

// flattenLogicalOps collects all logical operators from a binary expression tree in order.
func flattenLogicalOps(expr ast.Expr) []token.Token {
	be, ok := expr.(*ast.BinaryExpr)
	if !ok {
		return nil
	}
	if be.Op != token.LAND && be.Op != token.LOR {
		return nil
	}
	var ops []token.Token
	ops = append(ops, flattenLogicalOps(be.X)...)
	ops = append(ops, be.Op)
	ops = append(ops, flattenLogicalOps(be.Y)...)
	return ops
}

// funcQualifiedName returns the receiver-qualified name of a function.
func funcQualifiedName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	// Unwrap pointer receiver
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	var recvName string
	if ident, ok := recv.(*ast.Ident); ok {
		recvName = ident.Name
	}
	return recvName + "." + fn.Name.Name
}

type funcMetric struct {
	name       string
	file       string
	cyclomatic int
	cognitive  int
	lines      int
	startLine  int
	endLine    int
}

func (a *complexityAnalyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
	start := time.Now()

	var metrics []funcMetric

	for _, pkg := range project.Packages {
		for i, file := range pkg.Files {
			filePath := ""
			if i < len(pkg.FilePaths) {
				filePath = pkg.FilePaths[i]
			}

			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}

				name := funcQualifiedName(fn)
				cyclo := computeCyclomatic(fn)
				cogn := computeCognitive(fn)

				// Compute line count from the file set
				startPos := project.Fset.Position(fn.Pos())
				endPos := project.Fset.Position(fn.End())
				lines := endPos.Line - startPos.Line + 1

				relFile := parser.RelPath(project.RootDir, filePath)

				metrics = append(metrics, funcMetric{
					name:       name,
					file:       relFile,
					cyclomatic: cyclo,
					cognitive:  cogn,
					lines:      lines,
					startLine:  startPos.Line,
					endLine:    endPos.Line,
				})
			}
		}
	}

	var findings []*analyzer.Finding
	overThreshold := 0

	for _, m := range metrics {
		exceeded := false

		if m.cyclomatic > cfg.CyclomaticThreshold {
			exceeded = true
			sev, ok := analyzer.ResolveSeverity("high-cyclomatic-complexity", analyzer.SeverityWarning, cfg)
			if ok {
				findings = append(findings, &analyzer.Finding{
					Rule:     "high-cyclomatic-complexity",
					Category: analyzer.CategoryComplexity,
					Severity: sev,
					Message: fmt.Sprintf("function %s has cyclomatic complexity of %d (threshold: %d)",
						m.name, m.cyclomatic, cfg.CyclomaticThreshold),
					Location: analyzer.Location{
						File:    m.file,
						Line:    m.startLine,
						EndLine: m.endLine,
					},
					Meta: map[string]any{
						"function":   m.name,
						"cyclomatic": m.cyclomatic,
						"cognitive":  m.cognitive,
						"lines":      m.lines,
					},
				})
			}
		}

		if m.cognitive > cfg.CognitiveThreshold {
			exceeded = true
			sev, ok := analyzer.ResolveSeverity("high-cognitive-complexity", analyzer.SeverityWarning, cfg)
			if ok {
				findings = append(findings, &analyzer.Finding{
					Rule:     "high-cognitive-complexity",
					Category: analyzer.CategoryComplexity,
					Severity: sev,
					Message: fmt.Sprintf("function %s has cognitive complexity of %d (threshold: %d)",
						m.name, m.cognitive, cfg.CognitiveThreshold),
					Location: analyzer.Location{
						File:    m.file,
						Line:    m.startLine,
						EndLine: m.endLine,
					},
					Meta: map[string]any{
						"function":   m.name,
						"cyclomatic": m.cyclomatic,
						"cognitive":  m.cognitive,
						"lines":      m.lines,
					},
				})
			}
		}

		if exceeded {
			overThreshold++
		}
	}

	// Compute averages
	totalFuncs := len(metrics)
	avgCyclo := 0.0
	avgCogn := 0.0
	if totalFuncs > 0 {
		sumCyclo := 0
		sumCogn := 0
		for _, m := range metrics {
			sumCyclo += m.cyclomatic
			sumCogn += m.cognitive
		}
		avgCyclo = float64(sumCyclo) / float64(totalFuncs)
		avgCogn = float64(sumCogn) / float64(totalFuncs)
	}

	// Top 10 hotspots sorted by cyclomatic desc, then cognitive desc
	sorted := make([]funcMetric, len(metrics))
	copy(sorted, metrics)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].cyclomatic != sorted[j].cyclomatic {
			return sorted[i].cyclomatic > sorted[j].cyclomatic
		}
		return sorted[i].cognitive > sorted[j].cognitive
	})

	limit := 10
	if len(sorted) < limit {
		limit = len(sorted)
	}
	hotspots := make([]map[string]any, 0, limit)
	for _, m := range sorted[:limit] {
		hotspots = append(hotspots, map[string]any{
			"name":       m.name,
			"file":       m.file,
			"cyclomatic": m.cyclomatic,
			"cognitive":  m.cognitive,
		})
	}

	elapsed := time.Since(start)
	result := &analyzer.Result{
		Analyzer:   a.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats: map[string]any{
			"total_functions":     totalFuncs,
			"over_threshold":      overThreshold,
			"avg_cyclomatic":      avgCyclo,
			"avg_cognitive":       avgCogn,
			"complexity_hotspots": hotspots,
		},
	}

	return result, nil
}
