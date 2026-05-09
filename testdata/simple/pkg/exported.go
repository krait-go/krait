package pkg

import "strings"

// UsedFunc is called from main — should NOT be flagged as dead code.
func UsedFunc(s string) string {
	return strings.ToUpper(s)
}

// UnusedFunc is never called from outside this package — should be flagged.
func UnusedFunc(x int) int {
	return x * 2
}

// UnusedMethod is an exported method never called externally.
type Helper struct{}

func (h *Helper) UnusedMethod() string {
	return "unused"
}
