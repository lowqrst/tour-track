package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tour-track/internal/models"
)

func TestProfilesJoinByArtistIDAndUseListedDates(t *testing.T) {
	snapshot := models.APIResponse{
		Artists: []models.Artist{{ID: "2", Name: "Second", StartYear: 2020}, {ID: "1", Name: "First", StartYear: 2000}},
		Locations: []models.Location{
			{ArtistID: "1", Name: "london-uk"}, {ArtistID: "2", Name: "paris-france"},
			{ArtistID: "1", Name: "manchester-united_kingdom"},
			{ArtistID: "1", Name: "new_york-usa"}, {ArtistID: "1", Name: "chicago-us"},
			{ArtistID: "1", Name: "rio-brasil"}, {ArtistID: "1", Name: "sao_paulo-brazil"},
		},
		Dates: []models.ConcertDate{
			{ArtistID: "2", Date: "01-01-2020"},
			{ArtistID: "1", Date: "01-10-2026", RawDate: "*01-10-2026"},
			{ArtistID: "1", Date: "01-10-2026"},
		},
		Relations: []models.Relation{
			{ArtistID: "2", Location: "paris-france", Dates: []string{"01-01-2020"}},
			{ArtistID: "1", Location: "london-uk", Dates: []string{"*01-10-2026", "02-10-2026", "broken"}},
		},
	}
	profiles := buildProfiles(snapshot, time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC))
	first := profiles[0]
	if first.Artist.ID != "1" || first.Score != 17 || first.CareerYears != 26 || first.CareerBonus != 5 {
		t.Fatalf("wrong joined metrics: %+v", first)
	}
	if first.RecentConcerts != 2 || first.RecentRate != 1 || first.CareerRate != 2.0/26.0 || !first.RisingStar {
		t.Fatalf("incorrect recent and career rates: %+v", first)
	}
	if !reflect.DeepEqual(first.Countries, []string{"Brazil", "United Kingdom", "United States"}) {
		t.Fatalf("countries not deduplicated by normalized country: %v", first.Countries)
	}
	if len(first.ConcertDates) != 2 || len(first.ConcertEvents) != 3 || first.ConcertEvents[0].City != "London" || first.ConcertEvents[0].DateISO != "2026-10-01" {
		t.Fatalf("incorrect dates/relation handling: %+v", first)
	}
	if profiles[1].Countries[0] != "France" || len(profiles[1].ConcertDates) != 1 {
		t.Fatalf("second artist data mixed with first: %+v", profiles[1])
	}
	later := buildProfiles(snapshot, time.Date(2031, 11, 12, 0, 0, 0, 0, time.UTC))[0]
	if later.CareerYears != 31 || later.Score != first.Score+1 {
		t.Fatalf("career must change with processing year: %+v", later)
	}
}

func TestRecentConcertWindowExcludesFutureAndInvalidDates(t *testing.T) {
	now := time.Date(2026, 11, 12, 15, 0, 0, 0, time.FixedZone("UTC+5", 5*60*60))
	dates := []models.ConcertDate{
		{Date: "12-11-2024"}, {Date: "11-11-2024"}, {Date: "*12-11-2026"},
		{Date: "13-11-2026"}, {Date: "01-06-2025"}, {Date: "invalid"},
	}
	if got := countRecentConcerts(dates, now); got != 3 {
		t.Fatalf("recent concerts = %d, want 3", got)
	}
}

func TestFutureStartYearHasZeroCareer(t *testing.T) {
	profiles := buildProfiles(models.APIResponse{Artists: []models.Artist{{ID: "1", Name: "Future", StartYear: 2030}}}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if profiles[0].CareerYears != 0 || profiles[0].Score != 0 || profiles[0].RisingStar {
		t.Fatalf("future creation year produced invalid metrics: %+v", profiles[0])
	}
}

func TestCacheRetainsLastGoodSnapshotAndBacksOff(t *testing.T) {
	var requests atomic.Int32
	var fail atomic.Bool
	server := snapshotServer(t, &requests, &fail)
	defer server.Close()
	svc := NewTourTrackService(server.URL)
	for range 2 {
		profiles, err := svc.Load()
		if err != nil || len(profiles) != 52 {
			t.Fatalf("load = %d artists, %v", len(profiles), err)
		}
	}
	if got := requests.Load(); got != 4 {
		t.Fatalf("cache made %d requests, want 4", got)
	}
	fail.Store(true)
	svc.mu.Lock()
	svc.status.UpdatedAt = time.Now().Add(-cacheLifetime - time.Second)
	svc.mu.Unlock()
	profiles, err := svc.Load()
	waitForIdle(t, svc)
	if err != nil || len(profiles) != 52 || !svc.Status().Stale || svc.Status().Message == "" {
		t.Fatalf("last good data was lost: count=%d status=%+v error=%v", len(profiles), svc.Status(), err)
	}
	// Cancellation can stop sibling HTTP requests before they reach the server.
	// The count after the failed batch is therefore intentionally not assumed.
	previous := requests.Load()
	if _, err := svc.Load(); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != previous {
		t.Fatalf("backoff retried API: %d -> %d", previous, got)
	}
	fail.Store(false)
	if err := svc.Refresh(context.Background()); err != nil || svc.Status().Stale {
		t.Fatalf("successful refresh did not clear stale status: %v %+v", err, svc.Status())
	}
}

func TestCanceledColdLoadDoesNotCancelSharedRefresh(t *testing.T) {
	gate := make(chan struct{})
	started := make(chan struct{}, 4)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		started <- struct{}{}
		select {
		case <-gate:
			writeFixture(t, w, r)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	svc := NewTourTrackService(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := svc.LoadContext(ctx)
		result <- err
	}()
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("shared refresh did not start all four requests")
		}
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled cold reader returned %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("canceled cold reader waited for the shared upstream response")
	}
	if status := svc.Status(); !status.Refreshing || status.LastError != "" {
		t.Fatalf("visitor cancellation marked the API unavailable: %+v", status)
	}
	close(gate)
	profiles, err := svc.LoadContext(context.Background())
	if err != nil || len(profiles) != 52 {
		t.Fatalf("second visitor could not use the shared refresh: %d artists, %v", len(profiles), err)
	}
	if requests.Load() != 4 {
		t.Fatalf("cold readers did not share one batch: %d requests", requests.Load())
	}
	if status := svc.Status(); status.LastError != "" || status.Stale || status.Refreshing {
		t.Fatalf("successful shared refresh left a failure state: %+v", status)
	}
}

func TestBackgroundRefreshKeepsCachedReadsResponsiveAndCoalesces(t *testing.T) {
	var block atomic.Bool
	var requests atomic.Int32
	gate := make(chan struct{})
	started := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if block.Load() {
			started <- struct{}{}
			select {
			case <-gate:
			case <-r.Context().Done():
				return
			}
		}
		writeFixture(t, w, r)
	}))
	defer server.Close()
	defer func() {
		select {
		case <-gate:
		default:
			close(gate)
		}
	}()
	svc := NewTourTrackService(server.URL)
	if _, err := svc.Load(); err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	if status := svc.StartRefresh(); !status.Refreshing {
		t.Fatalf("asynchronous refresh was not accepted: %+v", status)
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("background refresh did not start")
		}
	}
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			if !svc.StartRefresh().Refreshing {
				t.Error("concurrent refresh did not join the current job")
			}
		}()
	}
	group.Wait()
	readDone := make(chan error, 1)
	go func() {
		profiles, err := svc.Load()
		if err == nil && len(profiles) != 52 {
			err = errors.New("cached artists were lost")
		}
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cached reader blocked behind the network refresh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	waiterDone := make(chan error, 1)
	go func() { waiterDone <- svc.Refresh(ctx) }()
	select {
	case err := <-waiterDone:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("refresh waiter returned %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("refresh waiter ignored its context deadline")
	}
	if requests.Load() != 8 {
		t.Fatalf("concurrent refreshes started duplicate batches: %d requests; want 8 including initial load", requests.Load())
	}
	close(gate)
	waitForIdle(t, svc)
	if status := svc.Status(); status.Refreshing || status.LastError != "" {
		t.Fatalf("refresh did not finish successfully: %+v", status)
	}
}

func TestDemoRefreshDoesNotStartAnUpstreamJob(t *testing.T) {
	svc, err := NewDemoService()
	if err != nil {
		t.Fatal(err)
	}
	if status := svc.StartRefresh(); status.Source != "demo" || status.Refreshing || status.LastError != "" {
		t.Fatalf("demo snapshot started an upstream refresh: %+v", status)
	}
	if err := svc.Refresh(context.Background()); err != nil {
		t.Fatalf("refreshing immutable demo snapshot failed: %v", err)
	}
}

func waitForIdle(t *testing.T, svc *TourTrackService) {
	t.Helper()
	svc.mu.Lock()
	job := svc.inflight
	svc.mu.Unlock()
	if job == nil {
		return
	}
	select {
	case <-job.done:
	case <-time.After(2 * time.Second):
		t.Fatal("background refresh did not finish")
	}
}

func TestConcurrentLoadAndRefresh(t *testing.T) {
	var requests atomic.Int32
	var fail atomic.Bool
	server := snapshotServer(t, &requests, &fail)
	defer server.Close()
	svc := NewTourTrackService(server.URL)
	var group sync.WaitGroup
	for index := range 12 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			if index%3 == 0 {
				if err := svc.Refresh(context.Background()); err != nil {
					t.Error(err)
				}
			}
			profiles, err := svc.Load()
			if err != nil || len(profiles) != 52 {
				t.Errorf("concurrent load: %d artists, %v", len(profiles), err)
			}
			_, found, err := svc.ArtistContext(context.Background(), "1")
			if err != nil || !found {
				t.Errorf("concurrent artist lookup: found=%v error=%v", found, err)
			}
		}(index)
	}
	group.Wait()
}

func TestInitialFailureDoesNotInventDemoData(t *testing.T) {
	var requests atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	server := snapshotServer(t, &requests, &fail)
	defer server.Close()
	svc := NewTourTrackService(server.URL)
	profiles, err := svc.Load()
	if err == nil || len(profiles) != 0 || svc.Status().Source != "live" {
		t.Fatalf("failed live load pretended to have demo data: %+v %v", profiles, err)
	}
	if _, err := svc.Load(); err == nil {
		t.Fatal("backoff after initial failure must still report unavailable")
	}
}

func TestReturnedProfilesDoNotExposeMutableCachedSlices(t *testing.T) {
	svc, err := NewDemoService()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := svc.Load()
	if err != nil {
		t.Fatal(err)
	}
	id := profiles[0].Artist.ID
	originalMember := profiles[0].Artist.Members[0]
	originalDate := profiles[0].ConcertDates[0].Date
	profiles[0].Artist.Members[0] = "changed"
	profiles[0].ConcertDates[0].Date = "changed"
	profile, found, err := svc.ArtistContext(context.Background(), id)
	if err != nil || !found {
		t.Fatalf("artist lookup failed: %v", err)
	}
	if profile.Artist.Members[0] != originalMember || profile.ConcertDates[0].Date != originalDate {
		t.Fatal("modifying a returned profile corrupted the cached snapshot")
	}
}

func TestFilteringIncludesMembersCountriesAndRisingStars(t *testing.T) {
	profiles := []ArtistProfile{
		{Artist: models.Artist{ID: "1", Name: "Queen", StartYear: 1970, Members: []string{"Brian May"}}, Countries: []string{"Japan"}, ActivityLabel: "Legendary", Score: 50, RisingStar: true},
		{Artist: models.Artist{ID: "2", Name: "A Band", StartYear: 2000}, Countries: []string{"France"}, ActivityLabel: "Active", Score: 12},
	}
	for _, options := range []FilterOptions{
		{Query: "BRIAN"}, {Query: "Japan"}, {Country: "japan"},
		{Year: "1970"}, {Member: "May"}, {Activity: "Rising Star"},
		{Activity: "legendary", Country: "Japan", Query: "Queen"},
	} {
		got := FilterAndSortProfiles(profiles, options)
		if len(got) != 1 || got[0].Artist.ID != "1" {
			t.Errorf("filter %+v returned %+v", options, got)
		}
	}
	if got := FilterAndSortProfiles(profiles, FilterOptions{Year: "bad"}); len(got) != 0 {
		t.Fatal("invalid year must not silently remove the filter")
	}
	if got := FilterAndSortProfiles(profiles, FilterOptions{Sort: "score"}); got[0].Artist.ID != "1" {
		t.Fatal("score sorting did not put highest score first")
	}
	if profiles[0].Artist.ID != "1" {
		t.Fatal("filtering modified the original ordering")
	}
}

func snapshotServer(t *testing.T, requests *atomic.Int32, fail *atomic.Bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if fail.Load() {
			http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
			return
		}
		endpoint := strings.TrimPrefix(r.URL.Path, "/")
		if endpoint != "artists" && endpoint != "locations" && endpoint != "dates" && endpoint != "relation" {
			http.NotFound(w, r)
			return
		}
		writeFixture(t, w, r)
	}))
}

func writeFixture(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "api", "data", strings.TrimPrefix(r.URL.Path, "/")+".json"))
	if err != nil {
		t.Error(err)
		http.Error(w, "fixture unavailable", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(body)
}
