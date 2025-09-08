package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/lock"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

// BenchmarkResult 基准测试结果
type BenchmarkResult struct {
	TestName        string                 `json:"test_name"`
	Duration        time.Duration          `json:"duration"`
	Concurrency     int                    `json:"concurrency"`
	TotalOperations int64                  `json:"total_operations"`
	SuccessfulOps   int64                  `json:"successful_ops"`
	FailedOps       int64                  `json:"failed_ops"`
	QPS             float64                `json:"qps"`
	SuccessRate     float64                `json:"success_rate"`
	AvgLatency      time.Duration          `json:"avg_latency"`
	P95Latency      time.Duration          `json:"p95_latency"`
	P99Latency      time.Duration          `json:"p99_latency"`
	Errors          map[string]int64       `json:"errors"`
	Timestamp       time.Time              `json:"timestamp"`
	Details         map[string]interface{} `json:"details,omitempty"`
}

// LatencyTracker 延迟跟踪器
type LatencyTracker struct {
	latencies []time.Duration
	mu        sync.Mutex
}

// Add 添加延迟记录
func (lt *LatencyTracker) Add(latency time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.latencies = append(lt.latencies, latency)
}

// GetPercentiles 获取延迟百分位数
func (lt *LatencyTracker) GetPercentiles() (avg, p95, p99 time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if len(lt.latencies) == 0 {
		return 0, 0, 0
	}

	// 简单排序
	for i := 0; i < len(lt.latencies)-1; i++ {
		for j := i + 1; j < len(lt.latencies); j++ {
			if lt.latencies[i] > lt.latencies[j] {
				lt.latencies[i], lt.latencies[j] = lt.latencies[j], lt.latencies[i]
			}
		}
	}

	// 计算平均值
	var total time.Duration
	for _, l := range lt.latencies {
		total += l
	}
	avg = total / time.Duration(len(lt.latencies))

	// 计算百分位数
	p95Index := int(float64(len(lt.latencies)) * 0.95)
	p99Index := int(float64(len(lt.latencies)) * 0.99)

	if p95Index >= len(lt.latencies) {
		p95Index = len(lt.latencies) - 1
	}
	if p99Index >= len(lt.latencies) {
		p99Index = len(lt.latencies) - 1
	}

	p95 = lt.latencies[p95Index]
	p99 = lt.latencies[p99Index]

	return
}

// LockBenchmark 分布式锁基准测试
type LockBenchmark struct {
	client      redis.Cmdable
	lockManager lock.LockManager
}

// NewLockBenchmark 创建基准测试实例
func NewLockBenchmark(client redis.Cmdable) *LockBenchmark {
	return &LockBenchmark{
		client:      client,
		lockManager: lock.NewRedisLockManager(client, nil),
	}
}

// RunAllBenchmarks 运行所有基准测试
func (lb *LockBenchmark) RunAllBenchmarks() []*BenchmarkResult {
	var results []*BenchmarkResult

	fmt.Println("=== 分布式锁基准测试开始 ===")

	// 1. 基本锁操作基准测试
	fmt.Println("\n1. 基本锁操作基准测试")
	results = append(results, lb.benchmarkBasicLock())

	// 2. 高并发竞争基准测试
	fmt.Println("\n2. 高并发竞争基准测试")
	results = append(results, lb.benchmarkHighConcurrency())

	// 3. 自动续租基准测试
	fmt.Println("\n3. 自动续租基准测试")
	results = append(results, lb.benchmarkAutoRenew())

	// 4. 大量锁基准测试
	fmt.Println("\n4. 大量锁基准测试")
	results = append(results, lb.benchmarkManyLocks())

	// 5. 锁超时基准测试
	fmt.Println("\n5. 锁超时基准测试")
	results = append(results, lb.benchmarkLockTimeout())

	fmt.Println("\n=== 基准测试完成 ===")
	return results
}

// benchmarkBasicLock 基本锁操作基准测试
func (lb *LockBenchmark) benchmarkBasicLock() *BenchmarkResult {
	const duration = 30 * time.Second
	const concurrency = 10

	var totalOps int64
	var successOps int64
	var failedOps int64
	var wg sync.WaitGroup

	errors := make(map[string]int64)
	var errorsMu sync.Mutex

	latencyTracker := &LatencyTracker{}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	startTime := time.Now()

	// 启动多个协程
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			distributedLock := lb.lockManager.CreateLock()
			lockKey := fmt.Sprintf("benchmark:basic_lock_%d", workerID)
			owner := fmt.Sprintf("worker_%d", workerID)

			for {
				select {
				case <-ctx.Done():
					return
				default:
					opStart := time.Now()
					atomic.AddInt64(&totalOps, 1)

					// 获取锁
					success, token, err := distributedLock.TryLock(ctx, lockKey, owner, 1*time.Second)
					if err != nil {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
						continue
					}

					if success {
						// 模拟业务处理
						time.Sleep(10 * time.Millisecond)

						// 释放锁
						released, err := distributedLock.Unlock(ctx, lockKey, owner, token)
						if err != nil {
							atomic.AddInt64(&failedOps, 1)
							errorsMu.Lock()
							errors[err.Error()]++
							errorsMu.Unlock()
						} else if released {
							atomic.AddInt64(&successOps, 1)
							latencyTracker.Add(time.Since(opStart))
						} else {
							atomic.AddInt64(&failedOps, 1)
							errorsMu.Lock()
							errors["unlock_failed"]++
							errorsMu.Unlock()
						}
					} else {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors["lock_not_acquired"]++
						errorsMu.Unlock()
					}

					// 短暂休息
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	avgLatency, p95Latency, p99Latency := latencyTracker.GetPercentiles()

	result := &BenchmarkResult{
		TestName:        "BasicLockOperation",
		Duration:        elapsed,
		Concurrency:     concurrency,
		TotalOperations: totalOps,
		SuccessfulOps:   successOps,
		FailedOps:       failedOps,
		QPS:             float64(totalOps) / elapsed.Seconds(),
		SuccessRate:     float64(successOps) / float64(totalOps) * 100,
		AvgLatency:      avgLatency,
		P95Latency:      p95Latency,
		P99Latency:      p99Latency,
		Errors:          errors,
		Timestamp:       time.Now(),
	}

	lb.printBenchmarkResult(result)
	return result
}

// benchmarkHighConcurrency 高并发竞争基准测试
func (lb *LockBenchmark) benchmarkHighConcurrency() *BenchmarkResult {
	const duration = 30 * time.Second
	const concurrency = 100
	const lockKey = "benchmark:high_concurrency_lock"

	var totalOps int64
	var successOps int64
	var failedOps int64
	var wg sync.WaitGroup

	errors := make(map[string]int64)
	var errorsMu sync.Mutex

	latencyTracker := &LatencyTracker{}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	startTime := time.Now()

	// 启动多个协程竞争同一把锁
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			distributedLock := lb.lockManager.CreateLock()
			owner := fmt.Sprintf("worker_%d", workerID)

			for {
				select {
				case <-ctx.Done():
					return
				default:
					opStart := time.Now()
					atomic.AddInt64(&totalOps, 1)

					// 尝试获取锁
					success, token, err := distributedLock.TryLock(ctx, lockKey, owner, 50*time.Millisecond)
					if err != nil {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
						continue
					}

					if success {
						// 极短的业务处理时间
						time.Sleep(5 * time.Millisecond)

						// 释放锁
						released, err := distributedLock.Unlock(ctx, lockKey, owner, token)
						if err != nil {
							atomic.AddInt64(&failedOps, 1)
							errorsMu.Lock()
							errors[err.Error()]++
							errorsMu.Unlock()
						} else if released {
							atomic.AddInt64(&successOps, 1)
							latencyTracker.Add(time.Since(opStart))
						}
					} else {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors["lock_not_acquired"]++
						errorsMu.Unlock()
					}

					// 极短休息时间
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	avgLatency, p95Latency, p99Latency := latencyTracker.GetPercentiles()

	result := &BenchmarkResult{
		TestName:        "HighConcurrencyLock",
		Duration:        elapsed,
		Concurrency:     concurrency,
		TotalOperations: totalOps,
		SuccessfulOps:   successOps,
		FailedOps:       failedOps,
		QPS:             float64(totalOps) / elapsed.Seconds(),
		SuccessRate:     float64(successOps) / float64(totalOps) * 100,
		AvgLatency:      avgLatency,
		P95Latency:      p95Latency,
		P99Latency:      p99Latency,
		Errors:          errors,
		Timestamp:       time.Now(),
		Details: map[string]interface{}{
			"lock_key": lockKey,
		},
	}

	lb.printBenchmarkResult(result)
	return result
}

// benchmarkAutoRenew 自动续租基准测试
func (lb *LockBenchmark) benchmarkAutoRenew() *BenchmarkResult {
	const duration = 60 * time.Second
	const concurrency = 5

	var totalOps int64
	var successOps int64
	var failedOps int64
	var wg sync.WaitGroup

	errors := make(map[string]int64)
	var errorsMu sync.Mutex

	latencyTracker := &LatencyTracker{}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	startTime := time.Now()

	// 启动多个协程测试自动续租
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			autoRenewLock := lb.lockManager.CreateAutoRenewLock()
			lockKey := fmt.Sprintf("benchmark:auto_renew_lock_%d", workerID)
			owner := fmt.Sprintf("worker_%d", workerID)

			for {
				select {
				case <-ctx.Done():
					return
				default:
					opStart := time.Now()
					atomic.AddInt64(&totalOps, 1)

					// 获取锁
					fencingToken, err := autoRenewLock.Lock(ctx, lockKey, owner, 2*time.Second, 5*time.Second)
					if err != nil {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
						continue
					}

					// 启动自动续租
					stopRenew, err := autoRenewLock.StartAutoRenew(ctx, lockKey, owner, 2*time.Second, 500*time.Millisecond, fencingToken)
					if err != nil {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
						continue
					}

					// 模拟长时间业务处理（超过锁的TTL）
					time.Sleep(5 * time.Second)

					// 停止续租
					stopRenew()

					// 释放锁
					released, err := autoRenewLock.Unlock(ctx, lockKey, owner, fencingToken)
					if err != nil {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
					} else if released {
						atomic.AddInt64(&successOps, 1)
						latencyTracker.Add(time.Since(opStart))
					}

					// 休息
					time.Sleep(1 * time.Second)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	avgLatency, p95Latency, p99Latency := latencyTracker.GetPercentiles()

	result := &BenchmarkResult{
		TestName:        "AutoRenewLock",
		Duration:        elapsed,
		Concurrency:     concurrency,
		TotalOperations: totalOps,
		SuccessfulOps:   successOps,
		FailedOps:       failedOps,
		QPS:             float64(totalOps) / elapsed.Seconds(),
		SuccessRate:     float64(successOps) / float64(totalOps) * 100,
		AvgLatency:      avgLatency,
		P95Latency:      p95Latency,
		P99Latency:      p99Latency,
		Errors:          errors,
		Timestamp:       time.Now(),
	}

	lb.printBenchmarkResult(result)
	return result
}

// benchmarkManyLocks 大量锁基准测试
func (lb *LockBenchmark) benchmarkManyLocks() *BenchmarkResult {
	const duration = 30 * time.Second
	const concurrency = 50
	const lockCount = 1000

	var totalOps int64
	var successOps int64
	var failedOps int64
	var wg sync.WaitGroup

	errors := make(map[string]int64)
	var errorsMu sync.Mutex

	latencyTracker := &LatencyTracker{}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	startTime := time.Now()

	// 启动多个协程操作大量不同的锁
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			distributedLock := lb.lockManager.CreateLock()
			owner := fmt.Sprintf("worker_%d", workerID)

			lockIndex := 0
			for {
				select {
				case <-ctx.Done():
					return
				default:
					opStart := time.Now()
					atomic.AddInt64(&totalOps, 1)

					// 循环使用不同的锁
					lockKey := fmt.Sprintf("benchmark:many_locks_%d", lockIndex%lockCount)
					lockIndex++

					// 获取锁
					success, token, err := distributedLock.TryLock(ctx, lockKey, owner, 500*time.Millisecond)
					if err != nil {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
						continue
					}

					if success {
						// 短暂业务处理
						time.Sleep(5 * time.Millisecond)

						// 释放锁
						released, err := distributedLock.Unlock(ctx, lockKey, owner, token)
						if err != nil {
							atomic.AddInt64(&failedOps, 1)
							errorsMu.Lock()
							errors[err.Error()]++
							errorsMu.Unlock()
						} else if released {
							atomic.AddInt64(&successOps, 1)
							latencyTracker.Add(time.Since(opStart))
						}
					} else {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors["lock_not_acquired"]++
						errorsMu.Unlock()
					}

					// 短暂休息
					time.Sleep(2 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	avgLatency, p95Latency, p99Latency := latencyTracker.GetPercentiles()

	result := &BenchmarkResult{
		TestName:        "ManyLocksOperation",
		Duration:        elapsed,
		Concurrency:     concurrency,
		TotalOperations: totalOps,
		SuccessfulOps:   successOps,
		FailedOps:       failedOps,
		QPS:             float64(totalOps) / elapsed.Seconds(),
		SuccessRate:     float64(successOps) / float64(totalOps) * 100,
		AvgLatency:      avgLatency,
		P95Latency:      p95Latency,
		P99Latency:      p99Latency,
		Errors:          errors,
		Timestamp:       time.Now(),
		Details: map[string]interface{}{
			"lock_count": lockCount,
		},
	}

	lb.printBenchmarkResult(result)
	return result
}

// benchmarkLockTimeout 锁超时基准测试
func (lb *LockBenchmark) benchmarkLockTimeout() *BenchmarkResult {
	const duration = 30 * time.Second
	const concurrency = 20

	var totalOps int64
	var successOps int64
	var failedOps int64
	var wg sync.WaitGroup

	errors := make(map[string]int64)
	var errorsMu sync.Mutex

	latencyTracker := &LatencyTracker{}
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	startTime := time.Now()

	// 启动多个协程测试锁超时场景
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			distributedLock := lb.lockManager.CreateLock()
			lockKey := fmt.Sprintf("benchmark:timeout_lock_%d", workerID%5) // 使用少量锁增加竞争
			owner := fmt.Sprintf("worker_%d", workerID)

			for {
				select {
				case <-ctx.Done():
					return
				default:
					opStart := time.Now()
					atomic.AddInt64(&totalOps, 1)

					// 尝试获取锁，使用较短的TTL
					success, token, err := distributedLock.TryLock(ctx, lockKey, owner, 100*time.Millisecond)
					if err != nil {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors[err.Error()]++
						errorsMu.Unlock()
						continue
					}

					if success {
						// 模拟可能超时的业务处理
						processingTime := time.Duration(50+workerID*10) * time.Millisecond
						time.Sleep(processingTime)

						// 尝试释放锁（可能已经超时）
						released, err := distributedLock.Unlock(ctx, lockKey, owner, token)
						if err != nil {
							atomic.AddInt64(&failedOps, 1)
							errorsMu.Lock()
							errors[err.Error()]++
							errorsMu.Unlock()
						} else if released {
							atomic.AddInt64(&successOps, 1)
							latencyTracker.Add(time.Since(opStart))
						} else {
							atomic.AddInt64(&failedOps, 1)
							errorsMu.Lock()
							errors["unlock_failed"]++
							errorsMu.Unlock()
						}
					} else {
						atomic.AddInt64(&failedOps, 1)
						errorsMu.Lock()
						errors["lock_not_acquired"]++
						errorsMu.Unlock()
					}

					// 短暂休息
					time.Sleep(20 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	avgLatency, p95Latency, p99Latency := latencyTracker.GetPercentiles()

	result := &BenchmarkResult{
		TestName:        "LockTimeoutTest",
		Duration:        elapsed,
		Concurrency:     concurrency,
		TotalOperations: totalOps,
		SuccessfulOps:   successOps,
		FailedOps:       failedOps,
		QPS:             float64(totalOps) / elapsed.Seconds(),
		SuccessRate:     float64(successOps) / float64(totalOps) * 100,
		AvgLatency:      avgLatency,
		P95Latency:      p95Latency,
		P99Latency:      p99Latency,
		Errors:          errors,
		Timestamp:       time.Now(),
	}

	lb.printBenchmarkResult(result)
	return result
}

// printBenchmarkResult 打印基准测试结果
func (lb *LockBenchmark) printBenchmarkResult(result *BenchmarkResult) {
	fmt.Printf("测试名称: %s\n", result.TestName)
	fmt.Printf("测试时长: %v\n", result.Duration)
	fmt.Printf("并发数: %d\n", result.Concurrency)
	fmt.Printf("总操作数: %d\n", result.TotalOperations)
	fmt.Printf("成功操作数: %d\n", result.SuccessfulOps)
	fmt.Printf("失败操作数: %d\n", result.FailedOps)
	fmt.Printf("QPS: %.2f\n", result.QPS)
	fmt.Printf("成功率: %.2f%%\n", result.SuccessRate)
	fmt.Printf("平均延迟: %v\n", result.AvgLatency)
	fmt.Printf("P95延迟: %v\n", result.P95Latency)
	fmt.Printf("P99延迟: %v\n", result.P99Latency)

	if len(result.Errors) > 0 {
		fmt.Println("错误统计:")
		for errorType, count := range result.Errors {
			fmt.Printf("  %s: %d\n", errorType, count)
		}
	}

	fmt.Println()
}

// SaveResults 保存基准测试结果到文件
func SaveResults(results []*BenchmarkResult) error {
	filename := fmt.Sprintf("day10_lock_benchmark_%s.json",
		time.Now().Format("20060102_150405"))

	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write results file: %w", err)
	}

	fmt.Printf("基准测试结果已保存到: %s\n", filename)
	return nil
}

// 运行基准测试的main函数
func runBenchmarkTests() {
	fmt.Println("=== Day 10: 分布式锁基准测试 ===")

	// 创建Redis客户端
	config := redisx.DefaultConfig()
	config.Addr = "localhost:6379"
	config.DB = 3 // 使用数据库3避免干扰

	client, err := redisx.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	// 健康检查
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		log.Fatalf("Redis health check failed: %v", err)
	}

	// 清理测试数据
	client.FlushDB(ctx)

	// 运行基准测试
	benchmark := NewLockBenchmark(client)
	results := benchmark.RunAllBenchmarks()

	// 保存结果
	if err := SaveResults(results); err != nil {
		log.Printf("Failed to save results: %v", err)
	}

	fmt.Println("=== 基准测试完成 ===")
}
