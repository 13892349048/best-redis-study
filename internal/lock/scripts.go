package lock

// Lua脚本用于原子性操作

// LockScript 获取锁的Lua脚本
// KEYS[1]: 锁的key
// KEYS[2]: fencing token计数器的key
// ARGV[1]: 锁的owner
// ARGV[2]: 锁的TTL（毫秒）
// 返回: {1, fencing_token} 成功获取锁，{0, 0} 锁已被占用
const LockScript = `
local lock_key = KEYS[1]
local token_key = KEYS[2]
local owner = ARGV[1]
local ttl = tonumber(ARGV[2])

-- 检查锁是否已经存在
local current_owner = redis.call('HGET', lock_key, 'owner')
if current_owner and current_owner ~= owner then
    -- 锁被其他人占用
    return {0, 0}
end

-- 获取或生成fencing token
local fencing_token
if current_owner == owner then
    -- 重入锁，使用现有token
    fencing_token = tonumber(redis.call('HGET', lock_key, 'token'))
else
    -- 新锁，生成新token
    fencing_token = redis.call('INCR', token_key)
end

-- 设置锁信息
redis.call('HMSET', lock_key, 
    'owner', owner,
    'token', fencing_token,
    'created_at', redis.call('TIME')[1]
)

-- 设置过期时间
redis.call('PEXPIRE', lock_key, ttl)

return {1, fencing_token}
`

// UnlockScript 释放锁的Lua脚本
// KEYS[1]: 锁的key
// ARGV[1]: 锁的owner
// ARGV[2]: fencing token
// 返回: 1 成功释放，0 锁不存在或owner不匹配，-1 token不匹配
const UnlockScript = `
local lock_key = KEYS[1]
local owner = ARGV[1]
local token = tonumber(ARGV[2])

-- 检查锁是否存在
local current_owner = redis.call('HGET', lock_key, 'owner')
if not current_owner then
    -- 锁不存在
    return 0
end

-- 检查owner是否匹配
if current_owner ~= owner then
    -- owner不匹配
    return 0
end

-- 检查fencing token是否匹配
local current_token = tonumber(redis.call('HGET', lock_key, 'token'))
if current_token ~= token then
    -- token不匹配
    return -1
end

-- 删除锁
redis.call('DEL', lock_key)
return 1
`

// RenewScript 续租锁的Lua脚本
// KEYS[1]: 锁的key
// ARGV[1]: 锁的owner
// ARGV[2]: 新的TTL（毫秒）
// ARGV[3]: fencing token
// 返回: 1 成功续租，0 锁不存在或owner不匹配，-1 token不匹配
const RenewScript = `
local lock_key = KEYS[1]
local owner = ARGV[1]
local ttl = tonumber(ARGV[2])
local token = tonumber(ARGV[3])

-- 检查锁是否存在
local current_owner = redis.call('HGET', lock_key, 'owner')
if not current_owner then
    -- 锁不存在
    return 0
end

-- 检查owner是否匹配
if current_owner ~= owner then
    -- owner不匹配
    return 0
end

-- 检查fencing token是否匹配
local current_token = tonumber(redis.call('HGET', lock_key, 'token'))
if current_token ~= token then
    -- token不匹配
    return -1
end

-- 续租
redis.call('PEXPIRE', lock_key, ttl)
return 1
`

// GetLockInfoScript 获取锁信息的Lua脚本
// KEYS[1]: 锁的key
// 返回: {owner, token, created_at, ttl} 或 nil
const GetLockInfoScript = `
local lock_key = KEYS[1]

-- 检查锁是否存在
local exists = redis.call('EXISTS', lock_key)
if exists == 0 then
    return nil
end

-- 获取锁信息
local owner = redis.call('HGET', lock_key, 'owner')
local token = redis.call('HGET', lock_key, 'token')
local created_at = redis.call('HGET', lock_key, 'created_at')
local ttl = redis.call('PTTL', lock_key)

return {owner, token, created_at, ttl}
`

// ForceUnlockScript 强制释放锁的Lua脚本（谨慎使用）
// KEYS[1]: 锁的key
// 返回: 1 成功释放，0 锁不存在
const ForceUnlockScript = `
local lock_key = KEYS[1]

-- 检查锁是否存在
local exists = redis.call('EXISTS', lock_key)
if exists == 0 then
    return 0
end

-- 删除锁
redis.call('DEL', lock_key)
return 1
`
