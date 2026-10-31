package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
)

// FinalizeTopicRadarExecution runs after generic artifact persistence. Durable
// domain ownership, not caller-supplied artifact IDs, authorizes every write.
// Invalid content is a terminal domain failure; infrastructure errors retry.
func (s *TopicRadarService) FinalizeTopicRadarExecution(ctx context.Context, task *model.Task, execution *model.TaskExecution) error {
	if task == nil || execution == nil || task.Channel != model.ChannelTopicRadar || execution.TaskID != task.ID || task.CurrentExecutionID == nil || *task.CurrentExecutionID != execution.ID {
		return errors.New("topic radar execution ownership mismatch")
	}
	snapshot, _ := task.AgentInput.Data()["snapshot"].(map[string]any)
	id, _ := snapshot["snapshot_id"].(string)
	if id == "" {
		return errors.New("topic radar snapshot identity missing")
	}
	switch task.Type {
	case model.TaskTypeTopicDigest:
		digest, err := s.repo.TopicDigests().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if digest.UserID != task.UserID || digest.ProjectID != task.ProjectID || digest.AgentTaskID == nil || *digest.AgentTaskID != task.ID || digest.AgentExecutionID == nil || *digest.AgentExecutionID != execution.ID {
			return errors.New("topic digest execution ownership mismatch")
		}
		if digest.Status == model.TopicDigestStatusSucceeded || digest.Status == model.TopicDigestStatusFailed {
			return nil
		}
		if execution.Status != model.TaskExecutionSucceeded {
			if execution.Status != model.TaskExecutionFailed && execution.Status != model.TaskExecutionCancelled {
				return errors.New("topic execution is not terminal")
			}
			cause := errors.New("topic digest execution failed or was cancelled")
			if err := s.failDigest(ctx, digest, cause); err != nil && !errors.Is(err, cause) {
				return err
			}
			return nil
		}
		body, err := s.topicExecutionArtifact(ctx, execution.ID, "output/topic-digest.json")
		if err != nil {
			return err
		}
		if err := s.finalizeOwnedTopicDigest(ctx, digest, body); err != nil {
			current, readErr := s.repo.TopicDigests().FindByID(ctx, id)
			if readErr != nil {
				return readErr
			}
			if current.Status != model.TopicDigestStatusFailed || current.AgentTaskID == nil || *current.AgentTaskID != task.ID || current.AgentExecutionID == nil || *current.AgentExecutionID != execution.ID {
				return err
			}
		}
		return nil
	case model.TaskTypeTopicScript:
		job, err := s.repo.TopicScriptJobs().FindByID(ctx, id)
		if err != nil {
			return err
		}
		if job.UserID != task.UserID || job.ProjectID != task.ProjectID || job.TaskID == nil || *job.TaskID != task.ID || job.ExecutionID == nil || *job.ExecutionID != execution.ID {
			return errors.New("topic script execution ownership mismatch")
		}
		if job.Status == model.TopicScriptJobSucceeded || job.Status == model.TopicScriptJobFailed {
			return nil
		}
		if execution.Status != model.TaskExecutionSucceeded {
			if execution.Status != model.TaskExecutionFailed && execution.Status != model.TaskExecutionCancelled {
				return errors.New("topic execution is not terminal")
			}
			cause := errors.New("topic script execution failed or was cancelled")
			if err := s.failTopicScript(ctx, job, cause); err != nil && !errors.Is(err, cause) {
				return err
			}
			return nil
		}
		const path = "output/topic-script.json"
		body, err := s.topicExecutionArtifact(ctx, execution.ID, path)
		if err != nil {
			return err
		}
		if err := s.finalizeOwnedTopicScript(ctx, job, body, path); err != nil {
			current, readErr := s.repo.TopicScriptJobs().FindByID(ctx, id)
			if readErr != nil {
				return readErr
			}
			if current.Status != model.TopicScriptJobFailed || current.TaskID == nil || *current.TaskID != task.ID || current.ExecutionID == nil || *current.ExecutionID != execution.ID {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported topic radar task type %q", task.Type)
	}
}

func (s *TopicRadarService) topicExecutionArtifact(ctx context.Context, executionID, path string) ([]byte, error) {
	if s.taskSvc == nil {
		return nil, ErrTopicManagedRuntimeUnavailable
	}
	body, found, err := s.taskSvc.readExecutionArtifact(ctx, executionID, path, maxTaskDeliveryJSONBytes)
	if err != nil {
		return nil, err
	}
	// Missing required artifacts are rejected by the domain validator. A storage
	// read failure above remains retryable and never becomes a content failure.
	if !found {
		return nil, nil
	}
	return body, nil
}
