package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"tour-track/internal/api"
	"tour-track/internal/handlers"
	"tour-track/internal/service"
)

//go:embed templates/*.html static/*
var assets embed.FS

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Printf("Tour Track: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	baseURL := os.Getenv("TOUR_TRACK_API_URL")
	if baseURL == "" {
		baseURL = api.DefaultBaseURL
	}
	svc := service.NewTourTrackService(baseURL)
	if os.Getenv("TOUR_TRACK_DEMO") == "1" {
		var err error
		svc, err = service.NewDemoService()
		if err != nil {
			return fmt.Errorf("load demo snapshot: %w", err)
		}
		log.Print("Demo mode: using the supplied API snapshot from 2026-11-12")
	}
	handler, err := newApplication(svc)
	if err != nil {
		return err
	}
	addr, err := listenAddress()
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	stopped := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				log.Printf("shutdown: %v", err)
				_ = server.Close()
			}
		case <-stopped:
		}
	}()
	defer func() {
		close(stopped)
		<-shutdownDone
	}()
	log.Printf("Tour Track listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}
	return nil
}

func newApplication(svc *service.TourTrackService) (http.Handler, error) {
	h, err := handlers.New(svc, assets)
	if err != nil {
		return nil, fmt.Errorf("load templates: %w", err)
	}
	staticFiles, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("load static files: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", readOnly(http.StripPrefix("/static/", http.FileServerFS(staticFiles))))
	mux.Handle("/health", readOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})))
	mux.HandleFunc("/api/refresh/status", h.RefreshStatus)
	mux.HandleFunc("/api/refresh", h.Refresh)
	mux.HandleFunc("/artist/", h.Artist)
	mux.HandleFunc("/", h.Index)
	return recoverMiddleware(mux), nil
}

// Hosting platforms commonly provide PORT; an explicit bind address takes priority.
func listenAddress() (string, error) {
	if addr := os.Getenv("TOUR_TRACK_ADDR"); addr != "" {
		return addr, nil
	}
	if port := os.Getenv("PORT"); port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return "", errors.New("PORT must be an integer between 1 and 65535")
		}
		return ":" + strconv.Itoa(number), nil
	}
	return ":8080", nil
}

func readOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: http: data:; style-src 'self'; script-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic serving %s: %v", r.URL.Path, recovered)
				http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
