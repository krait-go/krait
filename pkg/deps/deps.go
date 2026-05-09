package deps

import (
	"fmt"
	"strings"
	"time"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

type depsAnalyzer struct{}

// New returns a new dependency analyzer.
func New() analyzer.Analyzer {
	return &depsAnalyzer{}
}

func (a *depsAnalyzer) Name() string {
	return "dependency"
}

func (a *depsAnalyzer) Description() string {
	return "Detects unused and unlisted dependencies"
}

// isStdlib reports whether an import path is part of the Go standard library.
// A path is stdlib if the first segment (before the first slash) contains no dots.
func isStdlib(importPath string) bool {
	first := importPath
	if idx := strings.Index(importPath, "/"); idx >= 0 {
		first = importPath[:idx]
	}
	return !strings.Contains(first, ".")
}

func (a *depsAnalyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
	start := time.Now()

	// Collect all import paths with the first file they appear in.
	// importFirstFile maps import path -> first absolute file path that imports it.
	importFirstFile := make(map[string]string)

	for _, pkg := range project.Packages {
		for i, file := range pkg.Files {
			filePath := ""
			if i < len(pkg.FilePaths) {
				filePath = pkg.FilePaths[i]
			}

			for _, imp := range file.Imports {
				// Strip surrounding quotes from the import path literal.
				path := strings.Trim(imp.Path.Value, `"`)
				if path == "" {
					continue
				}
				if _, seen := importFirstFile[path]; !seen {
					importFirstFile[path] = filePath
				}
			}
		}
	}

	// Separate direct deps from indirect.
	var directDeps []analyzer.GoModDep
	allDeps := project.GoModDeps
	for _, dep := range allDeps {
		if !dep.Indirect {
			directDeps = append(directDeps, dep)
		}
	}

	var findings []*analyzer.Finding

	// Check unused direct dependencies.
	// A direct dep is unused if no import path uses it as a prefix.
	unusedCount := 0
	for _, dep := range directDeps {
		used := false
		for imp := range importFirstFile {
			if imp == dep.Path || strings.HasPrefix(imp, dep.Path+"/") {
				used = true
				break
			}
		}
		if !used {
			unusedCount++
			sev, ok := analyzer.ResolveSeverity("unused-dependency", analyzer.SeverityWarning, cfg)
			if ok {
				findings = append(findings, &analyzer.Finding{
					Rule:     "unused-dependency",
					Category: analyzer.CategoryDependency,
					Severity: sev,
					Message:  fmt.Sprintf("dependency %s is listed in go.mod but not imported", dep.Path),
					Location: analyzer.Location{
						File: "go.mod",
						Line: dep.Line,
					},
					Meta: map[string]any{
						"dependency": dep.Path,
						"version":    dep.Version,
					},
				})
			}
		}
	}

	// Check unlisted imports.
	// An import is unlisted if it is not stdlib, not internal (module prefix),
	// and no go.mod dep (direct or indirect) covers it as a prefix.
	unlistedCount := 0
	externalImports := make(map[string]struct{})

	for imp, filePath := range importFirstFile {
		if isStdlib(imp) {
			continue
		}
		if strings.HasPrefix(imp, project.ModulePath) {
			continue
		}

		// It's an external import — count it.
		externalImports[imp] = struct{}{}

		covered := false
		for _, dep := range allDeps {
			if imp == dep.Path || strings.HasPrefix(imp, dep.Path+"/") {
				covered = true
				break
			}
		}

		if !covered {
			unlistedCount++
			relFile := parser.RelPath(project.RootDir, filePath)
			sev, ok := analyzer.ResolveSeverity("unlisted-dependency", analyzer.SeverityError, cfg)
			if ok {
				findings = append(findings, &analyzer.Finding{
					Rule:     "unlisted-dependency",
					Category: analyzer.CategoryDependency,
					Severity: sev,
					Message:  fmt.Sprintf("import %q is not listed in go.mod", imp),
					Location: analyzer.Location{
						File: relFile,
					},
					Meta: map[string]any{
						"import": imp,
					},
				})
			}
		}
	}

	elapsed := time.Since(start)
	result := &analyzer.Result{
		Analyzer:   a.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats: map[string]any{
			"total_dependencies":  len(directDeps),
			"unused_dependencies": unusedCount,
			"unlisted_imports":    unlistedCount,
			"total_imports":       len(externalImports),
		},
	}

	return result, nil
}
