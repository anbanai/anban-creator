package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// TypeContentGenerate is the Asynq task type for content generation.
const TypeContentGenerate = "content:generate"

// TaskService handles task CRUD, manual creation, and execution orchestration.
type TaskService struct {
	repo      repository.Repository
	executor  agent.TaskExecutor
	logger    *zerolog.Logger
	enqueuer  TaskEnqueuer
	store     storage.Provider
	creditSvc *CreditService
}

// NewTaskService creates a new TaskService.
func NewTaskService(
	repo repository.Repository,
	executor agent.TaskExecutor,
	enqueuer TaskEnqueuer,
	store storage.Provider,
	creditSvc *CreditService,
	logger *zerolog.Logger,
) *TaskService {
	return &TaskService{
		repo:      repo,
		executor:  executor,
		logger:    logger,
		enqueuer:  enqueuer,
		store:     store,
		creditSvc: creditSvc,
	}
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
func (s *TaskService) CreateManual(ctx context.Context, userID, channelID, topic string, quantity int) ([]*model.Task, error) {
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
			ID:        taskID,
			UserID:    userID,
			ChannelID: channelID,
			Type:      taskType,
			Status:    model.TaskStatusPending,
			Topic:     topic,
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

// refundTasks refunds credits for a list of task IDs.
func (s *TaskService) refundTasks(ctx context.Context, taskIDs []string) {
	if s.creditSvc == nil {
		return
	}
	for _, tid := range taskIDs {
		if err := s.creditSvc.RefundForTask(ctx, tid); err != nil {
			s.logger.Error().Err(err).Str("task_id", tid).Msg("failed to refund credits during rollback")
		}
	}
}

// CreateFromPlan creates a task linked to a plan and enqueues it for execution.
func (s *TaskService) CreateFromPlan(ctx context.Context, plan *model.Plan) (*model.Task, error) {
	taskID := generateTaskID()

	topic := plan.TopicHint
	if topic == "" {
		topic = plan.Title
	}

	// Derive task type from the channel if ChannelID is set.
	taskType := plan.Type
	if plan.ChannelID != "" {
		ch, err := s.repo.Channels().FindByID(ctx, plan.ChannelID)
		if err == nil {
			taskType = ch.Platform
		}
	}

	task := &model.Task{
		ID:        taskID,
		UserID:    plan.UserID,
		ChannelID: plan.ChannelID,
		Type:      taskType,
		Status:    model.TaskStatusPending,
		Topic:     topic,
	}

	if err := s.repo.Tasks().Create(ctx, task); err != nil {
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

// Cancel sets a task's status to "cancelled".
func (s *TaskService) Cancel(ctx context.Context, id string) error {
	if err := s.repo.Tasks().UpdateStatus(ctx, id, model.TaskStatusCancelled); err != nil {
		return fmt.Errorf("cancel task: %w", err)
	}
	return nil
}

// GetFiles returns files associated with a task.
func (s *TaskService) GetFiles(ctx context.Context, taskID string) ([]*model.TaskFile, error) {
	files, err := s.repo.TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task files: %w", err)
	}
	return files, nil
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

	if s.enqueuer != nil {
		payload, err := json.Marshal(map[string]string{
			"task_id": task.ID,
			"user_id": task.UserID,
		})
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		if err := s.enqueuer.Enqueue(TypeContentGenerate, payload); err != nil {
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
			}
		}()
		if err := s.HandleExecution(context.Background(), task, channel); err != nil {
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

	running, err := s.repo.Tasks().CountRunningByChannel(ctx, channelID)
	if err != nil {
		return fmt.Errorf("count running: %w", err)
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

// UsageStats holds aggregated LLM usage statistics.
type UsageStats struct {
	TotalTasks           int                       `json:"total_tasks"`
	TotalInputTokens     int64                     `json:"total_input_tokens"`
	TotalOutputTokens    int64                     `json:"total_output_tokens"`
	TotalCacheReadTokens int64                     `json:"total_cache_read_tokens"`
	TotalCostUSD         float64                   `json:"total_cost_usd"`
	ByType               map[string]*TypeStatEntry `json:"by_type,omitempty"`
}

// TypeStatEntry holds per-type aggregated stats.
type TypeStatEntry struct {
	Count           int     `json:"count"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
}

// GetUsageStats returns aggregated token usage and cost stats for a user.
func (s *TaskService) GetUsageStats(ctx context.Context, userID string, from, to time.Time, channelID string) (*UsageStats, error) {
	tasks, err := s.repo.Tasks().FindByUserIDAndCreatedAtRange(ctx, userID, from, to, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}

	stats := &UsageStats{ByType: make(map[string]*TypeStatEntry)}
	for _, t := range tasks {
		if channelID != "" && t.ChannelID != channelID {
			continue
		}
		if t.Result == nil {
			continue
		}

		var result agent.ExecutionResult
		if err := json.Unmarshal([]byte(*t.Result), &result); err != nil {
			continue
		}

		stats.TotalTasks++

		// Per-type aggregation.
		te, ok := stats.ByType[t.Type]
		if !ok {
			te = &TypeStatEntry{}
			stats.ByType[t.Type] = te
		}
		te.Count++

		if result.TotalCostUSD != nil {
			stats.TotalCostUSD += *result.TotalCostUSD
			te.CostUSD += *result.TotalCostUSD
		}
		if result.TokenUsage != nil {
			stats.TotalInputTokens += int64(result.TokenUsage.InputTokens)
			stats.TotalOutputTokens += int64(result.TokenUsage.OutputTokens)
			stats.TotalCacheReadTokens += int64(result.TokenUsage.CacheReadTokens)
			te.InputTokens += int64(result.TokenUsage.InputTokens)
			te.OutputTokens += int64(result.TokenUsage.OutputTokens)
			te.CacheReadTokens += int64(result.TokenUsage.CacheReadTokens)
		}
	}

	return stats, nil
}
