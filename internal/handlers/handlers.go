package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"tour-track/internal/service"
)

type Handler struct {
	templates *template.Template
	svc       *service.TourTrackService
}

type indexViewData struct {
	Profiles []service.ArtistProfile
	Styles   []string
	Years    []int
	Query    string
	Style    string
	Year     string
	Member   string
	Activity string
	Sort     string
	View     string
}

type artistViewData struct {
	Profile service.ArtistProfile
}

func New(svc *service.TourTrackService) (*Handler, error) {
	tpl, err := template.ParseGlob(filepath.Join("templates", "*.html"))
	if err != nil {
		return nil, err
	}
	return &Handler{templates: tpl, svc: svc}, nil
}

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		h.renderError(w, http.StatusNotFound, "Page not found", "The requested page does not exist.")
		return
	}

	profiles, err := h.svc.Load()
	if err != nil {
		profiles = nil
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	styleFilter := strings.TrimSpace(r.URL.Query().Get("style"))
	yearFilter := strings.TrimSpace(r.URL.Query().Get("year"))
	memberFilter := strings.TrimSpace(r.URL.Query().Get("member"))
	activityFilter := strings.TrimSpace(r.URL.Query().Get("activity"))
	sortFilter := strings.TrimSpace(r.URL.Query().Get("sort"))
	viewFilter := strings.TrimSpace(r.URL.Query().Get("view"))
	if sortFilter == "" {
		sortFilter = "name"
	}
	if viewFilter == "" {
		viewFilter = "cards"
	}

	filtered := service.FilterAndSortProfiles(profiles, service.FilterOptions{
		Query:    query,
		Style:    styleFilter,
		Year:     yearFilter,
		Member:   memberFilter,
		Activity: activityFilter,
		Sort:     sortFilter,
		View:     viewFilter,
	})
	styles := uniqueStyles(profiles)
	years := uniqueYears(profiles)

	pageData := indexViewData{
		Profiles: filtered,
		Styles:   styles,
		Years:    years,
		Query:    query,
		Style:    styleFilter,
		Year:     yearFilter,
		Member:   memberFilter,
		Activity: activityFilter,
		Sort:     sortFilter,
		View:     viewFilter,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, "index.html", pageData); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Template error", err.Error())
	}
}

func (h *Handler) Artist(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/artist/")
	if id == "" || id == "/" {
		h.renderError(w, http.StatusNotFound, "Artist not found", "The requested artist could not be found.")
		return
	}

	profile, ok := h.svc.GetArtistByID(id)
	if !ok {
		h.renderError(w, http.StatusNotFound, "Artist not found", "The requested artist could not be found.")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.templates.ExecuteTemplate(w, "artist.html", artistViewData{Profile: profile}); err != nil {
		h.renderError(w, http.StatusInternalServerError, "Template error", err.Error())
	}
}

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		h.renderError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET and POST are supported for this endpoint.")
		return
	}

	result := <-h.svc.TriggerRefreshAsync()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": result})
}

func (h *Handler) renderError(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := h.templates.ExecuteTemplate(w, "error.html", struct {
		Status  int
		Title   string
		Message string
	}{
		Status:  status,
		Title:   title,
		Message: message,
	}); err != nil {
		http.Error(w, message, status)
	}
}

func filterProfiles(profiles []service.ArtistProfile, query, style, year, member, activity string) []service.ArtistProfile {
	filtered := make([]service.ArtistProfile, 0, len(profiles))
	for _, profile := range profiles {
		artist := profile.Artist
		if query != "" {
			needle := strings.ToLower(query)
			if !strings.Contains(strings.ToLower(artist.Name), needle) && !strings.Contains(strings.ToLower(artist.Style), needle) {
				continue
			}
		}
		if style != "" && !strings.EqualFold(artist.Style, style) {
			continue
		}
		if year != "" {
			wantYear, err := strconv.Atoi(year)
			if err == nil && artist.StartYear != wantYear {
				continue
			}
		}
		if member != "" {
			needle := strings.ToLower(member)
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
		if activity != "" && !strings.EqualFold(profile.ActivityLabel, activity) {
			continue
		}
		filtered = append(filtered, profile)
	}
	return filtered
}

func uniqueStyles(profiles []service.ArtistProfile) []string {
	styles := make([]string, 0)
	seen := map[string]struct{}{}
	for _, profile := range profiles {
		style := profile.Artist.Style
		if style == "" {
			continue
		}
		if _, ok := seen[style]; ok {
			continue
		}
		seen[style] = struct{}{}
		styles = append(styles, style)
	}
	sort.Strings(styles)
	return styles
}

func uniqueYears(profiles []service.ArtistProfile) []int {
	years := make([]int, 0)
	seen := map[int]struct{}{}
	for _, profile := range profiles {
		if profile.Artist.StartYear == 0 {
			continue
		}
		if _, ok := seen[profile.Artist.StartYear]; ok {
			continue
		}
		seen[profile.Artist.StartYear] = struct{}{}
		years = append(years, profile.Artist.StartYear)
	}
	sort.Ints(years)
	return years
}

func (h *Handler) maybeReadTemplate(name string) string {
	if h == nil || h.templates == nil {
		return ""
	}
	return fmt.Sprintf("template %q is loaded", name)
}
