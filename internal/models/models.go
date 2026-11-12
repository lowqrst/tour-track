package models

// Artist contains the fields supplied by the public artists endpoint.
type Artist struct {
	ID             string
	Name           string
	Image          string
	StartYear      int
	FirstAlbumDate string
	Members        []string
}

// Location retains the original city-country token from the locations endpoint.
type Location struct {
	ArtistID string
	Name     string
}

// ConcertDate preserves both the API value and its normalized date text.
type ConcertDate struct {
	ArtistID string
	Date     string
	RawDate  string
}

// Relation links one artist's concert dates to a location.
type Relation struct {
	ArtistID string
	Location string
	Dates    []string
}

// APIResponse is a complete, immutable snapshot of all four API endpoints.
type APIResponse struct {
	Artists   []Artist
	Locations []Location
	Dates     []ConcertDate
	Relations []Relation
}
