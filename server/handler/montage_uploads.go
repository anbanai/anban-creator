package handler

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func isMontageProjectForUser(ctx context.Context, repo repository.Repository, userID, projectID string) bool {
	if repo == nil || userID == "" || projectID == "" {
		return false
	}
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil || project.UserID != userID {
		return false
	}
	return model.IsMontagePlatform(project.Platform)
}
