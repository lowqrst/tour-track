package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"tour-track/internal/models"
)

// Client fetches artist metadata and related concert data from a REST API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// FetchAll retrieves every endpoint required for the app and merges it into one response.
func (c *Client) FetchAll() (models.APIResponse, error) {
	artists, err := c.FetchArtists()
	if err != nil {
		log.Printf("api: FetchArtists failed: %v", err)
		return fallbackAPIResponse(), err
	}
	locations, err := c.FetchLocations()
	if err != nil {
		log.Printf("api: FetchLocations failed: %v", err)
		return fallbackAPIResponse(), err
	}
	dates, err := c.FetchDates()
	if err != nil {
		log.Printf("api: FetchDates failed: %v", err)
		return fallbackAPIResponse(), err
	}
	relations, err := c.FetchRelations()
	if err != nil {
		log.Printf("api: FetchRelations failed: %v", err)
		return fallbackAPIResponse(), err
	}

	return models.APIResponse{
		Artists:   artists,
		Locations: locations,
		Dates:     dates,
		Relations: relations,
	}, nil
}

func (c *Client) FetchArtists() ([]models.Artist, error) {
	if c == nil || c.BaseURL == "" {
		return fallbackArtists(), nil
	}

	payload, err := c.fetchJSON("/artists")
	if err != nil {
		return fallbackArtists(), err
	}
	var result []models.Artist
	if err := decodeJSON(payload, &result, "artists"); err != nil {
		return fallbackArtists(), err
	}
	if len(result) == 0 {
		return fallbackArtists(), fmt.Errorf("artists response is empty")
	}
	return result, nil
}

func (c *Client) FetchLocations() ([]models.Location, error) {
	if c == nil || c.BaseURL == "" {
		return fallbackLocations(), nil
	}

	payload, err := c.fetchJSON("/locations")
	if err != nil {
		return fallbackLocations(), err
	}
	var result []models.Location
	if err := decodeJSON(payload, &result, "locations"); err != nil {
		return fallbackLocations(), err
	}
	if len(result) == 0 {
		return fallbackLocations(), fmt.Errorf("locations response is empty")
	}
	return result, nil
}

func (c *Client) FetchDates() ([]models.ConcertDate, error) {
	if c == nil || c.BaseURL == "" {
		return fallbackDates(), nil
	}

	payload, err := c.fetchJSON("/dates")
	if err != nil {
		return fallbackDates(), err
	}
	var result []models.ConcertDate
	if err := decodeJSON(payload, &result, "dates"); err != nil {
		return fallbackDates(), err
	}
	if len(result) == 0 {
		return fallbackDates(), fmt.Errorf("dates response is empty")
	}
	return result, nil
}

func (c *Client) FetchRelations() ([]models.Relation, error) {
	if c == nil || c.BaseURL == "" {
		return fallbackRelations(), nil
	}

	payload, err := c.fetchJSON("/relations")
	if err != nil {
		return fallbackRelations(), err
	}
	var result []models.Relation
	if err := decodeJSON(payload, &result, "relations"); err != nil {
		return fallbackRelations(), err
	}
	if len(result) == 0 {
		return fallbackRelations(), fmt.Errorf("relations response is empty")
	}
	return result, nil
}

func decodeJSON(payload []byte, target any, source string) error {
	if len(payload) == 0 {
		return fmt.Errorf("empty response from %s", source)
	}
	if !json.Valid(payload) {
		return fmt.Errorf("invalid JSON received from %s", source)
	}
	if err := json.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("failed to decode %s: %w", source, err)
	}
	return nil
}

func (c *Client) fetchJSON(path string) ([]byte, error) {
	if c == nil || c.BaseURL == "" {
		return nil, fmt.Errorf("missing API base URL")
	}

	url := c.BaseURL + path
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func fallbackAPIResponse() models.APIResponse {
	return models.APIResponse{
		Artists:   fallbackArtists(),
		Locations: fallbackLocations(),
		Dates:     fallbackDates(),
		Relations: fallbackRelations(),
	}
}

type artistSeed struct {
	ID             string
	Name           string
	Image          string
	StartYear      int
	FirstAlbumDate string
	Style          string
	Members        []string
	Shows          []showSeed
}

type showSeed struct {
	Date    string
	Venue   string
	City    string
	Country string
}

func fallbackArtists() []models.Artist {
	seeds := microDataset()
	artists := make([]models.Artist, 0, len(seeds))
	for _, seed := range seeds {
		artists = append(artists, models.Artist{
			ID:             seed.ID,
			Name:           seed.Name,
			Image:          seed.Image,
			StartYear:      seed.StartYear,
			FirstAlbumDate: seed.FirstAlbumDate,
			Members:        seed.Members,
			Style:          seed.Style,
		})
	}
	return artists
}

func fallbackLocations() []models.Location {
	seeds := microDataset()
	locations := make([]models.Location, 0)
	for _, seed := range seeds {
		for index, show := range seed.Shows {
			id := fmt.Sprintf("loc-%s-%02d", seed.ID, index+1)
			locations = append(locations, models.Location{
				ID:       id,
				ArtistID: seed.ID,
				Venue:    show.Venue,
				City:     show.City,
				Country:  show.Country,
				Date:     show.Date,
			})
		}
	}
	return locations
}

func fallbackDates() []models.ConcertDate {
	seeds := microDataset()
	dates := make([]models.ConcertDate, 0)
	for _, seed := range seeds {
		for index, show := range seed.Shows {
			dates = append(dates, models.ConcertDate{
				ID:       fmt.Sprintf("date-%s-%02d", seed.ID, index+1),
				ArtistID: seed.ID,
				Date:     show.Date,
			})
		}
	}
	return dates
}

func fallbackRelations() []models.Relation {
	seeds := microDataset()
	relations := make([]models.Relation, 0)
	for _, seed := range seeds {
		for index, show := range seed.Shows {
			dateID := fmt.Sprintf("date-%s-%02d", seed.ID, index+1)
			locID := fmt.Sprintf("loc-%s-%02d", seed.ID, index+1)
			relations = append(relations, models.Relation{
				ArtistID:   seed.ID,
				DateID:     dateID,
				LocationID: locID,
			})
			_ = show
		}
	}
	return relations
}

func microDataset() []artistSeed {
	return []artistSeed{
		{ID: "artist-01", Name: "Violet Echo", Image: "https://images.unsplash.com/photo-1501386761578-eac5c94b800a?auto=format&fit=crop&w=800&q=80", StartYear: 2022, FirstAlbumDate: "2023-09-15", Style: "Indie Pop", Members: []string{"Nia Park", "Theo Brooks", "Lena Ortiz"}, Shows: []showSeed{{Date: "2025-04-12", Venue: "Arcade Hall", City: "Austin", Country: "United States"}, {Date: "2025-06-21", Venue: "Mural Room", City: "Montreal", Country: "Canada"}, {Date: "2024-11-09", Venue: "Le Route", City: "Paris", Country: "France"}, {Date: "2024-02-18", Venue: "Moonlight Club", City: "Berlin", Country: "Germany"}}},
		{ID: "artist-02", Name: "Glass Harbor", Image: "https://images.unsplash.com/photo-1493225457124-a3eb161ffa5f?auto=format&fit=crop&w=800&q=80", StartYear: 2021, FirstAlbumDate: "2022-05-11", Style: "Synthwave", Members: []string{"Ari Vale", "Leo Frost"}, Shows: []showSeed{{Date: "2025-05-10", Venue: "Nova Stage", City: "Lisbon", Country: "Portugal"}, {Date: "2025-01-19", Venue: "Ocean Club", City: "Madrid", Country: "Spain"}, {Date: "2024-09-14", Venue: "Starlight Hall", City: "Rome", Country: "Italy"}, {Date: "2023-12-03", Venue: "Blue Lantern", City: "Prague", Country: "Czech Republic"}, {Date: "2023-06-09", Venue: "Glow Dome", City: "Budapest", Country: "Hungary"}}},
		{ID: "artist-03", Name: "Nocturne City", Image: "https://images.unsplash.com/photo-1516280440614-37939bbacd81?auto=format&fit=crop&w=800&q=80", StartYear: 2018, FirstAlbumDate: "2020-03-27", Style: "Indie Rock", Members: []string{"Mira Shaw", "Damian Poe", "Owen Ross"}, Shows: []showSeed{{Date: "2025-03-23", Venue: "Sunset Theater", City: "Denver", Country: "United States"}, {Date: "2025-02-05", Venue: "Red Door", City: "Mexico City", Country: "Mexico"}, {Date: "2024-07-22", Venue: "Velvet Pit", City: "Buenos Aires", Country: "Argentina"}, {Date: "2024-04-19", Venue: "Crescent Hall", City: "São Paulo", Country: "Brazil"}, {Date: "2023-10-12", Venue: "Nexus Stage", City: "Santiago", Country: "Chile"}, {Date: "2023-02-11", Venue: "Pine Room", City: "Bogotá", Country: "Colombia"}}},
		{ID: "artist-04", Name: "Wild Lantern", Image: "https://images.unsplash.com/photo-1501612780327-45045538702b?auto=format&fit=crop&w=800&q=80", StartYear: 2017, FirstAlbumDate: "2019-07-08", Style: "Alternative", Members: []string{"Jules Hart", "Iris Quinn", "Sam Cole"}, Shows: []showSeed{{Date: "2025-08-16", Venue: "Canal Hall", City: "Amsterdam", Country: "Netherlands"}, {Date: "2025-02-02", Venue: "Dawn Arena", City: "Oslo", Country: "Norway"}, {Date: "2024-12-12", Venue: "North Stage", City: "Stockholm", Country: "Sweden"}, {Date: "2024-08-04", Venue: "Copper Dome", City: "Copenhagen", Country: "Denmark"}, {Date: "2024-01-15", Venue: "Harbor Jet", City: "Helsinki", Country: "Finland"}, {Date: "2023-06-30", Venue: "King's Theater", City: "London", Country: "United Kingdom"}}},
		{ID: "artist-05", Name: "Hollow Pines", Image: "https://images.unsplash.com/photo-1524504388940-b1c1722653e1?auto=format&fit=crop&w=800&q=80", StartYear: 2015, FirstAlbumDate: "2018-03-02", Style: "Folk Rock", Members: []string{"Rosa Dean", "Evan Price", "Lucy Stone"}, Shows: []showSeed{{Date: "2025-04-30", Venue: "Forest Hall", City: "Chicago", Country: "United States"}, {Date: "2025-01-08", Venue: "Brass Room", City: "Toronto", Country: "Canada"}, {Date: "2024-09-16", Venue: "Luna Stage", City: "Seattle", Country: "United States"}, {Date: "2024-03-27", Venue: "Cedar Dome", City: "Vancouver", Country: "Canada"}, {Date: "2023-10-20", Venue: "Moss Theater", City: "Boston", Country: "United States"}}},
		{ID: "artist-06", Name: "Velvet Circuit", Image: "https://images.unsplash.com/photo-1524504388940-b1c1722653e1?auto=format&fit=crop&w=800&q=80", StartYear: 2012, FirstAlbumDate: "2014-02-22", Style: "Electronic", Members: []string{"Cora Lee", "Evan Stone", "Nia Frost"}, Shows: []showSeed{{Date: "2025-02-18", Venue: "Aurora Stage", City: "Sydney", Country: "Australia"}, {Date: "2024-11-30", Venue: "Nova Arena", City: "Toronto", Country: "Canada"}, {Date: "2024-05-17", Venue: "Quartz Club", City: "Tokyo", Country: "Japan"}, {Date: "2023-11-11", Venue: "Mirage Hall", City: "Seoul", Country: "South Korea"}, {Date: "2023-05-09", Venue: "Orbit Room", City: "Singapore", Country: "Singapore"}, {Date: "2022-09-14", Venue: "Copper Stage", City: "Bangkok", Country: "Thailand"}, {Date: "2022-02-03", Venue: "Solar Arena", City: "Dubai", Country: "United Arab Emirates"}}},
		{ID: "artist-07", Name: "Blue Ember", Image: "https://images.unsplash.com/photo-1506794778202-cad84cf45f1d?auto=format&fit=crop&w=800&q=80", StartYear: 2010, FirstAlbumDate: "2013-11-01", Style: "Rock", Members: []string{"Kian Wolfe", "Rhea Ford", "Noah Scott", "Milo Grant"}, Shows: []showSeed{{Date: "2025-06-12", Venue: "Stonegate", City: "Los Angeles", Country: "United States"}, {Date: "2025-03-08", Venue: "Summit Dome", City: "Chicago", Country: "United States"}, {Date: "2024-10-19", Venue: "River Hall", City: "New York", Country: "United States"}, {Date: "2024-05-04", Venue: "Cobalt Stage", City: "Mexico City", Country: "Mexico"}, {Date: "2023-09-22", Venue: "Silver Palace", City: "Miami", Country: "United States"}, {Date: "2023-02-16", Venue: "Bayside Arena", City: "Toronto", Country: "Canada"}, {Date: "2022-11-29", Venue: "Majestic Hall", City: "Boston", Country: "United States"}, {Date: "2022-05-05", Venue: "Neon Dome", City: "Seattle", Country: "United States"}}},
		{ID: "artist-08", Name: "Oasis Avenue", Image: "https://images.unsplash.com/photo-1517841905240-472988babdf9?auto=format&fit=crop&w=800&q=80", StartYear: 2008, FirstAlbumDate: "2011-04-20", Style: "Pop Rock", Members: []string{"Mila Hart", "Nate Ross", "Jade Cole"}, Shows: []showSeed{{Date: "2025-05-02", Venue: "Helios Arena", City: "London", Country: "United Kingdom"}, {Date: "2025-01-17", Venue: "Grand Hall", City: "Paris", Country: "France"}, {Date: "2024-10-28", Venue: "Royal Terrace", City: "Berlin", Country: "Germany"}, {Date: "2024-06-08", Venue: "Atlas Theater", City: "Madrid", Country: "Spain"}, {Date: "2023-11-19", Venue: "Summit Stage", City: "Lisbon", Country: "Portugal"}, {Date: "2023-03-04", Venue: "Breeze Dome", City: "Rome", Country: "Italy"}, {Date: "2022-05-25", Venue: "Harbor Lights", City: "Amsterdam", Country: "Netherlands"}, {Date: "2021-12-18", Venue: "Sunset Club", City: "Brussels", Country: "Belgium"}, {Date: "2021-08-14", Venue: "Palace Hall", City: "Dublin", Country: "Ireland"}}},
		{ID: "artist-09", Name: "The Last Horizon", Image: "https://images.unsplash.com/photo-1500648767791-00dcc994a43e?auto=format&fit=crop&w=800&q=80", StartYear: 2004, FirstAlbumDate: "2005-09-29", Style: "Classic Rock", Members: []string{"Jonas Price", "Wes Turner", "Drew Nolan", "Carmen Vale"}, Shows: []showSeed{{Date: "2025-07-11", Venue: "Skyline Arena", City: "New York", Country: "United States"}, {Date: "2025-03-15", Venue: "Queen Hall", City: "London", Country: "United Kingdom"}, {Date: "2024-09-09", Venue: "Olympic Dome", City: "Paris", Country: "France"}, {Date: "2024-02-14", Venue: "Sunset Field", City: "Berlin", Country: "Germany"}, {Date: "2023-10-20", Venue: "Milan Stage", City: "Milan", Country: "Italy"}, {Date: "2023-06-04", Venue: "Sakura Hall", City: "Tokyo", Country: "Japan"}, {Date: "2022-11-11", Venue: "Harbor Dome", City: "Sydney", Country: "Australia"}, {Date: "2022-05-28", Venue: "Bastion Arena", City: "Toronto", Country: "Canada"}, {Date: "2021-09-19", Venue: "Alpine Hall", City: "Zurich", Country: "Switzerland"}, {Date: "2021-04-09", Venue: "Sultan Theater", City: "Dubai", Country: "United Arab Emirates"}}},
		{ID: "artist-10", Name: "Monolith Parade", Image: "https://images.unsplash.com/photo-1511367461989-f85a21fda167?auto=format&fit=crop&w=800&q=80", StartYear: 2001, FirstAlbumDate: "2003-06-17", Style: "Alternative Metal", Members: []string{"Vera Flint", "Kurt Mills", "Lola Reid", "Greg Stone"}, Shows: []showSeed{{Date: "2025-06-06", Venue: "Empire Hall", City: "Los Angeles", Country: "United States"}, {Date: "2025-02-05", Venue: "Crystal Dome", City: "Mexico City", Country: "Mexico"}, {Date: "2024-11-20", Venue: "Calypso Stage", City: "Rio de Janeiro", Country: "Brazil"}, {Date: "2024-08-14", Venue: "Signal Arena", City: "Buenos Aires", Country: "Argentina"}, {Date: "2024-03-19", Venue: "Crown Theater", City: "Madrid", Country: "Spain"}, {Date: "2023-12-07", Venue: "Iron Plaza", City: "Lisbon", Country: "Portugal"}, {Date: "2023-06-02", Venue: "Mosaic Hall", City: "London", Country: "United Kingdom"}, {Date: "2022-09-11", Venue: "Kinetic Dome", City: "Paris", Country: "France"}, {Date: "2022-04-18", Venue: "Nightline Arena", City: "Berlin", Country: "Germany"}, {Date: "2021-11-30", Venue: "Pioneer Stage", City: "Toronto", Country: "Canada"}}},
		{ID: "artist-11", Name: "Feral Bloom", Image: "https://images.unsplash.com/photo-1487412720507-e7ab37603c6f?auto=format&fit=crop&w=800&q=80", StartYear: 1999, FirstAlbumDate: "2001-02-18", Style: "Grunge", Members: []string{"Mila Cross", "John Price", "Sage Kim", "Rory Wells"}, Shows: []showSeed{{Date: "2025-09-10", Venue: "Ridge Arena", City: "Seattle", Country: "United States"}, {Date: "2025-04-28", Venue: "Temple Hall", City: "Vancouver", Country: "Canada"}, {Date: "2024-10-16", Venue: "Blackwood Stage", City: "London", Country: "United Kingdom"}, {Date: "2024-03-30", Venue: "Aurora Dome", City: "Amsterdam", Country: "Netherlands"}, {Date: "2023-12-08", Venue: "Marina Hall", City: "Copenhagen", Country: "Denmark"}, {Date: "2023-05-27", Venue: "Iron Stage", City: "Stockholm", Country: "Sweden"}, {Date: "2022-11-19", Venue: "Harbor Dome", City: "Oslo", Country: "Norway"}, {Date: "2022-06-15", Venue: "Union Hall", City: "Berlin", Country: "Germany"}, {Date: "2021-08-05", Venue: "Plaza Arena", City: "Paris", Country: "France"}, {Date: "2021-01-20", Venue: "Velvet Room", City: "Brussels", Country: "Belgium"}, {Date: "2020-09-14", Venue: "Oak Stage", City: "Madrid", Country: "Spain"}}},
		{ID: "artist-12", Name: "Signal Bloom", Image: "https://images.unsplash.com/photo-1541534401786-2077eed87a74?auto=format&fit=crop&w=800&q=80", StartYear: 1998, FirstAlbumDate: "2000-04-11", Style: "Electronic Pop", Members: []string{"Kara Bloom", "Nico Vale", "Jules Park", "Dalia Grant"}, Shows: []showSeed{{Date: "2025-07-09", Venue: "Nova Arena", City: "Los Angeles", Country: "United States"}, {Date: "2025-04-04", Venue: "Cloud Hall", City: "Toronto", Country: "Canada"}, {Date: "2024-12-21", Venue: "Fleur Theater", City: "Paris", Country: "France"}, {Date: "2024-08-19", Venue: "Blue Terrace", City: "London", Country: "United Kingdom"}, {Date: "2024-03-04", Venue: "Moonlight Dome", City: "Tokyo", Country: "Japan"}, {Date: "2023-10-09", Venue: "Harbor Stage", City: "Seoul", Country: "South Korea"}, {Date: "2023-05-17", Venue: "Orbit Hall", City: "Singapore", Country: "Singapore"}, {Date: "2022-11-03", Venue: "Cairn Arena", City: "Sydney", Country: "Australia"}, {Date: "2022-06-26", Venue: "Glass Theater", City: "Dubai", Country: "United Arab Emirates"}, {Date: "2021-10-08", Venue: "Horizon Hall", City: "Berlin", Country: "Germany"}, {Date: "2021-02-14", Venue: "Eastgate", City: "Rome", Country: "Italy"}, {Date: "2020-07-24", Venue: "Forum Stage", City: "Madrid", Country: "Spain"}}},
		{ID: "artist-13", Name: "Crimson Atlas", Image: "https://images.unsplash.com/photo-1525201548942-d8732f6617a0?auto=format&fit=crop&w=800&q=80", StartYear: 1993, FirstAlbumDate: "1996-05-07", Style: "Progressive Rock", Members: []string{"Miles Ortiz", "Owen Reed", "Hannah Moss", "Victor Lane"}, Shows: []showSeed{{Date: "2025-08-20", Venue: "Titan Dome", City: "New York", Country: "United States"}, {Date: "2025-03-09", Venue: "Crystal Theater", City: "London", Country: "United Kingdom"}, {Date: "2024-11-28", Venue: "Arcade Arena", City: "Paris", Country: "France"}, {Date: "2024-06-18", Venue: "Ember Hall", City: "Berlin", Country: "Germany"}, {Date: "2024-02-12", Venue: "Riviera Stage", City: "Milan", Country: "Italy"}, {Date: "2023-09-15", Venue: "Sakura Arena", City: "Tokyo", Country: "Japan"}, {Date: "2023-04-26", Venue: "Cobalt Dome", City: "Seoul", Country: "South Korea"}, {Date: "2022-12-02", Venue: "Pacific Hall", City: "Sydney", Country: "Australia"}, {Date: "2022-07-17", Venue: "Highland Theater", City: "Toronto", Country: "Canada"}, {Date: "2021-11-04", Venue: "Mosaic Arena", City: "Barcelona", Country: "Spain"}, {Date: "2021-05-27", Venue: "Royal Stage", City: "Amsterdam", Country: "Netherlands"}, {Date: "2020-09-09", Venue: "Legacy Hall", City: "Prague", Country: "Czech Republic"}, {Date: "2020-04-13", Venue: "Cedar Dome", City: "Budapest", Country: "Hungary"}}},
		{ID: "artist-14", Name: "Echo Meridian", Image: "https://images.unsplash.com/photo-1542204165-65bf26472b9b?auto=format&fit=crop&w=800&q=80", StartYear: 1987, FirstAlbumDate: "1989-11-18", Style: "Arena Rock", Members: []string{"Leo Cross", "Ava Quinn", "Mason Gray", "Drew Nolan", "Sophie Vale"}, Shows: []showSeed{{Date: "2025-03-12", Venue: "Westgate Stadium", City: "Los Angeles", Country: "United States"}, {Date: "2025-01-30", Venue: "Kingside Hall", City: "London", Country: "United Kingdom"}, {Date: "2024-09-05", Venue: "Metro Dome", City: "Paris", Country: "France"}, {Date: "2024-05-14", Venue: "Ocean Stage", City: "Berlin", Country: "Germany"}, {Date: "2024-02-09", Venue: "Sunrise Arena", City: "Madrid", Country: "Spain"}, {Date: "2023-11-21", Venue: "Golden Dome", City: "Tokyo", Country: "Japan"}, {Date: "2023-07-03", Venue: "Shining Hall", City: "Seoul", Country: "South Korea"}, {Date: "2023-01-26", Venue: "Orbit Arena", City: "Singapore", Country: "Singapore"}, {Date: "2022-10-15", Venue: "Grand Dome", City: "Sydney", Country: "Australia"}, {Date: "2022-06-18", Venue: "Crown Arena", City: "Toronto", Country: "Canada"}, {Date: "2021-12-11", Venue: "Hearth Hall", City: "Chicago", Country: "United States"}, {Date: "2021-07-17", Venue: "Azure Stage", City: "Rome", Country: "Italy"}, {Date: "2020-11-28", Venue: "Blue Ridge", City: "Zurich", Country: "Switzerland"}, {Date: "2020-05-09", Venue: "Alpine Hall", City: "Vienna", Country: "Austria"}}},
		{ID: "artist-15", Name: "North Star Collective", Image: "https://images.unsplash.com/photo-1521119989659-a83eee488004?auto=format&fit=crop&w=800&q=80", StartYear: 1981, FirstAlbumDate: "1984-02-09", Style: "World Fusion", Members: []string{"Cora Lee", "Sage Kim", "Remy Sol", "Eli Stone", "Asha Reed"}, Shows: []showSeed{{Date: "2025-06-29", Venue: "Starlight Arena", City: "New York", Country: "United States"}, {Date: "2025-02-13", Venue: "Blackstone Hall", City: "London", Country: "United Kingdom"}, {Date: "2024-10-03", Venue: "Grand Plaza", City: "Paris", Country: "France"}, {Date: "2024-05-28", Venue: "River Palace", City: "Berlin", Country: "Germany"}, {Date: "2023-12-17", Venue: "Desert Dome", City: "Cairo", Country: "Egypt"}, {Date: "2023-08-29", Venue: "Emerald Hall", City: "Johannesburg", Country: "South Africa"}, {Date: "2023-02-15", Venue: "Coral Stage", City: "Mumbai", Country: "India"}, {Date: "2022-09-27", Venue: "Saffron Dome", City: "Dubai", Country: "United Arab Emirates"}, {Date: "2022-04-11", Venue: "Moon Plaza", City: "Bangkok", Country: "Thailand"}, {Date: "2021-11-23", Venue: "Breeze Hall", City: "Singapore", Country: "Singapore"}, {Date: "2021-06-30", Venue: "Pacific Arena", City: "Sydney", Country: "Australia"}, {Date: "2020-12-01", Venue: "Northern Stage", City: "Tokyo", Country: "Japan"}, {Date: "2020-06-19", Venue: "Horizon Dome", City: "Toronto", Country: "Canada"}, {Date: "2019-10-10", Venue: "Sunset Room", City: "Miami", Country: "United States"}}},
	}
}
