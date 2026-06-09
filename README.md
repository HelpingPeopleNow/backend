# HelpingPeopleNow Backend

Go REST API with hexagonal architecture. Orchestrates the chat flow: receives messages from the frontend, combines them with system prompts and LLM provider config, sends them to the helper service via gRPC, and updates user roles based on AI classification.

**Container:** `helpingpeoplenow-backend` | **Port:** `:8081`

---

## Stack

| Layer | Technology |
|-------|-----------|
| **Language** | Go 1.25 |
| **HTTP** | stdlib `net/http` (no frameworks) |
| **gRPC** | `google.golang.org/grpc` (client → helper) |
| **ORM** | GORM v1.25 (PostgreSQL driver) |
| **DB** | PostgreSQL 16 (`system_prompts` table) |
| **Logging** | `log/slog` (structured, text to stdout) |
| **Container** | golang:1.25 → alpine:3.20 (multi-stage, static binary) |

---

## What It Does

1. **Chat orchestration** — receives `POST /api/v1/chat` from the frontend, loads the system prompt + LLM provider from the in-memory cache, calls the helper via gRPC, returns the AI answer + detected user role
2. **User role detection** — when the helper identifies whether a user is a "worker" or "client", the backend calls the auth service (`PUT /api/auth/user/:id/role`) to persist the role
3. **System prompt management** — admin can read/update the helper prompt (`helper_prompt`) and the LLM provider (`llm_provider`) via REST endpoints
4. **LLM provider runtime switch** — admin can toggle between `opencode` (external) and `ollama` (local) without restarting the container; empty = falls back to the helper's `USE_OLLAMA` env var

---

## Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                         main.go                               │
│            (composition root: wire deps, start HTTP)          │
└───────────────────┬───────────────────┬───────────────────────┘
                    │                   │
             ┌──────▼──────┐    ┌──────▼──────────┐
             │ ChatHandler │    │ SystemPromptHandler │
             │ (chat +     │    │ (CRUD for          │
             │  gRPC +     │    │  helper_prompt +   │
             │  role)      │    │  llm_provider)     │
             └──────┬──────┘    └────────┬──────────┘
                    │                    │
             ┌──────▼────────────────────▼──────────┐
             │          In-Memory Cache              │
             │   systemPrompt (string)               │
             │   llmProvider  (string)               │
             │   (sync.RWMutex, loaded from DB       │
             │    at startup, refreshed on admin PUT)│
             └────────────────┬──────────────────────┘
                              │
             ┌────────────────▼──────────────────────┐
             │           gRPC Client                 │
             │   helper.HelperService.Ask()          │
             │   (sends question + history +         │
             │    system_prompt + llm_provider)      │
             └───────────────────────────────────────┘
```

### Layer Rules

- **No service layer** — the codebase was simplified after removing the `PromptHelper` CRUD. All business logic lives in handlers (`internal/adapters/handler/`)
- **No port/repository abstractions** — `SystemPromptHandler` uses `*gorm.DB` directly for DB operations, and `ChatHandler` uses the `*grpc.ClientConn` directly for gRPC calls
- **Cache pattern** — system prompt + provider are loaded into memory at startup and refreshed on every admin update via callbacks. This avoids hitting the DB on every chat request

---

## Request Flow

```
User sends message
       │
       ▼
POST /api/v1/chat ──► ChatHandler.ServeHTTP
       │
       ├─ getSystemPrompt() → cached system prompt (string)
       ├─ getLLMProvider()  → cached provider ("opencode"/"ollama"/"")
       │
       ├─ helper.Ask() ──gRPC──► HelperService
       │                             │
       │                             ├─ picks adapter based on llm_provider
       │                             │   (or falls back to env USE_OLLAMA)
       │                             └─ returns answer + detected_role
       │
       ├─ if detected_role != "":
       │   read cookie from request
       │   GET /api/auth/get-session (auth service) → user ID
       │   PUT /api/auth/user/{id}/role (auth service) → update role
       │
       └─ return { answer, role_updated }
```

---

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | No | Health check → `{"status":"ok"}` |
| POST | `/api/v1/chat` | Yes | Chat with AI → `{"answer","detected_role","role_updated"}` |
| GET | `/api/v1/system-prompts` | Yes | Get helper prompt + LLM provider |
| PUT | `/api/v1/system-prompts/helper` | Yes | Update the helper prompt text |
| PUT | `/api/v1/system-prompts/provider` | Yes | Set LLM provider (`"opencode"`, `"ollama"`, or `""` for env default) |

### Health

Simple health check — no auth required. Used by load balancers, orchestrators, and monitoring to verify the service is alive.

```bash
curl http://localhost:8081/health
# → {"status":"ok"}
```

Returns `200 OK` with `{"status":"ok"}`. Because there is no request body, session, or database dependency, this endpoint is fast and reliable for uptime checks.

### Chat

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -H "Cookie: better-auth-session=..." \
  -d '{"message":"I need a plumber","history":[]}'
```

Response:

```json
{
  "answer": "Great! I can connect you with local plumbers. What area are you in?",
  "detected_role": "client",
  "role_updated": true
}
```

### System Prompts

```bash
# Read current
curl http://localhost:8081/api/v1/system-prompts
# → {"helper_prompt":"...", "llm_provider":"opencode", "updated_at":"..."}

# Update helper prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/helper \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a helpful home services assistant..."}'

# Switch LLM provider
curl -X PUT http://localhost:8081/api/v1/system-prompts/provider \
  -H "Content-Type: application/json" \
  -d '{"content":"ollama"}'

# Reset to env default
curl -X PUT http://localhost:8081/api/v1/system-prompts/provider \
  -H "Content-Type: application/json" \
  -d '{"content":""}'
```

---

## Database

### `system_prompts` table

Singleton row (`id=1`) with two key columns:

| Column | Type | Purpose |
|--------|------|---------|
| `helper_prompt` | `TEXT` | System prompt sent to the helper on every chat request |
| `llm_provider` | `VARCHAR(32)` | `"opencode"`, `"ollama"`, or `""` to use env default |

---

## gRPC Integration

The backend is a **gRPC client** to the helper:

```protobuf
service HelperService {
  rpc Ask(AskRequest) returns (AskResponse);
}

message AskRequest {
  string question = 1;
  repeated Message history = 2;
  string system_prompt = 3;   // loaded by backend from DB
  string llm_provider = 4;    // "opencode" | "ollama" | "" (= env default)
}
```

Proto definition: `proto/helper/helper.proto`

The `ChatHandler` dials the helper at startup and reconnects if the connection drops.

---

## User Role Detection Flow

1. Helper returns `detected_role` in `AskResponse` (parsed from the LLM text response)
2. Backend calls `GET /api/auth/get-session` on the auth service with the user's session cookie to get the user ID
3. Backend calls `PUT /api/auth/user/{id}/role` on the auth service to persist the role
4. The frontend checks the user's role from the session and redirects to `/worker` or `/client`
5. If role update fails, the backend logs the error but still returns the chat response (non-blocking)

---

## Logging

All handlers use Go's `log/slog` with structured key-value pairs:

| Component | Events |
|-----------|--------|
| `main.go` | Startup, shutdown, request method/path/duration |
| `ChatHandler` | gRPC connection, message sizes, system prompt length, provider, role updates |
| `SystemPromptHandler` | GET/PUT operations, column name, cache refresh |
| `AuthMiddleware` | Session validation, missing/invalid cookies |

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP listen port |
| `HELPER_GRPC_ADDR` | `helpingpeoplenow-helper:50051` | Helper gRPC address |
| `HELPER_TIMEOUT_SECONDS` | `120` | gRPC request timeout |
| `DATABASE_URL` | — | Direct DSN (overrides individual vars below) |
| `DB_HOST` | `postgres` | PostgreSQL host |
| `DB_PORT` | `5432` | PostgreSQL port |
| `DB_USER` | `postgres` | DB user |
| `DB_PASSWORD` | `postgres` | DB password |
| `DB_NAME` | `helpingpeoplenow` | DB name |
| `DB_SSLMODE` | `disable` | SSL mode |

---

## Development

```bash
# Run locally (needs Postgres + helper running)
go run .

# Build binary
go build -o backend .

# Docker build
docker build -t ghcr.io/helpingpeoplenow/backend:latest .
```

---

## Project Structure

```
backend/
├── main.go                       # Composition root: init DB, wire handlers, start server
├── handler_health.go             # GET /health handler
├── Dockerfile                    # Multi-stage: golang:1.25 → alpine:3.20
├── go.mod / go.sum               # Go module dependencies
├── proto/
│   └── helper/
│       ├── helper.proto          # gRPC contract (shared with helper repo)
│       ├── helper.pb.go          # Generated protobuf Go types
│       └── helper_grpc.pb.go     # Generated gRPC Go client/server
├── database/
│   └── postgres.go               # GORM connection + AutoMigrate
└── internal/
    ├── core/
    │   └── system_prompt.go      # SystemPrompt GORM model
    └── adapters/
        └── handler/
            ├── chat_handler.go           # Chat + gRPC client + role sync
            └── system_prompt_handler.go  # System prompt CRUD + provider toggle
```
