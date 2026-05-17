package config

import "os"

const (
	defaultAddr         = ":8080"
	defaultDatabasePath = "agent_memory.db"
)

type Config struct {
	Addr         string
	DatabasePath string
}

func FromEnv() Config {
	return Config{
		Addr:         getenv("ADDR", defaultAddr),
		DatabasePath: getenv("DATABASE_PATH", defaultDatabasePath),
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
