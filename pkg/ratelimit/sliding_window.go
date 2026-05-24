package ratelimit

import (
	"context"
	"fmt"
	"time"

	clockpkg "github.com/ayu-v0/agent-cortex/pkg/clock"
)

type SlidingWindow struct {
	limit   int
	window  time.Duration
	clock   clockpkg.Clock
	metrics Metrics
	state   *shardedState[slidingWindowState]
}

var _ Limiter = (*SlidingWindow)(nil)

type slidingWindowState struct {
	records []time.Time
}

// NewSlidingWindow returns a keyed sliding window limiter.
func NewSlidingWindow(config Config) (Limiter, error) {
	if config.Rate <= 0 {
		return nil, fmt.Errorf("%w: rate must be positive", ErrInvalidConfig)
	}
	if config.Window <= 0 {
		return nil, fmt.Errorf("%w: window must be positive", ErrInvalidConfig)
	}

	return &SlidingWindow{
		limit:   config.Rate,
		window:  config.Window,
		clock:   normalizeClock(config.Clock),
		metrics: normalizeMetrics(config.Metrics),
		state:   newShardedState[slidingWindowState](config, config.Window),
	}, nil
}

func (l *SlidingWindow) Allow(ctx context.Context, key string) (Decision, error) {
	return l.AllowN(ctx, key, 1)
}

func (l *SlidingWindow) AllowN(ctx context.Context, key string, n int) (Decision, error) {
	start := time.Now()
	defer func() {
		observeLatency(l.metrics, TypeSlidingWindow, "allow", time.Since(start))
	}()

	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeSlidingWindow, key, err)
		return Decision{}, err
	}
	if err := validateRequestSize(n); err != nil {
		observeError(l.metrics, TypeSlidingWindow, key, err)
		return Decision{}, err
	}

	now := l.clock.Now()
	decision, err := l.state.WithEntry(
		key,
		now,
		func(time.Time) slidingWindowState {
			return slidingWindowState{}
		},
		func(state *slidingWindowState, ephemeral bool) (Decision, error) {
			return l.allowN(state, now, n), nil
		},
	)
	if err != nil {
		observeError(l.metrics, TypeSlidingWindow, key, err)
		return decision, err
	}

	observeDecision(l.metrics, TypeSlidingWindow, key, decision)
	observeState(l.metrics, TypeSlidingWindow, l.state.ActiveKeys())
	return decision, nil
}

func (l *SlidingWindow) Reset(key string) {
	if l.state.Reset(key) {
		observeState(l.metrics, TypeSlidingWindow, l.state.ActiveKeys())
	}
}

func (l *SlidingWindow) ResetContext(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeSlidingWindow, key, err)
		return err
	}
	l.Reset(key)
	return nil
}

func (l *SlidingWindow) allowN(state *slidingWindowState, now time.Time, n int) Decision {
	records := l.prune(state.records, now)
	remaining := l.limit - len(records)
	decision := Decision{
		Limit:     l.limit,
		Remaining: remaining,
	}

	if n <= remaining {
		for i := 0; i < n; i++ {
			records = append(records, now)
		}
		state.records = records
		decision.Allowed = true
		decision.Remaining = l.limit - len(records)
		decision.ResetAfter = l.resetAfter(records, now)
		return decision
	}

	state.records = records
	decision.Remaining = max(0, remaining)
	decision.RetryAfter = l.retryAfter(records, now)
	decision.ResetAfter = decision.RetryAfter
	return decision
}

func (l *SlidingWindow) prune(records []time.Time, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	first := 0
	for first < len(records) && !records[first].After(cutoff) {
		first++
	}
	return records[first:]
}

func (l *SlidingWindow) retryAfter(records []time.Time, now time.Time) time.Duration {
	if len(records) == 0 {
		return 0
	}

	until := records[0].Add(l.window).Sub(now)
	if until < 0 {
		return 0
	}
	return until
}

func (l *SlidingWindow) resetAfter(records []time.Time, now time.Time) time.Duration {
	if len(records) == 0 {
		return 0
	}

	until := records[len(records)-1].Add(l.window).Sub(now)
	if until < 0 {
		return 0
	}
	return until
}
