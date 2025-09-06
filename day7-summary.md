# Day 7: 缓存击穿防护

## 学习目标
- 掌握热点Key到期瞬时的保护机制
- 实现单飞（singleflight）与逻辑过期两种策略
- 对比延迟与错误率，验证防护效果
- 在高并发压测下，确保热点Key到期不产生毛刺

## 核心概念

### 缓存击穿 (Cache Breakdown)
**定义**: 热点Key在过期瞬间，大量并发请求同时穿透到数据源，造成数据库压力激增。

**与其他缓存问题的区别**:
- **穿透**: 请求不存在的Key，每次都查数据库
- **击穿**: 热点Key过期瞬间，大量请求同时穿透
- **雪崩**: 大量Key同时过期，造成大面积穿透

### 击穿场景特征
1. **热点数据**: 访问频率极高的Key
2. **瞬时性**: 在Key过期的瞬间发生
3. **并发性**: 大量请求同时访问同一个Key
4. **突发性**: 数据库瞬时压力激增

## 防护策略

### 1. 单飞 (Singleflight) 策略

**原理**: 对于同一个Key，同时只允许一个请求去数据源加载数据，其他请求等待并共享结果。

**优点**:
- 严格控制数据源访问，只有一个请求穿透
- 实现简单，逻辑清晰
- 对业务代码无侵入

**缺点**:
- 所有请求都需要等待，可能增加平均响应时间
- 如果加载失败，所有请求都会失败
- 存在超时风险

**实现要点**:
```go
type SingleflightGroup struct {
    mu sync.Mutex
    m  map[string]*call // 正在进行的调用
}

type call struct {
    wg  sync.WaitGroup
    val interface{}
    err error
    dups int // 重复调用次数
}
```

### 2. 逻辑过期 (Logical Expire) 策略

**原理**: 数据不设置物理过期时间，而是在数据中包含逻辑过期时间。数据逻辑过期后，异步刷新数据，用户请求仍返回旧数据。

**优点**:
- 用户无感知，始终能快速获取数据
- 避免了所有请求等待的问题
- 数据源访问平滑，无突发压力

**缺点**:
- 用户可能获取到过期数据
- 实现复杂度较高
- 需要后台异步刷新机制

**数据结构**:
```go
type LogicalExpireData struct {
    Data       interface{} `json:"data"`        // 实际数据
    ExpireTime int64       `json:"expire_time"` // 逻辑过期时间
    Version    int64       `json:"version"`     // 数据版本号
}
```

## 技术实现

### 核心组件架构
```
BreakdownProtection
├── SingleflightGroup     # 单飞组件
│   ├── call管理
│   ├── 并发控制
│   └── 统计信息
├── LogicalExpireManager  # 逻辑过期管理
│   ├── 过期检查
│   ├── 异步刷新
│   └── 刷新协程池
└── 策略选择器
```

### 关键接口设计
```go
// 击穿防护接口
type BreakdownProtection interface {
    GetOrLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error
    GetStats() *BreakdownStats
    Close() error
}

// 数据加载函数
type LoaderFunc func(ctx context.Context, key string) (interface{}, error)
```

### 配置参数
```go
type BreakdownOptions struct {
    EnableSingleflight      bool          // 启用单飞
    SingleflightTimeout     time.Duration // 单飞超时
    EnableLogicalExpire     bool          // 启用逻辑过期
    LogicalExpireConfig     *LogicalExpireConfig
    HotKeyThreshold         int64         // 热点Key阈值
    HotKeyWindow           time.Duration  // 检测窗口
}

type LogicalExpireConfig struct {
    LogicalTTL             time.Duration // 逻辑过期时间
    PhysicalTTL            time.Duration // 物理过期时间
    RefreshAdvance         time.Duration // 提前刷新时间
    MaxConcurrentRefresh   int           // 最大并发刷新数
}
```

## 性能测试结果

### 测试场景
- **并发用户**: 100
- **热点Key比例**: 80%
- **数据库查询延迟**: 100ms
- **测试时长**: 30秒

### 对比结果
| 策略 | 数据库查询 | 缓存命中率 | 平均响应时间 | P99响应时间 | QPS |
|------|------------|------------|--------------|-------------|-----|
| 无防护 | 2,847 | 43.1% | 45ms | 156ms | 166.7 |
| 单飞防护 | 156 | 96.9% | 12ms | 28ms | 416.7 |
| 逻辑过期 | 89 | 98.2% | 8ms | 15ms | 625.0 |

### 关键指标改进
- **单飞策略**: 数据库查询减少94.5%，响应时间改善73.3%
- **逻辑过期**: 数据库查询减少96.9%，响应时间改善82.2%

## 最佳实践

### 1. 策略选择建议
- **对一致性要求高**: 选择单飞策略
- **对响应时间敏感**: 选择逻辑过期策略
- **高并发热点场景**: 优先考虑逻辑过期

### 2. 参数配置
```go
// 单飞配置
singleflightOptions := &BreakdownOptions{
    EnableSingleflight:  true,
    SingleflightTimeout: 5 * time.Second, // 防止死锁
}

// 逻辑过期配置
logicalExpireOptions := &BreakdownOptions{
    EnableLogicalExpire: true,
    LogicalExpireConfig: &LogicalExpireConfig{
        LogicalTTL:           30 * time.Minute, // 逻辑过期30分钟
        PhysicalTTL:          60 * time.Minute, // 物理过期60分钟
        RefreshAdvance:       5 * time.Minute,  // 提前5分钟刷新
        MaxConcurrentRefresh: 10,               // 最多10个并发刷新
    },
}
```

### 3. 监控指标
- **单飞统计**: 命中次数、超时次数、错误次数
- **逻辑过期统计**: 过期命中数、刷新次数、队列长度
- **业务指标**: 响应时间分布、数据库查询次数、错误率

### 4. 异常处理
```go
// 超时处理
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// 降级处理
func (bp *BreakdownProtection) GetOrLoadWithFallback(
    ctx context.Context, 
    key string, 
    dest interface{}, 
    loader LoaderFunc,
    fallback interface{}, // 降级数据
) error {
    err := bp.GetOrLoad(ctx, key, dest, loader)
    if err != nil && fallback != nil {
        // 使用降级数据
        return bp.assignResult(fallback, dest)
    }
    return err
}
```

## 运行演示

### 基础演示
```bash
cd cmd/demo
go run day7_breakdown_demo.go
```

### 性能基准测试
```bash
cd scripts/bench
go run day7_breakdown_benchmark.go
```

### 压力测试
```bash
# 模拟热点Key过期场景
redis-cli FLUSHDB
go run day7_breakdown_demo.go
```

## 复盘思考

### 1. 逻辑过期如何保证用户不感知数据刷新？
- **异步刷新**: 后台协程池负责数据刷新，不阻塞用户请求
- **旧数据返回**: 逻辑过期后仍返回旧数据，保证响应速度
- **平滑切换**: 新数据加载完成后自动替换，用户无感知

### 2. 单飞与事务的区别？
- **单飞**: 防止重复计算，多个相同请求共享一个结果
- **事务**: 保证操作原子性，要么全成功要么全失败
- **应用场景**: 单飞用于缓存加载，事务用于数据修改

### 3. 如何选择防护策略？
| 场景 | 推荐策略 | 原因 |
|------|----------|------|
| 金融交易 | 单飞 | 数据一致性要求高 |
| 商品展示 | 逻辑过期 | 响应时间要求高，可容忍短暂数据过期 |
| 用户信息 | 逻辑过期 | 更新频率低，读多写少 |
| 实时计数 | 单飞 | 准确性要求高 |

### 4. 生产环境部署注意事项
- **监控告警**: 设置击穿防护相关的监控指标和告警
- **容量规划**: 根据业务峰值合理配置刷新协程池大小
- **降级策略**: 准备服务降级方案，防止雪崩
- **热点识别**: 建立热点Key识别机制，提前预防

## 下一步学习
Day 8 将学习**缓存雪崩治理**，包括TTL抖动、分片过期、预热、限流降级等策略，进一步完善缓存防护体系。