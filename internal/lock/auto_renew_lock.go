package lock

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// AutoRenewRedisLock 自动续租Redis分布式锁
type AutoRenewRedisLock struct {
	*RedisLock

	// 续租管理
	renewMap sync.Map // map[string]*renewInfo
}

// renewInfo 续租信息
type renewInfo struct {
	cancel    context.CancelFunc
	done      chan struct{}
	lockKey   string
	owner     string
	ttl       time.Duration
	interval  time.Duration
	token     int64
	lastError error
	mu        sync.RWMutex
}

// NewAutoRenewRedisLock 创建自动续租Redis分布式锁
func NewAutoRenewRedisLock(client redis.Cmdable, options *LockOptions) *AutoRenewRedisLock {
	return &AutoRenewRedisLock{
		RedisLock: NewRedisLock(client, options),
	}
}

// StartAutoRenew 启动自动续租
func (a *AutoRenewRedisLock) StartAutoRenew(ctx context.Context, lockKey, owner string, ttl, renewInterval time.Duration, fencingToken int64) (func(), error) {
	// 创建续租上下文
	renewCtx, cancel := context.WithCancel(ctx)

	info := &renewInfo{
		cancel:   cancel,
		done:     make(chan struct{}),
		lockKey:  lockKey,
		owner:    owner,
		ttl:      ttl,
		interval: renewInterval,
		token:    fencingToken,
	}

	// 生成续租key
	renewKey := a.getRenewKey(lockKey, owner)

	// 检查是否已经在续租
	if _, exists := a.renewMap.Load(renewKey); exists {
		cancel()
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: "auto renew already started for this lock",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	// 存储续租信息
	a.renewMap.Store(renewKey, info)

	// 启动续租协程
	go a.renewLoop(renewCtx, info)

	// 返回停止函数
	stopFunc := func() {
		cancel()
		<-info.done // 等待续租协程结束
		a.renewMap.Delete(renewKey)
	}

	return stopFunc, nil
}

// renewLoop 续租循环
func (a *AutoRenewRedisLock) renewLoop(ctx context.Context, info *renewInfo) {
	defer close(info.done)

	ticker := time.NewTicker(info.interval)
	defer ticker.Stop()

	log.Printf("[AutoRenewLock] Starting auto renew for lock %s, owner %s, interval %v",
		info.lockKey, info.owner, info.interval)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[AutoRenewLock] Auto renew stopped for lock %s, owner %s",
				info.lockKey, info.owner)
			return
		case <-ticker.C:
			a.performRenew(ctx, info)
		}
	}
}

// performRenew 执行续租
func (a *AutoRenewRedisLock) performRenew(ctx context.Context, info *renewInfo) {
	info.mu.Lock()
	defer info.mu.Unlock()

	// 创建续租上下文，设置超时
	renewCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	success, err := a.Renew(renewCtx, info.lockKey, info.owner, info.ttl, info.token)
	if err != nil {
		info.lastError = err
		log.Printf("[AutoRenewLock] Failed to renew lock %s, owner %s: %v",
			info.lockKey, info.owner, err)

		// 如果是token不匹配或锁不存在，说明锁可能被其他人获取或过期，停止续租
		if lockErr, ok := err.(*LockError); ok {
			if lockErr.Code == ErrCodeInvalidToken || lockErr.Code == ErrCodeLockNotFound {
				log.Printf("[AutoRenewLock] Lock %s, owner %s is invalid, stopping auto renew",
					info.lockKey, info.owner)
				return
			}
		}
		return
	}

	if !success {
		info.lastError = ErrRenewFailed
		log.Printf("[AutoRenewLock] Failed to renew lock %s, owner %s: renewal not successful",
			info.lockKey, info.owner)
		return
	}

	// 续租成功，清除错误
	info.lastError = nil
	log.Printf("[AutoRenewLock] Successfully renewed lock %s, owner %s",
		info.lockKey, info.owner)
}

// GetRenewStatus 获取续租状态
func (a *AutoRenewRedisLock) GetRenewStatus(lockKey, owner string) (*RenewStatus, error) {
	renewKey := a.getRenewKey(lockKey, owner)

	value, exists := a.renewMap.Load(renewKey)
	if !exists {
		return &RenewStatus{
			IsActive: false,
		}, nil
	}

	info, ok := value.(*renewInfo)
	if !ok {
		return nil, &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid renew info type",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	info.mu.RLock()
	defer info.mu.RUnlock()

	return &RenewStatus{
		IsActive:     true,
		LockKey:      info.lockKey,
		Owner:        info.owner,
		TTL:          info.ttl,
		Interval:     info.interval,
		FencingToken: info.token,
		LastError:    info.lastError,
	}, nil
}

// StopAutoRenew 停止自动续租
func (a *AutoRenewRedisLock) StopAutoRenew(lockKey, owner string) error {
	renewKey := a.getRenewKey(lockKey, owner)

	value, exists := a.renewMap.Load(renewKey)
	if !exists {
		return &LockError{
			Code:    ErrCodeLockNotFound,
			Message: "auto renew not found for this lock",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	info, ok := value.(*renewInfo)
	if !ok {
		return &LockError{
			Code:    ErrCodeRedisError,
			Message: "invalid renew info type",
			Key:     lockKey,
			Owner:   owner,
		}
	}

	// 取消续租
	info.cancel()

	// 等待续租协程结束
	<-info.done

	// 删除续租信息
	a.renewMap.Delete(renewKey)

	return nil
}

// StopAllAutoRenew 停止所有自动续租
func (a *AutoRenewRedisLock) StopAllAutoRenew() {
	var wg sync.WaitGroup

	a.renewMap.Range(func(key, value interface{}) bool {
		info, ok := value.(*renewInfo)
		if !ok {
			return true
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			info.cancel()
			<-info.done
		}()

		a.renewMap.Delete(key)
		return true
	})

	wg.Wait()
	log.Println("[AutoRenewLock] All auto renew stopped")
}

// getRenewKey 获取续租key
func (a *AutoRenewRedisLock) getRenewKey(lockKey, owner string) string {
	return lockKey + ":" + owner + ":renew"
}
