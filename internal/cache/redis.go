package cache

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// ErrCacheMiss 缓存未命中错误
	ErrCacheMiss = errors.New("cache miss")

	// ErrNilValue 空值错误
	ErrNilValue = errors.New("nil value")
)

// RedisCache Redis缓存实现
type RedisCache struct {
	client    redis.Cmdable
	options   *CacheAsideOptions
	metrics   *CacheMetrics
	namespace string
}

// NewRedisCache 创建Redis缓存
func NewRedisCache(client redis.Cmdable, opts *CacheAsideOptions) *RedisCache {
	if opts == nil {
		opts = DefaultCacheAsideOptions()
	}

	cache := &RedisCache{
		client:    client,
		options:   opts,
		namespace: opts.Namespace,
	}

	if opts.EnableMetrics {
		cache.metrics = NewCacheMetrics()
	}

	return cache
}

// buildKey 构建带命名空间的key
func (c *RedisCache) buildKey(key string) string {
	if c.namespace == "" {
		return key
	}
	return fmt.Sprintf("%s:%s", c.namespace, key)
}

// calculateTTL 计算带抖动的TTL
func (c *RedisCache) calculateTTL(baseTTL time.Duration) time.Duration {
	if c.options.TTLJitter <= 0 {
		return baseTTL
	}

	// 计算抖动范围
	jitter := c.options.TTLJitter
	minMultiplier := 1.0 - jitter
	maxMultiplier := 1.0 + jitter

	// 生成随机乘数
	multiplier := minMultiplier + rand.Float64()*(maxMultiplier-minMultiplier)

	return time.Duration(float64(baseTTL) * multiplier)
}

// Get 获取缓存值
func (c *RedisCache) Get(ctx context.Context, key string, dest interface{}) error {
	start := time.Now()
	defer func() {
		if c.metrics != nil {
			c.metrics.RecordGetLatency(time.Since(start))
		}
	}()

	redisKey := c.buildKey(key)

	// 从Redis获取数据
	result := c.client.Get(ctx, redisKey)
	if err := result.Err(); err != nil {
		if err == redis.Nil {
			if c.metrics != nil {
				c.metrics.IncMisses()
			}
			return ErrCacheMiss
		}

		if c.metrics != nil {
			c.metrics.IncGetErrors()
		}
		return fmt.Errorf("redis get error: %w", err)
	}

	data, err := result.Bytes()
	if err != nil {
		if c.metrics != nil {
			c.metrics.IncGetErrors()
		}
		return fmt.Errorf("get bytes error: %w", err)
	}

	// 反序列化
	if err := c.options.Serializer.Unmarshal(data, dest); err != nil {
		if c.metrics != nil {
			c.metrics.IncDeserializeErrors()
		}
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if c.metrics != nil {
		c.metrics.IncHits()
	}

	return nil
}

// Set 设置缓存值
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	start := time.Now()
	defer func() {
		if c.metrics != nil {
			c.metrics.RecordSetLatency(time.Since(start))
		}
	}()

	// 序列化数据
	data, err := c.options.Serializer.Marshal(value)
	if err != nil {
		if c.metrics != nil {
			c.metrics.IncSerializeErrors()
		}
		return fmt.Errorf("marshal error: %w", err)
	}

	redisKey := c.buildKey(key)

	// 计算实际TTL（带抖动）
	actualTTL := ttl
	if ttl > 0 {
		actualTTL = c.calculateTTL(ttl)
	}

	// 设置到Redis
	if err := c.client.Set(ctx, redisKey, data, actualTTL).Err(); err != nil {
		if c.metrics != nil {
			c.metrics.IncSetErrors()
		}
		return fmt.Errorf("redis set error: %w", err)
	}

	if c.metrics != nil {
		c.metrics.IncSets()
	}

	return nil
}

// Del 删除缓存
func (c *RedisCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	// 构建Redis key列表
	redisKeys := make([]string, len(keys))
	for i, key := range keys {
		redisKeys[i] = c.buildKey(key)
	}

	if err := c.client.Del(ctx, redisKeys...).Err(); err != nil {
		if c.metrics != nil {
			c.metrics.IncDelErrors()
		}
		return fmt.Errorf("redis del error: %w", err)
	}

	if c.metrics != nil {
		c.metrics.IncDels()
	}

	return nil
}

// Exists 检查key是否存在
func (c *RedisCache) Exists(ctx context.Context, keys ...string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}

	// 构建Redis key列表
	redisKeys := make([]string, len(keys))
	for i, key := range keys {
		redisKeys[i] = c.buildKey(key)
	}

	count, err := c.client.Exists(ctx, redisKeys...).Result()
	if err != nil {
		return 0, fmt.Errorf("redis exists error: %w", err)
	}

	return count, nil
}

// GetWithTTL 获取缓存值和剩余TTL
func (c *RedisCache) GetWithTTL(ctx context.Context, key string, dest interface{}) (time.Duration, error) {
	redisKey := c.buildKey(key)

	// 使用Pipeline同时获取值和TTL
	pipe := c.client.Pipeline()
	getCmd := pipe.Get(ctx, redisKey)
	ttlCmd := pipe.TTL(ctx, redisKey)

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return 0, fmt.Errorf("pipeline exec error: %w", err)
	}

	// 检查值是否存在
	if err := getCmd.Err(); err != nil {
		if err == redis.Nil {
			if c.metrics != nil {
				c.metrics.IncMisses()
			}
			return 0, ErrCacheMiss
		}
		return 0, fmt.Errorf("get value error: %w", err)
	}

	// 获取数据
	data, err := getCmd.Bytes()
	if err != nil {
		return 0, fmt.Errorf("get bytes error: %w", err)
	}

	// 反序列化
	if err := c.options.Serializer.Unmarshal(data, dest); err != nil {
		if c.metrics != nil {
			c.metrics.IncDeserializeErrors()
		}
		return 0, fmt.Errorf("unmarshal error: %w", err)
	}

	// 获取TTL
	ttl, err := ttlCmd.Result()
	if err != nil {
		return 0, fmt.Errorf("get ttl error: %w", err)
	}

	if c.metrics != nil {
		c.metrics.IncHits()
	}

	return ttl, nil
}

// Expire 设置过期时间
func (c *RedisCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	redisKey := c.buildKey(key)

	if err := c.client.Expire(ctx, redisKey, ttl).Err(); err != nil {
		return fmt.Errorf("redis expire error: %w", err)
	}

	return nil
}

// Close 关闭缓存
func (c *RedisCache) Close() error {
	// Redis缓存不需要特殊关闭操作
	return nil
}

// GetMetrics 获取指标
func (c *RedisCache) GetMetrics() *CacheMetrics {
	return c.metrics
}

// GetStats 获取统计信息
func (c *RedisCache) GetStats() *CacheStats {
	if c.metrics == nil {
		return nil
	}
	return c.metrics.GetStats()
}

// SetEmptyValue 设置空值（用于防穿透）
func (c *RedisCache) SetEmptyValue(ctx context.Context, key string) error {
	return c.Set(ctx, key, "", c.options.EmptyValueTTL)
}

// IsEmptyValue 检查是否为空值
func (c *RedisCache) IsEmptyValue(value interface{}) bool {
	if value == nil {
		return true
	}

	switch v := value.(type) {
	case string:
		return v == ""
	case []byte:
		return len(v) == 0
	default:
		return false
	}
}
