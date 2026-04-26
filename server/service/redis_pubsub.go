package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	// Redis channel prefixes for pub/sub.
	cancelChannelPrefix   = "anban:task:cancel:"
	progressChannelPrefix = "anban:task:progress:"

	// channelRunningCountPrefix is the Redis key prefix for per-channel running task counters.
	channelRunningCountPrefix = "anban:channel:running:"
	channelRunningCountTTL    = 1 * time.Hour
)

// reserveSlotScript is a Lua script that atomically increments a channel's running
// counter and checks if it exceeds the max. If so, decrements back and returns 0.
// Otherwise returns the new count. Sets a TTL to prevent key leaks.
var reserveSlotScript = redis.NewScript(`
local key = KEYS[1]
local max = tonumber(ARGV[1])
local ttl = tonumber(ARGV[2])
local count = redis.call('INCR', key)
if count > max then
	redis.call('DECR', key)
	return 0
end
redis.call('EXPIRE', key, ttl)
return count
`)

// CancelEvent is published to Redis when a task is cancelled, allowing
// other server replicas to propagate the cancellation to their in-process
// execution contexts.
type CancelEvent struct {
	TaskID string `json:"task_id"`
}

// ProgressEvent is published to Redis when task progress is updated,
// allowing SSE handlers on any replica to push updates to clients.
type ProgressEvent struct {
	TaskID  string `json:"task_id"`
	Message string `json:"message"`
}

// ProgressSubscriber receives progress events for a specific task.
// It is used by the SSE handler to receive real-time updates.
type ProgressSubscriber struct {
	ch      *redis.PubSub
	msgChan <-chan *redis.Message
}

// Events returns the channel of incoming progress messages.
func (s *ProgressSubscriber) Events() <-chan *redis.Message {
	return s.msgChan
}

// Close unsubscribes from the Redis channel and releases resources.
func (s *ProgressSubscriber) Close() error {
	return s.ch.Close()
}

// RedisPubSub manages Redis pub/sub for cross-replica task coordination.
type RedisPubSub struct {
	rdb    *redis.Client
	logger *zerolog.Logger
}

// NewRedisPubSub creates a new RedisPubSub. Pass nil for rdb to disable
// pub/sub (all operations become no-ops).
func NewRedisPubSub(rdb *redis.Client, logger *zerolog.Logger) *RedisPubSub {
	return &RedisPubSub{rdb: rdb, logger: logger}
}

// PublishCancel publishes a task cancellation event to Redis.
// Other replicas subscribe to this channel to cancel their in-process contexts.
func (ps *RedisPubSub) PublishCancel(ctx context.Context, taskID string) {
	if ps.rdb == nil {
		return
	}
	event := CancelEvent{TaskID: taskID}
	data, err := json.Marshal(event)
	if err != nil {
		ps.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to marshal cancel event")
		return
	}
	channel := cancelChannelPrefix + taskID
	if err := ps.rdb.Publish(ctx, channel, data).Err(); err != nil {
		ps.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to publish cancel event")
	}
}

// SubscribeCancel subscribes to cancellation events for all tasks.
// The returned channel delivers task IDs that should be cancelled.
// The caller must call the returned cancel function to stop the subscription.
func (ps *RedisPubSub) SubscribeCancel(ctx context.Context) (<-chan string, context.CancelFunc) {
	out := make(chan string, 16)

	if ps.rdb == nil {
		return out, func() { close(out) }
	}

	pattern := cancelChannelPrefix + "*"
	sub := ps.rdb.PSubscribe(ctx, pattern)

	subCtx, cancel := context.WithCancel(ctx)

	go func() {
		defer cancel()
		defer sub.Close()
		defer close(out)

		ch := sub.Channel()
		for {
			select {
			case <-subCtx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var event CancelEvent
				if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
					ps.logger.Error().Err(err).Msg("failed to unmarshal cancel event")
					continue
				}
				select {
				case out <- event.TaskID:
				case <-subCtx.Done():
					return
				}
			}
		}
	}()

	return out, cancel
}

// PublishProgress publishes a task progress event to Redis.
func (ps *RedisPubSub) PublishProgress(ctx context.Context, taskID, message string) {
	if ps.rdb == nil {
		return
	}
	event := ProgressEvent{TaskID: taskID, Message: message}
	data, err := json.Marshal(event)
	if err != nil {
		ps.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to marshal progress event")
		return
	}
	channel := progressChannelPrefix + taskID
	if err := ps.rdb.Publish(ctx, channel, data).Err(); err != nil {
		ps.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to publish progress event")
	}
}

// SubscribeProgress subscribes to progress events for a specific task.
// The caller must call Close() on the returned subscriber when done.
func (ps *RedisPubSub) SubscribeProgress(ctx context.Context, taskID string) *ProgressSubscriber {
	if ps.rdb == nil {
		return nil
	}
	channel := progressChannelPrefix + taskID
	sub := ps.rdb.Subscribe(ctx, channel)
	return &ProgressSubscriber{
		ch:      sub,
		msgChan: sub.Channel(redis.WithChannelSize(64)),
	}
}

// Available returns true if Redis pub/sub is available.
func (ps *RedisPubSub) Available() bool {
	return ps.rdb != nil
}

// TryReserveSlot atomically increments the running count for a channel and returns
// true if the slot was reserved (new count <= maxConcurrent). Returns (0, false) if
// no slot available. When Redis is nil, returns (0, true) to allow fallback to DB check.
func (ps *RedisPubSub) TryReserveSlot(ctx context.Context, channelID string, maxConcurrent int) (int64, bool, error) {
	if ps.rdb == nil {
		return 0, true, nil
	}
	key := channelRunningCountPrefix + channelID
	result, err := reserveSlotScript.Run(ctx, ps.rdb, []string{key}, maxConcurrent, int(channelRunningCountTTL.Seconds())).Int64()
	if err != nil {
		return 0, false, fmt.Errorf("reserve slot: %w", err)
	}
	if result == 0 {
		return 0, false, nil
	}
	return result, true, nil
}

// releaseSlotScript atomically decrements the counter, clamping at 0 to prevent
// negative values from double-release scenarios.
var releaseSlotScript = redis.NewScript(`
local key = KEYS[1]
local count = redis.call('GET', key)
if count and tonumber(count) > 0 then
	redis.call('DECR', key)
end
return redis.call('GET', key) or 0
`)

// ReleaseSlot decrements the running count for a channel, clamping at 0.
// No-op if Redis is nil.
func (ps *RedisPubSub) ReleaseSlot(ctx context.Context, channelID string) error {
	if ps.rdb == nil {
		return nil
	}
	key := channelRunningCountPrefix + channelID
	if err := releaseSlotScript.Run(ctx, ps.rdb, []string{key}).Err(); err != nil {
		ps.logger.Warn().Err(err).Str("channel_id", channelID).Msg("failed to release concurrency slot")
	}
	return nil
}

// SyncChannelCount sets the Redis counter to the actual DB count for reconciliation.
// No-op if Redis is nil.
func (ps *RedisPubSub) SyncChannelCount(ctx context.Context, channelID string, dbCount int64) error {
	if ps.rdb == nil {
		return nil
	}
	key := channelRunningCountPrefix + channelID
	if dbCount <= 0 {
		if err := ps.rdb.Del(ctx, key).Err(); err != nil {
			ps.logger.Warn().Err(err).Str("channel_id", channelID).Msg("failed to sync concurrency counter")
		}
		return nil
	}
	if err := ps.rdb.Set(ctx, key, dbCount, channelRunningCountTTL).Err(); err != nil {
		ps.logger.Warn().Err(err).Str("channel_id", channelID).Msg("failed to sync concurrency counter")
	}
	return nil
}
