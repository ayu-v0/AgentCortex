# Refactor memory retrieval with server-side coreference resolution

## Problem Statement

Memory search currently embeds the raw search question and uses that vector to retrieve candidate memories. This works for standalone questions, but it is weak for follow-up questions that depend on recent conversation context, such as "what about that one?", "how should I change it?", or "use the previous option". In those cases the embedded text lacks the entity or topic needed for semantic recall, so vector search can retrieve unrelated memories or miss the intended memory.

The target flow is:

1. Load the recent conversation turns for the same user, agent, and session.
2. Resolve references in the current question to produce a self-contained retrieval question.
3. Embed the resolved retrieval question.
4. Query the vector store and get similar memory IDs with distances.
5. Query the database by those IDs to get authoritative markdown location metadata.
6. Read the actual markdown memory entries and return markdown-backed content.

The current implementation already validates natural language search requests, embeds the question, searches sqlite-vec, and hydrates results from markdown. The refactor must make recent conversation context server-owned, add a two-stage coreference rewrite, and make the vector ID to database metadata to markdown entry chain explicit.

## Solution

Add server-side conversation history persistence keyed by `user_id`, `agent_id`, and required `session_id`. Successful QA turns append their final user question and assistant answer to this short-term history. Memory search uses the same required `session_id` to load the latest conversation turns, rewrite the search question into a self-contained question, embed that rewritten question, retrieve vector candidates, resolve candidate IDs through the database, and hydrate results from markdown.

This is a breaking API change: both `/api/v1/qa/stream` and `/api/v1/memories/search` require `session_id`. There is no default-session fallback.

The public search response remains centered on `results`. The API does not return the resolved question. The service logs rewrite metadata plus the original and resolved question by default, per product requirement.

## Commits

1. Add baseline tests for the current memory search behavior.

   Confirm that a standalone search request embeds its question, scopes vector search by user and agent, preserves vector result order, and returns markdown-backed content.

2. Add `session_id` request validation tests.

   Cover `session_id` required on `/api/v1/qa/stream` and `/api/v1/memories/search`. Reject empty, whitespace-padded, too-long, or invalid-character session IDs. Accept only `A-Z`, `a-z`, `0-9`, `_`, and `-`, with max length 128.

3. Update request structs and client contracts for `session_id`.

   Add `session_id` to QA stream and memory search requests. Update CLI client, integration tests, README examples, and any request helpers. CLI should generate one session ID per interactive run and generate a new one on `clear`.

4. Add a persistent conversation history package.

   Create `internal/conversation` with turn model, service, store interface, validation helpers, recent-turn query, and cleanup API. Keep this separate from `internal/session`, which manages running stream state, and separate from long-term `internal/memory`.

5. Add SQLite schema and storage for conversation turns.

   Add a `conversation_turns` table with an autoincrement primary key, `user_id`, `agent_id`, `session_id`, `question`, `answer`, `source`, and UTC `created_at`. Add indexes for `(user_id, agent_id, session_id, created_at DESC, id DESC)` and `created_at`.

6. Persist completed QA turns to conversation history.

   On normal `/qa/stream` completion, if both final user question and assistant answer are non-empty after trim, write the turn with `source="qa_stream"`. Do not write history on client disconnect, model error, interrupted stream, or empty answer.

7. Keep conversation history persistence independent.

   Attempt conversation history write before long-term memory save so the next turn can use the context even if memory save fails. If conversation history write fails, log the detailed error and emit a generic SSE warning such as `conversation history save failed`; do not block QA response, memory save, or markdown sync.

8. Add recent-turn retrieval behavior.

   Query recent turns strictly for the same `user_id + agent_id + session_id`. Do not cross session boundaries or backfill from other sessions. Query newest first using `created_at DESC, id DESC`, then reverse to chronological order before sending to rewrite prompts.

9. Add conversation retention configuration.

   Add `RECENT_SESSION_TURN_LIMIT`, default 5, configurable up to 20. Add `CONVERSATION_RETENTION_DAYS`, default 30 and must be greater than 0. Invalid config should fail startup.

10. Add nightly conversation cleanup scheduling.

   Add `CONVERSATION_CLEANUP_ENABLED`, default true, plus `CONVERSATION_CLEANUP_TIME`, default `03:00`, and `CONVERSATION_CLEANUP_TIMEZONE`, default `Asia/Shanghai`. The scheduler runs a global cleanup of all turns older than the retention cutoff. Store timestamps and compute delete cutoffs in UTC; use configured timezone only to compute the local schedule time.

11. Wire cleanup lifecycle through bootstrap/runtime.

   Start the cleanup scheduler from bootstrap/runtime after constructing the conversation service. Stop it from `Runtime.Close()`. Cleanup is idempotent and assumes single-instance deployment for now. Cleanup failure logs errors and does not affect `/health`.

12. Update model timeout defaults.

   Change the default `MODEL_TIMEOUT` from `30s` to `300s` and update config tests. Query rewrite uses the model timeout; do not add a separate `QUERY_REWRITE_TIMEOUT` in this refactor.

13. Add query rewrite output parsing with JSON repair.

   Add `github.com/kaptinlin/jsonrepair` as a dependency and wrap it behind a small internal adapter. For each model response: trim whitespace and code fences, try standard JSON decode, repair if decode fails, decode again, then validate fields. Log whether JSON repair was used, but do not log the repaired JSON blob.

14. Add the query rewrite abstraction.

   Introduce a rewriter interface that accepts current question plus recent conversation turns and returns the final retrieval question plus metadata. Use deterministic fakes in tests.

15. Implement stage 1 user-only rewrite.

   Stage 1 receives the current question and the recent 5 user questions only. Input budget is 20,000 characters. Always preserve the current question in full; if it alone exceeds 20,000 characters, skip rewrite and use the original question. Stage 1 model output must be JSON with `resolved_question`, `confidence`, and `needs_assistant_context`.

16. Implement stage 1 routing rules.

   If there is no conversation history, skip rewrite and use the original question. If stage 1 returns valid output with `confidence >= 0.75` and `needs_assistant_context=false`, use the stage 1 resolved question unless a pronoun heuristic forces stage 2. If `confidence < 0.75` or `needs_assistant_context=true`, enter stage 2. If output is invalid, retry stage 1 once; if still invalid, enter stage 2. Provider errors do not retry and fall back according to the final fallback rules.

17. Add pronoun heuristic routing.

   If the current question contains obvious coreference markers and the stage 1 resolved question is unchanged or nearly unchanged, force stage 2 even if stage 1 confidence is high. Start with common Chinese pronouns and references, plus English markers such as `this`, `that`, `it`, `they`, and `the previous`.

18. Implement stage 2 full-context rewrite.

   Stage 2 receives recent user questions and assistant answers plus the current question. Total input budget is 20,000 characters. Preserve current question first, keep newer turns over older turns, preserve user questions before assistant answers, and if an assistant answer is too long keep its beginning and end with an omission marker in the middle.

19. Implement stage 2 retry and fallback.

   Stage 2 uses the same JSON output contract. Invalid output retries once. Provider errors do not retry. A valid stage 2 result is used even if confidence is low. If stage 2 still fails, fall back to the original question and continue search.

20. Configure rewrite model calls.

   Use the existing `model.Client.Generate` and the existing model provider. Rewrite calls use `temperature=0.6`, `max_tokens=20000`, no tools, and `tool_choice=none` when supported. Tool calls or empty text responses are invalid output.

21. Add rewrite logging.

   Log `user_id`, `agent_id`, `session_id`, `stage1_status`, `stage2_status`, `final_query_source`, `stage1_confidence`, `history_turns_used`, `assistant_context_chars`, `json_repaired`, `fallback_used`, `original_question_chars`, `resolved_question_chars`, `original_question`, `resolved_question`, and latency. The original and resolved question are logged by default.

22. Add `internal/retrieval`.

   Create a retrieval service that orchestrates recent-turn loading, two-stage rewrite, embedding, vector search, database metadata lookup, and markdown hydration. `/memories/search` should call this service. `/qa/stream` should not call retrieval yet, but the service should be reusable for a later QA prompt-injection feature.

23. Split vector search from memory metadata lookup.

   Refactor interfaces so vector search returns only `memory_id` and `distance`. Add a batch database lookup by memory ID that returns metadata needed for markdown location: ID, user ID, agent ID, recorded time, and storage version. The sqlite backend can remain one concrete struct that implements the smaller interfaces.

24. Preserve vector result ordering.

   Database batch lookup should return a map by ID or otherwise avoid relying on SQL `IN` ordering. Final response order must follow vector similarity order.

25. Implement over-fetch and skip invalid candidates.

   Internal vector search should fetch `min(limit * 3, 100)` candidates. If a vector ID has no database row, skip it and log. If metadata points to missing or unreadable markdown, skip that candidate and log. Hydrate valid candidates in vector order and truncate to the requested limit. If every candidate is skipped, return `200` with empty `results`.

26. Move markdown read/write/parse logic out of HTTP.

   Extract markdown path rules, daily filename rules, marker parsing, reads, and writes into a non-HTTP internal package. Use the existing markdown tests as behavioral guards. HTTP and retrieval should consume the package instead of owning parser logic.

27. Keep long-term memory independent from session.

   Do not add `session_id` to memory rows or markdown files. Long-term memory search remains scoped by `user_id + agent_id`; `session_id` only selects recent conversation context for query rewriting. Saving memory continues to embed the original user question, not the resolved retrieval question.

28. Update `/memories/search` behavior.

   Search requires `session_id`, loads recent turns, rewrites if history exists, embeds the final retrieval question, performs over-fetched vector search, resolves IDs through database metadata, hydrates markdown content, skips invalid candidates, and returns the existing `results` shape.

29. Update documentation.

   Document `session_id` as required, CLI session behavior, conversation retention and cleanup config, two-stage coreference rewrite, fallback behavior, JSON repair dependency, default model timeout of 300 seconds, and the explicit vector ID to database metadata to markdown content flow.

30. Run focused tests after each layer.

   Run config tests after configuration changes, conversation tests after persistence and cleanup changes, model/rewrite tests after JSON parsing and prompt orchestration, memory/storage tests after interface splits, HTTP tests after request and handler changes, and CLI tests after client session changes.

31. Run final verification.

   Run `go test ./...`, inspect git status, and review the final diff for unrelated file changes.

## Decision Document

- `session_id` is required for both QA streaming and memory search. Missing `session_id` returns 400.

- `session_id` must be trim-clean, non-empty, max 128 characters, and contain only letters, digits, `_`, and `-`.

- This is a breaking API change. There is no default session fallback.

- CLI generates one session ID per interactive run and generates a new one on clear.

- Recent conversation context is server-side persisted, not supplied by search clients.

- Conversation history is keyed strictly by `user_id + agent_id + session_id`.

- Recent context defaults to the latest 5 turns and is configurable up to 20 turns.

- QA stream appends only the final completed turn: last user question plus assistant answer. It does not re-save all request messages.

- Search does not write conversation history.

- Conversation history write is independent from memory save. Failure logs and sends a generic SSE warning but does not block QA or memory persistence.

- Conversation history is stored in SQLite in a new `conversation_turns` table, separate from long-term memory.

- Conversation history includes `source`, initially fixed to `qa_stream`, and does not include arbitrary metadata JSON.

- Conversation timestamps are stored and filtered in UTC.

- Retention defaults to 30 days and is configurable.

- Cleanup runs globally at default `03:00` in `Asia/Shanghai`, can be disabled, and does not affect health status.

- Invalid cleanup config fails service startup.

- The implementation assumes single-instance cleanup. Cleanup SQL must be idempotent.

- Default `MODEL_TIMEOUT` changes to 300 seconds.

- Query rewrite reuses the existing non-streaming `model.Client.Generate`.

- Model client construction failure should fail service startup when HTTP model-backed functionality is required.

- Query rewrite uses `temperature=0.6`, `max_tokens=20000`, and disables tools.

- If there is no conversation history, rewrite is skipped and the original question is embedded.

- Stage 1 rewrite uses only recent user questions plus the current question.

- Stage 2 rewrite uses recent user questions and assistant answers plus the current question.

- Stage 1 and stage 2 input budgets are 20,000 characters.

- If the current question alone exceeds 20,000 characters, skip rewrite and embed the original question.

- Rewrite output must be JSON with `resolved_question`, `confidence`, and `needs_assistant_context`.

- JSON parsing uses `github.com/kaptinlin/jsonrepair` after standard JSON parsing fails.

- Stage 1 confidence threshold is 0.75.

- `needs_assistant_context=true` forces stage 2 even with high confidence.

- Obvious coreference markers plus an unchanged resolved question force stage 2.

- Invalid model output retries once per stage. Provider errors do not retry.

- A valid low-confidence stage 2 result is still used.

- If rewrite ultimately fails, search falls back to the original question and continues.

- Search response does not return the resolved question.

- Logs print both original and resolved question by default.

- The resolved question is used only for retrieval embedding.

- Long-term memory save continues to embed the original user question.

- Do not add `session_id` to memory rows or markdown files.

- Retrieval orchestration belongs in `internal/retrieval`.

- Short-term conversation history belongs in `internal/conversation`.

- Markdown read/write/parse logic should move out of the HTTP package into a reusable internal package.

- Vector search should return only ID and distance.

- Database metadata lookup by IDs should determine markdown location.

- Final result ordering follows vector similarity order.

- Internal over-fetch is `min(limit * 3, 100)`.

- Missing vector IDs, missing database metadata, missing markdown files, or missing markdown entries are skipped and logged.

- If all candidates are skipped, search returns 200 with empty results.

- Default external search limit remains 10.

- `/qa/stream` does not automatically inject retrieved memories into model prompts in this refactor, but the retrieval service must be reusable for that later feature.

## Testing Decisions

- Tests should assert external behavior rather than prompt wording: request validation, required `session_id`, rewrite routing, question text sent to embedding, vector search scope, result ordering, skipped-candidate behavior, SSE warnings, and returned markdown content.

- Query rewrite tests should use fake model clients and deterministic fake outputs.

- JSON repair tests should cover standard JSON, code-fenced JSON, repairable malformed JSON, unrecoverable JSON, empty `resolved_question`, and oversized `resolved_question`.

- Stage routing tests should cover no history, high-confidence stage 1, low-confidence stage 1 to stage 2, `needs_assistant_context`, pronoun heuristic forcing stage 2, invalid-output retry, provider-error fallback, and valid low-confidence stage 2.

- Conversation tests should cover append, trim behavior, same-session recent turn loading, cross-session isolation, chronological prompt order, retention cleanup, global cleanup, disabled scheduler, invalid config, and cleanup failure logging.

- Storage tests should cover vector search returning only IDs/distances and batch metadata lookup preserving final ordering through the retrieval layer.

- Markdown hydration tests should use real temporary markdown files to cover daily filenames, marker parsing, missing files, missing entries, skipped candidates, and empty final results.

- CLI tests should cover generated session ID reuse and reset on clear.

- Existing HTTP, memory, storage, model, config, and session tests are useful prior art and should remain green.

## Out of Scope

- Automatically injecting memory retrieval into `/qa/stream` prompt construction.

- Re-embedding existing stored memories.

- Rebuilding existing markdown files.

- Changing the public search result schema beyond requiring `session_id` in the request.

- Adding `session_id` to long-term memory rows or markdown files.

- Returning the resolved question to API clients.

- Full-text ranking inside markdown.

- Distributed cleanup coordination or leader election.

- Metrics endpoints or health response expansion for cleanup status.

## Further Notes

This refactor deliberately separates short-term conversation context from long-term memory. Conversation history exists to make the current retrieval question self-contained. Long-term memory remains cross-session for the same user and agent.

The biggest implementation risk is scope creep around model prompt design and QA prompt injection. Keep this refactor focused on retrieval correctness and reusable service boundaries. The retrieval service should make later QA integration straightforward, but `/qa/stream` should not call it yet.
