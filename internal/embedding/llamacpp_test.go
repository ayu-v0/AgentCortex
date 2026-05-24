package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLlamaCPPEmbedderSendsEmbeddingRequest(t *testing.T) {
	var gotMethod string
	var gotPath string
	var gotRequest llamaCPPEmbeddingRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3,0.4]}]}`))
	}))
	defer server.Close()

	embedder, err := NewLlamaCPPEmbedder(Config{
		Dimensions: 4,
		Endpoint:   server.URL,
		Model:      "test-model",
		Timeout:    time.Second,
	})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}

	vector, err := embedder.Embed(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("expected POST, got %s", gotMethod)
	}
	if gotPath != "/v1/embeddings" {
		t.Fatalf("expected /v1/embeddings, got %s", gotPath)
	}
	if gotRequest.Model != "test-model" {
		t.Fatalf("expected model test-model, got %q", gotRequest.Model)
	}
	if gotRequest.Input != "hello" {
		t.Fatalf("expected input hello, got %q", gotRequest.Input)
	}
	if len(vector) != 4 {
		t.Fatalf("expected 4 dimensions, got %d", len(vector))
	}
}

func TestLlamaCPPEmbedderUsesDefaultModel(t *testing.T) {
	var gotRequest llamaCPPEmbeddingRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	embedder, err := NewLlamaCPPEmbedder(Config{Dimensions: 2, Endpoint: server.URL})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}
	if _, err := embedder.Embed(context.Background(), Input{Text: "hello"}); err != nil {
		t.Fatalf("embed: %v", err)
	}
	if gotRequest.Model != DefaultLlamaCPPModel {
		t.Fatalf("expected default model %q, got %q", DefaultLlamaCPPModel, gotRequest.Model)
	}
}

func TestLlamaCPPEmbedderReturnsContextError(t *testing.T) {
	embedder, err := NewLlamaCPPEmbedder(Config{Dimensions: 4, Endpoint: "http://127.0.0.1:8081"})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = embedder.Embed(ctx, Input{Text: "hello"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestLlamaCPPEmbedderRejectsEmptyInput(t *testing.T) {
	embedder, err := NewLlamaCPPEmbedder(Config{Dimensions: 4, Endpoint: "http://127.0.0.1:8081"})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}

	_, err = embedder.Embed(context.Background(), Input{Text: ""})
	if !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("expected ErrEmptyInput, got %v", err)
	}
}

func TestLlamaCPPEmbedderMapsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	embedder, err := NewLlamaCPPEmbedder(Config{Dimensions: 4, Endpoint: server.URL})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}

	_, err = embedder.Embed(context.Background(), Input{Text: "hello"})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestLlamaCPPEmbedderMapsInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{`))
	}))
	defer server.Close()

	embedder, err := NewLlamaCPPEmbedder(Config{Dimensions: 4, Endpoint: server.URL})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}

	_, err = embedder.Embed(context.Background(), Input{Text: "hello"})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestLlamaCPPEmbedderMapsMissingEmbedding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	embedder, err := NewLlamaCPPEmbedder(Config{Dimensions: 4, Endpoint: server.URL})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}

	_, err = embedder.Embed(context.Background(), Input{Text: "hello"})
	if !errors.Is(err, ErrProviderUnavailable) {
		t.Fatalf("expected ErrProviderUnavailable, got %v", err)
	}
}

func TestLlamaCPPEmbedderRejectsDimensionMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	embedder, err := NewLlamaCPPEmbedder(Config{Dimensions: 4, Endpoint: server.URL})
	if err != nil {
		t.Fatalf("new llama cpp embedder: %v", err)
	}

	_, err = embedder.Embed(context.Background(), Input{Text: "hello"})
	if !errors.Is(err, ErrInvalidVector) {
		t.Fatalf("expected ErrInvalidVector, got %v", err)
	}
}
