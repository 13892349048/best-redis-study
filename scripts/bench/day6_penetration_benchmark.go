package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/bloom"
	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

// BenchmarkResult 基准测试结果
type BenchmarkResult struct {
	Scenario        string        `json:"scenario"`
	Duration        time.Duration `json:"duration"`
	TotalRequests   int64         `json:"total_requests"`
	SuccessRequests int64         `json:"success_requests"`
	ErrorRequests   int64         `json:"error_requests"`
	QPS             float64       `json:"qps"`
	AvgLatency      time.Duration `json:"avg_latency"`
	P50Latency      time.Duration `json:"p50_latency"`
	P95Latency      time.Duration `json:"p95_latency"`
	P99Latency      time.Duration `json:"p99_latency"`
	DataSourceHits  int64         `json:"data_source_hits"`
	BloomFilterHits int64         `json:"bloom_filter_hits"`
	CacheHits       int64         `json:"cache_hits"`
}

// BenchmarkConfig 基准测试配置
type BenchmarkConfig struct {
	Concurrency       int           `json:"concurrency"`
	Duration          time.Duration `json:"duration"`
	ExistingKeyRatio  float64       `json:"existing_key_ratio"` // 存在key的比例
	HotKeyRatio       float64       `json:"hot_key_ratio"`      // 热点key的比例
	DataSize          int           `json:"data_size"`          // 数据源大小
	EnableBloomFilter bool          `json:"enable_bloom_filter"`
	EnableEmptyCache  bool          `json:"enable_empty_cache"`
	EnableMutex       bool          `json:"enable_mutex"`
}

// LatencyTracker 延迟跟踪器
type LatencyTracker struct {
	latencies []time.Duration
	mu        sync.Mutex
}

func (lt *LatencyTracker) Record(latency time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	lt.latencies = append(lt.latencies, latency)
}

func (lt *LatencyTracker) GetPercentiles() (p50, p95, p99 time.Duration) {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if len(lt.latencies) == 0 {
		return 0, 0, 0
	}

	// 简单排序实现（实际应用中可以用更高效的算法）
	latencies := make([]time.Duration, len(lt.latencies))
	copy(latencies, lt.latencies)

	// 冒泡排序
	for i := 0; i < len(latencies); i++ {
		for j := 0; j < len(latencies)-1-i; j++ {
			if latencies[j] > latencies[j+1] {
				latencies[j], latencies[j+1] = latencies[j+1], latencies[j]
			}
		}
	}

	p50 = latencies[len(latencies)*50/100]
	p95 = latencies[len(latencies)*95/100]
	p99 = latencies[len(latencies)*99/100]

	return p50, p95, p99
}

func (lt *LatencyTracker) GetAverage() time.Duration {
	lt.mu.Lock()
	defer lt.mu.Unlock()

	if len(lt.latencies) == 0 {
		return 0
	}

	var total time.Duration
	for _, latency := range lt.latencies {
		total += latency
	}

	return total / time.Duration(len(lt.latencies))
}

// MockDataSource 模拟数据源
type MockDataSource struct {
	data         map[string]string
	mu           sync.RWMutex
	hitCount     int64
	queryLatency time.Duration
}

func NewMockDataSource(size int, queryLatency time.Duration) *MockDataSource {
	ds := &MockDataSource{
		data:         make(map[string]string),
		queryLatency: queryLatency,
	}

	// 生成测试数据
	for i := 1; i <= size; i++ {
		key := fmt.Sprintf("user:%d", i)
		value := fmt.Sprintf("User %d Data - %s", i, time.Now().Format(time.RFC3339))
		ds.data[key] = value
	}

	return ds
}

func (ds *MockDataSource) Get(key string) (string, error) {
	atomic.AddInt64(&ds.hitCount, 1)

	// 模拟查询延迟
	time.Sleep(ds.queryLatency)

	ds.mu.RLock()
	defer ds.mu.RUnlock()

	if value, exists := ds.data[key]; exists {
		return value, nil
	}

	return "", fmt.Errorf("data not found: %s", key)
}

func (ds *MockDataSource) GetHitCount() int64 {
	return atomic.LoadInt64(&ds.hitCount)
}

func (ds *MockDataSource) ResetHitCount() {
	atomic.StoreInt64(&ds.hitCount, 0)
}

// KeyGenerator 键生成器
type KeyGenerator struct {
	dataSize         int
	existingKeyRatio float64
	hotKeyRatio      float64
	hotKeys          []string
	rand             *rand.Rand
	mu               sync.Mutex
}

func NewKeyGenerator(dataSize int, existingKeyRatio, hotKeyRatio float64) *KeyGenerator {
	kg := &KeyGenerator{
		dataSize:         dataSize,
		existingKeyRatio: existingKeyRatio,
		hotKeyRatio:      hotKeyRatio,
		rand:             rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	// 生成热点key列表
	hotKeyCount := int(float64(dataSize) * hotKeyRatio)
	for i := 0; i < hotKeyCount; i++ {
		kg.hotKeys = append(kg.hotKeys, fmt.Sprintf("user:%d", i+1))
	}

	return kg
}

func (kg *KeyGenerator) NextKey() string {
	kg.mu.Lock()
	defer kg.mu.Unlock()

	// 决定生成存在的key还是不存在的key
	if kg.rand.Float64() < kg.existingKeyRatio {
		// 生成存在的key
		if kg.rand.Float64() < kg.hotKeyRatio && len(kg.hotKeys) > 0 {
			// 生成热点key
			return kg.hotKeys[kg.rand.Intn(len(kg.hotKeys))]
		} else {
			// 生成普通存在的key
			return fmt.Sprintf("user:%d", kg.rand.Intn(kg.dataSize)+1)
		}
	} else {
		// 生成不存在的key
		return fmt.Sprintf("user:%d", kg.rand.Intn(90000)+10000)
	}
}

// runBenchmark 运行基准测试
func runBenchmark(scenario string, config *BenchmarkConfig, dataSource *MockDataSource, cacheLoader func(ctx context.Context, key string, dest interface{}, loader cache.LoaderFunc) error) *BenchmarkResult {
	fmt.Printf("运行场景: %s\n", scenario)

	var (
		totalRequests   int64
		successRequests int64
		errorRequests   int64
		cacheHits       int64
	)

	latencyTracker := &LatencyTracker{}
	keyGenerator := NewKeyGenerator(config.DataSize, config.ExistingKeyRatio, config.HotKeyRatio)

	dataSource.ResetHitCount()

	// 数据加载函数
	loader := func(ctx context.Context, key string) (interface{}, error) {
		return dataSource.Get(key)
	}

	// 启动多个工作协程
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), config.Duration)
	defer cancel()

	for i := 0; i < config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					key := keyGenerator.NextKey()
					var result string

					start := time.Now()
					err := cacheLoader(context.Background(), key, &result, loader)
					latency := time.Since(start)

					atomic.AddInt64(&totalRequests, 1)
					latencyTracker.Record(latency)

					if err != nil {
						atomic.AddInt64(&errorRequests, 1)
					} else {
						atomic.AddInt64(&successRequests, 1)
						if latency < 5*time.Millisecond { // 假设缓存命中延迟很低
							atomic.AddInt64(&cacheHits, 1)
						}
					}
				}
			}
		}()
	}

	wg.Wait()

	// 计算结果
	actualDuration := config.Duration
	qps := float64(totalRequests) / actualDuration.Seconds()
	avgLatency := latencyTracker.GetAverage()
	p50, p95, p99 := latencyTracker.GetPercentiles()
	dataSourceHits := dataSource.GetHitCount()

	return &BenchmarkResult{
		Scenario:        scenario,
		Duration:        actualDuration,
		TotalRequests:   totalRequests,
		SuccessRequests: successRequests,
		ErrorRequests:   errorRequests,
		QPS:             qps,
		AvgLatency:      avgLatency,
		P50Latency:      p50,
		P95Latency:      p95,
		P99Latency:      p99,
		DataSourceHits:  dataSourceHits,
		CacheHits:       cacheHits,
	}
}

func main() {
	fmt.Println("=== Day 6: 缓存穿透防护压力测试 ===")

	// 创建Redis客户端
	client, err := redisx.NewClient(&redisx.Config{
		Addr: "localhost:6379",
		DB:   6,
	})
	if err != nil {
		log.Fatal("Failed to create Redis client:", err)
	}
	defer client.Close()

	// 清理测试数据
	client.FlushDB(context.Background())

	// 测试配置
	config := &BenchmarkConfig{
		Concurrency:       20,
		Duration:          30 * time.Second,
		ExistingKeyRatio:  0.7, // 70%的请求是存在的key
		HotKeyRatio:       0.1, // 10%是热点key
		DataSize:          10000,
		EnableBloomFilter: true,
		EnableEmptyCache:  true,
		EnableMutex:       true,
	}

	// 创建模拟数据源（10ms查询延迟）
	dataSource := NewMockDataSource(config.DataSize, 10*time.Millisecond)

	// 创建基础缓存
	redisCache := cache.NewRedisCache(client, cache.DefaultCacheAsideOptions())

	var results []*BenchmarkResult

	// 场景1: 无防护
	fmt.Println("\n=== 场景1: 无防护 ===")
	cacheAsideNormal := cache.NewCacheAside(redisCache, nil)
	result1 := runBenchmark("无防护", config, dataSource, cacheAsideNormal.GetOrLoad)
	results = append(results, result1)

	// 清理缓存
	client.FlushDB(context.Background())

	// 场景2: 仅启用布隆过滤器
	fmt.Println("\n=== 场景2: 仅启用布隆过滤器 ===")
	protectionOpts1 := &cache.PenetrationOptions{
		EnableBloomFilter:         true,
		BloomConfig:               bloom.DefaultBloomConfig("benchmark_bloom"),
		EnableEmptyValueCache:     false,
		EnableParameterValidation: false,
		EnableMutexProtection:     false,
		EnableMetrics:             true,
	}

	protection1, err := cache.NewPenetrationProtection(redisCache, client, protectionOpts1)
	if err != nil {
		log.Fatal("Failed to create protection1:", err)
	}

	// 预热布隆过滤器
	var existingKeys []string
	for i := 1; i <= config.DataSize; i++ {
		existingKeys = append(existingKeys, fmt.Sprintf("user:%d", i))
	}
	protection1.PrewarmBloomFilter(context.Background(), existingKeys)

	cacheAsideBloom := cache.NewCacheAsideWithProtection(redisCache, nil, protection1)
	result2 := runBenchmark("仅布隆过滤器", config, dataSource, cacheAsideBloom.GetOrLoad)
	results = append(results, result2)

	protection1.Close()
	client.FlushDB(context.Background())

	// 场景3: 仅启用空值缓存
	fmt.Println("\n=== 场景3: 仅启用空值缓存 ===")
	protectionOpts2 := &cache.PenetrationOptions{
		EnableBloomFilter:         false,
		EnableEmptyValueCache:     true,
		EmptyValueTTL:             5 * time.Minute,
		EmptyValueMaxCount:        1000,
		EnableParameterValidation: false,
		EnableMutexProtection:     false,
		EnableMetrics:             true,
	}

	protection2, err := cache.NewPenetrationProtection(redisCache, client, protectionOpts2)
	if err != nil {
		log.Fatal("Failed to create protection2:", err)
	}

	cacheAsideEmpty := cache.NewCacheAsideWithProtection(redisCache, nil, protection2)
	result3 := runBenchmark("仅空值缓存", config, dataSource, cacheAsideEmpty.GetOrLoad)
	results = append(results, result3)

	protection2.Close()
	client.FlushDB(context.Background())

	// 场景4: 完整防护
	fmt.Println("\n=== 场景4: 完整防护 ===")
	protectionOpts3 := cache.DefaultPenetrationOptions()
	protection3, err := cache.NewPenetrationProtection(redisCache, client, protectionOpts3)
	if err != nil {
		log.Fatal("Failed to create protection3:", err)
	}

	// 预热布隆过滤器
	protection3.PrewarmBloomFilter(context.Background(), existingKeys)

	cacheAsideFull := cache.NewCacheAsideWithProtection(redisCache, nil, protection3)
	result4 := runBenchmark("完整防护", config, dataSource, cacheAsideFull.GetOrLoad)
	results = append(results, result4)

	protection3.Close()

	// 输出结果
	fmt.Println("\n=== 测试结果对比 ===")
	printResults(results)

	// 保存结果到文件
	saveResults(results, config)

	fmt.Println("\n=== 布隆过滤器独立测试 ===")
	testBloomFilterPerformance(client)
}

func printResults(results []*BenchmarkResult) {
	fmt.Printf("%-15s %-10s %-10s %-10s %-10s %-10s %-10s %-10s %-10s\n",
		"场景", "QPS", "平均延迟", "P50", "P95", "P99", "成功率", "缓存命中", "数据源查询")
	fmt.Println(strings.Repeat("-", 120))

	for _, result := range results {
		successRate := float64(result.SuccessRequests) / float64(result.TotalRequests) * 100
		cacheHitRate := float64(result.CacheHits) / float64(result.SuccessRequests) * 100

		fmt.Printf("%-15s %-10.0f %-10s %-10s %-10s %-10s %-9.1f%% %-9.1f%% %-10d\n",
			result.Scenario,
			result.QPS,
			result.AvgLatency.Truncate(time.Microsecond),
			result.P50Latency.Truncate(time.Microsecond),
			result.P95Latency.Truncate(time.Microsecond),
			result.P99Latency.Truncate(time.Microsecond),
			successRate,
			cacheHitRate,
			result.DataSourceHits,
		)
	}
}

func saveResults(results []*BenchmarkResult, config *BenchmarkConfig) {
	data := map[string]interface{}{
		"timestamp": time.Now().Format(time.RFC3339),
		"config":    config,
		"results":   results,
	}

	filename := fmt.Sprintf("benchmark_results/penetration_benchmark_%s.json",
		time.Now().Format("20060102_150405"))

	// 确保目录存在
	os.MkdirAll("benchmark_results", 0755)

	file, err := os.Create(filename)
	if err != nil {
		log.Printf("Failed to create result file: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(data); err != nil {
		log.Printf("Failed to save results: %v", err)
	} else {
		fmt.Printf("结果已保存到: %s\n", filename)
	}
}

func testBloomFilterPerformance(client redis.Cmdable) {
	configs := []struct {
		name     string
		elements uint64
		fpr      float64
	}{
		{"小型", 1000, 0.01},
		{"中型", 100000, 0.01},
		{"大型", 1000000, 0.01},
		{"超大型", 10000000, 0.01},
	}

	for _, cfg := range configs {
		fmt.Printf("\n测试 %s 布隆过滤器 (预期元素: %d, 假阳性率: %.3f):\n",
			cfg.name, cfg.elements, cfg.fpr)

		config := &bloom.BloomConfig{
			Name:              fmt.Sprintf("perf_test_%s", cfg.name),
			ExpectedElements:  cfg.elements,
			FalsePositiveRate: cfg.fpr,
			Namespace:         "perf",
		}

		bf, err := bloom.NewStandardBloomFilter(client, config)
		if err != nil {
			log.Printf("Failed to create bloom filter: %v", err)
			continue
		}

		// 获取信息
		info, _ := bf.Info(context.Background())
		fmt.Printf("  位数组大小: %d (%.2f MB)\n", info.BitSize, float64(info.MemoryUsage)/(1024*1024))
		fmt.Printf("  哈希函数数量: %d\n", info.HashFunctions)

		// 测试添加性能
		keys := make([]string, 1000)
		for i := 0; i < 1000; i++ {
			keys[i] = fmt.Sprintf("test_key_%d", i)
		}

		start := time.Now()
		bf.AddMultiple(context.Background(), keys)
		addDuration := time.Since(start)
		fmt.Printf("  添加1000个元素耗时: %v (%.0f ops/s)\n",
			addDuration, 1000.0/addDuration.Seconds())

		// 测试查询性能
		start = time.Now()
		bf.TestMultiple(context.Background(), keys)
		testDuration := time.Since(start)
		fmt.Printf("  查询1000个元素耗时: %v (%.0f ops/s)\n",
			testDuration, 1000.0/testDuration.Seconds())

		bf.Close()
		bf.Clear(context.Background())
	}
}
