package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	projectmemory "github.com/anbanai/anban-creator/server/memory"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
)

type projectReadiness bool

func (r projectReadiness) Ready() bool { return bool(r) }

type projectMemoryDeleteFake struct {
	err error
}

func (f projectMemoryDeleteFake) DeleteProject(context.Context, string) error {
	return f.err
}

func TestProjectRequestMapsAgentConfig(t *testing.T) {
	req := projectRequest{Platform: "article", Name: "Article", AgentConfig: map[string]any{}, AgentConfigSet: true}
	project := req.toProject()
	if !project.AgentConfigSet || project.AgentConfig.Data() == nil {
		t.Fatalf("AgentConfig = %#v set=%v", project.AgentConfig.Data(), project.AgentConfigSet)
	}
}

func TestProjectHandlerRejectsInvalidAgentConfigWithStableBadRequest(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	userID := uuid.NewString()

	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
		"platform": model.PlatformArticle,
		"name":     "Article",
		"agent_config": map[string]any{
			"unexpected": true,
		},
	})
	body := decodeBody(t, resp)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body["msg"].(string), "invalid_agent_config") {
		t.Fatalf("create status/body = %d/%#v, want 400 invalid_agent_config", resp.StatusCode, body)
	}

	project := &model.Project{
		ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle,
		Name: "Article", Status: model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, app, http.MethodPut, "/api/v1/projects/"+project.ID, userID, map[string]any{
		"agent_config": map[string]any{"unexpected": true},
	})
	body = decodeBody(t, resp)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body["msg"].(string), "invalid_agent_config") {
		t.Fatalf("update status/body = %d/%#v, want 400 invalid_agent_config", resp.StatusCode, body)
	}
}

func TestProjectHandlerRejectsLegacyWechatPublishMode(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	userID := uuid.NewString()

	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
		"platform":            model.PlatformArticle,
		"name":                "Article",
		"wechat_publish_mode": "manual",
	})
	body := decodeBody(t, resp)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body["msg"].(string), "wechat_publish_mode is no longer supported") {
		t.Fatalf("create status/body = %d/%#v, want 400 legacy field rejection", resp.StatusCode, body)
	}

	project := &model.Project{
		ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle,
		Name: "Article", Status: model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, app, http.MethodPut, "/api/v1/projects/"+project.ID, userID, map[string]any{
		"wechat_publish_mode": "api_confirmed",
	})
	body = decodeBody(t, resp)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(body["msg"].(string), "wechat_publish_mode is no longer supported") {
		t.Fatalf("update status/body = %d/%#v, want 400 legacy field rejection", resp.StatusCode, body)
	}
}

func TestProjectRequestMapsMontageDefaults(t *testing.T) {
	req := projectRequest{
		Platform: model.PlatformMontage,
		Name:     "Montage",
		MontageDefaults: &model.MontageDefaults{
			DefaultPipeline: "social-short",
			Preferences: model.MontagePreferences{
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

type projectReferenceStore struct {
	*fakeStorageProvider
	signedKeys  []string
	downloadErr error
}

type projectHandlerRepositoryOverride struct {
	repository.Repository
	projects repository.ProjectRepository
	txCalls  int
	beforeTx func(int)
}

func (r *projectHandlerRepositoryOverride) Projects() repository.ProjectRepository {
	return r.projects
}

func (r *projectHandlerRepositoryOverride) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	r.txCalls++
	if r.beforeTx != nil {
		r.beforeTx(r.txCalls)
	}
	return r.Repository.WithTx(ctx, fn)
}

func (s *projectReferenceStore) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	s.signedKeys = append(s.signedKeys, key)
	if s.downloadErr != nil {
		return "", s.downloadErr
	}
	return "https://signed.example/" + key, nil
}

func setupProjectHandlerTest(t *testing.T) (*fiber.App, repository.Repository, *projectReferenceStore) {
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
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(repo.UploadSessions())}
	return newProjectHandlerTestApp(repo, store), repo, store
}

func newProjectHandlerTestApp(repo repository.Repository, store *projectReferenceStore) *fiber.App {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	projectSvc := service.NewProjectService(repo, &logger)
	h := NewProjectHandler(projectSvc, &logger)
	h.SetTemplateService(service.NewTemplateService(repo, &logger))
	h.SetStore(store)
	h.SetUploadRepository(repo)
	h.SetReferenceAssetService(service.NewReferenceAssetService(repo, store, time.Now))

	app := fiber.New()
	injectUser := func(c fiber.Ctx) error {
		if uid := c.Get("X-User-ID"); uid != "" {
			c.Locals("user_id", uid)
			c.Locals("user", &model.User{ID: uid, IsAdmin: c.Get("X-Admin") != "false"})
		}
		return c.Next()
	}
	app.Delete("/api/v1/projects/:id", injectUser, h.Delete)
	app.Get("/api/v1/projects/platform-configs", injectUser, h.GetPlatformConfigs)
	app.Get("/api/v1/projects", injectUser, h.List)
	app.Get("/api/v1/projects/:id", injectUser, h.Get)
	app.Post("/api/v1/projects", injectUser, h.Create)
	app.Put("/api/v1/projects/:id", injectUser, h.Update)
	return app
}

func doProjectRequestAsNonAdmin(t *testing.T, app *fiber.App, method, path, userID string, body any) *http.Response {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = strings.NewReader(string(raw))
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", userID)
	req.Header.Set("X-Admin", "false")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func TestProjectHandlerAdminOnlyPlatforms(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	projectIDs := make(map[string]string)
	for _, platform := range []string{model.PlatformArticle, model.PlatformSeednote, model.PlatformMoments, model.PlatformEcommerce, model.PlatformMontage, model.PlatformHypit} {
		projectID := uuid.NewString()
		if err := repo.Projects().Create(t.Context(), &model.Project{
			ID: projectID, UserID: userID, Platform: platform,
			Name: platform, Status: model.ProjectStatusActive,
		}); err != nil {
			t.Fatalf("create %s project: %v", platform, err)
		}
		projectIDs[platform] = projectID
	}

	resp := doProjectRequestAsNonAdmin(t, app, http.MethodGet, "/api/v1/projects", userID, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	items := decodeBody(t, resp)["data"].([]any)
	if len(items) != 3 {
		t.Fatalf("non-admin projects = %#v, want article, seednote, and montage", items)
	}
	for _, item := range items {
		platform := item.(map[string]any)["platform"].(string)
		if model.IsAdminOnlyProjectPlatform(platform) {
			t.Fatalf("non-admin list exposed %q", platform)
		}
	}

	for _, platform := range []string{model.PlatformMoments, model.PlatformEcommerce} {
		t.Run("reject create "+platform, func(t *testing.T) {
			resp := doProjectRequestAsNonAdmin(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
				"platform": platform,
				"name":     "Hidden " + platform,
			})
			if resp.StatusCode != fiber.StatusForbidden {
				t.Fatalf("non-admin create status = %d, want 403; body=%#v", resp.StatusCode, decodeBody(t, resp))
			}
		})
	}

	resp = doProjectRequestAsNonAdmin(t, app, http.MethodGet, "/api/v1/projects/"+projectIDs[model.PlatformMontage], userID, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("non-admin get montage project status = %d, want 200", resp.StatusCode)
	}

	resp = doProjectRequestAsNonAdmin(t, app, http.MethodPut, "/api/v1/projects/"+projectIDs[model.PlatformArticle], userID, map[string]any{
		"platform": model.PlatformMontage,
		"name":     "Public montage",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("non-admin update to montage status = %d, want 200; body=%#v", resp.StatusCode, decodeBody(t, resp))
	}

	resp = doProjectRequestAsNonAdmin(t, app, http.MethodGet, "/api/v1/projects/platform-configs", userID, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("non-admin platform configs status = %d, want 200", resp.StatusCode)
	}
	configs := decodeBody(t, resp)["data"].([]any)
	if len(configs) != 3 {
		t.Fatalf("non-admin platform configs = %#v, want article, seednote, and montage", configs)
	}
	for _, item := range configs {
		platform := item.(map[string]any)["id"].(string)
		if model.IsAdminOnlyProjectPlatform(platform) {
			t.Fatalf("non-admin platform configs exposed %q", platform)
		}
	}

	resp = doRequest(t, app, http.MethodGet, "/api/v1/projects/platform-configs", userID, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("admin platform configs status = %d, want 200", resp.StatusCode)
	}
	if configs := decodeBody(t, resp)["data"].([]any); len(configs) != 6 {
		t.Fatalf("admin platform configs = %#v, want all six configured platforms", configs)
	}
}

func TestProjectHandlerCreateReturnsCanonicalRecommendedTemplates(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	asset := createTemplateThumbnailAsset(t, repo, "admin-secret")
	tmpl := &model.Template{
		ID: uuid.NewString(), UserID: "admin-secret", Type: model.TemplateTypeSeednote,
		Name: "清透说明书", Category: model.SeednoteTemplateCategoryBeauty,
		ThumbnailAssetID: asset.ID, Prompt: "清透版式", PromptSource: model.ImageAnalysisSourceManual,
		ReadinessStatus: model.TemplateReadinessReady, Visibility: "public", IsActive: true,
		Writer: "must-not-leak", Tags: []string{"must-not-leak"},
	}
	if err := repo.Templates().Create(t.Context(), tmpl); err != nil {
		t.Fatalf("create recommended template: %v", err)
	}

	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", uuid.NewString(), map[string]any{
		"platform": "seednote",
		"name":     "美妆账号",
		"keywords": model.SeednoteTemplateCategoryBeauty,
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("create project status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	items := decodeBody(t, resp)["data"].(map[string]any)["recommended_templates"].([]any)
	if len(items) != 1 {
		t.Fatalf("recommended templates=%v, want one", items)
	}
	assertCanonicalTemplateResponse(t, items[0].(map[string]any))
}

func setupProjectDeleteHandlerTest(t *testing.T) (*fiber.App, repository.Repository) {
	t.Helper()
	app, repo, _ := setupProjectHandlerTest(t)
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
	if stored.ImageRatio != "9:16" {
		t.Fatalf("created Montage image ratio = %q, want 9:16", stored.ImageRatio)
	}

	resp = doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{
		"platform":    model.PlatformMontage,
		"image_ratio": "16:9",
		"montage_defaults": map[string]any{
			"default_pipeline": "product-demo",
			"preferences": map[string]any{
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
	if defaults.DefaultPipeline != "product-demo" || defaults.Preferences.DurationSeconds != 30 {
		t.Fatalf("updated MontageDefaults = %#v", defaults)
	}
	if stored.ImageRatio != "16:9" {
		t.Fatalf("updated Montage image ratio = %q, want 16:9", stored.ImageRatio)
	}
}

func TestProjectHandler_AcceptsImageRatioForMontage(t *testing.T) {
	app, _ := setupProjectDeleteHandlerTest(t)
	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", uuid.NewString(), map[string]any{
		"platform":    model.PlatformMontage,
		"name":        "Launch montage",
		"image_ratio": "9:16",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%#v", resp.StatusCode, decodeBody(t, resp))
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

func newSeednoteAdminTestApp(h *ProjectHandler) *fiber.App {
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		if userID := c.Get("X-User-ID"); userID != "" {
			c.Locals("user_id", userID)
			c.Locals("user", &model.User{ID: userID, IsAdmin: c.Get("X-Admin") == "true"})
		}
		return c.Next()
	})
	app.Get("/seednote/account/login-status", h.AdminSeednoteLoginStatus)
	app.Get("/seednote/account/login-qrcode", h.AdminSeednoteLoginQRCode)
	app.Delete("/seednote/account/login", h.AdminSeednoteLogout)
	return app
}

func TestProjectHandler_SeednoteAdministrationRequiresAdmin(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewProjectHandler(nil, &logger)
	h.SetSeednoteClient(seednote.NewClient("http://127.0.0.1:1", time.Second))
	app := newSeednoteAdminTestApp(h)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/seednote/account/login-status"},
		{http.MethodGet, "/seednote/account/login-qrcode"},
		{http.MethodDelete, "/seednote/account/login"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("X-User-ID", "user-1")
		req.Header.Set("X-Admin", "false")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		if resp.StatusCode != fiber.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403", tc.method, tc.path, resp.StatusCode)
		}
		_ = resp.Body.Close()
	}
}

func TestProjectHandler_AdminSeednoteLoginStatusUnavailableWhenSidecarNotReady(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewProjectHandler(nil, &logger)
	h.SetSeednoteClient(seednote.NewClient("http://127.0.0.1:1", time.Second))
	h.SetSeednoteReadiness(projectReadiness(false))
	app := newSeednoteAdminTestApp(h)

	req := httptest.NewRequest(http.MethodGet, "/seednote/account/login-status", nil)
	req.Header.Set("X-User-ID", "admin-1")
	req.Header.Set("X-Admin", "true")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	payload := decodeBody(t, resp)["data"].(map[string]any)
	if payload["available"] != false || payload["logged_in"] != false || !strings.Contains(payload["message"].(string), "后台连接") {
		t.Fatalf("unexpected response: %#v", payload)
	}
}

func TestProjectHandler_AdminSeednoteLoginOperations(t *testing.T) {
	var logoutCalled bool
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/login/status":
			_, _ = w.Write([]byte(`{"success":true,"logged_in":false}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/login/qrcode":
			_, _ = w.Write([]byte(`{"success":true,"data":{"qrcode_image":"cG5n"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/login/cookies":
			logoutCalled = true
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(sidecar.Close)

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewProjectHandler(nil, &logger)
	h.SetSeednoteClient(seednote.NewClient(sidecar.URL, time.Second))
	h.SetSeednoteReadiness(projectReadiness(true))
	app := newSeednoteAdminTestApp(h)

	request := func(method, path string) *http.Response {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("X-User-ID", "admin-1")
		req.Header.Set("X-Admin", "true")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
			t.Fatalf("%s %s Cache-Control = %q, want no-store", method, path, got)
		}
		return resp
	}

	status := decodeBody(t, request(http.MethodGet, "/seednote/account/login-status"))["data"].(map[string]any)
	if status["available"] != true || status["logged_in"] != false || !strings.Contains(status["message"].(string), "获取二维码") {
		t.Fatalf("status = %#v", status)
	}
	qr := decodeBody(t, request(http.MethodGet, "/seednote/account/login-qrcode"))["data"].(map[string]any)
	if qr["qrcode_image"] != "cG5n" {
		t.Fatalf("qrcode_image = %v", qr["qrcode_image"])
	}
	logout := decodeBody(t, request(http.MethodDelete, "/seednote/account/login"))["data"].(map[string]any)
	if logout["logged_in"] != false || !logoutCalled {
		t.Fatalf("logout = %#v, called=%v", logout, logoutCalled)
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

func TestProjectCreateFinalizesReferenceSessionAndPersistsAssetID(t *testing.T) {
	app, repo, store := setupProjectHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	refID := "reference-upload"
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: refID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/" + userID + "/" + refID + "/ref.png",
		FileName:   "ref.png", ContentType: "image/png", Size: 456,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session: %v", err)
	}

	resp := doRequest(t, app, "POST", "/api/v1/projects", userID, map[string]any{
		"platform":      model.PlatformArticle,
		"name":          "公众号项目",
		"wechat_app_id": "wx-app",
		"wechat_secret": "secret",
		"reference_image": map[string]any{
			"upload_session_id": refID,
		},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	responseBody := decodeBody(t, resp)
	responseJSON, _ := json.Marshal(responseBody)
	if strings.Contains(string(responseJSON), "reference_image_url") {
		t.Fatalf("response exposed legacy reference URL: %s", responseJSON)
	}
	projects, err := repo.Projects().ListByUserID(ctx, userID, repository.ProjectListOptions{})
	if err != nil || len(projects) != 1 {
		t.Fatalf("list persisted project: projects=%#v err=%v", projects, err)
	}
	if projects[0].ReferenceImageAssetID != refID {
		t.Fatalf("reference asset id = %q, want %q", projects[0].ReferenceImageAssetID, refID)
	}
	assertFinalizedAsset(t, repo, refID, "assets/users/"+userID+"/"+refID+"/ref.png")
	if len(store.signedKeys) != 1 || store.signedKeys[0] != "assets/users/"+userID+"/"+refID+"/ref.png" {
		t.Fatalf("signed keys = %#v", store.signedKeys)
	}
}

func TestProjectRejectsLegacyReferenceImageURL(t *testing.T) {
	legacyKeys := []string{"reference_image_url", "REFERENCE_IMAGE_URL", "Reference_Image_Url"}
	legacyValues := []string{`null`, `""`, `"https://example.com/ref.png"`}
	for _, key := range legacyKeys {
		for _, value := range legacyValues {
			t.Run(key+value, func(t *testing.T) {
				app, repo, _ := setupProjectHandlerTest(t)
				body := strings.NewReader(`{"platform":"article","name":"brand","` + key + `":` + value + `}`)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", body)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-User-ID", uuid.NewString())
				resp, err := app.Test(req)
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != fiber.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%v", resp.StatusCode, decodeBody(t, resp))
				}
				projects, err := repo.Projects().ListByUserID(t.Context(), req.Header.Get("X-User-ID"), repository.ProjectListOptions{})
				if err != nil || len(projects) != 0 {
					t.Fatalf("legacy request persisted projects=%#v err=%v", projects, err)
				}
			})
		}
	}
}

func TestProjectUpdateReferenceNullClearsAndOmissionPreserves(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	projectID := uuid.NewString()
	asset := &model.Asset{
		ID: "asset-1", UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StorageKey: "assets/users/" + userID + "/asset-1/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 3, ETag: "etag-1",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "brand",
		ReferenceImageAssetID: asset.ID, Status: model.ProjectStatusActive,
		Config: model.ProjectConfig{WechatAppID: "wx-app", WechatSecret: "secret"},
	}); err != nil {
		t.Fatal(err)
	}

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "renamed"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("omit status = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	got, _ := repo.Projects().FindByID(t.Context(), projectID)
	if got.ReferenceImageAssetID != asset.ID {
		t.Fatalf("omission changed reference to %q", got.ReferenceImageAssetID)
	}

	resp = doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"reference_image": nil})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("clear status = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	got, _ = repo.Projects().FindByID(t.Context(), projectID)
	if got.ReferenceImageAssetID != "" {
		t.Fatalf("explicit null left reference %q", got.ReferenceImageAssetID)
	}
}

func TestProjectUpdateReferenceSelectionReplacesAndFinalizes(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	projectID := uuid.NewString()
	sessionID := "replacement-session"
	if err := repo.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "brand",
		ReferenceImageAssetID: "old-asset", Status: model.ProjectStatusActive,
		Config: model.ProjectConfig{WechatAppID: "wx-app", WechatSecret: "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID: sessionID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/" + userID + "/" + sessionID + "/ref.jpg",
		FileName:   "ref.jpg", ContentType: "image/jpeg", Size: 12,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{
		"reference_image": map[string]any{"upload_session_id": sessionID},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	got, _ := repo.Projects().FindByID(t.Context(), projectID)
	if got.ReferenceImageAssetID != sessionID {
		t.Fatalf("reference id = %q, want %q", got.ReferenceImageAssetID, sessionID)
	}
}

func TestProjectReferenceSelectionMapsErrorsBeforePersistence(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		session    *model.UploadSession
		asset      *model.Asset
		selection  map[string]any
		storeError error
		wantStatus int
	}{
		{name: "missing asset is opaque", selection: map[string]any{"asset_id": "missing"}, wantStatus: fiber.StatusForbidden},
		{name: "foreign asset is opaque", asset: &model.Asset{ID: "foreign", UserID: "other", Purpose: service.DirectUploadPurposeProjectReference, StorageKey: "assets/users/other/foreign/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, ETag: "etag"}, selection: map[string]any{"asset_id": "foreign"}, wantStatus: fiber.StatusForbidden},
		{name: "asset purpose", asset: &model.Asset{ID: "purpose", Purpose: service.DirectUploadPurposeTaskReference, StorageKey: "assets/users/user/purpose/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, ETag: "etag"}, selection: map[string]any{"asset_id": "purpose"}, wantStatus: fiber.StatusBadRequest},
		{name: "asset metadata", asset: &model.Asset{ID: "metadata", Purpose: service.DirectUploadPurposeProjectReference, StorageKey: "assets/users/user/metadata/ref.txt", FileName: "ref.txt", ContentType: "text/plain", Size: 3, ETag: "etag"}, selection: map[string]any{"asset_id": "metadata"}, wantStatus: fiber.StatusBadRequest},
		{name: "expired session", session: &model.UploadSession{ID: "expired", Purpose: service.DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user/expired/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, Status: model.UploadSessionPending, ExpiresAt: now.Add(-time.Minute)}, selection: map[string]any{"upload_session_id": "expired"}, wantStatus: fiber.StatusGone},
		{name: "conflicting session", session: &model.UploadSession{ID: "conflict", Purpose: service.DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user/conflict/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, Status: model.UploadSessionFinalizing, ExpiresAt: now.Add(time.Hour), FinalizationToken: "other", FinalizationClaimedAt: &now}, selection: map[string]any{"upload_session_id": "conflict"}, wantStatus: fiber.StatusConflict},
		{name: "storage dependency", session: &model.UploadSession{ID: "storage", Purpose: service.DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user/storage/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Hour)}, selection: map[string]any{"upload_session_id": "storage"}, storeError: errors.New("storage unavailable"), wantStatus: fiber.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo, store := setupProjectHandlerTest(t)
			userID := "user"
			if tt.asset != nil {
				asset := *tt.asset
				if asset.UserID == "" {
					asset.UserID = userID
				}
				if err := repo.Assets().Create(t.Context(), &asset); err != nil {
					t.Fatal(err)
				}
			}
			if tt.session != nil {
				session := *tt.session
				session.UserID = userID
				if err := repo.UploadSessions().Create(t.Context(), &session); err != nil {
					t.Fatal(err)
				}
			}
			store.statErr = tt.storeError
			resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
				"platform": model.PlatformArticle, "name": "brand", "reference_image": tt.selection,
			})
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d want %d body=%v", resp.StatusCode, tt.wantStatus, decodeBody(t, resp))
			}
			projects, err := repo.Projects().ListByUserID(t.Context(), userID, repository.ProjectListOptions{})
			if err != nil || len(projects) != 0 {
				t.Fatalf("failed selection persisted project=%#v err=%v", projects, err)
			}
		})
	}
}

func TestProjectReferenceViewsAreSignedForCreateUpdateGetAndList(t *testing.T) {
	app, repo, store := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	asset := &model.Asset{
		ID: "asset-view", UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StorageKey: "assets/users/" + userID + "/asset-view/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 3, ETag: "etag",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	created := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
		"platform": model.PlatformArticle, "name": "brand", "wechat_app_id": "wx-app", "wechat_secret": "secret",
		"reference_image": map[string]any{"asset_id": asset.ID},
	})
	if created.StatusCode != fiber.StatusOK {
		t.Fatalf("create status=%d body=%v", created.StatusCode, decodeBody(t, created))
	}
	projects, _ := repo.Projects().ListByUserID(t.Context(), userID, repository.ProjectListOptions{})
	projectID := projects[0].ID
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPut, "/api/v1/projects/" + projectID, map[string]any{"name": "renamed"}},
		{http.MethodGet, "/api/v1/projects/" + projectID, nil},
		{http.MethodGet, "/api/v1/projects", nil},
	} {
		resp := doRequest(t, app, request.method, request.path, userID, request.body)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("%s %s status=%d body=%v", request.method, request.path, resp.StatusCode, decodeBody(t, resp))
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"asset_id":"asset-view"`) || !strings.Contains(string(body), `"download_url":"https://signed.example/`) {
			t.Fatalf("%s %s missing asset view: %s", request.method, request.path, body)
		}
		if strings.Contains(string(body), "reference_image_url") || strings.Contains(string(body), `"storage_key"`) {
			t.Fatalf("%s %s leaked persisted reference detail: %s", request.method, request.path, body)
		}
	}
	persisted, _ := repo.Projects().FindByID(t.Context(), projectID)
	if persisted.ReferenceImage != nil {
		t.Fatalf("transient view was persisted: %#v", persisted.ReferenceImage)
	}
	if len(store.signedKeys) != 4 {
		t.Fatalf("signed keys=%#v, want one per response", store.signedKeys)
	}
}

func TestProjectCreateSigningFailureDoesNotPersist(t *testing.T) {
	app, repo, store := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	asset := &model.Asset{
		ID: "asset-sign-failure", UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StorageKey: "assets/users/" + userID + "/asset-sign-failure/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 3, ETag: "etag",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	store.downloadErr = errors.New("signer unavailable")

	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
		"platform": model.PlatformArticle, "name": "must-not-persist",
		"reference_image": map[string]any{"asset_id": asset.ID},
	})
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	projects, err := repo.Projects().ListByUserID(t.Context(), userID, repository.ProjectListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("signing failure persisted projects: %#v", projects)
	}
}

func TestProjectUpdateSigningFailureDoesNotMutate(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		targetID string
	}{
		{name: "omission presents current asset", body: map[string]any{"name": "after"}, targetID: "asset-current"},
		{name: "replacement presents selected asset", body: map[string]any{"name": "after", "reference_image": map[string]any{"asset_id": "asset-replacement"}}, targetID: "asset-replacement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo, store := setupProjectHandlerTest(t)
			userID := uuid.NewString()
			projectID := uuid.NewString()
			for _, assetID := range []string{"asset-current", "asset-replacement"} {
				if err := repo.Assets().Create(t.Context(), &model.Asset{
					ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
					StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
					ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.Projects().Create(t.Context(), &model.Project{
				ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "before",
				ReferenceImageAssetID: "asset-current", Status: model.ProjectStatusActive,
			}); err != nil {
				t.Fatal(err)
			}
			store.downloadErr = errors.New("signer unavailable")

			resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, tt.body)
			if resp.StatusCode != fiber.StatusServiceUnavailable {
				t.Fatalf("status=%d want 503 body=%v", resp.StatusCode, decodeBody(t, resp))
			}
			if len(store.signedKeys) != 1 || store.signedKeys[0] != "assets/users/"+userID+"/"+tt.targetID+"/ref.png" {
				t.Fatalf("signed keys=%#v, want target %q", store.signedKeys, tt.targetID)
			}
			persisted, err := repo.Projects().FindByID(t.Context(), projectID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Name != "before" || persisted.ReferenceImageAssetID != "asset-current" {
				t.Fatalf("signing failure mutated project: name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
			}
		})
	}
}

func TestProjectUpdateReferenceOmissionRetriesCASAndReturnsMatchingView(t *testing.T) {
	base := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	projectID := uuid.NewString()
	for _, assetID := range []string{"asset-a", "asset-b"} {
		if err := base.Assets().Create(t.Context(), &model.Asset{
			ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
			StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := base.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "before",
		ReferenceImageAssetID: "asset-a", Status: model.ProjectStatusActive,
		Config: model.ProjectConfig{WechatAppID: "wx-app", WechatSecret: "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	repo := &projectHandlerRepositoryOverride{Repository: base, projects: base.Projects()}
	repo.beforeTx = func(call int) {
		if call != 1 {
			return
		}
		project, err := base.Projects().FindByID(t.Context(), projectID)
		if err != nil {
			t.Fatal(err)
		}
		project.ReferenceImageAssetID = "asset-b"
		if err := base.Projects().Update(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newProjectHandlerTestApp(repo, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "after"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"asset_id":"asset-b"`) || strings.Contains(string(body), `"asset_id":"asset-a"`) {
		t.Fatalf("response does not match retried reference: %s", body)
	}
	wantSigned := []string{
		"assets/users/" + userID + "/asset-a/ref.png",
		"assets/users/" + userID + "/asset-b/ref.png",
	}
	if strings.Join(store.signedKeys, "|") != strings.Join(wantSigned, "|") {
		t.Fatalf("signed keys=%#v want %#v", store.signedKeys, wantSigned)
	}
	persisted, err := base.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "after" || persisted.ReferenceImageAssetID != "asset-b" {
		t.Fatalf("persisted project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
	}
}

func TestProjectUpdateReferenceOmissionReturnsConflictAfterBoundedCASRetries(t *testing.T) {
	base := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	projectID := uuid.NewString()
	assetIDs := []string{"asset-a", "asset-b", "asset-c", "asset-d"}
	for _, assetID := range assetIDs {
		if err := base.Assets().Create(t.Context(), &model.Asset{
			ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
			StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := base.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "before",
		ReferenceImageAssetID: assetIDs[0], Status: model.ProjectStatusActive,
		Config: model.ProjectConfig{WechatAppID: "wx-app", WechatSecret: "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	repo := &projectHandlerRepositoryOverride{Repository: base, projects: base.Projects()}
	repo.beforeTx = func(call int) {
		project, err := base.Projects().FindByID(t.Context(), projectID)
		if err != nil {
			t.Fatal(err)
		}
		project.ReferenceImageAssetID = assetIDs[call]
		if err := base.Projects().Update(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newProjectHandlerTestApp(repo, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "must-not-write"})
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	if repo.txCalls != 3 {
		t.Fatalf("transactional CAS attempts=%d want 3", repo.txCalls)
	}
	wantSigned := []string{
		"assets/users/" + userID + "/asset-a/ref.png",
		"assets/users/" + userID + "/asset-b/ref.png",
		"assets/users/" + userID + "/asset-c/ref.png",
	}
	if strings.Join(store.signedKeys, "|") != strings.Join(wantSigned, "|") {
		t.Fatalf("signed keys=%#v want %#v", store.signedKeys, wantSigned)
	}
	persisted, err := base.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "before" || persisted.ReferenceImageAssetID != "asset-d" {
		t.Fatalf("request mutated project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
	}
}

func TestProjectUpdateReferenceOmissionMatchesNullReferenceRow(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	base := repository.New(db)
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := base.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle,
		Name: "before", Status: model.ProjectStatusActive,
		Config: model.ProjectConfig{WechatAppID: "wx-app", WechatSecret: "secret"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE projects SET reference_image_asset_id = NULL WHERE id = ?", projectID).Error; err != nil {
		t.Fatal(err)
	}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newProjectHandlerTestApp(base, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "after"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	persisted, err := base.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "after" || persisted.ReferenceImageAssetID != "" {
		t.Fatalf("persisted project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
	}
	if len(store.signedKeys) != 0 {
		t.Fatalf("empty reference signed keys=%#v", store.signedKeys)
	}
}

func TestProjectHandler_UpdateFinalizesAvatarUploadSession(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := "updated-avatar-upload"
	key := "uploads/pending/" + userID + "/" + uploadID + "/avatar.png"
	stagingURL := "https://cdn.example.com/" + key

	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "公众号项目",
		Status:   model.ProjectStatusActive,
		Config:   model.ProjectConfig{WechatAppID: "wx-app", WechatSecret: "secret"},
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID:         uploadID,
		UserID:     userID,
		Purpose:    service.DirectUploadPurposeProjectReference,
		StagingKey: key,

		FileName:    "avatar.png",
		ContentType: "image/png",
		Size:        123,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session: %v", err)
	}

	resp := doRequest(t, app, "PUT", "/api/v1/projects/"+projectID, userID, map[string]any{
		"avatar_url": stagingURL,
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	responseBody := decodeBody(t, resp)
	responseJSON, _ := json.Marshal(responseBody)
	if strings.Contains(string(responseJSON), "uploads/pending/") || !strings.Contains(string(responseJSON), "assets/users/") {
		t.Fatalf("updated project persisted non-final URL: %s", responseJSON)
	}

	assertFinalizedAsset(t, repo, uploadID, "assets/users/"+userID+"/"+uploadID+"/avatar.png")
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

func TestProjectHandler_DeleteDoesNotExposeMemoryProviderFailure(t *testing.T) {
	_, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard)
	svc := service.NewProjectService(repo, &logger)
	svc.SetProjectMemoryLifecycle(projectMemoryDeleteFake{err: errors.New("provider internal identity detail")})
	h := NewProjectHandler(svc, &logger)
	app := fiber.New()
	app.Delete("/api/v1/projects/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Delete(c)
	})

	resp := doRequest(t, app, http.MethodDelete, "/api/v1/projects/"+projectID, userID, nil)
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusInternalServerError)
	}
	body := decodeBody(t, resp)
	if msg, _ := body["msg"].(string); msg != "failed to delete project" {
		t.Fatalf("msg = %q, want generic project delete failure", msg)
	}
	if _, err := repo.Projects().FindByID(ctx, projectID); err != nil {
		t.Fatalf("project authority removed after provider failure: %v", err)
	}
}

func TestProjectHandlerMemoryPreviewRequiresOwnershipAndDoesNotCache(t *testing.T) {
	_, repo, _ := setupProjectHandlerTest(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: ownerID, Platform: model.PlatformArticle, Name: "Memory", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	memoryStore, err := projectmemory.NewFilesystemStore(root, projectmemory.Limits{MaxProjectBytes: 1 << 20, MaxFiles: 64, MaxDepth: 4, MaxFileBytes: 64 << 10, MaxPreviewBytes: 256 << 10})
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewProjectHandler(service.NewProjectService(repo, &logger), &logger)
	h.SetProjectMemoryStore(memoryStore)
	app := fiber.New()
	app.Get("/api/v1/projects/:id/memory", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.Memory(c)
	})

	resp := doRequest(t, app, http.MethodGet, "/api/v1/projects/"+projectID+"/memory", ownerID, nil)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("empty memory status/cache = %d/%q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["status"] != projectmemory.StatusEmpty {
		t.Fatalf("empty memory = %#v", data)
	}

	if err := memoryStore.EnsureProject(ctx, projectID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "projects", projectID, "MEMORY.md"), []byte("# Remember me"), 0o600); err != nil {
		t.Fatal(err)
	}
	resp = doRequest(t, app, http.MethodGet, "/api/v1/projects/"+projectID+"/memory", ownerID, nil)
	data = decodeBody(t, resp)["data"].(map[string]any)
	if data["status"] != projectmemory.StatusReady || len(data["files"].([]any)) != 1 {
		t.Fatalf("ready memory = %#v", data)
	}

	resp = doRequest(t, app, http.MethodGet, "/api/v1/projects/"+projectID+"/memory", uuid.NewString(), nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("foreign preview status = %d, want 403", resp.StatusCode)
	}
}

func TestProjectHandlerMemoryPreviewBoundsSerializedResponse(t *testing.T) {
	_, repo, _ := setupProjectHandlerTest(t)
	ctx := context.Background()
	ownerID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: ownerID, Platform: model.PlatformArticle, Name: "Memory", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	memoryStore, err := projectmemory.NewFilesystemStore(root, projectmemory.Limits{MaxProjectBytes: 1 << 20, MaxFiles: 64, MaxDepth: 4, MaxFileBytes: 64 << 10, MaxPreviewBytes: 256 << 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := memoryStore.EnsureProject(ctx, projectID); err != nil {
		t.Fatal(err)
	}
	for index := range 4 {
		name := fmt.Sprintf("memory-%d.md", index)
		if err := os.WriteFile(filepath.Join(root, "projects", projectID, name), []byte(strings.Repeat("\n", 64<<10)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	logger := zerolog.New(io.Discard)
	h := NewProjectHandler(service.NewProjectService(repo, &logger), &logger)
	h.SetProjectMemoryStore(memoryStore)
	app := fiber.New()
	app.Get("/api/v1/projects/:id/memory", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.Memory(c)
	})

	resp := doRequest(t, app, http.MethodGet, "/api/v1/projects/"+projectID+"/memory", ownerID, nil)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) > projectMemoryMaxResponseBytes {
		t.Fatalf("serialized memory response = %d bytes, want at most %d", len(body), projectMemoryMaxResponseBytes)
	}
	var envelope Response
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(envelope.Data)
	if err != nil || !strings.Contains(string(encoded), `"partial":true`) || !strings.Contains(string(encoded), `"truncated":true`) {
		t.Fatalf("bounded response must disclose truncation: data=%s err=%v", encoded, err)
	}
}
