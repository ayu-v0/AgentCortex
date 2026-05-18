# 限流器使用与扩展

`pkg/ratelimit` 提供按 key 限流的公共接口，当前实现为单进程内存限流，不依赖 Redis、数据库或其他外部存储。

## 快速开始

```go
package main

import (
	"errors"
	"net/http"
	"time"

	"github.com/ayu-v0/agent-cortex/pkg/ratelimit"
)

func main() {
	limiter, err := ratelimit.NewLimiter(ratelimit.Config{
		Type:   ratelimit.TypeTokenBucket,
		Rate:   100,
		Burst:  200,
		Window: time.Second,
	})
	if err != nil {
		panic(err)
	}

	http.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		key := r.RemoteAddr
		decision, err := limiter.Allow(r.Context(), key)
		if err != nil {
			if errors.Is(err, ratelimit.ErrLimitStateFull) {
				http.Error(w, "rate limit state is full", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "rate limit error", http.StatusInternalServerError)
			return
		}

		if !decision.Allowed {
			w.Header().Set("Retry-After", decision.RetryAfter.String())
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	_ = http.ListenAndServe(":8080", nil)
}
```

## 公共接口

```go
type Limiter interface {
	Allow(ctx context.Context, key string) (Decision, error)
	AllowN(ctx context.Context, key string, n int) (Decision, error)
	Reset(key string)
}
```

- `Allow`：消耗 1 个请求额度。
- `AllowN`：一次消耗 `n` 个请求额度，适合批量操作或按成本限流。
- `Reset`：清理指定 key 的限流状态。
- `key`：限流维度，常见取值包括用户 ID、租户 ID、API key、IP、路由名组合等。

如果调用方需要 context-aware reset，可以判断可选接口：

```go
if resetter, ok := limiter.(ratelimit.ContextResetter); ok {
	err := resetter.ResetContext(ctx, key)
	_ = err
}
```

## Decision 字段

```go
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	ResetAfter time.Duration
}
```

- `Allowed`：本次请求是否允许。
- `Limit`：当前算法语义下的限制值。
- `Remaining`：本次判断后剩余额度。
- `RetryAfter`：被拒绝后建议等待多久再试。
- `ResetAfter`：当前 key 状态恢复或窗口推进的大致时间。

典型处理：

```go
decision, err := limiter.Allow(ctx, userID)
if err != nil {
	return err
}
if !decision.Allowed {
	return fmt.Errorf("rate limited, retry after %s", decision.RetryAfter)
}
```

## 内置算法

### 令牌桶

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Config{
	Type:   ratelimit.TypeTokenBucket,
	Rate:   100,
	Burst:  200,
	Window: time.Second,
})
```

语义：

- `Rate`：每个 `Window` 补充的令牌数。
- `Burst`：桶容量，也是允许瞬时突发的最大额度。
- `Window <= 0` 时默认使用 `time.Second`。

适合：

- 允许短时间突发流量。
- 对请求速率做平滑控制。
- 常规 API 限流。

### 精确滑动窗口

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Config{
	Type:   ratelimit.TypeSlidingWindow,
	Rate:   100,
	Window: time.Minute,
})
```

语义：

- `Rate`：窗口内最多允许的请求数。
- `Window`：滑动窗口大小。
- 内部保存窗口内每次请求时间戳。

适合：

- QPS 不高但希望窗口语义精确。
- 需要严格统计最近一个窗口内请求数。

注意：

- 高 QPS 下内存开销随窗口内请求数增长。
- 高流量场景优先考虑分桶滑动窗口或 GCRA。

### 分桶滑动窗口

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Config{
	Type:        ratelimit.TypeBucketedSlidingWindow,
	Rate:        1000,
	Window:      time.Minute,
	BucketCount: 12,
})
```

语义：

- `Rate`：窗口内最多允许的请求数。
- `Window`：总窗口大小。
- `BucketCount`：窗口拆分桶数，默认 12。
- 每个桶只保存计数，不保存每次请求时间戳。

适合：

- 高 QPS。
- 大量 key。
- 可以接受近似滑动窗口语义。

取舍：

- 内存复杂度更稳定。
- 精度受 `BucketCount` 影响，桶越多越接近精确滑动窗口，但统计成本也更高。

### GCRA

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Config{
	Type:   ratelimit.TypeGCRA,
	Rate:   100,
	Burst:  100,
	Window: time.Second,
})
```

语义：

- `Rate`：每个 `Window` 允许的请求数。
- `Burst`：允许的突发容量；如果 `Burst <= 0`，默认等于 `Rate`。
- 每个 key 只维护理论到达时间，状态很小。

适合：

- 高 QPS。
- 大量 key。
- 希望每个 key 状态保持常数级。

注意：

- GCRA 的行为不是滑动窗口语义。
- 如果业务强依赖“最近 N 秒最多 M 次”，使用滑动窗口更直观。

## 配置说明

```go
type Config struct {
	Type            ratelimit.Type
	Rate            int
	Burst           int
	Window          time.Duration
	Clock           clock.Clock
	Shards          int
	IdleTTL         time.Duration
	MaxKeys         int
	StateFullPolicy ratelimit.StateFullPolicy
	BucketCount     int
	Metrics         ratelimit.Metrics
	Metadata        map[string]any
}
```

字段说明：

- `Type`：限流算法类型。
- `Rate`：单位窗口额度，大多数算法要求 `Rate > 0`。
- `Burst`：突发容量，令牌桶必填，GCRA 可选，滑动窗口忽略。
- `Window`：限流窗口。
- `Clock`：时间源，默认使用 `clock.System{}`。
- `Shards`：状态分片数量，默认 256，用于降低多 key 并发锁竞争。
- `IdleTTL`：key 空闲多久后可被惰性清理，默认 `10 * Window`。
- `MaxKeys`：最大活跃 key 数，`<= 0` 表示不限制。
- `StateFullPolicy`：状态满时的新 key 处理策略。
- `BucketCount`：分桶滑动窗口的桶数。
- `Metrics`：观测 hook，不配置时使用 no-op。
- `Metadata`：留给自定义工厂使用。

## 状态容量保护

当 `MaxKeys > 0` 且活跃 key 数达到上限时，组件会先尝试清理过期 key。

如果清理后仍然满，根据 `StateFullPolicy` 处理：

- `RejectNewKey`：拒绝新 key，并返回 `ErrLimitStateFull`。
- `AllowNewKey`：放行新 key，但不记录状态。

示例：

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Config{
	Type:            ratelimit.TypeSlidingWindow,
	Rate:            100,
	Window:          time.Minute,
	MaxKeys:         10000,
	StateFullPolicy: ratelimit.RejectNewKey,
})
```

错误处理：

```go
decision, err := limiter.Allow(ctx, key)
if errors.Is(err, ratelimit.ErrLimitStateFull) {
	// 保护内存优先，可以返回 503 或切换业务降级逻辑。
}
_ = decision
```

## Metrics 扩展

实现 `ratelimit.Metrics` 即可接收观测事件：

```go
type Metrics interface {
	ObserveDecision(algorithm ratelimit.Type, key string, decision ratelimit.Decision)
	ObserveError(algorithm ratelimit.Type, key string, err error)
	ObserveLatency(algorithm ratelimit.Type, operation string, duration time.Duration)
	ObserveState(algorithm ratelimit.Type, activeKeys int)
}
```

示例：

```go
type myMetrics struct{}

func (myMetrics) ObserveDecision(algorithm ratelimit.Type, key string, decision ratelimit.Decision) {
	// 记录 allowed / denied。
}

func (myMetrics) ObserveError(algorithm ratelimit.Type, key string, err error) {
	// 记录错误。
}

func (myMetrics) ObserveLatency(algorithm ratelimit.Type, operation string, duration time.Duration) {
	// 记录耗时。
}

func (myMetrics) ObserveState(algorithm ratelimit.Type, activeKeys int) {
	// 记录活跃 key 数。
}
```

使用：

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Config{
	Type:    ratelimit.TypeTokenBucket,
	Rate:    100,
	Burst:   100,
	Window:  time.Second,
	Metrics: myMetrics{},
})
```

metrics hook 内部 panic 会被限流组件恢复，不会影响主流程。

## 测试时钟

`Config.Clock` 使用公共接口 `pkg/clock.Clock`：

```go
type Clock interface {
	Now() time.Time
}
```

生产环境不需要配置，默认使用 `clock.System{}`。

测试中可以传入可控时钟：

```go
type fakeClock struct {
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func example() {
	c := &fakeClock{now: time.Unix(0, 0)}
	limiter, err := ratelimit.NewLimiter(ratelimit.Config{
		Type:   ratelimit.TypeTokenBucket,
		Rate:   1,
		Burst:  1,
		Window: time.Second,
		Clock:  c,
	})
	_ = limiter
	_ = err

	c.Advance(time.Second)
}
```

## 自定义限流算法

自定义算法需要实现 `Limiter`，并注册一个 `Factory`。

```go
type fixedLimiter struct {
	limit int
}

func (l *fixedLimiter) Allow(ctx context.Context, key string) (ratelimit.Decision, error) {
	return l.AllowN(ctx, key, 1)
}

func (l *fixedLimiter) AllowN(ctx context.Context, key string, n int) (ratelimit.Decision, error) {
	if err := ctx.Err(); err != nil {
		return ratelimit.Decision{}, err
	}
	return ratelimit.Decision{
		Allowed:   true,
		Limit:     l.limit,
		Remaining: l.limit,
	}, nil
}

func (l *fixedLimiter) Reset(key string) {}
```

注册到默认 registry：

```go
const TypeFixed ratelimit.Type = "fixed"

err := ratelimit.Register(TypeFixed, func(config ratelimit.Config) (ratelimit.Limiter, error) {
	if config.Rate <= 0 {
		return nil, ratelimit.ErrInvalidConfig
	}
	return &fixedLimiter{limit: config.Rate}, nil
})
```

使用：

```go
limiter, err := ratelimit.NewLimiter(ratelimit.Config{
	Type: TypeFixed,
	Rate: 100,
})
```

如果不希望污染全局默认 registry，可以使用独立 registry：

```go
registry := ratelimit.NewRegistry()

err := registry.Register(TypeFixed, func(config ratelimit.Config) (ratelimit.Limiter, error) {
	return &fixedLimiter{limit: config.Rate}, nil
})
if err != nil {
	panic(err)
}

limiter, err := registry.NewLimiter(ratelimit.Config{
	Type: TypeFixed,
	Rate: 100,
})
_ = limiter
_ = err
```

## 算法选择建议

| 场景 | 推荐算法 |
| --- | --- |
| 常规 API 限流，允许突发 | `TypeTokenBucket` |
| 需要精确最近窗口统计 | `TypeSlidingWindow` |
| 高 QPS，大量 key，允许近似窗口 | `TypeBucketedSlidingWindow` |
| 高 QPS，大量 key，希望状态极小 | `TypeGCRA` |

## 当前限制

- 当前实现是单进程内存限流。
- 服务重启后限流状态会丢失。
- 多实例部署时不会共享状态，也不保证全局限流。
- 当前阶段不引入 Redis、数据库或其他外部状态后端。
