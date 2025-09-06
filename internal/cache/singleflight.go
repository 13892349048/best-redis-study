package cache

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// SingleflightGroup 单飞组，防止同一时间对同一个key的多次请求
type SingleflightGroup struct {
	mu sync.Mutex
	m  map[string]*call // 正在进行的调用
	
	// 统计信息
	stats SingleflightStats
}

// call 代表一个正在进行的或已完成的Do调用
type call struct {
	wg sync.WaitGroup
	
	// 这些字段在wg.Done之前写入，在wg.Done之后读取
	val interface{}
	err error
	
	// 这些字段在第一次调用时设置，并且是只读的
	dups  int           // 重复调用次数
	chans []chan Result // 接收结果的通道列表
}

// Result 单飞调用结果
type Result struct {
	Val    interface{}
	Err    error
	Shared bool // 是否为共享结果（即不是第一个调用者）
}

// SingleflightStats 单飞统计信息
type SingleflightStats struct {
	Hits      int64 // 命中次数（共享结果）
	Misses    int64 // 未命中次数（第一次调用）
	Timeouts  int64 // 超时次数
	Errors    int64 // 错误次数
	mu        sync.RWMutex
}

// LoaderFunc 数据加载函数
type SingleflightLoaderFunc func() (interface{}, error)

// NewSingleflightGroup 创建新的单飞组
func NewSingleflightGroup() *SingleflightGroup {
	return &SingleflightGroup{
		m: make(map[string]*call),
	}
}

// Do 执行单飞调用
// 对于同一个key，同时只会有一个调用执行fn，其他调用会等待并共享结果
func (g *SingleflightGroup) Do(ctx context.Context, key string, fn SingleflightLoaderFunc) (interface{}, error) {
	g.mu.Lock()
	if g.m == nil {
		g.mu.Unlock()
		return nil, fmt.Errorf("singleflight group is closed")
	}
	
	if c, ok := g.m[key]; ok {
		// 已有正在进行的调用
		c.dups++
		g.mu.Unlock()
		
		// 等待结果
		select {
		case <-ctx.Done():
			g.incrementTimeouts()
			return nil, ctx.Err()
		case <-g.waitForCall(c):
			g.incrementHits()
			return c.val, c.err
		}
	}
	
	// 创建新的调用
	c := &call{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()
	
	// 执行调用
	g.doCall(c, key, fn)
	g.incrementMisses()
	
	return c.val, c.err
}

// DoChan 执行单飞调用（异步版本）
// 返回一个接收Result的通道
func (g *SingleflightGroup) DoChan(ctx context.Context, key string, fn SingleflightLoaderFunc) <-chan Result {
	ch := make(chan Result, 1)
	
	g.mu.Lock()
	if g.m == nil {
		g.mu.Unlock()
		ch <- Result{Err: fmt.Errorf("singleflight group is closed")}
		return ch
	}
	
	if c, ok := g.m[key]; ok {
		// 已有正在进行的调用
		c.dups++
		c.chans = append(c.chans, ch)
		g.mu.Unlock()
		
		// 启动协程等待结果
		go func() {
			select {
			case <-ctx.Done():
				ch <- Result{Err: ctx.Err()}
			case <-g.waitForCall(c):
				ch <- Result{Val: c.val, Err: c.err, Shared: true}
			}
		}()
		
		return ch
	}
	
	// 创建新的调用
	c := &call{chans: []chan Result{ch}}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()
	
	// 启动协程执行调用
	go g.doCall(c, key, fn)
	
	return ch
}

// doCall 执行实际的调用
func (g *SingleflightGroup) doCall(c *call, key string, fn SingleflightLoaderFunc) {
	defer func() {
		// 处理panic
		if r := recover(); r != nil {
			c.err = fmt.Errorf("singleflight panic: %v", r)
			g.incrementErrors()
		}
		
		c.wg.Done()
		
		// 清理并通知所有等待的通道
		g.mu.Lock()
		if g.m != nil {
			delete(g.m, key)
		}
		for _, ch := range c.chans {
			ch <- Result{Val: c.val, Err: c.err, Shared: c.dups > 0}
		}
		g.mu.Unlock()
	}()
	
	// 执行函数
	c.val, c.err = fn()
	if c.err != nil {
		g.incrementErrors()
	}
}

// waitForCall 等待调用完成
func (g *SingleflightGroup) waitForCall(c *call) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(ch)
	}()
	return ch
}

// Forget 从组中移除key，这样后续对该key的调用会触发新的函数执行
// 这对于防止某些永远不会完成的调用阻塞其他调用很有用
func (g *SingleflightGroup) Forget(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if g.m != nil {
		delete(g.m, key)
	}
}

// GetStats 获取统计信息
func (g *SingleflightGroup) GetStats() SingleflightStats {
	g.stats.mu.RLock()
	defer g.stats.mu.RUnlock()
	
	return SingleflightStats{
		Hits:     g.stats.Hits,
		Misses:   g.stats.Misses,
		Timeouts: g.stats.Timeouts,
		Errors:   g.stats.Errors,
	}
}

// Close 关闭单飞组
func (g *SingleflightGroup) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if g.m == nil {
		return nil // 已经关闭
	}
	
	// 等待所有正在进行的调用完成
	var wg sync.WaitGroup
	for _, c := range g.m {
		wg.Add(1)
		go func(call *call) {
			defer wg.Done()
			call.wg.Wait()
		}(c)
	}
	
	// 设置为nil表示已关闭
	g.m = nil
	
	// 等待所有调用完成（带超时）
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	
	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout waiting for singleflight calls to complete")
	}
}

// incrementHits 增加命中计数
func (g *SingleflightGroup) incrementHits() {
	g.stats.mu.Lock()
	g.stats.Hits++
	g.stats.mu.Unlock()
}

// incrementMisses 增加未命中计数
func (g *SingleflightGroup) incrementMisses() {
	g.stats.mu.Lock()
	g.stats.Misses++
	g.stats.mu.Unlock()
}

// incrementTimeouts 增加超时计数
func (g *SingleflightGroup) incrementTimeouts() {
	g.stats.mu.Lock()
	g.stats.Timeouts++
	g.stats.mu.Unlock()
}

// incrementErrors 增加错误计数
func (g *SingleflightGroup) incrementErrors() {
	g.stats.mu.Lock()
	g.stats.Errors++
	g.stats.mu.Unlock()
}

// GetActiveKeys 获取当前活跃的key列表
func (g *SingleflightGroup) GetActiveKeys() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if g.m == nil {
		return nil
	}
	
	keys := make([]string, 0, len(g.m))
	for key := range g.m {
		keys = append(keys, key)
	}
	
	return keys
}

// GetActiveCount 获取当前活跃调用数量
func (g *SingleflightGroup) GetActiveCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	
	if g.m == nil {
		return 0
	}
	
	return len(g.m)
}