// Package config handles loading and validating krait configuration files.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/krait-go/krait/pkg/analyzer"
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

		data = stripJSONCComments(data)

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

	return nil
}

// stripJSONCComments removes // comments from JSONC content.
func stripJSONCComments(data []byte) []byte {
	lines := splitLines(data)
	var result []byte
	for i, line := range lines {
		stripped := stripLineComment(line)
		result = append(result, stripped...)
		if i < len(lines)-1 {
			result = append(result, '\n')
		}
	}
	return result
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	for {
		idx := indexOf(data, '\n')
		if idx < 0 {
			lines = append(lines, data)
			break
		}
		lines = append(lines, data[:idx])
		data = data[idx+1:]
	}
	return lines
}

func indexOf(data []byte, b byte) int {
	for i, c := range data {
		if c == b {
			return i
		}
	}
	return -1
}

func stripLineComment(line []byte) []byte {
	inString := false
	escaped := false
	for i := range line {
		if escaped {
			escaped = false
			continue
		}
		switch line[i] {
		case '\\':
			if inString {
				escaped = true
			}
		case '"':
			inString = !inString
		case '/':
			if !inString && i+1 < len(line) && line[i+1] == '/' {
				// Trim trailing whitespace before the comment
				trimmed := line[:i]
				for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == ' ' || trimmed[len(trimmed)-1] == '\t') {
					trimmed = trimmed[:len(trimmed)-1]
				}
				return trimmed
			}
		}
	}
	return line
}
