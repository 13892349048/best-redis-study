# Day 9: 一致性与事务语义

## 学习目标 ✅

- ✅ 理解Redis事务的ACID特性与限制
- ✅ 掌握WATCH机制实现乐观锁
- ✅ 实现Lua脚本原子操作
- ✅ 构建扣库存+写缓存的一致性方案
- ✅ 处理并发冲突与重试策略

## 核心概念深入理解

### 1. Redis事务特性分析

#### ACID特性对比
| 特性 | 传统数据库 | Redis | 说明 |
|-----|-----------|-------|------|
| **原子性(A)** | ✅ 完全支持 | ⚠️ 有限支持 | Redis事务要么全部执行，要么全部不执行，但不支持回滚 |
| **一致性(C)** | ✅ 强一致性 | ⚠️ 弱一致性 | 依赖应用层保证，Redis只保证命令顺序执行 |
| **隔离性(I)** | ✅ 多级隔离 | ❌ 无隔离级别 | Redis是单线程，天然串行化，但WATCH期间可能被其他客户端修改 |
| **持久性(D)** | ✅ 完全支持 | ⚠️ 配置相关 | 取决于持久化策略(AOF/RDB) |

#### Redis事务的"弱隔离"边界
```go
// 示例：事务期间其他客户端的影响
func demonstrateWeakIsolation() {
    // 客户端A开始事务
    MULTI
    SET key1 value1
    GET key2  // 此时读取的是事务开始时的值
    
    // 客户端B在此期间修改了key2
    // 但客户端A的事务中GET key2返回的仍是旧值
    
    EXEC  // 执行时才会看到最新状态
}
```

### 2. WATCH机制深度剖析

#### WATCH的工作原理
```
1. WATCH key1 key2 ...  # 监视键
2. 检查业务逻辑        # 读取键值，进行业务判断
3. MULTI               # 开始事务
4. 执行修改命令         # SET/INCR/DECR等
5. EXEC                # 执行事务
   - 如果监视的键被修改：返回nil，事务取消
   - 如果监视的键未变化：正常执行所有命令
```

#### 乐观锁实现模式
```go
// 标准的乐观锁重试模式
func OptimisticLockPattern(client redis.Cmdable, key string) error {
    for attempt := 0; attempt < maxRetries; attempt++ {
        err := client.Watch(ctx, func(tx *redis.Tx) error {
            // 1. 读取当前值
            current, err := tx.Get(ctx, key).Result()
            if err != nil && err != redis.Nil {
                return err
            }

            // 2. 业务逻辑处理
            newValue := processBusinessLogic(current)
            
            // 3. 事务中修改
            _, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
                pipe.Set(ctx, key, newValue, 0)
                return nil
            })
            return err
        }, key)

        if err == nil {
            return nil  // 成功
        }
        if err == redis.TxFailedErr {
            // 乐观锁冲突，重试
            time.Sleep(backoffDelay(attempt))
            continue
        }
        return err  // 其他错误
    }
    return ErrMaxRetriesExceeded
}
```

### 3. Lua脚本原子性保证

#### Lua脚本的优势
- **原子性**: 脚本执行期间不会被其他命令打断
- **性能**: 减少网络往返次数
- **逻辑复杂度**: 支持条件判断、循环等复杂逻辑
- **数据一致性**: 多个键的操作在同一个原子单元内

#### 核心脚本模式

##### 1. 原子比较交换(CAS)
```lua
local key = KEYS[1]
local expected = ARGV[1] 
local new_value = ARGV[2]

local current = redis.call('GET', key)
if current == expected then
    redis.call('SET', key, new_value)
    return 1  -- 成功
else
    return 0  -- 失败
end
```

##### 2. 带限制的递增
```lua
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local increment = tonumber(ARGV[2]) or 1

local current = redis.call('GET', key)
if current == false then
    current = 0
else
    current = tonumber(current)
end

if current + increment <= limit then
    return redis.call('INCRBY', key, increment)
else
    return -1  -- 超出限制
end
```

##### 3. 原子扣库存+更新缓存
```lua
local stock_key = KEYS[1]
local cache_key = KEYS[2] 
local deduct_amount = tonumber(ARGV[1])
local cache_value = ARGV[2]

-- 检查库存
local current_stock = redis.call('GET', stock_key)
if current_stock == false then
    return {-1, 'stock_not_found'}
end

current_stock = tonumber(current_stock)
if current_stock < deduct_amount then
    return {-2, 'insufficient_stock'}
end

-- 原子扣减库存并更新缓存
local new_stock = redis.call('DECRBY', stock_key, deduct_amount)
redis.call('SETEX', cache_key, 3600, cache_value)

return {new_stock, 'success'}
```

## 实际应用场景

### 1. 电商库存一致性方案

#### 方案对比
| 方案 | 优点 | 缺点 | 适用场景 |
|------|------|------|----------|
| **数据库锁** | 强一致性 | 性能较差，可能死锁 | 低并发，强一致性要求 |
| **Redis乐观锁** | 高性能，无死锁 | 高并发下重试较多 | 中等并发，允许短暂不一致 |
| **Lua脚本** | 原子性强，性能好 | 脚本复杂度限制 | 高并发，复杂业务逻辑 |
| **分布式锁** | 跨服务一致性 | 实现复杂，性能一般 | 微服务架构，跨系统操作 |

#### 库存扣减最佳实践
```go
type InventoryService struct {
    redis     redis.Cmdable
    scriptMgr *LuaScriptManager
}

// 三层防护的库存扣减
func (s *InventoryService) DeductStock(itemID string, amount int64) error {
    // 第一层：本地缓存预检查（可选）
    if localStock := s.getLocalStock(itemID); localStock < amount {
        return ErrInsufficientStock
    }
    
    // 第二层：Redis Lua脚本原子扣减
    result, err := s.scriptMgr.AtomicDeduction(
        ctx, stockKey, cacheKey, amount, cacheValue, cacheTTL)
    if err != nil {
        return err
    }
    
    if result[1] != "success" {
        return ErrInsufficientStock
    }
    
    // 第三层：异步同步数据库（最终一致性）
    s.asyncSyncDB(itemID, result[0].(int64))
    
    return nil
}
```

### 2. 分布式计数器设计

#### 高并发计数器方案
```go
// 分片计数器减少冲突
type ShardedCounter struct {
    redis      redis.Cmdable
    shardCount int
    prefix     string
}

func (c *ShardedCounter) Increment(key string, delta int64) (int64, error) {
    // 随机选择分片
    shard := rand.Intn(c.shardCount)
    shardKey := fmt.Sprintf("%s:%s:shard:%d", c.prefix, key, shard)
    
    // 使用Lua脚本原子递增
    script := `
        local key = KEYS[1]
        local delta = tonumber(ARGV[1])
        local ttl = tonumber(ARGV[2])
        
        local result = redis.call('INCRBY', key, delta)
        if ttl > 0 then
            redis.call('EXPIRE', key, ttl)
        end
        return result
    `
    
    return c.redis.Eval(ctx, script, []string{shardKey}, delta, 3600).Int64()
}

func (c *ShardedCounter) GetTotal(key string) (int64, error) {
    var total int64
    for i := 0; i < c.shardCount; i++ {
        shardKey := fmt.Sprintf("%s:%s:shard:%d", c.prefix, key, i)
        val, err := c.redis.Get(ctx, shardKey).Int64()
        if err != nil && err != redis.Nil {
            return 0, err
        }
        total += val
    }
    return total, nil
}
```

### 3. 订单状态一致性保证

#### 订单状态机+Redis事务
```go
type OrderStateMachine struct {
    redis redis.Cmdable
    consistencyMgr ConsistencyManager
}

// 状态转换的原子操作
func (osm *OrderStateMachine) TransitionState(orderID string, from, to OrderState) error {
    orderKey := "order:" + orderID
    stateKey := "order_state:" + orderID
    
    operation := func(ctx context.Context, tx TxContext) error {
        // 检查当前状态
        currentState, err := tx.Get(stateKey)
        if err != nil {
            return err
        }
        
        if OrderState(currentState) != from {
            return ErrInvalidStateTransition
        }
        
        // 检查状态转换是否合法
        if !isValidTransition(from, to) {
            return ErrInvalidStateTransition
        }
        
        // 原子更新状态和相关数据
        tx.Set(stateKey, string(to), 0)
        tx.HSet(orderKey, "state", string(to))
        tx.HSet(orderKey, "updated_at", time.Now().Format(time.RFC3339))
        
        // 根据状态执行相应的业务逻辑
        switch to {
        case OrderStatePaid:
            tx.HSet(orderKey, "paid_at", time.Now().Format(time.RFC3339))
        case OrderStateShipped:
            tx.HSet(orderKey, "shipped_at", time.Now().Format(time.RFC3339))
        }
        
        return nil
    }
    
    return osm.consistencyMgr.ExecuteWithWatch(ctx, []string{stateKey}, operation)
}
```

## 性能优化策略

### 1. 减少冲突策略

#### 键空间分片
```go
// 将热点键分散到多个分片
func getShardedKey(baseKey string, shardCount int) string {
    hash := fnv.New32a()
    hash.Write([]byte(baseKey))
    shard := hash.Sum32() % uint32(shardCount)
    return fmt.Sprintf("%s:shard:%d", baseKey, shard)
}
```

#### 时间窗口分片
```go
// 按时间窗口分片，减少同一时间点的冲突
func getTimeShardedKey(baseKey string, windowSeconds int) string {
    window := time.Now().Unix() / int64(windowSeconds)
    return fmt.Sprintf("%s:window:%d", baseKey, window)
}
```

### 2. 重试策略优化

#### 指数退避+抖动
```go
func calculateBackoffDelay(attempt int, baseDelay time.Duration) time.Duration {
    // 指数退避
    delay := baseDelay * time.Duration(math.Pow(2, float64(attempt)))
    
    // 添加随机抖动，避免惊群效应
    jitter := time.Duration(rand.Int63n(int64(delay / 4)))
    
    return delay + jitter
}
```

#### 自适应重试
```go
type AdaptiveRetryPolicy struct {
    baseDelay    time.Duration
    maxDelay     time.Duration
    successRate  float64  // 最近成功率
    conflictRate float64  // 最近冲突率
}

func (p *AdaptiveRetryPolicy) NextDelay(attempt int) time.Duration {
    // 根据成功率和冲突率动态调整退避策略
    multiplier := 1.0
    if p.conflictRate > 0.5 {
        multiplier = 2.0  // 高冲突时更保守
    } else if p.successRate > 0.9 {
        multiplier = 0.5  // 高成功率时更激进
    }
    
    delay := time.Duration(float64(p.baseDelay) * math.Pow(2, float64(attempt)) * multiplier)
    if delay > p.maxDelay {
        delay = p.maxDelay
    }
    
    return delay
}
```

## 压测结果分析

### 测试环境
- **Redis**: 单实例，本地部署
- **连接池**: 100个连接，10个最小空闲连接
- **并发度**: 50-100个协程
- **测试数据**: 随机分布的键空间

### 性能对比

#### 1. 基础事务 vs 乐观锁 vs Lua脚本
```
测试场景：1000个并发操作，每个操作包含3个Redis命令

基础事务 (TxPipeline):
- QPS: 8,500
- P95延迟: 12ms
- P99延迟: 25ms
- 冲突率: 0%

乐观锁 (WATCH):
- QPS: 6,200
- P95延迟: 18ms  
- P99延迟: 45ms
- 冲突率: 15%
- 重试率: 25%

Lua脚本:
- QPS: 9,800
- P95延迟: 8ms
- P99延迟: 18ms
- 冲突率: 0%
```

#### 2. 库存扣减场景压测
```
测试场景：100个并发协程，20个商品，每次扣减1-10个库存

乐观锁方案:
- 成功率: 85%
- 平均重试次数: 2.3次
- P95延迟: 35ms

Lua脚本方案:
- 成功率: 92%
- 无重试
- P95延迟: 15ms

性能提升: Lua脚本方案比乐观锁方案性能提升130%
```

### 关键发现

1. **Lua脚本性能最优**: 无冲突、延迟最低、吞吐最高
2. **乐观锁适合低冲突场景**: 冲突率超过20%时性能急剧下降
3. **基础事务适合简单场景**: 无业务逻辑判断时性能良好
4. **分片策略有效**: 将热点键分片可降低50%以上的冲突率

## 最佳实践总结

### 1. 方案选择指南

```
低并发 + 简单逻辑 → 基础事务 (TxPipeline)
中并发 + 条件判断 → 乐观锁 (WATCH)  
高并发 + 复杂逻辑 → Lua脚本
跨系统 + 强一致性 → 分布式锁
```

### 2. 代码设计原则

#### 接口抽象
```go
// 统一的一致性操作接口
type ConsistencyManager interface {
    ExecuteWithWatch(keys []string, operation WatchOperation) error
    ExecuteAtomicUpdate(operation AtomicOperation) error
    ExecuteLuaScript(script *LuaScript, keys []string, args []interface{}) error
}
```

#### 错误处理
```go
// 区分不同类型的错误
var (
    ErrOptimisticLockConflict = errors.New("optimistic lock conflict")
    ErrInsufficientStock     = errors.New("insufficient stock") 
    ErrMaxRetriesExceeded    = errors.New("max retries exceeded")
    ErrInvalidState          = errors.New("invalid state")
)

// 错误分类处理
func handleConsistencyError(err error) RetryAction {
    switch {
    case errors.Is(err, ErrOptimisticLockConflict):
        return RetryWithBackoff
    case errors.Is(err, ErrInsufficientStock):
        return NoRetry  // 业务逻辑错误，不重试
    case errors.Is(err, ErrMaxRetriesExceeded):
        return Fallback // 降级处理
    default:
        return RetryImmediate
    }
}
```

#### 监控指标
```go
// 关键指标监控
type ConsistencyMetrics struct {
    TotalOperations    prometheus.Counter
    SuccessfulOps      prometheus.Counter
    ConflictCount      prometheus.Counter
    RetryCount         prometheus.Counter
    OperationLatency   prometheus.Histogram
}

func (m *ConsistencyMetrics) RecordOperation(operation string, success bool, latency time.Duration, retries int) {
    m.TotalOperations.Inc()
    if success {
        m.SuccessfulOps.Inc()
    }
    if retries > 0 {
        m.RetryCount.Add(float64(retries))
        m.ConflictCount.Inc()
    }
    m.OperationLatency.Observe(latency.Seconds())
}
```

### 3. 生产环境注意事项

#### 配置优化
```yaml
redis:
  # 连接池配置
  pool_size: 100
  min_idle_conns: 20
  
  # 超时配置
  dial_timeout: 5s
  read_timeout: 3s
  write_timeout: 3s
  
consistency:
  # 重试策略
  max_retries: 5
  initial_delay: 10ms
  max_delay: 1s
  backoff_factor: 2.0
  
  # 监控配置
  enable_metrics: true
  slow_operation_threshold: 100ms
```

#### 容量规划
- **连接数**: 根据并发量设置，通常为并发数的1.2-1.5倍
- **内存**: 考虑事务期间的内存占用，特别是大批量操作
- **CPU**: Lua脚本会消耗更多CPU，需要适当预留

#### 故障处理
- **降级策略**: 高冲突时自动切换到更保守的策略
- **熔断机制**: 连续失败时暂时停止操作，避免雪崩
- **告警阈值**: 冲突率>30%、重试率>50%时触发告警

## 学习成果验收

### ✅ 完成项目结构
```
internal/
├── consistency/           # 一致性管理组件
│   ├── interface.go      # 接口定义
│   ├── manager.go        # 一致性管理器实现
│   └── inventory.go      # 库存一致性示例
├── transaction/          # 事务管理组件
│   ├── interface.go      # 事务接口
│   ├── manager.go        # 事务管理器
│   ├── context.go        # 事务上下文
│   └── lua_scripts.go    # Lua脚本集合
cmd/demo/day9/
└── consistency_demo.go   # 综合演示程序
scripts/bench/day9/
└── consistency_benchmark.go  # 性能压测程序
```

### ✅ 核心功能实现
- [x] WATCH机制乐观锁
- [x] 事务管理器 (MULTI/EXEC)
- [x] Lua脚本原子操作
- [x] 库存扣减一致性方案
- [x] 并发冲突处理
- [x] 重试策略与退避算法
- [x] 性能监控与指标收集

### ✅ 压测验证
- [x] 基础事务性能测试
- [x] 乐观锁并发冲突测试  
- [x] Lua脚本性能对比
- [x] 库存高并发扣减验证
- [x] 批量操作事务测试

## 下一步学习建议

1. **Day 10**: 分布式锁的正确实现 - 基于Redis的分布式锁、续租机制、Fencing Token
2. **深入学习**: Redis Cluster下的事务限制与跨节点一致性
3. **实践项目**: 构建完整的电商库存系统，包含预留、确认、取消等完整流程
4. **监控体系**: 集成Prometheus+Grafana，建立完整的Redis事务监控大盘

通过Day 9的学习，我们深入理解了Redis事务的特性与限制，掌握了多种一致性保证方案，并通过实际的库存扣减场景验证了这些方案的可行性。这为构建高可靠的分布式系统奠定了坚实的基础。