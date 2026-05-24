package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestManagedLlamaCPPEmbedderStartsProcessAndEmbeds(t *testing.T) {
	var gotClosed bool
	var gotLaunch llamaCPPLaunchConfig

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request llamaCPPEmbeddingRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Input != "hello" {
			t.Fatalf("expected input hello, got %q", request.Input)
		}
		if request.Model != "model-name" {
			t.Fatalf("expected model-name, got %q", request.Model)
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3,0.4]}]}`))
	}))
	defer server.Close()

	modelPath := writeTestModel(t, "model.gguf")
	embedder, err := newManagedLlamaCPPEmbedder(context.Background(), Config{
		Provider:               ProviderLlamaCPP,
		Dimensions:             4,
		Model:                  "model-name",
		Endpoint:               server.URL,
		LlamaCPPExecutablePath: "llama-server",
		LlamaCPPModelPath:      modelPath,
		LlamaCPPHost:           "127.0.0.1",
		LlamaCPPPort:           18081,
		LlamaCPPStartupTimeout: time.Second,
		LlamaCPPExtraArgs:      []string{"--pooling", "mean"},
	}, func(ctx context.Context, launch llamaCPPLaunchConfig) (string, llamaCPPProcess, error) {
		gotLaunch = launch
		return launch.endpoint, closeFunc(func() error {
			gotClosed = true
			return nil
		}), nil
	})
	if err != nil {
		t.Fatalf("new managed llama cpp embedder: %v", err)
	}

	vector, err := embedder.Embed(context.Background(), Input{Text: "hello"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vector) != 4 {
		t.Fatalf("expected 4 dimensions, got %d", len(vector))
	}

	if gotLaunch.executable != "llama-server" {
		t.Fatalf("expected llama-server executable, got %q", gotLaunch.executable)
	}
	expectedArgs := []string{"-m", modelPath, "--embedding", "--host", "127.0.0.1", "--port", "18081", "--pooling", "mean"}
	if !reflect.DeepEqual(gotLaunch.args, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, gotLaunch.args)
	}

	if err := embedder.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !gotClosed {
		t.Fatal("expected managed process to be closed")
	}
}

func TestManagedLlamaCPPEmbedderClosesProcessWhenHTTPProviderFails(t *testing.T) {
	var gotClosed bool
	modelPath := writeTestModel(t, "model.gguf")

	_, err := newManagedLlamaCPPEmbedder(context.Background(), Config{
		Provider:               ProviderLlamaCPP,
		Dimensions:             0,
		LlamaCPPExecutablePath: "llama-server",
		LlamaCPPModelPath:      modelPath,
	}, func(ctx context.Context, launch llamaCPPLaunchConfig) (string, llamaCPPProcess, error) {
		return launch.endpoint, closeFunc(func() error {
			gotClosed = true
			return nil
		}), nil
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if !gotClosed {
		t.Fatal("expected managed process to be closed after provider creation failure")
	}
}

func TestNewLlamaCPPLaunchConfigDefaults(t *testing.T) {
	launch, err := newLlamaCPPLaunchConfig(Config{
		LlamaCPPExecutablePath: "llama-server",
		LlamaCPPModelPath:      "model.gguf",
	})
	if err != nil {
		t.Fatalf("new launch config: %v", err)
	}

	if launch.endpoint != "http://127.0.0.1:8081" {
		t.Fatalf("expected default endpoint, got %q", launch.endpoint)
	}
	if launch.address != "127.0.0.1:8081" {
		t.Fatalf("expected default address, got %q", launch.address)
	}
	if launch.startupTimeout != defaultLlamaCPPStartupTimeout {
		t.Fatalf("expected default startup timeout, got %s", launch.startupTimeout)
	}
}

func TestManagedLlamaCPPEmbedderDownloadsMissingModelPath(t *testing.T) {
	var gotLaunch llamaCPPLaunchConfig
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("gguf model"))
	}))
	defer downloadServer.Close()

	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer embeddingServer.Close()

	modelPath := filepath.Join(t.TempDir(), "models", "model.gguf")
	embedder, err := newManagedLlamaCPPEmbedder(context.Background(), Config{
		Provider:               ProviderLlamaCPP,
		Dimensions:             2,
		Endpoint:               embeddingServer.URL,
		LlamaCPPExecutablePath: "llama-server",
		LlamaCPPModelPath:      modelPath,
		LlamaCPPModelURL:       downloadServer.URL,
	}, func(ctx context.Context, launch llamaCPPLaunchConfig) (string, llamaCPPProcess, error) {
		gotLaunch = launch
		return launch.endpoint, closeFunc(func() error { return nil }), nil
	})
	if err != nil {
		t.Fatalf("new managed llama cpp embedder: %v", err)
	}
	defer embedder.Close()

	content, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("read downloaded model: %v", err)
	}
	if string(content) != "gguf model" {
		t.Fatalf("expected downloaded model content, got %q", string(content))
	}
	expectedArgs := []string{"-m", modelPath, "--embedding", "--host", "127.0.0.1", "--port", "8081"}
	if !reflect.DeepEqual(gotLaunch.args, expectedArgs) {
		t.Fatalf("expected args %v, got %v", expectedArgs, gotLaunch.args)
	}
}

func TestManagedLlamaCPPEmbedderDownloadsDefaultModelPath(t *testing.T) {
	downloadServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("default gguf model"))
	}))
	defer downloadServer.Close()

	embeddingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2]}]}`))
	}))
	defer embeddingServer.Close()

	cacheDir := t.TempDir()
	expectedModelPath := filepath.Join(cacheDir, DefaultLlamaCPPModelFile)
	embedder, err := newManagedLlamaCPPEmbedder(context.Background(), Config{
		Provider:               ProviderLlamaCPP,
		Dimensions:             2,
		Endpoint:               embeddingServer.URL,
		LlamaCPPExecutablePath: "llama-server",
		LlamaCPPModelURL:       downloadServer.URL,
		LlamaCPPModelCacheDir:  cacheDir,
	}, func(ctx context.Context, launch llamaCPPLaunchConfig) (string, llamaCPPProcess, error) {
		if launch.args[1] != expectedModelPath {
			t.Fatalf("expected model path %q, got %q", expectedModelPath, launch.args[1])
		}
		return launch.endpoint, closeFunc(func() error { return nil }), nil
	})
	if err != nil {
		t.Fatalf("new managed llama cpp embedder: %v", err)
	}
	defer embedder.Close()

	if _, err := os.Stat(expectedModelPath); err != nil {
		t.Fatalf("expected default model download: %v", err)
	}
}

func TestNewManagedLlamaCPPEmbedderRejectsMissingExecutable(t *testing.T) {
	_, err := NewManagedLlamaCPPEmbedder(context.Background(), Config{
		Provider:          ProviderLlamaCPP,
		Dimensions:        4,
		LlamaCPPModelPath: "model.gguf",
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNewProviderWithAutoStartRequiresManagedConfig(t *testing.T) {
	_, err := NewProvider(Config{
		Provider:   ProviderLlamaCPP,
		Dimensions: 4,
		AutoStart:  true,
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

type closeFunc func() error

func (f closeFunc) Close() error {
	return f()
}

func writeTestModel(t *testing.T, name string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("gguf"), 0o644); err != nil {
		t.Fatalf("write test model: %v", err)
	}
	return path
}
