package parser

import (
	"testing"

	"github.com/krait-go/krait/pkg/analyzer"
)

func defaultCfg() *analyzer.Config {
	return analyzer.DefaultConfig()
}

func TestParse_SimpleProject(t *testing.T) {
	cfg := defaultCfg()
	project, err := Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if project == nil {
		t.Fatal("Parse returned nil project")
	}
}

func TestParse_ModulePath(t *testing.T) {
	cfg := defaultCfg()
	project, err := Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if project.ModulePath != "example.com/simple" {
		t.Errorf("ModulePath = %q, want %q", project.ModulePath, "example.com/simple")
	}
}

func TestParse_AtLeastTwoPackages(t *testing.T) {
	cfg := defaultCfg()
	project, err := Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if len(project.Packages) < 2 {
		t.Errorf("expected at least 2 packages, got %d", len(project.Packages))
	}
}

func TestParse_GoModDepsContainsUnusedDep(t *testing.T) {
	cfg := defaultCfg()
	project, err := Parse("../../testdata/simple", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	found := false
	for _, dep := range project.GoModDeps {
		if dep.Path == "github.com/unused/dep" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("GoModDeps did not contain github.com/unused/dep; got: %v", project.GoModDeps)
	}
}

func TestParse_NonExistentDir(t *testing.T) {
	cfg := defaultCfg()
	_, err := Parse("../../testdata/nonexistent", cfg)
	if err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}
}

func TestRelPath(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		absPath string
		want    string
	}{
		{
			name:    "subdir",
			root:    "/a/b",
			absPath: "/a/b/c/d.go",
			want:    "c/d.go",
		},
		{
			name:    "same dir",
			root:    "/a/b",
			absPath: "/a/b/d.go",
			want:    "d.go",
		},
		{
			name:    "root itself",
			root:    "/a/b",
			absPath: "/a/b",
			want:    ".",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RelPath(tc.root, tc.absPath)
			if got != tc.want {
				t.Errorf("RelPath(%q, %q) = %q, want %q", tc.root, tc.absPath, got, tc.want)
			}
		})
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{
			name:     "vendor prefix",
			path:     "vendor/foo/bar.go",
			patterns: []string{"vendor/**"},
			want:     true,
		},
		{
			name:     "pb.go suffix",
			path:     "pkg/api/foo.pb.go",
			patterns: []string{"**/*.pb.go"},
			want:     true,
		},
		{
			name:     "no match",
			path:     "pkg/foo/bar.go",
			patterns: []string{"vendor/**", "**/*.pb.go"},
			want:     false,
		},
		{
			name:     "empty patterns",
			path:     "anything.go",
			patterns: []string{},
			want:     false,
		},
		{
			name:     "exact match",
			path:     "main.go",
			patterns: []string{"main.go"},
			want:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchesAny(tc.path, tc.patterns)
			if got != tc.want {
				t.Errorf("MatchesAny(%q, %v) = %v, want %v", tc.path, tc.patterns, got, tc.want)
			}
		})
	}
}

func TestIsExported(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Exported", true},
		{"NewFoo", true},
		{"unexported", false},
		{"_start", false},
		{"", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsExported(tc.name)
			if got != tc.want {
				t.Errorf("IsExported(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
