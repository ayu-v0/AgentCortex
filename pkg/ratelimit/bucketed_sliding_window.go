package ratelimit

import (
	"context"
	"fmt"
	"time"

	clockpkg "github.com/ayu-v0/agent-cortex/pkg/clock"
)

type BucketedSlidingWindow struct {
	limit       int
	window      time.Duration
	bucketCount int
	bucketSize  time.Duration
	clock       clockpkg.Clock
	metrics     Metrics
	state       *shardedState[bucketedSlidingWindowState]
}

var _ Limiter = (*BucketedSlidingWindow)(nil)

type bucketedSlidingWindowState struct {
	buckets map[int64]int
}

// NewBucketedSlidingWindow returns a keyed approximate sliding window limiter.
func NewBucketedSlidingWindow(config Config) (Limiter, error) {
	if config.Rate <= 0 {
		return nil, fmt.Errorf("%w: rate must be positive", ErrInvalidConfig)
	}
	if config.Window <= 0 {
		return nil, fmt.Errorf("%w: window must be positive", ErrInvalidConfig)
	}

	bucketCount := normalizeBucketCount(config.BucketCount)
	bucketSize := config.Window / time.Duration(bucketCount)
	if bucketSize <= 0 {
		bucketSize = time.Nanosecond
	}

	return &BucketedSlidingWindow{
		limit:       config.Rate,
		window:      config.Window,
		bucketCount: bucketCount,
		bucketSize:  bucketSize,
		clock:       normalizeClock(config.Clock),
		metrics:     normalizeMetrics(config.Metrics),
		state:       newShardedState[bucketedSlidingWindowState](config, config.Window),
	}, nil
}

func (l *BucketedSlidingWindow) Allow(ctx context.Context, key string) (Decision, error) {
	return l.AllowN(ctx, key, 1)
}

func (l *BucketedSlidingWindow) AllowN(ctx context.Context, key string, n int) (Decision, error) {
	start := time.Now()
	defer func() {
		observeLatency(l.metrics, TypeBucketedSlidingWindow, "allow", time.Since(start))
	}()

	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeBucketedSlidingWindow, key, err)
		return Decision{}, err
	}
	if err := validateRequestSize(n); err != nil {
		observeError(l.metrics, TypeBucketedSlidingWindow, key, err)
		return Decision{}, err
	}

	now := l.clock.Now()
	decision, err := l.state.WithEntry(
		key,
		now,
		func(time.Time) bucketedSlidingWindowState {
			return bucketedSlidingWindowState{buckets: make(map[int64]int, l.bucketCount)}
		},
		func(state *bucketedSlidingWindowState, ephemeral bool) (Decision, error) {
			return l.allowN(state, now, n), nil
		},
	)
	if err != nil {
		observeError(l.metrics, TypeBucketedSlidingWindow, key, err)
		return decision, err
	}

	observeDecision(l.metrics, TypeBucketedSlidingWindow, key, decision)
	observeState(l.metrics, TypeBucketedSlidingWindow, l.state.ActiveKeys())
	return decision, nil
}

func (l *BucketedSlidingWindow) Reset(key string) {
	if l.state.Reset(key) {
		observeState(l.metrics, TypeBucketedSlidingWindow, l.state.ActiveKeys())
	}
}

func (l *BucketedSlidingWindow) ResetContext(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		observeError(l.metrics, TypeBucketedSlidingWindow, key, err)
		return err
	}
	l.Reset(key)
	return nil
}

func (l *BucketedSlidingWindow) allowN(state *bucketedSlidingWindowState, now time.Time, n int) Decision {
	if state.buckets == nil {
		state.buckets = make(map[int64]int, l.bucketCount)
	}

	current := l.bucketIndex(now)
	l.pruneBuckets(state, current)
	total := 0
	oldest := current
	latest := current
	for idx, count := range state.buckets {
		total += count
		if idx < oldest {
			oldest = idx
		}
		if idx > latest {
			latest = idx
		}
	}

	remaining := l.limit - total
	decision := Decision{
		Limit:     l.limit,
		Remaining: max(0, remaining),
	}

	if n <= remaining {
		state.buckets[current] += n
		total += n
		decision.Allowed = true
		decision.Remaining = l.limit - total
		decision.ResetAfter = l.bucketExpires(current).Sub(now)
		if decision.ResetAfter < 0 {
			decision.ResetAfter = 0
		}
		return decision
	}

	decision.RetryAfter = l.bucketExpires(oldest).Sub(now)
	if decision.RetryAfter < 0 {
		decision.RetryAfter = 0
	}
	decision.ResetAfter = l.bucketExpires(latest).Sub(now)
	if decision.ResetAfter < 0 {
		decision.ResetAfter = 0
	}
	return decision
}

func (l *BucketedSlidingWindow) pruneBuckets(state *bucketedSlidingWindowState, current int64) {
	min := current - int64(l.bucketCount) + 1
	for idx := range state.buckets {
		if idx < min || idx > current {
			delete(state.buckets, idx)
		}
	}
}

func (l *BucketedSlidingWindow) bucketIndex(t time.Time) int64 {
	return t.UnixNano() / l.bucketSize.Nanoseconds()
}

func (l *BucketedSlidingWindow) bucketExpires(idx int64) time.Time {
	return time.Unix(0, idx*l.bucketSize.Nanoseconds()).Add(l.window)
}
