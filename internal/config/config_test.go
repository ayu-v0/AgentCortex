package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromEnvReadsModelConfig(t *testing.T) {
	t.Setenv("MODEL_PROVIDER", "openai-compatible")
	t.Setenv("MODEL_ENDPOINT", "http://127.0.0.1:8082")
	t.Setenv("MODEL_API_KEY", "secret")
	t.Setenv("MODEL_NAME", "test-model")
	t.Setenv("MODEL_TIMEOUT", "45s")

	cfg := FromEnv()

	if cfg.ModelProvider != "openai-compatible" {
		t.Fatalf("expected model provider, got %q", cfg.ModelProvider)
	}
	if cfg.ModelEndpoint != "http://127.0.0.1:8082" {
		t.Fatalf("expected model endpoint, got %q", cfg.ModelEndpoint)
	}
	if cfg.ModelAPIKey != "secret" {
		t.Fatalf("expected model api key, got %q", cfg.ModelAPIKey)
	}
	if cfg.ModelName != "test-model" {
		t.Fatalf("expected model name, got %q", cfg.ModelName)
	}
	if cfg.ModelTimeout != "45s" {
		t.Fatalf("expected model timeout, got %q", cfg.ModelTimeout)
	}
}

func TestFromEnvDefaultsModelConfigWithoutEnablingModelClient(t *testing.T) {
	cfg := FromEnv()

	if cfg.ModelProvider != "openai-compatible" {
		t.Fatalf("expected default model provider, got %q", cfg.ModelProvider)
	}
	if cfg.ModelEndpoint != "" {
		t.Fatalf("expected empty model endpoint by default, got %q", cfg.ModelEndpoint)
	}
	if cfg.ModelAPIKey != "" {
		t.Fatalf("expected empty model api key by default, got %q", cfg.ModelAPIKey)
	}
	if cfg.ModelName != "" {
		t.Fatalf("expected empty model name by default, got %q", cfg.ModelName)
	}
	if cfg.ModelTimeout != "30s" {
		t.Fatalf("expected default model timeout 30s, got %q", cfg.ModelTimeout)
	}
}

func TestFromYAMLReadsConfig(t *testing.T) {
	cfg, err := FromYAML([]byte(`
addr: ":9090"
database_path: "memory.db"
storage_backend: "sqlitevec"
embedding_provider: "llama.cpp"
embedding_endpoint: "http://127.0.0.1:8081"
model_provider: "openai-compatible"
model_endpoint: "http://127.0.0.1:8082"
model_api_key: "secret"
model_name: "test-model"
model_timeout: "45s"
`))
	if err != nil {
		t.Fatalf("from yaml: %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Fatalf("expected addr, got %q", cfg.Addr)
	}
	if cfg.DatabasePath != "memory.db" {
		t.Fatalf("expected database path, got %q", cfg.DatabasePath)
	}
	if cfg.StorageBackend != "sqlitevec" {
		t.Fatalf("expected storage backend, got %q", cfg.StorageBackend)
	}
	if cfg.EmbeddingProvider != "llama.cpp" {
		t.Fatalf("expected embedding provider, got %q", cfg.EmbeddingProvider)
	}
	if cfg.EmbeddingEndpoint != "http://127.0.0.1:8081" {
		t.Fatalf("expected embedding endpoint, got %q", cfg.EmbeddingEndpoint)
	}
	if cfg.ModelProvider != "openai-compatible" {
		t.Fatalf("expected model provider, got %q", cfg.ModelProvider)
	}
	if cfg.ModelEndpoint != "http://127.0.0.1:8082" {
		t.Fatalf("expected model endpoint, got %q", cfg.ModelEndpoint)
	}
	if cfg.ModelAPIKey != "secret" {
		t.Fatalf("expected model api key, got %q", cfg.ModelAPIKey)
	}
	if cfg.ModelName != "test-model" {
		t.Fatalf("expected model name, got %q", cfg.ModelName)
	}
	if cfg.ModelTimeout != "45s" {
		t.Fatalf("expected model timeout, got %q", cfg.ModelTimeout)
	}
}

func TestFromYAMLKeepsDefaultsForOmittedFields(t *testing.T) {
	cfg, err := FromYAML([]byte(`model_api_key: "secret"`))
	if err != nil {
		t.Fatalf("from yaml: %v", err)
	}

	if cfg.Addr != defaultAddr {
		t.Fatalf("expected default addr, got %q", cfg.Addr)
	}
	if cfg.DatabasePath != defaultDatabasePath {
		t.Fatalf("expected default database path, got %q", cfg.DatabasePath)
	}
	if cfg.ModelAPIKey != "secret" {
		t.Fatalf("expected model api key, got %q", cfg.ModelAPIKey)
	}
}

func TestFromYAMLReaderReturnsParseErrors(t *testing.T) {
	_, err := FromYAMLReader(strings.NewReader("addr: ["))
	if err == nil {
		t.Fatal("expected yaml parse error")
	}
}

func TestFromYAMLFileReadsConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("addr: \":9090\"\nmodel_name: \"file-model\"\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, err := FromYAMLFile(path)
	if err != nil {
		t.Fatalf("from yaml file: %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Fatalf("expected addr, got %q", cfg.Addr)
	}
	if cfg.ModelName != "file-model" {
		t.Fatalf("expected model name, got %q", cfg.ModelName)
	}
}

func TestLoadUsesYAMLFileAndEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte("addr: \":9090\"\nmodel_name: \"file-model\"\n"), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	t.Setenv("MODEL_NAME", "env-model")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Addr != ":9090" {
		t.Fatalf("expected addr from yaml, got %q", cfg.Addr)
	}
	if cfg.ModelName != "env-model" {
		t.Fatalf("expected env override, got %q", cfg.ModelName)
	}
}

func TestLoadWithoutPathUsesEnv(t *testing.T) {
	t.Setenv("ADDR", ":9191")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	if cfg.Addr != ":9191" {
		t.Fatalf("expected env addr, got %q", cfg.Addr)
	}
}
