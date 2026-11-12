package api

import (
	"embed"
	"fmt"

	"tour-track/internal/models"
)

// demoData is an explicitly selected offline snapshot, captured 2026-11-12.
// It is never substituted for a failed live API response.
//
//go:embed data/*.json
var demoData embed.FS

func LoadDemo() (models.APIResponse, error) {
	payloads := make(map[string][]byte, 4)
	for _, endpoint := range []string{"artists", "locations", "dates", "relation"} {
		body, err := demoData.ReadFile("data/" + endpoint + ".json")
		if err != nil {
			return models.APIResponse{}, fmt.Errorf("read demo %s: %w", endpoint, err)
		}
		payloads[endpoint] = body
	}
	return decodeSnapshot(payloads)
}
