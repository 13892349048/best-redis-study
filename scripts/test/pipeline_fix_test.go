package main

import (
	"context"
	"fmt"
	"log"

	"what_redis_can_do/internal/redisx"
)

func main() {
	fmt.Println("=== 测试 Pipeline 错误修复 ===")

	// 创建客户端
	config := redisx.DefaultConfig()
	config.Addr = "localhost:6379"
	config.EnableMetrics = true

	client, err := redisx.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// 健康检查
	if err := client.HealthCheck(ctx); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}
	fmt.Println("✓ Redis 连接正常")

	// 测试 SET 操作
	fmt.Println("\n--- 测试 SET 操作 ---")
	setResult, err := client.BenchmarkPipeline(ctx, "SET", 100, 10)
	if err != nil {
		log.Printf("SET benchmark failed: %v", err)
	} else {
		fmt.Printf("SET - QPS: %.0f, 平均延迟: %.2fms\n",
			setResult.OpsPerSecond, setResult.AvgLatencyMS)
	}

	// 测试 GET 操作
	fmt.Println("\n--- 测试 GET 操作 ---")
	getResult, err := client.BenchmarkPipeline(ctx, "GET", 100, 10)
	if err != nil {
		log.Printf("GET benchmark failed: %v", err)
	} else {
		fmt.Printf("GET - QPS: %.0f, 平均延迟: %.2fms\n",
			getResult.OpsPerSecond, getResult.AvgLatencyMS)
	}

	// 测试 INCR 操作
	fmt.Println("\n--- 测试 INCR 操作 ---")
	incrResult, err := client.BenchmarkPipeline(ctx, "INCR", 100, 10)
	if err != nil {
		log.Printf("INCR benchmark failed: %v", err)
	} else {
		fmt.Printf("INCR - QPS: %.0f, 平均延迟: %.2fms\n",
			incrResult.OpsPerSecond, incrResult.AvgLatencyMS)
	}

	// 显示最终指标
	fmt.Println("\n--- 最终指标统计 ---")
	if metrics := client.GetMetrics(); metrics != nil {
		stats := metrics.GetStats()
		fmt.Printf("总命令数: %d\n", stats.CommandTotal)
		fmt.Printf("成功命令数: %d\n", stats.CommandSuccess)
		fmt.Printf("失败命令数: %d\n", stats.CommandFailure)
		fmt.Printf("成功率: %.2f%%\n", stats.SuccessRate)
	}

	fmt.Println("\n=== 测试完成 ===")
}
