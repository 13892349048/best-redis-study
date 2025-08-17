package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	// 连接Redis
	rdb := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})

	ctx := context.Background()

	// 测试连接
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		log.Fatalf("连接Redis失败: %v", err)
	}

	fmt.Println("=== Day 2: 核心数据结构与业务建模 ===\n")

	// 场景1: 计数器 (String + INCR/DECR)
	fmt.Println("🔢 场景1: 计数器场景")
	demonstrateCounter(ctx, rdb)
	fmt.Println()

	// 场景2: 去重集 (Set)
	fmt.Println("🎯 场景2: 去重集场景")
	demonstrateDeduplication(ctx, rdb)
	fmt.Println()

	// 场景3: 排行榜 (ZSet)
	fmt.Println("🏆 场景3: 排行榜场景")
	demonstrateRanking(ctx, rdb)
	fmt.Println()

	// 场景4: 会话管理 (Hash)
	fmt.Println("👤 场景4: 会话管理场景")
	demonstrateSession(ctx, rdb)
	fmt.Println()

	// 场景5: 任务队列 (List)
	fmt.Println("📋 场景5: 任务队列场景")
	demonstrateTaskQueue(ctx, rdb)
	fmt.Println()

	// 大Key/热Key演示
	fmt.Println("⚠️  大Key/热Key风险演示")
	demonstrateBigKeyRisks(ctx, rdb)
}

// 场景1: 计数器 - 页面访问量、点赞数、库存数量
func demonstrateCounter(ctx context.Context, rdb *redis.Client) {
	fmt.Println("使用场景: 页面PV/UV统计、点赞数、库存管理")

	// Key设计: {业务}:{资源类型}:{资源ID}:{指标}
	pageViewKey := "stats:page:home:pv"
	likesKey := "social:post:12345:likes"
	stockKey := "inventory:product:iphone15:stock"

	// 1. 页面访问量统计
	fmt.Printf("📊 页面访问量统计 (Key: %s)\n", pageViewKey)

	// 模拟多次访问
	for i := 0; i < 5; i++ {
		pv, err := rdb.Incr(ctx, pageViewKey).Result()
		if err != nil {
			log.Printf("INCR失败: %v", err)
			continue
		}
		fmt.Printf("  第%d次访问, 当前PV: %d\n", i+1, pv)
	}

	// 设置过期时间 (当天结束)
	rdb.ExpireAt(ctx, pageViewKey, time.Now().Add(24*time.Hour))

	// 2. 点赞功能
	fmt.Printf("\n👍 点赞功能 (Key: %s)\n", likesKey)
	likes, _ := rdb.IncrBy(ctx, likesKey, 3).Result()
	fmt.Printf("  获得3个赞, 总赞数: %d\n", likes)

	// 取消1个赞
	likes, _ = rdb.DecrBy(ctx, likesKey, 1).Result()
	fmt.Printf("  取消1个赞, 当前赞数: %d\n", likes)

	// 3. 库存管理 (原子扣减)
	fmt.Printf("\n📦 库存管理 (Key: %s)\n", stockKey)

	// 初始化库存
	rdb.Set(ctx, stockKey, 100, 0)

	// 模拟购买扣减库存
	stock, _ := rdb.DecrBy(ctx, stockKey, 2).Result()
	fmt.Printf("  售出2台, 剩余库存: %d\n", stock)

	// 检查库存是否充足
	if stock < 0 {
		fmt.Println("  ❌ 库存不足!")
		rdb.IncrBy(ctx, stockKey, 2) // 回滚
	} else {
		fmt.Println("  ✅ 库存充足")
	}

	fmt.Printf("\n💡 Key设计要点:\n")
	fmt.Printf("  - 统一命名规范: {业务}:{资源类型}:{资源ID}:{指标}\n")
	fmt.Printf("  - TTL策略: 统计类数据设置合理过期时间\n")
	fmt.Printf("  - 原子性: 使用INCR/DECR保证并发安全\n")
}

// 场景2: 去重集 - 用户签到、IP黑名单、标签系统
func demonstrateDeduplication(ctx context.Context, rdb *redis.Client) {
	fmt.Println("使用场景: 每日签到、IP黑名单、文章标签、访客去重")

	// Key设计
	checkinKey := "checkin:daily:" + time.Now().Format("2006-01-02")
	blacklistKey := "security:ip_blacklist"
	tagsKey := "content:article:999:tags"

	// 1. 每日签到去重
	fmt.Printf("📅 每日签到 (Key: %s)\n", checkinKey)

	users := []string{"user_1001", "user_1002", "user_1001"} // user_1001重复签到
	for _, userID := range users {
		added, err := rdb.SAdd(ctx, checkinKey, userID).Result()
		if err != nil {
			log.Printf("签到失败: %v", err)
			continue
		}
		if added == 1 {
			fmt.Printf("  ✅ %s 签到成功\n", userID)
		} else {
			fmt.Printf("  ❌ %s 今日已签到\n", userID)
		}
	}

	// 查看今日签到总数
	count, _ := rdb.SCard(ctx, checkinKey).Result()
	fmt.Printf("  今日签到人数: %d\n", count)

	// 设置当日过期
	rdb.ExpireAt(ctx, checkinKey, time.Now().Add(24*time.Hour))

	// 2. IP黑名单
	fmt.Printf("\n🚫 IP黑名单管理 (Key: %s)\n", blacklistKey)

	// 添加恶意IP
	maliciousIPs := []string{"192.168.1.100", "10.0.0.50", "172.16.1.200"}
	rdb.SAdd(ctx, blacklistKey, maliciousIPs)
	fmt.Printf("  添加%d个恶意IP到黑名单\n", len(maliciousIPs))

	// 检查IP是否在黑名单
	checkIP := "192.168.1.100"
	exists, _ := rdb.SIsMember(ctx, blacklistKey, checkIP).Result()
	if exists {
		fmt.Printf("  ❌ IP %s 在黑名单中，拒绝访问\n", checkIP)
	} else {
		fmt.Printf("  ✅ IP %s 正常\n", checkIP)
	}

	// 3. 文章标签
	fmt.Printf("\n🏷️  文章标签 (Key: %s)\n", tagsKey)

	tags := []string{"Redis", "Go", "数据库", "缓存", "Go"} // "Go"重复
	for _, tag := range tags {
		added, _ := rdb.SAdd(ctx, tagsKey, tag).Result()
		if added == 1 {
			fmt.Printf("  ✅ 添加标签: %s\n", tag)
		} else {
			fmt.Printf("  ⚠️  标签 %s 已存在\n", tag)
		}
	}

	// 获取所有标签
	allTags, _ := rdb.SMembers(ctx, tagsKey).Result()
	fmt.Printf("  文章标签列表: %v\n", allTags)

	fmt.Printf("\n💡 Key设计要点:\n")
	fmt.Printf("  - 时间维度: 按日期分片 (checkin:daily:2024-01-15)\n")
	fmt.Printf("  - 全局集合: 持久化数据无需过期 (黑名单)\n")
	fmt.Printf("  - 去重特性: 利用Set天然去重避免重复逻辑\n")
}

// 场景3: 排行榜 - 游戏积分、热门文章、销量排行
func demonstrateRanking(ctx context.Context, rdb *redis.Client) {
	fmt.Println("使用场景: 游戏积分榜、热门文章、商品销量、学生成绩")

	// Key设计
	gameRankKey := "ranking:game:weekly"
	hotArticlesKey := "ranking:articles:daily"

	// 1. 游戏积分排行榜
	fmt.Printf("🎮 游戏积分排行榜 (Key: %s)\n", gameRankKey)

	// 添加玩家积分
	players := map[string]float64{
		"player_001": 15680,
		"player_002": 23450,
		"player_003": 18920,
		"player_004": 31200,
		"player_005": 27800,
	}

	for player, score := range players {
		rdb.ZAdd(ctx, gameRankKey, redis.Z{Score: score, Member: player})
		fmt.Printf("  玩家 %s 积分: %.0f\n", player, score)
	}

	// 获取前3名
	fmt.Println("\n🏆 积分排行榜 TOP 3:")
	top3, _ := rdb.ZRevRangeWithScores(ctx, gameRankKey, 0, 2).Result()
	for i, player := range top3 {
		fmt.Printf("  第%d名: %s (%.0f分)\n", i+1, player.Member, player.Score)
	}

	// 查看特定玩家排名
	player := "player_003"
	rank, _ := rdb.ZRevRank(ctx, gameRankKey, player).Result()
	score, _ := rdb.ZScore(ctx, gameRankKey, player).Result()
	fmt.Printf("\n🎯 玩家 %s: 第%d名, 积分%.0f\n", player, rank+1, score)

	// 获取积分范围内的玩家
	fmt.Println("\n📊 积分20000-30000的玩家:")
	midRange, _ := rdb.ZRangeByScoreWithScores(ctx, gameRankKey, &redis.ZRangeBy{
		Min: "20000",
		Max: "30000",
	}).Result()
	for _, player := range midRange {
		fmt.Printf("  %s: %.0f分\n", player.Member, player.Score)
	}

	// 2. 热门文章 (按浏览量)
	fmt.Printf("\n📰 热门文章排行 (Key: %s)\n", hotArticlesKey)

	articles := map[string]float64{
		"article_101": 5600, // 浏览量
		"article_102": 8900,
		"article_103": 12300,
		"article_104": 3400,
		"article_105": 15600,
	}

	for article, views := range articles {
		rdb.ZAdd(ctx, hotArticlesKey, redis.Z{Score: views, Member: article})
	}

	// 模拟文章浏览量增加
	rdb.ZIncrBy(ctx, hotArticlesKey, 1000, "article_101")
	fmt.Printf("  article_101 浏览量+1000\n")

	// 获取热门文章前5
	fmt.Println("\n🔥 今日热门文章 TOP 5:")
	hot5, _ := rdb.ZRevRangeWithScores(ctx, hotArticlesKey, 0, 4).Result()
	for i, article := range hot5 {
		fmt.Printf("  第%d名: %s (%.0f次浏览)\n", i+1, article.Member, article.Score)
	}

	// 设置过期时间
	rdb.Expire(ctx, gameRankKey, 7*24*time.Hour)  // 周榜保存7天
	rdb.Expire(ctx, hotArticlesKey, 24*time.Hour) // 日榜保存1天

	fmt.Printf("\n💡 Key设计要点:\n")
	fmt.Printf("  - 时间维度: 按周/日/月分片不同榜单\n")
	fmt.Printf("  - 分数更新: 使用ZINCRBY支持增量更新\n")
	fmt.Printf("  - 范围查询: 支持按分数范围和排名范围查询\n")
	fmt.Printf("  - TTL策略: 根据业务需求设置合理过期时间\n")
}

// 场景4: 会话管理 - 用户会话、购物车、表单暂存
func demonstrateSession(ctx context.Context, rdb *redis.Client) {
	fmt.Println("使用场景: 用户会话、购物车、表单草稿、用户偏好设置")

	// Key设计
	sessionKey := "session:user:12345"
	cartKey := "cart:user:12345"

	// 1. 用户会话信息
	fmt.Printf("🔐 用户会话 (Key: %s)\n", sessionKey)

	// 存储会话信息
	sessionData := map[string]interface{}{
		"user_id":     "12345",
		"username":    "john_doe",
		"login_time":  time.Now().Unix(),
		"ip":          "192.168.1.100",
		"role":        "user",
		"preferences": `{"theme": "dark", "lang": "zh-CN"}`,
	}

	err := rdb.HMSet(ctx, sessionKey, sessionData).Err()
	if err != nil {
		log.Printf("设置会话失败: %v", err)
		return
	}

	fmt.Printf("  ✅ 用户会话已创建\n")

	// 读取会话信息
	userID, _ := rdb.HGet(ctx, sessionKey, "user_id").Result()
	username, _ := rdb.HGet(ctx, sessionKey, "username").Result()
	loginTime, _ := rdb.HGet(ctx, sessionKey, "login_time").Result()

	fmt.Printf("  用户ID: %s\n", userID)
	fmt.Printf("  用户名: %s\n", username)
	fmt.Printf("  登录时间: %s\n", loginTime)

	// 更新最后活动时间
	rdb.HSet(ctx, sessionKey, "last_active", time.Now().Unix())
	fmt.Printf("  ✅ 更新最后活动时间\n")

	// 2. 购物车管理
	fmt.Printf("\n🛒 购物车管理 (Key: %s)\n", cartKey)

	// 添加商品到购物车
	cartItems := map[string]interface{}{
		"product_001": `{"name": "iPhone 15", "price": 5999, "qty": 1}`,
		"product_002": `{"name": "AirPods Pro", "price": 1899, "qty": 2}`,
		"product_003": `{"name": "MacBook Pro", "price": 14999, "qty": 1}`,
	}

	rdb.HMSet(ctx, cartKey, cartItems)
	fmt.Printf("  ✅ 添加3件商品到购物车\n")

	// 修改商品数量
	rdb.HSet(ctx, cartKey, "product_002", `{"name": "AirPods Pro", "price": 1899, "qty": 3}`)
	fmt.Printf("  ✅ AirPods Pro 数量更新为3\n")

	// 获取购物车商品数量
	itemCount, _ := rdb.HLen(ctx, cartKey).Result()
	fmt.Printf("  购物车商品种类: %d\n", itemCount)

	// 获取特定商品
	product, _ := rdb.HGet(ctx, cartKey, "product_001").Result()
	fmt.Printf("  iPhone 15 信息: %s\n", product)

	// 删除商品
	rdb.HDel(ctx, cartKey, "product_003")
	fmt.Printf("  ✅ 移除MacBook Pro\n")

	// 获取所有商品
	fmt.Printf("  购物车内容:\n")
	allItems, _ := rdb.HGetAll(ctx, cartKey).Result()
	for productID, info := range allItems {
		fmt.Printf("    %s: %s\n", productID, info)
	}

	// 设置会话过期时间
	rdb.Expire(ctx, sessionKey, 30*time.Minute) // 30分钟无活动过期
	rdb.Expire(ctx, cartKey, 24*time.Hour)      // 购物车保存24小时

	fmt.Printf("\n💡 Key设计要点:\n")
	fmt.Printf("  - 用户维度: 按用户ID分片存储\n")
	fmt.Printf("  - 字段更新: 使用HSET/HDEL支持部分更新\n")
	fmt.Printf("  - TTL策略: 会话30分钟，购物车24小时\n")
	fmt.Printf("  - JSON存储: 复杂对象序列化为JSON字符串\n")
}

// 场景5: 任务队列 - 异步任务、消息队列、延时任务
func demonstrateTaskQueue(ctx context.Context, rdb *redis.Client) {
	fmt.Println("使用场景: 异步任务、邮件队列、图片处理、数据同步")

	// Key设计
	emailQueueKey := "queue:email"
	imageQueueKey := "queue:image_process"

	// 1. 邮件发送队列
	fmt.Printf("📧 邮件发送队列 (Key: %s)\n", emailQueueKey)

	// 生产者: 添加邮件任务
	emailTasks := []string{
		`{"to": "user1@example.com", "subject": "Welcome", "type": "welcome"}`,
		`{"to": "user2@example.com", "subject": "Reset Password", "type": "reset_pwd"}`,
		`{"to": "user3@example.com", "subject": "Order Confirm", "type": "order"}`,
		`{"to": "user4@example.com", "subject": "Newsletter", "type": "newsletter"}`,
	}

	fmt.Printf("  📤 生产者添加任务:\n")
	for i, task := range emailTasks {
		rdb.LPush(ctx, emailQueueKey, task)
		fmt.Printf("    任务%d: %s\n", i+1, task)
	}

	// 查看队列长度
	queueLen, _ := rdb.LLen(ctx, emailQueueKey).Result()
	fmt.Printf("  队列长度: %d\n", queueLen)

	// 消费者: 处理邮件任务
	fmt.Printf("\n  📥 消费者处理任务:\n")
	for i := 0; i < 3; i++ {
		task, err := rdb.RPop(ctx, emailQueueKey).Result()
		if err == redis.Nil {
			fmt.Printf("    队列为空\n")
			break
		} else if err != nil {
			log.Printf("    消费失败: %v", err)
			break
		}

		fmt.Printf("    ✅ 处理任务: %s\n", task)

		// 模拟任务处理时间
		time.Sleep(100 * time.Millisecond)
	}

	// 查看剩余任务
	remaining, _ := rdb.LLen(ctx, emailQueueKey).Result()
	fmt.Printf("  剩余任务: %d\n", remaining)

	// 2. 图片处理队列 (带优先级)
	fmt.Printf("\n🖼️  图片处理队列 (Key: %s)\n", imageQueueKey)

	// 添加不同优先级的任务
	highPriorityTasks := []string{
		`{"image": "avatar_001.jpg", "resize": "200x200", "priority": "high"}`,
		`{"image": "profile_002.jpg", "resize": "100x100", "priority": "high"}`,
	}

	normalTasks := []string{
		`{"image": "banner_001.jpg", "resize": "1920x1080", "priority": "normal"}`,
		`{"image": "thumb_002.jpg", "resize": "300x200", "priority": "normal"}`,
	}

	// 高优先级任务从头部插入
	fmt.Printf("  📤 添加高优先级任务:\n")
	for _, task := range highPriorityTasks {
		rdb.LPush(ctx, imageQueueKey, task)
		fmt.Printf("    %s\n", task)
	}

	// 普通任务从尾部插入
	fmt.Printf("  📤 添加普通任务:\n")
	for _, task := range normalTasks {
		rdb.RPush(ctx, imageQueueKey, task)
		fmt.Printf("    %s\n", task)
	}

	// 查看队列内容 (不消费)
	fmt.Printf("\n  📋 队列内容预览:\n")
	tasks, _ := rdb.LRange(ctx, imageQueueKey, 0, -1).Result()
	for i, task := range tasks {
		fmt.Printf("    位置%d: %s\n", i, task)
	}

	// 批量消费
	fmt.Printf("\n  📥 批量消费任务:\n")
	for i := 0; i < len(tasks); i++ {
		task, err := rdb.LPop(ctx, imageQueueKey).Result()
		if err == redis.Nil {
			break
		}
		fmt.Printf("    ✅ 处理: %s\n", task)
	}

	fmt.Printf("\n💡 Key设计要点:\n")
	fmt.Printf("  - 队列命名: queue:{业务类型}\n")
	fmt.Printf("  - 生产消费: LPUSH生产, RPOP消费 (FIFO)\n")
	fmt.Printf("  - 优先级: 高优先级LPUSH, 普通RPUSH\n")
	fmt.Printf("  - 阻塞消费: 生产环境使用BRPOP避免轮询\n")
	fmt.Printf("  - 持久化: 无TTL确保任务不丢失\n")
}

// 大Key/热Key风险演示
func demonstrateBigKeyRisks(ctx context.Context, rdb *redis.Client) {
	fmt.Println("演示大Key和热Key的性能风险")

	// 创建大Hash演示
	bigHashKey := "big_hash_demo"

	fmt.Printf("🔍 创建大Hash Key: %s\n", bigHashKey)
	start := time.Now()

	// 模拟添加大量字段
	for i := 0; i < 100000; i++ {
		field := fmt.Sprintf("field_%04d", i)
		value := fmt.Sprintf("这是第%d个字段的值，包含一些测试数据", i)
		rdb.HSet(ctx, bigHashKey, field, value)
	}

	createTime := time.Since(start)
	fmt.Printf("  创建1000个字段耗时: %v\n", createTime)

	// 获取所有字段 (危险操作)
	fmt.Printf("  ⚠️  执行HGETALL (危险)...\n")
	start = time.Now()
	allFields, _ := rdb.HGetAll(ctx, bigHashKey).Result()
	hgetallTime := time.Since(start)
	fmt.Printf("  HGETALL返回%d个字段，耗时: %v\n", len(allFields), hgetallTime)

	// 推荐的分页获取方式
	fmt.Printf("  ✅ 推荐使用HSCAN分页获取:\n")
	start = time.Now()
	cursor := uint64(0)
	count := 0
	for {
		keys, newCursor, err := rdb.HScan(ctx, bigHashKey, cursor, "*", 1).Result()
		if err != nil {
			break
		}
		count += len(keys) / 2 // keys包含field和value
		cursor = newCursor
		if cursor == 0 {
			break
		}
	}
	hscanTime := time.Since(start)
	fmt.Printf("  HSCAN分页获取%d个字段，耗时: %v\n", count, hscanTime)

	// 清理演示数据
	rdb.Del(ctx, bigHashKey)

	fmt.Printf("\n💡 大Key风险与解决方案:\n")
	fmt.Printf("  ❌ 风险: 单个Key过大导致操作阻塞\n")
	fmt.Printf("  ❌ 风险: HGETALL/SMEMBERS等O(N)命令性能差\n")
	fmt.Printf("  ❌ 风险: 网络传输时间长，内存占用高\n")
	fmt.Printf("  ✅ 方案: 使用SCAN系列命令分页获取\n")
	fmt.Printf("  ✅ 方案: 拆分大Key为多个小Key\n")
	fmt.Printf("  ✅ 方案: 使用合适的数据结构和编码\n")

	fmt.Printf("\n💡 热Key识别与处理:\n")
	fmt.Printf("  🔍 识别: 监控SLOWLOG和INFO commandstats\n")
	fmt.Printf("  🔍 识别: 使用redis-cli --hotkeys (Redis 4.0.1+)\n")
	fmt.Printf("  ✅ 处理: 本地缓存减少Redis访问\n")
	fmt.Printf("  ✅ 处理: 读写分离，读请求路由到从库\n")
	fmt.Printf("  ✅ 处理: 数据分片，避免热点集中\n")
}
