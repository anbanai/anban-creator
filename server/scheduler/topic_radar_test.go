package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/hibiken/asynq"
	"github.com/rs/zerolog"
)

func TestTaskProcessorTopicRadarHandlers(t *testing.T) {
	logger := zerolog.Nop()
	processor := NewTaskProcessor(nil, nil, nil, nil, nil, "127.0.0.1:6379", "", 0, 1, &logger)
	registrar, ok := any(processor).(interface {
		RegisterTopicRadarHandlers(func(context.Context, string) error, func(context.Context, string) error)
	})
	if !ok {
		t.Fatal("TaskProcessor must register Topic Radar digest and script handlers")
	}
	wantErr := errors.New("retryable dispatch failure")
	var calls []string
	registrar.RegisterTopicRadarHandlers(func(_ context.Context, id string) error {
		calls = append(calls, "digest:"+id)
		return wantErr
	}, func(_ context.Context, id string) error {
		calls = append(calls, "script:"+id)
		return wantErr
	})
	for _, tc := range []struct{ taskType, payload, want string }{
		{model.TaskTypeTopicDigest, `{"digest_id":"digest-1"}`, "digest:digest-1"},
		{model.TaskTypeTopicScript, `{"script_job_id":"script-1"}`, "script:script-1"},
	} {
		t.Run(tc.taskType, func(t *testing.T) {
			before := len(calls)
			err := processor.mux.ProcessTask(context.Background(), asynq.NewTask(tc.taskType, []byte(tc.payload)))
			if !errors.Is(err, wantErr) || len(calls) != before+1 || calls[before] != tc.want {
				t.Fatalf("err=%v calls=%v; want propagated error and %s", err, calls, tc.want)
			}
			for _, payload := range []string{`{`, `{}`, `{"digest_id":" ","script_job_id":" "}`, `{"digest_id":42,"script_job_id":42}`} {
				before = len(calls)
				err = processor.mux.ProcessTask(context.Background(), asynq.NewTask(tc.taskType, []byte(payload)))
				if !errors.Is(err, asynq.SkipRetry) || len(calls) != before {
					t.Fatalf("invalid payload %s: err=%v calls=%v; want SkipRetry without dispatch", payload, err, calls)
				}
			}
		})
	}
}
