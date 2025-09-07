package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

func main() {
	// 创建Redis客户端
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	defer client.Close()

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	fmt.Println("=== WATCH机制详细演示 ===")

	// 清理测试数据
	client.Del(ctx, "account:A", "account:B", "transfer_log")

	// 初始化账户余额
	client.Set(ctx, "account:A", "1000", 0)
	client.Set(ctx, "account:B", "500", 0)

	fmt.Println("\n1. 单线程WATCH成功案例")
	demonstrateSuccessfulWatch(ctx, client)

	fmt.Println("\n2. 并发冲突导致WATCH失败案例")
	demonstrateConcurrentWatchFailure(ctx, client)

	fmt.Println("\n3. 详细的WATCH失败过程演示")
	demonstrateWatchFailureProcess(ctx, client)

	fmt.Println("\n4. 重试机制演示")
	demonstrateRetryMechanism(ctx, client)
}

// demonstrateSuccessfulWatch 演示成功的WATCH操作
func demonstrateSuccessfulWatch(ctx context.Context, client *redis.Client) {
	fmt.Println("  场景：转账100元从A到B，无并发干扰")

	// 显示初始状态
	balanceA, _ := client.Get(ctx, "account:A").Result()
	balanceB, _ := client.Get(ctx, "account:B").Result()
	fmt.Printf("  转账前 - A: %s, B: %s\n", balanceA, balanceB)

	// 使用WATCH执行转账
	err := client.Watch(ctx, func(tx *redis.Tx) error {
		// 1. 读取当前余额（在WATCH保护下）
		balanceAStr, err := tx.Get(ctx, "account:A").Result()
		if err != nil {
			return err
		}
		balanceBStr, err := tx.Get(ctx, "account:B").Result()
		if err != nil {
			return err
		}

		balanceAInt, _ := strconv.Atoi(balanceAStr)
		balanceBInt, _ := strconv.Atoi(balanceBStr)

		fmt.Printf("  事务中读取 - A: %d, B: %d\n", balanceAInt, balanceBInt)

		// 2. 业务逻辑检查
		if balanceAInt < 100 {
			return fmt.Errorf("余额不足")
		}

		// 3. 在事务中执行修改
		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, "account:A", strconv.Itoa(balanceAInt-100), 0)
			pipe.Set(ctx, "account:B", strconv.Itoa(balanceBInt+100), 0)
			pipe.Incr(ctx, "transfer_log")
			return nil
		})

		return err
	}, "account:A", "account:B")

	if err != nil {
		fmt.Printf("  ❌ 转账失败: %v\n", err)
	} else {
		fmt.Printf("  ✅ 转账成功\n")

		// 显示最终状态
		balanceA, _ = client.Get(ctx, "account:A").Result()
		balanceB, _ = client.Get(ctx, "account:B").Result()
		transferCount, _ := client.Get(ctx, "transfer_log").Result()
		fmt.Printf("  转账后 - A: %s, B: %s, 转账次数: %s\n", balanceA, balanceB, transferCount)
	}
}

// demonstrateConcurrentWatchFailure 演示并发导致的WATCH失败
func demonstrateConcurrentWatchFailure(ctx context.Context, client *redis.Client) {
	fmt.Println("  场景：两个客户端同时转账，观察WATCH失败")

	// 重置账户余额
	client.Set(ctx, "account:A", "1000", 0)
	client.Set(ctx, "account:B", "500", 0)
	client.Set(ctx, "transfer_log", "0", 0)

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]string, 2)

	// 模拟两个并发的转账操作
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(clientID int) {
			defer wg.Done()

			// 创建独立的客户端连接
			clientConn := redis.NewClient(&redis.Options{
				Addr:     "localhost:6379",
				Password: "",
				DB:       0,
			})
			defer clientConn.Close()

			transferAmount := 50 + clientID*50 // 客户端0转50，客户端1转100

			err := clientConn.Watch(ctx, func(tx *redis.Tx) error {
				// 读取当前余额
				balanceAStr, err := tx.Get(ctx, "account:A").Result()
				if err != nil {
					return err
				}
				balanceBStr, err := tx.Get(ctx, "account:B").Result()
				if err != nil {
					return err
				}

				balanceAInt, _ := strconv.Atoi(balanceAStr)
				balanceBInt, _ := strconv.Atoi(balanceBStr)

				mu.Lock()
				fmt.Printf("  客户端%d读取 - A: %d, B: %d (准备转账%d)\n",
					clientID, balanceAInt, balanceBInt, transferAmount)
				mu.Unlock()

				// 模拟一些处理时间，增加冲突概率
				time.Sleep(time.Millisecond * 100)

				// 检查余额
				if balanceAInt < transferAmount {
					return fmt.Errorf("余额不足")
				}

				// 执行转账
				_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
					pipe.Set(ctx, "account:A", strconv.Itoa(balanceAInt-transferAmount), 0)
					pipe.Set(ctx, "account:B", strconv.Itoa(balanceBInt+transferAmount), 0)
					pipe.Incr(ctx, "transfer_log")
					return nil
				})

				return err
			}, "account:A", "account:B")

			mu.Lock()
			if err != nil {
				if err == redis.TxFailedErr {
					results[clientID] = fmt.Sprintf("客户端%d: ❌ WATCH失败(乐观锁冲突)", clientID)
				} else {
					results[clientID] = fmt.Sprintf("客户端%d: ❌ 错误: %v", clientID, err)
				}
			} else {
				results[clientID] = fmt.Sprintf("客户端%d: ✅ 转账成功", clientID)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	// 打印结果
	for _, result := range results {
		fmt.Printf("  %s\n", result)
	}

	// 显示最终状态
	balanceA, _ := client.Get(ctx, "account:A").Result()
	balanceB, _ := client.Get(ctx, "account:B").Result()
	transferCount, _ := client.Get(ctx, "transfer_log").Result()
	fmt.Printf("  最终状态 - A: %s, B: %s, 转账次数: %s\n", balanceA, balanceB, transferCount)
}

// demonstrateWatchFailureProcess 详细演示WATCH失败的过程
func demonstrateWatchFailureProcess(ctx context.Context, client *redis.Client) {
	fmt.Println("  场景：详细跟踪WATCH失败的每个步骤")

	// 重置状态
	client.Set(ctx, "watched_key", "initial_value", 0)

	// 创建两个客户端
	client1 := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 0})
	client2 := redis.NewClient(&redis.Options{Addr: "localhost:6379", DB: 0})
	defer client1.Close()
	defer client2.Close()

	var wg sync.WaitGroup
	var mu sync.Mutex

	// 客户端1：使用WATCH
	wg.Add(1)
	go func() {
		defer wg.Done()

		err := client1.Watch(ctx, func(tx *redis.Tx) error {
			mu.Lock()
			fmt.Printf("  [客户端1] 步骤1: WATCH watched_key\n")
			mu.Unlock()

			// 读取当前值
			currentValue, err := tx.Get(ctx, "watched_key").Result()
			if err != nil {
				return err
			}

			mu.Lock()
			fmt.Printf("  [客户端1] 步骤2: 读取到值 '%s'\n", currentValue)
			mu.Unlock()

			// 模拟业务处理时间
			time.Sleep(time.Millisecond * 200)

			mu.Lock()
			fmt.Printf("  [客户端1] 步骤3: 开始事务，准备修改为 'modified_by_client1'\n")
			mu.Unlock()

			// 在事务中修改
			_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.Set(ctx, "watched_key", "modified_by_client1", 0)
				pipe.Incr(ctx, "modification_count")
				return nil
			})

			return err
		}, "watched_key")

		mu.Lock()
		if err != nil {
			if err == redis.TxFailedErr {
				fmt.Printf("  [客户端1] 步骤4: ❌ EXEC返回nil - 监视的键被修改，事务被丢弃\n")
			} else {
				fmt.Printf("  [客户端1] 步骤4: ❌ 其他错误: %v\n", err)
			}
		} else {
			fmt.Printf("  [客户端1] 步骤4: ✅ 事务成功执行\n")
		}
		mu.Unlock()
	}()

	// 客户端2：在客户端1处理期间修改被监视的键
	wg.Add(1)
	go func() {
		defer wg.Done()

		// 等待客户端1开始WATCH
		time.Sleep(time.Millisecond * 100)

		mu.Lock()
		fmt.Printf("  [客户端2] 在客户端1事务期间修改 watched_key\n")
		mu.Unlock()

		// 直接修改被WATCH的键
		client2.Set(ctx, "watched_key", "modified_by_client2", 0)

		mu.Lock()
		fmt.Printf("  [客户端2] ✅ 成功修改 watched_key = 'modified_by_client2'\n")
		mu.Unlock()
	}()

	wg.Wait()

	// 检查最终结果
	finalValue, _ := client.Get(ctx, "watched_key").Result()
	modCount, _ := client.Get(ctx, "modification_count").Result()
	if modCount == "" {
		modCount = "0"
	}

	fmt.Printf("  最终结果: watched_key = '%s', modification_count = %s\n", finalValue, modCount)
	fmt.Printf("  分析: 由于客户端2在客户端1的事务执行前修改了watched_key，\n")
	fmt.Printf("        客户端1的整个事务被丢弃，modification_count没有增加\n")
}

// demonstrateRetryMechanism 演示重试机制
func demonstrateRetryMechanism(ctx context.Context, client *redis.Client) {
	fmt.Println("  场景：实现自动重试机制处理WATCH失败")

	// 重置状态
	client.Set(ctx, "counter", "0", 0)

	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	// 启动多个协程并发递增计数器
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// 创建独立连接
			clientConn := redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
				DB:   0,
			})
			defer clientConn.Close()

			// 重试逻辑
			maxRetries := 10
			for attempt := 0; attempt < maxRetries; attempt++ {
				err := clientConn.Watch(ctx, func(tx *redis.Tx) error {
					// 读取当前计数
					currentStr, err := tx.Get(ctx, "counter").Result()
					if err != nil {
						return err
					}

					currentInt, _ := strconv.Atoi(currentStr)
					newValue := currentInt + 1

					mu.Lock()
					fmt.Printf("  [协程%d] 尝试%d: 读取=%d, 准备设置=%d\n",
						goroutineID, attempt+1, currentInt, newValue)
					mu.Unlock()

					// 模拟一些处理时间
					time.Sleep(time.Millisecond * 50)

					// 执行递增
					_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
						pipe.Set(ctx, "counter", strconv.Itoa(newValue), 0)
						return nil
					})

					return err
				}, "counter")

				if err == nil {
					mu.Lock()
					successCount++
					fmt.Printf("  [协程%d] ✅ 第%d次尝试成功\n", goroutineID, attempt+1)
					mu.Unlock()
					break
				} else if err == redis.TxFailedErr {
					mu.Lock()
					fmt.Printf("  [协程%d] ⚠️  第%d次尝试失败(WATCH冲突)，准备重试\n", goroutineID, attempt+1)
					mu.Unlock()

					// 指数退避
					backoffTime := time.Millisecond * time.Duration(1<<uint(attempt))
					if backoffTime > time.Millisecond*100 {
						backoffTime = time.Millisecond * 100
					}
					time.Sleep(backoffTime)
				} else {
					mu.Lock()
					fmt.Printf("  [协程%d] ❌ 第%d次尝试出现其他错误: %v\n", goroutineID, attempt+1, err)
					mu.Unlock()
					break
				}
			}
		}(i)
	}

	wg.Wait()

	finalCounter, _ := client.Get(ctx, "counter").Result()
	fmt.Printf("  最终计数器值: %s\n", finalCounter)
	fmt.Printf("  成功操作数: %d/5\n", successCount)
	fmt.Printf("  说明: 通过重试机制，即使有WATCH冲突，最终所有操作都能成功\n")
}
