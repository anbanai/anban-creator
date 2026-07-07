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

	srvconfig "github.com/anbanai/anban-creator/server/config"
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
	if body.Data.Balance != 120_000 || body.Data.MinBalance != 0 || !body.Data.MeetsMinBalance {
		t.Fatalf("balance gate = %+v", body.Data)
	}
}

func TestVideoEstimateAppliesBillingTierAndUserMultiplier(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	multiplier := 0.5
	if err := repo.Users().Create(ctx, &model.User{
		ID:                userID,
		Email:             "video-estimate-billing@example.com",
		Password:          "hashed",
		InviteCode:        "videoestimatebilling",
		Tier:              model.TierFree,
		CreditsBalance:    120_000,
		BillingMultiplier: &multiplier,
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
		AllowedModels: []string{"configured-video"},
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
			NoInputPricePerSecond: map[string]float64{
				"720p": 1,
			},
		},
	}, 1000, &logger)
	h.SetBillingConfig(srvconfig.BillingConfig{
		CreditsPerCNY:         1600,
		TierMultipliers:       map[string]float64{"free": 1.35},
		DefaultUserMultiplier: 1,
		MinimumChargeCredits:  1,
	})

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
			EstimatedCredits int `json:"estimated_credits"`
			PricingBreakdown struct {
				CreditsPerCNY  int     `json:"credits_per_cny"`
				TierMultiplier float64 `json:"tier_multiplier"`
				UserMultiplier float64 `json:"user_multiplier"`
			} `json:"pricing_breakdown"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.EstimatedCredits != 5400 {
		t.Fatalf("estimated credits = %d, want ceil(5*1600*1.35*0.5)=5400", body.Data.EstimatedCredits)
	}
	if body.Data.PricingBreakdown.CreditsPerCNY != 1600 || body.Data.PricingBreakdown.TierMultiplier != 1.35 || body.Data.PricingBreakdown.UserMultiplier != 0.5 {
		t.Fatalf("pricing breakdown = %+v, want configured billing multipliers", body.Data.PricingBreakdown)
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

func TestVideoPlaybooksReturnsSeedanceBusinessScenarios(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewVideoHandler(nil, nil, nil, 1000, &logger)

	app := fiber.New()
	app.Get("/video/playbooks", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Playbooks(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/video/playbooks", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			Items []struct {
				Key                    string   `json:"key"`
				Label                  string   `json:"label"`
				CreativeType           string   `json:"creative_type"`
				Purpose                string   `json:"purpose"`
				RequiredReferenceRoles []string `json:"required_reference_roles"`
				DefaultRatio           string   `json:"default_ratio"`
				PromptScaffold         string   `json:"prompt_scaffold"`
				QCFocus                []string `json:"qc_focus"`
				RiskNotes              []string `json:"risk_notes"`
				AgentBrief             string   `json:"agent_brief"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Items) < 13 {
		t.Fatalf("playbooks count = %d, want business and style scenarios", len(body.Data.Items))
	}
	foundLiveSelling := false
	foundCinematic := false
	for _, item := range body.Data.Items {
		switch item.Key {
		case "live_selling":
			foundLiveSelling = true
			if item.CreativeType != service.VideoCreativeTypeProductDemo || item.Purpose != service.VideoPurposeEcommerce {
				t.Fatalf("live_selling routing = %+v", item)
			}
			if item.DefaultRatio != "9:16" || len(item.RequiredReferenceRoles) == 0 || item.PromptScaffold == "" || len(item.QCFocus) == 0 || len(item.RiskNotes) == 0 || item.AgentBrief == "" {
				t.Fatalf("live_selling playbook lacks production guidance: %+v", item)
			}
		case "cinematic":
			foundCinematic = true
			if item.PromptScaffold == "" || len(item.QCFocus) == 0 {
				t.Fatalf("cinematic playbook lacks guidance: %+v", item)
			}
		}
	}
	if !foundLiveSelling || !foundCinematic {
		t.Fatalf("expected live_selling and cinematic playbooks, got %+v", body.Data.Items)
	}
}

func TestVideoEstimateReturnsProductionGuidance(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:             userID,
		Email:          "video-production-estimate@example.com",
		Password:       "hashed",
		InviteCode:     "videoproductionestimate",
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
		Purpose:    service.VideoPurposeEcommerce,
		ModelKey:   "configured-video",
		Resolution: "720p",
		Ratio:      "9:16",
		Duration:   16,
		Preflight:  true,
	})
	project.SetVideoModelPolicy(model.VideoModelPolicy{
		AllowedModels: []string{"configured-video"},
		DefaultModel:  "configured-video",
		MaxResolution: "720p",
		MaxDuration:   45,
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

	body := `{
		"project_id":"` + projectID + `",
		"prompt":"生成一条直播带货口播视频",
		"video_config":{
			"scenario_key":"live_selling",
			"production_mode":"guided",
			"retake_budget":5,
			"references":[
				{"type":"image_url","url":"https://cdn.example.com/product.png","reference_role":"product appearance"}
			]
		}
	}`
	req := httptest.NewRequest("POST", "/video/estimate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var decoded struct {
		Data struct {
			MissingReferenceRoles []string `json:"missing_reference_roles"`
			ExpectedArtifacts     []string `json:"expected_artifacts"`
			AffordableTakes       int      `json:"affordable_takes"`
			SegmentPlan           []struct {
				Index    int   `json:"index"`
				Duration int64 `json:"duration"`
			} `json:"segment_plan"`
			ResolvedConfig struct {
				ScenarioKey    string `json:"scenario_key"`
				ProductionMode string `json:"production_mode"`
				RetakeBudget   int    `json:"retake_budget"`
			} `json:"resolved_config"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Data.ResolvedConfig.ScenarioKey != "live_selling" || decoded.Data.ResolvedConfig.ProductionMode != "guided" || decoded.Data.ResolvedConfig.RetakeBudget != 5 {
		t.Fatalf("resolved production config = %+v", decoded.Data.ResolvedConfig)
	}
	if len(decoded.Data.SegmentPlan) != 2 || decoded.Data.SegmentPlan[0].Duration != 8 || decoded.Data.SegmentPlan[1].Duration != 8 {
		t.Fatalf("segment plan = %+v, want two 8s segments", decoded.Data.SegmentPlan)
	}
	if !containsString(decoded.Data.MissingReferenceRoles, "action") || !containsString(decoded.Data.MissingReferenceRoles, "voice tone") {
		t.Fatalf("missing reference roles = %+v", decoded.Data.MissingReferenceRoles)
	}
	if !containsString(decoded.Data.ExpectedArtifacts, "quality-review.md") || !containsString(decoded.Data.ExpectedArtifacts, "delivery-manifest.json") {
		t.Fatalf("expected artifacts = %+v", decoded.Data.ExpectedArtifacts)
	}
	if decoded.Data.AffordableTakes != 5 {
		t.Fatalf("affordable takes = %d, want capped retake budget 5", decoded.Data.AffordableTakes)
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
