# AgentCortex
AgentCortex is a Go-native memory infrastructure for AI agents, providing persistent storage, semantic recall, and structured memory management.

## HTTP API

Run the Gin server:

```powershell
go run .\cmd\agent-cortex
```

You can also load a YAML file:

```powershell
go run .\cmd\agent-cortex --config .\config.yml
```

The service listens on `:8080` by default. Set `ADDR` or `DATABASE_PATH` to override the listen address or SQLite database path.
Set `EMBEDDING_PROVIDER` or `EMBEDDING_ENDPOINT` to override the search embedding provider or endpoint. Environment variables override values loaded from the YAML config file.

Example `config.yml`:

```yaml
addr: ":8080"
database_path: "agent_memory.db"
storage_backend: "sqlitevec"
embedding_provider: "static"
embedding_endpoint: "http://127.0.0.1:8081"
model_provider: "openai-compatible"
model_endpoint: ""
model_api_key: ""
model_name: ""
model_timeout: "30s"
```

## CLI

Run the interactive CLI:

```powershell
go run .\cmd\agent-cortex-cli --config .\config.yml
```

The CLI is a separate entrypoint from the HTTP server and requires model configuration plus memory partition keys:

```yaml
model_provider: "openai-compatible"
model_endpoint: "http://127.0.0.1:8082"
model_api_key: ""
model_name: "test-model"
model_timeout: "30s"
agent_id: "agent-cortex-cli"
user_id: "default-user"
system_prompt: "You are a concise assistant."
cli_stream: true
```

Each successful Q/A turn is automatically saved as memory. Environment variables still override YAML values.

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
  "user_id": "user_001",
  "question": "What does the user like?",
  "answer": "The user likes building agent memory in Go.",
  "embedding": [0.1, 0.2, 0.3, 0.4]
}
```

### Search Memories

```http
POST /api/v1/memories/search
Content-Type: application/json

{
  "agent_id": "agent_001",
  "user_id": "user_001",
  "question": "What does the user like?",
  "limit": 10
}
```
