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

func TestVideoCreatorEstimateReturnsConfiguredAllowedModels(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "video-estimate@example.com",
		Password:   "hashed",
		InviteCode: "videoestimate",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := uuid.New().String()
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
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
	h := NewVideoHandler(repo, service.VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			DisplayName:          "Configured Video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
		},
	}, &logger)

	app := fiber.New()
	app.Post("/videocreator/estimate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Estimate(c)
	})

	req := httptest.NewRequest("POST", "/videocreator/estimate", strings.NewReader(`{"project_id":"`+projectID+`","prompt":"生成视频"}`))
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
			ResolvedCreatorConfig struct {
				ModelKey string `json:"model_key"`
			} `json:"resolved_creator_config"`
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
	if body.Data.ResolvedCreatorConfig.ModelKey != "configured-video" {
		t.Fatalf("estimate = %+v", body.Data)
	}

	legacyReq := httptest.NewRequest("POST", "/videocreator/estimate", strings.NewReader(`{"project_id":"`+projectID+`","video_config":{"duration":9}}`))
	legacyReq.Header.Set("Content-Type", "application/json")
	legacyResp, err := app.Test(legacyReq)
	if err != nil {
		t.Fatalf("legacy request failed: %v", err)
	}
	if legacyResp.StatusCode != fiber.StatusBadRequest {
		raw, _ := io.ReadAll(legacyResp.Body)
		t.Fatalf("legacy status = %d, want 400 body=%s", legacyResp.StatusCode, raw)
	}

	editorReq := httptest.NewRequest("POST", "/videocreator/estimate", strings.NewReader(`{"project_id":"`+projectID+`","video_editor_config":{"duration":9}}`))
	editorReq.Header.Set("Content-Type", "application/json")
	editorResp, err := app.Test(editorReq)
	if err != nil {
		t.Fatalf("editor request failed: %v", err)
	}
	if editorResp.StatusCode != fiber.StatusBadRequest {
		raw, _ := io.ReadAll(editorResp.Body)
		t.Fatalf("editor status = %d, want 400 body=%s", editorResp.StatusCode, raw)
	}
}

func TestVideoCreatorEstimateDoesNotExposeDynamicRetailPricing(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID: userID, Email: "video-estimate-billing@example.com", Password: "hashed",
		InviteCode: "videoestimatebilling", Tier: model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := uuid.New().String()
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
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
	h := NewVideoHandler(repo, service.VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			DisplayName:          "Configured Video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
		},
	}, &logger)

	app := fiber.New()
	app.Post("/videocreator/estimate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Estimate(c)
	})

	req := httptest.NewRequest("POST", "/videocreator/estimate", strings.NewReader(`{"project_id":"`+projectID+`","prompt":"生成视频"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"estimated_credits", "pricing_breakdown", "tier_multiplier", "user_multiplier", `"balance"`} {
		if strings.Contains(string(raw), removed) {
			t.Fatalf("response leaked removed dynamic retail field %q: %s", removed, raw)
		}
	}
}

func TestVideoCreatorEstimateAllowsEmptyPromptForConfigurationPreview(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "video-empty-estimate@example.com",
		Password:   "hashed",
		InviteCode: "videoemptyestimate",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := uuid.New().String()
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
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
	h := NewVideoHandler(repo, service.VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			DisplayName:          "Configured Video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
		},
	}, &logger)

	app := fiber.New()
	app.Post("/videocreator/estimate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Estimate(c)
	})

	req := httptest.NewRequest("POST", "/videocreator/estimate", strings.NewReader(`{"project_id":"`+projectID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestVideoCreatorPlaybooksReturnsSeedanceBusinessScenarios(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewVideoHandler(nil, nil, &logger)

	app := fiber.New()
	app.Get("/videocreator/playbooks", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Playbooks(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/videocreator/playbooks", nil))
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

func TestVideoCreatorEstimateReturnsProductionGuidance(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "video-production-estimate@example.com",
		Password:   "hashed",
		InviteCode: "videoproductionestimate",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := uuid.New().String()
	project := &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
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
	h := NewVideoHandler(repo, service.VideoModelCatalog{
		"configured-video": {
			Key:                  "configured-video",
			DisplayName:          "Configured Video",
			ModelID:              "provider-configured-video",
			SupportedResolutions: []string{"720p"},
			SupportedRatios:      []string{"9:16"},
			MinDuration:          1,
			MaxDuration:          15,
			SupportsVideoInput:   true,
		},
	}, &logger)

	app := fiber.New()
	app.Post("/videocreator/estimate", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Estimate(c)
	})

	body := `{
		"project_id":"` + projectID + `",
		"prompt":"生成一条直播带货口播视频",
		"video_creator_config":{
			"scenario_key":"live_selling",
			"production_mode":"guided",
			"retake_budget":5,
			"references":[
				{"type":"image_url","url":"https://cdn.example.com/product.png","reference_role":"product appearance"}
			]
		}
	}`
	req := httptest.NewRequest("POST", "/videocreator/estimate", strings.NewReader(body))
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
			SegmentPlan           []struct {
				Index    int   `json:"index"`
				Duration int64 `json:"duration"`
			} `json:"segment_plan"`
			ResolvedCreatorConfig struct {
				ScenarioKey    string `json:"scenario_key"`
				ProductionMode string `json:"production_mode"`
				RetakeBudget   int    `json:"retake_budget"`
			} `json:"resolved_creator_config"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if decoded.Data.ResolvedCreatorConfig.ScenarioKey != "live_selling" || decoded.Data.ResolvedCreatorConfig.ProductionMode != "guided" || decoded.Data.ResolvedCreatorConfig.RetakeBudget != 5 {
		t.Fatalf("resolved production config = %+v", decoded.Data.ResolvedCreatorConfig)
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
}

func TestVideoCreatorModelsReturnsOnlyConfiguredCatalog(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewVideoHandler(nil, service.VideoModelCatalog{
		"configured-video": {
			Key:         "configured-video",
			DisplayName: "Configured Video",
			ModelID:     "provider-configured-video",
		},
	}, &logger)

	app := fiber.New()
	app.Get("/videocreator/models", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Models(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/videocreator/models", nil))
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

func TestVideoCreatorModelsReturnsEmptyWhenCatalogUnconfigured(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewVideoHandler(nil, nil, &logger)

	app := fiber.New()
	app.Get("/videocreator/models", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Models(c)
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/videocreator/models", nil))
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
