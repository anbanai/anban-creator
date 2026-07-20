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
	repo    repository.Repository
	catalog service.VideoModelCatalog
	logger  *zerolog.Logger
}

func NewVideoHandler(repo repository.Repository, catalog service.VideoModelCatalog, logger *zerolog.Logger) *VideoHandler {
	if catalog == nil {
		catalog = service.VideoModelCatalog{}
	}
	return &VideoHandler{
		repo:    repo,
		catalog: catalog,
		logger:  logger,
	}
}

type videoEstimateRequest struct {
	ProjectID          string                 `json:"project_id"`
	Prompt             string                 `json:"prompt"`
	VideoCreatorConfig *model.VideoTaskConfig `json:"video_creator_config,omitempty"`
}

type videoEstimateResponse struct {
	AvailableModels       []service.VideoModelSpec       `json:"available_models"`
	ResolvedCreatorConfig model.VideoTaskConfig          `json:"resolved_creator_config"`
	Warnings              []string                       `json:"warnings,omitempty"`
	MissingReferenceRoles []string                       `json:"missing_reference_roles,omitempty"`
	ExpectedArtifacts     []string                       `json:"expected_artifacts,omitempty"`
	SegmentPlan           []model.VideoTaskSegmentConfig `json:"segment_plan,omitempty"`
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
	if err := rejectVideoCreatorOnlyFields(c.Body()); err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
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
	if !model.IsVideoCreatorPlatform(project.Platform) {
		return Error(c, fiber.StatusBadRequest, "project is not a video creator project")
	}

	policy := project.VideoModelPolicy.Data()
	available, warnings := h.availableModels(policy)
	plan, err := service.ResolveVideoGenerationPlan(
		videoGenerationRequestFromConfig(req.Prompt, req.VideoCreatorConfig),
		project.VideoDefaults.Data(),
		policy,
		h.catalog,
	)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, videoEstimateResponse{
		AvailableModels:       available,
		ResolvedCreatorConfig: videoTaskConfigFromGenerationPlan(plan),
		Warnings:              warnings,
		MissingReferenceRoles: videoMissingReferenceRoles(plan),
		ExpectedArtifacts:     service.VideoProductionArtifactNames(),
		SegmentPlan:           videoTaskSegmentsFromPlan(plan.Segments),
	})
}

func (h *VideoHandler) Playbooks(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	return Success(c, fiber.Map{"items": service.DefaultVideoPlaybooks()})
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
	req.ScenarioKey = cfg.ScenarioKey
	req.ProductionMode = cfg.ProductionMode
	req.Purpose = cfg.Purpose
	req.CreativeType = cfg.CreativeType
	req.SubjectProfile = cfg.SubjectProfile
	req.Audience = cfg.Audience
	req.SingleMessage = cfg.SingleMessage
	req.Model = cfg.ModelKey
	req.Resolution = cfg.Resolution
	req.Ratio = cfg.Ratio
	req.Duration = cfg.Duration
	req.Watermark = cfg.Watermark
	req.Preflight = &cfg.Preflight
	req.RetakeBudget = cfg.RetakeBudget
	req.DeliveryTargets = cfg.DeliveryTargets
	for _, asset := range cfg.References {
		req.ReferenceSet = append(req.ReferenceSet, service.VideoReferenceInput{
			Type:                 asset.Type,
			URL:                  asset.URL,
			Text:                 asset.Text,
			ReferenceRole:        asset.ReferenceRole,
			MustKeep:             asset.MustKeep,
			CanChange:            asset.CanChange,
			MustNotTransfer:      asset.MustNotTransfer,
			InputDurationSeconds: asset.InputDurationSeconds,
		})
	}
	return req
}

func videoTaskConfigFromGenerationPlan(plan service.VideoGenerationPlan) model.VideoTaskConfig {
	cfg := model.VideoTaskConfig{
		ScenarioKey:     plan.ScenarioKey,
		ProductionMode:  plan.ProductionMode,
		Purpose:         plan.Purpose,
		CreativeType:    plan.CreativeType,
		SubjectProfile:  plan.SubjectProfile,
		Audience:        plan.Audience,
		SingleMessage:   plan.SingleMessage,
		ModelKey:        plan.ModelKey,
		Model:           plan.Model,
		Resolution:      plan.Resolution,
		Ratio:           plan.Ratio,
		Duration:        plan.Duration,
		Watermark:       plan.Watermark,
		Preflight:       plan.Preflight,
		RetakeBudget:    plan.RetakeBudget,
		DeliveryTargets: plan.DeliveryTargets,
	}
	for _, ref := range plan.References {
		cfg.References = append(cfg.References, model.VideoReferenceAsset{
			Type:                 ref.Type,
			URL:                  ref.URL,
			Text:                 ref.Text,
			ReferenceRole:        ref.ReferenceRole,
			MustKeep:             ref.MustKeep,
			CanChange:            ref.CanChange,
			MustNotTransfer:      ref.MustNotTransfer,
			InputDurationSeconds: ref.InputDurationSeconds,
		})
	}
	return cfg
}

func videoMissingReferenceRoles(plan service.VideoGenerationPlan) []string {
	playbook, ok := service.FindVideoPlaybook(plan.ScenarioKey)
	if !ok {
		return nil
	}
	return service.MissingVideoReferenceRoles(plan.References, playbook)
}

func videoTaskSegmentsFromPlan(segments []service.VideoGenerationSegmentPlan) []model.VideoTaskSegmentConfig {
	result := make([]model.VideoTaskSegmentConfig, 0, len(segments))
	for _, seg := range segments {
		result = append(result, model.VideoTaskSegmentConfig{
			Index:       seg.Index,
			StartSecond: seg.StartSecond,
			EndSecond:   seg.EndSecond,
			Duration:    seg.Duration,
			Prompt:      seg.Prompt,
			ModelKey:    seg.ModelKey,
			Model:       seg.Model,
			Resolution:  seg.Resolution,
			Ratio:       seg.Ratio,
		})
	}
	return result
}
