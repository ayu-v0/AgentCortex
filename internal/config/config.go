package config

import (
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	defaultAddr              = ":8080"
	defaultDatabasePath      = "agent_memory.db"
	defaultStorageBackend    = "sqlitevec"
	defaultEmbeddingProvider = "static"
	defaultEmbeddingEndpoint = "http://127.0.0.1:8081"
	defaultModelProvider     = "openai-compatible"
	defaultModelTimeout      = "30s"
	defaultServerEndpoint    = "http://127.0.0.1:8080"
)

type Config struct {
	Addr              string `yaml:"addr"`
	DatabasePath      string `yaml:"database_path"`
	StorageBackend    string `yaml:"storage_backend"`
	EmbeddingProvider string `yaml:"embedding_provider"`
	EmbeddingEndpoint string `yaml:"embedding_endpoint"`
	ModelProvider     string `yaml:"model_provider"`
	ModelEndpoint     string `yaml:"model_endpoint"`
	ModelAPIKey       string `yaml:"model_api_key"`
	ModelName         string `yaml:"model_name"`
	ModelTimeout      string `yaml:"model_timeout"`
	ServerEndpoint    string `yaml:"server_endpoint"`
	AgentID           string `yaml:"agent_id"`
	UserID            string `yaml:"user_id"`
	SystemPrompt      string `yaml:"system_prompt"`
}

func FromEnv() Config {
	return FromEnvWithBase(Default())
}

func FromEnvWithBase(base Config) Config {
	base.Addr = getenv("ADDR", base.Addr)
	base.DatabasePath = getenv("DATABASE_PATH", base.DatabasePath)
	base.StorageBackend = getenv("STORAGE_BACKEND", base.StorageBackend)
	base.EmbeddingProvider = getenv("EMBEDDING_PROVIDER", base.EmbeddingProvider)
	base.EmbeddingEndpoint = getenv("EMBEDDING_ENDPOINT", base.EmbeddingEndpoint)
	base.ModelProvider = getenv("MODEL_PROVIDER", base.ModelProvider)
	base.ModelEndpoint = getenv("MODEL_ENDPOINT", base.ModelEndpoint)
	base.ModelAPIKey = getenv("MODEL_API_KEY", base.ModelAPIKey)
	base.ModelName = getenv("MODEL_NAME", base.ModelName)
	base.ModelTimeout = getenv("MODEL_TIMEOUT", base.ModelTimeout)
	base.ServerEndpoint = getenv("SERVER_ENDPOINT", base.ServerEndpoint)
	base.AgentID = getenv("AGENT_ID", base.AgentID)
	base.UserID = getenv("USER_ID", base.UserID)
	base.SystemPrompt = getenv("SYSTEM_PROMPT", base.SystemPrompt)
	return base
}

func FromYAML(data []byte) (Config, error) {
	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func FromYAMLReader(reader io.Reader) (Config, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Config{}, err
	}
	return FromYAML(data)
}

func FromYAMLFile(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer file.Close()

	return FromYAMLReader(file)
}

func Load(path string) (Config, error) {
	if path == "" {
		return FromEnv(), nil
	}

	cfg, err := FromYAMLFile(path)
	if err != nil {
		return Config{}, err
	}

	return FromEnvWithBase(cfg), nil
}

func Default() Config {
	return Config{
		Addr:              defaultAddr,
		DatabasePath:      defaultDatabasePath,
		StorageBackend:    defaultStorageBackend,
		EmbeddingProvider: defaultEmbeddingProvider,
		EmbeddingEndpoint: defaultEmbeddingEndpoint,
		ModelProvider:     defaultModelProvider,
		ModelEndpoint:     "",
		ModelAPIKey:       "",
		ModelName:         "",
		ModelTimeout:      defaultModelTimeout,
		ServerEndpoint:    defaultServerEndpoint,
		AgentID:           "",
		UserID:            "",
		SystemPrompt:      "",
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
