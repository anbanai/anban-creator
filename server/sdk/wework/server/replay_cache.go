package server

import (
	"strings"
	"sync"
	"time"
)

const callbackTimestampWindow = 5 * time.Minute

type callbackReplayCache struct {
	mu      sync.Mutex
	entries map[string]callbackReplayEntry
	nextID  uint64
}

type callbackReplayEntry struct {
	expiresAt time.Time
	claimID   uint64
}

type callbackReplayClaim struct {
	keys    []string
	claimID uint64
}

func newCallbackReplayCache() *callbackReplayCache {
	return &callbackReplayCache{entries: make(map[string]callbackReplayEntry)}
}

// claim atomically reserves all keys until the callback timestamp window expires.
// A duplicate key leaves the cache unchanged so a failed handler can be retried
// by the provider without permanently consuming the event.
func (c *callbackReplayCache) claim(keys []string, now time.Time) (callbackReplayClaim, bool) {
	if len(keys) == 0 {
		return callbackReplayClaim{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.entries {
		if !entry.expiresAt.After(now) {
			delete(c.entries, key)
		}
	}
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if entry, ok := c.entries[key]; ok && entry.expiresAt.After(now) {
			return callbackReplayClaim{}, false
		}
	}
	c.nextID++
	claim := callbackReplayClaim{keys: append([]string(nil), keys...), claimID: c.nextID}
	expiresAt := now.Add(callbackTimestampWindow)
	for _, key := range keys {
		if strings.TrimSpace(key) != "" {
			c.entries[key] = callbackReplayEntry{expiresAt: expiresAt, claimID: claim.claimID}
		}
	}
	return claim, true
}

func (c *callbackReplayCache) release(claim callbackReplayClaim) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range claim.keys {
		if entry, ok := c.entries[key]; ok && entry.claimID == claim.claimID {
			delete(c.entries, key)
		}
	}
}
