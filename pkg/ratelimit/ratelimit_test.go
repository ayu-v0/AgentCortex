package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(duration time.Duration) {
	c.now = c.now.Add(duration)
}

func TestNewLimiterCreatesBuiltInLimiters(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}

	tokenBucket, err := NewLimiter(Config{
		Type:   TypeTokenBucket,
		Rate:   10,
		Burst:  20,
		Window: time.Second,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new token bucket: %v", err)
	}
	if tokenBucket == nil {
		t.Fatal("expected token bucket limiter")
	}

	slidingWindow, err := NewLimiter(Config{
		Type:   TypeSlidingWindow,
		Rate:   10,
		Window: time.Minute,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new sliding window: %v", err)
	}
	if slidingWindow == nil {
		t.Fatal("expected sliding window limiter")
	}

	bucketedWindow, err := NewLimiter(Config{
		Type:   TypeBucketedSlidingWindow,
		Rate:   10,
		Window: time.Minute,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new bucketed sliding window: %v", err)
	}
	if bucketedWindow == nil {
		t.Fatal("expected bucketed sliding window limiter")
	}

	gcra, err := NewLimiter(Config{
		Type:   TypeGCRA,
		Rate:   10,
		Burst:  10,
		Window: time.Minute,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new gcra: %v", err)
	}
	if gcra == nil {
		t.Fatal("expected gcra limiter")
	}
}

func TestNewLimiterRejectsUnknownType(t *testing.T) {
	_, err := NewLimiter(Config{Type: "missing"})
	if !errors.Is(err, ErrUnknownType) {
		t.Fatalf("expected ErrUnknownType, got %v", err)
	}
}

func TestRegistrySupportsCustomLimiter(t *testing.T) {
	registry := NewRegistry()
	err := registry.Register("custom", func(Config) (Limiter, error) {
		return &stubLimiter{}, nil
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	limiter, err := registry.NewLimiter(Config{Type: "custom"})
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	if _, ok := limiter.(*stubLimiter); !ok {
		t.Fatalf("expected custom limiter, got %T", limiter)
	}
}

func TestTokenBucketAllowsBurstThenRefills(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewTokenBucket(Config{
		Rate:   2,
		Burst:  2,
		Window: time.Second,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new token bucket: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 1)
	assertAllowed(t, limiter, "agent-1", 0)

	decision, err := limiter.Allow(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected third request to be denied")
	}
	if decision.RetryAfter != 500*time.Millisecond {
		t.Fatalf("expected retry after 500ms, got %s", decision.RetryAfter)
	}

	clock.Advance(500 * time.Millisecond)
	assertAllowed(t, limiter, "agent-1", 0)
}

func TestTokenBucketLimitsKeysIndependently(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewTokenBucket(Config{
		Rate:   1,
		Burst:  1,
		Window: time.Second,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new token bucket: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 0)
	assertAllowed(t, limiter, "agent-2", 0)
}

func TestSlidingWindowDeniesUntilOldRecordsExpire(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewSlidingWindow(Config{
		Rate:   2,
		Window: time.Second,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new sliding window: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 1)
	assertAllowed(t, limiter, "agent-1", 0)

	decision, err := limiter.Allow(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected request to be denied")
	}
	if decision.RetryAfter != time.Second {
		t.Fatalf("expected retry after 1s, got %s", decision.RetryAfter)
	}

	clock.Advance(time.Second)
	assertAllowed(t, limiter, "agent-1", 1)
}

func TestLimiterResetClearsState(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewSlidingWindow(Config{
		Rate:   1,
		Window: time.Minute,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new sliding window: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 0)
	limiter.Reset("agent-1")
	assertAllowed(t, limiter, "agent-1", 0)
}

func TestBucketedSlidingWindowDeniesUntilBucketExpires(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewBucketedSlidingWindow(Config{
		Rate:        2,
		Window:      time.Second,
		BucketCount: 2,
		Clock:       clock,
	})
	if err != nil {
		t.Fatalf("new bucketed sliding window: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 1)
	assertAllowed(t, limiter, "agent-1", 0)

	decision, err := limiter.Allow(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected request to be denied")
	}
	if decision.RetryAfter != time.Second {
		t.Fatalf("expected retry after 1s, got %s", decision.RetryAfter)
	}

	clock.Advance(time.Second)
	assertAllowed(t, limiter, "agent-1", 1)
}

func TestGCRAAllowsBurstThenSpacesRequests(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewGCRA(Config{
		Rate:   2,
		Burst:  2,
		Window: time.Second,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new gcra: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 1)
	assertAllowed(t, limiter, "agent-1", 0)

	decision, err := limiter.Allow(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected third request to be denied")
	}
	if decision.RetryAfter != 500*time.Millisecond {
		t.Fatalf("expected retry after 500ms, got %s", decision.RetryAfter)
	}

	clock.Advance(500 * time.Millisecond)
	assertAllowed(t, limiter, "agent-1", 0)
}

func TestGCRAAllowNRespectsBurst(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewGCRA(Config{
		Rate:   2,
		Burst:  2,
		Window: time.Second,
		Clock:  clock,
	})
	if err != nil {
		t.Fatalf("new gcra: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 1)
	decision, err := limiter.AllowN(context.Background(), "agent-1", 2)
	if err != nil {
		t.Fatalf("allow n: %v", err)
	}
	if decision.Allowed {
		t.Fatal("expected batch request to be denied")
	}
	if decision.RetryAfter != 500*time.Millisecond {
		t.Fatalf("expected retry after 500ms, got %s", decision.RetryAfter)
	}
}

func TestIdleTTLCleansColdKeys(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewTokenBucket(Config{
		Rate:    1,
		Burst:   1,
		Window:  time.Second,
		Clock:   clock,
		Shards:  1,
		IdleTTL: time.Second,
		MaxKeys: 1,
	})
	if err != nil {
		t.Fatalf("new token bucket: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 0)
	clock.Advance(2 * time.Second)
	assertAllowed(t, limiter, "agent-2", 0)
}

func TestMaxKeysRejectsNewKeysWhenStateIsFull(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewSlidingWindow(Config{
		Rate:    1,
		Window:  time.Minute,
		Clock:   clock,
		MaxKeys: 1,
	})
	if err != nil {
		t.Fatalf("new sliding window: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 0)
	_, err = limiter.Allow(context.Background(), "agent-2")
	if !errors.Is(err, ErrLimitStateFull) {
		t.Fatalf("expected ErrLimitStateFull, got %v", err)
	}
}

func TestMaxKeysCanAllowNewKeysWithoutStoringState(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := NewSlidingWindow(Config{
		Rate:            1,
		Window:          time.Minute,
		Clock:           clock,
		MaxKeys:         1,
		StateFullPolicy: AllowNewKey,
	})
	if err != nil {
		t.Fatalf("new sliding window: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 0)
	assertAllowed(t, limiter, "agent-2", 0)
	assertAllowed(t, limiter, "agent-2", 0)
}

func TestMetricsHooksAreCalled(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	metrics := &recordingMetrics{}
	limiter, err := NewTokenBucket(Config{
		Rate:    1,
		Burst:   1,
		Window:  time.Second,
		Clock:   clock,
		Metrics: metrics,
	})
	if err != nil {
		t.Fatalf("new token bucket: %v", err)
	}

	assertAllowed(t, limiter, "agent-1", 0)
	_, err = limiter.Allow(context.Background(), "agent-1")
	if err != nil {
		t.Fatalf("allow denied request: %v", err)
	}

	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if metrics.decisions != 2 {
		t.Fatalf("expected 2 decisions, got %d", metrics.decisions)
	}
	if metrics.latencies != 2 {
		t.Fatalf("expected 2 latencies, got %d", metrics.latencies)
	}
	if metrics.states == 0 {
		t.Fatal("expected state observations")
	}
}

func TestLimitersAreConcurrentSafeForManyKeys(t *testing.T) {
	limiter, err := NewTokenBucket(Config{
		Rate:   1000,
		Burst:  1000,
		Window: time.Second,
		Shards: 16,
	})
	if err != nil {
		t.Fatalf("new token bucket: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("agent-%d", i)
			for j := 0; j < 100; j++ {
				_, err := limiter.Allow(context.Background(), key)
				if err != nil {
					t.Errorf("allow: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func assertAllowed(t *testing.T, limiter Limiter, key string, remaining int) {
	t.Helper()

	decision, err := limiter.Allow(context.Background(), key)
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if !decision.Allowed {
		t.Fatal("expected request to be allowed")
	}
	if decision.Remaining != remaining {
		t.Fatalf("expected remaining %d, got %d", remaining, decision.Remaining)
	}
}

type stubLimiter struct{}

func (l *stubLimiter) Allow(context.Context, string) (Decision, error) {
	return Decision{Allowed: true}, nil
}

func (l *stubLimiter) AllowN(context.Context, string, int) (Decision, error) {
	return Decision{Allowed: true}, nil
}

func (l *stubLimiter) Reset(string) {}

type recordingMetrics struct {
	mu        sync.Mutex
	decisions int
	errors    int
	latencies int
	states    int
}

func (m *recordingMetrics) ObserveDecision(Type, string, Decision) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.decisions++
}

func (m *recordingMetrics) ObserveError(Type, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors++
}

func (m *recordingMetrics) ObserveLatency(Type, string, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencies++
}

func (m *recordingMetrics) ObserveState(Type, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states++
}
