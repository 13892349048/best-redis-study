# Redis 线上危险命令清单与替代方案

## 🔴 绝对禁止的命令（可能导致服务不可用）

### 1. FLUSHALL
**危险原因**: 清空所有数据库的所有数据，不可恢复
**影响**: 数据完全丢失，服务不可用
**替代方案**: 
- 开发/测试环境专用
- 生产环境应禁用此命令
- 如需清理，使用 `DEL` 命令批量删除特定键

### 2. FLUSHDB  
**危险原因**: 清空当前数据库所有数据
**影响**: 当前数据库数据完全丢失
**替代方案**:
- 使用 `SCAN` + `DEL` 批量删除
- 按业务模块使用键前缀，精确删除

### 3. KEYS *
**危险原因**: 遍历所有键，阻塞主线程
**影响**: 在大数据量时可能导致几秒到几分钟的阻塞
**替代方案**:
```bash
# 禁用
KEYS *
KEYS user:*

# 使用 SCAN 替代
SCAN 0 MATCH user:* COUNT 100
```

### 4. SAVE
**危险原因**: 同步保存RDB，阻塞所有客户端
**影响**: 在数据量大时阻塞几秒到几分钟
**替代方案**:
```bash
# 禁用
SAVE

# 使用后台保存
BGSAVE
```

### 5. DEBUG SEGFAULT
**危险原因**: 故意使Redis崩溃
**影响**: 服务立即不可用
**替代方案**: 生产环境应禁用所有DEBUG命令

---

## 🟡 高危命令（可能导致性能问题）

### 6. SMEMBERS（大集合）
**危险原因**: 返回集合所有成员，可能非常大
**影响**: 内存占用高，网络传输量大
**替代方案**:
```bash
# 禁用（大集合）
SMEMBERS large_set

# 使用迭代器
SSCAN large_set 0 COUNT 100
```

### 7. HGETALL（大Hash）
**危险原因**: 返回Hash所有字段和值
**影响**: 内存和网络开销大
**替代方案**:
```bash
# 禁用（大Hash）  
HGETALL large_hash

# 使用迭代器或按需获取
HSCAN large_hash 0 COUNT 100
HMGET large_hash field1 field2 field3
```

### 8. LRANGE 0 -1（大列表）
**危险原因**: 返回整个列表
**影响**: 内存和网络开销大
**替代方案**:
```bash
# 禁用
LRANGE large_list 0 -1

# 使用分页
LRANGE large_list 0 99    # 前100个
LRANGE large_list 100 199 # 第101-200个
```

### 9. SORT（无LIMIT）
**危险原因**: 排序大量数据，CPU密集
**影响**: 阻塞时间长，CPU使用率高
**替代方案**:
```bash
# 禁用
SORT large_list

# 使用LIMIT限制结果
SORT large_list LIMIT 0 100
# 或考虑使用有序集合 ZSET
```

### 10. SUNION/SINTER/SDIFF（大集合）
**危险原因**: 集合运算，可能产生大结果集
**影响**: 内存和CPU开销大
**替代方案**:
```bash
# 小心使用，先检查集合大小
SCARD set1
SCARD set2

# 如果结果集可能很大，考虑:
SUNIONSTORE result_set set1 set2  # 存储到临时键
EXPIRE result_set 300              # 设置过期时间
```

### 11. ZUNIONSTORE/ZINTERSTORE（大有序集合）
**危险原因**: 有序集合运算，内存和CPU密集
**影响**: 可能产生非常大的结果集
**替代方案**:
```bash
# 先检查大小
ZCARD zset1
ZCARD zset2

# 使用权重和聚合控制结果
ZUNIONSTORE result zset1 zset2 WEIGHTS 1 0.5 AGGREGATE SUM
EXPIRE result 300
```

### 12. EVAL（复杂脚本）
**危险原因**: 长时间运行的Lua脚本会阻塞
**影响**: 阻塞所有其他操作
**替代方案**:
```lua
-- 避免死循环和长时间运算
-- 分批处理，控制循环次数
local count = 0
for i = 1, 1000 do  -- 限制循环次数
    -- 处理逻辑
    count = count + 1
    if count >= 100 then
        break  -- 控制单次处理量
    end
end
```

---

## 🟠 需谨慎使用的命令

### 13. MONITOR
**危险原因**: 输出所有命令，影响性能
**影响**: 网络流量大，可能影响Redis性能
**使用建议**: 仅调试时短时间使用

### 14. CLIENT LIST
**危险原因**: 在客户端很多时返回大量数据
**影响**: 内存和网络开销
**使用建议**: 定期清理，避免频繁调用

### 15. CONFIG SET（某些参数）
**危险原因**: 动态修改可能影响稳定性
**影响**: 可能导致性能下降或不稳定
**使用建议**: 
```bash
# 谨慎修改这些参数
CONFIG SET maxmemory 1gb           # 内存限制
CONFIG SET maxmemory-policy noeviction  # 淘汰策略
CONFIG SET save "900 1 300 10"    # 持久化策略
```

### 16. BGREWRITEAOF
**危险原因**: AOF重写可能影响性能
**影响**: 短时间内磁盘IO和CPU使用率高
**使用建议**: 在业务低峰期执行

### 17. MIGRATE
**危险原因**: 数据迁移可能阻塞
**影响**: 大键迁移时可能阻塞
**使用建议**: 分批迁移，监控性能

---

## 📋 生产环境配置建议

### 1. 禁用危险命令
在 redis.conf 中禁用危险命令:
```conf
# 重命名危险命令
rename-command FLUSHDB ""
rename-command FLUSHALL ""
rename-command KEYS ""
rename-command SAVE ""
rename-command DEBUG ""

# 或重命名为复杂名称
rename-command SHUTDOWN SHUTDOWN_MENGKEXIAN
rename-command CONFIG CONFIG_MENGKEXIAN
```

### 2. 设置命令执行时间限制
```conf
# 设置慢查询阈值（微秒）
slowlog-log-slower-than 10000  # 10ms

# 慢查询日志长度
slowlog-max-len 128
```

### 3. 客户端连接限制
```conf
# 最大客户端连接数
maxclients 10000

# 客户端超时时间（秒）
timeout 300
```

### 4. 内存管理
```conf
# 最大内存限制
maxmemory 2gb

# 内存淘汰策略
maxmemory-policy allkeys-lru
```

---

## 🛡️ 监控与告警

### 关键指标监控
1. **慢查询**: `SLOWLOG GET 10`
2. **内存使用**: `INFO memory`
3. **客户端连接**: `INFO clients`
4. **命令统计**: `INFO commandstats`
5. **键空间**: `INFO keyspace`

### 告警阈值建议
```yaml
# Prometheus 告警规则示例
- alert: RedisSlowQuery
  expr: redis_slowlog_length > 10
  
- alert: RedisHighMemory
  expr: redis_memory_used_bytes / redis_memory_max_bytes > 0.8
  
- alert: RedisHighConnections
  expr: redis_connected_clients > 1000
  
- alert: RedisCommandLatency
  expr: redis_command_duration_seconds_total > 0.1
```

---

## 📚 最佳实践总结

1. **键设计**: 使用有意义的前缀，便于管理和清理
2. **过期时间**: 所有键都应设置合理的TTL
3. **批量操作**: 优先使用Pipeline和批量命令
4. **大键拆分**: 避免单个键过大，考虑分片
5. **监控告警**: 建立完善的监控体系
6. **权限控制**: 使用ACL限制不同用户的命令权限
7. **定期巡检**: 定期检查慢查询、大键、内存使用等

记住：**任何可能阻塞Redis主线程的操作都应该避免在生产环境中使用！** 