package service

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"tour-track/internal/activity"
	"tour-track/internal/api"
	"tour-track/internal/models"
)

const (
	cacheLifetime = 5 * time.Minute
	retryDelay    = 30 * time.Second
)

type ConcertEvent struct {
	Date     string
	DateISO  string
	City     string
	Country  string
	Location string
	Upcoming bool
}

type FilterOptions struct {
	Query    string
	Year     string
	Member   string
	Country  string
	Activity string
	Sort     string
	View     string
}

// ArtistProfile is rebuilt at request time from the cached, unscored snapshot.
type ArtistProfile struct {
	Artist         models.Artist
	Countries      []string
	ConcertDates   []models.ConcertDate
	ConcertEvents  []ConcertEvent
	CareerYears    int
	CareerBonus    int
	RecentConcerts int
	RecentRate     float64
	CareerRate     float64
	Score          int
	ActivityLabel  string
	RisingStar     bool
}

type DataStatus struct {
	Source     string
	UpdatedAt  time.Time
	Stale      bool
	Message    string
	Refreshing bool
	LastError  string
}

type refreshJob struct {
	done chan struct{}
	err  error
}

// TourTrackService protects its raw snapshot and refresh state with one mutex.
// A refresh publishes only a complete snapshot, so readers cannot see half an update.
type TourTrackService struct {
	client      *api.Client
	mu          sync.Mutex
	snapshot    models.APIResponse
	status      DataStatus
	nextAttempt time.Time
	inflight    *refreshJob
}

func NewTourTrackService(baseURL string) *TourTrackService {
	return &TourTrackService{client: api.NewClient(baseURL), status: DataStatus{Source: "live"}}
}

func NewDemoService() (*TourTrackService, error) {
	snapshot, err := api.LoadDemo()
	if err != nil {
		return nil, err
	}
	return &TourTrackService{
		snapshot: snapshot,
		status: DataStatus{
			Source:    "demo",
			UpdatedAt: time.Date(2026, time.November, 12, 0, 0, 0, 0, time.UTC),
			Message:   "Offline demo · API snapshot captured 12 November 2026. Concert listings may be historical.",
		},
	}, nil
}

func (s *TourTrackService) Load() ([]ArtistProfile, error) {
	return s.LoadContext(context.Background())
}

func (s *TourTrackService) LoadContext(ctx context.Context) ([]ArtistProfile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	now := time.Now()
	if s.status.Source != "demo" && (len(s.snapshot.Artists) == 0 || now.Sub(s.status.UpdatedAt) >= cacheLifetime) && !now.Before(s.nextAttempt) {
		s.startRefreshLocked()
	}
	snapshot := s.snapshot
	job := s.inflight
	s.mu.Unlock()
	if len(snapshot.Artists) > 0 {
		// Readers can keep using the immutable last good snapshot while the
		// upstream request is running. No network operation holds the mutex.
		return buildProfiles(snapshot, time.Now()), nil
	}
	if job == nil {
		return nil, ErrUnavailable
	}
	if err := waitForRefresh(ctx, job); err != nil {
		return nil, err
	}
	s.mu.Lock()
	snapshot = s.snapshot
	s.mu.Unlock()
	return buildProfiles(snapshot, time.Now()), nil
}

// ErrUnavailable represents a failed initial load while the API retry is delayed.
var ErrUnavailable = errors.New("artist API temporarily unavailable")

func (s *TourTrackService) ArtistContext(ctx context.Context, id string) (ArtistProfile, bool, error) {
	profiles, err := s.LoadContext(ctx)
	if err != nil {
		return ArtistProfile{}, false, err
	}
	for _, profile := range profiles {
		if profile.Artist.ID == id {
			return profile, true, nil
		}
	}
	return ArtistProfile{}, false, nil
}

func (s *TourTrackService) Refresh(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	job := s.startRefreshLocked()
	s.mu.Unlock()
	return waitForRefresh(ctx, job)
}

// StartRefresh starts one shared background refresh, or joins the current one.
// The returned status is captured before the worker can complete, so a newly
// accepted live refresh always reports Refreshing. Demo snapshots are immutable.
func (s *TourTrackService) StartRefresh() DataStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.startRefreshLocked()
	return s.status
}

func (s *TourTrackService) startRefreshLocked() *refreshJob {
	if s.status.Source == "demo" {
		return nil
	}
	if s.inflight != nil {
		return s.inflight
	}
	job := &refreshJob{done: make(chan struct{})}
	s.inflight = job
	s.status.Refreshing = true
	s.status.LastError = ""
	s.status.Stale = len(s.snapshot.Artists) > 0 && time.Since(s.status.UpdatedAt) >= cacheLifetime
	s.status.Message = ""
	go s.runRefresh(job)
	return job
}

func (s *TourTrackService) runRefresh(job *refreshJob) {
	// A visitor leaving must not cancel a shared refresh or mark the API down
	// for everyone else. FetchAllContext applies its own bounded timeout.
	snapshot, err := s.client.FetchAllContext(context.Background())
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.nextAttempt = time.Now().Add(retryDelay)
		s.status.Stale = len(s.snapshot.Artists) > 0
		s.status.Message = "The artist API is temporarily unavailable. Please try again shortly."
		if s.status.Stale {
			s.status.Message = "The API is unavailable. Showing the last successfully loaded data."
		}
		s.status.LastError = s.status.Message
		s.status.Refreshing = false
	} else {
		s.snapshot = snapshot
		s.status = DataStatus{Source: "live", UpdatedAt: time.Now()}
		s.nextAttempt = time.Time{}
	}
	job.err = err
	s.inflight = nil
	close(job.done)
}

func waitForRefresh(ctx context.Context, job *refreshJob) error {
	if job == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-job.done:
		return job.err
	}
}

func (s *TourTrackService) Status() DataStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func buildProfiles(snapshot models.APIResponse, now time.Time) []ArtistProfile {
	locations := make(map[string][]models.Location)
	for _, location := range snapshot.Locations {
		locations[location.ArtistID] = append(locations[location.ArtistID], location)
	}
	dates := make(map[string][]models.ConcertDate)
	for _, date := range snapshot.Dates {
		// Each listed date is one input concert. Repeated dates remain listed
		// because two shows can share a day; relation is not added to this count.
		dates[date.ArtistID] = append(dates[date.ArtistID], date)
	}
	relations := make(map[string][]models.Relation)
	for _, relation := range snapshot.Relations {
		relations[relation.ArtistID] = append(relations[relation.ArtistID], relation)
	}
	profiles := make([]ArtistProfile, 0, len(snapshot.Artists))
	for _, artist := range snapshot.Artists {
		artist.Members = append([]string(nil), artist.Members...)
		countries := make(map[string]bool)
		for _, location := range locations[artist.ID] {
			_, country := splitLocation(location.Name)
			if country != "" {
				countries[country] = true
			}
		}
		countryList := make([]string, 0, len(countries))
		for country := range countries {
			countryList = append(countryList, country)
		}
		sort.Strings(countryList)
		concertDates := append([]models.ConcertDate(nil), dates[artist.ID]...)
		careerYears := max(0, now.Year()-artist.StartYear)
		recentConcerts := countRecentConcerts(concertDates, now)
		careerRate := 0.0
		if careerYears > 0 {
			careerRate = float64(len(concertDates)) / float64(careerYears)
		}
		profiles = append(profiles, ArtistProfile{
			Artist: artist, Countries: countryList, ConcertDates: concertDates,
			ConcertEvents: makeEvents(relations[artist.ID], now),
			CareerYears:   careerYears, CareerBonus: min(10, careerYears/5),
			RecentConcerts: recentConcerts, RecentRate: float64(recentConcerts) / 2, CareerRate: careerRate,
			Score:         activity.CalculateScore(len(countryList), len(concertDates), careerYears),
			ActivityLabel: activity.ClassifyActivity(len(countryList), len(concertDates), careerYears),
			RisingStar:    activity.IsRisingStar(recentConcerts, len(concertDates), careerYears),
		})
	}
	sort.Slice(profiles, func(i, j int) bool { return profileLess(profiles[i], profiles[j]) })
	return profiles
}

func makeEvents(relations []models.Relation, now time.Time) []ConcertEvent {
	events := make([]ConcertEvent, 0)
	today := dateOnly(now)
	for _, relation := range relations {
		city, country := splitLocation(relation.Location)
		for _, raw := range relation.Dates {
			date := api.NormalizeDate(raw)
			event := ConcertEvent{Date: date, City: city, Country: country, Location: relation.Location}
			if parsed, err := time.Parse("02-01-2006", date); err == nil {
				event.Date = parsed.Format("02 Jan 2006")
				event.DateISO = parsed.Format("2006-01-02")
				event.Upcoming = !parsed.Before(today)
			}
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].DateISO != events[j].DateISO {
			if events[i].DateISO == "" {
				return false
			}
			if events[j].DateISO == "" {
				return true
			}
			return events[i].DateISO < events[j].DateISO
		}
		return events[i].Location < events[j].Location
	})
	return events
}

func countRecentConcerts(dates []models.ConcertDate, now time.Time) int {
	// Dates have no time zone or time-of-day. Use an inclusive rolling two-year
	// calendar-date window and exclude future and malformed entries.
	end := dateOnly(now)
	start := end.AddDate(-2, 0, 0)
	count := 0
	for _, date := range dates {
		parsed, err := time.Parse("02-01-2006", api.NormalizeDate(date.Date))
		if err == nil && !parsed.Before(start) && !parsed.After(end) {
			count++
		}
	}
	return count
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func splitLocation(location string) (string, string) {
	separator := strings.LastIndex(location, "-")
	if separator <= 0 || separator == len(location)-1 {
		return readable(location), ""
	}
	city := readable(location[:separator])
	country := strings.ToLower(strings.TrimSpace(location[separator+1:]))
	switch country {
	case "us", "usa", "united_states", "united_states_of_america":
		return city, "United States"
	case "uk", "united_kingdom":
		return city, "United Kingdom"
	case "brasil", "brazil":
		return city, "Brazil"
	case "philippine", "philippines":
		return city, "Philippines"
	case "korea", "south_korea":
		return city, "South Korea"
	default:
		return city, readable(country)
	}
}

func readable(value string) string {
	words := strings.Fields(strings.ReplaceAll(value, "_", " "))
	for index, word := range words {
		letters := []rune(strings.ToLower(word))
		if len(letters) > 0 {
			letters[0] = unicode.ToUpper(letters[0])
		}
		words[index] = string(letters)
	}
	return strings.Join(words, " ")
}

func FilterAndSortProfiles(profiles []ArtistProfile, options FilterOptions) []ArtistProfile {
	filtered := make([]ArtistProfile, 0, len(profiles))
	for _, profile := range profiles {
		artist := profile.Artist
		if options.Query != "" && !matchesQuery(profile, options.Query) {
			continue
		}
		if options.Year != "" && strconv.Itoa(artist.StartYear) != options.Year {
			continue
		}
		if options.Member != "" && !containsText(artist.Members, options.Member) {
			continue
		}
		if options.Country != "" && !containsCountry(profile.Countries, options.Country) {
			continue
		}
		if options.Activity != "" {
			if strings.EqualFold(options.Activity, "Rising Star") {
				if !profile.RisingStar {
					continue
				}
			} else if !strings.EqualFold(profile.ActivityLabel, options.Activity) {
				continue
			}
		}
		filtered = append(filtered, profile)
	}
	sort.Slice(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		switch options.Sort {
		case "concerts":
			if len(a.ConcertDates) != len(b.ConcertDates) {
				return len(a.ConcertDates) > len(b.ConcertDates)
			}
		case "spread":
			if len(a.Countries) != len(b.Countries) {
				return len(a.Countries) > len(b.Countries)
			}
		case "year":
			if a.Artist.StartYear != b.Artist.StartYear {
				return a.Artist.StartYear > b.Artist.StartYear
			}
		case "score":
			if a.Score != b.Score {
				return a.Score > b.Score
			}
		}
		return profileLess(a, b)
	})
	return filtered
}

func matchesQuery(profile ArtistProfile, query string) bool {
	texts := append([]string{profile.Artist.Name, strconv.Itoa(profile.Artist.StartYear), profile.Artist.FirstAlbumDate}, profile.Artist.Members...)
	texts = append(texts, profile.Countries...)
	for _, event := range profile.ConcertEvents {
		texts = append(texts, event.City, event.Country)
	}
	return containsText(texts, query)
}

func containsText(values []string, needle string) bool {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), needle) {
			return true
		}
	}
	return false
}

func containsCountry(countries []string, country string) bool {
	for _, candidate := range countries {
		if strings.EqualFold(candidate, country) {
			return true
		}
	}
	return false
}

func profileLess(a, b ArtistProfile) bool {
	left, right := strings.ToLower(a.Artist.Name), strings.ToLower(b.Artist.Name)
	if left == right {
		return a.Artist.ID < b.Artist.ID
	}
	return left < right
}
