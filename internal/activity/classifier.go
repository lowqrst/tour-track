package activity

// CalculateScore applies the subject's weighted activity formula. Negative inputs
// are treated as zero. Career years use integer division, so only completed
// five-year periods contribute, up to a maximum career bonus of ten.
func CalculateScore(tourSpread, concertCount, careerYears int) int {
	tourSpread = max(0, tourSpread)
	concertCount = max(0, concertCount)
	careerYears = max(0, careerYears)
	return tourSpread*2 + concertCount*3 + min(10, careerYears/5)
}

// ClassifyActivity labels an artist using the four required score intervals.
func ClassifyActivity(tourSpread, concertCount, careerYears int) string {
	score := CalculateScore(tourSpread, concertCount, careerYears)

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

// IsRisingStar reports whether the recent two-year concert rate exceeds 150% of
// the career average. The caller supplies the concert count for that window.
// Without a positive career duration and concert counts, no rate is inferred.
func IsRisingStar(recentConcerts, totalConcerts, careerYears int) bool {
	if careerYears <= 0 || totalConcerts <= 0 || recentConcerts <= 0 {
		return false
	}

	avgTempo := float64(totalConcerts) / float64(careerYears)
	recentTempo := float64(recentConcerts) / 2.0
	return recentTempo > avgTempo*1.5
}
