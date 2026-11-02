package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/hibiken/asynq"
)

// RegisterTopicRadarHandlers routes durable job IDs; the service reloads all
// authoritative identity and frozen inputs before starting managed execution.
func (tp *TaskProcessor) RegisterTopicRadarHandlers(digest, script func(context.Context, string) error) {
	if tp == nil {
		return
	}
	for _, registration := range []struct {
		taskType string
		key      string
		handler  func(context.Context, string) error
	}{
		{model.TaskTypeTopicDigest, "digest_id", digest},
		{model.TaskTypeTopicScript, "script_job_id", script},
	} {
		if registration.handler == nil {
			continue
		}
		tp.mux.HandleFunc(registration.taskType, func(ctx context.Context, task *asynq.Task) error {
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(task.Payload(), &payload); err != nil {
				return fmt.Errorf("invalid topic radar payload: %w", asynq.SkipRetry)
			}
			var id string
			if err := json.Unmarshal(payload[registration.key], &id); err != nil || strings.TrimSpace(id) == "" {
				return fmt.Errorf("invalid topic radar %s: %w", registration.key, asynq.SkipRetry)
			}
			return registration.handler(ctx, strings.TrimSpace(id))
		})
	}
}
