package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"what_redis_can_do/internal/consistency"
	"what_redis_can_do/internal/transaction"

	"github.com/redis/go-redis/v9"
)

func main11() {
	// 创建Redis客户端
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	defer client.Close()

	// 测试连接
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}

	fmt.Println("=== Day 9: 一致性与事务语义演示 ===")

	// 清理测试数据
	cleanupTestData(ctx, client)

	// 1. 演示基础事务功能
	fmt.Println("1. 基础事务功能演示")
	demonstrateBasicTransaction(ctx, client)

	// 2. 演示乐观锁机制
	fmt.Println("\n2. 乐观锁机制演示")
	demonstrateOptimisticLock(ctx, client)

	// 3. 演示Lua脚本原子操作
	fmt.Println("\n3. Lua脚本原子操作演示")
	demonstrateLuaScripts(ctx, client)

	// 4. 演示库存扣减一致性
	fmt.Println("\n4. 库存扣减一致性演示")
	demonstrateInventoryConsistency(ctx, client)

	// 5. 演示并发冲突处理
	fmt.Println("\n5. 并发冲突处理演示")
	demonstrateConcurrencyControl(ctx, client)

	// 6. 演示批量操作事务
	fmt.Println("\n6. 批量操作事务演示")
	demonstrateBatchTransaction(ctx, client)

	fmt.Println("\n=== 所有演示完成 ===")
}

// demonstrateBasicTransaction 演示基础事务功能
func demonstrateBasicTransaction(ctx context.Context, client *redis.Client) {
	txMgr := transaction.NewTransactionManager(client)

	// 事务操作：转账示例
	operation := func(ctx context.Context, tx transaction.TransactionContext) error {
		// 检查账户A余额
		balanceA, err := tx.Get("account:A")
		if err != nil && err != redis.Nil {
			return err
		}
		if err == redis.Nil {
			balanceA = "1000" // 初始余额
			tx.Set("account:A", balanceA, 0)
		}

		// 检查账户B余额
		balanceB, err := tx.Get("account:B")
		if err != nil && err != redis.Nil {
			return err
		}
		if err == redis.Nil {
			balanceB = "500" // 初始余额
			tx.Set("account:B", balanceB, 0)
		}

		// 转账100元从A到B
		balanceAInt, _ := strconv.Atoi(balanceA)
		balanceBInt, _ := strconv.Atoi(balanceB)

		if balanceAInt < 100 {
			return fmt.Errorf("insufficient balance")
		}

		// 执行转账
		tx.Set("account:A", strconv.Itoa(balanceAInt-100), 0)
		tx.Set("account:B", strconv.Itoa(balanceBInt+100), 0)

		return nil
	}

	result, err := txMgr.ExecuteTransaction(ctx, operation)
	if err != nil {
		fmt.Printf("  事务执行失败: %v\n", err)
		return
	}

	fmt.Printf("  事务执行成功: 执行了%d个命令，耗时%v\n", result.Commands, result.Duration)

	// 验证结果
	balanceA, _ := client.Get(ctx, "account:A").Result()
	balanceB, _ := client.Get(ctx, "account:B").Result()
	fmt.Printf("  账户A余额: %s, 账户B余额: %s\n", balanceA, balanceB)
}

// demonstrateOptimisticLock 演示乐观锁机制
func demonstrateOptimisticLock(ctx context.Context, client *redis.Client) {
	consistencyMgr := consistency.NewConsistencyManager(client)

	// 初始化计数器
	client.Set(ctx, "counter", "0", 0)

	// 乐观锁操作：递增计数器
	operation := func(ctx context.Context, tx consistency.TxContext) error {
		current, err := tx.Get("counter")
		if err != nil {
			return err
		}

		currentInt, _ := strconv.Atoi(current)
		newValue := currentInt + 1

		// 模拟一些处理时间
		time.Sleep(time.Millisecond * 10)

		return tx.Set("counter", strconv.Itoa(newValue), 0)
	}

	// 并发执行多个乐观锁操作
	var wg sync.WaitGroup
	successCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := consistencyMgr.ExecuteWithWatch(ctx, []string{"counter"}, operation,
				consistency.WithRetryPolicy(consistency.RetryPolicy{
					MaxRetries:    5,
					InitialDelay:  time.Millisecond * 5,
					MaxDelay:      time.Millisecond * 50,
					BackoffFactor: 2.0,
				}),
			)

			mu.Lock()
			if err == nil {
				successCount++
				fmt.Printf("  协程%d: 乐观锁操作成功\n", id)
			} else {
				fmt.Printf("  协程%d: 乐观锁操作失败: %v\n", id, err)
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	finalValue, _ := client.Get(ctx, "counter").Result()
	fmt.Printf("  最终计数器值: %s, 成功操作数: %d\n", finalValue, successCount)
}

// demonstrateLuaScripts 演示Lua脚本原子操作
func demonstrateLuaScripts(ctx context.Context, client *redis.Client) {
	scriptMgr := transaction.NewLuaScriptManager(client)

	// 加载所有脚本
	if err := scriptMgr.LoadAllScripts(ctx); err != nil {
		fmt.Printf("  脚本加载失败: %v\n", err)
		return
	}

	// 演示原子比较交换
	fmt.Println("  3.1 原子比较交换演示")
	client.Set(ctx, "cas_key", "old_value", 0)

	success, err := scriptMgr.CompareAndSwap(ctx, "cas_key", "old_value", "new_value", 60)
	if err != nil {
		fmt.Printf("    CAS操作失败: %v\n", err)
	} else {
		fmt.Printf("    CAS操作结果: %v\n", success)
		value, _ := client.Get(ctx, "cas_key").Result()
		fmt.Printf("    当前值: %s\n", value)
	}

	// 演示带限制的递增
	fmt.Println("  3.2 带限制的递增演示")
	client.Del(ctx, "limited_counter")

	for i := 0; i < 15; i++ {
		result, err := scriptMgr.IncrWithLimit(ctx, "limited_counter", 10, 1, 60)
		if err != nil {
			fmt.Printf("    递增失败: %v\n", err)
		} else if result == -1 {
			fmt.Printf("    第%d次递增: 达到限制\n", i+1)
		} else {
			fmt.Printf("    第%d次递增: 当前值=%d\n", i+1, result)
		}

		if result == -1 {
			break
		}
	}

	// 演示滑动窗口计数器
	fmt.Println("  3.3 滑动窗口计数器演示")
	client.Del(ctx, "sliding_window")

	currentTime := time.Now().UnixMilli()
	windowSize := int64(1000) // 1秒窗口
	limit := int64(5)         // 限制5次

	for i := 0; i < 8; i++ {
		count, status, err := scriptMgr.SlidingWindowCounter(ctx, "sliding_window", windowSize, limit, currentTime+int64(i*100), 1)
		if err != nil {
			fmt.Printf("    计数器操作失败: %v\n", err)
		} else {
			fmt.Printf("    第%d次请求: 计数=%d, 状态=%s\n", i+1, count, status)
		}

		time.Sleep(time.Millisecond * 100)
	}
}

// demonstrateInventoryConsistency 演示库存扣减一致性
func demonstrateInventoryConsistency(ctx context.Context, client *redis.Client) {
	consistencyMgr := consistency.NewConsistencyManager(client)
	inventoryMgr := consistency.NewInventoryManager(client, consistencyMgr, consistency.InventoryConfig{})

	// 初始化库存
	client.Set(ctx, "stock:item001", "100", 0)

	// 并发扣减库存
	var wg sync.WaitGroup
	results := make([]error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			_, err := inventoryMgr.DeductStock(ctx, "item001", 5)
			results[id] = err

			if err == nil {
				fmt.Printf("  协程%d: 扣减成功\n", id)
			} else {
				fmt.Printf("  协程%d: 扣减失败: %v\n", id, err)
			}
		}(i)
	}

	wg.Wait()

	// 统计结果
	successCount := 0
	for _, err := range results {
		if err == nil {
			successCount++
		}
	}

	finalStock, _ := client.Get(ctx, "stock:item001").Result()
	fmt.Printf("  最终库存: %s, 成功扣减次数: %d\n", finalStock, successCount)

	// 演示预留机制
	fmt.Println("  4.1 库存预留机制演示")
	client.Set(ctx, "stock:item002", "50", 0)

	// 预留库存
	err := inventoryMgr.ReserveStock(ctx, "item002", 10, "reservation001")
	if err != nil {
		fmt.Printf("    预留失败: %v\n", err)
	} else {
		fmt.Println("    预留成功")

		// 确认预留
		item, err := inventoryMgr.ConfirmReservation(ctx, "item002", 10, "reservation001")
		if err != nil {
			fmt.Printf("    确认预留失败: %v\n", err)
		} else {
			fmt.Printf("    确认预留成功，剩余库存: %d\n", item.Stock)
		}
	}
}

// demonstrateConcurrencyControl 演示并发冲突处理
func demonstrateConcurrencyControl(ctx context.Context, client *redis.Client) {
	consistencyMgr := consistency.NewConsistencyManager(client)

	// 初始化共享资源
	client.Set(ctx, "shared_resource", "0", 0)

	// 并发修改共享资源
	var wg sync.WaitGroup
	conflictCount := 0
	var mu sync.Mutex

	operation := func(ctx context.Context, tx consistency.TxContext) error {
		current, err := tx.Get("shared_resource")
		if err != nil {
			return err
		}

		currentInt, _ := strconv.Atoi(current)

		// 模拟复杂的业务逻辑
		time.Sleep(time.Millisecond * 50)

		newValue := currentInt + 1
		return tx.Set("shared_resource", strconv.Itoa(newValue), 0)
	}

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			err := consistencyMgr.ExecuteWithWatch(ctx, []string{"shared_resource"}, operation,
				consistency.WithRetryPolicy(consistency.RetryPolicy{
					MaxRetries:    10,
					InitialDelay:  time.Millisecond * 5,
					MaxDelay:      time.Millisecond * 100,
					BackoffFactor: 2.0,
				}),
				consistency.WithRetryCallback(func(attempt int, err error) {
					mu.Lock()
					conflictCount++
					mu.Unlock()
					fmt.Printf("  协程%d: 第%d次重试，原因: %v\n", id, attempt, err)
				}),
			)

			if err == nil {
				fmt.Printf("  协程%d: 操作成功\n", id)
			} else {
				fmt.Printf("  协程%d: 操作最终失败: %v\n", id, err)
			}
		}(i)
	}

	wg.Wait()

	finalValue, _ := client.Get(ctx, "shared_resource").Result()
	fmt.Printf("  最终值: %s, 总冲突次数: %d\n", finalValue, conflictCount)
}

// demonstrateBatchTransaction 演示批量操作事务
func demonstrateBatchTransaction(ctx context.Context, client *redis.Client) {
	txMgr := transaction.NewTransactionManager(client)

	// 批量操作
	operations := []transaction.BatchOperation{
		{Key: "batch:key1", Operation: "SET", Args: []interface{}{"value1"}},
		{Key: "batch:key2", Operation: "SET", Args: []interface{}{"value2"}},
		{Key: "batch:key3", Operation: "INCR", Args: []interface{}{}},
		{Key: "batch:key4", Operation: "HSET", Args: []interface{}{"field1", "value1"}},
		{Key: "batch:key5", Operation: "HSET", Args: []interface{}{"field2", "value2"}},
	}

	result, err := txMgr.BatchExecute(ctx, operations)
	if err != nil {
		fmt.Printf("  批量操作失败: %v\n", err)
		return
	}

	fmt.Printf("  批量操作结果: 总数=%d, 成功=%d, 失败=%d, 耗时=%v\n",
		result.Total, result.Successful, result.Failed, result.Duration)

	// 验证结果
	for i, opResult := range result.Results {
		if opResult.Success {
			fmt.Printf("    操作%d成功: %s %s\n", i+1, opResult.Operation.Operation, opResult.Operation.Key)
		} else {
			fmt.Printf("    操作%d失败: %v\n", i+1, opResult.Error)
		}
	}

	// 验证设置的值
	key1, _ := client.Get(ctx, "batch:key1").Result()
	key2, _ := client.Get(ctx, "batch:key2").Result()
	key3, _ := client.Get(ctx, "batch:key3").Result()
	fmt.Printf("  验证结果: key1=%s, key2=%s, key3=%s\n", key1, key2, key3)
}

// cleanupTestData 清理测试数据
func cleanupTestData(ctx context.Context, client *redis.Client) {
	keys := []string{
		"account:A", "account:B", "counter", "cas_key", "limited_counter",
		"sliding_window", "stock:item001", "stock:item002", "shared_resource",
		"batch:key1", "batch:key2", "batch:key3", "batch:key4", "batch:key5",
		"inventory_cache:item001", "inventory_cache:item002",
		"reservation:reservation001",
	}

	for _, key := range keys {
		client.Del(ctx, key)
	}
}
