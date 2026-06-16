package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

func setupTemplateHandlerTest(t *testing.T) (*fiber.App, repository.Repository) {
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
	if err := db.AutoMigrate(&model.Template{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Exec("DELETE FROM templates")

	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := service.NewTemplateService(repo, &logger)
	h := NewTemplateHandler(svc, &logger)

	app := fiber.New()
	// Stub middleware: read X-User-ID into locals, mirroring how GetUserID works.
	injectUser := func(c fiber.Ctx) error {
		if uid := c.Get("X-User-ID"); uid != "" {
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

func createTemplateRow(t *testing.T, repo repository.Repository, ownerID, visibility, name string) *model.Template {
	t.Helper()
	tmpl := &model.Template{
		ID:           uuid.New().String(),
		UserID:       ownerID,
		Name:         name,
		Type:         "seednote",
		Visibility:   visibility,
		IsActive:     true,
		ThumbnailURL: "https://example.com/x.png",
		StylePrompt:  "暖色",
	}
	if err := repo.Templates().Create(t.Context(), tmpl); err != nil {
		t.Fatalf("seed template: %v", err)
	}
	return tmpl
}

func TestTemplateHandler_List_ScopeAllReturnsPublicPlusOwn(t *testing.T) {
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
	// owner sees: own private, own public, other's public (3 rows).
	// Does NOT see other's private.
	if len(items) != 3 {
		t.Errorf("items count = %d, want 3 (own private + own public + other public)", len(items))
	}
}

func TestTemplateHandler_List_ScopeMineReturnsOwnOnly(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	other := uuid.New().String()

	createTemplateRow(t, repo, owner, "private", "我的私有")
	createTemplateRow(t, repo, owner, "public", "我的公开")
	createTemplateRow(t, repo, other, "public", "别人的公开")

	resp := doRequest(t, app, "GET", "/api/v1/templates/?scope=mine", owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	items := body["data"].(map[string]any)["items"].([]any)
	if len(items) != 2 {
		t.Errorf("scope=mine items count = %d, want 2 (only own templates)", len(items))
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
		"name":         "新模板",
		"type":         "seednote",
		"thumbnail_url": "https://example.com/x.png",
		"style_prompt": "暖色",
		"visibility":   "private",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeBody(t, resp)
	data := body["data"].(map[string]any)
	if data["user_id"] != userID {
		t.Errorf("user_id = %v, want %s", data["user_id"], userID)
	}
	if data["visibility"] != "private" {
		t.Errorf("visibility = %v, want private", data["visibility"])
	}
}

func TestTemplateHandler_Update_NonOwnerReturns403(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	intruder := uuid.New().String()
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

func TestTemplateHandler_Update_OwnerSucceeds(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "old name")

	resp := doRequest(t, app, "PUT", "/api/v1/templates/"+tmpl.ID, owner, map[string]any{
		"name":         "new name",
		"type":         "seednote",
		"thumbnail_url": "https://example.com/new.png",
		"style_prompt": "新风格",
		"visibility":   "private",
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

func TestTemplateHandler_Delete_OwnerSucceeds(t *testing.T) {
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

func TestTemplateHandler_Delete_NonOwnerReturns403(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	intruder := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "public", "t")

	resp := doRequest(t, app, "DELETE", "/api/v1/templates/"+tmpl.ID, intruder, nil)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
}

func TestTemplateHandler_GetByID_PrivateTemplateReturns404ForNonOwner(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	intruder := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "private", "secret")

	resp := doRequest(t, app, "GET", "/api/v1/templates/"+tmpl.ID, intruder, nil)
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404 (non-owner of private should not see it)", resp.StatusCode)
	}
}

func TestTemplateHandler_GetByID_PrivateTemplateVisibleToOwner(t *testing.T) {
	app, repo := setupTemplateHandlerTest(t)
	owner := uuid.New().String()
	tmpl := createTemplateRow(t, repo, owner, "private", "secret")

	resp := doRequest(t, app, "GET", "/api/v1/templates/"+tmpl.ID, owner, nil)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
