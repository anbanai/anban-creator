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
		handlerTestProfile("cost_effective", "性价比", "低成本", "deepseek", "deepseek-v4-pro", model.TierFree),
		handlerTestProfile("balanced", "平衡型", "平衡", "volcengine_ark", "doubao-seed-evolving", model.TierPro),
		handlerTestProfile("maximum_quality", "极致效果", "旗舰", "moonshot", "kimi-k3[1m]", model.TierEnterprise),
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
	for _, forbidden := range []string{"test-secret", ".example/anthropic", "base_url", "auth_token", "runtime_env", "model_name", "model_id"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("response leaked %q: %s", forbidden, raw)
		}
	}
	for _, required := range []string{`"provider"`, `"protocol":"anthropic"`, `"models"`, `"claude"`} {
		if !strings.Contains(string(raw), required) {
			t.Fatalf("response omitted %q: %s", required, raw)
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

func handlerTestProfile(id, displayName, description, provider, modelID string, minTier model.Tier) service.AgentExecutionProfile {
	return service.AgentExecutionProfile{
		ID: id, DisplayName: displayName, Description: description, Provider: provider, Protocol: "anthropic",
		Models:            model.AgentModelMatrix{Default: modelID, Opus: modelID, Fable: modelID, Sonnet: modelID, Haiku: modelID},
		ModelUsageAliases: map[string]string{modelID: modelID}, BaseURL: "https://" + provider + ".example/anthropic",
		AuthToken: "test-secret", MinTier: minTier, Available: true,
	}
}
