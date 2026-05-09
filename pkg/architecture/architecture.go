package architecture

import (
	"strconv"
	"strings"
	"time"

	"github.com/krait-go/krait/internal/parser"
	"github.com/krait-go/krait/pkg/analyzer"
)

type pkgMetrics struct {
	ImportPath  string
	RelPath     string
	Imports     []string // internal import paths this package depends on
	ImportedBy  []string // internal import paths that depend on this package
	Ca          int      // afferent coupling: how many internal packages import this one
	Ce          int      // efferent coupling: how many internal packages this one imports
	Instability float64
	FileCount   int
	Layer       string
}

type architectureAnalyzer struct{}

// New returns a new architecture analyzer.
func New() analyzer.Analyzer {
	return &architectureAnalyzer{}
}

func (a *architectureAnalyzer) Name() string {
	return "architecture"
}

func (a *architectureAnalyzer) Description() string {
	return "Detects architecture layer violations and coupling issues"
}

func (a *architectureAnalyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
	start := time.Now()

	// Step 1: Build internal dependency graph.
	// metrics keyed by import path.
	metrics := make(map[string]*pkgMetrics)
	for importPath, pkg := range project.Packages {
		m := &pkgMetrics{
			ImportPath: importPath,
			RelPath:    pkg.RelPath,
			FileCount:  len(pkg.Files),
		}
		// Collect unique internal imports.
		seen := make(map[string]bool)
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				if imp.Path == nil {
					continue
				}
				depPath := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(depPath, project.ModulePath) {
					continue
				}
				if seen[depPath] {
					continue
				}
				seen[depPath] = true
				m.Imports = append(m.Imports, depPath)
			}
		}
		metrics[importPath] = m
	}

	// Step 2: Compute Ca/Ce and populate ImportedBy.
	for importPath, m := range metrics {
		m.Ce = len(m.Imports)
		for _, dep := range m.Imports {
			if target, ok := metrics[dep]; ok {
				target.Ca++
				target.ImportedBy = append(target.ImportedBy, importPath)
			}
		}
	}

	// Compute instability: Ce / (Ca + Ce). Packages with no edges are stable (0.0).
	for _, m := range metrics {
		if m.Ca+m.Ce > 0 {
			m.Instability = float64(m.Ce) / float64(m.Ca+m.Ce)
		}
	}

	// Step 3: Assign layers if configured.
	if len(cfg.ArchitectureLayers) > 0 {
		for _, m := range metrics {
			for _, layer := range cfg.ArchitectureLayers {
				for _, pattern := range layer.Packages {
					if matchLayerPattern(m.RelPath, pattern) {
						m.Layer = layer.Name
						break
					}
				}
				if m.Layer != "" {
					break
				}
			}
		}
	}

	// Build layer → CanImport lookup.
	layerCanImport := make(map[string]map[string]bool)
	for _, layer := range cfg.ArchitectureLayers {
		allowed := make(map[string]bool)
		for _, l := range layer.CanImport {
			allowed[l] = true
		}
		layerCanImport[layer.Name] = allowed
	}

	// Step 4: Find violations and god packages.
	var findings []*analyzer.Finding
	totalEdges := 0

	for _, m := range metrics {
		totalEdges += m.Ce

		// --- Layer violation check ---
		if len(cfg.ArchitectureLayers) > 0 && m.Layer != "" {
			allowed := layerCanImport[m.Layer]
			srcPkg := project.Packages[m.ImportPath]

			for _, depPath := range m.Imports {
				depMetrics, ok := metrics[depPath]
				if !ok {
					continue
				}
				tgtLayer := depMetrics.Layer
				if tgtLayer == "" || tgtLayer == m.Layer {
					// Same layer or unmapped target — allowed.
					continue
				}
				if allowed[tgtLayer] {
					continue
				}

				// Find the position of this import in the source files.
				loc := findImportLocation(project, srcPkg, depPath)

				sev, ok := analyzer.ResolveSeverity("layer-violation", analyzer.SeverityError, cfg)
				if !ok {
					continue
				}
				findings = append(findings, &analyzer.Finding{
					Rule:     "layer-violation",
					Category: analyzer.CategoryArchitecture,
					Severity: sev,
					Message:  "Layer \"" + m.Layer + "\" must not import layer \"" + tgtLayer + "\"",
					Location: loc,
					Meta: map[string]any{
						"source_layer":    m.Layer,
						"target_layer":    tgtLayer,
						"import":          depPath,
						"source_package":  m.RelPath,
					},
				})
			}
		}

		// --- God package check ---
		threshold := cfg.GodPackageThreshold
		if threshold <= 0 {
			threshold = 10
		}
		if m.Ce > threshold {
			sev, ok := analyzer.ResolveSeverity("god-package", analyzer.SeverityWarning, cfg)
			if ok {
				findings = append(findings, &analyzer.Finding{
					Rule:     "god-package",
					Category: analyzer.CategoryArchitecture,
					Severity: sev,
					Message:  "Package \"" + m.RelPath + "\" has too many internal imports (" + strconv.Itoa(m.Ce) + " > " + strconv.Itoa(threshold) + ")",
					Location: analyzer.Location{
						File: m.RelPath,
						Line: 1,
					},
					Meta: map[string]any{
						"package":      m.RelPath,
						"import_count": m.Ce,
						"imported_by":  m.Ca,
						"instability":  m.Instability,
					},
				})
			}
		}
	}

	// Step 5: Build stats.
	pkgMetricsList := make([]map[string]any, 0, len(metrics))
	for _, m := range metrics {
		pkgMetricsList = append(pkgMetricsList, map[string]any{
			"rel_path":    m.RelPath,
			"imports":     m.Ce,
			"imported_by": m.Ca,
			"instability": m.Instability,
			"file_count":  m.FileCount,
		})
	}

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   a.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats: map[string]any{
			"total_packages":   len(metrics),
			"dependency_edges": totalEdges,
			"package_metrics":  pkgMetricsList,
		},
	}, nil
}

// matchLayerPattern checks if a package's relPath belongs to a layer pattern.
// Supports ** glob patterns by extracting the core path segment and checking
// if relPath contains it as a path component.
func matchLayerPattern(relPath, pattern string) bool {
	if !strings.Contains(pattern, "**") {
		// Exact match or simple prefix.
		return relPath == pattern || strings.HasPrefix(relPath, pattern+"/")
	}

	// Strip **/  prefix and /**  suffix to get the core segment.
	core := pattern
	core = strings.TrimPrefix(core, "**/")
	core = strings.TrimSuffix(core, "/**")
	core = strings.TrimPrefix(core, "**")

	if core == "" {
		return true
	}

	// Check if relPath contains core as a path component.
	parts := strings.Split(relPath, "/")
	for i, part := range parts {
		if part == core {
			return true
		}
		// Also handle the case where core itself is a multi-segment path.
		if strings.HasPrefix(strings.Join(parts[i:], "/"), core) {
			return true
		}
	}
	return false
}

// findImportLocation returns the source location of a specific import spec
// inside a package's files.
func findImportLocation(project *analyzer.Project, pkg *analyzer.PackageInfo, importPath string) analyzer.Location {
	if pkg == nil {
		return analyzer.Location{Line: 1}
	}
	for i, file := range pkg.Files {
		filePath := ""
		if i < len(pkg.FilePaths) {
			filePath = parser.RelPath(project.RootDir, pkg.FilePaths[i])
		}
		for _, imp := range file.Imports {
			if imp.Path == nil {
				continue
			}
			if strings.Trim(imp.Path.Value, `"`) == importPath {
				pos := project.Fset.Position(imp.Pos())
				return analyzer.Location{
					File:   filePath,
					Line:   pos.Line,
					Column: pos.Column,
				}
			}
		}
	}
	// Fallback: use the package's relative path.
	if len(pkg.FilePaths) > 0 {
		return analyzer.Location{
			File: parser.RelPath(project.RootDir, pkg.FilePaths[0]),
			Line: 1,
		}
	}
	return analyzer.Location{Line: 1}
}

