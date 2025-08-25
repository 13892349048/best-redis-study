# Redis缓存最佳实践指南

## 🎯 概述

本指南基于Day 5的学习成果，提供Redis缓存在生产环境中的最佳实践，包括模式选择、配置优化、监控告警等。

## 🔧 缓存模式选择指南

### 1. Cache-Aside（旁路缓存）

**适用场景：**
- 读多写少的业务场景
- 允许短暂数据不一致
- 需要应用完全控制缓存逻辑

**配置示例：**
```go
userCache := cache.NewCacheAside(redisCache, &cache.CacheAsideOptions{
    TTL:           2 * time.Hour,     // 用户信息TTL
    TTLJitter:     0.1,               // 10%抖动防雪崩
    EmptyValueTTL: 5 * time.Minute,   // 空值缓存TTL
    Serializer:    &cache.JSONSerializer{},
    EnableMetrics: true,
    Namespace:     "user",
})
```

**使用模式：**
```go
func GetUser(ctx context.Context, userID int) (*User, error) {
    key := fmt.Sprintf("profile:%d", userID)
    var user User
    
    err := userCache.GetOrLoad(ctx, key, &user, func(ctx context.Context, key string) (interface{}, error) {
        return userService.LoadFromDB(ctx, userID)
    })
    
    return &user, err
}

func UpdateUser(ctx context.Context, user *User) error {
    // 先更新数据源
    if err := userService.UpdateDB(ctx, user); err != nil {
        return err
    }
    
    // 删除缓存，下次读取时会重新加载
    key := fmt.Sprintf("profile:%d", user.ID)
    userCache.Del(ctx, key)
    
    return nil
}
```

### 2. Write-Through（写穿透）

**适用场景：**
- 需要强数据一致性
- 写入频率不高
- 能容忍写入延迟增加

**配置示例：**
```go
configCache := cache.NewWriteThroughCache(redisCache, 
    func(ctx context.Context, key string, value interface{}) error {
        return configService.SaveToDB(ctx, key, value)
    }, 
    &cache.CacheAsideOptions{
        TTL:        24 * time.Hour,  // 配置信息TTL较长
        Serializer: &cache.JSONSerializer{},
        Namespace:  "config",
    })
```

### 3. Write-Behind（写回）

**适用场景：**
- 写入密集型场景
- 可以容忍数据丢失风险
- 需要高写入性能

**配置示例：**
```go
logCache := cache.NewWriteBehindCache(redisCache,
    func(ctx context.Context, key string, value interface{}) error {
        return logService.BatchWrite(ctx, key, value)
    },
    &cache.WriteBehindOptions{
        CacheAsideOptions: &cache.CacheAsideOptions{
            TTL:        1 * time.Hour,
            Serializer: &cache.JSONSerializer{},
            Namespace:  "logs",
        },
        BufferSize:    1000,              // 缓冲区大小
        FlushInterval: 5 * time.Second,   // 刷新间隔
        BatchSize:     100,               // 批量大小
        MaxRetries:    3,                 // 最大重试次数
    })
```

## ⚙️ TTL设计策略

### 1. 分层TTL配置

```go
func GetTTLByDataType(dataType string) time.Duration {
    switch dataType {
    case "user_profile":
        return 2 * time.Hour      // 用户信息变化不频繁
    case "product_info":
        return 1 * time.Hour      // 商品信息可能更新
    case "inventory":
        return 10 * time.Minute   // 库存变化频繁
    case "price":
        return 30 * time.Minute   // 价格可能调整
    case "hot_data":
        return 4 * time.Hour      // 热点数据保存更久
    case "session":
        return 30 * time.Minute   // 用户会话
    case "verification_code":
        return 5 * time.Minute    // 验证码短期有效
    case "temp_cache":
        return 1 * time.Minute    // 临时缓存
    default:
        return 1 * time.Hour      // 默认TTL
    }
}
```

### 2. 动态TTL调整

```go
func CalculateDynamicTTL(baseType string, accessFrequency int, dataSize int) time.Duration {
    baseTTL := GetTTLByDataType(baseType)
    
    // 根据访问频率调整
    if accessFrequency > 1000 {
        baseTTL = baseTTL * 2  // 热点数据延长TTL
    } else if accessFrequency < 10 {
        baseTTL = baseTTL / 2  // 冷数据缩短TTL
    }
    
    // 根据数据大小调整
    if dataSize > 1024*1024 { // 大于1MB
        baseTTL = baseTTL / 2  // 大数据缩短TTL
    }
    
    return baseTTL
}
```

### 3. TTL抖动配置

```go
// 根据数据量和重要性配置抖动
func GetJitterByDataVolume(expectedKeys int) float64 {
    switch {
    case expectedKeys > 100000:
        return 0.3  // 大量数据需要更大抖动
    case expectedKeys > 10000:
        return 0.2  // 中量数据中等抖动
    case expectedKeys > 1000:
        return 0.1  // 少量数据小抖动
    default:
        return 0.05 // 极少数据最小抖动
    }
}
```

## 🔑 Key设计规范

### 1. 命名规范

```go
// 推荐格式：namespace:service:type:id:version
const (
    UserProfileKey = "app:user:profile:%d:v1"
    OrderDetailKey = "app:order:detail:%s:v2"
    ProductInfoKey = "app:product:info:%d:v1"
    SessionKey     = "app:session:%s:v1"
)

func BuildUserKey(userID int) string {
    return fmt.Sprintf(UserProfileKey, userID)
}
```

### 2. Key长度优化

```go
// 使用短而有意义的缩写
const (
    // 好的例子
    UserKey  = "u:%d"           // 用户
    OrderKey = "o:%s"           // 订单  
    ProdKey  = "p:%d"           // 商品
    
    // 避免过长的key
    // "application:user:profile:detailed:information:%d" // 太长
)
```

### 3. 批量Key设计

```go
// 支持批量操作的Key设计
func BuildBatchUserKeys(userIDs []int) []string {
    keys := make([]string, len(userIDs))
    for i, id := range userIDs {
        keys[i] = fmt.Sprintf("u:%d", id)
    }
    return keys
}
```

## 📊 监控与告警

### 1. 关键指标监控

```go
type CacheMonitor struct {
    cache   *cache.RedisCache
    alerter Alerter
}

func (m *CacheMonitor) CheckHealth() {
    stats := m.cache.GetStats()
    
    // 命中率监控
    if stats.HitRate < 0.8 {
        m.alerter.Alert("LOW_HIT_RATE", map[string]interface{}{
            "hit_rate": stats.HitRate,
            "threshold": 0.8,
        })
    }
    
    // 延迟监控
    if stats.AvgGetLatencyUs > 5000 { // 5ms
        m.alerter.Alert("HIGH_LATENCY", map[string]interface{}{
            "latency_us": stats.AvgGetLatencyUs,
            "threshold": 5000,
        })
    }
    
    // 错误率监控
    totalOps := stats.Hits + stats.Misses
    if totalOps > 0 {
        errorRate := float64(stats.GetErrors) / float64(totalOps)
        if errorRate > 0.01 { // 1%
            m.alerter.Alert("HIGH_ERROR_RATE", map[string]interface{}{
                "error_rate": errorRate,
                "threshold": 0.01,
            })
        }
    }
}
```

### 2. 性能基准测试

```go
func BenchmarkCachePerformance(b *testing.B) {
    cache := setupTestCache()
    ctx := context.Background()
    
    // 写入性能测试
    b.Run("Set", func(b *testing.B) {
        for i := 0; i < b.N; i++ {
            key := fmt.Sprintf("bench:set:%d", i)
            cache.Set(ctx, key, "test_value", time.Hour)
        }
    })
    
    // 读取性能测试
    b.Run("Get", func(b *testing.B) {
        // 预填充数据
        for i := 0; i < 1000; i++ {
            key := fmt.Sprintf("bench:get:%d", i)
            cache.Set(ctx, key, "test_value", time.Hour)
        }
        
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            key := fmt.Sprintf("bench:get:%d", i%1000)
            var value string
            cache.Get(ctx, key, &value)
        }
    })
}
```

## 🚨 故障处理与容错

### 1. 缓存降级策略

```go
type CacheWithFallback struct {
    primary   cache.Cache
    secondary cache.Cache  // 本地缓存
    db        DataSource
}

func (c *CacheWithFallback) Get(ctx context.Context, key string, dest interface{}) error {
    // 尝试主缓存
    err := c.primary.Get(ctx, key, dest)
    if err == nil {
        return nil
    }
    
    // 主缓存失败，尝试二级缓存
    if c.secondary != nil {
        err = c.secondary.Get(ctx, key, dest)
        if err == nil {
            // 异步回写主缓存
            go c.asyncWriteBack(key, dest)
            return nil
        }
    }
    
    // 缓存全部失败，直接查询数据源
    return c.loadFromDataSource(ctx, key, dest)
}

func (c *CacheWithFallback) asyncWriteBack(key string, value interface{}) {
    ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
    defer cancel()
    
    c.primary.Set(ctx, key, value, time.Hour)
}
```

### 2. 熔断器模式

```go
type CircuitBreakerCache struct {
    cache   cache.Cache
    breaker *CircuitBreaker
}

func (c *CircuitBreakerCache) Get(ctx context.Context, key string, dest interface{}) error {
    if c.breaker.IsOpen() {
        return ErrCacheUnavailable
    }
    
    err := c.cache.Get(ctx, key, dest)
    if err != nil {
        c.breaker.RecordFailure()
        return err
    }
    
    c.breaker.RecordSuccess()
    return nil
}
```

### 3. 超时控制

```go
func GetWithTimeout(ctx context.Context, cache cache.Cache, key string, dest interface{}, timeout time.Duration) error {
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()
    
    done := make(chan error, 1)
    go func() {
        done <- cache.Get(ctx, key, dest)
    }()
    
    select {
    case err := <-done:
        return err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

## ⚡ 性能优化技巧

### 1. 连接池配置

```go
// 生产环境推荐配置
func ProductionRedisConfig() *redisx.Config {
    return &redisx.Config{
        Addr:     "redis-cluster.internal:6379",
        Password: getRedisPassword(),
        
        // 连接池配置
        PoolSize:        100,              // 根据并发量调整
        MinIdleConns:    20,               // 保持最小连接
        MaxIdleConns:    50,               // 最大空闲连接
        ConnMaxIdleTime: 30 * time.Minute, // 空闲连接超时
        ConnMaxLifetime: 2 * time.Hour,    // 连接最大生命周期
        
        // 超时配置
        DialTimeout:  5 * time.Second,     // 连接超时
        ReadTimeout:  3 * time.Second,     // 读超时
        WriteTimeout: 3 * time.Second,     // 写超时
        
        // 重试配置
        MaxRetries:      3,                          // 最大重试次数
        MinRetryBackoff: 8 * time.Millisecond,      // 最小重试间隔
        MaxRetryBackoff: 512 * time.Millisecond,    // 最大重试间隔
        
        EnableMetrics: true,
    }
}
```

### 2. 序列化优化

```go
// 根据数据类型选择最佳序列化器
func GetOptiMalSerializer(dataType reflect.Type) cache.Serializer {
    switch dataType.Kind() {
    case reflect.String:
        return &cache.StringSerializer{}  // 字符串直接存储
    case reflect.Int, reflect.Int64, reflect.Float64:
        return &cache.StringSerializer{}  // 数字转字符串存储
    default:
        return &cache.JSONSerializer{}    // 复杂对象用JSON
    }
}
```

### 3. 批量操作优化

```go
func BatchGet(ctx context.Context, cache *cache.RedisCache, keys []string) (map[string]interface{}, error) {
    // 使用Pipeline批量获取
    pipe := cache.Pipeline()
    cmds := make(map[string]*redis.StringCmd)
    
    for _, key := range keys {
        cmds[key] = pipe.Get(ctx, key)
    }
    
    _, err := pipe.Exec(ctx)
    if err != nil && err != redis.Nil {
        return nil, err
    }
    
    results := make(map[string]interface{})
    for key, cmd := range cmds {
        if cmd.Err() == nil {
            results[key] = cmd.Val()
        }
    }
    
    return results, nil
}
```

## 🧪 测试策略

### 1. 单元测试

```go
func TestCacheOperations(t *testing.T) {
    // 使用内存Redis或Mock进行测试
    cache := setupTestCache(t)
    ctx := context.Background()
    
    tests := []struct {
        name string
        key  string
        value interface{}
        ttl  time.Duration
    }{
        {"string", "test:string", "hello", time.Minute},
        {"number", "test:number", 42, time.Minute},
        {"object", "test:object", map[string]string{"key": "value"}, time.Minute},
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // 设置缓存
            err := cache.Set(ctx, tt.key, tt.value, tt.ttl)
            assert.NoError(t, err)
            
            // 获取缓存
            var result interface{}
            err = cache.Get(ctx, tt.key, &result)
            assert.NoError(t, err)
            assert.Equal(t, tt.value, result)
        })
    }
}
```

### 2. 集成测试

```go
func TestCacheIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("跳过集成测试")
    }
    
    // 连接真实Redis实例
    client := setupRealRedis(t)
    cache := cache.NewRedisCache(client, cache.DefaultCacheAsideOptions())
    
    // 测试完整业务流程
    testBusinessFlow(t, cache)
}
```

### 3. 压力测试

```go
func TestCacheConcurrency(t *testing.T) {
    cache := setupTestCache(t)
    ctx := context.Background()
    
    const numGoroutines = 100
    const numOperations = 1000
    
    var wg sync.WaitGroup
    
    // 并发写入测试
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < numOperations; j++ {
                key := fmt.Sprintf("test:%d:%d", id, j)
                cache.Set(ctx, key, fmt.Sprintf("value-%d-%d", id, j), time.Minute)
            }
        }(i)
    }
    
    wg.Wait()
    
    // 验证数据完整性
    stats := cache.GetStats()
    expectedSets := numGoroutines * numOperations
    assert.Equal(t, int64(expectedSets), stats.Sets)
}
```

## 📋 部署检查清单

### 生产环境部署前检查

- [ ] 连接池配置已优化
- [ ] TTL策略已根据业务设定
- [ ] 监控指标已配置
- [ ] 告警阈值已设定
- [ ] 降级策略已实现
- [ ] 压力测试已通过
- [ ] 文档已更新
- [ ] 运维手册已准备

### 监控指标配置

- [ ] 缓存命中率 (目标 > 80%)
- [ ] 平均延迟 (目标 < 5ms)
- [ ] 错误率 (目标 < 1%)
- [ ] 连接池使用率
- [ ] 内存使用率
- [ ] QPS统计

### 告警配置

- [ ] 命中率过低告警
- [ ] 延迟过高告警
- [ ] 错误率过高告警
- [ ] 连接数异常告警
- [ ] Redis服务不可用告警

## 🔗 相关资源

- [Day 5学习总结](../day5-summary.md)
- [Redis官方最佳实践](https://redis.io/docs/latest/develop/use/patterns/)
- [Go Redis客户端文档](https://redis.uptrace.dev/)
- [缓存设计模式](https://docs.microsoft.com/en-us/azure/architecture/patterns/cache-aside) 