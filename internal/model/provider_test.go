package model

import (
	"errors"
	"testing"
	"time"
)

func TestNewProviderDefaultsToOpenAICompatible(t *testing.T) {
	client, err := NewProvider(Config{Endpoint: "http://127.0.0.1:8082", Model: "test-model"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	if _, ok := client.(*OpenAICompatibleClient); !ok {
		t.Fatalf("expected OpenAICompatibleClient, got %T", client)
	}
}

func TestNewProviderRejectsUnknownProvider(t *testing.T) {
	_, err := NewProvider(Config{Provider: ProviderType("missing"), Endpoint: "http://127.0.0.1:8082", Model: "test-model"})
	if !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected ErrUnknownProvider, got %v", err)
	}
}

func TestNewProviderUsesDefaultTimeout(t *testing.T) {
	client, err := NewProvider(Config{Endpoint: "http://127.0.0.1:8082", Model: "test-model"})
	if err != nil {
		t.Fatalf("new provider: %v", err)
	}
	openAIClient := client.(*OpenAICompatibleClient)
	if openAIClient.client.Timeout != 30*time.Second {
		t.Fatalf("expected default timeout 30s, got %v", openAIClient.client.Timeout)
	}
}

func TestNewStreamingProviderReturnsStreamer(t *testing.T) {
	streamer, err := NewStreamingProvider(Config{Endpoint: "http://127.0.0.1:8082", Model: "test-model"})
	if err != nil {
		t.Fatalf("new streaming provider: %v", err)
	}
	if _, ok := streamer.(*OpenAICompatibleClient); !ok {
		t.Fatalf("expected OpenAICompatibleClient, got %T", streamer)
	}
}
