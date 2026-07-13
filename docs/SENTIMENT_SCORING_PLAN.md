# Sentiment Scoring — Implementation Plan

> **Status**: Draft
> **Author**: Hermes Agent (for Alvaro T)
> **Date**: 2026-07-13
> **Scope**: `backend` repo — new `internal/sentiment/` package + schema + main.go wiring

---

## 1. Problem

There is currently no way to know whether a conversation between a client and a trader is going well or badly. The admin can see messages, but has to read every conversation manually. An operator needs a quick signal: "which conversations are angry, which are happy, which need attention?"

## 2. What this is

A background goroutine inside the Go backend that periodically scores the tone of 1:1 direct messages between users and traders on a 0–10 scale (0 = very angry, 10 = very happy), stores the score on the conversation row, and exposes it to the admin dashboard.

**Scope boundary**: only `direct_conversations` / `direct_messages` (real 1:1 DMs). NOT the chat intake `conversations` / `messages` tables (machine↔user interview to gather profile fields — no human sentiment there).

## 3. Design

### 3.1 Tick loop

```
Every 5 minutes:
  1. SELECT id FROM direct_conversations
     WHERE last_message_at < NOW() - INTERVAL '24 hours'
       AND sentiment_score IS NULL
       AND status = 'active'
     ORDER BY last_message_at ASC
     LIMIT 50;

  2. For each conversation:
     a. Fetch the last 20 messages (configurable via SENTIMENT_SCORE_MAX_MESSAGES)
     b. Format as transcript: "CLIENT: ...\nTRADER: ...\nCLIENT: ..."
     c. Send to helper gRPC with provider="mistral" + system prompt
     d. Parse JSON response: {"score": 7, "reason": "Professional and respectful tone"}
     e. Write score + reason + timestamp back to the row
```

### 3.2 Score reset on new message (critical correctness fix)

Any new message in a scored conversation must clear the score so the 24h clock restarts:

```sql
UPDATE direct_conversations
SET sentiment_score = NULL,
    sentiment_scored_at = NULL
WHERE id = ?;
```

This happens **inside the existing SendMessage transaction** (which already updates `last_message_at`, `last_message_preview`, and unread counts). Atomic — no race conditions, no orphan scores.

Without this fix: a conversation scored 8/10 on Tuesday, then the client sends an angry message on Friday, would still show 8/10 forever.

### 3.3 LLM provider choice

Provider is **hardcoded to `"mistral"`** regardless of the chat default (`llm_provider` field). Rationale:
- Sentiment analysis is a one-shot extraction task, not a conversational chat
- Mistral Large handles Spanish/Catalan/sarcasm better than the cheap-first fallback chain
- Cost is ~$0.001 per conversation × ~50 max per tick = ~$0.05 per 5-minute cycle worst-case
- The chat LLM default should stay cheap for the intake interview flow

### 3.4 System prompt

```
You are a conversation tone analyst. Analyze the following conversation between a client
and a tradesperson (plumber, electrician, cleaner, etc.) on a home-services platform.

Score the overall tone from 0 to 10:
- 0 = extremely angry, hostile, threatening
- 1-2 = very frustrated, rude, aggressive
- 3-4 = somewhat frustrated, tense
- 5 = neutral, professional
- 6-7 = friendly, cooperative
- 8-9 = very positive, warm
- 10 = extremely happy, enthusiastic

Respond with JSON ONLY, no other text:
{"score": <integer 0-10>, "reason": "<≤120 char explanation>"}
```

### 3.5 Transcript format

Messages are formatted as:

```
CLIENT: Hola, necesito un electricista urgente, se me ha cortado la luz
TRADER: Buenos días. ¿En qué zona está usted?
CLIENT: En el centro, cerca de la plaza mayor
TRADER: Perfecto, puedo ir esta tarde a las 5. ¿Le parece bien?
CLIENT: Sí, genial, muchas gracias
```

Only `sender_type` (CLIENT/TRADER) + `body` are included. No timestamps (keep it short for the LLM). No metadata.

### 3.6 Parser

Parse the LLM JSON response. Rules:
- Extract `score` as int, clamp to [0, 10] (ignore out-of-range values)
- Extract `reason` as string, truncate to 120 chars
- If JSON parse fails or score is missing: log warning, skip conversation (don't write garbage)
- If the LLM returns markdown code fences (` ```json ... ``` `): strip them before parsing (same pattern as `helper_agent.py`)

## 4. Schema changes

Three new nullable columns on `direct_conversations`:

```sql
ALTER TABLE direct_conversations ADD COLUMN sentiment_score    SMALLINT;
ALTER TABLE direct_conversations ADD COLUMN sentiment_reason   TEXT;
ALTER TABLE direct_conversations ADD COLUMN sentiment_scored_at TIMESTAMPTZ;
```

Via GORM AutoMigrate (per HPN convention: no migration files, just add to `database/postgres.go`).

## 5. Files to create

| File | Purpose |
|------|---------|
| `internal/sentiment/scanner.go` | `Scanner` struct, `Run(ctx)` tick loop, constructor |
| `internal/sentiment/prompt.go` | System prompt template + `FormatTranscript(messages)` helper |
| `internal/sentiment/parser.go` | `ParseScore(raw string) (score int, reason string, err error)` |
| `internal/ports/sentiment_repository.go` | Port interface: `FindEligibleConversations`, `FetchMessages`, `WriteScore`, `ClearScore` |
| `internal/adapters/repository/sentiment_repo.go` | GORM implementation of the port |

## 6. Files to modify

| File | Change |
|------|--------|
| `internal/core/direct_conversation.go` | Add `SentimentScore`, `SentimentReason`, `SentimentScoredAt` fields |
| `database/postgres.go` | Add `&core.DirectConversation{}` to AutoMigrate (already there — new columns auto-added) |
| `main.go` | Start `sentiment.NewScanner(...)` goroutine alongside `runStalenessSweeper` |
| `internal/adapters/repository/direct_message_repo.go` | Extend `SendMessage` transaction to clear `sentiment_score` + `sentiment_scored_at` |
| `internal/adapters/handler/direct_messaging_handler.go` | Pass sentiment repository to handler (for admin dashboard DTO) |

## 7. Configuration

| Env Var | Default | Description |
|---------|---------|-------------|
| `SENTIMENT_SCANNER_ENABLED` | `true` | Kill switch for the goroutine |
| `SENTIMENT_SCANNER_INTERVAL` | `5m` | Tick interval |
| `SENTIMENT_SCORE_COOLDOWN` | `24h` | Minimum age of last message before scoring |
| `SENTIMENT_SCANNER_BATCH_SIZE` | `50` | Max conversations per tick |
| `SENTIMENT_SCORE_MAX_MESSAGES` | `20` | Max messages included in transcript |

## 8. main.go wiring

Same pattern as `runStalenessSweeper`:

```go
// Sentiment scanner (background goroutine).
if os.Getenv("SENTIMENT_SCANNER_ENABLED") != "false" {
    rootWG.Add(1)
    go func() {
        defer rootWG.Done()
        sentiment.Run(rootCtx, sentiment.Config{
            DB:         db,
            LLM:        llmService,
            Interval:   parseDuration("SENTIMENT_SCANNER_INTERVAL", 5*time.Minute),
            Cooldown:   parseDuration("SENTIMENT_SCORE_COOLDOWN", 24*time.Hour),
            BatchSize:  parseInt("SENTIMENT_SCANNER_BATCH_SIZE", 50),
            MaxMessages: parseInt("SENTIMENT_SCORE_MAX_MESSAGES", 20),
        })
    }()
}
```

## 9. Tests

### Unit tests

- `parser_test.go`: score clamping, JSON parse failure, markdown fence stripping, empty reason, long reason truncation
- `prompt_test.go`: transcript format with multiple messages, single message, empty list

### Integration test

- `sentiment_integration_test.go` (in `tests/integration/`):
  1. Create a `direct_conversations` row with `last_message_at` 2 days ago, `sentiment_score = NULL`
  2. Insert 5 `direct_messages` (mix of CLIENT/TRADER, neutral tone)
  3. Run scanner tick (mock the LLM response to return `{"score": 6, "reason": "Professional"}`)
  4. Assert `sentiment_score = 6`, `sentiment_reason = "Professional"`, `sentiment_scored_at` is set
  5. Insert a new message, assert `sentiment_score` is NULL again (reset works)

## 10. Observability

**slog events:**
- `sentiment: tick started` (debug) — every tick boundary
- `sentiment: scored` (info) — per-conversation, includes `conv_id`, `score`, `latency_ms`
- `sentiment: skipped` (warn) — eligible row fetch failed / message fetch failed / LLM error
- `sentiment: reset` (debug) — in the message-insert path, confirms score was cleared

**Prometheus metrics** (in `internal/metrics/`):
- `sentiment_scored_total{outcome="ok|error"}` (counter)
- `sentiment_latency_seconds` (histogram)
- `sentiment_enabled` (gauge, 0 or 1)

## 11. Deployment

1. `go build` + `go test -race` locally
2. Commit + push to `main` (triggers CI: lint → build → test → Docker build/push)
3. SSH to EC2, pull the new backend image
4. `docker compose up -d backend` (restarts backend with new code, GORM auto-adds the 3 columns)
5. Verify: `docker logs helpingpeoplenow-backend --tail 20` should show no errors
6. Verify: check a conversation row in the DB — `sentiment_score` should be NULL initially

No database migration step needed — GORM handles it on startup.

## 12. Rollback

Set `SENTIMENT_SCANNER_ENABLED=false` in the `.env` file and restart the backend container. The columns stay but nothing populates them. The message-insert reset still runs (harmless no-op).

To fully remove: drop the 3 columns (nullable, no other code depends on them).

## 13. Future improvements

- **Admin dashboard exposure**: add `sentiment_score` to the admin entities map so it shows up in the existing admin UI
- **Telegram alert**: on score 0–2, push a notification to the admin Telegram channel
- **Auto-escalation**: create a "needs review" row in a queue table for low-score conversations
- **Per-role scoring**: separate scores for client tone vs trader tone (2× LLM calls)
- **Trend tracking**: keep historical scores, show trend arrows (improving / declining)
- **Multi-language prompt**: detect conversation language, adjust prompt language accordingly
