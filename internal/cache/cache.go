// Package cache provides a read-through Redis cache for URL lookups.
// Only the API uses the cache; the worker does not need it.
package cache

import (
	"context"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// cacheTTL is how long a short→long mapping lives in Redis.
const cacheTTL = 5 * time.Minute

// Cache is a read-through Redis cache for URL lookups.
// A Cache with a nil client is disabled: GetURL always reports a miss,
// and SetURL is a no-op. This lets the service run with the cache off
// and degrade gracefully when Redis is unavailable.
type Cache struct {
	client *redis.Client
}

// New makes a Redis cache at addr. An empty addr makes a disabled cache.
func New(addr string) *Cache {
	if addr == "" {
		return &Cache{}
	}
	return &Cache{client: redis.NewClient(&redis.Options{Addr: addr})}
}

// Ping checks the Redis connection. It is safe to call on a disabled cache.
func (c *Cache) Ping(ctx context.Context) error {
	if c.client == nil {
		return nil
	}
	return c.client.Ping(ctx).Err()
}

// GetURL returns the long URL stored for code, or ("", false) on a cache miss.
// Any Redis error is logged and reported as a miss, so the service falls back
// to PostgreSQL when Redis is unavailable.
func (c *Cache) GetURL(ctx context.Context, code string) (string, bool) {
	if c.client == nil {
		return "", false
	}
	url, err := c.client.Get(ctx, "url:"+code).Result()
	if err == redis.Nil {
		return "", false
	}
	if err != nil {
		log.Printf("cache get %s: %v", code, err)
		return "", false
	}
	return url, true
}

// SetURL stores the long URL for code with the cache TTL.
// Errors are logged, not returned, because a failed set must not fail the redirect.
func (c *Cache) SetURL(ctx context.Context, code, url string) {
	if c.client == nil {
		return
	}
	if err := c.client.Set(ctx, "url:"+code, url, cacheTTL).Err(); err != nil {
		log.Printf("cache set %s: %v", code, err)
	}
}
