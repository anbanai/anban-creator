package service

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// ReconcileTopicDigestEffects resumes durable fan-out after a successful digest,
// including a crash between domain settlement and the task completion callback.
func (s *TopicRadarService) ReconcileTopicDigestEffects(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	digests, err := s.repo.TopicDigests().ListPendingEffects(ctx, limit)
	if err != nil {
		return err
	}
	var first error
	for _, digest := range digests {
		if err := s.queueTopicDigestEffects(ctx, digest); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *TopicRadarService) queueTopicDigestEffects(ctx context.Context, digest *model.TopicDigest) error {
	if digest.Status != model.TopicDigestStatusSucceeded || digest.EffectsQueuedAt != nil {
		return nil
	}
	var frozen topicFrozenContext
	if err := json.Unmarshal(digest.InputSnapshot, &frozen); err != nil {
		return fmt.Errorf("decode frozen topic context: %w", err)
	}
	var policy struct {
		Enabled         bool   `json:"enabled"`
		AutoGenerate    bool   `json:"auto_generate"`
		TopN            int    `json:"top_n"`
		Platform        string `json:"platform"`
		DurationSeconds int    `json:"duration_seconds"`
		AspectRatio     string `json:"aspect_ratio"`
	}
	if len(frozen.Subscription.ScriptPolicy) > 0 {
		if err := json.Unmarshal(frozen.Subscription.ScriptPolicy, &policy); err != nil {
			return err
		}
	}
	if policy.Enabled || policy.AutoGenerate {
		if policy.TopN <= 0 {
			policy.TopN = 1
		}
		if policy.Platform == "" {
			policy.Platform = "douyin"
		}
		suggestions, err := s.repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
		if err != nil {
			return err
		}
		for i, suggestion := range suggestions {
			if i >= policy.TopN {
				break
			}
			job, err := s.requestTopicScript(ctx, RequestTopicScriptParams{UserID: digest.UserID, AssetID: suggestion.ContentAssetID, ProjectID: digest.ProjectID, sourceDigestID: digest.ID, sourceSuggestionID: suggestion.ID, TargetPlatform: policy.Platform, DurationSeconds: policy.DurationSeconds, AspectRatio: policy.AspectRatio}, false)
			// A persisted queued job is itself a recoverable outbox entry.
			if err != nil && job == nil {
				return err
			}
		}
	}
	if _, err := s.QueueDigestNotifications(ctx, digest.ID); err != nil {
		return err
	}
	return s.repo.TopicDigests().MarkEffectsQueued(ctx, digest.ID, s.now())
}

func (s *TopicRadarService) ReconcileTopicNotifications(ctx context.Context, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	deliveries, err := s.repo.TopicNotificationDeliveries().ListRecoverable(ctx, s.now(), limit)
	if err != nil {
		return err
	}
	var first error
	for _, delivery := range deliveries {
		if err := s.SendTopicNotification(ctx, delivery.ID); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (s *TopicRadarService) ProcessTopicDigest(ctx context.Context, id string) error {
	digest, err := s.repo.TopicDigests().FindByID(ctx, id)
	if err != nil {
		return err
	}
	if digest.Status == model.TopicDigestStatusSucceeded {
		return s.queueTopicDigestEffects(ctx, digest)
	}
	if digest.Status == model.TopicDigestStatusFailed {
		return nil
	}
	task, err := s.EnsureTopicDigestManagedTask(ctx, id)
	if err != nil {
		// A permanent admission failure (profile or frozen-input validation)
		// must release the active-run guard instead of wedging the subscription
		// forever; infrastructure errors stay retryable.
		if !topicAdmissionErrorIsTransient(err) {
			return s.failDigest(ctx, digest, err)
		}
		return err
	}
	if task == nil {
		return errors.New("topic digest has no managed task")
	}
	if err := s.processTopicManagedTask(ctx, task); err != nil {
		return err
	}
	current, err := s.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		return err
	}
	if current.Status == model.TaskStatusFailed || current.Status == model.TaskStatusCancelled || current.Status == model.TaskStatusCompleted {
		latest, err := s.repo.TopicDigests().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if latest.Status != model.TopicDigestStatusSucceeded && latest.Status != model.TopicDigestStatusFailed && latest.AgentTaskID != nil && *latest.AgentTaskID == task.ID {
			return s.failDigest(ctx, latest, errors.New("managed digest task did not complete"))
		}
	}
	return nil
}

func (s *TopicRadarService) ProcessTopicScript(ctx context.Context, id string) error {
	job, err := s.repo.TopicScriptJobs().FindByID(ctx, id)
	if err != nil {
		return err
	}
	if job.Status == model.TopicScriptJobSucceeded || job.Status == model.TopicScriptJobFailed {
		return nil
	}
	task, err := s.EnsureTopicScriptManagedTask(ctx, id)
	if err != nil {
		if !topicAdmissionErrorIsTransient(err) {
			return s.failTopicScript(ctx, job, err)
		}
		return err
	}
	if task == nil {
		return errors.New("topic script has no managed task")
	}
	if err := s.processTopicManagedTask(ctx, task); err != nil {
		return err
	}
	current, err := s.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		return err
	}
	if current.Status == model.TaskStatusFailed || current.Status == model.TaskStatusCancelled || current.Status == model.TaskStatusCompleted {
		latest, err := s.repo.TopicScriptJobs().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if latest.Status != model.TopicScriptJobSucceeded && latest.Status != model.TopicScriptJobFailed && latest.TaskID != nil && *latest.TaskID == task.ID {
			latest.Status = model.TopicScriptJobFailed
			latest.Error = "managed script task did not complete"
			now := s.now()
			latest.FinishedAt = &now
			_, err = s.repo.TopicScriptJobs().UpdateIfNonterminal(ctx, latest)
			return err
		}
	}
	return nil
}

func (s *TopicRadarService) processTopicManagedTask(ctx context.Context, task *model.Task) error {
	if s.taskSvc == nil {
		return errors.New("topic radar task service unavailable")
	}
	if task.CurrentExecutionID != nil && *task.CurrentExecutionID != "" {
		execution, err := s.repo.TaskExecutions().FindByID(ctx, *task.CurrentExecutionID)
		if err != nil {
			return err
		}
		if execution.Status == model.TaskExecutionSucceeded || execution.Status == model.TaskExecutionFailed || execution.Status == model.TaskExecutionCancelled {
			return s.FinalizeTopicRadarExecution(ctx, task, execution)
		}
	}
	if task.Status == model.TaskStatusPending {
		return s.taskSvc.HandleExecutionFromPayload(ctx, task.ID, task.UserID)
	}
	return nil
}

// topicAdmissionErrorIsTransient keeps infrastructure failures retryable while
// permanent admission rejections terminalize the durable digest or script.
func topicAdmissionErrorIsTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, gorm.ErrInvalidDB) {
		return true
	}
	var netErr *net.OpError
	return errors.As(err, &netErr)
}
