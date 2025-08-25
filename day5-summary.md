# Day 5: 缓存模式与TTL设计

## 🎯 学习目标

掌握缓存的三种主要模式（Cache-Aside、Write-Through、Write-Behind）及其适用场景，理解TTL设计策略，实现可重用的缓存组件。

## 📚 理论要点

### 1. 缓存模式对比

| 模式 | 读取 | 写入 | 一致性 | 性能 | 适用场景 |
|------|------|------|--------|------|----------|
| **Cache-Aside** | 应用控制 | 应用控制 | 最终一致 | 读优化 | 读多写少，允许短暂不一致 |
| **Write-Through** | 应用控制 | 同步双写 | 强一致 | 写较慢 | 需要强一致性，写入不频繁 |
| **Write-Behind** | 应用控制 | 异步写入 | 弱一致 | 写优化 | 写密集，容忍数据丢失风险 |

### 2. Cache-Aside模式详解

**工作流程：**
1. 读取时先查缓存，命中则返回
2. 缓存未命中，查询数据源
3. 将查询结果写入缓存，设置TTL
4. 更新时直接更新数据源，删除缓存

**优点：**
- 应用完全控制缓存逻辑
- 缓存故障不影响业务
- 适合读多写少场景

**缺点：**
- 存在短暂的数据不一致
- 需要额外的代码复杂度

### 3. Write-Through模式详解

**工作流程：**
1. 写入时同时更新缓存和数据源
2. 只有两者都成功才算成功
3. 读取时优先从缓存读取

**优点：**
- 数据一致性强
- 缓存始终有最新数据

**缺点：**
- 写入延迟较高
- 数据源故障影响缓存写入

### 4. Write-Behind模式详解

**工作流程：**
1. 写入时立即更新缓存
2. 异步批量写入数据源
3. 可配置写入间隔和批次大小

**优点：**
- 写入性能极高
- 可以批量优化数据源操作

**缺点：**
- 存在数据丢失风险
- 数据一致性较弱

### 5. TTL设计策略

#### 5.1 随机抖动（Jitter）

**原理：**
防止缓存雪崩，在基准TTL基础上增加随机波动。

```go
// 计算带抖动的TTL
func calculateTTL(baseTTL time.Duration, jitter float64) time.Duration {
    minMultiplier := 1.0 - jitter
    maxMultiplier := 1.0 + jitter
    multiplier := minMultiplier + rand.Float64()*(maxMultiplier-minMultiplier)
    return time.Duration(float64(baseTTL) * multiplier)
}
```

**效果：**
- 基准TTL：1小时，抖动10%
- 实际TTL：54分钟 ~ 66分钟
- 避免大量key同时过期

#### 5.2 分层TTL策略

| 数据类型 | 基准TTL | 抖动 | 说明 |
|----------|---------|------|------|
| 热点数据 | 1小时 | 10% | 频繁访问的用户信息 |
| 普通数据 | 30分钟 | 20% | 一般业务数据 |
| 临时数据 | 5分钟 | 30% | 验证码、临时状态 |
| 空值缓存 | 5分钟 | 50% | 防穿透的空值 |

#### 5.3 命名空间设计

```go
// 推荐的Key命名规范
namespace:service:type:id:version
demo:user:profile:123:v1
demo:order:detail:456:v2
```

## 🛠️ 代码实现

### 1. 核心接口设计

```go
// 缓存接口
type Cache interface {
    Get(ctx context.Context, key string, dest interface{}) error
    Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error
    Del(ctx context.Context, keys ...string) error
    Exists(ctx context.Context, keys ...string) (int64, error)
    GetWithTTL(ctx context.Context, key string, dest interface{}) (time.Duration, error)
    Expire(ctx context.Context, key string, ttl time.Duration) error
    Close() error
}

// 数据加载函数
type LoaderFunc func(ctx context.Context, key string) (interface{}, error)

// 数据写入函数
type WriteFunc func(ctx context.Context, key string, value interface{}) error
```

### 2. 序列化器设计

支持可插拔的序列化方式：

```go
type Serializer interface {
    Marshal(v interface{}) ([]byte, error)
    Unmarshal(data []byte, v interface{}) error
    ContentType() string
}

// JSON序列化器
type JSONSerializer struct{}

// 字符串序列化器（性能更好）
type StringSerializer struct{}
```

### 3. 指标监控

```go
type CacheMetrics struct {
    hits              int64  // 缓存命中数
    misses            int64  // 缓存未命中数
    getTotalLatency   int64  // Get操作总延迟
    setTotalLatency   int64  // Set操作总延迟
    serializeErrors   int64  // 序列化错误数
    deserializeErrors int64  // 反序列化错误数
}
```

## 📊 性能对比

### 缓存命中场景（单次操作）

| 操作 | 直接数据源 | Cache-Aside | Write-Through | Write-Behind |
|------|------------|-------------|---------------|--------------|
| 读取 | 50ms | 1ms | 1ms | 1ms |
| 写入 | 30ms | 30ms | 31ms | 1ms |

### 缓存未命中场景

| 操作 | 直接数据源 | Cache-Aside | Write-Through | Write-Behind |
|------|------------|-------------|---------------|--------------|
| 读取 | 50ms | 51ms | 51ms | 51ms |

### 并发写入场景（1000次写入）

| 模式 | 总耗时 | 平均延迟 | 数据一致性 |
|------|--------|----------|------------|
| Write-Through | 31s | 31ms | 强一致 |
| Write-Behind | 1.2s | 1.2ms | 弱一致 |

## 🎯 最佳实践

### 1. 选择合适的缓存模式

```go
// 用户信息缓存（读多写少）- Cache-Aside
userCache := cache.NewCacheAside(redisCache, &cache.CacheAsideOptions{
    TTL:           1 * time.Hour,
    TTLJitter:     0.1,
    EmptyValueTTL: 5 * time.Minute,
})

// 配置信息缓存（需要强一致性）- Write-Through
configCache := cache.NewWriteThroughCache(redisCache, configWriter, nil)

// 操作日志缓存（写密集）- Write-Behind
logCache := cache.NewWriteBehindCache(redisCache, logWriter, &cache.WriteBehindOptions{
    BufferSize:    1000,
    FlushInterval: 5 * time.Second,
    BatchSize:     100,
})
```

### 2. TTL设计原则

```go
// 根据数据特点设置不同的TTL
opts := &cache.CacheAsideOptions{
    TTL:           calculateTTL(dataType, accessFrequency),
    TTLJitter:     calculateJitter(dataVolume),
    EmptyValueTTL: 5 * time.Minute, // 空值缓存时间要短
}

func calculateTTL(dataType string, frequency int) time.Duration {
    switch dataType {
    case "user_profile":
        return 2 * time.Hour      // 用户信息变化不频繁
    case "product_price":
        return 30 * time.Minute   // 价格可能调整
    case "inventory":
        return 5 * time.Minute    // 库存变化频繁
    default:
        return 1 * time.Hour
    }
}
```

### 3. 指标监控配置

```go
// 生产环境建议的告警阈值
func setupCacheAlerts(metrics *CacheMetrics) {
    stats := metrics.GetStats()
    
    // 命中率告警
    if stats.HitRate < 0.8 {
        alert("缓存命中率过低", stats.HitRate)
    }
    
    // 延迟告警
    if stats.AvgGetLatencyUs > 5000 { // 5ms
        alert("缓存读取延迟过高", stats.AvgGetLatencyUs)
    }
    
    // 错误率告警
    errorRate := float64(stats.GetErrors) / float64(stats.Hits+stats.Misses)
    if errorRate > 0.01 { // 1%
        alert("缓存错误率过高", errorRate)
    }
}
```

### 4. 容错设计

```go
// 缓存降级策略
func (ca *CacheAside) GetWithFallback(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
    // 尝试从缓存获取
    err := ca.cache.Get(ctx, key, dest)
    if err == nil {
        return nil
    }
    
    // 缓存失败，降级到数据源
    if !errors.Is(err, ErrCacheMiss) {
        log.Printf("Cache degraded for key %s: %v", key, err)
    }
    
    // 从数据源加载
    value, err := loader(ctx, key)
    if err != nil {
        return err
    }
    
    // 异步写回缓存（避免阻塞）
    go func() {
        ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
        defer cancel()
        
        if setErr := ca.cache.Set(ctx, key, value, ca.options.TTL); setErr != nil {
            log.Printf("Async cache set failed for key %s: %v", key, setErr)
        }
    }()
    
    return nil
}
```

## 🚨 常见陷阱与解决方案

### 1. 缓存雪崩

**问题：** 大量key同时过期导致数据源压力激增

**解决方案：**
```go
// TTL随机抖动
opts := &cache.CacheAsideOptions{
    TTL:       1 * time.Hour,
    TTLJitter: 0.2, // 20%抖动，防止同时过期
}

// 分批预热
func warmupCache(cache *CacheAside, keys []string) {
    batchSize := 10
    for i := 0; i < len(keys); i += batchSize {
        end := i + batchSize
        if end > len(keys) {
            end = len(keys)
        }
        
        // 分批预热，每批间隔100ms
        time.Sleep(100 * time.Millisecond)
        warmupBatch(cache, keys[i:end])
    }
}
```

### 2. 缓存穿透

**问题：** 查询不存在的数据，缓存无法防护

**解决方案：**
```go
// 空值缓存
func (ca *CacheAside) GetOrLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
    // ... 缓存查询逻辑
    
    value, err := loader(ctx, key)
    if err != nil {
        // 对于"不存在"的错误，设置空值缓存
        if isNotFoundError(err) {
            ca.cache.SetEmptyValue(ctx, key)
        }
        return err
    }
    
    // ... 正常缓存逻辑
}
```

### 3. 缓存击穿

**问题：** 热点key过期时并发访问数据源

**解决方案：** 将在Day 7详细实现单飞（singleflight）模式

### 4. Write-Behind数据丢失

**问题：** 程序崩溃导致缓冲区数据丢失

**解决方案：**
```go
// 优雅关闭
func (wb *WriteBehindCache) Close() error {
    // 设置关闭标志
    wb.mu.Lock()
    wb.closed = true
    wb.mu.Unlock()
    
    // 等待所有写入完成
    wb.wg.Wait()
    
    // 刷新剩余数据
    return wb.flushRemaining()
}

// 持久化缓冲区（可选）
func (wb *WriteBehindCache) persistBuffer() error {
    // 将缓冲区数据写入磁盘文件
    // 重启时恢复
}
```

## 🧪 测试验证

### 运行演示程序

由于cmd/demo下有多个main包，建议单独运行：

```bash
# 创建临时文件
cp cmd/demo/day5_cache_patterns.go /tmp/cache_demo.go

# 修改导入路径
cd /tmp
go mod init cache_demo
go get github.com/redis/go-redis/v9

# 运行演示
go run cache_demo.go
```

### 预期输出

```
🚀 Day 5: 缓存模式与TTL设计演示
================================

📚 1. Cache-Aside 模式演示
Cache-Aside模式: 读取时检查缓存，未命中时从数据源加载并缓存
📖 第一次读取用户1 (缓存未命中)...
✅ 获取用户成功: {ID:1 Name:Alice Email:alice@example.com CreateAt:1642xxx} (耗时: 52ms)
📖 第二次读取用户1 (缓存命中)...
✅ 获取用户成功: {ID:1 Name:Alice Email:alice@example.com CreateAt:1642xxx} (耗时: 1ms)

📊 Cache-Aside统计: &{Hits:1 Misses:1 HitRate:0.5 ...}
```

## 📈 性能优化建议

### 1. 连接池配置

```go
// 生产环境推荐配置
config := &redisx.Config{
    PoolSize:        50,              // 根据并发量调整
    MinIdleConns:    10,              // 保持最小连接
    ConnMaxLifetime: 1 * time.Hour,   // 连接生命周期
    ReadTimeout:     3 * time.Second, // 读超时
    WriteTimeout:    3 * time.Second, // 写超时
}
```

### 2. 序列化优化

```go
// 对于简单数据类型，使用字符串序列化
stringCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
    Serializer: &cache.StringSerializer{}, // 比JSON快3-5倍
})

// 对于复杂对象，考虑使用MessagePack
// 需要引入第三方库：github.com/vmihailenco/msgpack/v5
```

### 3. Pipeline批量操作

```go
// 批量获取多个缓存
func (c *RedisCache) MGet(ctx context.Context, keys []string) ([]interface{}, error) {
    pipe := c.client.Pipeline()
    cmds := make([]*redis.StringCmd, len(keys))
    
    for i, key := range keys {
        cmds[i] = pipe.Get(ctx, c.buildKey(key))
    }
    
    _, err := pipe.Exec(ctx)
    return processBatchResults(cmds), err
}
```

## 📋 检查清单

- [x] 实现Cache接口和序列化器
- [x] 实现Redis缓存基础功能
- [x] 实现Cache-Aside装饰器
- [x] 实现Write-Through模式
- [x] 实现Write-Behind模式
- [x] 实现TTL随机抖动
- [x] 实现缓存指标监控
- [x] 创建演示程序验证功能
- [x] 编写详细文档和最佳实践

## 🔗 相关资源

- [Redis官方文档 - 缓存策略](https://redis.io/docs/latest/develop/use/patterns/cache/)
- [go-redis Pipeline文档](https://redis.uptrace.dev/guide/pipeline.html)
- [分布式系统缓存模式对比](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html)

## 📚 下一步

Day 6将专注于**缓存穿透防护**，包括：
- 布隆过滤器实现
- 空值缓存策略
- 参数校验与业务逻辑防护
- 穿透攻击检测与限流 