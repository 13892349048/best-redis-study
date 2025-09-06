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

// User 用户数据结构
type User struct {
	ID       int64     `json:"id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	CreateAt time.Time `json:"create_at"`
}

// UserService 用户服务（模拟数据源）
type UserService struct {
	// 模拟数据库查询计数
	queryCount int64
	// 模拟查询延迟
	queryDelay time.Duration
	// 数据库数据
	users map[int64]*User
	mu    sync.RWMutex
}

// NewUserService 创建用户服务
func NewUserService() *UserService {
	users := make(map[int64]*User)

	// 初始化一些测试数据
	for i := int64(1); i <= 1000; i++ {
		users[i] = &User{
			ID:       i,
			Name:     fmt.Sprintf("User%d", i),
			Email:    fmt.Sprintf("user%d@example.com", i),
			CreateAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}
	}

	return &UserService{
		queryDelay: 100 * time.Millisecond, // 模拟100ms查询延迟
		users:      users,
	}
}

// GetUser 获取用户（模拟数据库查询）
func (us *UserService) GetUser(ctx context.Context, userID int64) (*User, error) {
	// 增加查询计数
	atomic.AddInt64(&us.queryCount, 1)

	// 模拟查询延迟
	select {
	case <-time.After(us.queryDelay):
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	us.mu.RLock()
	user, exists := us.users[userID]
	us.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("user %d not found", userID)
	}

	// 返回副本
	userCopy := *user
	return &userCopy, nil
}

// GetQueryCount 获取查询次数
func (us *UserService) GetQueryCount() int64 {
	return atomic.LoadInt64(&us.queryCount)
}

// ResetQueryCount 重置查询计数
func (us *UserService) ResetQueryCount() {
	atomic.StoreInt64(&us.queryCount, 0)
}

// SetQueryDelay 设置查询延迟
func (us *UserService) SetQueryDelay(delay time.Duration) {
	us.queryDelay = delay
}

// TestScenario 测试场景
type TestScenario struct {
	Name            string
	ConcurrentUsers int
	RequestsPerUser int
	HotKeyRatio     float64 // 热点Key比例
	TestDuration    time.Duration
}

// TestResult 测试结果
type TestResult struct {
	Scenario          string        `json:"scenario"`
	TotalRequests     int64         `json:"total_requests"`
	DBQueries         int64         `json:"db_queries"`
	CacheHitRate      float64       `json:"cache_hit_rate"`
	AvgResponseTime   time.Duration `json:"avg_response_time"`
	P95ResponseTime   time.Duration `json:"p95_response_time"`
	P99ResponseTime   time.Duration `json:"p99_response_time"`
	ErrorCount        int64         `json:"error_count"`
	SingleflightHits  int64         `json:"singleflight_hits"`
	LogicalExpireHits int64         `json:"logical_expire_hits"`
}

func main7() {
	// 创建Redis客户端
	client, err := redisx.NewClient(&redisx.Config{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: 100,
	})
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	// 清理Redis
	client.FlushDB(context.Background())

	// 创建用户服务
	userService := NewUserService()

	fmt.Println("=== Day 7: 缓存击穿防护演示 ===")
	fmt.Println()

	// 测试场景列表
	scenarios := []TestScenario{
		{
			Name:            "无防护（基线）",
			ConcurrentUsers: 100,
			RequestsPerUser: 50,
			HotKeyRatio:     0.8, // 80%的请求访问少数热点Key
			TestDuration:    30 * time.Second,
		},
		{
			Name:            "单飞防护",
			ConcurrentUsers: 100,
			RequestsPerUser: 50,
			HotKeyRatio:     0.8,
			TestDuration:    30 * time.Second,
		},
		{
			Name:            "逻辑过期防护",
			ConcurrentUsers: 100,
			RequestsPerUser: 50,
			HotKeyRatio:     0.8,
			TestDuration:    30 * time.Second,
		},
	}

	var results []TestResult

	for _, scenario := range scenarios {
		fmt.Printf("🧪 测试场景: %s\n", scenario.Name)

		// 清理缓存
		client.FlushDB(context.Background())
		userService.ResetQueryCount()

		// 运行测试
		result := runScenario(client, userService, scenario)
		results = append(results, result)

		// 输出结果
		printResult(result)
		fmt.Println()

		// 等待一段时间再进行下一个测试
		time.Sleep(2 * time.Second)
	}

	// 输出对比报告
	printComparisonReport(results)

	// 演示击穿场景
	demonstrateBreakdownScenario(client, userService)
}

// runScenario 运行测试场景
func runScenario(client redis.Cmdable, userService *UserService, scenario TestScenario) TestResult {
	// 创建缓存组件
	redisCache := cache.NewRedisCache(client, cache.DefaultCacheAsideOptions())

	var breakdownProtection *cache.BreakdownProtection

	switch scenario.Name {
	case "单飞防护":
		opts := cache.DefaultBreakdownOptions()
		opts.EnableSingleflight = true
		opts.EnableLogicalExpire = false
		breakdownProtection = cache.NewBreakdownProtection(redisCache, opts)
	case "逻辑过期防护":
		opts := cache.DefaultBreakdownOptions()
		opts.EnableSingleflight = false
		opts.EnableLogicalExpire = true
		breakdownProtection = cache.NewBreakdownProtection(redisCache, opts)
	}

	// 统计变量
	var totalRequests int64
	var errorCount int64
	var responseTimes []time.Duration
	var responseTimeMu sync.Mutex

	// 创建用户加载函数
	userLoader := func(ctx context.Context, key string) (interface{}, error) {
		var userID int64
		fmt.Sscanf(key, "user:%d", &userID)
		return userService.GetUser(ctx, userID)
	}

	// 并发测试
	var wg sync.WaitGroup
	_ = time.Now() // 占位符，避免unused变量错误

	for i := 0; i < scenario.ConcurrentUsers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < scenario.RequestsPerUser; j++ {
				// 生成用户ID（模拟热点Key）
				var userID int64
				if rand.Float64() < scenario.HotKeyRatio {
					// 热点Key（1-10）
					userID = int64(rand.Intn(10) + 1)
				} else {
					// 普通Key（11-1000）
					userID = int64(rand.Intn(990) + 11)
				}

				key := fmt.Sprintf("user:%d", userID)

				requestStart := time.Now()
				atomic.AddInt64(&totalRequests, 1)

				var user User
				var err error

				if breakdownProtection != nil {
					// 使用击穿防护
					err = breakdownProtection.GetOrLoad(context.Background(), key, &user, userLoader)
				} else {
					// 直接使用缓存
					err = redisCache.Get(context.Background(), key, &user)
					if err == cache.ErrCacheMiss {
						// 缓存未命中，查询数据库
						userPtr, loadErr := userService.GetUser(context.Background(), userID)
						if loadErr != nil {
							err = loadErr
						} else {
							user = *userPtr
							// 写入缓存
							redisCache.Set(context.Background(), key, user, 10*time.Minute)
							err = nil
						}
					}
				}

				responseTime := time.Since(requestStart)

				responseTimeMu.Lock()
				responseTimes = append(responseTimes, responseTime)
				responseTimeMu.Unlock()

				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					log.Printf("Worker %d error: %v", workerID, err)
				}

				// 随机间隔
				time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// 计算统计信息
	dbQueries := userService.GetQueryCount()
	cacheHitRate := float64(totalRequests-dbQueries) / float64(totalRequests) * 100

	// 计算响应时间统计
	avgResponseTime, p95ResponseTime, p99ResponseTime := calculateResponseTimeStats(responseTimes)

	result := TestResult{
		Scenario:        scenario.Name,
		TotalRequests:   totalRequests,
		DBQueries:       dbQueries,
		CacheHitRate:    cacheHitRate,
		AvgResponseTime: avgResponseTime,
		P95ResponseTime: p95ResponseTime,
		P99ResponseTime: p99ResponseTime,
		ErrorCount:      errorCount,
	}

	// 获取击穿防护统计
	if breakdownProtection != nil {
		stats := breakdownProtection.GetStats()
		result.SingleflightHits = stats.SingleflightHits
		result.LogicalExpireHits = stats.LogicalExpireHits
	}

	return result
}

// calculateResponseTimeStats 计算响应时间统计
func calculateResponseTimeStats(responseTimes []time.Duration) (avg, p95, p99 time.Duration) {
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

	// 计算平均值
	var total time.Duration
	for _, rt := range responseTimes {
		total += rt
	}
	avg = total / time.Duration(len(responseTimes))

	// 计算P95和P99
	p95Index := int(float64(len(responseTimes)) * 0.95)
	p99Index := int(float64(len(responseTimes)) * 0.99)

	if p95Index >= len(responseTimes) {
		p95Index = len(responseTimes) - 1
	}
	if p99Index >= len(responseTimes) {
		p99Index = len(responseTimes) - 1
	}

	p95 = responseTimes[p95Index]
	p99 = responseTimes[p99Index]

	return avg, p95, p99
}

// printResult 打印测试结果
func printResult(result TestResult) {
	fmt.Printf("📊 测试结果:\n")
	fmt.Printf("   总请求数: %d\n", result.TotalRequests)
	fmt.Printf("   数据库查询: %d\n", result.DBQueries)
	fmt.Printf("   缓存命中率: %.2f%%\n", result.CacheHitRate)
	fmt.Printf("   平均响应时间: %v\n", result.AvgResponseTime)
	fmt.Printf("   P95响应时间: %v\n", result.P95ResponseTime)
	fmt.Printf("   P99响应时间: %v\n", result.P99ResponseTime)
	fmt.Printf("   错误数: %d\n", result.ErrorCount)

	if result.SingleflightHits > 0 {
		fmt.Printf("   单飞命中数: %d\n", result.SingleflightHits)
	}
	if result.LogicalExpireHits > 0 {
		fmt.Printf("   逻辑过期命中数: %d\n", result.LogicalExpireHits)
	}
}

// printComparisonReport 打印对比报告
func printComparisonReport(results []TestResult) {
	fmt.Println("📈 性能对比报告:")
	fmt.Println("┌─────────────────┬─────────┬───────────┬─────────────┬─────────────┬─────────────┐")
	fmt.Println("│ 场景           │ 请求数   │ DB查询数   │ 缓存命中率   │ 平均响应     │ P99响应     │")
	fmt.Println("├─────────────────┼─────────┼───────────┼─────────────┼─────────────┼─────────────┤")

	for _, result := range results {
		fmt.Printf("│ %-15s │ %7d │ %9d │ %10.2f%% │ %11v │ %11v │\n",
			result.Scenario,
			result.TotalRequests,
			result.DBQueries,
			result.CacheHitRate,
			result.AvgResponseTime,
			result.P99ResponseTime,
		)
	}

	fmt.Println("└─────────────────┴─────────┴───────────┴─────────────┴─────────────┴─────────────┘")
	fmt.Println()

	// 分析改进效果
	if len(results) >= 2 {
		baseline := results[0]
		for i := 1; i < len(results); i++ {
			result := results[i]
			dbReduction := float64(baseline.DBQueries-result.DBQueries) / float64(baseline.DBQueries) * 100
			responseImprovement := float64(baseline.AvgResponseTime-result.AvgResponseTime) / float64(baseline.AvgResponseTime) * 100

			fmt.Printf("🎯 %s vs %s:\n", result.Scenario, baseline.Scenario)
			fmt.Printf("   数据库查询减少: %.2f%%\n", dbReduction)
			fmt.Printf("   响应时间改善: %.2f%%\n", responseImprovement)
			fmt.Println()
		}
	}
}

// demonstrateBreakdownScenario 演示击穿场景
func demonstrateBreakdownScenario(client redis.Cmdable, userService *UserService) {
	fmt.Println("🔥 缓存击穿场景演示:")
	fmt.Println("   模拟热点Key过期瞬间的大量并发访问")
	fmt.Println()

	// 创建缓存组件
	redisCache := cache.NewRedisCache(client, cache.DefaultCacheAsideOptions())

	// 单飞防护
	singleflightOpts := cache.DefaultBreakdownOptions()
	singleflightOpts.EnableSingleflight = true
	singleflightOpts.EnableLogicalExpire = false
	singleflightProtection := cache.NewBreakdownProtection(redisCache, singleflightOpts)

	hotKey := "user:999" // 热点Key

	userLoader := func(ctx context.Context, key string) (interface{}, error) {
		fmt.Printf("🔍 Loading data from DB for key: %s\n", key)
		return userService.GetUser(ctx, 999)
	}

	// 先预热缓存
	var user User
	singleflightProtection.GetOrLoad(context.Background(), hotKey, &user, userLoader)

	// 删除缓存模拟过期
	client.Del(context.Background(), hotKey)
	userService.ResetQueryCount()

	fmt.Println("🚀 启动100个并发请求访问已过期的热点Key...")

	var wg sync.WaitGroup
	startTime := time.Now()

	// 启动100个并发请求
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			var user User
			err := singleflightProtection.GetOrLoad(context.Background(), hotKey, &user, userLoader)
			if err != nil {
				fmt.Printf("❌ Request %d failed: %v\n", id, err)
			} else {
				fmt.Printf("✅ Request %d success, got user: %s\n", id, user.Name)
			}
		}(i)
	}

	wg.Wait()

	totalTime := time.Since(startTime)
	dbQueries := userService.GetQueryCount()

	fmt.Printf("\n📊 击穿防护效果:\n")
	fmt.Printf("   总耗时: %v\n", totalTime)
	fmt.Printf("   数据库查询次数: %d (理想情况应该是1次)\n", dbQueries)

	// 获取单飞统计
	stats := singleflightProtection.GetStats()
	fmt.Printf("   单飞命中数: %d\n", stats.SingleflightHits)
	fmt.Printf("   单飞未命中数: %d\n", stats.SingleflightMisses)

	if dbQueries == 1 {
		fmt.Println("🎉 击穿防护成功！只有一个请求穿透到数据库")
	} else {
		fmt.Printf("⚠️  仍有 %d 个请求穿透到数据库\n", dbQueries)
	}
}
