package app

import (
	"context"
	"log"

	"github.com/ayu-v0/agent-cortex/internal/bootstrap"
	transporthttp "github.com/ayu-v0/agent-cortex/internal/transport/http"
)

func Run() error {
	return RunWithConfigPath("")
}

func RunWithConfigPath(configPath string) error {
	runtime, err := bootstrap.New(context.Background(), configPath, bootstrap.Options{})
	if err != nil {
		return err
	}
	defer runtime.Close()

	server := transporthttp.NewServer(runtime.MemoryService, runtime.Embedder, runtime.ModelStreamer)
	log.Printf("agent-cortex HTTP server listening on %s", runtime.Config.Addr)
	if err := server.Run(runtime.Config.Addr); err != nil {
		return err
	}

	return nil
}
