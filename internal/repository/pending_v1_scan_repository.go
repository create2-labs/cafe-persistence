package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appredis "cafe-persistence/pkg/redis"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
)

const pendingV1ScanTTL = 48 * time.Hour

const redisCompareDeleteScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`

// PendingV1ScanRecord is a scan accepted on POST before persistence rows exist.
type PendingV1ScanRecord struct {
	ScanID    uuid.UUID `json:"scan_id"`
	UserID    uuid.UUID `json:"user_id"`
	Family    string    `json:"family"` // wallet | tls
	Address   string    `json:"address,omitempty"`
	Endpoint  string    `json:"endpoint,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// PendingV1ScanRepository stores short-lived correlation for requested-only scans (Redis).
type PendingV1ScanRepository interface {
	Put(ctx context.Context, rec *PendingV1ScanRecord) error
	PutWallet(ctx context.Context, rec *PendingV1ScanRecord) (reserved bool, err error)
	Get(ctx context.Context, scanID uuid.UUID) (*PendingV1ScanRecord, error)
	GetWalletByOwnerAddress(ctx context.Context, userID uuid.UUID, address string) (*PendingV1ScanRecord, error)
	Delete(ctx context.Context, scanID uuid.UUID) error
	DeleteWalletReservation(ctx context.Context, userID uuid.UUID, address string, scanID uuid.UUID) error
}

type redisPendingV1ScanRepository struct {
	redis appredis.Connection
}

func pendingV1RedisKey(scanID uuid.UUID) string {
	return "discovery:v1:pending_scan:" + scanID.String()
}

func pendingV1WalletAddressRedisKey(userID uuid.UUID, address string) string {
	return "discovery:v1:pending_wallet:" + userID.String() + ":" + strings.ToLower(strings.TrimSpace(address))
}

// NewRedisPendingV1ScanRepository builds a Redis-backed pending scan store.
func NewRedisPendingV1ScanRepository(redis appredis.Connection) (PendingV1ScanRepository, error) {
	if redis == nil {
		return nil, fmt.Errorf("redis connection is required for pending v1 scan repository")
	}
	return &redisPendingV1ScanRepository{redis: redis}, nil
}

func (r *redisPendingV1ScanRepository) Put(ctx context.Context, rec *PendingV1ScanRecord) error {
	if rec == nil {
		return fmt.Errorf("pending v1 scan record is required")
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return r.redis.Set(ctx, pendingV1RedisKey(rec.ScanID), string(b), pendingV1ScanTTL).Err()
}

func (r *redisPendingV1ScanRepository) PutWallet(ctx context.Context, rec *PendingV1ScanRecord) (bool, error) {
	if rec == nil {
		return false, fmt.Errorf("pending v1 wallet scan record is required")
	}
	if rec.ScanID == uuid.Nil || rec.UserID == uuid.Nil || strings.TrimSpace(rec.Address) == "" {
		return false, fmt.Errorf("pending v1 wallet scan record is incomplete")
	}
	rec.Family = "wallet"
	key := pendingV1WalletAddressRedisKey(rec.UserID, rec.Address)
	_, err := r.redis.SetArgs(ctx, key, rec.ScanID.String(), goredis.SetArgs{
		Mode: "NX",
		TTL:  pendingV1ScanTTL,
	}).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return false, nil
		}
		return false, fmt.Errorf("redis reserve pending v1 wallet scan: %w", err)
	}
	if err := r.Put(ctx, rec); err != nil {
		_ = r.redis.Del(ctx, key).Err()
		return false, err
	}
	return true, nil
}

func (r *redisPendingV1ScanRepository) Get(ctx context.Context, scanID uuid.UUID) (*PendingV1ScanRecord, error) {
	s, err := r.redis.Get(ctx, pendingV1RedisKey(scanID)).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get pending v1 scan: %w", err)
	}
	if s == "" {
		return nil, nil
	}
	var rec PendingV1ScanRecord
	if err := json.Unmarshal([]byte(s), &rec); err != nil {
		return nil, fmt.Errorf("decode pending v1 scan: %w", err)
	}
	return &rec, nil
}

func (r *redisPendingV1ScanRepository) GetWalletByOwnerAddress(ctx context.Context, userID uuid.UUID, address string) (*PendingV1ScanRecord, error) {
	s, err := r.redis.Get(ctx, pendingV1WalletAddressRedisKey(userID, address)).Result()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return nil, nil
		}
		return nil, fmt.Errorf("redis get pending v1 wallet scan reservation: %w", err)
	}
	scanID, err := uuid.Parse(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("decode pending v1 wallet scan reservation: %w", err)
	}
	rec, err := r.Get(ctx, scanID)
	if err != nil {
		return nil, err
	}
	if rec != nil {
		return rec, nil
	}
	return &PendingV1ScanRecord{
		ScanID:  scanID,
		UserID:  userID,
		Family:  "wallet",
		Address: strings.ToLower(strings.TrimSpace(address)),
	}, nil
}

func (r *redisPendingV1ScanRepository) Delete(ctx context.Context, scanID uuid.UUID) error {
	rec, getErr := r.Get(ctx, scanID)
	keys := []string{pendingV1RedisKey(scanID)}
	if err := r.redis.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("redis del pending v1 scan: %w", err)
	}
	if getErr == nil && rec != nil && rec.Family == "wallet" && rec.UserID != uuid.Nil && strings.TrimSpace(rec.Address) != "" {
		if err := r.DeleteWalletReservation(ctx, rec.UserID, rec.Address, rec.ScanID); err != nil {
			return err
		}
	}
	if getErr != nil {
		return getErr
	}
	return nil
}

func (r *redisPendingV1ScanRepository) DeleteWalletReservation(ctx context.Context, userID uuid.UUID, address string, scanID uuid.UUID) error {
	if scanID == uuid.Nil {
		return nil
	}
	key := pendingV1WalletAddressRedisKey(userID, address)
	if err := r.redis.Eval(ctx, redisCompareDeleteScript, []string{key}, scanID.String()).Err(); err != nil {
		return fmt.Errorf("redis del pending v1 wallet scan reservation: %w", err)
	}
	return nil
}
