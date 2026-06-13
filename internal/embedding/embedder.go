package embedding

import (
	"context"
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
		return ErrInvalidConfig
	}
	if len(v) != dimensions {
		return ErrInvalidVector
	}
	for _, value := range v {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return ErrInvalidVectorValue
		}
	}
	return nil
}
