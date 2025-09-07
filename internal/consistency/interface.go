package consistency

import (
	"context"
	"time"
)

// ConsistencyManager 一致性管理器接口
type ConsistencyManager interface {
	// ExecuteWithWatch 使用WATCH机制执行一致性操作
	ExecuteWithWatch(ctx context.Context, keys []string, operation WatchOperation, opts ...Option) error

	// ExecuteWithOptimisticLock 使用乐观锁执行操作
	ExecuteWithOptimisticLock(ctx context.Context, lockKey string, operation OptimisticOperation, opts ...Option) error

	// ExecuteAtomicUpdate 执行原子更新操作
	ExecuteAtomicUpdate(ctx context.Context, operation AtomicOperation, opts ...Option) error
}

// WatchOperation WATCH操作函数
type WatchOperation func(ctx context.Context, tx TxContext) error

// OptimisticOperation 乐观锁操作函数
type OptimisticOperation func(ctx context.Context, tx TxContext, version string) error

// AtomicOperation 原子操作函数
type AtomicOperation func(ctx context.Context, tx TxContext) error

// TxContext 事务上下文接口
type TxContext interface {
	// Get 获取值
	Get(key string) (string, error)

	// Set 设置值
	Set(key, value string, expiration time.Duration) error

	// Del 删除键
	Del(keys ...string) error

	// Incr 递增
	Incr(key string) (int64, error)

	// Decr 递减
	Decr(key string) (int64, error)

	// HGet 获取Hash字段
	HGet(key, field string) (string, error)

	// HSet 设置Hash字段
	HSet(key, field, value string) error

	// HDel 删除Hash字段
	HDel(key string, fields ...string) error

	// Exists 检查键是否存在
	Exists(keys ...string) (int64, error)

	// TTL 获取过期时间
	TTL(key string) (time.Duration, error)

	// Expire 设置过期时间
	Expire(key string, expiration time.Duration) error
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries    int           // 最大重试次数
	InitialDelay  time.Duration // 初始延迟
	MaxDelay      time.Duration // 最大延迟
	BackoffFactor float64       // 退避因子
}

// Option 配置选项
type Option func(*Config)

// Config 配置
type Config struct {
	RetryPolicy   RetryPolicy
	Timeout       time.Duration
	EnableMetrics bool
	MetricsPrefix string
	OnRetry       func(attempt int, err error)
	OnSuccess     func(attempts int, duration time.Duration)
	OnFailure     func(attempts int, finalErr error)
}

// DefaultRetryPolicy 默认重试策略
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:    3,
		InitialDelay:  time.Millisecond * 10,
		MaxDelay:      time.Millisecond * 100,
		BackoffFactor: 2.0,
	}
}

// WithRetryPolicy 设置重试策略
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(c *Config) {
		c.RetryPolicy = policy
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Timeout = timeout
	}
}

// WithMetrics 启用指标收集
func WithMetrics(prefix string) Option {
	return func(c *Config) {
		c.EnableMetrics = true
		c.MetricsPrefix = prefix
	}
}

// WithRetryCallback 设置重试回调
func WithRetryCallback(callback func(attempt int, err error)) Option {
	return func(c *Config) {
		c.OnRetry = callback
	}
}

// WithSuccessCallback 设置成功回调
func WithSuccessCallback(callback func(attempts int, duration time.Duration)) Option {
	return func(c *Config) {
		c.OnSuccess = callback
	}
}

// WithFailureCallback 设置失败回调
func WithFailureCallback(callback func(attempts int, finalErr error)) Option {
	return func(c *Config) {
		c.OnFailure = callback
	}
}
