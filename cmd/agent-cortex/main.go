package main

import (
	"log"
	"os"

	"github.com/ayu-v0/agent-cortex/internal/memory"
	transporthttp "github.com/ayu-v0/agent-cortex/internal/transport/http"
)

func main() {
	dbPath := getenv("DATABASE_PATH", "agent_memory.db")
	addr := getenv("ADDR", ":8080")

	store, err := memory.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	server := transporthttp.NewServer(store)
	log.Printf("agent-cortex HTTP server listening on %s", addr)
	if err := server.Run(addr); err != nil {
		log.Fatal(err)
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
