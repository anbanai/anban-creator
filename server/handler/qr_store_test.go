package handler

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// runStoreLifecycleTests exercises the QR state-machine contract against any
// QRStateStore implementation: both the in-memory fallback and the Redis store
// must satisfy it identically.
func runStoreLifecycleTests(t *testing.T, s QRStateStore) {
	t.Helper()
	ctx := context.Background()

	// Fresh scene is created as pending.
	if err := s.Create(ctx, "s1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if status, ok, err := s.Get(ctx, "s1"); err != nil || !ok || status != qrStatusPending {
		t.Fatalf("Get after Create = (%q,%v,%v), want (pending,true,nil)", status, ok, err)
	}

	// pending → scanned.
	if cur, ok, err := s.CompareAndSet(ctx, "s1", qrStatusPending, qrStatusScanned); err != nil || !ok || cur != qrStatusScanned {
		t.Fatalf("CAS pending→scanned = (%q,%v,%v), want (scanned,true,nil)", cur, ok, err)
	}

	// scanned → used.
	if cur, ok, err := s.CompareAndSet(ctx, "s1", qrStatusScanned, qrStatusUsed); err != nil || !ok || cur != qrStatusUsed {
		t.Fatalf("CAS scanned→used = (%q,%v,%v), want (used,true,nil)", cur, ok, err)
	}

	// wrong-from-status CAS fails, returning the current status, no transition.
	if err := s.Create(ctx, "s2"); err != nil { // s2 pending
		t.Fatalf("Create s2: %v", err)
	}
	if cur, ok, err := s.CompareAndSet(ctx, "s2", qrStatusScanned, qrStatusUsed); err != nil || ok || cur != qrStatusPending {
		t.Fatalf("CAS scanned→used on a pending scene = (%q,%v,%v), want (pending,false,nil)", cur, ok, err)
	}

	// missing scene: CAS reports empty status and does not transition.
	if cur, ok, err := s.CompareAndSet(ctx, "missing", qrStatusPending, qrStatusScanned); err != nil || ok || cur != "" {
		t.Fatalf("CAS on missing scene = (%q,%v,%v), want (\"\",false,nil)", cur, ok, err)
	}
	if _, ok, err := s.Get(ctx, "missing"); err != nil || ok {
		t.Fatalf("Get on missing scene = (_, %v, %v), want (_,false,nil)", ok, err)
	}

	// Delete removes the scene.
	if err := s.Delete(ctx, "s1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok, _ := s.Get(ctx, "s1"); ok {
		t.Fatalf("Get after Delete should be not-found")
	}
}

func TestMemoryQRStateStore_Lifecycle(t *testing.T) {
	s := newMemoryQRStateStore(qrStateTTL, nil, &zerolog.Logger{})
	defer s.close()
	runStoreLifecycleTests(t, s)
}

func TestRedisQRStateStore_Lifecycle(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	runStoreLifecycleTests(t, newRedisQRStateStore(rdb, qrStateTTL))
}

func TestMemoryQRStateStore_Expiry(t *testing.T) {
	clock := time.Now()
	s := newMemoryQRStateStore(qrStateTTL, func() time.Time { return clock }, &zerolog.Logger{})
	defer s.close()
	ctx := context.Background()

	if err := s.Create(ctx, "s1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Advancing the injected clock past the TTL expires the entry lazily.
	clock = clock.Add(qrStateTTL + time.Second)
	if _, ok, err := s.Get(ctx, "s1"); err != nil || ok {
		t.Fatalf("Get after TTL expiry = (_, %v, %v), want not-found", ok, err)
	}
	// CAS on an expired entry behaves as missing (status "", no transition).
	if cur, ok, err := s.CompareAndSet(ctx, "s1", qrStatusPending, qrStatusScanned); err != nil || ok || cur != "" {
		t.Fatalf("CAS on expired entry = (%q,%v,%v), want (\"\",false,nil)", cur, ok, err)
	}
}

func TestRedisQRStateStore_TTLExpiry(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	s := newRedisQRStateStore(rdb, qrStateTTL)

	if err := s.Create(ctx, "s1"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// Redis TTL auto-expires the key — no cleanup goroutine needed.
	mr.FastForward(qrStateTTL + time.Second)
	if _, ok, err := s.Get(ctx, "s1"); err != nil || ok {
		t.Fatalf("Get after Redis TTL = (_, %v, %v), want not-found", ok, err)
	}
}

// TestRedisQRStateStore_CASPreservesTTL verifies that a status transition does
// NOT reset the scene's TTL back to the full window (the original creation time
// must remain the expiry anchor, matching the in-memory store's CreatedAt).
func TestRedisQRStateStore_CASPreservesTTL(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	ctx := context.Background()
	s := newRedisQRStateStore(rdb, qrStateTTL)

	if err := s.Create(ctx, "s2"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	half := qrStateTTL / 2
	mr.FastForward(half) // half the TTL elapses before the scan
	if _, ok, err := s.CompareAndSet(ctx, "s2", qrStatusPending, qrStatusScanned); err != nil || !ok {
		t.Fatalf("CAS pending→scanned failed: ok=%v err=%v", ok, err)
	}
	// Fast-forward just past the remaining half. If CAS had RESET the TTL to the
	// full window, the scene would still be present here.
	mr.FastForward(half + time.Second)
	if _, ok, _ := s.Get(ctx, "s2"); ok {
		t.Fatalf("scene present after preserved-TTL expiry; CAS appears to have reset the TTL")
	}
}

func TestNewQRStateStore_Selection(t *testing.T) {
	if _, ok := newQRStateStore(nil, &zerolog.Logger{}).(*memoryQRStateStore); !ok {
		t.Fatal("nil rdb should select the in-memory store (degraded-mode fallback)")
	}

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	if _, ok := newQRStateStore(rdb, &zerolog.Logger{}).(*redisQRStateStore); !ok {
		t.Fatal("non-nil rdb should select the Redis store")
	}
}
