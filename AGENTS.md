# backend

Go REST API (stdlib `net/http`, `log/slog`) with hexagonal architecture. Orchestrates chat, manages system prompts/LLM provider, handles worker/client profiles, persists conversations.

## Commands

```bash
go run .              # needs Postgres + helper running
go build -o backend .
```

No tests, no lint/typecheck config, no CI.

## Architecture

- **No middleware auth** — the `authMiddleware` in `main.go:38` is dead code (never wired). Each handler does its own session validation.
- **SystemPromptHandler has no auth** — any authenticated session can read/write system prompts.
- **Two incompatible cookie names** — hand-coded DB lookups use `better-auth.session_token` (the real one); the dead `authMiddleware` uses `better-auth-session`.
- **No service/repository layer** — handlers inject `*gorm.DB` and `*grpc.ClientConn` directly.
- **Worker profile arrays** (certifications, languages, social_links) stored as JSON strings in DB, marshalled/unmarshalled at handler boundaries (`worker_handler.go:119-157`).
- **Client profile fields**: `FullName`, `Phone`, `City`, `Address`, `Bio`, `PreferredContact`, `PropertyType`, `Notes` — all strings.
- **System prompt is a singleton row** (`id=1`) with four columns: `helper_prompt`, `worker_profile_prompt`, `client_profile_prompt`, `llm_provider`. Upserted via raw SQL (`system_prompt_handler.go:121-126`).
- **Map-based profile merge** — both `HandleWorkerChat` and `HandleClientChat` load the existing profile from DB, then only overwrite fields present in the `[FIELDS]` block from the LLM response.
- **Conversations** — `ConversationHandler` lists/fetches saved conversations from the `conversations` table with `messages` sub-table. Used by frontend to resume chat on page reload.

## Handlers

| Handler | Path | Methods | Purpose |
|---------|------|---------|---------|
| `ChatHandler` | `/api/v1/chat` | POST | Main chat + role detection |
| `ChatHandler` | `/api/v1/worker/chat` | POST | Worker profile intake chat |
| `ChatHandler` | `/api/v1/client/chat` | POST | Client profile intake chat |
| `WorkerHandler` | `/api/v1/worker/profile` | GET, DELETE | Worker profile read/reset |
| `ClientHandler` | `/api/v1/client/profile` | GET, DELETE | Client profile read/reset |
| `SystemPromptHandler` | `/api/v1/system-prompts` | GET, PUT | System prompts + provider CRUD |
| `ChatHandler` | `/api/v1/user/reset-role` | PUT | Clear user role |
| `ConversationHandler` | `/api/v1/conversations` | GET | List/get conversations |

## Gotchas

- `authMiddleware` func in `main.go` is **unused** — do not try to wire it or reference it in middleware chains.
- Role updates bypass the auth service entirely — reads `better-auth.session_token` cookie, queries `session` table directly, writes `role` in `user` table (`chat_handler.go:215-241`).
- `extractUserIDFromRequest` (`worker_handler.go:177`) does the same direct DB lookup — matches `chat_handler.go`'s pattern.
- gRPC client uses `insecure.NewCredentials()` with `grpc.WithBlock()` at startup; failure is non-fatal and `ensureClient()` re-dials on each request if nil.
- Startup cache priming quirk: `NewChatHandler` is called first, then system prompt loaded into it, then a **second** `NewSystemPromptHandler` with `onUpdate` callbacks replaces the first one (`main.go:143-171`).
- Rate limit detection: gRPC error containing `"429"` or `"rate limit"` returns friendly JSON instead of HTTP error (`chat_handler.go:190-195`).
- CORS is wide open (`Access-Control-Allow-Origin: *`).
- PUT endpoints for worker/client profiles were **removed** — profiles are now saved automatically by the chat handlers. Only DELETE (reset) endpoints remain.
- The `skip_role_detection` field in the proto is used by worker/client profile chat to suppress the role-detection JSON instruction in the system prompt.

## Env vars

`PORT` (8081), `HELPER_GRPC_ADDR` (`helpingpeoplenow-helper:50051`), `HELPER_TIMEOUT_SECONDS` (60), `DATABASE_URL` or `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`.
