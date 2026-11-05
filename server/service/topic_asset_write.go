package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
)

// writeTopicAsset is the common persona-queue write path. Caller owns a transaction.
// Topic text in the queue is historical; ContentAsset owns all editable content.
func writeTopicAsset(ctx context.Context, repo repository.Repository, row *model.TopicPool, question string) (bool, *model.ContentAsset, error) {
	project, err := repo.Projects().FindByIDForUpdate(ctx, row.ProjectID)
	if err != nil || project.UserID != row.UserID {
		return false, nil, fmt.Errorf("project not owned by user")
	}
	row.Topic = strings.TrimSpace(row.Topic)
	if row.Topic == "" || len([]rune(row.Topic)) > maxTopicLength {
		return false, nil, fmt.Errorf("invalid topic title")
	}
	var asset *model.ContentAsset
	if row.ContentAssetID != nil && *row.ContentAssetID != "" {
		asset, err = repo.ContentAssets().FindByID(ctx, row.UserID, *row.ContentAssetID)
	} else {
		asset, err = repo.ContentAssets().FindByTitle(ctx, row.UserID, row.Topic)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			asset = model.NewContentAsset(row.UserID, row.Topic)
			asset.CoreQuestion = strings.TrimSpace(question)
			err = repo.ContentAssets().Create(ctx, asset)
		}
	}
	if err != nil {
		return false, nil, err
	}
	row.ContentAssetID = &asset.ID
	row.DedupeKey = "asset:" + asset.ID
	created, err := repo.TopicPools().AddWithSource(ctx, row)
	if err != nil {
		return false, nil, err
	}
	return created, asset, nil
}
