package redis

import (
	"cafe-persistence/internal/config"
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

// Connection wraps Redis client
type Connection interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	SetArgs(ctx context.Context, key string, value interface{}, args redis.SetArgs) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd
	Keys(ctx context.Context, pattern string) *redis.StringSliceCmd
	Close() error
	Ping(ctx context.Context) *redis.StatusCmd
}

type redisConnection struct {
	client *redis.Client
}

// New creates a new Redis connection
func New() (Connection, error) {
	redisURL := viper.GetString(config.RedisURL)
	if redisURL == "" {
		redisURL = "redis://localhost:6379"
	}

	log.Info().Str("url", redisURL).Msg("Connecting to Redis")

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opt)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	log.Info().Msg("Connected to Redis")

	return &redisConnection{client: client}, nil
}

func (rc *redisConnection) Get(ctx context.Context, key string) *redis.StringCmd {
	return rc.client.Get(ctx, key)
}

func (rc *redisConnection) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	return rc.client.Set(ctx, key, value, expiration)
}

func (rc *redisConnection) SetArgs(ctx context.Context, key string, value interface{}, args redis.SetArgs) *redis.StatusCmd {
	return rc.client.SetArgs(ctx, key, value, args)
}

func (rc *redisConnection) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	return rc.client.Del(ctx, keys...)
}

func (rc *redisConnection) Eval(ctx context.Context, script string, keys []string, args ...interface{}) *redis.Cmd {
	return rc.client.Eval(ctx, script, keys, args...)
}

func (rc *redisConnection) Keys(ctx context.Context, pattern string) *redis.StringSliceCmd {
	return rc.client.Keys(ctx, pattern)
}

func (rc *redisConnection) Close() error {
	if rc.client != nil {
		return rc.client.Close()
	}
	return nil
}

func (rc *redisConnection) Ping(ctx context.Context) *redis.StatusCmd {
	return rc.client.Ping(ctx)
}
