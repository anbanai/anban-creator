package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

// TaskEnqueuer abstracts the async task enqueue mechanism (Asynq, in-process, etc.).
type TaskEnqueuer interface {
	Enqueue(taskType string, payload []byte) error
	EnqueueIn(taskType string, payload []byte, delay time.Duration) error
}

type UniqueTaskEnqueuer interface {
	EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error)
}

var (
	ErrTaskCapabilityAccessDenied              = errors.New("task capability access denied")
	ErrManagedProfileLocalExecutionUnsupported = errors.New("managed Agent profiles require cloud execution")
)

func validateAgentExecutionTarget(target string) error {
	switch strings.TrimSpace(target) {
	case model.ExecutionTargetCloud:
		return nil
	case model.ExecutionTargetLocal, model.ExecutionTargetLocalClaimed:
		return ErrManagedProfileLocalExecutionUnsupported
	default:
		return fmt.Errorf("invalid execution target %q", target)
	}
}

// TypeContentGenerate is the Asynq task type for content generation.
const TypeContentGenerate = "content:generate"

// TaskService handles task CRUD, manual creation, and execution orchestration.
type TaskService struct {
	repo                     repository.Repository
	runtimeDispatcher        agent.RuntimeDispatcher
	dispatchLeaseDuration    time.Duration
	dispatchBeforeCreate     func()
	finalizationAfterStage   func(string) error
	finalizationAfterEffect  func(string) error
	finalizationAfterAdvance func(string) error
	finalizationLease        time.Duration
	finalizationRenewEvery   time.Duration
	finalizationRenewClaim   func(context.Context, string, string) (bool, error)
	localFinalizationLocks   sync.Map
	cleanupRetryBackoff      time.Duration
	projectConcurrencyCap    int
	logger                   *zerolog.Logger
	enqueuer                 TaskEnqueuer
	store                    storage.Provider
	publishingSvc            *PublishingService
	taskLogDir               string
	pubsub                   *RedisPubSub
	pubsubCancel             context.CancelFunc // stops the listenCancelEvents goroutine
	cancelFuncs              sync.Map           // taskID → context.CancelFunc
	montageCfg               srvconfig.MontageConfig
	// ilinkNotifier enqueues task success/failure/cancel messages for delivery
	// through the platform WeChat assistant. Nil when ilink is disabled.
	ilinkNotifier *IlinkNotifier
	topicPoolSvc  *TopicPoolService
	// executionTimeout bounds the fallback (Redis-down) in-process execution.
	// The asynq path is bounded by the asynq task Timeout (see scheduler).
	// Default 60m; override via SetExecutionTimeouts.
	executionTimeout time.Duration
	// persistTimeout bounds the post-execution DB writes (result/files/status),
	// decoupled from the execution ctx so completed work is saved even on overrun.
	// Default 10m; override via SetExecutionTimeouts.
	persistTimeout time.Duration
	// maxTurnsOverrides feeds the local-executor claim response.
	maxTurnsOverrides map[string]int
	nasResumeEnabled  bool
	taskWorkspace     TaskWorkspaceLifecycle
	referenceAssets   *ReferenceAssetService
	providerCostSvc   *ProviderCostService
	billingWalletSvc  *BillingWalletService
	billingCatalogSvc *BillingCatalogService
	agentProfiles     *AgentProfileRegistry
	imageCapabilities *ImageCapabilityResolver
}

type TaskWorkspaceLifecycle interface {
	DeleteTaskWorkspace(context.Context, *model.Task) error
}

// NewTaskService creates a new TaskService.
// If pubsub is nil, cross-replica cancel signaling and progress events are disabled.
func NewTaskService(
	repo repository.Repository,
	enqueuer TaskEnqueuer,
	store storage.Provider,
	logger *zerolog.Logger,
	taskLogDir string,
	pubsub *RedisPubSub,
	publishingSvc *PublishingService,
) *TaskService {
	svc := &TaskService{
		repo:                   repo,
		logger:                 logger,
		enqueuer:               enqueuer,
		store:                  store,
		publishingSvc:          publishingSvc,
		taskLogDir:             taskLogDir,
		pubsub:                 pubsub,
		executionTimeout:       60 * time.Minute,
		persistTimeout:         10 * time.Minute,
		finalizationLease:      time.Minute,
		finalizationRenewEvery: 15 * time.Second,
		cleanupRetryBackoff:    10 * time.Second,
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

// Storage returns the configured provider for handler-level upload verification.
func (s *TaskService) Storage() storage.Provider {
	if s == nil {
		return nil
	}
	return s.store
}

func (s *TaskService) SetTaskWorkspaceLifecycle(workspace TaskWorkspaceLifecycle) {
	s.taskWorkspace = workspace
}

func (s *TaskService) SetReferenceAssetService(referenceAssets *ReferenceAssetService) {
	if s != nil {
		s.referenceAssets = referenceAssets
	}
}

func (s *TaskService) SetProviderCostService(providerCostSvc *ProviderCostService) {
	if s != nil {
		s.providerCostSvc = providerCostSvc
	}
}

func (s *TaskService) SetBillingWalletService(wallet *BillingWalletService) {
	if s != nil {
		s.billingWalletSvc = wallet
	}
}

func (s *TaskService) SetBillingCatalogService(catalog *BillingCatalogService) {
	if s != nil {
		s.billingCatalogSvc = catalog
		if catalog != nil && s.agentProfiles != nil {
			catalog.SetAgentProfileRegistry(s.agentProfiles)
		}
	}
}

func (s *TaskService) SetAgentProfileRegistry(registry *AgentProfileRegistry) {
	if s != nil {
		s.agentProfiles = registry
		if s.billingCatalogSvc != nil {
			s.billingCatalogSvc.SetAgentProfileRegistry(registry)
		}
	}
}

func (s *TaskService) SetImageCapabilityResolver(resolver *ImageCapabilityResolver) {
	if s != nil {
		s.imageCapabilities = resolver
	}
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
	return s != nil && s.runtimeDispatcher != nil
}

// Close stops the Redis pub/sub subscriber goroutine.
// Call this during server graceful shutdown.
func (s *TaskService) Close() {
	if s.pubsubCancel != nil {
		s.pubsubCancel()
	}
}

func (s *TaskService) SetIlinkNotifier(n *IlinkNotifier) {
	s.ilinkNotifier = n
}

func (s *TaskService) notifyTerminal(ctx context.Context, task *model.Task, status, errMsg string) {
	if s.ilinkNotifier != nil {
		s.ilinkNotifier.NotifyTerminal(ctx, task, status, errMsg)
	}
}

func (s *TaskService) notifyTerminalDurable(ctx context.Context, task *model.Task, status, errMsg string) error {
	if s.ilinkNotifier == nil {
		return nil
	}
	return s.ilinkNotifier.NotifyTerminalDurable(ctx, task, status, errMsg)
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

func (s *TaskService) SetExecutorMaxTurns(maxTurnsOverrides map[string]int) {
	s.maxTurnsOverrides = maxTurnsOverrides
}

func (s *TaskService) SetProjectConcurrencyCap(cap int) {
	if cap < 0 {
		cap = 0
	}
	s.projectConcurrencyCap = cap
}

// SetNASResumeEnabled enables task continuation against durable managed workspaces.
func (s *TaskService) SetNASResumeEnabled(enabled bool) {
	s.nasResumeEnabled = enabled
}

func (s *TaskService) effectiveProjectMaxConcurrent(project *model.Project) int {
	maxConcurrent := DefaultMaxConcurrentTasks
	if project != nil && project.MaxConcurrentTasks > 0 {
		maxConcurrent = project.MaxConcurrentTasks
	}
	if s.projectConcurrencyCap > 0 && s.projectConcurrencyCap < maxConcurrent {
		return s.projectConcurrencyCap
	}
	return maxConcurrent
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

var (
	ErrMontageInput                         = errors.New("montage input invalid")
	ErrTaskCreationProjectInactive          = errors.New("task creation project is not active")
	ErrViralAnalysisRequiresSeednoteProject = errors.New("viral analysis requires a Seednote project")
)

func cloneEntryAttachments(in []model.EntryAttachment) []model.EntryAttachment {
	return append([]model.EntryAttachment(nil), in...)
}

// CreateManualParams holds the inputs for CreateManual. Fields map 1:1 to the
// model.Task attributes that callers can supply at creation time. Using a struct
// instead of a long positional signature keeps call sites readable as fields are
// added and prevents argument-order bugs.
type CreateManualParams struct {
	UserID            string
	ProjectID         string
	ExecutionProfile  string
	RequestedTaskType string
	// FrozenTaskType and PreserveFrozenConfig are internal clone controls. They
	// keep a clone on the source task contract even when the project changes.
	FrozenTaskType       string
	PreserveFrozenConfig bool
	Prompt               string
	Quantity             int
	ImageRatio           string
	ImageCapabilityKey   string
	// frozenImageCapabilitySnapshot is trusted internal clone state. New task
	// admission freezes a fresh snapshot; clone-without-overrides preserves it.
	frozenImageCapabilitySnapshot *model.ImageCapabilitySnapshot
	SkipRefImage                  *bool
	ReferenceImageAssetID         string
	UsePortraitReference          *bool
	// InputSourceTaskID is internal clone provenance. When set, bootstrap may
	// reuse input objects from this task's exact user/project/task prefix.
	InputSourceTaskID string
	// InputSourceProjectID is the project that owns InputSourceTaskID. It is
	// internal clone provenance and must be persisted with the source task ID.
	InputSourceProjectID string
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
	// AgentInput carries Pack-defined extension fields. Existing typed business
	// fields remain authoritative; this map is accepted only when the resolved
	// Pack declares a task_input Schema.
	AgentInput map[string]any
	Watermark  *bool
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
	// MontageInput carries the Montage-specific creation contract.
	// It is only valid for montage projects.
	MontageInput *model.MontageInput
	// ExecutionTarget, when model.ExecutionTargetLocal, routes the task to a
	// desktop local executor instead of cloud Asynq/Docker. The task is created
	// pending with a LocalClaimDeadline and is NOT enqueued; a desktop claims it
	// via ClaimLocalTask. Unclaimed tasks fall back to cloud after the deadline
	// (ReclaimExpiredLocalTasks). Empty = cloud (default).
	ExecutionTarget string
}

// ResolveTaskCreationProject loads the authoritative project once and applies
// the ownership/status checks required before task side effects.
func (s *TaskService) ResolveTaskCreationProject(ctx context.Context, userID, projectID string) (*model.Project, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("task repository is unavailable")
	}
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: %w", ErrProjectNotFound, err)
		}
		return nil, fmt.Errorf("find project: %w", err)
	}
	if err := validateTaskCreationProject(project, userID, projectID); err != nil {
		return nil, err
	}
	return project, nil
}

func validateTaskCreationProject(project *model.Project, userID, projectID string) error {
	if project == nil || project.ID != projectID {
		return ErrProjectNotFound
	}
	if project.UserID != userID {
		return ErrProjectOwnedByUser
	}
	if project.Status != model.ProjectStatusActive {
		return ErrTaskCreationProjectInactive
	}
	return nil
}

func (s *TaskService) validateTaskCreationReferences(ctx context.Context, userID, taskAssetID string, project *model.Project, snapshot *model.ProjectSnapshot) error {
	taskAssetPurposes := []string{DirectUploadPurposeTaskReference, DirectUploadPurposeProjectPortraitReference, DirectUploadPurposeAIEntryAttachment}
	checks := []struct {
		assetID string
		allowed []string
	}{
		{assetID: taskAssetID, allowed: taskAssetPurposes},
	}
	if snapshot != nil {
		checks = append(checks, struct {
			assetID string
			allowed []string
		}{assetID: snapshot.ReferenceImageAssetID, allowed: []string{DirectUploadPurposeProjectReference}})
		checks = append(checks, struct {
			assetID string
			allowed []string
		}{assetID: snapshot.PortraitReferenceImageAssetID, allowed: []string{DirectUploadPurposeProjectPortraitReference}})
	} else if project != nil {
		checks = append(checks, struct {
			assetID string
			allowed []string
		}{assetID: project.ReferenceImageAssetID, allowed: []string{DirectUploadPurposeProjectReference}})
		checks = append(checks, struct {
			assetID string
			allowed []string
		}{assetID: project.PortraitReferenceImageAssetID, allowed: []string{DirectUploadPurposeProjectPortraitReference}})
	}
	for _, check := range checks {
		if check.assetID == "" {
			continue
		}
		if s.referenceAssets == nil {
			return ErrReferenceAssetUnavailable
		}
		if _, err := s.referenceAssets.RequireOwned(ctx, userID, check.assetID, check.allowed); err != nil {
			return err
		}
	}
	return nil
}

// CreateManual creates tasks without a plan and enqueues them for execution.
// The Quantity field (1-5) determines how many tasks to create, each independently billed.
// ImageCapabilityKey optionally selects a per-task image model (validated upstream by the handler).
//
// Style/author/theme dimensions are snapshotted from the project at creation.
// Editing the project later does not change existing pending/running tasks.
func (s *TaskService) CreateManual(ctx context.Context, p CreateManualParams) ([]*model.Task, error) {
	if strings.TrimSpace(p.ExecutionProfile) == "" {
		return nil, fmt.Errorf("execution_profile is required")
	}
	if err := validateAgentExecutionTarget(p.ExecutionTarget); err != nil {
		return nil, err
	}
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

	project, err := s.ResolveTaskCreationProject(ctx, p.UserID, p.ProjectID)
	if err != nil {
		return nil, err
	}
	profile, err := resolveAgentProfileForUser(ctx, s.repo, s.agentProfiles, p.UserID, p.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	p.ExecutionProfile = profile.ID
	profileSnapshot, profileFingerprint, err := profile.Freeze()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentProfileSnapshotInvalid, err)
	}
	taskType := project.Platform
	effectiveReferenceImageAssetID := p.ReferenceImageAssetID
	if taskType == model.PlatformArticle && p.UsePortraitReference != nil && *p.UsePortraitReference {
		effectiveReferenceImageAssetID = project.PortraitReferenceImageAssetID
		if effectiveReferenceImageAssetID == "" {
			return nil, fmt.Errorf("公众号项目未配置人物参考图，请先在项目设置中上传")
		}
	}
	if err := s.validateTaskCreationReferences(ctx, p.UserID, effectiveReferenceImageAssetID, project, p.ProjectSnapshot); err != nil {
		return nil, err
	}

	if p.RequestedTaskType == model.TaskTypeViralAnalysis {
		if project.Platform != model.PlatformSeednote {
			return nil, ErrViralAnalysisRequiresSeednoteProject
		}
		taskType = model.TaskTypeViralAnalysis
	}
	if p.FrozenTaskType != "" {
		taskType = p.FrozenTaskType
	}
	isMontageTask := model.IsMontagePlatform(taskType)
	effectiveImageRatio := strings.TrimSpace(p.ImageRatio)
	if isMontageTask && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" {
		effectiveImageRatio = strings.TrimSpace(project.ImageRatio)
	}
	if isMontageTask && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" {
		effectiveImageRatio = model.DefaultImageRatio(project.Platform)
	}
	if len(model.SupportedImageRatios(project.Platform)) > 0 && !model.IsBusinessImageRatioAllowed(project.Platform, effectiveImageRatio) {
		return nil, fmt.Errorf("%s for platform %s: %s", model.ValidImageRatioHint, project.Platform, effectiveImageRatio)
	}
	agentInput, err := validateAndCloneAgentInput(taskType, p.AgentInput)
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
	effectiveImageCapabilityKey := strings.TrimSpace(p.ImageCapabilityKey)
	if taskType == model.PlatformEcommerce && !p.PreserveFrozenConfig {
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
		if effectiveImageCapabilityKey == "" {
			effectiveImageCapabilityKey = strings.TrimSpace(projEc.ImageCapabilityKey)
		}
	}
	var effectiveImageCapabilitySnapshot model.ImageCapabilitySnapshot
	if taskUsesFrozenImageCapability(taskType) {
		if s.imageCapabilities == nil {
			return nil, fmt.Errorf("resolve task image capability: %w", ErrImageCapabilityResolverUnavailable)
		}
		if p.frozenImageCapabilitySnapshot != nil {
			effectiveImageCapabilitySnapshot = *p.frozenImageCapabilitySnapshot
			if err := s.imageCapabilities.ValidateFrozenImageCapability(ctx, p.UserID, effectiveImageCapabilitySnapshot); err != nil {
				return nil, fmt.Errorf("validate cloned task image capability: %w", err)
			}
		} else {
			effectiveImageCapabilitySnapshot, err = s.imageCapabilities.FreezeImageCapability(ctx, p.UserID, effectiveImageCapabilityKey)
			if err != nil {
				return nil, fmt.Errorf("resolve task image capability: %w", err)
			}
		}
		effectiveImageCapabilityKey = effectiveImageCapabilitySnapshot.Key
	}
	if isMontageTask {
		quantity = 1
		if p.MontageInput == nil || strings.TrimSpace(p.MontageInput.Brief) == "" {
			return nil, fmt.Errorf("%w: montage task requires brief", ErrMontageInput)
		}
		if !p.PreserveFrozenConfig {
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
	}
	if err := validateAgentExecutionTarget(p.ExecutionTarget); err != nil {
		return nil, err
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

	tasks := make([]*model.Task, 0, quantity)

	for i := 0; i < quantity; i++ {
		taskID := generateTaskID()

		// Topic pool: when the caller supplied no prompt and the project is a
		// topic-driven platform (article/seednote/moments), claim the next unused topic
		// from the pool as this task's prompt — mirroring CreateFromPlan. Ecommerce
		// is product-image based with no topic semantics, so it is skipped. The pool
		// may run out mid-batch; remaining tasks keep an empty prompt and fall back
		// to the agent's auto-research path. The generation skill respects an
		// already-set topic, so it will not re-claim during execution.
		taskPrompt := p.Prompt
		if taskPrompt == "" && !p.PreserveFrozenConfig && s.topicPoolSvc != nil &&
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
			ImageRatio:               effectiveImageRatio,
			ImageCapabilityKey:       effectiveImageCapabilityKey,
			ReferenceImageAssetID:    effectiveReferenceImageAssetID,
			InputSourceTaskID:        p.InputSourceTaskID,
			InputSourceProjectID:     p.InputSourceProjectID,
			SkipReferenceImage:       p.SkipRefImage != nil && *p.SkipRefImage,
			Watermark:                p.Watermark != nil && *p.Watermark,
			HasContentImage:          hasContent,
			HasTailImage:             hasTail,
			ArticleWithCover:         &articleCover,
			ArticleWithContentImages: &articleContent,
			ExecutionTarget:          p.ExecutionTarget,
			ExecutionProfile:         p.ExecutionProfile,
			AgentProfileSnapshot:     profileSnapshot,
			AgentProfileFingerprint:  profileFingerprint,
		}
		if p.ProjectSnapshot != nil {
			task.SetProjectSnapshot(*p.ProjectSnapshot)
		} else {
			task.SetProjectSnapshot(model.SnapshotProject(project))
		}
		if taskUsesFrozenImageCapability(taskType) {
			task.SetImageCapabilitySnapshot(effectiveImageCapabilitySnapshot)
		}
		if p.Overrides != nil {
			task.SetOverrides(*p.Overrides)
		}
		if p.Ecommerce != nil {
			task.SetEcommerce(*p.Ecommerce)
		}
		if len(p.InputAttachments) > 0 {
			task.SetInputAttachments(cloneEntryAttachments(p.InputAttachments))
		}
		if agentInput != nil {
			task.SetAgentInput(agentInput)
		}
		if isMontageTask && p.MontageInput != nil {
			task.SetMontageInput(*p.MontageInput)
		}

		tasks = append(tasks, task)
	}
	if err := s.persistTasksWithFixedAdmission(ctx, tasks, p.HasContentImage); err != nil {
		if s.topicPoolSvc != nil {
			for _, task := range tasks {
				if relErr := s.topicPoolSvc.ReleaseForTask(ctx, task.ID); relErr != nil {
					s.logger.Error().Err(relErr).Str("task_id", task.ID).Msg("failed to release topic during batch rollback")
				}
			}
		}
		return nil, fmt.Errorf("create tasks: %w", err)
	}

	// Batch admission is fully committed before any task is dispatched.
	for _, task := range tasks {
		if err := s.EnqueueExecution(ctx, task, nil); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("failed to enqueue task, marking as failed")
			persistCtx, cancel := context.WithTimeout(context.Background(), s.persistTimeout)
			failErr := s.failPendingAdmittedTask(persistCtx, task, model.TaskBillingTerminalPlatformError, "failed to enqueue: "+err.Error())
			cancel()
			if failErr != nil {
				return nil, fmt.Errorf("finalize failed task enqueue: %w", failErr)
			}
		}
	}

	return tasks, nil
}

func (s *TaskService) persistTaskWithFixedAdmission(ctx context.Context, task *model.Task) error {
	if task == nil {
		return fmt.Errorf("task is required")
	}
	return s.persistTasksWithFixedAdmission(ctx, []*model.Task{task})
}

func (s *TaskService) persistTasksWithFixedAdmission(ctx context.Context, tasks []*model.Task, options ...interface{}) error {
	if len(tasks) == 0 {
		return fmt.Errorf("at least one task is required")
	}
	for _, task := range tasks {
		if task == nil {
			return fmt.Errorf("task is required")
		}
	}
	var hasContentImageOverride *bool
	allowMissingPlanID := ""
	for _, option := range options {
		switch value := option.(type) {
		case *bool:
			hasContentImageOverride = value
		case string:
			allowMissingPlanID = value
		}
	}
	explicitlyDisableContentImage := hasContentImageOverride != nil && !*hasContentImageOverride
	lockProjectAdmission := func(repo repository.Repository) error {
		project, err := repo.Projects().FindByIDForUpdate(ctx, tasks[0].ProjectID)
		if err != nil {
			return fmt.Errorf("lock project for task admission: %w", err)
		}
		if project.Status != model.ProjectStatusActive || project.DeletingAt != nil {
			return fmt.Errorf("project is not active")
		}
		for _, task := range tasks {
			if task.ProjectID != project.ID || task.UserID != project.UserID {
				return fmt.Errorf("task admission project identity mismatch")
			}
		}
		for _, task := range tasks {
			if task.PlanID == nil || strings.TrimSpace(*task.PlanID) == "" {
				continue
			}
			planID := strings.TrimSpace(*task.PlanID)
			lockedPlan, err := repo.Plans().FindByIDForUpdate(ctx, planID)
			if err != nil {
				// Some local callers construct an in-memory plan before persistence.
				// A persisted plan is always locked; only a missing row may use the
				// caller snapshot, and it must still be active.
				if errors.Is(err, gorm.ErrRecordNotFound) {
					if planID == allowMissingPlanID {
						continue
					}
					return fmt.Errorf("task plan %s does not exist", planID)
				}
				return fmt.Errorf("lock task plan for admission: %w", err)
			}
			if lockedPlan.Status != model.PlanStatusActive {
				return ErrTaskPlanPaused
			}
			if lockedPlan.ProjectID != "" && lockedPlan.ProjectID != project.ID {
				return fmt.Errorf("task admission plan project mismatch")
			}
		}
		return nil
	}
	createTask := func(repo repository.Repository, task *model.Task) error {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			return err
		}
		if !explicitlyDisableContentImage {
			return nil
		}
		// GORM applies the model's default:true tag to a false bool during Create.
		// Save the explicit user choice inside the same admission transaction while
		// retaining the true default for callers that omit the field.
		task.HasContentImage = false
		return repo.Tasks().Update(ctx, task)
	}
	if s.billingCatalogSvc == nil && s.billingWalletSvc == nil {
		return s.repo.WithTx(ctx, func(tx repository.Repository) error {
			if err := lockProjectAdmission(tx); err != nil {
				return err
			}
			for _, task := range tasks {
				if err := createTask(tx, task); err != nil {
					return err
				}
			}
			return nil
		})
	}
	if s.billingCatalogSvc == nil || s.billingWalletSvc == nil {
		return fmt.Errorf("fixed task billing is not fully configured")
	}
	type admission struct {
		task        *model.Task
		quote       *model.BillingQuote
		fingerprint string
	}
	admissions := make([]admission, 0, len(tasks))
	for _, task := range tasks {
		payload, err := json.Marshal(task)
		if err != nil {
			return fmt.Errorf("marshal task billing fingerprint: %w", err)
		}
		fingerprint := billingFingerprint("task-admission", string(payload))
		quote, err := s.billingCatalogSvc.CreateTaskQuote(ctx, TaskQuoteRequest{
			UserID: task.UserID, TaskType: task.Type, RequestFingerprint: fingerprint,
			ExecutionProfile: task.ExecutionProfile,
			IdempotencyScope: "task-admission-quote", IdempotencyKey: task.ID,
		})
		if err != nil {
			return err
		}
		admissions = append(admissions, admission{task: task, quote: quote, fingerprint: fingerprint})
	}
	return s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := lockProjectAdmission(tx); err != nil {
			return err
		}
		for _, item := range admissions {
			charge, err := s.billingWalletSvc.ChargeTaskAdmissionInTx(ctx, tx, TaskChargeRequest{
				UserID: item.task.UserID, TaskID: item.task.ID, QuoteID: item.quote.ID,
				CatalogID: item.quote.CatalogID, SKUID: item.quote.SKUID, RequestFingerprint: item.fingerprint,
				IdempotencyScope: "task-admission", IdempotencyKey: item.task.ID,
				ActorType: "user", ActorID: item.task.UserID, SourceService: "task-service",
			})
			if err != nil {
				return err
			}
			item.task.BillingQuoteID, item.task.BillingCatalogID, item.task.BillingSKUID = item.quote.ID, item.quote.CatalogID, item.quote.SKUID
			item.task.BillingPricingTier = charge.PricingTier
			item.task.BillingChargeID, item.task.BillingPriceCredits = stringPtr(charge.ID), charge.PriceCredits
			if err := createTask(tx, item.task); err != nil {
				return err
			}
		}
		return nil
	})
}

// CreateFromPlan creates a task linked to a plan and enqueues it for execution.
//
// The plan's scheduling-adjacent "what to produce" params (image model,
// reference image, watermark, and image composition) flow to the task.
// Project/account style config is frozen from the project into ProjectSnapshot.
func (s *TaskService) CreateFromPlan(ctx context.Context, plan *model.Plan) (*model.Task, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is required")
	}
	if strings.TrimSpace(plan.ExecutionProfile) == "" {
		return nil, fmt.Errorf("execution_profile is required")
	}
	profile, err := resolveAgentProfileForUser(ctx, s.repo, s.agentProfiles, plan.UserID, plan.ExecutionProfile)
	if err != nil {
		return nil, err
	}
	profileSnapshot, profileFingerprint, err := profile.Freeze()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAgentProfileSnapshotInvalid, err)
	}
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
		found, err := s.ResolveTaskCreationProject(ctx, plan.UserID, plan.ProjectID)
		if err != nil {
			return nil, err
		}
		project = found
		taskType = found.Platform
	}
	isMontageTask := model.IsMontagePlatform(taskType)
	effectiveImageRatio := strings.TrimSpace(plan.ImageRatio)
	if isMontageTask && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" && project != nil {
		effectiveImageRatio = strings.TrimSpace(project.ImageRatio)
	}
	if isMontageTask && effectiveImageRatio == model.ImageRatioAuto {
		effectiveImageRatio = ""
	}
	if effectiveImageRatio == "" {
		effectiveImageRatio = model.DefaultImageRatio(taskType)
	}
	if project != nil && len(model.SupportedImageRatios(project.Platform)) > 0 && !model.IsBusinessImageRatioAllowed(project.Platform, effectiveImageRatio) {
		return nil, fmt.Errorf("%s for platform %s: %s", model.ValidImageRatioHint, project.Platform, effectiveImageRatio)
	}
	effectiveReferenceImageAssetID := plan.ReferenceImageAssetID
	if taskType == model.PlatformArticle && plan.UsePortraitReference {
		if project == nil || project.PortraitReferenceImageAssetID == "" {
			return nil, fmt.Errorf("公众号项目未配置人物参考图，请先在项目设置中上传")
		}
		effectiveReferenceImageAssetID = project.PortraitReferenceImageAssetID
	}
	if err := s.validateTaskCreationReferences(ctx, plan.UserID, effectiveReferenceImageAssetID, project, nil); err != nil {
		return nil, err
	}
	effectiveImageCapabilityKey := strings.TrimSpace(plan.ImageCapabilityKey)
	var effectiveImageCapabilitySnapshot model.ImageCapabilitySnapshot
	if taskUsesFrozenImageCapability(taskType) {
		if s.imageCapabilities == nil {
			return nil, fmt.Errorf("resolve plan image capability: %w", ErrImageCapabilityResolverUnavailable)
		}
		resolved, err := s.imageCapabilities.FreezeImageCapability(ctx, plan.UserID, effectiveImageCapabilityKey)
		if err != nil {
			return nil, fmt.Errorf("resolve plan image capability: %w", err)
		}
		effectiveImageCapabilitySnapshot = resolved
		effectiveImageCapabilityKey = resolved.Key
	}
	var planMontageInput *model.MontageInput
	montageExecutionTarget := model.ExecutionTargetCloud
	if isMontageTask {
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

	task := &model.Task{
		ID:                       taskID,
		UserID:                   plan.UserID,
		ProjectID:                plan.ProjectID,
		PlanID:                   &plan.ID,
		Type:                     taskType,
		Status:                   model.TaskStatusPending,
		Prompt:                   prompt,
		ImageCapabilityKey:       effectiveImageCapabilityKey,
		ImageRatio:               effectiveImageRatio,
		ReferenceImageAssetID:    effectiveReferenceImageAssetID,
		SkipReferenceImage:       plan.SkipReferenceImage,
		Watermark:                plan.Watermark,
		HasContentImage:          plan.HasContentImage,
		HasTailImage:             plan.HasTailImage,
		ArticleWithCover:         plan.ArticleWithCover,
		ArticleWithContentImages: plan.ArticleWithContentImages,
		ExecutionProfile:         profile.ID,
		AgentProfileSnapshot:     profileSnapshot,
		AgentProfileFingerprint:  profileFingerprint,
	}
	if taskUsesFrozenImageCapability(taskType) {
		task.SetImageCapabilitySnapshot(effectiveImageCapabilitySnapshot)
	}
	agentInput, err := validateAndCloneAgentInput(taskType, plan.AgentInput.Data())
	if err != nil {
		return nil, err
	}
	if agentInput != nil {
		task.SetAgentInput(agentInput)
	}
	task.SetInputAttachments(cloneEntryAttachments(plan.InputAttachments.Data()))
	if model.IsMontagePlatform(taskType) {
		task.ExecutionTarget = montageExecutionTarget
	}
	if project != nil {
		task.SetProjectSnapshot(model.SnapshotProject(project))
	}
	if planMontageInput != nil {
		task.SetMontageInput(*planMontageInput)
	}

	if err := s.persistTasksWithFixedAdmission(ctx, []*model.Task{task}, plan.ID); err != nil {
		if errors.Is(err, ErrBillingDebtOutstanding) || errors.Is(err, ErrBillingInsufficientForTask) {
			s.logger.Warn().Err(err).Str("user_id", plan.UserID).Str("plan_id", plan.ID).Str("task_type", taskType).Msg("skipping plan task at fixed-SKU admission")
			return nil, nil
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
		persistCtx, cancel := context.WithTimeout(context.Background(), s.persistTimeout)
		failErr := s.failPendingAdmittedTask(persistCtx, task, model.TaskBillingTerminalPlatformError, "failed to enqueue: "+err.Error())
		cancel()
		if failErr != nil {
			return nil, fmt.Errorf("finalize failed plan task enqueue: %w", failErr)
		}
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

func (s *TaskService) ListTitlesForUser(ctx context.Context, userID, projectID string) ([]string, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("find project: %w", err)
	}
	if project.UserID != userID {
		return nil, fmt.Errorf("project not owned by user")
	}
	return s.ListTitles(ctx, projectID)
}

var artifactTaskTitles = map[string]struct{}{
	"图片内容规划":    {},
	"标题候选与评分":   {},
	"选题研究报告":    {},
	"违禁词合规检查报告": {},
}

// FinalizeTitle records the owning Agent's final title as the canonical task title.
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
// the running execution to stop. Accepted task charges remain posted on user cancellation.
// Uses CompareAndSwapStatus to prevent cancelling already-completed or already-failed tasks.
// If Redis pub/sub is available, it also publishes a cancel event so other replicas
// can propagate the cancellation to their in-process execution contexts.
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
	// Fetch task before CAS so ownership and cloud execution identity are stable.
	task, taskErr := s.repo.Tasks().FindByID(ctx, id)
	if userID != "" && taskErr == nil && task != nil && task.UserID != userID {
		return fmt.Errorf("task not found")
	}
	if taskErr == nil && task != nil && task.CurrentExecutionID != nil {
		execution, executionErr := s.repo.TaskExecutions().FindByID(ctx, *task.CurrentExecutionID)
		if executionErr != nil {
			return fmt.Errorf("find current execution before cancellation: %w", executionErr)
		}
		if execution.Target == model.ExecutionTargetLocalClaimed {
			return s.cancelLocalExecution(ctx, task, execution)
		}
		if s.runtimeDispatcher != nil {
			return s.cancelCloudExecution(ctx, task, userID)
		}
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
	if task != nil {
		if err := s.repo.Tasks().UpdateBillingTerminalReason(ctx, id, model.TaskBillingTerminalUserCancelled); err != nil {
			return fmt.Errorf("record cancellation billing reason: %w", err)
		}
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
	if err := s.EnrichFilesWithDeliveryMetadata(ctx, taskID, files); err != nil {
		return nil, fmt.Errorf("enrich task file delivery metadata: %w", err)
	}
	return files, nil
}

// GetVisibleFiles returns successful delivery files followed by retained files
// from failed attempts. Delivery and workflow code must continue using GetFiles.
func (s *TaskService) GetVisibleFiles(ctx context.Context, taskID string) ([]*model.TaskFile, error) {
	delivered, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get delivered task files: %w", err)
	}
	retained, err := s.repo.TaskFiles().FindRetainedByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get retained task files: %w", err)
	}
	files := append(delivered, retained...)
	s.EnrichFilesWithURLs(ctx, files)
	if err := s.EnrichFilesWithDeliveryMetadata(ctx, taskID, files); err != nil {
		return nil, fmt.Errorf("enrich task file delivery metadata: %w", err)
	}
	return files, nil
}

func (s *TaskService) GetVisibleFilesForUser(ctx context.Context, userID, taskID string) ([]*model.TaskFile, error) {
	if _, err := s.ValidateAgentTaskAccess(ctx, taskID, userID); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTaskCapabilityAccessDenied, err)
	}
	return s.GetVisibleFiles(ctx, taskID)
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
	authoritative, err := s.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("find task before enqueue: %w", err)
	}
	if authoritative.DeletingAt != nil {
		return ErrTaskDeleting
	}
	if model.IsTerminalTaskStatus(authoritative.Status) {
		if authoritative.CurrentExecutionID == nil {
			return nil
		}
		execution, findErr := s.repo.TaskExecutions().FindByID(ctx, strings.TrimSpace(*authoritative.CurrentExecutionID))
		if findErr != nil || (execution.Status != model.TaskExecutionStarting && execution.Status != model.TaskExecutionRunning && execution.FinalizationStatus == model.TaskExecutionFinalizationDone) {
			return nil
		}
	}
	task = authoritative
	// Load project if not provided, for concurrency check.
	if project == nil && task.ProjectID != "" {
		ch, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
		if err == nil {
			project = ch
		}
	}

	fallbackClaimToken := ""
	fallbackClaimOwned := false
	releaseFallbackClaim := func(releaseCtx context.Context) {
		if !fallbackClaimOwned {
			return
		}
		if _, err := s.pubsub.ReleaseFallbackClaim(releaseCtx, task.ID, fallbackClaimToken); err != nil {
			s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("failed to release fallback dispatch claim")
		}
		fallbackClaimOwned = false
	}
	defer func() { releaseFallbackClaim(context.Background()) }()
	if s.enqueuer == nil && s.pubsub != nil && s.pubsub.Available() {
		fallbackClaimToken = uuid.NewString()
		claimTTL := s.executionTimeout + s.persistTimeout + time.Minute
		if claimTTL <= 0 {
			claimTTL = time.Minute
		}
		claimed, err := s.pubsub.TryClaimFallback(ctx, task.ID, fallbackClaimToken, claimTTL)
		if err != nil {
			return fmt.Errorf("claim fallback task dispatch: %w", err)
		}
		if !claimed {
			s.logger.Info().Str("task_id", task.ID).Msg("fallback task dispatch already claimed")
			return nil
		}
		fallbackClaimOwned = true
	}

	// Check per-project concurrency limit.
	slotReserved := false
	if project != nil {
		maxConcurrent := s.effectiveProjectMaxConcurrent(project)

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

		uniqueEnqueuer, ok := s.enqueuer.(UniqueTaskEnqueuer)
		if !ok {
			if s.pubsub != nil && s.pubsub.Available() && project != nil {
				s.pubsub.ReleaseSlot(ctx, project.ID)
			}
			return errors.New("content task enqueuer does not support idempotent enqueue")
		}
		enqueued, err := uniqueEnqueuer.EnqueueUnique(TypeContentGenerate, payload, task.ID)
		if err != nil || !enqueued {
			if s.pubsub != nil && s.pubsub.Available() && project != nil {
				s.pubsub.ReleaseSlot(ctx, project.ID)
			}
		}
		if err != nil {
			return fmt.Errorf("enqueue task: %w", err)
		}
		if !enqueued {
			s.logger.Info().Str("task_id", task.ID).Msg("task already enqueued for async execution")
			return nil
		}

		s.logger.Info().Str("task_id", task.ID).Msg("task enqueued for async execution")
		return nil
	}

	// Fallback: run in goroutine if no enqueuer.
	s.logger.Warn().Str("task_id", task.ID).Msg("no enqueuer available, running task in goroutine")
	goroutineClaimToken := fallbackClaimToken
	goroutineOwnsClaim := fallbackClaimOwned
	fallbackClaimOwned = false
	go func(ownsFallbackClaim bool, claimToken string) {
		if ownsFallbackClaim {
			defer func() {
				if _, err := s.pubsub.ReleaseFallbackClaim(context.Background(), task.ID, claimToken); err != nil {
					s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("failed to release fallback dispatch claim")
				}
			}()
		}
		ownsSlot := slotReserved && s.pubsub != nil && s.pubsub.Available() && project != nil
		releaseOwnedSlot := func(releaseCtx context.Context) {
			if !ownsSlot {
				return
			}
			s.pubsub.ReleaseSlot(releaseCtx, project.ID)
			ownsSlot = false
		}
		releaseOwnedSlotIfNonTerminal := func(releaseCtx context.Context) {
			if !ownsSlot {
				return
			}
			current, err := s.repo.Tasks().FindByID(releaseCtx, task.ID)
			if err != nil {
				s.logger.Warn().Err(err).Str("task_id", task.ID).Msg("could not resolve fallback slot ownership; leaving count for periodic reconciliation")
				ownsSlot = false
				return
			}
			if model.IsTerminalTaskStatus(current.Status) {
				ownsSlot = false
				return
			}
			releaseOwnedSlot(releaseCtx)
		}
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error().
					Str("task_id", task.ID).
					Interface("panic", r).
					Msg("panic recovered in fallback task execution")
				releaseOwnedSlot(context.Background())
			}
		}()
		fallbackCtx, cancel := context.WithTimeout(context.Background(), s.executionTimeout)
		defer cancel()
		_, preparation, err := s.preparePendingExecution(fallbackCtx, task)
		if err != nil {
			releaseOwnedSlot(fallbackCtx)
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("fallback task preparation terminal persistence failed")
			return
		}
		if preparation == pendingExecutionTerminalized {
			ownsSlot = false
			return
		}
		if preparation != pendingExecutionReady {
			releaseOwnedSlotIfNonTerminal(fallbackCtx)
			return
		}
		if err := s.dispatchPendingTask(fallbackCtx, task); err != nil {
			releaseOwnedSlotIfNonTerminal(fallbackCtx)
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("fallback managed runtime dispatch failed")
		}
	}(goroutineOwnsClaim, goroutineClaimToken)
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

	var dispatchErr error
	for _, t := range pending {
		if err := s.EnqueueExecution(ctx, t, project); err != nil {
			if errors.Is(err, ErrTaskPlanPaused) {
				continue
			}
			s.logger.Error().Err(err).Str("task_id", t.ID).Msg("failed to dispatch pending task")
			dispatchErr = errors.Join(dispatchErr, fmt.Errorf("dispatch pending task %s: %w", t.ID, err))
		}
	}

	return dispatchErr
}

// UsageStats holds user-visible task activity. Provider usage and cost stay in
// the internal cost ledger and are never returned by this API.
type UsageStats struct {
	TotalTasks int                       `json:"total_tasks"`
	ByType     map[string]*TypeStatEntry `json:"by_type,omitempty"`
}

// TypeStatEntry holds per-type aggregated stats.
type TypeStatEntry struct {
	Count int `json:"count"`
}

// GetUsageStats returns user-visible task activity for a date range.
func (s *TaskService) GetUsageStats(ctx context.Context, userID string, from, to time.Time, projectID string) (*UsageStats, error) {
	stats := &UsageStats{ByType: make(map[string]*TypeStatEntry)}

	// SQL-level aggregation for totals.
	totalTasks, _, _, _, _, err :=
		s.repo.Tasks().AggregateUsageByUser(ctx, userID, from, to, projectID)
	if err != nil {
		return nil, fmt.Errorf("aggregate usage: %w", err)
	}
	stats.TotalTasks = int(totalTasks)

	// SQL GROUP BY for per-type breakdown.
	typeRows, err := s.repo.Tasks().AggregateUsageByType(ctx, userID, from, to, projectID)
	if err != nil {
		return nil, fmt.Errorf("aggregate usage by type: %w", err)
	}
	for _, row := range typeRows {
		stats.ByType[row.Type] = &TypeStatEntry{Count: int(row.Count)}
	}

	return stats, nil
}

// Delete permanently removes a task and its associated files.
// Running/pending tasks are cancelled first. User cancellation does not reverse
// the fixed task charge.
func (s *TaskService) Delete(ctx context.Context, id string) error {
	task, err := s.repo.Tasks().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	acquired, err := s.repo.Tasks().BeginDelete(ctx, id)
	if err != nil {
		return fmt.Errorf("begin task delete: %w", err)
	}
	task, err = s.repo.Tasks().FindByID(ctx, id)
	if err != nil {
		return fmt.Errorf("reload task delete authority: %w", err)
	}
	if !acquired && task.DeletingAt == nil {
		return fmt.Errorf("begin task delete: deletion authority was not acquired")
	}

	managedExecution := task.CurrentExecutionID != nil
	requiresCancel := task.Status == model.TaskStatusRunning || task.Status == model.TaskStatusPending
	var execution *model.TaskExecution
	if managedExecution {
		execution, err = s.repo.TaskExecutions().FindByID(ctx, *task.CurrentExecutionID)
		if err != nil {
			return fmt.Errorf("find current execution before task delete: %w", err)
		}
		if task.Status == model.TaskStatusCancelled && execution.CleanupStatus != model.TaskExecutionCleanupDone {
			requiresCancel = true
		}
	}
	if requiresCancel {
		if err := s.Cancel(ctx, id); err != nil {
			return fmt.Errorf("cancel task before delete: %w", err)
		}
	}
	if managedExecution {
		execution, err = s.repo.TaskExecutions().FindByID(ctx, *task.CurrentExecutionID)
		if err != nil {
			return fmt.Errorf("find current execution before task authority removal: %w", err)
		}
		if execution.FinalizationStatus != model.TaskExecutionFinalizationDone {
			return fmt.Errorf("cancel task before delete: runtime finalization is not complete")
		}
		if execution.CleanupStatus != model.TaskExecutionCleanupDone {
			return fmt.Errorf("cancel task before delete: runtime cleanup is not complete")
		}
	}
	if s.taskWorkspace != nil {
		if err := s.taskWorkspace.DeleteTaskWorkspace(ctx, task); err != nil {
			return fmt.Errorf("delete task workspace: %w", err)
		}
	}
	if err := s.repo.UploadSessions().ScheduleTaskArtifactPrefixExpiration(ctx, task.UserID, buildTaskArtifactTaskStoragePrefix(task), time.Now()); err != nil {
		return fmt.Errorf("schedule task artifact cleanup: %w", err)
	}

	storageKeys := make(map[string]struct{})
	files, err := s.repo.TaskFiles().FindAllByTaskID(ctx, id)
	if err != nil {
		return fmt.Errorf("list task files for deletion: %w", err)
	}
	for _, f := range files {
		if f.OSSKey == "" && f.CleanupOSSKey == "" {
			continue
		}
		if s.store == nil || f.StorageProvider == "" || f.StorageProvider != s.store.Name() {
			return fmt.Errorf("storage provider %q is unavailable for task file %s", f.StorageProvider, f.ID)
		}
		for _, key := range []string{f.OSSKey, f.CleanupOSSKey} {
			if key != "" {
				storageKeys[key] = struct{}{}
			}
		}
	}
	settlements, err := s.repo.Billing().ListSettlementsByTask(ctx, id)
	if err != nil {
		return fmt.Errorf("list task operation objects for deletion: %w", err)
	}
	for _, settlement := range settlements {
		if settlement.AttemptID == nil || len(settlement.ResultSnapshot) == 0 {
			continue
		}
		var snapshot struct {
			OperationObject json.RawMessage `json:"operation_object"`
		}
		if err := json.Unmarshal(settlement.ResultSnapshot, &snapshot); err != nil {
			return fmt.Errorf("decode task operation object settlement %s: %w", settlement.ID, err)
		}
		if len(snapshot.OperationObject) == 0 || string(snapshot.OperationObject) == "null" {
			continue
		}
		var object taskFileOperationObjectSnapshot
		if err := json.Unmarshal(snapshot.OperationObject, &object); err != nil {
			return fmt.Errorf("decode task operation object %s: %w", settlement.ID, err)
		}
		expectedPrefix := buildTaskMCPArtifactStoragePrefix(task, *settlement.AttemptID) + "operation-objects/"
		if object.OSSKey == "" || object.StorageProvider == "" || s.store == nil || object.StorageProvider != s.store.Name() ||
			!strings.HasPrefix(filepath.ToSlash(object.OSSKey), expectedPrefix) || !isImmutableOperationObjectKey(object.OSSKey) {
			return fmt.Errorf("invalid task operation object in settlement %s", settlement.ID)
		}
		storageKeys[object.OSSKey] = struct{}{}
	}
	if len(storageKeys) > 0 && s.store == nil {
		return fmt.Errorf("storage provider is required to delete task files")
	}
	if s.store != nil {
		keys := make([]string, 0, len(storageKeys))
		for key := range storageKeys {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		var cleanupErrors []error
		for _, key := range keys {
			if delErr := s.store.Delete(ctx, key); delErr != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("delete storage file %s: %w", key, delErr))
			}
		}
		if err := errors.Join(cleanupErrors...); err != nil {
			return err
		}
	}

	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		if err := tx.TaskFiles().DeleteByTaskID(ctx, id); err != nil {
			return fmt.Errorf("delete task files: %w", err)
		}
		if err := tx.WechatPublications().DeleteLifecycleByTaskID(ctx, id); err != nil {
			return fmt.Errorf("delete WeChat publication lifecycle: %w", err)
		}
		deleted, err := tx.Tasks().DeleteIfDeleting(ctx, id)
		if err != nil {
			return fmt.Errorf("delete task: %w", err)
		}
		if !deleted {
			return fmt.Errorf("delete task: deletion authority was lost")
		}
		return nil
	}); err != nil {
		return err
	}
	s.deregisterCancel(id)
	return nil
}
