package service

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	projectmemory "github.com/anbanai/anban-creator/server/memory"
)

type redisMemoryLocker struct {
	rdb    *redis.Client
	logger *zerolog.Logger
}

func NewRedisMemoryLocker(rdb *redis.Client, logger *zerolog.Logger) projectmemory.Locker {
	if rdb == nil {
		return nil
	}
	return &redisMemoryLocker{rdb: rdb, logger: logger}
}

func (l *redisMemoryLocker) TryLock(ctx context.Context, key string, ttl time.Duration) (func(), bool, error) {
	ok, err := l.rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return func() {
		if err := l.rdb.Del(context.Background(), key).Err(); err != nil && l.logger != nil {
			l.logger.Warn().Err(err).Str("key", key).Msg("failed to release project memory lock")
		}
	}, true, nil
}
