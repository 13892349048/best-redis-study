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

	"github.com/redis/go-redis/v9"

	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"
)

// AvalancheUser 用户数据结构（雪崩演示专用）
type AvalancheUser struct {
	ID       int64     `json:"id"`
	Username string    `json:"username"`
	Email    string    `json:"email"`
	CreateAt time.Time `json:"create_at"`
}

// AvalancheUserService 用户服务（模拟数据源）
type AvalancheUserService struct {
	users map[int64]*AvalancheUser
	mu    sync.RWMutex

	// 模拟数据库负载
	loadTime     time.Duration
	errorRate    float64
	requestCount int64
}

// NewAvalancheUserService 创建用户服务
func NewAvalancheUserService() *AvalancheUserService {
	users := make(map[int64]*AvalancheUser)

	// 初始化一些测试用户
	for i := int64(1); i <= 1000; i++ {
		users[i] = &AvalancheUser{
			ID:       i,
			Username: fmt.Sprintf("user_%d", i),
			Email:    fmt.Sprintf("user_%d@example.com", i),
			CreateAt: time.Now().Add(-time.Duration(rand.Intn(365*24)) * time.Hour),
		}
	}

	return &AvalancheUserService{
		users:     users,
		loadTime:  100 * time.Millisecond, // 模拟数据库查询延迟
		errorRate: 0.1,                    // 10%错误率
	}
}

// GetUser 获取用户（模拟数据库查询）
func (s *AvalancheUserService) GetUser(ctx context.Context, userID int64) (*AvalancheUser, error) {
	atomic.AddInt64(&s.requestCount, 1)

	// 模拟数据库延迟
	time.Sleep(s.loadTime)

	// 模拟错误
	if rand.Float64() < s.errorRate {
		return nil, fmt.Errorf("database error for user %d", userID)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	user, exists := s.users[userID]
	if !exists {
		return nil, fmt.Errorf("user %d not found", userID)
	}

	// 返回副本避免并发修改
	return &AvalancheUser{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		CreateAt: user.CreateAt,
	}, nil
}

// GetRequestCount 获取请求计数
func (s *AvalancheUserService) GetRequestCount() int64 {
	return atomic.LoadInt64(&s.requestCount)
}

// SetLoadTime 设置负载时间（模拟数据库压力）
func (s *AvalancheUserService) SetLoadTime(duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadTime = duration
}

// SetErrorRate 设置错误率
func (s *AvalancheUserService) SetErrorRate(rate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorRate = rate
}

// LoadTest 负载测试结果
type LoadTest struct {
	Name            string        `json:"name"`
	Duration        time.Duration `json:"duration"`
	TotalRequests   int64         `json:"total_requests"`
	SuccessRequests int64         `json:"success_requests"`
	ErrorRequests   int64         `json:"error_requests"`
	AvgLatency      time.Duration `json:"avg_latency"`
	P95Latency      time.Duration `json:"p95_latency"`
	P99Latency      time.Duration `json:"p99_latency"`
	QPS             float64       `json:"qps"`
	DBRequests      int64         `json:"db_requests"`
	CacheHitRate    float64       `json:"cache_hit_rate"`
}

// LatencyRecorder 延迟记录器
type LatencyRecorder struct {
	latencies []time.Duration
	mu        sync.Mutex
}

// Record 记录延迟
func (lr *LatencyRecorder) Record(latency time.Duration) {
	lr.mu.Lock()
	defer lr.mu.Unlock()
	lr.latencies = append(lr.latencies, latency)
}

// GetStats 获取统计信息
func (lr *LatencyRecorder) GetStats() (avg, p95, p99 time.Duration) {
	lr.mu.Lock()
	defer lr.mu.Unlock()

	if len(lr.latencies) == 0 {
		return 0, 0, 0
	}

	// 排序
	for i := 0; i < len(lr.latencies)-1; i++ {
		for j := 0; j < len(lr.latencies)-1-i; j++ {
			if lr.latencies[j] > lr.latencies[j+1] {
				lr.latencies[j], lr.latencies[j+1] = lr.latencies[j+1], lr.latencies[j]
			}
		}
	}

	// 计算平均值
	var total time.Duration
	for _, latency := range lr.latencies {
		total += latency
	}
	avg = total / time.Duration(len(lr.latencies))

	// 计算P95和P99
	p95Index := int(float64(len(lr.latencies)) * 0.95)
	p99Index := int(float64(len(lr.latencies)) * 0.99)

	if p95Index >= len(lr.latencies) {
		p95Index = len(lr.latencies) - 1
	}
	if p99Index >= len(lr.latencies) {
		p99Index = len(lr.latencies) - 1
	}

	p95 = lr.latencies[p95Index]
	p99 = lr.latencies[p99Index]

	return avg, p95, p99
}

// runLoadTest 运行负载测试
func runLoadTest(name string, duration time.Duration, concurrency int,
	userService *AvalancheUserService, avalancheProtection *cache.AvalancheProtection) *LoadTest {

	fmt.Printf("开始负载测试: %s (并发:%d, 时长:%v)\n", name, concurrency, duration)

	var (
		totalRequests   int64
		successRequests int64
		errorRequests   int64
		wg              sync.WaitGroup
		recorder        = &LatencyRecorder{}
	)

	// 重置数据库请求计数
	dbRequestsBefore := userService.GetRequestCount()

	// 获取缓存统计（如果可用）
	var cacheStatsBefore *cache.CacheStats
	if stats := avalancheProtection.GetStats(); stats != nil {
		if cacheData, ok := stats["cache"].(*cache.CacheStats); ok {
			cacheStatsBefore = cacheData
		}
	}

	startTime := time.Now()
	endTime := startTime.Add(duration)

	// 启动并发goroutine
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for time.Now().Before(endTime) {
				// 随机选择用户ID
				userID := int64(rand.Intn(1000)) + 1
				key := fmt.Sprintf("user:%d", userID)

				atomic.AddInt64(&totalRequests, 1)

				// 记录请求开始时间
				requestStart := time.Now()

				// 调用缓存获取用户
				var user AvalancheUser
				err := avalancheProtection.GetOrLoad(context.Background(), key, &user,
					func(ctx context.Context, key string) (interface{}, error) {
						userIDFromKey := userID // 从key解析userID
						return userService.GetUser(ctx, userIDFromKey)
					})

				// 记录延迟
				latency := time.Since(requestStart)
				recorder.Record(latency)

				if err != nil {
					atomic.AddInt64(&errorRequests, 1)
					if err != cache.ErrCircuitBreakerOpen && err != cache.ErrDegraded {
						log.Printf("Request error: %v", err)
					}
				} else {
					atomic.AddInt64(&successRequests, 1)
				}

				// 控制请求频率，避免过于密集
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// 等待所有goroutine完成
	wg.Wait()

	actualDuration := time.Since(startTime)
	dbRequestsAfter := userService.GetRequestCount()
	dbRequests := dbRequestsAfter - dbRequestsBefore

	// 计算延迟统计
	avgLatency, p95Latency, p99Latency := recorder.GetStats()

	// 计算缓存命中率
	var cacheHitRate float64
	if cacheStatsBefore != nil {
		if stats := avalancheProtection.GetStats(); stats != nil {
			if cacheData, ok := stats["cache"].(*cache.CacheStats); ok {
				totalCacheRequests := cacheData.Hits + cacheData.Misses -
					(cacheStatsBefore.Hits + cacheStatsBefore.Misses)
				if totalCacheRequests > 0 {
					cacheHits := cacheData.Hits - cacheStatsBefore.Hits
					cacheHitRate = float64(cacheHits) / float64(totalCacheRequests)
				}
			}
		}
	}

	result := &LoadTest{
		Name:            name,
		Duration:        actualDuration,
		TotalRequests:   totalRequests,
		SuccessRequests: successRequests,
		ErrorRequests:   errorRequests,
		AvgLatency:      avgLatency,
		P95Latency:      p95Latency,
		P99Latency:      p99Latency,
		QPS:             float64(totalRequests) / actualDuration.Seconds(),
		DBRequests:      dbRequests,
		CacheHitRate:    cacheHitRate,
	}

	fmt.Printf("测试完成: %s\n", name)
	fmt.Printf("  总请求数: %d, 成功: %d, 失败: %d\n",
		totalRequests, successRequests, errorRequests)
	fmt.Printf("  QPS: %.2f, 数据库请求: %d\n", result.QPS, dbRequests)
	fmt.Printf("  平均延迟: %v, P95: %v, P99: %v\n",
		avgLatency, p95Latency, p99Latency)
	fmt.Printf("  缓存命中率: %.2f%%\n", cacheHitRate*100)

	// 打印熔断器状态
	if cbStats := avalancheProtection.GetCircuitBreakerStats(); cbStats != nil {
		fmt.Printf("  熔断器状态: %v\n", cbStats)
	}

	fmt.Println()

	return result
}

// simulateAvalanche 模拟雪崩场景
func simulateAvalanche(redisClient redis.Cmdable, userService *AvalancheUserService) {
	fmt.Println("=== 模拟缓存雪崩场景 ===")

	// 1. 准备大量数据，设置相同的过期时间（模拟雪崩）
	fmt.Println("1. 准备缓存数据（设置相同TTL，模拟雪崩）...")

	baseCache := cache.NewRedisCache(redisClient, &cache.CacheAsideOptions{
		TTL:           30 * time.Second,
		TTLJitter:     0, // 不使用抖动，制造雪崩
		EmptyValueTTL: 5 * time.Second,
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "avalanche_test",
	})

	// 批量设置缓存，模拟同时过期
	ctx := context.Background()
	for i := int64(1); i <= 100; i++ {
		user := &AvalancheUser{
			ID:       i,
			Username: fmt.Sprintf("user_%d", i),
			Email:    fmt.Sprintf("user_%d@example.com", i),
			CreateAt: time.Now(),
		}

		key := fmt.Sprintf("user:%d", i)
		if err := baseCache.Set(ctx, key, user, 30*time.Second); err != nil {
			log.Printf("Set cache failed: %v", err)
		}
	}

	fmt.Println("2. 等待缓存数据接近过期...")
	time.Sleep(25 * time.Second)

	fmt.Println("3. 开始雪崩测试（无防护）...")

	// 增加数据库负载，模拟雪崩时的压力
	userService.SetLoadTime(200 * time.Millisecond)
	userService.SetErrorRate(0.3) // 30%错误率

	// 创建简单的雪崩防护（仅用于测试，不启用防护功能）
	simpleConfig := cache.DefaultAvalancheProtectionConfig()
	simpleConfig.EnableTTLSharding = false
	simpleConfig.CircuitBreakerConfig.FailureRate = 1.0 // 不会触发熔断
	simpleConfig.DegradeFunc = nil

	simpleProtection := cache.NewAvalancheProtection(baseCache, simpleConfig)
	defer simpleProtection.Close()

	// 运行负载测试（无防护）
	result1 := runLoadTest("雪崩场景-无防护", 30*time.Second, 50, userService, simpleProtection)

	fmt.Println("4. 等待系统恢复...")
	time.Sleep(10 * time.Second)

	// 重置服务状态
	userService.SetLoadTime(100 * time.Millisecond)
	userService.SetErrorRate(0.1)

	// 清理缓存
	for i := int64(1); i <= 100; i++ {
		key := fmt.Sprintf("user:%d", i)
		redisClient.Del(ctx, fmt.Sprintf("avalanche_test:%s", key))
	}

	fmt.Println("5. 使用雪崩防护重新测试...")

	// 创建带雪崩防护的缓存
	avalancheConfig := cache.DefaultAvalancheProtectionConfig()
	avalancheConfig.TTLJitter = 0.2 // 20%抖动
	avalancheConfig.ShardCount = 10
	avalancheConfig.ShardInterval = 5 * time.Second
	avalancheConfig.FailureRate = 0.5
	avalancheConfig.MinRequestCount = 10
	avalancheConfig.OpenTimeout = 10 * time.Second

	// 设置降级函数
	avalancheConfig.DegradeFunc = func(ctx context.Context, key string) (interface{}, error) {
		// 返回默认用户数据
		return &AvalancheUser{
			ID:       0,
			Username: "default_user",
			Email:    "default@example.com",
			CreateAt: time.Now(),
		}, nil
	}

	protectedCache := cache.NewRedisCache(redisClient, &cache.CacheAsideOptions{
		TTL:           30 * time.Second,
		TTLJitter:     0.2, // 20%抖动
		EmptyValueTTL: 5 * time.Second,
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "protected_test",
	})

	avalancheProtection := cache.NewAvalancheProtection(protectedCache, avalancheConfig)

	// 批量设置缓存（带分片TTL）
	items := make(map[string]interface{})
	for i := int64(1); i <= 100; i++ {
		user := &AvalancheUser{
			ID:       i,
			Username: fmt.Sprintf("user_%d", i),
			Email:    fmt.Sprintf("user_%d@example.com", i),
			CreateAt: time.Now(),
		}
		key := fmt.Sprintf("user:%d", i)
		items[key] = user
	}

	if err := avalancheProtection.BatchSetWithSharding(ctx, items, 30*time.Second); err != nil {
		log.Printf("Batch set with sharding failed: %v", err)
	}

	fmt.Println("6. 等待缓存数据接近过期...")
	time.Sleep(25 * time.Second)

	// 增加数据库负载
	userService.SetLoadTime(200 * time.Millisecond)
	userService.SetErrorRate(0.3)

	fmt.Println("7. 开始雪崩测试（有防护）...")

	// 运行负载测试（有防护）
	result2 := runLoadTest("雪崩场景-有防护", 30*time.Second, 50, userService, avalancheProtection)

	// 输出对比结果
	fmt.Println("=== 雪崩测试对比结果 ===")

	results := []*LoadTest{result1, result2}
	for _, result := range results {
		if result != nil {
			fmt.Printf("%s:\n", result.Name)
			fmt.Printf("  QPS: %.2f\n", result.QPS)
			fmt.Printf("  成功率: %.2f%%\n",
				float64(result.SuccessRequests)/float64(result.TotalRequests)*100)
			fmt.Printf("  平均延迟: %v\n", result.AvgLatency)
			fmt.Printf("  P95延迟: %v\n", result.P95Latency)
			fmt.Printf("  P99延迟: %v\n", result.P99Latency)
			fmt.Printf("  数据库请求: %d\n", result.DBRequests)
			fmt.Printf("  缓存命中率: %.2f%%\n", result.CacheHitRate*100)
			fmt.Println()
		}
	}

	// 保存结果到文件
	resultData := map[string]*LoadTest{
		"no_protection":   result1,
		"with_protection": result2,
	}

	if data, err := json.MarshalIndent(resultData, "", "  "); err == nil {
		filename := fmt.Sprintf("benchmark_results/avalanche_benchmark_%s.json",
			time.Now().Format("20060102_150405"))
		if err := writeToFile(filename, data); err != nil {
			log.Printf("保存结果失败: %v", err)
		} else {
			fmt.Printf("结果已保存到: %s\n", filename)
		}
	}

	// 清理资源
	avalancheProtection.Close()
}

// demonstrateFeatures 演示雪崩防护功能
func demonstrateFeatures() {
	fmt.Println("=== 雪崩防护功能演示 ===")

	// 连接Redis
	client, err := redisx.NewClient(&redisx.Config{
		Addr:         "localhost:6379",
		Password:     "",
		DB:           0,
		PoolSize:     10,
		MinIdleConns: 5,
	})
	if err != nil {
		log.Fatalf("连接Redis失败: %v", err)
	}
	defer client.Close()

	// 创建用户服务
	userService := NewAvalancheUserService()

	// 1. TTL抖动演示
	fmt.Println("1. TTL抖动演示")
	demonstrateTTLJitter(context.Background(), client)

	// 2. 分片过期演示
	fmt.Println("2. 分片过期演示")
	demonstrateShardedExpiration(client)

	// 3. 熔断器演示
	fmt.Println("3. 熔断器演示")
	demonstrateCircuitBreaker(client, userService)

	// 4. 预热演示
	fmt.Println("4. 预热演示")
	demonstrateWarmup(client, userService)

	// 5. 完整雪崩场景演示
	simulateAvalanche(client, userService)
}

// demonstrateTTLJitter 演示TTL抖动
func demonstrateTTLJitter(ctx context.Context, client redis.Cmdable) {
	fmt.Println("设置10个相同的key，观察TTL抖动效果...")

	cacheInstance := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:        60 * time.Second,
		TTLJitter:  0.3, // 30%抖动
		Serializer: &cache.JSONSerializer{},
		Namespace:  "jitter_demo",
	})

	// 设置多个key
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("test_key_%d", i)
		value := fmt.Sprintf("value_%d", i)

		if err := cacheInstance.Set(ctx, key, value, 60*time.Second); err != nil {
			log.Printf("Set failed: %v", err)
			continue
		}
	}

	// 检查TTL
	fmt.Println("各key的TTL (应该有抖动):")
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("jitter_demo:test_key_%d", i)
		ttl := client.TTL(ctx, key).Val()
		fmt.Printf("  %s: %v\n", key, ttl)
	}

	// 清理
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("jitter_demo:test_key_%d", i)
		client.Del(ctx, key)
	}

	fmt.Println()
}

// demonstrateShardedExpiration 演示分片过期
func demonstrateShardedExpiration(client redis.Cmdable) {
	fmt.Println("设置分片过期，观察不同key的过期时间分布...")

	config := cache.DefaultAvalancheProtectionConfig()
	config.ShardCount = 5
	config.ShardInterval = 10 * time.Second
	config.TTLJitter = 0.1

	cacheInstance := cache.NewRedisCache(client, config.CacheAsideOptions)
	protection := cache.NewAvalancheProtection(cacheInstance, config)
	defer protection.Close()

	ctx := context.Background()

	// 批量设置不同的key
	items := make(map[string]interface{})
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("shard_key_%d", i)
		items[key] = fmt.Sprintf("value_%d", i)
	}

	if err := protection.BatchSetWithSharding(ctx, items, 60*time.Second); err != nil {
		log.Printf("Batch set failed: %v", err)
		return
	}

	// 检查TTL分布
	fmt.Println("各key的TTL (应该分布在不同时间段):")
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("cache:shard_key_%d", i)
		ttl := client.TTL(ctx, key).Val()
		fmt.Printf("  %s: %v\n", key, ttl)
	}

	// 清理
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("cache:shard_key_%d", i)
		client.Del(ctx, key)
	}

	fmt.Println()
}

// demonstrateCircuitBreaker 演示熔断器
func demonstrateCircuitBreaker(client redis.Cmdable, userService *AvalancheUserService) {
	fmt.Println("演示熔断器功能...")

	config := cache.DefaultAvalancheProtectionConfig()
	config.FailureThreshold = 5
	config.FailureRate = 0.5 // 50%失败率触发熔断
	config.MinRequestCount = 10
	config.OpenTimeout = 5 * time.Second

	// 设置降级函数
	config.DegradeFunc = func(ctx context.Context, key string) (interface{}, error) {
		return &AvalancheUser{
			ID:       -1,
			Username: "degraded_user",
			Email:    "degraded@example.com",
			CreateAt: time.Now(),
		}, nil
	}

	cacheInstance := cache.NewRedisCache(client, config.CacheAsideOptions)
	protection := cache.NewAvalancheProtection(cacheInstance, config)
	defer protection.Close()

	// 设置高错误率模拟故障
	userService.SetErrorRate(0.8) // 80%错误率

	fmt.Println("发送请求触发熔断器...")

	ctx := context.Background()
	var successCount, errorCount int

	// 发送请求直到熔断器打开
	for i := 0; i < 30; i++ {
		var user AvalancheUser
		err := protection.GetOrLoad(ctx, fmt.Sprintf("user:%d", i%10+1), &user,
			func(ctx context.Context, key string) (interface{}, error) {
				userID := int64(i%10 + 1)
				return userService.GetUser(ctx, userID)
			})

		if err == nil {
			successCount++
			if user.Username == "degraded_user" {
				fmt.Printf("  请求%d: 降级响应\n", i+1)
			} else {
				fmt.Printf("  请求%d: 正常响应\n", i+1)
			}
		} else {
			errorCount++
			if err == cache.ErrCircuitBreakerOpen {
				fmt.Printf("  请求%d: 熔断器打开\n", i+1)
			} else {
				fmt.Printf("  请求%d: 错误 - %v\n", i+1, err)
			}
		}

		// 打印熔断器状态
		if (i+1)%5 == 0 {
			stats := protection.GetCircuitBreakerStats()
			fmt.Printf("    熔断器状态: %v\n", stats["state"])
		}

		time.Sleep(200 * time.Millisecond)
	}

	fmt.Printf("测试完成 - 成功: %d, 失败: %d\n", successCount, errorCount)

	// 等待熔断器恢复
	fmt.Println("等待熔断器恢复...")
	time.Sleep(6 * time.Second)

	// 恢复正常错误率
	userService.SetErrorRate(0.1)

	// 测试恢复后的请求
	fmt.Println("测试熔断器恢复后的请求...")
	for i := 0; i < 5; i++ {
		var user AvalancheUser
		err := protection.GetOrLoad(ctx, fmt.Sprintf("user:%d", i+1), &user,
			func(ctx context.Context, key string) (interface{}, error) {
				return userService.GetUser(ctx, int64(i+1))
			})

		if err == nil {
			fmt.Printf("  恢复请求%d: 成功\n", i+1)
		} else {
			fmt.Printf("  恢复请求%d: 失败 - %v\n", i+1, err)
		}
	}

	stats := protection.GetCircuitBreakerStats()
	fmt.Printf("最终熔断器状态: %v\n", stats)

	fmt.Println()
}

// demonstrateWarmup 演示预热功能
func demonstrateWarmup(client redis.Cmdable, userService *AvalancheUserService) {
	fmt.Println("演示缓存预热功能...")

	config := cache.DefaultAvalancheProtectionConfig()
	config.EnableWarmup = true
	config.WarmupInterval = 10 * time.Second
	config.WarmupKeys = []string{"user:1", "user:2", "user:3", "user:4", "user:5"}

	cacheInstance := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:        30 * time.Second,
		Serializer: &cache.JSONSerializer{},
		Namespace:  "warmup_demo",
	})

	protection := cache.NewAvalancheProtection(cacheInstance, config)
	defer protection.Close()

	// 设置预热加载器
	protection.SetWarmupKeys(config.WarmupKeys, func(ctx context.Context, key string) (interface{}, error) {
		// 从key解析userID
		var userID int64
		if _, err := fmt.Sscanf(key, "user:%d", &userID); err != nil {
			return nil, fmt.Errorf("invalid key format: %s", key)
		}
		return userService.GetUser(ctx, userID)
	})

	// 清理可能存在的缓存
	ctx := context.Background()
	for _, key := range config.WarmupKeys {
		client.Del(ctx, fmt.Sprintf("warmup_demo:%s", key))
	}

	fmt.Println("检查预热前缓存状态...")
	for _, key := range config.WarmupKeys {
		exists := client.Exists(ctx, fmt.Sprintf("warmup_demo:%s", key)).Val()
		fmt.Printf("  %s: 存在=%d\n", key, exists)
	}

	fmt.Println("执行手动预热...")
	protection.ForceWarmup()

	// 等待预热完成
	time.Sleep(2 * time.Second)

	fmt.Println("检查预热后缓存状态...")
	for _, key := range config.WarmupKeys {
		exists := client.Exists(ctx, fmt.Sprintf("warmup_demo:%s", key)).Val()
		ttl := client.TTL(ctx, fmt.Sprintf("warmup_demo:%s", key)).Val()
		fmt.Printf("  %s: 存在=%d, TTL=%v\n", key, exists, ttl)
	}

	// 清理
	for _, key := range config.WarmupKeys {
		client.Del(ctx, fmt.Sprintf("warmup_demo:%s", key))
	}

	fmt.Println()
}

// writeToFile 写入文件
func writeToFile(filename string, data []byte) error {
	// 创建目录
	if err := createDir("benchmark_results"); err != nil {
		return err
	}

	// 写入文件
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.Write(data)
	return err
}

// createDir 创建目录
func createDir(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return os.MkdirAll(dir, 0755)
	}
	return nil
}

func main() {
	rand.Seed(time.Now().UnixNano())

	fmt.Println("Redis 缓存雪崩防护演示")
	fmt.Println("======================")

	demonstrateFeatures()

	fmt.Println("演示完成！")
}
