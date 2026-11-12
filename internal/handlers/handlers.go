package handlers

import (
	"bytes"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"tour-track/internal/service"
)

type Handler struct {
	templates *template.Template
	svc       *service.TourTrackService
}

type sourceViewData struct {
	UpdatedAt string
	Stale     bool
	Demo      bool
	Warning   string
}

type indexViewData struct {
	sourceViewData
	Profiles       []service.ArtistProfile
	Featured       *service.ArtistProfile
	Years          []int
	Countries      []string
	Query          string
	Country        string
	Year           string
	Member         string
	Activity       string
	Sort           string
	View           string
	TotalArtists   int
	TotalConcerts  int
	TotalCountries int
}

type artistViewData struct {
	sourceViewData
	Profile service.ArtistProfile
}

func New(svc *service.TourTrackService, files fs.FS) (*Handler, error) {
	templates, err := template.ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Handler{templates: templates, svc: svc}, nil
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		h.renderError(w, http.StatusNotFound, "Page not found", "That page is off the setlist. Explore the artist directory instead.")
		return
	}
	if !h.allowRead(w, r) {
		return
	}
	profiles, err := h.svc.LoadContext(r.Context())
	if err != nil {
		log.Printf("load artists: %v", err)
		h.renderError(w, http.StatusServiceUnavailable, "The stage is taking a break", "The artist API is unavailable. Please try again shortly.")
		return
	}
	params := r.URL.Query()
	opts := service.FilterOptions{
		Query:    strings.TrimSpace(params.Get("q")),
		Country:  strings.TrimSpace(params.Get("country")),
		Year:     strings.TrimSpace(params.Get("year")),
		Member:   strings.TrimSpace(params.Get("member")),
		Activity: strings.TrimSpace(params.Get("activity")),
		Sort:     validChoice(params.Get("sort"), "name", "score", "concerts", "spread", "year"),
		View:     validChoice(params.Get("view"), "cards", "list", "table"),
	}
	if opts.Year != "" {
		if _, err := strconv.Atoi(opts.Year); err != nil {
			h.renderError(w, http.StatusBadRequest, "Check the year", "Use a whole year, such as 1970.")
			return
		}
	}
	data := indexViewData{
		sourceViewData: h.sourceData(),
		Profiles:       service.FilterAndSortProfiles(profiles, opts),
		Query:          opts.Query, Country: opts.Country, Year: opts.Year,
		Member: opts.Member, Activity: opts.Activity, Sort: opts.Sort, View: opts.View,
		TotalArtists: len(profiles),
	}
	years := make(map[int]struct{})
	countries := make(map[string]struct{})
	for i := range profiles {
		profile := &profiles[i]
		years[profile.Artist.StartYear] = struct{}{}
		data.TotalConcerts += len(profile.ConcertDates)
		for _, country := range profile.Countries {
			countries[country] = struct{}{}
		}
		if data.Featured == nil || profile.Score > data.Featured.Score {
			data.Featured = profile
		}
	}
	for year := range years {
		data.Years = append(data.Years, year)
	}
	for country := range countries {
		data.Countries = append(data.Countries, country)
	}
	sort.Ints(data.Years)
	sort.Strings(data.Countries)
	data.TotalCountries = len(countries)
	h.render(w, http.StatusOK, "index.html", data)
}

func (h *Handler) Artist(w http.ResponseWriter, r *http.Request) {
	if !h.allowRead(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/artist/")
	artistID, err := strconv.Atoi(id)
	if err != nil || artistID <= 0 || strconv.Itoa(artistID) != id {
		h.artistNotFound(w)
		return
	}
	profile, found, err := h.svc.ArtistContext(r.Context(), id)
	if err != nil {
		log.Printf("load artist %s: %v", id, err)
		h.renderError(w, http.StatusServiceUnavailable, "The stage is taking a break", "The artist API is unavailable. Please try again shortly.")
		return
	}
	if !found {
		h.artistNotFound(w)
		return
	}
	h.render(w, http.StatusOK, "artist.html", artistViewData{sourceViewData: h.sourceData(), Profile: profile})
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.writeJSON(w, http.StatusMethodNotAllowed, "error", "Use POST to refresh the artist directory.")
		return
	}
	// Reject cross-site browser writes while allowing non-browser API clients.
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		h.writeJSON(w, http.StatusForbidden, "error", "Refresh the directory from the Tour Track website.")
		return
	}
	if err := h.svc.Refresh(r.Context()); err != nil {
		log.Printf("refresh artists: %v", err)
		h.writeJSON(w, http.StatusBadGateway, "error", "The API could not be refreshed. Any previously loaded data is still available.")
		return
	}
	h.writeJSON(w, http.StatusOK, "ok", "Artist directory refreshed.")
}

func (h *Handler) sourceData() sourceViewData {
	status := h.svc.Status()
	updated := ""
	if !status.UpdatedAt.IsZero() {
		updated = status.UpdatedAt.UTC().Format("02 Jan 2006, 15:04 UTC")
	}
	return sourceViewData{UpdatedAt: updated, Stale: status.Stale, Demo: status.Source == "demo", Warning: status.Message}
}

func (h *Handler) allowRead(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	h.renderError(w, http.StatusMethodNotAllowed, "Method not allowed", "This page supports GET and HEAD requests.")
	return false
}

func (h *Handler) artistNotFound(w http.ResponseWriter) {
	h.renderError(w, http.StatusNotFound, "Artist not found", "We could not find that artist. Discover someone else in the directory.")
}

func (h *Handler) renderError(w http.ResponseWriter, status int, title, message string) {
	h.render(w, status, "error.html", struct {
		Status         int
		Title, Message string
	}{status, title, message})
}

func (h *Handler) render(w http.ResponseWriter, status int, name string, data any) {
	// Buffer templates so rendering failures cannot send a partial success page.
	var page bytes.Buffer
	if err := h.templates.ExecuteTemplate(&page, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "This page is temporarily unavailable. Please try again.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "60")
	}
	w.WriteHeader(status)
	_, _ = w.Write(page.Bytes())
}

func (h *Handler) writeJSON(w http.ResponseWriter, code int, status, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status, "message": message})
}

func validChoice(value string, choices ...string) string {
	for _, choice := range choices {
		if value == choice {
			return choice
		}
	}
	return choices[0]
}
