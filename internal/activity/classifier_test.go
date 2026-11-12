package activity

import "testing"

func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name                                  string
		tourSpread, concertCount, careerYears int
		want                                  int
	}{
		{"empty history", 0, 0, 0, 0},
		{"weighted inputs", 3, 4, 12, 20},
		{"countries count twice", 4, 0, 0, 8},
		{"concerts count three times", 0, 4, 0, 12},
		{"incomplete five-year period", 0, 0, 4, 0},
		{"first five-year period", 0, 0, 5, 1},
		{"integer division", 0, 0, 9, 1},
		{"last period before cap", 0, 0, 49, 9},
		{"career cap boundary", 0, 0, 50, 10},
		{"long career stays capped", 2, 3, 150, 23},
		{"negative inputs", -4, -2, -6, 0},
		{"negative spread preserves other inputs", -1, 2, 5, 7},
		{"negative concerts preserve other inputs", 2, -1, 5, 5},
		{"future career preserves touring inputs", 2, 2, -5, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CalculateScore(tt.tourSpread, tt.concertCount, tt.careerYears); got != tt.want {
				t.Fatalf("CalculateScore(%d, %d, %d) = %d; want %d", tt.tourSpread, tt.concertCount, tt.careerYears, got, tt.want)
			}
		})
	}
}

func TestClassifyActivityBoundaries(t *testing.T) {
	tests := []struct {
		name                                  string
		tourSpread, concertCount, careerYears int
		want                                  string
	}{
		{"score 0", 0, 0, 0, "Emerging"},
		{"score 10", 2, 2, 0, "Emerging"},
		{"score 11", 1, 3, 0, "Active"},
		{"score 25", 5, 5, 0, "Active"},
		{"score 26", 3, 6, 10, "Established"},
		{"score 40", 4, 10, 10, "Established"},
		{"score 41", 6, 9, 10, "Legendary"},
		{"large score", 20, 100, 100, "Legendary"},
		{"negative inputs", -1, -1, -1, "Emerging"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyActivity(tt.tourSpread, tt.concertCount, tt.careerYears); got != tt.want {
				t.Fatalf("ClassifyActivity(%d, %d, %d) = %q; want %q", tt.tourSpread, tt.concertCount, tt.careerYears, got, tt.want)
			}
		})
	}
}

func TestIsRisingStar(t *testing.T) {
	tests := []struct {
		name                                       string
		recentConcerts, totalConcerts, careerYears int
		want                                       bool
	}{
		{"subject example above threshold", 14, 40, 10, true},
		{"subject example below threshold", 10, 40, 10, false},
		{"exactly 150 percent is excluded", 12, 40, 10, false},
		{"one additional concert exceeds threshold", 13, 40, 10, true},
		{"fractional career average", 2, 5, 10, true},
		{"no recent concerts", 0, 20, 10, false},
		{"no career concerts", 2, 0, 10, false},
		{"zero career duration", 2, 20, 0, false},
		{"negative career duration", 2, 20, -1, false},
		{"negative total concerts", 2, -1, 10, false},
		{"negative recent concerts", -1, 20, 10, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRisingStar(tt.recentConcerts, tt.totalConcerts, tt.careerYears); got != tt.want {
				t.Fatalf("IsRisingStar(%d, %d, %d) = %t; want %t", tt.recentConcerts, tt.totalConcerts, tt.careerYears, got, tt.want)
			}
		})
	}
}
