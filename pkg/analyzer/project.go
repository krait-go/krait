package analyzer

import (
	"go/ast"
	"go/token"
)

// Project holds all parsed data for the Go module being analyzed.
type Project struct {
	RootDir    string
	ModulePath string
	Fset       *token.FileSet
	Packages   map[string]*PackageInfo
	Files      map[string]*ast.File
	GoModDeps  []GoModDep
}

// PackageInfo groups files belonging to the same Go package.
type PackageInfo struct {
	ImportPath string
	RelPath    string
	Name       string
	Dir        string
	Files      []*ast.File
	FilePaths  []string
}

// GoModDep represents a dependency line from go.mod.
type GoModDep struct {
	Path     string
	Version  string
	Indirect bool
	Line     int
}
