package pkg

import "fmt"

// ComplexFunction has high cyclomatic and cognitive complexity.
func ComplexFunction(data map[string][]int, mode string) (string, error) {
	if data == nil {
		return "", fmt.Errorf("data is nil")
	}

	var total int
	var count int

	for key, values := range data {
		if key == "" {
			continue
		}

		switch mode {
		case "sum":
			for _, v := range values {
				if v < 0 {
					if v < -100 {
						return "", fmt.Errorf("value too negative: %d", v)
					}
					total += -v
				} else if v == 0 {
					continue
				} else {
					total += v
				}
				count++
			}
		case "avg":
			for _, v := range values {
				if v >= 0 && v <= 1000 {
					total += v
					count++
				} else if v > 1000 {
					total += 1000
					count++
				}
			}
		case "max":
			for _, v := range values {
				if v > total || count == 0 {
					total = v
				}
				count++
			}
		default:
			return "", fmt.Errorf("unknown mode: %s", mode)
		}
	}

	if count == 0 {
		return "no data", nil
	}

	result := fmt.Sprintf("mode=%s total=%d count=%d", mode, total, count)
	return result, nil
}
