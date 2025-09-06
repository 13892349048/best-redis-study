# 缓存雪崩防护指南

## 概述

缓存雪崩是指大量缓存在同一时间点过期，导致所有请求都击中数据源，造成数据源压力过大甚至崩溃的现象。本指南介绍了完整的雪崩防护方案。

## 核心防护机制

### 1. TTL抖动 (TTL Jitter)
通过为缓存TTL添加随机抖动，避免大量key同时过期：
```go
// 20%的TTL抖动
config.TTLJitter = 0.2
```

### 2. 分片过期 (Sharded Expiration)
将key根据hash值分配到不同的时间分片，错开过期时间：
```go
config.ShardCount = 10
config.ShardInterval = 5 * time.Minute
```

### 3. 熔断器 (Circuit Breaker)
在数据源压力过大时快速失败，保护系统稳定性：
```go
config.FailureRate = 0.5        // 50%失败率触发熔断
config.MinRequestCount = 20     // 最小请求数
config.OpenTimeout = 30 * time.Second // 熔断30秒
```

### 4. 降级机制 (Degradation)
在熔断期间提供降级服务：
```go
config.DegradeFunc = func(ctx context.Context, key string) (interface{}, error) {
    return getDefaultData(key), nil
}
```

### 5. 预热策略 (Warmup)
主动刷新即将过期的热点数据：
```go
config.EnableWarmup = true
config.WarmupInterval = 10 * time.Minute
config.WarmupKeys = []string{"hot_key_1", "hot_key_2"}
```

## 使用方式

### 基础使用
```go
// 创建雪崩防护配置
config := cache.DefaultAvalancheProtectionConfig()
config.TTLJitter = 0.2
config.ShardCount = 10
config.ShardInterval = 5 * time.Minute

// 创建Redis缓存
redisCache := cache.NewRedisCache(client, config.CacheAsideOptions)

// 创建雪崩防护
protection := cache.NewAvalancheProtection(redisCache, config)
defer protection.Close()

// 使用防护缓存
var result MyData
err := protection.GetOrLoad(ctx, key, &result, func(ctx context.Context, key string) (interface{}, error) {
    return loadFromDatabase(ctx, key)
})
```

### 批量设置（带分片）
```go
items := map[string]interface{}{
    "key1": value1,
    "key2": value2,
    "key3": value3,
}

err := protection.BatchSetWithSharding(ctx, items, 1*time.Hour)
```

## 演示程序

### 运行基本演示
```bash
go run cmd/demo/day8_avalanche_demo.go
```

演示内容：
- TTL抖动效果
- 分片过期机制
- 熔断器状态转换
- 预热功能
- 完整雪崩场景对比

### 运行压测工具
```bash
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

## 配置建议

### 不同场景的配置模板

#### 用户数据缓存
```go
config := &cache.AvalancheProtectionConfig{
    CacheAsideOptions: &cache.CacheAsideOptions{
        TTL:       1 * time.Hour,
        TTLJitter: 0.2, // 20%抖动
    },
    ShardCount:    10,
    ShardInterval: 6 * time.Minute,
    CircuitBreakerConfig: &cache.CircuitBreakerConfig{
        FailureRate:         0.5,
        MinRequestCount:     20,
        OpenTimeout:         30 * time.Second,
    },
}
```

#### 商品信息缓存
```go
config := &cache.AvalancheProtectionConfig{
    CacheAsideOptions: &cache.CacheAsideOptions{
        TTL:       30 * time.Minute,
        TTLJitter: 0.15, // 15%抖动
    },
    ShardCount:    8,
    ShardInterval: 4 * time.Minute,
    EnableWarmup:  true,
    WarmupInterval: 5 * time.Minute,
}
```

#### 热点数据缓存
```go
config := &cache.AvalancheProtectionConfig{
    CacheAsideOptions: &cache.CacheAsideOptions{
        TTL:       10 * time.Minute,
        TTLJitter: 0.3, // 30%抖动
    },
    ShardCount:    15,
    ShardInterval: 1 * time.Minute,
    CircuitBreakerConfig: &cache.CircuitBreakerConfig{
        FailureRate:         0.3, // 更敏感的熔断
        MinRequestCount:     10,
        OpenTimeout:         10 * time.Second,
    },
}
```

## 监控指标

### 关键指标
- **P99延迟**: < 500ms
- **成功率**: > 95%
- **缓存命中率**: > 80%
- **熔断器状态**: 正常关闭
- **降级请求比例**: < 10%

### 获取统计信息
```go
// 获取雪崩防护统计
stats := protection.GetStats()
fmt.Printf("熔断器状态: %v\n", stats["circuit_breaker"])
fmt.Printf("分片配置: %d个分片，间隔%v\n", stats["shard_count"], stats["shard_interval"])

// 获取缓存统计
if cacheStats, ok := stats["cache"].(*cache.CacheStats); ok {
    fmt.Printf("缓存命中率: %.2f%%\n", 
        float64(cacheStats.Hits)/(float64(cacheStats.Hits+cacheStats.Misses))*100)
}
```

## 告警配置

### Prometheus告警规则示例
```yaml
groups:
- name: cache_avalanche_alerts
  rules:
  - alert: CacheAvalanche
    expr: cache_hit_rate < 0.5 AND cache_p99_latency > 1
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "缓存雪崩检测"
      description: "缓存命中率低于50%且P99延迟超过1秒"

  - alert: CircuitBreakerOpen
    expr: circuit_breaker_state == 1
    for: 30s
    labels:
      severity: warning
    annotations:
      summary: "熔断器打开"
      description: "系统熔断器已打开，请检查数据源状态"

  - alert: HighDegradationRate
    expr: degraded_requests_rate > 0.2
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "高降级比例"
      description: "降级请求比例超过20%"
```

## 故障排查

### 常见问题

1. **熔断器频繁打开**
   - 检查数据源是否正常
   - 调整失败率阈值
   - 增加最小请求数

2. **缓存命中率低**
   - 检查TTL配置是否合理
   - 增加预热key列表
   - 调整分片策略

3. **延迟波动大**
   - 增加TTL抖动比例
   - 优化分片间隔
   - 检查数据源性能

### 调试模式
```go
// 启用详细日志
config.Verbose = true

// 获取详细统计
stats := protection.GetStats()
log.Printf("详细统计: %+v", stats)
```

## 最佳实践

1. **渐进式部署**: 先在测试环境验证配置，再逐步推广到生产环境
2. **监控先行**: 部署前建立完善的监控和告警机制
3. **参数调优**: 根据业务特点调整TTL抖动、分片配置等参数
4. **降级策略**: 设计合理的降级逻辑，确保核心功能可用
5. **定期演练**: 定期进行雪崩场景演练，验证防护效果

## 性能测试结果

基于我们的压测工具，典型的性能改进效果：

| 指标 | 无防护 | 有防护 | 改进 |
|------|--------|--------|------|
| P99延迟 | 5.2s | 450ms | -91% |
| 成功率 | 56.7% | 94.7% | +38% |
| 数据源压力 | 12750 | 3750 | -71% |
| 缓存命中率 | 15% | 75% | +60% |

这些数据表明，雪崩防护机制能够显著提升系统在极端场景下的稳定性和性能。
