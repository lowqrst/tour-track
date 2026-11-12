package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFetchAllUsesRealSchemaAndConcurrentEndpoints(t *testing.T) {
	var started atomic.Int32
	var once sync.Once
	gate := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if started.Add(1) == 4 {
			once.Do(func() { close(gate) })
		}
		select {
		case <-gate:
		case <-r.Context().Done():
			return
		}
		fixture, err := demoData.ReadFile("data" + r.URL.Path + ".json")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	snapshot, err := NewClient(server.URL).FetchAllContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Artists) != 52 || snapshot.Artists[0].ID != "1" || snapshot.Artists[0].Name != "Queen" || snapshot.Artists[0].StartYear != 1970 {
		t.Fatalf("incorrect artists schema: %+v", snapshot.Artists)
	}
	if len(snapshot.Locations) == 0 || len(snapshot.Relations) == 0 || len(snapshot.Dates) == 0 {
		t.Fatal("all four endpoints must contribute to the snapshot")
	}
	if snapshot.Dates[0].RawDate != "*23-08-2019" || snapshot.Dates[0].Date != "23-08-2019" {
		t.Fatalf("date normalization lost original value: %+v", snapshot.Dates[0])
	}
}

func TestFetchAllRejectsFailedOrInvalidSnapshots(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		status   int
		body     string
	}{
		{"HTTP failure", "/artists", 503, `{}`},
		{"redirect without location", "/artists", 302, `{}`},
		{"malformed JSON", "/artists", 200, `[{`},
		{"empty artists", "/artists", 200, `[]`},
		{"trailing JSON", "/artists", 200, `[] {}`},
		{"oversized response", "/artists", 200, strings.Repeat(" ", maxBodyBytes+1)},
		{"wrong API schema", "/locations", 200, `[]`},
		{"missing artist joins", "/locations", 200, `{"index":[]}`},
		{"missing dates array", "/dates", 200, `{"index":[{"id":1}]}`},
		{"duplicate ID", "/artists", 200, `[{"id":1,"name":"A","creationDate":2000},{"id":1,"name":"B","creationDate":2001}]`},
		{"unknown relation ID", "/relation", 200, `{"index":[{"id":999,"datesLocations":{}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == test.endpoint {
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.body))
					return
				}
				fixture, err := demoData.ReadFile("data" + r.URL.Path + ".json")
				if err != nil {
					http.NotFound(w, r)
					return
				}
				_, _ = w.Write(fixture)
			}))
			defer server.Close()
			if snapshot, err := NewClient(server.URL).FetchAll(); err == nil || len(snapshot.Artists) != 0 {
				t.Fatalf("expected failure without a partial snapshot, got %d artists and %v", len(snapshot.Artists), err)
			}
		})
	}
}

func TestFetchAllHonorsCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := NewClient(server.URL).FetchAllContext(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

func TestDemoSnapshotHasAllArtists(t *testing.T) {
	snapshot, err := LoadDemo()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Artists) != 52 {
		t.Fatalf("demo has %d artists, want 52", len(snapshot.Artists))
	}
}
