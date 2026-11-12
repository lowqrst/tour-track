package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"tour-track/internal/service"
)

func demoApplication(t *testing.T) http.Handler {
	t.Helper()
	svc, err := service.NewDemoService()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := newApplication(svc)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestApplicationRoutes(t *testing.T) {
	app := demoApplication(t)
	for _, tc := range []struct {
		path, method, contains string
		status                 int
	}{
		{"/", "GET", "Queen", 200},
		{"/?q=queen", "GET", "Queen", 200},
		{"/?view=table", "GET", "Queen", 200},
		{"/?view=list", "GET", "Queen", 200},
		{"/?q=there-is-no-such-artist", "GET", "No artists", 200},
		{"/?year=not-a-year", "GET", "Check the year", 400},
		{"/artist/1", "GET", "Queen", 200},
		{"/artist/0", "GET", "Artist not found", 404},
		{"/artist/999999", "GET", "Artist not found", 404},
		{"/artist/1/extra", "GET", "Artist not found", 404},
		{"/missing", "GET", "Page not found", 404},
		{"/", "POST", "Method not allowed", 405},
		{"/artist/1", "DELETE", "Method not allowed", 405},
		{"/api/refresh", "GET", "Use POST", 405},
		{"/api/refresh", "POST", `"status":"ok"`, 200},
		{"/health", "GET", "ok", 200},
		{"/health", "POST", "Method not allowed", 405},
		{"/static/styles.css", "POST", "Method not allowed", 405},
		{"/api/refresh/status", "GET", `"status":"ok"`, 200},
		{"/api/refresh/status", "POST", "Use GET", 405},
		{"/static/styles.css", "GET", ":root", 200},
		{"/static/app.js", "GET", "refresh", 200},
		{"/static/missing.css", "GET", "404", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			app.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.status || !strings.Contains(response.Body.String(), tc.contains) {
				t.Fatalf("status=%d, want %d, contains %q=%v; body: %.300s", response.Code, tc.status, tc.contains, strings.Contains(response.Body.String(), tc.contains), response.Body.String())
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing content type protection")
			}
		})
	}
}

func TestEverySnapshotArtistHasWorkingDetailAndActivityBadge(t *testing.T) {
	svc, err := service.NewDemoService()
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := svc.Load()
	if err != nil || len(profiles) != 52 {
		t.Fatalf("snapshot artists=%d, want 52; error=%v", len(profiles), err)
	}
	app, err := newApplication(svc)
	if err != nil {
		t.Fatal(err)
	}
	index := httptest.NewRecorder()
	app.ServeHTTP(index, httptest.NewRequest("GET", "/", nil))
	for _, profile := range profiles {
		t.Run(profile.Artist.Name, func(t *testing.T) {
			if !strings.Contains(index.Body.String(), "/artist/"+profile.Artist.ID) {
				t.Fatal("artist has no link in catalog")
			}
			detail := httptest.NewRecorder()
			app.ServeHTTP(detail, httptest.NewRequest("GET", "/artist/"+profile.Artist.ID, nil))
			for _, value := range []string{profile.Artist.Name, profile.ActivityLabel, profile.Artist.FirstAlbumDate} {
				if detail.Code != 200 || !strings.Contains(html.UnescapeString(detail.Body.String()), value) {
					t.Fatalf("detail status=%d; missing %q", detail.Code, value)
				}
			}
		})
	}
}

func TestAPIOutageReturnsErrorsAndPreservesCachedData(t *testing.T) {
	var unavailable atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unavailable.Load() {
			http.Error(w, "offline", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/artists":
			_, _ = w.Write([]byte(`[{"id":1,"name":"Test Band","image":"https://example.com/artist.jpg","members":["Member"],"creationDate":2000,"firstAlbum":"01-01-2001"}]`))
		case "/locations":
			_, _ = w.Write([]byte(`{"index":[{"id":1,"locations":["london-uk"]}]}`))
		case "/dates":
			_, _ = w.Write([]byte(`{"index":[{"id":1,"dates":["*01-01-2020"]}]}`))
		case "/relation":
			_, _ = w.Write([]byte(`{"index":[{"id":1,"datesLocations":{"london-uk":["01-01-2020"]}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	app, err := newApplication(service.NewTourTrackService(upstream.URL))
	if err != nil {
		t.Fatal(err)
	}
	request := func(handler http.Handler, method, path string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		return recorder
	}
	if response := request(app, "GET", "/"); response.Code != 200 {
		t.Fatalf("initial request: %d %.200s", response.Code, response.Body.String())
	}
	unavailable.Store(true)
	response := request(app, "POST", "/api/refresh")
	if response.Code != http.StatusAccepted {
		t.Fatalf("refresh did not start asynchronously: %d %s", response.Code, response.Body)
	}
	payload := waitForRefresh(t, app)
	if payload.Status != "error" || !payload.Stale {
		t.Fatalf("refresh masked an upstream failure: %+v", payload)
	}
	if response := request(app, "GET", "/"); response.Code != 200 || !strings.Contains(response.Body.String(), "Test Band") {
		t.Fatal("refresh failure lost previously cached artist data")
	}
	coldApp, err := newApplication(service.NewTourTrackService(upstream.URL))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/artist/1"} {
		if response := request(coldApp, "GET", path); response.Code != 503 {
			t.Fatalf("cold outage at %s: status=%d, want 503", path, response.Code)
		}
	}
	if response := request(coldApp, "GET", "/health"); response.Code != 200 {
		t.Fatal("an API outage stopped the server")
	}
}

type refreshResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Stale   bool   `json:"stale"`
}

func waitForRefresh(t *testing.T, app http.Handler) refreshResponse {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		response := httptest.NewRecorder()
		app.ServeHTTP(response, httptest.NewRequest("GET", "/api/refresh/status", nil))
		var payload refreshResponse
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil {
			t.Fatalf("invalid refresh status: %d %s", response.Code, response.Body)
		}
		if payload.Status != "running" {
			return payload
		}
		select {
		case <-deadline:
			t.Fatal("background refresh did not finish")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestBackgroundRefreshKeepsCachedPagesAvailableAndCoalescesClicks(t *testing.T) {
	var block atomic.Bool
	var requests atomic.Int32
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if block.Load() {
			started <- struct{}{}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		data, err := os.ReadFile("internal/api/data" + r.URL.Path + ".json")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	defer upstream.Close()
	svc := service.NewTourTrackService(upstream.URL)
	if _, err := svc.Load(); err != nil {
		t.Fatal(err)
	}
	app, err := newApplication(svc)
	if err != nil {
		t.Fatal(err)
	}
	block.Store(true)
	response := httptest.NewRecorder()
	app.ServeHTTP(response, httptest.NewRequest("POST", "/api/refresh", nil))
	if response.Code != 202 || response.Header().Get("Location") != "/api/refresh/status" {
		t.Fatalf("expected immediate job acknowledgment: %d %s", response.Code, response.Body)
	}
	for range 4 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("background API requests did not start")
		}
	}
	response = httptest.NewRecorder()
	app.ServeHTTP(response, httptest.NewRequest("POST", "/api/refresh", nil))
	if response.Code != 202 {
		t.Fatalf("second click did not join the running job: %d", response.Code)
	}
	pageReady := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		page := httptest.NewRecorder()
		app.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
		pageReady <- page
	}()
	select {
	case page := <-pageReady:
		if page.Code != 200 || !strings.Contains(page.Body.String(), "Queen") {
			t.Fatal("cached catalog became unavailable during refresh")
		}
	case <-time.After(time.Second):
		unblock()
		t.Fatal("catalog waited for the upstream refresh")
	}
	unblock()
	if result := waitForRefresh(t, app); result.Status != "ok" {
		t.Fatalf("refresh failed: %+v", result)
	}
	if count := requests.Load(); count != 8 {
		t.Fatalf("duplicate clicks started additional requests: %d, want 8 including initial load", count)
	}
}

func TestListenAddressSupportsHostedPort(t *testing.T) {
	for _, tc := range []struct {
		addr, port, want string
		invalid          bool
	}{
		{"", "", ":8080", false},
		{"", "9000", ":9000", false},
		{"127.0.0.1:8085", "9000", "127.0.0.1:8085", false},
		{"", "0", "", true},
		{"", "65536", "", true},
		{"", "invalid", "", true},
	} {
		t.Run(tc.addr+"/"+tc.port, func(t *testing.T) {
			t.Setenv("TOUR_TRACK_ADDR", tc.addr)
			t.Setenv("PORT", tc.port)
			got, err := listenAddress()
			if (err != nil) != tc.invalid || got != tc.want {
				t.Fatalf("listenAddress() = %q, %v", got, err)
			}
		})
	}
}

func TestRecentActivityProducesRisingStarBadgeAndRateExplanation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recentDate := time.Now().AddDate(0, -1, 0).Format("02-01-2006")
		switch r.URL.Path {
		case "/artists":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": 1, "name": "Rising Test Artist", "creationDate": time.Now().Year() - 10,
				"firstAlbum": "01-01-2020", "members": []string{"A Member"},
			}})
		case "/locations":
			_, _ = w.Write([]byte(`{"index":[{"id":1,"locations":["london-uk"]}]}`))
		case "/dates":
			_ = json.NewEncoder(w).Encode(map[string]any{"index": []map[string]any{{"id": 1, "dates": []string{recentDate}}}})
		case "/relation":
			_ = json.NewEncoder(w).Encode(map[string]any{"index": []map[string]any{{"id": 1, "datesLocations": map[string][]string{"london-uk": {recentDate}}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	app, err := newApplication(service.NewTourTrackService(upstream.URL))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/artist/1"} {
		page := httptest.NewRecorder()
		app.ServeHTTP(page, httptest.NewRequest("GET", path, nil))
		if page.Code != 200 || !strings.Contains(page.Body.String(), "Rising Star") {
			t.Fatalf("recent activity did not produce a badge at %s: %d", path, page.Code)
		}
		if path == "/artist/1" && (!strings.Contains(page.Body.String(), "0.50") || !strings.Contains(page.Body.String(), "0.10")) {
			t.Fatal("artist detail did not explain the recent and career concert rates")
		}
	}
}

func TestPanicRecoveryAndCrossSiteRefresh(t *testing.T) {
	panicking := recoverMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("test failure")
	}))
	recorder := httptest.NewRecorder()
	panicking.ServeHTTP(recorder, httptest.NewRequest("GET", "/", nil))
	if recorder.Code != 500 || strings.Contains(recorder.Body.String(), "test failure") {
		t.Fatal("panic was not handled safely")
	}
	app := demoApplication(t)
	request := httptest.NewRequest("POST", "/api/refresh", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	recorder = httptest.NewRecorder()
	app.ServeHTTP(recorder, request)
	if recorder.Code != 403 {
		t.Fatalf("cross-site refresh status=%d, want 403", recorder.Code)
	}
}
