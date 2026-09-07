package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestContentMetadataServiceSubmitIsIdempotentPerExecution(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repository.New(db), nil)
	input := ContentMetadataInput{TaskID: "local-task-1", ExecutionID: "exec-1", TaxonomyVersion: model.ContentTaxonomyVersion, RawMetadata: []byte(`{"task_type":"article","tags":[{"dimension":"industry","value":"茶叶","confidence":0.9}],"feedback":{"scores":{"quality":8},"summary":"ok"}}`)}
	first, err := svc.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("ids differ: %q %q", first.ID, second.ID)
	}
	var count int64
	if err := db.Model(&model.ContentMetadataReport{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("reports = %d, want 1", count)
	}
	var feedback model.AgentFeedback
	if err := db.First(&feedback, "task_id = ? AND agent_name = ?", "local-task-1", "article").Error; err != nil {
		t.Fatal(err)
	}
	if feedback.Source != "hook" || feedback.ExecutionID != "exec-1" {
		t.Fatalf("feedback provenance = %#v", feedback)
	}
}

func TestContentMetadataServiceRejectsUnknownDimension(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service-invalid?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repository.New(db), nil)
	_, err = svc.Submit(context.Background(), ContentMetadataInput{TaskID: "local-task-1", ExecutionID: "exec-1", RawMetadata: []byte(`{"tags":[{"dimension":"unknown","value":"x"}]}`)})
	if err == nil {
		t.Fatal("expected unknown dimension error")
	}
}

func TestContentMetadataServiceNormalizesVocabularyAliasAndPersistsDisplayName(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service-vocab?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	aliasJSON := []byte(`["茶饮"]`)
	if err := db.Create(&model.ContentTagVocabulary{ID: "vocab-1", Dimension: "industry", Value: "tea", DisplayName: "茶叶", Status: model.ContentTagCanonical, Aliases: aliasJSON, TaxonomyVersion: model.ContentTaxonomyVersion}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repository.New(db), nil)
	report, err := svc.Submit(context.Background(), ContentMetadataInput{TaskID: "local-task-vocab", ExecutionID: "exec-vocab", RawMetadata: []byte(`{"tags":[{"dimension":"industry","value":"茶饮","confidence":0.9}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var tag model.ContentTagAssignment
	if err := db.First(&tag, "report_id = ?", report.ID).Error; err != nil {
		t.Fatal(err)
	}
	if tag.CanonicalValue != "tea" || tag.DisplayName != "茶叶" || tag.LabelStatus != model.ContentTagCanonical {
		t.Fatalf("normalized tag = %#v", tag)
	}
}

func TestContentMetadataServiceRejectsForeignAuthenticatedUser(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:content-metadata-service-auth?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}, &model.ContentMetadataReport{}, &model.ContentTagAssignment{}, &model.ContentTagVocabulary{}, &model.AgentFeedback{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	if err := repo.Tasks().Create(context.Background(), &model.Task{ID: "task-owned", UserID: "owner-1", ProjectID: "project-1", Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(context.Background(), &model.TaskExecution{ID: "exec-owned", TaskID: "task-owned", Attempt: 1, Status: model.TaskExecutionSucceeded}); err != nil {
		t.Fatal(err)
	}
	svc := NewContentMetadataService(repo, nil)
	_, err = svc.Submit(context.Background(), ContentMetadataInput{AuthenticatedUserID: "owner-2", TaskID: "task-owned", ExecutionID: "exec-owned", RawMetadata: []byte(`{"tags":[]}`)})
	if err == nil {
		t.Fatal("expected foreign user rejection")
	}
}
