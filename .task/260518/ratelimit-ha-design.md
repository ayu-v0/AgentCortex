# 限流组件高并发改造方案

## 背景

当前 `pkg/ratelimit` 已具备基础能力：

- 提供统一 `Limiter` 接口。
- 支持 `Allow`、`AllowN`、`Reset`。
- 支持令牌桶和滑动窗口两种内存算法。
- 支持通过 `Registry` 注册新的限流算法。
- 通过 `Clock` 注入支持确定性测试。

当前阶段暂时不引入 Redis 或其他外部存储。改造重点放在单机内存限流的高并发能力、状态生命周期管理、算法效率和可观测性上。

## 当前问题

### 高并发不足

1. 每个 limiter 只有一把全局锁。
   - 不同 key 之间会互相阻塞。
   - 热点 key 会拖慢其他 key。

2. 滑动窗口按请求保存时间戳。
   - 高 QPS 下内存增长快。
   - 每次请求都需要维护时间戳切片。
   - 清理成本随窗口内请求数量增长。

3. key 状态没有生命周期管理。
   - 冷 key 不会自动清理。
   - 长期运行可能造成内存持续增长。

4. 没有性能观测能力。
   - 无法知道允许数、拒绝数、限流耗时、活跃 key 数等指标。

### 可用性不足

当前不引入外部存储，因此这里的可用性目标限定在单进程内存组件内部：

1. 服务重启后限流状态会丢失。
   - 这是内存限流的预期限制。
   - 当前阶段接受该限制。

2. 多实例部署时无法保证全局限流。
   - 每个实例都有自己的限流状态。
   - 实际总限流额度会被实例数量放大。
   - 当前阶段先不解决全局限流。

3. 缺少内部保护机制。
   - 冷 key 过多时可能消耗大量内存。
   - 清理逻辑如果设计不当可能影响请求路径延迟。

## 当前阶段目标

1. 保留当前公共接口的简洁性。
2. 提升单机内存限流在多 key 并发场景下的吞吐。
3. 控制内存增长，避免冷 key 长期驻留。
4. 优化滑动窗口在高 QPS 场景下的内存和 CPU 成本。
5. 增加可观测扩展点，但不绑定具体监控系统。
6. 为未来外部存储预留扩展方向，但本阶段不实现。

## 非目标

当前阶段明确不做：

- 不引入 Redis。
- 不引入数据库或其他外部共享存储。
- 不实现多实例全局限流。
- 不实现跨进程状态同步。
- 不引入复杂熔断器。
- 不引入强依赖的 metrics SDK。

## 目标架构

当前阶段建议聚焦本地内存限流：

```text
Application
    |
    v
Limiter interface
    |
    +-- LocalLimiter
            +-- TokenBucket
            +-- SlidingWindow
            +-- BucketedSlidingWindow
            +-- GCRA
```

核心拆分：

- `Limiter`：面向调用方的统一接口。
- `Registry`：算法注册和创建入口。
- `Clock`：时间抽象，便于测试。
- `ShardedState`：分片状态存储，降低锁竞争。
- `Metrics`：可选观测接口，默认 no-op。

## 公共接口建议

当前接口可以继续保留：

```go
type Limiter interface {
    Allow(ctx context.Context, key string) (Decision, error)
    AllowN(ctx context.Context, key string, n int) (Decision, error)
    Reset(key string)
}
```

短期不建议直接改成 `Reset(ctx context.Context, key string) error`，因为当前阶段只有内存实现，`Reset` 不需要外部 IO，也基本不会失败。

如需为未来外部存储预留兼容空间，可以新增可选接口，而不是破坏现有接口：

```go
type ContextResetter interface {
    ResetContext(ctx context.Context, key string) error
}
```

这样当前调用方不受影响，未来需要外部后端时也有扩展余地。

## 高并发改造

### 1. 分片锁

将当前全局锁改成 shard 级别锁。

建议默认 256 个 shard：

```go
type shardedState[T any] struct {
    shards []stateShard[T]
}

type stateShard[T any] struct {
    mu    sync.Mutex
    items map[string]*T
}
```

key 通过 hash 定位 shard：

```go
idx := fnv32(key) % len(shards)
```

收益：

- 不同 key 分散到不同锁。
- 降低锁竞争。
- 热点 key 只影响所在 shard。

注意：

- 单个热点 key 仍然会串行，这是正确的，因为同一 key 的限流状态必须原子更新。
- shard 数量需要可配置，默认值不能过小。

建议配置：

```go
type Config struct {
    Shards int
}
```

默认规则：

- `Shards <= 0` 时使用 256。
- 如果配置不是 2 的幂，也允许使用，不强制报错。

### 2. 状态 TTL 与清理

为每个 key 状态维护 `lastSeen`。

清理策略：

- 惰性清理：访问 shard 时顺带清理少量过期 key。
- 可选后台清理：如果后续发现冷 key 积累明显，再增加后台扫描。

优先建议先做惰性清理，避免引入 goroutine 生命周期管理复杂度。

配置建议：

```go
type Config struct {
    IdleTTL time.Duration
}
```

默认值：

- `IdleTTL <= 0` 时使用 `10 * Window`。
- 如果 `Window <= 0`，使用 `10 * time.Second` 作为兜底。

清理要求：

- 清理逻辑不能每次全量扫描 shard。
- 可以每次访问最多清理固定数量，例如 16 个 key。
- 清理不能影响限流判断的正确性。

### 3. 滑动窗口优化

当前精确滑动窗口保存每次请求时间戳，不适合高 QPS。

建议提供两种滑动窗口实现：

1. 精确滑动窗口。
   - 保留当前行为。
   - 适合低 QPS、要求精确的场景。

2. 分桶滑动窗口。
   - 将窗口拆成多个小桶。
   - 每个桶只保存计数。
   - 内存复杂度从 O(requests) 降到 O(keys * buckets)。

示例：

```text
Window = 60s
BucketCount = 12
BucketSize = 5s
```

判断时统计最近 12 个桶的总计数。

建议新增类型：

```go
const TypeBucketedSlidingWindow Type = "bucketed_sliding_window"
```

配置建议：

```go
type Config struct {
    BucketCount int
}
```

默认规则：

- `BucketCount <= 0` 时使用 10 或 12。
- `BucketCount` 不宜过大，否则统计成本升高。

### 4. 增加 GCRA 算法

建议新增 GCRA(Generic Cell Rate Algorithm)。

优势：

- 状态极少。
- 每个 key 通常只需要保存一个理论到达时间。
- 高并发下比精确滑动窗口更轻。
- 很适合作为本地内存限流的生产默认算法候选。

建议新增类型：

```go
const TypeGCRA Type = "gcra"
```

GCRA 适用场景：

- 高 QPS。
- 大量 key。
- 希望状态常数级。
- 可以接受与滑动窗口不同的限流语义。

## 可用性改造

### 1. 内存保护

当前阶段不做分布式高可用，优先保证单进程内存组件不会被状态膨胀拖垮。

建议增加：

```go
type Config struct {
    MaxKeys int
}
```

策略：

- `MaxKeys <= 0` 表示不限制。
- 达到上限时优先清理过期 key。
- 清理后仍超限时，可以拒绝新 key 或返回明确错误。

建议错误：

```go
var ErrLimitStateFull = errors.New("rate limit state is full")
```

默认建议：

- 基础版本先不强制启用 `MaxKeys`。
- 对外暴露配置能力，业务方按场景开启。

### 2. 降级策略

由于当前没有外部后端，暂不需要 fail-open / fail-closed 这类后端失败策略。

可以预留轻量内部策略：

```go
type StateFullPolicy int

const (
    RejectNewKey StateFullPolicy = iota
    AllowNewKey
)
```

含义：

- `RejectNewKey`: 状态满时拒绝新 key，保护内存。
- `AllowNewKey`: 状态满时放行新 key，但不记录状态，保护可用性。

该策略只处理本地状态容量问题，不处理外部后端故障。

## 可观测性

建议新增 metrics hook：

```go
type Metrics interface {
    ObserveDecision(algorithm Type, key string, decision Decision)
    ObserveError(algorithm Type, key string, err error)
    ObserveLatency(algorithm Type, operation string, duration time.Duration)
    ObserveState(algorithm Type, activeKeys int)
}
```

需要观测的指标：

- allowed count
- denied count
- limiter error count
- retry after distribution
- limiter latency
- active keys
- cleanup count
- state full count

默认提供 no-op metrics，避免强依赖具体监控系统。

## 配置模型建议

建议扩展当前 `Config`：

```go
type Config struct {
    Type           Type
    Rate           int
    Burst          int
    Window         time.Duration
    Clock          Clock
    Shards         int
    IdleTTL        time.Duration
    MaxKeys        int
    StateFullPolicy StateFullPolicy
    BucketCount    int
    Metrics        Metrics
    Metadata       map[string]any
}
```

默认规则：

- `Shards <= 0` 时使用 256。
- `Clock == nil` 时使用系统时间。
- `IdleTTL <= 0` 时使用 `10 * Window`。
- `MaxKeys <= 0` 时不限制 key 数。
- `BucketCount <= 0` 时使用算法默认值。
- `Metrics == nil` 时使用 no-op metrics。

## 分阶段落地计划

### 阶段 1：分片锁与配置扩展

目标：先解决不同 key 之间的全局锁竞争。

任务：

1. 增加 `Shards` 配置。
2. 新增内部 `shardedState`。
3. 将 token bucket 改成按 shard 加锁。
4. 将 sliding window 改成按 shard 加锁。
5. 增加多 key 并发测试。

验收：

- `go test ./pkg/ratelimit` 通过。
- `go test -race ./pkg/ratelimit` 通过。
- benchmark 中多 key 并发吞吐高于全局锁版本。

### 阶段 2：状态 TTL 与内存保护

目标：避免冷 key 长期驻留和状态无限增长。

任务：

1. 为每个 key 状态增加 `lastSeen`。
2. 增加 `IdleTTL` 配置。
3. 实现惰性清理。
4. 增加 `MaxKeys` 配置。
5. 增加状态满时的处理策略。

验收：

- 冷 key 超过 TTL 后可被清理。
- 清理逻辑不会破坏限流判断。
- `MaxKeys` 生效。
- 状态满时行为符合配置。

### 阶段 3：算法增强

目标：减少高 QPS 场景的内存占用。

任务：

1. 增加分桶滑动窗口。
2. 增加 GCRA。
3. 明确各算法适用场景。
4. 增加边界测试。
5. 增加 benchmark。

验收：

- 高 QPS 下分桶滑动窗口内存使用稳定。
- GCRA 每个 key 状态保持常数级。
- 各算法的 `AllowN`、`Reset` 和 key 隔离行为一致。

### 阶段 4：可观测性

目标：提供生产排查需要的基础观测能力。

任务：

1. 增加 `Metrics` 接口。
2. 提供 no-op 实现。
3. 在 allow、deny、error、cleanup、state full 处打点。
4. 增加 metrics 调用测试。

验收：

- 不配置 metrics 时无额外依赖。
- 配置 metrics 后可以观察允许、拒绝、错误和延迟。
- metrics 异常不影响限流主流程。

## 测试计划

### 单元测试

- 配置校验。
- allow / deny 行为。
- `AllowN` 扣减。
- `Reset` 清理状态。
- 不同 key 独立限流。
- 时间推进边界。
- TTL 清理。
- `MaxKeys` 行为。

### 并发测试

- 多 goroutine 同 key。
- 多 goroutine 多 key。
- 高频 `AllowN`。
- `Reset` 与 `Allow` 并发。
- 惰性清理与 `Allow` 并发。

### Race 测试

```powershell
go test -race ./pkg/ratelimit
```

### Benchmark

建议增加：

```go
BenchmarkTokenBucketSameKey
BenchmarkTokenBucketManyKeys
BenchmarkSlidingWindowSameKey
BenchmarkSlidingWindowManyKeys
BenchmarkBucketedSlidingWindowManyKeys
BenchmarkGCRAManyKeys
```

## 风险与取舍

1. 分片锁能提升不同 key 并发，但不能解决单热点 key 的串行问题。
2. 惰性清理避免后台 goroutine 复杂度，但冷 key 只有在后续访问时才会被清理。
3. 分桶滑动窗口牺牲一定精度，换取更低内存和更高吞吐。
4. GCRA 状态更小，但语义不同于滑动窗口，需要在文档中说明。
5. `MaxKeys` 可以保护内存，但状态满时选择拒绝或放行都需要业务侧明确取舍。
6. 不引入外部存储意味着当前阶段不保证多实例全局限流。

## 推荐优先级

建议优先按以下顺序推进：

1. 分片锁。
2. key TTL 清理。
3. `MaxKeys` 内存保护。
4. benchmark 和 race test。
5. 分桶滑动窗口。
6. GCRA。
7. metrics hook。

这样可以先解决单机高并发瓶颈和内存增长风险，同时保持组件轻量，不引入 Redis 或其他外部依赖。

## 未来预留

如果后续确实需要多实例全局限流，再单独设计外部状态后端。

届时可以考虑：

- 新增 `Store` 抽象。
- 新增外部后端实现。
- 调整 `Reset` 为 context-aware 形式。
- 增加后端失败策略。
- 增加跨实例一致性测试。

这些内容不属于当前阶段。
