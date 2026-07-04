package service

import (
	"context"
	"time"

	"github.com/google/uuid"
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
	token := uuid.NewString()
	ok, err := l.rdb.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}
	return func() {
		const releaseScript = `if redis.call("GET", KEYS[1]) == ARGV[1] then return redis.call("DEL", KEYS[1]) else return 0 end`
		if err := l.rdb.Eval(context.Background(), releaseScript, []string{key}, token).Err(); err != nil && l.logger != nil {
			l.logger.Warn().Err(err).Str("key", key).Msg("failed to release project memory lock")
		}
	}, true, nil
}
