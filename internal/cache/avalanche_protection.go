package cache

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

var (
	// ErrCircuitBreakerOpen 熔断器打开错误
	ErrCircuitBreakerOpen = errors.New("circuit breaker is open")

	// ErrDegraded 降级错误
	ErrDegraded = errors.New("service degraded")
)

// CircuitBreakerState 熔断器状态
type CircuitBreakerState int32

const (
	// StateClosed 关闭状态（正常）
	StateClosed CircuitBreakerState = iota
	// StateOpen 打开状态（熔断）
	StateOpen
	// StateHalfOpen 半开状态（探测）
	StateHalfOpen
)

// CircuitBreakerConfig 熔断器配置
type CircuitBreakerConfig struct {
	// FailureThreshold 失败阈值
	FailureThreshold int
	// FailureRate 失败率阈值 (0.0-1.0)
	FailureRate float64
	// MinRequestCount 最小请求数（达到此数量才开始计算失败率）
	MinRequestCount int
	// OpenTimeout 熔断器打开持续时间
	OpenTimeout time.Duration
	// HalfOpenMaxCalls 半开状态下的最大探测请求数
	HalfOpenMaxCalls int
	// SlidingWindowSize 滑动窗口大小（秒）
	SlidingWindowSize int
}

// DefaultCircuitBreakerConfig 默认熔断器配置
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		FailureThreshold:  10,
		FailureRate:       0.5, // 50%
		MinRequestCount:   20,
		OpenTimeout:       30 * time.Second,
		HalfOpenMaxCalls:  3,
		SlidingWindowSize: 60, // 60秒滑动窗口
	}
}

// CircuitBreaker 熔断器
type CircuitBreaker struct {
	config        *CircuitBreakerConfig
	state         CircuitBreakerState
	failureCount  int64
	requestCount  int64
	lastFailTime  time.Time
	halfOpenCalls int64
	mu            sync.RWMutex

	// 滑动窗口统计
	window      []WindowBucket
	windowIndex int
	windowMu    sync.RWMutex
}

// WindowBucket 窗口桶
type WindowBucket struct {
	timestamp    time.Time
	requestCount int64
	failureCount int64
}

// NewCircuitBreaker 创建熔断器
func NewCircuitBreaker(config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	cb := &CircuitBreaker{
		config: config,
		state:  StateClosed,
		window: make([]WindowBucket, config.SlidingWindowSize),
	}

	// 初始化滑动窗口
	now := time.Now()
	for i := range cb.window {
		cb.window[i].timestamp = now.Add(time.Duration(-i) * time.Second)
	}

	return cb
}

// CanExecute 检查是否可以执行请求
func (cb *CircuitBreaker) CanExecute() error {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()

	switch cb.state {
	case StateClosed:
		return nil

	case StateOpen:
		// 检查是否可以转换到半开状态
		if now.Sub(cb.lastFailTime) >= cb.config.OpenTimeout {
			cb.state = StateHalfOpen
			cb.halfOpenCalls = 0
			return nil
		}
		return ErrCircuitBreakerOpen

	case StateHalfOpen:
		// 半开状态下限制并发请求数
		if cb.halfOpenCalls >= int64(cb.config.HalfOpenMaxCalls) {
			return ErrCircuitBreakerOpen
		}
		atomic.AddInt64(&cb.halfOpenCalls, 1)
		return nil

	default:
		return ErrCircuitBreakerOpen
	}
}

// RecordSuccess 记录成功请求
func (cb *CircuitBreaker) RecordSuccess() {
	cb.updateWindow(false)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		// 半开状态下的成功，可能转换到关闭状态
		successCount := cb.halfOpenCalls - cb.getRecentFailures()
		if successCount >= int64(cb.config.HalfOpenMaxCalls) {
			cb.state = StateClosed
			cb.failureCount = 0
			cb.requestCount = 0
		}
	}
}

// RecordFailure 记录失败请求
func (cb *CircuitBreaker) RecordFailure() {
	cb.updateWindow(true)

	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailTime = time.Now()

	switch cb.state {
	case StateClosed:
		// 检查是否需要打开熔断器
		if cb.shouldOpen() {
			cb.state = StateOpen
			log.Printf("Circuit breaker opened due to failure rate: %.2f", cb.getFailureRate())
		}

	case StateHalfOpen:
		// 半开状态下的失败，立即转换到打开状态
		cb.state = StateOpen
		log.Printf("Circuit breaker re-opened in half-open state")
	}
}

// updateWindow 更新滑动窗口
func (cb *CircuitBreaker) updateWindow(isFailure bool) {
	cb.windowMu.Lock()
	defer cb.windowMu.Unlock()

	now := time.Now()
	currentSecond := now.Truncate(time.Second)

	// 找到当前时间对应的桶
	if cb.window[cb.windowIndex].timestamp.Before(currentSecond) {
		// 需要创建新桶
		cb.windowIndex = (cb.windowIndex + 1) % len(cb.window)
		cb.window[cb.windowIndex] = WindowBucket{
			timestamp: currentSecond,
		}
	}

	// 更新统计
	atomic.AddInt64(&cb.window[cb.windowIndex].requestCount, 1)
	if isFailure {
		atomic.AddInt64(&cb.window[cb.windowIndex].failureCount, 1)
	}
}

// shouldOpen 判断是否应该打开熔断器
func (cb *CircuitBreaker) shouldOpen() bool {
	totalRequests, totalFailures := cb.getWindowStats()

	// 请求数不足，不打开
	if totalRequests < int64(cb.config.MinRequestCount) {
		return false
	}

	// 检查失败率
	failureRate := float64(totalFailures) / float64(totalRequests)
	return failureRate >= cb.config.FailureRate
}

// getWindowStats 获取滑动窗口统计
func (cb *CircuitBreaker) getWindowStats() (requests, failures int64) {
	cb.windowMu.RLock()
	defer cb.windowMu.RUnlock()

	cutoff := time.Now().Add(-time.Duration(cb.config.SlidingWindowSize) * time.Second)

	for _, bucket := range cb.window {
		if bucket.timestamp.After(cutoff) {
			requests += atomic.LoadInt64(&bucket.requestCount)
			failures += atomic.LoadInt64(&bucket.failureCount)
		}
	}

	return requests, failures
}

// getFailureRate 获取当前失败率
func (cb *CircuitBreaker) getFailureRate() float64 {
	requests, failures := cb.getWindowStats()
	if requests == 0 {
		return 0
	}
	return float64(failures) / float64(requests)
}

// getRecentFailures 获取半开状态下的最近失败数
func (cb *CircuitBreaker) getRecentFailures() int64 {
	// 简化实现：返回半开状态期间的失败数
	return 0 // 实际实现需要更精确的统计
}

// GetState 获取熔断器状态
func (cb *CircuitBreaker) GetState() CircuitBreakerState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// GetStats 获取熔断器统计信息
func (cb *CircuitBreaker) GetStats() map[string]interface{} {
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()

	requests, failures := cb.getWindowStats()
	failureRate := cb.getFailureRate()

	var stateStr string
	switch state {
	case StateClosed:
		stateStr = "closed"
	case StateOpen:
		stateStr = "open"
	case StateHalfOpen:
		stateStr = "half-open"
	}

	return map[string]interface{}{
		"state":           stateStr,
		"requests":        requests,
		"failures":        failures,
		"failure_rate":    failureRate,
		"half_open_calls": atomic.LoadInt64(&cb.halfOpenCalls),
	}
}

// AvalancheProtection 雪崩防护
type AvalancheProtection struct {
	cache          Cache
	options        *CacheAsideOptions
	circuitBreaker *CircuitBreaker
	degradeFunc    func(ctx context.Context, key string) (interface{}, error)

	// TTL分片配置
	shardCount    int
	shardInterval time.Duration

	// 预热配置
	warmupKeys   []string
	warmupLoader LoaderFunc
	warmupTicker *time.Ticker
	warmupStop   chan struct{}
	warmupMu     sync.RWMutex

	mu sync.RWMutex
}

// AvalancheProtectionConfig 雪崩防护配置
type AvalancheProtectionConfig struct {
	*CacheAsideOptions
	*CircuitBreakerConfig

	// TTL分片配置
	EnableTTLSharding bool
	ShardCount        int
	ShardInterval     time.Duration

	// 预热配置
	EnableWarmup   bool
	WarmupInterval time.Duration
	WarmupKeys     []string

	// 降级函数
	DegradeFunc func(ctx context.Context, key string) (interface{}, error)
}

// DefaultAvalancheProtectionConfig 默认雪崩防护配置
func DefaultAvalancheProtectionConfig() *AvalancheProtectionConfig {
	return &AvalancheProtectionConfig{
		CacheAsideOptions:    DefaultCacheAsideOptions(),
		CircuitBreakerConfig: DefaultCircuitBreakerConfig(),
		EnableTTLSharding:    true,
		ShardCount:           10,
		ShardInterval:        time.Minute,
		EnableWarmup:         false,
		WarmupInterval:       5 * time.Minute,
	}
}

// NewAvalancheProtection 创建雪崩防护
func NewAvalancheProtection(cache Cache, config *AvalancheProtectionConfig) *AvalancheProtection {
	if config == nil {
		config = DefaultAvalancheProtectionConfig()
	}

	ap := &AvalancheProtection{
		cache:          cache,
		options:        config.CacheAsideOptions,
		circuitBreaker: NewCircuitBreaker(config.CircuitBreakerConfig),
		degradeFunc:    config.DegradeFunc,
		shardCount:     config.ShardCount,
		shardInterval:  config.ShardInterval,
		warmupKeys:     config.WarmupKeys,
		warmupStop:     make(chan struct{}),
	}

	// 启动预热
	if config.EnableWarmup && len(config.WarmupKeys) > 0 {
		ap.startWarmup(config.WarmupInterval)
	}

	return ap
}

// GetOrLoad 获取缓存数据（带雪崩防护）
func (ap *AvalancheProtection) GetOrLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 1. 检查熔断器
	if err := ap.circuitBreaker.CanExecute(); err != nil {
		// 熔断器打开，尝试降级
		if ap.degradeFunc != nil {
			value, degErr := ap.degradeFunc(ctx, key)
			if degErr == nil {
				return ap.copyValue(value, dest)
			}
		}
		return err
	}

	// 2. 尝试从缓存获取
	err := ap.cache.Get(ctx, key, dest)
	if err == nil {
		ap.circuitBreaker.RecordSuccess()
		return nil
	}

	// 3. 缓存未命中，从数据源加载
	if !errors.Is(err, ErrCacheMiss) {
		// 缓存读取错误
		ap.circuitBreaker.RecordFailure()
		log.Printf("Cache get error for key %s: %v", key, err)
	}

	// 4. 调用加载函数
	value, err := loader(ctx, key)
	if err != nil {
		ap.circuitBreaker.RecordFailure()

		// 尝试降级
		if ap.degradeFunc != nil {
			value, degErr := ap.degradeFunc(ctx, key)
			if degErr == nil {
				return ap.copyValue(value, dest)
			}
		}

		return fmt.Errorf("loader failed: %w", err)
	}

	// 5. 写入缓存（带分片TTL）
	ttl := ap.calculateShardedTTL(key, ap.options.TTL)
	if err := ap.cache.Set(ctx, key, value, ttl); err != nil {
		log.Printf("Cache set error for key %s: %v", key, err)
		// 不记录为失败，因为数据已经成功加载
	}

	ap.circuitBreaker.RecordSuccess()
	return ap.copyValue(value, dest)
}

// calculateShardedTTL 计算分片TTL
func (ap *AvalancheProtection) calculateShardedTTL(key string, baseTTL time.Duration) time.Duration {
	if ap.shardCount <= 1 {
		return ap.calculateJitteredTTL(baseTTL)
	}

	// 根据key计算分片
	hash := ap.hashKey(key)
	shard := hash % ap.shardCount

	// 计算分片偏移
	shardOffset := time.Duration(shard) * ap.shardInterval

	// 基础TTL + 分片偏移 + 随机抖动
	shardedTTL := baseTTL + shardOffset
	return ap.calculateJitteredTTL(shardedTTL)
}

// calculateJitteredTTL 计算带抖动的TTL
func (ap *AvalancheProtection) calculateJitteredTTL(baseTTL time.Duration) time.Duration {
	if ap.options.TTLJitter <= 0 {
		return baseTTL
	}

	jitter := ap.options.TTLJitter
	minMultiplier := 1.0 - jitter
	maxMultiplier := 1.0 + jitter

	multiplier := minMultiplier + rand.Float64()*(maxMultiplier-minMultiplier)
	return time.Duration(float64(baseTTL) * multiplier)
}

// hashKey 计算key的哈希值
func (ap *AvalancheProtection) hashKey(key string) int {
	hash := 0
	for _, c := range key {
		hash = hash*31 + int(c)
	}
	if hash < 0 {
		hash = -hash
	}
	return hash
}

// copyValue 复制值到目标对象
func (ap *AvalancheProtection) copyValue(value interface{}, dest interface{}) error {
	if ap.options.Serializer != nil {
		data, err := ap.options.Serializer.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal value: %w", err)
		}

		if err := ap.options.Serializer.Unmarshal(data, dest); err != nil {
			return fmt.Errorf("unmarshal to dest: %w", err)
		}
	} else {
		return errors.New("no serializer configured")
	}

	return nil
}

// SetWarmupKeys 设置预热key列表
func (ap *AvalancheProtection) SetWarmupKeys(keys []string, loader LoaderFunc) {
	ap.warmupMu.Lock()
	defer ap.warmupMu.Unlock()

	ap.warmupKeys = keys
	ap.warmupLoader = loader
}

// startWarmup 启动预热
func (ap *AvalancheProtection) startWarmup(interval time.Duration) {
	ap.warmupMu.Lock()
	defer ap.warmupMu.Unlock()

	if ap.warmupTicker != nil {
		ap.warmupTicker.Stop()
	}

	ap.warmupTicker = time.NewTicker(interval)

	go func() {
		defer ap.warmupTicker.Stop()

		for {
			select {
			case <-ap.warmupTicker.C:
				ap.performWarmup()
			case <-ap.warmupStop:
				return
			}
		}
	}()
}

// performWarmup 执行预热
func (ap *AvalancheProtection) performWarmup() {
	ap.warmupMu.RLock()
	keys := make([]string, len(ap.warmupKeys))
	copy(keys, ap.warmupKeys)
	loader := ap.warmupLoader
	ap.warmupMu.RUnlock()

	if loader == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, key := range keys {
		// 检查缓存是否存在
		exists, err := ap.cache.Exists(ctx, key)
		if err != nil {
			log.Printf("Check cache exists failed for warmup key %s: %v", key, err)
			continue
		}

		if exists > 0 {
			continue // 缓存已存在，跳过
		}

		// 加载数据
		value, err := loader(ctx, key)
		if err != nil {
			log.Printf("Warmup loader failed for key %s: %v", key, err)
			continue
		}

		// 写入缓存
		ttl := ap.calculateShardedTTL(key, ap.options.TTL)
		if err := ap.cache.Set(ctx, key, value, ttl); err != nil {
			log.Printf("Warmup cache set failed for key %s: %v", key, err)
		} else {
			log.Printf("Warmup cache set success for key %s", key)
		}
	}
}

// ForceWarmup 强制执行一次预热
func (ap *AvalancheProtection) ForceWarmup() {
	go ap.performWarmup()
}

// GetCircuitBreakerStats 获取熔断器统计
func (ap *AvalancheProtection) GetCircuitBreakerStats() map[string]interface{} {
	return ap.circuitBreaker.GetStats()
}

// SetDegradeFunc 设置降级函数
func (ap *AvalancheProtection) SetDegradeFunc(fn func(ctx context.Context, key string) (interface{}, error)) {
	ap.mu.Lock()
	defer ap.mu.Unlock()
	ap.degradeFunc = fn
}

// Close 关闭雪崩防护
func (ap *AvalancheProtection) Close() error {
	ap.warmupMu.Lock()
	defer ap.warmupMu.Unlock()

	if ap.warmupTicker != nil {
		ap.warmupTicker.Stop()
	}

	close(ap.warmupStop)
	return ap.cache.Close()
}

// BatchSetWithSharding 批量设置缓存（带分片）
func (ap *AvalancheProtection) BatchSetWithSharding(ctx context.Context, items map[string]interface{}, baseTTL time.Duration) error {
	for key, value := range items {
		ttl := ap.calculateShardedTTL(key, baseTTL)
		if err := ap.cache.Set(ctx, key, value, ttl); err != nil {
			log.Printf("Batch set failed for key %s: %v", key, err)
		}
	}
	return nil
}

// GetStats 获取雪崩防护统计信息
func (ap *AvalancheProtection) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})

	// 熔断器统计
	stats["circuit_breaker"] = ap.circuitBreaker.GetStats()

	// 分片配置
	stats["shard_count"] = ap.shardCount
	stats["shard_interval"] = ap.shardInterval.String()

	// 预热配置
	ap.warmupMu.RLock()
	stats["warmup_keys_count"] = len(ap.warmupKeys)
	stats["warmup_enabled"] = ap.warmupTicker != nil
	ap.warmupMu.RUnlock()

	// 缓存统计
	if redisCache, ok := ap.cache.(*RedisCache); ok {
		if cacheStats := redisCache.GetStats(); cacheStats != nil {
			stats["cache"] = cacheStats
		}
	}

	return stats
}
