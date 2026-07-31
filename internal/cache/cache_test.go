package cache

import (
	"context"
	"testing"
)

// TestDisabledCacheGetURL verifies a disabled cache always reports a miss.
// This is the graceful-degradation contract: when Redis is unavailable,
// the service must fall back to PostgreSQL without error.
func TestDisabledCacheGetURL(t *testing.T) {
	c := New("") // empty addr → disabled
	url, ok := c.GetURL(context.Background(), "abc123")
	if ok {
		t.Fatalf("disabled cache GetURL returned hit: url=%q", url)
	}
	if url != "" {
		t.Fatalf("disabled cache GetURL returned non-empty url: %q", url)
	}
}

// TestDisabledCacheSetURL verifies a disabled cache SetURL is a no-op.
// It must not panic or error.
func TestDisabledCacheSetURL(t *testing.T) {
	c := New("")
	c.SetURL(context.Background(), "abc123", "https://example.com")
	// If we reach here without panicking, the test passes.
}

// TestDisabledCachePing verifies a disabled cache reports healthy.
// The service should not log a warning when the cache is intentionally off.
func TestDisabledCachePing(t *testing.T) {
	c := New("")
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("disabled cache Ping returned error: %v", err)
	}
}
