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

1. **Worker profile intake chat** — receives `POST /api/v1/worker/chat`, uses a separate `worker_profile_prompt` system prompt designed to gather worker profile fields conversationally, appends a language instruction to the system prompt based on the `lang` parameter, returns the answer + parsed `detected_fields` in JSON; the backend auto-merges fields into the worker profile via map-based upsert
2. **Client profile intake chat** — receives `POST /api/v1/client/chat`, uses a separate `client_profile_prompt` system prompt designed to gather client profile fields conversationally, appends a language instruction to the system prompt based on the `lang` parameter, returns the answer + parsed `detected_fields` in JSON; the backend auto-merges fields into the client profile via map-based upsert
3. **System prompt management** — admin can read/update the helper prompt (`helper_prompt`), the worker profile prompt (`worker_profile_prompt`), the client profile prompt (`client_profile_prompt`), and the LLM provider (`llm_provider`) via REST endpoints
4. **LLM provider runtime switch** — admin can toggle between `opencode` (external), `ollama` (local), and `mistral` (cloud) without restarting the container; empty = uses the helper's auto fallback chain (Mistral → OpenCode → Ollama)
5. **Conversation persistence** — all chat messages (worker, client) are saved to the database and can be loaded on page reload via the conversations API
6. **Profile reset** — worker and client profiles can be cleared via `DELETE /api/v1/worker/profile` and `DELETE /api/v1/client/profile`

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
 │ (worker +   │ │ (GET/DELETE  │ │(GET/DELETE  │ │(list/get    │
 │  client     │ │  profile)    │ │ profile)    │ │ conversations)│
 │  chat +     │ └─────────────┘ └─────────────┘ └─────────────┘
 │  find-chat  │
 │  gRPC +     │ ┌──────────────────────┐
 │  lang)      │ │ SystemPromptHandler  │
 └──────┬──────┘ │ (CRUD for            │
        │        │  helper_prompt +     │
        │        │  worker_profile +    │
        │        │  client_profile +    │
        │        │  llm_provider)       │
        │        └──────────┬───────────┘
        │                   │
 ┌──────▼───────────────────▼──────────┐
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

- **No service layer** — the codebase was simplified after removing the `PromptHelper` CRUD. All business logic lives in handlers (`internal/adapters/handler/`)
- **No port/repository abstractions** — `SystemPromptHandler` uses `*gorm.DB` directly for DB operations, and `ChatHandler` uses the `*grpc.ClientConn` directly for gRPC calls
- **Cache pattern** — system prompts + provider are loaded into memory at startup and refreshed on every admin update via callbacks. This avoids hitting the DB on every chat request

---

## Request Flows

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

### Client Profile Intake Chat (`/api/v1/client/chat`)

```
Client types message in chat panel
       │
       ▼
POST /api/v1/client/chat ──► ChatHandler.HandleClientChat
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
| Instagram | `instagram` | string |
| Facebook | `facebook` | string |
| Twitter | `twitter` | string |
| LinkedIn | `linkedin` | string |
| TikTok | `tiktok` | string |
| YouTube | `youtube` | string |

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
| GET | `/health` | No | Health check → `{"status":"ok"}` |
| POST | `/api/v1/worker/chat` | No* | Worker profile intake chat → `{"answer","detected_fields","conversation_id"}` |
| POST | `/api/v1/client/chat` | No* | Client profile intake chat → `{"answer","detected_fields","conversation_id"}` |
| POST | `/api/v1/client/find-chat` | No* | Client find-chat (search) → `{"answer","conversation_id"}` |
| GET | `/api/v1/worker/profile` | Yes* | Get worker profile for authenticated user |
| DELETE | `/api/v1/worker/profile` | Yes* | Clear worker profile for authenticated user |
| GET | `/api/v1/client/profile` | Yes* | Get client profile for authenticated user |
| DELETE | `/api/v1/client/profile` | Yes* | Clear client profile for authenticated user |
| GET | `/api/v1/system-prompts` | Yes | Get helper + worker + client prompts + LLM provider |
| PUT | `/api/v1/system-prompts/helper` | Yes | Update the helper prompt text |
| PUT | `/api/v1/system-prompts/worker_profile` | Yes | Update the worker profile prompt text |
| PUT | `/api/v1/system-prompts/client_profile` | Yes | Update the client profile prompt text |
| PUT | `/api/v1/system-prompts/provider` | Yes | Set LLM provider ("opencode", "ollama", "mistral", or "" for auto fallback chain) |
| PUT | `/api/v1/user/reset-role` | Yes* | Clear user role (reset to "") |
| GET | `/api/v1/conversations` | Yes* | List conversations (supports `?type=worker&limit=N`) |
| GET | `/api/v1/conversations/:id` | Yes* | Get conversation with full message history |

*Worker/client chat is session-independent — it creates a new conversation context per request.
Session validation is done via cookie parsing + direct DB lookup.

### Health

Simple health check — no auth required. Used by load balancers, orchestrators, and monitoring to verify the service is alive.

```bash
curl http://localhost:8081/health
# → {"status":"ok"}
```

Returns `200 OK` with `{"status":"ok"}`. Because there is no request body, session, or database dependency, this endpoint is fast and reliable for uptime checks.

### Worker Profile Intake Chat

```bash
curl -X POST http://localhost:8081/api/v1/worker/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"I am a plumber in Madrid","history":[],"lang":"en"}'
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
curl -X POST http://localhost:8081/api/v1/client/chat \
  -H "Content-Type: application/json" \
  -d '{"message":"Hi, I need help fixing my bathroom","history":[],"lang":"en"}'
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
# → {"helper_prompt":"...", "worker_profile_prompt":"...", "client_profile_prompt":"...", "llm_provider":"opencode", "updated_at":"..."}

# Update helper prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/helper \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a helpful home services assistant..."}'

# Update worker profile prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/worker_profile \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a friendly profile-building assistant..."}'

# Update client profile prompt
curl -X PUT http://localhost:8081/api/v1/system-prompts/client_profile \
  -H "Content-Type: application/json" \
  -d '{"content":"You are a friendly profile-building assistant for clients..."}'

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

Singleton row (`id=1`) with four key columns:

| Column | Type | Purpose |
|--------|------|---------|
| `helper_prompt` | `TEXT` | System prompt sent to the helper on every chat request |
| `worker_profile_prompt` | `TEXT` | System prompt sent to the helper on worker profile intake chat |
| `client_profile_prompt` | `TEXT` | System prompt sent to the helper on client profile intake chat |
| `llm_provider` | `VARCHAR(32)` | `"opencode"`, `"ollama"`, `"mistral"`, or `""` for auto fallback chain |

If `worker_profile_prompt` is empty at startup, a default prompt is seeded automatically that instructs the LLM to gather all 22 profile fields conversationally and output `[FIELDS]JSON[/FIELDS]` blocks.

If `client_profile_prompt` is empty at startup, a default prompt is seeded automatically that instructs the LLM to gather all 8 client profile fields conversationally and output `[FIELDS]JSON[/FIELDS]` blocks.

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
  string system_prompt = 3;   // loaded by backend from DB (helper or worker_profile or client_profile)
  string llm_provider = 4;    // "opencode" | "ollama" | "mistral" | "" (= auto fallback chain)
}
```

Proto definition: `proto/helper/helper.proto`

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
| `SystemPromptHandler` | GET/PUT operations, column name, cache refresh |
| `AuthMiddleware` | Session validation, missing/invalid cookies |
| `ConversationHandler` | Conversation list/get operations, user ID, conversation count |

---

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8081` | HTTP listen port |
| `HELPER_GRPC_ADDR` | `helpingpeoplenow-helper:50051` | Helper gRPC address |
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
    │   ├── system_prompt.go      # SystemPrompt GORM model (helper, worker_profile, client_profile, llm_provider)
    │   ├── worker.go             # WorkerProfile GORM model + DTO
    │   └── client.go             # ClientProfile GORM model + DTO
    └── adapters/
        └── handler/
            ├── chat_handler.go           # Worker/client profile chat + find-chat + gRPC client + lang
            ├── system_prompt_handler.go  # System prompt CRUD (helper, worker, client, provider)
            ├── worker_handler.go         # Worker profile (GET/DELETE)
            ├── client_handler.go         # Client profile (GET/DELETE)
            └── conversation_handler.go   # Conversation list/detail (messages table)
```
