package main

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"tour-track/internal/api"
	"tour-track/internal/handlers"
	"tour-track/internal/service"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered from panic: %v", r)
		}
	}()

	errorTemplate, err := template.ParseFiles(filepath.Join("templates", "error.html"))
	if err != nil {
		log.Printf("failed to load error template: %v", err)
		errorTemplate = template.Must(template.New("error.html").Parse(`<!doctype html><html><body><h1>{{.Status}}</h1><p>{{.Message}}</p></body></html>`))
	}

	baseURL := os.Getenv("TOUR_TRACK_API_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8081/api"
		log.Println("TOUR_TRACK_API_URL is not set; starting the built-in mock API on :8081")
		mockServer := api.StartMockAPIServer()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := mockServer.Shutdown(ctx); err != nil {
				log.Printf("failed to shut down mock API server: %v", err)
			}
		}()
	} else if strings.HasPrefix(baseURL, "http://localhost:8081/api") || strings.HasPrefix(baseURL, "http://127.0.0.1:8081/api") {
		log.Println("Using local mock API on :8081")
		mockServer := api.StartMockAPIServer()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := mockServer.Shutdown(ctx); err != nil {
				log.Printf("failed to shut down mock API server: %v", err)
			}
		}()
	}

	svc := service.NewTourTrackService(baseURL)
	h, err := handlers.New(svc)
	if err != nil {
		log.Printf("failed to initialize handlers: %v", err)
		h = nil
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if h != nil {
		mux.HandleFunc("/api/refresh", h.Refresh)
		mux.HandleFunc("/artist/", h.Artist)
		mux.HandleFunc("/", h.Index)
	} else {
		mux.HandleFunc("/api/refresh", func(w http.ResponseWriter, r *http.Request) {
			renderErrorPage(w, http.StatusServiceUnavailable, "Service unavailable", "The application is temporarily unavailable.", errorTemplate)
		})
		mux.HandleFunc("/artist/", func(w http.ResponseWriter, r *http.Request) {
			renderErrorPage(w, http.StatusServiceUnavailable, "Service unavailable", "The application is temporarily unavailable.", errorTemplate)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			renderErrorPage(w, http.StatusServiceUnavailable, "Service unavailable", "The application is temporarily unavailable.", errorTemplate)
		})
	}

	server := &http.Server{
		Addr:              ":8080",
		Handler:           recoverMiddleware(mux, errorTemplate),
		ReadHeaderTimeout: 3 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}

	log.Println("Tour Track server listening on :8080")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Printf("server error: %v", err)
	}
}

func recoverMiddleware(next http.Handler, errorTemplate *template.Template) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("recovered from panic while serving %s: %v", r.URL.Path, rec)
				renderErrorPage(w, http.StatusInternalServerError, "Internal server error", "Something went wrong while processing the request.", errorTemplate)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func renderErrorPage(w http.ResponseWriter, status int, title, message string, tmpl *template.Template) {
	if tmpl == nil {
		http.Error(w, message, status)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := tmpl.Execute(w, struct {
		Status  int
		Title   string
		Message string
	}{
		Status:  status,
		Title:   title,
		Message: message,
	}); err != nil {
		log.Printf("failed to render error template: %v", err)
		_, _ = w.Write([]byte(message))
	}
}
