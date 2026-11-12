package main

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

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
	var payload map[string]string
	if response.Code != 502 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || payload["status"] != "error" {
		t.Fatalf("refresh masked an upstream failure: %d %s", response.Code, response.Body)
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
