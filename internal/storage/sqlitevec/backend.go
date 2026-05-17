package sqlitevec

import (
	"database/sql"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	_ "github.com/mattn/go-sqlite3"
)

type Backend struct {
	db          *sql.DB
	query       *Query
	memoryStore *MemoryStore
}

var _ memory.Backend = (*Backend)(nil)

func Open(path string) (*Backend, error) {
	if err := registerSQLiteVec(); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	backend := newBackend(db)
	if err := backend.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}

	return backend, nil
}

func (b *Backend) Close() error {
	return b.db.Close()
}

func newBackend(db *sql.DB) *Backend {
	return &Backend{
		db:          db,
		query:       newQuery(db),
		memoryStore: newMemoryStore(db),
	}
}

func (b *Backend) Save(item memory.Memory) error {
	return b.memoryStore.Save(item)
}

func (b *Backend) Search(agentID string, embedding []float32, limit int) ([]memory.SearchResult, error) {
	return b.query.Search(agentID, embedding, limit)
}
