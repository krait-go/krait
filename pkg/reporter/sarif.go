package reporter

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sort"

	"github.com/krait-go/krait/pkg/analyzer"
)

const (
	sarifSchema  = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json"
	sarifVersion = "2.1.0"
)

// ruleDescriptions holds static, instance-independent descriptions for each rule.
// SARIF spec §3.49.1: shortDescription is a rule-level property, not a finding message.
var ruleDescriptions = map[string]string{
	"unused-export-func":         "Exported function is never referenced outside its package",
	"unused-export-method":       "Exported method is never referenced outside its package",
	"unused-export-type":         "Exported type is never referenced outside its package",
	"unused-export-var":          "Exported variable is never referenced outside its package",
	"unused-export-const":        "Exported constant is never referenced outside its package",
	"unused-dependency":          "Direct dependency in go.mod is never imported",
	"unlisted-dependency":        "Import path is not covered by any go.mod dependency",
	"high-cyclomatic-complexity": "Function has cyclomatic complexity above threshold",
	"high-cognitive-complexity":  "Function has cognitive complexity above threshold",
	"code-duplication":           "Duplicated code block found in multiple locations",
	"god-package":                "Package has excessive outgoing dependencies",
	"layer-violation":            "Import violates architecture layer boundary",
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name    string      `json:"name"`
	Version string      `json:"version"`
	Rules   []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	ShortDescription sarifMessage `json:"shortDescription"`
}

type sarifResult struct {
	RuleID           string          `json:"ruleId"`
	Level            string          `json:"level"`
	Message          sarifMessage    `json:"message"`
	Locations        []sarifLocation `json:"locations"`
	RelatedLocations []sarifLocation `json:"relatedLocations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

// sarifPhysical omits Region when nil so file-level findings (line == 0) are
// still valid per SARIF spec §3.29.3 (startLine must be >= 1 when present).
type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           *sarifRegion  `json:"region,omitempty"`
}

type sarifArtifact struct {
	URI       string `json:"uri"`
	URIBaseID string `json:"uriBaseId,omitempty"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine,omitempty"`
}

func sarifLevel(sev analyzer.Severity) string {
	switch sev {
	case analyzer.SeverityError:
		return "error"
	case analyzer.SeverityWarning:
		return "warning"
	default:
		return "note"
	}
}

func buildPhysical(loc analyzer.Location) sarifPhysical {
	physical := sarifPhysical{
		ArtifactLocation: sarifArtifact{
			URI:       filepath.ToSlash(loc.File),
			URIBaseID: "%SRCROOT%",
		},
	}
	if loc.Line > 0 {
		physical.Region = &sarifRegion{
			StartLine:   loc.Line,
			StartColumn: loc.Column,
			EndLine:     loc.EndLine,
		}
	}
	return physical
}

func formatSARIF(w io.Writer, report *analyzer.Report) error {
	// Fix 3: initialize to empty slices so JSON serializes as [] not null.
	seenRules := make(map[string]bool)
	rules := make([]sarifRule, 0)
	results := make([]sarifResult, 0)

	for _, res := range report.Results {
		for _, f := range res.Findings {
			if !seenRules[f.Rule] {
				seenRules[f.Rule] = true
				// Fix 1: use static rule description, not the finding message.
				desc := ruleDescriptions[f.Rule]
				if desc == "" {
					desc = f.Rule
				}
				rules = append(rules, sarifRule{
					ID:               f.Rule,
					ShortDescription: sarifMessage{Text: desc},
				})
			}

			// Fix 2 + 4: forward-slash URI, uriBaseId, region omitted when line == 0.
			loc := sarifLocation{
				PhysicalLocation: buildPhysical(f.Location),
			}

			related := make([]sarifLocation, 0, len(f.RelatedLocations))
			for _, rl := range f.RelatedLocations {
				related = append(related, sarifLocation{
					PhysicalLocation: buildPhysical(rl),
				})
			}

			result := sarifResult{
				RuleID:    f.Rule,
				Level:     sarifLevel(f.Severity),
				Message:   sarifMessage{Text: f.Message},
				Locations: []sarifLocation{loc},
			}
			// Only set RelatedLocations when non-empty; the field already has
			// omitempty but we avoid a non-nil empty slice that would marshal as [].
			if len(related) > 0 {
				result.RelatedLocations = related
			}
			results = append(results, result)
		}
	}

	// Fix 5: sort rules by ID for deterministic output.
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })

	log := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:    "krait",
						Version: report.Version,
						Rules:   rules,
					},
				},
				Results: results,
			},
		},
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
