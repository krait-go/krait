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
	files, err := parseFilesParallel(fset, goFiles)
	if err != nil {
		return nil, fmt.Errorf("parsing files: %w", err)
	}

	packages := groupIntoPackages(rootDir, modulePath, files, fset)

	return &analyzer.Project{
		RootDir:    rootDir,
		ModulePath: modulePath,
		Fset:       fset,
		Packages:   packages,
		Files:      files,
		GoModDeps:  deps,
	}, nil
}

// parseGoMod does a basic line-by-line parse of go.mod.
func parseGoMod(path string) (string, []analyzer.GoModDep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
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

		if inRequire && trimmed != "" && !strings.HasPrefix(trimmed, "//") {
			indirect := strings.Contains(trimmed, "// indirect")
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				deps = append(deps, analyzer.GoModDep{
					Path:     parts[0],
					Version:  parts[1],
					Indirect: indirect,
					Line:     lineNum,
				})
			}
		}

		// Handle single-line require
		if strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "(") {
			rest := strings.TrimPrefix(trimmed, "require ")
			parts := strings.Fields(rest)
			if len(parts) >= 2 {
				indirect := strings.Contains(trimmed, "// indirect")
				deps = append(deps, analyzer.GoModDep{
					Path:     parts[0],
					Version:  parts[1],
					Indirect: indirect,
					Line:     lineNum,
				})
			}
		}
	}

	if modulePath == "" {
		return "", nil, fmt.Errorf("no module directive found in %s", path)
	}

	return modulePath, deps, nil
}

// collectGoFiles walks the directory tree and returns all .go file paths.
func collectGoFiles(rootDir string, cfg *analyzer.Config) ([]string, error) {
	var files []string
	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			rel := RelPath(rootDir, path)
			// Skip hidden directories and vendor
			if rel != "." && strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}
			if filepath.Base(path) == "vendor" {
				return filepath.SkipDir
			}
			if filepath.Base(path) == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		rel := RelPath(rootDir, path)

		// Skip test files unless configured
		if !cfg.IncludeTests && strings.HasSuffix(path, "_test.go") {
			return nil
		}

		// Apply ignore patterns
		if MatchesAny(rel, cfg.IgnorePatterns) {
			return nil
		}

		files = append(files, path)
		return nil
	})
	return files, err
}

// parseFilesParallel parses Go files concurrently.
func parseFilesParallel(fset *token.FileSet, paths []string) (map[string]*ast.File, error) {
	type result struct {
		path string
		file *ast.File
		err  error
	}

	results := make(chan result, len(paths))
	sem := make(chan struct{}, runtime.NumCPU())
	var wg sync.WaitGroup

	for _, p := range paths {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			f, err := goparser.ParseFile(fset, path, nil, goparser.ParseComments)
			results <- result{path: path, file: f, err: err}
		}(p)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	files := make(map[string]*ast.File)
	var parseErrors []string
	for r := range results {
		if r.err != nil {
			parseErrors = append(parseErrors, fmt.Sprintf("%s: %v", r.path, r.err))
			continue
		}
		files[r.path] = r.file
	}

	if len(parseErrors) > 0 && len(files) == 0 {
		return nil, fmt.Errorf("all files failed to parse:\n%s", strings.Join(parseErrors, "\n"))
	}

	if len(parseErrors) > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d file(s) failed to parse:\n", len(parseErrors))
		for _, e := range parseErrors {
			fmt.Fprintf(os.Stderr, "  %s\n", e)
		}
	}

	return files, nil
}

// groupIntoPackages groups parsed files by their directory.
func groupIntoPackages(rootDir, modulePath string, files map[string]*ast.File, fset *token.FileSet) map[string]*analyzer.PackageInfo {
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
	// Handle ** patterns
	if strings.Contains(pattern, "**") {
		// Split pattern around **
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.Trim(parts[0], "/")
			suffix := strings.Trim(parts[1], "/")

			if prefix == "" && suffix == "" {
				return true
			}

			if prefix != "" && suffix == "" {
				return strings.HasPrefix(path, prefix) || strings.Contains(path, "/"+prefix)
			}

			if prefix == "" && suffix != "" {
				if matched, _ := filepath.Match(suffix, filepath.Base(path)); matched {
					return true
				}
				return strings.HasSuffix(path, suffix)
			}

			// Both prefix and suffix
			return (strings.Contains(path, prefix+"/") || strings.HasPrefix(path, prefix)) &&
				(strings.HasSuffix(path, suffix) || strings.Contains(path, suffix+"/"))
		}
	}

	// Simple glob match
	matched, _ := filepath.Match(pattern, path)
	if matched {
		return true
	}

	// Also try matching against just the filename
	matched, _ = filepath.Match(pattern, filepath.Base(path))
	return matched
}

// IsExported reports whether a name is exported.
func IsExported(name string) bool {
	if name == "" {
		return false
	}
	return name[0] >= 'A' && name[0] <= 'Z'
}
