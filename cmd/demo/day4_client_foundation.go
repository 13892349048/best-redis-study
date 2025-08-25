package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"what_redis_can_do/internal/redisx"
)

func main4() {
	fmt.Println("=== Day 4: Go 客户端基建（go-redis/v9）===")
	fmt.Println()

	// 1. 创建客户端配置
	fmt.Println("1. 创建 Redis 客户端配置")
	config := redisx.DefaultConfig()
	config.PoolSize = 20
	config.ReadTimeout = 2 * time.Second
	config.WriteTimeout = 2 * time.Second

	fmt.Printf("配置信息: %+v\n", config)
	fmt.Println()

	// 2. 创建客户端
	fmt.Println("2. 创建 Redis 客户端")
	client, err := redisx.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// 3. 健康检查
	fmt.Println("3. 执行健康检查")
	if err := client.HealthCheck(ctx); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}
	fmt.Println("✓ 健康检查通过")
	fmt.Println()

	// 4. 连接池统计
	fmt.Println("4. 连接池统计信息")
	stats := client.GetStats()
	fmt.Printf("总连接数: %d\n", stats.TotalConns)
	fmt.Printf("空闲连接数: %d\n", stats.IdleConns)
	fmt.Printf("过期连接数: %d\n", stats.StaleConns)
	fmt.Printf("命中数: %d\n", stats.Hits)
	fmt.Printf("超时数: %d\n", stats.Timeouts)
	fmt.Println()

	// 5. 基本操作演示
	fmt.Println("5. 基本操作演示")
	demonstrateBasicOperations(ctx, client)
	fmt.Println()

	// 6. Pipeline 演示
	fmt.Println("6. Pipeline 批量操作演示")
	demonstratePipeline(ctx, client)
	fmt.Println()

	// 7. 性能基准测试
	fmt.Println("7. 性能基准测试")
	runBenchmarks(ctx, client)
	fmt.Println()

	// 8. 指标统计
	fmt.Println("8. 客户端指标统计")
	showMetrics(client)
	fmt.Println()

	fmt.Println("=== Day 4 演示完成 ===")
}

// demonstrateBasicOperations 演示基本操作
func demonstrateBasicOperations(ctx context.Context, client *redisx.Client) {
	// SET/GET
	err := client.Set(ctx, "user:1:name", "Alice", time.Hour).Err()
	if err != nil {
		log.Printf("SET failed: %v", err)
		return
	}

	name, err := client.Get(ctx, "user:1:name").Result()
	if err != nil {
		log.Printf("GET failed: %v", err)
		return
	}
	fmt.Printf("✓ SET/GET: user:1:name = %s\n", name)

	// HASH操作
	err = client.HSet(ctx, "user:1:profile", "name", "Alice", "age", 25, "city", "Beijing").Err()
	if err != nil {
		log.Printf("HSET failed: %v", err)
		return
	}

	profile, err := client.HGetAll(ctx, "user:1:profile").Result()
	if err != nil {
		log.Printf("HGETALL failed: %v", err)
		return
	}
	fmt.Printf("✓ HASH操作: user:1:profile = %v\n", profile)

	// 原子计数
	count, err := client.Incr(ctx, "counter:views").Result()
	if err != nil {
		log.Printf("INCR failed: %v", err)
		return
	}
	fmt.Printf("✓ 原子计数: counter:views = %d\n", count)
}

// demonstratePipeline 演示Pipeline操作
func demonstratePipeline(ctx context.Context, client *redisx.Client) {
	// 创建Pipeline
	pipeline := client.NewPipeline()

	// 批量添加命令
	keys := make([]string, 10)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("pipeline:test:%d", i)
		value := fmt.Sprintf("value_%d", i)
		keys[i] = key
		pipeline.Set(ctx, key, value, time.Hour)
	}

	// 执行Pipeline
	start := time.Now()
	cmds, err := pipeline.Exec(ctx)
	duration := time.Since(start)

	if err != nil {
		log.Printf("Pipeline execution failed: %v", err)
		return
	}

	fmt.Printf("✓ Pipeline执行: %d个命令, 耗时: %v\n", len(cmds), duration)

	// 验证结果
	pipeline2 := client.NewPipeline()
	for _, key := range keys {
		pipeline2.Get(ctx, key)
	}

	cmds2, err := pipeline2.Exec(ctx)
	if err != nil {
		log.Printf("Pipeline GET failed: %v", err)
		return
	}

	successCount := 0
	for _, cmd := range cmds2 {
		if cmd.Err() == nil {
			successCount++
		}
	}
	fmt.Printf("✓ 验证结果: %d/%d 键值对读取成功\n", successCount, len(keys))

	// 事务Pipeline演示
	fmt.Println("\n--- 事务Pipeline演示 ---")
	txPipeline := client.NewTxPipeline()

	// 在事务中设置多个值
	txPipeline.Set(ctx, "tx:key1", "value1", time.Hour)
	txPipeline.Set(ctx, "tx:key2", "value2", time.Hour)
	txPipeline.Incr(ctx, "tx:counter")

	start = time.Now()
	txCmds, err := txPipeline.Exec(ctx)
	txDuration := time.Since(start)

	if err != nil {
		log.Printf("Transaction pipeline failed: %v", err)
		return
	}

	fmt.Printf("✓ 事务Pipeline: %d个命令, 耗时: %v\n", len(txCmds), txDuration)
}

// runBenchmarks 运行性能基准测试
func runBenchmarks(ctx context.Context, client *redisx.Client) {
	fmt.Println("开始性能基准测试...")

	// 测试不同批量大小的性能
	batchSizes := []int{1, 10, 50, 100, 500}
	totalOps := 1000

	fmt.Println("\n--- SET操作性能测试 ---")
	fmt.Printf("%-10s %-15s %-15s %-15s %-15s\n", "批量大小", "总操作数", "耗时(ms)", "QPS", "平均延迟(ms)")
	fmt.Println(strings.Repeat("-", 75))

	var setResults []*redisx.BenchmarkResult
	for _, batchSize := range batchSizes {
		result, err := client.BenchmarkPipeline(ctx, "SET", totalOps, batchSize)
		if err != nil {
			log.Printf("Benchmark failed for batch size %d: %v", batchSize, err)
			continue
		}
		setResults = append(setResults, result)

		fmt.Printf("%-10d %-15d %-15.2f %-15.0f %-15.2f\n",
			result.BatchSize,
			result.TotalOps,
			float64(result.Duration.Nanoseconds())/1e6,
			result.OpsPerSecond,
			result.AvgLatencyMS)
	}

	fmt.Println("\n--- GET操作性能测试 ---")
	fmt.Printf("%-10s %-15s %-15s %-15s %-15s\n", "批量大小", "总操作数", "耗时(ms)", "QPS", "平均延迟(ms)")
	fmt.Println(strings.Repeat("-", 75))

	var getResults []*redisx.BenchmarkResult
	for _, batchSize := range batchSizes {
		result, err := client.BenchmarkPipeline(ctx, "GET", totalOps, batchSize)
		if err != nil {
			log.Printf("Benchmark failed for batch size %d: %v", batchSize, err)
			continue
		}
		getResults = append(getResults, result)

		fmt.Printf("%-10d %-15d %-15.2f %-15.0f %-15.2f\n",
			result.BatchSize,
			result.TotalOps,
			float64(result.Duration.Nanoseconds())/1e6,
			result.OpsPerSecond,
			result.AvgLatencyMS)
	}

	// 单发 vs 批量对比
	fmt.Println("\n--- 单发 vs 批量对比 ---")
	if len(setResults) >= 2 {
		single := setResults[0]                // 批量大小为1
		batch := setResults[len(setResults)-1] // 最大批量大小

		improvement := batch.OpsPerSecond / single.OpsPerSecond
		fmt.Printf("SET操作 - 单发QPS: %.0f, 批量QPS: %.0f, 性能提升: %.1fx\n",
			single.OpsPerSecond, batch.OpsPerSecond, improvement)
	}

	if len(getResults) >= 2 {
		single := getResults[0]
		batch := getResults[len(getResults)-1]

		improvement := batch.OpsPerSecond / single.OpsPerSecond
		fmt.Printf("GET操作 - 单发QPS: %.0f, 批量QPS: %.0f, 性能提升: %.1fx\n",
			single.OpsPerSecond, batch.OpsPerSecond, improvement)
	}
}

// showMetrics 显示客户端指标
func showMetrics(client *redisx.Client) {
	metrics := client.GetMetrics()
	if metrics == nil {
		fmt.Println("指标收集未启用")
		return
	}

	stats := metrics.GetStats()

	// 格式化输出指标
	fmt.Println("--- 命令统计 ---")
	fmt.Printf("总命令数: %d\n", stats.CommandTotal)
	fmt.Printf("成功命令数: %d\n", stats.CommandSuccess)
	fmt.Printf("失败命令数: %d\n", stats.CommandFailure)
	fmt.Printf("成功率: %.2f%%\n", stats.SuccessRate)

	fmt.Println("\n--- 延迟统计 ---")
	fmt.Printf("平均延迟: %.2f μs\n", stats.AvgLatencyMicros)
	fmt.Printf("最大延迟: %d μs\n", stats.MaxLatencyMicros)
	fmt.Printf("最小延迟: %d μs\n", stats.MinLatencyMicros)

	fmt.Println("\n--- 连接池统计 ---")
	fmt.Printf("连接池命中: %d\n", stats.PoolHits)
	fmt.Printf("连接池未命中: %d\n", stats.PoolMisses)
	fmt.Printf("连接超时: %d\n", stats.PoolTimeouts)

	fmt.Println("\n--- Pipeline统计 ---")
	fmt.Printf("Pipeline总数: %d\n", stats.PipelineTotal)
	fmt.Printf("Pipeline批次总数: %d\n", stats.PipelineBatch)

	// 导出JSON格式指标
	jsonData, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal metrics: %v", err)
		return
	}

	fmt.Println("\n--- JSON格式指标 ---")
	fmt.Println(string(jsonData))
}
