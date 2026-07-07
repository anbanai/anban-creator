package handler

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func TestProjectRequestMapsRequirePublishApproval(t *testing.T) {
	req := projectRequest{
		Platform:               "article",
		Name:                   "Article",
		EnablePublishing:       true,
		RequirePublishApproval: true,
	}

	project := req.toProject()
	if !project.Config.EnablePublishing {
		t.Fatal("EnablePublishing was not mapped")
	}
	if !project.Config.RequirePublishApproval {
		t.Fatal("RequirePublishApproval was not mapped")
	}
	if got := req.getFieldValue("require_publish_approval"); got != "true" {
		t.Fatalf("getFieldValue(require_publish_approval) = %q, want true", got)
	}
}

func setupProjectDeleteHandlerTest(t *testing.T) (*fiber.App, repository.Repository) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "projects.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	projectSvc := service.NewProjectService(repo, &logger)
	h := NewProjectHandler(projectSvc, &logger)
	h.SetPendingUploadRepository(repo.PendingUploads())

	app := fiber.New()
	injectUser := func(c fiber.Ctx) error {
		if uid := c.Get("X-User-ID"); uid != "" {
			c.Locals("user_id", uid)
		}
		return c.Next()
	}
	app.Delete("/api/v1/projects/:id", injectUser, h.Delete)
	app.Post("/api/v1/projects", injectUser, h.Create)
	app.Put("/api/v1/projects/:id", injectUser, h.Update)
	return app, repo
}

func TestProjectHandler_CreateFinalizesPendingAvatarAndReference(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	avatarID := "avatar-upload"
	refID := "reference-upload"
	avatarURL := "https://cdn.example.com/uploads/pending/" + userID + "/" + avatarID + "/avatar.png"
	refURL := "https://cdn.example.com/uploads/pending/" + userID + "/" + refID + "/ref.png"

	for _, upload := range []*model.PendingUpload{
		{
			ID:          avatarID,
			UserID:      userID,
			Purpose:     service.DirectUploadPurposeProjectReference,
			Key:         "uploads/pending/" + userID + "/" + avatarID + "/avatar.png",
			PublicURL:   avatarURL,
			FileName:    "avatar.png",
			ContentType: "image/png",
			Size:        123,
			Status:      model.PendingUploadStatusPending,
			ExpiresAt:   time.Now().Add(time.Hour),
		},
		{
			ID:          refID,
			UserID:      userID,
			Purpose:     service.DirectUploadPurposeProjectReference,
			Key:         "uploads/pending/" + userID + "/" + refID + "/ref.png",
			PublicURL:   refURL,
			FileName:    "ref.png",
			ContentType: "image/png",
			Size:        456,
			Status:      model.PendingUploadStatusPending,
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	} {
		if err := repo.PendingUploads().CreatePendingUpload(ctx, upload); err != nil {
			t.Fatalf("seed pending upload %s: %v", upload.ID, err)
		}
	}

	resp := doRequest(t, app, "POST", "/api/v1/projects", userID, map[string]any{
		"platform":            model.PlatformArticle,
		"name":                "公众号项目",
		"avatar_url":          avatarURL,
		"reference_image_url": refURL,
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}

	for _, id := range []string{avatarID, refID} {
		upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, id)
		if err != nil {
			t.Fatalf("find pending upload %s: %v", id, err)
		}
		if upload.Status != model.PendingUploadStatusFinalized {
			t.Fatalf("upload %s status = %q, want finalized", id, upload.Status)
		}
	}
}

func TestProjectHandler_UpdateFinalizesPendingAvatar(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := "updated-avatar-upload"
	key := "uploads/pending/" + userID + "/" + uploadID + "/avatar.png"
	publicURL := "https://cdn.example.com/" + key

	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "公众号项目",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := repo.PendingUploads().CreatePendingUpload(ctx, &model.PendingUpload{
		ID:          uploadID,
		UserID:      userID,
		Purpose:     service.DirectUploadPurposeProjectReference,
		Key:         key,
		PublicURL:   publicURL,
		FileName:    "avatar.png",
		ContentType: "image/png",
		Size:        123,
		Status:      model.PendingUploadStatusPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed pending upload: %v", err)
	}

	resp := doRequest(t, app, "PUT", "/api/v1/projects/"+projectID, userID, map[string]any{
		"avatar_url": publicURL,
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}

	upload, err := repo.PendingUploads().FindPendingUploadByID(ctx, uploadID)
	if err != nil {
		t.Fatalf("find pending upload: %v", err)
	}
	if upload.Status != model.PendingUploadStatusFinalized {
		t.Fatalf("pending upload status = %q, want finalized", upload.Status)
	}
}

func TestProjectHandler_DeleteWithAssociatedTasksReturnsConflict(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()

	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	resp := doRequest(t, app, "DELETE", "/api/v1/projects/"+projectID, userID, nil)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}

	body := decodeBody(t, resp)
	msg, _ := body["msg"].(string)
	if !strings.Contains(msg, "cannot delete project with 1 associated tasks") ||
		!strings.Contains(msg, "archive it instead") {
		t.Fatalf("msg = %q, want associated task archive guidance", msg)
	}
}

func TestProjectHandler_DeleteWithAssociatedPlansReturnsConflict(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()

	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Plans().Create(ctx, &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Title:     "Daily ideas",
		Status:    model.PlanStatusActive,
	}); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	resp := doRequest(t, app, "DELETE", "/api/v1/projects/"+projectID, userID, nil)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}

	body := decodeBody(t, resp)
	msg, _ := body["msg"].(string)
	if !strings.Contains(msg, "cannot delete project with 1 associated plans") ||
		!strings.Contains(msg, "archive it instead") {
		t.Fatalf("msg = %q, want associated plan archive guidance", msg)
	}
}
