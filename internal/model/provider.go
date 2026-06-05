package model

import (
	"context"
	"fmt"
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
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, config.Provider)
	}
}
