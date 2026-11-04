package activity

import "testing"

func TestScoreFromMetrics(t *testing.T) {
	tests := []struct {
		name         string
		tourSpread   int
		concertCount int
		careerYears  int
		want         int
	}{
		{name: "zero values", tourSpread: 0, concertCount: 0, careerYears: 0, want: 0},
		{name: "positive values", tourSpread: 3, concertCount: 4, careerYears: 12, want: 3*2 + 4*3 + minInt(10, 12/5)},
		{name: "negative values sanitized", tourSpread: -4, concertCount: -2, careerYears: -6, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scoreFromMetrics(tt.tourSpread, tt.concertCount, tt.careerYears); got != tt.want {
				t.Fatalf("scoreFromMetrics(%d, %d, %d) = %d; want %d", tt.tourSpread, tt.concertCount, tt.careerYears, got, tt.want)
			}
		})
	}
}

func TestCalculateScore_EquivalentToHelper(t *testing.T) {
	for _, tc := range []struct {
		tourSpread, concertCount, careerYears int
	}{
		{0, 0, 0},
		{2, 1, 4},
		{5, 3, 12},
		{8, 9, 30},
	} {
		if got := CalculateScore(tc.tourSpread, tc.concertCount, tc.careerYears); got != scoreFromMetrics(tc.tourSpread, tc.concertCount, tc.careerYears) {
			t.Fatalf("CalculateScore(%d, %d, %d) = %d; helper returned %d", tc.tourSpread, tc.concertCount, tc.careerYears, got, scoreFromMetrics(tc.tourSpread, tc.concertCount, tc.careerYears))
		}
	}
}

func TestClassifyActivity_Boundaries(t *testing.T) {
	cases := []struct {
		name         string
		tourSpread   int
		concertCount int
		careerYears  int
		want         string
	}{
		{name: "boundary-0", tourSpread: 0, concertCount: 0, careerYears: 0, want: "Emerging"},
		{name: "boundary-10", tourSpread: 2, concertCount: 2, careerYears: 0, want: "Emerging"},
		{name: "boundary-11", tourSpread: 1, concertCount: 3, careerYears: 0, want: "Active"},
		{name: "boundary-25", tourSpread: 5, concertCount: 5, careerYears: 0, want: "Active"},
		{name: "boundary-26", tourSpread: 3, concertCount: 6, careerYears: 10, want: "Established"},
		{name: "boundary-40", tourSpread: 4, concertCount: 10, careerYears: 10, want: "Established"},
		{name: "boundary-41", tourSpread: 6, concertCount: 9, careerYears: 10, want: "Legendary"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyActivity(tc.tourSpread, tc.concertCount, tc.careerYears); got != tc.want {
				t.Fatalf("ClassifyActivity(%d, %d, %d) = %q; want %q", tc.tourSpread, tc.concertCount, tc.careerYears, got, tc.want)
			}
		})
	}
}

func TestIsRisingStar(t *testing.T) {
	if !IsRisingStar(12, 20, 8) {
		t.Fatal("expected rising star status when recent tempo exceeds long-term average by 50%+")
	}

	if IsRisingStar(2, 10, 10) {
		t.Fatal("did not expect rising star status when tempo is below threshold")
	}

	if IsRisingStar(4, 12, 8) {
		t.Fatal("did not expect rising star status below the 50% threshold")
	}
}
