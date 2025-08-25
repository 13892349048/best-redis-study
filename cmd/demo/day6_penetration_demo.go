package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"what_redis_can_do/internal/bloom"
	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

// 模拟的数据源
type DataSource struct {
	data map[string]string
	mu   sync.RWMutex
}

func NewDataSource() *DataSource {
	return &DataSource{
		data: make(map[string]string),
	}
}

func (ds *DataSource) Get(key string) (string, error) {
	ds.mu.RLock()
	defer ds.mu.RUnlock()

	// 模拟查询延迟
	time.Sleep(10 * time.Millisecond)

	if value, exists := ds.data[key]; exists {
		return value, nil
	}

	return "", fmt.Errorf("data not found: %s", key)
}

func (ds *DataSource) Set(key, value string) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.data[key] = value
}

func main6() {
	fmt.Println("=== Day 6: 缓存穿透防护演示 ===")

	// 创建Redis客户端
	client, err := redisx.NewClient(&redisx.Config{
		Addr: "localhost:6379",
		DB:   6, // 使用DB 6
	})
	if err != nil {
		log.Fatal("Failed to create Redis client:", err)
	}
	defer client.Close()

	// 清理测试数据
	client.FlushDB(context.Background())

	// 创建数据源
	dataSource := NewDataSource()

	// 预设一些数据
	for i := 1; i <= 1000; i++ {
		dataSource.Set(fmt.Sprintf("user:%d", i), fmt.Sprintf("User %d", i))
	}

	fmt.Println("\n1. 标准布隆过滤器演示")
	demoStandardBloomFilter(client)

	fmt.Println("\n2. 计数布隆过滤器演示")
	demoCountingBloomFilter(client)

	fmt.Println("\n3. 加权布隆过滤器演示")
	demoWeightedBloomFilter(client)

	fmt.Println("\n4. 缓存穿透防护演示")
	demoPenetrationProtection(client, dataSource)

	fmt.Println("\n5. 压力测试对比")
	benchmarkPenetrationProtection(client, dataSource)
}

// 标准布隆过滤器演示
func demoStandardBloomFilter(client redis.Cmdable) {
	config := &bloom.BloomConfig{
		Name:              "demo_standard",
		ExpectedElements:  10000,
		FalsePositiveRate: 0.01,
		Namespace:         "demo",
	}

	bf, err := bloom.NewStandardBloomFilter(client, config)
	if err != nil {
		log.Fatal("Failed to create bloom filter:", err)
	}
	defer bf.Close()

	ctx := context.Background()

	// 添加一些元素
	keys := []string{"user:1", "user:2", "user:3", "user:100", "user:999"}
	err = bf.AddMultiple(ctx, keys)
	if err != nil {
		log.Fatal("Failed to add keys:", err)
	}

	// 测试存在的元素
	fmt.Println("测试存在的元素:")
	for _, key := range keys {
		exists, err := bf.Test(ctx, key)
		if err != nil {
			log.Printf("Error testing key %s: %v", key, err)
			continue
		}
		fmt.Printf("  %s: %t\n", key, exists)
	}

	// 测试不存在的元素
	fmt.Println("测试不存在的元素:")
	nonExistentKeys := []string{"user:9999", "user:8888", "user:7777"}
	for _, key := range nonExistentKeys {
		exists, err := bf.Test(ctx, key)
		if err != nil {
			log.Printf("Error testing key %s: %v", key, err)
			continue
		}
		fmt.Printf("  %s: %t\n", key, exists)
	}

	// 获取布隆过滤器信息
	info, err := bf.Info(ctx)
	if err != nil {
		log.Printf("Error getting bloom filter info: %v", err)
	} else {
		fmt.Printf("布隆过滤器信息:\n")
		fmt.Printf("  位数组大小: %d\n", info.BitSize)
		fmt.Printf("  哈希函数数量: %d\n", info.HashFunctions)
		fmt.Printf("  预期元素数量: %d\n", info.ExpectedElements)
		fmt.Printf("  预期假阳性率: %.4f\n", info.FalsePositiveRate)
		fmt.Printf("  内存使用: %d bytes\n", info.MemoryUsage)
	}
}

// 计数布隆过滤器演示
func demoCountingBloomFilter(client redis.Cmdable) {
	config := &bloom.BloomConfig{
		Name:              "demo_counting",
		ExpectedElements:  1000,
		FalsePositiveRate: 0.01,
		Namespace:         "demo",
	}

	cbf, err := bloom.NewCountingBloomFilter(client, config)
	if err != nil {
		log.Fatal("Failed to create counting bloom filter:", err)
	}
	defer cbf.Close()

	ctx := context.Background()

	// 添加一些元素
	keys := []string{"item:1", "item:2", "item:3"}
	fmt.Println("添加元素:")
	for _, key := range keys {
		err := cbf.Add(ctx, key)
		if err != nil {
			log.Printf("Error adding key %s: %v", key, err)
			continue
		}
		fmt.Printf("  添加: %s\n", key)
	}

	// 重复添加某个元素
	fmt.Println("重复添加元素:")
	for i := 0; i < 3; i++ {
		err := cbf.Add(ctx, "item:1")
		if err != nil {
			log.Printf("Error adding key item:1: %v", err)
			continue
		}
		count, _ := cbf.Count(ctx, "item:1")
		fmt.Printf("  item:1 计数: %d\n", count)
	}

	// 测试元素
	fmt.Println("测试元素:")
	for _, key := range keys {
		exists, err := cbf.Test(ctx, key)
		if err != nil {
			log.Printf("Error testing key %s: %v", key, err)
			continue
		}
		count, _ := cbf.Count(ctx, key)
		fmt.Printf("  %s: 存在=%t, 计数=%d\n", key, exists, count)
	}

	// 删除元素
	fmt.Println("删除元素:")
	err = cbf.Remove(ctx, "item:1")
	if err != nil {
		log.Printf("Error removing key item:1: %v", err)
	} else {
		exists, _ := cbf.Test(ctx, "item:1")
		count, _ := cbf.Count(ctx, "item:1")
		fmt.Printf("  删除item:1后: 存在=%t, 计数=%d\n", exists, count)
	}
}

// 加权布隆过滤器演示
func demoWeightedBloomFilter(client redis.Cmdable) {
	config := &bloom.WeightedBloomConfig{
		BloomConfig:    bloom.DefaultBloomConfig("demo_weighted"),
		MaxWeight:      100.0,
		WeightDecay:    0.95,
		TimeWindowSecs: 60,
		BucketSize:     100,
	}

	wbf, err := bloom.NewWeightedBloomFilter(client, config)
	if err != nil {
		log.Fatal("Failed to create weighted bloom filter:", err)
	}
	defer wbf.Close()

	ctx := context.Background()

	// 添加带权重的元素
	fmt.Println("添加带权重的元素:")
	weights := map[string]float64{
		"hot_user:1":    80.0,
		"normal_user:1": 20.0,
		"cold_user:1":   5.0,
	}

	for key, weight := range weights {
		err := wbf.AddWithWeight(ctx, key, weight)
		if err != nil {
			log.Printf("Error adding weighted key %s: %v", key, err)
			continue
		}
		fmt.Printf("  添加: %s (权重: %.1f)\n", key, weight)
	}

	// 测试权重
	fmt.Println("测试元素权重:")
	for key := range weights {
		exists, weight, err := wbf.TestWithWeight(ctx, key)
		if err != nil {
			log.Printf("Error testing weighted key %s: %v", key, err)
			continue
		}
		fmt.Printf("  %s: 存在=%t, 权重=%.2f\n", key, exists, weight)
	}

	// 更新权重
	fmt.Println("更新权重:")
	err = wbf.UpdateWeight(ctx, "normal_user:1", 50.0)
	if err != nil {
		log.Printf("Error updating weight: %v", err)
	} else {
		weight, _ := wbf.GetWeight(ctx, "normal_user:1")
		fmt.Printf("  normal_user:1 新权重: %.2f\n", weight)
	}
}

// 缓存穿透防护演示
func demoPenetrationProtection(client redis.Cmdable, dataSource *DataSource) {
	// 创建缓存
	redisCache := cache.NewRedisCache(client, cache.DefaultCacheAsideOptions())

	// 创建穿透防护
	protectionOpts := cache.DefaultPenetrationOptions()
	protection, err := cache.NewPenetrationProtection(redisCache, client, protectionOpts)
	if err != nil {
		log.Fatal("Failed to create penetration protection:", err)
	}
	defer protection.Close()

	// 创建带防护的缓存装饰器
	cacheAside := cache.NewCacheAsideWithProtection(redisCache, nil, protection)

	ctx := context.Background()

	// 数据加载函数
	loader := func(ctx context.Context, key string) (interface{}, error) {
		fmt.Printf("  [数据源查询] %s\n", key)
		return dataSource.Get(key)
	}

	// 预热布隆过滤器
	fmt.Println("预热布隆过滤器:")
	existingKeys := []string{"user:1", "user:2", "user:3", "user:100", "user:999"}
	err = protection.PrewarmBloomFilter(ctx, existingKeys)
	if err != nil {
		log.Printf("Error prewarming bloom filter: %v", err)
	} else {
		fmt.Printf("  预热了 %d 个存在的key\n", len(existingKeys))
	}

	// 测试场景1: 正常查询（存在的数据）
	fmt.Println("\n场景1: 查询存在的数据")
	var result string
	err = cacheAside.GetOrLoad(ctx, "user:1", &result, loader)
	if err != nil {
		log.Printf("Error loading user:1: %v", err)
	} else {
		fmt.Printf("  结果: %s\n", result)
	}

	// 再次查询相同数据（应该命中缓存）
	fmt.Println("再次查询相同数据（缓存命中）:")
	err = cacheAside.GetOrLoad(ctx, "user:1", &result, loader)
	if err != nil {
		log.Printf("Error loading user:1: %v", err)
	} else {
		fmt.Printf("  结果: %s\n", result)
	}

	// 测试场景2: 查询不存在的数据（布隆过滤器拦截）
	fmt.Println("\n场景2: 查询不存在的数据（布隆过滤器拦截）")
	err = cacheAside.GetOrLoad(ctx, "user:99999", &result, loader)
	if err != nil {
		fmt.Printf("  预期错误: %v\n", err)
	}

	// 测试场景3: 查询未在布隆过滤器中的已存在数据
	fmt.Println("\n场景3: 查询未在布隆过滤器中的已存在数据")
	err = cacheAside.GetOrLoad(ctx, "user:500", &result, loader)
	if err != nil {
		log.Printf("Error loading user:500: %v", err)
	} else {
		fmt.Printf("  结果: %s\n", result)
	}

	// 获取布隆过滤器统计信息
	info, err := protection.GetBloomFilterInfo(ctx)
	if err != nil {
		log.Printf("Error getting bloom filter info: %v", err)
	} else {
		fmt.Printf("\n布隆过滤器统计:\n")
		fmt.Printf("  预期假阳性率: %.4f\n", info.FalsePositiveRate)
		fmt.Printf("  已插入元素: %d\n", info.InsertedElements)
		fmt.Printf("  内存使用: %d bytes\n", info.MemoryUsage)
	}
}

// 压力测试对比
func benchmarkPenetrationProtection(client redis.Cmdable, dataSource *DataSource) {
	fmt.Println("开始压力测试...")

	// 测试配置
	concurrency := 50
	requestsPerWorker := 100

	// 创建缓存（无防护）
	redisCache := cache.NewRedisCache(client, cache.DefaultCacheAsideOptions())
	cacheAsideNormal := cache.NewCacheAside(redisCache, nil)

	// 创建缓存（带防护）
	protectionOpts := cache.DefaultPenetrationOptions()
	protection, err := cache.NewPenetrationProtection(redisCache, client, protectionOpts)
	if err != nil {
		log.Fatal("Failed to create penetration protection:", err)
	}
	defer protection.Close()

	cacheAsideProtected := cache.NewCacheAsideWithProtection(redisCache, nil, protection)

	// 预热布隆过滤器
	var existingKeys []string
	for i := 1; i <= 1000; i++ {
		existingKeys = append(existingKeys, fmt.Sprintf("user:%d", i))
	}
	protection.PrewarmBloomFilter(context.Background(), existingKeys)

	loader := func(ctx context.Context, key string) (interface{}, error) {
		return dataSource.Get(key)
	}

	// 生成测试键（80%存在，20%不存在）
	generateTestKeys := func() []string {
		var keys []string
		for i := 0; i < requestsPerWorker; i++ {
			if rand.Float32() < 0.8 {
				// 80% 存在的key
				keys = append(keys, fmt.Sprintf("user:%d", rand.Intn(1000)+1))
			} else {
				// 20% 不存在的key
				keys = append(keys, fmt.Sprintf("user:%d", rand.Intn(9000)+2000))
			}
		}
		return keys
	}

	// 测试无防护版本
	fmt.Printf("\n测试无防护版本 (%d协程 x %d请求):\n", concurrency, requestsPerWorker)
	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys := generateTestKeys()
			var result string

			for _, key := range keys {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				cacheAsideNormal.GetOrLoad(ctx, key, &result, loader)
				cancel()
			}
		}()
	}

	wg.Wait()
	normalDuration := time.Since(start)
	fmt.Printf("  耗时: %v\n", normalDuration)

	// 清理缓存
	client.FlushDB(context.Background())

	// 测试带防护版本
	fmt.Printf("\n测试带防护版本 (%d协程 x %d请求):\n", concurrency, requestsPerWorker)
	start = time.Now()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys := generateTestKeys()
			var result string

			for _, key := range keys {
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				cacheAsideProtected.GetOrLoad(ctx, key, &result, loader)
				cancel()
			}
		}()
	}

	wg.Wait()
	protectedDuration := time.Since(start)
	fmt.Printf("  耗时: %v\n", protectedDuration)

	// 对比结果
	fmt.Printf("\n性能对比:\n")
	fmt.Printf("  无防护版本: %v\n", normalDuration)
	fmt.Printf("  带防护版本: %v\n", protectedDuration)
	if protectedDuration < normalDuration {
		improvement := float64(normalDuration-protectedDuration) / float64(normalDuration) * 100
		fmt.Printf("  性能提升: %.2f%%\n", improvement)
	} else {
		overhead := float64(protectedDuration-normalDuration) / float64(normalDuration) * 100
		fmt.Printf("  性能开销: %.2f%%\n", overhead)
	}
}
