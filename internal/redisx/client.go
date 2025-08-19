package redisx

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config Redis客户端配置
type Config struct {
	// 连接配置
	Addr     string `json:"addr"`     // Redis地址，如 "localhost:6379"
	Password string `json:"password"` // 密码
	DB       int    `json:"db"`       // 数据库编号

	// 连接池配置
	PoolSize        int           `json:"pool_size"`          // 连接池大小
	MinIdleConns    int           `json:"min_idle_conns"`     // 最小空闲连接数
	MaxIdleConns    int           `json:"max_idle_conns"`     // 最大空闲连接数
	ConnMaxIdleTime time.Duration `json:"conn_max_idle_time"` // 连接最大空闲时间
	ConnMaxLifetime time.Duration `json:"conn_max_lifetime"`  // 连接最大生存时间

	// 超时配置
	DialTimeout  time.Duration `json:"dial_timeout"`  // 连接超时
	ReadTimeout  time.Duration `json:"read_timeout"`  // 读超时
	WriteTimeout time.Duration `json:"write_timeout"` // 写超时

	// 重试配置
	MaxRetries      int           `json:"max_retries"`       // 最大重试次数
	MinRetryBackoff time.Duration `json:"min_retry_backoff"` // 最小重试间隔
	MaxRetryBackoff time.Duration `json:"max_retry_backoff"` // 最大重试间隔

	// TLS配置
	TLSConfig *tls.Config `json:"-"` // TLS配置

	// 其他配置
	EnableMetrics bool `json:"enable_metrics"` // 是否启用指标收集
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,

		PoolSize:        10,
		MinIdleConns:    5,
		MaxIdleConns:    10,
		ConnMaxIdleTime: 30 * time.Minute,
		ConnMaxLifetime: 1 * time.Hour,

		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,

		MaxRetries:      3,
		MinRetryBackoff: 8 * time.Millisecond,
		MaxRetryBackoff: 512 * time.Millisecond,

		EnableMetrics: true,
	}
}

// Client Redis客户端封装
type Client struct {
	*redis.Client
	config  *Config
	metrics *Metrics
}

// NewClient 创建新的Redis客户端
func NewClient(config *Config) (*Client, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// 创建Redis配置
	opts := &redis.Options{
		Addr:     config.Addr,
		Password: config.Password,
		DB:       config.DB,

		PoolSize:        config.PoolSize,
		MinIdleConns:    config.MinIdleConns,
		MaxIdleConns:    config.MaxIdleConns,
		ConnMaxIdleTime: config.ConnMaxIdleTime,
		ConnMaxLifetime: config.ConnMaxLifetime,

		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,

		MaxRetries:      config.MaxRetries,
		MinRetryBackoff: config.MinRetryBackoff,
		MaxRetryBackoff: config.MaxRetryBackoff,

		TLSConfig: config.TLSConfig,
	}

	// 创建Redis客户端
	rdb := redis.NewClient(opts)

	client := &Client{
		Client: rdb,
		config: config,
	}

	// 初始化指标
	if config.EnableMetrics {
		client.metrics = NewMetrics()
		client.addHooks()
	}

	return client, nil
}

// HealthCheck 健康检查
func (c *Client) HealthCheck(ctx context.Context) error {
	start := time.Now()
	defer func() {
		if c.metrics != nil {
			c.metrics.RecordHealthCheck(time.Since(start))
		}
	}()

	// 执行PING命令
	result := c.Ping(ctx)
	if result.Err() != nil {
		if c.metrics != nil {
			c.metrics.IncHealthCheckFailures()
		}
		return fmt.Errorf("redis health check failed: %w", result.Err())
	}

	// 检查返回值
	if result.Val() != "PONG" {
		err := fmt.Errorf("unexpected ping response: %s", result.Val())
		if c.metrics != nil {
			c.metrics.IncHealthCheckFailures()
		}
		return err
	}

	return nil
}

// GetStats 获取连接池统计信息
func (c *Client) GetStats() *redis.PoolStats {
	return c.PoolStats()
}

// GetConfig 获取配置信息
func (c *Client) GetConfig() *Config {
	return c.config
}

// GetMetrics 获取指标信息
func (c *Client) GetMetrics() *Metrics {
	return c.metrics
}

// Close 关闭客户端连接
func (c *Client) Close() error {
	log.Println("Closing Redis client...")
	return c.Client.Close()
}

// addHooks 添加钩子函数，用于指标收集
func (c *Client) addHooks() {
	c.AddHook(&MetricsHook{metrics: c.metrics})
}

// IsRetryableError 判断错误是否可重试
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// 网络错误通常可以重试
	switch err.Error() {
	case "connection refused", "connection reset by peer", "broken pipe":
		return true
	}

	// Redis specific errors
	if err == redis.TxFailedErr {
		return true
	}

	return false
}
