# backend

Go REST API (stdlib `net/http`, `log/slog`) with hexagonal architecture. Orchestrates chat, manages system prompts/LLM provider, handles worker profiles.

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
- **System prompt is a singleton row** (`id=1`) upserted via raw SQL (`system_prompt_handler.go:121-126`).

## Gotchas

- `authMiddleware` func in `main.go` is **unused** — do not try to wire it or reference it in middleware chains.
- Role updates bypass the auth service entirely — reads `better-auth.session_token` cookie, queries `session` table directly, writes `role` in `user` table (`chat_handler.go:215-241`).
- `extractUserIDFromRequest` (`worker_handler.go:177`) does the same direct DB lookup — matches `chat_handler.go`'s pattern.
- gRPC client uses `insecure.NewCredentials()` with `grpc.WithBlock()` at startup; failure is non-fatal and `ensureClient()` re-dials on each request if nil.
- Startup cache priming quirk: `NewChatHandler` is called first, then system prompt loaded into it, then a **second** `NewSystemPromptHandler` with `onUpdate` callbacks replaces the first one (`main.go:143-171`).
- Rate limit detection: gRPC error containing `"429"` or `"rate limit"` returns friendly JSON instead of HTTP error (`chat_handler.go:190-195`).
- CORS is wide open (`Access-Control-Allow-Origin: *`).

## Env vars

`PORT` (8081), `HELPER_GRPC_ADDR` (`helpingpeoplenow-helper:50051`), `HELPER_TIMEOUT_SECONDS` (60), `DATABASE_URL` or `DB_HOST/PORT/USER/PASSWORD/NAME/SSLMODE`.
