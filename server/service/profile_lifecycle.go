package service

import (
	"context"
	"strings"

	"gorm.io/datatypes"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// UpdateProfileLifecycle changes the execution marker while preserving the
// current dimensions and revision. It is intentionally idempotent so bootstrap
// retries and terminal reconciliation can call it repeatedly.
func UpdateProfileLifecycle(ctx context.Context, repo repository.Repository, projectID, taskID, state, message string) error {
	if repo == nil || strings.TrimSpace(projectID) == "" || strings.TrimSpace(taskID) == "" {
		return nil
	}
	return repo.WithTx(ctx, func(tx repository.Repository) error {
		project, err := tx.Projects().FindByIDForUpdate(ctx, projectID)
		if err != nil {
			return err
		}
		profile := project.Profile.Data()
		if profile.SchemaVersion == 0 || profile.AnalysisTaskID != taskID {
			return nil
		}
		profile.InitializationStatus = state
		profile.LastError = strings.TrimSpace(message)
		project.Profile = datatypes.NewJSONType(profile)
		if err := tx.Projects().Update(ctx, project); err != nil {
			return err
		}
		return persistProfileState(ctx, tx, projectID, profile)
	})
}

func ProfileTaskStateFromTaskStatus(status string) string {
	if status == model.TaskStatusFailed || status == model.TaskStatusCancelled {
		return model.ProfileInitializationFailed
	}
	return model.ProfileInitializationRunning
}
