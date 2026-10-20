package service

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// Only a policy pause before draft/add is recoverable here. Replaying the
// normal finalizer revalidates every artifact, review and compliance gate.
func (s *TaskService) resumeReuseDraftFinalization(ctx context.Context, task *model.Task) error {
	if task == nil || task.ReuseRunID == "" || task.Status != model.TaskStatusCompleted || task.CurrentExecutionID == nil {
		return nil
	}
	if _, err := contentReusePublicationPolicy(ctx, s.repo, task); err != nil {
		return err
	}
	execution, err := s.repo.TaskExecutions().FindByID(ctx, *task.CurrentExecutionID)
	if err != nil {
		return err
	}
	if execution.TaskID != task.ID || execution.Status != model.TaskExecutionSucceeded {
		return nil
	}
	if execution.DraftDeliveryStatus == model.TaskExecutionDraftDeliveryBlocked {
		var evidence struct {
			Code      string `json:"code"`
			Attempted bool   `json:"attempted"`
		}
		if json.Unmarshal(execution.DraftDeliveryResult, &evidence) != nil || evidence.Code != "content_reuse_policy_unavailable" || evidence.Attempted {
			return nil
		}
		if _, err := s.repo.WechatPublications().FindByTaskID(ctx, task.ID); err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		won, err := s.repo.TaskExecutions().TransitionDraftDelivery(ctx, execution.ID, model.TaskExecutionDraftDeliveryBlocked, "", []byte(`{}`))
		if err != nil || !won {
			return err
		}
	} else if execution.DraftDeliveryStatus != "" {
		return nil
	}
	if err := s.finalizeCloudDraftDelivery(ctx, task, execution, nil); err != nil {
		return err
	}
	return s.finalizeCloudTaskOutcome(ctx, task, execution, &agent.ExecutionResult{Success: true, RemoteArtifacts: true})
}
