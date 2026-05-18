package ratelimit

import "time"

// Metrics receives optional observations from limiter operations.
type Metrics interface {
	ObserveDecision(algorithm Type, key string, decision Decision)
	ObserveError(algorithm Type, key string, err error)
	ObserveLatency(algorithm Type, operation string, duration time.Duration)
	ObserveState(algorithm Type, activeKeys int)
}

type noopMetrics struct{}

func (noopMetrics) ObserveDecision(Type, string, Decision) {}

func (noopMetrics) ObserveError(Type, string, error) {}

func (noopMetrics) ObserveLatency(Type, string, time.Duration) {}

func (noopMetrics) ObserveState(Type, int) {}

func observeDecision(metrics Metrics, algorithm Type, key string, decision Decision) {
	defer recoverMetrics()
	metrics.ObserveDecision(algorithm, key, decision)
}

func observeError(metrics Metrics, algorithm Type, key string, err error) {
	defer recoverMetrics()
	metrics.ObserveError(algorithm, key, err)
}

func observeLatency(metrics Metrics, algorithm Type, operation string, duration time.Duration) {
	defer recoverMetrics()
	metrics.ObserveLatency(algorithm, operation, duration)
}

func observeState(metrics Metrics, algorithm Type, activeKeys int) {
	defer recoverMetrics()
	metrics.ObserveState(algorithm, activeKeys)
}

func recoverMetrics() {
	_ = recover()
}
