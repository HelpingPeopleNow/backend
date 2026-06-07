# Backend

A Go REST API service with PostgreSQL persistence, hexagonal (ports & adapters) architecture, and full CRUD for **PromptHelper** resources. Built with Go 1.23 stdlib + GORM.

## Tech Stack

| Layer            | Technology                    |
|------------------|-------------------------------|
| Language         | Go 1.23                       |
| HTTP Router      | `net/http` (stdlib)           |
| Serialization    | `encoding/json` (stdlib)      |
| ORM              | GORM (gorm.io)                |
| Database         | PostgreSQL 16 (via GORM)      |
| Container        | Alpine Linux 3.20             |
| CI/CD            | GitHub Actions → GHCR         |

## Architecture

The backend follows **hexagonal architecture** (ports & adapters / DDD layers):

```
main.go
  │
  ├── database/           ← Infrastructure: DB connection + migrations
  │   └── postgres.go
  │
  └── internal/
      ├── core/           ← Domain layer: pure Go entities, zero dependencies
      │   └── prompt.go
      ├── ports/          ← Ports layer: interfaces the hexagon defines
      │   └── repository.go
      ├── service/        ← Application use cases: business logic
      │   └── prompt.go
      └── adapters/       ← Adapters layer: implementations of ports
          ├── handler/
          │   └── http.go       ← Inbound adapter (HTTP delivery)
          └── repository/
              └── gorm.go       ← Outbound adapter (GORM persistence)
```

**Layering rules:**
- `core` → zero imports from other packages
- `ports` → depends only on `core`
- `service` → depends on `ports` (never on adapters)
- `adapters` → implements `ports`, uses `core`

## Endpoints

| Endpoint | Method | Body | Response |
|----------|--------|------|----------|
| `/health` | GET | — | `{"status": "ok"}` |
| `/api/v1/hello` | GET | — | `{"message": "hello, keep going"}` (random from 10 phrases) |
| `/api/v1/prompt-helpers` | GET | — | `[]` (list all prompt helpers) |
| `/api/v1/prompt-helpers` | POST | `{"title":"...","content":"...","category":"..."}` | Created prompt helper |
| `/api/v1/prompt-helpers/:id` | GET | — | Single prompt helper |
| `/api/v1/prompt-helpers/:id` | PATCH | `{"title":"...","content":"...","category":"..."}` | Updated prompt helper |
| `/api/v1/prompt-helpers/:id` | DELETE | — | `204 No Content` |

### PromptHelper Model

| Field | Type | Required |
|-------|------|----------|
| `id` | `uint` | Auto |
| `title` | `string` | ✅ |
| `content` | `string` | ✅ |
| `category` | `string` | ❌ |
| `created_at` | `datetime` | Auto |
| `updated_at` | `datetime` | Auto |

## Getting Started

### Prerequisites

- Go 1.23+ (or Docker)
- PostgreSQL 16

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP server port |
| `DB_HOST` | `localhost` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | Database user |
| `DB_PASSWORD` | `postgres` | Database password |
| `DB_NAME` | `helpingpeoplenow` | Database name |
| `DB_SSLMODE` | `disable` | SSL mode |

### Run Locally

```bash
# Ensure PostgreSQL is running, then:
go run .
```

### Run with Docker (standalone)

```bash
docker build -t backend .
docker run -d -p 8081:8081 \
  -e DB_HOST=host.docker.internal \
  --name backend backend
```

### Run with Docker Compose (recommended)

See [HelpingPeopleNow/infra](https://github.com/HelpingPeopleNow/infra) for the full stack setup including PostgreSQL.

### Test

```bash
curl http://localhost:8081/health
curl http://localhost:8081/api/v1/hello
curl http://localhost:8081/api/v1/prompt-helpers
```

## CI/CD

A GitHub Actions workflow (`.github/workflows/docker.yml`) builds and pushes Docker images to `ghcr.io/HelpingPeopleNow/backend` on every push or PR to `main`. Tags: `latest`, branch name, commit SHA.
