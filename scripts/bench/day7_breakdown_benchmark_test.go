package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

// BenchmarkConfig7 基准测试配置
type BenchmarkConfig7 struct {
	ConcurrentUsers int           `json:"concurrent_users"`
	TestDuration    time.Duration `json:"test_duration"`
	HotKeyCount     int           `json:"hot_key_count"`
	HotKeyRatio     float64       `json:"hot_key_ratio"`
	CacheExpireTTL  time.Duration `json:"cache_expire_ttl"`
	DBQueryDelay    time.Duration `json:"db_query_delay"`
	RedisAddress    string        `json:"redis_address"`
	OutputFile      string        `json:"output_file"`
}

// BenchmarkResult7 基准测试结果
type BenchmarkResult7 struct {
	Strategy              string              `json:"strategy"`
	Config                BenchmarkConfig7    `json:"config"`
	TotalRequests         int64               `json:"total_requests"`
	SuccessfulRequests    int64               `json:"successful_requests"`
	FailedRequests        int64               `json:"failed_requests"`
	DBQueries             int64               `json:"db_queries"`
	CacheHits             int64               `json:"cache_hits"`
	CacheHitRate          float64             `json:"cache_hit_rate"`
	AvgResponseTime       time.Duration       `json:"avg_response_time"`
	P50ResponseTime       time.Duration       `json:"p50_response_time"`
	P90ResponseTime       time.Duration       `json:"p90_response_time"`
	P95ResponseTime       time.Duration       `json:"p95_response_time"`
	P99ResponseTime       time.Duration       `json:"p99_response_time"`
	MaxResponseTime       time.Duration       `json:"max_response_time"`
	QPS                   float64             `json:"qps"`
	SingleflightStats     *SingleflightStats  `json:"singleflight_stats,omitempty"`
	LogicalExpireStats    *LogicalExpireStats `json:"logical_expire_stats,omitempty"`
	ResponseTimeHistogram map[string]int64    `json:"response_time_histogram"`
	Timestamp             time.Time           `json:"timestamp"`
}

// SingleflightStats 单飞统计
type SingleflightStats struct {
	Hits     int64 `json:"hits"`
	Misses   int64 `json:"misses"`
	Timeouts int64 `json:"timeouts"`
	Errors   int64 `json:"errors"`
}

// LogicalExpireStats 逻辑过期统计
type LogicalExpireStats struct {
	Hits         int64 `json:"hits"`
	ExpiredHits  int64 `json:"expired_hits"`
	RefreshCount int64 `json:"refresh_count"`
	ExpiredCount int64 `json:"expired_count"`
}

// MockDataService 模拟数据服务
type MockDataService struct {
	queryCount int64
	queryDelay time.Duration
	data       map[string]interface{}
	mu         sync.RWMutex
}

// NewMockDataService 创建模拟数据服务
func NewMockDataService(queryDelay time.Duration) *MockDataService {
	data := make(map[string]interface{})

	// 初始化测试数据
	for i := 1; i <= 10000; i++ {
		key := fmt.Sprintf("item:%d", i)
		data[key] = map[string]interface{}{
			"id":    i,
			"name":  fmt.Sprintf("Item %d", i),
			"value": rand.Float64() * 1000,
			"tags":  []string{fmt.Sprintf("tag%d", i%10), fmt.Sprintf("category%d", i%5)},
		}
	}

	return &MockDataService{
		queryDelay: queryDelay,
		data:       data,
	}
}

// Query 查询数据
func (mds *MockDataService) Query(ctx context.Context, key string) (interface{}, error) {
	atomic.AddInt64(&mds.queryCount, 1)

	// 模拟查询延迟
	select {
	case <-time.After(mds.queryDelay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	mds.mu.RLock()
	data, exists := mds.data[key]
	mds.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("data not found: %s", key)
	}

	return data, nil
}

// GetQueryCount 获取查询次数
func (mds *MockDataService) GetQueryCount() int64 {
	return atomic.LoadInt64(&mds.queryCount)
}

// ResetQueryCount 重置查询计数
func (mds *MockDataService) ResetQueryCount() {
	atomic.StoreInt64(&mds.queryCount, 0)
}

// BenchmarkRunner 基准测试运行器
type BenchmarkRunner struct {
	config      BenchmarkConfig7
	dataService *MockDataService
	client      redis.Cmdable
}

// NewBenchmarkRunner 创建基准测试运行器
func NewBenchmarkRunner(config BenchmarkConfig7) (*BenchmarkRunner, error) {
	// 创建Redis客户端
	client, err := redisx.NewClient(&redisx.Config{
		Addr:     config.RedisAddress,
		Password: "",
		DB:       0,
		PoolSize: config.ConcurrentUsers * 2,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client: %w", err)
	}

	// 创建数据服务
	dataService := NewMockDataService(config.DBQueryDelay)

	return &BenchmarkRunner{
		config:      config,
		dataService: dataService,
		client:      client,
	}, nil
}

// RunBenchmark 运行基准测试
func (br *BenchmarkRunner) RunBenchmark() (*BenchmarkResult7, error) {
	strategies := []string{"no_protection", "singleflight", "logical_expire"}
	var results []*BenchmarkResult7

	for _, strategy := range strategies {
		fmt.Printf("🧪 Running benchmark for strategy: %s\n", strategy)

		// 清理缓存
		br.client.FlushDB(context.Background())
		br.dataService.ResetQueryCount()

		result, err := br.runSingleStrategy(strategy)
		if err != nil {
			log.Printf("Error running strategy %s: %v", strategy, err)
			continue
		}

		results = append(results, result)

		// 打印结果
		br.printResult(result)
		fmt.Println()

		// 休息一下
		time.Sleep(2 * time.Second)
	}

	// 保存结果到文件
	if br.config.OutputFile != "" {
		err := br.saveResults(results)
		if err != nil {
			log.Printf("Error saving results: %v", err)
		}
	}

	// 打印对比报告
	br.printComparisonReport(results)

	return nil, nil
}

// runSingleStrategy 运行单个策略的测试
func (br *BenchmarkRunner) runSingleStrategy(strategy string) (*BenchmarkResult7, error) {
	// 创建缓存组件
	redisCache := cache.NewRedisCache(br.client, cache.DefaultCacheAsideOptions())

	var breakdownProtection *cache.BreakdownProtection

	switch strategy {
	case "singleflight":
		opts := cache.DefaultBreakdownOptions()
		opts.EnableSingleflight = true
		opts.EnableLogicalExpire = false
		breakdownProtection = cache.NewBreakdownProtection(redisCache, opts)
	case "logical_expire":
		opts := cache.DefaultBreakdownOptions()
		opts.EnableSingleflight = false
		opts.EnableLogicalExpire = true
		breakdownProtection = cache.NewBreakdownProtection(redisCache, opts)
	}

	// 创建数据加载函数
	loader := func(ctx context.Context, key string) (interface{}, error) {
		return br.dataService.Query(ctx, key)
	}

	// 统计变量
	var totalRequests int64
	var successfulRequests int64
	var failedRequests int64
	var responseTimes []time.Duration
	var responseTimeMu sync.Mutex

	// 启动并发测试
	ctx, cancel := context.WithTimeout(context.Background(), br.config.TestDuration)
	defer cancel()

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < br.config.ConcurrentUsers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// 生成Key
					key := br.generateKey()

					requestStart := time.Now()
					atomic.AddInt64(&totalRequests, 1)

					var result interface{}
					var err error

					if breakdownProtection != nil {
						// 使用击穿防护
						err = breakdownProtection.GetOrLoad(context.Background(), key, &result, loader)
					} else {
						// 直接使用缓存
						err = redisCache.Get(context.Background(), key, &result)
						if err == cache.ErrCacheMiss {
							// 缓存未命中，查询数据源
							result, err = br.dataService.Query(context.Background(), key)
							if err == nil {
								// 写入缓存
								redisCache.Set(context.Background(), key, result, br.config.CacheExpireTTL)
							}
						}
					}

					responseTime := time.Since(requestStart)

					responseTimeMu.Lock()
					responseTimes = append(responseTimes, responseTime)
					responseTimeMu.Unlock()

					if err != nil {
						atomic.AddInt64(&failedRequests, 1)
					} else {
						atomic.AddInt64(&successfulRequests, 1)
					}

					// 随机延迟
					time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	actualDuration := time.Since(startTime)

	// 计算统计信息
	dbQueries := br.dataService.GetQueryCount()
	cacheHits := successfulRequests - dbQueries
	cacheHitRate := float64(cacheHits) / float64(successfulRequests) * 100
	qps := float64(totalRequests) / actualDuration.Seconds()

	// 计算响应时间统计
	avgResponseTime, p50, p90, p95, p99, maxResponseTime := br.calculateResponseTimeStats(responseTimes)

	// 创建响应时间直方图
	histogram := br.createResponseTimeHistogram(responseTimes)

	result := &BenchmarkResult7{
		Strategy:              strategy,
		Config:                br.config,
		TotalRequests:         totalRequests,
		SuccessfulRequests:    successfulRequests,
		FailedRequests:        failedRequests,
		DBQueries:             dbQueries,
		CacheHits:             cacheHits,
		CacheHitRate:          cacheHitRate,
		AvgResponseTime:       avgResponseTime,
		P50ResponseTime:       p50,
		P90ResponseTime:       p90,
		P95ResponseTime:       p95,
		P99ResponseTime:       p99,
		MaxResponseTime:       maxResponseTime,
		QPS:                   qps,
		ResponseTimeHistogram: histogram,
		Timestamp:             time.Now(),
	}

	// 获取击穿防护统计
	if breakdownProtection != nil {
		stats := breakdownProtection.GetStats()

		if strategy == "singleflight" {
			result.SingleflightStats = &SingleflightStats{
				Hits:     stats.SingleflightHits,
				Misses:   stats.SingleflightMisses,
				Timeouts: stats.SingleflightTimeouts,
				Errors:   stats.SingleflightErrors,
			}
		} else if strategy == "logical_expire" {
			result.LogicalExpireStats = &LogicalExpireStats{
				Hits:         stats.LogicalExpireHits,
				ExpiredHits:  stats.LogicalExpireExpired,
				RefreshCount: stats.LogicalExpireRefresh,
				ExpiredCount: stats.LogicalExpireExpired,
			}
		}
	}

	return result, nil
}

// generateKey 生成测试Key
func (br *BenchmarkRunner) generateKey() string {
	if rand.Float64() < br.config.HotKeyRatio {
		// 热点Key
		id := rand.Intn(br.config.HotKeyCount) + 1
		return fmt.Sprintf("item:%d", id)
	} else {
		// 普通Key
		id := rand.Intn(10000-br.config.HotKeyCount) + br.config.HotKeyCount + 1
		return fmt.Sprintf("item:%d", id)
	}
}

// calculateResponseTimeStats 计算响应时间统计
func (br *BenchmarkRunner) calculateResponseTimeStats(responseTimes []time.Duration) (avg, p50, p90, p95, p99, max time.Duration) {
	if len(responseTimes) == 0 {
		return 0, 0, 0, 0, 0, 0
	}

	// 排序
	for i := 0; i < len(responseTimes)-1; i++ {
		for j := 0; j < len(responseTimes)-i-1; j++ {
			if responseTimes[j] > responseTimes[j+1] {
				responseTimes[j], responseTimes[j+1] = responseTimes[j+1], responseTimes[j]
			}
		}
	}

	// 计算各种统计值
	var total time.Duration
	for _, rt := range responseTimes {
		total += rt
	}
	avg = total / time.Duration(len(responseTimes))

	p50 = responseTimes[int(float64(len(responseTimes))*0.5)]
	p90 = responseTimes[int(float64(len(responseTimes))*0.9)]
	p95 = responseTimes[int(float64(len(responseTimes))*0.95)]
	p99 = responseTimes[int(float64(len(responseTimes))*0.99)]
	max = responseTimes[len(responseTimes)-1]

	return avg, p50, p90, p95, p99, max
}

// createResponseTimeHistogram 创建响应时间直方图
func (br *BenchmarkRunner) createResponseTimeHistogram(responseTimes []time.Duration) map[string]int64 {
	histogram := make(map[string]int64)

	buckets := []time.Duration{
		1 * time.Millisecond,
		5 * time.Millisecond,
		10 * time.Millisecond,
		50 * time.Millisecond,
		100 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
		5 * time.Second,
	}

	for _, rt := range responseTimes {
		for i, bucket := range buckets {
			if rt <= bucket {
				key := fmt.Sprintf("≤%v", bucket)
				histogram[key]++
				break
			}
			if i == len(buckets)-1 {
				histogram[">5s"]++
			}
		}
	}

	return histogram
}

// printResult 打印结果
func (br *BenchmarkRunner) printResult(result *BenchmarkResult7) {
	fmt.Printf("📊 Strategy: %s\n", result.Strategy)
	fmt.Printf("   Total Requests: %d\n", result.TotalRequests)
	fmt.Printf("   Successful: %d\n", result.SuccessfulRequests)
	fmt.Printf("   Failed: %d\n", result.FailedRequests)
	fmt.Printf("   DB Queries: %d\n", result.DBQueries)
	fmt.Printf("   Cache Hit Rate: %.2f%%\n", result.CacheHitRate)
	fmt.Printf("   QPS: %.2f\n", result.QPS)
	fmt.Printf("   Avg Response Time: %v\n", result.AvgResponseTime)
	fmt.Printf("   P50: %v, P90: %v, P95: %v, P99: %v\n",
		result.P50ResponseTime, result.P90ResponseTime, result.P95ResponseTime, result.P99ResponseTime)

	if result.SingleflightStats != nil {
		fmt.Printf("   Singleflight - Hits: %d, Misses: %d\n",
			result.SingleflightStats.Hits, result.SingleflightStats.Misses)
	}

	if result.LogicalExpireStats != nil {
		fmt.Printf("   Logical Expire - Hits: %d, Expired Hits: %d\n",
			result.LogicalExpireStats.Hits, result.LogicalExpireStats.ExpiredHits)
	}
}

// printComparisonReport 打印对比报告
func (br *BenchmarkRunner) printComparisonReport(results []*BenchmarkResult7) {
	fmt.Println("📈 Breakdown Protection Benchmark Comparison Report")
	fmt.Println("=" + fmt.Sprintf("%*s", 80, "="))

	fmt.Printf("%-20s %-10s %-12s %-12s %-12s %-12s %-10s\n",
		"Strategy", "QPS", "Cache Hit%", "DB Queries", "Avg Resp", "P99 Resp", "Errors")
	fmt.Println("-" + fmt.Sprintf("%*s", 100, "-"))

	for _, result := range results {
		fmt.Printf("%-20s %-10.0f %-12.2f %-12d %-12v %-12v %-10d\n",
			result.Strategy,
			result.QPS,
			result.CacheHitRate,
			result.DBQueries,
			result.AvgResponseTime,
			result.P99ResponseTime,
			result.FailedRequests,
		)
	}

	fmt.Println()

	// 分析改进效果
	if len(results) >= 2 {
		baseline := results[0] // 无防护作为基线

		for i := 1; i < len(results); i++ {
			result := results[i]

			dbReduction := float64(baseline.DBQueries-result.DBQueries) / float64(baseline.DBQueries) * 100
			qpsImprovement := (result.QPS - baseline.QPS) / baseline.QPS * 100
			responseImprovement := float64(baseline.AvgResponseTime-result.AvgResponseTime) / float64(baseline.AvgResponseTime) * 100

			fmt.Printf("🎯 %s vs %s:\n", result.Strategy, baseline.Strategy)
			fmt.Printf("   DB queries reduced: %.2f%%\n", dbReduction)
			fmt.Printf("   QPS improvement: %.2f%%\n", qpsImprovement)
			fmt.Printf("   Avg response time improvement: %.2f%%\n", responseImprovement)
			fmt.Println()
		}
	}
}

// saveResults 保存结果到文件
func (br *BenchmarkRunner) saveResults(results []*BenchmarkResult7) error {
	data, err := json.MarshalIndent(map[string]interface{}{
		"benchmark_config": br.config,
		"results":          results,
		"summary": map[string]interface{}{
			"timestamp":         time.Now(),
			"strategies_tested": len(results),
		},
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}

	return os.WriteFile(br.config.OutputFile, data, 0644)
}

func main7() {
	config := BenchmarkConfig7{
		ConcurrentUsers: 50,
		TestDuration:    30 * time.Second,
		HotKeyCount:     10,  // 前10个Key作为热点
		HotKeyRatio:     0.8, // 80%的请求访问热点Key
		CacheExpireTTL:  10 * time.Minute,
		DBQueryDelay:    50 * time.Millisecond,
		RedisAddress:    "localhost:6379",
		OutputFile:      "benchmark_results/breakdown_benchmark_" + time.Now().Format("20060102_150405") + ".json",
	}

	// 创建输出目录
	os.MkdirAll("benchmark_results", 0755)

	runner, err := NewBenchmarkRunner(config)
	if err != nil {
		log.Fatalf("Failed to create benchmark runner: %v", err)
	}

	fmt.Println("🚀 Starting Cache Breakdown Protection Benchmark")
	fmt.Printf("   Concurrent Users: %d\n", config.ConcurrentUsers)
	fmt.Printf("   Test Duration: %v\n", config.TestDuration)
	fmt.Printf("   Hot Key Ratio: %.1f%%\n", config.HotKeyRatio*100)
	fmt.Printf("   DB Query Delay: %v\n", config.DBQueryDelay)
	fmt.Println()

	_, err = runner.RunBenchmark()
	if err != nil {
		log.Fatalf("Benchmark failed: %v", err)
	}

	fmt.Println("✅ Benchmark completed successfully!")
	if config.OutputFile != "" {
		fmt.Printf("📁 Results saved to: %s\n", config.OutputFile)
	}
}
