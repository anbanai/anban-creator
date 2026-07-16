package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

// Clone copies a terminal task's configuration into a fresh pending task and
// enqueues it for execution. Credits are re-reserved at creation, so a manual
// clone is billed as a brand-new run while the original artifact is preserved.
//
// Completed, failed, and cancelled tasks may be cloned. The cloned task carries
// the original's project snapshot verbatim so it uses the same frozen config as
// the source run.
type CloneTaskParams struct {
	Prompt           *string
	InputAttachments *[]model.EntryAttachment
}

func (s *TaskService) Clone(ctx context.Context, taskID string, cloneParams CloneTaskParams) (*model.Task, error) {
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
	inputSourceTaskID := src.InputSourceTaskID
	if inputSourceTaskID == "" {
		inputSourceTaskID = src.ID
	}
	prompt := src.Prompt
	if cloneParams.Prompt != nil {
		prompt = strings.TrimSpace(*cloneParams.Prompt)
	}
	executionTarget, err := cloneExecutionTarget(src.ExecutionTarget)
	if err != nil {
		return nil, err
	}
	params := CreateManualParams{
		UserID:                   src.UserID,
		ProjectID:                src.ProjectID,
		FrozenTaskType:           src.Type,
		PreserveFrozenConfig:     true,
		Prompt:                   prompt,
		Quantity:                 1,
		ImageRatio:               src.ImageRatio,
		ImageModelKey:            src.ImageModelKey,
		SkipRefImage:             &skipRef,
		ReferenceImageURL:        src.ReferenceImageURL,
		InputSourceTaskID:        inputSourceTaskID,
		Overrides:                &overrides,
		ProjectSnapshot:          &snapshot,
		Watermark:                &watermark,
		Goal:                     src.Goal,
		GoalMode:                 src.GoalMode,
		HasContentImage:          &hasContent,
		HasTailImage:             &hasTail,
		ArticleWithCover:         articleCover,
		ArticleWithContentImages: articleContent,
		ExecutionTarget:          executionTarget,
	}

	// Preserve the e-commerce package config (module selection, product photos,
	// selling points) so the clone bills the same package and reuses the inputs.
	if src.Type == model.PlatformEcommerce {
		ec := src.Ecommerce.Data()
		params.Ecommerce = &ec
	}
	if cloneParams.InputAttachments != nil {
		params.InputAttachments = cloneEntryAttachments(*cloneParams.InputAttachments)
	} else if attachments := cloneOriginalInputAttachments(src.InputAttachments.Data()); len(attachments) > 0 {
		params.InputAttachments = attachments
	}
	if model.IsVideoCreatorPlatform(src.Type) {
		input := src.VideoInput.Data()
		if cloneParams.Prompt != nil {
			input.Brief = prompt
		}
		config := src.VideoConfig.Data()
		params.VideoCreatorInput = &input
		params.FrozenVideoConfig = &config
	}
	if model.IsVideoEditorPlatform(src.Type) {
		input := src.VideoInput.Data()
		if cloneParams.Prompt != nil {
			input.Brief = prompt
		}
		config := src.VideoConfig.Data()
		params.VideoEditorInput = &input
		params.FrozenVideoConfig = &config
	}
	if model.IsMontagePlatform(src.Type) {
		input := src.MontageInput.Data()
		if cloneParams.Prompt != nil {
			input.Brief = prompt
		}
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

func cloneExecutionTarget(source string) (string, error) {
	switch source {
	case model.ExecutionTargetLocal, model.ExecutionTargetLocalClaimed:
		return model.ExecutionTargetLocal, nil
	case model.ExecutionTargetCloud:
		return model.ExecutionTargetCloud, nil
	default:
		return "", fmt.Errorf("unsupported source execution target %q", source)
	}
}

func cloneOriginalInputAttachments(attachments []model.EntryAttachment) []model.EntryAttachment {
	originals := make([]model.EntryAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if !model.IsResumeEntryAttachment(attachment) {
			originals = append(originals, attachment)
		}
	}
	return originals
}
