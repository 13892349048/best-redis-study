package transaction

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisTransactionContext Redis事务上下文实现
type redisTransactionContext struct {
	tx   *redis.Tx
	pipe redis.Pipeliner
	ctx  context.Context
}

// Set 设置键值对
func (t *redisTransactionContext) Set(key, value string, expiration time.Duration) error {
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

// Get 获取键值
func (t *redisTransactionContext) Get(key string) (string, error) {
	if t.tx != nil {
		return t.tx.Get(t.ctx, key).Result()
	}
	// 在Pipeline中不能执行读操作
	return "", errors.New("cannot read in pipeline context")
}

// Del 删除键
func (t *redisTransactionContext) Del(keys ...string) error {
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

// Exists 检查键是否存在
func (t *redisTransactionContext) Exists(keys ...string) (int64, error) {
	if t.tx != nil {
		return t.tx.Exists(t.ctx, keys...).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

// Incr 递增
func (t *redisTransactionContext) Incr(key string) (int64, error) {
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

// Decr 递减
func (t *redisTransactionContext) Decr(key string) (int64, error) {
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

// IncrBy 按指定值递增
func (t *redisTransactionContext) IncrBy(key string, value int64) (int64, error) {
	if t.tx != nil {
		var result int64
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			cmd := pipe.IncrBy(t.ctx, key, value)
			result = cmd.Val()
			return nil
		})
		return result, err
	}
	if t.pipe != nil {
		cmd := t.pipe.IncrBy(t.ctx, key, value)
		return cmd.Val(), nil
	}
	return 0, errors.New("no transaction context available")
}

// DecrBy 按指定值递减
func (t *redisTransactionContext) DecrBy(key string, value int64) (int64, error) {
	if t.tx != nil {
		var result int64
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			cmd := pipe.DecrBy(t.ctx, key, value)
			result = cmd.Val()
			return nil
		})
		return result, err
	}
	if t.pipe != nil {
		cmd := t.pipe.DecrBy(t.ctx, key, value)
		return cmd.Val(), nil
	}
	return 0, errors.New("no transaction context available")
}

// HSet 设置Hash字段
func (t *redisTransactionContext) HSet(key, field, value string) error {
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

// HGet 获取Hash字段
func (t *redisTransactionContext) HGet(key, field string) (string, error) {
	if t.tx != nil {
		return t.tx.HGet(t.ctx, key, field).Result()
	}
	return "", errors.New("cannot read in pipeline context")
}

// HDel 删除Hash字段
func (t *redisTransactionContext) HDel(key string, fields ...string) error {
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

// HExists 检查Hash字段是否存在
func (t *redisTransactionContext) HExists(key, field string) (bool, error) {
	if t.tx != nil {
		return t.tx.HExists(t.ctx, key, field).Result()
	}
	return false, errors.New("cannot read in pipeline context")
}

// HIncrBy Hash字段递增
func (t *redisTransactionContext) HIncrBy(key, field string, incr int64) (int64, error) {
	if t.tx != nil {
		var result int64
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			cmd := pipe.HIncrBy(t.ctx, key, field, incr)
			result = cmd.Val()
			return nil
		})
		return result, err
	}
	if t.pipe != nil {
		cmd := t.pipe.HIncrBy(t.ctx, key, field, incr)
		return cmd.Val(), nil
	}
	return 0, errors.New("no transaction context available")
}

// LPush 从左侧推入列表
func (t *redisTransactionContext) LPush(key string, values ...interface{}) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.LPush(t.ctx, key, values...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.LPush(t.ctx, key, values...)
		return nil
	}
	return errors.New("no transaction context available")
}

// RPush 从右侧推入列表
func (t *redisTransactionContext) RPush(key string, values ...interface{}) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.RPush(t.ctx, key, values...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.RPush(t.ctx, key, values...)
		return nil
	}
	return errors.New("no transaction context available")
}

// LPop 从左侧弹出列表元素
func (t *redisTransactionContext) LPop(key string) (string, error) {
	if t.tx != nil {
		return t.tx.LPop(t.ctx, key).Result()
	}
	return "", errors.New("cannot read in pipeline context")
}

// RPop 从右侧弹出列表元素
func (t *redisTransactionContext) RPop(key string) (string, error) {
	if t.tx != nil {
		return t.tx.RPop(t.ctx, key).Result()
	}
	return "", errors.New("cannot read in pipeline context")
}

// LLen 获取列表长度
func (t *redisTransactionContext) LLen(key string) (int64, error) {
	if t.tx != nil {
		return t.tx.LLen(t.ctx, key).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

// SAdd 向集合添加成员
func (t *redisTransactionContext) SAdd(key string, members ...interface{}) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.SAdd(t.ctx, key, members...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.SAdd(t.ctx, key, members...)
		return nil
	}
	return errors.New("no transaction context available")
}

// SRem 从集合移除成员
func (t *redisTransactionContext) SRem(key string, members ...interface{}) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.SRem(t.ctx, key, members...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.SRem(t.ctx, key, members...)
		return nil
	}
	return errors.New("no transaction context available")
}

// SIsMember 检查是否是集合成员
func (t *redisTransactionContext) SIsMember(key string, member interface{}) (bool, error) {
	if t.tx != nil {
		return t.tx.SIsMember(t.ctx, key, member).Result()
	}
	return false, errors.New("cannot read in pipeline context")
}

// SCard 获取集合成员数量
func (t *redisTransactionContext) SCard(key string) (int64, error) {
	if t.tx != nil {
		return t.tx.SCard(t.ctx, key).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

// ZAdd 向有序集合添加成员
func (t *redisTransactionContext) ZAdd(key string, members ...interface{}) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			// 需要将interface{}转换为redis.Z
			var zMembers []redis.Z
			for i := 0; i < len(members); i += 2 {
				if i+1 < len(members) {
					score, ok1 := members[i].(float64)
					member, ok2 := members[i+1].(string)
					if ok1 && ok2 {
						zMembers = append(zMembers, redis.Z{Score: score, Member: member})
					}
				}
			}
			pipe.ZAdd(t.ctx, key, zMembers...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		var zMembers []redis.Z
		for i := 0; i < len(members); i += 2 {
			if i+1 < len(members) {
				score, ok1 := members[i].(float64)
				member, ok2 := members[i+1].(string)
				if ok1 && ok2 {
					zMembers = append(zMembers, redis.Z{Score: score, Member: member})
				}
			}
		}
		t.pipe.ZAdd(t.ctx, key, zMembers...)
		return nil
	}
	return errors.New("no transaction context available")
}

// ZRem 从有序集合移除成员
func (t *redisTransactionContext) ZRem(key string, members ...interface{}) error {
	if t.tx != nil {
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			pipe.ZRem(t.ctx, key, members...)
			return nil
		})
		return err
	}
	if t.pipe != nil {
		t.pipe.ZRem(t.ctx, key, members...)
		return nil
	}
	return errors.New("no transaction context available")
}

// ZScore 获取有序集合成员分数
func (t *redisTransactionContext) ZScore(key string, member string) (float64, error) {
	if t.tx != nil {
		return t.tx.ZScore(t.ctx, key, member).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

// ZCard 获取有序集合成员数量
func (t *redisTransactionContext) ZCard(key string) (int64, error) {
	if t.tx != nil {
		return t.tx.ZCard(t.ctx, key).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

// Expire 设置过期时间
func (t *redisTransactionContext) Expire(key string, expiration time.Duration) error {
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

// TTL 获取过期时间
func (t *redisTransactionContext) TTL(key string) (time.Duration, error) {
	if t.tx != nil {
		return t.tx.TTL(t.ctx, key).Result()
	}
	return 0, errors.New("cannot read in pipeline context")
}

// SetNX 仅在键不存在时设置
func (t *redisTransactionContext) SetNX(key, value string, expiration time.Duration) (bool, error) {
	if t.tx != nil {
		var result bool
		_, err := t.tx.TxPipelined(t.ctx, func(pipe redis.Pipeliner) error {
			cmd := pipe.SetNX(t.ctx, key, value, expiration)
			result = cmd.Val()
			return nil
		})
		return result, err
	}
	if t.pipe != nil {
		cmd := t.pipe.SetNX(t.ctx, key, value, expiration)
		return cmd.Val(), nil
	}
	return false, errors.New("no transaction context available")
}
