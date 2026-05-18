package ratelimit

import (
	"context"
	"fmt"
	"math"
	"time"

	clockpkg "github.com/ayu-v0/agent-cortex/pkg/clock"
)

type GCRA struct {
	rate      int
	burst     int
	window    time.Duration
	interval  time.Duration
	tolerance time.Duration
	clock     clockpkg.Clock
	metrics   Metrics
	state     *shardedState[gcraState]
}

var _ Limiter = (*GCRA)(nil)

type gcraState struct {
	tat time.Time
}

// NewGCRA returns a keyed Generic Cell Rate Algorithm limiter.
func NewGCRA(config Config) (Limiter, error) {
	if config.Rate <= 0 {
		return nil, fmt.Errorf("%w: rate must be positive", ErrInvalidConfig)
	}
	if config.Window <= 0 {
		return nil, fmt.Errorf("%w: window must be positive", ErrInvalidConfig)
	}

	burst := config.Burst
	if burst <= 0 {
		burst = config.Rate
	}
	interval := config.Window / time.Duration(config.Rate)
	if interval <= 0 {
		interval = time.Nanosecond
	}

	return &GCRA{
		rate:      config.Rate,
		burst:     burst,
		window:    config.Window,
		interval:  interval,
		tolerance: time.Duration(burst-1) * interval,
		clock:     normalizeClock(config.Clock),
		metrics:   normalizeMetrics(config.Metrics),
		state:     newShardedState[gcraState](config, config.Window),
	}, nil
}

func (l *GCRA) Allow(ctx context.Context, key string) (Decision, error) {
	return l.AllowN(ctx, key, 1)
}

func (l *GCRA) AllowN(ctx context.Context, key string, n int) (Decision, error) {
	start := time.Now()
	defer func() {
		observeLatency(l.metrics, TypeGCRA, "allow", time.Since(start))
	}()

	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeGCRA, key, err)
		return Decision{}, err
	}
	if err := validateRequestSize(n); err != nil {
		observeError(l.metrics, TypeGCRA, key, err)
		return Decision{}, err
	}

	now := l.clock.Now()
	decision, err := l.state.WithEntry(
		key,
		now,
		func(time.Time) gcraState {
			return gcraState{}
		},
		func(state *gcraState, ephemeral bool) (Decision, error) {
			return l.allowN(state, now, n), nil
		},
	)
	if err != nil {
		observeError(l.metrics, TypeGCRA, key, err)
		return decision, err
	}

	observeDecision(l.metrics, TypeGCRA, key, decision)
	observeState(l.metrics, TypeGCRA, l.state.ActiveKeys())
	return decision, nil
}

func (l *GCRA) Reset(key string) {
	if l.state.Reset(key) {
		observeState(l.metrics, TypeGCRA, l.state.ActiveKeys())
	}
}

func (l *GCRA) ResetContext(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeGCRA, key, err)
		return err
	}
	l.Reset(key)
	return nil
}

func (l *GCRA) allowN(state *gcraState, now time.Time, n int) Decision {
	if n > l.burst {
		return Decision{
			Allowed:    false,
			Limit:      l.burst,
			Remaining:  0,
			RetryAfter: l.interval,
			ResetAfter: l.interval,
		}
	}

	tat := state.tat
	if tat.IsZero() || tat.Before(now) {
		tat = now
	}

	allowedAt := tat.Add(time.Duration(n-1) * l.interval).Add(-l.tolerance)
	if now.Before(allowedAt) {
		retryAfter := allowedAt.Sub(now)
		return Decision{
			Allowed:    false,
			Limit:      l.burst,
			Remaining:  0,
			RetryAfter: retryAfter,
			ResetAfter: retryAfter,
		}
	}

	increment := time.Duration(n) * l.interval
	state.tat = tat.Add(increment)
	return Decision{
		Allowed:    true,
		Limit:      l.burst,
		Remaining:  l.remaining(now, state.tat),
		ResetAfter: state.tat.Sub(now),
	}
}

func (l *GCRA) remaining(now, tat time.Time) int {
	remainingWindow := now.Add(l.tolerance).Sub(tat)
	remaining := int(math.Floor(float64(remainingWindow)/float64(l.interval))) + 1
	return min(max(remaining, 0), l.burst)
}
