package memory

import (
	"fmt"
	"math"
	"reflect"
)

const EmbeddingDimensions = 4

const (
	defaultSearchLimit = 5
	MaxSearchLimit     = 100
)

type Backend interface {
	Close() error
	Save(memory Memory) error
	Search(agentID string, embedding []float32, limit int) ([]SearchResult, error)
}

type Store struct {
	backend Backend
}

type Memory struct {
	ID        string    `json:"id"`
	AgentID   string    `json:"agent_id"`
	Content   string    `json:"content"`
	Embedding []float32 `json:"embedding,omitempty"`
}

type SearchResult struct {
	ID       string  `json:"id"`
	Content  string  `json:"content"`
	Distance float64 `json:"distance"`
}

func NewStore(backend Backend) (*Store, error) {
	if isNilBackend(backend) {
		return nil, ErrNilBackend
	}

	return &Store{backend: backend}, nil
}

func (s *Store) Close() error {
	return s.backend.Close()
}

func (s *Store) Save(memory Memory) error {
	if err := validateEmbedding(memory.Embedding); err != nil {
		return err
	}

	return s.backend.Save(memory)
}

func (s *Store) Search(agentID string, embedding []float32, limit int) ([]SearchResult, error) {
	if err := validateEmbedding(embedding); err != nil {
		return nil, err
	}

	return s.backend.Search(agentID, embedding, normalizeSearchLimit(limit))
}

func validateEmbedding(embedding []float32) error {
	if len(embedding) != EmbeddingDimensions {
		return fmt.Errorf("%w: got %d", ErrInvalidEmbedding, len(embedding))
	}
	for i, value := range embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("%w: index %d", ErrInvalidEmbeddingValue, i)
		}
	}
	return nil
}

func normalizeSearchLimit(limit int) int {
	if limit <= 0 {
		return defaultSearchLimit
	}
	if limit > MaxSearchLimit {
		return MaxSearchLimit
	}
	return limit
}

func isNilBackend(backend Backend) bool {
	if backend == nil {
		return true
	}

	value := reflect.ValueOf(backend)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
