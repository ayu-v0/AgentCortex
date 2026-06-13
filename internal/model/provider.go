package model

import (
	"context"
	"time"
)

type ProviderType string

const (
	ProviderOpenAICompatible ProviderType = "openai-compatible"

	defaultProvider = ProviderOpenAICompatible
	defaultTimeout  = 30 * time.Second
)

type Config struct {
	Provider ProviderType
	Endpoint string
	APIKey   string
	Model    string
	Timeout  time.Duration
}

func NewProvider(config Config) (Client, error) {
	return NewProviderWithContext(context.Background(), config)
}

func NewStreamingProvider(config Config) (Streamer, error) {
	return NewStreamingProviderWithContext(context.Background(), config)
}

func NewProviderWithContext(ctx context.Context, config Config) (Client, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.Provider == "" {
		config.Provider = defaultProvider
	}
	if config.Timeout <= 0 {
		config.Timeout = defaultTimeout
	}

	switch config.Provider {
	case ProviderOpenAICompatible:
		return NewOpenAICompatibleClient(config)
	default:
		return nil, ErrUnknownProvider
	}
}

func NewStreamingProviderWithContext(ctx context.Context, config Config) (Streamer, error) {
	client, err := NewProviderWithContext(ctx, config)
	if err != nil {
		return nil, err
	}
	streamer, ok := client.(Streamer)
	if !ok {
		return nil, ErrInvalidConfig
	}
	return streamer, nil
}
