package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func TestVideoEstimateReturnsConfiguredAllowedModelsAndBalanceGate(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:             userID,
		Email:          "video-estimate@example.com",
		Password:       "hashed",
		InviteCode:     "videoestimate",
		CreditsBalance: 120_000,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := uuid.New().String()
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "Video",
		Status:   model.ProjectStatusActive,
	}
	watermark := false
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    service.VideoPurposePlanting,
		ModelKey:   "configured-video",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Watermark:  &watermark,
		Preflight:  true,
	})
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"configured-video", "missing-history-model"},
		DefaultModel:  "configured-video",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	creditSvc := service.NewCreditService(repo, nil, &logger)
	h := NewVideoHandler(repo, creditSvc, service.VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			DisplayName:          "Configured Video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
			NoInputPricePerSecond: map[string]float64{
				"720p": 1,
			},
			VideoInput5sMinPrice: map[string]float64{"720p": 5},
			VideoInput5sMaxPrice: map[string]float64{"720p": 10},
		},
	}, 1000, &logger)

	app := fiber.New()
	app.Post("/video/estimate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Estimate(c)
	})

	req := httptest.NewRequest("POST", "/video/estimate", strings.NewReader(`{"project_id":"`+projectID+`","prompt":"生成视频"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			AvailableModels []struct {
				Key string `json:"key"`
			} `json:"available_models"`
			EstimatedCredits int  `json:"estimated_credits"`
			Balance          int  `json:"balance"`
			MinBalance       int  `json:"min_balance"`
			MeetsMinBalance  bool `json:"meets_min_balance"`
			ResolvedConfig   struct {
				ModelKey string `json:"model_key"`
			} `json:"resolved_config"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := len(body.Data.AvailableModels); got != 1 {
		t.Fatalf("available models = %+v, want exactly configured allowed model", body.Data.AvailableModels)
	}
	if body.Data.AvailableModels[0].Key != "configured-video" {
		t.Fatalf("available model = %+v", body.Data.AvailableModels[0])
	}
	if body.Data.ResolvedConfig.ModelKey != "configured-video" || body.Data.EstimatedCredits != 5000 {
		t.Fatalf("estimate = %+v", body.Data)
	}
	if body.Data.Balance != 120_000 || body.Data.MinBalance != service.MinVideoCreationBalance || !body.Data.MeetsMinBalance {
		t.Fatalf("balance gate = %+v", body.Data)
	}
}

func TestVideoEstimateAllowsEmptyPromptForConfigurationPreview(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:             userID,
		Email:          "video-empty-estimate@example.com",
		Password:       "hashed",
		InviteCode:     "videoemptyestimate",
		CreditsBalance: 120_000,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := uuid.New().String()
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideo,
		Name:     "Video",
		Status:   model.ProjectStatusActive,
	}
	project.SetVideoDefaults(model.VideoDefaults{
		Purpose:    service.VideoPurposePlanting,
		ModelKey:   "configured-video",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   5,
		Preflight:  true,
	})
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"configured-video"},
		DefaultModel:  "configured-video",
		MaxResolution: "720p",
		MaxDuration:   15,
	})
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewVideoHandler(repo, service.NewCreditService(repo, nil, &logger), service.VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			DisplayName:          "Configured Video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
			NoInputPricePerSecond: map[string]float64{
				"720p": 1,
			},
		},
	}, 1000, &logger)

	app := fiber.New()
	app.Post("/video/estimate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Estimate(c)
	})

	req := httptest.NewRequest("POST", "/video/estimate", strings.NewReader(`{"project_id":"`+projectID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestVideoModelsReturnsOnlyConfiguredCatalog(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewVideoHandler(nil, nil, service.VideoModelCatalog{
		"configured-video": {
			Key:         "configured-video",
			DisplayName: "Configured Video",
			ModelID:     "provider-configured-video",
		},
	}, 1000, &logger)

	app := fiber.New()
	app.Get("/video/models", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Models(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/video/models", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			Items []struct {
				Key string `json:"key"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].Key != "configured-video" {
		t.Fatalf("items = %+v, want only configured-video", body.Data.Items)
	}
}

func TestVideoModelsReturnsEmptyWhenCatalogUnconfigured(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewVideoHandler(nil, nil, nil, 1000, &logger)

	app := fiber.New()
	app.Get("/video/models", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Models(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/video/models", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			Items []struct {
				Key string `json:"key"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) != 0 {
		t.Fatalf("items = %+v, want no models for unconfigured catalog", body.Data.Items)
	}
}
