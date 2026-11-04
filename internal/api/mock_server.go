package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// StartMockAPIServer starts a lightweight in-process mock REST API that exposes
// the same four logical domains required by the application.
func StartMockAPIServer() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/artists", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fallbackArtists(), http.StatusOK)
	})
	mux.HandleFunc("/api/locations", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fallbackLocations(), http.StatusOK)
	})
	mux.HandleFunc("/api/dates", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fallbackDates(), http.StatusOK)
	})
	mux.HandleFunc("/api/relations", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, fallbackRelations(), http.StatusOK)
	})

	srv := &http.Server{
		Addr:              ":8081",
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("mock api server error: %v", err)
		}
	}()

	return srv
}

func writeJSON(w http.ResponseWriter, payload any, status int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		log.Printf("failed to encode JSON response: %v", err)
	}
}
