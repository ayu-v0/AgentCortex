package memory

import "errors"

var (
	ErrInvalidEmbedding      = errors.New("embedding must contain exactly 4 values")
	ErrInvalidEmbeddingValue = errors.New("embedding values must be finite")
	ErrNilBackend            = errors.New("memory backend is nil")
	ErrUnsupportedBackend    = errors.New("unsupported memory storage backend")
)
