package cache

import (
	"sync/atomic"
	"time"
)

// CacheMetrics 缓存指标
type CacheMetrics struct {
	// 缓存操作统计
	hits   int64 // 缓存命中数
	misses int64 // 缓存未命中数
	sets   int64 // 缓存设置数
	dels   int64 // 缓存删除数

	// 延迟统计（微秒）
	getTotalLatency int64 // Get操作总延迟
	setTotalLatency int64 // Set操作总延迟

	// 错误统计
	getErrors int64 // Get操作错误数
	setErrors int64 // Set操作错误数
	delErrors int64 // Del操作错误数

	// 序列化统计
	serializeErrors   int64 // 序列化错误数
	deserializeErrors int64 // 反序列化错误数
}

// NewCacheMetrics 创建缓存指标
func NewCacheMetrics() *CacheMetrics {
	return &CacheMetrics{}
}

// IncHits 增加命中数
func (m *CacheMetrics) IncHits() {
	atomic.AddInt64(&m.hits, 1)
}

// IncMisses 增加未命中数
func (m *CacheMetrics) IncMisses() {
	atomic.AddInt64(&m.misses, 1)
}

// IncSets 增加设置数
func (m *CacheMetrics) IncSets() {
	atomic.AddInt64(&m.sets, 1)
}

// IncDels 增加删除数
func (m *CacheMetrics) IncDels() {
	atomic.AddInt64(&m.dels, 1)
}

// RecordGetLatency 记录Get操作延迟
func (m *CacheMetrics) RecordGetLatency(duration time.Duration) {
	atomic.AddInt64(&m.getTotalLatency, duration.Microseconds())
}

// RecordSetLatency 记录Set操作延迟
func (m *CacheMetrics) RecordSetLatency(duration time.Duration) {
	atomic.AddInt64(&m.setTotalLatency, duration.Microseconds())
}

// IncGetErrors 增加Get错误数
func (m *CacheMetrics) IncGetErrors() {
	atomic.AddInt64(&m.getErrors, 1)
}

// IncSetErrors 增加Set错误数
func (m *CacheMetrics) IncSetErrors() {
	atomic.AddInt64(&m.setErrors, 1)
}

// IncDelErrors 增加Del错误数
func (m *CacheMetrics) IncDelErrors() {
	atomic.AddInt64(&m.delErrors, 1)
}

// IncSerializeErrors 增加序列化错误数
func (m *CacheMetrics) IncSerializeErrors() {
	atomic.AddInt64(&m.serializeErrors, 1)
}

// IncDeserializeErrors 增加反序列化错误数
func (m *CacheMetrics) IncDeserializeErrors() {
	atomic.AddInt64(&m.deserializeErrors, 1)
}

// Stats 返回统计信息
type CacheStats struct {
	Hits              int64   `json:"hits"`
	Misses            int64   `json:"misses"`
	Sets              int64   `json:"sets"`
	Dels              int64   `json:"dels"`
	HitRate           float64 `json:"hit_rate"`
	GetErrors         int64   `json:"get_errors"`
	SetErrors         int64   `json:"set_errors"`
	DelErrors         int64   `json:"del_errors"`
	SerializeErrors   int64   `json:"serialize_errors"`
	DeserializeErrors int64   `json:"deserialize_errors"`
	AvgGetLatencyUs   float64 `json:"avg_get_latency_us"`
	AvgSetLatencyUs   float64 `json:"avg_set_latency_us"`
}

// GetStats 获取统计信息
func (m *CacheMetrics) GetStats() *CacheStats {
	hits := atomic.LoadInt64(&m.hits)
	misses := atomic.LoadInt64(&m.misses)
	sets := atomic.LoadInt64(&m.sets)
	dels := atomic.LoadInt64(&m.dels)

	total := hits + misses
	var hitRate float64
	if total > 0 {
		hitRate = float64(hits) / float64(total)
	}

	getTotalLatency := atomic.LoadInt64(&m.getTotalLatency)
	setTotalLatency := atomic.LoadInt64(&m.setTotalLatency)

	var avgGetLatency, avgSetLatency float64
	if hits+misses > 0 {
		avgGetLatency = float64(getTotalLatency) / float64(hits+misses)
	}
	if sets > 0 {
		avgSetLatency = float64(setTotalLatency) / float64(sets)
	}

	return &CacheStats{
		Hits:              hits,
		Misses:            misses,
		Sets:              sets,
		Dels:              dels,
		HitRate:           hitRate,
		GetErrors:         atomic.LoadInt64(&m.getErrors),
		SetErrors:         atomic.LoadInt64(&m.setErrors),
		DelErrors:         atomic.LoadInt64(&m.delErrors),
		SerializeErrors:   atomic.LoadInt64(&m.serializeErrors),
		DeserializeErrors: atomic.LoadInt64(&m.deserializeErrors),
		AvgGetLatencyUs:   avgGetLatency,
		AvgSetLatencyUs:   avgSetLatency,
	}
}

// Reset 重置统计信息
func (m *CacheMetrics) Reset() {
	atomic.StoreInt64(&m.hits, 0)
	atomic.StoreInt64(&m.misses, 0)
	atomic.StoreInt64(&m.sets, 0)
	atomic.StoreInt64(&m.dels, 0)
	atomic.StoreInt64(&m.getTotalLatency, 0)
	atomic.StoreInt64(&m.setTotalLatency, 0)
	atomic.StoreInt64(&m.getErrors, 0)
	atomic.StoreInt64(&m.setErrors, 0)
	atomic.StoreInt64(&m.delErrors, 0)
	atomic.StoreInt64(&m.serializeErrors, 0)
	atomic.StoreInt64(&m.deserializeErrors, 0)
}
