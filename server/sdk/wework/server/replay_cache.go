package server

import (
	"strings"
	"sync"
	"time"
)

const callbackTimestampWindow = 5 * time.Minute

type callbackReplayCache struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newCallbackReplayCache() *callbackReplayCache {
	return &callbackReplayCache{entries: make(map[string]time.Time)}
}

// claim atomically reserves all keys until the callback timestamp window expires.
// A duplicate key leaves the cache unchanged so a failed handler can be retried
// by the provider without permanently consuming the event.
func (c *callbackReplayCache) claim(keys []string, now time.Time) bool {
	if len(keys) == 0 {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, expiry := range c.entries {
		if !expiry.After(now) {
			delete(c.entries, key)
		}
	}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if expiry, ok := c.entries[key]; ok && expiry.After(now) {
			return false
		}
	}
	expiresAt := now.Add(callbackTimestampWindow)
	for _, key := range keys {
		if strings.TrimSpace(key) != "" {
			c.entries[key] = expiresAt
		}
	}
	return true
}

func (c *callbackReplayCache) release(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range keys {
		delete(c.entries, key)
	}
}
