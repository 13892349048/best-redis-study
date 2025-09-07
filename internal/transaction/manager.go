package transaction

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrTransactionFailed  = errors.New("transaction execution failed")
	ErrScriptNotFound     = errors.New("lua script not found")
	ErrMaxRetriesExceeded = errors.New("maximum retries exceeded")
)

// redisTransactionManager Redis事务管理器实现
type redisTransactionManager struct {
	client      *redis.Client
	config      Config
	stats       *TransactionStats
	statsMutex  sync.RWMutex
	scriptCache sync.Map // map[string]*LuaScript
}

// NewTransactionManager 创建事务管理器
func NewTransactionManager(client *redis.Client, opts ...Option) TransactionManager {
	config := Config{
		Timeout:       time.Second * 5,
		RetryPolicy:   DefaultRetryPolicy(),
		EnableMetrics: false,
		MetricsPrefix: "redis_transaction",
	}

	for _, opt := range opts {
		opt(&config)
	}

	return &redisTransactionManager{
		client: client,
		config: config,
		stats: &TransactionStats{
			MinLatency: time.Hour, // 初始化为一个大值
		},
	}
}

// ExecuteTransaction 执行事务
func (m *redisTransactionManager) ExecuteTransaction(ctx context.Context, operation TransactionOperation, opts ...Option) (*TransactionResult, error) {
	config := m.config
	for _, opt := range opts {
		opt(&config)
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	if config.OnStart != nil {
		config.OnStart(ctx)
	}

	startTime := time.Now()
	atomic.AddInt64(&m.stats.TotalTransactions, 1)

	// 执行事务
	result, err := m.executeTransaction(ctx, operation)

	duration := time.Since(startTime)
	result.Duration = duration

	// 更新统计信息
	m.updateStats(duration, err == nil)

	if err != nil {
		result.Success = false
		result.Error = err
		if config.OnFailure != nil {
			config.OnFailure(ctx, err)
		}
		return result, err
	}

	result.Success = true
	if config.OnSuccess != nil {
		config.OnSuccess(ctx, result)
	}

	return result, nil
}

// ExecuteOptimisticTransaction 执行乐观事务（带WATCH）
func (m *redisTransactionManager) ExecuteOptimisticTransaction(ctx context.Context, watchKeys []string, operation TransactionOperation, opts ...Option) (*TransactionResult, error) {
	config := m.config
	for _, opt := range opts {
		opt(&config)
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	if config.OnStart != nil {
		config.OnStart(ctx)
	}

	startTime := time.Now()
	atomic.AddInt64(&m.stats.TotalTransactions, 1)

	var lastErr error
	var result *TransactionResult

	// 重试逻辑
	for attempt := 0; attempt <= config.RetryPolicy.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := m.calculateBackoffDelay(attempt, config.RetryPolicy)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			if config.OnRetry != nil {
				config.OnRetry(ctx, attempt, lastErr)
			}
		}

		result, lastErr = m.executeOptimisticTransaction(ctx, watchKeys, operation)
		if lastErr == nil {
			break
		}

		// 检查是否是可重试的错误
		if !m.isRetriableError(lastErr) {
			break
		}
	}

	duration := time.Since(startTime)
	if result == nil {
		result = &TransactionResult{}
	}
	result.Duration = duration

	// 更新统计信息
	m.updateStats(duration, lastErr == nil)

	if lastErr != nil {
		result.Success = false
		result.Error = lastErr
		if config.OnFailure != nil {
			config.OnFailure(ctx, lastErr)
		}
		return result, lastErr
	}

	result.Success = true
	if config.OnSuccess != nil {
		config.OnSuccess(ctx, result)
	}

	return result, nil
}

// ExecuteLuaScript 执行Lua脚本
func (m *redisTransactionManager) ExecuteLuaScript(ctx context.Context, script *LuaScript, keys []string, args []interface{}, opts ...Option) (interface{}, error) {
	config := m.config
	for _, opt := range opts {
		opt(&config)
	}

	if config.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, config.Timeout)
		defer cancel()
	}

	// 确保脚本已加载
	if script.SHA == "" {
		script.SHA = m.calculateScriptSHA(script.Script)
	}

	// 先尝试使用EVALSHA
	result, err := m.client.EvalSha(ctx, script.SHA, keys, args...).Result()
	if err != nil {
		// 如果脚本不存在，使用EVAL
		if isScriptNotFoundError(err) {
			result, err = m.client.Eval(ctx, script.Script, keys, args...).Result()
		}
	}

	return result, err
}

// BatchExecute 批量执行操作
func (m *redisTransactionManager) BatchExecute(ctx context.Context, operations []BatchOperation, opts ...Option) (*BatchResult, error) {
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
	result := &BatchResult{
		Total:   len(operations),
		Results: make([]BatchOperationResult, len(operations)),
	}

	pipe := m.client.TxPipeline()

	// 添加所有操作到pipeline
	for i, op := range operations {
		err := m.addOperationToPipeline(pipe, op)
		if err != nil {
			result.Results[i] = BatchOperationResult{
				Operation: op,
				Success:   false,
				Error:     err,
			}
			result.Failed++
		}
	}

	// 执行pipeline
	cmds, err := pipe.Exec(ctx)
	if err != nil {
		result.Duration = time.Since(startTime)
		return result, err
	}

	// 处理结果
	for i, cmd := range cmds {
		if i >= len(result.Results) {
			break
		}

		if cmd.Err() != nil {
			result.Results[i].Success = false
			result.Results[i].Error = cmd.Err()
			result.Failed++
		} else {
			result.Results[i].Success = true
			result.Results[i].Result = cmd
			result.Successful++
		}
		result.Results[i].Operation = operations[i]
	}

	result.Success = result.Failed == 0
	result.Duration = time.Since(startTime)

	return result, nil
}

// executeTransaction 执行基础事务
func (m *redisTransactionManager) executeTransaction(ctx context.Context, operation TransactionOperation) (*TransactionResult, error) {
	pipe := m.client.TxPipeline()
	txCtx := &redisTransactionContext{pipe: pipe, ctx: ctx}

	err := operation(ctx, txCtx)
	if err != nil {
		return &TransactionResult{Commands: 0}, err
	}

	cmds, err := pipe.Exec(ctx)

	result := &TransactionResult{
		Commands: len(cmds),
		Results:  make([]interface{}, len(cmds)),
	}

	for i, cmd := range cmds {
		result.Results[i] = cmd
	}

	return result, err
}

// executeOptimisticTransaction 执行乐观事务
func (m *redisTransactionManager) executeOptimisticTransaction(ctx context.Context, watchKeys []string, operation TransactionOperation) (*TransactionResult, error) {
	txf := func(tx *redis.Tx) error {
		txCtx := &redisTransactionContext{tx: tx, ctx: ctx}
		return operation(ctx, txCtx)
	}

	err := m.client.Watch(ctx, txf, watchKeys...)
	if err == redis.TxFailedErr {
		return &TransactionResult{}, errors.New("optimistic lock conflict")
	}

	return &TransactionResult{}, err
}

// addOperationToPipeline 将操作添加到pipeline
func (m *redisTransactionManager) addOperationToPipeline(pipe redis.Pipeliner, op BatchOperation) error {
	switch op.Operation {
	case "SET":
		if len(op.Args) >= 2 {
			pipe.Set(context.Background(), op.Key, op.Args[0], time.Duration(0))
		}
	case "GET":
		pipe.Get(context.Background(), op.Key)
	case "DEL":
		pipe.Del(context.Background(), op.Key)
	case "INCR":
		pipe.Incr(context.Background(), op.Key)
	case "DECR":
		pipe.Decr(context.Background(), op.Key)
	case "HSET":
		if len(op.Args) >= 2 {
			pipe.HSet(context.Background(), op.Key, op.Args[0], op.Args[1])
		}
	case "HGET":
		if len(op.Args) >= 1 {
			pipe.HGet(context.Background(), op.Key, op.Args[0].(string))
		}
	default:
		return fmt.Errorf("unsupported operation: %s", op.Operation)
	}
	return nil
}

// calculateBackoffDelay 计算退避延迟
func (m *redisTransactionManager) calculateBackoffDelay(attempt int, policy RetryPolicy) time.Duration {
	delay := float64(policy.InitialDelay) * math.Pow(policy.BackoffFactor, float64(attempt-1))
	if delay > float64(policy.MaxDelay) {
		delay = float64(policy.MaxDelay)
	}
	return time.Duration(delay)
}

// isRetriableError 判断错误是否可重试
func (m *redisTransactionManager) isRetriableError(err error) bool {
	if err == redis.TxFailedErr {
		return true
	}

	// 网络相关错误通常可以重试
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return false
	}

	return true
}

// calculateScriptSHA 计算脚本SHA
func (m *redisTransactionManager) calculateScriptSHA(script string) string {
	hash := sha1.Sum([]byte(script))
	return hex.EncodeToString(hash[:])
}

// updateStats 更新统计信息
func (m *redisTransactionManager) updateStats(duration time.Duration, success bool) {
	m.statsMutex.Lock()
	defer m.statsMutex.Unlock()

	if success {
		atomic.AddInt64(&m.stats.SuccessfulTransactions, 1)
	} else {
		atomic.AddInt64(&m.stats.FailedTransactions, 1)
	}

	// 更新延迟统计
	if duration > m.stats.MaxLatency {
		m.stats.MaxLatency = duration
	}
	if duration < m.stats.MinLatency {
		m.stats.MinLatency = duration
	}

	// 简单的平均值计算（可以改进为滑动窗口）
	total := atomic.LoadInt64(&m.stats.TotalTransactions)
	if total > 0 {
		avgNanos := (int64(m.stats.AverageLatency)*(total-1) + int64(duration)) / total
		m.stats.AverageLatency = time.Duration(avgNanos)
	}
}

// isScriptNotFoundError 判断是否是脚本未找到错误
func isScriptNotFoundError(err error) bool {
	return err != nil && (err.Error() == "NOSCRIPT No matching script. Please use EVAL." ||
		err.Error() == "NOSCRIPT No matching script")
}
