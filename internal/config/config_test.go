package config

import "testing"

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
