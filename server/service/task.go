package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	repo               repository.Repository
	executor           agent.TaskExecutor
	logger             *zerolog.Logger
	enqueuer           TaskEnqueuer
	store              storage.Provider
	creditSvc          *CreditService
	publishingSvc      *PublishingService
	taskLogDir         string
	workspaceSvc       *WorkspaceService
	workspaceDir       string
	pubsub             *RedisPubSub
	pubsubCancel       context.CancelFunc // stops the listenCancelEvents goroutine
	cancelFuncs        sync.Map           // taskID → context.CancelFunc
	rednoteTrackingSvc PublishedTrackingService
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
		repo:          repo,
		executor:      executor,
		logger:        logger,
		enqueuer:      enqueuer,
		store:         store,
		creditSvc:     creditSvc,
		publishingSvc: publishingSvc,
		taskLogDir:    taskLogDir,
		workspaceSvc:  workspaceSvc,
		workspaceDir:  workspaceDir,
		pubsub:        pubsub,
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

func (s *TaskService) SetRednoteTrackingService(trackingSvc PublishedTrackingService) {
	s.rednoteTrackingSvc = trackingSvc
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

// CreateManual creates tasks without a plan and enqueues them for execution.
// The quantity parameter (1-5) determines how many tasks to create, each independently billed.
func (s *TaskService) CreateManual(ctx context.Context, userID, channelID, prompt string, quantity int, imageRatio string, generateVideo bool) ([]*model.Task, error) {
	if channelID == "" {
		return nil, fmt.Errorf("channel_id is required")
	}

	// Clamp quantity to 1-5.
	if quantity <= 0 {
		quantity = 1
	}
	if quantity > 5 {
		quantity = 5
	}

	// Load and validate channel.
	channel, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("find channel: %w", err)
	}
	if channel.UserID != userID {
		return nil, fmt.Errorf("channel not owned by user")
	}
	if channel.Status != model.ChannelStatusActive {
		return nil, fmt.Errorf("channel is not active")
	}

	taskType := channel.Platform

	// Pre-calculate total credit cost and deduct upfront to avoid race conditions.
	var deductedTaskIDs []string
	tasks := make([]*model.Task, 0, quantity)

	if s.creditSvc != nil {
		cost, ok := s.creditSvc.TaskCost(taskType)
		if !ok {
			return nil, fmt.Errorf("unknown task type: %s", taskType)
		}
		totalCost := cost * quantity

		// Generate all task IDs upfront so we can create individual transactions.
		taskIDs := make([]string, quantity)
		for i := range taskIDs {
			taskIDs[i] = generateTaskID()
		}

		// Deduct total cost in a single atomic transaction.
		if err := s.creditSvc.DeductBatch(ctx, userID, taskType, totalCost, taskIDs); err != nil {
			if errors.Is(err, ErrInsufficientCredits) {
				return nil, fmt.Errorf("积分不足: %w", err)
			}
			return nil, fmt.Errorf("deduct credits: %w", err)
		}
		deductedTaskIDs = taskIDs
	}

	for i := 0; i < quantity; i++ {
		var taskID string
		if s.creditSvc != nil && len(deductedTaskIDs) > i {
			taskID = deductedTaskIDs[i]
		} else {
			taskID = generateTaskID()
		}

		task := &model.Task{
			ID:            taskID,
			UserID:        userID,
			ChannelID:     channelID,
			Type:          taskType,
			Status:        model.TaskStatusPending,
			Prompt:        prompt,
			ImageRatio:    imageRatio,
			GenerateVideo: generateVideo,
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
			return nil, fmt.Errorf("create task: %w", err)
		}

		// Enqueue for async execution.
		if err := s.EnqueueExecution(ctx, task, nil); err != nil {
			s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to enqueue task, marking as failed")
			_ = s.repo.Tasks().UpdateStatusAndError(ctx, taskID, model.TaskStatusFailed, "failed to enqueue: "+err.Error())
		}

		tasks = append(tasks, task)
	}

	return tasks, nil
}

// CreateFromPlan creates a task linked to a plan and enqueues it for execution.
func (s *TaskService) CreateFromPlan(ctx context.Context, plan *model.Plan) (*model.Task, error) {
	taskID := generateTaskID()

	prompt := plan.Prompt
	if prompt == "" {
		prompt = plan.Title
	}

	// Derive task type from the channel if ChannelID is set.
	taskType := plan.Type
	if plan.ChannelID != "" {
		ch, err := s.repo.Channels().FindByID(ctx, plan.ChannelID)
		if err == nil {
			taskType = ch.Platform
		}
	}

	// Deduct credits for the plan task.
	if s.creditSvc != nil {
		if _, err := s.creditSvc.DeductForTask(ctx, plan.UserID, taskType, taskID); err != nil {
			if errors.Is(err, ErrInsufficientCredits) {
				s.logger.Warn().Str("user_id", plan.UserID).Str("plan_id", plan.ID).Str("task_type", taskType).Msg("skipping plan task due to insufficient credits")
				return nil, nil
			}
			s.logger.Error().Err(err).Str("user_id", plan.UserID).Str("plan_id", plan.ID).Msg("failed to deduct credits for plan task")
			return nil, nil
		}
	}

	task := &model.Task{
		ID:        taskID,
		UserID:    plan.UserID,
		ChannelID: plan.ChannelID,
		Type:      taskType,
		Status:    model.TaskStatusPending,
		Prompt:    prompt,
	}

	if err := s.repo.Tasks().Create(ctx, task); err != nil {
		// Refund the deducted credits if task creation fails.
		if s.creditSvc != nil {
			if refundErr := s.creditSvc.RefundForTask(ctx, taskID); refundErr != nil {
				s.logger.Error().Err(refundErr).Str("task_id", taskID).Msg("failed to refund credits during plan task rollback")
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

// List returns tasks for a user with optional status and channel filters and pagination.
func (s *TaskService) List(ctx context.Context, userID string, offset, limit int, status, channelID string) ([]*model.Task, int64, error) {
	var tasks []*model.Task
	var err error

	if status != "" {
		tasks, err = s.repo.Tasks().FindByUserIDAndStatus(ctx, userID, status, channelID, offset, limit)
	} else {
		tasks, err = s.repo.Tasks().FindByUserID(ctx, userID, channelID, offset, limit)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}

	var total int64
	if status != "" {
		total, err = s.repo.Tasks().CountByUserIDAndStatus(ctx, userID, status, channelID)
	} else {
		total, err = s.repo.Tasks().CountByUserID(ctx, userID, channelID)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}

	return tasks, total, nil
}

// ListTopics returns all existing topic texts for a channel, ordered by creation time descending.
func (s *TaskService) ListTopics(ctx context.Context, channelID string) ([]string, error) {
	return s.repo.Tasks().FindTopicsByChannelID(ctx, channelID)
}

// Cancel atomically transitions a task from pending/running to cancelled and signals
// the running execution to stop. Credits are refunded if the transition succeeds.
// Uses CompareAndSwapStatus to prevent cancelling already-completed or already-failed tasks.
// If Redis pub/sub is available, it also publishes a cancel event so other replicas
// can propagate the cancellation to their in-process execution contexts.
func (s *TaskService) Cancel(ctx context.Context, id string) error {
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
	if s.creditSvc != nil {
		if refundErr := s.creditSvc.RefundForTask(ctx, id, "cancel"); refundErr != nil {
			s.logger.Error().Err(refundErr).Str("task_id", id).Msg("failed to refund credits for cancelled task")
		}
	}
	// Release concurrency slot.
	if s.pubsub != nil {
		if task, err := s.repo.Tasks().FindByID(ctx, id); err == nil && task.ChannelID != "" {
			s.pubsub.ReleaseSlot(ctx, task.ChannelID)
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
// If the channel's concurrent task limit is reached, the task stays in DB as "pending"
// and will be dispatched later when a slot opens up.
func (s *TaskService) EnqueueExecution(ctx context.Context, task *model.Task, channel *model.Channel) error {
	// Load channel if not provided, for concurrency check.
	if channel == nil && task.ChannelID != "" {
		ch, err := s.repo.Channels().FindByID(ctx, task.ChannelID)
		if err == nil {
			channel = ch
		}
	}

	// Check per-channel concurrency limit.
	if channel != nil {
		maxConcurrent := channel.MaxConcurrentTasks
		if maxConcurrent <= 0 {
			maxConcurrent = DefaultMaxConcurrentTasks
		}

		slotReserved := false
		if s.pubsub != nil && s.pubsub.Available() {
			// Atomic check-and-reserve via Redis to prevent TOCTOU races.
			count, ok, err := s.pubsub.TryReserveSlot(ctx, channel.ID, maxConcurrent)
			if err != nil {
				s.logger.Warn().Err(err).
					Str("channel_id", channel.ID).
					Msg("failed to reserve concurrency slot via Redis, falling back to DB check")
			} else if !ok {
				s.logger.Info().
					Str("task_id", task.ID).
					Str("channel_id", channel.ID).
					Int("max", maxConcurrent).
					Msg("channel concurrency limit reached (Redis), task will be dispatched later")
				return nil
			} else {
				s.logger.Debug().
					Str("task_id", task.ID).
					Str("channel_id", channel.ID).
					Int64("running", count).
					Int("max", maxConcurrent).
					Msg("reserved concurrency slot via Redis")
				slotReserved = true
			}
		}

		// DB fallback: non-atomic check (when Redis unavailable or errored).
		if !slotReserved {
			running, err := s.repo.Tasks().CountRunningByChannel(ctx, channel.ID)
			if err != nil {
				s.logger.Warn().Err(err).
					Str("channel_id", channel.ID).
					Msg("failed to count running tasks, proceeding without limit check")
			} else if int(running) >= maxConcurrent {
				s.logger.Info().
					Str("task_id", task.ID).
					Str("channel_id", channel.ID).
					Int64("running", running).
					Int("max", maxConcurrent).
					Msg("channel concurrency limit reached, task will be dispatched later")
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
			if s.pubsub != nil && s.pubsub.Available() && channel != nil {
				s.pubsub.ReleaseSlot(ctx, channel.ID)
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
				if s.pubsub != nil && s.pubsub.Available() && channel != nil {
					s.pubsub.ReleaseSlot(context.Background(), channel.ID)
				}
			}
		}()
		fallbackCtx, cancel := context.WithTimeout(context.Background(), 35*time.Minute)
		defer cancel()
		// Set running status before execution to prevent plan checker from re-dispatching.
		swapped, _ := s.repo.Tasks().CompareAndSwapStatusAndStartedAt(fallbackCtx, task.ID, model.TaskStatusPending, model.TaskStatusRunning)
		if !swapped {
			// Task was cancelled or already running; release the reserved slot.
			if s.pubsub != nil && s.pubsub.Available() && channel != nil {
				s.pubsub.ReleaseSlot(fallbackCtx, channel.ID)
			}
			return
		}
		if err := s.HandleExecution(fallbackCtx, task, channel); err != nil {
			s.logger.Error().Err(err).Str("task_id", task.ID).Msg("fallback task execution failed")
		}
	}()
	return nil
}

// DefaultMaxConcurrentTasks is the default per-channel concurrent task limit.
const DefaultMaxConcurrentTasks = 10

// DispatchPendingTasks checks for pending tasks on a channel and enqueues them
// if there are available concurrency slots. Called after a task completes or fails.
func (s *TaskService) DispatchPendingTasks(ctx context.Context, channelID string) error {
	channel, err := s.repo.Channels().FindByID(ctx, channelID)
	if err != nil {
		return fmt.Errorf("find channel: %w", err)
	}

	maxConcurrent := channel.MaxConcurrentTasks
	if maxConcurrent <= 0 {
		maxConcurrent = DefaultMaxConcurrentTasks
	}

	var running int64
	if s.pubsub != nil && s.pubsub.Available() {
		key := channelRunningCountPrefix + channelID
		val, err := s.pubsub.rdb.Get(ctx, key).Int64()
		if err != nil {
			// Key may not exist; fall back to DB.
			running, _ = s.repo.Tasks().CountRunningByChannel(ctx, channelID)
		} else {
			running = val
		}
	} else {
		running, err = s.repo.Tasks().CountRunningByChannel(ctx, channelID)
		if err != nil {
			return fmt.Errorf("count running: %w", err)
		}
	}

	available := maxConcurrent - int(running)
	if available <= 0 {
		return nil
	}

	pending, err := s.repo.Tasks().FindPendingByChannel(ctx, channelID, available)
	if err != nil {
		return fmt.Errorf("find pending: %w", err)
	}

	for _, t := range pending {
		if err := s.EnqueueExecution(ctx, t, channel); err != nil {
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
func (s *TaskService) GetUsageStats(ctx context.Context, userID string, from, to time.Time, channelID string) (*UsageStats, error) {
	stats := &UsageStats{ByType: make(map[string]*TypeStatEntry)}

	// SQL-level aggregation for totals.
	totalTasks, totalInput, totalOutput, totalCacheRead, totalCacheCreation, totalCost, err :=
		s.repo.Tasks().AggregateUsageByUser(ctx, userID, from, to, channelID)
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
	typeRows, err := s.repo.Tasks().AggregateUsageByType(ctx, userID, from, to, channelID)
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
	if !published || task.Type != model.PlatformRednote || s.rednoteTrackingSvc == nil {
		return nil
	}
	if err := s.rednoteTrackingSvc.EnsureTrackingForPublishedTask(ctx, userID, task.ID); err != nil {
		return fmt.Errorf("ensure rednote tracking: %w", err)
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
