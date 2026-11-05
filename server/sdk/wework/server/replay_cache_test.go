package server

import (
	"testing"
	"time"
)

func TestCallbackReplayReleaseDoesNotDeleteReclaimedKeys(t *testing.T) {
	cache := newCallbackReplayCache()
	keys := []string{"callback:signature\x00nonce"}
	start := time.Unix(100, 0)
	first, ok := cache.claim(keys, start)
	if !ok {
		t.Fatal("first claim failed")
	}
	second, ok := cache.claim(keys, start.Add(callbackTimestampWindow+time.Second))
	if !ok {
		t.Fatal("reclaimed claim failed")
	}

	cache.release(first)
	if _, ok := cache.claim(keys, start.Add(callbackTimestampWindow+2*time.Second)); ok {
		t.Fatal("old release removed the newer claim")
	}

	cache.release(second)
	if _, ok := cache.claim(keys, start.Add(callbackTimestampWindow+3*time.Second)); !ok {
		t.Fatal("current claim was not released")
	}
}
