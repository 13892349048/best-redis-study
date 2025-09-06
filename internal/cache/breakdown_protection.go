package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// BreakdownProtection 缓存击穿防护
type BreakdownProtection struct {
	cache    Cache
	options  *BreakdownOptions
	
	// 单飞组
	singleflightGroup *SingleflightGroup
	
	// 逻辑过期管理
	logicalExpireManager *LogicalExpireManager
}

// BreakdownOptions 击穿防护配置
type BreakdownOptions struct {
	// 启用单飞防护
	EnableSingleflight bool
	
	// 单飞超时时间
	SingleflightTimeout time.Duration
	
	// 启用逻辑过期
	EnableLogicalExpire bool
	
	// 逻辑过期配置
	LogicalExpireConfig *LogicalExpireConfig
	
	// 启用指标收集
	EnableMetrics bool
	
	// 热点Key检测阈值（每秒请求数）
	HotKeyThreshold int64
	
	// 热点Key检测窗口
	HotKeyWindow time.Duration
}

// LogicalExpireConfig 逻辑过期配置
type LogicalExpireConfig struct {
	// 逻辑过期时间（相对于物理TTL）
	LogicalTTL time.Duration
	
	// 物理过期时间（实际Redis TTL，应该比逻辑过期时间长）
	PhysicalTTL time.Duration
	
	// 后台刷新时提前量（在逻辑过期前多久开始后台刷新）
	RefreshAdvance time.Duration
	
	// 并发刷新控制
	MaxConcurrentRefresh int
}

// DefaultBreakdownOptions 默认击穿防护配置
func DefaultBreakdownOptions() *BreakdownOptions {
	return &BreakdownOptions{
		EnableSingleflight:  true,
		SingleflightTimeout: 5 * time.Second,
		EnableLogicalExpire: false, // 默认关闭，可配置开启
		LogicalExpireConfig: &LogicalExpireConfig{
			LogicalTTL:           30 * time.Minute,
			PhysicalTTL:          60 * time.Minute, // 物理TTL是逻辑TTL的2倍
			RefreshAdvance:       5 * time.Minute,  // 提前5分钟刷新
			MaxConcurrentRefresh: 10,
		},
		EnableMetrics:   true,
		HotKeyThreshold: 100, // 每秒100次请求认为是热点
		HotKeyWindow:    1 * time.Minute,
	}
}

// BreakdownStats 击穿防护统计
type BreakdownStats struct {
	// 单飞统计
	SingleflightHits     int64 `json:"singleflight_hits"`
	SingleflightMisses   int64 `json:"singleflight_misses"`
	SingleflightTimeouts int64 `json:"singleflight_timeouts"`
	SingleflightErrors   int64 `json:"singleflight_errors"`
	
	// 逻辑过期统计
	LogicalExpireHits     int64 `json:"logical_expire_hits"`
	LogicalExpireRefresh  int64 `json:"logical_expire_refresh"`
	LogicalExpireExpired  int64 `json:"logical_expire_expired"`
	
	// 热点Key统计
	HotKeysDetected int64 `json:"hot_keys_detected"`
	HotKeysProtected int64 `json:"hot_keys_protected"`
}

// NewBreakdownProtection 创建击穿防护
func NewBreakdownProtection(cache Cache, opts *BreakdownOptions) *BreakdownProtection {
	if opts == nil {
		opts = DefaultBreakdownOptions()
	}

	bp := &BreakdownProtection{
		cache:   cache,
		options: opts,
	}

	// 初始化单飞组
	if opts.EnableSingleflight {
		bp.singleflightGroup = NewSingleflightGroup()
	}

	// 初始化逻辑过期管理器
	if opts.EnableLogicalExpire {
		bp.logicalExpireManager = NewLogicalExpireManager(cache, opts.LogicalExpireConfig)
	}

	return bp
}

// GetOrLoad 获取缓存数据，带击穿防护
func (bp *BreakdownProtection) GetOrLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 选择防护策略
	if bp.options.EnableLogicalExpire {
		return bp.getOrLoadWithLogicalExpire(ctx, key, dest, loader)
	} else if bp.options.EnableSingleflight {
		return bp.getOrLoadWithSingleflight(ctx, key, dest, loader)
	}
	
	// 没有启用任何防护，直接调用原始方法
	return bp.getOrLoadDirect(ctx, key, dest, loader)
}

// getOrLoadWithSingleflight 使用单飞策略
func (bp *BreakdownProtection) getOrLoadWithSingleflight(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 包装loader函数
	wrappedLoader := func() (interface{}, error) {
		// 首先再次尝试从缓存获取（可能其他协程已经加载）
		err := bp.cache.Get(ctx, key, dest)
		if err == nil {
			return dest, nil
		}
		
		if err != ErrCacheMiss {
			log.Printf("Cache get error in singleflight for key %s: %v", key, err)
		}
		
		// 调用原始loader
		value, err := loader(ctx, key)
		if err != nil {
			return nil, err
		}
		
		// 写入缓存
		if err := bp.cache.Set(ctx, key, value, 0); err != nil {
			log.Printf("Cache set error in singleflight for key %s: %v", key, err)
		}
		
		return value, nil
	}

	// 使用单飞组执行
	result, err := bp.singleflightGroup.Do(ctx, key, wrappedLoader)
	if err != nil {
		return err
	}
	
	// 如果result就是dest，说明缓存命中
	if result == dest {
		return nil
	}
	
	// 否则需要将result赋值给dest
	return bp.assignResult(result, dest)
}

// getOrLoadWithLogicalExpire 使用逻辑过期策略
func (bp *BreakdownProtection) getOrLoadWithLogicalExpire(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	return bp.logicalExpireManager.GetOrLoad(ctx, key, dest, loader)
}

// getOrLoadDirect 直接加载（无防护）
func (bp *BreakdownProtection) getOrLoadDirect(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 尝试从缓存获取
	err := bp.cache.Get(ctx, key, dest)
	if err == nil {
		return nil
	}
	
	if err != ErrCacheMiss {
		log.Printf("Cache get error for key %s: %v", key, err)
	}
	
	// 从数据源加载
	value, err := loader(ctx, key)
	if err != nil {
		return fmt.Errorf("loader failed: %w", err)
	}
	
	// 写入缓存
	if err := bp.cache.Set(ctx, key, value, 0); err != nil {
		log.Printf("Cache set error for key %s: %v", key, err)
	}
	
	// 赋值给dest
	return bp.assignResult(value, dest)
}

// assignResult 将结果赋值给目标对象
func (bp *BreakdownProtection) assignResult(result interface{}, dest interface{}) error {
	// 这里可以实现更复杂的类型转换逻辑
	// 暂时使用JSON序列化/反序列化
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	
	return json.Unmarshal(data, dest)
}

// GetStats 获取击穿防护统计
func (bp *BreakdownProtection) GetStats() *BreakdownStats {
	stats := &BreakdownStats{}
	
	// 获取单飞统计
	if bp.singleflightGroup != nil {
		sfStats := bp.singleflightGroup.GetStats()
		stats.SingleflightHits = sfStats.Hits
		stats.SingleflightMisses = sfStats.Misses
		stats.SingleflightTimeouts = sfStats.Timeouts
		stats.SingleflightErrors = sfStats.Errors
	}
	
	// 获取逻辑过期统计
	if bp.logicalExpireManager != nil {
		leStats := bp.logicalExpireManager.GetStats()
		stats.LogicalExpireHits = leStats.Hits
		stats.LogicalExpireRefresh = leStats.RefreshCount
		stats.LogicalExpireExpired = leStats.ExpiredCount
	}
	
	return stats
}

// Close 关闭击穿防护
func (bp *BreakdownProtection) Close() error {
	var errs []error
	
	if bp.singleflightGroup != nil {
		if err := bp.singleflightGroup.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	
	if bp.logicalExpireManager != nil {
		if err := bp.logicalExpireManager.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	
	if len(errs) > 0 {
		return fmt.Errorf("close breakdown protection failed: %v", errs)
	}
	
	return nil
}

// IsHotKey 检查是否为热点Key
func (bp *BreakdownProtection) IsHotKey(key string) bool {
	// TODO: 实现热点Key检测逻辑
	// 可以基于请求频率、时间窗口等进行检测
	return false
}

// EnableProtectionForKey 为特定Key启用防护
func (bp *BreakdownProtection) EnableProtectionForKey(key string, strategy string) error {
	// TODO: 实现动态防护策略配置
	return nil
}