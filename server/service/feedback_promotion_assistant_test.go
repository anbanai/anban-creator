package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

func TestAssistantReviewPromotionRequiresIndependentLaterAnalyticsRevision(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	if err := db.AutoMigrate(&model.Project{}, &model.ProjectProfileRevision{}, &model.ProjectProfileState{}, &model.AnalyticsState{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	profile := model.NewProjectProfile()
	profile.Status = model.ProfileStatusConfirmed
	profile.InitializationStatus = model.ProfileInitializationReady
	profile.Version = 4
	project := &model.Project{ID: uuid.NewString(), UserID: "user-1", Name: "test", Status: model.ProjectStatusActive, Profile: datatypes.NewJSONType(profile)}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsState{ProjectID: project.ID, Revision: 8, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	recommendations := map[string]feedbackDimensionRecommendation{
		"style": {Reason: "review hypothesis", Evidence: []string{"reviewer-confirmed observation"}, Updates: map[string]any{"opening_density": "lead with the conclusion"}},
	}
	analysis, err := json.Marshal(feedbackAnalysisArtifact{DimensionRecommendations: recommendations})
	if err != nil {
		t.Fatal(err)
	}
	candidate := &model.FeedbackInsight{
		ID: uuid.NewString(), UserID: "user-1", SourceSessionID: uuid.NewString(), JobID: uuid.NewString(), ExecutionID: uuid.NewString(),
		ProjectID: project.ID, TargetContentID: "content-1", AnalyticsRevision: 8, ProfileRevision: profile.Version,
		Trigger: assistantReviewTrigger, Kind: "content_postmortem", EvidenceJSON: string(analysis), Summary: "复盘候选",
		Confidence: "medium", Limitations: "等待后续数据验证", PromotionStatus: model.FeedbackPromotionConfirmed,
	}
	if err := repo.FeedbackLoop().CreateInsight(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	promotion := NewFeedbackPromotionService(repo, nil)
	if _, err := promotion.Validate(ctx, candidate.ID, "another-user"); !errors.Is(err, ErrFeedbackInsightNotFound) {
		t.Fatalf("Validate() with another user error = %v, want not found", err)
	}
	if _, err := promotion.Validate(ctx, candidate.ID, "user-1"); !errors.Is(err, ErrFeedbackInsightNotValidated) {
		t.Fatalf("Validate() before later analytics error = %v, want not validated", err)
	}

	job := &model.FeedbackJob{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: project.ID, Platform: model.PlatformWechat,
		Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly, PeriodStart: "2026-10-01", PeriodEnd: "2026-10-07",
		AnalyticsRevision: 9, TargetContentID: candidate.TargetContentID, ExecutionID: uuid.NewString(),
		Fingerprint: uuid.NewString(), Status: model.FeedbackJobSucceeded,
	}
	if _, err := repo.FeedbackLoop().CreateJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	independent := &model.FeedbackInsight{
		ID: uuid.NewString(), JobID: job.ID, ExecutionID: job.ExecutionID, ProjectID: project.ID,
		TargetContentID: candidate.TargetContentID, AnalyticsRevision: job.AnalyticsRevision, Kind: "content_postmortem",
		PromotionStatus: model.FeedbackPromotionCandidate,
	}
	if err := repo.FeedbackLoop().CreateInsight(ctx, independent); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AnalyticsState{}).Where("project_id = ?", project.ID).Update("revision", 9).Error; err != nil {
		t.Fatal(err)
	}

	validated, err := promotion.Validate(ctx, candidate.ID, "user-1")
	if err != nil {
		t.Fatalf("Validate() after independent later revision error = %v", err)
	}
	if validated.PromotionStatus != model.FeedbackPromotionPromoted || validated.ValidatedAt == nil {
		t.Fatalf("validated insight = %#v, want promoted and timestamped", validated)
	}
	updatedProject, err := repo.Projects().FindByID(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	updatedProfile := projectProfileFor(updatedProject)
	if updatedProfile.Version != 5 || updatedProfile.Dimensions.Style.Content["opening_density"] != "lead with the conclusion" {
		t.Fatalf("updated profile = %#v, want version 5 with confirmed recommendation", updatedProfile)
	}
	var revisions int64
	if err := db.Model(&model.ProjectProfileRevision{}).Where("project_id = ? AND revision = ?", project.ID, 5).Count(&revisions).Error; err != nil {
		t.Fatal(err)
	}
	if revisions != 1 {
		t.Fatalf("profile revision count = %d, want 1", revisions)
	}
}

func TestAssistantReviewPromotionRejectsAnalyticsInsightWithoutMatchingSuccessfulJob(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	if err := db.AutoMigrate(&model.Project{}, &model.AnalyticsState{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	project := &model.Project{ID: uuid.NewString(), UserID: "user-1", Name: "test", Status: model.ProjectStatusActive, Profile: datatypes.NewJSONType(model.NewProjectProfile())}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.AnalyticsState{ProjectID: project.ID, Revision: 12, ActiveGeneration: 1, Status: "ready"}).Error; err != nil {
		t.Fatal(err)
	}
	candidate := &model.FeedbackInsight{
		ID: uuid.NewString(), UserID: "user-1", SourceSessionID: uuid.NewString(), JobID: uuid.NewString(), ExecutionID: uuid.NewString(),
		ProjectID: project.ID, TargetContentID: "content-1", AnalyticsRevision: 10, ProfileRevision: 1,
		Trigger: assistantReviewTrigger, Kind: "content_postmortem", EvidenceJSON: `{"analysis":{"dimension_recommendations":{"style":{"reason":"x","evidence":["x"],"updates":{"voice":"x"}}}}}`,
		Summary: "review", PromotionStatus: model.FeedbackPromotionConfirmed,
	}
	if err := repo.FeedbackLoop().CreateInsight(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	// A newer insight row alone is insufficient proof. It must come from a
	// successful, independent FeedbackJob with the same frozen revision.
	decoy := &model.FeedbackInsight{
		ID: uuid.NewString(), JobID: uuid.NewString(), ProjectID: project.ID, TargetContentID: candidate.TargetContentID,
		AnalyticsRevision: 11, ExecutionID: uuid.NewString(), Kind: "content_postmortem", PromotionStatus: model.FeedbackPromotionCandidate,
	}
	if err := repo.FeedbackLoop().CreateInsight(ctx, decoy); err != nil {
		t.Fatal(err)
	}
	_, err := NewFeedbackPromotionService(repo, nil).Validate(ctx, candidate.ID, "user-1")
	if !errors.Is(err, ErrFeedbackInsightNotValidated) {
		t.Fatalf("Validate() with unbacked later insight error = %v, want not validated", err)
	}
}
