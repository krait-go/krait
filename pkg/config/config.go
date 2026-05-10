// Package config handles loading and validating krait configuration files.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/krait-go/krait/pkg/analyzer"
	"github.com/tailscale/hujson"
)

// Load searches for a config file and returns the merged configuration.
func Load(dir string) (*analyzer.Config, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving config dir: %w", err)
	}

	cfg := analyzer.DefaultConfig()

	candidates := []string{
		".krait.json",
		".krait.jsonc",
		"krait.json",
	}

	for _, name := range candidates {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}

		data, err = hujson.Standardize(data)
		if err != nil {
			return nil, fmt.Errorf("standardizing JSONC in %s: %w", path, err)
		}

		if err := json.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}

		return cfg, nil
	}

	return cfg, nil
}

// Validate checks the config for invalid values.
func Validate(cfg *analyzer.Config) error {
	if cfg.CyclomaticThreshold < 1 {
		return fmt.Errorf("cyclomatic_threshold must be >= 1, got %d", cfg.CyclomaticThreshold)
	}
	if cfg.CognitiveThreshold < 1 {
		return fmt.Errorf("cognitive_threshold must be >= 1, got %d", cfg.CognitiveThreshold)
	}
	if cfg.MinDuplicateLines < 2 {
		return fmt.Errorf("min_duplicate_lines must be >= 2, got %d", cfg.MinDuplicateLines)
	}

	// Normalize "warn" to "warning" to match the JSON schema alias.
	for rule, sev := range cfg.Rules {
		if sev == "warn" {
			cfg.Rules[rule] = analyzer.SeverityWarning
		}
	}

	validSeverities := map[analyzer.Severity]bool{
		analyzer.SeverityError:   true,
		analyzer.SeverityWarning: true,
		analyzer.SeverityInfo:    true,
		analyzer.SeverityOff:     true,
	}
	for rule, sev := range cfg.Rules {
		if !validSeverities[sev] {
			return fmt.Errorf("invalid severity %q for rule %q", sev, rule)
		}
	}

	for _, pattern := range cfg.IgnoreExports {
		if _, err := filepath.Match(pattern, "test"); err != nil {
			return fmt.Errorf("invalid ignore_export pattern %q: %w", pattern, err)
		}
	}

	layerNames := make(map[string]bool)
	for _, layer := range cfg.ArchitectureLayers {
		layerNames[layer.Name] = true
	}
	for _, layer := range cfg.ArchitectureLayers {
		for _, dep := range layer.CanImport {
			if !layerNames[dep] {
				return fmt.Errorf("layer %q references unknown layer %q in can_import", layer.Name, dep)
			}
		}
	}

	if cfg.HealthWeights != nil {
		w := cfg.HealthWeights
		if w.DeadCode < 0 || w.Duplication < 0 || w.Complexity < 0 || w.Architecture < 0 || w.Dependencies < 0 {
			return fmt.Errorf("health_weights values must be non-negative")
		}
	}
	if cfg.MinHealthScore < 0 || cfg.MinHealthScore > 100 {
		return fmt.Errorf("min_health_score must be 0-100, got %d", cfg.MinHealthScore)
	}

	return nil
}
