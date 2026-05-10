package churn

import (
	"testing"
)

func TestChurn_ParsePeriod(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"6m", "6.months.ago"},
		{"1m", "1.months.ago"},
		{"12m", "12.months.ago"},
		{"1y", "1.years.ago"},
		{"2y", "2.years.ago"},
		{"10y", "10.years.ago"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParsePeriod(tc.input)
			if err != nil {
				t.Fatalf("ParsePeriod(%q) returned unexpected error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParsePeriod(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestChurn_ParsePeriodInvalid(t *testing.T) {
	cases := []string{
		"",    // too short
		"6",   // no suffix
		"m",   // no number
		"abc", // non-numeric prefix, unsupported suffix
		"6d",  // unsupported suffix
		"0m",  // zero is not positive
		"-1m", // negative
	}

	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			_, err := ParsePeriod(input)
			if err == nil {
				t.Errorf("ParsePeriod(%q) expected error, got nil", input)
			}
		})
	}
}

func TestChurn_ComputeRiskScores(t *testing.T) {
	t.Run("multiplicative_scoring", func(t *testing.T) {
		churn := map[string]int{
			"high.go":   10,
			"medium.go": 5,
			"low.go":    1,
		}
		complexity := map[string]float64{
			"high.go":   20.0,
			"medium.go": 10.0,
			"low.go":    5.0,
		}

		scores := ComputeRiskScores(churn, complexity)

		// high.go: normalizedChurn=1.0, normalizedComplexity=1.0 → risk=1.0
		if scores["high.go"] != 1.0 {
			t.Errorf("high.go risk = %f, want 1.0", scores["high.go"])
		}
		// medium.go: normalizedChurn=0.5, normalizedComplexity=0.5 → risk=0.25
		const wantMedium = 0.25
		if scores["medium.go"] != wantMedium {
			t.Errorf("medium.go risk = %f, want %f", scores["medium.go"], wantMedium)
		}
		// low.go must be strictly less than medium.go
		if scores["low.go"] >= scores["medium.go"] {
			t.Errorf("low.go risk (%f) should be less than medium.go risk (%f)", scores["low.go"], scores["medium.go"])
		}
	})

	t.Run("zero_churn_means_zero_risk", func(t *testing.T) {
		churn := map[string]int{
			"active.go":  5,
			"passive.go": 0,
		}
		complexity := map[string]float64{
			"active.go":  10.0,
			"passive.go": 50.0, // very complex but never changed
		}

		scores := ComputeRiskScores(churn, complexity)

		if scores["passive.go"] != 0.0 {
			t.Errorf("passive.go (zero churn) risk = %f, want 0.0", scores["passive.go"])
		}
		if scores["active.go"] <= 0.0 {
			t.Errorf("active.go risk = %f, want > 0", scores["active.go"])
		}
	})

	t.Run("all_zero_churn_returns_zero_risk", func(t *testing.T) {
		churn := map[string]int{
			"a.go": 0,
			"b.go": 0,
		}
		complexity := map[string]float64{
			"a.go": 15.0,
			"b.go": 8.0,
		}

		scores := ComputeRiskScores(churn, complexity)

		for file, score := range scores {
			if score != 0.0 {
				t.Errorf("%s: expected risk 0.0 when all churn is zero, got %f", file, score)
			}
		}
	})

	t.Run("no_complexity_data_treats_all_files_equally", func(t *testing.T) {
		churn := map[string]int{
			"a.go": 10,
			"b.go": 5,
		}
		// Empty complexity map — no prior complexity analyzer results.
		scores := ComputeRiskScores(churn, map[string]float64{})

		// With no complexity data maxComplexity==0, so normalizedComplexity=1.0 for all.
		// Risk equals normalizedChurn * 1.0.
		if scores["a.go"] != 1.0 {
			t.Errorf("a.go risk = %f, want 1.0 (no complexity data)", scores["a.go"])
		}
		if scores["b.go"] != 0.5 {
			t.Errorf("b.go risk = %f, want 0.5 (no complexity data)", scores["b.go"])
		}
	})

	t.Run("file_with_churn_but_no_complexity_entry_gets_zero_complexity", func(t *testing.T) {
		churn := map[string]int{
			"known.go":   10,
			"unknown.go": 10,
		}
		complexity := map[string]float64{
			"known.go": 20.0,
			// "unknown.go" intentionally absent
		}

		scores := ComputeRiskScores(churn, complexity)

		// unknown.go has no complexity entry → normalizedComplexity=0 → risk=0
		if scores["unknown.go"] != 0.0 {
			t.Errorf("unknown.go risk = %f, want 0.0 (no complexity entry)", scores["unknown.go"])
		}
		// known.go should have risk=1.0 (both normalized to max)
		if scores["known.go"] != 1.0 {
			t.Errorf("known.go risk = %f, want 1.0", scores["known.go"])
		}
	})
}
