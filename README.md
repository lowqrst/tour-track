# Tour Track

A fast, accessible music artist catalog featuring concert geography, searchable artist data, and an explainable activity score.

Built with Go on the backend and a server-rendered HTML interface, Tour Track turns the four Groupie Trackers datasets into an interactive view of artists, locations, concerts, and career activity.

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

## Preview

- Responsive artist cards, list, and table views
- Dedicated artist profile pages
- Search by artist, member, and concert location
- Filters for country, founding year, member, and activity level
- Rising Star, location, and concert timeline insights
- Offline demo snapshot for reliable testing and demonstrations

## Quick Start

**Requirements:**

- Linux or macOS
- Go 1.22 or newer
- Bash

From the project root:

```bash
go run .
```

Open [http://localhost:8080](http://localhost:8080).

By default, Tour Track fetches live data from the four sections of the [Groupie Trackers API](https://groupietrackers.herokuapp.com/api).

## Demo Mode

Run the application with a bundled, immutable API snapshot captured on November 12, 2026:

```bash
TOUR_TRACK_DEMO=1 go run .
```

Demo mode makes the app fully usable offline and is useful for reviews, screenshots, testing, and environments without API access.

Stop the server with `Ctrl+C`. A normal `go run .` uses the live API unless `TOUR_TRACK_DEMO=1` remains set in your shell.

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `TOUR_TRACK_ADDR` | HTTP server listen address | `:8080` |
| `PORT` | Hosting-platform port; used when `TOUR_TRACK_ADDR` is unset | — |
| `TOUR_TRACK_API_URL` | API base URL for development or testing | Groupie Trackers API |
| `TOUR_TRACK_DEMO` | Set to `1` to enable the bundled offline snapshot | Disabled |

Example:

```bash
TOUR_TRACK_ADDR=127.0.0.1:8085 TOUR_TRACK_API_URL=https://example.com/api go run .
```

## Features

### Discovery

- Browse artists as cards, a compact list, or a sortable table.
- Search names, band members, and concert locations.
- Filter by country, founding year, member, and activity classification.
- Open a dedicated profile for each artist.

### Activity Classification

Every artist receives one of four activity labels:

- **Emerging** — score 0–10
- **Active** — score 11–25
- **Established** — score 26–40
- **Legendary** — score 41+

The calculation is deliberately simple and visible in the code:

```text
score = tourSpread * 2 + concertCount * 3 + min(10, careerYears / 5)
```

Where:

- `tourSpread` is the number of distinct concert locations.
- `concertCount` is the number of entries in the artist's concert dates dataset.
- `careerYears` is the current year minus the artist's creation year.

Career data is recalculated on each request, while the service cache stores raw data rather than precomputed scores.

### Rising Stars

Tour Track identifies accelerating concert activity over an inclusive two-year rolling window ending on the current calendar date.

The activity page also presents recent concert data for additional context. Concert records may be historical; a score describes the available API sample and does not predict future popularity or touring plans.

### Scoring and Data Assumptions

- Career-year division uses integer division, and career contribution is capped at `10`.
- Country is extracted from the text after the last `-` in a location. Underscores become spaces.
- Common country aliases such as `USA`, `US`, `UK`, and `GB` are normalized.
- Duplicate concert entries remain duplicate entries; multiple performances on the same date are preserved.
- `RawDate` preserves the original value. A leading `*` is removed before parsing a `DD-MM-YYYY` date.
- Invalid dates remain visible and are not silently discarded.

## Refresh and Concurrency

The **Refresh** control sends `POST /api/refresh`.

The server starts one shared background update task and immediately returns `202 Accepted` with a status endpoint. A completion channel prevents duplicate updates, and network requests have bounded timeouts.

Refresh behavior differs by mode:

- Live mode replaces the cached snapshot after a successful API refresh.
- Demo mode returns `200 OK` and explains that the immutable snapshot is in use.

## Architecture

```text
.
├── main.go                 # Server lifecycle, embedded assets, HTTP timeouts
├── internal/
│   ├── activity/           # Score calculation and activity classification
│   ├── api/                # Parallel API loading and data joining
│   ├── handlers/           # Routes, HTTP methods, filters, and rendering
│   └── service/            # Five-minute snapshot cache and refresh coordination
├── scripts/
│   └── start-public.sh     # Build and start a temporary Cloudflare tunnel
├── templates/              # Server-rendered HTML templates
├── static/                 # Styles and browser assets
└── data/                   # Bundled demo snapshot
```

- `internal/api` loads all four API sections concurrently using goroutines and a buffered channel.
- Artist records are joined by `id`, not array position.
- `internal/service` caches a consistent five-minute snapshot. A mutex protects reading and publishing state while network work happens outside it.
- `internal/handlers` manages routes, methods, errors, and filters. `html/template` escapes API data before rendering.
- `main.go` embeds resources, sets HTTP limits, recovers handler panics, and handles graceful shutdown on system signals.

Templates, styles, static assets, and the demo snapshot are embedded with `go:embed`. The compiled binary therefore works from any directory:

```bash
go build -o tour-track .
./tour-track
```

## Build with Docker

The Dockerfile uses a multi-stage build and produces a small static Go binary:

```bash
docker build -t tour-track .
docker run --rm -p 8080:8080 tour-track
```

Open [http://localhost:8080](http://localhost:8080).

## Temporary Public URL

Publish a local server through a temporary Cloudflare Tunnel:

```bash
bash scripts/start-public.sh
```

The script:

1. Builds the Go server.
2. Starts it on `127.0.0.1:8080`.
3. Waits for readiness.
4. Starts `cloudflared` and prints a public HTTPS URL.

To use another port:

```bash
bash scripts/start-public.sh 8085
```

Requirements:

- `curl`
- `cloudflared`

Cloudflare prints an address such as `https://random-name.trycloudflare.com`. No Cloudflare account is required.

Keep the terminal open. `Ctrl+C` stops both the tunnel and the server. Anyone with the temporary URL can access the site until the process stops or the tunnel expires.

## Testing

Run the standard Go checks:

```bash
go test ./...
go vet ./...
go build ./...
```

For systems with a configured C compiler:

```bash
go test -race ./...
```

Tests use `httptest`, a local snapshot, and fixed time values where date calculations are involved.

If the standard Go cache directory is unavailable, keep build cache data inside the project:

```bash
GOCACHE="$(pwd)/.cache/go-build" go test ./...
```

## Contributing

Contributions are welcome. Please keep changes focused, preserve the project's Go style, and add tests for behavior that is not covered.

## License

Add a `LICENSE` file to make the project’s license explicit. Until a license is present, no permissions beyond those permitted by default on GitHub are granted to reuse or distribute the code.
