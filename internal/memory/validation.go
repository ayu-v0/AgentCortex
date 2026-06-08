package memory

import "math"

const EmbeddingDimensions = 4

const (
	defaultSearchLimit = 10
	MaxSearchLimit     = 100
)

func validateEmbedding(embedding []float32) error {
	if len(embedding) != EmbeddingDimensions {
		return ErrInvalidEmbedding
	}
	for _, value := range embedding {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return ErrInvalidEmbeddingValue
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
