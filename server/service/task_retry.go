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
	ExecutionProfile string
	Prompt           *string
	InputAttachments *[]model.EntryAttachment
	AgentInput       *map[string]any
	Overrides        *CloneTaskOverrides
}

type CloneTaskOverrides struct {
	ProjectID                string
	Quantity                 int
	Prompt                   string
	ImageRatio               string
	ImageCapabilityKey       string
	SkipRefImage             *bool
	ReferenceImageAssetID    string
	InputAttachments         []model.EntryAttachment
	AgentInput               map[string]any
	Watermark                *bool
	HasContentImage          *bool
	HasTailImage             *bool
	ArticleWithCover         *bool
	ArticleWithContentImages *bool
	Ecommerce                *model.EcommerceConfig
	MontageInput             *model.MontageInput
	HypitInput               *model.HypitInput
}

func (s *TaskService) Clone(ctx context.Context, taskID string, cloneParams CloneTaskParams) ([]*model.Task, error) {
	cloneParams.ExecutionProfile = strings.TrimSpace(cloneParams.ExecutionProfile)
	if cloneParams.ExecutionProfile == "" {
		return nil, fmt.Errorf("execution_profile is required")
	}
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
	if cloneParams.Overrides == nil && taskUsesFrozenImageCapability(src.Type) {
		if strings.TrimSpace(src.ImageCapabilityKey) == "" || src.ImageCapabilitySnapshot.Data().Key != strings.TrimSpace(src.ImageCapabilityKey) {
			return nil, ErrTaskImageCapabilityMissing
		}
	}

	inputSourceTaskID, inputSourceProjectID := ResolveCloneInputSource(src)
	if model.IsHypitPlatform(src.Type) {
		inputSourceTaskID = src.ID
		inputSourceProjectID = src.ProjectID
	}

	if cloneParams.Overrides != nil {
		override := cloneParams.Overrides
		destinationProject, err := s.ResolveTaskCreationProject(ctx, src.UserID, override.ProjectID)
		if err != nil {
			return nil, err
		}
		attachments := cloneEntryAttachments(override.InputAttachments)
		referenceAssetID := strings.TrimSpace(override.ReferenceImageAssetID)
		preserveArticleReference := destinationProject.Platform == model.PlatformArticle
		if preserveArticleReference {
			attachments = removeAttachmentAsset(attachments, referenceAssetID)
			if override.ArticleWithCover != nil && !*override.ArticleWithCover {
				referenceAssetID = ""
			}
		} else {
			if referenceAssetID == "" {
				referenceAssetID = strings.TrimSpace(src.ReferenceImageAssetID)
			}
		}
		if !preserveArticleReference && referenceAssetID != strings.TrimSpace(src.ProjectSnapshot.Data().ReferenceImageAssetID) {
			attachments, err = s.prependVerifiedReferenceAttachment(ctx, src.UserID, referenceAssetID, attachments)
			if err != nil {
				return nil, err
			}
		}
		preservedReferenceAssetID := ""
		if preserveArticleReference {
			preservedReferenceAssetID = referenceAssetID
		}
		params := CreateManualParams{
			UserID:                   src.UserID,
			ProjectID:                override.ProjectID,
			ExecutionProfile:         cloneParams.ExecutionProfile,
			Prompt:                   override.Prompt,
			Quantity:                 override.Quantity,
			ImageRatio:               override.ImageRatio,
			ImageCapabilityKey:       override.ImageCapabilityKey,
			SkipRefImage:             override.SkipRefImage,
			ReferenceImageAssetID:    preservedReferenceAssetID,
			InputSourceTaskID:        inputSourceTaskID,
			InputSourceProjectID:     inputSourceProjectID,
			InputAttachments:         attachments,
			AgentInput:               override.AgentInput,
			Watermark:                override.Watermark,
			HasContentImage:          override.HasContentImage,
			HasTailImage:             override.HasTailImage,
			ArticleWithCover:         override.ArticleWithCover,
			ArticleWithContentImages: override.ArticleWithContentImages,
			Ecommerce:                override.Ecommerce,
			MontageInput:             override.MontageInput,
			HypitInput:               override.HypitInput,
			MontageSourceTaskID:      src.ID,
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
			Int("quantity", len(tasks)).
			Msg("task cloned as editable tasks")
		return tasks, nil
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
	prompt := src.Prompt
	if cloneParams.Prompt != nil {
		prompt = strings.TrimSpace(*cloneParams.Prompt)
	}
	preservedReferenceAssetID := ""
	if src.Type == model.PlatformArticle {
		directReferenceAssetID := strings.TrimSpace(src.ReferenceImageAssetID)
		if (src.ArticleWithCover == nil || *src.ArticleWithCover) && directReferenceAssetID != strings.TrimSpace(snapshot.ReferenceImageAssetID) {
			preservedReferenceAssetID = directReferenceAssetID
		}
	}
	params := CreateManualParams{
		UserID:                        src.UserID,
		ProjectID:                     src.ProjectID,
		ExecutionProfile:              cloneParams.ExecutionProfile,
		FrozenTaskType:                src.Type,
		PreserveFrozenConfig:          true,
		Prompt:                        prompt,
		Quantity:                      1,
		ImageRatio:                    src.ImageRatio,
		ImageCapabilityKey:            src.ImageCapabilityKey,
		frozenImageCapabilitySnapshot: ptrImageCapabilitySnapshot(src.ImageCapabilitySnapshot.Data()),
		SkipRefImage:                  &skipRef,
		ReferenceImageAssetID:         preservedReferenceAssetID,
		InputSourceTaskID:             inputSourceTaskID,
		InputSourceProjectID:          inputSourceProjectID,
		Overrides:                     &overrides,
		ProjectSnapshot:               &snapshot,
		Watermark:                     &watermark,
		HasContentImage:               &hasContent,
		HasTailImage:                  &hasTail,
		ArticleWithCover:              articleCover,
		ArticleWithContentImages:      articleContent,
		AgentInput:                    src.AgentInput.Data(),
	}
	if cloneParams.AgentInput != nil {
		params.AgentInput = *cloneParams.AgentInput
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
	if src.Type == model.PlatformArticle {
		params.InputAttachments = removeAttachmentAsset(params.InputAttachments, src.ReferenceImageAssetID)
	} else {
		params.InputAttachments, err = s.prependClonedReferenceAttachment(ctx, src, params.InputAttachments)
		if err != nil {
			return nil, err
		}
	}
	if model.IsHypitPlatform(src.Type) {
		in := src.HypitInput.Data()
		params.HypitInput = &in
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
	return tasks, nil
}

func removeAttachmentAsset(attachments []model.EntryAttachment, rawAssetID string) []model.EntryAttachment {
	assetID := strings.TrimSpace(rawAssetID)
	if assetID == "" {
		return attachments
	}
	filtered := make([]model.EntryAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.AssetID) != assetID {
			filtered = append(filtered, attachment)
		}
	}
	return filtered
}

func ptrImageCapabilitySnapshot(snapshot model.ImageCapabilitySnapshot) *model.ImageCapabilitySnapshot {
	return &snapshot
}

func (s *TaskService) prependClonedReferenceAttachment(ctx context.Context, source *model.Task, attachments []model.EntryAttachment) ([]model.EntryAttachment, error) {
	if strings.TrimSpace(source.ReferenceImageAssetID) == strings.TrimSpace(source.ProjectSnapshot.Data().ReferenceImageAssetID) {
		return attachments, nil
	}
	return s.prependVerifiedReferenceAttachment(ctx, source.UserID, source.ReferenceImageAssetID, attachments)
}

func (s *TaskService) prependVerifiedReferenceAttachment(ctx context.Context, userID, rawAssetID string, attachments []model.EntryAttachment) ([]model.EntryAttachment, error) {
	assetID := strings.TrimSpace(rawAssetID)
	if assetID == "" {
		return attachments, nil
	}
	if s.referenceAssets == nil {
		return nil, ErrReferenceAssetUnavailable
	}
	asset, err := s.referenceAssets.RequireOwned(ctx, userID, assetID, []string{DirectUploadPurposeTaskReference, DirectUploadPurposeAIEntryAttachment})
	if err != nil {
		return nil, fmt.Errorf("resolve cloned reference attachment: %w", err)
	}
	firstInstruction := ""
	foundReference := false
	filtered := make([]model.EntryAttachment, 0, len(attachments))
	for _, attachment := range attachments {
		if strings.TrimSpace(attachment.AssetID) == asset.ID {
			if !foundReference {
				firstInstruction = attachment.Instruction
				foundReference = true
			}
			continue
		}
		filtered = append(filtered, attachment)
	}
	reference := model.EntryAttachment{
		AssetID: asset.ID, Type: "image", FileName: asset.FileName,
		ContentType: asset.ContentType, Size: asset.Size, Instruction: firstInstruction,
	}
	return append([]model.EntryAttachment{reference}, filtered...), nil
}

// ResolveCloneInputSource returns the trusted root task/project pair whose
// immutable task-scoped inputs a clone may reuse. Legacy rows that predate the
// project field keep their source task under the source task's own project.
func ResolveCloneInputSource(src *model.Task) (taskID, projectID string) {
	if src == nil {
		return "", ""
	}
	taskID = strings.TrimSpace(src.InputSourceTaskID)
	projectID = strings.TrimSpace(src.InputSourceProjectID)
	if taskID == "" {
		return src.ID, src.ProjectID
	}
	if projectID == "" {
		projectID = src.ProjectID
	}
	return taskID, projectID
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
