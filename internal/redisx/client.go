package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// CommandInfo 命令信息
type CommandInfo struct {
	Name   string
	Args   []interface{}
	Result redis.Cmder
}

// PipelineWrapper Pipeline封装器
type PipelineWrapper struct {
	client      *Client
	pipeline    redis.Pipeliner
	commands    []redis.Cmder
	commandInfo []CommandInfo // 新增：保存命令参数信息
	batchSize   int
}

// NewPipeline 创建新的Pipeline
func (c *Client) NewPipeline() *PipelineWrapper {
	return &PipelineWrapper{
		client:      c,
		pipeline:    c.Pipeline(),
		commands:    make([]redis.Cmder, 0),
		commandInfo: make([]CommandInfo, 0),
		batchSize:   100, // 默认批量大小
	}
}

// NewTxPipeline 创建新的事务Pipeline
func (c *Client) NewTxPipeline() *PipelineWrapper {
	return &PipelineWrapper{
		client:      c,
		pipeline:    c.TxPipeline(),
		commands:    make([]redis.Cmder, 0),
		commandInfo: make([]CommandInfo, 0),
		batchSize:   100,
	}
}

// SetBatchSize 设置批量大小
func (p *PipelineWrapper) SetBatchSize(size int) *PipelineWrapper {
	p.batchSize = size
	return p
}

// Set 添加SET命令
func (p *PipelineWrapper) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	cmd := p.pipeline.Set(ctx, key, value, expiration)
	p.commands = append(p.commands, cmd)
	// 保存命令信息用于分批执行
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "SET",
		Args:   []interface{}{key, value, expiration},
		Result: cmd,
	})
	return cmd
}

// Get 添加GET命令
func (p *PipelineWrapper) Get(ctx context.Context, key string) *redis.StringCmd {
	cmd := p.pipeline.Get(ctx, key)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "GET",
		Args:   []interface{}{key},
		Result: cmd,
	})
	return cmd
}

// Del 添加DEL命令
func (p *PipelineWrapper) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	cmd := p.pipeline.Del(ctx, keys...)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "DEL",
		Args:   []interface{}{keys},
		Result: cmd,
	})
	return cmd
}

// Incr 添加INCR命令
func (p *PipelineWrapper) Incr(ctx context.Context, key string) *redis.IntCmd {
	cmd := p.pipeline.Incr(ctx, key)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "INCR",
		Args:   []interface{}{key},
		Result: cmd,
	})
	return cmd
}

// HSet 添加HSET命令
func (p *PipelineWrapper) HSet(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
	cmd := p.pipeline.HSet(ctx, key, values...)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "HSET",
		Args:   append([]interface{}{key}, values...),
		Result: cmd,
	})
	return cmd
}

// HGet 添加HGET命令
func (p *PipelineWrapper) HGet(ctx context.Context, key, field string) *redis.StringCmd {
	cmd := p.pipeline.HGet(ctx, key, field)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "HGET",
		Args:   []interface{}{key, field},
		Result: cmd,
	})
	return cmd
}

// LPush 添加LPUSH命令
func (p *PipelineWrapper) LPush(ctx context.Context, key string, values ...interface{}) *redis.IntCmd {
	cmd := p.pipeline.LPush(ctx, key, values...)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "LPUSH",
		Args:   append([]interface{}{key}, values...),
		Result: cmd,
	})
	return cmd
}

// RPop 添加RPOP命令
func (p *PipelineWrapper) RPop(ctx context.Context, key string) *redis.StringCmd {
	cmd := p.pipeline.RPop(ctx, key)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "RPOP",
		Args:   []interface{}{key},
		Result: cmd,
	})
	return cmd
}

// SAdd 添加SADD命令
func (p *PipelineWrapper) SAdd(ctx context.Context, key string, members ...interface{}) *redis.IntCmd {
	cmd := p.pipeline.SAdd(ctx, key, members...)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "SADD",
		Args:   append([]interface{}{key}, members...),
		Result: cmd,
	})
	return cmd
}

// ZAdd 添加ZADD命令
func (p *PipelineWrapper) ZAdd(ctx context.Context, key string, members ...redis.Z) *redis.IntCmd {
	cmd := p.pipeline.ZAdd(ctx, key, members...)
	p.commands = append(p.commands, cmd)
	p.commandInfo = append(p.commandInfo, CommandInfo{
		Name:   "ZADD",
		Args:   []interface{}{key, members},
		Result: cmd,
	})
	return cmd
}

// Exec 执行Pipeline
func (p *PipelineWrapper) Exec(ctx context.Context) ([]redis.Cmder, error) {
	if len(p.commands) == 0 {
		return nil, fmt.Errorf("no commands in pipeline")
	}

	start := time.Now()
	defer func() {
		if p.client.metrics != nil {
			p.client.metrics.RecordLatency(time.Since(start))
		}
	}()

	// 如果启用了指标收集
	if p.client.metrics != nil {
		p.client.metrics.IncPipelineTotal()
		p.client.metrics.AddPipelineBatch(int64(len(p.commands)))
	}

	return p.pipeline.Exec(ctx)
}

// ExecBatch 分批执行Pipeline
func (p *PipelineWrapper) ExecBatch(ctx context.Context) error {
	if len(p.commandInfo) == 0 {
		return fmt.Errorf("no commands in pipeline")
	}

	start := time.Now()
	defer func() {
		if p.client.metrics != nil {
			p.client.metrics.RecordLatency(time.Since(start))
		}
	}()

	// 分批执行
	for i := 0; i < len(p.commandInfo); i += p.batchSize {
		end := i + p.batchSize
		if end > len(p.commandInfo) {
			end = len(p.commandInfo)
		}

		// 创建新的Pipeline用于当前批次
		batchPipeline := p.client.Pipeline()

		// 添加当前批次的命令
		for j := i; j < end; j++ {
			cmdInfo := p.commandInfo[j]
			err := p.rebuildCommand(ctx, batchPipeline, cmdInfo)
			if err != nil {
				return fmt.Errorf("failed to rebuild command %s: %w", cmdInfo.Name, err)
			}
		}

		// 执行当前批次
		_, err := batchPipeline.Exec(ctx)
		if err != nil {
			return fmt.Errorf("batch execution failed at batch %d: %w", i/p.batchSize, err)
		}

		// 更新指标
		if p.client.metrics != nil {
			p.client.metrics.IncPipelineTotal()
			p.client.metrics.AddPipelineBatch(int64(end - i))
		}
	}

	return nil
}

// rebuildCommand 重建命令到新的Pipeline
func (p *PipelineWrapper) rebuildCommand(ctx context.Context, pipeline redis.Pipeliner, cmdInfo CommandInfo) error {
	switch cmdInfo.Name {
	case "SET":
		if len(cmdInfo.Args) >= 3 {
			key := cmdInfo.Args[0].(string)
			value := cmdInfo.Args[1]
			expiration := cmdInfo.Args[2].(time.Duration)
			pipeline.Set(ctx, key, value, expiration)
		}
	case "GET":
		if len(cmdInfo.Args) >= 1 {
			key := cmdInfo.Args[0].(string)
			pipeline.Get(ctx, key)
		}
	case "DEL":
		if len(cmdInfo.Args) >= 1 {
			keys := cmdInfo.Args[0].([]string)
			pipeline.Del(ctx, keys...)
		}
	case "INCR":
		if len(cmdInfo.Args) >= 1 {
			key := cmdInfo.Args[0].(string)
			pipeline.Incr(ctx, key)
		}
	case "HSET":
		if len(cmdInfo.Args) >= 2 {
			key := cmdInfo.Args[0].(string)
			values := cmdInfo.Args[1:]
			pipeline.HSet(ctx, key, values...)
		}
	case "HGET":
		if len(cmdInfo.Args) >= 2 {
			key := cmdInfo.Args[0].(string)
			field := cmdInfo.Args[1].(string)
			pipeline.HGet(ctx, key, field)
		}
	case "LPUSH":
		if len(cmdInfo.Args) >= 2 {
			key := cmdInfo.Args[0].(string)
			values := cmdInfo.Args[1:]
			pipeline.LPush(ctx, key, values...)
		}
	case "RPOP":
		if len(cmdInfo.Args) >= 1 {
			key := cmdInfo.Args[0].(string)
			pipeline.RPop(ctx, key)
		}
	case "SADD":
		if len(cmdInfo.Args) >= 2 {
			key := cmdInfo.Args[0].(string)
			members := cmdInfo.Args[1:]
			pipeline.SAdd(ctx, key, members...)
		}
	case "ZADD":
		if len(cmdInfo.Args) >= 2 {
			key := cmdInfo.Args[0].(string)
			members := cmdInfo.Args[1].([]redis.Z)
			pipeline.ZAdd(ctx, key, members...)
		}
	default:
		return fmt.Errorf("unsupported command: %s", cmdInfo.Name)
	}
	return nil
}

// Len 返回Pipeline中的命令数量
func (p *PipelineWrapper) Len() int {
	return len(p.commands)
}

// Clear 清空Pipeline
func (p *PipelineWrapper) Clear() {
	p.commands = p.commands[:0]
	p.commandInfo = p.commandInfo[:0]
	// 重新创建pipeline
	p.pipeline = p.client.Pipeline()
}

// BatchOperation 批量操作接口
type BatchOperation interface {
	Execute(ctx context.Context, pipeline *PipelineWrapper) error
}

// SetBatchOperation SET批量操作
type SetBatchOperation struct {
	Operations []SetOperation
}

type SetOperation struct {
	Key        string
	Value      interface{}
	Expiration time.Duration
}

func (op *SetBatchOperation) Execute(ctx context.Context, pipeline *PipelineWrapper) error {
	for _, setOp := range op.Operations {
		pipeline.Set(ctx, setOp.Key, setOp.Value, setOp.Expiration)
	}
	return nil
}

// GetBatchOperation GET批量操作
type GetBatchOperation struct {
	Keys []string
}

func (op *GetBatchOperation) Execute(ctx context.Context, pipeline *PipelineWrapper) error {
	for _, key := range op.Keys {
		pipeline.Get(ctx, key)
	}
	return nil
}

// ExecuteBatchOperations 执行批量操作
func (c *Client) ExecuteBatchOperations(ctx context.Context, operations []BatchOperation) error {
	pipeline := c.NewPipeline()

	// 执行所有操作
	for _, op := range operations {
		if err := op.Execute(ctx, pipeline); err != nil {
			return fmt.Errorf("failed to execute batch operation: %w", err)
		}
	}

	// 执行Pipeline
	_, err := pipeline.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to execute pipeline: %w", err)
	}

	return nil
}

// BenchmarkResult Pipeline性能测试结果
type BenchmarkResult struct {
	Operation     string        `json:"operation"`
	BatchSize     int           `json:"batch_size"`
	TotalOps      int           `json:"total_ops"`
	Duration      time.Duration `json:"duration"`
	OpsPerSecond  float64       `json:"ops_per_second"`
	AvgLatencyMS  float64       `json:"avg_latency_ms"`
	PipelineCount int           `json:"pipeline_count"`
}

// BenchmarkPipeline Pipeline性能基准测试
func (c *Client) BenchmarkPipeline(ctx context.Context, operation string, totalOps int, batchSize int) (*BenchmarkResult, error) {
	start := time.Now()
	pipelineCount := 0

	// 重置指标
	if c.metrics != nil {
		c.metrics.Reset()
	}

	// 为GET操作预设数据
	if operation == "GET" {
		// 预设测试数据
		setupPipeline := c.NewPipeline().SetBatchSize(500)
		for i := 0; i < totalOps; i++ {
			key := fmt.Sprintf("bench:get:%d", i)
			value := fmt.Sprintf("value_%d", i)
			setupPipeline.Set(ctx, key, value, time.Hour)
		}
		_, err := setupPipeline.Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to setup GET test data: %w", err)
		}
	}

	for i := 0; i < totalOps; i += batchSize {
		pipeline := c.NewPipeline().SetBatchSize(batchSize)

		currentBatch := batchSize
		if i+batchSize > totalOps {
			currentBatch = totalOps - i
		}

		// 根据操作类型生成命令
		switch operation {
		case "SET":
			for j := 0; j < currentBatch; j++ {
				key := fmt.Sprintf("bench:set:%d", i+j)
				value := fmt.Sprintf("value_%d", i+j)
				pipeline.Set(ctx, key, value, time.Hour)
			}
		case "GET":
			for j := 0; j < currentBatch; j++ {
				key := fmt.Sprintf("bench:get:%d", i+j)
				pipeline.Get(ctx, key)
			}
		case "INCR":
			for j := 0; j < currentBatch; j++ {
				key := fmt.Sprintf("bench:incr:%d", i+j)
				pipeline.Incr(ctx, key)
			}
		default:
			return nil, fmt.Errorf("unsupported operation: %s", operation)
		}

		cmds, err := pipeline.Exec(ctx)
		if err != nil {
			return nil, fmt.Errorf("pipeline execution failed: %w", err)
		}

		// 对于GET操作，检查命令结果，redis.Nil不算错误
		if operation == "GET" {
			for _, cmd := range cmds {
				if cmd.Err() != nil && cmd.Err() != redis.Nil {
					return nil, fmt.Errorf("unexpected GET error: %w", cmd.Err())
				}
			}
		}

		pipelineCount++
	}

	duration := time.Since(start)
	opsPerSecond := float64(totalOps) / duration.Seconds()
	avgLatencyMS := duration.Seconds() * 1000 / float64(pipelineCount)

	return &BenchmarkResult{
		Operation:     operation,
		BatchSize:     batchSize,
		TotalOps:      totalOps,
		Duration:      duration,
		OpsPerSecond:  opsPerSecond,
		AvgLatencyMS:  avgLatencyMS,
		PipelineCount: pipelineCount,
	}, nil
}
