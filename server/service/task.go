package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/agent"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/storage"
)

// TaskEnqueuer abstracts the async task enqueue mechanism (Asynq, in-process, etc.).
type TaskEnqueuer interface {
	Enqueue(taskType string, payload []byte) error
	EnqueueIn(taskType string, payload []byte, delay time.Duration) error
}

type PublishedTrackingService interface {
	EnsureTrackingForPublishedTask(ctx context.Context, userID, taskID string) error
}

// TypeContentGenerate is the Asynq task type for content generation.
const TypeContentGenerate = "content:generate"

// TaskService handles task CRUD, manual creation, and execution orchestration.
type TaskService struct {
	repo                repository.Repository
	executor            agent.TaskExecutor
	logger              *zerolog.Logger
	enqueuer            TaskEnqueuer
	store               storage.Provider
	creditSvc           *CreditService
	publishingSvc       *PublishingService
	taskLogDir          string
	workspaceSvc        *WorkspaceService
	workspaceDir        string
	pubsub              *RedisPubSub
	pubsubCancel        context.CancelFunc // stops the listenCancelEvents goroutine
	cancelFuncs         sync.Map           // taskID → context.CancelFunc
	seednoteTrackingSvc PublishedTrackingService
	// wcfNotifier pushes task success/failure/cancel messages to the task
	// owner's WeChat via the wcfLink sidecar. Nil when wcf is disabled — all
	// terminal hooks no-op. Best-effort; never fails the task pipeline.
	wcfNotifier    *WCFNotifier
	topicPoolSvc   *TopicPoolService
	goalMultiplier int
	// executionTimeout bounds the fallback (Redis-down) in-process execution.
	// The asynq path is bounded by the asynq task Timeout (see scheduler).
	// Default 60m; override via SetExecutionTimeouts.
	executionTimeout time.Duration
	// persistTimeout bounds the post-execution DB writes (result/files/status),
	// decoupled from the execution ctx so completed work is saved even on overrun.
	// Default 10m; override via SetExecutionTimeouts.
	persistTimeout time.Duration
	// defaultModel / maxTurnsOverrides feed the local-executor claim response
	// (LocalExecutionConfig) so a desktop-built agent argv mirrors what the cloud
	// DockerExecutor would pass. Set via SetExecutorDefaults during wiring.
	defaultModel      string
	maxTurnsOverrides map[string]int
}

// NewTaskService creates a new TaskService.
// If pubsub is nil, cross-replica cancel signaling and progress events are disabled.
func NewTaskService(
	repo repository.Repository,
	executor agent.TaskExecutor,
	enqueuer TaskEnqueuer,
	store storage.Provider,
	creditSvc *CreditService,
	logger *zerolog.Logger,
	taskLogDir string,
	workspaceSvc *WorkspaceService,
	workspaceDir string,
	pubsub *RedisPubSub,
	publishingSvc *PublishingService,
) *TaskService {
	svc := &TaskService{
		repo:             repo,
		executor:         executor,
		logger:           logger,
		enqueuer:         enqueuer,
		store:            store,
		creditSvc:        creditSvc,
		publishingSvc:    publishingSvc,
		taskLogDir:       taskLogDir,
		workspaceSvc:     workspaceSvc,
		workspaceDir:     workspaceDir,
		pubsub:           pubsub,
		executionTimeout: 60 * time.Minute,
		persistTimeout:   10 * time.Minute,
	}

	// Start listening for cross-replica cancel events.
	if pubsub != nil && pubsub.Available() {
		ctx, cancel := context.WithCancel(context.Background())
		svc.pubsubCancel = cancel
		go svc.listenCancelEvents(ctx)
	}

	return svc
}

// Close stops the Redis pub/sub subscriber goroutine.
// Call this during server graceful shutdown.
func (s *TaskService) Close() {
	if s.pubsubCancel != nil {
		s.pubsubCancel()
	}
}

func (s *TaskService) SetSeednoteTrackingService(trackingSvc PublishedTrackingService) {
	s.seednoteTrackingSvc = trackingSvc
}

// SetWCFNotifier wires the WeChat notification sidecar. Notifier is nil-safe;
// pass nil to disable all terminal-state WeChat notifications.
func (s *TaskService) SetWCFNotifier(n *WCFNotifier) {
	s.wcfNotifier = n
}

// SetTopicPoolService sets the topic pool service for plan-task integration.
func (s *TaskService) SetTopicPoolService(svc *TopicPoolService) {
	s.topicPoolSvc = svc
}

// SetExecutionTimeouts overrides the fallback execution timeout and the
// post-execution persistence timeout (both have sensible defaults).
// execution bounds the Redis-down in-process path; persist bounds the
// result/files/status DB writes that are decoupled from the execution ctx.
func (s *TaskService) SetExecutionTimeouts(execution, persist time.Duration) {
	if execution > 0 {
		s.executionTimeout = execution
	}
	if persist > 0 {
		s.persistTimeout = persist
	}
}

// SetExecutorDefaults wires the Claude model + per-type max-turns overrides used
// to build local-executor claim responses. Mirrors the values the cloud
// DockerExecutor receives, so a desktop-spawned agent argv matches the cloud path.
func (s *TaskService) SetExecutorDefaults(defaultModel string, maxTurnsOverrides map[string]int) {
	s.defaultModel = defaultModel
	s.maxTurnsOverrides = maxTurnsOverrides
}

// GoalMultiplier returns the configured goal-mode credit multiplier (default 3).
// Goal mode is charged upfront at this rate and never refunded — the goal
// evaluation loop runs inside Claude Code's built-in /goal mechanism, so the
// server cannot tell how many turns were consumed.
func (s *TaskService) GoalMultiplier() int {
	if s.goalMultiplier <= 0 {
		return 3
	}
	return s.goalMultiplier
}

// listenCancelEvents subscribes to Redis cancel events and triggers local
// context cancellation for tasks executing on this replica.
func (s *TaskService) listenCancelEvents(ctx context.Context) {
	ch, cancel := s.pubsub.SubscribeCancel(ctx)
	defer cancel()

	for taskID := range ch {
		if v, ok := s.cancelFuncs.Load(taskID); ok {
			if fn, ok := v.(context.CancelFunc); ok {
				fn()
				s.logger.Info().Str("task_id", taskID).Msg("received cross-replica cancel signal")
			}
		}
	}
}

// TaskLogDir returns the configured task log directory.
func (s *TaskService) TaskLogDir() string {
	return s.taskLogDir
}

// PubSub returns the Redis pub/sub instance (may be nil if Redis is unavailable).
func (s *TaskService) PubSub() *RedisPubSub {
	return s.pubsub
}

// StorageProviderName returns the name of the configured storage provider,
// or empty string if no provider is configured.
func (s *TaskService) StorageProviderName() string {
	if s.store == nil {
		return ""
	}
	return s.store.Name()
}

// CreateManualParams holds the inputs for CreateManual. Fields map 1:1 to the
// model.Task attributes that callers can supply at creation time. Using a struct
// instead of a long positional signature keeps call sites readable as fields are
// added and prevents argument-order bugs.
type CreateManualParams struct {
	UserID            string
	ProjectID         string
	Prompt            string
	Quantity          int
	ImageRatio        string
	ImageModelKey     string
	SkipRefImage      *bool
	ReferenceImageURL string
	// Overrides carries the task-level per-dimension overrides. Only the fields the
	// user explicitly overrode are populated; empty fields = inherit the project
	// value (resolved two-layer task.Overrides.X ?? project.X at execution via
	// ResolveStyle). nil = fully inherit the project. The task no longer snapshots
	// resolved values — editing the project immediately affects pending tasks.
	Overrides *model.StyleOverrides
	Watermark *bool
	Goal      string
	GoalMode  bool
	// HasContentImage / HasTailImage: seednote image composition (cover always
	// generated). nil → fall back to task model defaults (content on, tail off);
	// non-nil honors explicit user choice.
	HasContentImage *bool
	HasTailImage    *bool
	// ArticleWithCover / ArticleWithContentImages: 公众号 article image toggles
	// (cover NOT mandatory). nil → fall back to task model defaults (both on);
	// non-nil honors explicit user choice. Non-article task types ignore them.
	ArticleWithCover         *bool
	ArticleWithContentImages *bool
	// Ecommerce carries the e-commerce package config (selected modules, product
	// photos, target platform, selling points, language, provider-strategy
	// override). Only consulted when the project platform is "ecommerce"; ignored
	// otherwise. When set, the task is billed once as a single package at the sum
	// of selected module prices and quantity is forced to 1.
	Ecommerce *model.EcommerceConfig
	// ExecutionTarget, when model.ExecutionTargetLocal, routes the task to a
	// desktop local executor instead of cloud Asynq/Docker. The task is created
	// pending with a LocalClaimDeadline and is NOT enqueued; a desktop claims it
	// via ClaimLocalTask. Unclaimed tasks fall back to cloud after the deadline
	// (ReclaimExpiredLocalTasks). Empty = cloud (default).
	ExecutionTarget string
}

// CreateManual creates tasks without a plan and enqueues them for execution.
// The Quantity field (1-5) determines how many tasks to create, each independently billed.
// ImageModelKey optionally selects a per-task image model (validated upstream by the handler).
//
// Style/persona/theme dimensions are NOT resolved or snapshotted here: the task
// stores only the explicitly-overridden slots (p.Overrides) and inherits the rest
// from the project at execution via ResolveStyle (task.Overrides.X ?? project.X).
// This is the live-inheritance model — editing the project immediately affects
// pending tasks.
//
// When GoalMode is true, each task charges GoalMultiplier() × base cost upfront
// and never refunds. The goal condition is propagated to the agent process and
// prepended to the user prompt as a /goal slash command, letting Claude Code's
// built-in goal loop drive turn-by-turn evaluation inside a single session.
func (s *TaskService) CreateManual(ctx context.Context, p CreateManualParams) ([]*model.Task, error) {
	if p.ProjectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	// Clamp quantity to 1-5.
	quantity := p.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	if quantity > 5 {
		quantity = 5
	}

	// Load and validate project.
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

	taskType := project.Platform

	// E-commerce: merge the PROJECT's reusable e-commerce defaults (default
	// modules, target platform, brand brief, image model key) into the task config
	// with task-level explicit values winning. Product photos and selling points
	// stay per-task. Done before billing so the package cost reflects the merged
	// module selection. The project is the single source of truth — there is no
	// template layer in the runtime resolution chain.
	effectiveImageModelKey := p.ImageModelKey
	if taskType == model.PlatformEcommerce {
		projEc := project.EcommerceDefaults.Data()
		if p.Ecommerce == nil {
			p.Ecommerce = &model.EcommerceConfig{}
		}
		if len(p.Ecommerce.SelectedModules) == 0 && len(projEc.DefaultSelectedModules) > 0 {
			p.Ecommerce.SelectedModules = projEc.DefaultSelectedModules
		}
		if p.Ecommerce.TargetPlatform == "" {
			p.Ecommerce.TargetPlatform = projEc.TargetPlatform
		}
		if p.Ecommerce.BrandBrief == "" {
			p.Ecommerce.BrandBrief = projEc.BrandBrief
		}
		if effectiveImageModelKey == "" {
			effectiveImageModelKey = projEc.ImageModelKey
		}
	}

	// Pre-calculate total credit cost and deduct upfront to avoid race conditions.
	var deductedTaskIDs []string
	tasks := make([]*model.Task, 0, quantity)

	multiplier := 1
	if p.GoalMode {
		multiplier = s.GoalMultiplier()
	}

	if s.creditSvc != nil {
		if taskType == model.PlatformEcommerce {
			// E-commerce is billed as a single deliverable package whose cost is
			// the sum of selected module unit prices × quantities (see
			// CreditService.EcommercePackageCost). Quantity is forced to 1 (one
			// package per task); the goal-mode multiplier does not apply.
			quantity = 1
			if p.Ecommerce == nil || len(p.Ecommerce.SelectedModules) == 0 {
				return nil, fmt.Errorf("ecommerce task requires at least one selected module")
			}
			packageCost, known := s.creditSvc.EcommercePackageCost(p.Ecommerce.SelectedModules)
			if !known {
				return nil, fmt.Errorf("unknown ecommerce module selected")
			}
			if packageCost <= 0 {
				return nil, fmt.Errorf("ecommerce package cost must be positive")
			}
			taskID := generateTaskID()
			if _, err := s.creditSvc.DeductForTaskWithAmount(ctx, p.UserID, taskType, taskID, packageCost); err != nil {
				if errors.Is(err, ErrInsufficientCredits) {
					return nil, fmt.Errorf("积分不足: %w", err)
				}
				return nil, fmt.Errorf("deduct credits: %w", err)
			}
			deductedTaskIDs = []string{taskID}
		} else {
			cost, ok := s.creditSvc.TaskCost(taskType)
			if !ok {
				return nil, fmt.Errorf("unknown task type: %s", taskType)
			}
			totalCost := cost * quantity * multiplier

			// Generate all task IDs upfront so we can create individual transactions.
			taskIDs := make([]string, quantity)
			for i := range taskIDs {
				taskIDs[i] = generateTaskID()
			}

			// Deduct total cost in a single atomic transaction.
			var deductErr error
			if multiplier > 1 {
				deductErr = s.creditSvc.DeductBatchWithMultiplier(ctx, p.UserID, taskType, totalCost, taskIDs, multiplier)
			} else {
				deductErr = s.creditSvc.DeductBatch(ctx, p.UserID, taskType, totalCost, taskIDs)
			}
			if deductErr != nil {
				if errors.Is(deductErr, ErrInsufficientCredits) {
					return nil, fmt.Errorf("积分不足: %w", deductErr)
				}
				return nil, fmt.Errorf("deduct credits: %w", deductErr)
			}
			deductedTaskIDs = taskIDs
		}
	}

	for i := 0; i < quantity; i++ {
		var taskID string
		if s.creditSvc != nil && len(deductedTaskIDs) > i {
			taskID = deductedTaskIDs[i]
		} else {
			taskID = generateTaskID()
		}

		// Topic pool: when the caller supplied no prompt and the project is a
		// topic-driven platform (article/seednote), claim the next unused topic
		// from the pool as this task's prompt — mirroring CreateFromPlan. Ecommerce
		// is product-image based with no topic semantics, so it is skipped. The pool
		// may run out mid-batch; remaining tasks keep an empty prompt and fall back
		// to the agent's auto-research path. The generation skill respects an
		// already-set topic, so it will not re-claim during execution.
		taskPrompt := p.Prompt
		if taskPrompt == "" && s.topicPoolSvc != nil &&
			(taskType == model.PlatformArticle || taskType == model.PlatformSeednote) {
			claimed, claimErr := s.topicPoolSvc.ClaimForTask(ctx, p.UserID, p.ProjectID, taskID)
			if claimErr != nil {
				s.logger.Warn().Err(claimErr).Str("task_id", taskID).Msg("claim topic from pool failed, leaving prompt empty")
			} else if claimed != "" {
				taskPrompt = claimed
			}
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
		// Article image toggles: honor caller's explicit choice, otherwise rely on
		// the model's column defaults (both on). Ignored for non-article types.
		articleCover := true
		if p.ArticleWithCover != nil {
			articleCover = *p.ArticleWithCover
		}
		articleContent := true
		if p.ArticleWithContentImages != nil {
			articleContent = *p.ArticleWithContentImages
		}

		task := &model.Task{
			ID:                       taskID,
			UserID:                   p.UserID,
			ProjectID:                p.ProjectID,
			Type:                     taskType,
			Status:                   model.TaskStatusPending,
			Prompt:                   taskPrompt,
			ImageRatio:               p.ImageRatio,
			ImageModelKey:            effectiveImageModelKey,
			ReferenceImageURL:        p.ReferenceImageURL,
			SkipReferenceImage:       p.SkipRefImage != nil && *p.SkipRefImage,
			Watermark:                p.Watermark != nil && *p.Watermark,
			Goal:                     p.Goal,
			GoalMode:                 p.GoalMode,
			HasContentImage:          hasContent,
			HasTailImage:             hasTail,
			ArticleWithCover:         &articleCover,
			ArticleWithContentImages: &articleContent,
			ExecutionTarget:          p.ExecutionTarget,
		}
		if p.ExecutionTarget == model.ExecutionTargetLocal {
			deadline := time.Now().Add(LocalClaimWindow)
			task.LocalClaimDeadline = &deadline
		}
		if p.Overrides != nil {
			task.SetOverrides(*p.Overrides)
		}
		if p.Ecommerce != nil {
			task.SetEcommerce(*p.Ecommerce)
		}

		if err := s.repo.Tasks().Create(ctx, task); err != nil {
			// Refund only the tasks that were NOT successfully created.
			// deductedTaskIDs[0..i) were created successfully; [i..) were not.
			if s.creditSvc != nil {
				for j := i; j < len(deductedTaskIDs); j++ {
					if refundErr := s.creditSvc.RefundForTask(ctx, deductedTaskIDs[j]); refundErr != nil {
						s.logger.Error().Err(refundErr).Str("task_id", deductedTaskIDs[j]).Msg("failed to refund credits during rollback")
					}
				}
			}
			// Release this iteration's pre-claimed topic back to the pool so it
			// isn't orphaned (a no-op when no topic was claimed for this task).
			if s.topicPoolSvc != nil {
				if relErr := s.topicPoolSvc.ReleaseForTask(ctx, taskID); relErr != nil {
					s.logger.Error().Err(relErr).Str("task_id", taskID).Msg("failed to release topic during rollback")
				}
			}
			return nil, fmt.Errorf("create task: %w", err)
		}

		// Enqueue for async execution. Local-target tasks wait for a desktop
		// local executor to claim them (ClaimLocalTask); do NOT enqueue to cloud
		// Asynq. The fallback worker re-routes them to cloud if unclaimed past
		// the deadline, so they can never get stuck.
		if task.ExecutionTarget == model.ExecutionTargetLocal {
			s.logger.Info().Str("task_id", taskID).Msg("task routed to local executor, awaiting desktop claim")
		} else if err := s.EnqueueExecution(ctx, task, nil); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to enqueue task, marking as failed")
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "failed to enqueue: "+err.Error())
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// CreateFromPlan creates a task linked to a plan and enqueues it for execution.
//
// The plan's style/persona/theme dimensions are copied into the spawned task's
// Task.Overrides (all six, via SetOverrides — empty dimensions serialize to {}
// and the resolver falls through to the project for them). This lets two plans
// under one project theme their tasks differently. The plan's
// scheduling-adjacent "what to produce" params (image model, reference image,
// watermark, goal, seednote image composition) also flow to the task.
func (s *TaskService) CreateFromPlan(ctx context.Context, plan *model.Plan) (*model.Task, error) {
	taskID := generateTaskID()

	prompt := plan.Prompt
	if prompt == "" {
		prompt = plan.Title
	}

	// Try to claim a topic from the topic pool if no prompt is set.
	if prompt == "" && s.topicPoolSvc != nil && plan.ProjectID != "" {
		claimed, err := s.topicPoolSvc.ClaimForTask(ctx, plan.UserID, plan.ProjectID, taskID)
		if err != nil {
			s.logger.Warn().Err(err).Str("plan_id", plan.ID).Msg("failed to claim topic from pool, falling back to auto-research")
		} else if claimed != "" {
			prompt = claimed
		}
	}

	// Derive task type from the project if ProjectID is set.
	taskType := plan.Type
	if plan.ProjectID != "" {
		if found, err := s.repo.Projects().FindByID(ctx, plan.ProjectID); err == nil {
			taskType = found.Platform
		}
	}

	// Deduct credits for the plan task.
	// Plan-level goal mode (plan.GoalMode) propagates to the task and scales
	// the upfront charge by GoalMultiplier() to cover all retry attempts.
	planGoalMode := plan.GoalMode && strings.TrimSpace(plan.Goal) != ""
	planMultiplier := 1
	if planGoalMode {
		planMultiplier = s.GoalMultiplier()
	}

	if s.creditSvc != nil {
		if _, costOK := s.creditSvc.TaskCost(taskType); !costOK {
			s.logger.Warn().Str("user_id", plan.UserID).Str("plan_id", plan.ID).Str("task_type", taskType).Msg("skipping plan task with unknown task type")
			return nil, nil
		}
		if _, err := s.creditSvc.DeductForTask(ctx, plan.UserID, taskType, taskID, planMultiplier); err != nil {
			if errors.Is(err, ErrInsufficientCredits) {
				s.logger.Warn().Str("user_id", plan.UserID).Str("plan_id", plan.ID).Str("task_type", taskType).Msg("skipping plan task due to insufficient credits")
				return nil, nil
			}
			s.logger.Error().Err(err).Str("user_id", plan.UserID).Str("plan_id", plan.ID).Str("task_type", taskType).Msg("failed to deduct credits for plan task")
			return nil, nil
		}
	}

	task := &model.Task{
		ID:                       taskID,
		UserID:                   plan.UserID,
		ProjectID:                plan.ProjectID,
		Type:                     taskType,
		Status:                   model.TaskStatusPending,
		Prompt:                   prompt,
		ImageModelKey:            plan.ImageModelKey,
		ReferenceImageURL:        plan.ReferenceImageURL,
		SkipReferenceImage:       plan.SkipReferenceImage,
		Watermark:                plan.Watermark,
		Goal:                     plan.Goal,
		GoalMode:                 planGoalMode,
		HasContentImage:          plan.HasContentImage,
		HasTailImage:             plan.HasTailImage,
		ArticleWithCover:         plan.ArticleWithCover,
		ArticleWithContentImages: plan.ArticleWithContentImages,
	}

	// Copy the plan's style/persona/theme dimensions into the spawned task's
	// overrides. Empty dimensions serialize to {} and the resolver falls through
	// to the project for them, so this is safe even when the plan set nothing.
	task.SetOverrides(model.StyleOverrides{
		VisualStyle:   plan.VisualStyle,
		WriterKey:     plan.WriterKey,
		WritingVoice:  plan.WritingVoice,
		Byline:        plan.Byline,
		PersonaAvatar: plan.PersonaAvatar,
		Theme:         plan.Theme,
	})

	if err := s.repo.Tasks().Create(ctx, task); err != nil {
		// Refund the deducted credits if task creation fails.
		if s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForTask(ctx, taskID); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund credits during plan task rollback")
			}
		}
		// Release the pre-claimed topic back to the pool so it isn't orphaned
		// (a no-op when no topic was claimed for this task).
		if s.topicPoolSvc != nil {
			if relErr := s.topicPoolSvc.ReleaseForTask(ctx, taskID); relErr != nil {
				s.logger.Error().Err(relErr).Str("task_id", taskID).Msg("failed to release topic during plan task rollback")
			}
		}
		return nil, fmt.Errorf("create task from plan: %w", err)
	}

	if err := s.EnqueueExecution(ctx, task, nil); err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Str("plan_id", plan.ID).Msg("failed to enqueue plan task")
		_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "failed to enqueue: "+err.Error())
		return task, nil
	}

	return task, nil
}

// GetByID returns a task by its ID.
func (s *TaskService) GetByID(ctx context.Context, id string) (*model.Task, error) {
	task, err := s.repo.Tasks().FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find task: %w", err)
	}
	return task, nil
}

// List returns tasks for a user with optional status and project filters and pagination.
func (s *TaskService) List(ctx context.Context, userID string, offset, limit int, status, projectID string) ([]*model.Task, int64, error) {
	var tasks []*model.Task
	var err error

	if status != "" {
		tasks, err = s.repo.Tasks().FindByUserIDAndStatus(ctx, userID, status, projectID, offset, limit)
	} else {
		tasks, err = s.repo.Tasks().FindByUserID(ctx, userID, projectID, offset, limit)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}

	var total int64
	if status != "" {
		total, err = s.repo.Tasks().CountByUserIDAndStatus(ctx, userID, status, projectID)
	} else {
		total, err = s.repo.Tasks().CountByUserID(ctx, userID, projectID)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	return tasks, total, nil
}

// ListTitles returns all recorded titles for a project, ordered by creation time descending.
func (s *TaskService) ListTitles(ctx context.Context, projectID string) ([]string, error) {
	return s.repo.Tasks().FindTitlesByProjectID(ctx, projectID)
}

var artifactTaskTitles = map[string]struct{}{
	"图片内容规划":    {},
	"标题候选与评分":   {},
	"选题研究报告":    {},
	"违禁词合规检查报告": {},
}

// FinalizeTitle records the hook-reported final title as the canonical task title.
func (s *TaskService) FinalizeTitle(ctx context.Context, userID, taskID, title string) (string, error) {
	cleaned := cleanFinalTitle(title)
	if cleaned == "" {
		return "", fmt.Errorf("title is required")
	}
	if len([]rune(cleaned)) > 200 {
		return "", fmt.Errorf("title must be <= 200 characters")
	}
	if _, ok := artifactTaskTitles[cleaned]; ok {
		return "", fmt.Errorf("artifact title is not allowed: %s", cleaned)
	}

	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return "", fmt.Errorf("task not found: %w", err)
	}
	if userID != "" && task.UserID != userID {
		return "", fmt.Errorf("task not found")
	}

	tasks, err := s.repo.Tasks().FindTitleTasksByProjectID(ctx, task.ProjectID)
	if err != nil {
		return "", fmt.Errorf("list existing title tasks: %w", err)
	}
	normalized := normalizeTitleForDedup(cleaned)
	for _, existing := range tasks {
		if existing.ID != task.ID && normalizeTitleForDedup(existing.Title) == normalized {
			return "", fmt.Errorf("duplicate title: %s", cleaned)
		}
	}

	if err := s.repo.Tasks().UpdateTitle(ctx, taskID, cleaned); err != nil {
		return "", fmt.Errorf("update task title: %w", err)
	}
	return cleaned, nil
}

func (s *TaskService) ClearArtifactTitles(ctx context.Context) (int64, error) {
	titles := make([]string, 0, len(artifactTaskTitles))
	for title := range artifactTaskTitles {
		titles = append(titles, title)
	}
	count, err := s.repo.Tasks().ClearTitles(ctx, titles)
	if err != nil {
		return 0, fmt.Errorf("clear artifact titles: %w", err)
	}
	return count, nil
}

func cleanFinalTitle(title string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(title)), " ")
}

func normalizeTitleForDedup(title string) string {
	return strings.ToLower(strings.Join(strings.Fields(title), ""))
}

// Cancel atomically transitions a task from pending/running to cancelled and signals
// the running execution to stop. Credits are refunded if the transition succeeds.
// Uses CompareAndSwapStatus to prevent cancelling already-completed or already-failed tasks.
// If Redis pub/sub is available, it also publishes a cancel event so other replicas
// can propagate the cancellation to their in-process execution contexts.
//
// Goal-mode tasks receive a proportional refund based on remaining attempts;
// normal tasks receive a full refund.
func (s *TaskService) Cancel(ctx context.Context, id string) error {
	// Fetch task before CAS so we can decide refund strategy.
	task, taskErr := s.repo.Tasks().FindByID(ctx, id)

	// Atomically transition status: only pending or running can be cancelled.
	swapped, err := s.repo.Tasks().CompareAndSwapStatus(
		ctx, id, model.TaskStatusRunning, model.TaskStatusCancelled,
	)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	if !swapped {
		// Also try pending → cancelled (task may not have started running yet).
		swapped, err = s.repo.Tasks().CompareAndSwapStatus(
			ctx, id, model.TaskStatusPending, model.TaskStatusCancelled,
		)
		if err != nil {
			return fmt.Errorf("cancel task: %w", err)
		}
		if !swapped {
			return fmt.Errorf("task is not in a cancellable state (current status is not pending or running)")
		}
	}
	// Refund credits for the cancelled task (idempotent — double-refund protected).
	if s.creditSvc != nil && taskErr == nil && task != nil {
		s.refundTaskByMode(ctx, task, "取消")
	}
	// Release concurrency slot.
	if s.pubsub != nil {
		var projectID string
		if taskErr == nil && task != nil {
			projectID = task.ProjectID
		} else if t, err := s.repo.Tasks().FindByID(ctx, id); err == nil {
			projectID = t.ProjectID
		}
		if projectID != "" {
			s.pubsub.ReleaseSlot(ctx, projectID)
		}
	}
	// Signal the running execution (if any) to cancel via its context.
	if v, ok := s.cancelFuncs.Load(id); ok {
		if cancel, ok := v.(context.CancelFunc); ok {
			cancel()
			s.logger.Info().Str("task_id", id).Msg("signalled execution context cancellation")
		}
	}
	// Publish to Redis so other replicas can cancel their local contexts.
	if s.pubsub != nil {
		s.pubsub.PublishCancel(ctx, id)
	}
	return nil
}

// registerCancel stores a context.CancelFunc for a running task so it can be
// invoked by Cancel() to stop the execution.
func (s *TaskService) registerCancel(taskID string, cancel context.CancelFunc) {
	s.cancelFuncs.Store(taskID, cancel)
}

// deregisterCancel removes the stored cancel func for a completed task.
func (s *TaskService) deregisterCancel(taskID string) {
	s.cancelFuncs.Delete(taskID)
}

// CancelAllRunning cancels all in-progress task execution contexts.
// Returns the number of tasks that were cancelled.
func (s *TaskService) CancelAllRunning() int {
	count := 0
	s.cancelFuncs.Range(func(key, value any) bool {
		if cancel, ok := value.(context.CancelFunc); ok {
			cancel()
			count++
			s.logger.Info().Str("task_id", key.(string)).Msg("cancelled running task for shutdown")
		}
		return true
	})
	return count
}

// RunningTaskCount returns the number of currently executing tasks.
func (s *TaskService) RunningTaskCount() int {
	count := 0
	s.cancelFuncs.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// GetFiles returns files associated with a task.
func (s *TaskService) GetFiles(ctx context.Context, taskID string) ([]*model.TaskFile, error) {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task files: %w", err)
	}
	s.EnrichFilesWithURLs(ctx, files)
	return files, nil
}

func (s *TaskService) RebuildWorkflowStatus(ctx context.Context, taskID string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}

	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task files: %w", err)
	}

	var reviewJSON []byte
	for _, file := range files {
		if DetermineWorkflowArtifactRole(file.FileName, file.MimeType) != model.FileRoleReview {
			continue
		}
		data, err := s.getFileContent(ctx, file)
		if err != nil {
			return fmt.Errorf("read review file: %w", err)
		}
		reviewJSON = data
		break
	}

	status, err := BuildWorkflowStatus(task.Type, files, reviewJSON)
	if err != nil {
		return fmt.Errorf("build workflow status: %w", err)
	}
	data, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshal workflow status: %w", err)
	}
	if err := s.repo.Tasks().UpdateWorkflowStatus(ctx, taskID, string(data)); err != nil {
		return fmt.Errorf("persist workflow status: %w", err)
	}
	return nil
}

// EnqueueExecution enqueues a task for async execution.
// If no enqueuer is available (nil), it runs synchronously in a goroutine.
// If the project's concurrent task limit is reached, the task stays in DB as "pending"
// and will be dispatched later when a slot opens up.
func (s *TaskService) EnqueueExecution(ctx context.Context, task *model.Task, project *model.Project) error {
	// Load project if not provided, for concurrency check.
	if project == nil && task.ProjectID != "" {
		ch, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
		if err == nil {
			project = ch
		}
	}

	// Check per-project concurrency limit.
	if project != nil {
		maxConcurrent := project.MaxConcurrentTasks
		if maxConcurrent <= 0 {
			maxConcurrent = DefaultMaxConcurrentTasks
		}

		slotReserved := false
		if s.pubsub != nil && s.pubsub.Available() {
			// Atomic check-and-reserve via Redis to prevent TOCTOU races.
			count, ok, err := s.pubsub.TryReserveSlot(ctx, project.ID, maxConcurrent)
			if err != nil {
				s.logger.Warn().Err(err).
					Str("project_id", project.ID).
					Msg("failed to reserve concurrency slot via Redis, falling back to DB check")
			} else if !ok {
				s.logger.Info().
					Str("task_id", task.ID).
					Str("project_id", project.ID).
					Int("max", maxConcurrent).
					Msg("project concurrency limit reached (Redis), task will be dispatched later")
				return nil
			} else {
				s.logger.Debug().
					Str("task_id", task.ID).
					Str("project_id", project.ID).
					Int64("running", count).
					Int("max", maxConcurrent).
					Msg("reserved concurrency slot via Redis")
				slotReserved = true
			}
		}

		// DB fallback: non-atomic check (when Redis unavailable or errored).
		if !slotReserved {
			running, err := s.repo.Tasks().CountRunningByProject(ctx, project.ID)
			if err != nil {
				s.logger.Warn().Err(err).
					Str("project_id", project.ID).
					Msg("failed to count running tasks, proceeding without limit check")
			} else if int(running) >= maxConcurrent {
				s.logger.Info().
					Str("task_id", task.ID).
					Str("project_id", project.ID).
					Int64("running", running).
					Int("max", maxConcurrent).
					Msg("project concurrency limit reached, task will be dispatched later")
				return nil
			}
		}
	}

	if s.enqueuer != nil {
		payload, err := json.Marshal(map[string]string{
			"task_id": task.ID,
			"user_id": task.UserID,
		})
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		if err := s.enqueuer.Enqueue(TypeContentGenerate, payload); err != nil {
			if s.pubsub != nil && s.pubsub.Available() && project != nil {
				s.pubsub.ReleaseSlot(ctx, project.ID)
			}
			return fmt.Errorf("enqueue task: %w", err)
		}

		s.logger.Info().Str("task_id", task.ID).Msg("task enqueued for async execution")
		return nil
	}

	// Fallback: run in goroutine if no enqueuer.
	s.logger.Warn().Str("task_id", task.ID).Msg("no enqueuer available, running task in goroutine")
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error().
					Str("task_id", task.ID).
					Interface("panic", r).
					Msg("panic recovered in fallback task execution")
				if s.pubsub != nil && s.pubsub.Available() && project != nil {
					s.pubsub.ReleaseSlot(context.Background(), project.ID)
				}
			}
		}()
		fallbackCtx, cancel := context.WithTimeout(context.Background(), s.executionTimeout)
		defer cancel()
		// Set running status before execution to prevent plan checker from re-dispatching.
		swapped, _ := s.repo.Tasks().CompareAndSwapStatusAndStartedAt(fallbackCtx, task.ID, model.TaskStatusPending, model.TaskStatusRunning)
		if !swapped {
			// Task was cancelled or already running; release the reserved slot.
			if s.pubsub != nil && s.pubsub.Available() && project != nil {
				s.pubsub.ReleaseSlot(fallbackCtx, project.ID)
			}
			return
		}
		if err := s.HandleExecution(fallbackCtx, task, project); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("fallback task execution failed")
		}
	}()
	return nil
}

// DefaultMaxConcurrentTasks is the default per-project concurrent task limit.
const DefaultMaxConcurrentTasks = 10

// DispatchPendingTasks checks for pending tasks on a project and enqueues them
// if there are available concurrency slots. Called after a task completes or fails.
func (s *TaskService) DispatchPendingTasks(ctx context.Context, projectID string) error {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("find project: %w", err)
	}

	maxConcurrent := project.MaxConcurrentTasks
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrentTasks
	}

	var running int64
	if s.pubsub != nil && s.pubsub.Available() {
		key := projectRunningCountPrefix + projectID
		val, err := s.pubsub.rdb.Get(ctx, key).Int64()
		if err != nil {
			// Key may not exist; fall back to DB.
			running, _ = s.repo.Tasks().CountRunningByProject(ctx, projectID)
		} else {
			running = val
		}
	} else {
		running, err = s.repo.Tasks().CountRunningByProject(ctx, projectID)
		if err != nil {
			return fmt.Errorf("count running: %w", err)
		}
	}

	available := maxConcurrent - int(running)
	if available <= 0 {
		return nil
	}

	pending, err := s.repo.Tasks().FindPendingByProject(ctx, projectID, available)
	if err != nil {
		return fmt.Errorf("find pending: %w", err)
	}

	for _, t := range pending {
		if err := s.EnqueueExecution(ctx, t, project); err != nil {
			s.logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to dispatch pending task")
		}
	}

	return nil
}

// RefundForTask refunds credits for a failed task. This is a public wrapper
// around CreditService.RefundForTask for use by external callers (e.g., scheduler).
func (s *TaskService) RefundForTask(ctx context.Context, taskID string) error {
	if s.creditSvc == nil {
		return nil
	}
	return s.creditSvc.RefundForTask(ctx, taskID)
}

// refundTaskByMode issues the correct refund for a task based on whether goal
// mode is active.
//
// Goal-mode tasks never refund — the goal loop runs entirely inside Claude
// Code's /goal mechanism, so the server cannot tell how many turns were
// consumed. The upfront ×GoalMultiplier charge stands regardless of outcome.
//
// The refund path is idempotent (protected by FindRefundByTaskID), so it is
// safe for multiple callers (Cancel, HandleExecution cancel-detection,
// HandleExecutionFailure) to invoke this for the same task.
func (s *TaskService) refundTaskByMode(ctx context.Context, task *model.Task, reason string) {
	if s.creditSvc == nil || task == nil {
		return
	}
	if task.GoalMode {
		return
	}
	if refundErr := s.creditSvc.RefundForTask(ctx, task.ID, reason); refundErr != nil {
		s.logger.Error().Err(refundErr).Str("task_id", task.ID).Msg("failed to refund credits for task")
	}
}

// UsageStats holds aggregated LLM usage statistics.
type UsageStats struct {
	TotalTasks               int                       `json:"total_tasks"`
	TotalInputTokens         int64                     `json:"total_input_tokens"`
	TotalOutputTokens        int64                     `json:"total_output_tokens"`
	TotalCacheReadTokens     int64                     `json:"total_cache_read_tokens"`
	TotalCacheCreationTokens int64                     `json:"total_cache_creation_tokens"`
	TotalCostUSD             float64                   `json:"total_cost_usd"`
	ByType                   map[string]*TypeStatEntry `json:"by_type,omitempty"`
}

// TypeStatEntry holds per-type aggregated stats.
type TypeStatEntry struct {
	Count               int     `json:"count"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	CacheReadTokens     int64   `json:"cache_read_tokens"`
	CacheCreationTokens int64   `json:"cache_creation_tokens"`
	CostUSD             float64 `json:"cost_usd"`
}

// GetUsageStats returns aggregated token usage and cost stats for a user.
func (s *TaskService) GetUsageStats(ctx context.Context, userID string, from, to time.Time, projectID string) (*UsageStats, error) {
	stats := &UsageStats{ByType: make(map[string]*TypeStatEntry)}

	// SQL-level aggregation for totals.
	totalTasks, totalInput, totalOutput, totalCacheRead, totalCacheCreation, totalCost, err :=
		s.repo.Tasks().AggregateUsageByUser(ctx, userID, from, to, projectID)
	if err != nil {
		return nil, fmt.Errorf("aggregate usage: %w", err)
	}
	stats.TotalTasks = int(totalTasks)
	stats.TotalInputTokens = totalInput
	stats.TotalOutputTokens = totalOutput
	stats.TotalCacheReadTokens = totalCacheRead
	stats.TotalCacheCreationTokens = totalCacheCreation
	stats.TotalCostUSD = totalCost

	// SQL GROUP BY for per-type breakdown.
	typeRows, err := s.repo.Tasks().AggregateUsageByType(ctx, userID, from, to, projectID)
	if err != nil {
		return nil, fmt.Errorf("aggregate usage by type: %w", err)
	}
	for _, row := range typeRows {
		stats.ByType[row.Type] = &TypeStatEntry{
			Count:               int(row.Count),
			InputTokens:         row.InputTokens,
			OutputTokens:        row.OutputTokens,
			CacheReadTokens:     row.CacheReadTokens,
			CacheCreationTokens: row.CacheCreationTokens,
			CostUSD:             row.CostUSD,
		}
	}

	return stats, nil
}

// SetPublished toggles the published flag on a task.
func (s *TaskService) SetPublished(ctx context.Context, userID, taskID string, published bool) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task.UserID != userID {
		return fmt.Errorf("task does not belong to user")
	}
	return s.setPublishedAndMaybeTrack(ctx, userID, task, published)
}

func (s *TaskService) setPublishedAndMaybeTrack(ctx context.Context, userID string, task *model.Task, published bool) error {
	if err := s.repo.Tasks().SetPublished(ctx, task.ID, published); err != nil {
		return err
	}
	if !published || task.Type != model.PlatformSeednote || s.seednoteTrackingSvc == nil {
		return nil
	}
	if err := s.seednoteTrackingSvc.EnsureTrackingForPublishedTask(ctx, userID, task.ID); err != nil {
		return fmt.Errorf("ensure seednote tracking: %w", err)
	}
	return nil
}

// Delete permanently removes a task and its associated files.
// Running/pending tasks are cancelled first (with credit refund).
func (s *TaskService) Delete(ctx context.Context, id string) error {
	task, err := s.repo.Tasks().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}

	if task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusPending {
		if cancelErr := s.Cancel(ctx, id); cancelErr != nil {
			s.logger.Error().Err(cancelErr).Str("task_id", id).Msg("failed to cancel task before delete")
		}
	}

	files, err := s.repo.TaskFiles().FindByTaskID(ctx, id)
	if err != nil {
		s.logger.Error().Err(err).Str("task_id", id).Msg("failed to list task files for deletion")
	}

	for _, f := range files {
		if f.OSSKey != "" && s.store != nil {
			if delErr := s.store.Delete(ctx, f.OSSKey); delErr != nil {
				s.logger.Error().Err(delErr).Str("key", f.OSSKey).Msg("failed to delete storage file")
			}
		}
	}

	if err := s.repo.TaskFiles().DeleteByTaskID(ctx, id); err != nil {
		return fmt.Errorf("delete task files: %w", err)
	}

	if err := s.repo.Tasks().Delete(ctx, id); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	s.deregisterCancel(id)
	return nil
}
