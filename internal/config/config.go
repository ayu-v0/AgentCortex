package config

import "os"

const (
	defaultAddr              = ":8080"
	defaultDatabasePath      = "agent_memory.db"
	defaultStorageBackend    = "sqlitevec"
	defaultEmbeddingProvider = "static"
	defaultEmbeddingEndpoint = "http://127.0.0.1:8081"
	defaultModelProvider     = "openai-compatible"
	defaultModelTimeout      = "30s"
)

type Config struct {
	Addr              string
	DatabasePath      string
	StorageBackend    string
	EmbeddingProvider string
	EmbeddingEndpoint string
	ModelProvider     string
	ModelEndpoint     string
	ModelAPIKey       string
	ModelName         string
	ModelTimeout      string
}

func FromEnv() Config {
	return Config{
		Addr:              getenv("ADDR", defaultAddr),
		DatabasePath:      getenv("DATABASE_PATH", defaultDatabasePath),
		StorageBackend:    getenv("STORAGE_BACKEND", defaultStorageBackend),
		EmbeddingProvider: getenv("EMBEDDING_PROVIDER", defaultEmbeddingProvider),
		EmbeddingEndpoint: getenv("EMBEDDING_ENDPOINT", defaultEmbeddingEndpoint),
		ModelProvider:     getenv("MODEL_PROVIDER", defaultModelProvider),
		ModelEndpoint:     getenv("MODEL_ENDPOINT", ""),
		ModelAPIKey:       getenv("MODEL_API_KEY", ""),
		ModelName:         getenv("MODEL_NAME", ""),
		ModelTimeout:      getenv("MODEL_TIMEOUT", defaultModelTimeout),
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
