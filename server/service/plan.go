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

// Create validates the cron expression, computes the next run time, and persists the plan.
func (s *PlanService) Create(
	ctx context.Context,
	userID, planType, title, description, cronExpr, topicHint string,
) (*model.Plan, error) {
	if planType == "" {
		return nil, fmt.Errorf("type is required")
	}
	if cronExpr == "" {
		return nil, fmt.Errorf("cron_expr is required")
	}

	nextRun, err := s.computeNextRun(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression: %w", err)
	}

	plan := &model.Plan{
		ID:          uuid.New().String(),
		UserID:      userID,
		Type:        planType,
		Title:       title,
		Description: description,
		CronExpr:    cronExpr,
		TopicHint:   topicHint,
		Status:      model.PlanStatusActive,
		NextRunAt:   nextRun,
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

// List returns plans for a user with pagination. Returns plans and total count.
func (s *PlanService) List(ctx context.Context, userID string, offset, limit int) ([]*model.Plan, int64, error) {
	plans, err := s.repo.Plans().FindByUserID(ctx, userID, offset, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("list plans: %w", err)
	}

	total, err := s.repo.Plans().CountByUserID(ctx, userID)
	if err != nil {
		return nil, 0, fmt.Errorf("count plans: %w", err)
	}

	return plans, total, nil
}

// Update modifies a plan's fields. If the cron expression changed, next_run_at is recomputed.
func (s *PlanService) Update(
	ctx context.Context,
	id, title, description, cronExpr, topicHint string,
) (*model.Plan, error) {
	plan, err := s.repo.Plans().FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find plan: %w", err)
	}

	plan.Title = title
	plan.Description = description
	plan.TopicHint = topicHint

	// If cron expression changed, validate and recompute next run.
	if cronExpr != "" && cronExpr != plan.CronExpr {
		if _, err := cron.ParseStandard(cronExpr); err != nil {
			return nil, fmt.Errorf("invalid cron expression: %w", err)
		}
		plan.CronExpr = cronExpr
		nextRun, err := s.computeNextRun(cronExpr)
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
