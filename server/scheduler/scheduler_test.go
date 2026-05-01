package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

func TestTaskProcessor_RednoteHandlers(t *testing.T) {
	logger := zerolog.Nop()
	var discovered []string
	var captured []string
	processor := NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error { return nil },
		func(ctx context.Context, planID string) error { return nil },
		func(ctx context.Context) error { return nil },
		func(ctx context.Context, trackingID string) error {
			discovered = append(discovered, trackingID)
			return nil
		},
		func(ctx context.Context, trackingID string) error {
			captured = append(captured, trackingID)
			return nil
		},
		"127.0.0.1:6379",
		"",
		0,
		1,
		&logger,
	)

	if err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypeRednoteDiscover, []byte(`{"tracking_id":"tracking-1"}`))); err != nil {
		t.Fatalf("process discover: %v", err)
	}
	if err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypeRednoteCaptureMetrics, []byte(`{"tracking_id":"tracking-2"}`))); err != nil {
		t.Fatalf("process capture: %v", err)
	}

	if len(discovered) != 1 || discovered[0] != "tracking-1" {
		t.Fatalf("discovered = %#v, want tracking-1", discovered)
	}
	if len(captured) != 1 || captured[0] != "tracking-2" {
		t.Fatalf("captured = %#v, want tracking-2", captured)
	}
}

func TestTaskProcessor_RednoteHandlerErrorsPropagate(t *testing.T) {
	logger := zerolog.Nop()
	wantErr := errors.New("capture failed")
	processor := NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error { return nil },
		func(ctx context.Context, planID string) error { return nil },
		func(ctx context.Context) error { return nil },
		func(ctx context.Context, trackingID string) error { return nil },
		func(ctx context.Context, trackingID string) error { return wantErr },
		"127.0.0.1:6379",
		"",
		0,
		1,
		&logger,
	)

	err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypeRednoteCaptureMetrics, []byte(`{"tracking_id":"tracking-2"}`)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
