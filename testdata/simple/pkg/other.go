package pkg

import "fmt"

// TransformData contains a duplicated code block (same structure as helpers.go).
func TransformData(entries []string) []string {
	var output []string
	for _, entry := range entries {
		if entry == "" {
			continue
		}
		transformed := fmt.Sprintf("processed: %s", entry)
		output = append(output, transformed)
	}
	for i := 0; i < len(output); i++ {
		if output[i] == "" {
			output = append(output[:i], output[i+1:]...)
			i--
		}
	}
	return output
}
