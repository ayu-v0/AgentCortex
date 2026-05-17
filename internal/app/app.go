package app

import (
	"fmt"
	"log"

	"github.com/ayu-v0/agent-cortex/internal/config"
	"github.com/ayu-v0/agent-cortex/internal/memory"
	"github.com/ayu-v0/agent-cortex/internal/storage/sqlitevec"
	transporthttp "github.com/ayu-v0/agent-cortex/internal/transport/http"
)

func Run() error {
	cfg := config.FromEnv()

	backend, err := openMemoryBackend(cfg)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}

	store, err := memory.NewStore(backend)
	if err != nil {
		return fmt.Errorf("create memory store: %w", err)
	}
	defer store.Close()

	server := transporthttp.NewServer(store)
	log.Printf("agent-cortex HTTP server listening on %s", cfg.Addr)
	if err := server.Run(cfg.Addr); err != nil {
		return fmt.Errorf("run HTTP server: %w", err)
	}

	return nil
}

func openMemoryBackend(cfg config.Config) (memory.Backend, error) {
	switch cfg.StorageBackend {
	case "", "sqlitevec":
		return sqlitevec.Open(cfg.DatabasePath)
	default:
		return nil, fmt.Errorf("unsupported storage backend: %s", cfg.StorageBackend)
	}
}
