package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"
)

// AvalancheBenchmarkConfig 雪崩压测配置
type AvalancheBenchmarkConfig struct {
	// Redis配置
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// 压测参数
	Duration    time.Duration
	Concurrency int
	KeyCount    int
	CacheSize   int

	// 雪崩场景配置
	AvalancheTTL   time.Duration
	AvalancheDelay time.Duration

	// 数据源模拟配置
	DBLatency   time.Duration
	DBErrorRate float64

	// 防护配置
	EnableProtection bool
	TTLJitter        float64
	ShardCount       int
	ShardInterval    time.Duration
	CircuitBreaker   bool
	FailureRate      float64
	MinRequestCount  int
	OpenTimeout      time.Duration

	// 输出配置
	OutputFile string
	Verbose    bool
}

// AvalancheBenchmarkResult 雪崩压测结果
type AvalancheBenchmarkResult struct {
	Config    *AvalancheBenchmarkConfig `json:"config"`
	Scenario  string                    `json:"scenario"`
	StartTime time.Time                 `json:"start_time"`
	EndTime   time.Time                 `json:"end_time"`
	Duration  time.Duration             `json:"duration"`

	// 请求统计
	TotalRequests        int64 `json:"total_requests"`
	SuccessRequests      int64 `json:"success_requests"`
	ErrorRequests        int64 `json:"error_requests"`
	TimeoutRequests      int64 `json:"timeout_requests"`
	CircuitBreakRequests int64 `json:"circuit_break_requests"`
	DegradedRequests     int64 `json:"degraded_requests"`

	// 性能指标
	QPS        float64       `json:"qps"`
	AvgLatency time.Duration `json:"avg_latency"`
	P50Latency time.Duration `json:"p50_latency"`
	P95Latency time.Duration `json:"p95_latency"`
	P99Latency time.Duration `json:"p99_latency"`
	MaxLatency time.Duration `json:"max_latency"`

	// 缓存统计
	CacheHits    int64   `json:"cache_hits"`
	CacheMisses  int64   `json:"cache_misses"`
	CacheHitRate float64 `json:"cache_hit_rate"`
	CacheErrors  int64   `json:"cache_errors"`

	// 数据源统计
	DBRequests  int64   `json:"db_requests"`
	DBErrors    int64   `json:"db_errors"`
	DBErrorRate float64 `json:"db_error_rate"`

	// 熔断器统计
	CircuitBreakerStats map[string]interface{} `json:"circuit_breaker_stats,omitempty"`
}

// AvalancheLatencyCollector 延迟收集器
type AvalancheLatencyCollector struct {
	latencies []time.Duration
	mu        sync.Mutex
}

// Record 记录延迟
func (lc *AvalancheLatencyCollector) Record(latency time.Duration) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.latencies = append(lc.latencies, latency)
}

// GetPercentiles 获取百分位数
func (lc *AvalancheLatencyCollector) GetPercentiles() (avg, p50, p95, p99, max time.Duration) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if len(lc.latencies) == 0 {
		return
	}

	// 排序
	for i := 0; i < len(lc.latencies)-1; i++ {
		for j := 0; j < len(lc.latencies)-1-i; j++ {
			if lc.latencies[j] > lc.latencies[j+1] {
				lc.latencies[j], lc.latencies[j+1] = lc.latencies[j+1], lc.latencies[j]
			}
		}
	}

	// 计算平均值
	var total time.Duration
	for _, latency := range lc.latencies {
		total += latency
	}
	avg = total / time.Duration(len(lc.latencies))

	// 计算百分位数
	count := len(lc.latencies)
	p50 = lc.latencies[count/2]
	p95 = lc.latencies[int(float64(count)*0.95)]
	p99 = lc.latencies[int(float64(count)*0.99)]
	max = lc.latencies[count-1]

	return
}

// AvalancheMockDataSource 模拟数据源
type AvalancheMockDataSource struct {
	data         map[string]interface{}
	mu           sync.RWMutex
	latency      time.Duration
	errorRate    float64
	requestCount int64
	errorCount   int64
}

// NewAvalancheMockDataSource 创建模拟数据源
func NewAvalancheMockDataSource(size int, latency time.Duration, errorRate float64) *AvalancheMockDataSource {
	data := make(map[string]interface{})

	// 生成测试数据
	for i := 0; i < size; i++ {
		key := fmt.Sprintf("key_%d", i)
		data[key] = map[string]interface{}{
			"id":        i,
			"name":      fmt.Sprintf("name_%d", i),
			"value":     rand.Intn(1000),
			"timestamp": time.Now(),
		}
	}

	return &AvalancheMockDataSource{
		data:      data,
		latency:   latency,
		errorRate: errorRate,
	}
}

// Get 获取数据
func (ds *AvalancheMockDataSource) Get(ctx context.Context, key string) (interface{}, error) {
	atomic.AddInt64(&ds.requestCount, 1)

	// 模拟延迟
	select {
	case <-time.After(ds.latency):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// 模拟错误
	if rand.Float64() < ds.errorRate {
		atomic.AddInt64(&ds.errorCount, 1)
		return nil, fmt.Errorf("data source error for key: %s", key)
	}

	ds.mu.RLock()
	defer ds.mu.RUnlock()

	value, exists := ds.data[key]
	if !exists {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	return value, nil
}

// GetStats 获取统计信息
func (ds *AvalancheMockDataSource) GetStats() (requests, errors int64) {
	return atomic.LoadInt64(&ds.requestCount), atomic.LoadInt64(&ds.errorCount)
}

// ResetStats 重置统计信息
func (ds *AvalancheMockDataSource) ResetStats() {
	atomic.StoreInt64(&ds.requestCount, 0)
	atomic.StoreInt64(&ds.errorCount, 0)
}

// SetLatency 设置延迟
func (ds *AvalancheMockDataSource) SetLatency(latency time.Duration) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.latency = latency
}

// SetErrorRate 设置错误率
func (ds *AvalancheMockDataSource) SetErrorRate(rate float64) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.errorRate = rate
}

// runAvalancheBenchmark 运行雪崩压测
func runAvalancheBenchmark(config *AvalancheBenchmarkConfig, scenario string) (*AvalancheBenchmarkResult, error) {
	fmt.Printf("开始压测场景: %s\n", scenario)
	fmt.Printf("配置: 并发=%d, 时长=%v, key数量=%d\n",
		config.Concurrency, config.Duration, config.KeyCount)

	// 连接Redis
	client, err := redisx.NewClient(&redisx.Config{
		Addr:         config.RedisAddr,
		Password:     config.RedisPassword,
		DB:           config.RedisDB,
		PoolSize:     config.Concurrency * 2,
		MinIdleConns: config.Concurrency,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to redis failed: %w", err)
	}
	defer client.Close()

	// 创建模拟数据源
	dataSource := NewAvalancheMockDataSource(config.CacheSize, config.DBLatency, config.DBErrorRate)

	// 创建缓存
	var cacheInstance cache.Cache
	var avalancheProtection *cache.AvalancheProtection

	if config.EnableProtection {
		// 创建带防护的缓存
		avalancheConfig := cache.DefaultAvalancheProtectionConfig()
		avalancheConfig.TTLJitter = config.TTLJitter
		avalancheConfig.ShardCount = config.ShardCount
		avalancheConfig.ShardInterval = config.ShardInterval

		if config.CircuitBreaker {
			avalancheConfig.FailureRate = config.FailureRate
			avalancheConfig.MinRequestCount = config.MinRequestCount
			avalancheConfig.OpenTimeout = config.OpenTimeout
		}

		// 设置降级函数
		avalancheConfig.DegradeFunc = func(ctx context.Context, key string) (interface{}, error) {
			return map[string]interface{}{
				"id":        -1,
				"name":      "degraded",
				"value":     0,
				"timestamp": time.Now(),
				"degraded":  true,
			}, nil
		}

		baseCache := cache.NewRedisCache(client, avalancheConfig.CacheAsideOptions)
		avalancheProtection = cache.NewAvalancheProtection(baseCache, avalancheConfig)
		cacheInstance = baseCache
	} else {
		// 创建普通缓存
		cacheOptions := cache.DefaultCacheAsideOptions()
		cacheOptions.TTL = config.AvalancheTTL
		cacheOptions.TTLJitter = 0 // 不使用抖动，制造雪崩
		cacheInstance = cache.NewRedisCache(client, cacheOptions)
	}

	// 预热缓存（模拟雪崩前的状态）
	if err := warmupAvalancheCache(cacheInstance, dataSource, config); err != nil {
		return nil, fmt.Errorf("warmup cache failed: %w", err)
	}

	// 等待接近过期时间，触发雪崩
	if config.AvalancheDelay > 0 {
		fmt.Printf("等待 %v 触发雪崩...\n", config.AvalancheDelay)
		time.Sleep(config.AvalancheDelay)
	}

	// 重置统计
	dataSource.ResetStats()

	// 准备压测
	var (
		totalRequests        int64
		successRequests      int64
		errorRequests        int64
		timeoutRequests      int64
		circuitBreakRequests int64
		degradedRequests     int64

		latencyCollector = &AvalancheLatencyCollector{}
		wg               sync.WaitGroup
	)

	startTime := time.Now()
	endTime := startTime.Add(config.Duration)

	// 启动并发goroutine
	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for time.Now().Before(endTime) {
				// 随机选择key
				keyIndex := rand.Intn(config.KeyCount)
				key := fmt.Sprintf("key_%d", keyIndex)

				atomic.AddInt64(&totalRequests, 1)

				// 记录请求开始时间
				requestStart := time.Now()

				// 发送请求
				var result interface{}
				var err error

				if config.EnableProtection && avalancheProtection != nil {
					// 使用雪崩防护
					err = avalancheProtection.GetOrLoad(context.Background(), key, &result,
						func(ctx context.Context, key string) (interface{}, error) {
							return dataSource.Get(ctx, key)
						})
				} else {
					// 直接使用缓存
					err = cacheInstance.Get(context.Background(), key, &result)
					if err == cache.ErrCacheMiss {
						// 缓存未命中，从数据源加载
						result, err = dataSource.Get(context.Background(), key)
						if err == nil {
							// 写入缓存
							cacheInstance.Set(context.Background(), key, result, config.AvalancheTTL)
						}
					}
				}

				// 记录延迟
				latency := time.Since(requestStart)
				latencyCollector.Record(latency)

				// 统计结果
				if err == nil {
					atomic.AddInt64(&successRequests, 1)

					// 检查是否为降级响应
					if resultMap, ok := result.(map[string]interface{}); ok {
						if degraded, exists := resultMap["degraded"]; exists && degraded.(bool) {
							atomic.AddInt64(&degradedRequests, 1)
						}
					}
				} else {
					atomic.AddInt64(&errorRequests, 1)

					// 分类错误
					if err == cache.ErrCircuitBreakerOpen {
						atomic.AddInt64(&circuitBreakRequests, 1)
					} else if err == context.DeadlineExceeded {
						atomic.AddInt64(&timeoutRequests, 1)
					}

					if config.Verbose {
						log.Printf("Worker %d error: %v", workerID, err)
					}
				}

				// 控制请求频率
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// 等待所有goroutine完成
	wg.Wait()

	actualDuration := time.Since(startTime)

	// 收集统计信息
	dbRequests, dbErrors := dataSource.GetStats()

	// 计算延迟统计
	avgLatency, p50Latency, p95Latency, p99Latency, maxLatency := latencyCollector.GetPercentiles()

	// 获取缓存统计
	var cacheHits, cacheMisses, cacheErrors int64
	var cacheHitRate float64

	if redisCache, ok := cacheInstance.(*cache.RedisCache); ok {
		if stats := redisCache.GetStats(); stats != nil {
			cacheHits = stats.Hits
			cacheMisses = stats.Misses
			cacheErrors = stats.GetErrors

			totalCacheRequests := cacheHits + cacheMisses
			if totalCacheRequests > 0 {
				cacheHitRate = float64(cacheHits) / float64(totalCacheRequests)
			}
		}
	}

	// 获取熔断器统计
	var circuitBreakerStats map[string]interface{}
	if avalancheProtection != nil {
		circuitBreakerStats = avalancheProtection.GetCircuitBreakerStats()
	}

	// 构建结果
	result := &AvalancheBenchmarkResult{
		Config:               config,
		Scenario:             scenario,
		StartTime:            startTime,
		EndTime:              time.Now(),
		Duration:             actualDuration,
		TotalRequests:        totalRequests,
		SuccessRequests:      successRequests,
		ErrorRequests:        errorRequests,
		TimeoutRequests:      timeoutRequests,
		CircuitBreakRequests: circuitBreakRequests,
		DegradedRequests:     degradedRequests,
		QPS:                  float64(totalRequests) / actualDuration.Seconds(),
		AvgLatency:           avgLatency,
		P50Latency:           p50Latency,
		P95Latency:           p95Latency,
		P99Latency:           p99Latency,
		MaxLatency:           maxLatency,
		CacheHits:            cacheHits,
		CacheMisses:          cacheMisses,
		CacheHitRate:         cacheHitRate,
		CacheErrors:          cacheErrors,
		DBRequests:           dbRequests,
		DBErrors:             dbErrors,
		DBErrorRate:          float64(dbErrors) / float64(dbRequests),
		CircuitBreakerStats:  circuitBreakerStats,
	}

	// 打印结果
	printAvalancheResult(result)

	// 清理资源
	if avalancheProtection != nil {
		avalancheProtection.Close()
	}

	return result, nil
}

// warmupAvalancheCache 预热缓存
func warmupAvalancheCache(cacheInstance cache.Cache, dataSource *AvalancheMockDataSource, config *AvalancheBenchmarkConfig) error {
	fmt.Printf("预热缓存中... (预热 %d 个key)\n", config.KeyCount)

	ctx := context.Background()

	for i := 0; i < config.KeyCount; i++ {
		key := fmt.Sprintf("key_%d", i)

		// 从数据源获取数据
		value, err := dataSource.Get(ctx, key)
		if err != nil {
			continue
		}

		// 写入缓存
		if err := cacheInstance.Set(ctx, key, value, config.AvalancheTTL); err != nil {
			log.Printf("Warmup cache set failed for key %s: %v", key, err)
		}
	}

	fmt.Println("缓存预热完成")
	return nil
}

// printAvalancheResult 打印结果
func printAvalancheResult(result *AvalancheBenchmarkResult) {
	fmt.Printf("\n=== 压测结果: %s ===\n", result.Scenario)
	fmt.Printf("测试时长: %v\n", result.Duration)
	fmt.Printf("总请求数: %d\n", result.TotalRequests)
	fmt.Printf("成功请求: %d (%.2f%%)\n",
		result.SuccessRequests,
		float64(result.SuccessRequests)/float64(result.TotalRequests)*100)
	fmt.Printf("失败请求: %d (%.2f%%)\n",
		result.ErrorRequests,
		float64(result.ErrorRequests)/float64(result.TotalRequests)*100)

	if result.CircuitBreakRequests > 0 {
		fmt.Printf("熔断请求: %d (%.2f%%)\n",
			result.CircuitBreakRequests,
			float64(result.CircuitBreakRequests)/float64(result.TotalRequests)*100)
	}

	if result.DegradedRequests > 0 {
		fmt.Printf("降级请求: %d (%.2f%%)\n",
			result.DegradedRequests,
			float64(result.DegradedRequests)/float64(result.TotalRequests)*100)
	}

	fmt.Printf("QPS: %.2f\n", result.QPS)
	fmt.Printf("延迟统计:\n")
	fmt.Printf("  平均: %v\n", result.AvgLatency)
	fmt.Printf("  P50:  %v\n", result.P50Latency)
	fmt.Printf("  P95:  %v\n", result.P95Latency)
	fmt.Printf("  P99:  %v\n", result.P99Latency)
	fmt.Printf("  最大: %v\n", result.MaxLatency)

	fmt.Printf("缓存统计:\n")
	fmt.Printf("  命中: %d, 未命中: %d\n", result.CacheHits, result.CacheMisses)
	fmt.Printf("  命中率: %.2f%%\n", result.CacheHitRate*100)
	fmt.Printf("  缓存错误: %d\n", result.CacheErrors)

	fmt.Printf("数据源统计:\n")
	fmt.Printf("  请求数: %d, 错误数: %d\n", result.DBRequests, result.DBErrors)
	fmt.Printf("  错误率: %.2f%%\n", result.DBErrorRate*100)

	if result.CircuitBreakerStats != nil {
		fmt.Printf("熔断器统计: %+v\n", result.CircuitBreakerStats)
	}

	fmt.Println()
}

// saveAvalancheResults 保存结果
func saveAvalancheResults(results []*AvalancheBenchmarkResult, filename string) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}

	// 创建目录
	if err := os.MkdirAll("benchmark_results", 0755); err != nil {
		return err
	}

	return os.WriteFile(filename, data, 0644)
}

// runFullAvalancheBenchmark 运行完整雪崩压测
func runFullAvalancheBenchmark(config *AvalancheBenchmarkConfig) error {
	fmt.Println("=== 缓存雪崩压测 ===")

	var results []*AvalancheBenchmarkResult

	// 1. 无防护场景
	fmt.Println("\n1. 无防护场景测试")
	config.EnableProtection = false
	result1, err := runAvalancheBenchmark(config, "无防护-雪崩场景")
	if err != nil {
		log.Printf("无防护场景测试失败: %v", err)
	} else {
		results = append(results, result1)
	}

	// 等待系统恢复
	fmt.Println("等待系统恢复...")
	time.Sleep(10 * time.Second)

	// 2. 有防护场景
	fmt.Println("\n2. 有防护场景测试")
	config.EnableProtection = true
	result2, err := runAvalancheBenchmark(config, "有防护-雪崩场景")
	if err != nil {
		log.Printf("有防护场景测试失败: %v", err)
	} else {
		results = append(results, result2)
	}

	// 保存结果
	if len(results) > 0 {
		filename := fmt.Sprintf("benchmark_results/avalanche_benchmark_%s.json",
			time.Now().Format("20060102_150405"))
		if err := saveAvalancheResults(results, filename); err != nil {
			log.Printf("保存结果失败: %v", err)
		} else {
			fmt.Printf("结果已保存到: %s\n", filename)
		}
	}

	// 对比分析
	if len(results) >= 2 {
		fmt.Println("\n=== 对比分析 ===")
		compareAvalancheResults(results[0], results[1])
	}

	return nil
}

// compareAvalancheResults 对比结果
func compareAvalancheResults(baseline, protected *AvalancheBenchmarkResult) {
	fmt.Printf("场景对比: %s vs %s\n", baseline.Scenario, protected.Scenario)

	// QPS对比
	qpsImprovement := (protected.QPS - baseline.QPS) / baseline.QPS * 100
	fmt.Printf("QPS: %.2f -> %.2f (%.2f%%)\n",
		baseline.QPS, protected.QPS, qpsImprovement)

	// 成功率对比
	baselineSuccessRate := float64(baseline.SuccessRequests) / float64(baseline.TotalRequests) * 100
	protectedSuccessRate := float64(protected.SuccessRequests) / float64(protected.TotalRequests) * 100
	fmt.Printf("成功率: %.2f%% -> %.2f%% (+%.2f%%)\n",
		baselineSuccessRate, protectedSuccessRate, protectedSuccessRate-baselineSuccessRate)

	// 延迟对比
	fmt.Printf("平均延迟: %v -> %v\n", baseline.AvgLatency, protected.AvgLatency)
	fmt.Printf("P95延迟: %v -> %v\n", baseline.P95Latency, protected.P95Latency)
	fmt.Printf("P99延迟: %v -> %v\n", baseline.P99Latency, protected.P99Latency)

	// 数据源压力对比
	fmt.Printf("数据源请求: %d -> %d (减少%.2f%%)\n",
		baseline.DBRequests, protected.DBRequests,
		(float64(baseline.DBRequests-protected.DBRequests)/float64(baseline.DBRequests))*100)

	// 缓存命中率对比
	fmt.Printf("缓存命中率: %.2f%% -> %.2f%%\n",
		baseline.CacheHitRate*100, protected.CacheHitRate*100)
}

func main() {
	var config AvalancheBenchmarkConfig

	// 解析命令行参数
	flag.StringVar(&config.RedisAddr, "redis-addr", "localhost:6379", "Redis地址")
	flag.StringVar(&config.RedisPassword, "redis-password", "", "Redis密码")
	flag.IntVar(&config.RedisDB, "redis-db", 0, "Redis数据库")

	flag.DurationVar(&config.Duration, "duration", 30*time.Second, "压测时长")
	flag.IntVar(&config.Concurrency, "concurrency", 50, "并发数")
	flag.IntVar(&config.KeyCount, "key-count", 1000, "key数量")
	flag.IntVar(&config.CacheSize, "cache-size", 10000, "缓存大小")

	flag.DurationVar(&config.AvalancheTTL, "avalanche-ttl", 30*time.Second, "雪崩TTL")
	flag.DurationVar(&config.AvalancheDelay, "avalanche-delay", 25*time.Second, "雪崩延迟")

	flag.DurationVar(&config.DBLatency, "db-latency", 100*time.Millisecond, "数据源延迟")
	flag.Float64Var(&config.DBErrorRate, "db-error-rate", 0.1, "数据源错误率")

	flag.Float64Var(&config.TTLJitter, "ttl-jitter", 0.2, "TTL抖动比例")
	flag.IntVar(&config.ShardCount, "shard-count", 10, "分片数量")
	flag.DurationVar(&config.ShardInterval, "shard-interval", 5*time.Second, "分片间隔")

	flag.BoolVar(&config.CircuitBreaker, "circuit-breaker", true, "启用熔断器")
	flag.Float64Var(&config.FailureRate, "failure-rate", 0.5, "熔断失败率")
	flag.IntVar(&config.MinRequestCount, "min-request-count", 20, "最小请求数")
	flag.DurationVar(&config.OpenTimeout, "open-timeout", 10*time.Second, "熔断超时")

	flag.StringVar(&config.OutputFile, "output", "", "输出文件")
	flag.BoolVar(&config.Verbose, "verbose", false, "详细输出")

	flag.Parse()

	fmt.Println("Redis 缓存雪崩压测工具")
	fmt.Println("======================")

	// 设置随机种子
	rand.Seed(time.Now().UnixNano())

	// 运行压测
	if err := runFullAvalancheBenchmark(&config); err != nil {
		log.Fatalf("压测失败: %v", err)
	}

	fmt.Println("压测完成！")
}
