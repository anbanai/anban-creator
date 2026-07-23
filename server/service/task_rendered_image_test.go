package service

import (
	"context"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestTaskServiceRegisterRenderedImageOwnsValidationAndRegistration(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &fakeTaskStorage{name: "oss"}
	svc := NewTaskService(repo, nil, &mockEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: model.PlatformArticle, Status: model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}

	result, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: task.ID,
		Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(png),
	})
	if err != nil {
		t.Fatalf("RegisterRenderedImage: %v", err)
	}
	if result.TaskFileID == "" || result.Name != "cover.png" || result.Role != model.FileRoleCover || result.MimeType != "image/png" {
		t.Fatalf("result = %#v", result)
	}
	if _, err := svc.RegisterRenderedImage(ctx, RegisterRenderedImageRequest{
		UserID: uuid.NewString(), ProjectID: projectID, TaskID: task.ID,
		Name: "foreign.png", ImageBase64: base64.StdEncoding.EncodeToString(png),
	}); err == nil {
		t.Fatal("RegisterRenderedImage accepted a foreign user")
	}
}

func TestRenderedTaskImageInputValidation(t *testing.T) {
	textPayload := base64.StdEncoding.EncodeToString([]byte("this is text, not an image"))
	data, mimeType, err := loadRenderedTaskImage(RegisterRenderedImageRequest{ImageBase64: textPayload})
	if err != nil {
		t.Fatalf("load text payload: %v", err)
	}
	if err := validateRenderedTaskImage("not-image.png", mimeType); err == nil || !strings.Contains(err.Error(), "unsupported image MIME") {
		t.Fatalf("invalid MIME error = %v, data size = %d", err, len(data))
	}

	tooLarge := make([]byte, maxRenderedTaskImageBytes+1)
	if _, _, err := loadRenderedTaskImage(RegisterRenderedImageRequest{
		ImageBase64: base64.StdEncoding.EncodeToString(tooLarge),
	}); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversize error = %v", err)
	}
	if _, _, err := loadRenderedTaskImage(RegisterRenderedImageRequest{FilePath: "../secret.png"}); err == nil || !strings.Contains(err.Error(), "absolute server-local path") {
		t.Fatalf("unsafe path error = %v", err)
	}
}
