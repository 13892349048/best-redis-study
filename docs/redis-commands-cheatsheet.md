# Redis 常用命令速查表

## 连接与服务器信息

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `PING` | 测试连接 | `PING` | O(1) |
| `INFO` | 服务器信息 | `INFO memory` | O(1) |
| `DBSIZE` | 当前数据库键数量 | `DBSIZE` | O(1) |
| `FLUSHDB` | 清空当前数据库 | `FLUSHDB` | O(N) |
| `FLUSHALL` | 清空所有数据库 | `FLUSHALL` | O(N) |
| `SELECT` | 切换数据库 | `SELECT 1` | O(1) |

## String 字符串操作

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `SET` | 设置键值 | `SET key value` | O(1) |
| `GET` | 获取值 | `GET key` | O(1) |
| `SETNX` | 不存在时设置 | `SETNX key value` | O(1) |
| `SETEX` | 设置带过期时间 | `SETEX key 60 value` | O(1) |
| `MSET` | 批量设置 | `MSET k1 v1 k2 v2` | O(N) |
| `MGET` | 批量获取 | `MGET k1 k2 k3` | O(N) |
| `INCR` | 自增1 | `INCR counter` | O(1) |
| `INCRBY` | 自增指定值 | `INCRBY counter 10` | O(1) |
| `DECR` | 自减1 | `DECR counter` | O(1) |
| `APPEND` | 追加字符串 | `APPEND key value` | O(1) |
| `STRLEN` | 字符串长度 | `STRLEN key` | O(1) |

## Hash 哈希操作

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `HSET` | 设置字段 | `HSET user:1 name tom` | O(1) |
| `HGET` | 获取字段 | `HGET user:1 name` | O(1) |
| `HMSET` | 批量设置字段 | `HMSET user:1 name tom age 20` | O(N) |
| `HMGET` | 批量获取字段 | `HMGET user:1 name age` | O(N) |
| `HGETALL` | 获取所有字段 | `HGETALL user:1` | O(N) |
| `HKEYS` | 获取所有字段名 | `HKEYS user:1` | O(N) |
| `HVALS` | 获取所有值 | `HVALS user:1` | O(N) |
| `HDEL` | 删除字段 | `HDEL user:1 age` | O(N) |
| `HEXISTS` | 字段是否存在 | `HEXISTS user:1 name` | O(1) |
| `HLEN` | 字段数量 | `HLEN user:1` | O(1) |
| `HINCRBY` | 字段自增 | `HINCRBY user:1 age 1` | O(1) |

## List 列表操作

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `LPUSH` | 左侧插入 | `LPUSH queue item1` | O(1) |
| `RPUSH` | 右侧插入 | `RPUSH queue item2` | O(1) |
| `LPOP` | 左侧弹出 | `LPOP queue` | O(1) |
| `RPOP` | 右侧弹出 | `RPOP queue` | O(1) |
| `LLEN` | 列表长度 | `LLEN queue` | O(1) |
| `LRANGE` | 范围获取 | `LRANGE queue 0 -1` | O(S+N) |
| `LINDEX` | 按索引获取 | `LINDEX queue 0` | O(N) |
| `LSET` | 按索引设置 | `LSET queue 0 newvalue` | O(N) |
| `LTRIM` | 裁剪列表 | `LTRIM queue 0 99` | O(N) |
| `BLPOP` | 阻塞左弹出 | `BLPOP queue 0` | O(1) |
| `BRPOP` | 阻塞右弹出 | `BRPOP queue 0` | O(1) |

## Set 集合操作

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `SADD` | 添加成员 | `SADD tags java python` | O(1) |
| `SREM` | 删除成员 | `SREM tags java` | O(1) |
| `SISMEMBER` | 成员是否存在 | `SISMEMBER tags java` | O(1) |
| `SMEMBERS` | 所有成员 | `SMEMBERS tags` | O(N) |
| `SCARD` | 成员数量 | `SCARD tags` | O(1) |
| `SPOP` | 随机弹出 | `SPOP tags` | O(1) |
| `SRANDMEMBER` | 随机获取 | `SRANDMEMBER tags 2` | O(N) |
| `SUNION` | 并集 | `SUNION tags1 tags2` | O(N) |
| `SINTER` | 交集 | `SINTER tags1 tags2` | O(N*M) |
| `SDIFF` | 差集 | `SDIFF tags1 tags2` | O(N) |

## ZSet 有序集合操作

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `ZADD` | 添加成员 | `ZADD scores 100 tom 95 jerry` | O(log(N)) |
| `ZREM` | 删除成员 | `ZREM scores tom` | O(log(N)) |
| `ZSCORE` | 获取分数 | `ZSCORE scores tom` | O(1) |
| `ZRANK` | 获取排名 | `ZRANK scores tom` | O(log(N)) |
| `ZREVRANK` | 获取逆序排名 | `ZREVRANK scores tom` | O(log(N)) |
| `ZRANGE` | 范围获取 | `ZRANGE scores 0 -1` | O(log(N)+M) |
| `ZREVRANGE` | 逆序范围获取 | `ZREVRANGE scores 0 -1` | O(log(N)+M) |
| `ZRANGEBYSCORE` | 按分数范围 | `ZRANGEBYSCORE scores 90 100` | O(log(N)+M) |
| `ZCARD` | 成员数量 | `ZCARD scores` | O(1) |
| `ZCOUNT` | 分数范围计数 | `ZCOUNT scores 90 100` | O(log(N)) |
| `ZINCRBY` | 增加分数 | `ZINCRBY scores 5 tom` | O(log(N)) |

## 键管理

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `EXISTS` | 键是否存在 | `EXISTS key` | O(1) |
| `DEL` | 删除键 | `DEL key1 key2` | O(N) |
| `TYPE` | 键类型 | `TYPE key` | O(1) |
| `EXPIRE` | 设置过期时间 | `EXPIRE key 60` | O(1) |
| `EXPIREAT` | 设置过期时间戳 | `EXPIREAT key 1609459200` | O(1) |
| `TTL` | 剩余生存时间 | `TTL key` | O(1) |
| `PERSIST` | 移除过期时间 | `PERSIST key` | O(1) |
| `RENAME` | 重命名键 | `RENAME oldkey newkey` | O(1) |
| `KEYS` | 查找键（慎用） | `KEYS pattern` | O(N) |
| `SCAN` | 迭代键 | `SCAN 0 MATCH pattern` | O(1) |

## 事务与脚本

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `MULTI` | 开启事务 | `MULTI` | O(1) |
| `EXEC` | 执行事务 | `EXEC` | O(N) |
| `DISCARD` | 取消事务 | `DISCARD` | O(1) |
| `WATCH` | 监视键 | `WATCH key` | O(1) |
| `UNWATCH` | 取消监视 | `UNWATCH` | O(1) |
| `EVAL` | 执行脚本 | `EVAL script 0` | O(1) |
| `EVALSHA` | 执行脚本SHA | `EVALSHA sha 0` | O(1) |

## 发布订阅

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `PUBLISH` | 发布消息 | `PUBLISH channel message` | O(N+M) |
| `SUBSCRIBE` | 订阅频道 | `SUBSCRIBE channel` | O(N) |
| `UNSUBSCRIBE` | 取消订阅 | `UNSUBSCRIBE channel` | O(N) |
| `PSUBSCRIBE` | 模式订阅 | `PSUBSCRIBE news.*` | O(N) |
| `PUNSUBSCRIBE` | 取消模式订阅 | `PUNSUBSCRIBE news.*` | O(N) |

## 监控与调试

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `MONITOR` | 监控所有命令 | `MONITOR` | - |
| `SLOWLOG` | 慢查询日志 | `SLOWLOG GET 10` | O(1) |
| `LATENCY` | 延迟诊断 | `LATENCY DOCTOR` | O(1) |
| `CLIENT LIST` | 客户端列表 | `CLIENT LIST` | O(N) |
| `CLIENT KILL` | 杀死客户端 | `CLIENT KILL ip:port` | O(N) |
| `CONFIG GET` | 获取配置 | `CONFIG GET maxmemory` | O(N) |
| `CONFIG SET` | 设置配置 | `CONFIG SET maxmemory 1gb` | O(N) |

## 备份与持久化

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `SAVE` | 同步保存 | `SAVE` | O(N) |
| `BGSAVE` | 后台保存 | `BGSAVE` | O(1) |
| `BGREWRITEAOF` | 重写AOF | `BGREWRITEAOF` | O(1) |
| `LASTSAVE` | 最后保存时间 | `LASTSAVE` | O(1) |

## 性能优化提示

### 1. 批量操作优化
- 使用 `MGET`、`MSET` 而不是多次 `GET`、`SET`
- 使用 Pipeline 减少 RTT
- 使用 `HMGET`、`HMSET` 批量操作 Hash

### 2. 大集合操作
- 避免 `SMEMBERS` 大集合，使用 `SSCAN`
- 避免 `HGETALL` 大 Hash，使用 `HSCAN`
- 避免 `LRANGE 0 -1` 大列表，使用分页

### 3. 模式匹配
- 避免 `KEYS *`，使用 `SCAN`
- 使用更具体的模式减少匹配范围

### 4. 内存优化
- 设置合适的 TTL 避免内存泄漏
- 使用压缩友好的数据结构
- 监控内存使用和碎片率

### 5. 网络优化
- 使用连接池复用连接
- 批量操作减少网络往返
- 设置合适的超时时间 