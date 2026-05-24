package embedding

import (
	"context"
	"fmt"
	"time"
)

type ProviderType string

const (
	ProviderStatic   ProviderType = "static"
	ProviderLlamaCPP ProviderType = "llama.cpp"

	defaultProvider = ProviderLlamaCPP
)

const DefaultLlamaCPPModel = "ggml-org/embeddinggemma-300m-qat-q8_0-GGUF"

type Config struct {
	Provider   ProviderType
	Dimensions int
	Model      string
	Endpoint   string
	APIKey     string
	Timeout    time.Duration

	AutoStart              bool
	LlamaCPPExecutablePath string
	LlamaCPPModelPath      string
	LlamaCPPHost           string
	LlamaCPPPort           int
	LlamaCPPStartupTimeout time.Duration
	LlamaCPPExtraArgs      []string
}

func NewProvider(config Config) (Embedder, error) {
	return NewProviderWithContext(context.Background(), config)
}

func NewProviderWithContext(ctx context.Context, config Config) (Embedder, error) {
	if config.Provider == "" {
		config.Provider = defaultProvider
	}
	if config.Dimensions <= 0 {
		return nil, fmt.Errorf("%w: dimensions must be positive", ErrInvalidConfig)
	}

	switch config.Provider {
	case ProviderStatic:
		return NewStaticEmbedder(config.Dimensions)
	case ProviderLlamaCPP:
		if config.AutoStart {
			return NewManagedLlamaCPPEmbedder(ctx, config)
		}
		return NewLlamaCPPEmbedder(config)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownProvider, config.Provider)
	}
}
