package bloom

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

// StandardBloomFilter 标准布隆过滤器（基于Redis Bitmap实现）
type StandardBloomFilter struct {
	client    redis.Cmdable
	config    *BloomConfig
	bitSize   uint64
	hashFuncs []HashFunction
	keyPrefix string
}

// NewStandardBloomFilter 创建标准布隆过滤器
func NewStandardBloomFilter(client redis.Cmdable, config *BloomConfig) (*StandardBloomFilter, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// 计算最优参数
	bitSize, hashCount := config.OptimalParameters()

	bf := &StandardBloomFilter{
		client:    client,
		config:    config,
		bitSize:   bitSize,
		hashFuncs: GetHashFunctions(hashCount),
		keyPrefix: fmt.Sprintf("%s:%s", config.Namespace, config.Name),
	}

	return bf, nil
}

// Add 添加元素到布隆过滤器
func (bf *StandardBloomFilter) Add(ctx context.Context, key string) error {
	positions := bf.getPositions(key)

	// 使用Pipeline批量设置位
	pipe := bf.client.Pipeline()
	for _, pos := range positions {
		pipe.SetBit(ctx, bf.keyPrefix, int64(pos), 1)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add key %s: %w", key, err)
	}

	// 设置TTL（如果配置了）
	if bf.config.TTL > 0 {
		bf.client.Expire(ctx, bf.keyPrefix, time.Duration(bf.config.TTL)*time.Second)
	}

	return nil
}

// Test 测试元素是否可能存在
func (bf *StandardBloomFilter) Test(ctx context.Context, key string) (bool, error) {
	positions := bf.getPositions(key)

	// 使用Pipeline批量检查位
	pipe := bf.client.Pipeline()
	cmds := make([]*redis.IntCmd, len(positions))

	for i, pos := range positions {
		cmds[i] = pipe.GetBit(ctx, bf.keyPrefix, int64(pos))
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to test key %s: %w", key, err)
	}

	// 检查所有位是否都为1
	for _, cmd := range cmds {
		if cmd.Val() == 0 {
			return false, nil
		}
	}

	return true, nil
}

// AddMultiple 批量添加元素
func (bf *StandardBloomFilter) AddMultiple(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	pipe := bf.client.Pipeline()

	for _, key := range keys {
		positions := bf.getPositions(key)
		for _, pos := range positions {
			pipe.SetBit(ctx, bf.keyPrefix, int64(pos), 1)
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add multiple keys: %w", err)
	}

	// 设置TTL（如果配置了）
	if bf.config.TTL > 0 {
		bf.client.Expire(ctx, bf.keyPrefix, time.Duration(bf.config.TTL)*time.Second)
	}

	return nil
}

// TestMultiple 批量测试元素
func (bf *StandardBloomFilter) TestMultiple(ctx context.Context, keys []string) ([]bool, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	results := make([]bool, len(keys))
	pipe := bf.client.Pipeline()

	// 为每个key准备命令
	type keyCmd struct {
		keyIndex int
		cmds     []*redis.IntCmd
	}

	var keyCmds []keyCmd

	for i, key := range keys {
		positions := bf.getPositions(key)
		cmds := make([]*redis.IntCmd, len(positions))

		for j, pos := range positions {
			cmds[j] = pipe.GetBit(ctx, bf.keyPrefix, int64(pos))
		}

		keyCmds = append(keyCmds, keyCmd{
			keyIndex: i,
			cmds:     cmds,
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to test multiple keys: %w", err)
	}

	// 检查结果
	for _, keyCmd := range keyCmds {
		exists := true
		for _, cmd := range keyCmd.cmds {
			if cmd.Val() == 0 {
				exists = false
				break
			}
		}
		results[keyCmd.keyIndex] = exists
	}

	return results, nil
}

// Clear 清空布隆过滤器
func (bf *StandardBloomFilter) Clear(ctx context.Context) error {
	return bf.client.Del(ctx, bf.keyPrefix).Err()
}

// Info 获取布隆过滤器信息
func (bf *StandardBloomFilter) Info(ctx context.Context) (*BloomInfo, error) {
	// 获取位数组
	bits, err := bf.client.Get(ctx, bf.keyPrefix).Result()
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to get bloom filter data: %w", err)
	}

	var setBits uint64
	if err != redis.Nil {
		// 计算已设置的位数
		setBits = bf.countSetBits([]byte(bits))
	}

	// 估算已插入元素数量
	// n = -m * ln(1 - X/m) / k
	// 其中 m = 位数组大小，X = 已设置位数，k = 哈希函数数量
	var insertedElements uint64
	if setBits > 0 {
		m := float64(bf.bitSize)
		x := float64(setBits)
		k := float64(len(bf.hashFuncs))

		if x < m {
			n := -m * math.Log(1-x/m) / k
			insertedElements = uint64(math.Max(0, n))
		} else {
			insertedElements = bf.config.ExpectedElements
		}
	}

	// 计算实际假阳性率
	// p = (1 - e^(-kn/m))^k
	var actualFPR float64
	if insertedElements > 0 {
		k := float64(len(bf.hashFuncs))
		n := float64(insertedElements)
		m := float64(bf.bitSize)

		actualFPR = math.Pow(1-math.Exp(-k*n/m), k)
	}

	return &BloomInfo{
		BitSize:                 bf.bitSize,
		HashFunctions:           uint32(len(bf.hashFuncs)),
		ExpectedElements:        bf.config.ExpectedElements,
		FalsePositiveRate:       bf.config.FalsePositiveRate,
		InsertedElements:        insertedElements,
		ActualFalsePositiveRate: actualFPR,
		MemoryUsage:             bf.bitSize / 8, // 位转字节
	}, nil
}

// Close 关闭布隆过滤器
func (bf *StandardBloomFilter) Close() error {
	// Redis布隆过滤器不需要特殊关闭操作
	return nil
}

// getPositions 计算key在位数组中的位置
func (bf *StandardBloomFilter) getPositions(key string) []uint64 {
	data := []byte(key)
	positions := make([]uint64, len(bf.hashFuncs))

	for i, hashFunc := range bf.hashFuncs {
		hash := hashFunc(data)
		positions[i] = hash % bf.bitSize
	}

	return positions
}

// countSetBits 计算字节数组中设置的位数
func (bf *StandardBloomFilter) countSetBits(data []byte) uint64 {
	var count uint64
	for _, b := range data {
		count += uint64(popcount(b))
	}
	return count
}

// popcount 计算字节中设置的位数（Brian Kernighan算法）
func popcount(b byte) int {
	count := 0
	for b != 0 {
		b &= b - 1 // 清除最低位的1
		count++
	}
	return count
}

// ScalableBloomFilter 可扩展布隆过滤器
type ScalableBloomFilter struct {
	client    redis.Cmdable
	config    *BloomConfig
	filters   []*StandardBloomFilter
	capacity  uint64
	keyPrefix string
}

// NewScalableBloomFilter 创建可扩展布隆过滤器
func NewScalableBloomFilter(client redis.Cmdable, config *BloomConfig) (*ScalableBloomFilter, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	sbf := &ScalableBloomFilter{
		client:    client,
		config:    config,
		capacity:  config.ExpectedElements,
		keyPrefix: fmt.Sprintf("%s:scalable:%s", config.Namespace, config.Name),
	}

	// 创建第一个布隆过滤器
	firstConfig := *config
	firstConfig.Name = fmt.Sprintf("%s:0", config.Name)

	filter, err := NewStandardBloomFilter(client, &firstConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create first filter: %w", err)
	}

	sbf.filters = []*StandardBloomFilter{filter}

	return sbf, nil
}

// Add 添加元素
func (sbf *ScalableBloomFilter) Add(ctx context.Context, key string) error {
	// 检查当前过滤器是否需要扩展
	if err := sbf.maybeExpand(ctx); err != nil {
		return fmt.Errorf("failed to expand filter: %w", err)
	}

	// 添加到最新的过滤器
	lastFilter := sbf.filters[len(sbf.filters)-1]
	return lastFilter.Add(ctx, key)
}

// Test 测试元素是否存在
func (sbf *ScalableBloomFilter) Test(ctx context.Context, key string) (bool, error) {
	// 在所有过滤器中测试
	for _, filter := range sbf.filters {
		exists, err := filter.Test(ctx, key)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

// AddMultiple 批量添加元素
func (sbf *ScalableBloomFilter) AddMultiple(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := sbf.Add(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// TestMultiple 批量测试元素
func (sbf *ScalableBloomFilter) TestMultiple(ctx context.Context, keys []string) ([]bool, error) {
	results := make([]bool, len(keys))
	for i, key := range keys {
		exists, err := sbf.Test(ctx, key)
		if err != nil {
			return nil, err
		}
		results[i] = exists
	}
	return results, nil
}

// Clear 清空所有过滤器
func (sbf *ScalableBloomFilter) Clear(ctx context.Context) error {
	keys := make([]string, len(sbf.filters))
	for i, filter := range sbf.filters {
		keys[i] = filter.keyPrefix
	}

	if len(keys) > 0 {
		return sbf.client.Del(ctx, keys...).Err()
	}

	return nil
}

// Info 获取可扩展布隆过滤器信息
func (sbf *ScalableBloomFilter) Info(ctx context.Context) (*BloomInfo, error) {
	if len(sbf.filters) == 0 {
		return nil, fmt.Errorf("no filters available")
	}

	// 聚合所有过滤器的信息
	var totalBitSize uint64
	var totalInserted uint64
	var totalMemory uint64

	for _, filter := range sbf.filters {
		info, err := filter.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get filter info: %w", err)
		}

		totalBitSize += info.BitSize
		totalInserted += info.InsertedElements
		totalMemory += info.MemoryUsage
	}

	// 使用第一个过滤器的配置作为基准
	firstInfo, _ := sbf.filters[0].Info(ctx)

	return &BloomInfo{
		BitSize:                 totalBitSize,
		HashFunctions:           firstInfo.HashFunctions,
		ExpectedElements:        sbf.capacity * uint64(len(sbf.filters)),
		FalsePositiveRate:       sbf.config.FalsePositiveRate,
		InsertedElements:        totalInserted,
		ActualFalsePositiveRate: firstInfo.ActualFalsePositiveRate,
		MemoryUsage:             totalMemory,
	}, nil
}

// Close 关闭可扩展布隆过滤器
func (sbf *ScalableBloomFilter) Close() error {
	for _, filter := range sbf.filters {
		if err := filter.Close(); err != nil {
			return err
		}
	}
	return nil
}

// maybeExpand 检查是否需要扩展过滤器
func (sbf *ScalableBloomFilter) maybeExpand(ctx context.Context) error {
	if len(sbf.filters) == 0 {
		return fmt.Errorf("no filters available")
	}

	// 检查最后一个过滤器的负载
	lastFilter := sbf.filters[len(sbf.filters)-1]
	info, err := lastFilter.Info(ctx)
	if err != nil {
		return fmt.Errorf("failed to get filter info: %w", err)
	}

	// 如果插入元素数量超过80%容量，则扩展
	if float64(info.InsertedElements) > 0.8*float64(sbf.capacity) {
		return sbf.expand(ctx)
	}

	return nil
}

// expand 扩展过滤器
func (sbf *ScalableBloomFilter) expand(ctx context.Context) error {
	newIndex := len(sbf.filters)

	// 创建新的过滤器配置
	newConfig := *sbf.config
	newConfig.Name = fmt.Sprintf("%s:%d", sbf.config.Name, newIndex)
	newConfig.ExpectedElements = sbf.capacity

	// 降低假阳性率（每层过滤器的假阳性率应该递减）
	newConfig.FalsePositiveRate = sbf.config.FalsePositiveRate * math.Pow(0.5, float64(newIndex))

	filter, err := NewStandardBloomFilter(sbf.client, &newConfig)
	if err != nil {
		return fmt.Errorf("failed to create new filter: %w", err)
	}

	sbf.filters = append(sbf.filters, filter)

	return nil
}
