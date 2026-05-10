package analyzer

// PostAnalyzer processes results from other analyzers.
// Unlike Analyzer, it receives the results of all prior analyzer runs.
type PostAnalyzer interface {
	Name() string
	Description() string
	PostAnalyze(project *Project, cfg *Config, results []*Result) (*Result, error)
}

// HealthWeights configures the weight of each signal in the health score.
type HealthWeights struct {
	DeadCode     int `json:"dead_code"`
	Duplication  int `json:"duplication"`
	Complexity   int `json:"complexity"`
	Architecture int `json:"architecture"`
	Dependencies int `json:"dependencies"`
}

// DefaultHealthWeights returns the default health score weights.
func DefaultHealthWeights() *HealthWeights {
	return &HealthWeights{
		DeadCode:     20,
		Duplication:  20,
		Complexity:   25,
		Architecture: 20,
		Dependencies: 15,
	}
}
