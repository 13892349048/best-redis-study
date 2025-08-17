# Day 1 学习总结：Redis 基础与命令面

## 📚 学习目标达成情况

✅ **目标**: 理解 Redis 角色与使用边界，能熟练使用基础命令与观测入口  
✅ **理论要点**: KV 存储、事件循环、内存模型、时间复杂度、常见误区  
✅ **实操**: 基础命令练习、监控命令体验  
✅ **验收标准**: 完成常用命令速查笔记，列出危险命令及替代方案  

## 🛠️ 环境准备

### 1. 项目目录结构
```
what_redis_can_do/
├── cmd/
│   └── demo/              # 演示程序
├── internal/
│   ├── cache/             # 缓存组件
│   ├── lock/              # 分布式锁
│   ├── ratelimit/         # 限流组件
│   ├── stream/            # Streams消费者
│   ├── metrics/           # 指标埋点
│   └── redisx/            # Redis客户端封装
├── scripts/
│   ├── docker/            # Docker启动脚本
│   └── bench/             # 压测脚本
└── docs/                  # 文档
```

### 2. Docker 环境搭建
- ✅ Redis 7.2 容器配置
- ✅ 开发友好的配置文件
- ✅ 一键启动脚本

### 3. Go 环境
- ✅ Go 1.21+ 
- ✅ go-redis/v9 客户端库

## 📖 理论知识总结

### Redis 为什么快？

1. **内存存储**: 所有数据存储在内存中，避免磁盘I/O
2. **单线程模型**: 避免多线程上下文切换和锁竞争
3. **事件驱动**: 基于 epoll/kqueue 的I/O多路复用
4. **高效数据结构**: 针对不同场景优化的底层编码
5. **简单协议**: Redis协议(RESP)简单高效

### 核心数据结构特性

| 类型 | 使用场景 | 时间复杂度 | 注意事项 |
|------|----------|------------|----------|
| String | 缓存、计数器、会话 | O(1) | 单个值最大512MB |
| Hash | 对象存储、字段更新 | O(1) | 适合小对象，避免大Hash |
| List | 队列、栈、时间线 | O(1)/O(N) | LRANGE全量查询慎用 |
| Set | 去重、交并差集 | O(1)/O(N) | SMEMBERS大集合慎用 |
| ZSet | 排行榜、范围查询 | O(log N) | 适合需要排序的场景 |

### 内存优化要点

1. **编码优化**: 小对象使用压缩编码（ziplist、intset等）
2. **过期策略**: 合理设置TTL，避免内存泄漏
3. **淘汰策略**: 生产环境推荐 `allkeys-lru`
4. **大Key拆分**: 避免单个Key过大，考虑分片

## ⚠️ 线上危险命令 (17个)

### 🔴 绝对禁止 (5个)
1. `FLUSHALL` - 清空所有数据库
2. `FLUSHDB` - 清空当前数据库  
3. `KEYS *` - 遍历所有键（阻塞）
4. `SAVE` - 同步保存RDB（阻塞）
5. `DEBUG SEGFAULT` - 故意崩溃

### 🟡 高危命令 (7个)
6. `SMEMBERS` (大集合) - 返回所有成员
7. `HGETALL` (大Hash) - 返回所有字段
8. `LRANGE 0 -1` (大列表) - 返回整个列表
9. `SORT` (无LIMIT) - 排序大量数据
10. `SUNION/SINTER/SDIFF` (大集合) - 集合运算
11. `ZUNIONSTORE/ZINTERSTORE` (大有序集合) - 有序集合运算
12. `EVAL` (复杂脚本) - 长时间运行Lua脚本

### 🟠 需谨慎使用 (5个)
13. `MONITOR` - 监控所有命令
14. `CLIENT LIST` - 客户端列表
15. `CONFIG SET` - 动态修改配置
16. `BGREWRITEAOF` - AOF重写
17. `MIGRATE` - 数据迁移

## 🔧 替代方案

| 危险命令 | 安全替代 | 说明 |
|----------|----------|------|
| `KEYS *` | `SCAN 0 MATCH pattern COUNT 100` | 迭代器遍历 |
| `SMEMBERS large_set` | `SSCAN large_set 0 COUNT 100` | 分批获取 |
| `HGETALL large_hash` | `HSCAN` 或 `HMGET` | 按需获取 |
| `LRANGE 0 -1` | `LRANGE 0 99` | 分页查询 |
| `SAVE` | `BGSAVE` | 后台保存 |

## 🎯 实践演示

### 完成的演示代码
- ✅ **String操作**: SET/GET/INCR/MGET/MSET
- ✅ **Hash操作**: HSET/HGET/HMGET/HGETALL/HINCRBY  
- ✅ **List操作**: LPUSH/RPOP/LLEN/LRANGE
- ✅ **Set操作**: SADD/SISMEMBER/SMEMBERS/SRANDMEMBER
- ✅ **ZSet操作**: ZADD/ZREVRANGE/ZRANK/ZSCORE/ZRANGEBYSCORE
- ✅ **监控命令**: INFO/DBSIZE/SLOWLOG/CLIENT

### 运行方式

1. 启动Redis:
```bash
cd scripts/docker
./start.sh
```

2. 运行演示:
```bash
go run cmd/demo/day1_basics.go
```

## 📊 关键指标监控

### 必须监控的指标
1. **内存使用率**: `used_memory` / `maxmemory` > 80% 告警
2. **慢查询**: `slowlog-log-slower-than` 设置为 10ms
3. **客户端连接数**: 避免达到 `maxclients` 限制
4. **命令延迟**: P95/P99 延迟监控
5. **键空间事件**: 过期、淘汰事件

### Redis INFO 关键信息
```bash
# 内存信息
INFO memory

# 客户端信息  
INFO clients

# 命令统计
INFO commandstats

# 键空间信息
INFO keyspace
```

## 🎖️ 复盘问题

### Q1: 为什么 Redis 单线程也能很快？
**答**: 
1. 内存操作比磁盘快几个数量级
2. 避免了多线程上下文切换开销
3. 事件驱动的I/O多路复用模型
4. 高效的数据结构和算法实现
5. 简单的网络协议

### Q2: 哪些命令是 O(N) 易踩雷？
**答**:
- `KEYS *` - 遍历所有键
- `SMEMBERS` - 大集合所有成员
- `HGETALL` - 大Hash所有字段  
- `LRANGE 0 -1` - 整个列表
- `SORT` - 排序操作
- `SUNION/SINTER` - 集合运算
- `FLUSHDB/FLUSHALL` - 清空操作

### Q3: 生产环境配置建议？
**答**:
```conf
# 禁用危险命令
rename-command FLUSHDB ""
rename-command KEYS ""

# 内存管理
maxmemory 2gb
maxmemory-policy allkeys-lru

# 慢查询监控
slowlog-log-slower-than 10000
slowlog-max-len 128

# 持久化
save 900 1 300 10 60 10000
appendonly yes
appendfsync everysec
```

## 📋 下一步计划

### Day 2 目标: 核心数据结构与业务建模
- [ ] 深入学习底层编码优化
- [ ] 实现5个典型业务场景建模
- [ ] 大Key/热Key识别与处理
- [ ] 性能基准测试

### 准备工作
- [ ] 准备业务场景数据
- [ ] 搭建性能测试环境
- [ ] 编写基准测试工具


---

**Day 1 总结**: 成功搭建了Redis学习环境，掌握了基础命令操作，了解了线上使用的安全边界。为后续的深入学习打下了坚实基础。 