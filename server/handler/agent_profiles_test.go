package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAgentProfileHandlerListsAllProfilesWithoutSecrets(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	userID := uuid.NewString()
	if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Tier: model.TierPro}); err != nil {
		t.Fatal(err)
	}
	registry, err := service.NewAgentProfileRegistry([]service.AgentExecutionProfile{
		{ID: "cost_effective", DisplayName: "性价比", ModelName: "DeepSeek 4 Pro", Description: "低成本", Provider: "deepseek", ModelID: "deepseek-v4-pro", Protocol: "anthropic", BaseURL: "https://deepseek.example.com", AuthToken: "secret-deepseek", MinTier: model.TierFree, Available: true},
		{ID: "balanced", DisplayName: "平衡型", ModelName: "豆包", Description: "平衡", Provider: "volcengine_ark", ModelID: "doubao-seed-evolving", Protocol: "anthropic", BaseURL: "https://ark.example.com", AuthToken: "secret-ark", MinTier: model.TierPro, Available: true},
		{ID: "maximum_quality", DisplayName: "极致效果", ModelName: "Kimi K3（1M）", Description: "旗舰", Provider: "kimi", ModelID: "k3", Protocol: "anthropic", BaseURL: "https://api.kimi.com/coding/", AuthToken: "secret-kimi", MinTier: model.TierEnterprise, ContextWindow: 1048576, ReasoningEffort: "high", ThinkingRequired: true, Available: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewAgentProfileHandler(repo, registry, &logger)
	app := fiber.New()
	app.Get("/agent/execution-profiles", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.List(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/agent/execution-profiles", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.StatusCode, raw)
	}
	for _, forbidden := range []string{"secret-kimi", "secret-ark", "secret-deepseek", "base_url", "auth_token", "protocol", "provider"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, raw)
		}
	}
	var envelope struct {
		Data []service.AgentProfileCapability `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Data) != 3 || !envelope.Data[1].Available || envelope.Data[2].Available || envelope.Data[2].UnavailableReason != "requires_enterprise" {
		t.Fatalf("capabilities = %#v", envelope.Data)
	}
}
