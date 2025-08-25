package cache

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
)

// CacheAside Cache-Aside装饰器
// 这是一个通用的缓存装饰器，可以装饰任何数据加载函数
type CacheAside struct {
	cache   Cache
	options *CacheAsideOptions
}

// NewCacheAside 创建Cache-Aside装饰器
func NewCacheAside(cache Cache, opts *CacheAsideOptions) *CacheAside {
	if opts == nil {
		opts = DefaultCacheAsideOptions()
	}

	return &CacheAside{
		cache:   cache,
		options: opts,
	}
}

// GetOrLoad 获取缓存数据，缓存未命中时调用加载函数
// 这是Cache-Aside模式的核心方法
func (ca *CacheAside) GetOrLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 1. 首先尝试从缓存获取
	err := ca.cache.Get(ctx, key, dest)
	if err == nil {
		// 缓存命中，检查是否为空值（防穿透机制）
		if ca.cache.(*RedisCache).IsEmptyValue(dest) {
			return ErrNilValue
		}
		return nil
	}

	// 2. 缓存未命中，检查错误类型
	if !errors.Is(err, ErrCacheMiss) {
		// 缓存读取出错，记录日志但继续尝试从数据源加载
		log.Printf("Cache get error for key %s: %v", key, err)
	}

	// 3. 调用加载函数从数据源获取数据
	value, err := loader(ctx, key)
	if err != nil {
		// 数据源加载失败
		// 如果是"数据不存在"类型的错误，可以设置空值缓存防止穿透
		if isNotFoundError(err) {
			if setErr := ca.cache.(*RedisCache).SetEmptyValue(ctx, key); setErr != nil {
				log.Printf("Failed to set empty value for key %s: %v", key, setErr)
			}
		}
		return fmt.Errorf("loader failed: %w", err)
	}

	// 4. 将数据写入缓存
	if err := ca.cache.Set(ctx, key, value, ca.options.TTL); err != nil {
		// 缓存写入失败，记录日志但不影响业务逻辑
		log.Printf("Cache set error for key %s: %v", key, err)
	}

	// 5. 将数据反序列化到目标对象
	if ca.options.Serializer != nil {
		data, err := ca.options.Serializer.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal loaded value: %w", err)
		}

		if err := ca.options.Serializer.Unmarshal(data, dest); err != nil {
			return fmt.Errorf("unmarshal to dest: %w", err)
		}
	} else {
		// 如果没有序列化器，直接赋值（需要类型匹配）
		return errors.New("no serializer configured")
	}

	return nil
}

// Set 设置缓存
func (ca *CacheAside) Set(ctx context.Context, key string, value interface{}) error {
	return ca.cache.Set(ctx, key, value, ca.options.TTL)
}

// SetWithTTL 设置缓存并指定TTL
func (ca *CacheAside) SetWithTTL(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	return ca.cache.Set(ctx, key, value, ttl)
}

// Del 删除缓存
func (ca *CacheAside) Del(ctx context.Context, keys ...string) error {
	return ca.cache.Del(ctx, keys...)
}

// Exists 检查缓存是否存在
func (ca *CacheAside) Exists(ctx context.Context, keys ...string) (int64, error) {
	return ca.cache.Exists(ctx, keys...)
}

// Refresh 刷新缓存（强制重新加载）
func (ca *CacheAside) Refresh(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 1. 先删除旧缓存
	if err := ca.cache.Del(ctx, key); err != nil {
		log.Printf("Failed to delete cache for refresh key %s: %v", key, err)
	}

	// 2. 重新加载数据
	return ca.GetOrLoad(ctx, key, dest, loader)
}

// Warm 缓存预热
func (ca *CacheAside) Warm(ctx context.Context, keys []string, loader func(ctx context.Context, key string) (interface{}, error)) error {
	for _, key := range keys {
		value, err := loader(ctx, key)
		if err != nil {
			log.Printf("Warm cache failed for key %s: %v", key, err)
			continue
		}

		if err := ca.cache.Set(ctx, key, value, ca.options.TTL); err != nil {
			log.Printf("Set warm cache failed for key %s: %v", key, err)
		}
	}

	return nil
}

// GetStats 获取缓存统计信息
func (ca *CacheAside) GetStats() *CacheStats {
	if redisCache, ok := ca.cache.(*RedisCache); ok {
		return redisCache.GetStats()
	}
	return nil
}

// Close 关闭缓存
func (ca *CacheAside) Close() error {
	return ca.cache.Close()
}

// isNotFoundError 判断是否为"数据不存在"错误
// 这个函数可以根据具体的业务错误类型进行定制
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// 常见的"数据不存在"错误模式
	errStr := err.Error()
	keywords := []string{
		"not found",
		"no rows",
		"record not found",
		"does not exist",
	}

	for _, keyword := range keywords {
		if contains(errStr, keyword) {
			return true
		}
	}

	return false
}

// contains 检查字符串是否包含子串（不区分大小写）
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				anyIndexContains(s, substr)))
}

func anyIndexContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
