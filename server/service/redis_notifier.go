package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
)

// redisNotifier implements TaskProgressNotifier using Redis pub/sub.
type redisNotifier struct {
	rdb    *redis.Client
	logger *zerolog.Logger
}

// NewRedisNotifier creates a Redis-based TaskProgressNotifier.
func NewRedisNotifier(rdb *redis.Client, logger *zerolog.Logger) TaskProgressNotifier {
	return &redisNotifier{rdb: rdb, logger: logger}
}

// taskChannel returns the Redis pub/sub channel name for a task.
func taskChannel(taskID string) string {
	return fmt.Sprintf("task:%s:progress", taskID)
}

// Subscribe subscribes to progress events for the given task via Redis pub/sub.
func (n *redisNotifier) Subscribe(ctx context.Context, taskID string) (<-chan *ProgressEvent, func(), error) {
	ch := make(chan *ProgressEvent, 64)
	channel := taskChannel(taskID)

	sub := n.rdb.Subscribe(ctx, channel)
	// Wait for the subscription to be confirmed.
	_, err := sub.Receive(ctx)
	if err != nil {
		close(ch)
		return nil, nil, fmt.Errorf("redis subscribe: %w", err)
	}

	go func() {
		defer close(ch)
		redisCh := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-redisCh:
				if !ok {
					return
				}
				var event ProgressEvent
				if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
					n.logger.Warn().Err(err).Str("task_id", taskID).Msg("failed to unmarshal progress event")
					continue
				}
				select {
				case ch <- &event:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	cleanup := func() {
		_ = sub.Close()
	}

	return ch, cleanup, nil
}

// Publish publishes a progress event to the Redis channel for the task.
func (n *redisNotifier) Publish(ctx context.Context, event *ProgressEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal progress event: %w", err)
	}
	channel := taskChannel(event.TaskID)
	if err := n.rdb.Publish(ctx, channel, data).Err(); err != nil {
		return fmt.Errorf("redis publish: %w", err)
	}
	return nil
}

// pollingNotifier implements TaskProgressNotifier by polling the database.
// This is the graceful fallback when Redis pub/sub is not available.
type pollingNotifier struct {
	repo   taskProgressReader
	logger *zerolog.Logger
}

// taskProgressReader is a minimal interface needed by pollingNotifier to read
// task progress from the database.
type taskProgressReader interface {
	GetTaskProgressAndStatus(ctx context.Context, taskID string) (progressLog string, status string, err error)
}

// NewPollingNotifier creates a polling-based TaskProgressNotifier.
func NewPollingNotifier(repo taskProgressReader, logger *zerolog.Logger) TaskProgressNotifier {
	return &pollingNotifier{repo: repo, logger: logger}
}

// Subscribe starts a goroutine that polls the database every second for the
// given task and sends progress events on the returned channel.
func (p *pollingNotifier) Subscribe(ctx context.Context, taskID string) (<-chan *ProgressEvent, func(), error) {
	ch := make(chan *ProgressEvent, 64)

	var lastLogLen int
	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(ctx)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(ch)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				progressLog, status, err := p.repo.GetTaskProgressAndStatus(ctx, taskID)
				if err != nil {
					p.logger.Warn().Err(err).Str("task_id", taskID).Msg("polling notifier: failed to get task progress")
					return
				}

				// Send new progress entries.
				if len(progressLog) > lastLogLen {
					newLog := progressLog[lastLogLen:]
					lastLogLen = len(progressLog)
					event := &ProgressEvent{
						TaskID:  taskID,
						Message: newLog,
					}
					select {
					case ch <- event:
					case <-ctx.Done():
						return
					}
				}

				// Send terminal status events.
				if status == model.TaskStatusCompleted || status == model.TaskStatusFailed || status == model.TaskStatusCancelled {
					event := &ProgressEvent{
						TaskID:     taskID,
						Status:     status,
						IsComplete: true,
					}
					select {
					case ch <- event:
					case <-ctx.Done():
						return
					}
					return
				}
			}
		}
	}()

	cleanup := func() {
		cancel()
		wg.Wait()
	}

	return ch, cleanup, nil
}

// Publish is a no-op for the polling notifier since progress is read from DB.
func (p *pollingNotifier) Publish(_ context.Context, _ *ProgressEvent) error {
	return nil
}
