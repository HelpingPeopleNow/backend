# backend

Go REST API (stdlib `net/http`, `log/slog`) with hexagonal architecture. All chat traffic flows through a single unified endpoint (`/api/v1/chat`) with `mode` in the request body (`worker_intake`, `client_intake`, `search`). Manages system prompts/LLM provider, handles worker/client profiles, persists conversations, and powers worker↔client direct messaging. Vector search (`worker_embeddings` / pgvector + HNSW) is wired into the `mode: search` path.

## Commands

```bash
go run .              # needs Postgres + helper running
go build -o backend .
go test -race -coverprofile=coverage.out ./...
# Coverage thresholds enforced via .testcoverage.yml (60% overall; services/core 90%, handlers 65%)
```

CI is `.github/workflows/ci.yml`: `gofmt -l` + `go vet` + `go tool govulncheck` (lint) → `go build` → `go test -race` with PG service container → Docker build/push to `ghcr.io/helpingpeoplenow/backend` → Deploy to Hermes (self-hosted runner, runs `deploy-service.sh backend`).

A second workflow `.github/workflows/vector-parity.yml` runs `helper/scripts/test_byte_parity_gate.sh` to gate byte-level parity between Go (`BuildFieldTexts`) and Python (`backfill_embeddings.py`).

## Architecture

- **Session cookie names** — each handler checks `__Secure-better-auth.session_token` first, then falls back to `better-auth.session_token`. The legacy `better-auth-session` cookie name has been removed.
- **SystemPromptHandler PUT is admin-protected, GET is auth-only** — `buildMux` wraps only the `/api/v1/system-prompts/` (trailing slash) route in `d.Admin.Wrap(...)` (used by PUT). The bare `/api/v1/system-prompts` route (used by GET) is wrapped only by `d.Auth.Wrap(...)`, so any authenticated user can read prompts + LLM provider.
- **Hexagonal (ports & adapters) architecture.** `main.go` is the composition root. Handlers (`internal/adapters/handler/`) own HTTP parse, session validation, response shape — they delegate every use-case to a service or port. Services (`internal/services/`) own use-case logic: `SearchService.Search` (two-pass LLM: filter-fill pass then present pass, hybrid ILIKE/vector), `IntakeService.ProcessIntake` (chat → `[FIELDS]` → map-merge upsert + debounced re-embed), `SeedService.SeedSystemPrompts` (defaults at startup). Services depend **only** on the interfaces in `internal/ports/` — never on `*gorm.DB`, `*grpc.ClientConn`, or any concrete adapter. Adapters (`internal/adapters/`) implement the ports: `repository/` (GORM via `*gorm.DB`), `llm/` (`grpc_client.go::GRPCLLMService`), `realtime/` (SSE broker), `middleware/`, `ratelimit/`. The interfaces themselves live in `internal/ports/`: `LLMService`, `ProfileRepository`, `ChatRepository`, `SystemPromptRepository`, `DirectMessageRepository`, `DirectMessaging`, `FeedbackRepository`, `Notifier`, `Broker`, `RawQuerier`.
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
- **Reembed kill switch** — `IntakeService.SetReembedEnabled(bool)` toggles re-embedding at runtime. When disabled, `ReembedWorker` and `scheduleReembed` short-circuit immediately. Controlled via `POST /api/v1/admin/reembed` (admin-protected, `ReembedToggleHandler`). Env `REEMBED_ENABLED` sets the default at startup. The toggle and metrics live in `internal/metrics/` (not `handler/` or `services/`) to avoid a handler↔services import cycle.
- **`internal/metrics/` package** — Standalone Prometheus helpers (gauge, counter, render) used by both `services/intake_service.go` and `adapters/handler/metrics_handler.go`. Houses reembed metrics: `reembed_enabled` (gauge), `reembed_skipped_total{reason}`, `reembed_completed_total`. The handler's `metricsHandler` appends `metrics.Render()` to the `/metrics` output.

### Readiness / shutdown (SPOF Phase 1–3 — see `infra/docs/FOLLOW_UP_SPOF.md`)

The single-replica SPOF was remediated in three commits; each one ships a primitive the next consumes:

- **`/readyz` endpoint (`internal/adapters/handler/readyz_handler.go`)** — `NewReadyzHandler(ReadyFlag())` returns 200 once `MarkReady()` flips the singleton `*atomic.Bool`, 503 while it's false. The handler is mounted in `buildMux` alongside `/health` (full readiness incl. helper) and `/livez` (process-only liveness for the container healthcheck). The three endpoints are **deliberately distinct**: `/health` is for dashboards (200/503 with details), `/livez` is for the docker healthcheck (container liveness, helper-degradation-immune), `/readyz` is solely the Traefik LB health-check target.
- **`handler.ReadyFlag()`** is a package-level `*atomic.Bool` singleton (constructed lazily on first call). It's the **single source of truth** — `MarkReady()` and `MarkUnready()` are the only two ways it flips, with **explicitly distinct names** so a future caller can't pass a generic `bool` and accidentally flip the wrong way (the original audit-pass dual-semantics bug). Companion test: `TestReadyzFlipsBothWays` round-trips MarkReady → MarkUnready → MarkReady and asserts the `{ready:true|false}` JSON envelope + 200/503 status on every step.
- **`main.go` `runShutdownSequence(ctx, startShutdown, cancelRoot, drainWait)`** — the SIGTERM/SIGINT handler body, extracted from the inline goroutine in `main()` so the regression test injects a `startShutdown func(ctx) error` recorder and asserts event order without a real `*http.Server` or a wall-clock 14s sleep in CI. Ordering invariant pinned by `TestShutdownSequenceDrainPhase`:
  1. `cancelRoot()` — stop the staleness sweeper, parallelize its 65s bounded drain with the LB drain window (the SWEEPER runs IN PARALLEL with the LB drain, not before or after).
  2. `handler.MarkUnready()` — flip `ReadyFlag()` to false → next Traefik LB health-check tick sees 5xx and drains.
  3. `time.Sleep(drainWait)` — covers worst-case Traefik detection latency (10s interval + 3s timeout + 1s slack).
  4. `server.Shutdown(30s)` — TCP listener close + wait for in-flight handlers.
- **`shutdownDrainDur()`** reads the `SHUTDOWN_DRAIN_WAIT` env var (Go `time.ParseDuration` syntax, e.g. `14s`, `0s` for snappy local-dev rebuilds, invalid/negative falls back to 14s with a `slog.Warn`). The OPERATIONAL NOTE block in the helper warns: Docker's default `stop_grace_period` and Kubernetes' default `terminationGracePeriodSeconds` are **both below 14s** — `infra/{,docker-compose-dev.}yaml` backend service pins `stop_grace_period: 120s` (15s drain + 30s Shutdown + 65s sweeper + slack) so SIGKILL doesn't preempt the drain.
- **`SHUTDOWN_DRAIN_WAIT`** is a NEW env var (added in `37d28f7`); defaults to `14s` if unset.

### Search hardening (audit F1–F16)

- **Search rate limiting** — ChatHandler accepts a `SearchRateLimiter` (token-bucket, 10 req/min/user). Excess returns 429 with `retry_after` header (F1).
- **Independent helper timeouts** — `HELPER_LLM_TIMEOUT` (default 20s) for Pass-1/Pass-2, `HELPER_EMBED_TIMEOUT` (default 8s) for Embed. Separate from the general `HELPER_TIMEOUT_SECONDS` dial timeout (F2).
- **Circuit breaker on helper gRPC** — After 5 consecutive failures, state → `open` (returns error immediately). After 30s cooldown → `half-open` (allows one probe). Emits `helper_breaker_state` gauge (F3).
- **Search input cap** — Messages over 2KB are truncated before Pass-1/Embed (F10).
- **Embed failure branch** — If Embed fails, `filters.EmbedFailed` is set to true. FindWorkers returns `branch='ilike_embed_failed'` instead of silently falling back. Increments `embed_failures_total` counter.
- **Search cache bounds** — Max 200 entries (`maxSearchCacheEntries`). Lazy eviction removes oldest entry when full.
- **Pre-key cache layer** — Before Pass-1/Embed, a sha256 hash of `(message+city)` is checked against cache. Identical repeat queries skip both LLM calls entirely.
- **VECTOR_SEARCH_MIN_TOP_SCORE** — Now wired at runtime. If vector top score falls below this threshold, falls back to ILIKE with `branch='ilike_low_top_score'`.
- **hnsw.ef_search** — Set to 64 at migration time (default was 40). Tune up when corpus exceeds ~1k vectors.
- **Templated 0-result message** — When FindWorkers returns 0 workers, Pass-2 is skipped entirely and a templated 'no workers found' message is returned in the user's language.
- **Profession normalizer parity** — Both `normalizeProfession` (services) and `normalizeProfessionForEmbedding` (core) return identical canonical English values. `carpintero→Carpenter`, `pintura→painter`, etc. Parity test covers all known keys.

## Handlers

| Handler | Path | Methods | Purpose |
|---------|------|---------|---------|
| `HealthHandler` | `/health` | GET | Composite PG + helper gRPC health (no auth) — used for dashboards / alerting; deliberately NOT used for the docker container healthcheck |
| `HealthHandler` (Livez method) | `/livez` | GET | Process + PG liveness only — the docker container healthcheck target. The handler type is the same `*HealthHandler` as `/health`; the `/livez` route invokes the `Livez` method (helper-ignoring liveness) so a helper outage doesn't cascade to a full API outage. |
| `ReadyzHandler` | `/readyz` | GET | Singleton `*atomic.Bool`-backed readiness (no auth) — flipped by `MarkReady()` once `main.buildMux` returns; flipped back by `MarkUnready()` from `main.runShutdownSequence` on SIGTERM. **Used by the Traefik LB health-check** (10s interval, 3s timeout) so a 503 drains the replica from the routing pool. See the Readiness / shutdown section below + `infra/docs/FOLLOW_UP_SPOF.md`. |
| `MetricsHandler` | `/metrics` | GET | Homegrown Prometheus text |
| `ChatHandler` | `/api/v1/chat` | POST | Unified chat endpoint (mode in body: worker_intake, client_intake, search) |
| `WorkerHandler` | `/api/v1/worker/profile` | GET, DELETE | Worker profile read/reset |
| `ClientHandler` | `/api/v1/client/profile` | GET, DELETE | Client profile read/reset |
| `SystemPromptHandler` | `/api/v1/system-prompts`, `/api/v1/system-prompts/` | GET, PUT | System prompts + provider CRUD |
| `ConversationHandler` | `/api/v1/conversations`, `/api/v1/conversations/{id}` | GET | List/get conversations |
| `DirectMessagingHandler` | `/api/v1/workers/{id}/contact`, `/api/v1/direct-messages`, `/api/v1/direct-messages/{id}/{action}` | GET, POST, PATCH | Direct messaging: contact, inbox, thread, send, read, archive, block, report, SSE `/stream`, polling `/since` |
| `AdminHandler` | `/api/v1/admin/{entity}/{id?}` | GET, PUT, DELETE | Generic admin CRUD over 9 entity slugs (`users`, `worker-profiles`, `client-profiles`, `conversations`, `messages`, `direct-conversations`, `direct-messages`, `direct-message-reports`, `feedback`) |
| `ReembedToggleHandler` | `/api/v1/admin/reembed` | POST | Runtime kill switch for the re-embedding pipeline (admin-protected). Body: `{"enabled": true/false}`. |
| `PublicProfileHandler` | `/api/v1/workers/public/latest`, `/api/v1/workers/public/{slug}` | GET | Public worker profiles — no auth required. Latest returns paginated list (default limit 6), slug returns single profile by URL-friendly slug. Returns `WorkerPublicDTO` (private fields stripped). |
| `FeedbackHandler` | `/api/v1/feedback` | POST | User-submitted feedback (any authenticated user). Validates message (1–2000 chars), page_url (1–2048 chars), category (bug/idea/complaint/general). Saves to `feedback` table with status `open`. Sends async Telegram notification via `Notifier`. Admin CRUD goes through `/api/v1/admin/` generic entity endpoint (`feedback` entity). |

## Direct Messaging

Two-table schema: `direct_conversations` (unique per client+worker pair) + `direct_messages` (per-message rows with soft delete).

- **Repository**: `GormDirectMessageRepository` in `internal/adapters/repository/direct_message_repo.go` — implements `DirectMessageRepository` (12 methods on `DirectMessageRepository` interface in `internal/ports/direct_message_repository.go`)
- **Handler**: `DirectMessagingHandler` dispatches by path segment (`ServeHTTP` switch in `internal/adapters/handler/direct_messaging_handler.go`). Auth via `contextkeys.GetUserID(r.Context())`.
- **SSE broker**: In-process pub/sub in `internal/adapters/realtime/sse_broker.go`. Subscribe creates buffered channels (32), Publish copies subscriber list under RLock and does non-blocking sends (drops on full). Cleanup goroutines per subscription.
- **SSE `/stream`**: Heartbeat every 25s to keep connections alive through Traefik/nginx. Events: `open` (connection established), `message`, `read`, `archive`, `block`, `report`.
- **Rate limiting**: Per-user token bucket (30 msg/min) in `internal/adapters/ratelimit/rate_limiter.go` (cap 30, refill 1/min). Applied to `sendMessage` endpoint. Cleanup goroutine removes inactive buckets.
- **`pushSSE`**: Launched as async goroutine (`go h.pushSSE(...)`) after DB write — doesn't block HTTP response. Loads worker profile via `context.Background()` to resolve `worker_profiles.id` → `user_id` for publishing.
|- **Report endpoint**: `POST /api/v1/direct-messages/{id}/report` — persists a `DirectMessageReport` via `h.dm.CreateReport()`, archives the conversation for the reporting user, and emits a `report` SSE event.
|- **CAPTCHA verification on worker contact** — `DirectMessagingHandler` enforces a Cap CAPTCHA check on `GET /api/v1/workers/:id/contact` when `CAP_SERVER_URL`, `CAP_SITE_KEY`, and `CAP_SECRET_KEY` are configured. The client sends a `captcha_token` query parameter; the handler POSTs to the Cap server for verification before allowing conversation creation. If any CAPTCHA env var is unset, verification is skipped (dev-friendly default).

## Vector search

- **pgvector extension** auto-installed by `database.Connect()` on startup. HNSW index `idx_worker_embeddings_hnsw` (m=16, ef_construction=64) auto-created with cosine distance.
- **`worker_embeddings` table** is `core.WorkerEmbedding` (composite PK `user_id`+`field_name`, `embedding vector(768)`, `model`, `text_hash` SHA-256 hex, `timestamptz updated_at`).
- **`Embed`/`EmbedBatch` gRPC** is the same one helper exposes (`internal/adapters/llm/grpc_client.go::GRPCLLMService.Embed`).
- **Tunables** in env: `VECTOR_SEARCH_ENABLED` (kill switch, default `true`), `VECTOR_SEARCH_MIN_SCORE` (per-row gate, default `0.3`). `VECTOR_SEARCH_MIN_TOP_SCORE` is **wired** (default `0.5`). If the vector top-score falls below this threshold, `FindWorkers` falls back to ILIKE with `branch='ilike_low_top_score'` (F7).
- **Branch selection** — `ProfileRepository.FindWorkers` returns `FindResult{Branch, Workers, TopScore}` where `Branch` is `"vector"` / `"ilike"` / `"ilike_disabled_via_env"` / `"ilike_fallback"`. Selection is post-fact (in the repo), not pre-fact (in the service), so the slog branch reflects what actually ran.
- **Metrics** — `vector_search_total{branch=...}` counter and `vector_score` histogram (wired in `internal/adapters/handler/metrics_handler.go` and incremented from `ChatHandler`).
- **Re-backfill** on schema change or after first enable: `docker exec helpingpeoplenow-helper env DB_HOST=helpingpeoplenow-postgres DB_USER=postgres DB_PASSWORD=postgres DB_NAME=helpingpeoplenow HELPER_GRPC_ADDR=localhost:50051 python3 /app/scripts/backfill_embeddings.py` (idempotent — skips rows whose `text_hash` matches existing).

## Feedback system
User-submitted feedback with in-app widget + Telegram notifications + admin dashboard.
- **`Feedback` model** — UUID PK, `user_id` (UUID), `page_url` (text), `message` (text 1–2000), `category` (bug/idea/complaint/general), `status` (open/in_progress/resolved/dismissed), `admin_note` (text nullable), `created_at`/`updated_at` timestamps. Auto-migrated via `database/postgres.go`.
- **`FeedbackRepository`** — `internal/ports/feedback_repository.go` interface. GORM implementation in `internal/adapters/repository/feedback_repo.go`. Methods: `Create`, `List(status, limit, offset)`, `UpdateStatus(id, status, adminNote)`, `CountByStatus()`.
- **`Notifier` interface** — `internal/ports/notifier.go`. Single method: `SendFeedbackAlert(fb *core.Feedback) error`. Implementation: `internal/adapters/notification/telegram.go` (Telegram Bot API, 1 msg/sec global rate limit).
- **Admin entity** — `"feedback"` added to `AdminHandler` entities map → `GET /api/v1/admin/feedback` returns paginated list, `PUT /api/v1/admin/feedback?id=&status=&admin_note=` updates status/note.
- **Env vars** — `TELEGRAM_BOT_TOKEN` and `TELEGRAM_CHAT_ID` (required for notifications). If unset, feedback is still saved but notifications are skipped.
- **Frontend** — `FeedbackWidget` (bottom-right FAB, all pages except admin), `FeedbackPopover` (form), `FeedbackAdminPage` (`/admin/feedback`). See frontend AGENTS.md for details.

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
- `user.role` column was dropped via migration (superseded by `is_admin`).
- `chatRequest` struct includes `Lang` string — `IntakeService.applyLanguage` (and `SearchService`) append a Spanish/English instruction to the system prompt based on the value.
- `ApplyLanguage` runs in both passes of search: filter-fill (`FindTraderSearchPrompt`) AND results-presentation (`FindTraderPresentationPrompt`).
- **Shutdown drain**: `main.go` registers the staleness sweeper on a `sync.WaitGroup` and drains it for up to 65s on SIGTERM (slightly above the 60s per-worker `ReembedWorker` deadline). On SIGTERM the inline signal goroutine now delegates to `runShutdownSequence(ctx, startShutdown, cancelRoot, drainWait)` (extracted for testability) which fires `cancelRoot()` → `MarkUnready()` → `time.Sleep(SHUTDOWN_DRAIN_WAIT)` (default 14s) → `server.Shutdown(30s)` in that order, so a Traefik LB health-check tick (10s interval, 3s timeout — see Phase 2 in `infra/docs/FOLLOW_UP_SPOF.md`) drains the replica before the accept listener closes.
- **Readyz is wired**, `MarkUnready()` is the only flip back, `/readyz` is the Traefik LB health-check target (NOT `/livez` and NOT `/health`). See the readiness / shutdown section above for the full invariant.
- **Helper version drift**: backend `HelmClient` doesn't pin a proto version, but helper's `proto/helper.proto` is the canonical source of truth.

## Env vars

Required at startup: `DB_HOST`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`, `AUTH_SERVICE_URL`, `HELPER_GRPC_ADDR`, `HELPER_HEALTH_URL`.

Optional: `TELEGRAM_BOT_TOKEN`, `TELEGRAM_CHAT_ID` (feedback notifications — if unset, feedback is saved but alerts are skipped), `REEMBED_ENABLED` (default true).

Optional: `PORT` (default `8081`), `HELPER_TIMEOUT_SECONDS` (default `60`, but docker-compose sets `600`), `HELPER_LLM_TIMEOUT` (default `20s`), `HELPER_EMBED_TIMEOUT` (default `8s`), `DATABASE_URL` or `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`, `VECTOR_SEARCH_ENABLED` (default `true`), `VECTOR_SEARCH_MIN_SCORE` (default `0.3`), `VECTOR_SEARCH_MIN_TOP_SCORE` (default `0.5`, wired — see Vector search section), `SHUTDOWN_DRAIN_WAIT` (default `14s`; Go duration format; `0s` allowed for snappy local-dev rebuilds; README/AGENTS for bigger values when Traefik config uses longer LB health-check intervals).

CAPTCHA: `CAP_SERVER_URL` (Cap verification server URL), `CAP_SITE_KEY` (site/public key), `CAP_SECRET_KEY` (secret key). Used by `DirectMessagingHandler` for CAPTCHA verification on the worker contact endpoint (`/api/v1/workers/:id/contact`). If unset, CAPTCHA verification is skipped (dev-friendly default).