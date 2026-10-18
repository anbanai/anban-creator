package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

func TestApplyFeedbackProfileRecommendationsCreatesVersionedRevision(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}); err != nil {
		t.Fatal(err)
	}
	profile := model.NewProjectProfile()
	profile.Status = model.ProfileStatusConfirmed
	profile.InitializationStatus = model.ProfileInitializationReady
	profile.Version = 4
	project := &model.Project{ID: uuid.NewString(), UserID: "user-1", Name: "test", Status: model.ProjectStatusActive, Profile: datatypes.NewJSONType(profile)}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	recommendations := map[string]feedbackDimensionRecommendation{"style": {Reason: "high relative performance", Evidence: []string{"baseline p95"}, Updates: map[string]any{"opening": "specific contrast"}}}
	payload, err := json.Marshal(feedbackAnalysisArtifact{DimensionRecommendations: recommendations})
	if err != nil {
		t.Fatal(err)
	}
	job := &model.FeedbackJob{ID: uuid.NewString(), ProjectID: project.ID, ProfileRevision: 4}
	insight := &model.FeedbackInsight{ID: uuid.NewString(), JobID: job.ID, ProjectID: project.ID, AnalyticsRevision: 12, Trigger: FeedbackTriggerAutoAttribution, EvidenceJSON: string(payload)}
	service := NewFeedbackPromotionService(repo, nil)
	if err := repo.WithTx(context.Background(), func(tx repository.Repository) error {
		return service.applyProfileRecommendationsTx(context.Background(), tx, insight, job)
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Projects().FindByID(context.Background(), project.ID)
	if err != nil {
		t.Fatal(err)
	}
	updated := projectProfileFor(stored)
	if updated.Version != 5 || updated.Dimensions.Style.Content["opening"] != "specific contrast" {
		t.Fatalf("updated profile = %#v", updated)
	}
	var revisions int64
	if err := db.Model(&model.ProjectProfileRevision{}).Where("project_id = ? AND revision = ?", project.ID, 5).Count(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if revisions != 1 {
		t.Fatalf("profile revision count = %d, want 1", revisions)
	}
}

func TestApplyFeedbackProfileRecommendationsRejectsStaleProfileRevision(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}); err != nil {
		t.Fatal(err)
	}
	profile := model.NewProjectProfile()
	profile.Status = model.ProfileStatusConfirmed
	profile.InitializationStatus = model.ProfileInitializationReady
	profile.Version = 5
	project := &model.Project{ID: uuid.NewString(), UserID: "user-1", Name: "test", Status: model.ProjectStatusActive, Profile: datatypes.NewJSONType(profile)}
	if err := db.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(feedbackAnalysisArtifact{DimensionRecommendations: map[string]feedbackDimensionRecommendation{
		"style": {Reason: "observed pattern", Evidence: []string{"baseline"}, Updates: map[string]any{"opening": "contrast"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	job := &model.FeedbackJob{ID: uuid.NewString(), ProjectID: project.ID, ProfileRevision: 4}
	insight := &model.FeedbackInsight{ID: uuid.NewString(), JobID: job.ID, ProjectID: project.ID, Trigger: FeedbackTriggerAutoAttribution, EvidenceJSON: string(payload)}
	service := NewFeedbackPromotionService(repo, nil)
	err = repo.WithTx(context.Background(), func(tx repository.Repository) error {
		return service.applyProfileRecommendationsTx(context.Background(), tx, insight, job)
	})
	if err != ErrProjectProfileVersionConflict {
		t.Fatalf("apply stale recommendation error = %v, want profile version conflict", err)
	}
	var revisions int64
	if err := db.Model(&model.ProjectProfileRevision{}).Where("project_id = ?", project.ID).Count(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if revisions != 0 {
		t.Fatalf("stale recommendation created %d revisions, want 0", revisions)
	}
}
