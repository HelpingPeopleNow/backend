# backend

Go REST API (stdlib `net/http`, `log/slog`) with hexagonal architecture. All chat traffic flows through a single unified endpoint (`/api/v1/chat`) with `mode` in the request body (`worker_intake`, `client_intake`, `search`). Manages system prompts/LLM provider, handles worker/client profiles, persists conversations, and powers worker↔client direct messaging. Vector search (`worker_embeddings` / pgvector + HNSW) is wired into the `mode: search` path.

## Commands

```bash
go run .              # needs Postgres + helper running
go build -o backend .
go test -race -coverprofile=coverage.out ./...
# Coverage thresholds enforced via .testcoverage.yml (60% overall; services/core 90%, handlers 65%)
```

CI is `.github/workflows/ci.yml`: `gofmt -l` + `go vet` + `go tool govulncheck` (lint) → `go build` → `go test -race` with PG service container → Docker build/push to `ghcr.io/helpingpeoplenow/backend`.

A second workflow `.github/workflows/vector-parity.yml` runs `helper/scripts/test_byte_parity_gate.sh` to gate byte-level parity between Go (`BuildFieldTexts`) and Python (`backfill_embeddings.py`).

## Architecture

- **Session cookie names** — each handler checks `__Secure-better-auth.session_token` first, then falls back to `better-auth.session_token`. The legacy `better-auth-session` cookie name has been removed.
- **SystemPromptHandler PUT is admin-protected, GET is auth-only** — `main.go:78-79` wraps only the `/api/v1/system-prompts/` (trailing slash) routes in `d.Admin.Wrap(...)` (used by PUT). The bare `/api/v1/system-prompts` route (used by GET) is wrapped only by `d.Auth.Wrap(...)`, so any authenticated user can read prompts + LLM provider.
- **Hexagonal (ports & adapters) architecture.** `main.go` is the composition root. Handlers (`internal/adapters/handler/`) own HTTP parse, session validation, response shape — they delegate every use-case to a service or port. Services (`internal/services/`) own use-case logic: `SearchService.Search` (two-pass LLM: filter-fill pass then present pass, hybrid ILIKE/vector), `IntakeService.ProcessIntake` (chat → `[FIELDS]` → map-merge upsert + debounced re-embed), `SeedService.SeedSystemPrompts` (defaults at startup). Services depend **only** on the interfaces in `internal/ports/` — never on `*gorm.DB`, `*grpc.ClientConn`, or any concrete adapter. Adapters (`internal/adapters/`) implement the ports: `repository/` (GORM via `*gorm.DB`), `llm/` (`grpc_client.go::GRPCLLMService`), `realtime/` (SSE broker), `middleware/`, `ratelimit/`. The interfaces themselves live in `internal/ports/`: `LLMService`, `ProfileRepository`, `ChatRepository`, `SystemPromptRepository`, `DirectMessageRepository`, `DirectMessaging`.
- **DirectMessagingHandler is the one exception** — it injects multiple ports directly (`h.dm`, `h.profs`, `h.broker`) and skips the service layer (still no `*gorm.DB`). SSE/realtime concerns are tied to HTTP request lifecycle. Do not refactor this to add a service layer without first extracting a `DirectMessagingService`.
- **Worker profile arrays** (certifications, languages, social_links) stored as JSON strings in DB, marshalled/unmarshalled at handler boundaries via `WorkerProfile.MergeFields` / `core.mergeJSONArray` / `core.mergeSocialLinks` (`backend/internal/core/fields.go`).
- **WorkerProfile slug** — auto-generated from `BusinessName` on first upsert (`GenerateSlug` in `internal/core/worker.go`), collision-resolved with `-N` suffix. Used for public profile URLs. `WorkerPublicDTO` strips private fields (Phone, Address, UserID) for unauthenticated responses.
- **FindTraderCard** includes `Slug` so the frontend WorkerCard can link to `/profile/:slug`. Added as part of the public profiles feature.
- **Client profile fields**: `FullName`, `Phone`, `City`, `Address`, `Bio`, `PreferredContact`, `PropertyType`, `Notes` — all strings.
- **System prompt is a singleton row** (`id=1`) with five columns: `worker_profile_prompt`, `client_profile_prompt`, `find_trader_search_prompt`, `find_trader_presentation_prompt`, `llm_provider`. Upserted via `SeedService.SeedSystemPrompts` from the defaults in `backend/internal/core/prompts.go` when columns are empty. In-memory cache lives in `GormSystemPromptRepository`.
- **Map-based profile merge** — `IntakeService.ProcessIntake` loads the existing profile from DB, then only overwrites fields present in the `[FIELDS]` block from the LLM response.
- **Chat uses a single unified endpoint** (`/api/v1/chat`) with `mode` in the request body.
- **Conversations** — `ConversationHandler` lists/fetches saved conversations from the `conversations` table with `messages` sub-table. Used by frontend to resume chat on page reload.
- **Re-embed on profile change** — `IntakeService.scheduleReembed` debounces a 60s per-user timer; `runStalenessSweeper` runs every 10 min in `main.go` to catch profiles that updated without a chat message. Both paths feed `IntakeService.ReembedWorker` (bounded by `reembedSem` cap of 3 to respect Ollama's `NUM_PARALLEL=1`).

## Handlers

| Handler | Path | Methods | Purpose |
|---------|------|---------|---------|
| `HealthHandler` | `/health` | GET | Composite PG + helper gRPC health (no auth) |
| `MetricsHandler` | `/metrics` | GET | Homegrown Prometheus text |
| `ChatHandler` | `/api/v1/chat` | POST | Unified chat endpoint (mode in body: worker_intake, client_intake, search) |
| `WorkerHandler` | `/api/v1/worker/profile` | GET, DELETE | Worker profile read/reset |
| `ClientHandler` | `/api/v1/client/profile` | GET, DELETE | Client profile read/reset |
| `SystemPromptHandler` | `/api/v1/system-prompts`, `/api/v1/system-prompts/` | GET, PUT | System prompts + provider CRUD |
| `ConversationHandler` | `/api/v1/conversations`, `/api/v1/conversations/{id}` | GET | List/get conversations |
| `DirectMessagingHandler` | `/api/v1/workers/{id}/contact`, `/api/v1/direct-messages`, `/api/v1/direct-messages/{id}/{action}` | GET, POST, PATCH | Direct messaging: contact, inbox, thread, send, read, archive, block, report, SSE `/stream`, polling `/since` |
| `AdminHandler` | `/api/v1/admin/{entity}/{id?}` | GET, PUT, DELETE | Generic admin CRUD over 5 entity slugs (`users`, `worker-profiles`, `client-profiles`, `conversations`, `messages`) |
| `PublicProfileHandler` | `/api/v1/workers/public/latest`, `/api/v1/workers/public/{slug}` | GET | Public worker profiles — no auth required. Latest returns paginated list (default limit 6), slug returns single profile by URL-friendly slug. Returns `WorkerPublicDTO` (private fields stripped). |

## Direct Messaging

Two-table schema: `direct_conversations` (unique per client+worker pair) + `direct_messages` (per-message rows with soft delete).

- **Repository**: `GormDirectMessageRepository` in `internal/adapters/repository/direct_message_repo.go` — implements `DirectMessageRepository` (12 methods on `DirectMessageRepository` interface in `internal/ports/direct_message_repository.go`)
- **Handler**: `DirectMessagingHandler` dispatches by path segment (`ServeHTTP` switch in `internal/adapters/handler/direct_messaging_handler.go`). Auth via `contextkeys.GetUserID(r.Context())`.
- **SSE broker**: In-process pub/sub in `internal/adapters/realtime/sse_broker.go`. Subscribe creates buffered channels (32), Publish copies subscriber list under RLock and does non-blocking sends (drops on full). Cleanup goroutines per subscription.
- **SSE `/stream`**: Heartbeat every 25s to keep connections alive through Traefik/nginx. Events: `open` (connection established), `message`, `read`, `archive`, `block`, `report`.
- **Rate limiting**: Per-user token bucket (30 msg/min) in `internal/adapters/ratelimit/rate_limiter.go` (cap 30, refill 1/min). Applied to `sendMessage` endpoint. Cleanup goroutine removes inactive buckets.
- **`pushSSE`**: Launched as async goroutine (`go h.pushSSE(...)`) after DB write — doesn't block HTTP response. Loads worker profile via `context.Background()` to resolve `worker_profiles.id` → `user_id` for publishing.
- **Report endpoint**: `POST /api/v1/direct-messages/{id}/report` — persists a `DirectMessageReport` via `h.dm.CreateReport()`, archives the conversation for the reporting user, and emits a `report` SSE event.

## Vector search

- **pgvector extension** auto-installed by `database.Connect()` on startup. HNSW index `idx_worker_embeddings_hnsw` (m=16, ef_construction=64) auto-created with cosine distance.
- **`worker_embeddings` table** is `core.WorkerEmbedding` (composite PK `user_id`+`field_name`, `embedding vector(768)`, `model`, `text_hash` SHA-256 hex, `timestamptz updated_at`).
- **`Embed`/`EmbedBatch` gRPC** is the same one helper exposes (`internal/adapters/llm/grpc_client.go::GRPCLLMService.Embed`).
- **Tunables** in env: `VECTOR_SEARCH_ENABLED` (kill switch, default `true`), `VECTOR_SEARCH_MIN_SCORE` (per-row gate, default `0.3`). `VECTOR_SEARCH_MIN_TOP_SCORE` is defined (default `0.5`) but **not yet wired at runtime** — the top-score gate was deferred from V1 because the existing per-row `VECTOR_SEARCH_MIN_SCORE` gate + ILIKE fallback on zero results already handle low-quality queries at current scale. Wire it when workers exceed ~1,000 and vague queries start returning 50 low-score results instead of falling back to ILIKE. (VECTOR_SEARCH_PLAN §N1, fourth-pass review Pitfall #4).
- **Branch selection** — `ProfileRepository.FindWorkers` returns `FindResult{Branch, Workers, TopScore}` where `Branch` is `"vector"` / `"ilike"` / `"ilike_disabled_via_env"` / `"ilike_fallback"`. Selection is post-fact (in the repo), not pre-fact (in the service), so the slog branch reflects what actually ran.
- **Metrics** — `vector_search_total{branch=...}` counter and `vector_score` histogram (wired in `internal/adapters/handler/metrics_handler.go` and incremented from `ChatHandler`).
- **Re-backfill** on schema change or after first enable: `docker exec helpingpeoplenow-helper env DB_HOST=helpingpeoplenow-postgres DB_USER=postgres DB_PASSWORD=postgres DB_NAME=helpingpeoplenow HELPER_GRPC_ADDR=localhost:50051 python3 /app/scripts/backfill_embeddings.py` (idempotent — skips rows whose `text_hash` matches existing).

## GPS Geolocation

GPS coordinates enable proximity-based worker search. Clients and workers can optionally provide latitude/longitude, which are used to compute real-world distances and sort search results nearest-first.

- **Profile latitude/longitude fields** — `WorkerProfile` and `ClientProfile` both have `Latitude` and `Longitude` fields (nullable `*float64`). These persist the user's last-known location.
- **Haversine distance** — `core.HaversineKm(lat1, lon1, lat2, lon2)` in `internal/core/haversine.go` computes great-circle distance in kilometres between two coordinate pairs. Used by search to rank workers by proximity.
- **ChatHandler request body** — `latitude` and `longitude` optional float64 fields in the chat request. Accepted by all three modes (`worker_intake`, `client_intake`, `search`). The frontend sends the browser's geolocation on every chat request.
- **Intake GPS storage** — `IntakeService.ProcessIntake` upserts the request's latitude/longitude directly onto the profile, bypassing the LLM `[FIELDS]` parsing. Coordinates are never sent to the LLM; they are extracted from the request body and written to the profile before (or alongside) the map-merge step.
- **Search request coords override** — `SearchService.Search` prefers the latitude/longitude from the incoming chat request over whatever is stored on the profile. This means a client searching from a different location gets results relative to where they are *now*, not where they last saved.
- **Search results sorted nearest-first** — Both `findWorkersILIKE` (ILIKE branch) and `FindWorkers` (vector branch) compute `distance_km` for every candidate using `HaversineKm` against the request coordinates, then sort ascending by distance. Workers without valid coordinates sort to the end.
- **System prompts GPS-aware** — The `find_trader_search_prompt` and `find_trader_presentation_prompt` system prompts include instructions for the LLM to consider distance when presenting results to clients, e.g. mentioning proximity or suggesting the client confirm travel distance.
- **DB migration** — Adds `latitude DOUBLE PRECISION` and `longitude DOUBLE PRECISION` columns to both `worker_profiles` and `client_profiles` tables. Creates `idx_worker_profiles_coords` index on `(latitude, longitude)` for efficient coordinate lookups. Columns are nullable; existing rows default to NULL.

## Gotchas

- **Architecture source of truth.** The Architecture section above is the source of truth for the current hexagonal layout. Vector search is documented in the Vector search section below; the implementation plan was `infra/docs/VECTOR_SEARCH_PLAN.md` (deleted after shipping).
- `AuthMiddleware` IS wired: `main.go` constructs `*middleware.AuthMiddleware` via `middleware.NewAuthMiddleware(AUTH_SERVICE_URL, db)` and wraps every protected handler with `d.Auth.Wrap(...)`. Session is resolved via the auth service first, falling back to DB on failure. Do not bypass it from individual handlers.
- `sessionCookie()` / `rawSessionToken()` (`internal/adapters/middleware/auth.go`) check `__Secure-better-auth.session_token` first, then `better-auth.session_token`. The legacy `better-auth-session` cookie name has been removed.
- gRPC client uses `insecure.NewCredentials()` with `grpc.WithBlock()` at startup; failure is non-fatal and `ensureClient()` re-dials on each request if nil.
- Rate limit detection: gRPC error containing `"429"` or `"rate limit"` returns friendly JSON instead of HTTP error (`GRPCLLMService.Ask` returns `RATE_LIMIT:` prefix).
- CORS reflects the request `Origin` header with `Access-Control-Allow-Credentials: true`. Safe because `Origin` is set by the browser and cannot be spoofed cross-origin. When same-origin (no `Origin` header), no CORS headers are set at all.
- PUT endpoints for worker/client profiles were **removed** — profiles are now saved automatically by the chat handlers. Only DELETE (reset) endpoints remain.
- `user.role` column exists in DB but is no longer written by the backend.
- `chatRequest` struct includes `Lang` string — `IntakeService.applyLanguage` (and `SearchService`) append a Spanish/English instruction to the system prompt based on the value.
- `ApplyLanguage` runs in both passes of search: filter-fill (`FindTraderSearchPrompt`) AND results-presentation (`FindTraderPresentationPrompt`).
- **Shutdown drain**: `main.go` registers the staleness sweeper on a `sync.WaitGroup` and drains it for up to 65s on SIGTERM (slightly above the 60s per-worker `ReembedWorker` deadline).
- **Helper version drift**: backend `HelmClient` doesn't pin a proto version, but helper's `proto/helper.proto` is the canonical source of truth.

## Env vars

Required at startup: `DB_HOST`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `AUTH_SERVICE_URL`, `HELPER_GRPC_ADDR`, `HELPER_HEALTH_URL`.

Optional: `PORT` (default `8081`), `HELPER_TIMEOUT_SECONDS` (default `60`, but docker-compose sets `600`), `DATABASE_URL` or `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`, `VECTOR_SEARCH_ENABLED` (default `true`), `VECTOR_SEARCH_MIN_SCORE` (default `0.3`). Note: `VECTOR_SEARCH_MIN_TOP_SCORE` is defined but not wired (see Vector search section).