package deadcode

import (
	"go/ast"
	"path/filepath"
	"strings"
	"time"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

// commonInterfaceMethods lists well-known interface method names that satisfy
// implicit interfaces from the standard library or popular frameworks. These
// are excluded from unused-export reporting because their presence signals
// interface conformance, not dead code.
var commonInterfaceMethods = map[string]bool{
	"String":        true,
	"Error":         true,
	"MarshalJSON":   true,
	"UnmarshalJSON": true,
	"ServeHTTP":     true,
	"Read":          true,
	"Write":         true,
	"Close":         true,
	"Len":           true,
	"Less":          true,
	"Swap":          true,
	"Reset":         true,
	"ProtoMessage":  true,
	"Validate":      true,
	"TableName":     true,
	"Scan":          true,
	"Value":         true,
	"GormDataType":  true,
	"BeforeCreate":  true,
	"AfterCreate":   true,
}

type declaration struct {
	Name    string
	Kind    string // "func", "method", "type", "var", "const"
	File    string // relative path
	Line    int
	EndLine int
	PkgPath string
	Recv    string // receiver type for methods
}

type deadCodeAnalyzer struct{}

// New returns a new dead-code analyzer.
func New() analyzer.Analyzer {
	return &deadCodeAnalyzer{}
}

func (d *deadCodeAnalyzer) Name() string {
	return "dead-code"
}

func (d *deadCodeAnalyzer) Description() string {
	return "Detects unused exported symbols across packages"
}

func (d *deadCodeAnalyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
	start := time.Now()

	// Step 1: Collect all exported declarations across every package.
	decls := make(map[string]*declaration)
	for _, pkg := range project.Packages {
		for i, file := range pkg.Files {
			filePath := ""
			if i < len(pkg.FilePaths) {
				filePath = parser.RelPath(project.RootDir, pkg.FilePaths[i])
			}
			collectDeclarations(file, filePath, pkg.ImportPath, project, cfg, decls)
		}
	}

	// Step 2: Build a cross-package reference map by walking SelectorExpr nodes.
	refs := make(map[string]int)
	for _, pkg := range project.Packages {
		for _, file := range pkg.Files {
			collectReferences(file, pkg.ImportPath, refs)
		}
	}

	// Step 3: Build findings for declarations with zero cross-package references.
	unusedByKind := make(map[string]int)
	var findings []*analyzer.Finding

	for key, decl := range decls {
		if refs[key] > 0 {
			continue
		}
		ruleName := kindToRule(decl.Kind)
		sev, ok := analyzer.ResolveSeverity(ruleName, analyzer.SeverityWarning, cfg)
		if !ok {
			continue
		}
		unusedByKind[decl.Kind]++
		findings = append(findings, &analyzer.Finding{
			Rule:     ruleName,
			Category: analyzer.CategoryDeadCode,
			Severity: sev,
			Message:  "Exported " + decl.Kind + " " + decl.Name + " is never referenced outside its package",
			Location: analyzer.Location{
				File:    decl.File,
				Line:    decl.Line,
				EndLine: decl.EndLine,
			},
			Meta: map[string]any{
				"symbol":   decl.Name,
				"kind":     decl.Kind,
				"pkg_path": decl.PkgPath,
			},
		})
	}

	totalExported := len(decls)
	unusedCount := len(findings)
	unusedPct := 0.0
	if totalExported > 0 {
		unusedPct = float64(unusedCount) / float64(totalExported) * 100.0
	}

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   d.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats: map[string]any{
			"total_exported_symbols": totalExported,
			"unused_symbols":         unusedCount,
			"unused_by_kind":         unusedByKind,
			"unused_percentage":      unusedPct,
		},
	}, nil
}

// kindToRule maps a declaration kind to its lint rule name.
func kindToRule(kind string) string {
	switch kind {
	case "func":
		return "unused-export-func"
	case "method":
		return "unused-export-method"
	case "type":
		return "unused-export-type"
	case "var":
		return "unused-export-var"
	default:
		return "unused-export-const"
	}
}

// collectDeclarations walks a single file and records every exported symbol
// in decls, keyed by "<pkgImportPath>.<ReceiverType>.<Name>" for methods or
// "<pkgImportPath>.<Name>" for everything else.
func collectDeclarations(
	file *ast.File,
	filePath string,
	pkgImportPath string,
	project *analyzer.Project,
	cfg *analyzer.Config,
	decls map[string]*declaration,
) {
	isTestFile := strings.HasSuffix(filePath, "_test.go")

	for _, d := range file.Decls {
		switch fd := d.(type) {
		case *ast.FuncDecl:
			collectFuncDecl(fd, filePath, pkgImportPath, project, cfg, decls, isTestFile)
		case *ast.GenDecl:
			collectGenDecl(fd, filePath, pkgImportPath, project, cfg, decls)
		}
	}
}

// collectFuncDecl records an exported function or method declaration.
func collectFuncDecl(
	fd *ast.FuncDecl,
	filePath string,
	pkgImportPath string,
	project *analyzer.Project,
	cfg *analyzer.Config,
	decls map[string]*declaration,
	isTestFile bool,
) {
	name := fd.Name.Name
	if !parser.IsExported(name) {
		return
	}
	if name == "main" || name == "init" {
		return
	}
	if isTestFile && (strings.HasPrefix(name, "Test") ||
		strings.HasPrefix(name, "Benchmark") ||
		strings.HasPrefix(name, "Example")) {
		return
	}
	if analyzer.MatchesIgnoreExport(name, cfg.IgnoreExports) {
		return
	}

	kind := "func"
	recv := ""
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		kind = "method"
		recv = receiverTypeName(fd.Recv.List[0].Type)
		// Methods that satisfy well-known interfaces are never dead code.
		if commonInterfaceMethods[name] {
			return
		}
	}

	startLine := project.Fset.Position(fd.Pos()).Line
	endLine := project.Fset.Position(fd.End()).Line

	key := pkgImportPath + "." + name
	if kind == "method" {
		key = pkgImportPath + "." + recv + "." + name
	}

	decls[key] = &declaration{
		Name:    name,
		Kind:    kind,
		File:    filePath,
		Line:    startLine,
		EndLine: endLine,
		PkgPath: pkgImportPath,
		Recv:    recv,
	}
}

// collectGenDecl records exported type, var, and const declarations.
func collectGenDecl(
	fd *ast.GenDecl,
	filePath string,
	pkgImportPath string,
	project *analyzer.Project,
	cfg *analyzer.Config,
	decls map[string]*declaration,
) {
	for _, spec := range fd.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			collectTypeSpec(s, filePath, pkgImportPath, project, cfg, decls)
		case *ast.ValueSpec:
			collectValueSpec(s, fd, filePath, pkgImportPath, project, cfg, decls)
		}
	}
}

// collectTypeSpec records an exported type declaration.
func collectTypeSpec(
	s *ast.TypeSpec,
	filePath string,
	pkgImportPath string,
	project *analyzer.Project,
	cfg *analyzer.Config,
	decls map[string]*declaration,
) {
	if !parser.IsExported(s.Name.Name) || analyzer.MatchesIgnoreExport(s.Name.Name, cfg.IgnoreExports) {
		return
	}
	startLine := project.Fset.Position(s.Pos()).Line
	endLine := project.Fset.Position(s.End()).Line
	key := pkgImportPath + "." + s.Name.Name
	decls[key] = &declaration{
		Name:    s.Name.Name,
		Kind:    "type",
		File:    filePath,
		Line:    startLine,
		EndLine: endLine,
		PkgPath: pkgImportPath,
	}
}

// collectValueSpec records exported var and const declarations.
func collectValueSpec(
	s *ast.ValueSpec,
	fd *ast.GenDecl,
	filePath string,
	pkgImportPath string,
	project *analyzer.Project,
	cfg *analyzer.Config,
	decls map[string]*declaration,
) {
	kind := "var"
	if fd.Tok.String() == "const" {
		kind = "const"
	}
	for _, vname := range s.Names {
		if !parser.IsExported(vname.Name) || analyzer.MatchesIgnoreExport(vname.Name, cfg.IgnoreExports) {
			continue
		}
		startLine := project.Fset.Position(vname.Pos()).Line
		endLine := project.Fset.Position(vname.End()).Line
		key := pkgImportPath + "." + vname.Name
		decls[key] = &declaration{
			Name:    vname.Name,
			Kind:    kind,
			File:    filePath,
			Line:    startLine,
			EndLine: endLine,
			PkgPath: pkgImportPath,
		}
	}
}

// collectReferences increments refs["importPath.SymbolName"] for every
// SelectorExpr in file where the qualifier resolves to a known external import.
func collectReferences(
	file *ast.File,
	currentPkg string,
	refs map[string]int,
) {
	importMap := buildImportMap(file)

	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		importPath, found := importMap[ident.Name]
		if !found {
			return true
		}
		// Only track cross-package references.
		if importPath == currentPkg {
			return true
		}
		refs[importPath+"."+sel.Sel.Name]++
		return true
	})
}

// buildImportMap returns a map from local alias to import path for all imports
// in a file. Dot-imports and blank imports are skipped.
func buildImportMap(file *ast.File) map[string]string {
	m := make(map[string]string)
	for _, imp := range file.Imports {
		if imp.Path == nil {
			continue
		}
		importPath := strings.Trim(imp.Path.Value, `"`)
		var localName string
		if imp.Name != nil && imp.Name.Name != "_" && imp.Name.Name != "." {
			localName = imp.Name.Name
		} else if imp.Name != nil {
			// Blank or dot import — skip; we cannot resolve selector targets.
			continue
		} else {
			// Default local name is the last path segment.
			localName = filepath.Base(importPath)
		}
		m[localName] = importPath
	}
	return m
}

// receiverTypeName extracts the bare type identifier from a receiver type
// expression, stripping pointer stars and generic type parameters.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		// Generic receiver: T[P]
		return receiverTypeName(e.X)
	case *ast.IndexListExpr:
		// Generic receiver: T[P, Q]
		return receiverTypeName(e.X)
	}
	return ""
}
