package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

// Publish-approval gate errors (Batch 4A). The handler maps these to HTTP 409
// (state conflict): the request itself is well-formed, but the task is not in a
// state that allows the action.
var (
	// ErrPublishApprovalNotPending means the task is not awaiting publish review
	// (it was never gated, or has already been approved/rejected).
	ErrPublishApprovalNotPending = errors.New("publish approval: task is not pending review")
	// ErrPublishApprovalUnavailable means the task is pending but cannot be
	// published right now — publishing is disabled on the project, the publish
	// service is unavailable, or the frozen draft data is missing/empty.
	ErrPublishApprovalUnavailable = errors.New("publish approval: cannot publish (publishing disabled or draft data missing)")
)

// holdPublishForApproval freezes the extracted draft articles into the task and
// enters the "pending" approval state instead of auto-publishing. Called from
// the HandleExecution publish branch when project.GetRequirePublishApproval().
//
// Fail-loud: a DB failure setting the pending state is logged at Error and
// surfaced via a progress event. We deliberately do NOT fail-open (auto-publish)
// on a transient failure: this gate exists for brand safety, so publishing
// unreviewed content on an infra blip would defeat its purpose. The generated
// article survives in task_files, so failing-loud loses nothing — the operator
// can retry once the DB recovers. (The marshal itself is effectively infallible
// — DraftArticleInput is a struct of plain scalars.)
func (s *TaskService) holdPublishForApproval(ctx context.Context, taskID string, articles []DraftArticleInput) {
	articlesJSON, err := json.Marshal(articles)
	if err != nil {
		s.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to marshal articles for publish approval")
		if s.pubsub != nil {
			s.pubsub.PublishProgress(ctx, taskID, "⚠️ 发布审核暂存失败，请联系管理员或重试")
		}
		return
	}
	// Bounded retry: a transient DB blip at exactly this UPDATE would otherwise
	// leave the task completed-but-not-pending with no recovery path (ApprovePublish
	// rejects non-pending). A few short-backoff retries cover the realistic
	// transient failure without weakening the fail-loud stance for a persistent
	// outage. The generated article survives in task_files regardless.
	var updateErr error
	for attempt := 1; attempt <= 3; attempt++ {
		updateErr = s.repo.Tasks().UpdatePublishApproval(ctx, taskID, model.PublishApprovalStatePending, articlesJSON)
		if updateErr == nil {
			break
		}
		s.logger.Warn().Err(updateErr).Str("task_id", taskID).Int("attempt", attempt).Msg("publish approval pending write failed, retrying")
		if attempt < 3 {
			select {
			case <-time.After(500 * time.Millisecond):
			case <-ctx.Done():
			}
			if ctx.Err() != nil {
				break // context cancelled; stop retrying
			}
		}
	}
	if updateErr != nil {
		s.logger.Error().Err(updateErr).Str("task_id", taskID).Msg("failed to set publish approval pending after retries")
		if s.pubsub != nil {
			s.pubsub.PublishProgress(ctx, taskID, "⚠️ 发布审核暂存失败（数据库暂不可用），请联系管理员")
		}
		return
	}
	s.logger.Info().Str("task_id", taskID).Int("articles", len(articles)).Msg("publish approval pending: draft held for human review")
	if s.pubsub != nil {
		s.pubsub.PublishProgress(ctx, taskID, "草稿已完成，等待发布审核")
	}
}

// ApprovePublish resumes the held publish: it atomically flips the approval
// state pending→approved and publishes the frozen draft to the WeChat draft box.
// Publishing runs asynchronously (matching the existing auto-publish
// fire-and-forget semantics — PublishDraft may upload images and take seconds;
// failures are logged, the published flag is set only on success). logText is
// "" because the agent did NOT publish (that is precisely why the task was
// gated).
//
// Double-publish safety: the state flip is a single Compare-And-Swap
// (CompareAndSwapPublishApproval). Among concurrent approve calls exactly one
// CAS wins and spawns the publish goroutine; the rest get !swapped and return
// ErrPublishApprovalNotPending without publishing.
//
// CALLER MUST verify task ownership before calling. The handler performs the
// GetByID + UserID ownership check (same boundary Retry/Cancel rely on); this
// method does not re-check, matching those established services.
func (s *TaskService) ApprovePublish(ctx context.Context, taskID string) error {
	task, err := s.repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("find task: %w", err)
	}
	if task.PublishApprovalState != model.PublishApprovalStatePending {
		return ErrPublishApprovalNotPending
	}
	if s.publishingSvc == nil {
		return ErrPublishApprovalUnavailable
	}

	project, err := s.repo.Projects().FindByID(ctx, task.ProjectID)
	if err != nil {
		return fmt.Errorf("find project: %w", err)
	}
	if !project.GetEnablePublishing() {
		return ErrPublishApprovalUnavailable
	}

	var articles []DraftArticleInput
	if len(task.PendingDraftArticles) > 0 {
		if err := json.Unmarshal(task.PendingDraftArticles, &articles); err != nil {
			return fmt.Errorf("unmarshal pending draft articles: %w", err)
		}
	}
	if len(articles) == 0 {
		return ErrPublishApprovalUnavailable
	}

	// Atomic pending→approved (clears the frozen blob per spec). Captured
	// articles are already in memory, so clearing the blob does not lose them.
	// Only the single winning caller proceeds to publish; concurrent callers
	// get !swapped → ErrPublishApprovalNotPending. No double-publish.
	swapped, err := s.repo.Tasks().CompareAndSwapPublishApproval(ctx, taskID,
		model.PublishApprovalStatePending, model.PublishApprovalStateApproved, true)
	if err != nil {
		return fmt.Errorf("set publish approval approved: %w", err)
	}
	if !swapped {
		return ErrPublishApprovalNotPending
	}
	if s.pubsub != nil {
		s.pubsub.PublishProgress(ctx, taskID, "已通过发布审核，正在发布到草稿箱")
	}

	publishCtx, publishCancel := context.WithTimeout(context.Background(), 60*time.Second)
	taskCopy := *task
	projectCopy := *project
	go func() {
		defer publishCancel()
		s.autoPublishWithData(publishCtx, &taskCopy, &projectCopy, articles, "")
	}()
	return nil
}

// RejectPublish closes the approval gate without publishing: it atomically flips
// the state pending→rejected (clearing the frozen blob per spec) and notifies
// the user. reason is an optional human note surfaced in the progress event.
//
// CALLER MUST verify task ownership before calling (handler does so). The CAS
// is the sole guard here: if it does not win (e.g. a concurrent approve already
// flipped the state), it returns ErrPublishApprovalNotPending — no FindByID
// preflight is needed because no task data is required to reject.
func (s *TaskService) RejectPublish(ctx context.Context, taskID, reason string) error {
	swapped, err := s.repo.Tasks().CompareAndSwapPublishApproval(ctx, taskID,
		model.PublishApprovalStatePending, model.PublishApprovalStateRejected, true)
	if err != nil {
		return fmt.Errorf("set publish approval rejected: %w", err)
	}
	if !swapped {
		return ErrPublishApprovalNotPending
	}
	s.logger.Info().Str("task_id", taskID).Str("reason", reason).Msg("publish approval rejected")

	if s.pubsub != nil {
		msg := "已驳回发布审核"
		if reason != "" {
			msg = "已驳回发布审核：" + reason
		}
		s.pubsub.PublishProgress(ctx, taskID, msg)
	}
	return nil
}
