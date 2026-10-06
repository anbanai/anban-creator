package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrAttributionTaskNotFound         = errors.New("attribution task not found")
	ErrAttributionContentNotOwned      = errors.New("content does not belong to task")
	ErrAttributionWindowNotMature      = errors.New("content observation window is not mature")
	ErrAttributionNoObservation        = errors.New("content has no valid observations")
	ErrAttributionIdentityConflict     = errors.New("content identity is conflicted")
	ErrAttributionAlreadySucceeded     = errors.New("attribution already succeeded for analytics revision")
	ErrAttributionAccountBusy          = errors.New("account has a running feedback job")
	ErrAttributionSchedulerUnavailable = errors.New("feedback scheduler unavailable")
)

type FeedbackAttributionService struct {
	repo     repository.Repository
	enqueuer FeedbackTaskEnqueuer
	profiles *AgentProfileRegistry
}

func NewFeedbackAttributionService(repo repository.Repository, enqueuer FeedbackTaskEnqueuer) *FeedbackAttributionService {
	return &FeedbackAttributionService{repo: repo, enqueuer: enqueuer}
}

func (s *FeedbackAttributionService) SetAgentProfileRegistry(registry *AgentProfileRegistry) {
	s.profiles = registry
}

// EnsureManagedTask creates the internal, non-user-delivery task that owns a
// feedback execution. It is idempotent through FeedbackJob.TaskID.
func (s *FeedbackAttributionService) EnsureManagedTask(ctx context.Context, job *model.FeedbackJob) (*model.Task, error) {
	if s == nil || s.repo == nil || job == nil {
		return nil, ErrAttributionSchedulerUnavailable
	}
	expectedTaskID := strings.TrimSpace(job.TaskID)
	leaseScope := FeedbackAccountLeaseScope(job.AccountID)
	leaseHeld := false
	leaseCommitted := false
	if leaseScope != "" {
		var err error
		leaseHeld, err = s.repo.FeedbackLoop().AcquireFeedbackLease(ctx, leaseScope, job.ID, time.Now().UTC(), feedbackLeaseTTL)
		if err != nil {
			return nil, fmt.Errorf("acquire feedback account lease: %w", err)
		}
		if !leaseHeld {
			return nil, ErrAttributionAccountBusy
		}
		defer func() {
			if leaseHeld && !leaseCommitted {
				_ = s.repo.FeedbackLoop().ReleaseFeedbackLease(context.Background(), leaseScope, job.ID)
			}
		}()
	}
	// Reusing a pending/running Task is still an execution attempt and retains
	// the same account-level mutual exclusion as a newly created Task.
	if expectedTaskID != "" {
		current, findErr := s.repo.Tasks().FindByID(ctx, expectedTaskID)
		if findErr == nil && !model.IsTerminalTaskStatus(current.Status) {
			leaseCommitted = true
			return current, nil
		}
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return nil, findErr
		}
	}
	if s.profiles == nil {
		return nil, errors.New("feedback agent profile registry unavailable")
	}
	user, err := s.repo.Users().FindByID(ctx, job.UserID)
	if err != nil {
		return nil, err
	}
	profile, err := s.profiles.ResolveForTier("effective", model.ResolveTier(user.Tier))
	if err != nil {
		return nil, err
	}
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		return nil, err
	}
	input := map[string]any{"feedback_job_id": job.ID, "operation": job.Operation, "target_content_id": job.TargetContentID, "analytics_revision": job.AnalyticsRevision, "period_start": job.PeriodStart, "period_end": job.PeriodEnd, "fingerprint": job.Fingerprint}
	task := &model.Task{ID: uuid.NewString(), UserID: job.UserID, ProjectID: job.ProjectID, AgentID: model.AgentIDFeedbackAnalysis, Channel: model.ChannelFeedbackAnalysis, TaskKind: model.TaskKindFeedbackAnalysis, Type: model.TaskKindFeedbackAnalysis, Status: model.TaskStatusPending, Prompt: "Run the frozen feedback analysis job.", ExecutionProfile: profile.ID, AgentProfileSnapshot: snapshot, AgentProfileFingerprint: fingerprint}
	task.SetAgentInput(input)
	var claimed bool
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		var err error
		claimed, err = tx.FeedbackLoop().ClaimTaskID(ctx, job.ID, expectedTaskID, task.ID)
		if err != nil || !claimed {
			return err
		}
		return tx.Tasks().Create(ctx, task)
	}); err != nil {
		return nil, err
	}
	if !claimed {
		latest, err := s.repo.FeedbackLoop().FindJobByIDOrFingerprint(ctx, job.ID)
		if err != nil {
			return nil, err
		}
		if latest.TaskID == "" {
			return nil, errors.New("feedback task claim was lost")
		}
		return s.repo.Tasks().FindByID(ctx, latest.TaskID)
	}
	// Keep the durable account lease until the execution finalizer reaches a
	// terminal state. This is the mutual-exclusion boundary for the actual
	// managed Agent run, not just Task creation.
	leaseCommitted = true
	job.TaskID = task.ID
	return task, nil
}

type AttributionRequest struct {
	UserID            string
	TaskID            string
	ContentID         string
	ObservationWindow string
}

type AttributionResult struct {
	Job *model.FeedbackJob `json:"job"`
}

// FeedbackAccountLeaseScope returns the durable mutual-exclusion scope for a
// platform account. It is shared by scheduler dispatch and execution cleanup.
func FeedbackAccountLeaseScope(accountID string) string {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return ""
	}
	return "feedback:account:" + accountID
}

// CreateAttribution performs all deterministic admission checks before a job
// is persisted. It never invokes an Agent or an LLM.
func (s *FeedbackAttributionService) CreateAttribution(ctx context.Context, req AttributionRequest) (*AttributionResult, error) {
	if s == nil || s.repo == nil {
		return nil, ErrAttributionSchedulerUnavailable
	}
	taskID := strings.TrimSpace(req.TaskID)
	contentID := strings.TrimSpace(req.ContentID)
	if taskID == "" || contentID == "" {
		return nil, errors.New("task_id and content_id are required")
	}
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttributionTaskNotFound
		}
		return nil, fmt.Errorf("find attribution task: %w", err)
	}
	if req.UserID != "" && task.UserID != req.UserID {
		return nil, ErrAttributionTaskNotFound
	}
	if strings.TrimSpace(task.ProjectID) == "" {
		return nil, ErrAttributionContentNotOwned
	}
	var content model.AnalyticsContent
	db := s.repo.Analytics().DB().WithContext(ctx)
	if err := db.Where("id = ? AND project_id = ?", contentID, task.ProjectID).First(&content).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrAttributionContentNotOwned
		}
		return nil, fmt.Errorf("find attribution content: %w", err)
	}
	if strings.TrimSpace(content.TaskID) != "" && content.TaskID != task.ID {
		return nil, ErrAttributionContentNotOwned
	}
	if strings.TrimSpace(content.TaskID) == "" {
		return nil, ErrAttributionContentNotOwned
	}
	window := strings.TrimSpace(req.ObservationWindow)
	if window == "" {
		window = "7d"
	}
	days, err := observationWindowDays(window)
	if err != nil {
		return nil, err
	}
	publishedAt := content.Date
	if publishedAt == nil {
		return nil, ErrAttributionWindowNotMature
	}
	cutoff := publishedAt.Add(time.Duration(days) * 24 * time.Hour)
	if time.Now().UTC().Before(cutoff) {
		return nil, ErrAttributionWindowNotMature
	}
	var observations []model.AnalyticsObservation
	fromDate := cutoff.Format("2006-01-02")
	toDate := time.Now().UTC().Format("2006-01-02")
	if err := db.Where("project_id = ? AND content_id = ? AND revoked_at IS NULL AND stat_date >= ? AND stat_date <= ?", task.ProjectID, contentID, fromDate, toDate).Order("stat_date asc").Find(&observations).Error; err != nil {
		return nil, fmt.Errorf("load attribution observations: %w", err)
	}
	valid := 0
	for _, observation := range observations {
		if observationHasMetric(observation) {
			valid++
		}
	}
	if valid == 0 {
		return nil, ErrAttributionNoObservation
	}
	matureObserved := false
	for _, observation := range observations {
		if observationHasMetric(observation) && observation.StatDate >= cutoff.Format("2006-01-02") {
			matureObserved = true
			break
		}
	}
	if !matureObserved {
		return nil, ErrAttributionWindowNotMature
	}
	state, err := s.repo.Analytics().State(ctx, task.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("load analytics revision: %w", err)
	}
	if existing, err := s.repo.FeedbackLoop().FindSuccessfulAttribution(ctx, task.ProjectID, contentID, state.Revision); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, ErrAttributionAlreadySucceeded
	}
	accountID := "feedback:project:" + task.ProjectID
	if project, projectErr := s.repo.Projects().FindByID(ctx, task.ProjectID); projectErr == nil && project != nil {
		accountID = feedbackAccountID(project)
	}
	if running, err := s.repo.FeedbackLoop().ListRunningByAccount(ctx, accountID, "", 1); err != nil {
		return nil, err
	} else if len(running) > 0 {
		return nil, ErrAttributionAccountBusy
	}
	periodStart := publishedAt.Format("2006-01-02")
	periodEnd := time.Now().UTC().Format("2006-01-02")
	digestInput, _ := json.Marshal(map[string]any{"content_id": contentID, "revision": state.Revision, "window": window})
	digestBytes := sha256.Sum256(digestInput)
	digest := hex.EncodeToString(digestBytes[:])
	platform := strings.TrimSpace(content.Channel)
	if platform == "" {
		platform = strings.TrimSpace(content.Platform)
	}
	if platform == "" {
		platform = strings.TrimSpace(task.Channel)
	}
	if platform == "" {
		return nil, ErrAttributionIdentityConflict
	}
	activeStrategy, strategyErr := s.repo.FeedbackLoop().FindActiveStrategy(ctx, task.ProjectID, platform)
	if strategyErr != nil {
		return nil, strategyErr
	}
	strategyRevision := int64(0)
	if activeStrategy != nil {
		strategyRevision = activeStrategy.Revision
	}
	job := &model.FeedbackJob{ID: uuid.NewString(), UserID: task.UserID, ProjectID: task.ProjectID, Platform: platform, AccountID: accountID, Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly, PeriodStart: periodStart, PeriodEnd: periodEnd, MaturityCutoff: cutoff.Format("2006-01-02"), AnalyticsRevision: state.Revision, ContentSetDigest: digest, StrategyRevision: strategyRevision, TargetContentID: contentID, Trigger: FeedbackTriggerUserAttribution, Fingerprint: FeedbackJobFingerprint(FeedbackEligibilityInput{ProjectID: task.ProjectID, Platform: platform, AccountID: accountID, Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly, PeriodStart: periodStart, PeriodEnd: periodEnd, AnalyticsRevision: state.Revision, ContentSetDigest: digest, StrategyRevision: strategyRevision, TargetContentID: contentID}), Status: model.FeedbackJobQueued, SampleCount: valid, Coverage: "partial"}
	now := time.Now().UTC()
	job.QueuedAt = &now
	created, err := s.repo.FeedbackLoop().CreateJob(ctx, job)
	if err != nil {
		return nil, err
	}
	if !created {
		return &AttributionResult{Job: job}, nil
	}
	if s.enqueuer == nil {
		return &AttributionResult{Job: job}, nil
	}
	payload, _ := json.Marshal(map[string]string{"fingerprint": job.Fingerprint, "job_id": job.ID, "operation": job.Operation, "trigger": job.Trigger})
	if _, err := enqueueFeedbackTask(s.enqueuer, "feedback:weekly", payload, "feedback:"+job.Fingerprint, feedbackJitter(job.Fingerprint)); err != nil {
		job.Status = model.FeedbackJobFailed
		job.LastError = "enqueue: " + err.Error()
		_ = s.repo.FeedbackLoop().UpdateJob(ctx, job)
		return nil, err
	}
	return &AttributionResult{Job: job}, nil
}

func observationWindowDays(window string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "24h", "1d":
		return 1, nil
	case "7d":
		return 7, nil
	case "30d":
		return 30, nil
	default:
		return 0, fmt.Errorf("observation_window must be 24h, 7d, or 30d")
	}
}

// Admission only asks whether an observation carries evidence. MetricBasis
// describes time semantics and must never select a platform's vocabulary.
func observationHasMetric(o model.AnalyticsObservation) bool {
	for _, family := range []model.AnalyticsMetricFamily{model.AnalyticsMetricsWechat, model.AnalyticsMetricsSeednote} {
		for _, value := range o.Map(family) {
			if value != nil {
				return true
			}
		}
	}
	return false
}
