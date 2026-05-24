package embedding

import (
	"errors"
	"testing"
)

func TestNewProviderDefaultsToStatic(t *testing.T) {
	embedder, err := NewProvider(Config{Dimensions: 4})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, ok := embedder.(*StaticEmbedder); !ok {
		t.Fatalf("expected StaticEmbedder, got %T", embedder)
	}
}

func TestNewProviderCreatesStaticProvider(t *testing.T) {
	embedder, err := NewProvider(Config{Provider: ProviderStatic, Dimensions: 4})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, ok := embedder.(*StaticEmbedder); !ok {
		t.Fatalf("expected StaticEmbedder, got %T", embedder)
	}
}

func TestNewProviderCreatesLlamaCPPProvider(t *testing.T) {
	embedder, err := NewProvider(Config{
		Provider:   ProviderLlamaCPP,
		Dimensions: 4,
		Endpoint:   "http://127.0.0.1:8081",
	})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, ok := embedder.(*LlamaCPPEmbedder); !ok {
		t.Fatalf("expected LlamaCPPEmbedder, got %T", embedder)
	}
}

func TestNewProviderRejectsLlamaCPPWithoutEndpoint(t *testing.T) {
	_, err := NewProvider(Config{Provider: ProviderLlamaCPP, Dimensions: 4})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewProviderRejectsUnknownProvider(t *testing.T) {
	_, err := NewProvider(Config{Provider: "missing", Dimensions: 4})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected ErrUnknownProvider, got %v", err)
	}
}

func TestNewProviderRejectsInvalidDimensions(t *testing.T) {
	_, err := NewProvider(Config{Provider: ProviderStatic})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}
