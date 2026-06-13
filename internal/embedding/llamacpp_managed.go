package embedding

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultLlamaCPPHost           = "127.0.0.1"
	defaultLlamaCPPPort           = 8081
	defaultLlamaCPPStartupTimeout = 30 * time.Second
)

type ManagedLlamaCPPEmbedder struct {
	embedder *LlamaCPPEmbedder
	process  llamaCPPProcess
}

var _ Embedder = (*ManagedLlamaCPPEmbedder)(nil)
var _ io.Closer = (*ManagedLlamaCPPEmbedder)(nil)

func NewManagedLlamaCPPEmbedder(ctx context.Context, config Config) (*ManagedLlamaCPPEmbedder, error) {
	return newManagedLlamaCPPEmbedder(ctx, config, startLlamaCPPProcess)
}

func newManagedLlamaCPPEmbedder(ctx context.Context, config Config, starter llamaCPPProcessStarter) (*ManagedLlamaCPPEmbedder, error) {
	if strings.TrimSpace(config.LlamaCPPExecutablePath) == "" {
		return nil, ErrInvalidConfig
	}

	var err error
	config, err = ensureLlamaCPPModel(ctx, config, downloadLlamaCPPModel)
	if err != nil {
		return nil, err
	}

	launch, err := newLlamaCPPLaunchConfig(config)
	if err != nil {
		return nil, err
	}

	endpoint, process, err := starter(ctx, launch)
	if err != nil {
		return nil, err
	}

	config.Endpoint = endpoint
	embedder, err := NewLlamaCPPEmbedder(config)
	if err != nil {
		_ = process.Close()
		return nil, err
	}

	return &ManagedLlamaCPPEmbedder{
		embedder: embedder,
		process:  process,
	}, nil
}

func (e *ManagedLlamaCPPEmbedder) Embed(ctx context.Context, input Input) (Vector, error) {
	return e.embedder.Embed(ctx, input)
}

func (e *ManagedLlamaCPPEmbedder) Close() error {
	if e.process == nil {
		return nil
	}
	return e.process.Close()
}

type llamaCPPProcess interface {
	Close() error
}

type llamaCPPProcessStarter func(context.Context, llamaCPPLaunchConfig) (string, llamaCPPProcess, error)

type llamaCPPModelDownloadConfig struct {
	url  string
	path string
}

type llamaCPPModelDownloader func(context.Context, llamaCPPModelDownloadConfig) (string, error)

func ensureLlamaCPPModel(ctx context.Context, config Config, downloader llamaCPPModelDownloader) (Config, error) {
	modelPath := strings.TrimSpace(config.LlamaCPPModelPath)
	if modelPath == "" {
		defaultPath, err := defaultLlamaCPPModelPath(config)
		if err != nil {
			return Config{}, err
		}
		modelPath = defaultPath
	}

	ready, err := llamaCPPModelFileReady(modelPath)
	if err != nil {
		return Config{}, err
	}
	if ready {
		config.LlamaCPPModelPath = modelPath
		return config, nil
	}

	modelURL := strings.TrimSpace(config.LlamaCPPModelURL)
	if modelURL == "" {
		modelURL = DefaultLlamaCPPModelURL
	}

	downloadedPath, err := downloader(ctx, llamaCPPModelDownloadConfig{
		url:  modelURL,
		path: modelPath,
	})
	if err != nil {
		return Config{}, err
	}

	config.LlamaCPPModelPath = downloadedPath
	return config, nil
}

func defaultLlamaCPPModelPath(config Config) (string, error) {
	cacheDir := strings.TrimSpace(config.LlamaCPPModelCacheDir)
	if cacheDir == "" {
		userCacheDir, err := os.UserCacheDir()
		if err != nil {
			return "", errors.Join(ErrInvalidConfig, err)
		}
		cacheDir = filepath.Join(userCacheDir, "agent-cortex", "models")
	}
	return filepath.Join(cacheDir, DefaultLlamaCPPModelFile), nil
}

func llamaCPPModelFileReady(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		if info.IsDir() {
			return false, ErrInvalidConfig
		}
		if info.Size() == 0 {
			return false, ErrInvalidConfig
		}
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, errors.Join(ErrInvalidConfig, err)
}

type llamaCPPLaunchConfig struct {
	executable     string
	args           []string
	endpoint       string
	address        string
	startupTimeout time.Duration
}

func newLlamaCPPLaunchConfig(config Config) (llamaCPPLaunchConfig, error) {
	executable := strings.TrimSpace(config.LlamaCPPExecutablePath)
	if executable == "" {
		return llamaCPPLaunchConfig{}, ErrInvalidConfig
	}

	modelPath := strings.TrimSpace(config.LlamaCPPModelPath)
	if modelPath == "" {
		return llamaCPPLaunchConfig{}, ErrInvalidConfig
	}

	host := strings.TrimSpace(config.LlamaCPPHost)
	if host == "" {
		host = defaultLlamaCPPHost
	}

	port := config.LlamaCPPPort
	if port == 0 {
		port = defaultLlamaCPPPort
	}
	if port < 0 || port > 65535 {
		return llamaCPPLaunchConfig{}, ErrInvalidConfig
	}

	startupTimeout := config.LlamaCPPStartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = defaultLlamaCPPStartupTimeout
	}

	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" {
		endpoint = endpointForLlamaCPP(host, port)
	} else if _, err := url.ParseRequestURI(endpoint); err != nil {
		return llamaCPPLaunchConfig{}, errors.Join(ErrInvalidConfig, err)
	}

	args := []string{
		"-m", modelPath,
		"--embedding",
		"--host", host,
		"--port", strconv.Itoa(port),
	}
	args = append(args, config.LlamaCPPExtraArgs...)

	return llamaCPPLaunchConfig{
		executable:     executable,
		args:           args,
		endpoint:       endpoint,
		address:        net.JoinHostPort(connectHost(host), strconv.Itoa(port)),
		startupTimeout: startupTimeout,
	}, nil
}

func endpointForLlamaCPP(host string, port int) string {
	return "http://" + net.JoinHostPort(connectHost(host), strconv.Itoa(port))
}

func connectHost(host string) string {
	switch host {
	case "", "0.0.0.0", "::":
		return defaultLlamaCPPHost
	default:
		return host
	}
}

func downloadLlamaCPPModel(ctx context.Context, download llamaCPPModelDownloadConfig) (string, error) {
	modelURL := strings.TrimSpace(download.url)
	if modelURL == "" {
		return "", ErrInvalidConfig
	}
	modelPath := strings.TrimSpace(download.path)
	if modelPath == "" {
		return "", ErrInvalidConfig
	}

	if err := os.MkdirAll(filepath.Dir(modelPath), 0o755); err != nil {
		return "", errors.Join(ErrProviderUnavailable, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelURL, nil)
	if err != nil {
		return "", errors.Join(ErrInvalidConfig, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", errors.Join(ErrProviderUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return "", ErrProviderUnavailable
	}

	tempFile, err := os.CreateTemp(filepath.Dir(modelPath), filepath.Base(modelPath)+".*.tmp")
	if err != nil {
		return "", errors.Join(ErrProviderUnavailable, err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, err := io.Copy(tempFile, resp.Body); err != nil {
		_ = tempFile.Close()
		return "", errors.Join(ErrProviderUnavailable, err)
	}
	if err := tempFile.Close(); err != nil {
		return "", errors.Join(ErrProviderUnavailable, err)
	}

	if err := os.Rename(tempPath, modelPath); err != nil {
		if ready, statErr := llamaCPPModelFileReady(modelPath); statErr == nil && ready {
			return modelPath, nil
		}
		return "", errors.Join(ErrProviderUnavailable, err)
	}

	return modelPath, nil
}

func startLlamaCPPProcess(parent context.Context, launch llamaCPPLaunchConfig) (string, llamaCPPProcess, error) {
	processCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processCtx, launch.executable, launch.args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, errors.Join(ErrProviderUnavailable, err)
	}

	process := newExecLlamaCPPProcess(cmd, cancel)
	startupCtx, startupCancel := context.WithTimeout(parent, launch.startupTimeout)
	defer startupCancel()

	if err := waitForLlamaCPP(startupCtx, launch.address); err != nil {
		_ = process.Close()
		return "", nil, err
	}

	return launch.endpoint, process, nil
}

func waitForLlamaCPP(ctx context.Context, address string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}

		select {
		case <-ctx.Done():
			return errors.Join(ErrProviderUnavailable, ctx.Err())
		case <-ticker.C:
		}
	}
}

type execLlamaCPPProcess struct {
	cancel context.CancelFunc
	done   chan error
	once   sync.Once
	err    error
}

func newExecLlamaCPPProcess(cmd *exec.Cmd, cancel context.CancelFunc) *execLlamaCPPProcess {
	process := &execLlamaCPPProcess{
		cancel: cancel,
		done:   make(chan error, 1),
	}
	go func() {
		process.done <- cmd.Wait()
	}()
	return process
}

func (p *execLlamaCPPProcess) Close() error {
	p.once.Do(func() {
		p.cancel()
		<-p.done
	})
	return p.err
}
