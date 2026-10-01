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

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var (
	ErrUnsupportedPlanPlatform  = errors.New("plans are not supported for this project platform")
	ErrPlanUpdateConflict       = errors.New("plan changed concurrently")
	ErrCoverPortraitUnavailable = errors.New("portrait cover requires a supported task type, a project portrait reference, and an enabled cover")
)

// PlanService handles plan CRUD and lifecycle operations.
type PlanService struct {
	repo                repository.Repository
	logger              *zerolog.Logger
	referenceAssets     *ReferenceAssetService
	agentProfiles       *AgentProfileRegistry
	billingWalletSvc    *BillingWalletService
	hypitCapabilities   *HypitCapabilityService
	montageCapabilities *MontageCapabilityService
}

// CreatePlanEntryParams describes one Agent/channel execution in a Plan.
type CreatePlanEntryParams struct {
	UserID           string
	PlanID           string
	AgentID          string
	Channel          string
	TaskKind         string
	ExecutionProfile string
	AgentInput       map[string]any
	ImageDefaults    map[string]any
}

// UpdatePlanEntryParams uses pointer fields so omitted values retain the
// existing entry configuration.
type UpdatePlanEntryParams struct {
	UserID           string
	PlanID           string
	EntryID          string
	AgentID          *string
	Channel          *string
	TaskKind         *string
	ExecutionProfile *string
	AgentInput       *map[string]any
	ImageDefaults    *map[string]any
	Status           *string
}

func normalizePlanEntryParams(entry *CreatePlanEntryParams) {
	if entry == nil {
		return
	}
	entry.AgentID = strings.TrimSpace(entry.AgentID)
	entry.Channel = strings.TrimSpace(entry.Channel)
	entry.TaskKind = strings.TrimSpace(entry.TaskKind)
	entry.ExecutionProfile = strings.TrimSpace(entry.ExecutionProfile)
}

func validatePlanEntryIdentity(entry CreatePlanEntryParams) error {
	if entry.AgentID == "" || entry.Channel == "" || entry.TaskKind == "" {
		return fmt.Errorf("agent_id, channel, and task_kind are required")
	}
	expectedChannel, ok := model.AgentChannel(entry.AgentID)
	if !ok {
		return fmt.Errorf("unknown agent_id %q", entry.AgentID)
	}
	if expectedChannel != entry.Channel {
		return fmt.Errorf("agent %q is bound to channel %q", entry.AgentID, expectedChannel)
	}
	pack, ok := agentpack.Default().ForAgent(entry.AgentID)
	if !ok || pack.Kind != agentpack.KindManaged {
		return fmt.Errorf("unknown managed agent_id %q", entry.AgentID)
	}
	if !pack.SupportsTaskKind(entry.TaskKind) {
		return fmt.Errorf("unsupported task_kind %q", entry.TaskKind)
	}
	return nil
}

var (
	ErrPlanEntryNotFound  = errors.New("plan entry not found")
	ErrDuplicatePlanAgent = errors.New("plan already contains this Agent")
)

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

func (s *PlanService) planEntryPlan(ctx context.Context, userID, planID string) (*model.Plan, error) {
	plan, err := s.repo.Plans().FindByID(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("find plan: %w", err)
	}
	if plan.UserID != userID {
		return nil, fmt.Errorf("plan not owned by user")
	}
	return plan, nil
}

// CreateEntry adds one unique Agent entry to a plan.
func (s *PlanService) CreateEntry(ctx context.Context, p CreatePlanEntryParams) (*model.PlanEntry, error) {
	if strings.TrimSpace(p.PlanID) == "" || strings.TrimSpace(p.AgentID) == "" || strings.TrimSpace(p.Channel) == "" || strings.TrimSpace(p.TaskKind) == "" {
		return nil, fmt.Errorf("plan_id, agent_id, channel, and task_kind are required")
	}
	if strings.TrimSpace(p.ExecutionProfile) == "" {
		return nil, fmt.Errorf("execution_profile is required")
	}
	normalizePlanEntryParams(&p)
	if err := validatePlanEntryIdentity(p); err != nil {
		return nil, err
	}
	if _, err := s.planEntryPlan(ctx, p.UserID, p.PlanID); err != nil {
		return nil, err
	}
	if _, err := s.repo.PlanEntries().FindByPlanIDAndAgentID(ctx, p.PlanID, strings.TrimSpace(p.AgentID)); err == nil {
		return nil, ErrDuplicatePlanAgent
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("check existing plan entry: %w", err)
	}
	entry := &model.PlanEntry{
		ID: uuid.NewString(), PlanID: p.PlanID, AgentID: p.AgentID,
		Channel: p.Channel, TaskKind: p.TaskKind,
		ExecutionProfile: strings.TrimSpace(p.ExecutionProfile), Status: model.PlanEntryStatusActive,
	}
	entry.SetAgentInput(p.AgentInput)
	entry.SetImageDefaults(p.ImageDefaults)
	if err := s.repo.PlanEntries().Create(ctx, entry); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return nil, ErrDuplicatePlanAgent
		}
		return nil, fmt.Errorf("create plan entry: %w", err)
	}
	return entry, nil
}

func (s *PlanService) ListEntries(ctx context.Context, userID, planID string) ([]*model.PlanEntry, error) {
	if _, err := s.planEntryPlan(ctx, userID, planID); err != nil {
		return nil, err
	}
	return s.repo.PlanEntries().ListByPlanID(ctx, planID)
}

func (s *PlanService) UpdateEntry(ctx context.Context, p UpdatePlanEntryParams) (*model.PlanEntry, error) {
	entry, err := s.repo.PlanEntries().FindByID(ctx, p.EntryID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrPlanEntryNotFound
		}
		return nil, err
	}
	if _, err := s.planEntryPlan(ctx, p.UserID, entry.PlanID); err != nil {
		return nil, err
	}
	if p.AgentID != nil && strings.TrimSpace(*p.AgentID) != entry.AgentID {
		if expectedChannel, ok := model.AgentChannel(strings.TrimSpace(*p.AgentID)); !ok {
			return nil, fmt.Errorf("unknown agent_id %q", strings.TrimSpace(*p.AgentID))
		} else if entry.Channel != "" && expectedChannel != entry.Channel {
			return nil, fmt.Errorf("agent %q is bound to channel %q", strings.TrimSpace(*p.AgentID), expectedChannel)
		}
		if _, err := s.repo.PlanEntries().FindByPlanIDAndAgentID(ctx, entry.PlanID, strings.TrimSpace(*p.AgentID)); err == nil {
			return nil, ErrDuplicatePlanAgent
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		entry.AgentID = strings.TrimSpace(*p.AgentID)
	}
	if p.Channel != nil {
		entry.Channel = strings.TrimSpace(*p.Channel)
	}
	if p.TaskKind != nil {
		entry.TaskKind = strings.TrimSpace(*p.TaskKind)
	}
	if p.ExecutionProfile != nil {
		entry.ExecutionProfile = strings.TrimSpace(*p.ExecutionProfile)
	}
	if p.AgentInput != nil {
		entry.SetAgentInput(*p.AgentInput)
	}
	if p.ImageDefaults != nil {
		entry.SetImageDefaults(*p.ImageDefaults)
	}
	if p.Status != nil {
		entry.Status = strings.TrimSpace(*p.Status)
	}
	if expectedChannel, ok := model.AgentChannel(entry.AgentID); !ok {
		return nil, fmt.Errorf("unknown agent_id %q", entry.AgentID)
	} else if expectedChannel != entry.Channel {
		return nil, fmt.Errorf("agent %q is bound to channel %q", entry.AgentID, expectedChannel)
	}
	pack, ok := agentpack.Default().ForAgent(entry.AgentID)
	if !ok || pack.Kind != agentpack.KindManaged || !pack.SupportsTaskKind(entry.TaskKind) {
		return nil, fmt.Errorf("unsupported task_kind %q", entry.TaskKind)
	}
	if entry.AgentID == "" || entry.Channel == "" || entry.TaskKind == "" || entry.ExecutionProfile == "" {
		return nil, fmt.Errorf("agent_id, channel, task_kind, and execution_profile are required")
	}
	if err := s.repo.PlanEntries().Update(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *PlanService) DeleteEntry(ctx context.Context, userID, planID, entryID string) error {
	entry, err := s.repo.PlanEntries().FindByID(ctx, entryID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPlanEntryNotFound
		}
		return err
	}
	if strings.TrimSpace(planID) != "" && entry.PlanID != strings.TrimSpace(planID) {
		return ErrPlanEntryNotFound
	}
	if _, err := s.planEntryPlan(ctx, userID, entry.PlanID); err != nil {
		return err
	}
	return s.repo.PlanEntries().Delete(ctx, entryID)
}

func (s *PlanService) montageStorageProviderName() string {
	if s == nil || s.referenceAssets == nil || s.referenceAssets.store == nil {
		return ""
	}
	return s.referenceAssets.store.Name()
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
	CoverUsePortrait         bool
	MontageInput             *model.MontageInput
	HypitInput               *model.HypitInput
	InputAttachments         []model.EntryAttachment
	AgentInput               map[string]any
	// Entries are the independently executable Agent declarations for this
	// schedule. When present, each entry supplies its own execution profile and
	// identity; the legacy shared fields remain accepted for migrated plans.
	Entries []CreatePlanEntryParams
}

// Create validates the cron expression, resolves the project, computes the next run
// time, and persists the plan. The task type is derived from the project's platform.
// ImageCapabilityKey optionally selects a per-plan image model (validated upstream by the handler).
//
// HasContentImage / HasTailImage control seednote image composition on spawned
// tasks. nil falls back to the model's column defaults (content on, tail off).
func (s *PlanService) Create(ctx context.Context, p CreatePlanParams) (*model.Plan, error) {
	if strings.TrimSpace(p.ExecutionProfile) == "" && len(p.Entries) == 0 {
		return nil, fmt.Errorf("execution_profile is required")
	}
	if p.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if p.CronExpr == "" {
		return nil, fmt.Errorf("cron_expr is required")
	}
	if len(p.Entries) > 0 {
		seen := make(map[string]struct{}, len(p.Entries))
		for i := range p.Entries {
			entry := &p.Entries[i]
			if strings.TrimSpace(entry.ExecutionProfile) == "" {
				return nil, fmt.Errorf("entries[%d].execution_profile is required", i)
			}
			normalizePlanEntryParams(entry)
			if err := validatePlanEntryIdentity(*entry); err != nil {
				return nil, fmt.Errorf("entries[%d]: %w", i, err)
			}
			if _, exists := seen[entry.AgentID]; exists {
				return nil, ErrDuplicatePlanAgent
			}
			seen[entry.AgentID] = struct{}{}
		}
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
	// New channel-neutral projects derive task validation from their entries.
	// Legacy projects may still use the migration bridge on Project.Platform.
	validationTaskType := ""
	validationChannel := ""
	if len(p.Entries) > 0 {
		validationTaskType = legacyTaskTypeForIdentity(p.Entries[0].AgentID, p.Entries[0].Channel, p.Entries[0].TaskKind)
		validationChannel = p.Entries[0].Channel
	}
	effectivePlatform := strings.TrimSpace(project.Platform)
	if validationTaskType != "" {
		effectivePlatform = validationTaskType
	} else if effectivePlatform == "" {
		effectivePlatform = validationTaskType
	}
	legacySingleEntry := len(p.Entries) == 0
	if len(p.Entries) == 0 {
		requested := strings.TrimSpace(project.Platform)
		if requested == model.PlatformWechat {
			requested = model.TaskTypeWechatArticle
		}
		agentID, channel, taskKind, _, identityErr := resolveTaskIdentity(project, CreateManualParams{RequestedTaskType: requested})
		if identityErr != nil {
			return nil, identityErr
		}
		p.Entries = []CreatePlanEntryParams{{
			AgentID: agentID, Channel: channel, TaskKind: taskKind,
			ExecutionProfile: strings.TrimSpace(p.ExecutionProfile), AgentInput: p.AgentInput,
		}}
		validationTaskType = legacyTaskTypeForIdentity(agentID, channel, taskKind)
		validationChannel = channel
		effectivePlatform = validationTaskType
	}
	legacySingleEntry = legacySingleEntry || (len(p.Entries) == 1 && strings.TrimSpace(p.Entries[0].AgentID) != "")
	if effectivePlatform == "" {
		return nil, fmt.Errorf("plan requires at least one Agent entry")
	}
	if p.CoverUsePortrait && (!model.SupportsPortraitCover(effectivePlatform) || strings.TrimSpace(project.PortraitReferenceImageAssetID) == "") {
		return nil, ErrCoverPortraitUnavailable
	}
	var profile AgentExecutionProfile
	if strings.TrimSpace(p.ExecutionProfile) != "" {
		profile, err = resolveAgentProfileForUser(ctx, s.repo, s.agentProfiles, p.UserID, p.ExecutionProfile)
		if err != nil {
			return nil, err
		}
		p.ExecutionProfile = profile.ID
	}
	if p.ReferenceImageAssetID != "" {
		if s.referenceAssets == nil {
			return nil, ErrReferenceAssetUnavailable
		}
		if _, err := s.referenceAssets.RequireOwned(ctx, p.UserID, p.ReferenceImageAssetID, []string{DirectUploadPurposeTaskReference}); err != nil {
			return nil, err
		}
	}
	// Package-priced or one-off-only legacy platforms can't back plans. Reject up front
	// so API/MCP callers fail fast instead of creating schedules the task runner
	// should never execute for that platform.
	switch effectivePlatform {
	case model.PlatformEcommerce:
		return nil, fmt.Errorf("plans are not supported for e-commerce projects: %w", ErrUnsupportedPlanPlatform)
	case model.PlatformMoments:
		return nil, fmt.Errorf("plans are not supported for moments projects: %w", ErrUnsupportedPlanPlatform)
	}
	if effectivePlatform == model.PlatformWhiteboardAnimation {
		if err := validateWhiteboardAnimationInputs(p.InputAttachments); err != nil {
			return nil, err
		}
	}
	if legacySingleEntry && p.HypitInput != nil && !model.IsHypitPlatform(effectivePlatform) {
		return nil, ErrHypitInput
	}
	if legacySingleEntry && model.IsHypitPlatform(effectivePlatform) {
		if err := s.hypitCapabilities.NormalizeAndValidateInput(p.HypitInput, project.HypitDefaults.Data(), false); err != nil {
			return nil, err
		}
		if err := s.validateHypitPlanUploads(ctx, p.UserID, p.HypitInput); err != nil {
			return nil, err
		}
		if err := validateHypitTaskFiles(ctx, s.repo, p.UserID, p.ProjectID, s.montageStorageProviderName(), p.HypitInput, s.hypitCapabilities.config.Limits); err != nil {
			return nil, err
		}
	}
	if legacySingleEntry && p.MontageInput != nil && !model.IsMontagePlatform(effectivePlatform) {
		return nil, fmt.Errorf("%w: montage_input 只能用于视频生成计划", ErrMontageInput)
	}
	if legacySingleEntry && model.IsMontagePlatform(effectivePlatform) {
		if s.montageCapabilities != nil {
			var input *model.MontageInput
			if p.MontageInput != nil {
				copy := *p.MontageInput
				input = &copy
			}
			if err := s.montageCapabilities.NormalizeAndValidateInput(input, project.MontageDefaults.Data()); err != nil {
				return nil, err
			}
			if err := ValidateMaterializableMontageSourceTaskFiles(ctx, s.repo, s.montageStorageProviderName(), p.UserID, p.ProjectID, input.SourceAssets); err != nil {
				return nil, err
			}
			if err := ValidateMontageInlineBootstrapBudget(input, project, s.montageCapabilities.config.ToolPolicy, s.montageCapabilities.config.PipelineDefaults, p.InputAttachments); err != nil {
				return nil, err
			}
			p.MontageInput = input
		}
		if p.MontageInput == nil || strings.TrimSpace(p.MontageInput.Brief) == "" {
			return nil, fmt.Errorf("%w: 视频生成任务需要填写需求", ErrMontageInput)
		}
	}
	if validationTaskType == "" {
		validationTaskType = effectivePlatform
	}
	if validationTaskType == model.PlatformWechat {
		validationTaskType = model.TaskTypeWechatArticle
	}
	agentInput, err := validateAndCloneAgentInput(validationTaskType, p.AgentInput)
	if err != nil {
		return nil, err
	}
	effectiveImageRatio := strings.TrimSpace(p.ImageRatio)
	if legacySingleEntry && model.IsHypitPlatform(effectivePlatform) && p.CoverUsePortrait {
		effectiveImageRatio = hypitPortraitCoverRatio(p.HypitInput.Preferences)
	}
	if legacySingleEntry && model.IsMontagePlatform(effectivePlatform) && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" {
		effectiveImageRatio = strings.TrimSpace(project.ImageRatio)
	}
	if legacySingleEntry && model.IsMontagePlatform(effectivePlatform) && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" {
		if validationChannel != "" {
			effectiveImageRatio = model.DefaultImageRatioForChannel(validationChannel)
		} else {
			effectiveImageRatio = model.DefaultImageRatio(effectivePlatform)
		}
	}
	if validationChannel != "" && len(model.SupportedImageRatiosForChannel(validationChannel)) > 0 && !model.IsBusinessImageRatioAllowedForChannel(validationChannel, effectiveImageRatio) {
		return nil, fmt.Errorf("%s for channel %s: %s", model.ValidImageRatioHint, validationChannel, effectiveImageRatio)
	}
	if validationChannel == "" && len(model.SupportedImageRatios(effectivePlatform)) > 0 && !model.IsBusinessImageRatioAllowed(effectivePlatform, effectiveImageRatio) {
		return nil, fmt.Errorf("%s for channel %s: %s", model.ValidImageRatioHint, effectivePlatform, effectiveImageRatio)
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
	if p.CoverUsePortrait && (effectivePlatform == model.PlatformWechat || effectivePlatform == model.ChannelArticle) && !articleCover {
		return nil, ErrCoverPortraitUnavailable
	}
	// A plan carries scheduling-adjacent "what to produce" image params.
	// Project/account style config is snapshotted when a task is spawned.
	plan := &model.Plan{
		ID:        uuid.New().String(),
		UserID:    p.UserID,
		ProjectID: p.ProjectID,
		Type: func() string {
			if !legacySingleEntry {
				return ""
			}
			if project.Platform == model.PlatformWechat {
				return model.TaskTypeWechatArticle
			}
			return project.Platform
		}(),
		ExecutionProfile:         strings.TrimSpace(p.ExecutionProfile),
		CronExpr:                 p.CronExpr,
		Prompt:                   p.Prompt,
		Status:                   model.PlanStatusActive,
		NextRunAt:                nextRun,
		ImageCapabilityKey:       effectiveImageCapabilityKey,
		ImageRatio:               effectiveImageRatio,
		ReferenceImageAssetID:    p.ReferenceImageAssetID,
		SkipReferenceImage:       p.SkipReferenceImage != nil && *p.SkipReferenceImage,
		Watermark:                p.Watermark != nil && *p.Watermark,
		HasContentImage:          hasContent,
		HasTailImage:             hasTail,
		ArticleWithCover:         &articleCover,
		ArticleWithContentImages: &articleContent,
		CoverUsePortrait:         p.CoverUsePortrait,
	}
	plan.SetInputAttachments(cloneEntryAttachments(p.InputAttachments))
	if agentInput != nil {
		plan.SetAgentInput(agentInput)
	}
	if p.HypitInput != nil {
		plan.SetHypitInput(*p.HypitInput)
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
		if err := tx.Plans().Create(ctx, plan); err != nil {
			return err
		}
		for _, params := range p.Entries {
			entry := &model.PlanEntry{ID: uuid.NewString(), PlanID: plan.ID, AgentID: params.AgentID, Channel: params.Channel, TaskKind: params.TaskKind, ExecutionProfile: params.ExecutionProfile, Status: model.PlanEntryStatusActive}
			entry.SetAgentInput(params.AgentInput)
			entry.SetImageDefaults(params.ImageDefaults)
			if err := tx.PlanEntries().Create(ctx, entry); err != nil {
				return fmt.Errorf("create plan entry: %w", err)
			}
			plan.Entries = append(plan.Entries, entry)
		}
		return nil
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
	Watermark                *bool
	HasContentImage          *bool
	HasTailImage             *bool
	ArticleWithCover         *bool
	ArticleWithContentImages *bool
	CoverUsePortrait         *bool
	MontageInput             *model.MontageInput
	HypitInput               *model.HypitInput
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
	if p.CoverUsePortrait != nil {
		plan.CoverUsePortrait = *p.CoverUsePortrait
	}
	if plan.CoverUsePortrait {
		if !model.SupportsPortraitCover(plan.Type) || ((plan.Type == model.TaskTypeWechatArticle || plan.Type == model.TaskTypeWechatPicture) && plan.ArticleWithCover != nil && !*plan.ArticleWithCover) {
			return nil, ErrCoverPortraitUnavailable
		}
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(project.PortraitReferenceImageAssetID) == "" {
			return nil, ErrCoverPortraitUnavailable
		}
	}
	if p.InputAttachments != nil {
		plan.SetInputAttachments(cloneEntryAttachments(*p.InputAttachments))
	}
	if plan.Type == model.PlatformWhiteboardAnimation {
		if err := validateWhiteboardAnimationInputs(plan.InputAttachments.Data()); err != nil {
			return nil, err
		}
	}
	if p.AgentInput != nil {
		agentInput, err := validateAndCloneAgentInput(plan.Type, *p.AgentInput)
		if err != nil {
			return nil, err
		}
		plan.SetAgentInput(agentInput)
	}
	if p.HypitInput != nil || model.IsHypitPlatform(plan.Type) {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, err
		}
		if !model.IsHypitPlatform(project.Platform) {
			return nil, ErrHypitInput
		}
		in := plan.HypitInput.Data()
		if p.HypitInput != nil {
			in = *p.HypitInput
		}
		if err := s.hypitCapabilities.NormalizeAndValidateInput(&in, project.HypitDefaults.Data(), false); err != nil {
			return nil, err
		}
		if err := s.validateHypitPlanUploads(ctx, plan.UserID, &in); err != nil {
			return nil, err
		}
		if err := validateHypitTaskFiles(ctx, s.repo, plan.UserID, plan.ProjectID, s.montageStorageProviderName(), &in, s.hypitCapabilities.config.Limits); err != nil {
			return nil, err
		}
		plan.SetHypitInput(in)
		if plan.CoverUsePortrait {
			plan.ImageRatio = hypitPortraitCoverRatio(in.Preferences)
		}
	}
	if p.MontageInput != nil || (model.IsMontagePlatform(plan.Type) && s.montageCapabilities != nil) {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find project: %w", err)
		}
		if !model.IsMontagePlatform(project.Platform) {
			return nil, fmt.Errorf("%w: montage_input 只能用于视频生成计划", ErrMontageInput)
		}
		candidate := plan.MontageInput.Data()
		if p.MontageInput != nil {
			candidate = *p.MontageInput
		}
		if s.montageCapabilities != nil {
			if err := s.montageCapabilities.NormalizeAndValidateInput(&candidate, project.MontageDefaults.Data()); err != nil {
				return nil, err
			}
			if err := ValidateMaterializableMontageSourceTaskFiles(ctx, s.repo, s.montageStorageProviderName(), plan.UserID, plan.ProjectID, candidate.SourceAssets); err != nil {
				return nil, err
			}
			if err := ValidateMontageInlineBootstrapBudget(&candidate, project, s.montageCapabilities.config.ToolPolicy, s.montageCapabilities.config.PipelineDefaults, plan.InputAttachments.Data()); err != nil {
				return nil, err
			}
		}
		if strings.TrimSpace(candidate.Brief) == "" {
			return nil, fmt.Errorf("%w: 视频生成任务需要填写需求", ErrMontageInput)
		}
		if p.MontageInput != nil {
			plan.SetMontageInput(candidate)
		}
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
