package handler

import (
	"sort"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

type VideoHandler struct {
	repo             repository.Repository
	creditSvc        *service.CreditService
	catalog          service.VideoModelCatalog
	creditMultiplier int
	logger           *zerolog.Logger
}

func NewVideoHandler(repo repository.Repository, creditSvc *service.CreditService, catalog service.VideoModelCatalog, creditMultiplier int, logger *zerolog.Logger) *VideoHandler {
	if catalog == nil {
		catalog = service.VideoModelCatalog{}
	}
	if creditMultiplier <= 0 {
		creditMultiplier = 1000
	}
	return &VideoHandler{
		repo:             repo,
		creditSvc:        creditSvc,
		catalog:          catalog,
		creditMultiplier: creditMultiplier,
		logger:           logger,
	}
}

type videoEstimateRequest struct {
	ProjectID   string                 `json:"project_id"`
	Prompt      string                 `json:"prompt"`
	VideoConfig *model.VideoTaskConfig `json:"video_config,omitempty"`
}

type videoEstimateResponse struct {
	AvailableModels  []service.VideoModelSpec     `json:"available_models"`
	ResolvedConfig   model.VideoTaskConfig        `json:"resolved_config"`
	EstimatedCredits int                          `json:"estimated_credits"`
	PricingBreakdown *model.VideoPricingBreakdown `json:"pricing_breakdown,omitempty"`
	Balance          int                          `json:"balance"`
	MinBalance       int                          `json:"min_balance"`
	MeetsMinBalance  bool                         `json:"meets_min_balance"`
	Warnings         []string                     `json:"warnings,omitempty"`
}

func (h *VideoHandler) Estimate(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req videoEstimateRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}
	project, err := h.repo.Projects().FindByID(c.Context(), req.ProjectID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "project not found")
	}
	if project.UserID != userID {
		return Forbidden(c, "you do not have access to this project")
	}
	if project.Platform != model.PlatformVideo {
		return Error(c, fiber.StatusBadRequest, "project is not a video project")
	}

	policy := project.VideoModelPolicy.Data()
	available, warnings := h.availableModels(policy)
	plan, err := service.ResolveVideoGenerationPlan(
		videoGenerationRequestFromConfig(req.Prompt, req.VideoConfig),
		project.VideoDefaults.Data(),
		policy,
		h.catalog,
		h.creditMultiplier,
	)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	balance := 0
	if h.creditSvc != nil {
		balance, err = h.creditSvc.GetBalance(c.Context(), userID)
		if err != nil {
			if h.logger != nil {
				h.logger.Error().Err(err).Str("user_id", userID).Msg("video estimate balance lookup failed")
			}
			return Error(c, fiber.StatusInternalServerError, "failed to get credit balance")
		}
	}
	return Success(c, videoEstimateResponse{
		AvailableModels:  available,
		ResolvedConfig:   videoTaskConfigFromGenerationPlan(plan),
		EstimatedCredits: plan.EstimatedCredits,
		PricingBreakdown: plan.PricingBreakdown,
		Balance:          balance,
		MinBalance:       service.MinVideoCreationBalance,
		MeetsMinBalance:  balance >= service.MinVideoCreationBalance,
		Warnings:         warnings,
	})
}

func (h *VideoHandler) Models(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	models := make([]service.VideoModelSpec, 0, len(h.catalog))
	for _, spec := range h.catalog {
		models = append(models, spec)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Key < models[j].Key })
	return Success(c, fiber.Map{"items": models})
}

func (h *VideoHandler) availableModels(policy model.VideoModelPolicy) ([]service.VideoModelSpec, []string) {
	models := make([]service.VideoModelSpec, 0, len(policy.AllowedModels))
	warnings := []string{}
	for _, key := range policy.AllowedModels {
		spec, ok := h.catalog[key]
		if !ok {
			warnings = append(warnings, "video model "+key+" is not configured or unavailable")
			continue
		}
		models = append(models, spec)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Key < models[j].Key })
	return models, warnings
}

func videoGenerationRequestFromConfig(prompt string, cfg *model.VideoTaskConfig) service.VideoGenerationRequest {
	req := service.VideoGenerationRequest{Prompt: prompt}
	if cfg == nil {
		return req
	}
	req.Purpose = cfg.Purpose
	req.Model = cfg.ModelKey
	req.Resolution = cfg.Resolution
	req.Ratio = cfg.Ratio
	req.Duration = cfg.Duration
	req.Watermark = cfg.Watermark
	req.Preflight = &cfg.Preflight
	for _, asset := range cfg.References {
		req.ReferenceSet = append(req.ReferenceSet, service.VideoReferenceInput{
			Type:                 asset.Type,
			URL:                  asset.URL,
			Text:                 asset.Text,
			ReferenceRole:        asset.ReferenceRole,
			InputDurationSeconds: asset.InputDurationSeconds,
		})
	}
	return req
}

func videoTaskConfigFromGenerationPlan(plan service.VideoGenerationPlan) model.VideoTaskConfig {
	cfg := model.VideoTaskConfig{
		Purpose:          plan.Purpose,
		ModelKey:         plan.ModelKey,
		Model:            plan.Model,
		Resolution:       plan.Resolution,
		Ratio:            plan.Ratio,
		Duration:         plan.Duration,
		Watermark:        plan.Watermark,
		Preflight:        plan.Preflight,
		EstimatedCredits: plan.EstimatedCredits,
		PricingBreakdown: plan.PricingBreakdown,
	}
	for _, ref := range plan.References {
		cfg.References = append(cfg.References, model.VideoReferenceAsset{
			Type:                 ref.Type,
			URL:                  ref.URL,
			Text:                 ref.Text,
			ReferenceRole:        ref.ReferenceRole,
			InputDurationSeconds: ref.InputDurationSeconds,
		})
	}
	return cfg
}
