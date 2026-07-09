package handler

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func isOpenMontageProjectForUser(ctx context.Context, repo repository.Repository, userID, projectID string) bool {
	if repo == nil || userID == "" || projectID == "" {
		return false
	}
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil || project.UserID != userID {
		return false
	}
	return model.IsOpenMontagePlatform(project.Platform)
}
