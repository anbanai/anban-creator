package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
)

// MigrateProjectProfiles backfills the durable profile read model and revision
// history for projects created before those tables existed. AutoMigrate only
// creates the tables; this pass preserves the existing Project.Profile JSON as
// the initial database revision and is safe to run repeatedly.
func MigrateProjectProfiles(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var projects []model.Project
		if err := tx.Find(&projects).Error; err != nil {
			return fmt.Errorf("load projects for profile migration: %w", err)
		}
		for _, project := range projects {
			profile := project.Profile.Data()
			status := strings.TrimSpace(profile.InitializationStatus)
			if status == "" {
				status = model.ProfileInitializationNotStarted
			}
			state := model.ProjectProfileState{
				ProjectID: project.ID, Status: status, Revision: profile.Version,
				ActiveTaskID: profile.AnalysisTaskID, LastError: profile.LastError,
				UpdatedAt: project.UpdatedAt,
			}
			var existingState model.ProjectProfileState
			if err := tx.Where("project_id = ?", project.ID).First(&existingState).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("load profile state for project %s: %w", project.ID, err)
				}
				if err := tx.Create(&state).Error; err != nil {
					return fmt.Errorf("create profile state for project %s: %w", project.ID, err)
				}
			}
			if profile.Version <= 0 {
				continue
			}
			var existingRevision model.ProjectProfileRevision
			if err := tx.Where("project_id = ? AND revision = ?", project.ID, profile.Version).First(&existingRevision).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return fmt.Errorf("load profile revision for project %s: %w", project.ID, err)
				}
				dimensions, err := json.Marshal(profile.Dimensions)
				if err != nil {
					return fmt.Errorf("encode profile revision for project %s: %w", project.ID, err)
				}
				revision := &model.ProjectProfileRevision{
					ID:        uuid.NewSHA1(uuid.Nil, []byte("project-profile-revision:"+project.ID+":"+fmt.Sprint(profile.Version))).String(),
					ProjectID: project.ID, Revision: profile.Version, SixDimensions: dimensions,
					SourceTaskID: profile.AnalysisTaskID, CreatedAt: project.UpdatedAt,
				}
				if err := tx.Create(revision).Error; err != nil {
					return fmt.Errorf("create profile revision for project %s: %w", project.ID, err)
				}
			}
		}
		if log != nil {
			log.Info().Int("projects", len(projects)).Msg("project profile read model migration completed")
		}
		return nil
	})
}
