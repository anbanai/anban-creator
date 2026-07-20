package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrUnsupportedPlanPlatform = errors.New("plans are not supported for this project platform")
	ErrPlanUpdateConflict      = errors.New("plan changed concurrently")
)

// PlanService handles plan CRUD and lifecycle operations.
type PlanService struct {
	repo                  repository.Repository
	logger                *zerolog.Logger
	videoCatalog          VideoModelCatalog
	videoCreditMultiplier int
	videoBilling          srvconfig.BillingConfig
	creditSvc             *CreditService
	referenceAssets       *ReferenceAssetService
}

// NewPlanService creates a new PlanService.
func NewPlanService(repo repository.Repository, logger *zerolog.Logger) *PlanService {
	return &PlanService{repo: repo, logger: logger}
}

func (s *PlanService) SetVideoCatalogAndCreditMultiplier(catalog VideoModelCatalog, creditMultiplier int) {
	if s == nil {
		return
	}
	s.videoCatalog = catalog
	s.videoCreditMultiplier = creditMultiplier
}

func (s *PlanService) SetCreditService(creditSvc *CreditService) {
	if s == nil {
		return
	}
	s.creditSvc = creditSvc
}

func (s *PlanService) SetReferenceAssetService(referenceAssets *ReferenceAssetService) {
	if s != nil {
		s.referenceAssets = referenceAssets
	}
}

func (s *PlanService) SetVideoBillingConfig(billing srvconfig.BillingConfig) {
	if s == nil {
		return
	}
	s.videoBilling = billing
}

func (s *PlanService) resolvedVideoCatalog() VideoModelCatalog {
	if s != nil && s.videoCatalog != nil {
		return s.videoCatalog
	}
	return VideoModelCatalog{}
}

func (s *PlanService) resolvedVideoCreditMultiplier() int {
	if s != nil && s.videoCreditMultiplier > 0 {
		return s.videoCreditMultiplier
	}
	return 1000
}

func (s *PlanService) videoBillingOptions(ctx context.Context, userID string) VideoBillingOptions {
	fallback := s.resolvedVideoCreditMultiplier()
	tier := model.TierFree
	userMultiplier := 1.0
	var billing srvconfig.BillingConfig
	if s != nil {
		billing = s.videoBilling
	}
	if s == nil || s.creditSvc == nil || userID == "" {
		return VideoBillingOptionsFromConfig(billing, fallback, tier, userMultiplier)
	}
	if foundTier, err := s.creditSvc.GetUserTier(ctx, userID); err == nil {
		tier = foundTier
	} else if s.logger != nil {
		s.logger.Warn().Err(err).Str("user_id", userID).Msg("video tier lookup failed")
	}
	foundMultiplier, err := s.creditSvc.GetUserBillingMultiplier(ctx, userID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn().Err(err).Str("user_id", userID).Msg("video billing multiplier lookup failed")
		}
	} else if foundMultiplier > 0 {
		userMultiplier = foundMultiplier
	}
	return VideoBillingOptionsFromConfig(billing, fallback, tier, userMultiplier)
}

// CreatePlanParams holds the inputs for PlanService.Create. Pointer-typed optional
// fields use the same nil-means-default / nil-means-unchanged semantics as the
// underlying model. Struct form keeps call sites readable as fields are added
// and prevents argument-order bugs on a signature that has grown past a dozen
// positional params.
type CreatePlanParams struct {
	UserID                string
	ProjectID             string
	CronExpr              string
	Prompt                string
	ImageModelKey         string
	SkipReferenceImage    *bool
	ReferenceImageAssetID string
	Watermark             *bool
	Goal                  string
	GoalMode              bool
	// HasContentImage / HasTailImage: seednote image composition (cover always
	// generated). nil → fall back to plan model defaults (content on, tail off);
	// non-nil honors explicit user choice.
	HasContentImage *bool
	HasTailImage    *bool
	// ArticleWithCover / ArticleWithContentImages: 公众号 article image toggles
	// (cover NOT mandatory). nil → fall back to plan model defaults (both on);
	// non-nil honors explicit user choice.
	ArticleWithCover         *bool
	ArticleWithContentImages *bool
	VideoCreatorConfig       *model.VideoTaskConfig
	VideoCreatorInput        *model.VideoInput
	MontageInput             *model.MontageInput
	InputAttachments         []model.EntryAttachment
}

// Create validates the cron expression, resolves the project, computes the next run
// time, and persists the plan. The task type is derived from the project's platform.
// ImageModelKey optionally selects a per-plan image model (validated upstream by the handler).
//
// Goal and GoalMode propagate to tasks spawned from this plan; when GoalMode is
// true, spawned tasks charge ×GoalMultiplier upfront and evaluate the goal
// after each execution.
//
// HasContentImage / HasTailImage control seednote image composition on spawned
// tasks. nil falls back to the model's column defaults (content on, tail off).
func (s *PlanService) Create(ctx context.Context, p CreatePlanParams) (*model.Plan, error) {
	if p.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if p.CronExpr == "" {
		return nil, fmt.Errorf("cron_expr is required")
	}

	// Load project to derive type and validate ownership.
	project, err := s.repo.Projects().FindByID(ctx, p.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if project.UserID != p.UserID {
		return nil, fmt.Errorf("project not owned by user")
	}
	if project.Status != model.ProjectStatusActive {
		return nil, fmt.Errorf("project is not active")
	}
	if p.ReferenceImageAssetID != "" {
		if s.referenceAssets == nil {
			return nil, ErrReferenceAssetUnavailable
		}
		if _, err := s.referenceAssets.RequireOwned(ctx, p.UserID, p.ReferenceImageAssetID, []string{DirectUploadPurposeTaskReference}); err != nil {
			return nil, err
		}
	}
	// Package-priced or one-off-only platforms can't back plans. Reject up front
	// so API/MCP callers fail fast instead of creating schedules the task runner
	// should never execute for that platform.
	switch project.Platform {
	case model.PlatformEcommerce:
		return nil, fmt.Errorf("plans are not supported for e-commerce projects: %w", ErrUnsupportedPlanPlatform)
	case model.PlatformMoments:
		return nil, fmt.Errorf("plans are not supported for moments projects: %w", ErrUnsupportedPlanPlatform)
	case model.PlatformVideoEditor:
		return nil, fmt.Errorf("plans are not supported for video editor projects: %w", ErrUnsupportedPlanPlatform)
	}
	if !model.IsVideoCreatorPlatform(project.Platform) && (p.VideoCreatorInput != nil || p.VideoCreatorConfig != nil) {
		return nil, fmt.Errorf("%w: video_creator_input/video_creator_config can only be set on videocreator plans", ErrVideoTaskInput)
	}
	if p.MontageInput != nil && !model.IsMontagePlatform(project.Platform) {
		return nil, fmt.Errorf("%w: montage_input can only be set on montage plans", ErrMontageInput)
	}
	if model.IsMontagePlatform(project.Platform) {
		if p.MontageInput == nil || strings.TrimSpace(p.MontageInput.Brief) == "" {
			return nil, fmt.Errorf("%w: montage task requires brief", ErrMontageInput)
		}
	}

	nextRun, err := s.computeNextRun(p.CronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	// Seednote image composition: honor caller's explicit choice, otherwise rely
	// on the model's column defaults (content on, tail off).
	hasContent := true
	if p.HasContentImage != nil {
		hasContent = *p.HasContentImage
	}
	hasTail := false
	if p.HasTailImage != nil {
		hasTail = *p.HasTailImage
	}
	// Article image toggles: honor caller's explicit choice, otherwise rely on the
	// model's column defaults (both on). Ignored for non-article types.
	articleCover := true
	if p.ArticleWithCover != nil {
		articleCover = *p.ArticleWithCover
	}
	articleContent := true
	if p.ArticleWithContentImages != nil {
		articleContent = *p.ArticleWithContentImages
	}
	// A plan carries scheduling-adjacent "what to produce" image params + goal
	// mode. Project/account style config is snapshotted when a task is spawned.
	plan := &model.Plan{
		ID:                       uuid.New().String(),
		UserID:                   p.UserID,
		ProjectID:                p.ProjectID,
		Type:                     project.Platform,
		CronExpr:                 p.CronExpr,
		Prompt:                   p.Prompt,
		Status:                   model.PlanStatusActive,
		NextRunAt:                nextRun,
		ImageModelKey:            p.ImageModelKey,
		ReferenceImageAssetID:    p.ReferenceImageAssetID,
		SkipReferenceImage:       p.SkipReferenceImage != nil && *p.SkipReferenceImage,
		Watermark:                p.Watermark != nil && *p.Watermark,
		Goal:                     p.Goal,
		GoalMode:                 p.GoalMode,
		HasContentImage:          hasContent,
		HasTailImage:             hasTail,
		ArticleWithCover:         &articleCover,
		ArticleWithContentImages: &articleContent,
	}
	plan.SetInputAttachments(cloneEntryAttachments(p.InputAttachments))
	if model.IsVideoCreatorPlatform(project.Platform) {
		if p.VideoCreatorInput != nil {
			plan.SetVideoInput(*p.VideoCreatorInput)
		} else if p.VideoCreatorConfig != nil {
			plan.SetVideoInput(videoInputFromTaskConfig(p.Prompt, p.VideoCreatorConfig))
		} else if p.Prompt != "" {
			plan.SetVideoInput(model.VideoInput{Brief: p.Prompt})
		}
	}
	if model.IsMontagePlatform(project.Platform) && p.MontageInput != nil {
		plan.SetMontageInput(*p.MontageInput)
	}

	if err := s.repo.Plans().Create(ctx, plan); err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}

	return plan, nil
}

// GetByID returns a plan by its ID.
func (s *PlanService) GetByID(ctx context.Context, id string) (*model.Plan, error) {
	plan, err := s.repo.Plans().FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find plan: %w", err)
	}
	return plan, nil
}

// List returns plans for a user with optional project filter and pagination.
// Returns plans and total count.
func (s *PlanService) List(ctx context.Context, userID string, offset, limit int, projectID string) ([]*model.Plan, int64, error) {
	plans, err := s.repo.Plans().FindByUserID(ctx, userID, projectID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list plans: %w", err)
	}

	total, err := s.repo.Plans().CountByUserID(ctx, userID, projectID)
	if err != nil {
		return nil, 0, fmt.Errorf("count plans: %w", err)
	}

	return plans, total, nil
}

// UpdatePlanParams holds the inputs for PlanService.Update. Pointer-typed fields
// use leave-unchanged semantics:
//   - ImageModelKey: nil = leave unchanged; &"" = clear to system default
//     (use model.ImageModelKeySystemDefault / ImageModelKeyCustom for clarity)
//   - SkipReferenceImage: nil = leave unchanged; &true/&false = set
//   - ReferenceImageAssetID: nil = leave unchanged; &"" = clear; &"value" = set
//   - Watermark: nil = leave unchanged; &true/&false = set
//   - GoalMode: nil = leave unchanged; &true/&false = set
//   - HasContentImage / HasTailImage: nil = leave unchanged; &true/&false = set
//   - VideoCreatorInput: nil = leave unchanged; non-nil = update the user-authored creator intake
//
// ID, CronExpr, Prompt, and Goal are plain strings. CronExpr=="" means "leave
// unchanged"; empty Prompt/Goal is a valid value meaning "no prompt / no goal".
type UpdatePlanParams struct {
	ID                       string
	CronExpr                 string
	Prompt                   string
	ImageModelKey            *string
	SkipReferenceImage       *bool
	ReferenceImageAssetID    *string
	Watermark                *bool
	Goal                     string
	GoalMode                 *bool
	HasContentImage          *bool
	HasTailImage             *bool
	ArticleWithCover         *bool
	ArticleWithContentImages *bool
	VideoCreatorConfig       *model.VideoTaskConfig
	VideoCreatorInput        *model.VideoInput
	MontageInput             *model.MontageInput
	InputAttachments         *[]model.EntryAttachment
}

// Update modifies a plan's fields per UpdatePlanParams. If the cron expression
// changed, next_run_at is recomputed. See UpdatePlanParams for field semantics.
func (s *PlanService) Update(ctx context.Context, p UpdatePlanParams) (*model.Plan, error) {
	plan, scheduleChanged, err := s.preparePlanUpdate(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Plans().UpdateEditable(ctx, plan, scheduleChanged); err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}
	return plan, nil
}

func (s *PlanService) UpdateIfReferenceImageAssetID(ctx context.Context, p UpdatePlanParams, expectedID string) (*model.Plan, error) {
	plan, scheduleChanged, err := s.preparePlanUpdate(ctx, p)
	if err != nil {
		return nil, err
	}
	if plan.ReferenceImageAssetID != expectedID {
		return nil, ErrPlanUpdateConflict
	}
	won, err := s.repo.Plans().UpdateEditableIfReferenceImageAssetID(ctx, plan, expectedID, scheduleChanged)
	if err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}
	if !won {
		return nil, ErrPlanUpdateConflict
	}
	return plan, nil
}

func (s *PlanService) preparePlanUpdate(ctx context.Context, p UpdatePlanParams) (*model.Plan, bool, error) {
	plan, err := s.repo.Plans().FindByID(ctx, p.ID)
	if err != nil {
		return nil, false, fmt.Errorf("find plan: %w", err)
	}
	scheduleChanged := p.CronExpr != "" && p.CronExpr != plan.CronExpr
	plan, err = s.applyPlanUpdate(ctx, plan, p, scheduleChanged)
	if err != nil {
		return nil, false, err
	}
	return plan, scheduleChanged, nil
}

func (s *PlanService) applyPlanUpdate(ctx context.Context, plan *model.Plan, p UpdatePlanParams, scheduleChanged bool) (*model.Plan, error) {
	plan.Prompt = p.Prompt
	if p.ReferenceImageAssetID != nil {
		if *p.ReferenceImageAssetID != "" {
			if s.referenceAssets == nil {
				return nil, ErrReferenceAssetUnavailable
			}
			if _, err := s.referenceAssets.RequireOwned(ctx, plan.UserID, *p.ReferenceImageAssetID, []string{DirectUploadPurposeTaskReference}); err != nil {
				return nil, err
			}
		}
		plan.ReferenceImageAssetID = *p.ReferenceImageAssetID
	}
	plan.Goal = p.Goal
	if p.ImageModelKey != nil {
		plan.ImageModelKey = *p.ImageModelKey
	}
	if p.SkipReferenceImage != nil {
		plan.SkipReferenceImage = *p.SkipReferenceImage
	}
	if p.Watermark != nil {
		plan.Watermark = *p.Watermark
	}
	if p.GoalMode != nil {
		plan.GoalMode = *p.GoalMode
	}
	if p.HasContentImage != nil {
		plan.HasContentImage = *p.HasContentImage
	}
	if p.HasTailImage != nil {
		plan.HasTailImage = *p.HasTailImage
	}
	if p.ArticleWithCover != nil {
		v := *p.ArticleWithCover
		plan.ArticleWithCover = &v
	}
	if p.ArticleWithContentImages != nil {
		v := *p.ArticleWithContentImages
		plan.ArticleWithContentImages = &v
	}
	if p.InputAttachments != nil {
		plan.SetInputAttachments(cloneEntryAttachments(*p.InputAttachments))
	}
	if p.VideoCreatorInput != nil {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find project: %w", err)
		}
		if !model.IsVideoCreatorPlatform(project.Platform) {
			return nil, fmt.Errorf("%w: video_creator_input can only be set on videocreator plans", ErrVideoTaskInput)
		}
		plan.SetVideoInput(*p.VideoCreatorInput)
		plan.VideoEstimatedCredits = 0
	} else if p.VideoCreatorConfig != nil {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find project: %w", err)
		}
		if !model.IsVideoCreatorPlatform(project.Platform) {
			return nil, fmt.Errorf("%w: video_creator_config can only be set on videocreator plans", ErrVideoTaskInput)
		}
		plan.SetVideoInput(videoInputFromTaskConfig(plan.Prompt, p.VideoCreatorConfig))
		plan.VideoEstimatedCredits = 0
	}
	if p.MontageInput != nil {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find project: %w", err)
		}
		if !model.IsMontagePlatform(project.Platform) {
			return nil, fmt.Errorf("%w: montage_input can only be set on montage plans", ErrMontageInput)
		}
		if strings.TrimSpace(p.MontageInput.Brief) == "" {
			return nil, fmt.Errorf("%w: montage task requires brief", ErrMontageInput)
		}
		plan.SetMontageInput(*p.MontageInput)
	}
	// If cron expression changed, validate and recompute next run.
	if scheduleChanged {
		if _, err := cron.ParseStandard(p.CronExpr); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
		plan.CronExpr = p.CronExpr
		nextRun, err := s.computeNextRun(p.CronExpr)
		if err != nil {
			return nil, fmt.Errorf("compute next run: %w", err)
		}
		plan.NextRunAt = nextRun
	}

	return plan, nil
}

// Delete removes a plan by ID.
func (s *PlanService) Delete(ctx context.Context, id string) error {
	if err := s.repo.Plans().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete plan: %w", err)
	}
	return nil
}

// Pause sets a plan's status to "paused".
func (s *PlanService) Pause(ctx context.Context, id string) error {
	plan, err := s.repo.Plans().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find plan: %w", err)
	}

	if err := s.repo.Plans().UpdateStatusAndNextRunAt(ctx, plan.ID, model.PlanStatusPaused, nil); err != nil {
		return fmt.Errorf("pause plan: %w", err)
	}

	return nil
}

// Resume sets a plan's status to "active" and recomputes next_run_at.
func (s *PlanService) Resume(ctx context.Context, id string) error {
	plan, err := s.repo.Plans().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find plan: %w", err)
	}

	nextRun, err := s.computeNextRun(plan.CronExpr)
	if err != nil {
		return fmt.Errorf("compute next run: %w", err)
	}
	if err := s.repo.Plans().UpdateStatusAndNextRunAt(ctx, plan.ID, model.PlanStatusActive, nextRun); err != nil {
		return fmt.Errorf("resume plan: %w", err)
	}

	return nil
}

// computeNextRun parses a cron expression and returns the next scheduled run time.
func (s *PlanService) computeNextRun(cronExpr string) (*time.Time, error) {
	schedule, err := cron.ParseStandard(cronExpr)
	if err != nil {
		return nil, err
	}
	next := schedule.Next(time.Now())
	return &next, nil
}
