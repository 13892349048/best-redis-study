package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"
)

// LogicalExpireManager 逻辑过期管理器
type LogicalExpireManager struct {
	cache       Cache
	config      *LogicalExpireConfig
	refreshPool *RefreshPool
	stats       LogicalExpireStats
}

// LogicalExpireData 逻辑过期数据包装
type LogicalExpireData struct {
	Data       interface{} `json:"data"`        // 实际数据
	ExpireTime int64       `json:"expire_time"` // 逻辑过期时间戳（秒）
	Version    int64       `json:"version"`     // 数据版本号
}

// LogicalExpireStats 逻辑过期统计
type LogicalExpireStats struct {
	Hits         int64 // 逻辑未过期命中
	ExpiredHits  int64 // 逻辑过期命中（返回旧数据）
	RefreshCount int64 // 后台刷新次数
	ExpiredCount int64 // 过期数据次数
	mu           sync.RWMutex
}

// RefreshPool 刷新协程池
type RefreshPool struct {
	workerCount int
	jobChan     chan *RefreshJob
	quit        chan struct{}
	wg          sync.WaitGroup
}

// RefreshJob 刷新任务
type RefreshJob struct {
	Key    string
	Loader LoaderFunc
	ctx    context.Context
	cache  Cache
	config *LogicalExpireConfig
}

// NewLogicalExpireManager 创建逻辑过期管理器
func NewLogicalExpireManager(cache Cache, config *LogicalExpireConfig) *LogicalExpireManager {
	if config == nil {
		config = &LogicalExpireConfig{
			LogicalTTL:           30 * time.Minute,
			PhysicalTTL:          60 * time.Minute,
			RefreshAdvance:       5 * time.Minute,
			MaxConcurrentRefresh: 10,
		}
	}

	lem := &LogicalExpireManager{
		cache:  cache,
		config: config,
	}

	// 创建刷新协程池
	lem.refreshPool = NewRefreshPool(config.MaxConcurrentRefresh)

	return lem
}

// GetOrLoad 获取缓存数据（逻辑过期策略）
func (lem *LogicalExpireManager) GetOrLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 1. 尝试从缓存获取数据
	var expireData LogicalExpireData
	err := lem.cache.Get(ctx, key, &expireData)
	
	if err == nil {
		// 缓存命中，检查逻辑过期时间
		now := time.Now().Unix()
		
		if expireData.ExpireTime > now {
			// 数据未过期，直接返回
			lem.incrementHits()
			return lem.assignData(expireData.Data, dest)
		}
		
		// 数据已逻辑过期
		lem.incrementExpiredHits()
		
		// 异步刷新数据
		lem.asyncRefresh(ctx, key, loader)
		
		// 返回过期数据（用户无感知）
		return lem.assignData(expireData.Data, dest)
	}
	
	// 缓存未命中或读取出错
	if err != ErrCacheMiss {
		log.Printf("Cache get error for key %s: %v", key, err)
	}
	
	// 同步加载数据
	return lem.syncLoad(ctx, key, dest, loader)
}

// SetWithLogicalExpire 设置带逻辑过期的缓存
func (lem *LogicalExpireManager) SetWithLogicalExpire(ctx context.Context, key string, value interface{}) error {
	now := time.Now()
	expireData := LogicalExpireData{
		Data:       value,
		ExpireTime: now.Add(lem.config.LogicalTTL).Unix(),
		Version:    now.UnixNano(), // 使用纳秒时间戳作为版本号
	}
	
	return lem.cache.Set(ctx, key, expireData, lem.config.PhysicalTTL)
}

// syncLoad 同步加载数据
func (lem *LogicalExpireManager) syncLoad(ctx context.Context, key string, dest interface{}, loader LoaderFunc) error {
	// 调用加载函数
	value, err := loader(ctx, key)
	if err != nil {
		return fmt.Errorf("loader failed: %w", err)
	}
	
	// 设置到缓存
	if err := lem.SetWithLogicalExpire(ctx, key, value); err != nil {
		log.Printf("Cache set error for key %s: %v", key, err)
	}
	
	// 赋值给dest
	return lem.assignData(value, dest)
}

// asyncRefresh 异步刷新数据
func (lem *LogicalExpireManager) asyncRefresh(ctx context.Context, key string, loader LoaderFunc) {
	job := &RefreshJob{
		Key:    key,
		Loader: loader,
		ctx:    ctx,
		cache:  lem.cache,
		config: lem.config,
	}
	
	// 提交到刷新协程池
	select {
	case lem.refreshPool.jobChan <- job:
		// 成功提交
	default:
		// 刷新池满，丢弃任务
		log.Printf("Refresh pool full, dropping refresh job for key %s", key)
	}
}

// assignData 将数据赋值给目标对象
func (lem *LogicalExpireManager) assignData(data interface{}, dest interface{}) error {
	// 使用JSON序列化/反序列化进行类型转换
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal data: %w", err)
	}
	
	return json.Unmarshal(jsonData, dest)
}

// GetStats 获取统计信息
func (lem *LogicalExpireManager) GetStats() LogicalExpireStats {
	lem.stats.mu.RLock()
	defer lem.stats.mu.RUnlock()
	
	return LogicalExpireStats{
		Hits:         lem.stats.Hits,
		ExpiredHits:  lem.stats.ExpiredHits,
		RefreshCount: lem.stats.RefreshCount,
		ExpiredCount: lem.stats.ExpiredCount,
	}
}

// Close 关闭逻辑过期管理器
func (lem *LogicalExpireManager) Close() error {
	if lem.refreshPool != nil {
		return lem.refreshPool.Close()
	}
	return nil
}

// incrementHits 增加命中计数
func (lem *LogicalExpireManager) incrementHits() {
	lem.stats.mu.Lock()
	lem.stats.Hits++
	lem.stats.mu.Unlock()
}

// incrementExpiredHits 增加过期命中计数
func (lem *LogicalExpireManager) incrementExpiredHits() {
	lem.stats.mu.Lock()
	lem.stats.ExpiredHits++
	lem.stats.mu.Unlock()
}

// incrementRefreshCount 增加刷新计数
func (lem *LogicalExpireManager) incrementRefreshCount() {
	lem.stats.mu.Lock()
	lem.stats.RefreshCount++
	lem.stats.mu.Unlock()
}

// incrementExpiredCount 增加过期计数
func (lem *LogicalExpireManager) incrementExpiredCount() {
	lem.stats.mu.Lock()
	lem.stats.ExpiredCount++
	lem.stats.mu.Unlock()
}

// NewRefreshPool 创建刷新协程池
func NewRefreshPool(workerCount int) *RefreshPool {
	if workerCount <= 0 {
		workerCount = 10
	}
	
	pool := &RefreshPool{
		workerCount: workerCount,
		jobChan:     make(chan *RefreshJob, workerCount*2), // 缓冲为worker数量的2倍
		quit:        make(chan struct{}),
	}
	
	// 启动worker协程
	for i := 0; i < workerCount; i++ {
		pool.wg.Add(1)
		go pool.worker()
	}
	
	return pool
}

// worker 工作协程
func (rp *RefreshPool) worker() {
	defer rp.wg.Done()
	
	for {
		select {
		case job := <-rp.jobChan:
			rp.processJob(job)
		case <-rp.quit:
			return
		}
	}
}

// processJob 处理刷新任务
func (rp *RefreshPool) processJob(job *RefreshJob) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Refresh job panic for key %s: %v", job.Key, r)
		}
	}()
	
	// 创建带超时的上下文
	ctx, cancel := context.WithTimeout(job.ctx, 30*time.Second)
	defer cancel()
	
	// 调用加载函数
	value, err := job.Loader(ctx, job.Key)
	if err != nil {
		log.Printf("Refresh loader failed for key %s: %v", job.Key, err)
		return
	}
	
	// 创建新的过期数据
	now := time.Now()
	expireData := LogicalExpireData{
		Data:       value,
		ExpireTime: now.Add(job.config.LogicalTTL).Unix(),
		Version:    now.UnixNano(),
	}
	
	// 更新缓存
	if err := job.cache.Set(ctx, job.Key, expireData, job.config.PhysicalTTL); err != nil {
		log.Printf("Refresh cache set failed for key %s: %v", job.Key, err)
	} else {
		log.Printf("Successfully refreshed cache for key %s", job.Key)
	}
}

// Close 关闭刷新协程池
func (rp *RefreshPool) Close() error {
	close(rp.quit)
	
	// 等待所有worker完成（带超时）
	done := make(chan struct{})
	go func() {
		rp.wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for refresh pool workers to stop")
	}
}

// GetQueueLength 获取队列长度
func (rp *RefreshPool) GetQueueLength() int {
	return len(rp.jobChan)
}

// IsExpiringSoon 检查数据是否即将逻辑过期
func (lem *LogicalExpireManager) IsExpiringSoon(expireTime int64) bool {
	now := time.Now().Unix()
	advanceTime := now + int64(lem.config.RefreshAdvance.Seconds())
	
	return expireTime <= advanceTime && expireTime > now
}