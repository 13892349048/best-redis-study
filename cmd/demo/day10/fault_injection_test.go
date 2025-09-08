package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"what_redis_can_do/internal/lock"
	"what_redis_can_do/internal/redisx"

	"github.com/redis/go-redis/v9"
)

// FaultInjectionTest 故障注入测试
type FaultInjectionTest struct {
	client      redis.Cmdable
	lockManager lock.LockManager
}

// NewFaultInjectionTest 创建故障注入测试
func NewFaultInjectionTest(client redis.Cmdable) *FaultInjectionTest {
	return &FaultInjectionTest{
		client:      client,
		lockManager: lock.NewRedisLockManager(client, nil),
	}
}

// RunAllTests 运行所有故障注入测试
func (f *FaultInjectionTest) RunAllTests() {
	fmt.Println("=== 故障注入测试开始 ===")

	// 1. GC停顿模拟测试
	fmt.Println("\n1. GC停顿模拟测试")
	f.testGCPause()

	// 2. 网络延迟测试
	fmt.Println("\n2. 网络延迟模拟测试")
	f.testNetworkLatency()

	// 3. 高并发竞争测试
	fmt.Println("\n3. 高并发竞争测试")
	f.testHighConcurrency()

	// 4. 锁泄漏测试
	fmt.Println("\n4. 锁泄漏测试")
	f.testLockLeak()

	// 5. 续租失败测试
	fmt.Println("\n5. 续租失败测试")
	f.testRenewFailure()

	fmt.Println("\n=== 故障注入测试完成 ===")
}

// testGCPause 测试GC停顿场景
func (f *FaultInjectionTest) testGCPause() {
	fmt.Println("模拟GC停顿导致的锁超时场景...")

	autoRenewLock := f.lockManager.CreateAutoRenewLock()
	ctx := context.Background()
	lockKey := "test:gc_pause_lock"
	owner := "gc_test_owner"
	lockTTL := 3 * time.Second
	renewInterval := 1 * time.Second

	// 获取锁并启动自动续租
	fencingToken, err := autoRenewLock.Lock(ctx, lockKey, owner, lockTTL, 10*time.Second)
	if err != nil {
		log.Printf("Failed to get lock: %v", err)
		return
	}

	fmt.Printf("✅ 获取锁成功，Token: %d，启动自动续租\n", fencingToken)

	stopRenew, err := autoRenewLock.StartAutoRenew(ctx, lockKey, owner, lockTTL, renewInterval, fencingToken)
	if err != nil {
		log.Printf("Failed to start auto renew: %v", err)
		return
	}

	// 模拟GC停顿
	fmt.Println("模拟GC停顿（5秒）...")
	go func() {
		time.Sleep(2 * time.Second)
		// 强制GC
		runtime.GC()
		// 模拟长时间停顿
		time.Sleep(5 * time.Second)
	}()

	// 检查锁状态
	for i := 0; i < 8; i++ {
		time.Sleep(1 * time.Second)
		locked, currentOwner, currentToken, err := autoRenewLock.IsLocked(ctx, lockKey)
		if err != nil {
			fmt.Printf("第%d秒: 检查锁状态失败 - %v\n", i+1, err)
		} else {
			fmt.Printf("第%d秒: 锁状态=%v, owner=%s, token=%d\n",
				i+1, locked, currentOwner, currentToken)
		}
	}

	stopRenew()
	fmt.Println("✅ GC停顿测试完成")
}

// testNetworkLatency 测试网络延迟场景
func (f *FaultInjectionTest) testNetworkLatency() {
	fmt.Println("模拟网络延迟场景...")

	distributedLock := f.lockManager.CreateLock()
	lockKey := "test:network_latency_lock"
	owner := "network_test_owner"

	var successCount int32
	var failureCount int32
	var wg sync.WaitGroup

	// 启动多个协程模拟网络延迟下的锁竞争
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 创建带超时的上下文模拟网络延迟
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			ownerID := fmt.Sprintf("%s_%d", owner, id)

			// 模拟网络延迟
			time.Sleep(time.Duration(id*100) * time.Millisecond)

			success, token, err := distributedLock.TryLock(ctx, lockKey, ownerID, 5*time.Second)
			if err != nil {
				atomic.AddInt32(&failureCount, 1)
				fmt.Printf("Worker %d: 获取锁失败 - %v\n", id, err)
				return
			}

			if success {
				atomic.AddInt32(&successCount, 1)
				fmt.Printf("Worker %d: ✅ 获取锁成功，Token: %d\n", id, token)

				// 模拟业务处理
				time.Sleep(500 * time.Millisecond)

				// 释放锁
				released, err := distributedLock.Unlock(ctx, lockKey, ownerID, token)
				if err != nil {
					fmt.Printf("Worker %d: 释放锁失败 - %v\n", id, err)
				} else if released {
					fmt.Printf("Worker %d: ✅ 释放锁成功\n", id)
				}
			} else {
				atomic.AddInt32(&failureCount, 1)
				fmt.Printf("Worker %d: ❌ 获取锁失败\n", id)
			}
		}(i)
	}

	wg.Wait()
	fmt.Printf("网络延迟测试结果: 成功=%d, 失败=%d\n", successCount, failureCount)
}

// testHighConcurrency 测试高并发场景
func (f *FaultInjectionTest) testHighConcurrency() {
	fmt.Println("高并发竞争测试...")

	const concurrency = 50
	const testDuration = 10 * time.Second

	var totalAttempts int64
	var successfulLocks int64
	var failedLocks int64
	var wg sync.WaitGroup

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	lockKey := "test:high_concurrency_lock"

	startTime := time.Now()

	// 启动多个协程进行高并发测试
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			distributedLock := f.lockManager.CreateLock()
			owner := fmt.Sprintf("worker_%d", workerID)

			for {
				select {
				case <-ctx.Done():
					return
				default:
					atomic.AddInt64(&totalAttempts, 1)

					success, token, err := distributedLock.TryLock(ctx, lockKey, owner, 100*time.Millisecond)
					if err != nil {
						atomic.AddInt64(&failedLocks, 1)
						continue
					}

					if success {
						atomic.AddInt64(&successfulLocks, 1)

						// 极短的业务处理时间
						time.Sleep(10 * time.Millisecond)

						// 释放锁
						distributedLock.Unlock(ctx, lockKey, owner, token)
					} else {
						atomic.AddInt64(&failedLocks, 1)
					}

					// 短暂休息
					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	fmt.Printf("高并发测试结果:\n")
	fmt.Printf("  测试时长: %v\n", elapsed)
	fmt.Printf("  并发数: %d\n", concurrency)
	fmt.Printf("  总尝试次数: %d\n", totalAttempts)
	fmt.Printf("  成功获取锁: %d\n", successfulLocks)
	fmt.Printf("  获取锁失败: %d\n", failedLocks)
	fmt.Printf("  成功率: %.2f%%\n", float64(successfulLocks)/float64(totalAttempts)*100)
	fmt.Printf("  QPS: %.2f\n", float64(totalAttempts)/elapsed.Seconds())
}

// testLockLeak 测试锁泄漏场景
func (f *FaultInjectionTest) testLockLeak() {
	fmt.Println("锁泄漏测试...")

	distributedLock := f.lockManager.CreateLock()
	ctx := context.Background()

	// 创建一些会"泄漏"的锁（模拟程序崩溃或网络中断）
	leakedLocks := []string{
		"test:leaked_lock_1",
		"test:leaked_lock_2",
		"test:leaked_lock_3",
	}

	fmt.Println("创建泄漏锁...")
	for i, lockKey := range leakedLocks {
		owner := fmt.Sprintf("leaked_owner_%d", i)
		success, token, err := distributedLock.TryLock(ctx, lockKey, owner, 30*time.Second)
		if err != nil {
			log.Printf("Failed to create leaked lock %s: %v", lockKey, err)
			continue
		}
		if success {
			fmt.Printf("✅ 创建泄漏锁: %s, Token: %d\n", lockKey, token)
		}
	}

	// 检查锁统计
	fmt.Println("\n检查锁统计...")
	stats, err := f.lockManager.GetLockStats(ctx, "test:leaked_lock_*")
	if err != nil {
		log.Printf("Failed to get lock stats: %v", err)
		return
	}

	fmt.Printf("泄漏锁统计: 总数=%d, 活跃=%d\n", stats.TotalLocks, stats.ActiveLocks)

	// 清理泄漏锁
	fmt.Println("\n清理泄漏锁...")
	cleanedCount, err := f.lockManager.CleanExpiredLocks(ctx, "test:leaked_lock_*")
	if err != nil {
		log.Printf("Failed to clean expired locks: %v", err)
		return
	}

	fmt.Printf("清理了 %d 个过期锁\n", cleanedCount)

	// 强制清理剩余锁
	for _, lockKey := range leakedLocks {
		err := f.lockManager.ForceUnlock(ctx, lockKey)
		if err != nil {
			fmt.Printf("强制清理锁 %s 失败: %v\n", lockKey, err)
		} else {
			fmt.Printf("✅ 强制清理锁: %s\n", lockKey)
		}
	}
}

// testRenewFailure 测试续租失败场景
func (f *FaultInjectionTest) testRenewFailure() {
	fmt.Println("续租失败测试...")

	autoRenewLock := f.lockManager.CreateAutoRenewLock()
	ctx := context.Background()
	lockKey := "test:renew_failure_lock"
	owner := "renew_test_owner"
	lockTTL := 2 * time.Second
	renewInterval := 500 * time.Millisecond

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

	// 运行一段时间让续租正常工作
	time.Sleep(3 * time.Second)

	// 模拟续租失败：手动删除锁
	fmt.Println("模拟续租失败：手动删除锁...")
	err = f.lockManager.ForceUnlock(ctx, lockKey)
	if err != nil {
		log.Printf("Failed to force unlock: %v", err)
	} else {
		fmt.Println("✅ 手动删除锁成功")
	}

	// 继续观察续租行为
	fmt.Println("观察续租行为...")
	for i := 0; i < 5; i++ {
		time.Sleep(1 * time.Second)

		locked, currentOwner, currentToken, err := autoRenewLock.IsLocked(ctx, lockKey)
		if err != nil {
			fmt.Printf("第%d秒: 检查锁状态失败 - %v\n", i+1, err)
		} else {
			fmt.Printf("第%d秒: 锁状态=%v, owner=%s, token=%d\n",
				i+1, locked, currentOwner, currentToken)
		}

		// 检查续租状态
		status, err := autoRenewLock.GetRenewStatus(lockKey, owner)
		if err != nil {
			fmt.Printf("第%d秒: 获取续租状态失败 - %v\n", i+1, err)
		} else {
			if status.LastError != nil {
				fmt.Printf("第%d秒: 续租状态=活跃:%v, 最后错误:%v\n",
					i+1, status.IsActive, status.LastError)
			}
		}
	}

	stopRenew()
	fmt.Println("✅ 续租失败测试完成")
}

// 运行故障注入测试的main函数
func runFaultInjectionTests() {
	fmt.Println("=== Day 10: 分布式锁故障注入测试 ===")

	// 创建Redis客户端
	config := redisx.DefaultConfig()
	config.Addr = "localhost:6379"
	config.DB = 2 // 使用数据库2避免干扰

	client, err := redisx.NewClient(config)
	if err != nil {
		log.Fatalf("Failed to create Redis client: %v", err)
	}
	defer client.Close()

	// 健康检查
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err != nil {
		log.Fatalf("Redis health check failed: %v", err)
	}

	// 清理测试数据
	client.FlushDB(ctx)

	// 运行故障注入测试
	tester := NewFaultInjectionTest(client)
	tester.RunAllTests()

	fmt.Println("\n=== 故障注入测试完成 ===")
}
