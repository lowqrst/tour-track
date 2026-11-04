package models

// Artist represents a musician or band profile.
type Artist struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Image          string   `json:"image"`
	StartYear      int      `json:"start_year"`
	FirstAlbumDate string   `json:"first_album_date"`
	Members        []string `json:"members"`
	Style          string   `json:"style"`
}

// Location describes a concert venue in a specific city/country.
type Location struct {
	ID       string `json:"id"`
	ArtistID string `json:"artist_id"`
	Venue    string `json:"venue"`
	City     string `json:"city"`
	Country  string `json:"country"`
	Date     string `json:"date"`
}

// ConcertDate stores a concert occurrence for an artist.
type ConcertDate struct {
	ID       string `json:"id"`
	ArtistID string `json:"artist_id"`
	Date     string `json:"date"`
}

// Relation links an artist, concert date, and venue/location together.
type Relation struct {
	ArtistID   string `json:"artist_id"`
	DateID     string `json:"date_id"`
	LocationID string `json:"location_id"`
}

// APIResponse wraps all data returned by the external REST API.
type APIResponse struct {
	Artists   []Artist      `json:"artists"`
	Locations []Location    `json:"locations"`
	Dates     []ConcertDate `json:"dates"`
	Relations []Relation    `json:"relations"`
}
