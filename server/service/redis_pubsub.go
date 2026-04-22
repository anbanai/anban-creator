package service

import (
	"context"
	"encoding/json"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	// Redis channel prefixes for pub/sub.
	cancelChannelPrefix  = "anban:task:cancel:"
	progressChannelPrefix = "anban:task:progress:"
)

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
		msgChan: sub.Channel(),
	}
}

// Available returns true if Redis pub/sub is available.
func (ps *RedisPubSub) Available() bool {
	return ps.rdb != nil
}
