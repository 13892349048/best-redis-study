package lock

import (
	"context"
	"time"
)

// Lock 分布式锁接口
type Lock interface {
	// TryLock 尝试获取锁
	// lockKey: 锁的键名
	// owner: 锁的拥有者标识（通常是UUID）
	// ttl: 锁的过期时间
	// 返回: 是否成功获取锁，fencing token，错误
	TryLock(ctx context.Context, lockKey, owner string, ttl time.Duration) (bool, int64, error)

	// Lock 阻塞式获取锁
	// lockKey: 锁的键名
	// owner: 锁的拥有者标识
	// ttl: 锁的过期时间
	// timeout: 获取锁的超时时间
	// 返回: fencing token，错误
	Lock(ctx context.Context, lockKey, owner string, ttl, timeout time.Duration) (int64, error)

	// Unlock 释放锁
	// lockKey: 锁的键名
	// owner: 锁的拥有者标识
	// fencingToken: fencing token用于防止释放别人的锁
	// 返回: 是否成功释放，错误
	Unlock(ctx context.Context, lockKey, owner string, fencingToken int64) (bool, error)

	// Renew 续租锁
	// lockKey: 锁的键名
	// owner: 锁的拥有者标识
	// ttl: 新的过期时间
	// fencingToken: fencing token用于验证锁的有效性
	// 返回: 是否成功续租，错误
	Renew(ctx context.Context, lockKey, owner string, ttl time.Duration, fencingToken int64) (bool, error)

	// IsLocked 检查锁是否存在
	// lockKey: 锁的键名
	// 返回: 是否被锁定，锁的拥有者，fencing token，错误
	IsLocked(ctx context.Context, lockKey string) (bool, string, int64, error)

	// GetFencingToken 获取当前锁的fencing token
	// lockKey: 锁的键名
	// owner: 锁的拥有者标识
	// 返回: fencing token，错误
	GetFencingToken(ctx context.Context, lockKey, owner string) (int64, error)
}

// AutoRenewLock 自动续租锁接口
type AutoRenewLock interface {
	Lock

	// StartAutoRenew 启动自动续租
	// lockKey: 锁的键名
	// owner: 锁的拥有者标识
	// ttl: 锁的过期时间
	// renewInterval: 续租间隔（建议为ttl的1/3）
	// fencingToken: fencing token
	// 返回: 停止续租的函数，错误
	StartAutoRenew(ctx context.Context, lockKey, owner string, ttl, renewInterval time.Duration, fencingToken int64) (func(), error)

	// GetRenewStatus 获取续租状态
	GetRenewStatus(lockKey, owner string) (*RenewStatus, error)

	// StopAutoRenew 停止自动续租
	StopAutoRenew(lockKey, owner string) error

	// StopAllAutoRenew 停止所有自动续租
	StopAllAutoRenew()
}

// RenewStatus 续租状态
type RenewStatus struct {
	IsActive     bool          `json:"is_active"`
	LockKey      string        `json:"lock_key,omitempty"`
	Owner        string        `json:"owner,omitempty"`
	TTL          time.Duration `json:"ttl,omitempty"`
	Interval     time.Duration `json:"interval,omitempty"`
	FencingToken int64         `json:"fencing_token,omitempty"`
	LastError    error         `json:"last_error,omitempty"`
}

// LockManager 锁管理器接口
type LockManager interface {
	// CreateLock 创建锁实例
	CreateLock() Lock

	// CreateAutoRenewLock 创建自动续租锁实例
	CreateAutoRenewLock() AutoRenewLock

	// GetLockInfo 获取锁信息
	GetLockInfo(ctx context.Context, lockKey string) (*LockInfo, error)

	// ListLocks 列出所有锁（用于调试）
	ListLocks(ctx context.Context, pattern string) ([]*LockInfo, error)

	// ForceUnlock 强制释放锁（谨慎使用）
	ForceUnlock(ctx context.Context, lockKey string) error

	// GetLockStats 获取锁统计信息
	GetLockStats(ctx context.Context, pattern string) (*LockStats, error)

	// CleanExpiredLocks 清理过期锁
	CleanExpiredLocks(ctx context.Context, pattern string) (int, error)
}

// LockInfo 锁信息
type LockInfo struct {
	Key          string    `json:"key"`           // 锁的键名
	Owner        string    `json:"owner"`         // 锁的拥有者
	FencingToken int64     `json:"fencing_token"` // fencing token
	CreatedAt    time.Time `json:"created_at"`    // 创建时间
	ExpiresAt    time.Time `json:"expires_at"`    // 过期时间
	TTL          int64     `json:"ttl"`           // 剩余生存时间（秒）
}

// LockStats 锁统计信息
type LockStats struct {
	TotalLocks   int            `json:"total_locks"`
	ActiveLocks  int            `json:"active_locks"`
	ExpiredLocks int            `json:"expired_locks"`
	OwnerStats   map[string]int `json:"owner_stats"`
}

// LockOptions 锁选项
type LockOptions struct {
	// 基本选项
	TTL           time.Duration `json:"ttl"`            // 锁的过期时间
	RetryInterval time.Duration `json:"retry_interval"` // 重试间隔
	MaxRetries    int           `json:"max_retries"`    // 最大重试次数

	// 自动续租选项
	AutoRenew     bool          `json:"auto_renew"`     // 是否启用自动续租
	RenewInterval time.Duration `json:"renew_interval"` // 续租间隔

	// Fencing Token选项
	EnableFencing bool `json:"enable_fencing"` // 是否启用fencing token

	// 监控选项
	EnableMetrics bool `json:"enable_metrics"` // 是否启用指标收集
}

// DefaultLockOptions 默认锁选项
func DefaultLockOptions() *LockOptions {
	return &LockOptions{
		TTL:           30 * time.Second,
		RetryInterval: 100 * time.Millisecond,
		MaxRetries:    10,
		AutoRenew:     false,
		RenewInterval: 10 * time.Second, // TTL的1/3
		EnableFencing: true,
		EnableMetrics: true,
	}
}

// LockError 锁相关错误
type LockError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Key     string `json:"key,omitempty"`
	Owner   string `json:"owner,omitempty"`
}

func (e *LockError) Error() string {
	return e.Message
}

// 预定义错误代码
const (
	ErrCodeLockNotAcquired = "LOCK_NOT_ACQUIRED"
	ErrCodeLockNotFound    = "LOCK_NOT_FOUND"
	ErrCodeLockExpired     = "LOCK_EXPIRED"
	ErrCodeInvalidOwner    = "INVALID_OWNER"
	ErrCodeInvalidToken    = "INVALID_TOKEN"
	ErrCodeRenewFailed     = "RENEW_FAILED"
	ErrCodeTimeout         = "TIMEOUT"
	ErrCodeRedisError      = "REDIS_ERROR"
)

// 预定义错误
var (
	ErrLockNotAcquired = &LockError{Code: ErrCodeLockNotAcquired, Message: "failed to acquire lock"}
	ErrLockNotFound    = &LockError{Code: ErrCodeLockNotFound, Message: "lock not found"}
	ErrLockExpired     = &LockError{Code: ErrCodeLockExpired, Message: "lock expired"}
	ErrInvalidOwner    = &LockError{Code: ErrCodeInvalidOwner, Message: "invalid lock owner"}
	ErrInvalidToken    = &LockError{Code: ErrCodeInvalidToken, Message: "invalid fencing token"}
	ErrRenewFailed     = &LockError{Code: ErrCodeRenewFailed, Message: "failed to renew lock"}
	ErrTimeout         = &LockError{Code: ErrCodeTimeout, Message: "lock operation timeout"}
)
