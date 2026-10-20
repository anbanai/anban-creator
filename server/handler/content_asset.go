package handler

import (
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

type ContentAssetHandler struct {
	service *service.ContentAssetService
	logger  *zerolog.Logger
}

func NewContentAssetHandler(svc *service.ContentAssetService, logger *zerolog.Logger) *ContentAssetHandler {
	return &ContentAssetHandler{service: svc, logger: logger}
}

type createContentAssetRequest struct {
	Title          string `json:"title"`
	CoreQuestion   string `json:"core_question"`
	Audience       string `json:"audience"`
	SemanticKernel string `json:"semantic_kernel"`
	Lifecycle      string `json:"lifecycle"`
	Status         string `json:"status"`
}

type ingestContentAssetRequest struct {
	createContentAssetRequest
	Evidence []model.ContentAssetEvidence `json:"evidence"`
}

type updateContentAssetRequest struct {
	Title          *string `json:"title"`
	CoreQuestion   *string `json:"core_question"`
	Audience       *string `json:"audience"`
	SemanticKernel *string `json:"semantic_kernel"`
	Lifecycle      *string `json:"lifecycle"`
	Status         *string `json:"status"`
}

type createReuseRunRequest struct {
	SourceContentKind string         `json:"source_content_kind"`
	SourceContentID   string         `json:"source_content_id"`
	TargetProjectID   string         `json:"target_project_id"`
	TargetPlatform    string         `json:"target_platform"`
	TargetAgentID     string         `json:"target_agent_id"`
	IdempotencyKey    string         `json:"idempotency_key"`
	ScoreSnapshot     map[string]any `json:"score_snapshot"`
	PolicySnapshot    map[string]any `json:"policy_snapshot"`
}

type linkContentAssetRequest struct {
	ProjectID   string `json:"project_id"`
	ContentKind string `json:"content_kind"`
	ContentID   string `json:"content_id"`
	Platform    string `json:"platform"`
	Relation    string `json:"relation"`
	Angle       string `json:"angle"`
}

type scoreContentAssetRequest struct {
	ProjectID string `json:"project_id"`
}

type updateContentAutopilotPolicyRequest struct {
	Enabled                         *bool           `json:"enabled"`
	ProjectIDs                      json.RawMessage `json:"project_ids"`
	Platforms                       json.RawMessage `json:"platforms"`
	DailyBudgetCredits              *int64          `json:"daily_budget_credits"`
	MaxRunCostCredits               *int64          `json:"max_run_cost_credits"`
	MinRecommendationScore          *int            `json:"min_recommendation_score"`
	PlatformMinRecommendationScores json.RawMessage `json:"platform_min_recommendation_scores"`
	MinVideoConfidence              *int            `json:"min_video_confidence"`
	VideoFirst                      *bool           `json:"video_first"`
	PublishWindows                  json.RawMessage `json:"publish_windows"`
	DraftEnabled                    *bool           `json:"draft_enabled"`
	PublishEnabled                  *bool           `json:"publish_enabled"`
	ComplianceVersion               *string         `json:"compliance_version"`
	FailureMode                     *string         `json:"failure_mode"`
}

func mergeContentAutopilotPolicy(existing *model.ContentAutopilotPolicy, request updateContentAutopilotPolicyRequest) model.ContentAutopilotPolicy {
	merged := model.ContentAutopilotPolicy{}
	if existing != nil {
		merged = *existing
		merged.ProjectIDs = append([]byte(nil), existing.ProjectIDs...)
		merged.Platforms = append([]byte(nil), existing.Platforms...)
		merged.PublishWindows = append([]byte(nil), existing.PublishWindows...)
		merged.PlatformMinRecommendationScores = append([]byte(nil), existing.PlatformMinRecommendationScores...)
	}
	if request.Enabled != nil {
		merged.Enabled = *request.Enabled
	}
	if request.ProjectIDs != nil {
		merged.ProjectIDs = append([]byte(nil), request.ProjectIDs...)
	}
	if request.Platforms != nil {
		merged.Platforms = append([]byte(nil), request.Platforms...)
	}
	if request.DailyBudgetCredits != nil {
		merged.DailyBudgetCredits = *request.DailyBudgetCredits
	}
	if request.MaxRunCostCredits != nil {
		merged.MaxRunCostCredits = *request.MaxRunCostCredits
	}
	if request.MinRecommendationScore != nil {
		merged.MinRecommendationScore = *request.MinRecommendationScore
	}
	if request.PlatformMinRecommendationScores != nil {
		merged.PlatformMinRecommendationScores = append([]byte(nil), request.PlatformMinRecommendationScores...)
	}
	if request.MinVideoConfidence != nil {
		merged.MinVideoConfidence = *request.MinVideoConfidence
	}
	if request.VideoFirst != nil {
		merged.VideoFirst = *request.VideoFirst
	}
	if request.PublishWindows != nil {
		merged.PublishWindows = append([]byte(nil), request.PublishWindows...)
	}
	if request.DraftEnabled != nil {
		merged.DraftEnabled = *request.DraftEnabled
	}
	if request.PublishEnabled != nil {
		merged.PublishEnabled = *request.PublishEnabled
	}
	if request.ComplianceVersion != nil {
		merged.ComplianceVersion = *request.ComplianceVersion
	}
	if request.FailureMode != nil {
		merged.FailureMode = *request.FailureMode
	}
	return merged
}

func (h *ContentAssetHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	items, total, err := h.service.ListFiltered(c.Context(), service.ContentAssetListParams{UserID: userID, Status: c.Query("status", ""), Query: c.Query("q", ""), Platform: c.Query("platform", ""), Source: c.Query("source", ""), Lifecycle: c.Query("lifecycle", ""), ProjectID: c.Query("project_id", ""), Offset: offset, Limit: limit})
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to list content assets")
	}
	return Success(c, fiber.Map{"items": items, "total": total})
}

func (h *ContentAssetHandler) ListReuseRuns(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "50"))
	items, total, err := h.service.ListReuseRuns(c.Context(), repository.ContentReuseRunListFilter{UserID: userID, AssetID: c.Query("asset_id", ""), TargetProjectID: c.Query("target_project_id", ""), TargetPlatform: c.Query("target_platform", ""), Status: c.Query("status", ""), Stage: c.Query("stage", ""), Offset: offset, Limit: limit})
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to list content reuse runs")
	}
	return Success(c, fiber.Map{"items": items, "total": total, "offset": max(offset, 0), "limit": limit})
}

func (h *ContentAssetHandler) Recommendations(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	limit, _ := strconv.Atoi(c.Query("limit", "5"))
	items, err := h.service.ListRecommendations(c.Context(), userID, limit)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to list content asset recommendations")
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *ContentAssetHandler) Create(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req createContentAssetRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	asset, err := h.service.Create(c.Context(), service.CreateContentAssetParams{UserID: userID, Title: req.Title, CoreQuestion: req.CoreQuestion, Audience: req.Audience, SemanticKernel: req.SemanticKernel, Lifecycle: req.Lifecycle, Status: req.Status})
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, asset)
}

func (h *ContentAssetHandler) Ingest(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req ingestContentAssetRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	detail, err := h.service.Ingest(c.Context(), service.IngestContentAssetParams{CreateContentAssetParams: service.CreateContentAssetParams{UserID: userID, Title: req.Title, CoreQuestion: req.CoreQuestion, Audience: req.Audience, SemanticKernel: req.SemanticKernel, Lifecycle: req.Lifecycle, Status: req.Status}, Evidence: req.Evidence})
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, detail)
}

func (h *ContentAssetHandler) Get(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	detail, err := h.service.Get(c.Context(), userID, c.Params("id"))
	if err != nil {
		return Error(c, fiber.StatusNotFound, "content asset not found")
	}
	return Success(c, detail)
}

func (h *ContentAssetHandler) GetLineage(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	lineage, err := h.service.GetLineage(c.Context(), userID, c.Params("id"))
	if err != nil {
		return Error(c, fiber.StatusNotFound, "content asset lineage not found")
	}
	return Success(c, lineage)
}

func (h *ContentAssetHandler) GetLineageByContent(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	items, err := h.service.GetLineageByContent(c.Context(), userID, c.Query("project_id", ""), c.Query("content_kind", ""), c.Query("content_id", ""))
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, fiber.Map{"items": items})
}

func (h *ContentAssetHandler) Score(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req scoreContentAssetRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	snapshot, err := h.service.ScoreAssetForProject(c.Context(), userID, c.Params("id"), req.ProjectID)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, snapshot)
}

func (h *ContentAssetHandler) Link(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req linkContentAssetRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	link, err := h.service.LinkContent(c.Context(), service.LinkContentAssetParams{UserID: userID, AssetID: c.Params("id"), ProjectID: req.ProjectID, ContentKind: req.ContentKind, ContentID: req.ContentID, Platform: req.Platform, Relation: req.Relation, Angle: req.Angle})
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, link)
}

func (h *ContentAssetHandler) Update(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req updateContentAssetRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	asset, err := h.service.Update(c.Context(), service.UpdateContentAssetParams{UserID: userID, AssetID: c.Params("id"), Title: req.Title, CoreQuestion: req.CoreQuestion, Audience: req.Audience, SemanticKernel: req.SemanticKernel, Lifecycle: req.Lifecycle, Status: req.Status})
	if err != nil {
		if errors.Is(err, service.ErrContentAssetNotFound) {
			return Error(c, fiber.StatusNotFound, "content asset not found")
		}
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, asset)
}

func (h *ContentAssetHandler) CreateReuseRun(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	var req createReuseRunRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	run, err := h.service.CreateReuseRun(c.Context(), service.CreateReuseRunParams{UserID: userID, AssetID: c.Params("id"), SourceContentKind: req.SourceContentKind, SourceContentID: req.SourceContentID, TargetProjectID: req.TargetProjectID, TargetPlatform: req.TargetPlatform, TargetAgentID: req.TargetAgentID, IdempotencyKey: req.IdempotencyKey, ScoreSnapshot: req.ScoreSnapshot, PolicySnapshot: req.PolicySnapshot})
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, run)
}

func (h *ContentAssetHandler) GetReuseRun(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	run, err := h.service.GetReuseRun(c.Context(), userID, c.Params("id"))
	if err != nil {
		return Error(c, fiber.StatusNotFound, "content reuse run not found")
	}
	return Success(c, run)
}

func (h *ContentAssetHandler) SetReuseRunStatus(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	status := c.Params("status")
	run, err := h.service.SetReuseRunStatus(c.Context(), userID, c.Params("id"), status)
	if err != nil {
		if errors.Is(err, service.ErrContentAssetNotFound) {
			return Error(c, fiber.StatusNotFound, "content reuse run not found")
		}
		return Error(c, fiber.StatusConflict, err.Error())
	}
	return Success(c, run)
}

func (h *ContentAssetHandler) PauseReuseRun(c fiber.Ctx) error {
	return h.setReuseRunStatus(c, model.ContentReuseRunStatusPaused)
}

func (h *ContentAssetHandler) ResumeReuseRun(c fiber.Ctx) error {
	return h.setReuseRunStatus(c, model.ContentReuseRunStatusQueued)
}

func (h *ContentAssetHandler) CancelReuseRun(c fiber.Ctx) error {
	return h.setReuseRunStatus(c, model.ContentReuseRunStatusCancelled)
}

func (h *ContentAssetHandler) setReuseRunStatus(c fiber.Ctx, status string) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	run, err := h.service.SetReuseRunStatus(c.Context(), userID, c.Params("id"), status)
	if err != nil {
		if errors.Is(err, service.ErrContentAssetNotFound) {
			return Error(c, fiber.StatusNotFound, "content reuse run not found")
		}
		return Error(c, fiber.StatusConflict, err.Error())
	}
	return Success(c, run)
}

func (h *ContentAssetHandler) GetPolicy(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	policy, err := h.service.GetPolicy(c.Context(), userID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return Error(c, fiber.StatusInternalServerError, "failed to load content autopilot policy")
	}
	return Success(c, policy)
}

func (h *ContentAssetHandler) UpdatePolicy(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	existing, err := h.service.GetPolicy(c.Context(), userID)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "failed to load content autopilot policy")
	}
	var request updateContentAutopilotPolicyRequest
	if err := c.Bind().Body(&request); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	merged := mergeContentAutopilotPolicy(existing, request)
	merged.UserID = userID
	policy := merged
	saved, err := h.service.UpsertPolicy(c.Context(), &policy)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	return Success(c, saved)
}
