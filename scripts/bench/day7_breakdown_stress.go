package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"

	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

// StressTestConfig 压力测试配置
type StressTestConfig struct {
	// 击穿场景参数
	ConcurrentUsers  int           `json:"concurrent_users"`   // 并发用户数
	HotKeyCount      int           `json:"hot_key_count"`      // 热点Key数量
	CacheExpireEvery time.Duration `json:"cache_expire_every"` // 缓存过期间隔
	DBQueryDelay     time.Duration `json:"db_query_delay"`     // DB查询延迟
	TestDuration     time.Duration `json:"test_duration"`      // 测试时长

	// 击穿强度控制
	ExpireAllHotKeys bool          `json:"expire_all_hot_keys"` // 是否同时让所有热点Key过期
	ExpireInterval   time.Duration `json:"expire_interval"`     // 过期间隔
}

// StressTestResult 压力测试结果
type StressTestResult struct {
	Strategy           string           `json:"strategy"`
	Config             StressTestConfig `json:"config"`
	TotalRequests      int64            `json:"total_requests"`
	DBQueries          int64            `json:"db_queries"`
	CacheHitRate       float64          `json:"cache_hit_rate"`
	AvgResponseTime    time.Duration    `json:"avg_response_time"`
	P99ResponseTime    time.Duration    `json:"p99_response_time"`
	MaxResponseTime    time.Duration    `json:"max_response_time"`
	ErrorCount         int64            `json:"error_count"`
	SingleflightHits   int64            `json:"singleflight_hits"`
	LogicalExpireHits  int64            `json:"logical_expire_hits"`
	DBQueriesPerSecond float64          `json:"db_queries_per_second"`
	ResponseTimeSpikes int64            `json:"response_time_spikes"` // 响应时间峰值次数
	Timestamp          time.Time        `json:"timestamp"`
}

// HotKeyExpirer 热点Key过期器
type HotKeyExpirer struct {
	client redis.Cmdable
	config StressTestConfig
	quit   chan struct{}
	wg     sync.WaitGroup
}

// NewHotKeyExpirer 创建热点Key过期器
func NewHotKeyExpirer(client redis.Cmdable, config StressTestConfig) *HotKeyExpirer {
	return &HotKeyExpirer{
		client: client,
		config: config,
		quit:   make(chan struct{}),
	}
}

// Start 启动过期器
func (hke *HotKeyExpirer) Start() {
	hke.wg.Add(1)
	go hke.expireWorker()
}

// Stop 停止过期器
func (hke *HotKeyExpirer) Stop() {
	select {
	case <-hke.quit:
		// 已经关闭了
		return
	default:
		close(hke.quit)
	}
	hke.wg.Wait()
}

// expireWorker 过期工作协程
func (hke *HotKeyExpirer) expireWorker() {
	defer hke.wg.Done()

	ticker := time.NewTicker(hke.config.ExpireInterval)
	defer ticker.Stop()

	for {
		select {
		case <-hke.quit:
			return
		case <-ticker.C:
			hke.expireHotKeys()
		}
	}
}

// expireHotKeys 使热点Key过期
func (hke *HotKeyExpirer) expireHotKeys() {
	if hke.config.ExpireAllHotKeys {
		// 同时过期所有热点Key（最极端的击穿场景）
		var keys []string
		for i := 1; i <= hke.config.HotKeyCount; i++ {
			keys = append(keys, fmt.Sprintf("hot_item:%d", i))
		}

		if len(keys) > 0 {
			hke.client.Del(context.Background(), keys...)
			fmt.Printf("⚡ Expired %d hot keys at %s\n", len(keys), time.Now().Format("15:04:05.000"))
		}
	} else {
		// 随机过期部分热点Key
		expireCount := hke.config.HotKeyCount / 2
		for i := 0; i < expireCount; i++ {
			keyId := rand.Intn(hke.config.HotKeyCount) + 1
			key := fmt.Sprintf("hot_item:%d", keyId)
			hke.client.Del(context.Background(), key)
		}
		fmt.Printf("⚡ Expired %d random hot keys at %s\n", expireCount, time.Now().Format("15:04:05.000"))
	}
}

// MockHeavyService 模拟重型服务
type MockHeavyService struct {
	queryCount int64
	queryDelay time.Duration
}

// NewMockHeavyService 创建重型服务
func NewMockHeavyService(queryDelay time.Duration) *MockHeavyService {
	return &MockHeavyService{
		queryDelay: queryDelay,
	}
}

// HeavyQuery 重型查询
func (mhs *MockHeavyService) HeavyQuery(ctx context.Context, key string) (interface{}, error) {
	count := atomic.AddInt64(&mhs.queryCount, 1)

	// 只为热点Key打印日志，减少输出
	if key == "hot_item:1" {
		fmt.Printf("🐌 DB Query #%d for HOT KEY: %s\n", count, key)
	}

	// 模拟复杂查询
	select {
	case <-time.After(mhs.queryDelay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	if key == "hot_item:1" {
		fmt.Printf("✅ DB Query #%d for HOT KEY completed\n", count)
	}

	// 返回模拟数据
	return map[string]interface{}{
		"key":        key,
		"data":       fmt.Sprintf("Heavy data for %s", key),
		"generated":  time.Now().Unix(),
		"complexity": "high",
	}, nil
}

// GetQueryCount 获取查询次数
func (mhs *MockHeavyService) GetQueryCount() int64 {
	return atomic.LoadInt64(&mhs.queryCount)
}

// ResetQueryCount 重置查询计数
func (mhs *MockHeavyService) ResetQueryCount() {
	atomic.StoreInt64(&mhs.queryCount, 0)
}

// StressTestRunner 压力测试运行器
type StressTestRunner struct {
	client  redis.Cmdable
	config  StressTestConfig
	service *MockHeavyService
	expirer *HotKeyExpirer
}

// NewStressTestRunner 创建压力测试运行器
func NewStressTestRunner(config StressTestConfig) (*StressTestRunner, error) {
	// 创建Redis客户端
	client, err := redisx.NewClient(&redisx.Config{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: config.ConcurrentUsers * 2,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Redis client: %w", err)
	}

	// 创建重型服务
	service := NewMockHeavyService(config.DBQueryDelay)

	return &StressTestRunner{
		client:  client,
		config:  config,
		service: service,
		expirer: nil, // 不在这里创建过期器
	}, nil
}

// RunStressTest 运行压力测试
func (str *StressTestRunner) RunStressTest() error {
	strategies := []string{"no_protection", "singleflight", "logical_expire"}
	var results []*StressTestResult

	for _, strategy := range strategies {
		fmt.Printf("\n🔥 Running STRESS test for strategy: %s\n", strategy)
		fmt.Printf("   Concurrent Users: %d\n", str.config.ConcurrentUsers)
		fmt.Printf("   Hot Keys: %d\n", str.config.HotKeyCount)
		fmt.Printf("   DB Query Delay: %s\n", str.config.DBQueryDelay)
		fmt.Printf("   Expire All Hot Keys: %v\n", str.config.ExpireAllHotKeys)
		fmt.Printf("   Expire Interval: %s\n", str.config.ExpireInterval)
		fmt.Println()

		// 清理并预热
		str.client.FlushDB(context.Background())
		str.service.ResetQueryCount()
		str.preWarmCache()

		// 运行单个策略测试
		result, err := str.runSingleStrategyStress(strategy)
		if err != nil {
			log.Printf("Error running strategy %s: %v", strategy, err)
			continue
		}

		results = append(results, result)
		str.printStressResult(result)
	}

	// 打印对比报告
	str.printStressComparisonReport(results)

	return nil
}

// preWarmCache 预热缓存
func (str *StressTestRunner) preWarmCache() {
	fmt.Printf("🔥 Pre-warming cache with %d hot keys...\n", str.config.HotKeyCount)

	redisCache := cache.NewRedisCache(str.client, cache.DefaultCacheAsideOptions())

	for i := 1; i <= str.config.HotKeyCount; i++ {
		key := fmt.Sprintf("hot_item:%d", i)
		data, _ := str.service.HeavyQuery(context.Background(), key)
		redisCache.Set(context.Background(), key, data, 10*time.Second) // 设置较短TTL，便于观察过期效果
	}

	// 重置计数器（预热不算入统计）
	str.service.ResetQueryCount()
	fmt.Println("✅ Cache pre-warming completed")
}

// runSingleStrategyStress 运行单个策略的压力测试
func (str *StressTestRunner) runSingleStrategyStress(strategy string) (*StressTestResult, error) {
	// 创建缓存组件
	redisCache := cache.NewRedisCache(str.client, cache.DefaultCacheAsideOptions())

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
		return str.service.HeavyQuery(ctx, key)
	}

	// 统计变量
	var totalRequests int64
	var errorCount int64
	var responseTimes []time.Duration
	var responseTimeSpikes int64
	var responseTimeMu sync.Mutex

	// 为这个策略创建独立的过期器
	expirer := NewHotKeyExpirer(str.client, str.config)
	expirer.Start()
	defer func() {
		// 安全地停止过期器
		expirer.Stop()
	}()

	// 启动并发测试
	ctx, cancel := context.WithTimeout(context.Background(), str.config.TestDuration)
	defer cancel()

	var wg sync.WaitGroup
	startTime := time.Now()

	for i := 0; i < str.config.ConcurrentUsers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					// 主要访问热点Key（90%概率）
					var key string
					if rand.Float64() < 0.9 {
						keyId := rand.Intn(str.config.HotKeyCount) + 1
						key = fmt.Sprintf("hot_item:%d", keyId)
					} else {
						keyId := rand.Intn(1000) + str.config.HotKeyCount + 1
						key = fmt.Sprintf("normal_item:%d", keyId)
					}

					requestStart := time.Now()
					atomic.AddInt64(&totalRequests, 1)

					var result interface{}
					var err error

					if breakdownProtection != nil {
						err = breakdownProtection.GetOrLoad(context.Background(), key, &result, loader)
					} else {
						err = redisCache.Get(context.Background(), key, &result)
						if err == cache.ErrCacheMiss {
							// 增加一个小延迟，模拟检查缓存到开始查询DB之间的间隙
							// 这个间隙让更多并发请求能感知到缓存未命中
							time.Sleep(10 * time.Millisecond)
							result, err = str.service.HeavyQuery(context.Background(), key)
							if err == nil {
								// 使用较短的TTL，让击穿能够重复发生
								redisCache.Set(context.Background(), key, result, 10*time.Second)
							}
						}
					}

					responseTime := time.Since(requestStart)

					// 检查响应时间峰值
					if responseTime > 100*time.Millisecond {
						atomic.AddInt64(&responseTimeSpikes, 1)
					}

					responseTimeMu.Lock()
					responseTimes = append(responseTimes, responseTime)
					responseTimeMu.Unlock()

					if err != nil {
						atomic.AddInt64(&errorCount, 1)
					}

					// 短暂休息
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	actualDuration := time.Since(startTime)

	// 计算统计信息
	dbQueries := str.service.GetQueryCount()
	cacheHits := totalRequests - dbQueries
	cacheHitRate := float64(cacheHits) / float64(totalRequests) * 100
	dbQueriesPerSecond := float64(dbQueries) / actualDuration.Seconds()

	// 计算响应时间统计
	avgResponseTime, p99ResponseTime, maxResponseTime := str.calculateStressResponseTimeStats(responseTimes)

	result := &StressTestResult{
		Strategy:           strategy,
		Config:             str.config,
		TotalRequests:      totalRequests,
		DBQueries:          dbQueries,
		CacheHitRate:       cacheHitRate,
		AvgResponseTime:    avgResponseTime,
		P99ResponseTime:    p99ResponseTime,
		MaxResponseTime:    maxResponseTime,
		ErrorCount:         errorCount,
		DBQueriesPerSecond: dbQueriesPerSecond,
		ResponseTimeSpikes: responseTimeSpikes,
		Timestamp:          time.Now(),
	}

	// 获取击穿防护统计
	if breakdownProtection != nil {
		stats := breakdownProtection.GetStats()
		result.SingleflightHits = stats.SingleflightHits
		result.LogicalExpireHits = stats.LogicalExpireHits
	}

	return result, nil
}

// calculateStressResponseTimeStats 计算压力测试响应时间统计
func (str *StressTestRunner) calculateStressResponseTimeStats(responseTimes []time.Duration) (avg, p99, max time.Duration) {
	if len(responseTimes) == 0 {
		return 0, 0, 0
	}

	// 排序
	for i := 0; i < len(responseTimes)-1; i++ {
		for j := 0; j < len(responseTimes)-i-1; j++ {
			if responseTimes[j] > responseTimes[j+1] {
				responseTimes[j], responseTimes[j+1] = responseTimes[j+1], responseTimes[j]
			}
		}
	}

	// 计算统计值
	var total time.Duration
	for _, rt := range responseTimes {
		total += rt
	}
	avg = total / time.Duration(len(responseTimes))

	p99Index := int(float64(len(responseTimes)) * 0.99)
	if p99Index >= len(responseTimes) {
		p99Index = len(responseTimes) - 1
	}
	p99 = responseTimes[p99Index]
	max = responseTimes[len(responseTimes)-1]

	return avg, p99, max
}

// printStressResult 打印压力测试结果
func (str *StressTestRunner) printStressResult(result *StressTestResult) {
	fmt.Printf("🔥 STRESS Test Result - %s:\n", result.Strategy)
	fmt.Printf("   Total Requests: %d\n", result.TotalRequests)
	fmt.Printf("   DB Queries: %d (%.2f queries/sec)\n", result.DBQueries, result.DBQueriesPerSecond)
	fmt.Printf("   Cache Hit Rate: %.2f%%\n", result.CacheHitRate)
	fmt.Printf("   Avg Response Time: %v\n", result.AvgResponseTime)
	fmt.Printf("   P99 Response Time: %v\n", result.P99ResponseTime)
	fmt.Printf("   Max Response Time: %v\n", result.MaxResponseTime)
	fmt.Printf("   Response Time Spikes (>100ms): %d\n", result.ResponseTimeSpikes)
	fmt.Printf("   Errors: %d\n", result.ErrorCount)

	if result.SingleflightHits > 0 {
		fmt.Printf("   🛡️  Singleflight Hits: %d\n", result.SingleflightHits)
	}
	if result.LogicalExpireHits > 0 {
		fmt.Printf("   ⏰ Logical Expire Hits: %d\n", result.LogicalExpireHits)
	}
}

// printStressComparisonReport 打印压力测试对比报告
func (str *StressTestRunner) printStressComparisonReport(results []*StressTestResult) {
	fmt.Println("\n🔥 CACHE BREAKDOWN STRESS TEST COMPARISON")
	fmt.Println("=" + fmt.Sprintf("%*s", 80, "="))

	fmt.Printf("%-20s %-12s %-12s %-15s %-15s %-12s\n",
		"Strategy", "DB Queries", "DB QPS", "Avg Response", "P99 Response", "Spikes")
	fmt.Println("-" + fmt.Sprintf("%*s", 100, "-"))

	for _, result := range results {
		fmt.Printf("%-20s %-12d %-12.1f %-15v %-15v %-12d\n",
			result.Strategy,
			result.DBQueries,
			result.DBQueriesPerSecond,
			result.AvgResponseTime,
			result.P99ResponseTime,
			result.ResponseTimeSpikes,
		)
	}

	fmt.Println()

	// 分析击穿防护效果
	if len(results) >= 2 {
		baseline := results[0] // 无防护作为基线

		fmt.Printf("🎯 BREAKDOWN PROTECTION EFFECTIVENESS:\n")
		for i := 1; i < len(results); i++ {
			result := results[i]

			dbReduction := float64(baseline.DBQueries-result.DBQueries) / float64(baseline.DBQueries) * 100
			spikeReduction := float64(baseline.ResponseTimeSpikes-result.ResponseTimeSpikes) / float64(baseline.ResponseTimeSpikes) * 100
			p99Improvement := float64(baseline.P99ResponseTime-result.P99ResponseTime) / float64(baseline.P99ResponseTime) * 100

			fmt.Printf("\n   %s vs %s:\n", result.Strategy, baseline.Strategy)
			fmt.Printf("   ✅ DB queries reduced: %.2f%%\n", dbReduction)
			fmt.Printf("   ✅ Response spikes reduced: %.2f%%\n", spikeReduction)
			fmt.Printf("   ✅ P99 response time improved: %.2f%%\n", p99Improvement)

			if result.SingleflightHits > 0 {
				protection_rate := float64(result.SingleflightHits) / float64(result.TotalRequests) * 100
				fmt.Printf("   🛡️  Singleflight protection rate: %.2f%%\n", protection_rate)
			}
		}
	}
}

func main() {
	// 极端击穿场景配置 - 修改关键参数使击穿更明显
	config := StressTestConfig{
		ConcurrentUsers:  50,                     // 减少并发，避免过多日志
		HotKeyCount:      1,                      // 只用1个热点Key，集中击穿压力
		CacheExpireEvery: 2 * time.Second,        // 更短的过期时间
		DBQueryDelay:     200 * time.Millisecond, // 减少DB延迟，避免卡死
		TestDuration:     10 * time.Second,       // 进一步缩短测试时间
		ExpireAllHotKeys: true,                   // 同时过期所有热点Key
		ExpireInterval:   15 * time.Second,       // 减少过期频率，每15秒一次
	}

	fmt.Println("🔥🔥🔥 CACHE BREAKDOWN STRESS TEST 🔥🔥🔥")
	fmt.Println("🎯 This test simulates EXTREME cache breakdown scenarios")
	fmt.Printf("   🔥 %d hot keys will expire SIMULTANEOUSLY every %v\n", config.HotKeyCount, config.ExpireInterval)
	fmt.Printf("   ⚡ %d concurrent users will hammer the same hot keys\n", config.ConcurrentUsers)
	fmt.Printf("   🐌 Each DB query takes %v (simulating heavy operations)\n", config.DBQueryDelay)
	fmt.Println()

	runner, err := NewStressTestRunner(config)
	if err != nil {
		log.Fatalf("Failed to create stress test runner: %v", err)
	}

	err = runner.RunStressTest()
	if err != nil {
		log.Fatalf("Stress test failed: %v", err)
	}

	fmt.Println("\n🎉 Stress test completed! Check the results above to see the breakdown protection effectiveness.")
}
