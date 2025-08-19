package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"

	"github.com/redis/go-redis/v9"
)

// Day 3 高级数据结构简化演示
func main33() {
	ctx := context.Background()

	// 连接Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer rdb.Close()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("Redis连接失败:", err)
	}

	fmt.Println("🚀 Day 3: 高级数据结构演示")

	// 1. Bitmap演示
	fmt.Println("\n📊 Bitmap - 用户活跃统计")
	bitmapDemo1(ctx, rdb)

	// 2. HyperLogLog演示
	fmt.Println("\n🔢 HyperLogLog - UV统计")
	hllDemo(ctx, rdb)

	// 3. Geo演示
	fmt.Println("\n🗺️ Geo - 地理位置服务")
	geoDemo1(ctx, rdb)
}

func bitmapDemo1(ctx context.Context, rdb *redis.Client) {
	key := "active:today"

	// 设置活跃用户
	for i := 0; i < 1000; i++ {
		if rand.Float32() < 0.3 { // 30%活跃率
			rdb.SetBit(ctx, key, int64(i), 1)
		}
	}

	// 统计活跃用户数
	count, _ := rdb.BitCount(ctx, key, nil).Result()
	fmt.Printf("活跃用户数: %d/1000\n", count)

	// 检查特定用户
	isActive, _ := rdb.GetBit(ctx, key, 100).Result()
	fmt.Printf("用户100活跃状态: %d\n", isActive)

	rdb.Del(ctx, key)
}

func hllDemo(ctx context.Context, rdb *redis.Client) {
	key := "uv:today"

	// 模拟10万次访问，约5万独立用户
	realUsers := make(map[string]bool)
	for i := 0; i < 100000; i++ {
		userID := fmt.Sprintf("user_%d", rand.Intn(50000))
		realUsers[userID] = true
		rdb.PFAdd(ctx, key, userID)
	}

	// 对比结果
	realUV := len(realUsers)
	hllUV, _ := rdb.PFCount(ctx, key).Result()

	fmt.Printf("真实UV: %d\n", realUV)
	fmt.Printf("HLL估算: %d\n", hllUV)
	fmt.Printf("误差: %.2f%%\n",
		float64(hllUV-int64(realUV))/float64(realUV)*100)

	rdb.Del(ctx, key)
}

func geoDemo1(ctx context.Context, rdb *redis.Client) {
	key := "drivers:online"

	// 添加司机位置 (北京坐标)
	rdb.GeoAdd(ctx, key,
		&redis.GeoLocation{Name: "driver1", Longitude: 116.397, Latitude: 39.900},
		&redis.GeoLocation{Name: "driver2", Longitude: 116.400, Latitude: 39.910},
		&redis.GeoLocation{Name: "driver3", Longitude: 116.420, Latitude: 39.890},
	)

	// 用户位置
	userLng, userLat := 116.400, 39.905

	// 查找2公里内司机
	nearby, _ := rdb.GeoRadius(ctx, key, userLng, userLat,
		&redis.GeoRadiusQuery{
			Radius: 2, Unit: "km", WithDist: true, Count: 5,
		}).Result()

	fmt.Printf("用户位置: (%.3f, %.3f)\n", userLng, userLat)
	fmt.Printf("2公里内找到%d个司机:\n", len(nearby))
	for i, driver := range nearby {
		fmt.Printf("  %d. %s - 距离%.2fkm\n",
			i+1, driver.Name, driver.Dist)
	}

	rdb.Del(ctx, key)
}
