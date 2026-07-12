package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
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
	repo                  repository.Repository
	executor              agent.TaskExecutor
	kubernetesDispatcher  agent.KubernetesDispatcher
	logger                *zerolog.Logger
	enqueuer              TaskEnqueuer
	store                 storage.Provider
	creditSvc             *CreditService
	publishingSvc         *PublishingService
	taskLogDir            string
	workspaceSvc          *WorkspaceService
	workspaceDir          string
	pubsub                *RedisPubSub
	pubsubCancel          context.CancelFunc // stops the listenCancelEvents goroutine
	cancelFuncs           sync.Map           // taskID → context.CancelFunc
	seednoteTrackingSvc   PublishedTrackingService
	videoCatalog          VideoModelCatalog
	videoCreditMultiplier int
	videoBilling          srvconfig.BillingConfig
	montageCfg            srvconfig.MontageConfig
	// ilinkNotifier enqueues task success/failure/cancel messages for delivery
	// through the platform WeChat assistant. Nil when ilink is disabled.
	ilinkNotifier  *IlinkNotifier
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
	memoryMgr         *projectmemory.ProjectMemoryManager
	nasResumeEnabled  bool
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
	svc.montageCfg = defaultMontageServiceConfig()

	// Start listening for cross-replica cancel events.
	if pubsub != nil && pubsub.Available() {
		ctx, cancel := context.WithCancel(context.Background())
		svc.pubsubCancel = cancel
		go svc.listenCancelEvents(ctx)
	}

	return svc
}

// Repository returns the backing repository for cross-service MCP bookkeeping
// that must update task-adjacent records in the same persistence layer.
func (s *TaskService) Repository() repository.Repository {
	if s == nil {
		return nil
	}
	return s.repo
}

func (s *TaskService) SetProjectMemoryManager(memoryMgr *projectmemory.ProjectMemoryManager) {
	s.memoryMgr = memoryMgr
}

func (s *TaskService) SetVideoCatalogAndCreditMultiplier(catalog VideoModelCatalog, creditMultiplier int) {
	if s == nil {
		return
	}
	s.videoCatalog = catalog
	s.videoCreditMultiplier = creditMultiplier
}

func (s *TaskService) SetVideoBillingConfig(billing srvconfig.BillingConfig) {
	if s == nil {
		return
	}
	s.videoBilling = billing
}

func (s *TaskService) SetMontageConfig(cfg srvconfig.MontageConfig) {
	if s == nil {
		return
	}
	s.montageCfg = cfg
}

func defaultMontageServiceConfig() srvconfig.MontageConfig {
	cfg := srvconfig.MontageConfig{}
	cfg.ApplyDefaults()
	return cfg
}

func (s *TaskService) montageCloudAvailable() bool {
	return s != nil && (s.enqueuer != nil || s.executor != nil)
}

func (s *TaskService) resolvedVideoCatalog() VideoModelCatalog {
	if s != nil && s.videoCatalog != nil {
		return s.videoCatalog
	}
	return VideoModelCatalog{}
}

func (s *TaskService) resolvedVideoCreditMultiplier() int {
	if s != nil && s.videoCreditMultiplier > 0 {
		return s.videoCreditMultiplier
	}
	return 1000
}

func (s *TaskService) videoBillingOptions(ctx context.Context, userID string) VideoBillingOptions {
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

func (s *TaskService) SetIlinkNotifier(n *IlinkNotifier) {
	s.ilinkNotifier = n
}

func (s *TaskService) notifyTerminal(ctx context.Context, task *model.Task, status, errMsg string) {
	if s.ilinkNotifier != nil {
		s.ilinkNotifier.NotifyTerminal(ctx, task, status, errMsg)
	}
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

func (s *TaskService) SetProjectConcurrencyCap(cap int) {
	// Retained until the Kubernetes executor wiring is replaced by the Job
	// dispatcher. Per-project concurrency is governed by Project.MaxConcurrentTasks.
}

// SetNASResumeEnabled enables task continuation against durable Kubernetes NAS
// workspaces. It must remain disabled for local and Docker executors.
func (s *TaskService) SetNASResumeEnabled(enabled bool) {
	s.nasResumeEnabled = enabled
}

func (s *TaskService) taskWorkspaceDir(taskID string) string {
	if s.workspaceDir != "" {
		return filepath.Join(s.workspaceDir, taskID)
	}
	return agent.DefaultWorkspaceDir(taskID)
}

// ResolveWorkspacePath converts a task-relative path into a server-local path
// when the API server can see that task's workspace. It returns false when the
// original path should be used as-is, including Kubernetes/remote workspaces.
func (s *TaskService) ResolveWorkspacePath(taskID, filePath string) (string, bool) {
	filePath = strings.TrimSpace(filePath)
	if taskID == "" || filePath == "" || filepath.IsAbs(filePath) {
		return filePath, false
	}
	cleanRelPath, err := CleanTaskFileRelativePath(filePath)
	if err != nil {
		return filePath, false
	}
	workDir := s.taskWorkspaceDir(taskID)
	if info, err := os.Stat(workDir); err == nil && info.IsDir() {
		return filepath.Join(workDir, cleanRelPath), true
	}
	return filePath, false
}

func (s *TaskService) effectiveProjectMaxConcurrent(project *model.Project) int {
	maxConcurrent := DefaultMaxConcurrentTasks
	if project != nil && project.MaxConcurrentTasks > 0 {
		maxConcurrent = project.MaxConcurrentTasks
	}
	return maxConcurrent
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

var ErrMontageInput = errors.New("montage input invalid")

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
	// Overrides is deprecated. New Studio/API flows do not set task-level style
	// overrides; runtime style/account config comes from ProjectSnapshot.
	Overrides *model.StyleOverrides
	// ProjectSnapshot, when set, is copied verbatim. Clone uses this to preserve
	// the original task's frozen config. New manual tasks leave it nil and snapshot
	// the current project at creation time.
	ProjectSnapshot *model.ProjectSnapshot
	// InputAttachments stores the original AI-entry attachments on the task so
	// executors can materialize them into the agent workspace.
	InputAttachments []model.EntryAttachment
	Watermark        *bool
	Goal             string
	GoalMode         bool
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
	// VideoCreatorConfig/VideoCreatorInput carry generation intake for
	// videocreator tasks. VideoConfig remains agent-owned resolved execution
	// state; user-authored creation input is persisted through Task.VideoInput.
	VideoCreatorConfig *model.VideoTaskConfig
	VideoCreatorInput  *model.VideoInput
	// VideoEditorConfig/VideoEditorInput carry edit/post-production intake for
	// videoeditor tasks. They are rejected for creator projects and vice versa.
	VideoEditorConfig *model.VideoTaskConfig
	VideoEditorInput  *model.VideoInput
	// MontageInput carries the Montage-specific creation contract.
	// It is independent from video creator/editor payloads and is only valid
	// for montage projects.
	MontageInput *model.MontageInput
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
// Style/author/theme dimensions are snapshotted from the project at creation.
// Editing the project later does not change existing pending/running tasks.
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
	videoCfg, videoInput, err := p.videoPayloadForTask(taskType)
	if err != nil {
		return nil, err
	}
	if p.MontageInput != nil && !model.IsMontagePlatform(taskType) {
		return nil, fmt.Errorf("%w: montage_input can only be set on montage tasks", ErrMontageInput)
	}

	// E-commerce: merge the PROJECT's reusable e-commerce defaults (default
	// modules, target platform, brand brief, image model key) into the task config
	// with task-level explicit values winning. Product photos and selling points
	// stay per-task. Done before validation so selected modules are available to
	// the agent; modules shape later MCP usage, not the creation-time base fee.
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
	if model.IsVideoPlatform(taskType) {
		quantity = 1
	}
	if model.IsMontagePlatform(taskType) {
		quantity = 1
		if p.MontageInput == nil || strings.TrimSpace(p.MontageInput.Brief) == "" {
			return nil, fmt.Errorf("%w: montage task requires brief", ErrMontageInput)
		}
		target, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
			Config:          s.montageCfg,
			TaskType:        taskType,
			LocalAvailable:  containsMontageTarget(s.montageCfg.ExecutionTargets, model.ExecutionTargetLocal),
			CloudAvailable:  s.montageCloudAvailable(),
			AssetsCloudSafe: true,
		})
		if err != nil {
			return nil, err
		}
		p.ExecutionTarget = target
	}
	if model.IsVideoEditorPlatform(taskType) && !hasVideoEditorSourceVideo(videoInput, p.InputAttachments) {
		return nil, fmt.Errorf("videoeditor task requires at least one source video")
	}
	if taskType == model.PlatformEcommerce {
		// E-commerce creates one deliverable package task. Selected modules
		// influence later image/vision MCP usage; creation deducts only the
		// configured base task service fee when billing is enabled.
		quantity = 1
		if p.Ecommerce == nil || len(p.Ecommerce.SelectedModules) == 0 {
			return nil, fmt.Errorf("ecommerce task requires at least one selected module")
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
		{
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

			// Deduct total base service fees in a single atomic transaction.
			// Claude Code runtime cost is platform-paid and is not reserved or
			// settled against the user's balance.
			var deductErr error
			if p.ExecutionTarget == model.ExecutionTargetLocal {
				if multiplier > 1 {
					deductErr = s.creditSvc.DeductBatchWithMultiplier(ctx, p.UserID, taskType, totalCost, taskIDs, multiplier)
				} else {
					deductErr = s.creditSvc.DeductBatch(ctx, p.UserID, taskType, totalCost, taskIDs)
				}
			} else {
				deductErr = s.creditSvc.DeductBatchForTaskCreation(ctx, p.UserID, taskType, totalCost, taskIDs, multiplier)
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
		// topic-driven platform (article/seednote/moments), claim the next unused topic
		// from the pool as this task's prompt — mirroring CreateFromPlan. Ecommerce
		// is product-image based with no topic semantics, so it is skipped. The pool
		// may run out mid-batch; remaining tasks keep an empty prompt and fall back
		// to the agent's auto-research path. The generation skill respects an
		// already-set topic, so it will not re-claim during execution.
		taskPrompt := p.Prompt
		if taskPrompt == "" && s.topicPoolSvc != nil &&
			(taskType == model.PlatformArticle || taskType == model.PlatformSeednote || taskType == model.PlatformMoments) {
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
		if p.ProjectSnapshot != nil {
			task.SetProjectSnapshot(*p.ProjectSnapshot)
		} else {
			task.SetProjectSnapshot(model.SnapshotProject(project))
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
		if len(p.InputAttachments) > 0 {
			task.SetInputAttachments(p.InputAttachments)
		}
		if model.IsVideoPlatform(taskType) {
			if videoInput != nil {
				task.SetVideoInput(*videoInput)
			} else if videoCfg != nil {
				task.SetVideoInput(videoInputFromTaskConfig(taskPrompt, videoCfg))
			} else if model.IsVideoCreatorPlatform(taskType) && strings.TrimSpace(taskPrompt) != "" {
				task.SetVideoInput(model.VideoInput{Brief: taskPrompt})
			}
		}
		if model.IsMontagePlatform(taskType) && p.MontageInput != nil {
			task.SetMontageInput(*p.MontageInput)
		}

		if err := s.repo.Tasks().Create(ctx, task); err != nil {
			// Refund only the tasks that were NOT successfully created.
			// deductedTaskIDs[0..i) were created successfully; [i..) were not.
			if s.creditSvc != nil {
				for j := i; j < len(deductedTaskIDs); j++ {
					if refundErr := s.creditSvc.RefundForTask(ctx, deductedTaskIDs[j]); refundErr != nil {
						s.logger.Error().Err(refundErr).Str("task_id", deductedTaskIDs[j]).Msg("failed to refund credits during rollback")
					}
					if refundErr := s.creditSvc.RefundAgentRuntimeReserve(ctx, deductedTaskIDs[j], "任务创建失败退还 Claude Code 运行预留"); refundErr != nil {
						s.logger.Error().Err(refundErr).Str("task_id", deductedTaskIDs[j]).Msg("failed to refund runtime reserve during rollback")
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

func (p CreateManualParams) videoPayloadForTask(taskType string) (*model.VideoTaskConfig, *model.VideoInput, error) {
	hasCreatorPayload := p.VideoCreatorConfig != nil || p.VideoCreatorInput != nil
	hasEditorPayload := p.VideoEditorConfig != nil || p.VideoEditorInput != nil

	switch {
	case model.IsVideoCreatorPlatform(taskType):
		if hasEditorPayload {
			return nil, nil, fmt.Errorf("%w: video_editor_input/video_editor_config can only be set on videoeditor tasks", ErrVideoTaskInput)
		}
		return p.VideoCreatorConfig, p.VideoCreatorInput, nil
	case model.IsVideoEditorPlatform(taskType):
		if hasCreatorPayload {
			return nil, nil, fmt.Errorf("%w: video_creator_input/video_creator_config can only be set on videocreator tasks", ErrVideoTaskInput)
		}
		return p.VideoEditorConfig, p.VideoEditorInput, nil
	default:
		if hasCreatorPayload || hasEditorPayload {
			return nil, nil, fmt.Errorf("%w: video_creator_input/video_editor_input can only be set on videocreator or videoeditor tasks", ErrVideoTaskInput)
		}
		return nil, nil, nil
	}
}

func hasVideoEditorSourceVideo(input *model.VideoInput, attachments []model.EntryAttachment) bool {
	if input != nil {
		for _, ref := range input.References {
			if ref.Type == VideoReferenceVideo || ref.Type == "video_url" {
				if strings.TrimSpace(ref.URL) != "" || strings.TrimSpace(ref.TaskFileID) != "" {
					return true
				}
			}
		}
	}
	for _, attachment := range attachments {
		if normalizeEntryAttachmentType(attachment.Type, attachment.ContentType) == "video" && strings.TrimSpace(attachment.URL) != "" {
			return true
		}
	}
	return false
}

func videoRequestFromTaskConfig(prompt string, cfg *model.VideoTaskConfig) VideoGenerationRequest {
	req := VideoGenerationRequest{Prompt: prompt}
	if cfg == nil {
		return req
	}
	req.ScenarioKey = cfg.ScenarioKey
	req.ProductionMode = cfg.ProductionMode
	req.Purpose = cfg.Purpose
	req.CreativeType = cfg.CreativeType
	req.SubjectProfile = cfg.SubjectProfile
	req.Audience = cfg.Audience
	req.SingleMessage = cfg.SingleMessage
	req.Model = cfg.ModelKey
	req.Resolution = cfg.Resolution
	req.Ratio = cfg.Ratio
	req.Duration = cfg.Duration
	req.TargetDurationReason = cfg.TargetDurationReason
	req.Watermark = cfg.Watermark
	req.Preflight = &cfg.Preflight
	req.RetakeBudget = cfg.RetakeBudget
	req.DeliveryTargets = cfg.DeliveryTargets
	req.ReferenceSet = videoReferencesFromAssets(cfg.References)
	return req
}

func videoInputFromTaskConfig(prompt string, cfg *model.VideoTaskConfig) model.VideoInput {
	input := model.VideoInput{Brief: strings.TrimSpace(prompt)}
	if cfg == nil {
		return input
	}
	input.References = cfg.References
	input.HardConstraints = model.VideoHardConstraints{
		Ratio:     cfg.Ratio,
		Duration:  cfg.Duration,
		Watermark: cfg.Watermark,
	}
	return input
}

func videoReferencesFromAssets(assets []model.VideoReferenceAsset) []VideoReferenceInput {
	if len(assets) == 0 {
		return nil
	}
	refs := make([]VideoReferenceInput, 0, len(assets))
	for _, asset := range assets {
		refs = append(refs, VideoReferenceInput{
			Type:                 asset.Type,
			URL:                  asset.URL,
			Text:                 asset.Text,
			ReferenceRole:        asset.ReferenceRole,
			MustKeep:             asset.MustKeep,
			CanChange:            asset.CanChange,
			MustNotTransfer:      asset.MustNotTransfer,
			InputDurationSeconds: asset.InputDurationSeconds,
		})
	}
	return refs
}

func videoAssetsFromReferences(refs []VideoReferenceInput) []model.VideoReferenceAsset {
	if len(refs) == 0 {
		return nil
	}
	assets := make([]model.VideoReferenceAsset, 0, len(refs))
	for _, ref := range refs {
		assets = append(assets, model.VideoReferenceAsset{
			Type:                 ref.Type,
			URL:                  ref.URL,
			Text:                 ref.Text,
			ReferenceRole:        ref.ReferenceRole,
			MustKeep:             ref.MustKeep,
			CanChange:            ref.CanChange,
			MustNotTransfer:      ref.MustNotTransfer,
			InputDurationSeconds: ref.InputDurationSeconds,
		})
	}
	return assets
}

func videoTaskConfigFromPlan(plan VideoGenerationPlan) model.VideoTaskConfig {
	return model.VideoTaskConfig{
		ScenarioKey:               plan.ScenarioKey,
		ProductionMode:            plan.ProductionMode,
		Purpose:                   plan.Purpose,
		CreativeType:              plan.CreativeType,
		SubjectProfile:            plan.SubjectProfile,
		Audience:                  plan.Audience,
		SingleMessage:             plan.SingleMessage,
		ModelKey:                  plan.ModelKey,
		Model:                     plan.Model,
		Resolution:                plan.Resolution,
		Ratio:                     plan.Ratio,
		Duration:                  plan.Duration,
		TargetDurationSeconds:     plan.TargetDurationSeconds,
		TargetDurationSource:      plan.TargetDurationSource,
		TargetDurationReason:      plan.TargetDurationReason,
		SegmentMaxDurationSeconds: plan.SegmentMaxDurationSeconds,
		SegmentMinDurationSeconds: plan.SegmentMinDurationSeconds,
		Segments:                  videoTaskSegmentsFromPlan(plan.Segments),
		Watermark:                 plan.Watermark,
		Preflight:                 plan.Preflight,
		References:                videoAssetsFromReferences(plan.References),
		RetakeBudget:              plan.RetakeBudget,
		DeliveryTargets:           plan.DeliveryTargets,
		EstimatedCredits:          plan.EstimatedCredits,
		PricingBreakdown:          plan.PricingBreakdown,
	}
}

func videoTaskSegmentsFromPlan(segments []VideoGenerationSegmentPlan) []model.VideoTaskSegmentConfig {
	if len(segments) == 0 {
		return nil
	}
	out := make([]model.VideoTaskSegmentConfig, 0, len(segments))
	for _, seg := range segments {
		out = append(out, model.VideoTaskSegmentConfig{
			Index:            seg.Index,
			StartSecond:      seg.StartSecond,
			EndSecond:        seg.EndSecond,
			Duration:         seg.Duration,
			Prompt:           seg.Prompt,
			ModelKey:         seg.ModelKey,
			Model:            seg.Model,
			Resolution:       seg.Resolution,
			Ratio:            seg.Ratio,
			EstimatedCredits: seg.EstimatedCredits,
		})
	}
	return out
}

// CreateFromPlan creates a task linked to a plan and enqueues it for execution.
//
// The plan's scheduling-adjacent "what to produce" params (image model,
// reference image, watermark, goal, seednote image composition) flow to the task.
// Project/account style config is frozen from the project into ProjectSnapshot.
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
	var project *model.Project
	if plan.ProjectID != "" {
		if found, err := s.repo.Projects().FindByID(ctx, plan.ProjectID); err == nil {
			project = found
			taskType = found.Platform
		}
	}
	var planVideoInput *model.VideoInput
	if model.IsVideoCreatorPlatform(taskType) {
		vi := plan.VideoInput.Data()
		if vi.Brief == "" && prompt != "" {
			vi.Brief = prompt
		}
		planVideoInput = &vi
	}
	var planMontageInput *model.MontageInput
	montageExecutionTarget := model.ExecutionTargetCloud
	if model.IsMontagePlatform(taskType) {
		input := plan.MontageInput.Data()
		planMontageInput = &input
		target, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
			Config:          s.montageCfg,
			TaskType:        taskType,
			FromPlan:        true,
			CloudAvailable:  s.montageCloudAvailable(),
			AssetsCloudSafe: true,
		})
		if err != nil {
			s.logger.Warn().Err(err).Str("user_id", plan.UserID).Str("plan_id", plan.ID).Msg("skipping montage plan task due to execution target policy")
			return nil, nil
		}
		montageExecutionTarget = target
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
		} else if _, err := s.creditSvc.DeductForTaskCreation(ctx, plan.UserID, taskType, taskID, planMultiplier); err != nil {
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
		PlanID:                   &plan.ID,
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
	if model.IsMontagePlatform(taskType) {
		task.ExecutionTarget = montageExecutionTarget
	}
	if project != nil {
		task.SetProjectSnapshot(model.SnapshotProject(project))
	}
	if planVideoInput != nil {
		task.SetVideoInput(*planVideoInput)
	}
	if planMontageInput != nil {
		task.SetMontageInput(*planMontageInput)
	}

	if err := s.repo.Tasks().Create(ctx, task); err != nil {
		// Refund the deducted credits if task creation fails.
		if s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForTask(ctx, taskID); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund credits during plan task rollback")
			}
			if refundErr := s.creditSvc.RefundAgentRuntimeReserve(ctx, taskID, "计划任务创建失败退还 Claude Code 运行预留"); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund runtime reserve during plan task rollback")
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

// List returns tasks for a user with optional status, project, and plan filters and pagination.
func (s *TaskService) List(ctx context.Context, userID string, offset, limit int, status, projectID, planID string) ([]*model.Task, int64, error) {
	var tasks []*model.Task
	var err error

	if status != "" {
		tasks, err = s.repo.Tasks().FindByUserIDAndStatus(ctx, userID, status, projectID, planID, offset, limit)
	} else {
		tasks, err = s.repo.Tasks().FindByUserID(ctx, userID, projectID, planID, offset, limit)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}

	var total int64
	if status != "" {
		total, err = s.repo.Tasks().CountByUserIDAndStatus(ctx, userID, status, projectID, planID)
	} else {
		total, err = s.repo.Tasks().CountByUserID(ctx, userID, projectID, planID)
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
	return s.cancel(ctx, id, "")
}

// CancelForUser cancels a user-owned pending/running task. The status transition
// is guarded by task_id + user_id so callers cannot cancel another account's
// task even if they know its ID.
func (s *TaskService) CancelForUser(ctx context.Context, userID, id string) error {
	if userID == "" {
		return fmt.Errorf("user_id is required")
	}
	return s.cancel(ctx, id, userID)
}

func (s *TaskService) cancel(ctx context.Context, id, userID string) error {
	// Fetch task before CAS so we can decide refund strategy.
	task, taskErr := s.repo.Tasks().FindByID(ctx, id)
	if userID != "" && taskErr == nil && task != nil && task.UserID != userID {
		return fmt.Errorf("task not found")
	}

	// Atomically transition status: only pending or running can be cancelled.
	swapped, err := s.compareAndSwapCancelStatus(ctx, id, userID, model.TaskStatusRunning)
	if err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	if !swapped {
		// Also try pending → cancelled (task may not have started running yet).
		swapped, err = s.compareAndSwapCancelStatus(ctx, id, userID, model.TaskStatusPending)
		if err != nil {
			return fmt.Errorf("cancel task: %w", err)
		}
		if !swapped {
			return fmt.Errorf("task is not in a cancellable state (current status is not pending or running)")
		}
	}
	// Refund credits for the cancelled task (idempotent — double-refund protected).
	if s.creditSvc != nil && task != nil {
		s.refundTaskByMode(ctx, task, "取消")
		if task.Status == model.TaskStatusPending {
			if refundErr := s.creditSvc.RefundAgentRuntimeReserve(ctx, task.ID, "任务取消退还 Claude Code 运行预留"); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("task_id", task.ID).Msg("failed to refund runtime reserve for cancelled pending task")
			}
		}
	}
	if task != nil {
		if err := s.repo.Tasks().SetCompletedAt(ctx, id); err != nil {
			s.logger.Error().Err(err).Str("task_id", id).Msg("failed to set completed_at on cancellation")
		}
		s.notifyTerminal(ctx, task, model.TaskStatusCancelled, "用户取消")
	}
	// Release concurrency slot.
	if s.pubsub != nil {
		var projectID string
		if task != nil {
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

func (s *TaskService) compareAndSwapCancelStatus(ctx context.Context, id, userID, expected string) (bool, error) {
	if userID != "" {
		return s.repo.Tasks().CompareAndSwapStatusForUser(ctx, id, userID, expected, model.TaskStatusCancelled)
	}
	return s.repo.Tasks().CompareAndSwapStatus(ctx, id, expected, model.TaskStatusCancelled)
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
		maxConcurrent := s.effectiveProjectMaxConcurrent(project)

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

	maxConcurrent := s.effectiveProjectMaxConcurrent(project)

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
