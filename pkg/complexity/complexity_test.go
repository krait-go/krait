package complexity

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"testing"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

// parseFuncDecl parses src as a Go file and returns the first *ast.FuncDecl.
func parseFuncDecl(t *testing.T, src string) *ast.FuncDecl {
	t.Helper()
	fset := token.NewFileSet()
	f, err := goparser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, decl := range f.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			return fn
		}
	}
	t.Fatal("no FuncDecl found in source")
	return nil
}

func TestComputeCyclomatic_EmptyFunction(t *testing.T) {
	src := `package p
func empty() {}`
	fn := parseFuncDecl(t, src)
	got := computeCyclomatic(fn)
	if got != 1 {
		t.Errorf("empty function: cyclomatic = %d, want 1", got)
	}
}

func TestComputeCyclomatic_SingleIf(t *testing.T) {
	src := `package p
func f(x int) {
	if x > 0 {
	}
}`
	fn := parseFuncDecl(t, src)
	got := computeCyclomatic(fn)
	if got != 2 {
		t.Errorf("single if: cyclomatic = %d, want 2", got)
	}
}

func TestComputeCyclomatic_ForPlusIf(t *testing.T) {
	src := `package p
func f(items []int) {
	for _, v := range items {
		if v > 0 {
		}
	}
}`
	fn := parseFuncDecl(t, src)
	got := computeCyclomatic(fn)
	// 1 base + 1 range + 1 if = 3
	if got != 3 {
		t.Errorf("for+if: cyclomatic = %d, want 3", got)
	}
}

func TestComputeCyclomatic_LogicalOperators(t *testing.T) {
	src := `package p
func f(a, b, c bool) bool {
	return a && b || c
}`
	fn := parseFuncDecl(t, src)
	got := computeCyclomatic(fn)
	// 1 base + 1 && + 1 || = 3
	// Note: the return statement has a BinaryExpr (||) whose X is another BinaryExpr (&&)
	// so we get +2 binary logical operators
	if got != 3 {
		t.Errorf("logical operators: cyclomatic = %d, want 3", got)
	}
}

func TestComputeCyclomatic_SwitchWithThreeCases(t *testing.T) {
	src := `package p
func f(x int) {
	switch x {
	case 1:
	case 2:
	case 3:
	}
}`
	fn := parseFuncDecl(t, src)
	got := computeCyclomatic(fn)
	// 1 base + 3 non-default case clauses = 4
	if got != 4 {
		t.Errorf("switch 3 cases: cyclomatic = %d, want 4", got)
	}
}

func TestComputeCyclomatic_NilBody(t *testing.T) {
	// Build a FuncDecl with nil body manually (represents an abstract/interface method).
	fn := &ast.FuncDecl{
		Name: ast.NewIdent("F"),
		Type: &ast.FuncType{},
		Body: nil,
	}
	got := computeCyclomatic(fn)
	if got != 1 {
		t.Errorf("nil body: cyclomatic = %d, want 1", got)
	}
}

func TestComplexFunction_ExceedsDefaultThreshold(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	project, err := parser.Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	// ComplexFunction should trigger high-cyclomatic-complexity
	found := false
	for _, f := range result.Findings {
		if f.Rule == "high-cyclomatic-complexity" {
			if meta, ok := f.Meta["function"]; ok {
				if meta == "ComplexFunction" {
					found = true
					break
				}
			}
		}
	}
	if !found {
		t.Errorf("expected finding for ComplexFunction exceeding cyclomatic threshold, but not found; findings: %+v", result.Findings)
	}
}

func TestAnalyzer_Name(t *testing.T) {
	a := New()
	if a.Name() != "complexity" {
		t.Errorf("Name() = %q, want %q", a.Name(), "complexity")
	}
}

func TestAnalyzer_Description(t *testing.T) {
	a := New()
	if a.Description() == "" {
		t.Error("Description() returned empty string")
	}
}
