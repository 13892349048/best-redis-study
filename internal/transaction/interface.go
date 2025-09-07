package transaction

import (
	"context"
	"time"
)

// TransactionManager 事务管理器接口
type TransactionManager interface {
	// ExecuteTransaction 执行事务
	ExecuteTransaction(ctx context.Context, operation TransactionOperation, opts ...Option) (*TransactionResult, error)

	// ExecuteOptimisticTransaction 执行乐观事务（带WATCH）
	ExecuteOptimisticTransaction(ctx context.Context, watchKeys []string, operation TransactionOperation, opts ...Option) (*TransactionResult, error)

	// ExecuteLuaScript 执行Lua脚本
	ExecuteLuaScript(ctx context.Context, script *LuaScript, keys []string, args []interface{}, opts ...Option) (interface{}, error)

	// BatchExecute 批量执行操作
	BatchExecute(ctx context.Context, operations []BatchOperation, opts ...Option) (*BatchResult, error)
}

// TransactionOperation 事务操作函数
type TransactionOperation func(ctx context.Context, tx TransactionContext) error

// BatchOperation 批量操作
type BatchOperation struct {
	Key       string
	Operation string
	Args      []interface{}
}

// TransactionContext 事务上下文接口
type TransactionContext interface {
	// 基础操作
	Set(key, value string, expiration time.Duration) error
	Get(key string) (string, error)
	Del(keys ...string) error
	Exists(keys ...string) (int64, error)

	// 数值操作
	Incr(key string) (int64, error)
	Decr(key string) (int64, error)
	IncrBy(key string, value int64) (int64, error)
	DecrBy(key string, value int64) (int64, error)

	// Hash操作
	HSet(key, field, value string) error
	HGet(key, field string) (string, error)
	HDel(key string, fields ...string) error
	HExists(key, field string) (bool, error)
	HIncrBy(key, field string, incr int64) (int64, error)

	// List操作
	LPush(key string, values ...interface{}) error
	RPush(key string, values ...interface{}) error
	LPop(key string) (string, error)
	RPop(key string) (string, error)
	LLen(key string) (int64, error)

	// Set操作
	SAdd(key string, members ...interface{}) error
	SRem(key string, members ...interface{}) error
	SIsMember(key string, member interface{}) (bool, error)
	SCard(key string) (int64, error)

	// ZSet操作
	ZAdd(key string, members ...interface{}) error
	ZRem(key string, members ...interface{}) error
	ZScore(key string, member string) (float64, error)
	ZCard(key string) (int64, error)

	// 过期时间
	Expire(key string, expiration time.Duration) error
	TTL(key string) (time.Duration, error)

	// 条件操作
	SetNX(key, value string, expiration time.Duration) (bool, error)
}

// TransactionResult 事务执行结果
type TransactionResult struct {
	Success  bool
	Commands int
	Duration time.Duration
	Error    error
	Results  []interface{}
}

// BatchResult 批量操作结果
type BatchResult struct {
	Success    bool
	Total      int
	Successful int
	Failed     int
	Duration   time.Duration
	Results    []BatchOperationResult
}

// BatchOperationResult 批量操作单个结果
type BatchOperationResult struct {
	Operation BatchOperation
	Success   bool
	Result    interface{}
	Error     error
}

// LuaScript Lua脚本定义
type LuaScript struct {
	Script string
	SHA    string // 脚本的SHA1哈希，用于EVALSHA
}

// TransactionStats 事务统计信息
type TransactionStats struct {
	TotalTransactions      int64
	SuccessfulTransactions int64
	FailedTransactions     int64
	AverageLatency         time.Duration
	MaxLatency             time.Duration
	MinLatency             time.Duration
}

// Option 配置选项
type Option func(*Config)

// Config 事务配置
type Config struct {
	Timeout       time.Duration
	RetryPolicy   RetryPolicy
	EnableMetrics bool
	MetricsPrefix string
	OnStart       func(ctx context.Context)
	OnSuccess     func(ctx context.Context, result *TransactionResult)
	OnFailure     func(ctx context.Context, err error)
	OnRetry       func(ctx context.Context, attempt int, err error)
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries    int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
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

// 配置选项函数

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) {
		c.Timeout = timeout
	}
}

// WithRetryPolicy 设置重试策略
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(c *Config) {
		c.RetryPolicy = policy
	}
}

// WithMetrics 启用指标收集
func WithMetrics(prefix string) Option {
	return func(c *Config) {
		c.EnableMetrics = true
		c.MetricsPrefix = prefix
	}
}

// WithStartCallback 设置开始回调
func WithStartCallback(callback func(ctx context.Context)) Option {
	return func(c *Config) {
		c.OnStart = callback
	}
}

// WithSuccessCallback 设置成功回调
func WithSuccessCallback(callback func(ctx context.Context, result *TransactionResult)) Option {
	return func(c *Config) {
		c.OnSuccess = callback
	}
}

// WithFailureCallback 设置失败回调
func WithFailureCallback(callback func(ctx context.Context, err error)) Option {
	return func(c *Config) {
		c.OnFailure = callback
	}
}

// WithRetryCallback 设置重试回调
func WithRetryCallback(callback func(ctx context.Context, attempt int, err error)) Option {
	return func(c *Config) {
		c.OnRetry = callback
	}
}
