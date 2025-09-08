package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"what_redis_can_do/internal/lock"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

func main() {
	fmt.Println("=== Day 10: 分布式锁演示 ===")

	// 创建Redis客户端
	client, err := createRedisClient()
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	// 演示基本锁功能
	fmt.Println("\n1. 基本锁功能演示")
	basicLockDemo(client)

	// 演示Fencing Token
	fmt.Println("\n2. Fencing Token演示")
	fencingTokenDemo(client)

	// 演示自动续租
	fmt.Println("\n3. 自动续租演示")
	autoRenewDemo(client)

	// 演示并发竞争
	fmt.Println("\n4. 并发竞争演示")
	concurrentLockDemo(client)

	// 演示锁管理
	fmt.Println("\n5. 锁管理演示")
	lockManagerDemo(client)

	// 演示故障场景
	fmt.Println("\n6. 故障场景演示")
	failureScenarioDemo(client)

	fmt.Println("\n=== 演示完成 ===")
}

// createRedisClient 创建Redis客户端
func createRedisClient() (*redisx.Client, error) {
	config := redisx.DefaultConfig()
	config.Addr = "localhost:6379"
	config.DB = 1 // 使用数据库1避免干扰其他数据

	client, err := redisx.NewClient(config)
	if err != nil {
		return nil, err
	}

	// 健康检查
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		return nil, fmt.Errorf("Redis health check failed: %w", err)
	}

	// 清理测试数据
	client.FlushDB(ctx)

	return client, nil
}

// basicLockDemo 基本锁功能演示
func basicLockDemo(client redis.Cmdable) {
	// lockManager := lock.NewRedisLockManager(client, nil)
	// distributedLock := lockManager.CreateLock()
	lockService := lock.NewRedisLockService(client, nil)
	distributedLock := lockService.Lock

	ctx := context.Background()
	lockKey := "demo:basic_lock"
	owner := "demo_owner_1"

	fmt.Printf("尝试获取锁: %s\n", lockKey)

	// 尝试获取锁
	success, fencingToken, err := distributedLock.TryLock(ctx, lockKey, owner, 10*time.Second)
	if err != nil {
		log.Printf("Failed to try lock: %v", err)
		return
	}

	if success {
		fmt.Printf("✅ 成功获取锁，Fencing Token: %d\n", fencingToken)

		// 检查锁状态
		locked, lockOwner, token, err := distributedLock.IsLocked(ctx, lockKey)
		if err != nil {
			log.Printf("Failed to check lock status: %v", err)
		} else {
			fmt.Printf("锁状态: locked=%v, owner=%s, token=%d\n", locked, lockOwner, token)
		}

		// 模拟业务处理
		fmt.Println("执行业务逻辑...")
		time.Sleep(2 * time.Second)

		// 释放锁
		released, err := distributedLock.Unlock(ctx, lockKey, owner, fencingToken)
		if err != nil {
			log.Printf("Failed to unlock: %v", err)
		} else if released {
			fmt.Println("✅ 成功释放锁")
		}
	} else {
		fmt.Println("❌ 获取锁失败")
	}
}

// fencingTokenDemo Fencing Token演示
func fencingTokenDemo(client redis.Cmdable) {
	lockManager := lock.NewRedisLockManager(client, nil)
	distributedLock := lockManager.CreateLock()

	ctx := context.Background()
	lockKey := "demo:fencing_lock"
	owner1 := "owner_1"
	owner2 := "owner_2"

	// Owner1获取锁
	success1, token1, err := distributedLock.TryLock(ctx, lockKey, owner1, 5*time.Second)
	if err != nil {
		log.Printf("Owner1 failed to get lock: %v", err)
		return
	}

	if success1 {
		fmt.Printf("Owner1 获取锁成功，Token: %d\n", token1)

		// Owner2尝试获取同一把锁
		success2, token2, err := distributedLock.TryLock(ctx, lockKey, owner2, 5*time.Second)
		if err != nil {
			fmt.Printf("Owner2 尝试获取锁时出错: %v\n", err)
		} else if !success2 {
			fmt.Printf("Owner2 获取锁失败 (预期行为)\n")
		}

		// 等待锁过期
		fmt.Println("等待锁过期...")
		time.Sleep(6 * time.Second)

		// Owner2再次尝试获取锁
		success2, token2, err = distributedLock.TryLock(ctx, lockKey, owner2, 5*time.Second)
		if err != nil {
			log.Printf("Owner2 second attempt failed: %v", err)
		} else if success2 {
			fmt.Printf("Owner2 获取锁成功，Token: %d (Token递增)\n", token2)

			// Owner1尝试用旧token释放锁（应该失败）
			released, err := distributedLock.Unlock(ctx, lockKey, owner1, token1)
			if err != nil {
				fmt.Printf("Owner1 用旧Token释放锁失败: %v (预期行为)\n", err)
			} else if !released {
				fmt.Printf("Owner1 用旧Token释放锁失败 (预期行为)\n")
			}

			// Owner2用正确token释放锁
			released, err = distributedLock.Unlock(ctx, lockKey, owner2, token2)
			if err != nil {
				log.Printf("Owner2 unlock failed: %v", err)
			} else if released {
				fmt.Printf("Owner2 成功释放锁\n")
			}
		}
	}
}

// autoRenewDemo 自动续租演示
func autoRenewDemo(client redis.Cmdable) {
	lockManager := lock.NewRedisLockManager(client, nil)
	autoRenewLock := lockManager.CreateAutoRenewLock()

	ctx := context.Background()
	lockKey := "demo:auto_renew_lock"
	owner := "auto_renew_owner"
	lockTTL := 5 * time.Second
	renewInterval := 2 * time.Second

	fmt.Printf("获取锁并启动自动续租: TTL=%v, 续租间隔=%v\n", lockTTL, renewInterval)

	// 获取锁
	fencingToken, err := autoRenewLock.Lock(ctx, lockKey, owner, lockTTL, 10*time.Second)
	if err != nil {
		log.Printf("Failed to get lock: %v", err)
		return
	}

	fmt.Printf("✅ 获取锁成功，Token: %d\n", fencingToken)

	// 启动自动续租
	stopRenew, err := autoRenewLock.StartAutoRenew(ctx, lockKey, owner, lockTTL, renewInterval, fencingToken)
	if err != nil {
		log.Printf("Failed to start auto renew: %v", err)
		return
	}

	fmt.Println("✅ 自动续租已启动")

	// 模拟长时间业务处理
	fmt.Println("模拟长时间业务处理（12秒）...")
	for i := 0; i < 12; i++ {
		time.Sleep(1 * time.Second)

		// 检查锁状态
		locked, _, _, err := autoRenewLock.IsLocked(ctx, lockKey)
		if err != nil {
			log.Printf("Failed to check lock: %v", err)
		} else {
			fmt.Printf("第%d秒: 锁状态=%v\n", i+1, locked)
		}
	}

	// 停止自动续租
	fmt.Println("停止自动续租...")
	stopRenew()

	// 释放锁
	released, err := autoRenewLock.Unlock(ctx, lockKey, owner, fencingToken)
	if err != nil {
		log.Printf("Failed to unlock: %v", err)
	} else if released {
		fmt.Println("✅ 成功释放锁")
	}
}

// concurrentLockDemo 并发竞争演示
func concurrentLockDemo(client redis.Cmdable) {
	lockManager := lock.NewRedisLockManager(client, nil)
	lockKey := "demo:concurrent_lock"

	var wg sync.WaitGroup
	var successCount int32
	var mu sync.Mutex
	results := make([]string, 0)

	// 启动多个协程竞争同一把锁
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			distributedLock := lockManager.CreateLock()
			ctx := context.Background()
			owner := fmt.Sprintf("worker_%d", id)

			// 尝试获取锁
			success, fencingToken, err := distributedLock.TryLock(ctx, lockKey, owner, 3*time.Second)

			mu.Lock()
			if err != nil {
				results = append(results, fmt.Sprintf("Worker %d: 获取锁失败 - %v", id, err))
			} else if success {
				successCount++
				results = append(results, fmt.Sprintf("Worker %d: ✅ 获取锁成功，Token: %d", id, fencingToken))

				// 模拟业务处理
				time.Sleep(1 * time.Second)

				// 释放锁
				released, err := distributedLock.Unlock(ctx, lockKey, owner, fencingToken)
				if err != nil {
					results = append(results, fmt.Sprintf("Worker %d: 释放锁失败 - %v", id, err))
				} else if released {
					results = append(results, fmt.Sprintf("Worker %d: ✅ 释放锁成功", id))
				}
			} else {
				results = append(results, fmt.Sprintf("Worker %d: ❌ 获取锁失败 (被其他worker占用)", id))
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// 输出结果
	fmt.Printf("并发竞争结果 (成功获取锁的数量: %d/5):\n", successCount)
	for _, result := range results {
		fmt.Printf("  %s\n", result)
	}
}

// lockManagerDemo 锁管理演示
func lockManagerDemo(client redis.Cmdable) {
	lockManager := lock.NewRedisLockManager(client, nil)
	distributedLock := lockManager.CreateLock()
	ctx := context.Background()

	// 创建几个测试锁
	locks := []struct {
		key   string
		owner string
		ttl   time.Duration
	}{
		{"demo:lock_1", "owner_1", 10 * time.Second},
		{"demo:lock_2", "owner_2", 15 * time.Second},
		{"demo:lock_3", "owner_1", 20 * time.Second},
	}

	fmt.Println("创建测试锁...")
	var tokens []int64
	for _, l := range locks {
		success, token, err := distributedLock.TryLock(ctx, l.key, l.owner, l.ttl)
		if err != nil {
			log.Printf("Failed to create lock %s: %v", l.key, err)
			continue
		}
		if success {
			fmt.Printf("✅ 创建锁: %s (owner: %s, token: %d)\n", l.key, l.owner, token)
			tokens = append(tokens, token)
		}
	}

	// 列出所有锁
	fmt.Println("\n列出所有锁:")
	allLocks, err := lockManager.ListLocks(ctx, "demo:lock_*")
	if err != nil {
		log.Printf("Failed to list locks: %v", err)
	} else {
		for _, lockInfo := range allLocks {
			fmt.Printf("  锁: %s, 拥有者: %s, Token: %d, 创建时间: %v, 过期时间: %v\n",
				lockInfo.Key, lockInfo.Owner, lockInfo.FencingToken,
				lockInfo.CreatedAt.Format("15:04:05"), lockInfo.ExpiresAt.Format("15:04:05"))
		}
	}

	// 获取锁统计信息
	fmt.Println("\n锁统计信息:")
	stats, err := lockManager.GetLockStats(ctx, "demo:lock_*")
	if err != nil {
		log.Printf("Failed to get lock stats: %v", err)
	} else {
		fmt.Printf("  总锁数: %d\n", stats.TotalLocks)
		fmt.Printf("  活跃锁数: %d\n", stats.ActiveLocks)
		fmt.Printf("  过期锁数: %d\n", stats.ExpiredLocks)
		fmt.Printf("  拥有者统计: %v\n", stats.OwnerStats)
	}

	// 强制释放一个锁
	if len(allLocks) > 0 {
		lockToForce := allLocks[0].Key
		fmt.Printf("\n强制释放锁: %s\n", lockToForce)
		err := lockManager.ForceUnlock(ctx, lockToForce)
		if err != nil {
			log.Printf("Failed to force unlock: %v", err)
		} else {
			fmt.Printf("✅ 强制释放锁成功\n")
		}
	}
}

// failureScenarioDemo 故障场景演示
func failureScenarioDemo(client redis.Cmdable) {
	lockManager := lock.NewRedisLockManager(client, nil)
	distributedLock := lockManager.CreateLock()
	ctx := context.Background()

	fmt.Println("测试各种故障场景...")

	// 场景1：尝试释放不存在的锁
	fmt.Println("\n场景1: 释放不存在的锁")
	released, err := distributedLock.Unlock(ctx, "demo:nonexistent_lock", "owner", 123)
	if err != nil {
		fmt.Printf("❌ 释放不存在的锁失败: %v (预期行为)\n", err)
	} else {
		fmt.Printf("释放结果: %v\n", released)
	}

	// 场景2：用错误的owner释放锁
	fmt.Println("\n场景2: 用错误的owner释放锁")
	lockKey := "demo:wrong_owner_lock"
	success, token, err := distributedLock.TryLock(ctx, lockKey, "correct_owner", 10*time.Second)
	if err != nil {
		log.Printf("Failed to create lock: %v", err)
		return
	}
	if success {
		fmt.Printf("创建锁成功，Token: %d\n", token)

		// 尝试用错误的owner释放
		released, err := distributedLock.Unlock(ctx, lockKey, "wrong_owner", token)
		if err != nil {
			fmt.Printf("❌ 用错误owner释放锁失败: %v (预期行为)\n", err)
		} else {
			fmt.Printf("释放结果: %v\n", released)
		}

		// 用正确的owner释放
		released, err = distributedLock.Unlock(ctx, lockKey, "correct_owner", token)
		if err != nil {
			log.Printf("Failed to unlock with correct owner: %v", err)
		} else if released {
			fmt.Printf("✅ 用正确owner释放锁成功\n")
		}
	}

	// 场景3：锁过期后的操作
	fmt.Println("\n场景3: 锁过期后的操作")
	shortLockKey := "demo:short_ttl_lock"
	success, token, err = distributedLock.TryLock(ctx, shortLockKey, "owner", 1*time.Second)
	if err != nil {
		log.Printf("Failed to create short TTL lock: %v", err)
		return
	}
	if success {
		fmt.Printf("创建短TTL锁成功，Token: %d\n", token)

		// 等待锁过期
		fmt.Println("等待锁过期...")
		time.Sleep(2 * time.Second)

		// 尝试释放过期的锁
		released, err := distributedLock.Unlock(ctx, shortLockKey, "owner", token)
		if err != nil {
			fmt.Printf("❌ 释放过期锁失败: %v (预期行为)\n", err)
		} else {
			fmt.Printf("释放结果: %v\n", released)
		}

		// 尝试续租过期的锁
		renewed, err := distributedLock.Renew(ctx, shortLockKey, "owner", 5*time.Second, token)
		if err != nil {
			fmt.Printf("❌ 续租过期锁失败: %v (预期行为)\n", err)
		} else {
			fmt.Printf("续租结果: %v\n", renewed)
		}
	}
}
