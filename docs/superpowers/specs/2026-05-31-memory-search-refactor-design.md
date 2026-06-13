# Memory Search Refactor Design

## Goal

Refactor the memory search API so callers submit natural language search input instead of a precomputed embedding. The search request accepts `agent_id`, `user_id`, `question`, and optional `limit`. When omitted, `limit` defaults to 10.

The response keeps the current shape:

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

Each result's `content` should come from the markdown memory file rather than directly from the database row.

## Current State

`POST /api/v1/memories/search` currently accepts `agent_id`, `embedding`, and `limit`. The handler forwards the embedding to `memory.Service.Search`, which validates the vector and asks the sqlite-vec backend for nearest memory rows by `agent_id`.

Memory creation already writes structured markdown files under the configured markdown directory. Filenames are derived from sanitized `user_id` and `agent_id` values:

```text
<user_id>_<agent_id>_Memory.md
```

The backend stores `user_id`, `agent_id`, `question`, `answer`, `content`, and optional embedding per memory row, but vector search currently filters only by `agent_id`.

## API Contract

Request:

```json
{
  "agent_id": "agent-1",
  "user_id": "user-1",
  "question": "What does the user prefer?",
  "limit": 10
}
```

Rules:

- `agent_id`, `user_id`, and `question` are required.
- `limit` is optional and defaults to 10.
- `limit` keeps the existing maximum of 100.
- The previous `embedding` request field is no longer required for search.

## Search Flow

1. Trim and validate the request fields.
2. Embed `question` using the configured embedding provider.
3. Search the vector backend with the generated embedding.
4. Filter vector candidates by both `agent_id` and `user_id`.
5. Resolve the markdown filename using the same sanitization rules used by memory creation.
6. Read the markdown memory file.
7. For each vector candidate, find the matching markdown entry by `MemoryID`.
8. Return results in vector distance order, preserving `id` and `distance`, and replacing `content` with the matched markdown entry content.

If a candidate memory ID is not found in markdown, the implementation may fall back to the database content for that result. If the markdown file is missing or unreadable, the endpoint should return an internal server error and avoid leaking local paths.

## Components

### HTTP Request Model

`searchMemoryRequest` changes from `agent_id + embedding + limit` to:

- `agent_id`
- `user_id`
- `question`
- `limit`

### Embedding Dependency

The HTTP server needs access to an `embedding.Embedder`. The app startup should construct the provider from config and pass it into the transport layer. Tests can use a small fake embedder.

### Backend Query

The backend search method should accept `user_id` in addition to `agent_id`, so sqlite-vec can restrict candidates to the user's markdown memory file. This prevents a user's query from retrieving another user's memory under the same agent.

### Markdown Search

Markdown lookup should be a focused helper that:

- uses the existing markdown filename sanitization behavior,
- reads one resolved markdown file,
- splits entries on the existing `---` separator,
- extracts the entry whose `MemoryID:` matches the vector candidate ID.

This keeps the markdown read path deterministic and avoids broad filesystem scans.

## Error Handling

- Invalid JSON or missing required fields returns 400.
- Empty `question` should return 400 through embedding input validation or explicit request validation.
- Invalid embedding provider output returns 400 when it is a vector validation issue.
- Provider failures, backend failures, and markdown filesystem failures return 500 with the existing generic public message.

## Testing

Use test-first implementation.

Key tests:

- Search request rejects missing `question`.
- Search request defaults missing `limit` to 10.
- Search embeds the question and passes the generated vector to the backend.
- Backend search filters by `agent_id` and `user_id`.
- Search response content is read from the matching markdown `MemoryID` entry.
- Missing markdown file returns a masked internal server error.
- Existing limit maximum behavior remains intact.

## Out of Scope

- Changing the save-memory API to auto-embed on create.
- Rebuilding existing markdown files.
- Full-text ranking inside markdown.
- Returning a new response schema.
