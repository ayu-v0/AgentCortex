package memory

import (
	"fmt"
	"math"
)

const EmbeddingDimensions = 4

const (
	defaultSearchLimit = 5
	MaxSearchLimit     = 100
)

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
