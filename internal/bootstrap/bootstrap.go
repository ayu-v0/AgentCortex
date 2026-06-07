package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/ayu-v0/agent-cortex/internal/storage/sqlitevec"
)

type Options struct {
	RequireModel bool
}

type Runtime struct {
	Config        config.Config
	MemoryService *memory.Service
	Embedder      embedding.Embedder
	ModelClient   model.Client
	ModelStreamer model.Streamer
}

func New(ctx context.Context, configPath string, options Options) (*Runtime, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}

	backend, err := openMemoryBackend(cfg)
	if err != nil {
		return nil, err
	}

	memoryService, err := memory.NewService(backend)
	if err != nil {
		_ = backend.Close()
		return nil, err
	}

	embedder, err := embedding.NewProvider(embedding.Config{
		Provider:   embedding.ProviderType(cfg.EmbeddingProvider),
		Dimensions: memory.EmbeddingDimensions,
		Endpoint:   cfg.EmbeddingEndpoint,
	})
	if err != nil {
		_ = memoryService.Close()
		return nil, err
	}

	runtime := &Runtime{
		Config:        cfg,
		MemoryService: memoryService,
		Embedder:      embedder,
	}

	if options.RequireModel || hasCompleteModelConfig(cfg) {
		timeout, err := time.ParseDuration(strings.TrimSpace(cfg.ModelTimeout))
		if err != nil {
			_ = runtime.Close()
			return nil, fmt.Errorf("parse model timeout: %w", err)
		}

		modelConfig := model.Config{
			Provider: model.ProviderType(cfg.ModelProvider),
			Endpoint: cfg.ModelEndpoint,
			APIKey:   cfg.ModelAPIKey,
			Model:    cfg.ModelName,
			Timeout:  timeout,
		}

		client, err := model.NewProviderWithContext(ctx, modelConfig)
		if err != nil {
			_ = runtime.Close()
			return nil, err
		}
		streamer, err := model.NewStreamingProviderWithContext(ctx, modelConfig)
		if err != nil {
			_ = runtime.Close()
			return nil, err
		}

		runtime.ModelClient = client
		runtime.ModelStreamer = streamer
	}

	if options.RequireModel && (runtime.ModelClient == nil || runtime.ModelStreamer == nil) {
		_ = runtime.Close()
		return nil, fmt.Errorf("%w: model endpoint and model name are required", model.ErrInvalidConfig)
	}

	return runtime, nil
}

func (r *Runtime) Close() error {
	var errs []error
	if closer, ok := r.Embedder.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if r.MemoryService != nil {
		if err := r.MemoryService.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func openMemoryBackend(cfg config.Config) (memory.Backend, error) {
	switch cfg.StorageBackend {
	case "", "sqlitevec":
		return sqlitevec.Open(cfg.DatabasePath)
	default:
		return nil, memory.ErrUnsupportedBackend
	}
}

func hasCompleteModelConfig(cfg config.Config) bool {
	return strings.TrimSpace(cfg.ModelEndpoint) != "" && strings.TrimSpace(cfg.ModelName) != ""
}
