package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Day 3: 高级数据结构 - Bitmap/HyperLogLog/Geo
// 学习目标: 掌握低成本计数、地域检索与签到/活跃场景

func main3() {
	ctx := context.Background()

	// 连接Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	defer rdb.Close()

	// 测试连接
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("无法连接到Redis:", err)
	}

	fmt.Println("🚀 Day 3: 高级数据结构学习与实战")
	fmt.Println(strings.Repeat("=", 50))

	// 1. Bitmap - 用户活跃统计
	fmt.Println("\n📊 1. Bitmap - 用户活跃统计")
	bitmapDemo(ctx, rdb)

	// 2. HyperLogLog - 基数统计
	fmt.Println("\n🔢 2. HyperLogLog - 基数统计")
	hyperLogLogDemo(ctx, rdb)

	// 3. Geo - 地理位置服务
	fmt.Println("\n🗺️  3. Geo - 地理位置服务")
	geoDemo(ctx, rdb)

	// 4. 性能对比与分析
	fmt.Println("\n⚡ 4. 性能对比与分析")
	performanceComparison(ctx, rdb)
}

// Bitmap演示 - 用户活跃统计
func bitmapDemo(ctx context.Context, rdb *redis.Client) {
	fmt.Println("场景: 统计用户每日签到和活跃度")

	// 模拟用户ID范围: 1-10000
	const maxUserID = 10000
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")

	// Key设计规范
	todayKey := fmt.Sprintf("active:daily:%s", today)
	yesterdayKey := fmt.Sprintf("active:daily:%s", yesterday)
	weeklyKey := fmt.Sprintf("active:weekly:%s", time.Now().Format("2006-W02"))

	fmt.Printf("Key设计: %s, %s\n", todayKey, weeklyKey)

	// 1. 模拟今日活跃用户 (30%活跃度)
	fmt.Println("\n📈 模拟用户活跃数据...")
	activeToday := simulateActiveUsers(ctx, rdb, todayKey, maxUserID, 0.3)
	activeYesterday := simulateActiveUsers(ctx, rdb, yesterdayKey, maxUserID, 0.25)

	fmt.Printf("模拟数据: 今日%d用户活跃, 昨日%d用户活跃\n", activeToday, activeYesterday)

	// 2. 统计今日活跃用户数
	todayCount, err := rdb.BitCount(ctx, todayKey, nil).Result()
	if err != nil {
		log.Printf("统计今日活跃用户失败: %v", err)
		return
	}

	yesterdayCount, err := rdb.BitCount(ctx, yesterdayKey, nil).Result()
	if err != nil {
		log.Printf("统计昨日活跃用户失败: %v", err)
		return
	}

	fmt.Printf("今日活跃用户数: %d (%.2f%%)\n", todayCount, float64(todayCount)/maxUserID*100)
	fmt.Printf("昨日活跃用户数: %d (%.2f%%)\n", yesterdayCount, float64(yesterdayCount)/maxUserID*100)

	// 3. 连续活跃用户分析 (AND操作)
	fmt.Println("\n🔗 连续活跃用户分析...")
	continuousKey := "active:continuous:2days"
	err = rdb.BitOpAnd(ctx, continuousKey, todayKey, yesterdayKey).Err()
	if err != nil {
		log.Printf("计算连续活跃用户失败: %v", err)
		return
	}

	continuousCount, err := rdb.BitCount(ctx, continuousKey, nil).Result()
	if err != nil {
		log.Printf("统计连续活跃用户失败: %v", err)
		return
	}

	fmt.Printf("连续2天活跃用户数: %d (留存率: %.2f%%)\n",
		continuousCount, float64(continuousCount)/float64(yesterdayCount)*100)

	// 4. 新增活跃用户 (今日有，昨日无)
	fmt.Println("\n✨ 新增活跃用户分析...")
	// 先复制今日数据
	newUserKey := "active:new:today"
	err = rdb.BitOpOr(ctx, newUserKey, todayKey).Err()
	if err != nil {
		log.Printf("复制今日活跃数据失败: %v", err)
		return
	}

	// 减去昨日活跃用户 (今日 AND NOT 昨日)
	// Redis没有直接的ANDNOT，我们用 today AND (NOT yesterday)
	// 实现方式: 先创建yesterday的反码，再与today做AND
	notYesterdayKey := "active:not_yesterday"
	err = rdb.BitOpNot(ctx, notYesterdayKey, yesterdayKey).Err()
	if err != nil {
		log.Printf("创建昨日反码失败: %v", err)
		return
	}

	err = rdb.BitOpAnd(ctx, newUserKey, todayKey, notYesterdayKey).Err()
	if err != nil {
		log.Printf("计算新增用户失败: %v", err)
		return
	}

	newUserCount, err := rdb.BitCount(ctx, newUserKey, nil).Result()
	if err != nil {
		log.Printf("统计新增用户失败: %v", err)
		return
	}

	fmt.Printf("新增活跃用户数: %d\n", newUserCount)

	// 5. 检查特定用户状态
	fmt.Println("\n👤 特定用户活跃状态检查...")
	testUserIDs := []int{1, 100, 1000, 5000}
	for _, userID := range testUserIDs {
		isActiveToday, err := rdb.GetBit(ctx, todayKey, int64(userID)).Result()
		if err != nil {
			log.Printf("检查用户%d状态失败: %v", userID, err)
			continue
		}
		isActiveYesterday, err := rdb.GetBit(ctx, yesterdayKey, int64(userID)).Result()
		if err != nil {
			log.Printf("检查用户%d昨日状态失败: %v", userID, err)
			continue
		}

		status := "新用户"
		if isActiveYesterday == 1 && isActiveToday == 1 {
			status = "连续活跃"
		} else if isActiveYesterday == 1 && isActiveToday == 0 {
			status = "流失用户"
		} else if isActiveToday == 1 {
			status = "回流/新增"
		} else {
			status = "非活跃"
		}

		fmt.Printf("用户%d: %s (昨日:%d, 今日:%d)\n",
			userID, status, isActiveYesterday, isActiveToday)
	}

	// 6. 内存使用分析
	fmt.Println("\n💾 Bitmap内存使用分析...")
	memory, err := rdb.MemoryUsage(ctx, todayKey).Result()
	if err != nil {
		log.Printf("获取内存使用失败: %v", err)
	} else {
		fmt.Printf("Bitmap内存使用: %d bytes (理论最小: %d bytes)\n",
			memory, maxUserID/8+1)
		fmt.Printf("平均每用户: %.2f bytes\n", float64(memory)/float64(todayCount))
	}

	// 设置过期时间 (保留30天)
	rdb.Expire(ctx, todayKey, 30*24*time.Hour)
	rdb.Expire(ctx, yesterdayKey, 30*24*time.Hour)

	// 清理临时key
	rdb.Del(ctx, continuousKey, newUserKey, notYesterdayKey)

	fmt.Println("✅ Bitmap演示完成")
}

// 模拟活跃用户数据
func simulateActiveUsers(ctx context.Context, rdb *redis.Client, key string, maxUsers int, activeRate float64) int {
	pipeline := rdb.Pipeline()
	activeCount := 0

	for userID := 1; userID <= maxUsers; userID++ {
		if rand.Float64() < activeRate {
			pipeline.SetBit(ctx, key, int64(userID), 1)
			activeCount++
		}
	}

	_, err := pipeline.Exec(ctx)
	if err != nil {
		log.Printf("批量设置活跃用户失败: %v", err)
		return 0
	}

	return activeCount
}

// HyperLogLog演示 - 基数统计
func hyperLogLogDemo(ctx context.Context, rdb *redis.Client) {
	fmt.Println("场景: 网站UV统计和误差分析")

	// Key设计
	today := time.Now().Format("2006-01-02")
	uvKey := fmt.Sprintf("uv:daily:%s", today)
	uvWeeklyKey := fmt.Sprintf("uv:weekly:%s", time.Now().Format("2006-W02"))

	fmt.Printf("Key设计: %s, %s\n", uvKey, uvWeeklyKey)

	// 1. 模拟访客数据 (使用真实的用户行为模式)
	fmt.Println("\n📊 模拟网站访客数据...")

	// 生成10万次访问，但实际独立用户约5万 (考虑重复访问)
	visits := 100000
	realUniqueUsers := make(map[string]bool)

	pipeline := rdb.Pipeline()
	for i := 0; i < visits; i++ {
		// 模拟用户ID: user_1 到 user_50000，但有重复访问
		userID := fmt.Sprintf("user_%d", rand.Intn(50000)+1)
		realUniqueUsers[userID] = true

		// 添加到HyperLogLog
		pipeline.PFAdd(ctx, uvKey, userID)

		// 每1000个批量执行
		if i%1000 == 0 {
			pipeline.Exec(ctx)
			pipeline = rdb.Pipeline()
		}
	}
	pipeline.Exec(ctx) // 执行剩余的

	// 2. 统计结果对比
	fmt.Println("\n📈 统计结果对比...")

	// HyperLogLog统计结果
	hllCount, err := rdb.PFCount(ctx, uvKey).Result()
	if err != nil {
		log.Printf("HLL统计失败: %v", err)
		return
	}

	// 真实独立用户数
	realCount := int64(len(realUniqueUsers))

	// 误差计算
	error := math.Abs(float64(hllCount-realCount)) / float64(realCount) * 100

	fmt.Printf("真实独立用户数: %d\n", realCount)
	fmt.Printf("HyperLogLog估算: %d\n", hllCount)
	fmt.Printf("误差: %.2f%% (理论误差标准: ±1.04%%)\n", error)

	// 3. 多天数据合并
	fmt.Println("\n📅 多天UV数据合并...")

	// 模拟昨天的数据
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	uvYesterdayKey := fmt.Sprintf("uv:daily:%s", yesterday)

	// 昨天有30000个独立用户，部分与今天重叠
	pipeline = rdb.Pipeline()
	for i := 0; i < 30000; i++ {
		userID := fmt.Sprintf("user_%d", rand.Intn(60000)+1) // 范围稍大，增加重叠
		pipeline.PFAdd(ctx, uvYesterdayKey, userID)
	}
	pipeline.Exec(ctx)

	// 合并两天的数据
	uvTwoDaysKey := "uv:2days:combined"
	err = rdb.PFMerge(ctx, uvTwoDaysKey, uvKey, uvYesterdayKey).Err()
	if err != nil {
		log.Printf("HLL合并失败: %v", err)
		return
	}

	todayUV, _ := rdb.PFCount(ctx, uvKey).Result()
	yesterdayUV, _ := rdb.PFCount(ctx, uvYesterdayKey).Result()
	twoDaysUV, _ := rdb.PFCount(ctx, uvTwoDaysKey).Result()

	fmt.Printf("今日UV: %d\n", todayUV)
	fmt.Printf("昨日UV: %d\n", yesterdayUV)
	fmt.Printf("两日合并UV: %d\n", twoDaysUV)
	fmt.Printf("重叠用户估算: %d\n", todayUV+yesterdayUV-twoDaysUV)

	// 4. 内存使用分析
	fmt.Println("\n💾 HyperLogLog内存使用分析...")
	memory, err := rdb.MemoryUsage(ctx, uvKey).Result()
	if err != nil {
		log.Printf("获取内存使用失败: %v", err)
	} else {
		fmt.Printf("HLL内存使用: %d bytes (固定约12KB)\n", memory)
		fmt.Printf("如果用Set存储%d个用户，预估需要: %.2f MB\n",
			realCount, float64(realCount*20)/1024/1024) // 假设每个userID约20字节
		fmt.Printf("内存节省: %.2f%%\n",
			(1-float64(memory)/float64(realCount*20))*100)
	}

	// 5. 误差测试 - 不同数据量下的精度
	fmt.Println("\n🎯 不同数据量下的精度测试...")
	testSizes := []int{1000, 10000, 100000, 1000000}

	for _, size := range testSizes {
		testKey := fmt.Sprintf("uv:test:%d", size)

		// 生成精确数量的独立用户
		pipeline = rdb.Pipeline()
		for i := 0; i < size; i++ {
			userID := fmt.Sprintf("test_user_%d", i)
			pipeline.PFAdd(ctx, testKey, userID)
		}
		pipeline.Exec(ctx)

		// 统计估算值
		estimated, err := rdb.PFCount(ctx, testKey).Result()
		if err != nil {
			continue
		}

		error := math.Abs(float64(estimated-int64(size))) / float64(size) * 100
		fmt.Printf("数据量%d: 估算%d, 误差%.2f%%\n", size, estimated, error)

		// 清理测试数据
		rdb.Del(ctx, testKey)
	}

	// 设置过期时间
	rdb.Expire(ctx, uvKey, 30*24*time.Hour)
	rdb.Expire(ctx, uvYesterdayKey, 30*24*time.Hour)
	rdb.Del(ctx, uvTwoDaysKey)

	fmt.Println("✅ HyperLogLog演示完成")
}

// Geo演示 - 地理位置服务
func geoDemo(ctx context.Context, rdb *redis.Client) {
	fmt.Println("场景: 附近的人/店铺/司机定位服务")

	// Key设计
	driversKey := "geo:drivers:online"
	storesKey := "geo:stores:all"
	usersKey := "geo:users:online"

	fmt.Printf("Key设计: %s, %s, %s\n", driversKey, storesKey, usersKey)

	// 1. 添加地理位置数据
	fmt.Println("\n📍 添加地理位置数据...")

	// 北京市中心区域的一些位置 (经纬度范围: 116.3-116.5, 39.8-40.0)
	locations := []struct {
		name string
		lng  float64
		lat  float64
		key  string
	}{
		// 司机位置
		{"driver_001", 116.397428, 39.900000, driversKey},
		{"driver_002", 116.400000, 39.910000, driversKey},
		{"driver_003", 116.420000, 39.890000, driversKey},
		{"driver_004", 116.380000, 39.920000, driversKey},
		{"driver_005", 116.450000, 39.880000, driversKey},

		// 商店位置
		{"store_001", 116.395000, 39.895000, storesKey},
		{"store_002", 116.410000, 39.905000, storesKey},
		{"store_003", 116.425000, 39.885000, storesKey},
		{"store_004", 116.385000, 39.915000, storesKey},

		// 用户位置
		{"user_001", 116.398000, 39.898000, usersKey},
		{"user_002", 116.405000, 39.902000, usersKey},
	}

	pipeline := rdb.Pipeline()
	for _, loc := range locations {
		pipeline.GeoAdd(ctx, loc.key, &redis.GeoLocation{
			Name:      loc.name,
			Longitude: loc.lng,
			Latitude:  loc.lat,
		})
	}
	_, err := pipeline.Exec(ctx)
	if err != nil {
		log.Printf("添加地理位置失败: %v", err)
		return
	}

	fmt.Printf("添加了%d个位置点\n", len(locations))

	// 2. 附近的司机查询 (用户视角)
	fmt.Println("\n🚗 查找附近的司机...")
	userLng, userLat := 116.400000, 39.900000 // 用户当前位置

	// 查找5公里内的司机
	nearbyDrivers, err := rdb.GeoRadius(ctx, driversKey, userLng, userLat, &redis.GeoRadiusQuery{
		Radius:    5,
		Unit:      "km",
		WithCoord: true,
		WithDist:  true,
		Count:     10,
		Sort:      "ASC", // 按距离排序
	}).Result()

	if err != nil {
		log.Printf("查询附近司机失败: %v", err)
		return
	}

	fmt.Printf("用户位置: (%.6f, %.6f)\n", userLng, userLat)
	fmt.Printf("5公里内找到%d个司机:\n", len(nearbyDrivers))
	for i, driver := range nearbyDrivers {
		fmt.Printf("  %d. %s - 距离%.2fkm (%.6f, %.6f)\n",
			i+1, driver.Name, driver.Dist,
			driver.Longitude, driver.Latitude)
	}

	// 3. 附近的商店查询 (按距离和评分排序)
	fmt.Println("\n🏪 查找附近的商店...")
	nearbyStores, err := rdb.GeoRadius(ctx, storesKey, userLng, userLat, &redis.GeoRadiusQuery{
		Radius:    3,
		Unit:      "km",
		WithCoord: true,
		WithDist:  true,
		Count:     5,
		Sort:      "ASC",
	}).Result()

	if err != nil {
		log.Printf("查询附近商店失败: %v", err)
		return
	}

	fmt.Printf("3公里内找到%d个商店:\n", len(nearbyStores))
	for i, store := range nearbyStores {
		fmt.Printf("  %d. %s - 距离%.2fkm\n", i+1, store.Name, store.Dist)
	}

	// 4. 两点间距离计算
	fmt.Println("\n📏 计算两点间距离...")
	distances, err := rdb.GeoDist(ctx, driversKey, "driver_001", "driver_002", "km").Result()
	if err != nil {
		log.Printf("计算距离失败: %v", err)
	} else {
		fmt.Printf("driver_001 到 driver_002 的距离: %.2f km\n", distances)
	}

	// 5. 获取位置坐标
	fmt.Println("\n🎯 获取指定对象的坐标...")
	positions, err := rdb.GeoPos(ctx, driversKey, "driver_001", "driver_003").Result()
	if err != nil {
		log.Printf("获取位置失败: %v", err)
	} else {
		for i, pos := range positions {
			if pos != nil {
				driverName := []string{"driver_001", "driver_003"}[i]
				fmt.Printf("%s 位置: (%.6f, %.6f)\n", driverName, pos.Longitude, pos.Latitude)
			}
		}
	}

	// 6. GeoHash编码
	fmt.Println("\n🔢 GeoHash编码...")
	hashes, err := rdb.GeoHash(ctx, driversKey, "driver_001", "driver_002").Result()
	if err != nil {
		log.Printf("获取GeoHash失败: %v", err)
	} else {
		drivers := []string{"driver_001", "driver_002"}
		for i, hash := range hashes {
			fmt.Printf("%s GeoHash: %s\n", drivers[i], hash)
		}
	}

	// 7. 按成员查询附近位置 (以某个司机为中心)
	fmt.Println("\n👥 以driver_001为中心查找附近的其他司机...")
	nearbyByMember, err := rdb.GeoRadiusByMember(ctx, driversKey, "driver_001", &redis.GeoRadiusQuery{
		Radius:   2,
		Unit:     "km",
		WithDist: true,
		Count:    5,
		Sort:     "ASC",
	}).Result()

	if err != nil {
		log.Printf("按成员查询失败: %v", err)
	} else {
		fmt.Printf("driver_001 2公里内的其他司机:\n")
		for _, member := range nearbyByMember {
			if member.Name != "driver_001" { // 排除自己
				fmt.Printf("  %s - 距离%.2fkm\n", member.Name, member.Dist)
			}
		}
	}

	// 8. 模拟实时位置更新
	fmt.Println("\n🔄 模拟司机位置实时更新...")

	// 司机移动到新位置
	newLng, newLat := 116.405000, 39.905000
	err = rdb.GeoAdd(ctx, driversKey, &redis.GeoLocation{
		Name:      "driver_001",
		Longitude: newLng,
		Latitude:  newLat,
	}).Err()

	if err != nil {
		log.Printf("更新司机位置失败: %v", err)
	} else {
		fmt.Printf("driver_001 移动到新位置: (%.6f, %.6f)\n", newLng, newLat)

		// 重新计算与用户的距离
		newDist, err := rdb.GeoDist(ctx, driversKey, "driver_001", "user_001", "km").Result()
		if err == nil {
			fmt.Printf("更新后与user_001的距离: %.2fkm\n", newDist)
		}
	}

	// 9. 内存使用分析
	fmt.Println("\n💾 Geo内存使用分析...")
	memory, err := rdb.MemoryUsage(ctx, driversKey).Result()
	if err != nil {
		log.Printf("获取内存使用失败: %v", err)
	} else {
		driverCount, _ := rdb.ZCard(ctx, driversKey).Result() // Geo底层是ZSet
		fmt.Printf("Geo内存使用: %d bytes (%d个位置点)\n", memory, driverCount)
		fmt.Printf("平均每个位置点: %.2f bytes\n", float64(memory)/float64(driverCount))
	}

	// 设置过期时间 (在线司机4小时过期)
	rdb.Expire(ctx, driversKey, 4*time.Hour)
	rdb.Expire(ctx, usersKey, 2*time.Hour)
	// 商店位置不设置过期时间，永久保存

	fmt.Println("✅ Geo演示完成")
}

// 性能对比与分析
func performanceComparison(ctx context.Context, rdb *redis.Client) {
	fmt.Println("不同数据结构的性能和内存对比")

	// 测试场景: 10万用户的活跃统计
	userCount := 100000

	fmt.Printf("测试场景: %d用户的活跃统计\n", userCount)
	fmt.Println("\n对比维度:")

	// 1. Bitmap vs Set 内存对比
	fmt.Println("\n1️⃣ Bitmap vs Set - 内存使用对比")

	bitmapKey := "perf:bitmap:active"
	setKey := "perf:set:active"

	// 添加30%的活跃用户到两种结构
	activeUsers := int(float64(userCount) * 0.3)

	start := time.Now()
	pipeline := rdb.Pipeline()
	for i := 0; i < activeUsers; i++ {
		userID := rand.Intn(userCount) + 1
		pipeline.SetBit(ctx, bitmapKey, int64(userID), 1)
		pipeline.SAdd(ctx, setKey, userID)
	}
	pipeline.Exec(ctx)
	bitmapTime := time.Since(start)

	// 内存使用对比
	bitmapMem, _ := rdb.MemoryUsage(ctx, bitmapKey).Result()
	setMem, _ := rdb.MemoryUsage(ctx, setKey).Result()

	fmt.Printf("Bitmap内存: %d bytes\n", bitmapMem)
	fmt.Printf("Set内存: %d bytes\n", setMem)
	fmt.Printf("内存节省: %.2f%%\n", (1-float64(bitmapMem)/float64(setMem))*100)
	fmt.Printf("写入时间: %v\n", bitmapTime)

	// 2. HyperLogLog vs Set 精度对比
	fmt.Println("\n2️⃣ HyperLogLog vs Set - 精度与内存对比")

	hllKey := "perf:hll:uv"
	setUVKey := "perf:set:uv"

	// 模拟UV统计
	visits := 500000
	realUsers := make(map[string]bool)

	start = time.Now()
	pipeline = rdb.Pipeline()
	for i := 0; i < visits; i++ {
		userID := fmt.Sprintf("user_%d", rand.Intn(100000)+1)
		realUsers[userID] = true
		pipeline.PFAdd(ctx, hllKey, userID)
		pipeline.SAdd(ctx, setUVKey, userID)

		if i%1000 == 0 {
			pipeline.Exec(ctx)
			pipeline = rdb.Pipeline()
		}
	}
	pipeline.Exec(ctx)
	uvTime := time.Since(start)

	realUV := int64(len(realUsers))
	hllUV, _ := rdb.PFCount(ctx, hllKey).Result()
	setUV, _ := rdb.SCard(ctx, setUVKey).Result()

	hllMem, _ := rdb.MemoryUsage(ctx, hllKey).Result()
	setUVMem, _ := rdb.MemoryUsage(ctx, setUVKey).Result()

	hllError := math.Abs(float64(hllUV-realUV)) / float64(realUV) * 100

	fmt.Printf("真实UV: %d\n", realUV)
	fmt.Printf("HLL估算: %d (误差: %.2f%%)\n", hllUV, hllError)
	fmt.Printf("Set精确: %d\n", setUV)
	fmt.Printf("HLL内存: %d bytes\n", hllMem)
	fmt.Printf("Set内存: %d bytes\n", setUVMem)
	fmt.Printf("内存节省: %.2f%%\n", (1-float64(hllMem)/float64(setUVMem))*100)
	fmt.Printf("处理时间: %v\n", uvTime)

	// 3. Geo vs Hash 位置存储对比
	fmt.Println("\n3️⃣ Geo vs Hash - 位置存储对比")

	geoKey := "perf:geo:locations"
	hashKey := "perf:hash:locations"

	locationCount := 10000

	start = time.Now()
	pipeline = rdb.Pipeline()
	for i := 0; i < locationCount; i++ {
		name := fmt.Sprintf("loc_%d", i)
		lng := 116.0 + rand.Float64()*0.5 // 北京经度范围
		lat := 39.5 + rand.Float64()*0.5  // 北京纬度范围

		// Geo存储
		pipeline.GeoAdd(ctx, geoKey, &redis.GeoLocation{
			Name: name, Longitude: lng, Latitude: lat,
		})

		// Hash存储 (需要两个字段)
		pipeline.HMSet(ctx, hashKey, map[string]interface{}{
			name + ":lng": lng,
			name + ":lat": lat,
		})
	}
	pipeline.Exec(ctx)
	geoTime := time.Since(start)

	geoMem, _ := rdb.MemoryUsage(ctx, geoKey).Result()
	hashMem, _ := rdb.MemoryUsage(ctx, hashKey).Result()

	fmt.Printf("Geo内存: %d bytes\n", geoMem)
	fmt.Printf("Hash内存: %d bytes\n", hashMem)
	fmt.Printf("Geo优势: %.2f%%\n", (1-float64(geoMem)/float64(hashMem))*100)
	fmt.Printf("存储时间: %v\n", geoTime)

	// 4. 查询性能对比
	fmt.Println("\n4️⃣ 查询性能对比")

	// Bitmap查询性能
	start = time.Now()
	for i := 0; i < 1000; i++ {
		userID := rand.Intn(userCount) + 1
		rdb.GetBit(ctx, bitmapKey, int64(userID))
	}
	bitmapQueryTime := time.Since(start)

	// Set查询性能
	start = time.Now()
	for i := 0; i < 1000; i++ {
		userID := rand.Intn(userCount) + 1
		rdb.SIsMember(ctx, setKey, userID)
	}
	setQueryTime := time.Since(start)

	// Geo范围查询性能
	start = time.Now()
	for i := 0; i < 100; i++ {
		lng := 116.0 + rand.Float64()*0.5
		lat := 39.5 + rand.Float64()*0.5
		rdb.GeoRadius(ctx, geoKey, lng, lat, &redis.GeoRadiusQuery{
			Radius: 1, Unit: "km", Count: 10,
		})
	}
	geoQueryTime := time.Since(start)

	fmt.Printf("Bitmap查询1000次: %v (平均: %v)\n",
		bitmapQueryTime, bitmapQueryTime/1000)
	fmt.Printf("Set查询1000次: %v (平均: %v)\n",
		setQueryTime, setQueryTime/1000)
	fmt.Printf("Geo范围查询100次: %v (平均: %v)\n",
		geoQueryTime, geoQueryTime/100)

	// 清理测试数据
	rdb.Del(ctx, bitmapKey, setKey, hllKey, setUVKey, geoKey, hashKey)

	fmt.Println("\n✅ 性能对比完成")
}
