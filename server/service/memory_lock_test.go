package service

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

func TestRedisMemoryLockerReleaseDoesNotDeleteRenewedLock(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	locker := NewRedisMemoryLocker(rdb, &logger)
	ctx := context.Background()

	releaseFirst, ok, err := locker.TryLock(ctx, "memory:project:test", time.Second)
	if err != nil {
		t.Fatalf("first TryLock: %v", err)
	}
	if !ok {
		t.Fatal("first lock not acquired")
	}

	mr.FastForward(2 * time.Second)

	releaseSecond, ok, err := locker.TryLock(ctx, "memory:project:test", time.Second)
	if err != nil {
		t.Fatalf("second TryLock: %v", err)
	}
	if !ok {
		t.Fatal("second lock not acquired after TTL")
	}

	releaseFirst()
	if _, ok, err := locker.TryLock(ctx, "memory:project:test", time.Second); err != nil {
		t.Fatalf("third TryLock: %v", err)
	} else if ok {
		t.Fatal("stale release deleted the renewed lock")
	}

	releaseSecond()
	if _, ok, err := locker.TryLock(ctx, "memory:project:test", time.Second); err != nil {
		t.Fatalf("fourth TryLock: %v", err)
	} else if !ok {
		t.Fatal("current owner release did not free the lock")
	}
}
