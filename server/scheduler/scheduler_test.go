package scheduler

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

type fakeAsynqEnqueueClient struct {
	calls   int
	options []asynq.Option
}

func (c *fakeAsynqEnqueueClient) Enqueue(_ *asynq.Task, options ...asynq.Option) (*asynq.TaskInfo, error) {
	c.calls++
	c.options = append([]asynq.Option(nil), options...)
	if c.calls > 1 {
		return nil, asynq.ErrTaskIDConflict
	}
	return nil, nil
}

func (*fakeAsynqEnqueueClient) Close() error { return nil }

func TestAsynqClientEnqueueUniqueUsesTaskIDAndAcceptsReplay(t *testing.T) {
	fake := &fakeAsynqEnqueueClient{}
	client := &AsynqClient{client: fake, timeout: time.Minute}
	for i := range 2 {
		enqueued, err := client.EnqueueUnique(TypeContentGenerate, []byte(`{"task_id":"task-1"}`), "task-1")
		if err != nil {
			t.Fatal(err)
		}
		if enqueued != (i == 0) {
			t.Fatalf("enqueue %d created=%v, want %v", i+1, enqueued, i == 0)
		}
	}
	if fake.calls != 2 {
		t.Fatalf("enqueue calls=%d, want 2", fake.calls)
	}
	foundTaskID := false
	for _, option := range fake.options {
		if option.Type() == asynq.TaskIDOpt && option.Value() == "task-1" {
			foundTaskID = true
		}
	}
	if !foundTaskID {
		t.Fatal("unique enqueue did not pass Asynq TaskID")
	}
}

func TestTaskProcessorDoesNotRegisterSeednoteDiscoveryAndCapturesMetrics(t *testing.T) {
	logger := zerolog.Nop()
	var captured []string
	processor := NewTaskProcessor(
		func(ctx context.Context, taskID, userID string) error { return nil },
		func(ctx context.Context, planID string) error { return nil },
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

	if err := processor.mux.ProcessTask(context.Background(), asynq.NewTask("seednote:"+"discover", []byte(`{"tracking_id":"tracking-1"}`))); err == nil || !strings.Contains(err.Error(), "handler not found") {
		t.Fatalf("legacy seednote discovery error = %v, want handler not found", err)
	}
	if err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(TypeSeednoteCaptureMetrics, []byte(`{"tracking_id":"tracking-2"}`))); err != nil {
		t.Fatalf("process capture: %v", err)
	}

	if len(captured) != 1 || captured[0] != "tracking-2" {
		t.Fatalf("captured = %#v, want tracking-2", captured)
	}
}

func TestTaskProcessorDoesNotRegisterLegacyViralAnalysisJob(t *testing.T) {
	logger := zerolog.Nop()
	processor := NewTaskProcessor(
		func(context.Context, string, string) error { return nil },
		func(context.Context, string) error { return nil },
		nil,
		"127.0.0.1:6379", "", 0, 1, &logger,
	)
	err := processor.mux.ProcessTask(context.Background(), asynq.NewTask("viral:"+"analyze", []byte(`{"analysis_id":"legacy"}`)))
	if err == nil || !strings.Contains(err.Error(), "handler not found") {
		t.Fatalf("legacy viral job error = %v, want handler not found", err)
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
		func(ctx context.Context, trackingID string) error { return wantErr },
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
