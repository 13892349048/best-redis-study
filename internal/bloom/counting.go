package bloom

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// CountingBloomFilterImpl 计数布隆过滤器实现
type CountingBloomFilterImpl struct {
	client      redis.Cmdable
	config      *BloomConfig
	bitSize     uint64
	hashFuncs   []HashFunction
	keyPrefix   string
	counterBits uint32 // 每个计数器的位数
	maxCount    uint32 // 最大计数值
}

// NewCountingBloomFilter 创建计数布隆过滤器
func NewCountingBloomFilter(client redis.Cmdable, config *BloomConfig) (*CountingBloomFilterImpl, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// 计算最优参数
	bitSize, hashCount := config.OptimalParameters()

	// 默认使用4位计数器（最大计数15）
	counterBits := uint32(4)
	maxCount := uint32(1<<counterBits) - 1

	cbf := &CountingBloomFilterImpl{
		client:      client,
		config:      config,
		bitSize:     bitSize,
		hashFuncs:   GetHashFunctions(hashCount),
		keyPrefix:   fmt.Sprintf("%s:counting:%s", config.Namespace, config.Name),
		counterBits: counterBits,
		maxCount:    maxCount,
	}

	return cbf, nil
}

// Add 添加元素到计数布隆过滤器
func (cbf *CountingBloomFilterImpl) Add(ctx context.Context, key string) error {
	positions := cbf.getPositions(key)

	// 使用Pipeline增加每个位置的计数器
	pipe := cbf.client.Pipeline()
	for _, pos := range positions {
		counterKey := cbf.getCounterKey(pos)
		pipe.Incr(ctx, counterKey)
	}

	results, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add key %s: %w", key, err)
	}

	// 检查是否有计数器溢出
	for i, result := range results {
		if incrResult, ok := result.(*redis.IntCmd); ok {
			count := incrResult.Val()
			if uint32(count) > cbf.maxCount {
				// 计数器溢出，回滚并报错
				cbf.rollbackAdd(ctx, positions[:i+1])
				return fmt.Errorf("counter overflow for key %s at position %d", key, positions[i])
			}
		}
	}

	// 设置TTL（如果配置了）
	if cbf.config.TTL > 0 {
		for _, pos := range positions {
			counterKey := cbf.getCounterKey(pos)
			cbf.client.Expire(ctx, counterKey, time.Duration(cbf.config.TTL)*time.Second)
		}
	}

	return nil
}

// Remove 从计数布隆过滤器中删除元素
func (cbf *CountingBloomFilterImpl) Remove(ctx context.Context, key string) error {
	positions := cbf.getPositions(key)

	// 首先检查所有位置的计数器是否都大于0
	pipe := cbf.client.Pipeline()
	getCmds := make([]*redis.StringCmd, len(positions))

	for i, pos := range positions {
		counterKey := cbf.getCounterKey(pos)
		getCmds[i] = pipe.Get(ctx, counterKey)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to check counters for key %s: %w", key, err)
	}

	// 验证所有计数器都大于0
	for i, getCmd := range getCmds {
		countStr := getCmd.Val()
		if countStr == "" {
			return fmt.Errorf("cannot remove key %s: counter at position %d is 0", key, positions[i])
		}

		count, err := strconv.ParseInt(countStr, 10, 64)
		if err != nil || count <= 0 {
			return fmt.Errorf("cannot remove key %s: counter at position %d is %d", key, positions[i], count)
		}
	}

	// 减少所有位置的计数器
	pipe = cbf.client.Pipeline()
	for _, pos := range positions {
		counterKey := cbf.getCounterKey(pos)
		pipe.Decr(ctx, counterKey)
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to remove key %s: %w", key, err)
	}

	return nil
}

// Test 测试元素是否可能存在
func (cbf *CountingBloomFilterImpl) Test(ctx context.Context, key string) (bool, error) {
	positions := cbf.getPositions(key)

	pipe := cbf.client.Pipeline()
	getCmds := make([]*redis.StringCmd, len(positions))

	for i, pos := range positions {
		counterKey := cbf.getCounterKey(pos)
		getCmds[i] = pipe.Get(ctx, counterKey)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to test key %s: %w", key, err)
	}

	// 检查所有计数器是否都大于0
	for _, getCmd := range getCmds {
		countStr := getCmd.Val()
		if countStr == "" {
			return false, nil
		}

		count, err := strconv.ParseInt(countStr, 10, 64)
		if err != nil || count <= 0 {
			return false, nil
		}
	}

	return true, nil
}

// Count 获取元素的最小计数值
func (cbf *CountingBloomFilterImpl) Count(ctx context.Context, key string) (uint32, error) {
	positions := cbf.getPositions(key)

	pipe := cbf.client.Pipeline()
	getCmds := make([]*redis.StringCmd, len(positions))

	for i, pos := range positions {
		counterKey := cbf.getCounterKey(pos)
		getCmds[i] = pipe.Get(ctx, counterKey)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get count for key %s: %w", key, err)
	}

	minCount := uint32(^uint32(0)) // 最大uint32值
	hasValidCount := false

	for _, getCmd := range getCmds {
		countStr := getCmd.Val()
		if countStr == "" {
			return 0, nil // 如果有任何位置为0，则最小计数为0
		}

		count, err := strconv.ParseUint(countStr, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("invalid counter value: %s", countStr)
		}

		if uint32(count) < minCount {
			minCount = uint32(count)
		}
		hasValidCount = true
	}

	if !hasValidCount {
		return 0, nil
	}

	return minCount, nil
}

// AddMultiple 批量添加元素
func (cbf *CountingBloomFilterImpl) AddMultiple(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := cbf.Add(ctx, key); err != nil {
			return fmt.Errorf("failed to add key %s: %w", key, err)
		}
	}
	return nil
}

// RemoveMultiple 批量删除元素
func (cbf *CountingBloomFilterImpl) RemoveMultiple(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := cbf.Remove(ctx, key); err != nil {
			return fmt.Errorf("failed to remove key %s: %w", key, err)
		}
	}
	return nil
}

// TestMultiple 批量测试元素
func (cbf *CountingBloomFilterImpl) TestMultiple(ctx context.Context, keys []string) ([]bool, error) {
	results := make([]bool, len(keys))
	for i, key := range keys {
		exists, err := cbf.Test(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("failed to test key %s: %w", key, err)
		}
		results[i] = exists
	}
	return results, nil
}

// Clear 清空计数布隆过滤器
func (cbf *CountingBloomFilterImpl) Clear(ctx context.Context) error {
	// 使用SCAN命令找到所有相关的计数器key
	var keys []string
	iter := cbf.client.Scan(ctx, 0, cbf.keyPrefix+":*", 0).Iterator()

	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if len(keys) > 0 {
		return cbf.client.Del(ctx, keys...).Err()
	}

	return nil
}

// Info 获取计数布隆过滤器信息
func (cbf *CountingBloomFilterImpl) Info(ctx context.Context) (*BloomInfo, error) {
	// 扫描所有计数器
	var totalCounters uint64
	var nonZeroCounters uint64

	iter := cbf.client.Scan(ctx, 0, cbf.keyPrefix+":*", 0).Iterator()
	for iter.Next(ctx) {
		totalCounters++

		// 检查计数器值
		val, err := cbf.client.Get(ctx, iter.Val()).Result()
		if err != nil && err != redis.Nil {
			continue
		}

		if val != "" && val != "0" {
			nonZeroCounters++
		}
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan counters: %w", err)
	}

	// 估算插入元素数量（简化估算）
	var insertedElements uint64
	if nonZeroCounters > 0 && len(cbf.hashFuncs) > 0 {
		// 粗略估算：非零计数器数量除以哈希函数数量
		insertedElements = nonZeroCounters / uint64(len(cbf.hashFuncs))
	}

	return &BloomInfo{
		BitSize:                 cbf.bitSize,
		HashFunctions:           uint32(len(cbf.hashFuncs)),
		ExpectedElements:        cbf.config.ExpectedElements,
		FalsePositiveRate:       cbf.config.FalsePositiveRate,
		InsertedElements:        insertedElements,
		ActualFalsePositiveRate: 0, // 难以精确计算
		MemoryUsage:             totalCounters * uint64(cbf.counterBits) / 8,
	}, nil
}

// Close 关闭计数布隆过滤器
func (cbf *CountingBloomFilterImpl) Close() error {
	return nil
}

// getPositions 计算key在位数组中的位置
func (cbf *CountingBloomFilterImpl) getPositions(key string) []uint64 {
	data := []byte(key)
	positions := make([]uint64, len(cbf.hashFuncs))

	for i, hashFunc := range cbf.hashFuncs {
		hash := hashFunc(data)
		positions[i] = hash % cbf.bitSize
	}

	return positions
}

// getCounterKey 获取计数器的Redis key
func (cbf *CountingBloomFilterImpl) getCounterKey(position uint64) string {
	return fmt.Sprintf("%s:%d", cbf.keyPrefix, position)
}

// rollbackAdd 回滚添加操作（在计数器溢出时使用）
func (cbf *CountingBloomFilterImpl) rollbackAdd(ctx context.Context, positions []uint64) {
	pipe := cbf.client.Pipeline()
	for _, pos := range positions {
		counterKey := cbf.getCounterKey(pos)
		pipe.Decr(ctx, counterKey)
	}
	pipe.Exec(ctx) // 忽略错误，这是清理操作
}

// OptimizedCountingBloomFilter 优化的计数布隆过滤器
// 使用Redis的Hash数据结构存储计数器，减少key数量
type OptimizedCountingBloomFilter struct {
	client      redis.Cmdable
	config      *BloomConfig
	bitSize     uint64
	hashFuncs   []HashFunction
	keyPrefix   string
	bucketSize  uint64 // 每个桶的大小
	bucketCount uint64 // 桶的数量
}

// NewOptimizedCountingBloomFilter 创建优化的计数布隆过滤器
func NewOptimizedCountingBloomFilter(client redis.Cmdable, config *BloomConfig, bucketSize uint64) (*OptimizedCountingBloomFilter, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if bucketSize == 0 {
		bucketSize = 1000 // 默认每个桶1000个计数器
	}

	// 计算最优参数
	bitSize, hashCount := config.OptimalParameters()
	bucketCount := (bitSize + bucketSize - 1) / bucketSize // 向上取整

	ocbf := &OptimizedCountingBloomFilter{
		client:      client,
		config:      config,
		bitSize:     bitSize,
		hashFuncs:   GetHashFunctions(hashCount),
		keyPrefix:   fmt.Sprintf("%s:opt_counting:%s", config.Namespace, config.Name),
		bucketSize:  bucketSize,
		bucketCount: bucketCount,
	}

	return ocbf, nil
}

// Add 添加元素
func (ocbf *OptimizedCountingBloomFilter) Add(ctx context.Context, key string) error {
	positions := ocbf.getPositions(key)

	// 按桶分组操作
	bucketOps := make(map[uint64][]uint64)
	for _, pos := range positions {
		bucketIndex := pos / ocbf.bucketSize
		field := pos % ocbf.bucketSize
		bucketOps[bucketIndex] = append(bucketOps[bucketIndex], field)
	}

	// 执行批量操作
	pipe := ocbf.client.Pipeline()
	for bucketIndex, fields := range bucketOps {
		bucketKey := ocbf.getBucketKey(bucketIndex)
		for _, field := range fields {
			pipe.HIncrBy(ctx, bucketKey, strconv.FormatUint(field, 10), 1)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add key %s: %w", key, err)
	}

	// 设置TTL
	if ocbf.config.TTL > 0 {
		for bucketIndex := range bucketOps {
			bucketKey := ocbf.getBucketKey(bucketIndex)
			ocbf.client.Expire(ctx, bucketKey, time.Duration(ocbf.config.TTL)*time.Second)
		}
	}

	return nil
}

// Test 测试元素是否存在
func (ocbf *OptimizedCountingBloomFilter) Test(ctx context.Context, key string) (bool, error) {
	positions := ocbf.getPositions(key)

	// 按桶分组查询
	bucketOps := make(map[uint64][]uint64)
	for _, pos := range positions {
		bucketIndex := pos / ocbf.bucketSize
		field := pos % ocbf.bucketSize
		bucketOps[bucketIndex] = append(bucketOps[bucketIndex], field)
	}

	// 执行批量查询
	pipe := ocbf.client.Pipeline()
	type bucketQuery struct {
		bucketIndex uint64
		fields      []uint64
		cmd         *redis.SliceCmd
	}

	var queries []bucketQuery
	for bucketIndex, fields := range bucketOps {
		bucketKey := ocbf.getBucketKey(bucketIndex)
		fieldStrs := make([]string, len(fields))
		for i, field := range fields {
			fieldStrs[i] = strconv.FormatUint(field, 10)
		}

		cmd := pipe.HMGet(ctx, bucketKey, fieldStrs...)
		queries = append(queries, bucketQuery{
			bucketIndex: bucketIndex,
			fields:      fields,
			cmd:         cmd,
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to test key %s: %w", key, err)
	}

	// 检查所有计数器是否都大于0
	for _, query := range queries {
		values := query.cmd.Val()
		for _, val := range values {
			if val == nil {
				return false, nil
			}

			if countStr, ok := val.(string); ok {
				count, err := strconv.ParseInt(countStr, 10, 64)
				if err != nil || count <= 0 {
					return false, nil
				}
			} else {
				return false, nil
			}
		}
	}

	return true, nil
}

// 其他方法的实现类似，主要是将单独的Redis key改为Hash的field操作

// getBucketKey 获取桶的Redis key
func (ocbf *OptimizedCountingBloomFilter) getBucketKey(bucketIndex uint64) string {
	return fmt.Sprintf("%s:bucket:%d", ocbf.keyPrefix, bucketIndex)
}

// getPositions 计算key在位数组中的位置
func (ocbf *OptimizedCountingBloomFilter) getPositions(key string) []uint64 {
	data := []byte(key)
	positions := make([]uint64, len(ocbf.hashFuncs))

	for i, hashFunc := range ocbf.hashFuncs {
		hash := hashFunc(data)
		positions[i] = hash % ocbf.bitSize
	}

	return positions
}
