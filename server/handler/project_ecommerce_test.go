package handler

import (
	"io"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

// setupProjectEcommerceTest wires a ProjectHandler backed by an in-memory repo.
func setupProjectEcommerceTest(t *testing.T) (*fiber.App, repository.Repository, *model.Template) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:proj_ecom_test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	if err := db.AutoMigrate(&model.Template{}, &model.Project{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Exec("DELETE FROM templates")
	db.Exec("DELETE FROM projects")

	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()

	projectSvc := service.NewProjectService(repo, &logger)
	templateSvc := service.NewTemplateService(repo, &logger)
	h := NewProjectHandler(projectSvc, &logger)
	h.SetTemplateService(templateSvc)
	// Create calls signProjectURLs; inject a store so it never dereferences nil.
	h.SetStore(&fakeStorageProvider{data: map[string][]byte{}})

	app := fiber.New()
	injectUser := func(c fiber.Ctx) error {
		if uid := c.Get("X-User-ID"); uid != "" {
			c.Locals("user_id", uid)
			c.Locals("user", &model.User{ID: uid, IsAdmin: true})
		}
		return c.Next()
	}
	app.Post("/api/v1/projects", injectUser, h.Create)
	return app, repo, nil
}

func TestProjectHandler_CreateEcommerceStoresProjectDefaults(t *testing.T) {
	app, _, _ := setupProjectEcommerceTest(t)
	owner := uuid.New().String()

	resp := doRequest(t, app, "POST", "/api/v1/projects", owner, map[string]any{
		"platform": "ecommerce",
		"name":     "测试店铺",
		"ecommerce_defaults": map[string]any{
			"default_selected_modules": map[string]any{"main": 6, "detail": 4},
			"target_platform":          "wechat-store",
			"brand_brief":              "高端护肤品牌",
			"image_capability_key":     "openai-gpt-image",
		},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data := decodeBody(t, resp)["data"].(map[string]any)
	project := data["project"].(map[string]any)
	ec, ok := project["ecommerce_defaults"].(map[string]any)
	if !ok {
		t.Fatalf("ecommerce_defaults NOT populated on created project. data=%#v", data)
	}
	if ec["target_platform"] != "wechat-store" {
		t.Errorf("target_platform = %v, want wechat-store", ec["target_platform"])
	}
	if ec["brand_brief"] != "高端护肤品牌" {
		t.Errorf("brand_brief = %v, want 高端护肤品牌", ec["brand_brief"])
	}
	if ec["image_capability_key"] != "openai-gpt-image" {
		t.Errorf("image_capability_key = %v, want openai-gpt-image", ec["image_capability_key"])
	}
	modules, ok := ec["default_selected_modules"].(map[string]any)
	if !ok || modules["main"] == nil {
		t.Errorf("default_selected_modules not copied from template: %#v", ec["default_selected_modules"])
	}
}

// TestProjectHandler_CreateEcommerceWithoutTemplateLeavesDefaultsEmpty guards
// the opposite direction: a project created without a template must NOT inherit
// any defaults (no spurious population).
func TestProjectHandler_CreateEcommerceWithoutTemplateLeavesDefaultsEmpty(t *testing.T) {
	app, _, _ := setupProjectEcommerceTest(t)
	owner := uuid.New().String()

	resp := doRequest(t, app, "POST", "/api/v1/projects", owner, map[string]any{
		"platform": "ecommerce",
		"name":     "无模板店铺",
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	data, _ := decodeBody(t, resp)["data"].(map[string]any)
	if data == nil {
		t.Fatalf("missing data envelope")
	}
	project, _ := data["project"].(map[string]any)
	if ec, ok := project["ecommerce_defaults"].(map[string]any); ok && len(ec) > 0 {
		t.Errorf("ecommerce_defaults must be empty without a template: %#v", ec)
	}
}
