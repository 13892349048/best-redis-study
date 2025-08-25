package bloom

import (
	"context"
	"math"
)

// BloomFilter 布隆过滤器接口
type BloomFilter interface {
	// Add 添加元素到布隆过滤器
	Add(ctx context.Context, key string) error

	// Test 测试元素是否可能存在
	// 返回true：元素可能存在（假阳性可能）
	// 返回false：元素一定不存在
	Test(ctx context.Context, key string) (bool, error)

	// AddMultiple 批量添加元素
	AddMultiple(ctx context.Context, keys []string) error

	// TestMultiple 批量测试元素
	TestMultiple(ctx context.Context, keys []string) ([]bool, error)

	// Clear 清空布隆过滤器
	Clear(ctx context.Context) error

	// Info 获取布隆过滤器信息
	Info(ctx context.Context) (*BloomInfo, error)

	// Close 关闭布隆过滤器
	Close() error
}

// CountingBloomFilter 计数布隆过滤器接口（支持删除）
type CountingBloomFilter interface {
	BloomFilter

	// Remove 从布隆过滤器中删除元素
	// 注意：可能产生假阴性（误删其他元素）
	Remove(ctx context.Context, key string) error

	// RemoveMultiple 批量删除元素
	RemoveMultiple(ctx context.Context, keys []string) error

	// Count 获取元素的计数值
	Count(ctx context.Context, key string) (uint32, error)
}

// WeightedBloomFilter 加权布隆过滤器接口
type WeightedBloomFilter interface {
	BloomFilter

	// AddWithWeight 添加带权重的元素
	AddWithWeight(ctx context.Context, key string, weight float64) error

	// TestWithWeight 测试元素并返回权重
	TestWithWeight(ctx context.Context, key string) (bool, float64, error)

	// GetWeight 获取元素权重
	GetWeight(ctx context.Context, key string) (float64, error)

	// UpdateWeight 更新元素权重
	UpdateWeight(ctx context.Context, key string, weight float64) error
}

// BloomInfo 布隆过滤器信息
type BloomInfo struct {
	// BitSize 位数组大小
	BitSize uint64 `json:"bit_size"`

	// HashFunctions 哈希函数数量
	HashFunctions uint32 `json:"hash_functions"`

	// ExpectedElements 预期元素数量
	ExpectedElements uint64 `json:"expected_elements"`

	// FalsePositiveRate 预期假阳性率
	FalsePositiveRate float64 `json:"false_positive_rate"`

	// InsertedElements 已插入元素数量
	InsertedElements uint64 `json:"inserted_elements"`

	// ActualFalsePositiveRate 实际假阳性率（估算）
	ActualFalsePositiveRate float64 `json:"actual_false_positive_rate"`

	// MemoryUsage 内存使用量（字节）
	MemoryUsage uint64 `json:"memory_usage"`

	// AverageWeight 平均权重
	AverageWeight float64 `json:"average_weight"`
}

// BloomConfig 布隆过滤器配置
type BloomConfig struct {
	// Name 布隆过滤器名称
	Name string

	// ExpectedElements 预期元素数量
	ExpectedElements uint64

	// FalsePositiveRate 预期假阳性率 (0.0-1.0)
	FalsePositiveRate float64

	// BitSize 位数组大小（可选，会根据预期元素数和假阳性率计算）
	BitSize uint64

	// HashFunctions 哈希函数数量（可选，会根据位数组大小和预期元素数计算）
	HashFunctions uint32

	// Namespace Redis key命名空间
	Namespace string

	// TTL 布隆过滤器过期时间（0表示不过期）
	TTL int64
}

// DefaultBloomConfig 默认布隆过滤器配置
func DefaultBloomConfig(name string) *BloomConfig {
	return &BloomConfig{
		Name:              name,
		ExpectedElements:  1000000, // 100万元素
		FalsePositiveRate: 0.01,    // 1%假阳性率
		Namespace:         "bloom",
		TTL:               0, // 不过期
	}
}

// OptimalParameters 计算最优参数
func (c *BloomConfig) OptimalParameters() (bitSize uint64, hashFunctions uint32) {
	if c.BitSize > 0 && c.HashFunctions > 0 {
		return c.BitSize, c.HashFunctions
	}

	// 计算最优位数组大小
	// m = -n * ln(p) / (ln(2)^2)
	// 其中 n = 预期元素数，p = 假阳性率
	n := float64(c.ExpectedElements)
	p := c.FalsePositiveRate

	m := -n * math.Log(p) / (math.Log(2) * math.Log(2))
	bitSize = uint64(math.Ceil(m))

	// 计算最优哈希函数数量
	// k = (m/n) * ln(2)
	k := (float64(bitSize) / n) * math.Log(2)
	hashFunctions = uint32(math.Ceil(k))

	// 确保至少有1个哈希函数
	if hashFunctions == 0 {
		hashFunctions = 1
	}

	return bitSize, hashFunctions
}
