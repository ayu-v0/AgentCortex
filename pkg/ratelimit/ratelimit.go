package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	clockpkg "github.com/ayu-v0/agent-cortex/pkg/clock"
)

type Type string

const (
	TypeTokenBucket           Type = "token_bucket"
	TypeSlidingWindow         Type = "sliding_window"
	TypeBucketedSlidingWindow Type = "bucketed_sliding_window"
	TypeGCRA                  Type = "gcra"
	defaultShards                  = 256
	defaultBucketCount             = 12
)

// Limiter decides whether requests for a key are currently allowed.
type Limiter interface {
	Allow(ctx context.Context, key string) (Decision, error)
	AllowN(ctx context.Context, key string, n int) (Decision, error)
	Reset(key string)
}

// ContextResetter is an optional extension for limiters that need context-aware
// reset operations.
type ContextResetter interface {
	ResetContext(ctx context.Context, key string) error
}

// Decision describes the outcome of a limit check.
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	ResetAfter time.Duration
}

// StateFullPolicy controls how a limiter behaves when MaxKeys is reached.
type StateFullPolicy int

const (
	RejectNewKey StateFullPolicy = iota
	AllowNewKey
)

// Config configures a limiter implementation.
//
// Rate means tokens per Window for token bucket and requests per Window for
// window-based algorithms. Burst is required by token bucket, used by GCRA, and
// ignored by sliding window implementations. Metadata is reserved for custom
// factories.
type Config struct {
	Type            Type
	Rate            int
	Burst           int
	Window          time.Duration
	Clock           clockpkg.Clock
	Shards          int
	IdleTTL         time.Duration
	MaxKeys         int
	StateFullPolicy StateFullPolicy
	BucketCount     int
	Metrics         Metrics
	Metadata        map[string]any
}

// Factory builds a limiter from Config.
type Factory func(Config) (Limiter, error)

// Registry stores limiter factories by type.
type Registry struct {
	mu        sync.RWMutex
	factories map[Type]Factory
}

// NewRegistry returns an empty limiter registry.
func NewRegistry() *Registry {
	return &Registry{
		factories: make(map[Type]Factory),
	}
}

// Register stores or replaces a limiter factory.
func (r *Registry) Register(limitType Type, factory Factory) error {
	if factory == nil {
		return ErrNilFactory
	}
	if limitType == "" {
		return fmt.Errorf("%w: type is required", ErrInvalidConfig)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.factories[limitType] = factory
	return nil
}

// NewLimiter builds a limiter using a registered factory.
func (r *Registry) NewLimiter(config Config) (Limiter, error) {
	r.mu.RLock()
	factory, ok := r.factories[config.Type]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownType, config.Type)
	}

	return factory(config)
}

var defaultRegistry = newDefaultRegistry()

// Register stores or replaces a factory on the default registry.
func Register(limitType Type, factory Factory) error {
	return defaultRegistry.Register(limitType, factory)
}

// NewLimiter builds a limiter using the default registry.
func NewLimiter(config Config) (Limiter, error) {
	return defaultRegistry.NewLimiter(config)
}

func newDefaultRegistry() *Registry {
	registry := NewRegistry()
	_ = registry.Register(TypeTokenBucket, NewTokenBucket)
	_ = registry.Register(TypeSlidingWindow, NewSlidingWindow)
	_ = registry.Register(TypeBucketedSlidingWindow, NewBucketedSlidingWindow)
	_ = registry.Register(TypeGCRA, NewGCRA)
	return registry
}

func normalizeClock(clock clockpkg.Clock) clockpkg.Clock {
	if clock == nil {
		return clockpkg.System{}
	}
	return clock
}

func validateRequestSize(n int) error {
	if n <= 0 {
		return fmt.Errorf("%w: n must be positive", ErrInvalidConfig)
	}
	return nil
}

func normalizeShards(shards int) int {
	if shards <= 0 {
		return defaultShards
	}
	return shards
}

func normalizeIdleTTL(idleTTL, window time.Duration) time.Duration {
	if idleTTL > 0 {
		return idleTTL
	}
	if window > 0 {
		return 10 * window
	}
	return 10 * time.Second
}

func normalizeMetrics(metrics Metrics) Metrics {
	if metrics == nil {
		return noopMetrics{}
	}
	return metrics
}

func normalizeBucketCount(count int) int {
	if count <= 0 {
		return defaultBucketCount
	}
	return count
}
