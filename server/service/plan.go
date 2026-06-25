package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/app/writer"
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
	ProjectID          string
	CronExpr           string
	Prompt             string
	ImageModelKey      string
	SkipReferenceImage *bool
	ReferenceImageURL  string
	Style              string
	// WritingStyle / Theme carry the plan-level 写作风格 / 排版样式 (the other two
	// orthogonal dimensions). Resolved alongside Style with precedence
	// plan > template > project, then copied to spawned tasks by CreateFromPlan.
	WritingStyle string
	Theme        string
	// Author / AuthorStyleIntro / AuthorAvatarURL carry the plan-level 作者（署名）
	// + 写作风格（free-text imitation） + 可选人设头像 overrides. Resolved alongside
	// Style/WritingStyle/Theme with precedence plan > template > project, then
	// copied to Task by CreateFromPlan (task-level override wins).
	Author           string
	AuthorStyleIntro string
	AuthorAvatarURL  string
	Watermark        *bool
	Goal             string
	GoalMode         bool
	// TemplateID records the template selected during plan creation. Propagated to
	// spawned tasks by CreateFromPlan so the agent can surface the template's
	// content scaffold via get_project_profile(task_id). nil = no template.
	TemplateID *string
	// HasContentImage / HasTailImage: seednote image composition (cover always
	// generated). nil → fall back to plan model defaults (content on, tail off);
	// non-nil honors explicit user choice.
	HasContentImage *bool
	HasTailImage    *bool
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
	// E-commerce projects can't back plans: e-commerce tasks are package-priced
	// (sum of selected modules) and require per-task product photos + module
	// selection, none of which a plan can supply. Reject up front so an
	// API/legacy plan referencing an e-commerce project fails fast here instead
	// of silently no-op'ing (and re-firing every check) at spawn time — see
	// TaskService.CreateFromPlan, where taskType resolves to the project platform.
	if project.Platform == model.PlatformEcommerce {
		return nil, fmt.Errorf("plans are not supported for e-commerce projects")
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

	// Resolve the three orthogonal style dimensions with precedence
	// plan > template > project. Each dimension is independent — the writer never
	// drives the visual style. A missing template is logged and treated as "no
	// template override" so a stale template_id never blocks plan creation.
	var tmpl *model.Template
	if p.TemplateID != nil && *p.TemplateID != "" {
		if t, terr := s.repo.Templates().FindByID(ctx, *p.TemplateID); terr == nil {
			tmpl = t
		} else {
			s.logger.Warn().Err(terr).Str("template_id", *p.TemplateID).Msg("template not found during style resolution")
		}
	}
	effectiveVisual := firstNonEmpty(p.Style, templateVisual(tmpl), project.Style)
	effectiveWriter := firstNonEmpty(p.WritingStyle, templateWritingStyle(tmpl), project.WritingStyle)
	effectiveTheme := firstNonEmpty(p.Theme, templateTheme(tmpl), project.Theme)
	// 公众号人设维度（作者署名 + 写作风格模仿 + 可选头像），与视觉/排版正交，同链解析。
	effectiveAuthor := firstNonEmpty(p.Author, templateAuthorName(tmpl), project.Author)
	effectiveAuthorIntro := firstNonEmpty(p.AuthorStyleIntro, templateAuthorStyleIntro(tmpl), project.AuthorStyleIntro)
	effectiveAuthorAvatar := firstNonEmpty(p.AuthorAvatarURL, templateAuthorAvatar(tmpl), project.AuthorAvatarURL)
	if effectiveWriter == "" && project.Platform == model.PlatformArticle {
		effectiveWriter = writer.DefaultStyleName
	}

	plan := &model.Plan{
		ID:                 uuid.New().String(),
		UserID:             p.UserID,
		ProjectID:          p.ProjectID,
		Type:               project.Platform,
		CronExpr:           p.CronExpr,
		Prompt:             p.Prompt,
		Status:             model.PlanStatusActive,
		NextRunAt:          nextRun,
		ImageModelKey:      p.ImageModelKey,
		ReferenceImageURL:  p.ReferenceImageURL,
		Style:              effectiveVisual,
		WritingStyle:       effectiveWriter,
		Theme:              effectiveTheme,
		Author:             effectiveAuthor,
		AuthorStyleIntro:   effectiveAuthorIntro,
		AuthorAvatarURL:    effectiveAuthorAvatar,
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
//   - ReferenceImageURL: nil = leave unchanged; &"" = clear; &"value" = set
//   - Style: nil = leave unchanged; &"" = clear; &"value" = set
//   - WritingStyle / Theme: nil = leave unchanged; &"" = clear; &"value" = set
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
	WritingStyle       *string
	Theme              *string
	// Author / AuthorStyleIntro / AuthorAvatarURL: leave-unchanged semantics
	// (nil = unchanged; &"" = clear; &"value" = set), same as Style/WritingStyle/Theme.
	Author           *string
	AuthorStyleIntro *string
	AuthorAvatarURL  *string
	Watermark        *bool
	Goal             string
	GoalMode         *bool
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
	if p.WritingStyle != nil {
		plan.WritingStyle = *p.WritingStyle
	}
	if p.Theme != nil {
		plan.Theme = *p.Theme
	}
	if p.Author != nil {
		plan.Author = *p.Author
	}
	if p.AuthorStyleIntro != nil {
		plan.AuthorStyleIntro = *p.AuthorStyleIntro
	}
	if p.AuthorAvatarURL != nil {
		plan.AuthorAvatarURL = *p.AuthorAvatarURL
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
