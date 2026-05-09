// Package parser provides shared Go AST parsing utilities used by all analyzers.
package parser

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/krait-go/krait/pkg/analyzer"
)

// Parse parses all Go files in the given root directory.
func Parse(rootDir string, cfg *analyzer.Config) (*analyzer.Project, error) {
	rootDir, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolving root dir: %w", err)
	}

	modulePath, deps, err := parseGoMod(filepath.Join(rootDir, "go.mod"))
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	goFiles, err := collectGoFiles(rootDir, cfg)
	if err != nil {
		return nil, fmt.Errorf("collecting files: %w", err)
	}

	fset := token.NewFileSet()
	files, warnings, err := parseFilesParallel(fset, goFiles)
	if err != nil {
		return nil, fmt.Errorf("parsing files: %w", err)
	}

	packages := groupIntoPackages(rootDir, modulePath, files)

	return &analyzer.Project{
		RootDir:    rootDir,
		ModulePath: modulePath,
		Fset:       fset,
		Packages:   packages,
		Files:      files,
		GoModDeps:  deps,
		Warnings:   warnings,
	}, nil
}

// parseGoMod does a basic line-by-line parse of go.mod.
func parseGoMod(path string) (string, []analyzer.GoModDep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var modulePath string
	var deps []analyzer.GoModDep
	lines := strings.Split(string(data), "\n")
	inRequire := false
	lineNum := 0

	for _, line := range lines {
		lineNum++
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "module ") {
			modulePath = strings.TrimSpace(strings.TrimPrefix(trimmed, "module "))
			continue
		}

		if trimmed == "require (" {
			inRequire = true
			continue
		}
		if trimmed == ")" {
			inRequire = false
			continue
		}

		if inRequire {
			if dep, ok := parseRequireEntry(trimmed, lineNum); ok {
				deps = append(deps, dep)
			}
			continue
		}

		// Handle single-line require
		if dep, ok := parseSingleLineRequire(trimmed, lineNum); ok {
			deps = append(deps, dep)
		}
	}

	if modulePath == "" {
		return "", nil, fmt.Errorf("no module directive found in %s", path)
	}

	return modulePath, deps, nil
}

// parseRequireEntry parses a single dependency line inside a require ( ... ) block.
func parseRequireEntry(trimmed string, lineNum int) (analyzer.GoModDep, bool) {
	if trimmed == "" || strings.HasPrefix(trimmed, "//") {
		return analyzer.GoModDep{}, false
	}
	parts := strings.Fields(trimmed)
	if len(parts) < 2 {
		return analyzer.GoModDep{}, false
	}
	return analyzer.GoModDep{
		Path:     parts[0],
		Version:  parts[1],
		Indirect: strings.Contains(trimmed, "// indirect"),
		Line:     lineNum,
	}, true
}

// parseSingleLineRequire parses a single-line require directive, e.g. `require foo v1.0.0`.
func parseSingleLineRequire(trimmed string, lineNum int) (analyzer.GoModDep, bool) {
	if !strings.HasPrefix(trimmed, "require ") || strings.Contains(trimmed, "(") {
		return analyzer.GoModDep{}, false
	}
	rest := strings.TrimPrefix(trimmed, "require ")
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return analyzer.GoModDep{}, false
	}
	return analyzer.GoModDep{
		Path:     parts[0],
		Version:  parts[1],
		Indirect: strings.Contains(trimmed, "// indirect"),
		Line:     lineNum,
	}, true
}

// collectGoFiles walks the directory tree and returns all .go file paths.
func collectGoFiles(rootDir string, cfg *analyzer.Config) ([]string, error) {
	var files []string
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return shouldSkipDir(path, rootDir)
		}
		if shouldIncludeFile(path, rootDir, cfg) {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// shouldSkipDir returns filepath.SkipDir for directories that should be excluded
// (hidden dirs, vendor, testdata), or nil to continue walking.
func shouldSkipDir(path, rootDir string) error {
	base := filepath.Base(path)
	rel := RelPath(rootDir, path)
	if rel != "." && strings.HasPrefix(base, ".") {
		return filepath.SkipDir
	}
	if base == "vendor" || base == "testdata" {
		return filepath.SkipDir
	}
	return nil
}

// shouldIncludeFile reports whether a file path should be collected for parsing.
func shouldIncludeFile(path, rootDir string, cfg *analyzer.Config) bool {
	if !strings.HasSuffix(path, ".go") {
		return false
	}
	if !cfg.IncludeTests && strings.HasSuffix(path, "_test.go") {
		return false
	}
	rel := RelPath(rootDir, path)
	return !MatchesAny(rel, cfg.IgnorePatterns)
}

// parseFilesParallel reads Go files concurrently then parses them sequentially.
// token.FileSet is not safe for concurrent writes, so parsing must be sequential.
// Returns parsed files, per-file warnings (non-fatal parse failures), and a fatal error
// only when every file fails.
func parseFilesParallel(fset *token.FileSet, paths []string) (map[string]*ast.File, []string, error) {
	// Phase 1: read file contents concurrently.
	type readResult struct {
		path string
		data []byte
		err  error
	}

	readResults := make(chan readResult, len(paths))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup

	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			data, err := os.ReadFile(path)
			readResults <- readResult{path: path, data: data, err: err}
		}(p)
	}

	go func() {
		wg.Wait()
		close(readResults)
	}()

	type fileData struct {
		path string
		data []byte
	}
	var toparse []fileData
	var warnings []string
	for r := range readResults {
		if r.err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", r.path, r.err))
			continue
		}
		toparse = append(toparse, fileData{path: r.path, data: r.data})
	}

	// Phase 2: parse sequentially — fset.AddFile is not goroutine-safe.
	files := make(map[string]*ast.File)
	for _, fd := range toparse {
		f, err := goparser.ParseFile(fset, fd.path, fd.data, goparser.ParseComments)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", fd.path, err))
			continue
		}
		files[fd.path] = f
	}

	if len(files) == 0 && len(warnings) > 0 {
		return nil, nil, fmt.Errorf("all files failed to parse:\n%s", strings.Join(warnings, "\n"))
	}

	return files, warnings, nil
}

// groupIntoPackages groups parsed files by their directory.
func groupIntoPackages(rootDir, modulePath string, files map[string]*ast.File) map[string]*analyzer.PackageInfo {
	pkgs := make(map[string]*analyzer.PackageInfo)

	for absPath, file := range files {
		dir := filepath.Dir(absPath)
		relDir := RelPath(rootDir, dir)
		if relDir == "." {
			relDir = ""
		}

		importPath := modulePath
		if relDir != "" {
			importPath = modulePath + "/" + relDir
		}

		pkg, ok := pkgs[importPath]
		if !ok {
			pkg = &analyzer.PackageInfo{
				ImportPath: importPath,
				RelPath:    relDir,
				Name:       file.Name.Name,
				Dir:        dir,
			}
			pkgs[importPath] = pkg
		}

		pkg.Files = append(pkg.Files, file)
		pkg.FilePaths = append(pkg.FilePaths, absPath)
	}

	return pkgs
}

// RelPath returns the path relative to rootDir, using forward slashes.
func RelPath(rootDir, absPath string) string {
	rel, err := filepath.Rel(rootDir, absPath)
	if err != nil {
		return absPath
	}
	return filepath.ToSlash(rel)
}

// MatchesAny checks if a relative path matches any of the given glob patterns.
// Supports ** by checking if any path segment matches the core pattern.
func MatchesAny(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchPattern(relPath, pattern) {
			return true
		}
	}
	return false
}

// matchPattern matches a path against a pattern that may contain **.
func matchPattern(path, pattern string) bool {
	if strings.Contains(pattern, "**") {
		return matchDoubleStarPattern(path, pattern)
	}
	// Simple glob match against the full path, then the filename.
	if matched, _ := filepath.Match(pattern, path); matched {
		return true
	}
	matched, _ := filepath.Match(pattern, filepath.Base(path))
	return matched
}

// matchDoubleStarPattern handles patterns containing **, splitting on the first
// ** to derive a prefix and suffix anchor and testing both against the path.
func matchDoubleStarPattern(path, pattern string) bool {
	parts := strings.Split(pattern, "**")
	if len(parts) != 2 {
		return false
	}
	prefix := strings.Trim(parts[0], "/")
	suffix := strings.Trim(parts[1], "/")

	if prefix == "" && suffix == "" {
		return true
	}
	if prefix != "" && suffix == "" {
		return strings.HasPrefix(path, prefix) || strings.Contains(path, "/"+prefix)
	}
	if prefix == "" {
		// suffix-only: match against filename or path suffix
		if matched, _ := filepath.Match(suffix, filepath.Base(path)); matched {
			return true
		}
		return strings.HasSuffix(path, suffix)
	}
	// Both prefix and suffix present.
	return (strings.Contains(path, prefix+"/") || strings.HasPrefix(path, prefix)) &&
		(strings.HasSuffix(path, suffix) || strings.Contains(path, suffix+"/"))
}

// IsExported reports whether a name is exported.
func IsExported(name string) bool {
	return ast.IsExported(name)
}
