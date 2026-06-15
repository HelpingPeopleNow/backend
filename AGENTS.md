# backend

Go REST API (stdlib `net/http`, `log/slog`) with hexagonal architecture. Chat is split into dedicated per-role endpoints (worker/chat, client/chat, client/find-chat). Manages system prompts/LLM provider, handles worker/client profiles, persists conversations.

## Commands

```bash
go run .              # needs Postgres + helper running
go build -o backend .
```

No tests, no lint/typecheck config, no CI.

## Architecture

- **Session cookie names** — each handler checks `__Secure-better-auth.session_token` first, then falls back to `better-auth.session_token`. The legacy `better-auth-session` cookie name has been removed.
- **SystemPromptHandler has no auth** — any authenticated session can read/write system prompts.
- **No service/repository layer** — handlers inject `*gorm.DB` and `*grpc.ClientConn` directly.
- **Worker profile arrays** (certifications, languages, social_links) stored as JSON strings in DB, marshalled/unmarshalled at handler boundaries (`worker_handler.go:119-157`).
- **Client profile fields**: `FullName`, `Phone`, `City`, `Address`, `Bio`, `PreferredContact`, `PropertyType`, `Notes` — all strings.
- **System prompt is a singleton row** (`id=1`) with four columns: `worker_profile_prompt`, `client_profile_prompt`, `find_trader_search_prompt`, `find_trader_presentation_prompt`, `llm_provider`. Upserted via raw SQL.
- **Map-based profile merge** — `handleIntake()` loads the existing profile from DB, then only overwrite fields present in the `[FIELDS]` block from the LLM response.
- **Chat is split into dedicated per-role endpoints** (`worker/chat`, `client/chat`, `client/find-chat`).
- **Conversations** — `ConversationHandler` lists/fetches saved conversations from the `conversations` table with `messages` sub-table. Used by frontend to resume chat on page reload.

## Handlers

| Handler | Path | Methods | Purpose |
|---------|------|---------|---------|
| `WorkerHandler` | `/api/v1/worker/chat` | POST | Worker chat endpoint (worker_intake) |
| `ClientHandler` | `/api/v1/client/chat` | POST | Client chat endpoint (client_intake) |
| `ClientHandler` | `/api/v1/client/find-chat` | POST | Find-chat endpoint (search) |
| `WorkerHandler` | `/api/v1/worker/profile` | GET, DELETE | Worker profile read/reset |
| `ClientHandler` | `/api/v1/client/profile` | GET, DELETE | Client profile read/reset |
| `SystemPromptHandler` | `/api/v1/system-prompts` | GET, PUT | System prompts + provider CRUD |
| `ConversationHandler` | `/api/v1/conversations` | GET | List/get conversations |

## Gotchas

- `authMiddleware` was removed — each handler does its own session validation via `sessionCookie()` + `rawSessionToken()`.
- `extractUserIDFromRequest` (`worker_handler.go:177`) does direct DB lookup for session.
- gRPC client uses `insecure.NewCredentials()` with `grpc.WithBlock()` at startup; failure is non-fatal and `ensureClient()` re-dials on each request if nil.
- Startup cache priming quirk: `NewChatHandler` is called first, then system prompt loaded into it, then a **second** `NewSystemPromptHandler` with `onUpdate` callbacks replaces the first one (`main.go:143-171`).
- Rate limit detection: gRPC error containing `"429"` or `"rate limit"` returns friendly JSON instead of HTTP error.
- CORS reflects the request `Origin` header with `Access-Control-Allow-Credentials: true`. Safe because `Origin` is set by the browser and cannot be spoofed cross-origin. When same-origin (no `Origin` header), no CORS headers are set at all.
- PUT endpoints for worker/client profiles were **removed** — profiles are now saved automatically by the chat handlers. Only DELETE (reset) endpoints remain.
- `user.role` column exists in DB but is no longer written by the backend.
- `helper_prompt` column exists in `system_prompts` table but is unmapped by GORM.
- `chatRequest` struct includes `Lang` string — backend appends language instruction to system prompt based on value (`es` → Spanish, `en` → English).

## Env vars

`PORT` (8081), `HELPER_GRPC_ADDR` (`helpingpeoplenow-helper:50051`), `HELPER_TIMEOUT_SECONDS` (60), `DATABASE_URL` or `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`.
