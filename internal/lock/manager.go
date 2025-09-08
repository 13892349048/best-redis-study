package lock

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisLockManager Redis锁管理器
type RedisLockManager struct {
	client  redis.Cmdable
	options *LockOptions

	// 脚本
	forceUnlockScript *redis.Script
	getLockInfoScript *redis.Script
}

// NewRedisLockManager 创建Redis锁管理器
func NewRedisLockManager(client redis.Cmdable, options *LockOptions) *RedisLockManager {
	if options == nil {
		options = DefaultLockOptions()
	}

	return &RedisLockManager{
		client:            client,
		options:           options,
		forceUnlockScript: redis.NewScript(ForceUnlockScript),
		getLockInfoScript: redis.NewScript(GetLockInfoScript),
	}
}

type RedisLockService struct {
	Manager   *RedisLockManager
	Lock      Lock
	RenewLock AutoRenewLock
}

func NewRedisLockService(client redis.Cmdable, options *LockOptions) *RedisLockService {
	return &RedisLockService{
		Manager:   NewRedisLockManager(client, options),
		Lock:      NewRedisLock(client, options),
		RenewLock: NewAutoRenewRedisLock(client, options),
	}
}

// CreateLock 创建锁实例
func (m *RedisLockManager) CreateLock() Lock {
	return NewRedisLock(m.client, m.options)
}

// CreateAutoRenewLock 创建自动续租锁实例  返回结构体
func (m *RedisLockManager) CreateAutoRenewLock() AutoRenewLock {
	return NewAutoRenewRedisLock(m.client, m.options)
}

// GetLockInfo 获取锁信息
func (m *RedisLockManager) GetLockInfo(ctx context.Context, lockKey string) (*LockInfo, error) {
	keys := []string{lockKey}

	result, err := m.getLockInfoScript.Run(ctx, m.client, keys).Result()
	if err != nil {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to get lock info: %v", err),
			Key:     lockKey,
		}
	}

	if result == nil {
		return nil, &LockError{
			Code:    ErrCodeLockNotFound,
			Message: "lock not found",
			Key:     lockKey,
		}
	}

	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 4 {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid lock info format",
			Key:     lockKey,
		}
	}

	owner, ok := resultSlice[0].(string)
	if !ok {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid owner in lock info",
			Key:     lockKey,
		}
	}

	tokenStr, ok := resultSlice[1].(string)
	if !ok {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid token in lock info",
			Key:     lockKey,
		}
	}

	fencingToken, err := strconv.ParseInt(tokenStr, 10, 64)
	if err != nil {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to parse fencing token: %v", err),
			Key:     lockKey,
		}
	}

	createdAtStr, ok := resultSlice[2].(string)
	if !ok {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid created_at in lock info",
			Key:     lockKey,
		}
	}

	createdAtUnix, err := strconv.ParseInt(createdAtStr, 10, 64)
	if err != nil {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to parse created_at: %v", err),
			Key:     lockKey,
		}
	}

	ttlMs, ok := resultSlice[3].(int64)
	if !ok {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid ttl in lock info",
			Key:     lockKey,
		}
	}

	createdAt := time.Unix(createdAtUnix, 0)
	expiresAt := time.Now().Add(time.Duration(ttlMs) * time.Millisecond)

	return &LockInfo{
		Key:          lockKey,
		Owner:        owner,
		FencingToken: fencingToken,
		CreatedAt:    createdAt,
		ExpiresAt:    expiresAt,
		TTL:          ttlMs / 1000, // 转换为秒
	}, nil
}

// ListLocks 列出所有锁（用于调试）
func (m *RedisLockManager) ListLocks(ctx context.Context, pattern string) ([]*LockInfo, error) {
	if pattern == "" {
		pattern = "*"
	}

	// 使用SCAN命令获取匹配的keys
	var allKeys []string
	iter := m.client.Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		// 过滤掉token counter keys
		if !strings.HasSuffix(key, ":token_counter") {
			allKeys = append(allKeys, key)
		}
	}
	if err := iter.Err(); err != nil {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to scan keys: %v", err),
		}
	}

	var locks []*LockInfo
	for _, key := range allKeys {
		lockInfo, err := m.GetLockInfo(ctx, key)
		if err != nil {
			// 如果锁不存在，跳过（可能在扫描过程中过期了）
			if lockErr, ok := err.(*LockError); ok && lockErr.Code == ErrCodeLockNotFound {
				continue
			}
			return nil, err
		}
		locks = append(locks, lockInfo)
	}

	return locks, nil
}

// ForceUnlock 强制释放锁（谨慎使用）
func (m *RedisLockManager) ForceUnlock(ctx context.Context, lockKey string) error {
	keys := []string{lockKey}

	result, err := m.forceUnlockScript.Run(ctx, m.client, keys).Result()
	if err != nil {
		return &LockError{
			Code:    ErrCodeRedisError,
			Message: fmt.Sprintf("failed to force unlock: %v", err),
			Key:     lockKey,
		}
	}

	resultCode, ok := result.(int64)
	if !ok {
		return &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid force unlock result",
			Key:     lockKey,
		}
	}

	if resultCode == 0 {
		return &LockError{
			Code:    ErrCodeLockNotFound,
			Message: "lock not found",
			Key:     lockKey,
		}
	}

	return nil
}

// CleanExpiredLocks 清理过期锁（定期维护任务）
func (m *RedisLockManager) CleanExpiredLocks(ctx context.Context, pattern string) (int, error) {
	locks, err := m.ListLocks(ctx, pattern)
	if err != nil {
		return 0, err
	}

	cleaned := 0
	now := time.Now()

	for _, lock := range locks {
		// 检查锁是否过期
		if lock.ExpiresAt.Before(now) {
			err := m.ForceUnlock(ctx, lock.Key)
			if err != nil {
				// 记录错误但继续清理其他锁
				fmt.Printf("Failed to clean expired lock %s: %v\n", lock.Key, err)
				continue
			}
			cleaned++
		}
	}

	return cleaned, nil
}

// GetLockStats 获取锁统计信息
func (m *RedisLockManager) GetLockStats(ctx context.Context, pattern string) (*LockStats, error) {
	locks, err := m.ListLocks(ctx, pattern)
	if err != nil {
		return nil, err
	}

	stats := &LockStats{
		TotalLocks:   len(locks),
		ActiveLocks:  0,
		ExpiredLocks: 0,
		OwnerStats:   make(map[string]int),
	}

	now := time.Now()
	for _, lock := range locks {
		if lock.ExpiresAt.After(now) {
			stats.ActiveLocks++
		} else {
			stats.ExpiredLocks++
		}

		stats.OwnerStats[lock.Owner]++
	}

	return stats, nil
}
