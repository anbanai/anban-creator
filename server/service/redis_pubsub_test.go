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

func newFallbackClaimTestPubSub(t *testing.T) (*RedisPubSub, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	return NewRedisPubSub(rdb, &logger), mr
}

func TestRedisPubSubFallbackClaimAllowsOnlyOneOwnerPerTask(t *testing.T) {
	pubsub, _ := newFallbackClaimTestPubSub(t)
	ctx := context.Background()

	claimed, err := pubsub.TryClaimFallback(ctx, "task-1", "token-a", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("first claim = %v, %v; want true, nil", claimed, err)
	}
	claimed, err = pubsub.TryClaimFallback(ctx, "task-1", "token-b", time.Minute)
	if err != nil || claimed {
		t.Fatalf("second claim = %v, %v; want false, nil", claimed, err)
	}
}

func TestRedisPubSubFallbackClaimReleaseRequiresCurrentToken(t *testing.T) {
	pubsub, _ := newFallbackClaimTestPubSub(t)
	ctx := context.Background()

	claimed, err := pubsub.TryClaimFallback(ctx, "task-1", "token-a", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim A = %v, %v", claimed, err)
	}
	released, err := pubsub.ReleaseFallbackClaim(ctx, "task-1", "token-a")
	if err != nil || !released {
		t.Fatalf("release A = %v, %v", released, err)
	}
	claimed, err = pubsub.TryClaimFallback(ctx, "task-1", "token-b", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim B = %v, %v", claimed, err)
	}
	released, err = pubsub.ReleaseFallbackClaim(ctx, "task-1", "token-a")
	if err != nil || released {
		t.Fatalf("stale release A = %v, %v; want false, nil", released, err)
	}
	claimed, err = pubsub.TryClaimFallback(ctx, "task-1", "token-c", time.Minute)
	if err != nil || claimed {
		t.Fatalf("claim C while B owns claim = %v, %v; want false, nil", claimed, err)
	}
	released, err = pubsub.ReleaseFallbackClaim(ctx, "task-1", "token-b")
	if err != nil || !released {
		t.Fatalf("release B = %v, %v", released, err)
	}
}

func TestRedisPubSubFallbackClaimExpires(t *testing.T) {
	pubsub, mr := newFallbackClaimTestPubSub(t)
	ctx := context.Background()

	claimed, err := pubsub.TryClaimFallback(ctx, "task-1", "token-a", time.Second)
	if err != nil || !claimed {
		t.Fatalf("claim A = %v, %v", claimed, err)
	}
	mr.FastForward(time.Second)
	claimed, err = pubsub.TryClaimFallback(ctx, "task-1", "token-b", time.Minute)
	if err != nil || !claimed {
		t.Fatalf("claim B after expiry = %v, %v; want true, nil", claimed, err)
	}
}
