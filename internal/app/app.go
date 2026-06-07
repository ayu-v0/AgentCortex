package app

import (
	"log"

	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/embedding"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/storage/sqlitevec"
	transporthttp "github.com/ayu-v0/agent-cortex/internal/transport/http"
)

func Run() error {
	return RunWithConfigPath("")
}

func RunWithConfigPath(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	backend, err := openMemoryBackend(cfg)
	if err != nil {
		return err
	}

	memoryService, err := memory.NewService(backend)
	if err != nil {
		return err
	}
	defer memoryService.Close()

	embedder, err := embedding.NewProvider(embedding.Config{
		Provider:   embedding.ProviderType(cfg.EmbeddingProvider),
		Dimensions: memory.EmbeddingDimensions,
		Endpoint:   cfg.EmbeddingEndpoint,
	})
	if err != nil {
		return err
	}
	if closer, ok := embedder.(interface{ Close() error }); ok {
		defer closer.Close()
	}

	server := transporthttp.NewServer(memoryService, embedder)
	log.Printf("agent-cortex HTTP server listening on %s", cfg.Addr)
	if err := server.Run(cfg.Addr); err != nil {
		return err
	}

	return nil
}

func openMemoryBackend(cfg config.Config) (memory.Backend, error) {
	switch cfg.StorageBackend {
	case "", "sqlitevec":
		return sqlitevec.Open(cfg.DatabasePath)
	default:
		return nil, memory.ErrUnsupportedBackend
	}
}
