package ratelimit

import (
	"context"
	"fmt"
	"math"
	"time"

	clockpkg "github.com/ayu-v0/agent-cortex/pkg/clock"
)

type TokenBucket struct {
	rate     int
	burst    int
	interval time.Duration
	clock    clockpkg.Clock
	metrics  Metrics
	state    *shardedState[tokenBucketState]
}

var _ Limiter = (*TokenBucket)(nil)

type tokenBucketState struct {
	tokens float64
	last   time.Time
}

// NewTokenBucket returns a keyed token bucket limiter.
func NewTokenBucket(config Config) (Limiter, error) {
	if config.Rate <= 0 {
		return nil, fmt.Errorf("%w: rate must be positive", ErrInvalidConfig)
	}
	if config.Burst <= 0 {
		return nil, fmt.Errorf("%w: burst must be positive", ErrInvalidConfig)
	}

	window := config.Window
	if window <= 0 {
		window = time.Second
	}

	return &TokenBucket{
		rate:     config.Rate,
		burst:    config.Burst,
		interval: window,
		clock:    normalizeClock(config.Clock),
		metrics:  normalizeMetrics(config.Metrics),
		state:    newShardedState[tokenBucketState](config, window),
	}, nil
}

func (l *TokenBucket) Allow(ctx context.Context, key string) (Decision, error) {
	return l.AllowN(ctx, key, 1)
}

func (l *TokenBucket) AllowN(ctx context.Context, key string, n int) (Decision, error) {
	start := time.Now()
	defer func() {
		observeLatency(l.metrics, TypeTokenBucket, "allow", time.Since(start))
	}()

	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeTokenBucket, key, err)
		return Decision{}, err
	}
	if err := validateRequestSize(n); err != nil {
		observeError(l.metrics, TypeTokenBucket, key, err)
		return Decision{}, err
	}

	now := l.clock.Now()
	decision, err := l.state.WithEntry(
		key,
		now,
		func(now time.Time) tokenBucketState {
			return tokenBucketState{
				tokens: float64(l.burst),
				last:   now,
			}
		},
		func(bucket *tokenBucketState, ephemeral bool) (Decision, error) {
			return l.allowN(bucket, now, n), nil
		},
	)
	if err != nil {
		observeError(l.metrics, TypeTokenBucket, key, err)
		return decision, err
	}

	observeDecision(l.metrics, TypeTokenBucket, key, decision)
	observeState(l.metrics, TypeTokenBucket, l.state.ActiveKeys())
	return decision, nil
}

func (l *TokenBucket) Reset(key string) {
	if l.state.Reset(key) {
		observeState(l.metrics, TypeTokenBucket, l.state.ActiveKeys())
	}
}

func (l *TokenBucket) ResetContext(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeTokenBucket, key, err)
		return err
	}
	l.Reset(key)
	return nil
}

func (l *TokenBucket) allowN(bucket *tokenBucketState, now time.Time, n int) Decision {
	l.refill(bucket, now)

	decision := Decision{
		Limit:      l.burst,
		Remaining:  int(math.Floor(bucket.tokens)),
		ResetAfter: l.timeToFull(bucket.tokens),
	}

	if bucket.tokens >= float64(n) {
		bucket.tokens -= float64(n)
		decision.Allowed = true
		decision.Remaining = int(math.Floor(bucket.tokens))
		decision.ResetAfter = l.timeToFull(bucket.tokens)
		return decision
	}

	missing := float64(n) - bucket.tokens
	decision.RetryAfter = l.timeForTokens(missing)
	return decision
}

func (l *TokenBucket) refill(bucket *tokenBucketState, now time.Time) {
	if !now.After(bucket.last) {
		return
	}

	elapsed := now.Sub(bucket.last)
	refill := float64(l.rate) * elapsed.Seconds() / l.interval.Seconds()
	bucket.tokens = math.Min(float64(l.burst), bucket.tokens+refill)
	bucket.last = now
}

func (l *TokenBucket) timeForTokens(tokens float64) time.Duration {
	if tokens <= 0 {
		return 0
	}

	seconds := tokens * l.interval.Seconds() / float64(l.rate)
	return time.Duration(math.Ceil(seconds * float64(time.Second)))
}

func (l *TokenBucket) timeToFull(tokens float64) time.Duration {
	missing := float64(l.burst) - tokens
	return l.timeForTokens(missing)
}
