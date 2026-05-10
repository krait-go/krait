// Package circular detects circular import dependencies using Tarjan's
// strongly connected components algorithm.
package circular

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/krait-go/krait/pkg/analyzer"
)

// Analyzer implements analyzer.Analyzer for circular dependency detection.
type Analyzer struct{}

// New returns a new circular dependency analyzer.
func New() analyzer.Analyzer {
	return &Analyzer{}
}

var _ analyzer.Analyzer = (*Analyzer)(nil)

func (a *Analyzer) Name() string {
	return "circular"
}

func (a *Analyzer) Description() string {
	return "Detects circular import dependencies between packages"
}

// Analyze inspects all packages in the project and reports any import cycles
// found using Tarjan's strongly connected components algorithm.
func (a *Analyzer) Analyze(project *analyzer.Project, cfg *analyzer.Config) (*analyzer.Result, error) {
	start := time.Now()

	sev, ok := analyzer.ResolveSeverity("circular-dependency", analyzer.SeverityWarning, cfg)

	adj := buildAdjacency(project)
	sccs := tarjanSCC(adj)

	var findings []*analyzer.Finding
	packagesInCycles := make(map[string]struct{})
	longestCycle := 0

	// Sort SCCs by their sorted-first member for determinism.
	sort.Slice(sccs, func(i, j int) bool {
		si := append([]string(nil), sccs[i]...)
		sj := append([]string(nil), sccs[j]...)
		sort.Strings(si)
		sort.Strings(sj)
		return si[0] < sj[0]
	})

	for _, scc := range sccs {
		if len(scc) <= 1 {
			continue
		}

		// Sort the members so the message and meta are deterministic.
		cycle := append([]string(nil), scc...)
		sort.Strings(cycle)

		for _, pkg := range cycle {
			packagesInCycles[pkg] = struct{}{}
		}
		if len(cycle) > longestCycle {
			longestCycle = len(cycle)
		}

		if !ok {
			// Rule is off; still count for stats but emit no finding.
			continue
		}

		chain := strings.Join(cycle, " -> ") + " -> " + cycle[0]
		findings = append(findings, &analyzer.Finding{
			Rule:     "circular-dependency",
			Category: analyzer.CategoryCircular,
			Severity: sev,
			Message:  fmt.Sprintf("circular dependency detected: %s", chain),
			Location: analyzer.Location{
				File: "go.mod",
				Line: 1,
			},
			Meta: map[string]any{
				"cycle":        cycle,
				"cycle_length": len(cycle),
			},
		})
	}

	// Sort findings by message for stable output.
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Message < findings[j].Message
	})

	elapsed := time.Since(start)
	return &analyzer.Result{
		Analyzer:   a.Name(),
		Duration:   elapsed,
		DurationMs: elapsed.Milliseconds(),
		Findings:   findings,
		Stats: map[string]any{
			"total_cycles":         len(findings),
			"longest_cycle_length": longestCycle,
			"packages_in_cycles":   len(packagesInCycles),
		},
	}, nil
}

// buildAdjacency constructs an adjacency list of internal imports keyed by
// import path. Only edges whose target starts with project.ModulePath are kept.
func buildAdjacency(project *analyzer.Project) map[string][]string {
	adj := make(map[string][]string, len(project.Packages))

	// Seed every package so isolated nodes appear in the graph.
	for importPath := range project.Packages {
		adj[importPath] = nil
	}

	for importPath, pkg := range project.Packages {
		seen := make(map[string]bool)
		for _, file := range pkg.Files {
			for _, imp := range file.Imports {
				if imp.Path == nil {
					continue
				}
				dep := strings.Trim(imp.Path.Value, `"`)
				if !strings.HasPrefix(dep, project.ModulePath+"/") && dep != project.ModulePath {
					continue
				}
				if seen[dep] {
					continue
				}
				seen[dep] = true
				adj[importPath] = append(adj[importPath], dep)
			}
		}
	}

	return adj
}

// tarjanState holds mutable state for Tarjan's SCC algorithm.
type tarjanState struct {
	adj     map[string][]string
	index   map[string]int
	lowlink map[string]int
	onStack map[string]bool
	stack   []string
	counter int
	sccs    [][]string
}

// tarjanSCC runs Tarjan's algorithm and returns all SCCs.
func tarjanSCC(adj map[string][]string) [][]string {
	// Sort nodes for deterministic DFS traversal order.
	nodes := make([]string, 0, len(adj))
	for n := range adj {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)

	s := &tarjanState{
		adj:     adj,
		index:   make(map[string]int, len(nodes)),
		lowlink: make(map[string]int, len(nodes)),
		onStack: make(map[string]bool, len(nodes)),
	}

	for _, n := range nodes {
		if _, visited := s.index[n]; !visited {
			s.strongconnect(n)
		}
	}

	return s.sccs
}

// strongconnect is the recursive core of Tarjan's algorithm.
func (s *tarjanState) strongconnect(v string) {
	s.index[v] = s.counter
	s.lowlink[v] = s.counter
	s.counter++
	s.stack = append(s.stack, v)
	s.onStack[v] = true

	// Sort neighbours for determinism.
	neighbours := append([]string(nil), s.adj[v]...)
	sort.Strings(neighbours)

	for _, w := range neighbours {
		if _, visited := s.index[w]; !visited {
			s.strongconnect(w)
			if s.lowlink[w] < s.lowlink[v] {
				s.lowlink[v] = s.lowlink[w]
			}
		} else if s.onStack[w] {
			if s.index[w] < s.lowlink[v] {
				s.lowlink[v] = s.index[w]
			}
		}
	}

	// v is a root node — pop the SCC.
	if s.lowlink[v] == s.index[v] {
		var scc []string
		for {
			w := s.stack[len(s.stack)-1]
			s.stack = s.stack[:len(s.stack)-1]
			s.onStack[w] = false
			scc = append(scc, w)
			if w == v {
				break
			}
		}
		s.sccs = append(s.sccs, scc)
	}
}
