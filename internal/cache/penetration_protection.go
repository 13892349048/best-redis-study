package cache

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"what_redis_can_do/internal/bloom"
)

// PenetrationProtection 缓存穿透防护
type PenetrationProtection struct {
	cache           Cache
	bloomFilter     bloom.BloomFilter
	options         *PenetrationOptions
	
	// 空值缓存统计
	emptyValueStats sync.Map
	
	// 互斥锁（防止并发穿透）
	mutexMap sync.Map
}

// PenetrationOptions 穿透防护配置
type PenetrationOptions struct {
	// 启用布隆过滤器
	EnableBloomFilter bool
	
	// 布隆过滤器配置
	BloomConfig *bloom.BloomConfig
	
	// 启用空值缓存
	EnableEmptyValueCache bool
	
	// 空值缓存TTL
	EmptyValueTTL time.Duration
	
	// 空值缓存最大数量（防止缓存污染）
	EmptyValueMaxCount int64
	
	// 启用参数校验
	EnableParameterValidation bool
	
	// 参数校验函数
	ParameterValidator func(key string) error
	
	// 启用互斥锁防护
	EnableMutexProtection bool
	
	// 互斥锁超时时间
	MutexTimeout time.Duration
	
	// 启用指标收集
	EnableMetrics bool
}

// DefaultPenetrationOptions 默认穿透防护配置
func DefaultPenetrationOptions() *PenetrationOptions {
	return &PenetrationOptions{
		EnableBloomFilter:         true,
		BloomConfig:              bloom.DefaultBloomConfig("cache_protection"),
		EnableEmptyValueCache:    true,
		EmptyValueTTL:            5 * time.Minute,
		EmptyValueMaxCount:       10000,
		EnableParameterValidation: true,
		ParameterValidator:       defaultParameterValidator,
		EnableMutexProtection:    true,
		MutexTimeout:             1 * time.Second,
		EnableMetrics:            true,
	}
}

// PenetrationStats 穿透防护统计
type PenetrationStats struct {
	// 布隆过滤器统计
	BloomFilterHits   int64 `json:"bloom_filter_hits"`
	BloomFilterMisses int64 `json:"bloom_filter_misses"`
	BloomFilterErrors int64 `json:"bloom_filter_errors"`
	
	// 空值缓存统计
	EmptyValueHits   int64 `json:"empty_value_hits"`
	EmptyValueSets   int64 `json:"empty_value_sets"`
	EmptyValueErrors int64 `json:"empty_value_errors"`
	
	// 参数校验统计
	ParameterValidationPassed int64 `json:"parameter_validation_passed"`
	ParameterValidationFailed int64 `json:"parameter_validation_failed"`
	
	// 互斥锁统计
	MutexLockAcquired int64 `json:"mutex_lock_acquired"`
	MutexLockTimeout  int64 `json:"mutex_lock_timeout"`
	
	// 穿透统计
	PenetrationPrevented int64 `json:"penetration_prevented"`
	PenetrationAllowed   int64 `json:"penetration_allowed"`
}

// NewPenetrationProtection 创建穿透防护
func NewPenetrationProtection(cache Cache, client redis.Cmdable, opts *PenetrationOptions) (*PenetrationProtection, error) {
	if opts == nil {
		opts = DefaultPenetrationOptions()
	}

	pp := &PenetrationProtection{
		cache:   cache,
		options: opts,
	}

	// 创建布隆过滤器
	if opts.EnableBloomFilter {
		bf, err := bloom.NewStandardBloomFilter(client, opts.BloomConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create bloom filter: %w", err)
		}
		pp.bloomFilter = bf
	}

	return pp, nil
}

// GetOrLoad 获取缓存数据，带穿透防护
func (pp *PenetrationProtection) GetOrLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 1. 参数校验
	if pp.options.EnableParameterValidation {
		if err := pp.options.ParameterValidator(key); err != nil {
			if pp.options.EnableMetrics {
				// 增加参数校验失败计数
			}
			return fmt.Errorf("parameter validation failed: %w", err)
		}
		if pp.options.EnableMetrics {
			// 增加参数校验成功计数
		}
	}

	// 2. 布隆过滤器检查
	if pp.options.EnableBloomFilter && pp.bloomFilter != nil {
		exists, err := pp.bloomFilter.Test(ctx, key)
		if err != nil {
			log.Printf("Bloom filter test error for key %s: %v", key, err)
			if pp.options.EnableMetrics {
				// 增加布隆过滤器错误计数
			}
		} else if !exists {
			// 布隆过滤器显示不存在，直接返回
			if pp.options.EnableMetrics {
				// 增加布隆过滤器命中计数（穿透被阻止）
			}
			return ErrCacheMiss
		} else {
			if pp.options.EnableMetrics {
				// 增加布隆过滤器未命中计数（可能存在）
			}
		}
	}

	// 3. 尝试从缓存获取
	err := pp.cache.Get(ctx, key, dest)
	if err == nil {
		// 缓存命中，检查是否为空值
		if pp.isEmptyValue(dest) {
			if pp.options.EnableMetrics {
				// 增加空值缓存命中计数
			}
			return ErrNilValue
		}
		return nil
	}

	// 4. 缓存未命中，检查错误类型
	if err != ErrCacheMiss {
		log.Printf("Cache get error for key %s: %v", key, err)
	}

	// 5. 互斥锁防护（防止并发穿透）
	if pp.options.EnableMutexProtection {
		return pp.getOrLoadWithMutex(ctx, key, dest, loader)
	}

	// 6. 直接加载数据
	return pp.loadAndCache(ctx, key, dest, loader)
}

// getOrLoadWithMutex 使用互斥锁防护的加载
func (pp *PenetrationProtection) getOrLoadWithMutex(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 获取或创建互斥锁
	mutexInterface, _ := pp.mutexMap.LoadOrStore(key, &sync.Mutex{})
	mutex := mutexInterface.(*sync.Mutex)

	// 带超时的锁获取
	lockChan := make(chan struct{})
	go func() {
		mutex.Lock()
		close(lockChan)
	}()

	select {
	case <-lockChan:
		defer mutex.Unlock()
		if pp.options.EnableMetrics {
			// 增加互斥锁获取成功计数
		}
		
		// 再次尝试从缓存获取（可能其他协程已经加载了）
		err := pp.cache.Get(ctx, key, dest)
		if err == nil {
			if pp.isEmptyValue(dest) {
				return ErrNilValue
			}
			return nil
		}
		
		// 加载数据
		return pp.loadAndCache(ctx, key, dest, loader)
		
	case <-time.After(pp.options.MutexTimeout):
		if pp.options.EnableMetrics {
			// 增加互斥锁超时计数
		}
		// 超时，直接加载数据（可能造成穿透，但避免死锁）
		return pp.loadAndCache(ctx, key, dest, loader)
		
	case <-ctx.Done():
		return ctx.Err()
	}
}

// loadAndCache 加载数据并缓存
func (pp *PenetrationProtection) loadAndCache(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 调用加载函数
	value, err := loader(ctx, key)
	if err != nil {
		// 数据源加载失败
		if isNotFoundError(err) {
			// 数据不存在，设置空值缓存
			if pp.options.EnableEmptyValueCache {
				if setErr := pp.setEmptyValue(ctx, key); setErr != nil {
					log.Printf("Failed to set empty value for key %s: %v", key, setErr)
				} else {
					if pp.options.EnableMetrics {
						// 增加空值缓存设置计数
					}
				}
			}
			
			if pp.options.EnableMetrics {
				// 增加穿透被阻止计数
			}
		}
		return fmt.Errorf("loader failed: %w", err)
	}

	// 数据加载成功
	
	// 添加到布隆过滤器
	if pp.options.EnableBloomFilter && pp.bloomFilter != nil {
		if err := pp.bloomFilter.Add(ctx, key); err != nil {
			log.Printf("Failed to add key %s to bloom filter: %v", key, err)
		}
	}

	// 写入缓存
	if err := pp.cache.Set(ctx, key, value, 0); err != nil {
		log.Printf("Cache set error for key %s: %v", key, err)
	}

	// 反序列化到目标对象
	if serializer, ok := pp.cache.(*RedisCache); ok {
		data, err := serializer.options.Serializer.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal loaded value: %w", err)
		}

		if err := serializer.options.Serializer.Unmarshal(data, dest); err != nil {
			return fmt.Errorf("unmarshal to dest: %w", err)
		}
	}

	if pp.options.EnableMetrics {
		// 增加穿透允许计数
	}

	return nil
}

// setEmptyValue 设置空值缓存
func (pp *PenetrationProtection) setEmptyValue(ctx context.Context, key string) error {
	// 检查空值缓存数量限制
	if pp.options.EmptyValueMaxCount > 0 {
		emptyCount := pp.getEmptyValueCount()
		if emptyCount >= pp.options.EmptyValueMaxCount {
			// 清理一些旧的空值缓存
			pp.cleanupEmptyValues(ctx)
		}
	}

	emptyKey := pp.getEmptyValueKey(key)
	err := pp.cache.Set(ctx, emptyKey, "", pp.options.EmptyValueTTL)
	if err == nil {
		// 记录空值缓存
		pp.emptyValueStats.Store(key, time.Now().Unix())
	}
	
	return err
}

// isEmptyValue 检查是否为空值
func (pp *PenetrationProtection) isEmptyValue(value interface{}) bool {
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

// getEmptyValueKey 获取空值缓存key
func (pp *PenetrationProtection) getEmptyValueKey(key string) string {
	return fmt.Sprintf("empty:%s", key)
}

// getEmptyValueCount 获取空值缓存数量
func (pp *PenetrationProtection) getEmptyValueCount() int64 {
	var count int64
	pp.emptyValueStats.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// cleanupEmptyValues 清理空值缓存
func (pp *PenetrationProtection) cleanupEmptyValues(ctx context.Context) {
	var keysToDelete []string
	var keysToCleanup []string
	
	currentTime := time.Now().Unix()
	
	pp.emptyValueStats.Range(func(key, value interface{}) bool {
		keyStr := key.(string)
		timestamp := value.(int64)
		
		// 清理超过TTL的记录
		if currentTime-timestamp > int64(pp.options.EmptyValueTTL.Seconds()) {
			keysToDelete = append(keysToDelete, keyStr)
			keysToCleanup = append(keysToCleanup, pp.getEmptyValueKey(keyStr))
		}
		
		return len(keysToDelete) < 100 // 限制清理数量
	})
	
	// 从统计中删除
	for _, key := range keysToDelete {
		pp.emptyValueStats.Delete(key)
	}
	
	// 从缓存中删除
	if len(keysToCleanup) > 0 {
		pp.cache.Del(ctx, keysToCleanup...)
	}
}

// PrewarmBloomFilter 预热布隆过滤器
func (pp *PenetrationProtection) PrewarmBloomFilter(ctx context.Context, keys []string) error {
	if !pp.options.EnableBloomFilter || pp.bloomFilter == nil {
		return fmt.Errorf("bloom filter not enabled")
	}

	return pp.bloomFilter.AddMultiple(ctx, keys)
}

// GetBloomFilterInfo 获取布隆过滤器信息
func (pp *PenetrationProtection) GetBloomFilterInfo(ctx context.Context) (*bloom.BloomInfo, error) {
	if !pp.options.EnableBloomFilter || pp.bloomFilter == nil {
		return nil, fmt.Errorf("bloom filter not enabled")
	}

	return pp.bloomFilter.Info(ctx)
}

// GetStats 获取穿透防护统计
func (pp *PenetrationProtection) GetStats(ctx context.Context) (*PenetrationStats, error) {
	// 这里需要实现实际的指标收集逻辑
	// 可以使用 prometheus 或其他指标系统
	
	return &PenetrationStats{
		// 返回实际统计数据
	}, nil
}

// Close 关闭穿透防护
func (pp *PenetrationProtection) Close() error {
	if pp.bloomFilter != nil {
		return pp.bloomFilter.Close()
	}
	return nil
}

// defaultParameterValidator 默认参数校验器
func defaultParameterValidator(key string) error {
	if key == "" {
		return fmt.Errorf("key cannot be empty")
	}
	
	if len(key) > 250 {
		return fmt.Errorf("key too long: %d", len(key))
	}
	
	// 检查是否包含非法字符
	for _, char := range key {
		if char < 32 || char > 126 {
			return fmt.Errorf("key contains invalid character: %c", char)
		}
	}
	
	return nil
}