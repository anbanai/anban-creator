package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// PlanService handles plan CRUD and lifecycle operations.
type PlanService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewPlanService creates a new PlanService.
func NewPlanService(repo repository.Repository, logger *zerolog.Logger) *PlanService {
	return &PlanService{repo: repo, logger: logger}
}

// CreatePlanParams holds the inputs for PlanService.Create. Pointer-typed optional
// fields use the same nil-means-default / nil-means-unchanged semantics as the
// underlying model. Struct form keeps call sites readable as fields are added
// and prevents argument-order bugs on a signature that has grown past a dozen
// positional params.
type CreatePlanParams struct {
	UserID             string
	ChannelID          string
	CronExpr           string
	Prompt             string
	ImageModelKey      string
	SkipReferenceImage *bool
	ReferenceImageURL  string
	Style              string
	Watermark          *bool
	Goal               string
	GoalMode           bool
	// TemplateID records the template selected during plan creation. Propagated to
	// spawned tasks by CreateFromPlan so the agent can surface the template's
	// content scaffold via get_channel_profile(task_id). nil = no template.
	TemplateID *string
	// HasContentImage / HasTailImage: seednote image composition (cover always
	// generated). nil → fall back to plan model defaults (content on, tail off);
	// non-nil honors explicit user choice.
	HasContentImage *bool
	HasTailImage    *bool
}

// Create validates the cron expression, resolves the channel, computes the next run
// time, and persists the plan. The task type is derived from the channel's platform.
// ImageModelKey optionally selects a per-plan image model (validated upstream by the handler).
//
// Goal and GoalMode propagate to tasks spawned from this plan; when GoalMode is
// true, spawned tasks charge ×GoalMultiplier upfront and evaluate the goal
// after each execution.
//
// HasContentImage / HasTailImage control seednote image composition on spawned
// tasks. nil falls back to the model's column defaults (content on, tail off).
func (s *PlanService) Create(ctx context.Context, p CreatePlanParams) (*model.Plan, error) {
	if p.ChannelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}
	if p.CronExpr == "" {
		return nil, fmt.Errorf("cron_expr is required")
	}

	// Load channel to derive type and validate ownership.
	channel, err := s.repo.Channels().FindByID(ctx, p.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if channel.UserID != p.UserID {
		return nil, fmt.Errorf("channel not owned by user")
	}
	if channel.Status != model.ChannelStatusActive {
		return nil, fmt.Errorf("channel is not active")
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

	plan := &model.Plan{
		ID:                 uuid.New().String(),
		UserID:             p.UserID,
		ChannelID:          p.ChannelID,
		Type:               channel.Platform,
		CronExpr:           p.CronExpr,
		Prompt:             p.Prompt,
		Status:             model.PlanStatusActive,
		NextRunAt:          nextRun,
		ImageModelKey:      p.ImageModelKey,
		ReferenceImageURL:  p.ReferenceImageURL,
		Style:              p.Style,
		TemplateID:         p.TemplateID,
		SkipReferenceImage: p.SkipReferenceImage != nil && *p.SkipReferenceImage,
		Watermark:          p.Watermark != nil && *p.Watermark,
		Goal:               p.Goal,
		GoalMode:           p.GoalMode,
		HasContentImage:    hasContent,
		HasTailImage:       hasTail,
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

// List returns plans for a user with optional channel filter and pagination.
// Returns plans and total count.
func (s *PlanService) List(ctx context.Context, userID string, offset, limit int, channelID string) ([]*model.Plan, int64, error) {
	plans, err := s.repo.Plans().FindByUserID(ctx, userID, channelID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list plans: %w", err)
	}

	total, err := s.repo.Plans().CountByUserID(ctx, userID, channelID)
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
//   - ReferenceImageURL: nil = leave unchanged; &"" = clear; &"value" = set
//   - Style: nil = leave unchanged; &"" = clear; &"value" = set
//   - TemplateID: nil = leave unchanged; &"" = clear; &"value" = set
//   - Watermark: nil = leave unchanged; &true/&false = set
//   - GoalMode: nil = leave unchanged; &true/&false = set
//   - HasContentImage / HasTailImage: nil = leave unchanged; &true/&false = set
//
// ID, CronExpr, Prompt, and Goal are plain strings. CronExpr=="" means "leave
// unchanged"; empty Prompt/Goal is a valid value meaning "no prompt / no goal".
type UpdatePlanParams struct {
	ID                 string
	CronExpr           string
	Prompt             string
	ImageModelKey      *string
	SkipReferenceImage *bool
	ReferenceImageURL  *string
	Style              *string
	Watermark          *bool
	Goal               string
	GoalMode           *bool
	// TemplateID: nil = leave unchanged; &"" = clear; &"value" = set.
	TemplateID      *string
	HasContentImage *bool
	HasTailImage    *bool
}

// Update modifies a plan's fields per UpdatePlanParams. If the cron expression
// changed, next_run_at is recomputed. See UpdatePlanParams for field semantics.
func (s *PlanService) Update(ctx context.Context, p UpdatePlanParams) (*model.Plan, error) {
	plan, err := s.repo.Plans().FindByID(ctx, p.ID)
	if err != nil {
		return nil, fmt.Errorf("find plan: %w", err)
	}

	plan.Prompt = p.Prompt
	if p.ReferenceImageURL != nil {
		plan.ReferenceImageURL = *p.ReferenceImageURL
	}
	plan.Goal = p.Goal
	if p.Style != nil {
		plan.Style = *p.Style
	}
	if p.TemplateID != nil {
		plan.TemplateID = p.TemplateID
	}
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

	// If cron expression changed, validate and recompute next run.
	if p.CronExpr != "" && p.CronExpr != plan.CronExpr {
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

	if err := s.repo.Plans().Update(ctx, plan); err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
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

	plan.Status = model.PlanStatusPaused
	plan.NextRunAt = nil

	if err := s.repo.Plans().Update(ctx, plan); err != nil {
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

	plan.Status = model.PlanStatusActive
	nextRun, err := s.computeNextRun(plan.CronExpr)
	if err != nil {
		return fmt.Errorf("compute next run: %w", err)
	}
	plan.NextRunAt = nextRun

	if err := s.repo.Plans().Update(ctx, plan); err != nil {
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
