package pkg

import "fmt"

// ProcessData contains a duplicated code block (same as in other.go).
func ProcessData(items []string) []string {
	var result []string
	for _, item := range items {
		if item == "" {
			continue
		}
		processed := fmt.Sprintf("processed: %s", item)
		result = append(result, processed)
	}
	for i := 0; i < len(result); i++ {
		if result[i] == "" {
			result = append(result[:i], result[i+1:]...)
			i--
		}
	}
	return result
}
