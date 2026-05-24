package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

type StaticEmbedder struct {
	dimensions int
}

var _ Embedder = (*StaticEmbedder)(nil)

func NewStaticEmbedder(dimensions int) (*StaticEmbedder, error) {
	if dimensions <= 0 {
		return nil, fmt.Errorf("%w: dimensions must be positive", ErrInvalidConfig)
	}
	return &StaticEmbedder{dimensions: dimensions}, nil
}

func (e *StaticEmbedder) Embed(ctx context.Context, input Input) (Vector, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, ErrEmptyInput
	}

	vector := make(Vector, e.dimensions)
	var norm float64
	for i := range vector {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", text, i)))
		raw := binary.BigEndian.Uint32(digest[:4])
		value := (float64(raw)/float64(math.MaxUint32))*2 - 1
		vector[i] = float32(value)
		norm += value * value
	}

	if norm > 0 {
		scale := float32(math.Sqrt(norm))
		for i := range vector {
			vector[i] /= scale
		}
	}

	return vector, vector.Validate(e.dimensions)
}
