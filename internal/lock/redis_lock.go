package lock

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLock Redis分布式锁实现
type RedisLock struct {
	client  redis.Cmdable
	options *LockOptions

	// Lua脚本
	lockScript        *redis.Script
	unlockScript      *redis.Script
	renewScript       *redis.Script
	getLockInfoScript *redis.Script
	forceUnlockScript *redis.Script
}

// NewRedisLock 创建Redis分布式锁
func NewRedisLock(client redis.Cmdable, options *LockOptions) *RedisLock {
	if options == nil {
		options = DefaultLockOptions()
	}

	return &RedisLock{
		client:  client,
		options: options,

		lockScript:        redis.NewScript(LockScript),
		unlockScript:      redis.NewScript(UnlockScript),
		renewScript:       redis.NewScript(RenewScript),
		getLockInfoScript: redis.NewScript(GetLockInfoScript),
		forceUnlockScript: redis.NewScript(ForceUnlockScript),
	}
}

// TryLock 尝试获取锁
func (r *RedisLock) TryLock(ctx context.Context, lockKey, owner string, ttl time.Duration) (bool, int64, error) {
	tokenKey := r.getTokenKey(lockKey)
	keys := []string{lockKey, tokenKey}
	args := []interface{}{owner, ttl.Milliseconds()}

	result, err := r.lockScript.Run(ctx, r.client, keys, args...).Result()
	if err != nil {
		return false, 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to execute lock script: %v", err),
			Key:     lockKey,
			Owner:   owner,
		}
	}

	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 2 {
		return false, 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: "unexpected script result format",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	success, ok := resultSlice[0].(int64)
	if !ok {
		return false, 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid success flag in script result",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	fencingToken, ok := resultSlice[1].(int64)
	if !ok {
		return false, 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid fencing token in script result",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	if success == 0 {
		return false, 0, ErrLockNotAcquired
	}

	return true, fencingToken, nil
}

// Lock 阻塞式获取锁
func (r *RedisLock) Lock(ctx context.Context, lockKey, owner string, ttl, timeout time.Duration) (int64, error) {
	deadline := time.Now().Add(timeout)
	retryCount := 0

	for time.Now().Before(deadline) {
		success, fencingToken, err := r.TryLock(ctx, lockKey, owner, ttl)
		if err != nil {
			// 如果是Redis错误，直接返回
			if lockErr, ok := err.(*LockError); ok && lockErr.Code == ErrCodeRedisError {
				return 0, err
			}
		}

		if success {
			return fencingToken, nil
		}

		// 检查是否达到最大重试次数
		retryCount++
		if r.options.MaxRetries > 0 && retryCount >= r.options.MaxRetries {
			break
		}

		// 等待重试间隔
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(r.options.RetryInterval):
			continue
		}
	}

	return 0, &LockError{
		Code:    ErrCodeTimeout,
		Message: fmt.Sprintf("failed to acquire lock within timeout %v", timeout),
		Key:     lockKey,
		Owner:   owner,
	}
}

// Unlock 释放锁
func (r *RedisLock) Unlock(ctx context.Context, lockKey, owner string, fencingToken int64) (bool, error) {
	keys := []string{lockKey}
	args := []interface{}{owner, fencingToken}

	result, err := r.unlockScript.Run(ctx, r.client, keys, args...).Result()
	if err != nil {
		return false, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to execute unlock script: %v", err),
			Key:     lockKey,
			Owner:   owner,
		}
	}

	resultCode, ok := result.(int64)
	if !ok {
		return false, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid unlock script result",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	switch resultCode {
	case 1:
		return true, nil
	case 0:
		return false, &LockError{
			Code:    ErrCodeLockNotFound,
			Message: "lock not found or owner mismatch",
			Key:     lockKey,
			Owner:   owner,
		}
	case -1:
		return false, &LockError{
			Code:    ErrCodeInvalidToken,
			Message: "invalid fencing token",
			Key:     lockKey,
			Owner:   owner,
		}
	default:
		return false, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("unexpected unlock result: %d", resultCode),
			Key:     lockKey,
			Owner:   owner,
		}
	}
}

// Renew 续租锁
func (r *RedisLock) Renew(ctx context.Context, lockKey, owner string, ttl time.Duration, fencingToken int64) (bool, error) {
	keys := []string{lockKey}
	args := []interface{}{owner, ttl.Milliseconds(), fencingToken}

	result, err := r.renewScript.Run(ctx, r.client, keys, args...).Result()
	if err != nil {
		return false, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to execute renew script: %v", err),
			Key:     lockKey,
			Owner:   owner,
		}
	}

	resultCode, ok := result.(int64)
	if !ok {
		return false, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid renew script result",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	switch resultCode {
	case 1:
		return true, nil
	case 0:
		return false, &LockError{
			Code:    ErrCodeLockNotFound,
			Message: "lock not found or owner mismatch",
			Key:     lockKey,
			Owner:   owner,
		}
	case -1:
		return false, &LockError{
			Code:    ErrCodeInvalidToken,
			Message: "invalid fencing token",
			Key:     lockKey,
			Owner:   owner,
		}
	default:
		return false, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("unexpected renew result: %d", resultCode),
			Key:     lockKey,
			Owner:   owner,
		}
	}
}

// IsLocked 检查锁是否存在
func (r *RedisLock) IsLocked(ctx context.Context, lockKey string) (bool, string, int64, error) {
	keys := []string{lockKey}

	result, err := r.getLockInfoScript.Run(ctx, r.client, keys).Result()
	if err != nil {
		return false, "", 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to execute get lock info script: %v", err),
			Key:     lockKey,
		}
	}

	if result == nil {
		return false, "", 0, nil
	}

	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 4 {
		return false, "", 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid get lock info script result format",
			Key:     lockKey,
		}
	}

	owner, ok := resultSlice[0].(string)
	if !ok {
		return false, "", 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid owner in lock info",
			Key:     lockKey,
		}
	}

	tokenStr, ok := resultSlice[1].(string)
	if !ok {
		return false, "", 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid token in lock info",
			Key:     lockKey,
		}
	}

	fencingToken, err := strconv.ParseInt(tokenStr, 10, 64)
	if err != nil {
		return false, "", 0, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to parse fencing token: %v", err),
			Key:     lockKey,
		}
	}

	return true, owner, fencingToken, nil
}

// GetFencingToken 获取当前锁的fencing token
func (r *RedisLock) GetFencingToken(ctx context.Context, lockKey, owner string) (int64, error) {
	locked, lockOwner, fencingToken, err := r.IsLocked(ctx, lockKey)
	if err != nil {
		return 0, err
	}

	if !locked {
		return 0, &LockError{
			Code:    ErrCodeLockNotFound,
			Message: "lock not found",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	if lockOwner != owner {
		return 0, &LockError{
			Code:    ErrCodeInvalidOwner,
			Message: "lock owner mismatch",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	return fencingToken, nil
}

// getTokenKey 获取fencing token计数器的key
func (r *RedisLock) getTokenKey(lockKey string) string {
	return lockKey + ":token_counter"
}
