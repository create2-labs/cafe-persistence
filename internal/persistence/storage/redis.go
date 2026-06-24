package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"cafe-persistence/internal/domain"
	redisconn "cafe-persistence/pkg/redis"

	"github.com/google/uuid"
)

const (
	tlsKeyPrefix     = "tls:user:"
	walletKeyPrefix  = "wallet:user:"
	scanResultTTL    = 30 * time.Minute
)

// RedisCache writes scan results for backend read (write-through from persistence).
type RedisCache struct {
	redis redisconn.Connection
}

func NewRedisCache(redis redisconn.Connection) *RedisCache {
	return &RedisCache{redis: redis}
}

// SaveTLSScan writes TLS result for user+url and sets TTL.
func (c *RedisCache) SaveTLSScan(ctx context.Context, userID uuid.UUID, url string, result *domain.TLSScanResult) error {
	key := tlsKeyPrefix + userID.String() + ":" + url
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal tls result: %w", err)
	}
	if err := c.redis.Set(ctx, key, data, scanResultTTL).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// SaveWalletScan writes wallet result for user+address and sets TTL.
func (c *RedisCache) SaveWalletScan(ctx context.Context, userID uuid.UUID, address string, result *domain.ScanResult) error {
	key := walletKeyPrefix + userID.String() + ":" + address
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal wallet result: %w", err)
	}
	if err := c.redis.Set(ctx, key, data, scanResultTTL).Err(); err != nil {
		return fmt.Errorf("redis set: %w", err)
	}
	return nil
}

// SaveTLSFailure writes a minimal failure state for TLS so backend can return NOT_READY/404.
func (c *RedisCache) SaveTLSFailure(ctx context.Context, userID uuid.UUID, url, errMsg string) error {
	key := tlsKeyPrefix + userID.String() + ":" + url
	// Store minimal JSON so backend can detect failure
	payload := map[string]interface{}{"status": "FAILED", "error": errMsg}
	data, _ := json.Marshal(payload)
	return c.redis.Set(ctx, key, data, scanResultTTL).Err()
}

// SaveWalletFailure writes a minimal failure state for wallet.
func (c *RedisCache) SaveWalletFailure(ctx context.Context, userID uuid.UUID, address, errMsg string) error {
	key := walletKeyPrefix + userID.String() + ":" + address
	payload := map[string]interface{}{"status": "FAILED", "error": errMsg}
	data, _ := json.Marshal(payload)
	return c.redis.Set(ctx, key, data, scanResultTTL).Err()
}
