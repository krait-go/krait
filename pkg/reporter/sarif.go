package reporter

import (
	"encoding/json"
	"io"

	"github.com/krait-go/krait/pkg/analyzer"
)

const (
	sarifSchema  = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json"
	sarifVersion = "2.1.0"
)

type sarifLog struct {
	Schema  string      `json:"$schema"`
	Version string      `json:"version"`
	Runs    []sarifRun  `json:"runs"`
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
	ID               string              `json:"id"`
	ShortDescription sarifMessage        `json:"shortDescription"`
}

type sarifResult struct {
	RuleID           string           `json:"ruleId"`
	Level            string           `json:"level"`
	Message          sarifMessage     `json:"message"`
	Locations        []sarifLocation  `json:"locations"`
	RelatedLocations []sarifLocation  `json:"relatedLocations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
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

func formatSARIF(w io.Writer, report *analyzer.Report) error {
	// Collect unique rules across all results.
	seenRules := make(map[string]bool)
	var rules []sarifRule
	var results []sarifResult

	for _, res := range report.Results {
		for _, f := range res.Findings {
			if !seenRules[f.Rule] {
				seenRules[f.Rule] = true
				rules = append(rules, sarifRule{
					ID:               f.Rule,
					ShortDescription: sarifMessage{Text: f.Message},
				})
			}

			loc := sarifLocation{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: f.Location.File},
					Region: sarifRegion{
						StartLine:   f.Location.Line,
						StartColumn: f.Location.Column,
						EndLine:     f.Location.EndLine,
					},
				},
			}

			var related []sarifLocation
			for _, rl := range f.RelatedLocations {
				related = append(related, sarifLocation{
					PhysicalLocation: sarifPhysicalLocation{
						ArtifactLocation: sarifArtifactLocation{URI: rl.File},
						Region: sarifRegion{
							StartLine:   rl.Line,
							StartColumn: rl.Column,
							EndLine:     rl.EndLine,
						},
					},
				})
			}

			results = append(results, sarifResult{
				RuleID:           f.Rule,
				Level:            sarifLevel(f.Severity),
				Message:          sarifMessage{Text: f.Message},
				Locations:        []sarifLocation{loc},
				RelatedLocations: related,
			})
		}
	}

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
