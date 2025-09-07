package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/consistency"
	"what_redis_can_do/internal/transaction"

	"github.com/redis/go-redis/v9"
)

// BenchmarkResult 压测结果
type BenchmarkResult struct {
	TestName        string        `json:"test_name"`
	Duration        time.Duration `json:"duration"`
	TotalOperations int64         `json:"total_operations"`
	SuccessfulOps   int64         `json:"successful_ops"`
	FailedOps       int64         `json:"failed_ops"`
	QPS             float64       `json:"qps"`
	AvgLatency      time.Duration `json:"avg_latency"`
	P50Latency      time.Duration `json:"p50_latency"`
	P95Latency      time.Duration `json:"p95_latency"`
	P99Latency      time.Duration `json:"p99_latency"`
	MaxLatency      time.Duration `json:"max_latency"`
	MinLatency      time.Duration `json:"min_latency"`
	ConflictCount   int64         `json:"conflict_count"`
	RetryCount      int64         `json:"retry_count"`
	Timestamp       time.Time     `json:"timestamp"`
}

// LatencyCollector 延迟收集器
type LatencyCollector struct {
	latencies []time.Duration
	mutex     sync.RWMutex
}

func (lc *LatencyCollector) Add(latency time.Duration) {
	lc.mutex.Lock()
	defer lc.mutex.Unlock()
	lc.latencies = append(lc.latencies, latency)
}

func (lc *LatencyCollector) GetStats() (time.Duration, time.Duration, time.Duration, time.Duration, time.Duration, time.Duration) {
	lc.mutex.RLock()
	defer lc.mutex.RUnlock()

	if len(lc.latencies) == 0 {
		return 0, 0, 0, 0, 0, 0
	}

	// 排序
	latencies := make([]time.Duration, len(lc.latencies))
	copy(latencies, lc.latencies)

	for i := 0; i < len(latencies)-1; i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[i] > latencies[j] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	// 计算统计值
	var sum time.Duration
	for _, lat := range latencies {
		sum += lat
	}

	avg := sum / time.Duration(len(latencies))
	min := latencies[0]
	max := latencies[len(latencies)-1]
	p50 := latencies[len(latencies)*50/100]
	p95 := latencies[len(latencies)*95/100]
	p99 := latencies[len(latencies)*99/100]

	return avg, min, max, p50, p95, p99
}

func main() {
	// 创建Redis客户端
	client := redis.NewClient(&redis.Options{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     100,
		MinIdleConns: 10,
	})
	defer client.Close()

	// 测试连接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	fmt.Println("=== Day 9: 一致性与事务性能压测 ===\n")

	// 清理测试数据
	cleanupBenchmarkData(ctx, client)

	var results []BenchmarkResult

	// 1. 基础事务性能测试
	fmt.Println("1. 基础事务性能测试")
	result1 := benchmarkBasicTransaction(ctx, client)
	results = append(results, result1)

	// 2. 乐观锁并发性能测试
	fmt.Println("\n2. 乐观锁并发性能测试")
	result2 := benchmarkOptimisticLock(ctx, client)
	results = append(results, result2)

	// 3. Lua脚本性能测试
	fmt.Println("\n3. Lua脚本性能测试")
	result3 := benchmarkLuaScripts(ctx, client)
	results = append(results, result3)

	// 4. 库存扣减高并发测试
	fmt.Println("\n4. 库存扣减高并发测试")
	result4 := benchmarkInventoryDeduction(ctx, client)
	results = append(results, result4)

	// 5. 批量事务性能测试
	fmt.Println("\n5. 批量事务性能测试")
	result5 := benchmarkBatchTransaction(ctx, client)
	results = append(results, result5)

	// 保存结果
	saveResults(results)

	// 打印汇总
	printSummary(results)

	fmt.Println("\n=== 压测完成 ===")
}

// benchmarkBasicTransaction 基础事务性能测试
func benchmarkBasicTransaction(ctx context.Context, client *redis.Client) BenchmarkResult {
	txMgr := transaction.NewTransactionManager(client)
	collector := &LatencyCollector{}

	var successCount, failCount int64
	concurrency := 200
	operationsPerWorker := 500

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				opStart := time.Now()

				operation := func(ctx context.Context, tx transaction.TransactionContext) error {
					key1 := fmt.Sprintf("tx_test:%d:%d:1", workerID, j)
					key2 := fmt.Sprintf("tx_test:%d:%d:2", workerID, j)

					tx.Set(key1, "value1", time.Minute)
					tx.Set(key2, "value2", time.Minute)
					tx.Incr(fmt.Sprintf("tx_counter:%d", workerID))

					return nil
				}

				_, err := txMgr.ExecuteTransaction(ctx, operation)

				latency := time.Since(opStart)
				collector.Add(latency)

				if err != nil {
					atomic.AddInt64(&failCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := successCount + failCount
	avg, min, max, p50, p95, p99 := collector.GetStats()

	result := BenchmarkResult{
		TestName:        "BasicTransaction",
		Duration:        duration,
		TotalOperations: totalOps,
		SuccessfulOps:   successCount,
		FailedOps:       failCount,
		QPS:             float64(totalOps) / duration.Seconds(),
		AvgLatency:      avg,
		P50Latency:      p50,
		P95Latency:      p95,
		P99Latency:      p99,
		MaxLatency:      max,
		MinLatency:      min,
		Timestamp:       time.Now(),
	}

	printResult(result)
	return result
}

// benchmarkOptimisticLock 乐观锁并发性能测试
func benchmarkOptimisticLock(ctx context.Context, client *redis.Client) BenchmarkResult {
	consistencyMgr := consistency.NewConsistencyManager(client)
	collector := &LatencyCollector{}

	var successCount, failCount, conflictCount, retryCount int64
	concurrency := 50
	operationsPerWorker := 200

	// 初始化共享计数器
	sharedKeys := 100
	for i := 0; i < sharedKeys; i++ {
		client.Set(ctx, fmt.Sprintf("opt_counter:%d", i), "0", 0)
	}

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				opStart := time.Now()
				keyID := rand.Intn(sharedKeys)
				key := fmt.Sprintf("opt_counter:%d", keyID)

				operation := func(ctx context.Context, tx consistency.TxContext) error {
					current, err := tx.Get(key)
					if err != nil && err != redis.Nil {
						return err
					}
					if err == redis.Nil {
						current = "0"
					}

					currentInt, _ := strconv.Atoi(current)
					newValue := currentInt + 1

					// 模拟一些处理时间
					time.Sleep(time.Microsecond * 100)

					return tx.Set(key, strconv.Itoa(newValue), 0)
				}

				err := consistencyMgr.ExecuteWithWatch(ctx, []string{key}, operation,
					consistency.WithRetryPolicy(consistency.RetryPolicy{
						MaxRetries:    10,
						InitialDelay:  time.Microsecond * 100,
						MaxDelay:      time.Millisecond * 10,
						BackoffFactor: 2.0,
					}),
					consistency.WithRetryCallback(func(attempt int, err error) {
						atomic.AddInt64(&retryCount, 1)
						if attempt == 1 {
							atomic.AddInt64(&conflictCount, 1)
						}
					}),
				)

				latency := time.Since(opStart)
				collector.Add(latency)

				if err != nil {
					atomic.AddInt64(&failCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := successCount + failCount
	avg, min, max, p50, p95, p99 := collector.GetStats()

	result := BenchmarkResult{
		TestName:        "OptimisticLock",
		Duration:        duration,
		TotalOperations: totalOps,
		SuccessfulOps:   successCount,
		FailedOps:       failCount,
		QPS:             float64(totalOps) / duration.Seconds(),
		AvgLatency:      avg,
		P50Latency:      p50,
		P95Latency:      p95,
		P99Latency:      p99,
		MaxLatency:      max,
		MinLatency:      min,
		ConflictCount:   conflictCount,
		RetryCount:      retryCount,
		Timestamp:       time.Now(),
	}

	printResult(result)
	return result
}

// benchmarkLuaScripts Lua脚本性能测试
func benchmarkLuaScripts(ctx context.Context, client *redis.Client) BenchmarkResult {
	scriptMgr := transaction.NewLuaScriptManager(client)
	collector := &LatencyCollector{}

	// 加载脚本
	scriptMgr.LoadAllScripts(ctx)

	var successCount, failCount int64
	concurrency := 200
	operationsPerWorker := 500

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				opStart := time.Now()

				// 随机选择脚本操作
				switch j % 3 {
				case 0:
					// CAS操作
					key := fmt.Sprintf("cas_key:%d", workerID)
					client.Set(ctx, key, "old", 0)
					_, err := scriptMgr.CompareAndSwap(ctx, key, "old", "new", 60)
					if err != nil {
						atomic.AddInt64(&failCount, 1)
					} else {
						atomic.AddInt64(&successCount, 1)
					}

				case 1:
					// 限制递增
					key := fmt.Sprintf("limit_key:%d", workerID)
					_, err := scriptMgr.IncrWithLimit(ctx, key, 100, 1, 60)
					if err != nil {
						atomic.AddInt64(&failCount, 1)
					} else {
						atomic.AddInt64(&successCount, 1)
					}

				case 2:
					// 滑动窗口计数
					key := fmt.Sprintf("window_key:%d", workerID)
					currentTime := time.Now().UnixMilli()
					_, _, err := scriptMgr.SlidingWindowCounter(ctx, key, 1000, 50, currentTime, 1)
					if err != nil {
						atomic.AddInt64(&failCount, 1)
					} else {
						atomic.AddInt64(&successCount, 1)
					}
				}

				latency := time.Since(opStart)
				collector.Add(latency)
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := successCount + failCount
	avg, min, max, p50, p95, p99 := collector.GetStats()

	result := BenchmarkResult{
		TestName:        "LuaScripts",
		Duration:        duration,
		TotalOperations: totalOps,
		SuccessfulOps:   successCount,
		FailedOps:       failCount,
		QPS:             float64(totalOps) / duration.Seconds(),
		AvgLatency:      avg,
		P50Latency:      p50,
		P95Latency:      p95,
		P99Latency:      p99,
		MaxLatency:      max,
		MinLatency:      min,
		Timestamp:       time.Now(),
	}

	printResult(result)
	return result
}

// benchmarkInventoryDeduction 库存扣减高并发测试
func benchmarkInventoryDeduction(ctx context.Context, client *redis.Client) BenchmarkResult {
	consistencyMgr := consistency.NewConsistencyManager(client)
	inventoryMgr := consistency.NewInventoryManager(client, consistencyMgr, consistency.InventoryConfig{})
	collector := &LatencyCollector{}

	var successCount, failCount, retryCount int64
	concurrency := 100
	operationsPerWorker := 50

	// 初始化库存
	itemCount := 10
	for i := 0; i < itemCount; i++ {
		client.Set(ctx, fmt.Sprintf("stock:item%03d", i), "100", 0)
	}

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				opStart := time.Now()
				itemID := fmt.Sprintf("item%03d", rand.Intn(itemCount))
				amount := int64(rand.Intn(10) + 1)

				_, err := inventoryMgr.DeductStock(ctx, itemID, amount)

				latency := time.Since(opStart)
				collector.Add(latency)

				if err != nil {
					atomic.AddInt64(&failCount, 1)
					if err == consistency.ErrInsufficientStock {
						// 库存不足是正常的业务逻辑，不算真正的失败
					}
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := successCount + failCount
	avg, min, max, p50, p95, p99 := collector.GetStats()

	result := BenchmarkResult{
		TestName:        "InventoryDeduction",
		Duration:        duration,
		TotalOperations: totalOps,
		SuccessfulOps:   successCount,
		FailedOps:       failCount,
		QPS:             float64(totalOps) / duration.Seconds(),
		AvgLatency:      avg,
		P50Latency:      p50,
		P95Latency:      p95,
		P99Latency:      p99,
		MaxLatency:      max,
		MinLatency:      min,
		RetryCount:      retryCount,
		Timestamp:       time.Now(),
	}

	printResult(result)
	return result
}

// benchmarkBatchTransaction 批量事务性能测试
func benchmarkBatchTransaction(ctx context.Context, client *redis.Client) BenchmarkResult {
	txMgr := transaction.NewTransactionManager(client)
	collector := &LatencyCollector{}

	var successCount, failCount int64
	concurrency := 20
	operationsPerWorker := 100
	batchSize := 10

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < operationsPerWorker; j++ {
				opStart := time.Now()

				// 构建批量操作
				operations := make([]transaction.BatchOperation, batchSize)
				for k := 0; k < batchSize; k++ {
					operations[k] = transaction.BatchOperation{
						Key:       fmt.Sprintf("batch:%d:%d:%d", workerID, j, k),
						Operation: "SET",
						Args:      []interface{}{fmt.Sprintf("value_%d", k)},
					}
				}

				_, err := txMgr.BatchExecute(ctx, operations)

				latency := time.Since(opStart)
				collector.Add(latency)

				if err != nil {
					atomic.AddInt64(&failCount, 1)
				} else {
					atomic.AddInt64(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(startTime)

	totalOps := successCount + failCount
	avg, min, max, p50, p95, p99 := collector.GetStats()

	result := BenchmarkResult{
		TestName:        "BatchTransaction",
		Duration:        duration,
		TotalOperations: totalOps,
		SuccessfulOps:   successCount,
		FailedOps:       failCount,
		QPS:             float64(totalOps) / duration.Seconds(),
		AvgLatency:      avg,
		P50Latency:      p50,
		P95Latency:      p95,
		P99Latency:      p99,
		MaxLatency:      max,
		MinLatency:      min,
		Timestamp:       time.Now(),
	}

	printResult(result)
	return result
}

// printResult 打印单个测试结果
func printResult(result BenchmarkResult) {
	fmt.Printf("  测试: %s\n", result.TestName)
	fmt.Printf("  总操作数: %d, 成功: %d, 失败: %d\n", result.TotalOperations, result.SuccessfulOps, result.FailedOps)
	fmt.Printf("  QPS: %.2f, 耗时: %v\n", result.QPS, result.Duration)
	fmt.Printf("  延迟统计: 平均=%v, P50=%v, P95=%v, P99=%v, 最大=%v\n",
		result.AvgLatency, result.P50Latency, result.P95Latency, result.P99Latency, result.MaxLatency)

	if result.ConflictCount > 0 || result.RetryCount > 0 {
		fmt.Printf("  冲突统计: 冲突次数=%d, 重试次数=%d\n", result.ConflictCount, result.RetryCount)
	}
}

// printSummary 打印汇总结果
func printSummary(results []BenchmarkResult) {
	fmt.Println("\n=== 压测结果汇总 ===")

	totalOps := int64(0)
	totalDuration := time.Duration(0)

	for _, result := range results {
		totalOps += result.TotalOperations
		totalDuration += result.Duration
	}

	fmt.Printf("总操作数: %d\n", totalOps)
	fmt.Printf("总耗时: %v\n", totalDuration)
	fmt.Printf("平均QPS: %.2f\n", float64(totalOps)/totalDuration.Seconds())

	fmt.Println("\n各测试性能对比:")
	fmt.Println("测试名称\t\tQPS\t\tP95延迟\t\tP99延迟")
	fmt.Println("------------------------------------------------------------")
	for _, result := range results {
		fmt.Printf("%-20s\t%.2f\t\t%v\t\t%v\n",
			result.TestName, result.QPS, result.P95Latency, result.P99Latency)
	}
}

// saveResults 保存结果到文件
func saveResults(results []BenchmarkResult) {
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("benchmark_results/day9_consistency_benchmark_%s.json", timestamp)

	// 确保目录存在
	os.MkdirAll("benchmark_results", 0755)

	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Failed to create result file: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(results); err != nil {
		log.Printf("Failed to encode results: %v", err)
		return
	}

	fmt.Printf("\n结果已保存到: %s\n", filename)
}

// cleanupBenchmarkData 清理压测数据
func cleanupBenchmarkData(ctx context.Context, client redis.Cmdable) {
	// 使用SCAN清理测试键
	patterns := []string{
		"tx_test:*", "tx_counter:*", "opt_counter:*", "cas_key:*",
		"limit_key:*", "window_key:*", "stock:item*", "inventory_cache:*",
		"batch:*", "reservation:*",
	}

	for _, pattern := range patterns {
		iter := client.Scan(ctx, 0, pattern, 1000).Iterator()
		for iter.Next(ctx) {
			client.Del(ctx, iter.Val())
		}
	}
}
