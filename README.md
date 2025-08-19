# Redis 学习与实战 14 天计划（面向 Go 高并发与业务落地）

本计划旨在帮助你在 14 天内系统掌握 Redis 的核心原理、工程实践与高可用运维，最终能够在高并发业务中稳健应用（缓存、限流、锁、异步、队列、排行榜等），并用自研压测工具对方案进行验证与回归。

---

## 目标成果
- 能独立完成：缓存三大问题（穿透/击穿/雪崩）设计与实现，并具备观测与回归能力
- 能落地：分布式锁（含续租与 Fencing Token）、可靠队列（Streams）、限流（滑动窗口）
- 掌握：Redis 高可用（主从/哨兵/Cluster）、持久化（RDB/AOF/混合）、复制一致性与常见故障处理
- 具备：性能压测与容量规划能力（p50/p95/p99、慢查询、fork、AOF fsync 延迟）
- 建立：一套可复用的 Go 客户端基建（连接池、Pipeline、重试、单飞、降级、指标与日志）

---

## 预备与环境
- Go >= 1.21，推荐 `go-redis/v9`
- 本地/容器 Redis（建议装 docker 与 docker-compose 便于切换拓扑）
- 可选：`redis-benchmark`、`redis-cli`、`rdb-tools`、`prometheus + grafana`、`redis_exporter`

建议在本仓库采用如下目录（后续每天会逐步补齐）：
```
what_redis_can_do/
  cmd/
    demo/            # 小演示程序（缓存/锁/限流/队列）
  internal/
    cache/           # 缓存组件（接口 + 实现：cache-aside、单飞、逻辑过期等）
    lock/            # 分布式锁与续租、fencing
    ratelimit/       # 限流（固定/滑动窗口）
    stream/          # Streams 消费者组、重试/死信
    metrics/         # Prometheus 指标埋点
    redisx/          # go-redis 客户端封装（连接、pipeline、重试、超时）
  scripts/
    docker/          # redis/sentinel/cluster 一键启动脚本
    bench/           # redis-benchmark、自定义压测脚本
  README.md
  go.mod
```

---

## 学习计划：14 天逐日安排

### Day 1｜Redis 基础与命令面
- 目标：理解 Redis 角色与使用边界，能熟练使用基础命令与观测入口
- 理论要点：KV 存储、事件循环、内存模型、时间复杂度、常见误区（禁止线上 KEYS/FLUSHALL）
- 实操：
  - 使用容器启动 Redis，进入 `redis-cli`，练习 String/Hash/List/Set/ZSet 基本命令
  - `INFO`、`SLOWLOG`、`MONITOR`、`LATENCY DOCTOR` 初体验
- 复盘问题：
  - 为什么 Redis 单线程也能很快？哪些命令是 O(N) 易踩雷？
- 验收标准：
  - 完成一页常用命令速查笔记，列出 10+ 个线上禁用/慎用命令及替代方案

### Day 2｜核心数据结构与业务建模
- 目标：用 String/Hash/List/Set/ZSet 建模 5 个典型场景
- 理论要点：底层编码（int/raw/embstr/ziplist/quicklist/listpack）、大 Key 风险
- 实操：
  - 用 5 个例子完成：计数器、去重集、排行榜、会话、任务队列（list/lpush+rpop）
- 复盘问题：
  - 何为大 Key/热 Key？如何拆分与旁路？
- 验收标准：
  - 每个场景给出 Key 设计与 TTL 策略，提供可运行的 Go 小样例

### Day 3｜高级结构：Bitmap/HyperLogLog/Geo
- 目标：掌握低成本计数、地域检索与签到/活跃场景
- 理论要点：bitmap 位操作、HLL 误差特性、Geo 发布与检索范围
- 实操：
  - 活跃用户去重（HLL）与布点检索（Geo），实现查询封装
- 复盘问题：
  - 什么时候用 Set，什么时候用 HLL？Geo 的排序与半径搜索开销？
- 验收标准：
  - 输出对比表：精度/内存/适用场景/复杂度

### Day 4｜Go 客户端基建（go-redis/v9）
- 目标：掌握连接池、超时、重试、pipeline 的工程化用法
- 理论要点：`PoolSize/MinIdleConns/ReadTimeout/WriteTimeout/DialTimeout`、重试与幂等
- 实操：
  - 封装 `internal/redisx`：标准化创建客户端、健康检查、指标埋点
  - 实测 pipeline 批量与 RTT 降低
- 复盘问题：
  - pipeline 与事务的区别？什么时候用 TxPipeline？
- 验收标准：
  - 提供基准对比：单发 vs 批量（p95/吞吐）

### Day 5｜缓存模式与 TTL 设计
- 目标：掌握 Cache-Aside/Write-Through/Write-Behind
- 理论要点：一致性边界、二级缓存（本地 + Redis）、Key 命名规范
- 实操：
  - 在 `internal/cache` 实现通用 Cache-Aside 装饰器：加载函数、序列化、随机抖动 TTL
- 复盘问题：
  - 本地缓存如何与 Redis 协同？如何应对数据热度变化？
- 验收标准：
  - 接口清晰、可插拔的序列化器、带 metrics 的缓存组件

### Day 6｜缓存穿透防护
- 目标：掌握穿透的根因与多策略防护
- 理论要点：参数校验、空值缓存、布隆过滤器（可用 RedisBloom 或自建 bitmap bloom）
- 实操：
  - 在缓存组件中新增空值缓存策略 + 布隆过滤器拦截
- 复盘问题：
  - 空值缓存如何避免缓存污染？布隆误判如何处理？
- 验收标准：
  - 压测验证穿透流量下降（对比图表）

### Day 7｜缓存击穿防护
- 目标：掌握热点 Key 到期瞬时的保护
- 理论要点：互斥锁/单飞（singleflight）、逻辑过期
- 实操：
  - 实现单飞与逻辑过期两种策略；对比延迟与错误率
- 复盘问题：
  - 逻辑过期如何保证用户不感知数据刷新？
- 验收标准：
  - 在高并发压测下，热点 Key 到期不产生毛刺

### Day 8｜缓存雪崩治理
- 目标：处理大面积同时过期导致的崩溃风险
- 理论要点：TTL 抖动、分片过期、预热、限流/降级
- 实操：
  - 自动为批量设置的 TTL 添加抖动；增加熔断开关
- 复盘问题：
  - 读降级/只读模式如何切换？
- 验收标准：
  - 雪崩场景演练通过（CPU/延迟无尖峰）

### Day 9｜一致性与事务语义
- 目标：与下游数据一致性设计
- 理论要点：MULTI/EXEC、WATCH、Lua 原子性；最终一致、Outbox 与 CDC
- 实操：
  - 用 WATCH + 重试实现“扣库存 + 写缓存”一致；Lua 实现复杂原子更新
- 复盘问题：
  - Redis 事务的隔离级别与“弱隔离”边界是什么？
- 验收标准：
  - 多故障注入下仍无超卖与重复写

### Day 10｜分布式锁正确用法
- 目标：实现可用于生产的锁
- 理论要点：SET NX PX + 续租、租约漂移、Fencing Token；RedLock 争议
- 实操：
  - 在 `internal/lock` 实现锁：获取、自动续租、心跳、释放、Fencing Token 校验
- 复盘问题：
  - 为什么仅“释放脚本 + value 比对”还不够？
- 验收标准：
  - 故障注入（GC 停顿/网络分区）下无双写

### Day 11｜消息与异步（Streams）
- 目标：构建可靠队列与重试体系
- 理论要点：Pub/Sub 局限；Streams + 消费者组 + 待处理条目（PEL）
- 实操：
  - 在 `internal/stream` 封装消费者组，支持重试、回溯、死信
- 复盘问题：
  - 如何避免重复消费？如何保障至少一次/至多一次？
- 验收标准：
  - 压测下“稳定消费 + 可视化重试曲线”

### Day 12｜持久化与数据安全
- 目标：理解 RDB/AOF/混合持久化的权衡与恢复演练
- 理论要点：AOF everysec/always、AOF 重写、fork 与写时复制影响
- 实操：
  - 切换不同持久化策略做基准；模拟宕机恢复；验证数据丢失窗口
- 复盘问题：
  - 为什么 fork 会导致延迟尖峰？如何观测与缓解？
- 验收标准：
  - 提交一份“持久化策略建议”与“恢复手册”

### Day 13｜高可用与扩展（哨兵/Cluster）
- 目标：掌握主从复制、哨兵故障转移与 Cluster 分片
- 理论要点：复制延迟、半同步、Slot/重分片/迁移、热点分片与倾斜治理
- 实操：
  - 用脚本一键拉起 sentinel 与 3 节点 cluster，验证故障切换与 slot 迁移
- 复盘问题：
  - 读写分离下的一致性风险？如何做读旁路与幂等？
- 验收标准：
  - 故障注入/迁移期间服务无感或快速恢复

### Day 14｜可观测性、压测与演练收官
- 目标：构建指标/日志/追踪，形成常态化压测与演练流程
- 理论要点：关键指标（Ops/Hit Ratio/Evicted/Expired/Blocked/Slowlog/AOF fsync/Fork 时间）
- 实操：
  - 集成 redis_exporter + Prometheus + Grafana；自研压测器回放典型流量
- 复盘问题：
  - 你的 p99 指标在不同策略下如何变化？
- 验收标准：
  - 仓库提供“压测场景集合 + 策略开关 + 指标看板截图”

---

## 复盘题库（建议每日从中选做）
- Redis 为什么快？请按：协议/IO/事件/编码/指令复杂度 逐项解释
- 列举 10 个 O(N) 或可能阻塞的命令及替代
- 设计一个“库存扣减 + 缓存”的一致性方案（失败/超时/重试/幂等）
- 讲清：穿透/击穿/雪崩的根因、检测信号、治理优先级
- 何时用本地缓存？如何做容量与一致性控制？
- 如何识别热 Key 与大 Key？如何拆分？
- AOF 重写与 RDB 的权衡、混合持久化带来的收益与风险
- Sentinel 脑裂/误判的风险如何缓解？
- Streams“至少一次”如何去重？
- 分布式锁为什么需要 Fencing Token？如何生成与校验？

---

## 常用清单

### 线上高危命令（禁止/慎用）
- 禁止：`FLUSHALL`、`FLUSHDB`、`KEYS *`、`SAVE`、`BGREWRITEAOF`（视时机）
- 慎用：`SMEMBERS` 大集合、`LRANGE 0 -1` 大列表、`HGETALL` 大 Hash、`SORT`、`SUNION`、`SINTER`、`ZUNIONSTORE`
- 替代：`SCAN` 系列、分页、限制 COUNT、按业务索引拆分

### 淘汰策略与建议
- `allkeys-lru`：通用均衡；`volatile-ttl`：更偏向短 TTL；避免 `noeviction` 线上默认

### 指标与告警（建议阈值按环境调优）
- **性能**：p99 延迟、OPS、Pipeline 吞吐
- **内存**：内存使用率 > 70%、碎片率 > 1.5、Big Key 数量
- **持久化**：AOF fsync 延迟、重写耗时、RDB 保存周期
- **复制/高可用**：复制延迟、主从断连次数、Sentinel 选举次数
- **错误**：超时、OOM、BUSY、连接拒绝、阻塞客户端数

### Go 客户端最佳实践
- 所有调用携带 `context.Context`（含超时/截止时间）
- 连接池：`PoolSize` 与 CPU/并发匹配，预热 `MinIdleConns`
- 错误分类：超时/网络/数据/服务端繁忙；失败快速降级
- 批量优先：能 Pipeline 就 Pipeline，避免 chatty
- 幂等：幂等键 + 去重集合 + 短 TTL 防重放

---

## 压测与演示建议
- 构造可切换的策略开关：空值缓存、布隆、单飞、逻辑过期、TTL 抖动、限流、降级
- 录制/回放典型业务流量（读多写少/突发热点/批量过期）
- 输出报告：对比各策略下的 p50/p95/p99、错误率、CPU 与内存占用

---

## 参考资料

### 官方资源
- 官方文档：`https://redis.io/docs/latest/`
- Redis 命令：`https://redis.io/commands/`
- go-redis：`https://github.com/redis/go-redis`
- Redis 开源工具：`https://github.com/oliver006/redis_exporter`

### 本项目文档
- [Redis命令速查表](docs/redis-commands-cheatsheet.md) - 基础命令完整参考
- [危险命令清单](docs/dangerous-commands.md) - 生产环境风险防范
- [高级数据结构命令](docs/advanced-structures-commands.md) - Bitmap/HLL/Geo详细用法
- [高级结构陷阱防范](docs/advanced-structures-pitfalls.md) - 性能陷阱与最佳实践
- [高级结构快速参考](docs/advanced-structures-cheatcard.md) - 三分钟速查卡片

### 高质量文章与书籍
- Redis 设计与实现（黄健宏）
- Redis 深度历险
- 官方博客与 release note（关注持久化、复制、Cluster 变更）

---

## 如何使用本计划
1) 每天按“目标→要点→实操→复盘→验收”推进，记录关键参数与现象
2) 将代码与脚本逐步补齐到建议目录，形成可复用的组件与演示
3) 每完成一个主题，运行压测并截图指标曲线，沉淀到仓库
4) 第 14 天统一回归，形成一份“生产就绪清单”

> 提示：计划紧凑，可按工作节奏灵活拉长到 3~4 周，但最好保持“学-做-测-复盘”的闭环。
