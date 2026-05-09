package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/krait-go/krait/pkg/analyzer"
)

func TestLoad_NoConfigFile_ReturnsDefaults(t *testing.T) {
	// Use a temp dir with no config file.
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load returned nil config")
	}
	// Check defaults
	defaults := analyzer.DefaultConfig()
	if cfg.CyclomaticThreshold != defaults.CyclomaticThreshold {
		t.Errorf("CyclomaticThreshold = %d, want %d", cfg.CyclomaticThreshold, defaults.CyclomaticThreshold)
	}
	if cfg.CognitiveThreshold != defaults.CognitiveThreshold {
		t.Errorf("CognitiveThreshold = %d, want %d", cfg.CognitiveThreshold, defaults.CognitiveThreshold)
	}
	if cfg.MinDuplicateLines != defaults.MinDuplicateLines {
		t.Errorf("MinDuplicateLines = %d, want %d", cfg.MinDuplicateLines, defaults.MinDuplicateLines)
	}
}

func TestLoad_ValidConfigFile(t *testing.T) {
	dir := t.TempDir()
	content := `{"cyclomatic_threshold": 20, "cognitive_threshold": 25}`
	if err := os.WriteFile(filepath.Join(dir, ".krait.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.CyclomaticThreshold != 20 {
		t.Errorf("CyclomaticThreshold = %d, want 20", cfg.CyclomaticThreshold)
	}
	if cfg.CognitiveThreshold != 25 {
		t.Errorf("CognitiveThreshold = %d, want 25", cfg.CognitiveThreshold)
	}
}

func TestLoad_JSNCCommentStripping(t *testing.T) {
	dir := t.TempDir()
	// .krait.jsonc with comments — should parse correctly.
	content := `{
  // This is a comment
  "cyclomatic_threshold": 10, // inline comment
  "cognitive_threshold": 12
}`
	if err := os.WriteFile(filepath.Join(dir, ".krait.jsonc"), []byte(content), 0o600); err != nil {
		t.Fatalf("writing config file: %v", err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load returned error for JSONC: %v", err)
	}
	if cfg.CyclomaticThreshold != 10 {
		t.Errorf("CyclomaticThreshold = %d, want 10", cfg.CyclomaticThreshold)
	}
	if cfg.CognitiveThreshold != 12 {
		t.Errorf("CognitiveThreshold = %d, want 12", cfg.CognitiveThreshold)
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	if err := Validate(cfg); err != nil {
		t.Errorf("Validate returned error for valid config: %v", err)
	}
}

func TestValidate_CyclomaticThresholdBelowOne(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.CyclomaticThreshold = 0
	if err := Validate(cfg); err == nil {
		t.Error("expected error for CyclomaticThreshold=0, got nil")
	}
}

func TestValidate_CognitiveThresholdBelowOne(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.CognitiveThreshold = 0
	if err := Validate(cfg); err == nil {
		t.Error("expected error for CognitiveThreshold=0, got nil")
	}
}

func TestValidate_MinDuplicateLinesBelowTwo(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.MinDuplicateLines = 1
	if err := Validate(cfg); err == nil {
		t.Error("expected error for MinDuplicateLines=1, got nil")
	}
}

func TestValidate_InvalidSeverity(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.Rules["unused-export-func"] = analyzer.Severity("invalid")
	if err := Validate(cfg); err == nil {
		t.Error("expected error for invalid severity, got nil")
	}
}

func TestValidate_WarnAliasAccepted(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.Rules["unused-export-func"] = analyzer.Severity("warn")
	if err := Validate(cfg); err != nil {
		t.Errorf("expected \"warn\" to be accepted as a severity alias, got error: %v", err)
	}
	// After Validate, the value should be normalized to "warning".
	if cfg.Rules["unused-export-func"] != analyzer.SeverityWarning {
		t.Errorf("expected rule to be normalized to %q, got %q", analyzer.SeverityWarning, cfg.Rules["unused-export-func"])
	}
}

func TestValidate_UnknownLayerInCanImport(t *testing.T) {
	cfg := analyzer.DefaultConfig()
	cfg.ArchitectureLayers = []analyzer.LayerConfig{
		{
			Name:      "domain",
			Packages:  []string{"domain"},
			CanImport: []string{"nonexistent-layer"},
		},
	}
	if err := Validate(cfg); err == nil {
		t.Error("expected error for unknown layer in can_import, got nil")
	}
}

func TestStripJSONCComments_CommentOutsideString(t *testing.T) {
	input := []byte(`{"key": "value"} // comment`)
	result := stripJSONCComments(input)
	got := string(result)
	want := `{"key": "value"}`
	if got != want {
		t.Errorf("stripJSONCComments = %q, want %q", got, want)
	}
}

func TestStripJSONCComments_CommentInsideString(t *testing.T) {
	// The // inside a string should not be stripped.
	input := []byte(`{"key": "http://example.com"}`)
	result := stripJSONCComments(input)
	got := string(result)
	// Should be unchanged.
	if got != string(input) {
		t.Errorf("stripJSONCComments changed string with URL: got %q, want %q", got, string(input))
	}
}

func TestStripJSONCComments_MultiLine(t *testing.T) {
	input := []byte("{\n  // comment line\n  \"k\": 1\n}")
	result := stripJSONCComments(input)
	got := string(result)
	// Comment line should be stripped to empty.
	expected := "{\n\n  \"k\": 1\n}"
	if got != expected {
		t.Errorf("stripJSONCComments multiline = %q, want %q", got, expected)
	}
}
