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

	metrics := buildDependencyGraph(project)
	computeCoupling(metrics)
	assignLayers(metrics, cfg)

	layerCanImport := buildLayerCanImport(cfg)
	findings, totalEdges := detectViolations(project, metrics, cfg, layerCanImport)
	stats := buildArchStats(metrics, totalEdges)

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   a.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats:      stats,
	}, nil
}

// buildDependencyGraph constructs a map of pkgMetrics keyed by import path.
// Each entry lists the unique internal imports of that package.
func buildDependencyGraph(project *analyzer.Project) map[string]*pkgMetrics {
	metrics := make(map[string]*pkgMetrics)
	for importPath, pkg := range project.Packages {
		m := &pkgMetrics{
			ImportPath: importPath,
			RelPath:    pkg.RelPath,
			FileCount:  len(pkg.Files),
		}
		seen := make(map[string]bool)
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				if imp.Path == nil {
					continue
				}
				depPath := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(depPath, project.ModulePath) || seen[depPath] {
					continue
				}
				seen[depPath] = true
				m.Imports = append(m.Imports, depPath)
			}
		}
		metrics[importPath] = m
	}
	return metrics
}

// computeCoupling fills in Ca, Ce, Instability, and ImportedBy for all packages.
func computeCoupling(metrics map[string]*pkgMetrics) {
	for importPath, m := range metrics {
		m.Ce = len(m.Imports)
		for _, dep := range m.Imports {
			if target, ok := metrics[dep]; ok {
				target.Ca++
				target.ImportedBy = append(target.ImportedBy, importPath)
			}
		}
	}
	for _, m := range metrics {
		if m.Ca+m.Ce > 0 {
			m.Instability = float64(m.Ce) / float64(m.Ca+m.Ce)
		}
	}
}

// assignLayers sets the Layer field on each pkgMetrics entry based on the
// configured architecture layers and their package patterns.
func assignLayers(metrics map[string]*pkgMetrics, cfg *analyzer.Config) {
	if len(cfg.ArchitectureLayers) == 0 {
		return
	}
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

// buildLayerCanImport returns a lookup map of layer name -> allowed import layer names.
func buildLayerCanImport(cfg *analyzer.Config) map[string]map[string]bool {
	layerCanImport := make(map[string]map[string]bool)
	for _, layer := range cfg.ArchitectureLayers {
		allowed := make(map[string]bool)
		for _, l := range layer.CanImport {
			allowed[l] = true
		}
		layerCanImport[layer.Name] = allowed
	}
	return layerCanImport
}

// detectViolations walks all packages and emits findings for layer violations
// and god packages. It also accumulates the total dependency edge count.
func detectViolations(
	project *analyzer.Project,
	metrics map[string]*pkgMetrics,
	cfg *analyzer.Config,
	layerCanImport map[string]map[string]bool,
) ([]*analyzer.Finding, int) {
	var findings []*analyzer.Finding
	totalEdges := 0
	for _, m := range metrics {
		totalEdges += m.Ce
		findings = append(findings, detectLayerViolations(project, m, metrics, cfg, layerCanImport)...)
		findings = append(findings, detectGodPackage(m, cfg)...)
	}
	return findings, totalEdges
}

// detectLayerViolations returns findings for each import from m that crosses a
// forbidden layer boundary.
func detectLayerViolations(
	project *analyzer.Project,
	m *pkgMetrics,
	metrics map[string]*pkgMetrics,
	cfg *analyzer.Config,
	layerCanImport map[string]map[string]bool,
) []*analyzer.Finding {
	if len(cfg.ArchitectureLayers) == 0 || m.Layer == "" {
		return nil
	}

	allowed := layerCanImport[m.Layer]
	srcPkg := project.Packages[m.ImportPath]
	var findings []*analyzer.Finding

	for _, depPath := range m.Imports {
		depMetrics, ok := metrics[depPath]
		if !ok {
			continue
		}
		tgtLayer := depMetrics.Layer
		if tgtLayer == "" || tgtLayer == m.Layer || allowed[tgtLayer] {
			continue
		}

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
				"source_layer":   m.Layer,
				"target_layer":   tgtLayer,
				"import":         depPath,
				"source_package": m.RelPath,
			},
		})
	}
	return findings
}

// detectGodPackage returns a finding if the package exceeds the god-package
// import threshold.
func detectGodPackage(m *pkgMetrics, cfg *analyzer.Config) []*analyzer.Finding {
	threshold := cfg.GodPackageThreshold
	if threshold <= 0 {
		threshold = 10
	}
	if m.Ce <= threshold {
		return nil
	}
	sev, ok := analyzer.ResolveSeverity("god-package", analyzer.SeverityWarning, cfg)
	if !ok {
		return nil
	}
	return []*analyzer.Finding{{
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
	}}
}

// buildArchStats constructs the stats map for the result.
func buildArchStats(metrics map[string]*pkgMetrics, totalEdges int) map[string]any {
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
	return map[string]any{
		"total_packages":   len(metrics),
		"dependency_edges": totalEdges,
		"package_metrics":  pkgMetricsList,
	}
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
