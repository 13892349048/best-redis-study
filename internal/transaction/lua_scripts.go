package transaction

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// 常用的Lua脚本定义

// CompareAndSwapScript 原子比较交换脚本
var CompareAndSwapScript = &LuaScript{
	Script: `
		local key = KEYS[1]
		local expected = ARGV[1]
		local new_value = ARGV[2]
		local ttl_seconds = tonumber(ARGV[3]) or -1
		
		local current = redis.call('GET', key)
		if current == expected then
			if ttl_seconds > 0 then
				redis.call('SETEX', key, ttl_seconds, new_value)
			else
				redis.call('SET', key, new_value)
			end
			return 1
		else
			return 0
		end
	`,
}

// IncrWithLimitScript 带限制的递增脚本
var IncrWithLimitScript = &LuaScript{
	Script: `
		local key = KEYS[1]
		local limit = tonumber(ARGV[1])
		local increment = tonumber(ARGV[2]) or 1
		local ttl_seconds = tonumber(ARGV[3]) or -1
		
		local current = redis.call('GET', key)
		if current == false then
			current = 0
		else
			current = tonumber(current)
		end
		
		if current + increment <= limit then
			local new_value = redis.call('INCRBY', key, increment)
			if ttl_seconds > 0 then
				redis.call('EXPIRE', key, ttl_seconds)
			end
			return new_value
		else
			return -1
		end
	`,
}

// AtomicDeductionScript 原子扣减库存脚本
var AtomicDeductionScript = &LuaScript{
	Script: `
		local stock_key = KEYS[1]
		local cache_key = KEYS[2]
		local deduct_amount = tonumber(ARGV[1])
		local cache_value = ARGV[2]
		local cache_ttl = tonumber(ARGV[3]) or 3600
		
		-- 检查库存
		local current_stock = redis.call('GET', stock_key)
		if current_stock == false then
			return {-1, 'stock_not_found'}
		end
		
		current_stock = tonumber(current_stock)
		if current_stock < deduct_amount then
			return {-2, 'insufficient_stock'}
		end
		
		-- 扣减库存
		local new_stock = redis.call('DECRBY', stock_key, deduct_amount)
		
		-- 更新缓存
		if cache_value ~= nil and cache_value ~= '' then
			redis.call('SETEX', cache_key, cache_ttl, cache_value)
		end
		
		return {new_stock, 'success'}
	`,
}

// DistributedLockScript 分布式锁脚本
var DistributedLockScript = &LuaScript{
	Script: `
		local lock_key = KEYS[1]
		local identifier = ARGV[1]
		local ttl_seconds = tonumber(ARGV[2])
		
		if redis.call('SET', lock_key, identifier, 'NX', 'EX', ttl_seconds) then
			return 1
		else
			return 0
		end
	`,
}

// ReleaseLockScript 释放锁脚本
var ReleaseLockScript = &LuaScript{
	Script: `
		local lock_key = KEYS[1]
		local identifier = ARGV[1]
		
		if redis.call('GET', lock_key) == identifier then
			return redis.call('DEL', lock_key)
		else
			return 0
		end
	`,
}

// RenewLockScript 续租锁脚本
var RenewLockScript = &LuaScript{
	Script: `
		local lock_key = KEYS[1]
		local identifier = ARGV[1]
		local ttl_seconds = tonumber(ARGV[2])
		
		if redis.call('GET', lock_key) == identifier then
			return redis.call('EXPIRE', lock_key, ttl_seconds)
		else
			return 0
		end
	`,
}

// SlidingWindowCounterScript 滑动窗口计数器脚本
var SlidingWindowCounterScript = &LuaScript{
	Script: `
		local key = KEYS[1]
		local window_size = tonumber(ARGV[1])
		local limit = tonumber(ARGV[2])
		local current_time = tonumber(ARGV[3])
		local increment = tonumber(ARGV[4]) or 1
		
		-- 清理过期的时间戳
		redis.call('ZREMRANGEBYSCORE', key, '-inf', current_time - window_size)
		
		-- 获取当前窗口内的计数
		local current_count = redis.call('ZCARD', key)
		
		if current_count < limit then
			-- 添加新的时间戳
			for i = 1, increment do
				redis.call('ZADD', key, current_time, current_time .. ':' .. i)
			end
			redis.call('EXPIRE', key, math.ceil(window_size / 1000))
			return {current_count + increment, 'allowed'}
		else
			return {current_count, 'rate_limited'}
		end
	`,
}

// BatchOperationScript 批量操作脚本
var BatchOperationScript = &LuaScript{
	Script: `
		local results = {}
		local operations = cjson.decode(ARGV[1])
		
		for i, op in ipairs(operations) do
			local result
			if op.cmd == 'SET' then
				result = redis.call('SET', op.key, op.value)
				if op.ttl and op.ttl > 0 then
					redis.call('EXPIRE', op.key, op.ttl)
				end
			elseif op.cmd == 'GET' then
				result = redis.call('GET', op.key)
			elseif op.cmd == 'INCR' then
				result = redis.call('INCR', op.key)
			elseif op.cmd == 'DECR' then
				result = redis.call('DECR', op.key)
			elseif op.cmd == 'DEL' then
				result = redis.call('DEL', op.key)
			elseif op.cmd == 'HSET' then
				result = redis.call('HSET', op.key, op.field, op.value)
			elseif op.cmd == 'HGET' then
				result = redis.call('HGET', op.key, op.field)
			else
				result = 'UNKNOWN_COMMAND'
			end
			table.insert(results, result)
		end
		
		return results
	`,
}

// MultiKeyTransactionScript 多键事务脚本
var MultiKeyTransactionScript = &LuaScript{
	Script: `
		local keys = KEYS
		local values = {}
		local operations = cjson.decode(ARGV[1])
		
		-- 读取所有键的当前值
		for i, key in ipairs(keys) do
			values[key] = redis.call('GET', key)
		end
		
		-- 执行操作
		for i, op in ipairs(operations) do
			if op.type == 'check' then
				if values[op.key] ~= op.expected then
					return {false, 'check_failed', op.key, values[op.key], op.expected}
				end
			elseif op.type == 'set' then
				redis.call('SET', op.key, op.value)
				if op.ttl and op.ttl > 0 then
					redis.call('EXPIRE', op.key, op.ttl)
				end
			elseif op.type == 'incr' then
				redis.call('INCRBY', op.key, op.amount or 1)
			elseif op.type == 'decr' then
				redis.call('DECRBY', op.key, op.amount or 1)
			elseif op.type == 'del' then
				redis.call('DEL', op.key)
			end
		end
		
		return {true, 'success'}
	`,
}

// LuaScriptManager Lua脚本管理器
type LuaScriptManager struct {
	client  *redis.Client
	scripts map[string]*LuaScript
}

// NewLuaScriptManager 创建脚本管理器
func NewLuaScriptManager(client *redis.Client) *LuaScriptManager {
	manager := &LuaScriptManager{
		client:  client,
		scripts: make(map[string]*LuaScript),
	}

	// 注册预定义脚本
	manager.RegisterScript("compare_and_swap", CompareAndSwapScript)
	manager.RegisterScript("incr_with_limit", IncrWithLimitScript)
	manager.RegisterScript("atomic_deduction", AtomicDeductionScript)
	manager.RegisterScript("distributed_lock", DistributedLockScript)
	manager.RegisterScript("release_lock", ReleaseLockScript)
	manager.RegisterScript("renew_lock", RenewLockScript)
	manager.RegisterScript("sliding_window_counter", SlidingWindowCounterScript)
	manager.RegisterScript("batch_operation", BatchOperationScript)
	manager.RegisterScript("multi_key_transaction", MultiKeyTransactionScript)

	return manager
}

// calculateScriptSHA 计算脚本SHA
func calculateScriptSHA(script string) string {
	hash := sha1.Sum([]byte(script))
	return hex.EncodeToString(hash[:])
}

// RegisterScript 注册脚本
func (m *LuaScriptManager) RegisterScript(name string, script *LuaScript) {
	if script.SHA == "" {
		script.SHA = calculateScriptSHA(script.Script)
	}
	m.scripts[name] = script
}

// GetScript 获取脚本
func (m *LuaScriptManager) GetScript(name string) (*LuaScript, bool) {
	script, exists := m.scripts[name]
	return script, exists
}

// ExecuteScript 执行脚本
func (m *LuaScriptManager) ExecuteScript(ctx context.Context, name string, keys []string, args []interface{}) (interface{}, error) {
	script, exists := m.GetScript(name)
	if !exists {
		return nil, fmt.Errorf("script not found: %s", name)
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

// LoadAllScripts 加载所有脚本到Redis
func (m *LuaScriptManager) LoadAllScripts(ctx context.Context) error {
	for name, script := range m.scripts {
		sha, err := m.client.ScriptLoad(ctx, script.Script).Result()
		if err != nil {
			return fmt.Errorf("failed to load script %s: %v", name, err)
		}
		script.SHA = sha
	}
	return nil
}

// CompareAndSwap 原子比较交换
func (m *LuaScriptManager) CompareAndSwap(ctx context.Context, key, expected, newValue string, ttlSeconds int) (bool, error) {
	result, err := m.ExecuteScript(ctx, "compare_and_swap", []string{key}, []interface{}{expected, newValue, ttlSeconds})
	if err != nil {
		return false, err
	}

	if result == int64(1) {
		return true, nil
	}
	return false, nil
}

// IncrWithLimit 带限制的递增
func (m *LuaScriptManager) IncrWithLimit(ctx context.Context, key string, limit, increment int64, ttlSeconds int) (int64, error) {
	result, err := m.ExecuteScript(ctx, "incr_with_limit", []string{key}, []interface{}{limit, increment, ttlSeconds})
	if err != nil {
		return 0, err
	}

	if val, ok := result.(int64); ok {
		return val, nil
	}
	return 0, fmt.Errorf("unexpected result type: %T", result)
}

// AtomicDeduction 原子扣减库存
func (m *LuaScriptManager) AtomicDeduction(ctx context.Context, stockKey, cacheKey string, deductAmount int64, cacheValue string, cacheTTL int) (int64, string, error) {
	result, err := m.ExecuteScript(ctx, "atomic_deduction", []string{stockKey, cacheKey}, []interface{}{deductAmount, cacheValue, cacheTTL})
	if err != nil {
		return 0, "", err
	}

	if results, ok := result.([]interface{}); ok && len(results) == 2 {
		stock, _ := results[0].(int64)
		message, _ := results[1].(string)
		return stock, message, nil
	}

	return 0, "", fmt.Errorf("unexpected result format: %v", result)
}

// AcquireLock 获取分布式锁
func (m *LuaScriptManager) AcquireLock(ctx context.Context, lockKey, identifier string, ttlSeconds int) (bool, error) {
	result, err := m.ExecuteScript(ctx, "distributed_lock", []string{lockKey}, []interface{}{identifier, ttlSeconds})
	if err != nil {
		return false, err
	}

	if result == int64(1) {
		return true, nil
	}
	return false, nil
}

// ReleaseLock 释放分布式锁
func (m *LuaScriptManager) ReleaseLock(ctx context.Context, lockKey, identifier string) (bool, error) {
	result, err := m.ExecuteScript(ctx, "release_lock", []string{lockKey}, []interface{}{identifier})
	if err != nil {
		return false, err
	}

	if result == int64(1) {
		return true, nil
	}
	return false, nil
}

// RenewLock 续租分布式锁
func (m *LuaScriptManager) RenewLock(ctx context.Context, lockKey, identifier string, ttlSeconds int) (bool, error) {
	result, err := m.ExecuteScript(ctx, "renew_lock", []string{lockKey}, []interface{}{identifier, ttlSeconds})
	if err != nil {
		return false, err
	}

	if result == int64(1) {
		return true, nil
	}
	return false, nil
}

// SlidingWindowCounter 滑动窗口计数器
func (m *LuaScriptManager) SlidingWindowCounter(ctx context.Context, key string, windowSize, limit int64, currentTime int64, increment int) (int64, string, error) {
	result, err := m.ExecuteScript(ctx, "sliding_window_counter", []string{key}, []interface{}{windowSize, limit, currentTime, increment})
	if err != nil {
		return 0, "", err
	}

	if results, ok := result.([]interface{}); ok && len(results) == 2 {
		count, _ := results[0].(int64)
		status, _ := results[1].(string)
		return count, status, nil
	}

	return 0, "", fmt.Errorf("unexpected result format: %v", result)
}
