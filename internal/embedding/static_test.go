package embedding

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestStaticEmbedderRejectsEmptyInput(t *testing.T) {
	embedder, err := NewStaticEmbedder(4)
	if err != nil {
		t.Fatalf("new static embedder: %v", err)
	}

	_, err = embedder.Embed(context.Background(), Input{Text: " "})
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("expected ErrEmptyInput, got %v", err)
	}
}

func TestStaticEmbedderReturnsDeterministicVector(t *testing.T) {
	embedder, err := NewStaticEmbedder(4)
	if err != nil {
		t.Fatalf("new static embedder: %v", err)
	}

	first, err := embedder.Embed(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatalf("embed first: %v", err)
	}
	second, err := embedder.Embed(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatalf("embed second: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic vector, got %v and %v", first, second)
	}
	if len(first) != 4 {
		t.Fatalf("expected 4 dimensions, got %d", len(first))
	}
}

func TestStaticEmbedderReturnsDifferentVectorsForDifferentInputs(t *testing.T) {
	embedder, err := NewStaticEmbedder(4)
	if err != nil {
		t.Fatalf("new static embedder: %v", err)
	}

	first, err := embedder.Embed(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatalf("embed first: %v", err)
	}
	second, err := embedder.Embed(context.Background(), Input{Text: "world"})
	if err != nil {
		t.Fatalf("embed second: %v", err)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatalf("expected different vectors, got %v", first)
	}
}

func TestStaticEmbedderReturnsContextError(t *testing.T) {
	embedder, err := NewStaticEmbedder(4)
	if err != nil {
		t.Fatalf("new static embedder: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = embedder.Embed(ctx, Input{Text: "hello"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestVectorValidate(t *testing.T) {
	if err := (Vector{0, 1}).Validate(4); !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("expected ErrInvalidVector, got %v", err)
	}
	if err := (Vector{0, float32(math.NaN())}).Validate(2); !errors.Is(err, ErrInvalidVectorValue) {
		t.Fatalf("expected ErrInvalidVectorValue for NaN, got %v", err)
	}
	if err := (Vector{0, float32(math.Inf(1))}).Validate(2); !errors.Is(err, ErrInvalidVectorValue) {
		t.Fatalf("expected ErrInvalidVectorValue for Inf, got %v", err)
	}
	if err := (Vector{0, 1}).Validate(2); err != nil {
		t.Fatalf("validate vector: %v", err)
	}
}
