package consistency

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrWatchKeyChanged        = errors.New("watched key changed during transaction")
	ErrOptimisticLockConflict = errors.New("optimistic lock conflict")
	ErrMaxRetriesExceeded     = errors.New("maximum retries exceeded")
	ErrTransactionFailed      = errors.New("transaction execution failed")
)

// redisConsistencyManager Redis一致性管理器实现
type redisConsistencyManager struct {
	client *redis.Client
	config Config
}

// NewConsistencyManager 创建一致性管理器
func NewConsistencyManager(client *redis.Client, opts ...Option) ConsistencyManager {
	config := Config{
		RetryPolicy:   DefaultRetryPolicy(),
		Timeout:       time.Second * 5,
		EnableMetrics: false,
		MetricsPrefix: "redis_consistency",
	}

	for _, opt := range opts {
		opt(&config)
	}

	return &redisConsistencyManager{
		client: client,
		config: config,
	}
}

// ExecuteWithWatch 使用WATCH机制执行一致性操作
func (m *redisConsistencyManager) ExecuteWithWatch(ctx context.Context, keys []string, operation WatchOperation, opts ...Option) error {
	config := m.config
	for _, opt := range opts {
		opt(&config)
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	startTime := time.Now()
	var lastErr error

	for attempt := 0; attempt <= config.RetryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			// 计算退避延迟
			delay := m.calculateBackoffDelay(attempt, config.RetryPolicy)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}

			if config.OnRetry != nil {
				config.OnRetry(attempt, lastErr)
			}
		}

		// 执行带WATCH的事务
		err := m.executeWatchTransaction(ctx, keys, operation)
		if err == nil {
			// 成功
			if config.OnSuccess != nil {
				config.OnSuccess(attempt+1, time.Since(startTime))
			}
			return nil
		}

		lastErr = err

		// 检查是否是可重试的错误
		if !m.isRetriableError(err) {
			break
		}
	}

	if config.OnFailure != nil {
		config.OnFailure(config.RetryPolicy.MaxRetries+1, lastErr)
	}

	return fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, lastErr)
}

// ExecuteWithOptimisticLock 使用乐观锁执行操作
func (m *redisConsistencyManager) ExecuteWithOptimisticLock(ctx context.Context, lockKey string, operation OptimisticOperation, opts ...Option) error {
	config := m.config
	for _, opt := range opts {
		opt(&config)
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	startTime := time.Now()
	var lastErr error

	for attempt := 0; attempt <= config.RetryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := m.calculateBackoffDelay(attempt, config.RetryPolicy)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}

			if config.OnRetry != nil {
				config.OnRetry(attempt, lastErr)
			}
		}

		// 获取当前版本
		version, err := m.client.Get(ctx, lockKey).Result()
		if err != nil && err != redis.Nil {
			lastErr = err
			continue
		}
		if err == redis.Nil {
			version = ""
		}

		// 执行乐观锁事务
		err = m.executeOptimisticTransaction(ctx, lockKey, version, operation)
		if err == nil {
			if config.OnSuccess != nil {
				config.OnSuccess(attempt+1, time.Since(startTime))
			}
			return nil
		}

		lastErr = err
		if !m.isRetriableError(err) {
			break
		}
	}

	if config.OnFailure != nil {
		config.OnFailure(config.RetryPolicy.MaxRetries+1, lastErr)
	}

	return fmt.Errorf("%w: %v", ErrMaxRetriesExceeded, lastErr)
}

// ExecuteAtomicUpdate 执行原子更新操作
func (m *redisConsistencyManager) ExecuteAtomicUpdate(ctx context.Context, operation AtomicOperation, opts ...Option) error {
	config := m.config
	for _, opt := range opts {
		opt(&config)
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	startTime := time.Now()

	// 原子操作通常不需要重试，因为它们要么成功要么失败
	err := m.executeAtomicTransaction(ctx, operation)

	if err == nil && config.OnSuccess != nil {
		config.OnSuccess(1, time.Since(startTime))
	} else if err != nil && config.OnFailure != nil {
		config.OnFailure(1, err)
	}

	return err
}

// executeWatchTransaction 执行带WATCH的事务
func (m *redisConsistencyManager) executeWatchTransaction(ctx context.Context, keys []string, operation WatchOperation) error {
	txf := func(tx *redis.Tx) error {
		// 在事务函数内部执行业务逻辑
		txCtx := &redisTxContext{tx: tx, ctx: ctx}
		return operation(ctx, txCtx)
	}

	err := m.client.Watch(ctx, txf, keys...)
	if err == redis.TxFailedErr {
		return ErrWatchKeyChanged
	}
	return err
}

// executeOptimisticTransaction 执行乐观锁事务
func (m *redisConsistencyManager) executeOptimisticTransaction(ctx context.Context, lockKey, expectedVersion string, operation OptimisticOperation) error {
	txf := func(tx *redis.Tx) error {
		// 检查版本是否仍然匹配
		currentVersion, err := tx.Get(ctx, lockKey).Result()
		if err != nil && err != redis.Nil {
			return err
		}
		if err == redis.Nil {
			currentVersion = ""
		}

		if currentVersion != expectedVersion {
			return ErrOptimisticLockConflict
		}

		txCtx := &redisTxContext{tx: tx, ctx: ctx}
		return operation(ctx, txCtx, expectedVersion)
	}

	err := m.client.Watch(ctx, txf, lockKey)
	if err == redis.TxFailedErr {
		return ErrOptimisticLockConflict
	}
	return err
}

// executeAtomicTransaction 执行原子事务
func (m *redisConsistencyManager) executeAtomicTransaction(ctx context.Context, operation AtomicOperation) error {
	pipe := m.client.TxPipeline()
	txCtx := &redisTxContext{pipe: pipe, ctx: ctx}

	err := operation(ctx, txCtx)
	if err != nil {
		return err
	}

	_, err = pipe.Exec(ctx)
	return err
}

// calculateBackoffDelay 计算退避延迟
func (m *redisConsistencyManager) calculateBackoffDelay(attempt int, policy RetryPolicy) time.Duration {
	delay := float64(policy.InitialDelay) * math.Pow(policy.BackoffFactor, float64(attempt-1))
	if delay > float64(policy.MaxDelay) {
		delay = float64(policy.MaxDelay)
	}
	return time.Duration(delay)
}

// isRetriableError 判断错误是否可重试
func (m *redisConsistencyManager) isRetriableError(err error) bool {
	if errors.Is(err, ErrWatchKeyChanged) ||
		errors.Is(err, ErrOptimisticLockConflict) ||
		errors.Is(err, redis.TxFailedErr) {
		return true
	}

	// 网络相关错误通常可以重试
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return false
	}

	return true
}

// redisTxContext Redis事务上下文实现
type redisTxContext struct {
	tx   *redis.Tx
	pipe redis.Pipeliner
	ctx  context.Context
}

func (t *redisTxContext) Get(key string) (string, error) {
	if t.tx != nil {
		return t.tx.Get(t.ctx, key).Result()
	}
	// 在Pipeline中不能执行读操作
	return "", errors.New("cannot read in pipeline context")
}

func (t *redisTxContext) Set(key, value string, expiration time.Duration) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(t.ctx, key, value, expiration)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.Set(t.ctx, key, value, expiration)
		return nil
	}
	return errors.New("no transaction context available")
}

func (t *redisTxContext) Del(keys ...string) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.Del(t.ctx, keys...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.Del(t.ctx, keys...)
		return nil
	}
	return errors.New("no transaction context available")
}

func (t *redisTxContext) Incr(key string) (int64, error) {
	if t.tx != nil {
		var result int64
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			cmd := pipe.Incr(t.ctx, key)
			result = cmd.Val()
			return nil
		})
		return result, err
	}
	if t.pipe != nil {
		cmd := t.pipe.Incr(t.ctx, key)
		return cmd.Val(), nil
	}
	return 0, errors.New("no transaction context available")
}

func (t *redisTxContext) Decr(key string) (int64, error) {
	if t.tx != nil {
		var result int64
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			cmd := pipe.Decr(t.ctx, key)
			result = cmd.Val()
			return nil
		})
		return result, err
	}
	if t.pipe != nil {
		cmd := t.pipe.Decr(t.ctx, key)
		return cmd.Val(), nil
	}
	return 0, errors.New("no transaction context available")
}

func (t *redisTxContext) HGet(key, field string) (string, error) {
	if t.tx != nil {
		return t.tx.HGet(t.ctx, key, field).Result()
	}
	return "", errors.New("cannot read in pipeline context")
}

func (t *redisTxContext) HSet(key, field, value string) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.HSet(t.ctx, key, field, value)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.HSet(t.ctx, key, field, value)
		return nil
	}
	return errors.New("no transaction context available")
}

func (t *redisTxContext) HDel(key string, fields ...string) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.HDel(t.ctx, key, fields...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.HDel(t.ctx, key, fields...)
		return nil
	}
	return errors.New("no transaction context available")
}

func (t *redisTxContext) Exists(keys ...string) (int64, error) {
	if t.tx != nil {
		return t.tx.Exists(t.ctx, keys...).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

func (t *redisTxContext) TTL(key string) (time.Duration, error) {
	if t.tx != nil {
		return t.tx.TTL(t.ctx, key).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

func (t *redisTxContext) Expire(key string, expiration time.Duration) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.Expire(t.ctx, key, expiration)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.Expire(t.ctx, key, expiration)
		return nil
	}
	return errors.New("no transaction context available")
}
