// Package cache wraps the Redis client with the configured key prefix.
package cache

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/junler/intent-admin/internal/config"
)

// Cache is the prefixed Redis client.
//
// Every key goes through Key(), so a single Redis instance can host several
// deployments and the cache-monitor whitelist has a stable prefix to match on.
type Cache struct {
	*redis.Client
	prefix string
}

// OpenRedis connects to Redis and verifies reachability.
func Open(cfg *config.Config) (*Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("Redis 不可用: %w", err)
	}
	return &Cache{Client: client, prefix: cfg.Redis.KeyPrefix}, nil
}

// Key applies the configured prefix.
func (c *Cache) Key(parts ...string) string {
	key := c.prefix
	for i, p := range parts {
		if i > 0 {
			key += ":"
		}
		key += p
	}
	return key
}

// Prefix returns the configured key prefix.
func (c *Cache) Prefix() string { return c.prefix }

// Close releases the connection pool.
func (c *Cache) Close() {
	if c == nil || c.Client == nil {
		return
	}
	if err := c.Client.Close(); err != nil {
		slog.Warn("关闭 Redis 连接失败", "err", err)
	}
}
