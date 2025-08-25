package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"

	"what_redis_can_do/internal/cache"
	"what_redis_can_do/internal/redisx"
)

// User 用户结构体（演示用）
type User struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Email    string `json:"email"`
	CreateAt int64  `json:"create_at"`
}

// UserService 模拟的用户服务
type UserService struct {
	users map[int]*User // 模拟数据库
}

// NewUserService 创建用户服务
func NewUserService() *UserService {
	// 预填充一些测试数据
	users := map[int]*User{
		1: {ID: 1, Name: "Alice", Email: "alice@example.com", CreateAt: time.Now().Unix()},
		2: {ID: 2, Name: "Bob", Email: "bob@example.com", CreateAt: time.Now().Unix()},
		3: {ID: 3, Name: "Charlie", Email: "charlie@example.com", CreateAt: time.Now().Unix()},
	}

	return &UserService{users: users}
}

// GetUser 从数据源获取用户（模拟数据库查询）
func (s *UserService) GetUser(ctx context.Context, userID int) (*User, error) {
	// 模拟数据库查询延迟
	time.Sleep(50 * time.Millisecond)

	user, exists := s.users[userID]
	if !exists {
		return nil, fmt.Errorf("user %d not found", userID)
	}

	// 返回副本以避免数据竞争
	return &User{
		ID:       user.ID,
		Name:     user.Name,
		Email:    user.Email,
		CreateAt: user.CreateAt,
	}, nil
}

// SaveUser 保存用户到数据源
func (s *UserService) SaveUser(ctx context.Context, user *User) error {
	// 模拟数据库写入延迟
	time.Sleep(30 * time.Millisecond)

	s.users[user.ID] = &User{
		ID:       user.ID,
		Name:     user.Name,
		Email:    user.Email,
		CreateAt: user.CreateAt,
	}

	return nil
}

func main() {
	// 初始化Redis客户端
	client, err := redisx.NewClient(redisx.DefaultConfig())
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	// 健康检查
	ctx := context.Background()
	if err := client.HealthCheck(ctx); err != nil {
		log.Fatalf("Redis health check failed: %v", err)
	}

	fmt.Println("🚀 Day 5: 缓存模式与TTL设计演示")
	fmt.Println("================================")

	// 创建用户服务
	userService := NewUserService()

	// 演示各种缓存模式
	fmt.Println("\n📚 1. Cache-Aside 模式演示")
	demonstrateCacheAside(ctx, client, userService)

	fmt.Println("\n📚 2. Write-Through 模式演示")
	demonstrateWriteThrough(ctx, client, userService)

	fmt.Println("\n📚 3. Write-Behind 模式演示")
	demonstrateWriteBehind(ctx, client, userService)

	fmt.Println("\n📚 4. TTL抖动效果演示")
	demonstrateTTLJitter(ctx, client)

	fmt.Println("\n📚 5. 缓存指标统计演示")
	demonstrateCacheMetrics(ctx, client, userService)

	fmt.Println("\n✅ 所有演示完成！")
}

// demonstrateCacheAside 演示Cache-Aside模式
func demonstrateCacheAside(ctx context.Context, client redis.Cmdable, userService *UserService) {
	// 创建Redis缓存
	redisCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:           30 * time.Second,
		TTLJitter:     0.1,
		EmptyValueTTL: 5 * time.Second,
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "demo:cache_aside",
	})

	// 创建Cache-Aside装饰器
	cacheAside := cache.NewCacheAside(redisCache, nil)

	// 定义数据加载函数
	userLoader := func(ctx context.Context, key string) (interface{}, error) {
		// 从key中提取用户ID
		var userID int
		if _, err := fmt.Sscanf(key, "user:%d", &userID); err != nil {
			return nil, fmt.Errorf("invalid user key: %s", key)
		}

		return userService.GetUser(ctx, userID)
	}

	fmt.Println("Cache-Aside模式: 读取时检查缓存，未命中时从数据源加载并缓存")

	// 测试用户ID
	userID := 1
	key := fmt.Sprintf("user:%d", userID)

	// 第一次读取（缓存未命中）
	fmt.Printf("📖 第一次读取用户%d (缓存未命中)...\n", userID)
	start := time.Now()
	var user User
	err := cacheAside.GetOrLoad(ctx, key, &user, userLoader)
	duration := time.Since(start)

	if err != nil {
		log.Printf("❌ 获取用户失败: %v", err)
	} else {
		fmt.Printf("✅ 获取用户成功: %+v (耗时: %v)\n", user, duration)
	}

	// 第二次读取（缓存命中）
	fmt.Printf("📖 第二次读取用户%d (缓存命中)...\n", userID)
	start = time.Now()
	err = cacheAside.GetOrLoad(ctx, key, &user, userLoader)
	duration = time.Since(start)

	if err != nil {
		log.Printf("❌ 获取用户失败: %v", err)
	} else {
		fmt.Printf("✅ 获取用户成功: %+v (耗时: %v)\n", user, duration)
	}

	// 测试不存在的用户（演示空值缓存）
	fmt.Printf("📖 读取不存在的用户999 (演示空值缓存)...\n")
	key = "user:999"
	err = cacheAside.GetOrLoad(ctx, key, &user, userLoader)
	if err != nil {
		fmt.Printf("✅ 预期的错误: %v\n", err)
	}

	fmt.Printf("📊 Cache-Aside统计: %+v\n", cacheAside.GetStats())
}

// demonstrateWriteThrough 演示Write-Through模式
func demonstrateWriteThrough(ctx context.Context, client redis.Cmdable, userService *UserService) {
	// 创建Redis缓存
	redisCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:           30 * time.Second,
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "demo:write_through",
	})

	// 定义写入数据源的函数
	writeFunc := func(ctx context.Context, key string, value interface{}) error {
		user, ok := value.(*User)
		if !ok {
			return fmt.Errorf("invalid user type")
		}
		return userService.SaveUser(ctx, user)
	}

	// 创建Write-Through缓存
	writeThrough := cache.NewWriteThroughCache(redisCache, writeFunc, nil)

	fmt.Println("Write-Through模式: 同时写入缓存和数据源，保证强一致性")

	// 创建新用户
	newUser := &User{
		ID:       100,
		Name:     "David",
		Email:    "david@example.com",
		CreateAt: time.Now().Unix(),
	}

	key := fmt.Sprintf("user:%d", newUser.ID)

	// Write-Through写入
	fmt.Printf("💾 Write-Through写入用户%d...\n", newUser.ID)
	start := time.Now()
	err := writeThrough.Set(ctx, key, newUser)
	duration := time.Since(start)

	if err != nil {
		log.Printf("❌ 写入失败: %v", err)
	} else {
		fmt.Printf("✅ 写入成功 (耗时: %v)\n", duration)
	}

	// 验证数据已写入缓存和数据源
	fmt.Printf("📖 从缓存读取用户%d...\n", newUser.ID)
	var cachedUser User
	err = writeThrough.Get(ctx, key, &cachedUser)
	if err != nil {
		log.Printf("❌ 从缓存读取失败: %v", err)
	} else {
		fmt.Printf("✅ 缓存中的用户: %+v\n", cachedUser)
	}

	// 从数据源验证
	dbUser, err := userService.GetUser(ctx, newUser.ID)
	if err != nil {
		log.Printf("❌ 从数据源读取失败: %v", err)
	} else {
		fmt.Printf("✅ 数据源中的用户: %+v\n", dbUser)
	}
}

// demonstrateWriteBehind 演示Write-Behind模式
func demonstrateWriteBehind(ctx context.Context, client redis.Cmdable, userService *UserService) {
	// 创建Redis缓存
	redisCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:           30 * time.Second,
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "demo:write_behind",
	})

	// 定义写入数据源的函数
	writeFunc := func(ctx context.Context, key string, value interface{}) error {
		fmt.Printf("⏰ 异步写入数据源: %s\n", key)
		user, ok := value.(*User)
		if !ok {
			return fmt.Errorf("invalid user type")
		}
		return userService.SaveUser(ctx, user)
	}

	// 创建Write-Behind缓存
	writeBehind := cache.NewWriteBehindCache(redisCache, writeFunc, &cache.WriteBehindOptions{
		CacheAsideOptions: &cache.CacheAsideOptions{
			TTL:           30 * time.Second,
			Serializer:    &cache.JSONSerializer{},
			EnableMetrics: true,
			Namespace:     "demo:write_behind",
		},
		BufferSize:    100,
		FlushInterval: 2 * time.Second,
		BatchSize:     10,
		MaxRetries:    3,
	})
	defer writeBehind.Close()

	fmt.Println("Write-Behind模式: 立即写入缓存，异步写入数据源，适合写密集场景")

	// 批量写入多个用户
	fmt.Printf("💾 批量Write-Behind写入5个用户...\n")
	start := time.Now()

	for i := 200; i < 205; i++ {
		user := &User{
			ID:       i,
			Name:     fmt.Sprintf("User%d", i),
			Email:    fmt.Sprintf("user%d@example.com", i),
			CreateAt: time.Now().Unix(),
		}

		key := fmt.Sprintf("user:%d", user.ID)
		err := writeBehind.Set(ctx, key, user)
		if err != nil {
			log.Printf("❌ 写入用户%d失败: %v", i, err)
		} else {
			fmt.Printf("✅ 用户%d已写入缓存\n", i)
		}
	}

	writeDuration := time.Since(start)
	fmt.Printf("✅ 批量写入完成，耗时: %v\n", writeDuration)

	// 立即从缓存读取
	fmt.Printf("📖 立即从缓存读取用户200...\n")
	key := "user:200"
	var user User
	err := writeBehind.Get(ctx, key, &user)
	if err != nil {
		log.Printf("❌ 从缓存读取失败: %v", err)
	} else {
		fmt.Printf("✅ 缓存中的用户: %+v\n", user)
	}

	// 等待异步写入完成
	fmt.Printf("⏳ 等待异步写入完成...\n")
	time.Sleep(3 * time.Second)

	// 手动刷新
	fmt.Printf("🔄 手动刷新缓冲区...\n")
	err = writeBehind.Flush(ctx)
	if err != nil {
		log.Printf("❌ 刷新失败: %v", err)
	}

	time.Sleep(1 * time.Second)

	// 获取统计信息
	cacheStats, wbStats := writeBehind.GetStats()
	fmt.Printf("📊 缓存统计: %+v\n", cacheStats)
	fmt.Printf("📊 Write-Behind统计: %+v\n", wbStats)
}

// demonstrateTTLJitter 演示TTL抖动效果
func demonstrateTTLJitter(ctx context.Context, client redis.Cmdable) {
	fmt.Println("TTL抖动演示: 防止缓存雪崩，TTL会在基准值附近随机波动")

	// 创建带抖动的缓存
	redisCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:           10 * time.Second, // 基准TTL
		TTLJitter:     0.3,              // 30%抖动
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: false,
		Namespace:     "demo:ttl_jitter",
	})

	// 批量设置缓存，观察TTL变化
	fmt.Printf("⏱️  设置10个key，基准TTL=10s，抖动=30%%\n")

	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("jitter_test:%d", i)
		value := fmt.Sprintf("value_%d", i)

		// 设置缓存
		err := redisCache.Set(ctx, key, value, 10*time.Second)
		if err != nil {
			log.Printf("❌ 设置key %s失败: %v", key, err)
			continue
		}

		// 立即查看TTL，构建完整的key名称
		fullKey := fmt.Sprintf("demo:ttl_jitter:%s", key)
		ttl := client.TTL(ctx, fullKey).Val()
		fmt.Printf("🔑 %s: TTL = %v\n", key, ttl)
	}

	fmt.Printf("💡 可以看到TTL在7-13秒之间随机分布，有效防止同时过期\n")
}

// demonstrateCacheMetrics 演示缓存指标统计
func demonstrateCacheMetrics(ctx context.Context, client redis.Cmdable, userService *UserService) {
	fmt.Println("缓存指标统计演示: 监控缓存性能和健康状况")

	// 创建带指标的缓存
	redisCache := cache.NewRedisCache(client, &cache.CacheAsideOptions{
		TTL:           15 * time.Second,
		Serializer:    &cache.JSONSerializer{},
		EnableMetrics: true,
		Namespace:     "demo:metrics",
	})

	// 创建Cache-Aside装饰器
	cacheAside := cache.NewCacheAside(redisCache, nil)

	// 定义用户加载函数
	userLoader := func(ctx context.Context, key string) (interface{}, error) {
		var userID int
		if _, err := fmt.Sscanf(key, "user:%d", &userID); err != nil {
			return nil, fmt.Errorf("invalid user key: %s", key)
		}
		return userService.GetUser(ctx, userID)
	}

	// 模拟一些缓存操作
	fmt.Printf("🔄 执行混合读写操作...\n")

	userIDs := []int{1, 2, 3, 1, 2, 4, 1, 5, 2, 3} // 有些重复，有些不存在

	for i, userID := range userIDs {
		key := fmt.Sprintf("user:%d", userID)
		var user User

		start := time.Now()
		err := cacheAside.GetOrLoad(ctx, key, &user, userLoader)
		duration := time.Since(start)

		if err != nil {
			fmt.Printf("❌ 操作%d: 获取用户%d失败: %v (耗时: %v)\n", i+1, userID, err, duration)
		} else {
			fmt.Printf("✅ 操作%d: 获取用户%d成功 (耗时: %v)\n", i+1, userID, duration)
		}

		// 随机休眠
		time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
	}

	// 显示详细指标
	stats := cacheAside.GetStats()
	if stats != nil {
		fmt.Printf("\n📊 详细缓存指标:\n")
		fmt.Printf("   命中次数: %d\n", stats.Hits)
		fmt.Printf("   未命中次数: %d\n", stats.Misses)
		fmt.Printf("   命中率: %.2f%%\n", stats.HitRate*100)
		fmt.Printf("   设置次数: %d\n", stats.Sets)
		fmt.Printf("   删除次数: %d\n", stats.Dels)
		fmt.Printf("   平均Get延迟: %.2f μs\n", stats.AvgGetLatencyUs)
		fmt.Printf("   平均Set延迟: %.2f μs\n", stats.AvgSetLatencyUs)
		fmt.Printf("   Get错误数: %d\n", stats.GetErrors)
		fmt.Printf("   Set错误数: %d\n", stats.SetErrors)
		fmt.Printf("   序列化错误: %d\n", stats.SerializeErrors)
		fmt.Printf("   反序列化错误: %d\n", stats.DeserializeErrors)

		// 输出JSON格式便于监控系统采集
		statsJSON, _ := json.MarshalIndent(stats, "   ", "  ")
		fmt.Printf("\n📈 JSON格式指标（便于监控）:\n   %s\n", string(statsJSON))
	}
}
