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

	importFirstFile := collectAllImports(project)
	directDeps := filterDirectDeps(project.GoModDeps)

	unusedFindings, unusedCount := findUnusedDeps(directDeps, importFirstFile, cfg)
	unlistedFindings, unlistedCount, externalImports := findUnlistedDeps(project, importFirstFile, cfg)

	findings := append(unusedFindings, unlistedFindings...)

	elapsed := time.Since(start)
	return &analyzer.Result{
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
	}, nil
}

// collectAllImports returns a map from import path to the first absolute file
// path that contains that import, across all packages in the project.
func collectAllImports(project *analyzer.Project) map[string]string {
	importFirstFile := make(map[string]string)
	for _, pkg := range project.Packages {
		for i, file := range pkg.Files {
			filePath := ""
			if i < len(pkg.FilePaths) {
				filePath = pkg.FilePaths[i]
			}
			for _, imp := range file.Imports {
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
	return importFirstFile
}

// filterDirectDeps returns only the non-indirect entries from allDeps.
func filterDirectDeps(allDeps []analyzer.GoModDep) []analyzer.GoModDep {
	var direct []analyzer.GoModDep
	for _, dep := range allDeps {
		if !dep.Indirect {
			direct = append(direct, dep)
		}
	}
	return direct
}

// findUnusedDeps checks each direct dependency against the imported paths and
// returns findings for any that are not used, plus the unused count.
func findUnusedDeps(
	directDeps []analyzer.GoModDep,
	importFirstFile map[string]string,
	cfg *analyzer.Config,
) ([]*analyzer.Finding, int) {
	var findings []*analyzer.Finding
	unusedCount := 0
	for _, dep := range directDeps {
		if isDepUsed(dep.Path, importFirstFile) {
			continue
		}
		unusedCount++
		sev, ok := analyzer.ResolveSeverity("unused-dependency", analyzer.SeverityWarning, cfg)
		if !ok {
			continue
		}
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
	return findings, unusedCount
}

// isDepUsed reports whether any import path in importFirstFile is covered by depPath.
func isDepUsed(depPath string, importFirstFile map[string]string) bool {
	for imp := range importFirstFile {
		if imp == depPath || strings.HasPrefix(imp, depPath+"/") {
			return true
		}
	}
	return false
}

// findUnlistedDeps checks external imports against go.mod and returns findings
// for unlisted ones, plus the unlisted count and the full external import set.
func findUnlistedDeps(
	project *analyzer.Project,
	importFirstFile map[string]string,
	cfg *analyzer.Config,
) ([]*analyzer.Finding, int, map[string]struct{}) {
	allDeps := project.GoModDeps
	externalImports := make(map[string]struct{})
	var findings []*analyzer.Finding
	unlistedCount := 0

	for imp, filePath := range importFirstFile {
		if isStdlib(imp) || strings.HasPrefix(imp, project.ModulePath) {
			continue
		}
		externalImports[imp] = struct{}{}

		if isDepCovered(imp, allDeps) {
			continue
		}
		unlistedCount++
		relFile := parser.RelPath(project.RootDir, filePath)
		sev, ok := analyzer.ResolveSeverity("unlisted-dependency", analyzer.SeverityError, cfg)
		if !ok {
			continue
		}
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
	return findings, unlistedCount, externalImports
}

// isDepCovered reports whether any entry in allDeps covers the given import path.
func isDepCovered(imp string, allDeps []analyzer.GoModDep) bool {
	for _, dep := range allDeps {
		if imp == dep.Path || strings.HasPrefix(imp, dep.Path+"/") {
			return true
		}
	}
	return false
}
