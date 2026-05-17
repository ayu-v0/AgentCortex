# AgentCortex
AgentCortex is a Go-native memory infrastructure for AI agents, providing persistent storage, semantic recall, and structured memory management.

## HTTP API

Run the Gin server:

```powershell
go run .\cmd\agent-cortex
```

The service listens on `:8080` by default. Set `ADDR` or `DATABASE_PATH` to override the listen address or SQLite database path.

### Health

```http
GET /health
```

### Save Memory

```http
POST /api/v1/memories
Content-Type: application/json

{
  "id": "mem_001",
  "agent_id": "agent_001",
  "content": "User likes building agent memory in Go.",
  "embedding": [0.1, 0.2, 0.3, 0.4]
}
```

### Search Memories

```http
POST /api/v1/memories/search
Content-Type: application/json

{
  "agent_id": "agent_001",
  "embedding": [0.1, 0.2, 0.3, 0.4],
  "limit": 5
}
```
