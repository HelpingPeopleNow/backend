# HelpingPeopleNow Backend

Go REST API with hexagonal architecture. Serves chat, prompt helpers, and system prompts. Proxies chat requests to the helper service via gRPC.

**Port:** `:8081` (configurable via `PORT` env)

## Stack

| Layer | Technology |
|-------|-----------|
| **Language** | Go 1.25 |
| **HTTP** | stdlib `net/http` (no frameworks) |
| **gRPC** | `google.golang.org/grpc` v1.81.1 |
| **Protobuf** | `google.golang.org/protobuf` v1.36.11 |
| **ORM** | GORM v1.25.12 (PostgreSQL driver) |
| **DB** | PostgreSQL 16 |
| **Logging** | `log/slog` (structured, JSON) |
| **Container** | golang:1.25.11-alpine → alpine:3.20 (multi-stage) |
| **CI/CD** | GitHub Actions → ghcr.io |

## Architecture (Hexagonal)

```
┌─────────────────────────────────────────────┐
│                   main.go                   │
│  (composition root: wire deps, start server)│
└──────────┬──────────────────────┬───────────┘
           │                      │
    ┌──────▼──────┐        ┌─────▼──────┐
    │  Inbound    │        │  Outbound  │
    │  Adapters   │        │  Adapters  │
    │  (handler/) │        │(repository/)│
    └──────┬──────┘        └─────┬──────┘
           │                     │
    ┌──────▼─────────────────────▼──────┐
    │           Service Layer           │
    │        (service/prompt.go)        │
    └──────────────┬────────────────────┘
                   │
    ┌──────────────▼──────────────┐
    │      Domain (core/)         │
    │  PromptHelper, SystemPrompt │
    │  (zero dependencies)        │
    └─────────────────────────────┘
```

**Layer rules:**
- Domain (`core/`) → zero dependencies, pure Go types
- Ports (`ports/`) → interfaces only
- Service (`service/`) → uses ports, implements use cases
- Adapters (`adapters/`) → concrete implementations (HTTP handlers, GORM repo, gRPC client)

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check → `{"status":"ok"}` |
| POST | `/api/v1/chat` | Send chat message → `{"answer":"..."}` |
| GET | `/api/v1/system-prompts` | Get system prompts |
| PUT | `/api/v1/system-prompts/{name}` | Update system prompt column |
| GET | `/api/v1/prompt-helpers` | List all prompt helpers |
| POST | `/api/v1/prompt-helpers` | Create prompt helper |
| GET | `/api/v1/prompt-helpers/{id}` | Get by ID |
| PATCH | `/api/v1/prompt-helpers/{id}` | Update by ID |
| DELETE | `/api/v1/prompt-helpers/{id}` | Delete by ID |

### Chat

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H 'Content-Type: application/json' \
  -d '{"message":"Hello","history":[]}'
```

Accepts a `message` string and optional `history` array (`[{role, content}]`). Proxies to the helper service via gRPC and returns the AI response.

## Logging

All handlers use Go's `log/slog` (structured logging) with the following log points:

| Component | Events Logged |
|-----------|--------------|
| `main.go` | Startup, shutdown, request method/path/duration |
| `chat_handler.go` | gRPC connection attempts, request size, gRPC call duration, errors |
| `system_prompt_handler.go` | GET/PUT operations, column name, DB errors |
| `http.go` (prompt-helpers CRUD) | Create/read/update/delete with IDs, errors |

Log output format (text handler to stdout):
```
time=2026-06-08T... level=INFO msg="request started" method=POST path=/api/v1/chat ...
time=2026-06-08T... level=INFO msg="request completed" method=POST path=/api/v1/chat duration_ms=1234
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP listen port |
| `HELPER_GRPC_ADDR` | `helpingpeoplenow-helper:50051` | gRPC helper address |
| `DATABASE_URL` | — | Direct DSN (overrides individual vars) |
| `DB_HOST` | `postgres` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | DB user |
| `DB_PASSWORD` | `postgres` | DB password |
| `DB_NAME` | `helpingpeoplenow` | DB name |
| `DB_SSLMODE` | `disable` | SSL mode |

## gRPC Integration

The backend is a gRPC **client** to the helper service:

```
Protocol: helper.HelperService.Ask(AskRequest → AskResponse)
Proto:    proto/helper.proto
Stubs:    proto/helper/*.pb.go (generated)
```

The `ChatHandler` dials the helper on startup and reconnects if the connection drops (`ensureClient()`).

## Development

```bash
go run .                    # Run locally
go build -o backend .       # Build binary
docker build -t backend .   # Docker build
```

## Docker

```dockerfile
# Multi-stage build
FROM golang:1.25.11-alpine3.22 AS builder
FROM alpine:3.20 AS runtime
# Static binary, CGO_ENABLED=0, exposes :8081
```
