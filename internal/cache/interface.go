package cache

import (
	"context"
	"time"
)

// Cache 缓存接口
type Cache interface {
	// Get 获取缓存值
	Get(ctx context.Context, key string, dest interface{}) error

	// Set 设置缓存值
	Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error

	// Del 删除缓存
	Del(ctx context.Context, keys ...string) error

	// Exists 检查key是否存在
	Exists(ctx context.Context, keys ...string) (int64, error)

	// GetWithTTL 获取缓存值和剩余TTL
	GetWithTTL(ctx context.Context, key string, dest interface{}) (time.Duration, error)

	// Expire 设置过期时间
	Expire(ctx context.Context, key string, ttl time.Duration) error

	// Close 关闭缓存
	Close() error
}

// LoaderFunc 数据加载函数类型
// 当缓存未命中时，调用此函数从数据源加载数据
type LoaderFunc func(ctx context.Context, key string) (interface{}, error)

// CacheAsideOptions Cache-Aside模式配置选项
type CacheAsideOptions struct {
	// TTL 缓存过期时间
	TTL time.Duration

	// TTLJitter TTL随机抖动比例 (0.0-1.0)
	// 例如：TTL=1小时，TTLJitter=0.1，实际TTL会在54分钟-66分钟之间随机
	TTLJitter float64

	// EmptyValueTTL 空值缓存TTL（防穿透）
	EmptyValueTTL time.Duration

	// Serializer 序列化器
	Serializer Serializer

	// EnableMetrics 是否启用指标收集
	EnableMetrics bool

	// Namespace 命名空间前缀
	Namespace string
}

// DefaultCacheAsideOptions 默认配置
func DefaultCacheAsideOptions() *CacheAsideOptions {
	return &CacheAsideOptions{
		TTL:           1 * time.Hour,
		TTLJitter:     0.1, // 10%抖动
		EmptyValueTTL: 5 * time.Minute,
		Serializer:    &JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "cache",
	}
}
