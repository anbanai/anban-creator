package service

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTaskStreamPubSubSeparatesLifecycleAndLogChannels(t *testing.T) {
	miniRedis := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	pubsub := NewRedisPubSub(rdb, &logger)
	ctx := context.Background()
	taskID := "task-1"
	logSub := pubsub.SubscribeLogs(ctx, taskID)
	lifecycleSub := pubsub.SubscribeLifecycle(ctx, taskID)
	t.Cleanup(func() { _ = logSub.Close(); _ = lifecycleSub.Close() })
	waitForSubscription := func(channel string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for miniRedis.PubSubNumSub(channel)[channel] != 1 {
			if time.Now().After(deadline) {
				t.Fatalf("subscription not established for %s", channel)
			}
		}
	}
	waitForSubscription(logChannelPrefix + taskID)
	waitForSubscription(lifecycleChannelPrefix + taskID)

	pubsub.PublishLog(ctx, taskID, "raw tool output")
	pubsub.PublishLifecycle(ctx, taskID, model.TaskLifecycle{Version: 1, Revision: 3, ExecutionID: "execution-1"})

	select {
	case message := <-logSub.Events():
		var event TaskLogEvent
		if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
			t.Fatal(err)
		}
		if event.TaskID != taskID || event.Message != "raw tool output" {
			t.Fatalf("log event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for log event")
	}
	select {
	case message := <-lifecycleSub.Events():
		var event TaskLifecycleEvent
		if err := json.Unmarshal([]byte(message.Payload), &event); err != nil {
			t.Fatal(err)
		}
		if event.TaskID != taskID || event.Lifecycle.Revision != 3 {
			t.Fatalf("lifecycle event = %#v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for lifecycle event")
	}

	select {
	case message := <-logSub.Events():
		t.Fatalf("lifecycle leaked onto log channel: %s", message.Payload)
	case <-time.After(50 * time.Millisecond):
	}
	select {
	case message := <-lifecycleSub.Events():
		t.Fatalf("log leaked onto lifecycle channel: %s", message.Payload)
	case <-time.After(50 * time.Millisecond):
	}
}
