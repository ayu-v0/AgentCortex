package embedding

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
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
		return llamaCPPLaunchConfig{}, fmt.Errorf("%w: llama.cpp executable path is required", ErrInvalidConfig)
	}

	modelPath := strings.TrimSpace(config.LlamaCPPModelPath)
	if modelPath == "" {
		return llamaCPPLaunchConfig{}, fmt.Errorf("%w: llama.cpp model path is required", ErrInvalidConfig)
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
		return llamaCPPLaunchConfig{}, fmt.Errorf("%w: llama.cpp port must be between 1 and 65535", ErrInvalidConfig)
	}

	startupTimeout := config.LlamaCPPStartupTimeout
	if startupTimeout <= 0 {
		startupTimeout = defaultLlamaCPPStartupTimeout
	}

	endpoint := strings.TrimRight(strings.TrimSpace(config.Endpoint), "/")
	if endpoint == "" {
		endpoint = endpointForLlamaCPP(host, port)
	} else if _, err := url.ParseRequestURI(endpoint); err != nil {
		return llamaCPPLaunchConfig{}, fmt.Errorf("%w: invalid endpoint: %v", ErrInvalidConfig, err)
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

func startLlamaCPPProcess(parent context.Context, launch llamaCPPLaunchConfig) (string, llamaCPPProcess, error) {
	processCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(processCtx, launch.executable, launch.args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		cancel()
		return "", nil, fmt.Errorf("%w: start llama.cpp: %v", ErrProviderUnavailable, err)
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
			return fmt.Errorf("%w: llama.cpp startup timeout: %v", ErrProviderUnavailable, ctx.Err())
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
