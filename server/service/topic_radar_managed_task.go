package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrTopicManagedRuntimeUnavailable = errors.New("topic radar managed runtime unavailable")
var errTopicTaskAlreadyClaimed = errors.New("topic radar task ownership already claimed")

func (s *TopicRadarService) EnsureTopicDigestManagedTask(ctx context.Context, digestID string) (*model.Task, error) {
	if s == nil || s.taskSvc == nil {
		return nil, ErrTopicManagedRuntimeUnavailable
	}
	digest, err := s.repo.TopicDigests().FindByID(ctx, strings.TrimSpace(digestID))
	if err != nil {
		return nil, err
	}
	if digest.AgentTaskID != nil && strings.TrimSpace(*digest.AgentTaskID) != "" {
		return s.repo.Tasks().FindByID(ctx, *digest.AgentTaskID)
	}
	if !json.Valid(digest.InputSnapshot) {
		return nil, errors.New("invalid frozen digest input")
	}
	refs, _ := frozenTopicSourceRefs(digest.InputSnapshot)
	if refs == nil {
		refs = []model.TopicSourceRef{}
	}
	input := map[string]any{
		"schema_version": "1.0", "task_id": "", "execution_id": "", "task_kind": model.TaskKindTopicDigest,
		"snapshot":       map[string]any{"snapshot_id": digest.ID, "snapshot_hash": digest.InputHash, "captured_at": digest.CreatedAt.UTC().Format(time.RFC3339Nano), "project_id": digest.ProjectID, "subscription_id": digest.SubscriptionID, "digest_id": digest.ID},
		"source_records": refs, "frozen_context": json.RawMessage(digest.InputSnapshot),
	}
	input, err = normalizeTopicPackInput(input)
	if err != nil {
		return nil, err
	}
	created, err := s.taskSvc.CreateManual(ctx, CreateManualParams{
		UserID: digest.UserID, ProjectID: digest.ProjectID, AgentID: model.AgentIDTopicRadar, Channel: model.ChannelTopicRadar,
		TaskKind: model.TaskKindTopicDigest, RequestedTaskType: model.TaskTypeTopicDigest, ExecutionProfile: "effective",
		Prompt: "Generate source-attributed suggestions and dimension scores from the frozen topic radar context. Server computes final ranking.", AgentInput: input, topicDigestID: digest.ID,
	})
	if errors.Is(err, errTopicTaskAlreadyClaimed) {
		current, findErr := s.repo.TopicDigests().FindByID(ctx, digest.ID)
		if findErr != nil {
			return nil, findErr
		}
		if current.AgentTaskID != nil {
			return s.repo.Tasks().FindByID(ctx, *current.AgentTaskID)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(created) != 1 || created[0] == nil {
		return nil, errors.New("topic digest task creation returned no task")
	}
	return created[0], nil
}

func (s *TopicRadarService) EnsureTopicScriptManagedTask(ctx context.Context, jobID string) (*model.Task, error) {
	if s == nil || s.taskSvc == nil {
		return nil, ErrTopicManagedRuntimeUnavailable
	}
	job, err := s.repo.TopicScriptJobs().FindByID(ctx, strings.TrimSpace(jobID))
	if err != nil {
		return nil, err
	}
	if job.TaskID != nil && strings.TrimSpace(*job.TaskID) != "" {
		return s.repo.Tasks().FindByID(ctx, *job.TaskID)
	}
	var input map[string]any
	if err := json.Unmarshal(job.InputSnapshot, &input); err != nil || len(input) == 0 {
		return nil, errors.New("invalid frozen script input")
	}
	input, err = normalizeTopicPackInput(input)
	if err != nil {
		return nil, err
	}
	created, err := s.taskSvc.CreateManual(ctx, CreateManualParams{
		UserID: job.UserID, ProjectID: job.ProjectID, AgentID: model.AgentIDTopicRadar, Channel: model.ChannelTopicRadar,
		TaskKind: model.TaskKindTopicScript, RequestedTaskType: model.TaskTypeTopicScript, ExecutionProfile: "effective",
		Prompt: "Generate a dedicated short-video script for the selected topic.", AgentInput: input, topicScriptJobID: job.ID, ContentAssetID: job.ContentAssetID,
	})
	if errors.Is(err, errTopicTaskAlreadyClaimed) {
		current, findErr := s.repo.TopicScriptJobs().FindByID(ctx, job.ID)
		if findErr != nil {
			return nil, findErr
		}
		if current.TaskID != nil {
			return s.repo.Tasks().FindByID(ctx, *current.TaskID)
		}
	}
	if err != nil {
		return nil, err
	}
	if len(created) != 1 || created[0] == nil {
		return nil, errors.New("topic script task creation returned no task")
	}
	return created[0], nil
}

// Called inside the same admission transaction that inserts and bills the task.
func bindTopicManagedTask(ctx context.Context, repo repository.Repository, task *model.Task, p *CreateManualParams) error {
	var claimed bool
	var err error
	switch task.Type {
	case model.TaskTypeTopicDigest:
		if p == nil || p.topicDigestID == "" {
			return errors.New("topic digest requires server-owned admission")
		}
		claimed, err = repo.TopicDigests().ClaimTask(ctx, p.topicDigestID, task)
	case model.TaskTypeTopicScript:
		if p == nil || p.topicScriptJobID == "" {
			return errors.New("topic script requires server-owned admission")
		}
		claimed, err = repo.TopicScriptJobs().ClaimTask(ctx, p.topicScriptJobID, task)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	if !claimed {
		return errTopicTaskAlreadyClaimed
	}
	return nil
}

func normalizeTopicPackInput(input map[string]any) (map[string]any, error) {
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	err = json.Unmarshal(data, &result)
	return result, err
}

func topicStringPtr(value string) *string { return &value }
