package ratelimit

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func BenchmarkTokenBucketSameKey(b *testing.B) {
	benchmarkLimiterSameKey(b, TypeTokenBucket)
}

func BenchmarkTokenBucketManyKeys(b *testing.B) {
	benchmarkLimiterManyKeys(b, TypeTokenBucket)
}

func BenchmarkSlidingWindowSameKey(b *testing.B) {
	benchmarkLimiterSameKey(b, TypeSlidingWindow)
}

func BenchmarkSlidingWindowManyKeys(b *testing.B) {
	benchmarkLimiterManyKeys(b, TypeSlidingWindow)
}

func BenchmarkBucketedSlidingWindowManyKeys(b *testing.B) {
	benchmarkLimiterManyKeys(b, TypeBucketedSlidingWindow)
}

func BenchmarkGCRAManyKeys(b *testing.B) {
	benchmarkLimiterManyKeys(b, TypeGCRA)
}

func benchmarkLimiterSameKey(b *testing.B, limitType Type) {
	limiter := newBenchmarkLimiter(b, limitType)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = limiter.Allow(context.Background(), "agent")
		}
	})
}

func benchmarkLimiterManyKeys(b *testing.B, limitType Type) {
	limiter := newBenchmarkLimiter(b, limitType)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_, _ = limiter.Allow(context.Background(), fmt.Sprintf("agent-%d", i%1024))
			i++
		}
	})
}

func newBenchmarkLimiter(b *testing.B, limitType Type) Limiter {
	b.Helper()

	config := Config{
		Type:        limitType,
		Rate:        1_000_000,
		Burst:       1_000_000,
		Window:      time.Second,
		Shards:      256,
		BucketCount: 12,
	}
	limiter, err := NewLimiter(config)
	if err != nil {
		b.Fatalf("new limiter: %v", err)
	}
	return limiter
}
