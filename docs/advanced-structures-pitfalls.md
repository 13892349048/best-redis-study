# Redis 高级数据结构使用陷阱与风险防范

## 🔴 高危操作（可能导致性能问题或数据丢失）

### 1. BITCOUNT 大型Bitmap全量统计
**危险原因**: 大型bitmap的全量统计会阻塞主线程
**影响**: 可能导致几秒到几十秒的阻塞
**替代方案**:
```bash
# 禁用（大型bitmap）
BITCOUNT active:all                    # 可能阻塞很久

# 使用分段统计
BITCOUNT active:daily:2025-01-21 0 100000     # 分段统计
BITCOUNT active:daily:2025-01-21 100001 200000

# 或按时间维度分片
BITCOUNT active:daily:2025-01-21      # 日维度分片
BITCOUNT active:weekly:2025-w04       # 周维度分片
```

### 2. GEORADIUS 大范围无限制查询
**危险原因**: 大范围查询可能返回大量结果，消耗大量CPU和内存
**影响**: 查询缓慢，可能导致超时或OOM
**替代方案**:
```bash
# 禁用
GEORADIUS drivers:all 116.4 39.9 100 km        # 范围过大
GEORADIUS drivers:dense_area 116.4 39.9 5 km   # 无COUNT限制

# 推荐做法
GEORADIUS drivers:beijing 116.4 39.9 5 km COUNT 50 ASC  # 限制范围和数量
GEORADIUS drivers:shanghai 121.5 31.2 3 km COUNT 20     # 按城市分片
```

### 3. 大量PFMERGE同时操作
**危险原因**: 合并大量HyperLogLog会消耗大量CPU
**影响**: 可能导致短时间阻塞
**替代方案**:
```bash
# 禁用
PFMERGE result uv:1 uv:2 uv:3 ... uv:100      # 一次合并太多

# 分批合并
PFMERGE temp1 uv:1 uv:2 uv:3 uv:4              # 先合并小批量
PFMERGE temp2 uv:5 uv:6 uv:7 uv:8
PFMERGE result temp1 temp2                     # 再合并结果
```

### 4. Bitmap稀疏ID存储
**危险原因**: 稀疏的用户ID会导致内存大量浪费
**影响**: 内存使用率极低，成本高昂
**替代方案**:
```bash
# 禁用
SETBIT active:all 999999999 1          # UUID或稀疏ID

# 使用Hash分片或Set
SADD active:today user_uuid_12345       # 改用Set存储UUID
HSET active:hash user_12345 1           # 或用Hash存储
```

---

## 🟡 性能风险操作（需要谨慎使用）

### 5. 频繁的Bitmap位运算
**危险原因**: 大型bitmap的位运算消耗CPU和内存
**影响**: 可能影响Redis整体性能
**使用建议**:
```bash
# 谨慎使用
BITOP AND result active:today active:yesterday     # 确保bitmap不会太大
BITOP OR result active:day1 active:day2 active:day3

# 优化建议
# 1. 预先检查bitmap大小
STRLEN active:today                               # 检查bitmap字节长度
# 2. 在业务低峰期执行位运算
# 3. 考虑异步处理结果
```

### 6. Geo数据热点区域
**危险原因**: 某些热点区域数据过多，查询性能下降
**影响**: 热点区域查询缓慢
**使用建议**:
```bash
# 监控数据密度
ZCARD drivers:beijing                   # 检查区域内数据量

# 热点区域进一步分片
GEOADD drivers:beijing:cbd 116.4 39.9 driver1     # CBD区域单独分片
GEOADD drivers:beijing:airport 116.6 40.1 driver2  # 机场区域单独分片
```

### 7. HyperLogLog小数据量使用
**危险原因**: 小数据量时HLL误差相对较大，不如直接计数
**影响**: 精度损失，业务决策偏差
**使用建议**:
```bash
# 数据量阈值判断
# < 1万数据：使用Set精确计数
SADD uv:small:today user1 user2                    # 小量数据用Set

# > 10万数据：使用HLL节省内存
PFADD uv:large:today user1 user2                   # 大量数据用HLL
```

### 8. Geo精度敏感场景
**危险原因**: GeoHash有约1米的量化误差
**影响**: 对精度要求极高的场景可能不满足需求
**使用建议**:
```bash
# 评估精度需求
# ✅ 适合场景：找附近餐厅、司机、朋友
# ❌ 不适合：厘米级定位、室内导航

# 精度要求高时的替代方案
HSET location:precise driver1 "116.397428,39.900000"  # 用Hash存储高精度坐标
```

---

## 🟠 数据一致性风险

### 9. Bitmap并发修改
**危险原因**: 多个客户端同时修改同一位可能导致竞态条件
**影响**: 数据不一致
**解决方案**:
```bash
# 使用Lua脚本保证原子性
local key = KEYS[1]
local offset = ARGV[1]
local value = ARGV[2]
local old_value = redis.call('GETBIT', key, offset)
if old_value == 0 then
    redis.call('SETBIT', key, offset, value)
    return 1
else
    return 0
end
```

### 10. Geo位置更新一致性
**危险原因**: 同一对象在不同时刻的位置更新可能乱序
**影响**: 位置信息不准确
**解决方案**:
```bash
# 使用时间戳确保更新顺序
local key = KEYS[1]
local member = ARGV[1]
local lng = ARGV[2]
local lat = ARGV[3]
local timestamp = ARGV[4]

-- 检查时间戳是否更新
local current_ts = redis.call('HGET', key .. ':ts', member)
if not current_ts or tonumber(timestamp) > tonumber(current_ts) then
    redis.call('GEOADD', key, lng, lat, member)
    redis.call('HSET', key .. ':ts', member, timestamp)
    return 1
else
    return 0
end
```

---

## 📊 内存与性能监控

### 关键监控指标

```bash
# 1. 内存使用监控
MEMORY USAGE active:daily:2025-01-21        # 单个bitmap内存
INFO memory                                 # 总内存使用

# 2. 慢查询监控
SLOWLOG GET 10                              # 重点关注BITCOUNT、GEORADIUS

# 3. 命令频率监控  
INFO commandstats                           # 统计各命令调用频率

# 4. 键空间监控
INFO keyspace                               # 各数据库键数量
SCAN 0 MATCH active:* COUNT 100             # 扫描特定模式的键
```

### 性能基准测试

```bash
# Bitmap性能测试
redis-benchmark -t set,get -n 100000 -d 1  # 基准SET性能
redis-benchmark -t bitop -n 1000           # BITOP性能测试

# Geo性能测试  
redis-benchmark -t georadius -n 10000      # GEORADIUS性能测试

# HLL性能测试
redis-benchmark -t pfadd,pfcount -n 100000 # HLL操作性能
```

---

## 🛡️ 生产环境最佳实践

### 1. 数据分片策略

```bash
# 时间维度分片
active:daily:2025-01-21           # 按日分片
active:weekly:2025-w04            # 按周分片
active:monthly:2025-01            # 按月分片

# 地理维度分片
geo:drivers:beijing               # 按城市分片
geo:drivers:shanghai              
geo:stores:zone:cbd               # 按区域分片

# 业务维度分片
bitmap:user:normal                # 按用户类型分片
bitmap:user:vip
hll:uv:mobile                     # 按平台分片
hll:uv:web
```

### 2. TTL策略配置

```bash
# Bitmap过期策略
EXPIRE active:daily:2025-01-21 2592000     # 30天过期
EXPIRE checkin:monthly:2025-01 31536000    # 1年过期

# HyperLogLog过期策略  
EXPIRE uv:daily:2025-01-21 2592000         # 30天过期
EXPIRE uv:weekly:2025-w04 7776000          # 90天过期

# Geo过期策略
EXPIRE drivers:online 14400                # 4小时过期（动态位置）
# stores:all 不设过期（静态位置）
```

### 3. 容量规划

| 数据结构 | 1万用户 | 10万用户 | 100万用户 | 1000万用户 |
|---------|---------|----------|-----------|-----------|
| **Bitmap** | 1.25KB | 12.5KB | 125KB | 1.25MB |
| **HyperLogLog** | 12KB | 12KB | 12KB | 12KB |
| **Set** | 200KB | 2MB | 20MB | 200MB |
| **Geo** | 800KB | 8MB | 80MB | 800MB |

### 4. 告警配置

```yaml
# Prometheus告警规则
groups:
- name: redis_advanced_structures
  rules:
  # Bitmap内存告警
  - alert: BitmapMemoryHigh
    expr: redis_memory_usage_bytes{key=~"active:.*"} > 50*1024*1024  # 50MB
    for: 5m
    
  # Geo查询延迟告警
  - alert: GeoQuerySlow  
    expr: redis_command_duration_seconds{cmd="georadius"} > 0.1
    for: 2m
    
  # HLL误差告警（需要业务埋点）
  - alert: HLLHighError
    expr: abs(hll_count - actual_count) / actual_count > 0.05
    for: 5m
```

---

## 🔧 故障排查指南

### 性能问题排查

```bash
# 1. 识别慢查询
SLOWLOG GET 100
# 重点关注：BITCOUNT、GEORADIUS、PFMERGE

# 2. 检查大键
redis-cli --bigkeys
# 关注bitmap和geo类型的大键

# 3. 内存分析
MEMORY DOCTOR
INFO memory
# 检查内存碎片和使用情况

# 4. 命令统计
INFO commandstats
# 分析各命令的调用频率和延迟
```

### 数据异常排查

```bash
# 1. Bitmap数据验证
BITCOUNT key                        # 检查置位数量
STRLEN key                          # 检查bitmap字节长度

# 2. HLL精度验证  
PFCOUNT key                         # HLL估算值
# 与实际统计对比验证精度

# 3. Geo数据验证
ZCARD geo_key                       # 检查位置点数量
GEORADIUS key lng lat 0.1 km        # 检查特定位置周围数据
```

---

## 📋 预防性维护检查清单

### 每日检查
- [ ] 慢查询日志审查（重点关注BITCOUNT、GEORADIUS）
- [ ] 内存使用量检查
- [ ] 大键识别和处理

### 每周检查  
- [ ] Bitmap数据密度分析
- [ ] Geo热点区域识别
- [ ] HLL精度抽样验证

### 每月检查
- [ ] 历史数据清理（过期TTL设置）
- [ ] 分片策略优化评估
- [ ] 容量规划调整

---

## ⚠️ 关键提醒

1. **选择合适的数据结构**: 根据数据量、精度要求和查询模式选择
2. **合理设置分片**: 避免单个键过大导致性能问题  
3. **监控内存使用**: 特别关注bitmap的内存效率
4. **控制查询范围**: Geo查询避免过大半径和无限制返回
5. **设置合理TTL**: 防止数据无限增长
6. **定期性能测试**: 在业务增长时重新评估性能表现

记住：**高级数据结构虽然强大，但使用不当反而会成为性能瓶颈！** 