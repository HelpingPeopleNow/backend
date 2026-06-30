# HelpingPeopleNow Backend

Go REST API with hexagonal architecture. Orchestrates the chat flow: receives messages from the frontend, combines them with system prompts and LLM provider config, sends them to the helper service via gRPC, and appends a language instruction based on the request's `lang` parameter.

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

1. **Worker profile intake chat** — receives `POST /api/v1/chat` with `mode: "worker_intake"`, uses the `worker_profile_prompt` system prompt designed to gather worker profile fields conversationally, appends a language instruction to the system prompt based on the `lang` parameter, returns the answer + parsed `detected_fields` in JSON; the backend auto-merges fields into the worker profile via map-based upsert
2. **Client profile intake chat** — receives `POST /api/v1/chat` with `mode: "client_intake"`, uses the `client_profile_prompt` system prompt designed to gather client profile fields conversationally, appends a language instruction to the system prompt based on the `lang` parameter, returns the answer + parsed `detected_fields` in JSON; the backend auto-merges fields into the client profile via map-based upsert
3. **System prompt management** — admin can read/update the worker profile prompt (`worker_profile_prompt`), the client profile prompt (`client_profile_prompt`), and the LLM provider (`llm_provider`) via REST endpoints
4. **LLM provider runtime switch** — admin can toggle between `opencode0` (big-pickle), `opencode1`, `opencode2` (external), `ollama` (local), and `mistral` (cloud) without restarting the container; empty = uses the helper's auto fallback chain (Mistral → OpenCode 0 → OpenCode 1 → OpenCode 2 → Ollama)
5. **Conversation persistence** — all chat messages (worker, client) are saved to the database and can be loaded on page reload via the conversations API
6. **Search/find professionals** — receives `POST /api/v1/chat` with `mode: "search"`, uses two-pass LLM (filter-fill then presentation) to match clients with workers, returns recommended worker cards
7. **Profile reset** — worker and client profiles can be cleared via `DELETE /api/v1/worker/profile` and `DELETE /api/v1/client/profile`

---

## Architecture
```
┌───────────────────────────────────────────────────────────────┐
│                         main.go                               │
│            (composition root: wire deps, start HTTP)          │
└───────┬───────────┬───────────────┬───────────────┬───────────┘
        │           │               │               │
 ┌──────▼──────┐ ┌──▼──────────┐ ┌──▼──────────┐ ┌──▼──────────┐
 │ ChatHandler │ │ WorkerHandler│ │ClientHandler│ │ConvHandler  │
 │ (POST       │ │ (GET/DELETE  │ │(GET/DELETE  │ │(list/get    │
 │  /api/v1/   │ │  profile)    │ │ profile)    │ │ conversations)│
 │  chat with  │ └─────────────┘ └─────────────┘ └─────────────┘
 │  mode in    │
 │  body)      │ ┌──────────────────────┐
 │             │ │ SystemPromptHandler  │
 │  ┌──────────┘ │ (CRUD for            │
 │  │            │  worker_profile +    │
 │  │ gRPC +     │  client_profile +    │
 │  │ lang       │  provider +          │
 │  ▼            │  search +            │
 │              │  presentation)       │
 │              └──────────┬───────────┘
 │                         │
 ┌─────────────────────────▼──────────┐
 │          In-Memory Cache             │
 │   systemPrompt (string)             │
 │   workerProfilePrompt (string)      │
 │   clientProfilePrompt (string)      │
 │   llmProvider  (string)             │
 │   (sync.RWMutex, loaded from DB     │
 │    at startup, refreshed on admin PUT)│
 └────────────────┬────────────────────┘
                  │
 ┌────────────────▼────────────────────┐
 │           gRPC Client               │
 │   helper.HelperService.Ask()        │
 │   (sends question + history +       │
 │    system_prompt + llm_provider)    │
 └─────────────────────────────────────┘
```

### Layer Rules

The backend uses **hexagonal (ports & adapters) architecture**. `main.go` is the composition root and wires every component via the interfaces in `internal/ports/`.

| Layer | Path | Owns |
|---|---|---|
| Handlers | `internal/adapters/handler/` | HTTP parse, session validation, response shape. Delegate to a service or port. Never hold `*gorm.DB`. |
| Services | `internal/services/` | Use-case logic. `SearchService.Search` (two-pass LLM), `IntakeService.ProcessIntake` (chat → `[FIELDS]` → map-merge upsert), `SeedService.SeedSystemPrompts`. Depend only on `ports/` interfaces. |
| Ports | `internal/ports/` | Interface contracts: `LLMService`, `ProfileRepository`, `ChatRepository`, `SystemPromptRepository`, `DirectMessageRepository`, `DirectMessaging`. |
| Adapters | `internal/adapters/` | Concrete port implementations: `repository/` (GORM via `*gorm.DB`), `llm/` (`grpc_client.go::GRPCLLMService`), `realtime/` (SSE broker), `middleware/`, `ratelimit/`. |
| Core | `internal/core/` | Domain models (`WorkerProfile`, `ClientProfile`, `Conversation`, `Message`, `DirectConversation`, `DirectMessage`) and pure helpers (`MergeFields`, DTO mappers). |
| Cache | in-process map + callbacks | System prompt + LLM provider snapshots loaded at startup from `system_prompts` (`id=1`); refreshed via `SystemPromptHandler` `onUpdate` callback chain at `main.go:143-171`. |

#### Direct messaging exception

`DirectMessagingHandler` (`internal/adapters/handler/direct_messaging_handler.go`) injects multiple ports directly (`h.profs`, `h.dm`, `h.broker`) without going through a service — still no `*gorm.DB`. SSE/realtime concerns are tied to the HTTP request lifecycle. Don't refactor this to add a service layer without first extracting a `DirectMessagingService`.

#### Cache pattern

System prompts + LLM provider are loaded into memory at startup from the `system_prompts` singleton row (`id=1`). Admin updates via `PUT /api/v1/system-prompts/...` fire `onUpdate` callbacks so chat latency doesn't DB-hit on every request.

> **Note for contributors:** the layer table above is the source of truth for the current architecture. Earlier revisions of this file (and `backend/AGENTS.md`) described a flatter "no service layer" / "no port/repository abstractions" model — that description predates the hexagonal refactor and is not accurate against the current `internal/services/`, `internal/ports/`, `internal/adapters/`, `internal/core/` tree. New contributors should follow the table in this Layer Rules section.

---

## Request Flows

### Worker Profile Intake Chat (`POST /api/v1/chat` with `mode: "worker_intake"`)

```
Worker types message in chat panel
       │
       ▼
POST /api/v1/chat { mode: "worker_intake" } ──► ChatHandler.ServeHTTP
       │
       ├─ getWorkerProfilePrompt() → cached worker prompt (string)
       ├─ getLLMProvider()         → cached provider
       │
       ├─ append language instruction to system prompt:
       │   if lang == "es": "IMPORTANTE: Responde SIEMPRE en español al usuario"
       │   if lang == "en": "IMPORTANT: Always respond in English"
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
       ├─ saveConversation(): persist messages to DB
       │
       ├─ if fields extracted and user has session:
       │   map-based merge: loads existing WorkerProfile from DB
       │   only overwrites fields present in [FIELDS] block
       │   saves back to DB
       │
       └─ return { answer, detected_fields, conversation_id }
```

The worker profile chat does NOT update user roles — the user is already known as a worker. The LLM is prompted to append a `[FIELDS]{"profession":"plumber","city":"Madrid",...}[/FIELDS]` block to every response, including ALL known fields cumulatively. The backend parses this out, merges it with any existing profile in the DB, and sends `detected_fields` to the frontend for display.

### Client Profile Intake Chat (`POST /api/v1/chat` with `mode: "client_intake"`)

```
Client types message in chat panel
       │
       ▼
POST /api/v1/chat { mode: "client_intake" } ──► ChatHandler.ServeHTTP
       │
       ├─ getClientProfilePrompt() → cached client prompt (string)
       ├─ getLLMProvider()          → cached provider
       │
       ├─ append language instruction to system prompt:
       │   if lang == "es": "IMPORTANTE: Responde SIEMPRE en español al usuario"
       │   if lang == "en": "IMPORTANT: Always respond in English"
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
       ├─ saveConversation(): persist messages to DB
       │
       ├─ if fields extracted and user has session:
       │   map-based merge: loads existing ClientProfile from DB
       │   only overwrites fields present in [FIELDS] block
       │   saves back to DB
       │
       └─ return { answer, detected_fields, conversation_id }
```

### Field Merging Flow

1. User sends chat message like *"I'm a plumber in Madrid"*
2. LLM responds conversationally, may include `[FIELDS]{json}[/FIELDS]` with ALL known fields (cumulative)
3. Backend strips the tag and returns `{ "answer": "...", "detected_fields": { "profession": "plumber", "city": "Madrid" } }`
4. Backend merges `detected_fields` into the existing profile in the DB (map-based merge — only overwrites fields present in the block)
5. Frontend displays the updated profile as read-only cards
6. User can continue chatting to add more fields, or use "Reset Profile" to clear via DELETE

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
| Social Links | `social_links` (preferred) OR per-platform `instagram`/`facebook`/`twitter`/`linkedin`/`tiktok`/`youtube` (each `string` URL) | `{platform,url}[]` — both forms are normalized into a single deduplicated `{platform,url}` array by the package-internal `core.mergeSocialLinks` helper (`backend/internal/core/fields.go`), invoked from `WorkerProfile.MergeFields` (`backend/internal/core/worker.go:113`). Not callable directly outside the `core` package. |

**Client profile fields mapped from detected_fields:**

| Field | JSON key | Type |
|-------|----------|------|
| Full Name | `full_name` | string |
| Phone | `phone` | string |
| City | `city` | string |
| Address | `address` | string |
| Bio | `bio` | string |
| Preferred Contact | `preferred_contact` | string |
| Property Type | `property_type` | string |
| Notes | `notes` | string |

---

## API Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/health` | No | Health check (composite: PG ping + helper gRPC `health`). Returns 200 when both deps are `ok`; 503 when either is `down`. Response: `{"status":"ok"\|"degraded", "postgres":"ok"\|"down", "grpc_helper":"ok"\|"down", "details":{<key>:<err>}}` — `details` is a **map[string]string** keyed by `postgres_err` / `grpc_helper_err`. `Content-Type: application/json`. |
| POST | `/api/v1/chat` | No* | Unified chat: `mode` in body (`"worker_intake"`, `"client_intake"`, `"search"`) → `{"answer","detected_fields","conversation_id"}` |
| GET | `/api/v1/worker/profile` | Yes* | Get worker profile for authenticated user |
| DELETE | `/api/v1/worker/profile` | Yes* | Clear worker profile for authenticated user |
| GET | `/api/v1/client/profile` | Yes* | Get client profile for authenticated user |
| DELETE | `/api/v1/client/profile` | Yes* | Clear client profile for authenticated user |
| GET | `/api/v1/system-prompts` | Yes | Get all four prompt columns + `llm_provider` (auth-only, NOT admin-only) |
| PUT | `/api/v1/system-prompts/worker_profile` | Yes (admin) | Update the worker profile prompt text |
| PUT | `/api/v1/system-prompts/client_profile` | Yes (admin) | Update the client profile prompt text |
| PUT | `/api/v1/system-prompts/find_trader_search` | Yes (admin) | Update the find-trader search-params prompt |
| PUT | `/api/v1/system-prompts/find_trader_presentation` | Yes (admin) | Update the find-trader results-presentation prompt |
| PUT | `/api/v1/system-prompts/provider` | Yes (admin) | Set LLM provider ("opencode0", "opencode1", "opencode2", "ollama", "mistral", or "" for auto fallback chain) |
| PUT | `/api/v1/user/reset-role` | Yes* | Clear user role (reset to "") |
| GET | `/api/v1/conversations` | Yes | List conversations (supports `?type=worker&limit=N`) |
| GET | `/api/v1/conversations/:id` | Yes | Get conversation with full message history |
| GET | `/api/v1/workers/:id/contact` | Yes | Create-or-resume a direct-message conversation with another worker (returns `conversation_id`) |
| GET, POST, PATCH | `/api/v1/direct-messages`, `/api/v1/direct-messages/:id/*` | Yes (DM middleware sets user from session) | Direct messaging: inbox, thread, send, read, archive, block, report, SSE stream (`/stream`), polling (`/since`). Errors: `body` over 4000 chars → 400; empty `body` → 400; invalid JSON → 400; `/since?ts=` missing → 400; `/since?ts=` unparseable RFC3339 → 400; `DELETE` on `/api/v1/direct-messages` → 404; SSE `stream` with nil broker → 501. Auth/authorization: no `user_id` in context → 401; authenticated but not a conversation participant → 403; conversation `status="blocked"` → 403 on send. |
| GET | `/metrics` | No | Prometheus metrics in text/plain (counters: `http_requests_total`, `chat_requests_total`, `vector_search_total`, `profile_saves_total`, `conversations_total`, `dm_sent_total`, `dm_received_total`, `auth_resolve_errors_total`; histograms: `chat_llm_duration_seconds`, `auth_resolve_duration_seconds`, `vector_score`). Registered by `metrics_handler.RegisterMetricsRoutes`. |
| GET | `/api/v1/workers/public/latest` | No | Public worker profiles — paginated list (default limit 6, capped at 20). Returns `WorkerPublicDTO` (private fields stripped). |
| GET | `/api/v1/workers/public/{slug}` | No | Public worker profile by URL-friendly slug. Returns `WorkerPublicDTO` (private fields stripped). 404 on missing/invalid slug. |
| GET | `/admin/*` | Yes (admin) | Admin entity CRUD over exactly 5 entity slugs: `users`, `worker-profiles`, `client-profiles`, `conversations`, `messages` |

*Chat handler is wrapped by `AuthMiddleware` but does not require a session — anonymous users get chat, and only authenticated requests merge fields into the user's profile.

### Health

Composite health check — no auth required. Pings PostgreSQL and the helper gRPC `health` endpoint together. Used by load balancers, orchestrators, and monitoring to verify both dependencies.

```bash
curl http://localhost:8081/health
# → 200 {"status":"ok","postgres":"ok","grpc_helper":"ok","details":{}}
# → 503 {"status":"degraded","postgres":"ok","grpc_helper":"down","details":{"grpc_helper_err":"<msg>"}}
```

Returns `200 OK` when both PostgreSQL and the helper gRPC endpoint are reachable. Returns `503 Service Unavailable` when either is `down`; the `details` object (`map[string]string`, omitted from JSON when empty) carries `postgres_err` / `grpc_helper_err` keys with the per-component error message. `Content-Type: application/json`. Note: a storage or gRPC outage is reflected here even though `:8081` itself can still respond.

### Worker Profile Intake Chat

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"mode":"worker_intake","message":"I am a plumber in Madrid","history":[],"lang":"en"}'
```

Response:

```json
{
  "answer": "Welcome! A plumber in Madrid — that's great! What's your business name?",
  "detected_fields": {
    "profession": "plumber",
    "city": "Madrid"
  },
  "conversation_id": "42"
}
```

### Client Profile Intake Chat

```bash
curl -X POST http://localhost:8081/api/v1/chat \
  -H "Content-Type: application/json" \
  -d '{"mode":"client_intake","message":"Hi, I need help fixing my bathroom","history":[],"lang":"en"}'
```

Response:

```json
{
  "answer": "Hello! I'd love to help you find someone for your bathroom. What's your name?",
  "detected_fields": {
    "notes": "bathroom repair"
  },
  "conversation_id": "43"
}
```

### Profile Reset

```bash
# Reset worker profile
curl -X DELETE http://localhost:8081/api/v1/worker/profile \
  -H "Cookie: better-auth.something=..."
# → 204 No Content

# Reset client profile
curl -X DELETE http://localhost:8081/api/v1/client/profile \
  -H "Cookie: better-auth.something=..."
# → 204 No Content
```

### System Prompts

```bash
# Read current
curl http://localhost:8081/api/v1/system-prompts
# → {"worker_profile_prompt":"...", "client_profile_prompt":"...", "llm_provider":"opencode", "updated_at":"..."}

# Update worker profile prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/worker_profile \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a friendly profile-building assistant..."}'

# Update client profile prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/client_profile \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a friendly profile-building assistant for clients..."}'

# Update find-trader search prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/find_trader_search \
  -H "Content-Type: application/json" \
  -d '{"content":"Extract search params from the user request..."}'

# Update find-trader results-presentation prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/find_trader_presentation \
  -H "Content-Type: application/json" \
  -d '{"content":"Present matched workers conversationally..."}'

# Switch LLM provider
curl -X PUT http://localhost:8081/api/v1/system-prompts/provider \
  -H "Content-Type: application/json" \
  -d '{"content":"ollama"}'

# Reset to auto fallback chain (Mistral → OpenCode → Ollama)
curl -X PUT http://localhost:8081/api/v1/system-prompts/provider \
  -H "Content-Type: application/json" \
  -d '{"content":""}'
```

### User Role Reset

```bash
curl -X PUT http://localhost:8081/api/v1/user/reset-role \
  -H "Cookie: better-auth.something=..."
# → 200 OK, role cleared to ""
```

---

## Database

### `system_prompts` table

Singleton row (`id=1`) with five key columns:

| Column | Type | Purpose |
|--------|------|---------|
| `worker_profile_prompt` | `TEXT` | System prompt sent to the helper on worker profile intake chat |
| `client_profile_prompt` | `TEXT` | System prompt sent to the helper on client profile intake chat |
| `find_trader_search_prompt` | `TEXT` | System prompt sent to the helper for the search-params pass |
| `find_trader_presentation_prompt` | `TEXT` | System prompt sent to the helper for the results-presentation pass |
| `llm_provider` | `VARCHAR(32)` | `"opencode0"`, `"opencode1"`, `"opencode2"`, `"ollama"`, `"mistral"`, or `""` for auto fallback chain |

NOTE: there is no `helper_prompt` column. (The unread `helper_prompt` column referenced in older docs is not part of the `system_prompts` GORM model — admin PUT/GET talk to the four columns above.)

If `worker_profile_prompt` is empty at startup, a default prompt is seeded automatically that instructs the LLM to gather all 22 profile fields conversationally and output `[FIELDS]JSON[/FIELDS]` blocks.

If `client_profile_prompt` is empty at startup, a default prompt is seeded automatically that instructs the LLM to gather all 8 client profile fields conversationally and output `[FIELDS]JSON[/FIELDS]` blocks.

---

## gRPC Integration

The backend is a **gRPC client** to the helper:

```protobuf
service HelperService {
  rpc Ask(AskRequest) returns (AskResponse);
  rpc Embed(EmbedRequest) returns (EmbedResponse);          // vector search
  rpc EmbedBatch(EmbedBatchRequest) returns (EmbedBatchResponse);  // backfill
}

message AskRequest {
  string question = 1;
  repeated Message history = 2;
  string system_prompt = 3;   // loaded by backend from DB (worker_profile, client_profile, find_trader_*)
  string llm_provider = 4;    // "opencode0" | "opencode1" | "opencode2" | "mistral" | "ollama" | "" (= auto fallback chain)
  bool skip_role_detection = 5;  // backend always sends true; JSON role-tag detection is reserved for future search flows
}
```

> Note: the proto field comment uses `"ollama" | "opencode" | ""` as a shorthand, but the helper loads adapters under the keys `opencode0`, `opencode1`, `opencode2`, `mistral`, `ollama` — see `helper/main.py` and `infra/docker-compose.yml`. The backend always sends `skip_role_detection=true`; the helper never appends the role-tag JSON instruction.

Proto definition: `proto/helper/helper.proto` (canonical source). Generated Go bindings in `helper.pb.go` / `helper_grpc.pb.go` are checked in.

The `ChatHandler` dials the helper at startup and reconnects if the connection drops.

---

## Language (`lang`) Parameter

All chat endpoints accept a `lang` field in the JSON request body. The backend appends a language instruction to the system prompt before sending it to the helper:

- `lang: "es"` → appends `IMPORTANTE: Responde SIEMPRE en español al usuario`
- `lang: "en"` → appends `IMPORTANT: Always respond in English`

This instruction is appended to the system prompt for both `handleIntake` (worker/client) and both passes of `handleSearch`.

---

## Logging

All handlers use Go's `log/slog` with structured key-value pairs:

| Component | Events |
|-----------|--------|
| `main.go` | Startup, shutdown, request method/path/duration |
| `ChatHandler` (worker) | Message sizes, prompt length, provider, lang, detected_fields JSON, field counts |
| `ChatHandler` (client) | Message sizes, prompt length, provider, lang, detected_fields JSON, field counts |
| `SearchService` | Branch (vector/ilike), top score, cache_hit, duration_ms |
| `SystemPromptHandler` | GET/PUT operations, column name, cache refresh |
| `AuthMiddleware` | Session validation via auth service + DB fallback, missing/invalid cookies |
| `ConversationHandler` | Conversation list/get operations, user ID, conversation count, workers JSON readout |
| `PublicProfileHandler` | Public profile lookups by slug, latest profiles list |
| `DirectMessagingHandler` | DM send/read/archive, SSE subscribers, rate-limit decisions |

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP listen port |
| `AUTH_SERVICE_URL` | — (required) | Base URL of the auth service for session resolution (e.g. `http://helpingpeoplenow-auth:8083`) |
| `HELPER_GRPC_ADDR` | `helpingpeoplenow-helper:50051` | Helper gRPC address |
| `HELPER_HEALTH_URL` | `http://helpingpeoplenow-helper:8084/health` | Helper HTTP health endpoint (used by `/health` composite check) |
| `HELPER_TIMEOUT_SECONDS` | `60` | gRPC request timeout |
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

# Tests (race-detector + coverage; CI runs the same command)
go test -race -coverprofile=coverage.out ./...

# Format + vet + govulncheck (CI lint job)
gofmt -l . | tee fmt.out && test ! -s fmt.out
go vet ./...
go tool govulncheck ./...

# Docker build
docker build -t ghcr.io/helpingpeoplenow/backend:latest .
```

Coverage thresholds are enforced via `.testcoverage.yml` (60% overall; `internal/services/` and `internal/core/` at 90%; `internal/adapters/handler/` at 65%; `internal/adapters/realtime/` at 100%; `internal/ports/`, `internal/contextkeys/`, `internal/testingutil/`, `internal/adapters/repository/` at 0%).

The CI pipeline (`.github/workflows/ci.yml`) runs: `gofmt -l` + `go vet` + `govulncheck` (lint) → `go build` → `go test -race -coverprofile=coverage.out -covermode=atomic ./...` with a PostgreSQL 16 service container → Docker build/push to `ghcr.io/helpingpeoplenow/backend`. A second workflow `vector-parity.yml` runs `helper/scripts/test_byte_parity_gate.sh` to gate byte-level parity between Go and Python field-text hashing.

---

## Project Structure

```
backend/
├── main.go                            # Composition root: init DB, wire handlers, start server, sweeper goroutine
├── Dockerfile                         # Multi-stage: golang:1.25 → alpine:3.20 (static binary)
├── go.mod / go.sum                    # Go module dependencies
├── .testcoverage.yml                  # vladopajic/go-test-coverage thresholds (60% overall; per-package overrides)
├── VERSION                            # 0.4
├── proto/
│   └── helper/
│       ├── helper.proto               # gRPC contract (Ask + Embed + EmbedBatch)
│       ├── helper.pb.go               # Generated protobuf Go types
│       └── helper_grpc.pb.go          # Generated gRPC Go client/server
├── database/
│   └── postgres.go                    # GORM connection + pgvector extension + HNSW index + idempotent migrations
├── cmd/
│   └── hash_fixture/
│       └── main.go                    # CLI: prints Go's canonical field-name → text → SHA-256 fixtures
│                                       # (parity partner for helper/scripts/test_byte_parity_gate.sh)
└── internal/
    ├── core/
    │   ├── system_prompt.go           # SystemPrompt GORM model (4 prompt columns + llm_provider)
    │   ├── worker.go                  # WorkerProfile GORM model + DTO + ToFindTraderCard
    │   ├── worker_embeddings.go       # WorkerEmbedding (vector(768), text_hash, updated_at trigger)
    │   ├── client.go                  # ClientProfile GORM model + DTO
    │   ├── conversation.go            # Conversation + Message
    │   ├── direct_conversation.go     # DirectConversation
    │   ├── direct_message.go          # DirectMessage
    │   ├── fields.go                  # Field-merge helpers (rawString, rawFloat, rawInt, rawBool, mergeJSONArray, mergeSocialLinks)
    │   ├── parser.go                  # [FIELDS] / [SEARCH] block extractors
    │   ├── search.go                  # WorkerSearchFilters struct
    │   ├── prompts.go                 # Default system prompts (4)
    │   ├── env.go                     # GetEnvFloat / GetEnvBool helpers (vector tunables)
    │   ├── money.go / money_format.go # Hourly rate formatting
    │   └── phone.go
    ├── ports/                         # Interfaces only — services depend on these
    │   ├── llm_service.go             # LLMService (Ask, Health, Embed)
    │   ├── profile_repository.go      # ProfileRepository + RawQuerier + FindResult (Branch|Workers|TopScore)
    │   ├── chat_repository.go         # ChatRepository
    │   ├── system_prompt_repository.go
    │   ├── direct_message_repository.go
    │   └── direct_messaging.go        # Broker + Event (SSE pub/sub)
    ├── services/                      # Use-case orchestration
    │   ├── intake_service.go          # ProcessIntake: chat → [FIELDS] → map-merge upsert + debounced re-embed
    │   ├── search_service.go          # Search: two-pass LLM (filter-fill → present) + hybrid ILIKE/vector + 60s cache
    │   └── seed_service.go            # SeedSystemPrompts: defaults at startup
    ├── adapters/
    │   ├── handler/                   # HTTP handlers
    │   │   ├── chat_handler.go        # POST /api/v1/chat — mode dispatch + gRPC + lang
    │   │   ├── system_prompt_handler.go # System prompt CRUD (worker, client, search, presentation, provider)
    │   │   ├── worker_handler.go      # Worker profile (GET/DELETE)
    │   │   ├── client_handler.go      # Client profile (GET/DELETE)
    │   │   ├── conversation_handler.go # Conversation list/detail
    │   │   ├── direct_messaging_handler.go # DM: contact, inbox, thread, send, read, archive, block, report, SSE
    │   │   ├── admin_handler.go       # Generic CRUD over 5 admin entities
    │   │   ├── admin_table.go         # Entity metadata (table → columns)
    │   │   ├── health_handler.go      # Composite PG + helper-gRPC health
    │   │   ├── metrics_handler.go     # Homegrown Prometheus text (no client_golang)
    │   │   └── response.go            # writeJSON / writeError helpers
    │   ├── repository/                # GORM implementations
    │   │   ├── profile_repo.go        # Worker/Client CRUD + FindWorkers (vector|ilike) + embedding CRUD
    │   │   ├── chat_repo.go           # Conversations + Messages
    │   │   ├── system_prompt_repo.go  # SystemPrompt + in-memory cache
    │   │   └── direct_message_repo.go # DM CRUD (12 methods)
    │   ├── llm/
    │   │   └── grpc_client.go         # GRPCLLMService — gRPC client to helper (Ask, Health, Embed)
    │   ├── realtime/
    │   │   └── sse_broker.go          # In-process pub/sub for DM SSE
    │   ├── middleware/
    │   │   ├── auth.go                # AuthMiddleware (auth-service → DB fallback)
    │   │   ├── admin.go               # AdminMiddleware (calls AUTH_SERVICE_URL)
    │   │   ├── cors.go                # Origin-reflective CORS
    │   │   └── logging.go             # Structured slog HTTP logging
    │   └── ratelimit/
    │       └── rate_limiter.go        # Per-user token bucket (30 msg/min for DM send)
    ├── contextkeys/
    │   └── keys.go                    # SetUserID / GetUserID context helpers
    └── testingutil/
        └── fakes.go                   # Shared mocks for handler/service tests
└── tests/
    └── integration/                   # End-to-end PG-backed tests (chat_flow, profile_flow, conversations, etc.)
```
