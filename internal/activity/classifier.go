package activity

// CalculateScore computes the artist activity score based on spread, concert count, and career length.
func CalculateScore(tourSpread, concertCount, careerYears int) int {
	return scoreFromMetrics(tourSpread, concertCount, careerYears)
}

func scoreFromMetrics(tourSpread, concertCount, careerYears int) int {
	if tourSpread < 0 {
		tourSpread = 0
	}
	if concertCount < 0 {
		concertCount = 0
	}
	if careerYears < 0 {
		careerYears = 0
	}

	careerBonus := minInt(10, careerYears/5)
	return (tourSpread * 2) + (concertCount * 3) + careerBonus
}

// ClassifyActivity maps a score to a human-friendly activity label.
func ClassifyActivity(tourSpread, concertCount, careerYears int) string {
	score := scoreFromMetrics(tourSpread, concertCount, careerYears)

	switch {
	case score <= 10:
		return "Emerging"
	case score <= 25:
		return "Active"
	case score <= 40:
		return "Established"
	default:
		return "Legendary"
	}
}

// IsRisingStar compares recent activity tempo to the long-term average.
// It returns true when the average concerts-per-year in the last two years exceeds
// the career baseline by more than 50%.
func IsRisingStar(recentConcerts, totalConcerts, careerYears int) bool {
	if careerYears <= 0 || totalConcerts <= 0 {
		return false
	}

	avgTempo := float64(totalConcerts) / float64(careerYears)
	recentTempo := float64(recentConcerts) / 2.0
	return recentTempo > avgTempo*1.5
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
