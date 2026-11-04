package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

// projectTopicLink projects owned lineage within the caller's transaction.
// References attach a persona, but do not change the referenced task's identity.
func projectTopicLink(ctx context.Context, repo repository.Repository, link *model.ContentAssetLink) error {
	if link.ProjectID == "" {
		return nil
	}
	if link.Relation == model.ContentAssetLinkReference {
		return repo.ContentAssets().ProjectTopicAssociation(ctx, link.UserID, link.ProjectID, link.AssetID, "", false, link.CreatedAt)
	}
	task, at, published, err := topicLinkedTask(ctx, repo, link.UserID, link.ProjectID, link.ContentKind, link.ContentID)
	if err != nil {
		return err
	}
	taskID := ""
	if task != nil {
		taskID = task.ID
		published = topicTaskIsProduction(task)
		at = task.CreatedAt
	}
	if at.IsZero() {
		at = link.CreatedAt
	}
	return repo.ContentAssets().ProjectTopicAssociation(ctx, link.UserID, link.ProjectID, link.AssetID, taskID, published, at)
}

func topicLinkedTask(ctx context.Context, repo repository.Repository, userID, projectID, kind, contentID string) (*model.Task, time.Time, bool, error) {
	var at time.Time
	taskID := ""
	published := false
	switch kind {
	case "task", "video_task":
		taskID = contentID
	case "wechat_publication":
		publication, err := repo.WechatPublications().FindByID(ctx, contentID)
		if err != nil {
			return nil, at, false, err
		}
		if publication.UserID != userID || publication.ProjectID != projectID {
			return nil, at, false, fmt.Errorf("publication ownership mismatch")
		}
		taskID = publication.TaskID
		at = publication.CreatedAt
		published = publication.Status == model.WechatPublicationStatusPublished
		if publication.PublishedAt != nil {
			at = *publication.PublishedAt
		}
	case "seednote_post":
		post, err := repo.SeednotePosts().FindByID(ctx, projectID, contentID)
		if err != nil {
			return nil, at, false, err
		}
		if post.UserID != userID {
			return nil, at, false, fmt.Errorf("post ownership mismatch")
		}
		taskID = post.TaskID
		at = post.CreatedAt
		published = true
		if post.FirstPublishedAt != nil {
			at = *post.FirstPublishedAt
		}
	}
	if taskID == "" {
		return nil, at, published, nil
	}
	task, err := repo.Tasks().FindByID(ctx, taskID)
	// Imported publications may legitimately have no managed task in this database.
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, at, published, nil
	}
	if err != nil {
		return nil, at, false, err
	}
	if task.UserID != userID || task.ProjectID != projectID {
		return nil, at, false, fmt.Errorf("task ownership mismatch")
	}
	return task, at, published, nil
}

func resolvePublishedTopic(ctx context.Context, repo repository.Repository, userID, projectID string, row repository.AnalyticsContentRow) (*model.ContentAsset, error) {
	kind, contentID := discoveredContentIdentity(row)
	task, _, _, err := topicLinkedTask(ctx, repo, userID, projectID, kind, contentID)
	if err != nil {
		return nil, err
	}
	if task == nil && row.TaskID != "" {
		task, _, _, err = topicLinkedTask(ctx, repo, userID, projectID, "task", row.TaskID)
		if err != nil {
			return nil, err
		}
	}
	if task != nil && task.ContentAssetID != "" {
		return repo.ContentAssets().FindByID(ctx, userID, task.ContentAssetID)
	}
	identities := [][2]string{{kind, contentID}}
	if task != nil {
		identities = append(identities, [2]string{"task", task.ID}, [2]string{"video_task", task.ID})
	}
	for _, identity := range identities {
		links, err := repo.ContentAssets().ListLinksByContent(ctx, userID, projectID, identity[0], identity[1])
		if err != nil {
			return nil, err
		}
		for _, link := range links {
			if link.Relation == model.ContentAssetLinkOrigin || link.Relation == model.ContentAssetLinkDerivative {
				return repo.ContentAssets().FindByID(ctx, userID, link.AssetID)
			}
		}
	}
	return nil, gorm.ErrRecordNotFound
}
