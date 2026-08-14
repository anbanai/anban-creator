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
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Template{}, &model.User{}, &model.UploadSession{}, &model.Asset{}); err != nil {
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
	h.SetStore(store)
	h.SetUploadRepository(repo)

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

	valid := map[string]any{
		"name": "晨光模板", "type": "seednote", "category": model.SeednoteTemplateCategoryProduct,
		"prompt": "暖色晨光，标题居中，正文留白", "visibility": "public",
	}
	resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", "regular-api-user", valid)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("regular create status = %d, want 403; body=%v", resp.StatusCode, decodeBody(t, resp))
	}

	resp = doRequest(t, app, http.MethodPost, "/api/v1/templates/", "admin-api-user", valid)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("admin create status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["prompt"] != valid["prompt"] {
		t.Fatalf("prompt = %v, want %v", data["prompt"], valid["prompt"])
	}
	for _, legacy := range []string{"style_prompt", "visual_style"} {
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

func TestTemplateHandlerAnalyzeThumbnailRequiresAdminAndUsesRestrictedPrompt(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.Template{}, &model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	createTemplateHandlerUser(t, repo, "regular-analyze-user", false)
	createTemplateHandlerUser(t, repo, "admin-analyze-user", true)
	logger := zerolog.New(io.Discard)
	svc := service.NewTemplateService(repo, &logger)
	h := NewTemplateHandler(svc, &logger)
	llm := &fakeProjectLLM{
		response:           "低饱和暖色；上图下文；标题居中；大面积留白",
		validationResponse: `{"allowed":true}`,
	}
	h.SetVisionClient(llm)
	key := "uploads/references/admin-analyze-user/template-thumbnail.png"
	store := &fakeStorageProvider{data: map[string][]byte{key: {'\x89', 'P', 'N', 'G', '\r', '\n', '\x1a', '\n'}}}
	h.SetStore(store)

	app := fiber.New()
	app.Post("/api/v1/templates/analyze-thumbnail", func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return h.AnalyzeThumbnail(c)
	})
	body := map[string]any{"type": "seednote", "thumbnail_url": "/api/v1/files/" + key}
	resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/analyze-thumbnail", "regular-analyze-user", body)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("regular analyze status = %d, want 403", resp.StatusCode)
	}

	resp = doRequest(t, app, http.MethodPost, "/api/v1/templates/analyze-thumbnail", "admin-analyze-user", body)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("admin analyze status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["prompt"] != llm.response || len(data) != 1 {
		t.Fatalf("analyze data = %v, want prompt only", data)
	}
	for _, required := range []string{"视觉", "版式", "商业目标", "卖点", "正文", "CTA", "产品事实"} {
		if !strings.Contains(llm.imagePrompt, required) {
			t.Errorf("analysis prompt missing restriction/subject %q: %s", required, llm.imagePrompt)
		}
	}
	for _, required := range []string{"目标受众", "受众策略"} {
		if !strings.Contains(llm.imagePrompt, required) {
			t.Errorf("analysis prompt missing audience restriction %q: %s", required, llm.imagePrompt)
		}
	}

	for _, prohibited := range []string{
		"目标受众是年轻女性",
		"受众策略采用职场新人定位",
		"产品卖点是快速见效",
		"正文策略先痛点后转化",
		"CTA：立即购买",
		"产品事实：售价99元",
	} {
		llm.response = prohibited
		resp = doRequest(t, app, http.MethodPost, "/api/v1/templates/analyze-thumbnail", "admin-analyze-user", body)
		if resp.StatusCode != fiber.StatusUnprocessableEntity {
			t.Fatalf("prohibited response %q status=%d, want 422; body=%v", prohibited, resp.StatusCode, decodeBody(t, resp))
		}
		errorBody := decodeBody(t, resp)
		raw, _ := json.Marshal(errorBody)
		if bytes.Contains(raw, []byte(prohibited)) {
			t.Fatalf("prohibited model output leaked in response: %s", raw)
		}
		if errorBody["code"] == nil || errorBody["msg"] == nil {
			t.Fatalf("prohibited response is not structured: %v", errorBody)
		}
	}

	llm.response = "面向年轻女性的补水保湿画面，右下角放购买按钮"
	llm.validationResponse = `{"allowed":false}`
	resp = doRequest(t, app, http.MethodPost, "/api/v1/templates/analyze-thumbnail", "admin-analyze-user", body)
	if resp.StatusCode != fiber.StatusUnprocessableEntity {
		t.Fatalf("semantic boundary bypass status=%d, want 422; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	for _, required := range []string{"视觉", "版式", "受众", "卖点", "CTA", "产品事实"} {
		if !strings.Contains(llm.validationPrompt, required) {
			t.Errorf("validation prompt missing boundary %q: %s", required, llm.validationPrompt)
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
		"thumbnail_url": true, "prompt": true, "visibility": true,
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

func createTemplateRow(t *testing.T, repo repository.Repository, ownerID, visibility, name string) *model.Template {
	t.Helper()
	tmpl := &model.Template{
		ID:           uuid.New().String(),
		UserID:       ownerID,
		Name:         name,
		Type:         "seednote",
		Category:     model.SeednoteTemplateCategoryProduct,
		Visibility:   visibility,
		IsActive:     true,
		ThumbnailURL: "https://example.com/x.png",
		Prompt:       "暖色",
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
	app, _ := setupTemplateHandlerTest(t)
	userID := uuid.New().String()

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":          "新模板",
		"type":          "seednote",
		"category":      model.SeednoteTemplateCategoryProduct,
		"thumbnail_url": "https://example.com/x.png",
		"prompt":        "暖色",
		"visibility":    "private",
		"sort_order":    17,
		"is_active":     false,
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
	tmpl.UserID = "must-not-leak"
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
	publicURL := "https://cdn.example.com/" + key
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID:         uploadID,
		UserID:     userID,
		Purpose:    service.DirectUploadPurposeProjectReference,
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
		"name":          "新模板",
		"type":          "seednote",
		"category":      model.SeednoteTemplateCategoryProduct,
		"thumbnail_url": publicURL,
		"prompt":        "暖色",
		"visibility":    "private",
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
			wantStatus: fiber.StatusInternalServerError,
		},
		{
			name: "timeout is redacted internal error",
			configure: func(store *fakeStorageProvider) {
				store.statErr = context.DeadlineExceeded
			},
			wantStatus: fiber.StatusInternalServerError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo := setupTemplateHandlerTest(t, tt.configure)
			if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
				ID: "thumbnail-upload", UserID: "user-1", Purpose: service.DirectUploadPurposeProjectReference,
				StagingKey: objectKey,
				FileName:   "thumb.png", ContentType: "image/png", Size: 123,
				Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
			}); err != nil {
				t.Fatalf("seed upload session: %v", err)
			}

			resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", "user-1", map[string]any{
				"name": "template", "type": "seednote", "category": model.SeednoteTemplateCategoryProduct,
				"prompt": "暖色留白排版", "thumbnail_url": "https://cdn.example.com/" + objectKey,
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
	app, _ := setupTemplateHandlerTest(t)
	userID := uuid.New().String()

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":          "老李的公众号",
		"type":          "seednote",
		"category":      model.SeednoteTemplateCategoryKnowledge,
		"thumbnail_url": "https://example.com/x.png",
		"prompt":        "暖色",
		"visibility":    "public",
		"author":        "老李",
		"writer":        "dan-koe",
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
	app, _ := setupTemplateHandlerTest(t)
	userID := uuid.New().String()

	resp := doRequest(t, app, "POST", "/api/v1/templates/", userID, map[string]any{
		"name":       "视觉模板",
		"type":       "seednote",
		"category":   model.SeednoteTemplateCategoryProduct,
		"prompt":     "暖色生活摄影",
		"visibility": "public",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	if data["prompt"] != "暖色生活摄影" {
		t.Fatalf("prompt = %v, want 暖色生活摄影", data["prompt"])
	}
}

func TestTemplateHandler_CreateAndUpdateRejectBlankPrompt(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	adminID := uuid.New().String()
	seedPending := func(id string) string {
		key := "uploads/pending/" + adminID + "/" + id + "/thumb.png"
		if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
			ID: id, UserID: adminID, Purpose: service.DirectUploadPurposeProjectReference,
			StagingKey: key, FileName: "thumb.png", ContentType: "image/png", Size: 123,
			Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("seed pending upload %s: %v", id, err)
		}
		return "https://cdn.example.com/" + key
	}

	createUploadID := "blank-create-thumbnail"
	resp := doRequest(t, app, http.MethodPost, "/api/v1/templates/", adminID, map[string]any{
		"name": "无内容模板", "type": "seednote",
		"category": model.SeednoteTemplateCategoryProduct, "prompt": "  ",
		"thumbnail_url": seedPending(createUploadID),
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("create status = %d, want 400; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	if session, err := repo.UploadSessions().FindByID(t.Context(), createUploadID); err != nil || session.Status != model.UploadSessionPending {
		t.Fatalf("create upload session = %#v, err=%v; want pending", session, err)
	}

	blankNameUploadID := "blank-derived-name-thumbnail"
	resp = doRequest(t, app, http.MethodPost, "/api/v1/templates/", adminID, map[string]any{
		"name": "  ", "type": "seednote",
		"category": model.SeednoteTemplateCategoryProduct, "prompt": "整体氛围：",
		"thumbnail_url": seedPending(blankNameUploadID),
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("blank derived name create status = %d, want 400; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	if session, err := repo.UploadSessions().FindByID(t.Context(), blankNameUploadID); err != nil || session.Status != model.UploadSessionPending {
		t.Fatalf("blank derived name upload session = %#v, err=%v; want pending", session, err)
	}

	tmpl := createTemplateRow(t, repo, adminID, "public", "existing")
	updateUploadID := "blank-update-thumbnail"
	resp = doRequest(t, app, http.MethodPut, "/api/v1/templates/"+tmpl.ID, adminID, map[string]any{
		"prompt": "\n\t", "thumbnail_url": seedPending(updateUploadID),
	})
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("update status = %d, want 400; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	if session, err := repo.UploadSessions().FindByID(t.Context(), updateUploadID); err != nil || session.Status != model.UploadSessionPending {
		t.Fatalf("update upload session = %#v, err=%v; want pending", session, err)
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

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, owner, map[string]any{
		"name":          "new name",
		"type":          "seednote",
		"thumbnail_url": "https://example.com/new.png",
		"prompt":        "新风格",
		"visibility":    "private",
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
	publicURL := "https://cdn.example.com/" + key
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID:         uploadID,
		UserID:     owner,
		Purpose:    service.DirectUploadPurposeProjectReference,
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
		"thumbnail_url": publicURL,
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
		ID: uploadID, UserID: owner, Purpose: service.DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/" + owner + "/" + uploadID + "/thumb.png",
		FileName:   "thumb.png", ContentType: "image/png", Size: 123,
		Status: model.UploadSessionFinalized, AssetID: uploadID,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed finalized upload session: %v", err)
	}
	if err := repo.Assets().Create(t.Context(), &model.Asset{
		ID: uploadID, UserID: owner, Purpose: service.DirectUploadPurposeProjectReference,
		StorageKey: finalKey, FileName: "thumb.png", ContentType: "image/png", Size: 123,
	}); err != nil {
		t.Fatalf("seed finalized thumbnail asset: %v", err)
	}
	tmpl := createTemplateRow(t, repo, owner, "public", "old name")
	tmpl.ThumbnailURL = "/api/v1/files/" + finalKey
	if err := repo.Templates().Update(t.Context(), tmpl); err != nil {
		t.Fatalf("seed template thumbnail: %v", err)
	}

	resp := doRequest(t, app, http.MethodPut, "/api/v1/templates/"+tmpl.ID, editor, map[string]any{
		"name":          "renamed by another admin",
		"thumbnail_url": "/api/v1/files/" + finalKey + "?signature=existing",
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
	if persisted.ThumbnailURL != "/api/v1/files/"+finalKey {
		t.Fatalf("thumbnail_url = %q, want canonical existing URL", persisted.ThumbnailURL)
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
