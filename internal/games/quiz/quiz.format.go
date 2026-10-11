package quiz

import "fmt"

func formatSeconds(ms int) string {
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

func formatRatio(correct, total int) string {
	return fmt.Sprintf("%d of %d", correct, total)
}

func formatStreak(streak int) string {
	return fmt.Sprintf("%d in a row", streak)
}
