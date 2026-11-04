package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// TopicPoolService manages the topic pool per project.
type TopicPoolService struct {
	repo   repository.Repository
	logger *zerolog.Logger
}

// NewTopicPoolService creates a new TopicPoolService.
func NewTopicPoolService(repo repository.Repository, logger *zerolog.Logger) *TopicPoolService {
	return &TopicPoolService{repo: repo, logger: logger}
}

const maxTopicLength = 500

type ClaimTopicRequest struct {
	UserID    string
	ProjectID string
	TaskID    string
}

type ClaimTopicResult struct {
	Topic   string
	AssetID string
	TaskID  string
}

func (s *TopicPoolService) ClaimTopic(ctx context.Context, req ClaimTopicRequest) (*ClaimTopicResult, error) {
	if strings.TrimSpace(req.TaskID) == "" {
		return nil, fmt.Errorf("task_id is required to claim a topic")
	}
	result := &ClaimTopicResult{TaskID: req.TaskID}
	err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		project, err := tx.Projects().FindByIDForUpdate(ctx, req.ProjectID)
		if err != nil || project.UserID != req.UserID {
			return fmt.Errorf("project not owned by user")
		}
		task, err := tx.Tasks().FindByIDForUpdate(ctx, req.TaskID)
		if err != nil || task.UserID != req.UserID || task.ProjectID != req.ProjectID {
			return fmt.Errorf("task does not belong to this user and project")
		}
		if task.Type == model.TaskTypeTopicScript || task.Type == model.TaskTypeTopicDigest || task.ResearchJobID != "" {
			return fmt.Errorf("this task does not consume topics")
		}
		if task.ContentAssetID != "" {
			result.AssetID = task.ContentAssetID
			result.Topic = task.Prompt
			return nil
		}
		if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
			return fmt.Errorf("task is not active")
		}
		if err := bindTaskTopic(ctx, tx, task, nil); err != nil {
			return err
		}
		if task.ContentAssetID == "" {
			return nil
		}
		if err := tx.Tasks().Update(ctx, task); err != nil {
			return err
		}
		if err := tx.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: generateTaskID(), UserID: task.UserID, ProjectID: task.ProjectID, AssetID: task.ContentAssetID, ContentKind: "task", ContentID: task.ID, Relation: model.ContentAssetLinkDerivative, Platform: project.Platform}); err != nil {
			return err
		}
		result.AssetID = task.ContentAssetID
		result.Topic = task.Prompt
		return nil
	})
	return result, err
}

// Add creates one or more topics in the pool for the given project.
func (s *TopicPoolService) Add(ctx context.Context, userID, projectID string, topics []string) ([]*model.TopicPool, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}
	if len(topics) == 0 {
		return nil, fmt.Errorf("at least one topic is required")
	}
	if err := s.validateProjectOwnership(ctx, userID, projectID); err != nil {
		return nil, err
	}

	items := make([]*model.TopicPool, 0, len(topics))
	seen := make(map[string]struct{}, len(topics))
	for _, t := range topics {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		key := model.NormalizeTopicDedupeKey(t)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if len([]rune(t)) > maxTopicLength {
			return nil, fmt.Errorf("topic exceeds %d character limit: %.50q...", maxTopicLength, t)
		}
		items = append(items, &model.TopicPool{
			UserID:    userID,
			ProjectID: projectID,
			Topic:     t,
			DedupeKey: model.NormalizeTopicDedupeKey(t),
			Status:    model.TopicStatusUnused,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("at least one non-empty topic is required")
	}
	added := make([]*model.TopicPool, 0, len(items))
	if err := s.repo.WithTx(ctx, func(tx repository.Repository) error {
		for _, item := range items {
			created, _, err := writeTopicAsset(ctx, tx, item, "")
			if err != nil {
				return err
			}
			if created {
				added = append(added, item)
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return added, nil
}

// List returns topics for a project with optional status filter and pagination.
func (s *TopicPoolService) List(ctx context.Context, userID, projectID, status string, offset, limit int) ([]*model.TopicPool, int64, error) {
	if err := s.validateProjectOwnership(ctx, userID, projectID); err != nil {
		return nil, 0, err
	}
	rows, total, err := s.repo.TopicPools().FindByProject(ctx, projectID, status, offset, limit)
	return rows, total, err
}

func (s *TopicPoolService) validateProjectOwnership(ctx context.Context, userID, projectID string) error {
	ch, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		return fmt.Errorf("find project: %w", err)
	}
	if ch.UserID != userID {
		return fmt.Errorf("project not owned by user")
	}
	return nil
}
