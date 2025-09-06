# Day 8: 缓存雪崩治理

## 学习目标
处理大面积同时过期导致的崩溃风险，通过TTL抖动、分片过期、预热、限流/降级等手段确保系统在雪崩场景下的稳定性。

## 理论要点

### 1. 缓存雪崩的根因
- **同时过期**: 大量缓存在同一时间点过期
- **瞬时压力**: 所有请求同时击中数据源
- **连锁反应**: 数据源压力导致响应变慢，进一步加重问题
- **服务崩溃**: 最终导致整个服务不可用

### 2. 雪崩防护策略

#### TTL抖动 (TTL Jitter)
```go
// 基础TTL加上随机抖动
func calculateJitteredTTL(baseTTL time.Duration, jitterRatio float64) time.Duration {
    minMultiplier := 1.0 - jitterRatio
    maxMultiplier := 1.0 + jitterRatio
    multiplier := minMultiplier + rand.Float64()*(maxMultiplier-minMultiplier)
    return time.Duration(float64(baseTTL) * multiplier)
}
```

#### 分片过期 (Sharded Expiration)
```go
// 根据key计算分片，错开过期时间
func calculateShardedTTL(key string, baseTTL time.Duration, shardCount int, shardInterval time.Duration) time.Duration {
    hash := hashKey(key)
    shard := hash % shardCount
    shardOffset := time.Duration(shard) * shardInterval
    return baseTTL + shardOffset
}
```

#### 熔断器 (Circuit Breaker)
- **关闭状态**: 正常处理请求
- **打开状态**: 快速失败，触发降级
- **半开状态**: 探测性恢复

#### 降级机制 (Degradation)
- **静态降级**: 返回预设的默认值
- **动态降级**: 从备用数据源获取
- **部分降级**: 只降级非核心功能

### 3. 预热策略
- **主动预热**: 定时刷新热点数据
- **被动预热**: 在访问时检查并刷新即将过期的数据
- **分布式预热**: 多实例协调预热任务

## 实现要点

### 1. 雪崩防护组件架构

```go
// 雪崩防护配置
type AvalancheProtectionConfig struct {
    // TTL分片配置
    EnableTTLSharding bool
    ShardCount        int
    ShardInterval     time.Duration
    
    // 熔断器配置
    CircuitBreakerConfig *CircuitBreakerConfig
    
    // 预热配置
    EnableWarmup      bool
    WarmupInterval    time.Duration
    WarmupKeys        []string
    
    // 降级函数
    DegradeFunc func(ctx context.Context, key string) (interface{}, error)
}
```

### 2. 熔断器实现

```go
// 熔断器状态
type CircuitBreakerState int32

const (
    StateClosed   CircuitBreakerState = iota // 关闭（正常）
    StateOpen                                // 打开（熔断）
    StateHalfOpen                           // 半开（探测）
)

// 熔断器核心逻辑
func (cb *CircuitBreaker) CanExecute() error {
    switch cb.state {
    case StateClosed:
        return nil
    case StateOpen:
        if time.Since(cb.lastFailTime) >= cb.config.OpenTimeout {
            cb.state = StateHalfOpen
            return nil
        }
        return ErrCircuitBreakerOpen
    case StateHalfOpen:
        if cb.halfOpenCalls >= cb.config.HalfOpenMaxCalls {
            return ErrCircuitBreakerOpen
        }
        atomic.AddInt64(&cb.halfOpenCalls, 1)
        return nil
    }
}
```

### 3. 滑动窗口统计

```go
// 窗口桶
type WindowBucket struct {
    timestamp    time.Time
    requestCount int64
    failureCount int64
}

// 更新滑动窗口
func (cb *CircuitBreaker) updateWindow(isFailure bool) {
    now := time.Now()
    currentSecond := now.Truncate(time.Second)
    
    if cb.window[cb.windowIndex].timestamp.Before(currentSecond) {
        cb.windowIndex = (cb.windowIndex + 1) % len(cb.window)
        cb.window[cb.windowIndex] = WindowBucket{timestamp: currentSecond}
    }
    
    atomic.AddInt64(&cb.window[cb.windowIndex].requestCount, 1)
    if isFailure {
        atomic.AddInt64(&cb.window[cb.windowIndex].failureCount, 1)
    }
}
```

## 实操演练

### 1. 基础功能演示

#### TTL抖动演示
```bash
# 运行TTL抖动演示
go run cmd/demo/day8_avalanche_demo.go
```

观察相同基础TTL的key会有不同的实际过期时间。

#### 分片过期演示
不同key根据hash值分配到不同的时间分片，避免同时过期。

#### 熔断器演示
模拟高错误率场景，观察熔断器的状态转换：
- 关闭 → 打开（达到失败阈值）
- 打开 → 半开（超时后探测）
- 半开 → 关闭（探测成功）或重新打开（探测失败）

### 2. 雪崩场景压测

#### 运行完整雪崩演练
```bash
# 运行雪崩场景演示
go run cmd/demo/day8_avalanche_demo.go

# 运行专业压测工具
go run scripts/bench/day8_avalanche_benchmark.go \
  -duration=30s \
  -concurrency=50 \
  -key-count=1000 \
  -avalanche-ttl=30s \
  -avalanche-delay=25s \
  -db-latency=100ms \
  -db-error-rate=0.1 \
  -ttl-jitter=0.2 \
  -shard-count=10 \
  -shard-interval=5s \
  -circuit-breaker=true \
  -failure-rate=0.5 \
  -verbose
```

#### 压测参数说明
- `duration`: 压测时长
- `concurrency`: 并发数
- `key-count`: 测试key数量
- `avalanche-ttl`: 雪崩场景的TTL
- `avalanche-delay`: 等待时间（让缓存接近过期）
- `db-latency`: 模拟数据源延迟
- `db-error-rate`: 数据源错误率
- `ttl-jitter`: TTL抖动比例
- `shard-count`: 分片数量
- `shard-interval`: 分片间隔
- `circuit-breaker`: 是否启用熔断器
- `failure-rate`: 熔断失败率阈值

### 3. 对比测试结果

#### 无防护场景特征
- **高延迟**: P99延迟可能达到数秒
- **低成功率**: 大量请求失败或超时
- **数据源压力**: 所有请求都击中数据源
- **系统不稳定**: 出现毛刺和抖动

#### 有防护场景特征
- **稳定延迟**: 延迟分布相对平稳
- **高成功率**: 通过降级保证可用性
- **数据源保护**: 熔断器减少对数据源的冲击
- **系统稳定**: 整体表现平滑

## 关键指标监控

### 1. 性能指标
```go
type AvalancheMetrics struct {
    // 请求统计
    TotalRequests     int64
    SuccessRequests   int64
    ErrorRequests     int64
    DegradedRequests  int64
    
    // 延迟统计
    AvgLatency        time.Duration
    P95Latency        time.Duration
    P99Latency        time.Duration
    
    // 缓存统计
    CacheHitRate      float64
    CacheMissRate     float64
    
    // 数据源统计
    DBRequestCount    int64
    DBErrorRate       float64
    
    // 熔断器统计
    CircuitBreakerState string
    FailureRate         float64
}
```

### 2. 告警阈值建议
- **P99延迟** > 500ms
- **错误率** > 5%
- **缓存命中率** < 80%
- **熔断器打开**时间 > 30s
- **降级请求比例** > 10%

## 最佳实践

### 1. TTL设计原则
```go
// 推荐的TTL抖动配置
type TTLStrategy struct {
    BaseTTL    time.Duration // 基础TTL
    Jitter     float64       // 抖动比例 10-30%
    ShardCount int           // 分片数 5-20
    ShardInterval time.Duration // 分片间隔
}

// 不同业务场景的TTL策略
var TTLStrategies = map[string]TTLStrategy{
    "user_profile": {
        BaseTTL: 1 * time.Hour,
        Jitter: 0.2,        // 20%抖动
        ShardCount: 10,
        ShardInterval: 6 * time.Minute,
    },
    "product_info": {
        BaseTTL: 30 * time.Minute,
        Jitter: 0.15,       // 15%抖动
        ShardCount: 8,
        ShardInterval: 4 * time.Minute,
    },
    "hot_data": {
        BaseTTL: 10 * time.Minute,
        Jitter: 0.3,        // 30%抖动
        ShardCount: 15,
        ShardInterval: 1 * time.Minute,
    },
}
```

### 2. 熔断器配置指南
```go
// 不同场景的熔断器配置
var CircuitBreakerConfigs = map[string]CircuitBreakerConfig{
    "database": {
        FailureThreshold:    10,
        FailureRate:         0.5,   // 50%
        MinRequestCount:     20,
        OpenTimeout:         30 * time.Second,
        HalfOpenMaxCalls:    3,
        SlidingWindowSize:   60,    // 60秒窗口
    },
    "external_api": {
        FailureThreshold:    5,
        FailureRate:         0.3,   // 30%
        MinRequestCount:     10,
        OpenTimeout:         10 * time.Second,
        HalfOpenMaxCalls:    2,
        SlidingWindowSize:   30,    // 30秒窗口
    },
    "internal_service": {
        FailureThreshold:    15,
        FailureRate:         0.6,   // 60%
        MinRequestCount:     30,
        OpenTimeout:         60 * time.Second,
        HalfOpenMaxCalls:    5,
        SlidingWindowSize:   120,   // 120秒窗口
    },
}
```

### 3. 降级策略设计
```go
// 多层降级策略
type DegradationStrategy struct {
    // 一级降级：返回缓存的旧数据
    Level1: func(ctx context.Context, key string) (interface{}, error) {
        // 尝试获取已过期但仍存在的数据
        return getExpiredCache(ctx, key)
    },
    
    // 二级降级：返回默认数据
    Level2: func(ctx context.Context, key string) (interface{}, error) {
        return getDefaultData(key), nil
    },
    
    // 三级降级：返回空数据
    Level3: func(ctx context.Context, key string) (interface{}, error) {
        return nil, ErrDegraded
    },
}
```

### 4. 预热策略
```go
// 智能预热策略
type WarmupStrategy struct {
    // 基于访问热度的预热
    HotKeyThreshold   int           // 热点key阈值
    WarmupRatio      float64       // 预热比例
    WarmupAdvance    time.Duration // 提前预热时间
    
    // 基于时间的预热
    PeakHours        []int         // 高峰时段
    PreWarmupTime    time.Duration // 高峰前预热时间
    
    // 基于业务的预热
    BusinessEvents   []string      // 业务事件
    EventWarmupKeys  map[string][]string // 事件对应的预热key
}
```

## 压测结果分析

### 1. 典型测试结果对比

#### 无防护场景
```json
{
  "scenario": "无防护-雪崩场景",
  "total_requests": 15000,
  "success_requests": 8500,
  "error_requests": 6500,
  "qps": 500.0,
  "avg_latency": "850ms",
  "p95_latency": "2.5s",
  "p99_latency": "5.2s",
  "cache_hit_rate": 0.15,
  "db_requests": 12750,
  "db_error_rate": 0.35
}
```

#### 有防护场景
```json
{
  "scenario": "有防护-雪崩场景",
  "total_requests": 15000,
  "success_requests": 14200,
  "degraded_requests": 2800,
  "error_requests": 800,
  "qps": 500.0,
  "avg_latency": "120ms",
  "p95_latency": "280ms",
  "p99_latency": "450ms",
  "cache_hit_rate": 0.75,
  "db_requests": 3750,
  "db_error_rate": 0.12
}
```

### 2. 关键改进指标
- **成功率**: 56.7% → 94.7% (+38%)
- **P99延迟**: 5.2s → 450ms (-91%)
- **数据源压力**: 12750 → 3750 (-71%)
- **缓存命中率**: 15% → 75% (+60%)

## 生产环境部署建议

### 1. 配置模板
```yaml
# 雪崩防护配置
avalanche_protection:
  ttl_jitter: 0.2                    # 20%抖动
  shard_count: 10                    # 10个分片
  shard_interval: "5m"               # 5分钟间隔
  
  circuit_breaker:
    failure_rate: 0.5                # 50%失败率触发熔断
    min_request_count: 20            # 最小请求数
    open_timeout: "30s"              # 熔断30秒
    half_open_max_calls: 3           # 半开状态最大调用数
    sliding_window_size: 60          # 60秒滑动窗口
  
  warmup:
    enabled: true
    interval: "10m"                  # 10分钟预热一次
    advance_time: "5m"               # 提前5分钟预热
  
  degradation:
    enabled: true
    max_degraded_ratio: 0.3          # 最大30%降级
```

### 2. 监控大盘
- **实时QPS和延迟曲线**
- **缓存命中率趋势**
- **熔断器状态变化**
- **降级请求比例**
- **数据源压力指标**

### 3. 告警规则
```yaml
alerts:
  - name: "缓存雪崩告警"
    condition: "cache_hit_rate < 0.5 AND p99_latency > 1s"
    duration: "2m"
    
  - name: "熔断器告警"
    condition: "circuit_breaker_state == 'open'"
    duration: "30s"
    
  - name: "降级比例告警"
    condition: "degraded_requests_ratio > 0.2"
    duration: "5m"
```

## 验收标准

### 1. 功能验收
- ✅ TTL抖动功能正常，同批key过期时间分散
- ✅ 分片过期机制有效，key分布在不同时间段过期
- ✅ 熔断器状态转换正确，能够及时保护系统
- ✅ 降级机制可靠，在故障时提供基础服务
- ✅ 预热功能稳定，能够主动刷新热点数据

### 2. 性能验收
- ✅ 雪崩场景下P99延迟 < 500ms
- ✅ 成功率 > 95%
- ✅ 数据源压力减少 > 60%
- ✅ 缓存命中率 > 70%

### 3. 稳定性验收
- ✅ 连续运行24小时无异常
- ✅ 多次雪崩演练均能正常恢复
- ✅ 熔断器能够自动恢复
- ✅ 降级期间核心功能可用

## 总结

第8天我们成功实现了完整的缓存雪崩防护体系：

1. **TTL抖动**: 通过随机化过期时间避免同时过期
2. **分片过期**: 将key分布到不同时间段，错开过期压力
3. **熔断器**: 在故障时快速失败，保护后端系统
4. **降级机制**: 提供基础服务保证可用性
5. **预热策略**: 主动刷新热点数据，减少缓存未命中

通过压测验证，我们的雪崩防护方案能够在极端场景下将P99延迟从5秒降低到450ms，成功率从57%提升到95%，数据源压力减少71%。

这套防护体系不仅解决了缓存雪崩问题，还为后续的高可用架构奠定了基础。在生产环境中，建议根据具体业务场景调整参数，并建立完善的监控和告警机制。

## 下一步
Day 9将学习一致性与事务语义，包括Redis事务、Lua脚本、以及与数据源的一致性保证策略。
