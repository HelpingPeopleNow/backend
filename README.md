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

1. **Main chat** — receives `POST /api/v1/chat` from the frontend, loads the system prompt + LLM provider from the in-memory cache, calls the helper via gRPC, returns the AI answer + detected user role
2. **Worker profile intake chat** — receives `POST /api/v1/worker/chat`, uses a separate `worker_profile_prompt` system prompt designed to gather worker profile fields conversationally, returns the answer + parsed `detected_fields` in JSON that the frontend uses to auto-fill the worker profile form
3. **User role detection** — when the helper identifies whether a user is a "worker" or "client", the backend calls the auth service (`PUT /api/auth/user/:id/role`) to persist the role
4. **System prompt management** — admin can read/update the helper prompt (`helper_prompt`), the worker profile prompt (`worker_profile_prompt`), and the LLM provider (`llm_provider`) via REST endpoints
5. **LLM provider runtime switch** — admin can toggle between `opencode` (external) and `ollama` (local) without restarting the container; empty = falls back to the helper's `USE_OLLAMA` env var

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
             │  worker     │    │  helper_prompt +   │
             │  chat +     │    │  worker_profile +  │
             │  gRPC +     │    │  llm_provider)     │
             │  roles)     │    └────────┬──────────┘
             └──────┬──────┘            │
                    │                   │
             ┌──────▼───────────────────▼──────────┐
             │          In-Memory Cache              │
             │   systemPrompt (string)               │
             │   workerProfilePrompt (string)        │
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
- **Cache pattern** — system prompts + provider are loaded into memory at startup and refreshed on every admin update via callbacks. This avoids hitting the DB on every chat request

---

## Request Flows

### Main Chat (`/api/v1/chat`)

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
       │   split JWT on "." for raw session token
       │   DB: SELECT user_id FROM session WHERE token = ?
       │   PUT /api/auth/user/{id}/role (auth service) → update role
       │
       └─ return { answer, role_updated }
```

### Worker Profile Intake Chat (`/api/v1/worker/chat`)

```
Worker types message in chat panel
       │
       ▼
POST /api/v1/worker/chat ──► ChatHandler.HandleWorkerChat
       │
       ├─ getWorkerProfilePrompt() → cached worker prompt (string)
       ├─ getLLMProvider()         → cached provider
       │
       ├─ helper.Ask() ──gRPC──► HelperService
       │                             │
       │                             └─ returns answer (may contain [FIELDS] block)
       │
       ├─ parseFieldsFromAnswer():
       │   extract [FIELDS]{"field":"value"}[/FIELDS] from response
       │   strip tags from answer text
       │   validate JSON, return detected_fields
       │
       └─ return { answer, detected_fields }
```

The worker profile chat does NOT update user roles — the user is already known as a worker. The LLM is prompted to append a `[FIELDS]{"profession":"plumber","city":"Madrid",...}[/FIELDS]` block to its responses once at least 6 fields have been collected. The backend parses this out and sends `detected_fields` to the frontend, which auto-fills the worker profile form.

### Field Merging Flow

1. User sends chat message like *"I'm a plumber in Madrid"*
2. LLM responds conversationally, may include `[FIELDS]{json}[/FIELDS]` with known fields
3. Backend strips the tag and returns `{ "answer": "...", "detected_fields": { "profession": "plumber", "city": "Madrid" } }`
4. Frontend merges `detected_fields` into the worker profile form by field name mapping
5. User can either continue chatting (more fields) or edit the form directly
6. Hitting "Save Profile" persists all fields via `PUT /api/v1/worker/profile`

**Worker profile fields mapped from detected_fields:**
| Field | JSON key | Type |
|-------|----------|------|
| Profession | `profession` | string |
| Business Name | `business_name` | string |
| Bio | `bio` | string |
| Phone | `phone` | string |
| City | `city` | string |
| Address | `address` | string |
| Service Radius | `service_radius_km` | number |
| Hourly Rate | `hourly_rate` | number |
| Minimum Charge | `minimum_charge` | number |
| Free Estimate | `free_estimate` | boolean |
| Years Exp | `years_experience` | number |
| Certifications | `certifications` | string[] |
| Has Insurance | `has_insurance` | boolean |
| Languages | `languages` | string[] |
| Emergency | `emergency_service` | boolean |
| Website | `website` | string |

---

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | No | Health check → `{"status":"ok"}` |
| POST | `/api/v1/chat` | Yes | Chat with AI → `{"answer","detected_role","role_updated"}` |
| POST | `/api/v1/worker/chat` | No* | Worker profile intake chat → `{"answer","detected_fields"}` |
| GET | `/api/v1/system-prompts` | Yes | Get helper + worker prompts + LLM provider |
| PUT | `/api/v1/system-prompts/helper` | Yes | Update the helper prompt text |
| PUT | `/api/v1/system-prompts/worker_profile` | Yes | Update the worker profile prompt text |
| PUT | `/api/v1/system-prompts/provider` | Yes | Set LLM provider ("opencode", "ollama", or "" for env default) |

*Worker chat is session-independent — it creates a new conversation context per request.
Automatic session validation (for protecting user data) is planned.

### Health

Simple health check — no auth required. Used by load balancers, orchestrators, and monitoring to verify the service is alive.

```bash
curl http://localhost:8081/health
# → {"status":"ok"}
```

Returns `200 OK` with `{"status":"ok"}`. Because there is no request body, session, or database dependency, this endpoint is fast and reliable for uptime checks.

### Main Chat

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -H "Cookie: better-auth.something=..." \
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

### Worker Profile Intake Chat

```bash
curl -X POST http://localhost:8081/api/v1/worker/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"I am a plumber in Madrid","history":[]}'
```

Response:

```json
{
  "answer": "Welcome! A plumber in Madrid — that's great! What's your business name?",
  "detected_fields": {
    "profession": "plumber",
    "city": "Madrid"
  }
}
```

### System Prompts

```bash
# Read current
curl http://localhost:8081/api/v1/system-prompts
# → {"helper_prompt":"...", "worker_profile_prompt":"...", "llm_provider":"opencode", "updated_at":"..."}

# Update helper prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/helper \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a helpful home services assistant..."}'

# Update worker profile prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/worker_profile \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a friendly profile-building assistant..."}'

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

Singleton row (`id=1`) with three key columns:

| Column | Type | Purpose |
|--------|------|---------|
| `helper_prompt` | `TEXT` | System prompt sent to the helper on every main chat request |
| `worker_profile_prompt` | `TEXT` | System prompt sent to the helper on worker profile intake chat |
| `llm_provider` | `VARCHAR(32)` | `"opencode"`, `"ollama"`, or `""` to use env default |

If `worker_profile_prompt` is empty at startup, a default prompt is seeded automatically that instructs the LLM to gather all 16 profile fields conversationally and output `[FIELDS]JSON[/FIELDS]` blocks.

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
  string system_prompt = 3;   // loaded by backend from DB (helper or worker_profile)
  string llm_provider = 4;    // "opencode" | "ollama" | "" (= env default)
}
```

Proto definition: `proto/helper/helper.proto`

The `ChatHandler` dials the helper at startup and reconnects if the connection drops.

---

## User Role Detection Flow

1. Helper returns `detected_role` in `AskResponse` (parsed from the LLM text response)
2. Backend reads the session cookie, splits the JWT on `"."` to extract the raw session token
3. Backend looks up the user ID via `SELECT userId FROM session WHERE token = ?`
4. Backend calls `PUT /api/auth/user/{id}/role` on the auth service (the role authority)
5. The frontend checks the user's role from the session and redirects to `/worker` or `/client`
6. If role update fails, the backend logs the error but still returns the chat response (non-blocking)

---

## Logging

All handlers use Go's `log/slog` with structured key-value pairs:

| Component | Events |
|-----------|--------|
| `main.go` | Startup, shutdown, request method/path/duration |
| `ChatHandler` (chat) | gRPC connection, message sizes, system prompt length, provider, role updates |
| `ChatHandler` (worker) | Message sizes, prompt length, provider, detected_fields JSON, field counts |
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
    │   └── system_prompt.go      # SystemPrompt GORM model (helper, worker_profile, llm_provider)
    └── adapters/
        └── handler/
            ├── chat_handler.go           # Main chat + worker profile chat + gRPC client + role sync
            ├── system_prompt_handler.go  # System prompt CRUD (helper, worker, provider)
            └── worker_handler.go         # Worker profile CRUD (GET/PUT)
```
