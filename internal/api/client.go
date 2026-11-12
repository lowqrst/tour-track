package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tour-track/internal/models"
)

const (
	DefaultBaseURL = "https://groupietrackers.01edu.ai/api"
	maxBodyBytes   = 2 << 20
)

// Client fetches a coherent snapshot; partial responses are never published.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 6 * time.Second},
	}
}

func (c *Client) FetchAll() (models.APIResponse, error) {
	return c.FetchAllContext(context.Background())
}

// FetchAllContext uses independent goroutines and a buffered result channel.
// Cancellation stops sibling requests and cannot leave a sender blocked.
func (c *Client) FetchAllContext(ctx context.Context) (models.APIResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	type result struct {
		endpoint string
		body     []byte
		err      error
	}
	results := make(chan result, 4)
	for _, endpoint := range []string{"artists", "locations", "dates", "relation"} {
		go func(endpoint string) {
			body, err := c.fetchJSON(ctx, endpoint)
			results <- result{endpoint: endpoint, body: body, err: err}
		}(endpoint)
	}
	payloads := make(map[string][]byte, 4)
	for range 4 {
		select {
		case <-ctx.Done():
			return models.APIResponse{}, fmt.Errorf("fetch API snapshot: %w", ctx.Err())
		case result := <-results:
			if result.err != nil {
				return models.APIResponse{}, result.err
			}
			payloads[result.endpoint] = result.body
		}
	}
	return decodeSnapshot(payloads)
}

func (c *Client) fetchJSON(ctx context.Context, endpoint string) ([]byte, error) {
	if c == nil || c.BaseURL == "" {
		return nil, errors.New("missing API base URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/"+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("prepare %s request: %w", endpoint, err)
	}
	request.Header.Set("Accept", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 6 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", endpoint, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", endpoint, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", endpoint, err)
	}
	if len(body) > maxBodyBytes {
		return nil, fmt.Errorf("%s response exceeds %d bytes", endpoint, maxBodyBytes)
	}
	return body, nil
}

type artistRecord struct {
	ID           int      `json:"id"`
	Name         string   `json:"name"`
	Image        string   `json:"image"`
	Members      []string `json:"members"`
	CreationDate int      `json:"creationDate"`
	FirstAlbum   string   `json:"firstAlbum"`
}

type locationRecord struct {
	ID        int      `json:"id"`
	Locations []string `json:"locations"`
}

type dateRecord struct {
	ID    int      `json:"id"`
	Dates []string `json:"dates"`
}

type relationRecord struct {
	ID             int                 `json:"id"`
	DatesLocations map[string][]string `json:"datesLocations"`
}

func decodeSnapshot(payloads map[string][]byte) (models.APIResponse, error) {
	var artists []artistRecord
	var locations struct {
		Index []locationRecord `json:"index"`
	}
	var dates struct {
		Index []dateRecord `json:"index"`
	}
	var relations struct {
		Index []relationRecord `json:"index"`
	}
	for _, section := range []struct {
		name   string
		target any
	}{
		{"artists", &artists}, {"locations", &locations},
		{"dates", &dates}, {"relation", &relations},
	} {
		if err := json.Unmarshal(payloads[section.name], section.target); err != nil {
			return models.APIResponse{}, fmt.Errorf("decode %s: %w", section.name, err)
		}
	}
	if len(artists) == 0 {
		return models.APIResponse{}, errors.New("artists response is empty")
	}
	snapshot := models.APIResponse{}
	artistIDs := make(map[int]bool, len(artists))
	for _, artist := range artists {
		if artist.ID <= 0 || artistIDs[artist.ID] || strings.TrimSpace(artist.Name) == "" || artist.CreationDate <= 0 {
			return models.APIResponse{}, errors.New("artists contains an invalid or duplicate record")
		}
		artistIDs[artist.ID] = true
		snapshot.Artists = append(snapshot.Artists, models.Artist{
			ID: strconv.Itoa(artist.ID), Name: artist.Name, Image: artist.Image,
			StartYear: artist.CreationDate, FirstAlbumDate: artist.FirstAlbum,
			Members: artist.Members,
		})
	}
	validateIDs := func(section string, ids []int) error {
		seen := make(map[int]bool, len(ids))
		for _, id := range ids {
			if !artistIDs[id] || seen[id] {
				return fmt.Errorf("%s contains an unknown or duplicate artist ID", section)
			}
			seen[id] = true
		}
		if len(seen) != len(artistIDs) {
			return fmt.Errorf("%s is missing artist records", section)
		}
		return nil
	}
	ids := make([]int, 0, len(locations.Index))
	for _, record := range locations.Index {
		ids = append(ids, record.ID)
		if record.Locations == nil {
			return models.APIResponse{}, errors.New("locations record is missing its locations array")
		}
		for _, location := range record.Locations {
			snapshot.Locations = append(snapshot.Locations, models.Location{ArtistID: strconv.Itoa(record.ID), Name: location})
		}
	}
	if err := validateIDs("locations", ids); err != nil {
		return models.APIResponse{}, err
	}
	ids = ids[:0]
	for _, record := range dates.Index {
		ids = append(ids, record.ID)
		if record.Dates == nil {
			return models.APIResponse{}, errors.New("dates record is missing its dates array")
		}
		for _, date := range record.Dates {
			snapshot.Dates = append(snapshot.Dates, models.ConcertDate{ArtistID: strconv.Itoa(record.ID), RawDate: date, Date: NormalizeDate(date)})
		}
	}
	if err := validateIDs("dates", ids); err != nil {
		return models.APIResponse{}, err
	}
	ids = ids[:0]
	for _, record := range relations.Index {
		ids = append(ids, record.ID)
		if record.DatesLocations == nil {
			return models.APIResponse{}, errors.New("relation record is missing datesLocations")
		}
		for location, dateList := range record.DatesLocations {
			snapshot.Relations = append(snapshot.Relations, models.Relation{ArtistID: strconv.Itoa(record.ID), Location: location, Dates: dateList})
		}
	}
	if err := validateIDs("relation", ids); err != nil {
		return models.APIResponse{}, err
	}
	return snapshot, nil
}

// NormalizeDate removes only the API's leading marker; RawDate stays unchanged.
func NormalizeDate(raw string) string {
	return strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(raw), "*"))
}
