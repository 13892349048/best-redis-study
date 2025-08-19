package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"what_redis_can_do/internal/redisx"
)

// BenchmarkComparison 基准测试对比结果
type BenchmarkComparison struct {
	Operation        string                  `json:"operation"`
	SingleMode       *redisx.BenchmarkResult `json:"single_mode"`
	PipelineMode     *redisx.BenchmarkResult `json:"pipeline_mode"`
	Improvement      float64                 `json:"improvement"`
	ThroughputGain   float64                 `json:"throughput_gain"`
	LatencyReduction float64                 `json:"latency_reduction"`
}

func main() {
	fmt.Println("=== Day 4: Redis Pipeline 性能基准测试 ===")
	fmt.Println()

	// 创建客户端
	config := redisx.DefaultConfig()
	config.Addr = "localhost:6379"
	config.PoolSize = 50
	config.MinIdleConns = 20
	config.EnableMetrics = true

	client, err := redisx.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	ctx := context.Background()

	// 健康检查
	if err := client.HealthCheck(ctx); err != nil {
		log.Fatalf("Health check failed: %v", err)
	}
	fmt.Println("✓ Redis连接正常")
	fmt.Println()

	// 测试参数
	operations := []string{"SET", "GET", "INCR"}
	totalOps := 10000
	pipelineBatchSize := 100

	var comparisons []BenchmarkComparison

	for _, operation := range operations {
		fmt.Printf("=== 测试操作: %s ===\n", operation)

		// 准备测试数据（为GET操作预先设置数据）
		if operation == "GET" {
			fmt.Println("准备GET测试数据...")
			prepareGetData(ctx, client, totalOps)
		}

		// 单发模式测试
		fmt.Println("执行单发模式测试...")
		singleResult, err := client.BenchmarkPipeline(ctx, operation, totalOps, 1)
		if err != nil {
			log.Printf("Single mode benchmark failed: %v", err)
			continue
		}

		// Pipeline模式测试
		fmt.Println("执行Pipeline模式测试...")
		pipelineResult, err := client.BenchmarkPipeline(ctx, operation, totalOps, pipelineBatchSize)
		if err != nil {
			log.Printf("Pipeline mode benchmark failed: %v", err)
			continue
		}

		// 计算性能提升
		improvement := pipelineResult.OpsPerSecond / singleResult.OpsPerSecond
		throughputGain := (pipelineResult.OpsPerSecond - singleResult.OpsPerSecond) / singleResult.OpsPerSecond * 100
		latencyReduction := (singleResult.AvgLatencyMS - pipelineResult.AvgLatencyMS) / singleResult.AvgLatencyMS * 100

		comparison := BenchmarkComparison{
			Operation:        operation,
			SingleMode:       singleResult,
			PipelineMode:     pipelineResult,
			Improvement:      improvement,
			ThroughputGain:   throughputGain,
			LatencyReduction: latencyReduction,
		}
		comparisons = append(comparisons, comparison)

		// 打印结果
		printComparison(comparison)
		fmt.Println()
	}

	// 生成汇总报告
	fmt.Println("=== 性能测试汇总报告 ===")
	generateSummaryReport(comparisons)

	// 导出详细结果到JSON文件
	exportResults(comparisons)

	fmt.Println("\n=== 基准测试完成 ===")
}

// prepareGetData 为GET操作准备测试数据
func prepareGetData(ctx context.Context, client *redisx.Client, count int) {
	pipeline := client.NewPipeline().SetBatchSize(500)

	for i := 0; i < count; i++ {
		key := fmt.Sprintf("bench:get:%d", i)
		value := fmt.Sprintf("value_%d", i)
		pipeline.Set(ctx, key, value, time.Hour)
	}

	_, err := pipeline.Exec(ctx)
	if err != nil {
		log.Printf("Failed to prepare GET data: %v", err)
	}
}

// printComparison 打印单个操作的对比结果
func printComparison(comp BenchmarkComparison) {
	fmt.Printf("操作类型: %s\n", comp.Operation)
	fmt.Println(strings.Repeat("-", 60))

	fmt.Printf("%-20s %-15s %-15s\n", "指标", "单发模式", "Pipeline模式")
	fmt.Println(strings.Repeat("-", 60))

	fmt.Printf("%-20s %-15.0f %-15.0f\n", "QPS", comp.SingleMode.OpsPerSecond, comp.PipelineMode.OpsPerSecond)
	fmt.Printf("%-20s %-15.2f %-15.2f\n", "平均延迟(ms)", comp.SingleMode.AvgLatencyMS, comp.PipelineMode.AvgLatencyMS)
	fmt.Printf("%-20s %-15.0f %-15.0f\n", "总耗时(ms)", float64(comp.SingleMode.Duration.Nanoseconds())/1e6, float64(comp.PipelineMode.Duration.Nanoseconds())/1e6)
	fmt.Printf("%-20s %-15d %-15d\n", "总操作数", comp.SingleMode.TotalOps, comp.PipelineMode.TotalOps)
	fmt.Printf("%-20s %-15d %-15d\n", "批次数", comp.SingleMode.PipelineCount, comp.PipelineMode.PipelineCount)

	fmt.Println(strings.Repeat("-", 60))
	fmt.Printf("性能提升: %.1fx\n", comp.Improvement)
	fmt.Printf("吞吐量增益: +%.1f%%\n", comp.ThroughputGain)
	fmt.Printf("延迟降低: %.1f%%\n", comp.LatencyReduction)
}

// generateSummaryReport 生成汇总报告
func generateSummaryReport(comparisons []BenchmarkComparison) {
	fmt.Printf("%-10s %-15s %-15s %-15s %-15s\n", "操作", "单发QPS", "Pipeline QPS", "性能提升", "吞吐量增益")
	fmt.Println(strings.Repeat("-", 80))

	totalImprovement := 0.0
	totalThroughputGain := 0.0

	for _, comp := range comparisons {
		fmt.Printf("%-10s %-15.0f %-15.0f %-15.1fx %-15.1f%%\n",
			comp.Operation,
			comp.SingleMode.OpsPerSecond,
			comp.PipelineMode.OpsPerSecond,
			comp.Improvement,
			comp.ThroughputGain)

		totalImprovement += comp.Improvement
		totalThroughputGain += comp.ThroughputGain
	}

	fmt.Println(strings.Repeat("-", 80))
	avgImprovement := totalImprovement / float64(len(comparisons))
	avgThroughputGain := totalThroughputGain / float64(len(comparisons))

	fmt.Printf("平均性能提升: %.1fx\n", avgImprovement)
	fmt.Printf("平均吞吐量增益: %.1f%%\n", avgThroughputGain)

	// 分析结论
	fmt.Println("\n--- 性能分析结论 ---")
	if avgImprovement > 10 {
		fmt.Println("✓ Pipeline显著提升了Redis操作性能")
	} else if avgImprovement > 5 {
		fmt.Println("✓ Pipeline明显提升了Redis操作性能")
	} else {
		fmt.Println("! Pipeline性能提升有限，可能需要调整批量大小")
	}

	fmt.Printf("✓ 建议在高并发场景下使用Pipeline，平均可提升 %.1fx 性能\n", avgImprovement)
	fmt.Printf("✓ 网络RTT减少是性能提升的主要原因\n")
}

// exportResults 导出结果到JSON文件
func exportResults(comparisons []BenchmarkComparison) {
	// 创建输出目录
	os.MkdirAll("benchmark_results", 0755)

	// 生成文件名（包含时间戳）
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("benchmark_results/pipeline_benchmark_%s.json", timestamp)

	// 准备输出数据
	output := struct {
		Timestamp   string                `json:"timestamp"`
		Description string                `json:"description"`
		Comparisons []BenchmarkComparison `json:"comparisons"`
	}{
		Timestamp:   time.Now().Format(time.RFC3339),
		Description: "Redis Pipeline vs Single Command Performance Benchmark",
		Comparisons: comparisons,
	}

	// 序列化为JSON
	jsonData, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal results: %v", err)
		return
	}

	// 写入文件
	err = os.WriteFile(filename, jsonData, 0644)
	if err != nil {
		log.Printf("Failed to write results file: %v", err)
		return
	}

	fmt.Printf("✓ 详细结果已导出到: %s\n", filename)
}

// runLatencyDistributionTest 运行延迟分布测试
func runLatencyDistributionTest(ctx context.Context, client *redisx.Client) {
	fmt.Println("=== 延迟分布测试 ===")

	// 测试不同批量大小下的延迟分布
	batchSizes := []int{1, 10, 50, 100, 200, 500}
	operation := "SET"
	totalOps := 1000

	fmt.Printf("%-10s %-15s %-15s %-15s\n", "批量大小", "平均延迟(ms)", "QPS", "耗时(ms)")
	fmt.Println(strings.Repeat("-", 60))

	for _, batchSize := range batchSizes {
		result, err := client.BenchmarkPipeline(ctx, operation, totalOps, batchSize)
		if err != nil {
			log.Printf("Benchmark failed for batch size %d: %v", batchSize, err)
			continue
		}

		fmt.Printf("%-10d %-15.2f %-15.0f %-15.0f\n",
			batchSize,
			result.AvgLatencyMS,
			result.OpsPerSecond,
			float64(result.Duration.Nanoseconds())/1e6)
	}
}

// analyzeOptimalBatchSize 分析最优批量大小
func analyzeOptimalBatchSize(ctx context.Context, client *redisx.Client) {
	fmt.Println("\n=== 最优批量大小分析 ===")

	batchSizes := []int{1, 5, 10, 20, 50, 100, 200, 500, 1000}
	operation := "SET"
	totalOps := 2000

	bestQPS := 0.0
	bestBatchSize := 0

	for _, batchSize := range batchSizes {
		result, err := client.BenchmarkPipeline(ctx, operation, totalOps, batchSize)
		if err != nil {
			continue
		}

		if result.OpsPerSecond > bestQPS {
			bestQPS = result.OpsPerSecond
			bestBatchSize = batchSize
		}
	}

	fmt.Printf("✓ 最优批量大小: %d (QPS: %.0f)\n", bestBatchSize, bestQPS)
	fmt.Printf("✓ 建议: 根据实际业务场景和网络条件，在 %d-%d 之间选择批量大小\n",
		bestBatchSize/2, bestBatchSize*2)
}
