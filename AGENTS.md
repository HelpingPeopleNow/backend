# backend

Go REST API (stdlib `net/http`, `log/slog`) with hexagonal architecture. All chat traffic flows through a single unified endpoint (`/api/v1/chat`) with `mode` in the request body (`worker_intake`, `client_intake`, `search`). Manages system prompts/LLM provider, handles worker/client profiles, persists conversations, and powers worker↔client direct messaging.

## Commands

```bash
go run .              # needs Postgres + helper running
go build -o backend .
```

No tests, no lint/typecheck config, no CI.

## Architecture

- **Session cookie names** — each handler checks `__Secure-better-auth.session_token` first, then falls back to `better-auth.session_token`. The legacy `better-auth-session` cookie name has been removed.
- **SystemPromptHandler is admin-protected** — both GET and PUT require admin via `adminMiddleware`.
- **Hexagonal (ports & adapters) architecture.** `main.go` is the composition root. Handlers (`internal/adapters/handler/`) own HTTP parse, session validation, response shape — they delegate every use-case to a service or port. Services (`internal/services/`) own use-case logic: `SearchService.Search` (two-pass LLM: filter-fill pass then present pass), `IntakeService.ProcessIntake` (chat → `[FIELDS]` → map-merge upsert), `SeedService.SeedSystemPrompts` (defaults at startup). Services depend **only** on the interfaces in `internal/ports/` — never on `*gorm.DB`, `*grpc.ClientConn`, or any concrete adapter. Adapters (`internal/adapters/`) implement the ports: `repository/` (GORM via `*gorm.DB`), `llm/` (`grpc_client.go::GRPCLLMService`), `realtime/` (SSE broker), `middleware/`, `ratelimit/`. The interfaces themselves live in `internal/ports/`: `LLMService`, `ProfileRepository`, `ChatRepository`, `SystemPromptRepository`, `DirectMessageRepository`, `DirectMessaging`. *(See Gotchas for the note on prior doc drift in older revisions of this file.)*
- **DirectMessagingHandler is the one exception** — it injects multiple ports directly (`h.profs`, `h.dm`, `h.broker`) and skips the service layer (still no `*gorm.DB`). SSE/realtime concerns are tied to HTTP request lifecycle. Do not refactor this to add a service layer without first extracting a `DirectMessagingService`.
- **Worker profile arrays** (certifications, languages, social_links) stored as JSON strings in DB, marshalled/unmarshalled at handler boundaries (`worker_handler.go:119-157`).
- **Client profile fields**: `FullName`, `Phone`, `City`, `Address`, `Bio`, `PreferredContact`, `PropertyType`, `Notes` — all strings.
- **System prompt is a singleton row** (`id=1`) with five columns: `worker_profile_prompt`, `client_profile_prompt`, `find_trader_search_prompt`, `find_trader_presentation_prompt`, `llm_provider`. Upserted via raw SQL.
- **Map-based profile merge** — `handleIntake()` loads the existing profile from DB, then only overwrite fields present in the `[FIELDS]` block from the LLM response.
- **Chat uses a single unified endpoint** (`/api/v1/chat`) with `mode` in the request body.
- **Conversations** — `ConversationHandler` lists/fetches saved conversations from the `conversations` table with `messages` sub-table. Used by frontend to resume chat on page reload.

## Handlers

| Handler | Path | Methods | Purpose |
|---------|------|---------|---------|
| `ChatHandler` | `/api/v1/chat` | POST | Unified chat endpoint (mode in body: worker_intake, client_intake, search) |
| `WorkerHandler` | `/api/v1/worker/profile` | GET, DELETE | Worker profile read/reset |
| `ClientHandler` | `/api/v1/client/profile` | GET, DELETE | Client profile read/reset |
| `SystemPromptHandler` | `/api/v1/system-prompts` | GET, PUT | System prompts + provider CRUD |
| `ConversationHandler` | `/api/v1/conversations` | GET | List/get conversations |
| `DirectMessagingHandler` | `/api/v1/workers/:id/contact`, `/api/v1/direct-messages`, `/api/v1/direct-messages/:id/*` | GET, POST, PATCH | Direct messaging: create contact, inbox, thread, send, read, archive, block, report, SSE /stream, polling /since |

## Direct Messaging

Two-table schema: `direct_conversations` (unique per client+worker pair) + `direct_messages` (per-message rows with soft delete).

- **Repository**: `GormDirectMessageRepository` in `internal/adapters/repository/direct_message_repo.go` — implements `DirectMessageRepository` (12 methods)
- **Handler**: `DirectMessagingHandler` dispatches by path segment (see `ServeHTTP` switch). Auth via `contextkeys.GetUserID(r.Context())`.
- **SSE broker**: In-process pub/sub in `internal/adapters/realtime/sse_broker.go`. Subscribe creates buffered channels (32), Publish copies subscriber list under RLock and does non-blocking sends (drops on full). Cleanup goroutines per subscription.
- **SSE /stream**: Heartbeat every 25s to keep connections alive through Traefik/nginx. Events: `message`, `read`.
- **Rate limiting**: Per-user token bucket (30 msg/min) in `internal/adapters/middleware/rate_limiter.go`. Applied to `sendMessage` endpoint. Cleanup goroutine removes inactive buckets.
- **pushSSE**: Launched as async goroutine (`go h.pushSSE(...)`) after DB write — doesn't block HTTP response. Loads worker profile via `context.Background()` to resolve worker_profiles.id → user_id for publishing.
- **Report endpoint**: `POST /api/v1/direct-messages/:id/report` — logs a warning with conv_id + reported_by + reason. No DB storage yet.

## Gotchas

- **Architecture source of truth.** The Architecture section above is the source of truth for the current hexagonal layout. Deeper architectural notes (including the vector-search plan) live in `infra/docs/VECTOR_SEARCH_PLAN.md`. Older revisions of this file (and `README.md`) described a flatter "no service layer" / "no port/repository abstractions" model — that description predates the hexagonal refactor and is not accurate against the current `main.go` wiring.
- `AuthMiddleware` IS wired (contrary to older doc revisions): `main.go` constructs `*middleware.AuthMiddleware` via `middleware.NewAuthMiddleware(AUTH_SERVICE_URL, db)` and wraps every protected handler with `d.Auth.Wrap(...)`. Session is resolved via the auth service first, falling back to DB on failure. Do not bypass it from individual handlers.
- `sessionCookie()` / `rawSessionToken()` (`internal/adapters/middleware/auth.go`) check `__Secure-better-auth.session_token` first, then `better-auth.session_token`. The legacy `better-auth-session` cookie name has been removed.
- gRPC client uses `insecure.NewCredentials()` with `grpc.WithBlock()` at startup; failure is non-fatal and `ensureClient()` re-dials on each request if nil.
- Startup cache priming quirk: `NewChatHandler` is called first, then system prompt loaded into it, then a **second** `NewSystemPromptHandler` with `onUpdate` callbacks replaces the first one (`main.go:143-171`).
- Rate limit detection: gRPC error containing `"429"` or `"rate limit"` returns friendly JSON instead of HTTP error.
- CORS reflects the request `Origin` header with `Access-Control-Allow-Credentials: true`. Safe because `Origin` is set by the browser and cannot be spoofed cross-origin. When same-origin (no `Origin` header), no CORS headers are set at all.
- PUT endpoints for worker/client profiles were **removed** — profiles are now saved automatically by the chat handlers. Only DELETE (reset) endpoints remain.
- `user.role` column exists in DB but is no longer written by the backend.
- `chatRequest` struct includes `Lang` string — backend appends language instruction to system prompt based on value (`es` → Spanish, `en` → English).
- Vector search: search service emits an `Embed` gRPC call to the helper on each cache miss; repository picks an ILIKE or vector branch and reports it post-fact in `result.Branch`. Vector metrics (counts, top-score) are wired in `chat_handler.go → IncrVectorSearch/ObserveVectorScore`.

## Env vars

`PORT` (8081), `HELPER_GRPC_ADDR` (`helpingpeoplenow-helper:50051`), `HELPER_TIMEOUT_SECONDS` (60), `DATABASE_URL` or `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`.
