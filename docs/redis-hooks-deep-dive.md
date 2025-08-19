# Redis Hook 机制深度解析

## 什么是 Hook？

Hook（钩子）是一种设计模式，允许你在某个过程的特定点插入自定义逻辑。在 go-redis 中，Hook 让我们能够在 Redis 操作的不同阶段注入自定义代码，比如：

- 记录性能指标
- 日志记录
- 请求追踪
- 重试逻辑
- 熔断器

## go-redis 中的 Hook 类型

go-redis 提供了三种主要的 Hook 接口：

### 1. ProcessHook - 单命令处理钩子
```go
type ProcessHook func(next ProcessHook) ProcessHook

type ProcessHook func(ctx context.Context, cmd Cmder) error
```

### 2. ProcessPipelineHook - Pipeline 处理钩子
```go
type ProcessPipelineHook func(next ProcessPipelineHook) ProcessPipelineHook

type ProcessPipelineHook func(ctx context.Context, cmds []Cmder) error
```

### 3. DialHook - 连接建立钩子
```go
type DialHook func(next DialHook) DialHook

type DialHook func(ctx context.Context, network, addr string) (net.Conn, error)
```

## Hook 的工作原理

### 装饰器模式（Decorator Pattern）

Hook 使用装饰器模式，通过层层包装来增强原有功能：

```go
// 原始函数
func originalProcess(ctx context.Context, cmd Cmder) error {
    // Redis 核心处理逻辑
    return executeCommand(cmd)
}

// Hook 包装
func metricsHook(next ProcessHook) ProcessHook {
    return func(ctx context.Context, cmd Cmder) error {
        // 前置处理
        start := time.Now()
        
        // 调用下一个 Hook 或原始函数
        err := next(ctx, cmd)
        
        // 后置处理
        recordMetrics(time.Since(start), err)
        return err
    }
}
```

### 调用链

多个 Hook 形成一个调用链：

```
Request → Hook1 → Hook2 → Hook3 → Redis Core → Hook3 → Hook2 → Hook1 → Response
```

## 我们的 MetricsHook 实现详解

### 1. ProcessHook - 单命令指标收集

```go
// ProcessHook 处理钩子
func (h *MetricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        // === 前置处理：命令开始前 ===
        start := time.Now()
        h.metrics.IncCommandTotal()  // 增加命令总数
        
        // === 执行核心逻辑 ===
        err := next(ctx, cmd)  // 调用下一个Hook或Redis核心逻辑
        
        // === 后置处理：命令完成后 ===
        if err != nil {
            h.metrics.IncCommandFailure()  // 记录失败
        } else {
            h.metrics.IncCommandSuccess()  // 记录成功
        }
        
        h.metrics.RecordLatency(time.Since(start))  // 记录延迟
        
        return err  // 返回原始错误，不修改业务逻辑
    }
}
```

**关键点解析：**

1. **时间测量**：在命令执行前记录开始时间，执行后计算延迟
2. **计数器**：区分成功和失败的命令数量
3. **透明性**：不修改原始的错误和返回值
4. **线程安全**：使用原子操作更新指标

### 2. ProcessPipelineHook - Pipeline 指标收集

```go
func (h *MetricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
    return func(ctx context.Context, cmds []redis.Cmder) error {
        start := time.Now()
        
        // === Pipeline 级别的指标 ===
        h.metrics.IncPipelineTotal()                    // Pipeline 总数
        h.metrics.AddPipelineBatch(int64(len(cmds)))    // 批次大小
        
        // === 执行 Pipeline ===
        err := next(ctx, cmds)
        
        // === 统计每个命令的结果 ===
        for _, cmd := range cmds {
            h.metrics.IncCommandTotal()
            if cmd.Err() != nil {
                h.metrics.IncCommandFailure()
            } else {
                h.metrics.IncCommandSuccess()
            }
        }
        
        h.metrics.RecordLatency(time.Since(start))
        return err
    }
}
```

**Pipeline Hook 特点：**

1. **批量处理**：一次性处理多个命令
2. **分层统计**：既统计 Pipeline 级别，也统计命令级别
3. **批次效率**：记录批量大小以分析批量效果

### 3. DialHook - 连接级别监控

```go
func (h *MetricsHook) DialHook(next redis.DialHook) redis.DialHook {
    return func(ctx context.Context, network, addr string) (net.Conn, error) {
        start := time.Now()
        
        // === 尝试建立连接 ===
        conn, err := next(ctx, network, addr)
        
        // === 记录连接结果 ===
        if err != nil {
            h.metrics.IncPoolTimeouts()  // 连接失败/超时
        } else {
            h.metrics.IncPoolHits()      // 连接成功
        }
        
        h.metrics.RecordLatency(time.Since(start))
        return conn, err
    }
}
```

## Hook 注册机制

### 在客户端中注册 Hook

```go
// 在 Client 创建时注册 Hook
func (c *Client) addHooks() {
    hook := &MetricsHook{metrics: c.metrics}
    c.AddHook(hook)  // go-redis 会自动识别 Hook 的类型并注册
}
```

### go-redis 内部的 Hook 识别

go-redis 使用反射来识别 Hook 类型：

```go
// go-redis 内部实现（简化版）
func (c *Client) AddHook(hook interface{}) {
    if h, ok := hook.(interface {
        ProcessHook(ProcessHook) ProcessHook
    }); ok {
        c.addProcessHook(h.ProcessHook)
    }
    
    if h, ok := hook.(interface {
        ProcessPipelineHook(ProcessPipelineHook) ProcessPipelineHook
    }); ok {
        c.addProcessPipelineHook(h.ProcessPipelineHook)
    }
    
    if h, ok := hook.(interface {
        DialHook(DialHook) DialHook
    }); ok {
        c.addDialHook(h.DialHook)
    }
}
```

## 高级 Hook 应用示例

### 1. 日志记录 Hook

```go
type LoggingHook struct {
    logger *log.Logger
}

func (h *LoggingHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        start := time.Now()
        
        // 记录命令开始
        h.logger.Printf("Redis command started: %s", cmd.Name())
        
        err := next(ctx, cmd)
        
        // 记录命令结果
        duration := time.Since(start)
        if err != nil {
            h.logger.Printf("Redis command failed: %s, duration: %v, error: %v", 
                cmd.Name(), duration, err)
        } else {
            h.logger.Printf("Redis command succeeded: %s, duration: %v", 
                cmd.Name(), duration)
        }
        
        return err
    }
}
```

### 2. 重试 Hook

```go
type RetryHook struct {
    maxRetries int
    backoff    time.Duration
}

func (h *RetryHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        var lastErr error
        
        for i := 0; i <= h.maxRetries; i++ {
            err := next(ctx, cmd)
            if err == nil {
                return nil  // 成功，直接返回
            }
            
            lastErr = err
            
            // 判断是否可重试
            if !isRetryableError(err) {
                break
            }
            
            if i < h.maxRetries {
                // 等待后重试
                time.Sleep(h.backoff * time.Duration(i+1))
            }
        }
        
        return lastErr
    }
}

func isRetryableError(err error) bool {
    // 网络错误、超时等可以重试
    return strings.Contains(err.Error(), "timeout") ||
           strings.Contains(err.Error(), "connection refused")
}
```

### 3. 熔断器 Hook

```go
type CircuitBreakerHook struct {
    breaker *CircuitBreaker
}

func (h *CircuitBreakerHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        // 检查熔断器状态
        if h.breaker.IsOpen() {
            return fmt.Errorf("circuit breaker is open")
        }
        
        err := next(ctx, cmd)
        
        // 更新熔断器状态
        if err != nil {
            h.breaker.RecordFailure()
        } else {
            h.breaker.RecordSuccess()
        }
        
        return err
    }
}
```

## Hook 性能考虑

### 1. 避免阻塞操作

```go
// ❌ 错误：在 Hook 中执行阻塞操作
func (h *BadHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        // 发送 HTTP 请求（阻塞）
        http.Post("http://metrics.example.com", "application/json", data)
        
        return next(ctx, cmd)
    }
}

// ✅ 正确：异步处理
func (h *GoodHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        start := time.Now()
        err := next(ctx, cmd)
        
        // 异步发送指标
        go func() {
            h.sendMetricsAsync(cmd.Name(), time.Since(start), err)
        }()
        
        return err
    }
}
```

### 2. 使用高效的数据结构

```go
// 使用原子操作而不是互斥锁
type Metrics struct {
    commandCount int64  // 使用 atomic.AddInt64
}

func (m *Metrics) IncCommandCount() {
    atomic.AddInt64(&m.commandCount, 1)
}
```

### 3. 条件化的指标收集

```go
func (h *MetricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        var start time.Time
        if h.collectLatency {  // 只在需要时测量延迟
            start = time.Now()
        }
        
        err := next(ctx, cmd)
        
        if h.collectLatency {
            h.metrics.RecordLatency(time.Since(start))
        }
        
        return err
    }
}
```

## 调试和测试 Hook

### 1. Hook 调试技巧

```go
type DebugHook struct {
    name string
}

func (h *DebugHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
    return func(ctx context.Context, cmd redis.Cmder) error {
        fmt.Printf("[%s] Before: %s\n", h.name, cmd.Name())
        
        err := next(ctx, cmd)
        
        fmt.Printf("[%s] After: %s, err: %v\n", h.name, cmd.Name(), err)
        return err
    }
}

// 使用多个 Hook 查看调用顺序
client.AddHook(&DebugHook{name: "Hook1"})
client.AddHook(&DebugHook{name: "Hook2"})
client.AddHook(&MetricsHook{metrics: metrics})
```

### 2. Hook 单元测试

```go
func TestMetricsHook(t *testing.T) {
    metrics := NewMetrics()
    hook := &MetricsHook{metrics: metrics}
    
    // 模拟成功的命令
    mockNext := func(ctx context.Context, cmd redis.Cmder) error {
        return nil
    }
    
    wrappedHook := hook.ProcessHook(mockNext)
    
    // 执行 Hook
    cmd := &mockCmd{name: "GET"}
    err := wrappedHook(context.Background(), cmd)
    
    // 验证指标
    assert.NoError(t, err)
    assert.Equal(t, int64(1), metrics.GetStats().CommandTotal)
    assert.Equal(t, int64(1), metrics.GetStats().CommandSuccess)
}
```

## 总结

Hook 机制的核心价值：

1. **非侵入性**：不修改业务逻辑的情况下增加功能
2. **可组合性**：多个 Hook 可以组合使用
3. **职责分离**：每个 Hook 专注于一个特定功能
4. **可测试性**：可以独立测试每个 Hook

我们的 MetricsHook 通过这种机制实现了：
- 实时性能监控
- 连接池状态跟踪
- Pipeline 效率分析
- 错误率统计

这种设计让监控和业务逻辑完全解耦，同时保持了高性能和可扩展性。 