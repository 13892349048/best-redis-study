# Redis 高级数据结构快速参考卡片

## 🎯 三秒钟选择指南

| 需求 | 数据量 | 推荐结构 | 理由 |
|------|--------|----------|------|
| 用户状态标记 | < 1万 | Set | 简单直接 |
| 用户状态标记 | > 1万 | **Bitmap** | 极致空间效率 |
| 基数统计(精确) | < 10万 | Set | 100%精确 |
| 基数统计(近似) | > 10万 | **HyperLogLog** | 固定12KB内存 |
| 地理位置服务 | 任意 | **Geo** | 空间查询优化 |

---

## 📊 Bitmap - 用户状态标记神器

### 💡 核心优势
- **超低内存**: 1000万用户仅需1.25MB
- **极速查询**: O(1)检查用户状态  
- **位运算**: AND/OR/XOR分析用户群体

### 🔧 关键命令
```bash
SETBIT active:today 1001 1        # 设置用户1001活跃
GETBIT active:today 1001          # 检查用户1001状态
BITCOUNT active:today             # 统计今日活跃用户数
BITOP AND result key1 key2        # 连续活跃用户分析
```

### ⚡ 性能要点
- **适用**: 用户ID连续、状态标记场景
- **避免**: 稀疏ID(如UUID)、大bitmap全量统计
- **优化**: 按时间分片、分段统计

### 🎯 典型场景
```bash
active:daily:2025-01-21          # 日活跃用户
checkin:monthly:2025-01          # 月签到统计
perm:feature_a                   # 功能权限标记
experiment:group_b               # AB测试分组
```

---

## 🔢 HyperLogLog - 大规模基数统计

### 💡 核心优势
- **固定内存**: 永远只占12KB，无论数据量多大
- **高精度**: ±1.04%理论误差，实际< 2%
- **可合并**: 多维度统计合并

### 🔧 关键命令
```bash
PFADD uv:today user_12345         # 记录用户访问
PFCOUNT uv:today                  # 获取UV统计
PFMERGE uv:week uv:1 uv:2 uv:3    # 合并多天数据
```

### ⚡ 性能要点
- **适用**: 数据量>10万、可接受1-2%误差
- **避免**: 小数据量(<1万)、需要100%精确
- **优化**: 预先分片、定期合并

### 🎯 典型场景
```bash
uv:daily:2025-01-21              # 日UV统计
uv:page:/product/123             # 页面独立访客
user:channel:wechat              # 渠道用户去重
api:distinct:calls               # API调用去重
```

---

## 🗺️ Geo - 地理空间服务引擎

### 💡 核心优势
- **空间索引**: 基于GeoHash的高效范围查询
- **距离计算**: 内置距离计算和排序
- **实时更新**: 支持位置实时更新

### 🔧 关键命令
```bash
GEOADD drivers 116.4 39.9 driver1    # 添加司机位置
GEORADIUS drivers 116.4 39.9 5 km    # 查找5km内司机
GEODIST drivers driver1 driver2 km   # 计算两点距离
GEOPOS drivers driver1               # 获取坐标
```

### ⚡ 性能要点
- **适用**: LBS服务、附近查询、地理围栏
- **避免**: 过大搜索半径(>10km)、无COUNT限制
- **优化**: 按城市分片、限制返回数量

### 🎯 典型场景
```bash
drivers:online                   # 在线司机定位
stores:restaurants               # 餐厅位置服务
users:nearby                     # 附近的人
poi:hospitals                    # 医院POI查询
```

---

## ⚡ 性能对比一览表

### 内存效率 (100万数据)
| 结构 | 内存使用 | 节省率 | 适用数据量 |
|------|---------|-------|------------|
| **Bitmap** | 125KB | 99%+ | 1万-1亿 |
| **HyperLogLog** | 12KB | 99.9%+ | 10万-100亿 |
| **Geo** | 80MB | 基准 | 1万-1000万 |
| Set | 20MB | 基准 | 1万以下 |

### 查询性能 (单次操作)
| 操作 | Bitmap | HyperLogLog | Geo | 复杂度 |
|------|--------|-------------|-----|--------|
| **单点查询** | 0.01ms | 0.01ms | 0.1ms | O(1) |
| **统计计数** | 1-10ms | 0.01ms | - | O(N) vs O(1) |
| **范围查询** | - | - | 1-50ms | O(N+logM) |

---

## 🚨 常见陷阱速查

### ❌ 绝对避免
```bash
BITCOUNT huge_bitmap              # 大bitmap全量统计
GEORADIUS all 116.4 39.9 100 km   # 超大范围查询
SETBIT sparse 999999999 1         # 稀疏ID存储
PFMERGE result uv:1 ... uv:100    # 大量HLL同时合并
```

### ⚠️ 谨慎使用
```bash
BITOP AND large1 large2           # 大bitmap位运算
PFADD small_uv user1              # 小数据量用HLL
GEORADIUS dense_area 5 km         # 热点区域无限制
```

### ✅ 推荐做法
```bash
# 分片存储
active:daily:2025-01-21
geo:drivers:beijing
uv:channel:mobile

# 限制查询
GEORADIUS drivers 116.4 39.9 5 km COUNT 50
BITCOUNT large_bitmap 0 100000

# 合理TTL
EXPIRE active:today 2592000       # 30天
EXPIRE drivers:online 14400       # 4小时
```

---

## 🎯 选择决策树

```
数据需求
├── 用户状态标记
│   ├── ID连续 → Bitmap (极致内存)
│   └── ID稀疏 → Set (简单直接)
├── 基数统计
│   ├── 需要100%精确 → Set
│   ├── 可接受误差 & 大数据量 → HyperLogLog
│   └── 小数据量 → Set
└── 地理位置
    ├── 需要范围查询 → Geo
    ├── 只存储坐标 → Hash
    └── 高精度要求 → 自建索引
```

---

## 🔍 快速诊断命令

```bash
# 性能监控
SLOWLOG GET 10                    # 慢查询
INFO commandstats                 # 命令统计
MEMORY USAGE key                  # 内存使用

# 数据验证  
BITCOUNT bitmap_key               # Bitmap密度
PFCOUNT hll_key                   # HLL估算
ZCARD geo_key                     # Geo数据量

# 容量分析
redis-cli --bigkeys               # 大键识别
INFO memory                       # 内存概览
```

---

## 📱 移动端速查

### Bitmap 一行决策
**连续ID + 状态标记 = Bitmap**

### HyperLogLog 一行决策  
**大数据量 + 基数统计 + 可接受误差 = HLL**

### Geo 一行决策
**坐标数据 + 范围查询 = Geo**

---

**记住**: 选对数据结构 > 性能优化，先选择再优化！ 