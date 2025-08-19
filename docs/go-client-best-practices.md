# Go Redis 客户端最佳实践快速参考

## 快速配置

### 生产环境标准配置
```go
import "what_redis_can_do/internal/redisx"

config := &redisx.Config{
    Addr:     "redis.production.com:6379",
    Password: os.Getenv("REDIS_PASSWORD"),
    DB:       0,
    
    // 连接池配置
    PoolSize:        20,              // CPU核数 × 2-4
    MinIdleConns:    10,              // PoolSize / 2
    ConnMaxIdleTime: 30 * time.Minute,
    ConnMaxLifetime: time.Hour,
    
    // 超时配置
    DialTimeout:  5 * time.Second,
    ReadTimeout:  2 * time.Second,
    WriteTimeout: 2 * time.Second,
    
    // 重试配置
    MaxRetries:      3,
    MinRetryBackoff: 8 * time.Millisecond,
    MaxRetryBackoff: 512 * time.Millisecond,
    
    EnableMetrics: true,
}

client, err := redisx.NewClient(config)
```

### 开发环境简化配置
```go
config := redisx.DefaultConfig()
config.Addr = "localhost:6379"
client, err := redisx.NewClient(config)
```

## Pipeline 最佳实践

### ✅ 推荐用法

#### 批量写入
```go
pipeline := client.NewPipeline()
for i := 0; i < 1000; i++ {
    key := fmt.Sprintf("user:%d", i)
    pipeline.Set(ctx, key, userData, time.Hour)
}
_, err := pipeline.Exec(ctx)
```

#### 批量读取
```go
pipeline := client.NewPipeline()
var cmds []*redis.StringCmd

for _, key := range keys {
    cmd := pipeline.Get(ctx, key)
    cmds = append(cmds, cmd)
}

_, err := pipeline.Exec(ctx)

// 获取结果
for _, cmd := range cmds {
    value, err := cmd.Result()
    // 处理结果
}
```

#### 混合操作
```go
pipeline := client.NewPipeline()
pipeline.Set(ctx, "counter", 0, time.Hour)
pipeline.Incr(ctx, "visits")
pipeline.HSet(ctx, "user:123", "last_login", time.Now())
_, err := pipeline.Exec(ctx)
```

### ❌ 避免的用法

```go
// ❌ 单个操作不要用Pipeline
pipeline := client.NewPipeline()
pipeline.Set(ctx, "key", "value", time.Hour)
pipeline.Exec(ctx)

// ✅ 直接使用
client.Set(ctx, "key", "value", time.Hour)
```

## 错误处理模式

### 基本错误处理
```go
result := client.Get(ctx, "key")
if err := result.Err(); err != nil {
    if err == redis.Nil {
        // 键不存在，正常情况
        return nil, nil
    }
    // 其他错误
    return nil, fmt.Errorf("redis get failed: %w", err)
}
return result.Val(), nil
```

### Pipeline 错误处理
```go
pipeline := client.NewPipeline()
cmd1 := pipeline.Set(ctx, "key1", "value1", time.Hour)
cmd2 := pipeline.Get(ctx, "key2")

_, err := pipeline.Exec(ctx)
if err != nil {
    // Pipeline执行失败
    return fmt.Errorf("pipeline failed: %w", err)
}

// 检查每个命令的结果
if err := cmd1.Err(); err != nil {
    log.Printf("SET failed: %v", err)
}

value, err := cmd2.Result()
if err != nil && err != redis.Nil {
    log.Printf("GET failed: %v", err)
}
```

### 重试机制
```go
func withRetry(fn func() error) error {
    var lastErr error
    
    for i := 0; i < 3; i++ {
        if err := fn(); err != nil {
            if redisx.IsRetryableError(err) {
                lastErr = err
                time.Sleep(time.Duration(i*100) * time.Millisecond)
                continue
            }
            return err
        }
        return nil
    }
    
    return fmt.Errorf("max retries exceeded: %w", lastErr)
}
```

## 性能优化技巧

### 1. 批量大小选择
```go
// 根据网络条件选择合适的批量大小
var batchSize int
if isLocalNetwork {
    batchSize = 50   // 本地网络
} else {
    batchSize = 200  // 远程网络
}

pipeline := client.NewPipeline().SetBatchSize(batchSize)
```

### 2. 连接池调优
```go
// 监控连接池状态
stats := client.GetStats()
hitRate := float64(stats.Hits) / float64(stats.Hits + stats.Misses) * 100

if hitRate < 90 {
    // 考虑增加连接池大小
    log.Printf("Pool hit rate low: %.2f%%", hitRate)
}

if stats.Timeouts > 0 {
    // 连接超时，可能需要增加连接数或调整超时时间
    log.Printf("Pool timeouts: %d", stats.Timeouts)
}
```

### 3. 上下文使用
```go
// ✅ 总是使用带超时的context
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

result := client.Get(ctx, "key")

// ✅ 批量操作使用较长超时
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

pipeline.Exec(ctx)
```

## 监控和指标

### 基本指标收集
```go
client := redisx.NewClient(config)

// 定期收集指标
go func() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for range ticker.C {
        metrics := client.GetMetrics()
        if metrics == nil {
            continue
        }
        
        stats := metrics.GetStats()
        
        // 发送到监控系统
        sendMetrics(map[string]interface{}{
            "redis.ops.total":     stats.CommandTotal,
            "redis.ops.success":   stats.CommandSuccess,
            "redis.ops.failure":   stats.CommandFailure,
            "redis.latency.avg":   stats.AvgLatencyMicros,
            "redis.latency.max":   stats.MaxLatencyMicros,
            "redis.pool.hits":     stats.PoolHits,
            "redis.pool.misses":   stats.PoolMisses,
        })
    }
}()
```

### 健康检查
```go
// 定期健康检查
func healthCheck(client *redisx.Client) error {
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()
    
    if err := client.HealthCheck(ctx); err != nil {
        return fmt.Errorf("redis health check failed: %w", err)
    }
    
    // 检查连接池状态
    stats := client.GetStats()
    if stats.TotalConns == 0 {
        return fmt.Errorf("no redis connections available")
    }
    
    return nil
}
```

## 常见陷阱与解决方案

### 1. 连接泄漏
```go
// ❌ 错误：忘记关闭客户端
func badExample() {
    client, _ := redisx.NewClient(config)
    client.Set(ctx, "key", "value", time.Hour)
    // 缺少 client.Close()
}

// ✅ 正确：确保关闭客户端
func goodExample() {
    client, err := redisx.NewClient(config)
    if err != nil {
        return err
    }
    defer client.Close()
    
    return client.Set(ctx, "key", "value", time.Hour).Err()
}
```

### 2. 超时设置不当
```go
// ❌ 错误：超时时间过短
config.ReadTimeout = 100 * time.Millisecond  // 太短

// ✅ 正确：根据业务需求设置合理超时
config.ReadTimeout = 2 * time.Second         // 合理
config.WriteTimeout = 2 * time.Second
```

### 3. Pipeline 使用错误
```go
// ❌ 错误：每个操作都创建新的Pipeline
for _, key := range keys {
    pipeline := client.NewPipeline()
    pipeline.Set(ctx, key, value, ttl)
    pipeline.Exec(ctx)
}

// ✅ 正确：复用Pipeline
pipeline := client.NewPipeline()
for _, key := range keys {
    pipeline.Set(ctx, key, value, ttl)
}
pipeline.Exec(ctx)
```

## 配置参数速查

| 参数 | 生产环境建议 | 开发环境建议 | 说明 |
|------|-------------|-------------|------|
| PoolSize | CPU核数×2-4 | 5-10 | 连接池大小 |
| MinIdleConns | PoolSize/2 | 2-5 | 最小空闲连接 |
| ReadTimeout | 2-5s | 1-3s | 读超时 |
| WriteTimeout | 2-5s | 1-3s | 写超时 |
| DialTimeout | 5-10s | 3-5s | 连接超时 |
| MaxRetries | 3 | 1 | 最大重试次数 |
| BatchSize | 100-200 | 50-100 | Pipeline批量大小 |

## 故障排查清单

### 连接问题
1. 检查Redis服务是否运行
2. 验证网络连通性
3. 确认认证信息正确
4. 检查防火墙设置

### 性能问题
1. 监控连接池命中率
2. 检查Pipeline批量大小
3. 分析慢查询日志
4. 监控网络延迟

### 内存问题
1. 检查连接池大小设置
2. 监控Redis内存使用
3. 分析大Key问题
4. 检查TTL设置

这个快速参考应该能帮助你在日常开发中快速找到所需的配置和最佳实践。记住始终根据实际环境进行基准测试和调优。 