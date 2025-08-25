package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"
)

func main() {
	fmt.Println("🧪 Redis缓存组件测试")
	fmt.Println("===================")

	// 创建Redis客户端
	client, err := redisx.NewClient(redisx.DefaultConfig())
	if err != nil {
		log.Fatalf("创建Redis客户端失败: %v", err)
	}
	defer client.Close()

	// 健康检查
	ctx := context.Background()
	if err := client.HealthCheck(ctx); err != nil {
		log.Fatalf("Redis健康检查失败: %v", err)
	}
	fmt.Println("✅ Redis连接正常")

	// 测试基础缓存功能
	testBasicCache(ctx, client)

	// 测试TTL抖动
	testTTLJitter(ctx, client)

	// 测试序列化器
	testSerializers(ctx, client)

	fmt.Println("\n✅ 所有测试通过！")
}

func testBasicCache(ctx context.Context, client *redisx.Client) {
	fmt.Println("\n📝 测试基础缓存功能...")

	// 创建Redis缓存
	redisCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:           30 * time.Second,
		TTLJitter:     0.1,
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "test",
	})

	// 测试数据
	testKey := "user:123"
	testValue := map[string]interface{}{
		"id":   123,
		"name": "测试用户",
		"age":  25,
	}

	// 设置缓存
	err := redisCache.Set(ctx, testKey, testValue, 10*time.Second)
	if err != nil {
		log.Fatalf("设置缓存失败: %v", err)
	}
	fmt.Printf("✅ 设置缓存成功: %s\n", testKey)

	// 获取缓存
	var result map[string]interface{}
	err = redisCache.Get(ctx, testKey, &result)
	if err != nil {
		log.Fatalf("获取缓存失败: %v", err)
	}
	fmt.Printf("✅ 获取缓存成功: %+v\n", result)

	// 检查TTL
	ttl, err := redisCache.GetWithTTL(ctx, testKey, &result)
	if err != nil {
		log.Fatalf("获取TTL失败: %v", err)
	}
	fmt.Printf("✅ 剩余TTL: %v\n", ttl)

	// 删除缓存
	err = redisCache.Del(ctx, testKey)
	if err != nil {
		log.Fatalf("删除缓存失败: %v", err)
	}
	fmt.Printf("✅ 删除缓存成功\n")

	// 验证已删除
	err = redisCache.Get(ctx, testKey, &result)
	if err != cache.ErrCacheMiss {
		log.Fatalf("缓存应该已被删除，但仍然存在")
	}
	fmt.Printf("✅ 验证缓存已删除\n")

	// 显示指标
	stats := redisCache.GetStats()
	if stats != nil {
		fmt.Printf("📊 缓存指标: 命中=%d, 未命中=%d, 命中率=%.2f%%\n",
			stats.Hits, stats.Misses, stats.HitRate*100)
	}
}

func testTTLJitter(ctx context.Context, client *redisx.Client) {
	fmt.Println("\n⏱️  测试TTL抖动功能...")

	// 创建带抖动的缓存
	redisCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:        10 * time.Second,
		TTLJitter:  0.3, // 30%抖动
		Serializer: &cache.StringSerializer{},
		Namespace:  "jitter_test",
	})

	// 设置多个key，观察TTL分布
	fmt.Printf("设置5个key，基准TTL=10s，抖动=30%%:\n")
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("test_key_%d", i)
		value := fmt.Sprintf("value_%d", i)

		err := redisCache.Set(ctx, key, value, 10*time.Second)
		if err != nil {
			log.Printf("设置key %s失败: %v", key, err)
			continue
		}

		// 获取实际TTL
		fullKey := fmt.Sprintf("jitter_test:%s", key)
		ttl := client.TTL(ctx, fullKey).Val()
		fmt.Printf("  %s: TTL = %v\n", key, ttl)
	}
	fmt.Printf("✅ TTL抖动测试完成，可以看到TTL在7-13秒之间分布\n")
}

func testSerializers(ctx context.Context, client *redisx.Client) {
	fmt.Println("\n🔧 测试序列化器...")

	// 测试JSON序列化器
	jsonCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:        5 * time.Second,
		Serializer: &cache.JSONSerializer{},
		Namespace:  "json_test",
	})

	complexData := map[string]interface{}{
		"string":  "测试字符串",
		"number":  42,
		"boolean": true,
		"array":   []int{1, 2, 3},
		"nested": map[string]string{
			"key": "value",
		},
	}

	err := jsonCache.Set(ctx, "complex", complexData, 5*time.Second)
	if err != nil {
		log.Fatalf("JSON序列化设置失败: %v", err)
	}

	var result map[string]interface{}
	err = jsonCache.Get(ctx, "complex", &result)
	if err != nil {
		log.Fatalf("JSON序列化获取失败: %v", err)
	}
	fmt.Printf("✅ JSON序列化测试通过: %+v\n", result)

	// 测试字符串序列化器
	stringCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:        5 * time.Second,
		Serializer: &cache.StringSerializer{},
		Namespace:  "string_test",
	})

	testString := "这是一个测试字符串"
	err = stringCache.Set(ctx, "simple", testString, 5*time.Second)
	if err != nil {
		log.Fatalf("字符串序列化设置失败: %v", err)
	}

	var resultString string
	err = stringCache.Get(ctx, "simple", &resultString)
	if err != nil {
		log.Fatalf("字符串序列化获取失败: %v", err)
	}
	fmt.Printf("✅ 字符串序列化测试通过: %s\n", resultString)
}
