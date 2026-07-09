package service

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
)

// Clone copies a terminal task's configuration into a fresh pending task and
// enqueues it for execution. Credits are re-reserved at creation, so a manual
// clone is billed as a brand-new run while the original artifact is preserved.
//
// Completed, failed, and cancelled tasks may be cloned. The cloned task carries
// the original's project snapshot verbatim so it uses the same frozen config as
// the source run.
func (s *TaskService) Clone(ctx context.Context, taskID string) (*model.Task, error) {
	src, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}

	// Defensive guard; the handler also enforces this for a clean 400, but the
	// service is the source of truth for the business rule. Active tasks cannot
	// be cloned because that would duplicate in-flight work and billing.
	if src.Status != model.TaskStatusCompleted && src.Status != model.TaskStatusFailed && src.Status != model.TaskStatusCancelled {
		return nil, fmt.Errorf("only completed, failed, or cancelled tasks can be cloned (current status: %s)", src.Status)
	}

	// Copy scalar fields to locals before taking their addresses so each *bool
	// points to a distinct variable (CreateManualParams expects pointers).
	skipRef := src.SkipReferenceImage
	watermark := src.Watermark
	hasContent := src.HasContentImage
	hasTail := src.HasTailImage
	articleCover := src.ArticleWithCover
	articleContent := src.ArticleWithContentImages

	overrides := src.Overrides.Data()
	snapshot := src.ProjectSnapshot.Data()
	params := CreateManualParams{
		UserID:                   src.UserID,
		ProjectID:                src.ProjectID,
		Prompt:                   src.Prompt,
		Quantity:                 1,
		ImageRatio:               src.ImageRatio,
		ImageModelKey:            src.ImageModelKey,
		SkipRefImage:             &skipRef,
		ReferenceImageURL:        src.ReferenceImageURL,
		Overrides:                &overrides,
		ProjectSnapshot:          &snapshot,
		Watermark:                &watermark,
		Goal:                     src.Goal,
		GoalMode:                 src.GoalMode,
		HasContentImage:          &hasContent,
		HasTailImage:             &hasTail,
		ArticleWithCover:         articleCover,
		ArticleWithContentImages: articleContent,
	}

	// Preserve the e-commerce package config (module selection, product photos,
	// selling points) so the clone bills the same package and reuses the inputs.
	if src.Type == model.PlatformEcommerce {
		ec := src.Ecommerce.Data()
		params.Ecommerce = &ec
	}
	if attachments := src.InputAttachments.Data(); len(attachments) > 0 {
		params.InputAttachments = attachments
	}
	if model.IsVideoCreatorPlatform(src.Type) {
		input := src.VideoInput.Data()
		params.VideoCreatorInput = &input
	}
	if model.IsVideoEditorPlatform(src.Type) {
		input := src.VideoInput.Data()
		params.VideoEditorInput = &input
	}
	if model.IsMontagePlatform(src.Type) {
		input := src.MontageInput.Data()
		params.MontageInput = &input
	}

	tasks, err := s.CreateManual(ctx, params)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("clone did not create a task")
	}

	s.logger.Info().
		Str("src_task_id", taskID).
		Str("new_task_id", tasks[0].ID).
		Msg("task cloned as new task")
	return tasks[0], nil
}
