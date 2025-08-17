package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func main1() {
	// Day 1: Redis 基础与命令面演示

	// 连接Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "", // no password
		DB:       0,  // default DB
	})

	ctx := context.Background()

	// 测试连接
	fmt.Println("=== 连接测试 ===")
	pong, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatal("Redis连接失败:", err)
	}
	fmt.Printf("PING: %s\n", pong)

	// 清理测试数据
	defer cleanup(ctx, rdb)

	// String 操作演示
	fmt.Println("\n=== String 字符串操作 ===")
	demonstrateString(ctx, rdb)

	// Hash 操作演示
	fmt.Println("\n=== Hash 哈希操作 ===")
	demonstrateHash(ctx, rdb)

	// List 操作演示
	fmt.Println("\n=== List 列表操作 ===")
	demonstrateList(ctx, rdb)

	// Set 操作演示
	fmt.Println("\n=== Set 集合操作 ===")
	demonstrateSet(ctx, rdb)

	// ZSet 操作演示
	fmt.Println("\n=== ZSet 有序集合操作 ===")
	demonstrateZSet(ctx, rdb)

	// 监控命令演示
	fmt.Println("\n=== 监控与观测 ===")
	demonstrateMonitoring(ctx, rdb)

	fmt.Println("\n=== Day 1 演示完成 ===")

}

func demonstrateString(ctx context.Context, rdb *redis.Client) {
	// 基本设置和获取
	err := rdb.Set(ctx, "user:1:name", "Alice", 0).Err()
	if err != nil {
		log.Printf("SET error: %v", err)
		return
	}

	val, err := rdb.Get(ctx, "user:1:name").Result()
	if err != nil {
		log.Printf("GET error: %v", err)
		return
	}
	fmt.Printf("GET user:1:name: %s\n", val)

	// 带过期时间的设置
	err = rdb.SetEx(ctx, "session:abc123", "user_data", 30*time.Second).Err()
	if err != nil {
		log.Printf("SETEX error: %v", err)
		return
	}

	// 检查TTL
	ttl, err := rdb.TTL(ctx, "session:abc123").Result()
	if err != nil {
		log.Printf("TTL error: %v", err)
		return
	}
	fmt.Printf("TTL session:abc123: %v\n", ttl)

	// 计数器操作
	counter, err := rdb.Incr(ctx, "page_views").Result()
	if err != nil {
		log.Printf("INCR error: %v", err)
		return
	}
	fmt.Printf("INCR page_views: %d\n", counter)

	// 批量操作
	err = rdb.MSet(ctx, "key1", "value1", "key2", "value2").Err()
	if err != nil {
		log.Printf("MSET error: %v", err)
		return
	}

	vals, err := rdb.MGet(ctx, "key1", "key2").Result()
	if err != nil {
		log.Printf("MGET error: %v", err)
		return
	}
	fmt.Printf("MGET key1, key2: %v\n", vals)
}

func demonstrateHash(ctx context.Context, rdb *redis.Client) {
	// Hash 操作 - 用户信息
	userKey := "user:1001"

	err := rdb.HMSet(ctx, userKey, map[string]interface{}{
		"name":  "Bob",
		"email": "bob@example.com",
		"age":   "25",
		"city":  "Beijing",
	}).Err()
	if err != nil {
		log.Printf("HMSET error: %v", err)
		return
	}

	// 获取单个字段
	name, err := rdb.HGet(ctx, userKey, "name").Result()
	if err != nil {
		log.Printf("HGET error: %v", err)
		return
	}
	fmt.Printf("HGET %s name: %s\n", userKey, name)

	// 获取多个字段
	fields, err := rdb.HMGet(ctx, userKey, "name", "email", "age").Result()
	if err != nil {
		log.Printf("HMGET error: %v", err)
		return
	}
	fmt.Printf("HMGET %s name,email,age: %v\n", userKey, fields)

	// 获取所有字段
	allFields, err := rdb.HGetAll(ctx, userKey).Result()
	if err != nil {
		log.Printf("HGETALL error: %v", err)
		return
	}
	fmt.Printf("HGETALL %s: %v\n", userKey, allFields)

	// 增加年龄
	newAge, err := rdb.HIncrBy(ctx, userKey, "age", 1).Result()
	if err != nil {
		log.Printf("HINCRBY error: %v", err)
		return
	}
	fmt.Printf("HINCRBY %s age 1: %d\n", userKey, newAge)
}

func demonstrateList(ctx context.Context, rdb *redis.Client) {
	queueKey := "task_queue"

	// 添加任务到队列
	tasks := []string{"task1", "task2", "task3", "task4"}
	for _, task := range tasks {
		err := rdb.LPush(ctx, queueKey, task).Err()
		if err != nil {
			log.Printf("LPUSH error: %v", err)
			continue
		}
	}

	// 检查队列长度
	length, err := rdb.LLen(ctx, queueKey).Result()
	if err != nil {
		log.Printf("LLEN error: %v", err)
		return
	}
	fmt.Printf("LLEN %s: %d\n", queueKey, length)

	// 查看队列内容（不弹出）
	items, err := rdb.LRange(ctx, queueKey, 0, -1).Result()
	if err != nil {
		log.Printf("LRANGE error: %v", err)
		return
	}
	fmt.Printf("LRANGE %s 0 -1: %v\n", queueKey, items)

	// 从队列取出任务
	task, err := rdb.RPop(ctx, queueKey).Result()
	if err != nil {
		log.Printf("RPOP error: %v", err)
		return
	}
	fmt.Printf("RPOP %s: %s\n", queueKey, task)

	// 再次检查长度
	length, err = rdb.LLen(ctx, queueKey).Result()
	if err != nil {
		log.Printf("LLEN error: %v", err)
		return
	}
	fmt.Printf("LLEN %s after RPOP: %d\n", queueKey, length)
}

func demonstrateSet(ctx context.Context, rdb *redis.Client) {
	tagsKey := "user:1001:tags"

	// 添加标签
	tags := []string{"golang", "redis", "docker", "kubernetes"}
	for _, tag := range tags {
		err := rdb.SAdd(ctx, tagsKey, tag).Err()
		if err != nil {
			log.Printf("SADD error: %v", err)
			continue
		}
	}

	// 检查成员数量
	count, err := rdb.SCard(ctx, tagsKey).Result()
	if err != nil {
		log.Printf("SCARD error: %v", err)
		return
	}
	fmt.Printf("SCARD %s: %d\n", tagsKey, count)

	// 检查是否包含某个标签
	exists, err := rdb.SIsMember(ctx, tagsKey, "golang").Result()
	if err != nil {
		log.Printf("SISMEMBER error: %v", err)
		return
	}
	fmt.Printf("SISMEMBER %s golang: %t\n", tagsKey, exists)

	// 获取所有成员
	members, err := rdb.SMembers(ctx, tagsKey).Result()
	if err != nil {
		log.Printf("SMEMBERS error: %v", err)
		return
	}
	fmt.Printf("SMEMBERS %s: %v\n", tagsKey, members)

	// 随机获取成员
	randomMembers, err := rdb.SRandMemberN(ctx, tagsKey, 2).Result()
	if err != nil {
		log.Printf("SRANDMEMBER error: %v", err)
		return
	}
	fmt.Printf("SRANDMEMBER %s 2: %v\n", tagsKey, randomMembers)
}

func demonstrateZSet(ctx context.Context, rdb *redis.Client) {
	leaderboardKey := "game:leaderboard"

	// 添加玩家分数
	players := []redis.Z{
		{Score: 1000, Member: "player1"},
		{Score: 1500, Member: "player2"},
		{Score: 800, Member: "player3"},
		{Score: 2000, Member: "player4"},
		{Score: 1200, Member: "player5"},
	}

	err := rdb.ZAdd(ctx, leaderboardKey, players...).Err()
	if err != nil {
		log.Printf("ZADD error: %v", err)
		return
	}

	// 获取排行榜（分数从高到低）
	topPlayers, err := rdb.ZRevRangeWithScores(ctx, leaderboardKey, 0, 2).Result()
	if err != nil {
		log.Printf("ZREVRANGE error: %v", err)
		return
	}
	fmt.Printf("Top 3 players:\n")
	for i, player := range topPlayers {
		fmt.Printf("  %d. %s: %.0f\n", i+1, player.Member, player.Score)
	}

	// 获取玩家排名
	rank, err := rdb.ZRevRank(ctx, leaderboardKey, "player2").Result()
	if err != nil {
		log.Printf("ZREVRANK error: %v", err)
		return
	}
	fmt.Printf("player2 rank: %d\n", rank+1) // rank is 0-based

	// 获取玩家分数
	score, err := rdb.ZScore(ctx, leaderboardKey, "player2").Result()
	if err != nil {
		log.Printf("ZSCORE error: %v", err)
		return
	}
	fmt.Printf("player2 score: %.0f\n", score)

	// 按分数范围查询
	highScorers, err := rdb.ZRangeByScoreWithScores(ctx, leaderboardKey, &redis.ZRangeBy{
		Min: "1200",
		Max: "+inf",
	}).Result()
	if err != nil {
		log.Printf("ZRANGEBYSCORE error: %v", err)
		return
	}
	fmt.Printf("High scorers (>= 1200): %v\n", highScorers)
}

func demonstrateMonitoring(ctx context.Context, rdb *redis.Client) {
	// 获取Redis信息
	info, err := rdb.Info(ctx, "memory").Result()
	if err != nil {
		log.Printf("INFO error: %v", err)
		return
	}
	fmt.Printf("Memory info (first 200 chars): %s...\n", info[:min(200, len(info))])

	// 获取数据库大小
	dbSize, err := rdb.DBSize(ctx).Result()
	if err != nil {
		log.Printf("DBSIZE error: %v", err)
		return
	}
	fmt.Printf("DBSIZE: %d\n", dbSize)

	// 获取慢查询日志
	slowLog, err := rdb.SlowLogGet(ctx, 5).Result()
	if err != nil {
		log.Printf("SLOWLOG error: %v", err)
		return
	}
	fmt.Printf("Recent slow queries count: %d\n", len(slowLog))
	for _, log := range slowLog {
		fmt.Printf("  ID: %d, Duration: %v, Command: %v\n",
			log.ID, log.Duration, log.Args[:min(3, len(log.Args))])
	}

	// 获取客户端信息
	clientInfo, err := rdb.ClientInfo(ctx).Result()
	if err != nil {
		log.Printf("CLIENT INFO error: %v", err)
		return
	}
	fmt.Printf("Client info: %s\n", clientInfo)
}

func cleanup(ctx context.Context, rdb *redis.Client) {
	// 清理测试数据
	keys := []string{
		"user:1:name", "session:abc123", "page_views",
		"key1", "key2", "user:1001", "task_queue",
		"user:1001:tags", "game:leaderboard",
	}

	for _, key := range keys {
		rdb.Del(ctx, key)
	}
	fmt.Println("\n=== 清理完成 ===")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
