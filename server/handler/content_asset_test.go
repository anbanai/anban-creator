package handler

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func TestMergeContentAutopilotPolicyPreservesOmittedFields(t *testing.T) {
	existing := &model.ContentAutopilotPolicy{
		UserID: "user-1", Enabled: true, DraftEnabled: true, PublishEnabled: true,
		ProjectIDs: []byte(`["project-1"]`), Platforms: []byte(`["wechat"]`),
		PublishWindows:     []byte(`[{"start":"09:00","end":"21:00","timezone":"Asia/Shanghai"}]`),
		DailyBudgetCredits: 50, MaxRunCostCredits: 20, MinRecommendationScore: 70,
		MinVideoConfidence: 80, ComplianceVersion: "v1", FailureMode: model.ContentAutopilotFailurePause,
	}
	request := updateContentAutopilotPolicyRequest{DailyBudgetCredits: int64Ptr(100)}
	merged := mergeContentAutopilotPolicy(existing, request)
	if !merged.Enabled || !merged.DraftEnabled || !merged.PublishEnabled || merged.DailyBudgetCredits != 100 ||
		string(merged.ProjectIDs) != string(existing.ProjectIDs) || string(merged.PublishWindows) != string(existing.PublishWindows) ||
		merged.ComplianceVersion != "v1" {
		t.Fatalf("merged policy = %#v", merged)
	}
}

func int64Ptr(value int64) *int64 { return &value }

func TestContentAssetHandlerRequiresAuthAndCreatesAsset(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	h := NewContentAssetHandler(service.NewContentAssetService(repo), nil)
	app := fiber.New()
	app.Post("/content-assets", func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return h.Create(c) })
	app.Get("/content-assets", func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return h.List(c) })
	unauth, _ := app.Test(httptest.NewRequest("POST", "/content-assets", nil))
	if unauth.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauth.StatusCode)
	}
	body, _ := json.Marshal(map[string]string{"title": "一个可复用选题"})
	req := httptest.NewRequest("POST", "/content-assets", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "user-1")
	resp, err := app.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("create response = %d %v", resp.StatusCode, err)
	}
	listReq := httptest.NewRequest("GET", "/content-assets", nil)
	listReq.Header.Set("X-User-ID", "user-1")
	list, err := app.Test(listReq)
	if err != nil || list.StatusCode != fiber.StatusOK {
		t.Fatalf("list response = %d %v", list.StatusCode, err)
	}
}
