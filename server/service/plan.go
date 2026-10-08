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
	pack, ok := agentpack.Default().ForAgent(entry.AgentID)
	if !ok || pack.Kind != agentpack.KindManaged {
		return fmt.Errorf("unknown managed agent_id %q", entry.AgentID)
	}
	if strings.TrimSpace(pack.Channel) != strings.TrimSpace(entry.Channel) {
		return fmt.Errorf("agent %q is bound to channel %q", entry.AgentID, pack.Channel)
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

func planEntriesFromAgentIDs(agentIDs []string, executionProfile string) ([]CreatePlanEntryParams, error) {
	profile := strings.TrimSpace(executionProfile)
	if profile == "" {
		return nil, fmt.Errorf("execution_profile is required")
	}
	entries := make([]CreatePlanEntryParams, 0, len(agentIDs))
	seen := make(map[string]struct{}, len(agentIDs))
	for _, rawID := range agentIDs {
		agentID := strings.TrimSpace(rawID)
		if agentID == "" {
			return nil, fmt.Errorf("agent_ids must not contain empty values")
		}
		if _, exists := seen[agentID]; exists {
			return nil, ErrDuplicatePlanAgent
		}
		seen[agentID] = struct{}{}
		pack, ok := agentpack.Default().ForAgent(agentID)
		if !ok || !pack.SupportsPlan() {
			return nil, fmt.Errorf("agent_id %q is not available for plans", agentID)
		}
		entries = append(entries, CreatePlanEntryParams{
			AgentID: agentID, Channel: strings.TrimSpace(pack.Channel),
			TaskKind: strings.TrimSpace(pack.PlanTaskKind), ExecutionProfile: profile,
		})
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("at least one agent_id is required")
	}
	return entries, nil
}

func syncPlanEntries(ctx context.Context, repo repository.Repository, planID string, desired []CreatePlanEntryParams) error {
	existing, err := repo.PlanEntries().ListByPlanID(ctx, planID)
	if err != nil {
		return fmt.Errorf("list plan entries: %w", err)
	}
	byAgent := make(map[string]*model.PlanEntry, len(existing))
	for _, entry := range existing {
		if entry != nil {
			byAgent[entry.AgentID] = entry
		}
	}
	keep := make(map[string]struct{}, len(desired))
	for _, params := range desired {
		keep[params.AgentID] = struct{}{}
		if entry := byAgent[params.AgentID]; entry != nil {
			entry.Channel = params.Channel
			entry.TaskKind = params.TaskKind
			entry.ExecutionProfile = params.ExecutionProfile
			entry.Status = model.PlanEntryStatusActive
			if err := repo.PlanEntries().Update(ctx, entry); err != nil {
				return fmt.Errorf("update plan entry %s: %w", entry.ID, err)
			}
			continue
		}
		entry := &model.PlanEntry{
			ID: uuid.NewString(), PlanID: planID, AgentID: params.AgentID,
			Channel: params.Channel, TaskKind: params.TaskKind,
			ExecutionProfile: params.ExecutionProfile, Status: model.PlanEntryStatusActive,
		}
		entry.SetAgentInput(params.AgentInput)
		entry.SetImageDefaults(params.ImageDefaults)
		if err := repo.PlanEntries().Create(ctx, entry); err != nil {
			return fmt.Errorf("create plan entry: %w", err)
		}
	}
	for _, entry := range existing {
		if entry == nil {
			continue
		}
		if _, ok := keep[entry.AgentID]; !ok {
			if err := repo.PlanEntries().Delete(ctx, entry.ID); err != nil {
				return fmt.Errorf("delete plan entry %s: %w", entry.ID, err)
			}
		}
	}
	return nil
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
		candidate := strings.TrimSpace(*p.AgentID)
		pack, ok := agentpack.Default().ForAgent(candidate)
		if !ok || pack.Kind != agentpack.KindManaged {
			return nil, fmt.Errorf("unknown managed agent_id %q", candidate)
		}
		if strings.TrimSpace(pack.Channel) != strings.TrimSpace(entry.Channel) {
			return nil, fmt.Errorf("agent %q is bound to channel %q", candidate, pack.Channel)
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
	pack, ok := agentpack.Default().ForAgent(entry.AgentID)
	if !ok || pack.Kind != agentpack.KindManaged || strings.TrimSpace(pack.Channel) != strings.TrimSpace(entry.Channel) || !pack.SupportsTaskKind(entry.TaskKind) {
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
	UserID                        string
	ProjectID                     string
	ExecutionProfile              string
	CronExpr                      string
	Prompt                        string
	ImageCapabilityKey            string
	ImageRatio                    string
	SkipReferenceImage            *bool
	ReferenceImageAssetID         string
	PortraitReferenceImageAssetID *string
	CoverUsePortrait              bool
	Watermark                     *bool
	InputAttachments              []model.EntryAttachment
	// AgentIDs is the public plan selection. Channel and task kind are resolved
	// from the server-owned Agent Pack Catalog.
	AgentIDs []string
	// Entries are the independently executable Agent declarations for this
	// schedule. They are populated by the server from AgentIDs for public calls;
	// migration code may provide fully resolved entries directly.
	Entries []CreatePlanEntryParams
}

func validatePlanPortraitSelection(portraitAssetID string, enabled bool, entries []CreatePlanEntryParams) error {
	if !enabled {
		return nil
	}
	if strings.TrimSpace(portraitAssetID) == "" {
		return ErrCoverPortraitUnavailable
	}
	for _, entry := range entries {
		if planEntryUsesPortraitCover(enabled, entry.AgentID, entry.Channel, entry.TaskKind) {
			return nil
		}
	}
	return ErrCoverPortraitUnavailable
}

func planEntryUsesPortraitCover(enabled bool, agentID, channel, taskKind string) bool {
	return enabled && model.SupportsPortraitCover(taskTypeForIdentity(agentID, channel, taskKind))
}

// Create validates the public schedule contract, resolves the project, computes
// the next run time, and persists the plan plus its independently executable
// entries. Task identity always comes from AgentIDs or explicit migration
// entries; project platform is context only.
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
	if len(p.AgentIDs) > 0 {
		entries, err := planEntriesFromAgentIDs(p.AgentIDs, p.ExecutionProfile)
		if err != nil {
			return nil, err
		}
		p.Entries = entries
	}
	if len(p.Entries) == 0 {
		return nil, fmt.Errorf("at least one agent_id is required")
	}
	if strings.TrimSpace(p.ExecutionProfile) == "" {
		p.ExecutionProfile = strings.TrimSpace(p.Entries[0].ExecutionProfile)
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

	// Load the project for ownership/context only. Its platform never determines
	// the task identity or the set of outputs in a plan.
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
	effectivePortraitAssetID := strings.TrimSpace(project.PortraitReferenceImageAssetID)
	if p.PortraitReferenceImageAssetID != nil {
		effectivePortraitAssetID = strings.TrimSpace(*p.PortraitReferenceImageAssetID)
	}
	if effectivePortraitAssetID != "" {
		if s.referenceAssets == nil {
			return nil, ErrReferenceAssetUnavailable
		}
		if _, err := s.referenceAssets.RequireOwned(ctx, p.UserID, effectivePortraitAssetID, []string{DirectUploadPurposeProjectPortraitReference}); err != nil {
			return nil, err
		}
	}
	if err := validatePlanPortraitSelection(effectivePortraitAssetID, p.CoverUsePortrait, p.Entries); err != nil {
		return nil, err
	}
	var profile AgentExecutionProfile
	profile, err = resolveAgentProfileForUser(ctx, s.repo, s.agentProfiles, p.UserID, p.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	p.ExecutionProfile = profile.ID
	if p.ReferenceImageAssetID != "" {
		if s.referenceAssets == nil {
			return nil, ErrReferenceAssetUnavailable
		}
		if _, err := s.referenceAssets.RequireOwned(ctx, p.UserID, p.ReferenceImageAssetID, []string{DirectUploadPurposeTaskReference}); err != nil {
			return nil, err
		}
	}
	effectiveImageRatio := strings.TrimSpace(p.ImageRatio)
	if effectiveImageRatio == "" {
		effectiveImageRatio = strings.TrimSpace(project.ImageRatio)
	}
	if effectiveImageRatio == "" {
		effectiveImageRatio = model.ImageRatioAuto
	}
	if !model.ValidImageRatios[effectiveImageRatio] {
		return nil, fmt.Errorf("%s: %s", model.ValidImageRatioHint, effectiveImageRatio)
	}
	effectiveImageCapabilityKey := strings.TrimSpace(p.ImageCapabilityKey)

	nextRun, err := s.computeNextRun(p.CronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	// A plan carries scheduling-adjacent "what to produce" image params.
	// Project/account style config is snapshotted when a task is spawned.
	plan := &model.Plan{
		ID:                            uuid.New().String(),
		UserID:                        p.UserID,
		ProjectID:                     p.ProjectID,
		ExecutionProfile:              strings.TrimSpace(p.ExecutionProfile),
		CronExpr:                      p.CronExpr,
		Prompt:                        p.Prompt,
		Status:                        model.PlanStatusActive,
		NextRunAt:                     nextRun,
		ImageCapabilityKey:            effectiveImageCapabilityKey,
		ImageRatio:                    effectiveImageRatio,
		ReferenceImageAssetID:         p.ReferenceImageAssetID,
		PortraitReferenceImageAssetID: effectivePortraitAssetID,
		PortraitReferenceConfigured:   true,
		CoverUsePortrait:              p.CoverUsePortrait,
		SkipReferenceImage:            p.SkipReferenceImage != nil && *p.SkipReferenceImage,
		Watermark:                     p.Watermark != nil && *p.Watermark,
	}
	plan.SetInputAttachments(cloneEntryAttachments(p.InputAttachments))

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
	plan.Entries, err = s.repo.PlanEntries().ListByPlanID(ctx, plan.ID)
	if err != nil {
		return nil, fmt.Errorf("list plan entries: %w", err)
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
	for _, plan := range plans {
		plan.Entries, err = s.repo.PlanEntries().ListByPlanID(ctx, plan.ID)
		if err != nil {
			return nil, 0, fmt.Errorf("list plan entries: %w", err)
		}
	}
	return plans, total, nil
}

// UpdatePlanParams holds the inputs for PlanService.Update. Pointer-typed fields
// use leave-unchanged semantics:
//   - ImageCapabilityKey: nil = leave unchanged; &"" = clear to the configured default
//   - SkipReferenceImage: nil = leave unchanged; &true/&false = set
//   - ReferenceImageAssetID: nil = leave unchanged; &"" = clear; &"value" = set
//   - Watermark: nil = leave unchanged; &true/&false = set
//
// ID, CronExpr, and Prompt are plain strings. CronExpr=="" means "leave
// unchanged"; an empty Prompt is a valid value meaning "no prompt".
type UpdatePlanParams struct {
	ID                            string
	ExecutionProfile              string
	CronExpr                      string
	Prompt                        string
	ImageCapabilityKey            *string
	ImageRatio                    *string
	SkipReferenceImage            *bool
	ReferenceImageAssetID         *string
	PortraitReferenceImageAssetID *string
	CoverUsePortrait              *bool
	Watermark                     *bool
	InputAttachments              *[]model.EntryAttachment
	// AgentIDs replaces the complete output selection when non-nil.
	AgentIDs *[]string
}

// Update modifies a plan's fields per UpdatePlanParams. If the cron expression
// changed, next_run_at is recomputed. See UpdatePlanParams for field semantics.
func (s *PlanService) Update(ctx context.Context, p UpdatePlanParams) (*model.Plan, error) {
	return s.updatePlan(ctx, p, nil)
}

func (s *PlanService) UpdateIfReferenceImageAssetID(ctx context.Context, p UpdatePlanParams, expectedID string) (*model.Plan, error) {
	return s.updatePlan(ctx, p, &expectedID)
}

func (s *PlanService) updatePlan(ctx context.Context, p UpdatePlanParams, expectedID *string) (*model.Plan, error) {
	plan, scheduleChanged, err := s.preparePlanUpdate(ctx, p)
	if err != nil {
		return nil, err
	}
	var desired []CreatePlanEntryParams
	if p.AgentIDs != nil {
		desired, err = planEntriesFromAgentIDs(*p.AgentIDs, plan.ExecutionProfile)
		if err != nil {
			return nil, err
		}
	}
	if expectedID != nil && plan.ReferenceImageAssetID != *expectedID {
		return nil, ErrPlanUpdateConflict
	}
	err = s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if expectedID == nil {
			if err := tx.Plans().UpdateEditable(ctx, plan, scheduleChanged); err != nil {
				return fmt.Errorf("update plan: %w", err)
			}
		} else {
			won, err := tx.Plans().UpdateEditableIfReferenceImageAssetID(ctx, plan, *expectedID, scheduleChanged)
			if err != nil {
				return fmt.Errorf("update plan: %w", err)
			}
			if !won {
				return ErrPlanUpdateConflict
			}
		}
		if p.AgentIDs != nil {
			if err := syncPlanEntries(ctx, tx, plan.ID, desired); err != nil {
				return err
			}
		} else {
			entries, err := tx.PlanEntries().ListByPlanID(ctx, plan.ID)
			if err != nil {
				return err
			}
			for _, entry := range entries {
				entry.ExecutionProfile = plan.ExecutionProfile
				if err := tx.PlanEntries().Update(ctx, entry); err != nil {
					return err
				}
			}
		}
		plan.Entries, err = tx.PlanEntries().ListByPlanID(ctx, plan.ID)
		return err
	})
	if err != nil {
		return nil, err
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
	if p.PortraitReferenceImageAssetID != nil {
		portraitID := strings.TrimSpace(*p.PortraitReferenceImageAssetID)
		if portraitID != "" {
			if s.referenceAssets == nil {
				return nil, ErrReferenceAssetUnavailable
			}
			if _, err := s.referenceAssets.RequireOwned(ctx, plan.UserID, portraitID, []string{DirectUploadPurposeProjectPortraitReference}); err != nil {
				return nil, err
			}
		}
		plan.PortraitReferenceImageAssetID = portraitID
		plan.PortraitReferenceConfigured = true
	}
	if p.CoverUsePortrait != nil {
		plan.CoverUsePortrait = *p.CoverUsePortrait
		if plan.CoverUsePortrait && !plan.PortraitReferenceConfigured {
			project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
			if err != nil {
				return nil, fmt.Errorf("find project for portrait default: %w", err)
			}
			plan.PortraitReferenceImageAssetID = strings.TrimSpace(project.PortraitReferenceImageAssetID)
			plan.PortraitReferenceConfigured = true
		}
	}
	portraitIDForValidation := plan.PortraitReferenceImageAssetID
	if plan.CoverUsePortrait && !plan.PortraitReferenceConfigured {
		project, err := s.repo.Projects().FindByID(ctx, plan.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("find project for portrait default: %w", err)
		}
		portraitIDForValidation = strings.TrimSpace(project.PortraitReferenceImageAssetID)
	}
	var entries []CreatePlanEntryParams
	if p.AgentIDs != nil {
		entries, err = planEntriesFromAgentIDs(*p.AgentIDs, plan.ExecutionProfile)
	} else {
		var existing []*model.PlanEntry
		existing, err = s.repo.PlanEntries().ListByPlanID(ctx, plan.ID)
		if err == nil {
			entries = make([]CreatePlanEntryParams, 0, len(existing))
			for _, entry := range existing {
				if entry != nil {
					entries = append(entries, CreatePlanEntryParams{AgentID: entry.AgentID, Channel: entry.Channel, TaskKind: entry.TaskKind, ExecutionProfile: entry.ExecutionProfile})
				}
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("resolve plan entries for portrait cover: %w", err)
	}
	if err := validatePlanPortraitSelection(portraitIDForValidation, plan.CoverUsePortrait, entries); err != nil {
		return nil, err
	}
	if p.ImageRatio != nil {
		nextRatio := strings.TrimSpace(*p.ImageRatio)
		if nextRatio != "" && !model.ValidImageRatios[nextRatio] {
			return nil, fmt.Errorf("%s: %s", model.ValidImageRatioHint, nextRatio)
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
	if p.InputAttachments != nil {
		plan.SetInputAttachments(cloneEntryAttachments(*p.InputAttachments))
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
