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
	input := map[string]any{"feedback_job_id": job.ID, "operation": job.Operation, "target_content_id": job.TargetContentID, "analytics_revision": job.AnalyticsRevision, "profile_revision": job.ProfileRevision, "attribution_direction": job.AttributionDirection, "performance_metric": job.PerformanceMetric, "performance_metric_basis": job.PerformanceMetricBasis, "performance_score": job.PerformanceScore, "performance_percentile": job.PerformancePercentile, "period_start": job.PeriodStart, "period_end": job.PeriodEnd, "fingerprint": job.Fingerprint}
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
	content.Channel = normalizeAttributionChannel(content.Channel, content.Platform)
	if content.Channel == "" {
		return nil, ErrAttributionIdentityConflict
	}
	if err := verifyPublishedAttributionContent(ctx, s.repo, content); err != nil {
		return nil, err
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
	project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return nil, err
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
	if project != nil {
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
	profile := projectProfileFor(project)
	targetObservations := maturedAttributionObservations(observations, content, project.Timezone, time.Now().UTC(), time.Duration(days)*24*time.Hour)
	baseline, err := loadAttributionBaseline(ctx, db, content, project.Timezone, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("load attribution baseline: %w", err)
	}
	frozen := model.FeedbackFrozenContext{SchemaVersion: "1.0", AnalyticsRevision: state.Revision, ProfileRevision: profile.Version, TargetContent: content, TargetObservations: targetObservations, Baseline: baseline, ProjectProfile: profile}
	frozenBytes, err := json.Marshal(frozen)
	if err != nil {
		return nil, fmt.Errorf("marshal frozen attribution context: %w", err)
	}
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
	job := &model.FeedbackJob{ID: uuid.NewString(), UserID: task.UserID, ProjectID: task.ProjectID, Platform: platform, AccountID: accountID, Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly, PeriodStart: periodStart, PeriodEnd: periodEnd, MaturityCutoff: cutoff.Format("2006-01-02"), AnalyticsRevision: state.Revision, ContentSetDigest: digest, ProfileRevision: profile.Version, FrozenContextJSON: model.NullableJSON(string(frozenBytes)), StrategyRevision: strategyRevision, TargetContentID: contentID, Trigger: FeedbackTriggerUserAttribution, Fingerprint: FeedbackJobFingerprint(FeedbackEligibilityInput{ProjectID: task.ProjectID, Platform: platform, AccountID: accountID, Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly, PeriodStart: periodStart, PeriodEnd: periodEnd, AnalyticsRevision: state.Revision, ContentSetDigest: digest, StrategyRevision: strategyRevision, TargetContentID: contentID}), Status: model.FeedbackJobQueued, SampleCount: valid, Coverage: "partial"}
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

func verifyPublishedAttributionContent(ctx context.Context, repo repository.Repository, content model.AnalyticsContent) error {
	db := repo.Analytics().DB().WithContext(ctx)
	switch content.Channel {
	case model.ChannelArticle, model.ChannelWechatPicture:
		var publication model.WechatPublication
		query := db.Where("project_id = ? AND status = ?", content.ProjectID, model.WechatPublicationStatusPublished)
		if content.PublicationID != "" {
			query = query.Where("id = ?", content.PublicationID)
		} else if content.TaskID != "" {
			query = query.Where("task_id = ?", content.TaskID)
		} else {
			return ErrAttributionContentNotOwned
		}
		if err := query.First(&publication).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAttributionWindowNotMature
			}
			return err
		}
	case model.ChannelSeednote:
		if content.PostID == "" {
			return ErrAttributionContentNotOwned
		}
		var post model.SeednotePost
		if err := db.Where("id = ? AND project_id = ? AND first_published_at IS NOT NULL", content.PostID, content.ProjectID).First(&post).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrAttributionWindowNotMature
			}
			return err
		}
		if post.FirstPublishedAtBatchID != "" {
			var batch model.SeednoteImportBatch
			if err := db.Where("id = ? AND project_id = ? AND revoked_at IS NULL", post.FirstPublishedAtBatchID, content.ProjectID).First(&batch).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrAttributionWindowNotMature
				}
				return err
			}
		}
	}
	return nil
}

func normalizeAttributionChannel(channel, platform string) string {
	channel = strings.TrimSpace(channel)
	if channel != "" {
		return channel
	}
	switch strings.TrimSpace(platform) {
	case model.PlatformWechat:
		return model.ChannelArticle
	case model.PlatformSeednote:
		return model.ChannelSeednote
	default:
		return ""
	}
}

func loadAttributionBaseline(ctx context.Context, db *gorm.DB, target model.AnalyticsContent, timezone string, at time.Time) ([]model.FeedbackBaselineItem, error) {
	if db == nil || strings.TrimSpace(target.Channel) == "" {
		return nil, nil
	}
	var contents []model.AnalyticsContent
	q := db.WithContext(ctx).Where("project_id = ? AND channel = ? AND content_type = ? AND id <> ?", target.ProjectID, target.Channel, target.ContentType, target.ID)
	switch target.Channel {
	case model.ChannelArticle, model.ChannelWechatPicture:
		q = q.Where(`EXISTS (SELECT 1 FROM wechat_publications wp WHERE wp.project_id = analytics_contents.project_id AND (wp.id = analytics_contents.publication_id OR (analytics_contents.task_id <> '' AND wp.task_id = analytics_contents.task_id)) AND wp.status = ?)`, model.WechatPublicationStatusPublished)
	case model.ChannelSeednote:
		q = q.Where(`analytics_contents.post_id <> '' AND EXISTS (SELECT 1 FROM seednote_posts sp WHERE sp.id = analytics_contents.post_id AND sp.project_id = analytics_contents.project_id AND sp.first_published_at IS NOT NULL AND (sp.first_published_at_batch_id IS NULL OR sp.first_published_at_batch_id = '' OR EXISTS (SELECT 1 FROM seednote_import_batches sb WHERE sb.id = sp.first_published_at_batch_id AND sb.revoked_at IS NULL)))`)
	}
	if target.Date != nil {
		q = q.Where("date < ?", *target.Date)
	}
	if err := q.Order("date desc").Limit(100).Find(&contents).Error; err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(contents))
	byID := make(map[string]model.AnalyticsContent, len(contents))
	for _, item := range contents {
		ids = append(ids, item.ID)
		byID[item.ID] = item
	}
	if len(ids) == 0 {
		return []model.FeedbackBaselineItem{}, nil
	}
	var observations []model.AnalyticsObservation
	if err := db.WithContext(ctx).Where("project_id = ? AND content_id IN ? AND revoked_at IS NULL AND metric_basis = ? AND stat_date <= ?", target.ProjectID, ids, "cumulative", at.Format("2006-01-02")).Order("content_id asc, stat_date desc, effective_at desc, received_at desc, sequence desc").Find(&observations).Error; err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	result := make([]model.FeedbackBaselineItem, 0, len(observations))
	for _, observation := range observations {
		if seen[observation.ContentID] || !observationHasMetric(observation) {
			continue
		}
		content := byID[observation.ContentID]
		if !analyticsObservationMatured(observation, content.Date, timezone, 7*24*time.Hour) {
			continue
		}
		seen[observation.ContentID] = true
		score, ok := attributionEngagementRate(observation, target.Channel)
		if ok {
			result = append(result, model.FeedbackBaselineItem{Content: content, Observation: observation, Score: score})
		}
	}
	return result, nil
}

func maturedAttributionObservations(observations []model.AnalyticsObservation, content model.AnalyticsContent, timezone string, at time.Time, age time.Duration) []model.AnalyticsObservation {
	result := make([]model.AnalyticsObservation, 0, len(observations))
	for _, observation := range observations {
		if observationHasMetric(observation) && observation.StatDate <= at.Format("2006-01-02") && analyticsObservationMatured(observation, content.Date, timezone, age) {
			result = append(result, observation)
		}
	}
	return result
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
