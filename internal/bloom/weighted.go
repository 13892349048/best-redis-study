package bloom

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// WeightInfo 权重信息
type WeightInfo struct {
	Weight    float64 `json:"weight"`
	Count     uint32  `json:"count"`
	Timestamp int64   `json:"timestamp"`
}

// WeightedBloomFilterImpl 加权布隆过滤器实现
type WeightedBloomFilterImpl struct {
	client       redis.Cmdable
	config       *BloomConfig
	bitSize      uint64
	hashFuncs    []HashFunction
	keyPrefix    string
	weightPrefix string
	bucketSize   uint64
	bucketCount  uint64

	// 权重配置
	maxWeight      float64
	weightDecay    float64 // 权重衰减系数
	timeWindowSecs int64   // 时间窗口（秒）
}

// WeightedBloomConfig 加权布隆过滤器配置
type WeightedBloomConfig struct {
	*BloomConfig
	MaxWeight      float64 // 最大权重值
	WeightDecay    float64 // 权重衰减系数 (0.0-1.0)
	TimeWindowSecs int64   // 时间窗口（秒）
	BucketSize     uint64  // 桶大小
}

// DefaultWeightedBloomConfig 默认加权布隆过滤器配置
func DefaultWeightedBloomConfig(name string) *WeightedBloomConfig {
	return &WeightedBloomConfig{
		BloomConfig:    DefaultBloomConfig(name),
		MaxWeight:      100.0,
		WeightDecay:    0.95,
		TimeWindowSecs: 3600, // 1小时
		BucketSize:     1000,
	}
}

// NewWeightedBloomFilter 创建加权布隆过滤器
func NewWeightedBloomFilter(client redis.Cmdable, config *WeightedBloomConfig) (*WeightedBloomFilterImpl, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	// 计算最优参数
	bitSize, hashCount := config.BloomConfig.OptimalParameters()
	bucketCount := (bitSize + config.BucketSize - 1) / config.BucketSize

	wbf := &WeightedBloomFilterImpl{
		client:         client,
		config:         config.BloomConfig,
		bitSize:        bitSize,
		hashFuncs:      GetHashFunctions(hashCount),
		keyPrefix:      fmt.Sprintf("%s:weighted:%s", config.Namespace, config.Name),
		weightPrefix:   fmt.Sprintf("%s:weighted:%s:weights", config.Namespace, config.Name),
		bucketSize:     config.BucketSize,
		bucketCount:    bucketCount,
		maxWeight:      config.MaxWeight,
		weightDecay:    config.WeightDecay,
		timeWindowSecs: config.TimeWindowSecs,
	}

	return wbf, nil
}

// Add 添加元素（默认权重1.0）
func (wbf *WeightedBloomFilterImpl) Add(ctx context.Context, key string) error {
	return wbf.AddWithWeight(ctx, key, 1.0)
}

// AddWithWeight 添加带权重的元素
func (wbf *WeightedBloomFilterImpl) AddWithWeight(ctx context.Context, key string, weight float64) error {
	if weight <= 0 {
		return fmt.Errorf("weight must be positive")
	}
	if weight > wbf.maxWeight {
		weight = wbf.maxWeight
	}

	positions := wbf.getPositions(key)
	currentTime := getCurrentTimestamp()

	// 准备批量操作
	pipe := wbf.client.Pipeline()

	// 按桶分组操作
	bucketOps := make(map[uint64][]bucketOp)
	for _, pos := range positions {
		bucketIndex := pos / wbf.bucketSize
		field := pos % wbf.bucketSize
		bucketOps[bucketIndex] = append(bucketOps[bucketIndex], bucketOp{
			field:  field,
			weight: weight,
		})
	}

	// 更新位计数器和权重信息
	for bucketIndex, ops := range bucketOps {
		bitBucketKey := wbf.getBitBucketKey(bucketIndex)
		weightBucketKey := wbf.getWeightBucketKey(bucketIndex)

		for _, op := range ops {
			fieldStr := strconv.FormatUint(op.field, 10)

			// 增加位计数器
			pipe.HIncrBy(ctx, bitBucketKey, fieldStr, 1)

			// 更新权重信息
			weightInfo := WeightInfo{
				Weight:    op.weight,
				Count:     1,
				Timestamp: currentTime,
			}

			weightData, _ := json.Marshal(weightInfo)
			pipe.HSet(ctx, weightBucketKey, fieldStr, string(weightData))
		}
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to add weighted key %s: %w", key, err)
	}

	// 设置TTL
	if wbf.config.TTL > 0 {
		for bucketIndex := range bucketOps {
			bitBucketKey := wbf.getBitBucketKey(bucketIndex)
			weightBucketKey := wbf.getWeightBucketKey(bucketIndex)
			wbf.client.Expire(ctx, bitBucketKey, time.Duration(wbf.config.TTL)*time.Second)
			wbf.client.Expire(ctx, weightBucketKey, time.Duration(wbf.config.TTL)*time.Second)
		}
	}

	return nil
}

// Test 测试元素是否存在
func (wbf *WeightedBloomFilterImpl) Test(ctx context.Context, key string) (bool, error) {
	exists, _, err := wbf.TestWithWeight(ctx, key)
	return exists, err
}

// TestWithWeight 测试元素并返回权重
func (wbf *WeightedBloomFilterImpl) TestWithWeight(ctx context.Context, key string) (bool, float64, error) {
	positions := wbf.getPositions(key)

	// 按桶分组查询
	bucketQueries := make(map[uint64][]uint64)
	for _, pos := range positions {
		bucketIndex := pos / wbf.bucketSize
		field := pos % wbf.bucketSize
		bucketQueries[bucketIndex] = append(bucketQueries[bucketIndex], field)
	}

	// 执行批量查询
	pipe := wbf.client.Pipeline()
	type queryResult struct {
		bucketIndex uint64
		fields      []uint64
		bitCmd      *redis.SliceCmd
		weightCmd   *redis.SliceCmd
	}

	var results []queryResult
	for bucketIndex, fields := range bucketQueries {
		bitBucketKey := wbf.getBitBucketKey(bucketIndex)
		weightBucketKey := wbf.getWeightBucketKey(bucketIndex)

		fieldStrs := make([]string, len(fields))
		for i, field := range fields {
			fieldStrs[i] = strconv.FormatUint(field, 10)
		}

		bitCmd := pipe.HMGet(ctx, bitBucketKey, fieldStrs...)
		weightCmd := pipe.HMGet(ctx, weightBucketKey, fieldStrs...)

		results = append(results, queryResult{
			bucketIndex: bucketIndex,
			fields:      fields,
			bitCmd:      bitCmd,
			weightCmd:   weightCmd,
		})
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		return false, 0, fmt.Errorf("failed to test weighted key %s: %w", key, err)
	}

	// 处理查询结果
	var totalWeight float64
	var validWeights int
	currentTime := getCurrentTimestamp()

	for _, result := range results {
		bitValues := result.bitCmd.Val()
		weightValues := result.weightCmd.Val()

		for i, bitVal := range bitValues {
			// 检查位是否存在
			if bitVal == nil {
				return false, 0, nil
			}

			if bitStr, ok := bitVal.(string); ok {
				count, err := strconv.ParseInt(bitStr, 10, 64)
				if err != nil || count <= 0 {
					return false, 0, nil
				}
			} else {
				return false, 0, nil
			}

			// 获取权重信息
			if i < len(weightValues) && weightValues[i] != nil {
				if weightStr, ok := weightValues[i].(string); ok {
					var weightInfo WeightInfo
					if err := json.Unmarshal([]byte(weightStr), &weightInfo); err == nil {
						// 应用时间衰减
						decayedWeight := wbf.applyTimeDecay(weightInfo.Weight, weightInfo.Timestamp, currentTime)
						totalWeight += decayedWeight
						validWeights++
					}
				}
			}
		}
	}

	if validWeights == 0 {
		return true, 0, nil
	}

	avgWeight := totalWeight / float64(validWeights)
	return true, avgWeight, nil
}

// GetWeight 获取元素权重
func (wbf *WeightedBloomFilterImpl) GetWeight(ctx context.Context, key string) (float64, error) {
	exists, weight, err := wbf.TestWithWeight(ctx, key)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, fmt.Errorf("key %s not found", key)
	}
	return weight, nil
}

// UpdateWeight 更新元素权重
func (wbf *WeightedBloomFilterImpl) UpdateWeight(ctx context.Context, key string, weight float64) error {
	if weight <= 0 {
		return fmt.Errorf("weight must be positive")
	}
	if weight > wbf.maxWeight {
		weight = wbf.maxWeight
	}

	// 首先检查元素是否存在
	exists, err := wbf.Test(ctx, key)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("key %s not found", key)
	}

	positions := wbf.getPositions(key)
	currentTime := getCurrentTimestamp()

	// 按桶分组操作
	bucketOps := make(map[uint64][]uint64)
	for _, pos := range positions {
		bucketIndex := pos / wbf.bucketSize
		field := pos % wbf.bucketSize
		bucketOps[bucketIndex] = append(bucketOps[bucketIndex], field)
	}

	// 更新权重信息
	pipe := wbf.client.Pipeline()
	for bucketIndex, fields := range bucketOps {
		weightBucketKey := wbf.getWeightBucketKey(bucketIndex)

		for _, field := range fields {
			fieldStr := strconv.FormatUint(field, 10)

			weightInfo := WeightInfo{
				Weight:    weight,
				Count:     1, // 这里可以考虑累积计数
				Timestamp: currentTime,
			}

			weightData, _ := json.Marshal(weightInfo)
			pipe.HSet(ctx, weightBucketKey, fieldStr, string(weightData))
		}
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to update weight for key %s: %w", key, err)
	}

	return nil
}

// AddMultiple 批量添加元素
func (wbf *WeightedBloomFilterImpl) AddMultiple(ctx context.Context, keys []string) error {
	for _, key := range keys {
		if err := wbf.Add(ctx, key); err != nil {
			return fmt.Errorf("failed to add key %s: %w", key, err)
		}
	}
	return nil
}

// TestMultiple 批量测试元素
func (wbf *WeightedBloomFilterImpl) TestMultiple(ctx context.Context, keys []string) ([]bool, error) {
	results := make([]bool, len(keys))
	for i, key := range keys {
		exists, err := wbf.Test(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("failed to test key %s: %w", key, err)
		}
		results[i] = exists
	}
	return results, nil
}

// Clear 清空加权布隆过滤器
func (wbf *WeightedBloomFilterImpl) Clear(ctx context.Context) error {
	// 删除所有相关的桶
	var keys []string

	// 扫描位桶
	iter := wbf.client.Scan(ctx, 0, wbf.keyPrefix+":bits:*", 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	// 扫描权重桶
	iter = wbf.client.Scan(ctx, 0, wbf.weightPrefix+":*", 0).Iterator()
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if len(keys) > 0 {
		return wbf.client.Del(ctx, keys...).Err()
	}

	return nil
}

// Info 获取加权布隆过滤器信息
func (wbf *WeightedBloomFilterImpl) Info(ctx context.Context) (*BloomInfo, error) {
	var totalCounters uint64
	var nonZeroCounters uint64
	var totalWeight float64
	var weightCount int64

	// 扫描所有位桶
	iter := wbf.client.Scan(ctx, 0, wbf.keyPrefix+":bits:*", 0).Iterator()
	for iter.Next(ctx) {
		bucketKey := iter.Val()

		// 获取桶中的所有字段
		fields, err := wbf.client.HGetAll(ctx, bucketKey).Result()
		if err != nil {
			continue
		}

		for field, countStr := range fields {
			totalCounters++

			count, err := strconv.ParseInt(countStr, 10, 64)
			if err != nil || count <= 0 {
				continue
			}

			nonZeroCounters++

			// 获取对应的权重信息
			weightBucketKey := strings.Replace(bucketKey, ":bits:", ":weights:", 1)
			weightStr, err := wbf.client.HGet(ctx, weightBucketKey, field).Result()
			if err != nil {
				continue
			}

			var weightInfo WeightInfo
			if err := json.Unmarshal([]byte(weightStr), &weightInfo); err == nil {
				currentTime := getCurrentTimestamp()
				decayedWeight := wbf.applyTimeDecay(weightInfo.Weight, weightInfo.Timestamp, currentTime)
				totalWeight += decayedWeight
				weightCount++
			}
		}
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan bit buckets: %w", err)
	}

	// 估算插入元素数量
	var insertedElements uint64
	if nonZeroCounters > 0 && len(wbf.hashFuncs) > 0 {
		insertedElements = nonZeroCounters / uint64(len(wbf.hashFuncs))
	}

	var averageWeight float64
	// 计算平均权重
	if weightCount > 0 {
		averageWeight = totalWeight / float64(weightCount)
	}

	info := &BloomInfo{
		BitSize:                 wbf.bitSize,
		HashFunctions:           uint32(len(wbf.hashFuncs)),
		ExpectedElements:        wbf.config.ExpectedElements,
		FalsePositiveRate:       wbf.config.FalsePositiveRate,
		InsertedElements:        insertedElements,
		ActualFalsePositiveRate: 0,                 // 难以精确计算
		MemoryUsage:             totalCounters * 8, // 估算
		AverageWeight:           averageWeight,
	}

	// 可以添加自定义字段存储权重信息
	// info.AverageWeight = avgWeight

	return info, nil
}

// Close 关闭加权布隆过滤器
func (wbf *WeightedBloomFilterImpl) Close() error {
	return nil
}

// 辅助类型和方法

type bucketOp struct {
	field  uint64
	weight float64
}

// getPositions 计算key在位数组中的位置
func (wbf *WeightedBloomFilterImpl) getPositions(key string) []uint64 {
	data := []byte(key)
	positions := make([]uint64, len(wbf.hashFuncs))

	for i, hashFunc := range wbf.hashFuncs {
		hash := hashFunc(data)
		positions[i] = hash % wbf.bitSize
	}

	return positions
}

// getBitBucketKey 获取位桶的Redis key
func (wbf *WeightedBloomFilterImpl) getBitBucketKey(bucketIndex uint64) string {
	return fmt.Sprintf("%s:bits:%d", wbf.keyPrefix, bucketIndex)
}

// getWeightBucketKey 获取权重桶的Redis key
func (wbf *WeightedBloomFilterImpl) getWeightBucketKey(bucketIndex uint64) string {
	return fmt.Sprintf("%s:weights:%d", wbf.weightPrefix, bucketIndex)
}

// applyTimeDecay 应用时间衰减
func (wbf *WeightedBloomFilterImpl) applyTimeDecay(originalWeight float64, timestamp, currentTime int64) float64 {
	if wbf.weightDecay >= 1.0 || wbf.timeWindowSecs <= 0 {
		return originalWeight
	}

	timeDiff := currentTime - timestamp
	if timeDiff <= 0 {
		return originalWeight
	}

	// 计算衰减倍数：每个时间窗口衰减一次
	decayPeriods := float64(timeDiff) / float64(wbf.timeWindowSecs)
	decayFactor := math.Pow(wbf.weightDecay, decayPeriods)

	return originalWeight * decayFactor
}

// getCurrentTimestamp 获取当前时间戳
func getCurrentTimestamp() int64 {
	return time.Now().Unix()
}

// WeightedBloomStats 加权布隆过滤器统计信息
type WeightedBloomStats struct {
	*BloomInfo
	AverageWeight  float64 `json:"average_weight"`
	TotalWeight    float64 `json:"total_weight"`
	WeightVariance float64 `json:"weight_variance"`
	MaxWeight      float64 `json:"max_weight"`
	MinWeight      float64 `json:"min_weight"`
}

// GetDetailedStats 获取详细统计信息
func (wbf *WeightedBloomFilterImpl) GetDetailedStats(ctx context.Context) (*WeightedBloomStats, error) {
	info, err := wbf.Info(ctx)
	if err != nil {
		return nil, err
	}

	var weights []float64
	var totalWeight float64
	currentTime := getCurrentTimestamp()

	// 收集所有权重信息
	iter := wbf.client.Scan(ctx, 0, wbf.weightPrefix+":*", 0).Iterator()
	for iter.Next(ctx) {
		weightBucketKey := iter.Val()

		fields, err := wbf.client.HGetAll(ctx, weightBucketKey).Result()
		if err != nil {
			continue
		}

		for _, weightStr := range fields {
			var weightInfo WeightInfo
			if err := json.Unmarshal([]byte(weightStr), &weightInfo); err == nil {
				decayedWeight := wbf.applyTimeDecay(weightInfo.Weight, weightInfo.Timestamp, currentTime)
				weights = append(weights, decayedWeight)
				totalWeight += decayedWeight
			}
		}
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to collect weight stats: %w", err)
	}

	var avgWeight, variance, minWeight, maxWeight float64
	if len(weights) > 0 {
		avgWeight = totalWeight / float64(len(weights))

		minWeight = weights[0]
		maxWeight = weights[0]

		var sumSquaredDiff float64
		for _, weight := range weights {
			diff := weight - avgWeight
			sumSquaredDiff += diff * diff

			if weight < minWeight {
				minWeight = weight
			}
			if weight > maxWeight {
				maxWeight = weight
			}
		}

		variance = sumSquaredDiff / float64(len(weights))
	}

	return &WeightedBloomStats{
		BloomInfo:      info,
		AverageWeight:  avgWeight,
		TotalWeight:    totalWeight,
		WeightVariance: variance,
		MaxWeight:      maxWeight,
		MinWeight:      minWeight,
	}, nil
}
