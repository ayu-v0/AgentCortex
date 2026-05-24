package embedding

import (
	"context"
	"fmt"
	"math"
)

type Embedder interface {
	Embed(ctx context.Context, input Input) (Vector, error)
}

type Input struct {
	Text string
}

type Vector []float32

func (v Vector) Validate(dimensions int) error {
	if dimensions <= 0 {
		return fmt.Errorf("%w: dimensions must be positive", ErrInvalidConfig)
	}
	if len(v) != dimensions {
		return fmt.Errorf("%w: expected %d values, got %d", ErrInvalidVector, dimensions, len(v))
	}
	for i, value := range v {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("%w: index %d", ErrInvalidVectorValue, i)
		}
	}
	return nil
}
