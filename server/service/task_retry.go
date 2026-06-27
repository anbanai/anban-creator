package service

import (
	"context"
	"fmt"

	"github.com/royalrick/anbanwriter/server/model"
)

// Retry clones a failed task's configuration into a fresh pending task and
// enqueues it for execution. Credits are re-reserved at creation — the original
// failed task was already refunded when it failed (see HandleExecutionFailure /
// refundTaskByMode), so a retry is billed as a brand-new run with zero
// double-charge risk.
//
// Only failed (or cancelled) tasks may be retried. The cloned task carries the
// original's per-dimension overrides (src.Overrides) verbatim, so it inherits
// the same project dimensions and applies the same task-level overrides as the
// failed run (live inheritance: task.Overrides.X ?? project.X).
func (s *TaskService) Retry(ctx context.Context, taskID string) (*model.Task, error) {
	src, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}

	// Defensive guard; the handler also enforces this for a clean 400, but the
	// service is the source of truth for the business rule. Both failed and
	// cancelled tasks are terminal and already refunded, so either may be retried
	// as a fresh billed run.
	if src.Status != model.TaskStatusFailed && src.Status != model.TaskStatusCancelled {
		return nil, fmt.Errorf("only failed or cancelled tasks can be retried (current status: %s)", src.Status)
	}

	// Copy scalar fields to locals before taking their addresses so each *bool
	// points to a distinct variable (CreateManualParams expects pointers).
	skipRef := src.SkipReferenceImage
	watermark := src.Watermark
	hasContent := src.HasContentImage
	hasTail := src.HasTailImage

	overrides := src.Overrides.Data()
	params := CreateManualParams{
		UserID:            src.UserID,
		ProjectID:         src.ProjectID,
		Prompt:            src.Prompt,
		Quantity:          1,
		ImageRatio:        src.ImageRatio,
		ImageModelKey:     src.ImageModelKey,
		SkipRefImage:      &skipRef,
		ReferenceImageURL: src.ReferenceImageURL,
		Overrides:         &overrides,
		Watermark:         &watermark,
		Goal:              src.Goal,
		GoalMode:          src.GoalMode,
		HasContentImage:   &hasContent,
		HasTailImage:      &hasTail,
	}

	// Preserve the e-commerce package config (module selection, product photos,
	// selling points) so the retry bills the same package and reuses the inputs.
	if src.Type == model.PlatformEcommerce {
		ec := src.Ecommerce.Data()
		params.Ecommerce = &ec
	}

	tasks, err := s.CreateManual(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("retry did not create a task")
	}

	s.logger.Info().
		Str("src_task_id", taskID).
		Str("new_task_id", tasks[0].ID).
		Msg("task retried as new task")
	return tasks[0], nil
}
