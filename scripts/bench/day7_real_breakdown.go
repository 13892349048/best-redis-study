package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

// BreakdownSimulator 击穿模拟器
type BreakdownSimulator struct {
	client      redis.Cmdable
	dataService *SlowDataService
	hotKey      string
}

// SlowDataService 慢速数据服务（模拟复杂查询）
type SlowDataService struct {
	queryCount int64
	queryDelay time.Duration
	mu         sync.Mutex
}

func NewSlowDataService(delay time.Duration) *SlowDataService {
	return &SlowDataService{queryDelay: delay}
}

func (sds *SlowDataService) SlowQuery(ctx context.Context, key string) (interface{}, error) {
	count := atomic.AddInt64(&sds.queryCount, 1)

	fmt.Printf("🐌 DB Query #%d started for key: %s\n", count, key)

	// 模拟慢查询
	time.Sleep(sds.queryDelay)

	fmt.Printf("✅ DB Query #%d completed for key: %s\n", count, key)

	return map[string]interface{}{
		"key":      key,
		"data":     fmt.Sprintf("Heavy data for %s", key),
		"query_id": count,
		"time":     time.Now().Unix(),
	}, nil
}

func (sds *SlowDataService) GetQueryCount() int64 {
	return atomic.LoadInt64(&sds.queryCount)
}

func (sds *SlowDataService) Reset() {
	atomic.StoreInt64(&sds.queryCount, 0)
}

// simulateRealBreakdown 模拟真实的击穿场景
func simulateRealBreakdown(strategy string) {
	fmt.Printf("\n🔥🔥 REAL BREAKDOWN SIMULATION: %s 🔥🔥\n", strategy)

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

	// 清理缓存
	client.FlushDB(context.Background())

	// 创建慢速数据服务（500ms查询延迟）
	dataService := NewSlowDataService(500 * time.Millisecond)

	// 创建缓存组件
	redisCache := cache.NewRedisCache(client, cache.DefaultCacheAsideOptions())
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

	hotKey := "critical_data"
	loader := func(ctx context.Context, key string) (interface{}, error) {
		return dataService.SlowQuery(ctx, key)
	}

	// 预热阶段
	fmt.Printf("🔥 Phase 1: Pre-warming cache...\n")
	var preloadData interface{}
	if breakdownProtection != nil {
		breakdownProtection.GetOrLoad(context.Background(), hotKey, &preloadData, loader)
	} else {
		result, _ := dataService.SlowQuery(context.Background(), hotKey)
		redisCache.Set(context.Background(), hotKey, result, 1*time.Hour)
	}
	dataService.Reset()
	fmt.Printf("✅ Cache pre-warmed, DB queries reset\n\n")

	// 击穿阶段
	fmt.Printf("🔥 Phase 2: Simulating breakdown...\n")
	fmt.Printf("⚡ Deleting hot key: %s\n", hotKey)
	client.Del(context.Background(), hotKey)

	// 等待一小段时间确保删除生效
	time.Sleep(10 * time.Millisecond)

	// 并发访问阶段
	fmt.Printf("🚀 Phase 3: Launching concurrent access...\n")

	concurrentUsers := 50
	requestsPerUser := 2
	var wg sync.WaitGroup
	var requestCounter int64

	startTime := time.Now()

	// 启动并发访问
	for i := 0; i < concurrentUsers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < requestsPerUser; j++ {
				requestID := atomic.AddInt64(&requestCounter, 1)

				fmt.Printf("📡 Request #%d from worker %d started\n", requestID, workerID)
				requestStart := time.Now()

				var result interface{}
				var err error

				if breakdownProtection != nil {
					err = breakdownProtection.GetOrLoad(context.Background(), hotKey, &result, loader)
				} else {
					err = redisCache.Get(context.Background(), hotKey, &result)
					if err == cache.ErrCacheMiss {
						result, err = dataService.SlowQuery(context.Background(), hotKey)
						if err == nil {
							redisCache.Set(context.Background(), hotKey, result, 1*time.Hour)
						}
					}
				}

				responseTime := time.Since(requestStart)

				if err != nil {
					fmt.Printf("❌ Request #%d failed: %v\n", requestID, err)
				} else {
					fmt.Printf("✅ Request #%d completed in %v\n", requestID, responseTime)
				}

				// 小延迟模拟真实请求间隔
				time.Sleep(50 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()
	totalTime := time.Since(startTime)

	// 结果统计
	totalRequests := requestCounter
	dbQueries := dataService.GetQueryCount()

	fmt.Printf("\n📊 BREAKDOWN SIMULATION RESULTS:\n")
	fmt.Printf("   Strategy: %s\n", strategy)
	fmt.Printf("   Total Requests: %d\n", totalRequests)
	fmt.Printf("   DB Queries: %d\n", dbQueries)
	fmt.Printf("   Total Time: %v\n", totalTime)
	fmt.Printf("   DB Query Rate: %.2f%%\n", float64(dbQueries)/float64(totalRequests)*100)

	if breakdownProtection != nil {
		stats := breakdownProtection.GetStats()
		if stats.SingleflightHits > 0 {
			fmt.Printf("   🛡️  Singleflight Protected: %d requests\n", stats.SingleflightHits)
		}
		if stats.LogicalExpireHits > 0 {
			fmt.Printf("   ⏰ Logical Expire Hits: %d\n", stats.LogicalExpireHits)
		}
	}

	// 分析击穿情况
	if dbQueries == 1 {
		fmt.Printf("🎉 PERFECT! Only 1 DB query - breakdown effectively prevented!\n")
	} else if dbQueries <= 3 {
		fmt.Printf("👍 GOOD! %d DB queries - breakdown mostly prevented\n", dbQueries)
	} else if dbQueries <= 10 {
		fmt.Printf("⚠️  MODERATE! %d DB queries - some breakdown occurred\n", dbQueries)
	} else {
		fmt.Printf("❌ BREAKDOWN! %d DB queries - protection failed\n", dbQueries)
	}
}

func main777() {
	fmt.Println("🔥🔥🔥 REAL CACHE BREAKDOWN SIMULATION 🔥🔥🔥")
	fmt.Println("🎯 This test creates a REAL breakdown scenario:")
	fmt.Println("   • Hot key is deleted (simulating expiration)")
	fmt.Println("   • 50 concurrent users immediately access it")
	fmt.Println("   • Each DB query takes 500ms (heavy operation)")
	fmt.Println("   • We observe actual DB penetration")
	fmt.Println()

	strategies := []string{"no_protection", "singleflight", "logical_expire"}

	for i, strategy := range strategies {
		simulateRealBreakdown(strategy)

		if i < len(strategies)-1 {
			fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
			time.Sleep(2 * time.Second) // 休息一下
		}
	}

	fmt.Printf("\n🎉 Real breakdown simulation completed!\n")
	fmt.Printf("\n💡 Expected Results:\n")
	fmt.Printf("   • no_protection: Many DB queries (breakdown occurs)\n")
	fmt.Printf("   • singleflight: ~1 DB query (protection works)\n")
	fmt.Printf("   • logical_expire: ~0-1 DB queries (best protection)\n")
}
