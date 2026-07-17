package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
)

type projectReadiness bool

func (r projectReadiness) Ready() bool { return bool(r) }

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

func TestProjectRequestMapsMontageDefaults(t *testing.T) {
	req := projectRequest{
		Platform: model.PlatformMontage,
		Name:     "Montage",
		MontageDefaults: &model.MontageDefaults{
			DefaultPipeline: "social-short",
			Preferences: model.MontagePreferences{
				AspectRatio:     "9:16",
				DurationSeconds: 45,
				Style:           "documentary",
				MusicPrompt:     "minimal electronic",
				SubtitleMode:    "burned-in",
				VoiceoverMode:   "narrated",
			},
			AssetGuidance:   "prefer source footage",
			DeliveryTargets: []string{"final_video", "subtitles"},
		},
	}

	project := req.toProject()
	if !project.MontageDefaultsSet {
		t.Fatal("MontageDefaultsSet = false")
	}
	got := project.MontageDefaults.Data()
	if got.DefaultPipeline != "social-short" || got.Preferences.MusicPrompt != "minimal electronic" || got.AssetGuidance != "prefer source footage" {
		t.Fatalf("MontageDefaults = %#v", got)
	}
	if len(got.DeliveryTargets) != 2 || got.DeliveryTargets[1] != "subtitles" {
		t.Fatalf("DeliveryTargets = %#v", got.DeliveryTargets)
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

func TestProjectHandler_CreateAndUpdateMontageDefaults(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.NewString()

	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
		"platform": model.PlatformMontage,
		"name":     "Launch montage",
		"montage_defaults": map[string]any{
			"default_pipeline": "social-short",
			"preferences": map[string]any{
				"aspect_ratio":     "9:16",
				"duration_seconds": 45,
				"style":            "documentary",
				"music_prompt":     "minimal electronic",
				"subtitle_mode":    "burned-in",
				"voiceover_mode":   "narrated",
			},
			"asset_guidance":   "prefer source footage",
			"delivery_targets": []string{"final_video", "subtitles"},
		},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("create status = %d, body=%#v", resp.StatusCode, decodeBody(t, resp))
	}
	createData := decodeBody(t, resp)["data"].(map[string]any)
	createdProject := createData["project"].(map[string]any)
	projectID := createdProject["id"].(string)
	stored, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatalf("FindByID after create: %v", err)
	}
	defaults := stored.MontageDefaults.Data()
	if defaults.DefaultPipeline != "social-short" || defaults.Preferences.DurationSeconds != 45 || defaults.Preferences.VoiceoverMode != "narrated" {
		t.Fatalf("created MontageDefaults = %#v", defaults)
	}

	resp = doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{
		"platform": model.PlatformMontage,
		"montage_defaults": map[string]any{
			"default_pipeline": "product-demo",
			"preferences": map[string]any{
				"aspect_ratio":     "16:9",
				"duration_seconds": 30,
			},
			"delivery_targets": []string{"final_video"},
		},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("update status = %d, body=%#v", resp.StatusCode, decodeBody(t, resp))
	}
	stored, err = repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatalf("FindByID after update: %v", err)
	}
	defaults = stored.MontageDefaults.Data()
	if defaults.DefaultPipeline != "product-demo" || defaults.Preferences.AspectRatio != "16:9" || defaults.Preferences.DurationSeconds != 30 {
		t.Fatalf("updated MontageDefaults = %#v", defaults)
	}
}

func TestProjectHandler_RejectsMontageDefaultsForArticle(t *testing.T) {
	app, _ := setupProjectDeleteHandlerTest(t)
	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", uuid.NewString(), map[string]any{
		"platform": model.PlatformArticle,
		"name":     "Article",
		"montage_defaults": map[string]any{
			"default_pipeline": "social-short",
		},
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%#v", resp.StatusCode, decodeBody(t, resp))
	}
}

func TestProjectHandler_SeednoteLoginStatusUnavailableWhenSidecarNotReady(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewProjectHandler(nil, &logger)
	h.SetSeednoteClient(seednote.NewClient("http://127.0.0.1:1", time.Second))
	h.SetSeednoteReadiness(projectReadiness(false))

	app := fiber.New()
	app.Get("/seednote/login-status", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.SeednoteLoginStatus(c)
	})

	req := httptest.NewRequest("GET", "/seednote/login-status", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var payload struct {
		Data struct {
			Available bool   `json:"available"`
			LoggedIn  bool   `json:"logged_in"`
			Message   string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response %q: %v", string(body), err)
	}
	if payload.Data.Available || payload.Data.LoggedIn || !strings.Contains(payload.Data.Message, "后台连接") {
		t.Fatalf("unexpected response: %s", string(body))
	}
}

func TestProjectHandler_FetchProfileReturnsUnavailableWhenSeednoteNotReady(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewProjectHandler(nil, &logger)
	h.SetSeednoteClient(seednote.NewClient("http://127.0.0.1:1", time.Second))
	h.SetSeednoteReadiness(projectReadiness(false))
	if h.seednoteReady == nil || h.seednoteReady.Ready() {
		t.Fatalf("seednote readiness not installed or unexpectedly ready: %#v", h.seednoteReady)
	}

	app := fiber.New()
	app.Post("/projects/fetch-profile", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.FetchProfile(c)
	})

	body := strings.NewReader(`{"platform":"seednote","profile_url":"https://www.xiaohongshu.com/user/profile/abc?xsec_token=test"}`)
	req := httptest.NewRequest(http.MethodPost, "/projects/fetch-profile", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", resp.StatusCode, string(got))
	}
	if !strings.Contains(string(got), "后台连接") {
		t.Fatalf("body = %s, want background connection hint", string(got))
	}
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
