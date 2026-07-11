package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

func TestTaskProcessor_SeednoteHandlers(t *testing.T) {
	logger := zerolog.Nop()
	var discovered []string
	var captured []string
	processor := NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error { return nil },
		func(ctx context.Context, planID string) error { return nil },
		func(ctx context.Context, trackingID string) error {
			discovered = append(discovered, trackingID)
			return nil
		},
		func(ctx context.Context, trackingID string) error {
			captured = append(captured, trackingID)
			return nil
		},
		nil,
		"127.0.0.1:6379",
		"",
		0,
		1,
		&logger,
	)

	if err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypeSeednoteDiscover, []byte(`{"tracking_id":"tracking-1"}`))); err != nil {
		t.Fatalf("process discover: %v", err)
	}
	if err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypeSeednoteCaptureMetrics, []byte(`{"tracking_id":"tracking-2"}`))); err != nil {
		t.Fatalf("process capture: %v", err)
	}

	if len(discovered) != 1 || discovered[0] != "tracking-1" {
		t.Fatalf("discovered = %#v, want tracking-1", discovered)
	}
	if len(captured) != 1 || captured[0] != "tracking-2" {
		t.Fatalf("captured = %#v, want tracking-2", captured)
	}
}

func TestTaskProcessor_PlanTriggerHandler(t *testing.T) {
	logger := zerolog.Nop()
	var triggered []string
	processor := NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error { return nil },
		func(ctx context.Context, planID string) error {
			triggered = append(triggered, planID)
			return nil
		},
		nil,
		nil,
		nil,
		"127.0.0.1:6379",
		"",
		0,
		1,
		&logger,
	)

	if err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypePlanTrigger, []byte(`{"plan_id":"plan-1"}`))); err != nil {
		t.Fatalf("process plan trigger: %v", err)
	}

	if len(triggered) != 1 || triggered[0] != "plan-1" {
		t.Fatalf("triggered = %#v, want plan-1", triggered)
	}
}

func TestTaskProcessor_PlanTriggerHandlerErrorsPropagate(t *testing.T) {
	logger := zerolog.Nop()
	wantErr := errors.New("plan trigger failed")
	processor := NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error { return nil },
		func(ctx context.Context, planID string) error { return wantErr },
		nil,
		nil,
		nil,
		"127.0.0.1:6379",
		"",
		0,
		1,
		&logger,
	)

	err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypePlanTrigger, []byte(`{"plan_id":"plan-1"}`)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestTaskProcessor_SeednoteHandlerErrorsPropagate(t *testing.T) {
	logger := zerolog.Nop()
	wantErr := errors.New("capture failed")
	processor := NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error { return nil },
		func(ctx context.Context, planID string) error { return nil },
		func(ctx context.Context, trackingID string) error { return nil },
		func(ctx context.Context, trackingID string) error { return wantErr },
		nil,
		"127.0.0.1:6379",
		"",
		0,
		1,
		&logger,
	)

	err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypeSeednoteCaptureMetrics, []byte(`{"tracking_id":"tracking-2"}`)))
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}
