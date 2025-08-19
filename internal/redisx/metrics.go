package redisx

import (
	"context"
	"net"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

// Metrics Redis客户端指标
type Metrics struct {
	// 命令统计
	commandTotal   int64 // 总命令数
	commandSuccess int64 // 成功命令数
	commandFailure int64 // 失败命令数

	// 延迟统计（微秒）
	totalLatency int64 // 总延迟
	maxLatency   int64 // 最大延迟
	minLatency   int64 // 最小延迟

	// 健康检查
	healthCheckTotal    int64 // 健康检查总数
	healthCheckFailures int64 // 健康检查失败数
	healthCheckLatency  int64 // 健康检查延迟

	// 连接池统计
	poolHits     int64 // 连接池命中数
	poolMisses   int64 // 连接池未命中数
	poolTimeouts int64 // 连接池超时数

	// Pipeline统计
	pipelineTotal int64 // Pipeline总数
	pipelineBatch int64 // Pipeline批次总数
}

// NewMetrics 创建新的指标对象
func NewMetrics() *Metrics {
	return &Metrics{
		minLatency: int64(^uint64(0) >> 1), // 初始化为最大值
	}
}

// IncCommandTotal 增加总命令数
func (m *Metrics) IncCommandTotal() {
	atomic.AddInt64(&m.commandTotal, 1)
}

// IncCommandSuccess 增加成功命令数
func (m *Metrics) IncCommandSuccess() {
	atomic.AddInt64(&m.commandSuccess, 1)
}

// IncCommandFailure 增加失败命令数
func (m *Metrics) IncCommandFailure() {
	atomic.AddInt64(&m.commandFailure, 1)
}

// RecordLatency 记录延迟
func (m *Metrics) RecordLatency(duration time.Duration) {
	latencyMicros := duration.Microseconds()

	// 更新总延迟
	atomic.AddInt64(&m.totalLatency, latencyMicros)

	// 更新最大延迟
	for {
		current := atomic.LoadInt64(&m.maxLatency)
		if latencyMicros <= current {
			break
		}
		if atomic.CompareAndSwapInt64(&m.maxLatency, current, latencyMicros) {
			break
		}
	}

	// 更新最小延迟
	for {
		current := atomic.LoadInt64(&m.minLatency)
		if latencyMicros >= current {
			break
		}
		if atomic.CompareAndSwapInt64(&m.minLatency, current, latencyMicros) {
			break
		}
	}
}

// IncHealthCheckFailures 增加健康检查失败数
func (m *Metrics) IncHealthCheckFailures() {
	atomic.AddInt64(&m.healthCheckFailures, 1)
}

// RecordHealthCheck 记录健康检查
func (m *Metrics) RecordHealthCheck(duration time.Duration) {
	atomic.AddInt64(&m.healthCheckTotal, 1)
	atomic.AddInt64(&m.healthCheckLatency, duration.Microseconds())
}

// IncPoolHits 增加连接池命中数
func (m *Metrics) IncPoolHits() {
	atomic.AddInt64(&m.poolHits, 1)
}

// IncPoolMisses 增加连接池未命中数
func (m *Metrics) IncPoolMisses() {
	atomic.AddInt64(&m.poolMisses, 1)
}

// IncPoolTimeouts 增加连接池超时数
func (m *Metrics) IncPoolTimeouts() {
	atomic.AddInt64(&m.poolTimeouts, 1)
}

// IncPipelineTotal 增加Pipeline总数
func (m *Metrics) IncPipelineTotal() {
	atomic.AddInt64(&m.pipelineTotal, 1)
}

// AddPipelineBatch 增加Pipeline批次数
func (m *Metrics) AddPipelineBatch(count int64) {
	atomic.AddInt64(&m.pipelineBatch, count)
}

// GetStats 获取统计信息
func (m *Metrics) GetStats() *MetricsStats {
	total := atomic.LoadInt64(&m.commandTotal)
	success := atomic.LoadInt64(&m.commandSuccess)
	failure := atomic.LoadInt64(&m.commandFailure)
	totalLatency := atomic.LoadInt64(&m.totalLatency)
	maxLatency := atomic.LoadInt64(&m.maxLatency)
	minLatency := atomic.LoadInt64(&m.minLatency)

	var avgLatency float64
	if total > 0 {
		avgLatency = float64(totalLatency) / float64(total)
	}

	var successRate float64
	if total > 0 {
		successRate = float64(success) / float64(total) * 100
	}

	return &MetricsStats{
		CommandTotal:   total,
		CommandSuccess: success,
		CommandFailure: failure,
		SuccessRate:    successRate,

		AvgLatencyMicros: avgLatency,
		MaxLatencyMicros: maxLatency,
		MinLatencyMicros: minLatency,

		HealthCheckTotal:    atomic.LoadInt64(&m.healthCheckTotal),
		HealthCheckFailures: atomic.LoadInt64(&m.healthCheckFailures),
		HealthCheckLatency:  atomic.LoadInt64(&m.healthCheckLatency),

		PoolHits:     atomic.LoadInt64(&m.poolHits),
		PoolMisses:   atomic.LoadInt64(&m.poolMisses),
		PoolTimeouts: atomic.LoadInt64(&m.poolTimeouts),

		PipelineTotal: atomic.LoadInt64(&m.pipelineTotal),
		PipelineBatch: atomic.LoadInt64(&m.pipelineBatch),
	}
}

// MetricsStats 指标统计信息
type MetricsStats struct {
	// 命令统计
	CommandTotal   int64   `json:"command_total"`
	CommandSuccess int64   `json:"command_success"`
	CommandFailure int64   `json:"command_failure"`
	SuccessRate    float64 `json:"success_rate"`

	// 延迟统计（微秒）
	AvgLatencyMicros float64 `json:"avg_latency_micros"`
	MaxLatencyMicros int64   `json:"max_latency_micros"`
	MinLatencyMicros int64   `json:"min_latency_micros"`

	// 健康检查
	HealthCheckTotal    int64 `json:"health_check_total"`
	HealthCheckFailures int64 `json:"health_check_failures"`
	HealthCheckLatency  int64 `json:"health_check_latency"`

	// 连接池统计
	PoolHits     int64 `json:"pool_hits"`
	PoolMisses   int64 `json:"pool_misses"`
	PoolTimeouts int64 `json:"pool_timeouts"`

	// Pipeline统计
	PipelineTotal int64 `json:"pipeline_total"`
	PipelineBatch int64 `json:"pipeline_batch"`
}

// Reset 重置所有指标
func (m *Metrics) Reset() {
	atomic.StoreInt64(&m.commandTotal, 0)
	atomic.StoreInt64(&m.commandSuccess, 0)
	atomic.StoreInt64(&m.commandFailure, 0)
	atomic.StoreInt64(&m.totalLatency, 0)
	atomic.StoreInt64(&m.maxLatency, 0)
	atomic.StoreInt64(&m.minLatency, int64(^uint64(0)>>1))
	atomic.StoreInt64(&m.healthCheckTotal, 0)
	atomic.StoreInt64(&m.healthCheckFailures, 0)
	atomic.StoreInt64(&m.healthCheckLatency, 0)
	atomic.StoreInt64(&m.poolHits, 0)
	atomic.StoreInt64(&m.poolMisses, 0)
	atomic.StoreInt64(&m.poolTimeouts, 0)
	atomic.StoreInt64(&m.pipelineTotal, 0)
	atomic.StoreInt64(&m.pipelineBatch, 0)
}

// MetricsHook Redis客户端钩子，用于收集指标
type MetricsHook struct {
	metrics *Metrics
}

// DialHook 连接钩子
func (h *MetricsHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		start := time.Now()
		conn, err := next(ctx, network, addr)

		if err != nil {
			h.metrics.IncPoolTimeouts()
		} else {
			h.metrics.IncPoolHits()
		}

		h.metrics.RecordLatency(time.Since(start))
		return conn, err
	}
}

// ProcessHook 处理钩子
func (h *MetricsHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		start := time.Now()

		h.metrics.IncCommandTotal()

		err := next(ctx, cmd)

		if err != nil {
			h.metrics.IncCommandFailure()
		} else {
			h.metrics.IncCommandSuccess()
		}

		h.metrics.RecordLatency(time.Since(start))

		return err
	}
}

// ProcessPipelineHook Pipeline处理钩子
func (h *MetricsHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		start := time.Now()

		h.metrics.IncPipelineTotal()
		h.metrics.AddPipelineBatch(int64(len(cmds)))

		err := next(ctx, cmds)

		// 统计每个命令的成功/失败
		for _, cmd := range cmds {
			h.metrics.IncCommandTotal()
			if cmd.Err() != nil {
				h.metrics.IncCommandFailure()
			} else {
				h.metrics.IncCommandSuccess()
			}
		}

		h.metrics.RecordLatency(time.Since(start))

		return err
	}
}
