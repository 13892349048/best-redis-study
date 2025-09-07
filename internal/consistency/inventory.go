package consistency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrInsufficientStock = errors.New("insufficient stock")
	ErrStockNotFound     = errors.New("stock not found")
	ErrInvalidAmount     = errors.New("invalid amount")
)

// InventoryItem 库存项
type InventoryItem struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Stock    int64     `json:"stock"`
	Reserved int64     `json:"reserved"`
	Price    float64   `json:"price"`
	UpdateAt time.Time `json:"update_at"`
}

// InventoryManager 库存管理器接口
type InventoryManager interface {
	// GetStock 获取库存信息
	GetStock(ctx context.Context, itemID string) (*InventoryItem, error)

	// DeductStock 扣减库存（带缓存更新）
	DeductStock(ctx context.Context, itemID string, amount int64) (*InventoryItem, error)

	// ReserveStock 预留库存
	ReserveStock(ctx context.Context, itemID string, amount int64, reservationID string) error

	// ReleaseReservation 释放预留
	ReleaseReservation(ctx context.Context, itemID string, amount int64, reservationID string) error

	// ConfirmReservation 确认预留（扣减库存）
	ConfirmReservation(ctx context.Context, itemID string, amount int64, reservationID string) (*InventoryItem, error)

	// BatchDeductStock 批量扣减库存
	BatchDeductStock(ctx context.Context, items []DeductionItem) ([]DeductionResult, error)

	// RefreshCache 刷新缓存
	RefreshCache(ctx context.Context, itemID string) error
}

// DeductionItem 扣减项
type DeductionItem struct {
	ItemID string `json:"item_id"`
	Amount int64  `json:"amount"`
}

// DeductionResult 扣减结果
type DeductionResult struct {
	ItemID  string         `json:"item_id"`
	Success bool           `json:"success"`
	Error   string         `json:"error,omitempty"`
	Item    *InventoryItem `json:"item,omitempty"`
}

// ReservationInfo 预留信息
type ReservationInfo struct {
	ReservationID string    `json:"reservation_id"`
	ItemID        string    `json:"item_id"`
	Amount        int64     `json:"amount"`
	ExpiresAt     time.Time `json:"expires_at"`
	CreatedAt     time.Time `json:"created_at"`
}

// redisInventoryManager Redis库存管理器实现
type redisInventoryManager struct {
	client            *redis.Client
	consistencyMgr    ConsistencyManager
	stockKeyPrefix    string
	cacheKeyPrefix    string
	reservationPrefix string
	cacheTTL          time.Duration
	reservationTTL    time.Duration
}

// InventoryConfig 库存管理器配置
type InventoryConfig struct {
	StockKeyPrefix    string
	CacheKeyPrefix    string
	ReservationPrefix string
	CacheTTL          time.Duration
	ReservationTTL    time.Duration
}

// NewInventoryManager 创建库存管理器
func NewInventoryManager(client *redis.Client, consistencyMgr ConsistencyManager, config InventoryConfig) InventoryManager {
	if config.StockKeyPrefix == "" {
		config.StockKeyPrefix = "stock:"
	}
	if config.CacheKeyPrefix == "" {
		config.CacheKeyPrefix = "inventory_cache:"
	}
	if config.ReservationPrefix == "" {
		config.ReservationPrefix = "reservation:"
	}
	if config.CacheTTL == 0 {
		config.CacheTTL = time.Minute * 30
	}
	if config.ReservationTTL == 0 {
		config.ReservationTTL = time.Minute * 15
	}

	return &redisInventoryManager{
		client:            client,
		consistencyMgr:    consistencyMgr,
		stockKeyPrefix:    config.StockKeyPrefix,
		cacheKeyPrefix:    config.CacheKeyPrefix,
		reservationPrefix: config.ReservationPrefix,
		cacheTTL:          config.CacheTTL,
		reservationTTL:    config.ReservationTTL,
	}
}

// GetStock 获取库存信息
func (m *redisInventoryManager) GetStock(ctx context.Context, itemID string) (*InventoryItem, error) {
	cacheKey := m.cacheKeyPrefix + itemID

	// 先从缓存获取
	cached, err := m.client.Get(ctx, cacheKey).Result()
	if err == nil {
		var item InventoryItem
		if err := json.Unmarshal([]byte(cached), &item); err == nil {
			return &item, nil
		}
	}

	// 缓存未命中，从数据库获取（这里模拟从数据库获取）
	item, err := m.getStockFromDB(ctx, itemID)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	itemJSON, _ := json.Marshal(item)
	m.client.SetEx(ctx, cacheKey, string(itemJSON), m.cacheTTL)

	return item, nil
}

// DeductStock 扣减库存（带缓存更新）
func (m *redisInventoryManager) DeductStock(ctx context.Context, itemID string, amount int64) (*InventoryItem, error) {
	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	stockKey := m.stockKeyPrefix + itemID
	cacheKey := m.cacheKeyPrefix + itemID

	// 使用WATCH机制确保一致性
	operation := func(ctx context.Context, tx TxContext) error {
		// 检查当前库存
		stockStr, err := tx.Get(stockKey)
		if err != nil {
			if err == redis.Nil {
				return ErrStockNotFound
			}
			return err
		}

		currentStock, err := strconv.ParseInt(stockStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid stock value: %v", err)
		}

		if currentStock < amount {
			return ErrInsufficientStock
		}

		// 扣减库存
		newStock := currentStock - amount
		err = tx.Set(stockKey, strconv.FormatInt(newStock, 10), 0)
		if err != nil {
			return err
		}

		// 更新缓存中的库存信息
		item := &InventoryItem{
			ID:       itemID,
			Stock:    newStock,
			UpdateAt: time.Now(),
		}

		// 从缓存或数据库获取完整信息
		if cachedItem, err := m.getCachedItem(ctx, cacheKey); err == nil {
			item.Name = cachedItem.Name
			item.Reserved = cachedItem.Reserved
			item.Price = cachedItem.Price
		}

		itemJSON, _ := json.Marshal(item)
		return tx.Set(cacheKey, string(itemJSON), m.cacheTTL)
	}

	err := m.consistencyMgr.ExecuteWithWatch(ctx, []string{stockKey}, operation,
		WithRetryPolicy(RetryPolicy{
			MaxRetries:    3,
			InitialDelay:  time.Millisecond * 10,
			MaxDelay:      time.Millisecond * 100,
			BackoffFactor: 2.0,
		}),
	)

	if err != nil {
		return nil, err
	}

	// 返回更新后的库存信息
	return m.GetStock(ctx, itemID)
}

// ReserveStock 预留库存
func (m *redisInventoryManager) ReserveStock(ctx context.Context, itemID string, amount int64, reservationID string) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}

	stockKey := m.stockKeyPrefix + itemID
	reservationKey := m.reservationPrefix + reservationID

	operation := func(ctx context.Context, tx TxContext) error {
		// 检查库存
		stockStr, err := tx.Get(stockKey)
		if err != nil {
			if err == redis.Nil {
				return ErrStockNotFound
			}
			return err
		}

		currentStock, err := strconv.ParseInt(stockStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid stock value: %v", err)
		}

		if currentStock < amount {
			return ErrInsufficientStock
		}

		// 创建预留记录
		reservation := ReservationInfo{
			ReservationID: reservationID,
			ItemID:        itemID,
			Amount:        amount,
			ExpiresAt:     time.Now().Add(m.reservationTTL),
			CreatedAt:     time.Now(),
		}

		reservationJSON, _ := json.Marshal(reservation)
		return tx.Set(reservationKey, string(reservationJSON), m.reservationTTL)
	}

	return m.consistencyMgr.ExecuteWithWatch(ctx, []string{stockKey}, operation)
}

// ReleaseReservation 释放预留
func (m *redisInventoryManager) ReleaseReservation(ctx context.Context, itemID string, amount int64, reservationID string) error {
	reservationKey := m.reservationPrefix + reservationID

	// 检查预留是否存在
	reservationStr, err := m.client.Get(ctx, reservationKey).Result()
	if err != nil {
		if err == redis.Nil {
			return errors.New("reservation not found")
		}
		return err
	}

	var reservation ReservationInfo
	if err := json.Unmarshal([]byte(reservationStr), &reservation); err != nil {
		return fmt.Errorf("invalid reservation data: %v", err)
	}

	// 验证预留信息
	if reservation.ItemID != itemID || reservation.Amount != amount {
		return errors.New("reservation mismatch")
	}

	// 删除预留记录
	return m.client.Del(ctx, reservationKey).Err()
}

// ConfirmReservation 确认预留（扣减库存）
func (m *redisInventoryManager) ConfirmReservation(ctx context.Context, itemID string, amount int64, reservationID string) (*InventoryItem, error) {
	reservationKey := m.reservationPrefix + reservationID

	// 检查预留
	reservationStr, err := m.client.Get(ctx, reservationKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, errors.New("reservation not found")
		}
		return nil, err
	}

	var reservation ReservationInfo
	if err := json.Unmarshal([]byte(reservationStr), &reservation); err != nil {
		return nil, fmt.Errorf("invalid reservation data: %v", err)
	}

	// 验证预留信息
	if reservation.ItemID != itemID || reservation.Amount != amount {
		return nil, errors.New("reservation mismatch")
	}

	// 检查预留是否过期
	if time.Now().After(reservation.ExpiresAt) {
		m.client.Del(ctx, reservationKey) // 清理过期预留
		return nil, errors.New("reservation expired")
	}

	// 扣减库存并删除预留
	stockKey := m.stockKeyPrefix + itemID

	operation := func(ctx context.Context, tx TxContext) error {
		// 扣减库存
		_, err := tx.Decr(stockKey)
		if err != nil {
			return err
		}

		// 删除预留记录
		return tx.Del(reservationKey)
	}

	err = m.consistencyMgr.ExecuteWithWatch(ctx, []string{stockKey, reservationKey}, operation)
	if err != nil {
		return nil, err
	}

	// 刷新缓存
	m.RefreshCache(ctx, itemID)

	return m.GetStock(ctx, itemID)
}

// BatchDeductStock 批量扣减库存
func (m *redisInventoryManager) BatchDeductStock(ctx context.Context, items []DeductionItem) ([]DeductionResult, error) {
	results := make([]DeductionResult, len(items))

	for i, item := range items {
		result := DeductionResult{
			ItemID: item.ItemID,
		}

		deductedItem, err := m.DeductStock(ctx, item.ItemID, item.Amount)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = true
			result.Item = deductedItem
		}

		results[i] = result
	}

	return results, nil
}

// RefreshCache 刷新缓存
func (m *redisInventoryManager) RefreshCache(ctx context.Context, itemID string) error {
	cacheKey := m.cacheKeyPrefix + itemID

	// 删除旧缓存
	m.client.Del(ctx, cacheKey)

	// 重新获取并缓存
	_, err := m.GetStock(ctx, itemID)
	return err
}

// getStockFromDB 模拟从数据库获取库存信息
func (m *redisInventoryManager) getStockFromDB(ctx context.Context, itemID string) (*InventoryItem, error) {
	stockKey := m.stockKeyPrefix + itemID

	// 从Redis获取库存数量
	stockStr, err := m.client.Get(ctx, stockKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, ErrStockNotFound
		}
		return nil, err
	}

	stock, err := strconv.ParseInt(stockStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid stock value: %v", err)
	}

	// 模拟从数据库获取其他信息
	return &InventoryItem{
		ID:       itemID,
		Name:     fmt.Sprintf("Product %s", itemID),
		Stock:    stock,
		Reserved: 0,
		Price:    99.99,
		UpdateAt: time.Now(),
	}, nil
}

// getCachedItem 获取缓存中的物品信息
func (m *redisInventoryManager) getCachedItem(ctx context.Context, cacheKey string) (*InventoryItem, error) {
	cached, err := m.client.Get(ctx, cacheKey).Result()
	if err != nil {
		return nil, err
	}

	var item InventoryItem
	if err := json.Unmarshal([]byte(cached), &item); err != nil {
		return nil, err
	}

	return &item, nil
}
