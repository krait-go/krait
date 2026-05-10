// Package unusedfiles detects packages that have no internal importers,
// making every file in such packages dead from the perspective of the module.
package unusedfiles

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

type unusedFilesAnalyzer struct{}

// New returns a new unused-files analyzer.
func New() analyzer.Analyzer {
	return &unusedFilesAnalyzer{}
}

var _ analyzer.Analyzer = (*unusedFilesAnalyzer)(nil)

func (a *unusedFilesAnalyzer) Name() string {
	return "unused-files"
}

func (a *unusedFilesAnalyzer) Description() string {
	return "Detects packages with no internal importers (unreachable from any entry point)"
}

func (a *unusedFilesAnalyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
	start := time.Now()

	// Step 1: Build the internal import graph.
	// importGraph[pkgPath] = set of internal packages that pkgPath imports.
	importGraph := buildImportGraph(project)

	// Step 2: Identify entry points via BFS seed.
	entryPoints := findEntryPoints(project)

	// Step 3: BFS from entry points to find all reachable packages.
	reachable := bfsReachable(entryPoints, importGraph)

	// Step 4: Collect unreachable packages, excluding _test packages.
	unusedPkgs := collectUnusedPackages(project, reachable)

	// Step 5: Emit one finding per file in each unused package.
	// Sort for deterministic output.
	sort.Strings(unusedPkgs)

	sev, sevOK := analyzer.ResolveSeverity("unused-file", analyzer.SeverityWarning, cfg)

	var findings []*analyzer.Finding
	unusedFileCount := 0

	for _, pkgPath := range unusedPkgs {
		pkg := project.Packages[pkgPath]
		if pkg == nil {
			continue
		}

		// Sort file paths within the package for determinism.
		filePaths := make([]string, len(pkg.FilePaths))
		copy(filePaths, pkg.FilePaths)
		sort.Strings(filePaths)

		for _, absPath := range filePaths {
			unusedFileCount++
			if !sevOK {
				continue
			}
			relPath := parser.RelPath(project.RootDir, absPath)
			// Use pkg.RelPath as the package identifier in the message.
			pkgRelPath := pkg.RelPath
			if pkgRelPath == "" {
				pkgRelPath = "."
			}
			findings = append(findings, &analyzer.Finding{
				Rule:     "unused-file",
				Category: analyzer.CategoryDeadCode,
				Severity: sev,
				Message:  fmt.Sprintf("File belongs to unused package %q (no internal importers)", pkgRelPath),
				Location: analyzer.Location{
					File: relPath,
					Line: 1,
				},
				Meta: map[string]any{
					"package": pkgPath,
					"rel_path": pkgRelPath,
				},
			})
		}
	}

	totalPackages := len(project.Packages)
	unusedPackageCount := len(unusedPkgs)
	unusedPct := 0.0
	if totalPackages > 0 {
		unusedPct = float64(unusedPackageCount) / float64(totalPackages) * 100.0
	}

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   a.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats: map[string]any{
			"total_packages":    totalPackages,
			"unused_packages":   unusedPackageCount,
			"unused_files":      unusedFileCount,
			"unused_percentage": unusedPct,
		},
	}, nil
}

// buildImportGraph constructs a map from each package's import path to the set
// of internal packages it imports (i.e., packages whose import path starts with
// the module path).
func buildImportGraph(project *analyzer.Project) map[string]map[string]bool {
	graph := make(map[string]map[string]bool, len(project.Packages))

	for pkgPath, pkg := range project.Packages {
		deps := make(map[string]bool)
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				if imp.Path == nil {
					continue
				}
				depPath := strings.Trim(imp.Path.Value, `"`)
				if strings.HasPrefix(depPath, project.ModulePath) {
					deps[depPath] = true
				}
			}
		}
		graph[pkgPath] = deps
	}

	return graph
}

// findEntryPoints returns the set of package import paths that are entry points:
//   - packages named "main" (which includes packages with func main())
//   - packages targeted by blank imports (`_ "pkg"`) anywhere in the project
func findEntryPoints(project *analyzer.Project) map[string]bool {
	entries := make(map[string]bool)

	for pkgPath, pkg := range project.Packages {
		if pkg.Name == "main" {
			entries[pkgPath] = true
		}
	}

	// Blank imports act as explicit entry-point declarations: they pull in a
	// package's init() side-effects without referencing any symbol. Any
	// internally-blank-imported package is therefore live.
	for _, pkg := range project.Packages {
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				if imp.Name == nil || imp.Path == nil {
					continue
				}
				if imp.Name.Name != "_" {
					continue
				}
				depPath := strings.Trim(imp.Path.Value, `"`)
				if strings.HasPrefix(depPath, project.ModulePath) {
					entries[depPath] = true
				}
			}
		}
	}

	return entries
}

// bfsReachable performs a breadth-first search starting from the given entry
// points and returns the set of all reachable package import paths.
func bfsReachable(entryPoints map[string]bool, importGraph map[string]map[string]bool) map[string]bool {
	visited := make(map[string]bool, len(importGraph))
	queue := make([]string, 0, len(entryPoints))

	for ep := range entryPoints {
		if !visited[ep] {
			visited[ep] = true
			queue = append(queue, ep)
		}
	}

	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]

		for dep := range importGraph[curr] {
			if !visited[dep] {
				visited[dep] = true
				queue = append(queue, dep)
			}
		}
	}

	return visited
}

// collectUnusedPackages returns the import paths of packages that are not
// reachable from any entry point. Test packages (import paths ending in
// "_test" or packages named with a "_test" suffix) are excluded.
func collectUnusedPackages(project *analyzer.Project, reachable map[string]bool) []string {
	var unused []string
	for pkgPath, pkg := range project.Packages {
		if reachable[pkgPath] {
			continue
		}
		// Exclude external test packages (package foo_test) and test-only
		// packages whose import path ends with _test.
		if strings.HasSuffix(pkgPath, "_test") || strings.HasSuffix(pkg.Name, "_test") {
			continue
		}
		unused = append(unused, pkgPath)
	}
	return unused
}
