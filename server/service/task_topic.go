package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrTopicAlreadyUsed = errors.New("该人设已使用此选题，请从选题详情选择跨平台复用")

// bindTaskTopic runs inside task admission, after the project has been locked.
// A rejected admission rolls back both the queue claim and task persistence.
func bindTaskTopic(ctx context.Context, repo repository.Repository, task *model.Task, admission *CreateManualParams) error {
	if task.ReuseRunID != "" || task.ResearchJobID != "" || task.Type == model.TaskTypeTopicDigest || task.Type == model.TaskTypeTopicScript {
		return nil
	}
	switch task.Type {
	case model.TaskTypeWechatArticle, model.TaskTypeWechatPicture, model.PlatformSeednote, model.PlatformMoments:
	default:
		if task.ContentAssetID != "" {
			return fmt.Errorf("this task type does not consume topics")
		}
		return nil
	}
	if admission != nil && admission.PreserveFrozenConfig {
		if task.ContentAssetID != "" {
			if _, err := repo.ContentAssets().FindByID(ctx, task.UserID, task.ContentAssetID); err != nil {
				return ErrContentAssetNotFound
			}
		}
		return nil
	}
	if task.ContentAssetID == "" && strings.TrimSpace(task.Prompt) != "" {
		return nil
	}
	var asset *model.ContentAsset
	if task.ContentAssetID != "" {
		var err error
		asset, err = repo.ContentAssets().FindByID(ctx, task.UserID, task.ContentAssetID)
		if err != nil {
			return ErrContentAssetNotFound
		}
		if asset.Status == model.ContentAssetStatusArchived {
			return fmt.Errorf("topic is archived")
		}
		// Association is explicit; selecting a library topic can attach it to a new persona.
		row := &model.TopicPool{UserID: task.UserID, ProjectID: task.ProjectID, Topic: asset.Title, ContentAssetID: &asset.ID, Status: model.TopicStatusUnused}
		if _, err := repo.TopicPools().AddWithSource(ctx, row); err != nil {
			return err
		}
		if row.ContentAssetID == nil || *row.ContentAssetID != asset.ID {
			return fmt.Errorf("persona already has a different topic with this title")
		}
	}
	var row *model.TopicPool
	var err error
	if asset == nil {
		row, err = repo.TopicPools().ClaimWithTask(ctx, task.UserID, task.ProjectID, task.ID)
	} else {
		row, err = repo.TopicPools().ClaimAssetWithTask(ctx, task.UserID, task.ProjectID, asset.ID, task.ID)
	}
	if err != nil {
		return fmt.Errorf("claim topic: %w", err)
	}
	if row == nil {
		if asset != nil {
			return ErrTopicAlreadyUsed
		}
		return nil
	}
	if row.ContentAssetID == nil {
		return fmt.Errorf("topic has no canonical asset")
	}
	if asset == nil {
		asset, err = repo.ContentAssets().FindByID(ctx, task.UserID, *row.ContentAssetID)
		if err != nil {
			return err
		}
	}
	task.ContentAssetID = asset.ID
	if strings.TrimSpace(task.Prompt) == "" {
		task.Prompt = asset.Title
		if asset.CoreQuestion != "" {
			task.Prompt += "\n切入点：" + asset.CoreQuestion
		}
	}
	return nil
}

// Planning/script executions and reference evidence do not consume article supply.
func topicTaskIsProduction(task *model.Task) bool {
	if task.ResearchJobID != "" {
		return false
	}
	switch task.Type {
	case model.TaskTypeContentResearch, model.TaskTypeContentValidation, model.TaskTypeContentScript, model.TaskTypeTopicDigest, model.TaskTypeTopicScript:
		return false
	}
	switch task.TaskKind {
	case model.TaskKindContentResearch, model.TaskKindContentValidation, model.TaskKindContentScript:
		return false
	}
	switch task.Type {
	case model.TaskTypeWechatArticle, model.TaskTypeWechatPicture, model.PlatformSeednote, model.PlatformMoments:
		return true
	}
	return task.TaskKind == model.TaskKindContentGeneration
}
