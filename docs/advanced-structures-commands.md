# Redis 高级数据结构命令速查表

## Bitmap 位图操作

### 基础命令

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `SETBIT` | 设置位值 | `SETBIT active:today 1001 1` | O(1) |
| `GETBIT` | 获取位值 | `GETBIT active:today 1001` | O(1) |
| `BITCOUNT` | 统计位数 | `BITCOUNT active:today` | O(N) |
| `BITPOS` | 查找位置 | `BITPOS active:today 1` | O(N) |

### 位运算命令

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `BITOP AND` | 位与运算 | `BITOP AND result key1 key2` | O(N) |
| `BITOP OR` | 位或运算 | `BITOP OR result key1 key2` | O(N) |
| `BITOP XOR` | 位异或运算 | `BITOP XOR result key1 key2` | O(N) |
| `BITOP NOT` | 位非运算 | `BITOP NOT result key1` | O(N) |

### 实用场景示例

```bash
# 用户签到统计
SETBIT checkin:daily:2025-01-21 1001 1     # 用户1001签到
GETBIT checkin:daily:2025-01-21 1001       # 检查用户1001是否签到
BITCOUNT checkin:daily:2025-01-21          # 今日总签到人数

# 活跃用户分析
BITOP AND continuous:2days active:today active:yesterday  # 连续活跃用户
BITOP OR active:7days active:day1 active:day2 ... active:day7  # 7日活跃用户
BITCOUNT continuous:2days                   # 连续活跃用户数

# 权限管理
SETBIT perm:feature_a 1001 1               # 给用户1001开通功能A权限
GETBIT perm:feature_a 1001                 # 检查用户1001是否有权限
BITOP AND vip:active perm:vip active:today # VIP活跃用户
```

---

## HyperLogLog 基数统计

### 基础命令

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `PFADD` | 添加元素 | `PFADD uv:today user1 user2` | O(1) |
| `PFCOUNT` | 统计基数 | `PFCOUNT uv:today` | O(1) |
| `PFMERGE` | 合并HLL | `PFMERGE uv:week uv:day1 uv:day2` | O(N) |

### 实用场景示例

```bash
# UV统计
PFADD uv:daily:2025-01-21 user_12345       # 记录用户访问
PFCOUNT uv:daily:2025-01-21                # 当日UV统计
PFMERGE uv:weekly:2025-w04 uv:daily:2025-01-20 uv:daily:2025-01-21  # 周UV合并

# 页面UV统计
PFADD uv:page:/product/123 user_456        # 页面访问记录
PFCOUNT uv:page:/product/123               # 页面独立访客数

# API调用统计
PFADD uv:api:/user/profile user_789        # API调用记录
PFCOUNT uv:api:/user/profile               # API独立调用用户数

# 渠道用户统计
PFADD user:channel:wechat user_111         # 微信渠道用户
PFADD user:channel:app user_222            # APP渠道用户
PFMERGE user:total user:channel:wechat user:channel:app  # 全渠道用户

# 多维度分析
PFADD uv:mobile:2025-01-21 user_333        # 移动端UV
PFADD uv:web:2025-01-21 user_444           # Web端UV
PFMERGE uv:all:2025-01-21 uv:mobile:2025-01-21 uv:web:2025-01-21  # 全端UV
```

---

## Geo 地理位置操作

### 基础命令

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `GEOADD` | 添加位置 | `GEOADD drivers 116.4 39.9 driver1` | O(log(N)) |
| `GEOPOS` | 获取坐标 | `GEOPOS drivers driver1` | O(log(N)) |
| `GEODIST` | 计算距离 | `GEODIST drivers driver1 driver2 km` | O(log(N)) |
| `GEOHASH` | 获取GeoHash | `GEOHASH drivers driver1` | O(log(N)) |

### 范围查询命令

| 命令 | 说明 | 示例 | 时间复杂度 |
|------|------|------|------------|
| `GEORADIUS` | 坐标范围查询 | `GEORADIUS drivers 116.4 39.9 5 km` | O(N+log(M)) |
| `GEORADIUSBYMEMBER` | 成员范围查询 | `GEORADIUSBYMEMBER drivers driver1 2 km` | O(N+log(M)) |

### 查询选项参数

| 参数 | 说明 | 示例 |
|------|------|------|
| `WITHDIST` | 返回距离 | `GEORADIUS ... WITHDIST` |
| `WITHCOORD` | 返回坐标 | `GEORADIUS ... WITHCOORD` |
| `WITHHASH` | 返回GeoHash | `GEORADIUS ... WITHHASH` |
| `COUNT` | 限制结果数 | `GEORADIUS ... COUNT 10` |
| `ASC/DESC` | 排序方式 | `GEORADIUS ... ASC` |
| `STORE` | 存储结果 | `GEORADIUS ... STORE result` |

### 实用场景示例

```bash
# 司机定位服务
GEOADD drivers:online 116.397428 39.900000 driver_001  # 司机上线
GEORADIUS drivers:online 116.4 39.9 5 km WITHDIST COUNT 10 ASC  # 找5km内最近10个司机
GEODIST drivers:online driver_001 driver_002 km        # 计算两司机距离

# 外卖配送
GEOADD stores:restaurants 116.395 39.895 store_001    # 添加餐厅位置
GEORADIUS stores:restaurants 116.4 39.9 3 km WITHDIST COUNT 20  # 找3km内餐厅
GEORADIUSBYMEMBER couriers:working store_001 10 km COUNT 1 ASC  # 找最近配送员

# 社交服务 - 附近的人
GEOADD users:location 116.398 39.898 user_001         # 用户位置更新
GEORADIUSBYMEMBER users:location user_001 1 km WITHDIST COUNT 50  # 1km内其他用户

# 兴趣点(POI)查询
GEOADD poi:hospitals 116.385 39.915 hospital_001      # 添加医院位置
GEORADIUS poi:hospitals 116.4 39.9 5 km WITHDIST     # 查找5km内医院

# 实时位置更新
GEOADD drivers:online 116.405 39.905 driver_001       # 更新司机位置（覆盖旧位置）
GEOPOS drivers:online driver_001                      # 获取司机当前位置

# 地理围栏
GEORADIUS geo:fence 116.4 39.9 0.5 km                # 检查0.5km围栏内对象
```

---

## 性能优化与最佳实践

### Bitmap 优化要点

```bash
# ✅ 推荐做法
SETBIT active:daily:2025-01-21 1001 1     # 按日期分片，用户ID连续
BITCOUNT active:daily:2025-01-21 0 1000   # 分段统计大bitmap
EXPIRE active:daily:2025-01-21 2592000    # 30天过期

# ❌ 避免的做法
SETBIT active:all 999999999 1             # 稀疏ID导致内存浪费
BITCOUNT active:large                     # 大bitmap全量统计阻塞
```

### HyperLogLog 优化要点

```bash
# ✅ 推荐做法
PFADD uv:daily:2025-01-21 user_12345      # 按时间维度分片
PFMERGE uv:weekly uv:day1 uv:day2 uv:day3  # 定期合并避免临时HLL

# ❌ 避免的做法
PFADD uv:temp user_123                    # 频繁创建临时HLL
PFCOUNT uv:huge1 uv:huge2 uv:huge3        # 同时统计多个大HLL
```

### Geo 优化要点

```bash
# ✅ 推荐做法
GEOADD drivers:beijing 116.4 39.9 driver1   # 按城市分片
GEORADIUS drivers:beijing 116.4 39.9 2 km COUNT 50  # 限制返回数量

# ❌ 避免的做法
GEORADIUS drivers:all 116.4 39.9 100 km     # 过大搜索半径
GEORADIUS drivers:all 116.4 39.9 5 km       # 未按地理区域分片
```

---

## 内存使用对比

### 100万用户数据的内存占用

| 数据结构 | 内存使用 | 适用场景 | 精度 |
|---------|---------|----------|------|
| **Bitmap** | ~125KB | 用户状态标记 | 100%精确 |
| **HyperLogLog** | 12KB | 基数统计 | ±1.04%误差 |
| **Set** | ~20MB | 小规模精确集合 | 100%精确 |
| **Geo** | ~80MB | 地理位置存储 | ~1米精度 |

### 选择决策矩阵

| 需求 | < 1万数据 | 1万-10万数据 | > 10万数据 |
|------|----------|-------------|------------|
| **用户状态标记** | Set | Bitmap | Bitmap |
| **基数统计(精确)** | Set | Set | 考虑Bitmap+计数 |
| **基数统计(近似)** | Set | HyperLogLog | HyperLogLog |
| **地理位置** | Hash | Geo | Geo(分片) |

---

## 监控与告警指标

### 关键监控项

```bash
# 内存监控
INFO memory                                # 总内存使用
MEMORY USAGE active:daily:2025-01-21       # 单key内存使用

# 性能监控
SLOWLOG GET 10                             # 慢查询（关注BITCOUNT、GEORADIUS）
INFO commandstats                          # 命令统计

# 业务监控
BITCOUNT active:daily:2025-01-21           # 日活用户数
PFCOUNT uv:daily:2025-01-21               # 日UV数
ZCARD drivers:online                       # 在线司机数（Geo底层是ZSet）
```

### 告警阈值建议

```yaml
# Bitmap告警
- alert: BitmapLargeMemory
  expr: redis_memory_usage_bytes{key=~"active:.*"} > 10MB
  
# HyperLogLog告警  
- alert: HLLHighErrorRate
  expr: abs(hll_estimated - actual_count) / actual_count > 0.05

# Geo查询告警
- alert: GeoQuerySlow
  expr: redis_command_duration_seconds{cmd="georadius"} > 0.05
```

---

## 常见陷阱与解决方案

### Bitmap 陷阱

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 内存暴涨 | 用户ID稀疏 | 使用Hash分片或改用Set |
| 统计慢 | Bitmap过大 | 按时间/业务维度分片 |
| 位偏移错误 | ID超出范围 | 添加边界检查 |

### HyperLogLog 陷阱

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 误差过大 | 数据量太小 | 小数据量改用Set |
| 合并慢 | 同时合并太多HLL | 分批合并或预先分组 |
| 无法获取明细 | HLL只能计数 | 需要明细时使用Set |

### Geo 陷阱

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 查询慢 | 搜索半径过大 | 限制半径<10km，按区域分片 |
| 精度问题 | GeoHash量化误差 | 业务容忍度内可接受 |
| 热点问题 | 某区域数据过多 | 按密度进一步分片 |

---

记住：**选择合适的数据结构比优化更重要！根据业务场景、数据量和精度要求做出明智选择。** 