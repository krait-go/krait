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

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/mod/modfile"

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

// parseGoMod parses go.mod using golang.org/x/mod/modfile.
func parseGoMod(path string) (string, []analyzer.GoModDep, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("reading %s: %w", path, err)
	}
	f, err := modfile.Parse(path, data, nil)
	if err != nil {
		return "", nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if f.Module == nil {
		return "", nil, fmt.Errorf("no module directive found in %s", path)
	}
	var deps []analyzer.GoModDep
	for _, req := range f.Require {
		deps = append(deps, analyzer.GoModDep{
			Path:     req.Mod.Path,
			Version:  req.Mod.Version,
			Indirect: req.Indirect,
			Line:     req.Syntax.Start.Line,
		})
	}
	return f.Module.Mod.Path, deps, nil
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
func MatchesAny(relPath string, patterns []string) bool {
	for _, pattern := range patterns {
		if matched, _ := doublestar.Match(pattern, relPath); matched {
			return true
		}
	}
	return false
}

// IsExported reports whether a name is exported.
func IsExported(name string) bool {
	return ast.IsExported(name)
}
