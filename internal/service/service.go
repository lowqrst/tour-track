package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"tour-track/internal/activity"
	"tour-track/internal/api"
	"tour-track/internal/models"
)

// EventBus provides a lightweight async event channel for UI refreshes and notifications.
type EventBus struct {
	events chan string
}

func NewEventBus() *EventBus {
	return &EventBus{events: make(chan string, 32)}
}

func (b *EventBus) Publish(message string) {
	if b == nil || b.events == nil {
		return
	}
	go func() {
		b.events <- message
	}()
}

func (b *EventBus) Subscribe() <-chan string {
	if b == nil {
		return nil
	}
	return b.events
}

type ConcertEvent struct {
	Date    string
	Venue   string
	City    string
	Country string
}

type FilterOptions struct {
	Query    string
	Style    string
	Year     string
	Member   string
	Activity string
	Sort     string
	View     string
}

// ArtistProfile contains enriched artist data with activity scoring.
type ArtistProfile struct {
	Artist        models.Artist
	Countries     []string
	ConcertDates  []models.ConcertDate
	ConcertEvents []ConcertEvent
	Score         int
	ActivityLabel string
	RisingStar    bool
}

// TourTrackService coordinates external API access and activity classification.
type TourTrackService struct {
	client   *api.Client
	profiles []ArtistProfile
	events   *EventBus
}

func NewTourTrackService(baseURL string) *TourTrackService {
	service := &TourTrackService{
		client: api.NewClient(baseURL),
		events: NewEventBus(),
	}
	service.events.Publish("tour-track booted")
	return service
}

func (s *TourTrackService) Load() ([]ArtistProfile, error) {
	profiles, err := s.buildProfiles()
	if err != nil {
		return nil, err
	}
	s.profiles = profiles
	return profiles, nil
}

func (s *TourTrackService) GetArtistByID(id string) (ArtistProfile, bool) {
	if len(s.profiles) == 0 {
		profiles, err := s.Load()
		if err != nil {
			return ArtistProfile{}, false
		}
		s.profiles = profiles
	}
	for _, profile := range s.profiles {
		if profile.Artist.ID == id {
			return profile, true
		}
	}
	return ArtistProfile{}, false
}

func (s *TourTrackService) TriggerRefreshAsync() <-chan string {
	result := make(chan string, 1)
	go func() {
		defer close(result)
		s.events.Publish("refresh started")
		time.Sleep(300 * time.Millisecond)
		profiles, err := s.buildProfiles()
		if err != nil {
			message := fmt.Sprintf("refresh failed: %v", err)
			s.events.Publish(message)
			result <- message
			return
		}
		s.profiles = profiles
		message := fmt.Sprintf("updated %d artists", len(profiles))
		s.events.Publish(message)
		result <- message
	}()
	return result
}

func (s *TourTrackService) buildProfiles() ([]ArtistProfile, error) {
	response, err := s.client.FetchAll()
	if err != nil {
		return nil, err
	}

	locationByID := make(map[string]models.Location, len(response.Locations))
	for _, location := range response.Locations {
		locationByID[location.ID] = location
	}

	dateToLocationIDs := make(map[string][]string)
	for _, relation := range response.Relations {
		dateToLocationIDs[relation.DateID] = append(dateToLocationIDs[relation.DateID], relation.LocationID)
	}

	datesByArtist := make(map[string][]models.ConcertDate)
	for _, date := range response.Dates {
		datesByArtist[date.ArtistID] = append(datesByArtist[date.ArtistID], date)
	}

	countriesByArtist := make(map[string]map[string]struct{})
	for _, location := range response.Locations {
		if countriesByArtist[location.ArtistID] == nil {
			countriesByArtist[location.ArtistID] = map[string]struct{}{}
		}
		countriesByArtist[location.ArtistID][location.Country] = struct{}{}
	}

	profiles := make([]ArtistProfile, 0, len(response.Artists))
	for _, artist := range response.Artists {
		concertDates := datesByArtist[artist.ID]
		countries := make([]string, 0, len(countriesByArtist[artist.ID]))
		for country := range countriesByArtist[artist.ID] {
			countries = append(countries, country)
		}
		sort.Strings(countries)

		concertEvents := make([]ConcertEvent, 0, len(concertDates))
		seenDates := map[string]struct{}{}
		for _, concert := range concertDates {
			if _, ok := seenDates[concert.Date]; ok {
				continue
			}
			seenDates[concert.Date] = struct{}{}
			for _, locationID := range dateToLocationIDs[concert.ID] {
				location, ok := locationByID[locationID]
				if !ok {
					continue
				}
				concertEvents = append(concertEvents, ConcertEvent{
					Date:    concert.Date,
					Venue:   location.Venue,
					City:    location.City,
					Country: location.Country,
				})
			}
			if len(dateToLocationIDs[concert.ID]) == 0 {
				concertEvents = append(concertEvents, ConcertEvent{Date: concert.Date})
			}
		}

		careerYears := time.Now().Year() - artist.StartYear
		if careerYears < 0 {
			careerYears = 0
		}

		score := activity.CalculateScore(len(countries), len(concertDates), careerYears)
		label := activity.ClassifyActivity(len(countries), len(concertDates), careerYears)
		recentConcerts := countRecentConcerts(concertDates, 2)
		rising := activity.IsRisingStar(recentConcerts, len(concertDates), careerYears)

		profiles = append(profiles, ArtistProfile{
			Artist:        artist,
			Countries:     countries,
			ConcertDates:  concertDates,
			ConcertEvents: concertEvents,
			Score:         score,
			ActivityLabel: label,
			RisingStar:    rising,
		})
	}

	sort.Slice(profiles, func(i, j int) bool {
		return strings.ToLower(profiles[i].Artist.Name) < strings.ToLower(profiles[j].Artist.Name)
	})
	return profiles, nil
}

func countRecentConcerts(dates []models.ConcertDate, recentYears int) int {
	if len(dates) == 0 {
		return 0
	}
	currentYear := time.Now().Year()
	cutoffYear := currentYear - recentYears
	count := 0
	for _, date := range dates {
		parsed, err := time.Parse("2006-01-02", date.Date)
		if err != nil {
			continue
		}
		if parsed.Year() >= cutoffYear {
			count++
		}
	}
	return count
}

func FilterAndSortProfiles(profiles []ArtistProfile, opts FilterOptions) []ArtistProfile {
	filtered := make([]ArtistProfile, 0, len(profiles))
	for _, profile := range profiles {
		artist := profile.Artist
		if opts.Query != "" {
			needle := strings.ToLower(opts.Query)
			if !strings.Contains(strings.ToLower(artist.Name), needle) && !strings.Contains(strings.ToLower(artist.Style), needle) {
				continue
			}
		}
		if opts.Style != "" && !strings.EqualFold(artist.Style, opts.Style) {
			continue
		}
		if opts.Year != "" {
			wantYear, err := strconv.Atoi(opts.Year)
			if err == nil && artist.StartYear != wantYear {
				continue
			}
		}
		if opts.Member != "" {
			needle := strings.ToLower(opts.Member)
			matched := false
			for _, person := range artist.Members {
				if strings.Contains(strings.ToLower(person), needle) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if opts.Activity != "" && !strings.EqualFold(profile.ActivityLabel, opts.Activity) {
			continue
		}
		filtered = append(filtered, profile)
	}

	sortKey := strings.TrimSpace(strings.ToLower(opts.Sort))
	switch sortKey {
	case "concerts":
		sort.Slice(filtered, func(i, j int) bool { return len(filtered[i].ConcertDates) > len(filtered[j].ConcertDates) })
	case "spread":
		sort.Slice(filtered, func(i, j int) bool { return len(filtered[i].Countries) > len(filtered[j].Countries) })
	case "year":
		sort.Slice(filtered, func(i, j int) bool { return filtered[i].Artist.StartYear > filtered[j].Artist.StartYear })
	case "name":
		fallthrough
	default:
		sort.Slice(filtered, func(i, j int) bool {
			return strings.ToLower(filtered[i].Artist.Name) < strings.ToLower(filtered[j].Artist.Name)
		})
	}
	return filtered
}
