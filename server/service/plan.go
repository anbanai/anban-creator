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
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrUnsupportedPlanPlatform = errors.New("plans are not supported for this project platform")
	ErrPlanUpdateConflict      = errors.New("plan changed concurrently")
	ErrPlanPortraitReference   = errors.New("公众号项目未配置人物参考图，请先在项目设置中上传")
)

// PlanService handles plan CRUD and lifecycle operations.
type PlanService struct {
	repo                repository.Repository
	logger              *zerolog.Logger
	referenceAssets     *ReferenceAssetService
	agentProfiles       *AgentProfileRegistry
	billingWalletSvc    *BillingWalletService
	montageCapabilities *MontageCapabilityService
}

// NewPlanService creates a new PlanService.
func NewPlanService(repo repository.Repository, logger *zerolog.Logger) *PlanService {
	return &PlanService{repo: repo, logger: logger}
}

func (s *PlanService) SetReferenceAssetService(referenceAssets *ReferenceAssetService) {
	if s != nil {
		s.referenceAssets = referenceAssets
	}
}

func (s *PlanService) SetAgentProfileRegistry(registry *AgentProfileRegistry) {
	if s != nil {
		s.agentProfiles = registry
	}
}

func (s *PlanService) SetBillingWalletService(wallet *BillingWalletService) {
	if s != nil {
		s.billingWalletSvc = wallet
	}
}

func (s *PlanService) SetMontageCapabilityService(capabilities *MontageCapabilityService) {
	if s != nil {
		s.montageCapabilities = capabilities
	}
}

// CreatePlanParams holds the inputs for PlanService.Create. Pointer-typed optional
// fields use the same nil-means-default / nil-means-unchanged semantics as the
// underlying model. Struct form keeps call sites readable as fields are added
// and prevents argument-order bugs on a signature that has grown past a dozen
// positional params.
type CreatePlanParams struct {
	UserID                string
	ProjectID             string
	ExecutionProfile      string
	CronExpr              string
	Prompt                string
	ImageCapabilityKey    string
	ImageRatio            string
	SkipReferenceImage    *bool
	ReferenceImageAssetID string
	UsePortraitReference  bool
	Watermark             *bool
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
	MontageInput             *model.MontageInput
	InputAttachments         []model.EntryAttachment
	AgentInput               map[string]any
}

// Create validates the cron expression, resolves the project, computes the next run
// time, and persists the plan. The task type is derived from the project's platform.
// ImageCapabilityKey optionally selects a per-plan image model (validated upstream by the handler).
//
// HasContentImage / HasTailImage control seednote image composition on spawned
// tasks. nil falls back to the model's column defaults (content on, tail off).
func (s *PlanService) Create(ctx context.Context, p CreatePlanParams) (*model.Plan, error) {
	if strings.TrimSpace(p.ExecutionProfile) == "" {
		return nil, fmt.Errorf("execution_profile is required")
	}
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
	profile, err := resolveAgentProfileForUser(ctx, s.repo, s.agentProfiles, p.UserID, p.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	p.ExecutionProfile = profile.ID
	if p.UsePortraitReference && project.Platform == model.PlatformArticle {
		if project.PortraitReferenceImageAssetID == "" {
			return nil, ErrPlanPortraitReference
		}
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
	}
	if p.MontageInput != nil && !model.IsMontagePlatform(project.Platform) {
		return nil, fmt.Errorf("%w: montage_input can only be set on montage plans", ErrMontageInput)
	}
	if model.IsMontagePlatform(project.Platform) {
		if s.montageCapabilities != nil {
			var input *model.MontageInput
			if p.MontageInput != nil {
				copy := *p.MontageInput
				input = &copy
			}
			if err := s.montageCapabilities.NormalizeAndValidateInput(input, project.MontageDefaults.Data()); err != nil {
				return nil, err
			}
			p.MontageInput = input
		}
		if p.MontageInput == nil || strings.TrimSpace(p.MontageInput.Brief) == "" {
			return nil, fmt.Errorf("%w: montage task requires brief", ErrMontageInput)
		}
	}
	agentInput, err := validateAndCloneAgentInput(project.Platform, p.AgentInput)
	if err != nil {
		return nil, err
	}
	effectiveImageRatio := strings.TrimSpace(p.ImageRatio)
	if model.IsMontagePlatform(project.Platform) && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" {
		effectiveImageRatio = strings.TrimSpace(project.ImageRatio)
	}
	if model.IsMontagePlatform(project.Platform) && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" {
		effectiveImageRatio = model.DefaultImageRatio(project.Platform)
	}
	if len(model.SupportedImageRatios(project.Platform)) > 0 && !model.IsBusinessImageRatioAllowed(project.Platform, effectiveImageRatio) {
		return nil, fmt.Errorf("%s for platform %s: %s", model.ValidImageRatioHint, project.Platform, effectiveImageRatio)
	}
	effectiveImageCapabilityKey := strings.TrimSpace(p.ImageCapabilityKey)

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
	// A plan carries scheduling-adjacent "what to produce" image params.
	// Project/account style config is snapshotted when a task is spawned.
	plan := &model.Plan{
		ID:                       uuid.New().String(),
		UserID:                   p.UserID,
		ProjectID:                p.ProjectID,
		Type:                     project.Platform,
		ExecutionProfile:         strings.TrimSpace(p.ExecutionProfile),
		CronExpr:                 p.CronExpr,
		Prompt:                   p.Prompt,
		Status:                   model.PlanStatusActive,
		NextRunAt:                nextRun,
		ImageCapabilityKey:       effectiveImageCapabilityKey,
		ImageRatio:               effectiveImageRatio,
		ReferenceImageAssetID:    p.ReferenceImageAssetID,
		UsePortraitReference:     p.UsePortraitReference,
		SkipReferenceImage:       p.SkipReferenceImage != nil && *p.SkipReferenceImage,
		Watermark:                p.Watermark != nil && *p.Watermark,
		HasContentImage:          hasContent,
		HasTailImage:             hasTail,
		ArticleWithCover:         &articleCover,
		ArticleWithContentImages: &articleContent,
	}
	plan.SetInputAttachments(cloneEntryAttachments(p.InputAttachments))
	if agentInput != nil {
		plan.SetAgentInput(agentInput)
	}
	if model.IsMontagePlatform(project.Platform) && p.MontageInput != nil {
		plan.SetMontageInput(*p.MontageInput)
	}

	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		authoritative, err := tx.Projects().FindByIDForUpdate(ctx, p.ProjectID)
		if err != nil {
			return err
		}
		if authoritative.UserID != p.UserID {
			return fmt.Errorf("project not owned by user")
		}
		if authoritative.Status != model.ProjectStatusActive || authoritative.DeletingAt != nil {
			return fmt.Errorf("project is not active")
		}
		return tx.Plans().Create(ctx, plan)
	}); err != nil {
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
//   - ImageCapabilityKey: nil = leave unchanged; &"" = clear to the configured default
//   - SkipReferenceImage: nil = leave unchanged; &true/&false = set
//   - ReferenceImageAssetID: nil = leave unchanged; &"" = clear; &"value" = set
//   - Watermark: nil = leave unchanged; &true/&false = set
//   - HasContentImage / HasTailImage: nil = leave unchanged; &true/&false = set
//
// ID, CronExpr, and Prompt are plain strings. CronExpr=="" means "leave
// unchanged"; an empty Prompt is a valid value meaning "no prompt".
type UpdatePlanParams struct {
	ID                       string
	ExecutionProfile         string
	CronExpr                 string
	Prompt                   string
	ImageCapabilityKey       *string
	ImageRatio               *string
	SkipReferenceImage       *bool
	ReferenceImageAssetID    *string
	UsePortraitReference     *bool
	Watermark                *bool
	HasContentImage          *bool
	HasTailImage             *bool
	ArticleWithCover         *bool
	ArticleWithContentImages *bool
	MontageInput             *model.MontageInput
	InputAttachments         *[]model.EntryAttachment
	AgentInput               *map[string]any
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
	if strings.TrimSpace(p.ExecutionProfile) == "" {
		return nil, false, fmt.Errorf("execution_profile is required")
	}
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
	profile, err := resolveAgentProfileForUser(ctx, s.repo, s.agentProfiles, plan.UserID, p.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	plan.ExecutionProfile = profile.ID
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
	if p.UsePortraitReference != nil {
		plan.UsePortraitReference = *p.UsePortraitReference
		if *p.UsePortraitReference && plan.Type == model.PlatformArticle {
			project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
			if err != nil {
				return nil, fmt.Errorf("find project: %w", err)
			}
			if project.PortraitReferenceImageAssetID == "" {
				return nil, ErrPlanPortraitReference
			}
		}
	}
	if p.ImageRatio != nil {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, err
		}
		if len(model.SupportedImageRatios(project.Platform)) > 0 && !model.IsBusinessImageRatioAllowed(project.Platform, strings.TrimSpace(*p.ImageRatio)) {
			return nil, fmt.Errorf("%s for platform %s: %s", model.ValidImageRatioHint, project.Platform, *p.ImageRatio)
		}
		nextRatio := strings.TrimSpace(*p.ImageRatio)
		if model.IsMontagePlatform(plan.Type) && nextRatio == model.ImageRatioAuto {
			nextRatio = model.DefaultImageRatio(plan.Type)
		}
		plan.ImageRatio = nextRatio
	}
	if p.ImageCapabilityKey != nil {
		plan.ImageCapabilityKey = *p.ImageCapabilityKey
	}
	if p.SkipReferenceImage != nil {
		plan.SkipReferenceImage = *p.SkipReferenceImage
	}
	if p.Watermark != nil {
		plan.Watermark = *p.Watermark
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
	if p.AgentInput != nil {
		agentInput, err := validateAndCloneAgentInput(plan.Type, *p.AgentInput)
		if err != nil {
			return nil, err
		}
		plan.SetAgentInput(agentInput)
	}
	if p.MontageInput != nil {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find project: %w", err)
		}
		if !model.IsMontagePlatform(project.Platform) {
			return nil, fmt.Errorf("%w: montage_input can only be set on montage plans", ErrMontageInput)
		}
		if s.montageCapabilities != nil {
			copy := *p.MontageInput
			if err := s.montageCapabilities.NormalizeAndValidateInput(&copy, project.MontageDefaults.Data()); err != nil {
				return nil, err
			}
			p.MontageInput = &copy
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

// Pause sets a plan's status to "paused" and atomically cancels its pending
// backlog. Any task admission charges for cancelled tasks are reversed via the
// billing settlement outbox; running tasks are deliberately left untouched.
func (s *PlanService) Pause(ctx context.Context, id string) error {
	plan, err := s.repo.Plans().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find plan: %w", err)
	}
	return s.repo.WithTx(ctx, func(txRepo repository.Repository) error {
		if err := txRepo.Plans().UpdateStatusAndNextRunAt(ctx, plan.ID, model.PlanStatusPaused, nil); err != nil {
			return fmt.Errorf("pause plan: %w", err)
		}
		pending, err := txRepo.Tasks().FindPendingByPlanID(ctx, plan.ID)
		if err != nil {
			return fmt.Errorf("find pending plan tasks: %w", err)
		}
		for _, task := range pending {
			if task == nil {
				continue
			}
			cancelled, err := txRepo.Tasks().CancelPendingTask(ctx, task.ID, "plan paused before task execution")
			if err != nil {
				return fmt.Errorf("cancel pending task %s: %w", task.ID, err)
			}
			if !cancelled {
				continue
			}
			if err := s.enqueuePlanPauseReversal(ctx, txRepo, task); err != nil {
				return fmt.Errorf("reverse admission for cancelled task %s: %w", task.ID, err)
			}
		}
		return nil
	})
}

func (s *PlanService) enqueuePlanPauseReversal(ctx context.Context, txRepo repository.Repository, task *model.Task) error {
	if task == nil || task.BillingChargeID == nil || strings.TrimSpace(*task.BillingChargeID) == "" {
		return nil
	}
	if s.billingWalletSvc == nil {
		return errors.New("fixed task billing wallet is not configured")
	}
	chargeID := strings.TrimSpace(*task.BillingChargeID)
	if _, err := txRepo.Billing().FindReversal(ctx, chargeID); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if _, err := txRepo.Billing().FindSettlementByKey(ctx, "task-terminal-reversal", chargeID); err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	_, err := s.billingWalletSvc.EnqueueSettlementInTx(ctx, txRepo, SettlementIntent{
		Action: model.BillingSettlementActionReverseTask,
		UserID: task.UserID, ResourceType: "task", ResourceID: task.ID,
		TaskID: task.ID, ChargeID: chargeID,
		CatalogID: task.BillingCatalogID, SKUID: task.BillingSKUID,
		Reason:             model.TaskBillingTerminalPlanPaused,
		RequestFingerprint: billingFingerprint("task-terminal-reversal", task.ID, chargeID, model.TaskBillingTerminalPlanPaused),
		IdempotencyScope:   "task-terminal-reversal", IdempotencyKey: chargeID,
	})
	return err
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
