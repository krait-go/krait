package architecture

import (
	"testing"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

func cleanArchConfig() *analyzer.Config {
	cfg := analyzer.DefaultConfig()
	cfg.ArchitectureLayers = []analyzer.LayerConfig{
		{
			Name:      "domain",
			Packages:  []string{"domain", "domain/**"},
			CanImport: []string{},
		},
		{
			Name:      "usecase",
			Packages:  []string{"usecase", "usecase/**"},
			CanImport: []string{"domain"},
		},
		{
			Name:      "adapter",
			Packages:  []string{"handler", "handler/**"},
			CanImport: []string{"domain", "usecase"},
		},
	}
	return cfg
}

func TestArchitecture_LayerViolation_DomainImportsHandler(t *testing.T) {
	cfg := cleanArchConfig()
	project, err := parser.Parse("../../testdata/clean-arch", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.Rule == "layer-violation" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected at least one layer-violation finding; got: %+v", result.Findings)
	}
}

func TestArchitecture_LayerViolation_SourceLayerIsDomain(t *testing.T) {
	cfg := cleanArchConfig()
	project, err := parser.Parse("../../testdata/clean-arch", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	foundDomainViolation := false
	for _, f := range result.Findings {
		if f.Rule != "layer-violation" {
			continue
		}
		srcLayer, ok := f.Meta["source_layer"]
		if !ok {
			continue
		}
		if srcLayer == "domain" {
			foundDomainViolation = true
			// Also verify target_layer is set.
			if _, hasTarget := f.Meta["target_layer"]; !hasTarget {
				t.Error("layer-violation finding is missing target_layer in meta")
			}
			break
		}
	}
	if !foundDomainViolation {
		t.Errorf("expected layer-violation with source_layer=domain; findings: %+v", result.Findings)
	}
}

func TestArchitecture_NoLayerConfig_NoViolations(t *testing.T) {
	// Without architecture layers configured, no layer-violation findings should appear.
	cfg := analyzer.DefaultConfig()
	// cfg.ArchitectureLayers is empty by default.
	project, err := parser.Parse("../../testdata/clean-arch", cfg)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	a := New()
	result, err := a.Analyze(project, cfg)
	if err != nil {
		t.Fatalf("Analyze returned error: %v", err)
	}

	for _, f := range result.Findings {
		if f.Rule == "layer-violation" {
			t.Errorf("expected no layer-violation findings without layer config, got: %+v", f)
		}
	}
}

func TestArchitecture_Name(t *testing.T) {
	a := New()
	if a.Name() != "architecture" {
		t.Errorf("Name() = %q, want %q", a.Name(), "architecture")
	}
}

func TestArchitecture_Description(t *testing.T) {
	a := New()
	if a.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestMatchLayerPattern(t *testing.T) {
	tests := []struct {
		relPath string
		pattern string
		want    bool
	}{
		{"domain", "domain", true},
		{"domain/entity", "domain/**", true},
		{"handler", "handler", true},
		{"usecase/service", "usecase/**", true},
		{"other", "domain", false},
		{"", "domain", false},
	}

	for _, tc := range tests {
		t.Run(tc.relPath+"/"+tc.pattern, func(t *testing.T) {
			got := matchLayerPattern(tc.relPath, tc.pattern)
			if got != tc.want {
				t.Errorf("matchLayerPattern(%q, %q) = %v, want %v", tc.relPath, tc.pattern, got, tc.want)
			}
		})
	}
}
