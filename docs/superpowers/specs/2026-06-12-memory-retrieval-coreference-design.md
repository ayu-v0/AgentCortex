# Memory Retrieval Coreference Refactor Design

## Goal

Refactor memory retrieval so search questions are resolved against recent server-side conversation context before embedding. The new retrieval flow is:

1. Load recent conversation turns for the same `user_id`, `agent_id`, and `session_id`.
2. Resolve references in the current question into a self-contained retrieval question.
3. Embed the resolved retrieval question.
4. Search the vector store for similar memory IDs and distances.
5. Look up those IDs in the database for authoritative markdown location metadata.
6. Read the matching markdown memory entries and return markdown-backed result content.

The external search response remains result-oriented:

```json
{
  "results": [
    {
      "id": "memory-1",
      "content": "...",
      "distance": 0.25
    }
  ]
}
```

The API must not return the resolved question. The service logs rewrite metadata, the original question, and the resolved question by default.

## Current State

`POST /api/v1/memories/search` currently accepts `agent_id`, `user_id`, `question`, and optional `limit`. The handler embeds the raw question, calls memory search, and replaces result content from markdown entries.

`POST /api/v1/qa/stream` accepts `agent_id`, `user_id`, and a message list. It streams a model response, then saves the completed Q/A as long-term memory and markdown. It does not currently persist short-term conversation turns for later retrieval rewriting.

The current session package manages active streaming session state, but it does not store completed conversation history. The CLI has an in-process conversation object, but that is not a server-side source of truth.

The sqlite backend stores long-term memories and vector embeddings. Vector search currently joins memory rows to obtain metadata such as `recorded_at`. The refactor should make vector search and database metadata lookup explicit separate steps.

## API Contract

### QA Stream Request

`POST /api/v1/qa/stream`

```json
{
  "agent_id": "agent-1",
  "user_id": "user-1",
  "session_id": "session-1",
  "messages": [
    {
      "role": "user",
      "content": "What should we change?"
    }
  ]
}
```

Rules:

- `agent_id`, `user_id`, `session_id`, and `messages` are required.
- `session_id` is a breaking required field.
- Missing or invalid `session_id` returns 400.
- Completed QA turns are persisted to conversation history after successful stream completion.

### Memory Search Request

`POST /api/v1/memories/search`

```json
{
  "agent_id": "agent-1",
  "user_id": "user-1",
  "session_id": "session-1",
  "question": "What about that one?",
  "limit": 10
}
```

Rules:

- `agent_id`, `user_id`, `session_id`, and `question` are required.
- `session_id` is required and has no default fallback.
- `limit` is optional and defaults to 10.
- `limit` keeps the existing maximum of 100.
- Search does not write conversation history.

### Session ID Validation

`session_id` must:

- be non-empty after trim,
- not contain leading or trailing whitespace,
- be at most 128 characters,
- contain only letters, digits, `_`, and `-`.

The CLI should generate one session ID per interactive run and reset it when the user clears the conversation.

## Conversation History

Add a short-term conversation history subsystem in `internal/conversation`.

### Model

Each persisted turn contains:

- autoincrement primary key,
- `user_id`,
- `agent_id`,
- `session_id`,
- `question`,
- `answer`,
- `source`,
- UTC `created_at`.

`source` is initially fixed to `qa_stream`. Do not add arbitrary metadata JSON in this refactor.

### Persistence Rules

After normal `/qa/stream` completion:

1. Trim the final user question and assistant answer.
2. If either is empty, do not write a conversation turn.
3. Write only the final completed Q/A turn, not the full request message list.
4. Write conversation history before long-term memory save.
5. If conversation history write fails, log the detailed error and emit a generic SSE warning.
6. Do not block QA response, markdown sync, or long-term memory save on conversation history failures.

Do not persist conversation history on client disconnect, interrupted stream, model error, or empty answer.

### Recent Turn Lookup

Memory retrieval loads recent turns strictly by `user_id + agent_id + session_id`.

The service must not:

- cross session boundaries,
- backfill from other sessions,
- use long-term memory rows as conversation history,
- use client-provided message history for retrieval rewriting.

Lookup queries should fetch newest turns first using `created_at DESC, id DESC`, then reverse them into chronological order for prompts.

### Retention and Cleanup

Configuration:

- `RECENT_SESSION_TURN_LIMIT`: default 5, maximum 20.
- `CONVERSATION_RETENTION_DAYS`: default 30, must be greater than 0.
- `CONVERSATION_CLEANUP_ENABLED`: default true.
- `CONVERSATION_CLEANUP_TIME`: default `03:00`.
- `CONVERSATION_CLEANUP_TIMEZONE`: default `Asia/Shanghai`.

Cleanup behavior:

- Start cleanup from bootstrap/runtime after constructing the conversation service.
- Stop cleanup from `Runtime.Close()`.
- Run global cleanup of all conversation turns older than the retention cutoff.
- Store timestamps and compute delete cutoffs in UTC.
- Use the configured timezone only to compute the nightly schedule time.
- Cleanup is idempotent and assumes single-instance deployment.
- Cleanup failures are logged but do not affect `/health`.
- Invalid cleanup config fails service startup.

## Coreference Rewrite

Add a query rewrite abstraction that accepts:

- current question,
- recent conversation turns,
- rewrite configuration,
- request context.

It returns:

- final retrieval question,
- rewrite stage metadata,
- whether fallback was used,
- logging fields.

The implementation should use the existing non-streaming `model.Client.Generate` method.

### Model Call Configuration

Rewrite model calls use:

- existing model provider and model client,
- `temperature=0.6`,
- `max_tokens=20000`,
- no tools,
- `tool_choice=none` when supported,
- existing `MODEL_TIMEOUT`.

Default `MODEL_TIMEOUT` changes from `30s` to `300s`. Do not add a separate rewrite timeout in this refactor.

Tool calls or empty text responses are invalid rewrite output.

### Output Contract

The model must return JSON:

```json
{
  "resolved_question": "What should we change in the memory retrieval refactor?",
  "confidence": 0.82,
  "needs_assistant_context": false
}
```

Validation rules:

- `resolved_question` must be non-empty.
- `resolved_question` over 100,000 characters is invalid.
- `confidence` is used for routing.
- `needs_assistant_context=true` forces stage 2.

### JSON Repair

Use `github.com/kaptinlin/jsonrepair` through an internal adapter.

Parsing sequence:

1. Trim surrounding whitespace.
2. Remove surrounding markdown code fences when the whole response is fenced JSON.
3. Try standard JSON decode.
4. If decode fails, repair with `jsonrepair`.
5. Decode the repaired JSON.
6. Validate fields.

Do not log the full repaired JSON blob. Log whether repair was used.

Invalid model output retries once per stage. Provider errors do not retry.

### Stage 1: User-Only Rewrite

Stage 1 input:

- current question,
- recent user questions only,
- no assistant answers.

Input budget is 20,000 characters. Preserve the current question in full. If the current question alone exceeds 20,000 characters, skip rewrite and use the original question.

Routing:

- If no conversation history exists, skip rewrite and use the original question.
- If valid stage 1 output has `confidence >= 0.75` and `needs_assistant_context=false`, use stage 1 output unless the pronoun heuristic forces stage 2.
- If `confidence < 0.75`, enter stage 2.
- If `needs_assistant_context=true`, enter stage 2.
- If output is invalid, retry stage 1 once, then enter stage 2 if still invalid.

### Pronoun Heuristic

If the current question contains obvious coreference markers and stage 1 leaves the question unchanged or nearly unchanged, force stage 2 even when confidence is high.

The initial marker set should include common Chinese pronouns and references, plus English markers such as:

- `this`
- `that`
- `it`
- `they`
- `the previous`

### Stage 2: Full-Context Rewrite

Stage 2 input:

- current question,
- recent user questions,
- recent assistant answers.

Input budget is 20,000 characters.

Cropping priority:

1. Preserve the current question in full.
2. Prefer newer turns over older turns.
3. Preserve user questions before assistant answers.
4. If an assistant answer is too long, keep its beginning and end with an omission marker in the middle.

Stage 2 uses the same JSON output contract as stage 1. Invalid output retries once. A valid stage 2 result is used even when confidence is low. If stage 2 still fails, fallback to the original question and continue retrieval.

### Rewrite Logging

Log these fields:

- `user_id`
- `agent_id`
- `session_id`
- `stage1_status`
- `stage2_status`
- `final_query_source`
- `stage1_confidence`
- `history_turns_used`
- `assistant_context_chars`
- `json_repaired`
- `fallback_used`
- `original_question_chars`
- `resolved_question_chars`
- `original_question`
- `resolved_question`
- latency

`original_question` and `resolved_question` are logged by default.

The search response must not return the resolved question.

## Retrieval Flow

Add `internal/retrieval` as the orchestration layer.

The retrieval service coordinates:

1. Validate request inputs.
2. Load recent conversation turns.
3. Rewrite the current question when history exists.
4. Embed the final retrieval question.
5. Over-fetch vector candidates.
6. Resolve vector IDs through database metadata.
7. Hydrate markdown entries.
8. Skip invalid candidates.
9. Return valid results in vector order.

`/memories/search` should call this service. `/qa/stream` should not call retrieval in this refactor, but the service should be reusable for later QA prompt injection.

## Vector Search and Metadata Lookup

Split vector search from memory metadata lookup.

Vector search should return only:

- `memory_id`,
- `distance`.

Database metadata lookup should batch query by memory IDs and return:

- memory ID,
- user ID,
- agent ID,
- recorded timestamp,
- storage version.

The sqlite backend can remain a single concrete implementation, but retrieval should depend on smaller interfaces.

Final response ordering must follow vector similarity order. Database `IN` query order must not be trusted; use a map by ID or equivalent reordering logic.

## Candidate Hydration

Internal vector search should over-fetch:

```text
internal_limit = min(request_limit * 3, 100)
```

Hydration behavior:

- If vector ID has no database metadata, skip it and log.
- If metadata points to missing markdown file, skip it and log.
- If markdown entry is missing or unreadable, skip it and log.
- Preserve vector order for valid hydrated results.
- Truncate valid results to the requested `limit`.
- If all candidates are skipped, return `200` with empty `results`.

Provider failures, database query failures, and embedding failures remain request-level errors. Candidate-specific consistency problems are skipped.

## Markdown Package

Move markdown read/write/parse logic out of the HTTP package into a reusable internal package.

The package should own:

- path rules,
- daily filename rules,
- entry marker rendering,
- entry marker parsing,
- read helpers,
- write helpers,
- entry lookup by memory ID.

HTTP and retrieval should consume this package instead of owning markdown parser logic.

## Long-Term Memory Rules

Long-term memory remains separate from session context.

Rules:

- Do not add `session_id` to memory rows.
- Do not add `session_id` to markdown files.
- Long-term memory search remains scoped by `user_id + agent_id`.
- `session_id` only selects recent conversation context for query rewriting.
- Saving memory continues to embed the original user question, not the resolved retrieval question.
- Search embeds the resolved retrieval question.

## Configuration

Add or update:

- `MODEL_TIMEOUT`: default `300s`.
- `RECENT_SESSION_TURN_LIMIT`: default `5`, maximum `20`.
- `CONVERSATION_RETENTION_DAYS`: default `30`, must be greater than 0.
- `CONVERSATION_CLEANUP_ENABLED`: default `true`.
- `CONVERSATION_CLEANUP_TIME`: default `03:00`.
- `CONVERSATION_CLEANUP_TIMEZONE`: default `Asia/Shanghai`.

Invalid required config should fail startup.

## Error Handling

HTTP 400:

- invalid JSON,
- missing required field,
- invalid `session_id`,
- invalid `agent_id` or `user_id`,
- invalid `limit`.

HTTP 500:

- embedding provider failure,
- vector search failure,
- database metadata lookup failure,
- systemic markdown filesystem failure when the retrieval service cannot proceed,
- model client construction failure at startup.

Degraded but successful behavior:

- no recent conversation history: skip rewrite and search original question,
- rewrite provider failure: fallback to original question and log,
- invalid rewrite output after retries: fallback to original question and log,
- missing candidate metadata: skip candidate and log,
- missing candidate markdown entry: skip candidate and log,
- all candidates skipped: return 200 with empty results.

SSE warnings:

- conversation history write failure emits a generic warning and does not block QA.

Cleanup failures:

- log only,
- do not affect `/health`.

## Testing

Use test-first implementation.

Key tests:

- `session_id` is required on QA stream and memory search.
- Invalid `session_id` returns 400.
- CLI generates one session ID per interactive run.
- CLI resets session ID on clear.
- Completed QA stream writes exactly one conversation turn.
- QA stream does not write conversation history on disconnect, model error, or empty answer.
- Conversation write failure emits SSE warning and does not block memory save.
- Recent turns are loaded only from the same user, agent, and session.
- Recent turns are presented to rewrite prompts in chronological order.
- Cleanup deletes expired rows globally.
- Cleanup can be disabled.
- Invalid cleanup config fails startup.
- Default model timeout is 300 seconds.
- Rewrite skips when no history exists.
- Stage 1 high confidence uses user-only resolved question.
- Stage 1 low confidence enters stage 2.
- `needs_assistant_context=true` enters stage 2.
- Pronoun heuristic forces stage 2.
- Invalid stage output retries once.
- Provider error does not retry.
- Stage 2 valid low-confidence output is used.
- Rewrite failure falls back to original question.
- JSON repair handles repairable malformed output.
- Empty or oversized resolved question is invalid.
- Search embeds resolved question.
- Search does not return resolved question.
- Search logs original and resolved question.
- Vector search returns only IDs and distances.
- Batch metadata lookup resolves markdown location.
- Final ordering follows vector result order.
- Over-fetch skips invalid candidates and truncates valid results to request limit.
- All skipped candidates return empty results with 200.
- Long-term memory save still embeds original question.
- Markdown parser behavior remains compatible after package extraction.

## Out of Scope

- Automatically injecting retrieved memories into `/qa/stream` prompts.
- Re-embedding existing stored memories.
- Rebuilding existing markdown files.
- Adding `session_id` to long-term memory rows or markdown files.
- Returning resolved questions in API responses.
- Full-text ranking inside markdown.
- Distributed cleanup coordination.
- Health response expansion for cleanup status.
- Metrics endpoints.

## Migration Notes

This refactor introduces a breaking API change because `session_id` becomes required. Update all clients and tests in the same change set.

Existing memory rows and markdown files do not need migration. Conversation history starts empty after deployment.

The new `conversation_turns` table can be created with `CREATE TABLE IF NOT EXISTS`. Existing sqlite memory tables should not be rewritten for this feature.
