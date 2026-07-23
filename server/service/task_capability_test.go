package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTaskServiceListTitlesForUserChecksProjectOwnership(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	projectID := createTestProject(t, repo, ownerID, model.PlatformArticle)

	if _, err := svc.ListTitlesForUser(ctx, uuid.NewString(), projectID); err == nil {
		t.Fatal("ListTitlesForUser accepted a non-owner")
	}
	if _, err := svc.ListTitlesForUser(ctx, ownerID, projectID); err != nil {
		t.Fatalf("ListTitlesForUser owner: %v", err)
	}
}

func TestTaskServiceGetVisibleFilesForUserChecksTaskOwnership(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	projectID := createTestProject(t, repo, ownerID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID: ownerID, ProjectID: projectID, Quantity: 1, Prompt: "topic",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}

	if _, err := svc.GetVisibleFilesForUser(ctx, uuid.NewString(), tasks[0].ID); err == nil {
		t.Fatal("GetVisibleFilesForUser accepted a non-owner")
	}
	files, err := svc.GetVisibleFilesForUser(ctx, ownerID, tasks[0].ID)
	if err != nil {
		t.Fatalf("GetVisibleFilesForUser owner: %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("files = %d, want 0", len(files))
	}
}
