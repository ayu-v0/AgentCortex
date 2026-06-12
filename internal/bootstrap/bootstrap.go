package bootstrap

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/conversation"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/model"
	"github.com/ayu-v0/agent-cortex/internal/retrieval"
	"github.com/ayu-v0/agent-cortex/internal/storage/sqlitevec"
)

type Options struct {
	RequireModel bool
}

type Runtime struct {
	Config              config.Config
	MemoryService       *memory.Service
	ConversationService *conversation.Service
	RetrievalService    *retrieval.Service
	CleanupScheduler    *conversation.CleanupScheduler
	Embedder            embedding.Embedder
	ModelClient         model.Client
	ModelStreamer       model.Streamer
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

	conversationStore, ok := backend.(conversation.Store)
	if !ok {
		_ = memoryService.Close()
		return nil, conversation.ErrNilStore
	}
	conversationService, err := conversation.NewService(conversationStore)
	if err != nil {
		_ = memoryService.Close()
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
		Config:              cfg,
		MemoryService:       memoryService,
		ConversationService: conversationService,
		Embedder:            embedder,
	}

	if options.RequireModel || hasCompleteModelConfig(cfg) {
		timeout, err := time.ParseDuration(strings.TrimSpace(cfg.ModelTimeout))
		if err != nil {
			_ = runtime.Close()
			return nil, errors.Join(model.ErrInvalidConfig, err)
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
		return nil, model.ErrInvalidConfig
	}

	if runtime.ModelClient != nil {
		rewriter, err := retrieval.NewModelRewriter(runtime.ModelClient)
		if err != nil {
			_ = runtime.Close()
			return nil, err
		}
		vectorSearcher, ok := backend.(retrieval.VectorSearcher)
		if !ok {
			_ = runtime.Close()
			return nil, memory.ErrUnsupportedBackend
		}
		metadataFinder, ok := backend.(retrieval.MetadataFinder)
		if !ok {
			_ = runtime.Close()
			return nil, memory.ErrUnsupportedBackend
		}
		recentLimit, err := parseIntConfig(cfg.RecentTurnLimit, conversation.DefaultRecentTurnLimit)
		if err != nil || recentLimit <= 0 || recentLimit > conversation.MaxRecentTurnLimit {
			_ = runtime.Close()
			return nil, conversation.ErrInvalidConfig
		}
		runtime.RetrievalService, err = retrieval.NewService(conversationService, rewriter, embedder, vectorSearcher, metadataFinder, retrieval.Config{
			MemoryMarkdownDir: ".memory",
			RecentTurnLimit:   recentLimit,
		})
		if err != nil {
			_ = runtime.Close()
			return nil, err
		}
	}

	cleanupEnabled, err := parseBoolConfig(cfg.CleanupEnabled, true)
	if err != nil {
		_ = runtime.Close()
		return nil, conversation.ErrInvalidConfig
	}
	retentionDays, err := parseIntConfig(cfg.RetentionDays, 30)
	if err != nil || retentionDays <= 0 {
		_ = runtime.Close()
		return nil, conversation.ErrInvalidConfig
	}
	if cleanupEnabled {
		scheduler, err := conversation.NewCleanupScheduler(conversationService, conversation.CleanupSchedule{
			Enabled:       true,
			RetentionDays: retentionDays,
			TimeOfDay:     cfg.CleanupTime,
			Timezone:      cfg.CleanupTimezone,
		})
		if err != nil {
			_ = runtime.Close()
			return nil, err
		}
		scheduler.Start()
		runtime.CleanupScheduler = scheduler
	}

	return runtime, nil
}

func (r *Runtime) Close() error {
	var errs []error
	if r.CleanupScheduler != nil {
		r.CleanupScheduler.Stop()
	}
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

func parseIntConfig(value string, fallback int) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	return strconv.Atoi(value)
}

func parseBoolConfig(value string, fallback bool) (bool, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	return strconv.ParseBool(value)
}
