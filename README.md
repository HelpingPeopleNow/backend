# Backend

A lightweight, zero-dependency Go backend service providing health check and greeting endpoints. Built with the Go 1.23 standard library only — no frameworks, no external dependencies.

## Tech Stack

| Layer       | Technology                    |
|-------------|-------------------------------|
| Language    | Go 1.23                       |
| HTTP Router | `net/http` (stdlib)           |
| Serialization | `encoding/json` (stdlib)    |
| Container   | Alpine Linux 3.20             |
| CI/CD       | GitHub Actions → GHCR         |

No third-party packages are used. The entire application runs on the Go standard library.

## Project Structure

```
.
├── main.go                    # Entry point — routes, handlers, server startup
├── go.mod                     # Module definition (go 1.23, zero dependencies)
├── Dockerfile                 # Multi-stage Docker build (golang:1.23-alpine → alpine:3.20)
├── .github/
│   └── workflows/
│       └── docker.yml         # CI/CD: build & push to ghcr.io on push/PR to main
├── .gitignore
└── README.md
```

## Endpoints

### `GET /health`

Returns a simple health-check response.

**Response `200 OK`**
```json
{"status": "ok"}
```

---

### `GET /api/v1/hello`

Returns a friendly greeting with a randomly selected motivational phrase.

**Response `200 OK`**
```json
{"message": "hello, keep going"}
```

Available phrases are randomly chosen from: *keep going*, *you've got this*, *make it happen*, *stay curious*, *build something awesome*, *one step at a time*, *dream big*, *code on*, *never stop learning*, *be the change*.

## Getting Started

### Prerequisites

- Go 1.23 or later

### Run Locally

```bash
go run .
```

The server starts on port `8081` by default. You can override the port with the `PORT` environment variable:

```bash
PORT=9090 go run .
```

### Build

```bash
go build -o backend
./backend
```

### Test

```bash
curl http://localhost:8081/health
curl http://localhost:8081/api/v1/hello
```

## Docker Usage

### Build the Image

```bash
docker build -t backend .
```

### Run the Container

```bash
docker run -d -p 8081:8081 --name backend backend
```

The container listens on port `8081` (exposed by the Dockerfile). Set the `PORT` environment variable to change it:

```bash
docker run -d -p 9090:9090 -e PORT=9090 --name backend backend
```

### Verify

```bash
curl http://localhost:8081/health
curl http://localhost:8081/api/v1/hello
```

## CI/CD

A GitHub Actions workflow (`.github/workflows/docker.yml`) automatically builds and pushes a Docker image to the GitHub Container Registry (GHCR) on every push or pull request to the `main` branch.

- **Trigger**: push or PR to `main`
- **Registry**: `ghcr.io/HelpingPeopleNow/backend`
- **Tags**: `latest` (on default branch), branch name, and commit SHA
- **Caching**: GitHub Actions cache (`type=gha`) for faster subsequent builds
- **On PR**: the image is built and cached but **not** pushed

## Environment Variables

| Variable | Default | Description          |
|----------|---------|----------------------|
| `PORT`   | `8081`  | HTTP server port     |
