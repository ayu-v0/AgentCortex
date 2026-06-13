# Memory Search Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor memory search so callers send `agent_id`, `user_id`, `question`, and optional `limit`, while search embeds the question, retrieves vector candidates, and returns markdown-backed result content.

**Architecture:** Keep vector storage in the existing `memory.Backend` boundary, but add `user_id` to backend search so sqlite-vec filters candidates to one user's memories. Add an `embedding.Embedder` dependency to the HTTP server and a focused markdown helper in the HTTP package to resolve the matching markdown entry by `MemoryID`.

**Tech Stack:** Go, Gin, sqlite-vec, existing `internal/embedding` providers, standard `os`/`strings` helpers, `go test`.

---

## File Structure

- Modify `internal/memory/service.go`: add `userID` to `Backend.Search` and `Service.Search`.
- Modify `internal/memory/query.go`: add `userID` to query search and keep vector validation and limit normalization.
- Modify `internal/memory/query_test.go`: prove default search limit is 10 and user ID is forwarded.
- Modify `internal/memory/validation.go`: change default search limit from 5 to 10.
- Modify `internal/storage/sqlitevec/backend.go`: update backend method signature.
- Modify `internal/storage/sqlitevec/query.go`: filter vector candidates by `agent_id` and `user_id`.
- Modify `internal/transport/http/request.go`: replace search request embedding with `user_id` and `question`.
- Modify `internal/transport/http/server.go`: accept an embedder dependency while keeping test construction simple.
- Modify `internal/transport/http/handler.go`: embed the question, call memory search, read markdown, and replace each result's content.
- Modify `internal/transport/http/errors.go`: map embedding input/vector validation errors to 400 and provider failures to 500.
- Modify `internal/transport/http/server_test.go`: cover request validation, default limit, embedding call, user filtering, markdown-backed content, and missing markdown errors.
- Modify `internal/app/app.go`: construct an embedding provider and pass it to the HTTP server.
- Modify `internal/config/config.go`: expose minimal embedding provider config.
- Modify `README.md`: update search request example.

## Task 1: Memory Service Search Signature and Default Limit

**Files:**
- Modify: `internal/memory/service.go`
- Modify: `internal/memory/query.go`
- Modify: `internal/memory/query_test.go`
- Modify: `internal/memory/service_test.go`
- Modify: `internal/memory/validation.go`

- [ ] **Step 1: Write the failing service/query test**

Add assertions to `internal/memory/query_test.go` so the fake backend receives `userID` and the default limit is 10:

```go
func TestQueryNormalizesLimitAndForwardsUser(t *testing.T) {
	backend := &fakeBackend{}
	query := newQuery(backend)

	_, err := query.Search("agent-1", "user-1", validEmbedding(), MaxSearchLimit+1)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchAgentID != "agent-1" {
		t.Fatalf("expected search agent ID agent-1, got %q", backend.searchAgentID)
	}
	if backend.searchUserID != "user-1" {
		t.Fatalf("expected search user ID user-1, got %q", backend.searchUserID)
	}
	if backend.searchLimit != MaxSearchLimit {
		t.Fatalf("expected search limit %d, got %d", MaxSearchLimit, backend.searchLimit)
	}

	_, err = query.Search("agent-1", "user-1", validEmbedding(), 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if backend.searchLimit != 10 {
		t.Fatalf("expected default search limit 10, got %d", backend.searchLimit)
	}
}
```

Update `fakeBackend` in `internal/memory/service_test.go`:

```go
type fakeBackend struct {
	closed        bool
	savedMemory   Memory
	searchAgentID string
	searchUserID  string
	searchLimit   int
}

func (b *fakeBackend) Search(agentID string, userID string, embedding []float32, limit int) ([]SearchResult, error) {
	b.searchAgentID = agentID
	b.searchUserID = userID
	b.searchLimit = limit
	return nil, nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/memory`

Expected: compile failure because `Query.Search`, `Service.Search`, and `Backend.Search` still accept `agentID, embedding, limit`.

- [ ] **Step 3: Implement minimal service/query changes**

Change `internal/memory/service.go`:

```go
type Backend interface {
	Close() error
	Save(memory Memory) error
	Search(agentID string, userID string, embedding []float32, limit int) ([]SearchResult, error)
}

func (s *Service) Search(agentID string, userID string, embedding []float32, limit int) ([]SearchResult, error) {
	return s.query.Search(agentID, userID, embedding, limit)
}
```

Change `internal/memory/query.go`:

```go
func (q *Query) Search(agentID string, userID string, embedding []float32, limit int) ([]SearchResult, error) {
	if err := validateEmbedding(embedding); err != nil {
		return nil, err
	}

	return q.backend.Search(agentID, userID, embedding, normalizeSearchLimit(limit))
}
```

Change `internal/memory/validation.go`:

```go
const (
	defaultSearchLimit = 10
	MaxSearchLimit     = 100
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/memory`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/memory/service.go internal/memory/query.go internal/memory/query_test.go internal/memory/service_test.go internal/memory/validation.go
git commit -m "Refine memory search query contract"
```

## Task 2: sqlite-vec User Filter

**Files:**
- Modify: `internal/storage/sqlitevec/backend.go`
- Modify: `internal/storage/sqlitevec/query.go`

- [ ] **Step 1: Write the failing backend contract compile check**

No new test file is required for this step because Task 1 changes the `memory.Backend` interface. Running sqlite-vec tests now should fail until the method signatures are updated.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/sqlitevec`

Expected: compile failure that `*Backend` does not implement `memory.Backend` or that `Search` is called with the wrong arguments.

- [ ] **Step 3: Implement sqlite-vec user filtering**

Change `internal/storage/sqlitevec/backend.go`:

```go
func (b *Backend) Search(agentID string, userID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	return b.query.Search(agentID, userID, embedding, limit)
}
```

Change `internal/storage/sqlitevec/query.go`:

```go
func (q *Query) Search(agentID string, userID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	rows, err := q.db.Query(`
		SELECT
			m.id,
			m.content,
			v.distance
		FROM memory_vectors v
		JOIN memories m ON m.id = v.memory_id
		WHERE v.embedding MATCH ?
		  AND k = ?
		  AND m.agent_id = ?
		  AND m.user_id = ?
		ORDER BY v.distance
	`, float32VectorToBytes(embedding), limit, agentID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]memory.SearchResult, 0)
	for rows.Next() {
		var result memory.SearchResult
		if err := rows.Scan(&result.ID, &result.Content, &result.Distance); err != nil {
			return nil, err
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return results, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/sqlitevec`

Expected: PASS or existing sqlite-vec extension skip behavior, with no compile errors.

- [ ] **Step 5: Commit**

```bash
git add internal/storage/sqlitevec/backend.go internal/storage/sqlitevec/query.go
git commit -m "Filter vector memory search by user"
```

## Task 3: HTTP Search Request, Embedding, and Markdown Content

**Files:**
- Modify: `internal/transport/http/request.go`
- Modify: `internal/transport/http/server.go`
- Modify: `internal/transport/http/handler.go`
- Modify: `internal/transport/http/errors.go`
- Modify: `internal/transport/http/server_test.go`

- [ ] **Step 1: Write failing HTTP tests**

In `internal/transport/http/server_test.go`, add a fake embedder and update the recording backend:

```go
type fakeEmbedder struct {
	text   string
	vector []float32
	err    error
}

func (e *fakeEmbedder) Embed(ctx context.Context, input embedding.Input) (embedding.Vector, error) {
	e.text = input.Text
	if e.err != nil {
		return nil, e.err
	}
	return embedding.Vector(e.vector), nil
}
```

Update imports with:

```go
import (
	"context"
	...
	"github.com/ayu-v0/agent-cortex/internal/embedding"
)
```

Update `recordingBackend`:

```go
type recordingBackend struct {
	saved           memory.Memory
	searchAgentID   string
	searchUserID    string
	searchEmbedding []float32
	searchLimit     int
	results         []memory.SearchResult
}

func (b *recordingBackend) Search(agentID string, userID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	b.searchAgentID = agentID
	b.searchUserID = userID
	b.searchEmbedding = append([]float32(nil), embedding...)
	b.searchLimit = limit
	if b.results != nil {
		return b.results, nil
	}
	return []memory.SearchResult{{ID: "memory-1", Content: "database content", Distance: 0.25}}, nil
}
```

Add or replace search tests:

```go
func TestSearchMemoryRejectsMissingQuestion(t *testing.T) {
	server := newTestServer(t, &recordingBackend{})

	body := `{"agent_id":"agent-1","user_id":"user-1","limit":10}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestSearchMemoryEmbedsQuestionAndReturnsMarkdownContent(t *testing.T) {
	backend := &recordingBackend{}
	markdownDir := t.TempDir()
	markdownPath := filepath.Join(markdownDir, "user-1_agent-1_Memory.md")
	err := os.WriteFile(markdownPath, []byte(`# Memory

UserID: user-1
AgentID: agent-1

## Memory

MemoryID: memory-1

## Question

stored question

## Answer

stored answer
`), 0o644)
	if err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	embedder := &fakeEmbedder{vector: []float32{0, 1, 2, 3}}
	server := newTestServerWithMarkdownDirAndEmbedder(t, backend, markdownDir, embedder)

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"what is stored?"}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusOK {
		t.Fatalf("expected status 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}
	if embedder.text != "what is stored?" {
		t.Fatalf("expected embedder question, got %q", embedder.text)
	}
	if backend.searchAgentID != "agent-1" {
		t.Fatalf("expected agent filter agent-1, got %q", backend.searchAgentID)
	}
	if backend.searchUserID != "user-1" {
		t.Fatalf("expected user filter user-1, got %q", backend.searchUserID)
	}
	if backend.searchLimit != 10 {
		t.Fatalf("expected default limit 10, got %d", backend.searchLimit)
	}
	if len(backend.searchEmbedding) != 4 {
		t.Fatalf("expected generated embedding to be passed to backend, got %v", backend.searchEmbedding)
	}
	if !strings.Contains(recorder.Body.String(), `"content":"## Memory\n\nMemoryID: memory-1`) {
		t.Fatalf("expected markdown content in response, got %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), "database content") {
		t.Fatalf("expected database content to be replaced, got %s", recorder.Body.String())
	}
}

func TestSearchMemoryReturnsMaskedErrorForMissingMarkdown(t *testing.T) {
	backend := &recordingBackend{}
	embedder := &fakeEmbedder{vector: []float32{0, 1, 2, 3}}
	server := newTestServerWithMarkdownDirAndEmbedder(t, backend, t.TempDir(), embedder)

	body := `{"agent_id":"agent-1","user_id":"user-1","question":"what is stored?","limit":10}`
	recorder := performRequest(server, "POST", "/api/v1/memories/search", body)

	if recorder.Code != stdhttp.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
	if strings.Contains(recorder.Body.String(), "Memory.md") {
		t.Fatalf("response leaked markdown path: %s", recorder.Body.String())
	}
}
```

Update test helpers:

```go
func newTestServerWithMarkdownDir(t *testing.T, backend memory.Backend, markdownDir string) *Server {
	t.Helper()
	return newTestServerWithMarkdownDirAndEmbedder(t, backend, markdownDir, &fakeEmbedder{vector: validEmbedding()})
}

func newTestServerWithMarkdownDirAndEmbedder(t *testing.T, backend memory.Backend, markdownDir string, embedder embedding.Embedder) *Server {
	t.Helper()

	service, err := memory.NewService(backend)
	if err != nil {
		t.Fatalf("new service: %v", err)
	}
	return newServer(service, embedder, markdownDir)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/transport/http`

Expected: compile failures for missing `embedding` imports, old request fields, old backend signature, and `newServer` signature.

- [ ] **Step 3: Implement minimal HTTP changes**

Change `internal/transport/http/request.go`:

```go
type searchMemoryRequest struct {
	AgentID  string `json:"agent_id" binding:"required"`
	UserID   string `json:"user_id" binding:"required"`
	Question string `json:"question" binding:"required"`
	Limit    int    `json:"limit" binding:"omitempty,min=1,max=100"`
}
```

Change `internal/transport/http/server.go`:

```go
func NewServer(service *memory.Service, embedder embedding.Embedder) *Server {
	return newServer(service, embedder, defaultMemoryMarkdownDir)
}

func newServer(service *memory.Service, embedder embedding.Embedder, memoryMarkdownDir string) *Server {
	handlers := newHandlers(service, embedder, memoryMarkdownDir)
	router := newRouter(handlers)
	return &Server{router: router}
}
```

Change `internal/transport/http/handler.go`:

```go
type handlers struct {
	memoryService     *memory.Service
	embedder          embedding.Embedder
	memoryMarkdownDir string
	memoryMarkdownMu  sync.Mutex
}

func newHandlers(service *memory.Service, embedder embedding.Embedder, memoryMarkdownDir string) *handlers {
	memoryMarkdownDir = strings.TrimSpace(memoryMarkdownDir)
	if memoryMarkdownDir == "" {
		memoryMarkdownDir = defaultMemoryMarkdownDir
	}
	return &handlers{
		memoryService:     service,
		embedder:          embedder,
		memoryMarkdownDir: memoryMarkdownDir,
	}
}
```

Replace search handler:

```go
func (h *handlers) searchMemory(c *gin.Context) {
	var req searchMemoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeErrorJSON(c, stdhttp.StatusBadRequest, err.Error())
		return
	}

	vector, err := h.embedder.Embed(c.Request.Context(), embedding.Input{Text: req.Question})
	if err != nil {
		writeHTTPError(c, err)
		return
	}

	results, err := h.memoryService.Search(req.AgentID, req.UserID, []float32(vector), req.Limit)
	if err != nil {
		writeHTTPError(c, err)
		return
	}

	results, err = h.populateMarkdownSearchContent(req.UserID, req.AgentID, results)
	if err != nil {
		log.Printf("memory markdown search error: %v", err)
		writeErrorJSON(c, stdhttp.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(c, stdhttp.StatusOK, searchMemoryResponse{Results: results})
}
```

Add markdown extraction helpers in `handler.go`:

```go
func (h *handlers) populateMarkdownSearchContent(userID, agentID string, results []memory.SearchResult) ([]memory.SearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	filename, err := memoryMarkdownFilename(userID, agentID)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(filepath.Join(h.memoryMarkdownDir, filename))
	if err != nil {
		return nil, errors.Join(ErrMemoryMarkdown, err)
	}

	entries := memoryMarkdownEntriesByID(string(content))
	for i := range results {
		if entry, ok := entries[results[i].ID]; ok {
			results[i].Content = entry
		}
	}
	return results, nil
}

func memoryMarkdownEntriesByID(markdown string) map[string]string {
	entries := make(map[string]string)
	for _, part := range strings.Split(markdown, "\n---\n") {
		entry := strings.TrimSpace(part)
		if !strings.Contains(entry, "MemoryID:") {
			continue
		}
		for _, line := range strings.Split(entry, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "MemoryID:") {
				id := strings.TrimSpace(strings.TrimPrefix(line, "MemoryID:"))
				if id != "" {
					entries[id] = entry
				}
				break
			}
		}
	}
	return entries
}
```

Add imports to `handler.go`:

```go
import (
	"path/filepath"
	...
	"github.com/ayu-v0/agent-cortex/internal/embedding"
)
```

Change `internal/transport/http/errors.go`:

```go
case errors.Is(err, memory.ErrInvalidEmbedding),
	errors.Is(err, memory.ErrInvalidEmbeddingValue),
	errors.Is(err, embedding.ErrEmptyInput),
	errors.Is(err, embedding.ErrInvalidVector),
	errors.Is(err, embedding.ErrInvalidVectorValue):
	return stdhttp.StatusBadRequest
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/transport/http`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/transport/http/request.go internal/transport/http/server.go internal/transport/http/handler.go internal/transport/http/errors.go internal/transport/http/server_test.go
git commit -m "Search memories from question and markdown"
```

## Task 4: App Wiring and Documentation

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/app/app.go`
- Modify: `README.md`

- [ ] **Step 1: Write the failing app/config test or compile check**

No dedicated app test exists. The red check is a full compile after Task 3, because `transporthttp.NewServer` now requires an embedder.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./...`

Expected: compile failure in `internal/app/app.go` because `NewServer` is called without an embedder.

- [ ] **Step 3: Implement app embedding wiring**

Change `internal/config/config.go`:

```go
const (
	defaultAddr              = ":8080"
	defaultDatabasePath      = "agent_memory.db"
	defaultStorageBackend    = "sqlitevec"
	defaultEmbeddingProvider = "static"
	defaultEmbeddingEndpoint = "http://127.0.0.1:8081"
)

type Config struct {
	Addr              string
	DatabasePath      string
	StorageBackend    string
	EmbeddingProvider string
	EmbeddingEndpoint string
}

func FromEnv() Config {
	return Config{
		Addr:              getenv("ADDR", defaultAddr),
		DatabasePath:      getenv("DATABASE_PATH", defaultDatabasePath),
		StorageBackend:    getenv("STORAGE_BACKEND", defaultStorageBackend),
		EmbeddingProvider: getenv("EMBEDDING_PROVIDER", defaultEmbeddingProvider),
		EmbeddingEndpoint: getenv("EMBEDDING_ENDPOINT", defaultEmbeddingEndpoint),
	}
}
```

Change `internal/app/app.go`:

```go
func Run() error {
	cfg := config.FromEnv()

	backend, err := openMemoryBackend(cfg)
	if err != nil {
		return err
	}

	memoryService, err := memory.NewService(backend)
	if err != nil {
		return err
	}
	defer memoryService.Close()

	embedder, err := embedding.NewProvider(embedding.Config{
		Provider:   embedding.ProviderType(cfg.EmbeddingProvider),
		Dimensions: memory.EmbeddingDimensions,
		Endpoint:   cfg.EmbeddingEndpoint,
	})
	if err != nil {
		return err
	}
	if closer, ok := embedder.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	server := transporthttp.NewServer(memoryService, embedder)
	log.Printf("agent-cortex HTTP server listening on %s", cfg.Addr)
	if err := server.Run(cfg.Addr); err != nil {
		return err
	}

	return nil
}
```

Add import:

```go
import "github.com/ayu-v0/agent-cortex/internal/embedding"
```

Update `README.md` search example:

```json
{
  "agent_id": "agent_001",
  "user_id": "user_001",
  "question": "What does the user like?",
  "limit": 10
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/app/app.go README.md
git commit -m "Wire embedding provider into search API"
```

## Task 5: Final Verification

**Files:**
- Verify all changed files.

- [ ] **Step 1: Run full test suite**

Run: `go test ./...`

Expected: PASS.

- [ ] **Step 2: Review git status**

Run: `git status --short`

Expected: only pre-existing unrelated untracked files remain, such as `.idea/`, `.workplace/`, and `docs/architecture.md`.

- [ ] **Step 3: Inspect final diff**

Run: `git log --oneline -5` and `git show --stat --oneline HEAD`

Expected: recent commits match the task commits and do not include unrelated files.

## Self-Review

- Spec coverage: API request shape, default limit 10, question embedding, vector search, user filtering, markdown content replacement, masked markdown error, and unchanged response shape are all covered.
- Marker scan: no unresolved markers or unspecified implementation steps remain.
- Type consistency: `Search(agentID, userID, embedding, limit)` is used consistently across memory, sqlite-vec, HTTP test fakes, and app wiring.
