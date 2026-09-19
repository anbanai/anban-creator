package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
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
	"github.com/anbanai/anban-creator/server/storage"
)

func setupTemplateHandlerTest(t *testing.T, configureStore ...func(*fakeStorageProvider)) (*fiber.App, repository.Repository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Template{}, &model.User{}, &model.UploadSession{}, &model.Asset{}, &model.ImageAnalysisJob{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Exec("DELETE FROM templates")
	db.Exec("DELETE FROM users")
	db.Exec("DELETE FROM upload_sessions")
	db.Exec("DELETE FROM assets")

	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := service.NewTemplateService(repo, &logger)
	h := NewTemplateHandler(svc, &logger)
	store := uploadSessionStatStore(repo.UploadSessions())
	for _, configure := range configureStore {
		configure(store)
	}
	analyses := service.NewImageAnalysisService(repo, store, nil, nil, nil, service.ImageAnalysisConfig{}, &logger)
	svc.SetImageAnalysisService(analyses)
	h.SetReferenceAssetService(service.NewReferenceAssetService(repo, store, time.Now))

	app := fiber.New()
	// Stub middleware: read X-User-ID into locals, mirroring how GetUserID works.
	injectUser := func(c fiber.Ctx) error {
		if uid := c.Get("X-User-ID"); uid != "" {
			if c.Method() == http.MethodPost || c.Method() == http.MethodPut || c.Method() == http.MethodDelete {
				if _, err := repo.Users().FindByID(c.Context(), uid); errors.Is(err, gorm.ErrRecordNotFound) {
					if err := repo.Users().Create(c.Context(), &model.User{
						ID: uid, Email: uid + "@example.com", Password: "test", InviteCode: strings.ReplaceAll(uuid.NewString(), "-", "")[:8], IsAdmin: true,
					}); err != nil {
						t.Fatalf("seed legacy mutation admin %s: %v", uid, err)
					}
				}
			}
			c.Locals("user_id", uid)
		}
		return c.Next()
	}
	g := app.Group("/api/v1/templates", injectUser)
	g.Get("/", h.List)
	g.Post("/", h.Create)
	g.Get("/:id", h.GetByID)
	g.Put("/:id", h.Update)
	g.Delete("/:id", h.Delete)
	return app, repo
}

func createTemplateHandlerUser(t *testing.T, repo repository.Repository, id string, isAdmin bool) {
	t.Helper()
	if err := repo.Users().Create(t.Context(), &model.User{
		ID: id, Email: id + "@example.com", Password: "test", InviteCode: strings.ToUpper(strings.ReplaceAll(id, "-", ""))[:8], IsAdmin: isAdmin,
	}); err != nil {
		t.Fatalf("create template handler user %s: %v", id, err)
	}
}

func TestTemplateHandlerCreateRequiresAdminAndCanonicalPrompt(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	createTemplateHandlerUser(t, repo, "regular-api-user", false)
	createTemplateHandlerUser(t, repo, "admin-api-user", true)

	regularRequest := map[string]any{
		"name": "晨光模板", "type": "seednote", "category": model.SeednoteTemplateCategoryProduct,
		"prompt": "暖色晨光，标题居中，正文留白", "visibility": "public",
		"thumbnail_image": templateThumbnailSelection(createTemplateThumbnailAsset(t, repo, "regular-api-user")),
	}
	resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", "regular-api-user", regularRequest)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("regular create status = %d, want 403; body=%v", resp.StatusCode, decodeBody(t, resp))
	}

	valid := maps.Clone(regularRequest)
	valid["thumbnail_image"] = templateThumbnailSelection(createTemplateThumbnailAsset(t, repo, "admin-api-user"))
	resp = doRequest(t, app, http.MethodPost, "/api/v1/templates/", "admin-api-user", valid)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("admin create status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["prompt"] != valid["prompt"] {
		t.Fatalf("prompt = %v, want %v", data["prompt"], valid["prompt"])
	}
	for _, legacy := range []string{"style_prompt", "visual_style", "thumbnail_url"} {
		if _, exists := data[legacy]; exists {
			t.Fatalf("response retained legacy prompt alias %q", legacy)
		}
		legacyBody := maps.Clone(valid)
		delete(legacyBody, "prompt")
		legacyBody[legacy] = "legacy"
		legacyResp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", "admin-api-user", legacyBody)
		if legacyResp.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("%s create status = %d, want 400", legacy, legacyResp.StatusCode)
		}
	}
}

func TestTemplateCreateRejectsRemovedImageModelField(t *testing.T) {
	app, _ := setupTemplateHandlerTest(t)
	resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", uuid.NewString(), map[string]any{
		"name": "legacy", "type": "article", "visibility": "private", "image_model_key": "standard_image",
	})
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusBadRequest || !bytes.Contains(body, []byte("use image_capability_key")) {
		t.Fatalf("status/body = %d, %s; want 400 with migration hint", resp.StatusCode, body)
	}
}

func doRequest(t *testing.T, app *fiber.App, method, path, userID string, body any) *http.Response {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reqBody)
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func assertCanonicalTemplateResponse(t *testing.T, data map[string]any) {
	t.Helper()
	want := map[string]bool{
		"id": true, "type": true, "name": true, "category": true,
		"thumbnail": true, "prompt": true, "prompt_source": true,
		"readiness_status": true, "activate_when_ready": true, "visibility": true,
		"sort_order": true, "is_active": true, "created_at": true, "updated_at": true,
	}
	if len(data) != len(want) {
		t.Fatalf("template response keys = %v, want exactly %v", maps.Keys(data), maps.Keys(want))
	}
	for key := range want {
		if _, ok := data[key]; !ok {
			t.Errorf("template response missing %q: %v", key, data)
		}
	}
}

func createTemplateThumbnailAsset(t *testing.T, repo repository.Repository, ownerID string) *model.Asset {
	t.Helper()
	id := uuid.NewString()
	asset := &model.Asset{
		ID: id, UserID: ownerID, Purpose: service.DirectUploadPurposeTemplateThumbnail,
		StorageKey: "assets/users/" + ownerID + "/" + id + "/thumb.png",
		FileName:   "thumb.png", ContentType: "image/png", Size: 123, ETag: "etag-" + id,
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatalf("seed template thumbnail asset: %v", err)
	}
	return asset
}

func templateThumbnailSelection(asset *model.Asset) map[string]any {
	return map[string]any{"asset_id": asset.ID}
}

func createTemplateRow(t *testing.T, repo repository.Repository, ownerID, visibility, name string) *model.Template {
	t.Helper()
	asset := createTemplateThumbnailAsset(t, repo, ownerID)
	tmpl := &model.Template{
		ID: uuid.New().String(), UserID: ownerID, Name: name, Type: "seednote",
		Category: model.SeednoteTemplateCategoryProduct, Visibility: visibility, IsActive: true,
		ThumbnailAssetID: asset.ID, Prompt: "暖色", PromptSource: model.ImageAnalysisSourceManual,
		ReadinessStatus: model.TemplateReadinessReady,
	}
	if err := repo.Templates().Create(t.Context(), tmpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	return tmpl
}

func TestTemplateHandler_List_ScopeAllForOrdinaryUserReturnsPublicOnly(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	other := uuid.New().String()

	createTemplateRow(t, repo, owner, "private", "我的私有")
	createTemplateRow(t, repo, owner, "public", "我的公开")
	createTemplateRow(t, repo, other, "public", "别人的公开")
	createTemplateRow(t, repo, other, "private", "别人的私有")

	resp := doRequest(t, app, "GET", "/api/v1/templates/?scope=all", owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Errorf("items count = %d, want 2 public templates", len(items))
	}
}

func TestTemplateHandler_List_ReturnsCanonicalPrompt(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	createTemplateRow(t, repo, owner, "public", "视觉模板")

	resp := doRequest(t, app, "GET", "/api/v1/templates/?scope=all", owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items count = %d, want 1", len(items))
	}
	item := items[0].(map[string]any)
	if item["prompt"] != "暖色" {
		t.Fatalf("prompt = %v, want 暖色", item["prompt"])
	}
}

func TestTemplateHandler_List_ScopePublicReturnsPublicOnly(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()

	createTemplateRow(t, repo, owner, "private", "我的私有")
	createTemplateRow(t, repo, owner, "public", "我的公开")

	resp := doRequest(t, app, "GET", "/api/v1/templates/?scope=public", owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Errorf("scope=public items count = %d, want 1 (only public)", len(items))
	}
}

func TestTemplateHandler_List_InvalidScopeReturns400(t *testing.T) {
	app, _ := setupTemplateHandlerTest(t)
	userID := uuid.New().String()

	resp := doRequest(t, app, "GET", "/api/v1/templates/?scope=bogus", userID, nil)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid scope", resp.StatusCode)
	}
}

func TestTemplateHandler_Create_InvalidTypeReturns400(t *testing.T) {
	app, _ := setupTemplateHandlerTest(t)
	userID := uuid.New().String()

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":       "bad",
		"type":       "weird",
		"visibility": "public",
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTemplateHandler_Create_RequiresUserID(t *testing.T) {
	app, _ := setupTemplateHandlerTest(t)
	resp := doRequest(t, app, "POST", "/api/v1/templates/", "", map[string]any{
		"name":       "x",
		"type":       "seednote",
		"visibility": "public",
	})
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestTemplateHandler_Create_Success(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	userID := uuid.New().String()
	asset := createTemplateThumbnailAsset(t, repo, userID)

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":            "新模板",
		"type":            "seednote",
		"category":        model.SeednoteTemplateCategoryProduct,
		"thumbnail_image": templateThumbnailSelection(asset),
		"prompt":          "暖色",
		"visibility":      "private",
		"sort_order":      17,
		"is_active":       false,
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	data := body["data"].(map[string]any)
	assertCanonicalTemplateResponse(t, data)
	if data["visibility"] != "private" {
		t.Errorf("visibility = %v, want private", data["visibility"])
	}
	if data["sort_order"] != float64(17) {
		t.Errorf("sort_order = %v, want 17", data["sort_order"])
	}
	if data["is_active"] != false {
		t.Errorf("is_active = %v, want false", data["is_active"])
	}
}

func TestTemplateHandlerCanonicalResponsesHidePersistenceFields(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	adminID := uuid.NewString()
	createTemplateHandlerUser(t, repo, adminID, true)
	tmpl := createTemplateRow(t, repo, adminID, "public", "strict response")
	tmpl.Category = model.SeednoteTemplateCategoryProduct
	tmpl.Writer = "legacy-writer"
	tmpl.Theme = "legacy-theme"
	tmpl.Author = "legacy-author"
	tmpl.Tags = []string{"legacy-tag"}
	if err := repo.Templates().Update(t.Context(), tmpl); err != nil {
		t.Fatalf("update legacy fixture: %v", err)
	}

	for _, path := range []string{"/api/v1/templates/?scope=all", "/api/v1/templates/" + tmpl.ID} {
		resp := doRequest(t, app, http.MethodGet, path, adminID, nil)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("GET %s status=%d", path, resp.StatusCode)
		}
		body := decodeBody(t, resp)
		if strings.Contains(path, "scope=") {
			item := body["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
			assertCanonicalTemplateResponse(t, item)
		} else {
			assertCanonicalTemplateResponse(t, body["data"].(map[string]any))
		}
	}
}

func TestTemplateHandlerRejectsRemovedTagsAndUpdatesSortOrder(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	adminID := uuid.NewString()
	createTemplateHandlerUser(t, repo, adminID, true)
	base := map[string]any{
		"name": "strict request", "type": model.TemplateTypeSeednote,
		"category": model.SeednoteTemplateCategoryProduct, "prompt": "留白排版",
		"thumbnail_image": templateThumbnailSelection(createTemplateThumbnailAsset(t, repo, adminID)),
	}
	withTags := maps.Clone(base)
	withTags["tags"] = []string{"removed"}
	resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", adminID, withTags)
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("create tags status=%d, want 400", resp.StatusCode)
	}

	resp = doRequest(t, app, http.MethodPost, "/api/v1/templates/", adminID, base)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("create status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	created := decodeBody(t, resp)["data"].(map[string]any)
	id := created["id"].(string)
	resp = doRequest(t, app, http.MethodPut, "/api/v1/templates/"+id, adminID, map[string]any{"sort_order": 29})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("update sort_order status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	updated := decodeBody(t, resp)["data"].(map[string]any)
	assertCanonicalTemplateResponse(t, updated)
	if updated["sort_order"] != float64(29) {
		t.Fatalf("updated sort_order=%v, want 29", updated["sort_order"])
	}
	resp = doRequest(t, app, http.MethodPut, "/api/v1/templates/"+id, adminID, map[string]any{"tags": []string{"removed"}})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("update tags status=%d, want 400", resp.StatusCode)
	}
}

func TestTemplateHandler_CreateFinalizesThumbnailUploadSession(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	userID := uuid.New().String()
	uploadID := "thumbnail-upload"
	key := "uploads/pending/" + userID + "/" + uploadID + "/thumb.png"
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID:         uploadID,
		UserID:     userID,
		Purpose:    service.DirectUploadPurposeTemplateThumbnail,
		StagingKey: key,

		FileName:    "thumb.png",
		ContentType: "image/png",
		Size:        123,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session: %v", err)
	}

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":            "新模板",
		"type":            "seednote",
		"category":        model.SeednoteTemplateCategoryProduct,
		"thumbnail_image": map[string]any{"upload_session_id": uploadID},
		"prompt":          "暖色",
		"visibility":      "private",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	responseBody := decodeBody(t, resp)
	responseJSON, _ := json.Marshal(responseBody)
	if bytes.Contains(responseJSON, []byte("uploads/pending/")) || !bytes.Contains(responseJSON, []byte("assets/users/")) {
		t.Fatalf("template persisted non-final thumbnail: %s", responseJSON)
	}

	assertFinalizedAsset(t, repo, uploadID, "assets/users/"+userID+"/"+uploadID+"/thumb.png")
}

func TestTemplateHandler_CreateClassifiesUploadSessionFinalizeErrors(t *testing.T) {
	const (
		objectKey     = "uploads/pending/user-1/thumbnail-upload/thumb.png"
		backendDetail = "oss-cn-hangzhou.aliyuncs.com provider secret"
	)
	tests := []struct {
		name       string
		configure  func(*fakeStorageProvider)
		wantStatus int
	}{
		{
			name: "missing object is redacted bad request",
			configure: func(store *fakeStorageProvider) {
				store.statErr = storage.ErrObjectNotFound
			},
			wantStatus: fiber.StatusBadRequest,
		},
		{
			name: "metadata mismatch is redacted bad request",
			configure: func(store *fakeStorageProvider) {
				store.statInfo = &storage.ObjectInfo{Key: objectKey, Size: 124, ContentType: "image/png"}
			},
			wantStatus: fiber.StatusBadRequest,
		},
		{
			name: "backend error is redacted internal error",
			configure: func(store *fakeStorageProvider) {
				store.statErr = errors.New(backendDetail)
			},
			wantStatus: fiber.StatusServiceUnavailable,
		},
		{
			name: "timeout is redacted internal error",
			configure: func(store *fakeStorageProvider) {
				store.statErr = context.DeadlineExceeded
			},
			wantStatus: fiber.StatusServiceUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo := setupTemplateHandlerTest(t, tt.configure)
			if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
				ID: "thumbnail-upload", UserID: "user-1", Purpose: service.DirectUploadPurposeTemplateThumbnail,
				StagingKey: objectKey,
				FileName:   "thumb.png", ContentType: "image/png", Size: 123,
				Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatalf("seed upload session: %v", err)
			}

			resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", "user-1", map[string]any{
				"name": "template", "type": "seednote", "category": model.SeednoteTemplateCategoryProduct,
				"prompt": "暖色留白排版", "thumbnail_image": map[string]any{"upload_session_id": "thumbnail-upload"},
			})
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("read response: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, body = %s; want %d", resp.StatusCode, body, tt.wantStatus)
			}
			for _, secret := range []string{objectKey, backendDetail, "oss-cn-hangzhou", "provider secret"} {
				if bytes.Contains(body, []byte(secret)) {
					t.Fatalf("response leaked %q: %s", secret, body)
				}
			}
		})
	}
}

func TestTemplateHandler_Create_IgnoresNonVisualRuntimeFields(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	userID := uuid.New().String()
	asset := createTemplateThumbnailAsset(t, repo, userID)

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":            "老李的公众号",
		"type":            "seednote",
		"category":        model.SeednoteTemplateCategoryKnowledge,
		"thumbnail_image": templateThumbnailSelection(asset),
		"prompt":          "暖色",
		"visibility":      "public",
		"author":          "老李",
		"writer":          "dan-koe",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["prompt"] != "暖色" {
		t.Errorf("prompt = %v, want 暖色", data["prompt"])
	}
	for _, legacy := range []string{"author", "writer"} {
		if _, exists := data[legacy]; exists {
			t.Errorf("response exposed ignored legacy field %q", legacy)
		}
	}
}

func TestTemplateHandler_Create_ReturnsCanonicalPrompt(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	userID := uuid.New().String()
	asset := createTemplateThumbnailAsset(t, repo, userID)

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":            "视觉模板",
		"type":            "seednote",
		"category":        model.SeednoteTemplateCategoryProduct,
		"prompt":          "暖色生活摄影",
		"visibility":      "public",
		"thumbnail_image": templateThumbnailSelection(asset),
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["prompt"] != "暖色生活摄影" {
		t.Fatalf("prompt = %v, want 暖色生活摄影", data["prompt"])
	}
}

func TestTemplateHandlerBlankPromptCreatesAndRestartsBackgroundAnalysis(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	adminID := uuid.New().String()
	asset := createTemplateThumbnailAsset(t, repo, adminID)

	resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", adminID, map[string]any{
		"name": "待识别模板", "type": "seednote", "category": model.SeednoteTemplateCategoryProduct,
		"prompt": "  ", "thumbnail_image": templateThumbnailSelection(asset), "is_active": true,
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("create status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	created := decodeBody(t, resp)["data"].(map[string]any)
	if created["readiness_status"] != model.TemplateReadinessAnalyzing || created["is_active"] != false || created["activate_when_ready"] != true {
		t.Fatalf("created analysis state = %#v", created)
	}
	analysis := created["image_analysis"].(map[string]any)
	if analysis["status"] != model.ImageAnalysisStatusQueued || analysis["kind"] != model.ImageAnalysisKindTemplatePrompt {
		t.Fatalf("image_analysis = %#v", analysis)
	}

	tmpl := createTemplateRow(t, repo, adminID, "public", "existing")
	replacement := createTemplateThumbnailAsset(t, repo, adminID)
	resp = doRequest(t, app, http.MethodPut, "/api/v1/templates/"+tmpl.ID, adminID, map[string]any{
		"prompt": "\n\t", "thumbnail_image": templateThumbnailSelection(replacement),
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	updated := decodeBody(t, resp)["data"].(map[string]any)
	if updated["prompt"] != "" || updated["readiness_status"] != model.TemplateReadinessAnalyzing || updated["is_active"] != false {
		t.Fatalf("updated analysis state = %#v", updated)
	}
}

func TestTemplateHandler_UpdateRejectsBlankName(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	adminID := uuid.New().String()
	tmpl := createTemplateRow(t, repo, adminID, "public", "existing")

	resp := doRequest(t, app, http.MethodPut, "/api/v1/templates/"+tmpl.ID, adminID, map[string]any{"name": " \n "})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
}

func TestTemplateHandler_Update_NonAdminReturns403(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	intruder := uuid.New().String()
	createTemplateHandlerUser(t, repo, intruder, false)
	tmpl := createTemplateRow(t, repo, owner, "private", "owner's template")

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, intruder, map[string]any{
		"name":       "hacked",
		"type":       "seednote",
		"visibility": "public",
	})
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestTemplateHandler_Update_NotFoundReturns404(t *testing.T) {
	app, _ := setupTemplateHandlerTest(t)
	userID := uuid.New().String()
	missingID := uuid.New().String()

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+missingID, userID, map[string]any{
		"name":       "x",
		"type":       "seednote",
		"visibility": "public",
	})
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestTemplateHandler_Update_InvalidTypeReturns400(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "t")

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, owner, map[string]any{
		"name":       "x",
		"type":       "weird",
		"visibility": "public",
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTemplateHandler_Update_AdminSucceeds(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "old name")
	replacement := createTemplateThumbnailAsset(t, repo, owner)

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, owner, map[string]any{
		"name":            "new name",
		"type":            "seednote",
		"thumbnail_image": templateThumbnailSelection(replacement),
		"prompt":          "新风格",
		"visibility":      "private",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	data := body["data"].(map[string]any)
	if data["name"] != "new name" {
		t.Errorf("name = %v, want new name", data["name"])
	}
	if data["visibility"] != "private" {
		t.Errorf("visibility = %v, want private", data["visibility"])
	}
}

func TestTemplateHandler_UpdateFinalizesThumbnailUploadSession(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "old name")
	uploadID := "updated-thumbnail-upload"
	key := "uploads/pending/" + owner + "/" + uploadID + "/thumb.png"
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID:         uploadID,
		UserID:     owner,
		Purpose:    service.DirectUploadPurposeTemplateThumbnail,
		StagingKey: key,

		FileName:    "thumb.png",
		ContentType: "image/png",
		Size:        123,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session: %v", err)
	}

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, owner, map[string]any{
		"thumbnail_image": map[string]any{"upload_session_id": uploadID},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}

	assertFinalizedAsset(t, repo, uploadID, "assets/users/"+owner+"/"+uploadID+"/thumb.png")
}

func TestTemplateHandler_UpdateByAnotherAdminPreservesExistingFinalizedThumbnail(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	editor := uuid.New().String()
	createTemplateHandlerUser(t, repo, owner, true)
	createTemplateHandlerUser(t, repo, editor, true)

	uploadID := "existing-thumbnail-upload"
	finalKey := "assets/users/" + owner + "/" + uploadID + "/thumb.png"
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID: uploadID, UserID: owner, Purpose: service.DirectUploadPurposeTemplateThumbnail,
		StagingKey: "uploads/pending/" + owner + "/" + uploadID + "/thumb.png",
		FileName:   "thumb.png", ContentType: "image/png", Size: 123,
		Status: model.UploadSessionFinalized, AssetID: uploadID,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed finalized upload session: %v", err)
	}
	if err := repo.Assets().Create(t.Context(), &model.Asset{
		ID: uploadID, UserID: owner, Purpose: service.DirectUploadPurposeTemplateThumbnail,
		StorageKey: finalKey, FileName: "thumb.png", ContentType: "image/png", Size: 123, ETag: "etag-existing",
	}); err != nil {
		t.Fatalf("seed finalized thumbnail asset: %v", err)
	}
	tmpl := createTemplateRow(t, repo, owner, "public", "old name")
	tmpl.ThumbnailAssetID = uploadID
	if err := repo.Templates().Update(t.Context(), tmpl); err != nil {
		t.Fatalf("seed template thumbnail: %v", err)
	}

	resp := doRequest(t, app, http.MethodPut, "/api/v1/templates/"+tmpl.ID, editor, map[string]any{
		"name": "renamed by another admin",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	persisted, err := repo.Templates().FindByID(t.Context(), tmpl.ID)
	if err != nil {
		t.Fatalf("find updated template: %v", err)
	}
	if persisted.Name != "renamed by another admin" {
		t.Fatalf("name = %q, want cross-admin update", persisted.Name)
	}
	if persisted.ThumbnailAssetID != uploadID {
		t.Fatalf("thumbnail_asset_id = %q, want %q", persisted.ThumbnailAssetID, uploadID)
	}
}

// Empty type means "leave unchanged" (PATCH semantics). The handler must not
// 400 on missing type; the service treats it as a no-op for that field.
func TestTemplateHandler_Update_EmptyTypeLeavesUnchanged(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "name")

	// Omit type entirely — JSON marshal of map without "type" key leaves req.Type empty.
	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, owner, map[string]any{
		"name":       "renamed",
		"visibility": "private",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200 (empty type should be allowed)", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	data := body["data"].(map[string]any)
	// Type should still be the original seednote from createTemplateRow.
	if data["type"] != "seednote" {
		t.Errorf("type = %v, want unchanged seednote", data["type"])
	}
}

func TestTemplateHandler_UpdateDeleteRejectLegacyTemplateTypes(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "article template")
	tmpl.Type = "article"
	tmpl.Prompt = "暖色生活摄影"
	tmpl.Author = "老李"
	tmpl.Writer = "dan-koe"
	tmpl.Theme = "autumn-warm"
	if err := repo.Templates().Update(t.Context(), tmpl); err != nil {
		t.Fatalf("seed article template update: %v", err)
	}

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, owner, map[string]any{
		"prompt": "新视觉",
	})
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("update status = %d, want 404", resp.StatusCode)
	}
	resp = doRequest(t, app, "DELETE", "/api/v1/templates/"+tmpl.ID, owner, nil)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("delete status = %d, want 404", resp.StatusCode)
	}
	persisted, err := repo.Templates().FindByID(t.Context(), tmpl.ID)
	if err != nil {
		t.Fatalf("legacy template was deleted: %v", err)
	}
	if persisted.Prompt != "暖色生活摄影" {
		t.Fatalf("legacy prompt = %q, want unchanged", persisted.Prompt)
	}
}

func TestTemplateHandler_Delete_AdminSucceeds(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "t")

	resp := doRequest(t, app, "DELETE", "/api/v1/templates/"+tmpl.ID, owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// Verify it's actually gone.
	getResp := doRequest(t, app, "GET", "/api/v1/templates/"+tmpl.ID, owner, nil)
	if getResp.StatusCode != fiber.StatusNotFound {
		t.Errorf("after delete, GET status = %d, want 404", getResp.StatusCode)
	}
}

func TestTemplateHandler_Delete_NonAdminReturns403(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	intruder := uuid.New().String()
	createTemplateHandlerUser(t, repo, intruder, false)
	tmpl := createTemplateRow(t, repo, owner, "public", "t")

	resp := doRequest(t, app, "DELETE", "/api/v1/templates/"+tmpl.ID, intruder, nil)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestTemplateHandler_GetByID_PrivateTemplateReturns404ForOrdinaryUser(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	intruder := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "private", "secret")

	resp := doRequest(t, app, "GET", "/api/v1/templates/"+tmpl.ID, intruder, nil)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404 (ordinary users cannot see private templates)", resp.StatusCode)
	}
}

func TestTemplateHandler_GetByID_PrivateTemplateVisibleToAdmin(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	createTemplateHandlerUser(t, repo, owner, true)
	tmpl := createTemplateRow(t, repo, owner, "private", "secret")

	resp := doRequest(t, app, "GET", "/api/v1/templates/"+tmpl.ID, owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestTemplateHandler_GetByID_ReturnsCanonicalPrompt(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "视觉模板")

	resp := doRequest(t, app, "GET", "/api/v1/templates/"+tmpl.ID, owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["prompt"] != "暖色" {
		t.Fatalf("prompt = %v, want 暖色", data["prompt"])
	}
}
