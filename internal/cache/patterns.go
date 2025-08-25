package cache

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// WriteFunc 写入数据源的函数类型
type WriteFunc func(ctx context.Context, key string, value interface{}) error

// WriteThroughCache Write-Through缓存模式
// 同时写入缓存和数据源，保证强一致性
type WriteThroughCache struct {
	cache     Cache
	writeFunc WriteFunc
	options   *CacheAsideOptions
}

// NewWriteThroughCache 创建Write-Through缓存
func NewWriteThroughCache(cache Cache, writeFunc WriteFunc, opts *CacheAsideOptions) *WriteThroughCache {
	if opts == nil {
		opts = DefaultCacheAsideOptions()
	}

	return &WriteThroughCache{
		cache:     cache,
		writeFunc: writeFunc,
		options:   opts,
	}
}

// Get 获取数据
func (wt *WriteThroughCache) Get(ctx context.Context, key string, dest interface{}) error {
	return wt.cache.Get(ctx, key, dest)
}

// Set Write-Through写入：同时写入缓存和数据源
func (wt *WriteThroughCache) Set(ctx context.Context, key string, value interface{}) error {
	// 1. 先写入数据源
	if err := wt.writeFunc(ctx, key, value); err != nil {
		return fmt.Errorf("write to data source failed: %w", err)
	}

	// 2. 再写入缓存
	if err := wt.cache.Set(ctx, key, value, wt.options.TTL); err != nil {
		// 缓存写入失败，记录日志但不返回错误
		// 因为数据已经成功写入数据源
		log.Printf("Cache set failed in write-through for key %s: %v", key, err)
	}

	return nil
}

// Del 删除数据
func (wt *WriteThroughCache) Del(ctx context.Context, keys ...string) error {
	return wt.cache.Del(ctx, keys...)
}

// WriteBehindCache Write-Behind(Write-Back)缓存模式
// 先写入缓存，异步写入数据源，适合写密集场景
type WriteBehindCache struct {
	cache         Cache
	writeFunc     WriteFunc
	options       *CacheAsideOptions
	writeBuffer   chan *writeRequest
	bufferSize    int
	flushInterval time.Duration
	batchSize     int
	mu            sync.RWMutex
	closed        bool
	wg            sync.WaitGroup
}

// writeRequest 写入请求
type writeRequest struct {
	key        string
	value      interface{}
	retryCount int
}

// WriteBehindOptions Write-Behind配置选项
type WriteBehindOptions struct {
	*CacheAsideOptions
	BufferSize    int           // 写入缓冲区大小
	FlushInterval time.Duration // 刷新间隔
	BatchSize     int           // 批量写入大小
	MaxRetries    int           // 最大重试次数
}

// DefaultWriteBehindOptions 默认Write-Behind配置
func DefaultWriteBehindOptions() *WriteBehindOptions {
	return &WriteBehindOptions{
		CacheAsideOptions: DefaultCacheAsideOptions(),
		BufferSize:        1000,
		FlushInterval:     5 * time.Second,
		BatchSize:         100,
		MaxRetries:        3,
	}
}

// NewWriteBehindCache 创建Write-Behind缓存
func NewWriteBehindCache(cache Cache, writeFunc WriteFunc, opts *WriteBehindOptions) *WriteBehindCache {
	if opts == nil {
		opts = DefaultWriteBehindOptions()
	}

	wb := &WriteBehindCache{
		cache:         cache,
		writeFunc:     writeFunc,
		options:       opts.CacheAsideOptions,
		writeBuffer:   make(chan *writeRequest, opts.BufferSize),
		bufferSize:    opts.BufferSize,
		flushInterval: opts.FlushInterval,
		batchSize:     opts.BatchSize,
	}

	// 启动后台写入goroutine
	wb.wg.Add(1)
	go wb.backgroundWriter(opts.MaxRetries)

	return wb
}

// Get 获取数据
func (wb *WriteBehindCache) Get(ctx context.Context, key string, dest interface{}) error {
	return wb.cache.Get(ctx, key, dest)
}

// Set Write-Behind写入：先写入缓存，异步写入数据源
func (wb *WriteBehindCache) Set(ctx context.Context, key string, value interface{}) error {
	wb.mu.RLock()
	defer wb.mu.RUnlock()

	if wb.closed {
		return fmt.Errorf("write-behind cache is closed")
	}

	// 1. 立即写入缓存
	if err := wb.cache.Set(ctx, key, value, wb.options.TTL); err != nil {
		return fmt.Errorf("cache set failed: %w", err)
	}

	// 2. 异步写入数据源
	req := &writeRequest{
		key:   key,
		value: value,
	}

	select {
	case wb.writeBuffer <- req:
		// 成功放入缓冲区
	default:
		// 缓冲区满，记录日志
		log.Printf("Write buffer full, dropping write request for key %s", key)
	}

	return nil
}

// Del 删除数据
func (wb *WriteBehindCache) Del(ctx context.Context, keys ...string) error {
	return wb.cache.Del(ctx, keys...)
}

// Flush 立即刷新所有待写入的数据
func (wb *WriteBehindCache) Flush(ctx context.Context) error {
	wb.mu.RLock()
	defer wb.mu.RUnlock()

	if wb.closed {
		return fmt.Errorf("write-behind cache is closed")
	}

	// 创建一个特殊的flush请求
	flushReq := &writeRequest{key: "__FLUSH__"}

	select {
	case wb.writeBuffer <- flushReq:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close 关闭Write-Behind缓存
func (wb *WriteBehindCache) Close() error {
	wb.mu.Lock()
	defer wb.mu.Unlock()

	if wb.closed {
		return nil
	}

	wb.closed = true
	close(wb.writeBuffer)
	wb.wg.Wait()

	return wb.cache.Close()
}

// backgroundWriter 后台写入goroutine
func (wb *WriteBehindCache) backgroundWriter(maxRetries int) {
	defer wb.wg.Done()

	ticker := time.NewTicker(wb.flushInterval)
	defer ticker.Stop()

	batch := make([]*writeRequest, 0, wb.batchSize)

	for {
		select {
		case req, ok := <-wb.writeBuffer:
			if !ok {
				// 通道关闭，刷新剩余数据后退出
				if len(batch) > 0 {
					wb.writeBatch(batch, maxRetries)
				}
				return
			}

			// 检查是否为flush请求
			if req.key == "__FLUSH__" {
				if len(batch) > 0 {
					wb.writeBatch(batch, maxRetries)
					batch = batch[:0]
				}
				continue
			}

			batch = append(batch, req)

			// 批次满了，立即写入
			if len(batch) >= wb.batchSize {
				wb.writeBatch(batch, maxRetries)
				batch = batch[:0]
			}

		case <-ticker.C:
			// 定时刷新
			if len(batch) > 0 {
				wb.writeBatch(batch, maxRetries)
				batch = batch[:0]
			}
		}
	}
}

// writeBatch 批量写入数据源
func (wb *WriteBehindCache) writeBatch(batch []*writeRequest, maxRetries int) {
	for _, req := range batch {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		err := wb.writeFunc(ctx, req.key, req.value)
		cancel()

		if err != nil {
			log.Printf("Write to data source failed for key %s: %v", req.key, err)

			// 重试逻辑
			if req.retryCount < maxRetries {
				req.retryCount++

				// 放回缓冲区重试
				select {
				case wb.writeBuffer <- req:
					// 重新排队成功
				default:
					// 缓冲区满，放弃重试
					log.Printf("Failed to requeue failed write for key %s", req.key)
				}
			} else {
				log.Printf("Exceeded max retries for key %s, dropping write request", req.key)
			}
		}
	}
}

// GetStats 获取统计信息
func (wb *WriteBehindCache) GetStats() (*CacheStats, map[string]interface{}) {
	cacheStats := wb.cache.(*RedisCache).GetStats()

	wb.mu.RLock()
	bufferLen := len(wb.writeBuffer)
	wb.mu.RUnlock()

	writeBehindStats := map[string]interface{}{
		"buffer_length": bufferLen,
		"buffer_size":   wb.bufferSize,
		"closed":        wb.closed,
	}

	return cacheStats, writeBehindStats
}
