package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

const (
	maxTaskPromptCharacters = 5120
	maxTaskResumeFiles      = maxAgentInputAttachments
	maxTaskResumeFileBytes  = 25 * 1024 * 1024
)

// TaskHandler handles task-related HTTP endpoints.
type TaskHandler struct {
	service               *service.TaskService
	logger                *zerolog.Logger
	dataDir               string // local storage data directory (for ServeLocalFile)
	taskLogDir            string // task log directory (for GetLog)
	imageCapabilityRoutes config.ImageGenerationRoutesConfig
	repo                  repository.Repository
	store                 storage.Provider
	referenceAssets       *service.ReferenceAssetService
}

func (h *TaskHandler) SetReferenceAssetService(referenceAssets *service.ReferenceAssetService) {
	h.referenceAssets = referenceAssets
}

// NewTaskHandler creates a new TaskHandler.
//
// Optional variadic options:
//   - first string arg: local data directory (for ServeLocalFile).
//   - SetImageCapabilities / SetRepository: configure tier-gated image capability validation.
func NewTaskHandler(svc *service.TaskService, logger *zerolog.Logger, dirs ...string) *TaskHandler {
	h := &TaskHandler{service: svc, logger: logger}
	if svc != nil {
		h.taskLogDir = svc.TaskLogDir()
	}
	if len(dirs) > 0 {
		h.dataDir = dirs[0]
	}
	return h
}

// SetImageCapabilities wires the system-managed image capabilities for tier-gated
// validation of createTaskRequest.ImageCapabilityKey.
func (h *TaskHandler) SetImageCapabilities(routes config.ImageGenerationRoutesConfig) {
	h.imageCapabilityRoutes = routes
}

// SetRepository wires the user repository so the handler can resolve the caller's
// tier for image-model validation.
func (h *TaskHandler) SetRepository(repo repository.Repository) {
	h.repo = repo
}

// SetStore wires storage ownership checks used by response serialization and
// as the handler-side fallback for pending upload finalization.
func (h *TaskHandler) SetStore(store storage.Provider) {
	h.store = store
}

func (h *TaskHandler) presentTaskReference(ctx context.Context, userID string, task *model.Task) (*model.AssetView, error) {
	if task == nil {
		return nil, nil
	}
	assetID := task.ReferenceImageAssetID
	allowed := []string{service.DirectUploadPurposeTaskReference, service.DirectUploadPurposeAIEntryAttachment}
	if portraitID := task.ProjectSnapshot.Data().PortraitReferenceImageAssetID; assetID == portraitID && portraitID != "" {
		allowed = []string{service.DirectUploadPurposeProjectPortraitReference}
	}
	if assetID == "" && !task.SkipReferenceImage {
		assetID = task.ProjectSnapshot.Data().ReferenceImageAssetID
		allowed = []string{service.DirectUploadPurposeProjectReference}
	}
	if assetID == "" {
		task.ReferenceImage = nil
		return nil, nil
	}
	if h.referenceAssets == nil {
		return nil, service.ErrReferenceAssetUnavailable
	}
	view, err := h.referenceAssets.Present(ctx, userID, assetID, allowed)
	if err != nil {
		return nil, err
	}
	task.ReferenceImage = view
	return view, nil
}

func (h *TaskHandler) presentTaskReferences(ctx context.Context, userID string, tasks []*model.Task) error {
	for _, task := range tasks {
		if _, err := h.presentTaskReference(ctx, userID, task); err != nil {
			return err
		}
	}
	return nil
}

func (h *TaskHandler) presentCloneTaskReference(ctx context.Context, userID string, task *model.Task) (*model.AssetView, error) {
	if task == nil || strings.TrimSpace(task.ReferenceImageAssetID) == "" {
		return h.presentTaskReference(ctx, userID, task)
	}
	if h.referenceAssets == nil {
		return nil, service.ErrReferenceAssetUnavailable
	}
	allowed := []string{service.DirectUploadPurposeTaskReference, service.DirectUploadPurposeAIEntryAttachment}
	if trustedTaskCreationProjectReference(task, task.ReferenceImageAssetID) {
		allowed = append(allowed, service.DirectUploadPurposeProjectReference)
	}
	view, err := h.referenceAssets.Present(ctx, userID, task.ReferenceImageAssetID, allowed)
	if err != nil {
		return nil, err
	}
	task.ReferenceImage = view
	return view, nil
}

// Request types.

type createTaskRequest struct {
	ProjectID          string                           `json:"project_id"`
	Type               string                           `json:"type"`
	ExecutionProfile   string                           `json:"execution_profile"`
	Prompt             string                           `json:"prompt"`
	Quantity           int                              `json:"quantity"`
	ImageRatio         string                           `json:"image_ratio"`
	ImageCapabilityKey string                           `json:"image_capability_key"`
	SkipReferenceImage *bool                            `json:"skip_reference_image"`
	ReferenceImage     *service.ReferenceImageSelection `json:"reference_image"`
	InputAttachments   []model.EntryAttachment          `json:"input_attachments,omitempty"`
	AgentInput         map[string]any                   `json:"agent_input,omitempty"`
	Watermark          *bool                            `json:"watermark"`
	// HasContentImage / HasTailImage: seednote image composition (cover always
	// generated). nil → fall back to CreateManualParams defaults (content on,
	// tail off). Non-seednote task types ignore them.
	HasContentImage *bool `json:"has_content_image,omitempty"`
	HasTailImage    *bool `json:"has_tail_image,omitempty"`
	// ArticleWithCover / ArticleWithContentImages: 公众号 article image toggles
	// (cover is NOT mandatory). nil → fall back to model defaults (both on).
	// Non-article task types ignore them.
	ArticleWithCover         *bool `json:"article_with_cover,omitempty"`
	ArticleWithContentImages *bool `json:"article_with_content_images,omitempty"`
	ArticleCoverUsePortrait  bool  `json:"article_cover_use_portrait,omitempty"`
	// E-commerce package fields (project platform = "ecommerce"). SelectedModules
	// maps a module key (main_images / detail_page / cover_banner / share_image /
	// sku_images) to its quantity; creation billing uses the ecommerce base task
	// fee, and selected modules only guide later image/vision MCP usage.
	// ProductPhotos are browser-upload URLs finalized into immutable task input
	// attachments before creation.
	ProductPhotos   []string            `json:"product_photos,omitempty"`
	SelectedModules map[string]int      `json:"selected_modules,omitempty"`
	TargetPlatform  string              `json:"target_platform,omitempty"`
	SellingPoints   string              `json:"selling_points,omitempty"`
	Language        string              `json:"language,omitempty"`
	MontageInput    *model.MontageInput `json:"montage_input,omitempty"`
	HypitInput      *model.HypitInput   `json:"hypit_input,omitempty"`
}

type cloneTaskRequest struct {
	ProjectID                string                           `json:"project_id"`
	ExecutionProfile         string                           `json:"execution_profile"`
	Prompt                   *string                          `json:"prompt"`
	Quantity                 int                              `json:"quantity"`
	ImageRatio               string                           `json:"image_ratio"`
	ImageCapabilityKey       string                           `json:"image_capability_key"`
	SkipReferenceImage       *bool                            `json:"skip_reference_image"`
	ReferenceImage           *service.ReferenceImageSelection `json:"reference_image"`
	InputAttachments         *[]model.EntryAttachment         `json:"input_attachments"`
	AgentInput               *map[string]any                  `json:"agent_input"`
	Watermark                *bool                            `json:"watermark"`
	HasContentImage          *bool                            `json:"has_content_image,omitempty"`
	HasTailImage             *bool                            `json:"has_tail_image,omitempty"`
	ArticleWithCover         *bool                            `json:"article_with_cover,omitempty"`
	ArticleWithContentImages *bool                            `json:"article_with_content_images,omitempty"`
	ArticleCoverUsePortrait  *bool                            `json:"article_cover_use_portrait,omitempty"`
	ProductPhotos            []string                         `json:"product_photos,omitempty"`
	SelectedModules          map[string]int                   `json:"selected_modules,omitempty"`
	TargetPlatform           string                           `json:"target_platform,omitempty"`
	SellingPoints            string                           `json:"selling_points,omitempty"`
	Language                 string                           `json:"language,omitempty"`
	MontageInput             *model.MontageInput              `json:"montage_input,omitempty"`
	HypitInput               *model.HypitInput                `json:"hypit_input,omitempty"`
}

type resumeTaskRequest struct {
	Prompt           string                  `json:"prompt"`
	InputAttachments []model.EntryAttachment `json:"input_attachments"`
}

type bulkDownloadTaskFilesRequest struct {
	TaskIDs []string `json:"task_ids"`
}

// bulkTaskIDsRequest is the shared request body for bulk cancel / delete.
type bulkTaskIDsRequest struct {
	TaskIDs []string `json:"task_ids"`
}

type bulkCloneTasksRequest struct {
	TaskIDs          []string `json:"task_ids"`
	ExecutionProfile string   `json:"execution_profile"`
}

// bulkTaskResult is the per-task outcome of a bulk operation. OK=false tasks
// carry a machine-readable Reason so the UI can explain what was skipped.
type bulkTaskResult struct {
	ID        string `json:"id"`
	OK        bool   `json:"ok"`
	Reason    string `json:"reason,omitempty"`
	NewTaskID string `json:"new_task_id,omitempty"` // clone only
}

// bulkTasksResponse summarises a best-effort bulk operation.
type bulkTasksResponse struct {
	Total     int              `json:"total"`
	Succeeded int              `json:"succeeded"`
	Skipped   int              `json:"skipped"`
	Results   []bulkTaskResult `json:"results"`
}

type taskBillingChargeDetailResponse struct {
	ID           string                  `json:"id,omitempty"`
	ChargeKind   model.BillingChargeKind `json:"charge_kind"`
	Policy       string                  `json:"policy,omitempty"`
	SKUID        string                  `json:"sku_id,omitempty"`
	Credits      int64                   `json:"credits"`
	PricingTier  string                  `json:"pricing_tier,omitempty"`
	ListPrice    int64                   `json:"list_price_credits,omitempty"`
	Discount     int64                   `json:"discount_credits,omitempty"`
	ResourceType string                  `json:"resource_type,omitempty"`
	ResourceID   string                  `json:"resource_id,omitempty"`
	ToolCallID   *string                 `json:"tool_call_id,omitempty"`
	ReversalOfID *string                 `json:"reversal_of_id,omitempty"`
	CreatedAt    time.Time               `json:"created_at,omitempty"`
}

// Create handles POST /api/v1/tasks.
func (h *TaskHandler) Create(c fiber.Ctx) error {
	if err := rejectRemovedRequestFields(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	var req createTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	prepared, err := h.prepareTaskCreation(c, userID, &req, false, nil)
	if err != nil || prepared == nil {
		return err
	}
	tasks, err := h.service.CreateManual(c.Context(), prepared.params)
	if err != nil {
		return h.respondTaskCreationServiceError(c, userID, err, "create task failed", "failed to create task")
	}
	for _, task := range tasks {
		task.ReferenceImage = prepared.referenceView
	}

	// Montage is always a single deliverable and Studio expects one task
	// object even when the request quantity is clamped by the service.
	if len(tasks) == 1 && (prepared.quantity == 1 || req.MontageInput != nil) {
		return Success(c, taskAPIResponse(tasks[0], h.store))
	}
	return Success(c, taskAPIResponses(tasks, h.store))
}

type preparedTaskCreation struct {
	params        service.CreateManualParams
	quantity      int
	referenceView *model.AssetView
}

type taskScopedCreationInput struct {
	userID    string
	projectID string
	taskID    string
}

type parsedTaskCreationInput struct {
	identity   taskScopedCreationInput
	key        string
	taskScoped bool
}

func trustedTaskCreationSource(source *model.Task) taskScopedCreationInput {
	if source == nil {
		return taskScopedCreationInput{}
	}
	taskID, projectID := service.ResolveCloneInputSource(source)
	return taskScopedCreationInput{userID: source.UserID, projectID: projectID, taskID: taskID}
}

func trustedTaskCreationProjectReference(source *model.Task, assetID string) bool {
	assetID = strings.TrimSpace(assetID)
	if source == nil || assetID == "" {
		return false
	}
	return assetID == strings.TrimSpace(source.ReferenceImageAssetID) ||
		assetID == strings.TrimSpace(source.ProjectSnapshot.Data().ReferenceImageAssetID)
}

func taskCreationReferencePurposes(source *model.Task, selection service.ReferenceImageSelection, targetPlatform string) []string {
	allowed := []string{service.DirectUploadPurposeTaskReference}
	assetID := strings.TrimSpace(selection.AssetID)
	if source != nil && assetID != "" && assetID == strings.TrimSpace(source.ReferenceImageAssetID) {
		allowed = append(allowed, service.DirectUploadPurposeAIEntryAttachment)
	}
	if targetPlatform != model.PlatformArticle && trustedTaskCreationProjectReference(source, assetID) {
		allowed = append(allowed, service.DirectUploadPurposeProjectReference)
	}
	return allowed
}

func validateTaskCreationSourceReuse(store storage.Provider, source *model.Task, attachments []model.EntryAttachment, productPhotos []string, montageInput *model.MontageInput) error {
	trusted := trustedTaskCreationSource(source)
	validate := func(raw string, keyOnly bool) (parsedTaskCreationInput, error) {
		parsed, err := parseTaskScopedCreationInput(store, raw, keyOnly)
		if err != nil {
			return parsedTaskCreationInput{}, fmt.Errorf("task input storage URL is invalid")
		}
		if !parsed.taskScoped {
			return parsed, nil
		}
		if source == nil || parsed.identity != trusted {
			return parsedTaskCreationInput{}, fmt.Errorf("task-scoped input is not authorized for this clone source")
		}
		return parsed, nil
	}
	for _, attachment := range attachments {
		parsedURL, err := validate(attachment.URL, false)
		if err != nil {
			return err
		}
		parsedKey, err := validate(attachment.Key, true)
		if err != nil {
			return err
		}
		if strings.TrimSpace(attachment.URL) != "" && strings.TrimSpace(attachment.Key) != "" && (parsedURL.key == "" || parsedKey.key == "" || parsedURL.key != parsedKey.key) {
			return fmt.Errorf("attachment URL and key must identify the same storage object")
		}
	}
	for _, rawURL := range productPhotos {
		if _, err := validate(rawURL, false); err != nil {
			return err
		}
	}
	for _, rawURL := range montageSourceAssetURLs(montageInput) {
		if _, err := validate(rawURL, false); err != nil {
			return err
		}
	}
	return nil
}

func parseTaskScopedCreationInput(store storage.Provider, raw string, keyOnly bool) (parsedTaskCreationInput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return parsedTaskCreationInput{}, nil
	}
	key := raw
	if !keyOnly {
		isLocalAPI := strings.HasPrefix(raw, "/api/v1/files/") || strings.HasPrefix(raw, "/files/")
		if !isLocalAPI && (store == nil || !store.IsOwnedURL(raw)) {
			return parsedTaskCreationInput{}, nil
		}
		if strings.HasPrefix(raw, "/files/") {
			parsedURL, err := url.Parse(raw)
			if err != nil {
				return parsedTaskCreationInput{}, err
			}
			key = strings.TrimPrefix(parsedURL.Path, "/files/")
		} else {
			var ok bool
			key, ok = storage.StorageKeyFromURL(raw)
			if !ok {
				return parsedTaskCreationInput{}, storage.ErrInvalidRuntimeStorageKey
			}
		}
	}
	parsed, err := storage.ParseRuntimeStorageKey(strings.TrimPrefix(key, "/"))
	if err != nil {
		return parsedTaskCreationInput{}, err
	}
	result := parsedTaskCreationInput{key: parsed.Key}
	parts := strings.Split(parsed.Key, "/")
	if len(parts) < 8 || parts[0] != "uploads" || parts[1] != "users" || parts[2] == "" || parts[3] != "projects" || parts[4] == "" || parts[5] != "tasks" || parts[6] == "" {
		return result, nil
	}
	result.identity = taskScopedCreationInput{userID: parts[2], projectID: parts[4], taskID: parts[6]}
	result.taskScoped = true
	return result, nil
}

func (h *TaskHandler) prepareTaskCreation(c fiber.Ctx, userID string, req *createTaskRequest, strictQuantity bool, source *model.Task) (*preparedTaskCreation, error) {
	if req.ProjectID == "" {
		return nil, Error(c, fiber.StatusBadRequest, "project_id is required")
	}
	if strings.TrimSpace(req.ExecutionProfile) == "" {
		return nil, Error(c, fiber.StatusBadRequest, "execution_profile is required")
	}
	if req.MontageInput != nil && strings.TrimSpace(req.MontageInput.Brief) == "" {
		return nil, Error(c, fiber.StatusBadRequest, "视频生成任务需要填写需求")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if utf8.RuneCountInString(prompt) > maxTaskPromptCharacters {
		return nil, Error(c, fiber.StatusBadRequest, "prompt must not exceed 5120 characters")
	}
	project, err := h.service.ResolveTaskCreationProject(c.Context(), userID, req.ProjectID)
	if err != nil {
		return nil, respondTaskCreationProjectError(c, h.logger, err)
	}
	requestedTaskType := strings.TrimSpace(req.Type)
	switch {
	case requestedTaskType == "":
		requestedTaskType = project.Platform
	case requestedTaskType == model.TaskTypeViralAnalysis:
		if project.Platform != model.PlatformSeednote {
			return nil, Error(c, fiber.StatusBadRequest, "viral_analysis requires a Seednote project")
		}
	case requestedTaskType != project.Platform:
		return nil, Error(c, fiber.StatusBadRequest, "type must match project platform")
	}
	projectSnapshot := model.SnapshotProject(project)

	var referenceAssetID string
	var referenceView *model.AssetView
	if req.ReferenceImage != nil {
		if h.referenceAssets == nil {
			return nil, respondReferenceAssetError(c, h.logger, service.ErrReferenceAssetUnavailable)
		}
		allowed := taskCreationReferencePurposes(source, *req.ReferenceImage, project.Platform)
		resolved, err := h.referenceAssets.ResolveSelection(c.Context(), userID, *req.ReferenceImage, allowed)
		if err != nil {
			return nil, respondReferenceAssetError(c, h.logger, err)
		}
		referenceAssetID = resolved
		referenceView, err = h.referenceAssets.Present(c.Context(), userID, resolved, allowed)
		if err != nil {
			return nil, respondReferenceAssetError(c, h.logger, err)
		}
	}
	if referenceView == nil && (req.SkipReferenceImage == nil || !*req.SkipReferenceImage) && project.ReferenceImageAssetID != "" {
		if h.referenceAssets == nil {
			return nil, respondReferenceAssetError(c, h.logger, service.ErrReferenceAssetUnavailable)
		}
		presented, presentErr := h.referenceAssets.Present(c.Context(), userID, project.ReferenceImageAssetID, []string{service.DirectUploadPurposeProjectReference})
		if presentErr != nil {
			return nil, respondReferenceAssetError(c, h.logger, presentErr)
		}
		referenceView = presented
	}

	var pending repository.Repository
	if h.repo != nil {
		pending = h.repo
	}
	allowedAttachmentTypes := allAgentAttachmentTypes
	if project.Platform == model.PlatformSeednote {
		allowedAttachmentTypes = map[string]bool{"image": true}
	}
	attachmentValidation := InputAttachmentValidationOptions{
		MaxCount:             maxAgentInputAttachments,
		AllowedTypes:         allowedAttachmentTypes,
		AllowedAssetPurposes: taskInputAttachmentAssetPurposes,
	}
	validatedAttachments, err := validateInputAttachments(c.Context(), h.service.Storage(), pending, userID, req.InputAttachments, attachmentValidation)
	if err != nil {
		return nil, respondInputAttachmentError(c, h.logger, err)
	}
	req.InputAttachments = validatedAttachments

	quantity := req.Quantity
	if strictQuantity && (quantity < 1 || quantity > 5) {
		return nil, Error(c, fiber.StatusBadRequest, "quantity must be between 1 and 5")
	}
	if quantity <= 0 {
		quantity = 1
	}
	if quantity > 5 {
		return nil, Error(c, fiber.StatusBadRequest, "quantity must be between 1 and 5")
	}
	if req.ImageRatio != "" && len(model.SupportedImageRatios(project.Platform)) > 0 && !model.IsBusinessImageRatioAllowed(project.Platform, req.ImageRatio) {
		return nil, Error(c, fiber.StatusBadRequest, model.ValidImageRatioHint)
	}
	for _, u := range req.ProductPhotos {
		if !validAttachmentURL(u) {
			return nil, Error(c, fiber.StatusBadRequest, "product_photos must be internal file paths or http(s) URLs")
		}
	}
	if err := validateMontageSourceAssetURLs(req.MontageInput); err != nil {
		return nil, Error(c, fiber.StatusBadRequest, err.Error())
	}
	if strings.TrimSpace(req.ImageCapabilityKey) == "" && project.Platform == model.PlatformEcommerce {
		req.ImageCapabilityKey = strings.TrimSpace(project.EcommerceDefaults.Data().ImageCapabilityKey)
	}
	if err := h.validateImageCapabilityKeyForUser(c, userID, req.ImageCapabilityKey); err != nil {
		return nil, Error(c, fiber.StatusForbidden, err.Error())
	}
	if h.repo != nil {
		finalizationStore := h.service.Storage()
		if finalizationStore == nil {
			finalizationStore = h.store
		}
		originalProductPhotos := append([]string(nil), req.ProductPhotos...)
		finalStore, _ := finalizationStore.(service.DirectUploadFinalizationStorage)
		var ownedURLChecks []func(string) bool
		if finalizationStore != nil {
			ownedURLChecks = append(ownedURLChecks, finalizationStore.IsOwnedURL)
		}
		rewrites, productAssets, err := service.FinalizeUploadSessionURLAssets(c.Context(), finalStore, h.repo, userID, service.DirectUploadPurposeEcommercePhoto, originalProductPhotos, time.Now(), ownedURLChecks...)
		if err != nil {
			return nil, respondUploadSessionFinalizeError(c, h.logger, err)
		}
		rewriteFinalizedUploadURLSlice(req.ProductPhotos, rewrites)
		for i, raw := range originalProductPhotos {
			asset := productAssets[raw]
			if asset == nil {
				return nil, Error(c, fiber.StatusBadRequest, "product_photos must be immutable ecommerce uploads")
			}
			req.InputAttachments = append(req.InputAttachments, model.EntryAttachment{
				AssetID: asset.ID, Type: "image", FileName: asset.FileName,
				ContentType: asset.ContentType, Size: asset.Size,
				Role:        model.EntryAttachmentRoleEcommerceProduct,
				Instruction: fmt.Sprintf("电商产品参考图 %d", i+1),
			})
		}
		if model.IsHypitPlatform(project.Platform) {
			rewrites, err = finalizeUploadSessionURLs(c.Context(), finalizationStore, h.repo, userID, service.DirectUploadPurposeHypitAsset, hypitAssetURLs(req.HypitInput))
			if err != nil {
				return nil, respondUploadSessionFinalizeError(c, h.logger, err)
			}
			rewriteHypitAssetURLs(req.HypitInput, rewrites)
		}
		if model.IsMontagePlatform(project.Platform) {
			rewrites, err = finalizeUploadSessionURLs(c.Context(), finalizationStore, h.repo, userID, service.DirectUploadPurposeMontageAsset, montageSourceAssetURLs(req.MontageInput))
			if err != nil {
				return nil, respondUploadSessionFinalizeError(c, h.logger, err)
			}
			rewriteFinalizedMontageAssetURLs(req.MontageInput, rewrites)
		}
	}
	if err := validateTaskCreationSourceReuse(h.service.Storage(), source, req.InputAttachments, req.ProductPhotos, req.MontageInput); err != nil {
		return nil, Error(c, fiber.StatusBadRequest, err.Error())
	}

	var ecommerceCfg *model.EcommerceConfig
	if len(req.SelectedModules) > 0 || len(req.ProductPhotos) > 0 || req.TargetPlatform != "" || req.SellingPoints != "" || req.Language != "" {
		ecommerceCfg = &model.EcommerceConfig{
			ProductPhotos:   req.ProductPhotos,
			SelectedModules: req.SelectedModules,
			TargetPlatform:  req.TargetPlatform,
			SellingPoints:   req.SellingPoints,
			Language:        req.Language,
		}
	}
	return &preparedTaskCreation{
		quantity:      quantity,
		referenceView: referenceView,
		params: service.CreateManualParams{
			UserID:                   userID,
			ProjectID:                req.ProjectID,
			RequestedTaskType:        requestedTaskType,
			ExecutionProfile:         strings.TrimSpace(req.ExecutionProfile),
			Prompt:                   prompt,
			Quantity:                 quantity,
			ImageRatio:               req.ImageRatio,
			ImageCapabilityKey:       req.ImageCapabilityKey,
			SkipRefImage:             req.SkipReferenceImage,
			ReferenceImageAssetID:    referenceAssetID,
			ProjectSnapshot:          &projectSnapshot,
			InputAttachments:         req.InputAttachments,
			AgentInput:               req.AgentInput,
			Watermark:                req.Watermark,
			HasContentImage:          req.HasContentImage,
			HasTailImage:             req.HasTailImage,
			ArticleWithCover:         req.ArticleWithCover,
			ArticleWithContentImages: req.ArticleWithContentImages,
			ArticleCoverUsePortrait:  req.ArticleCoverUsePortrait,
			Ecommerce:                ecommerceCfg,
			MontageInput:             req.MontageInput,
			HypitInput:               req.HypitInput,
		},
	}, nil
}

func (h *TaskHandler) respondTaskCreationServiceError(c fiber.Ctx, userID string, err error, logMessage, fallbackMessage string) error {
	if errors.Is(err, service.ErrProjectNotFound) || errors.Is(err, service.ErrProjectOwnedByUser) || errors.Is(err, service.ErrTaskCreationProjectInactive) {
		return respondTaskCreationProjectError(c, h.logger, err)
	}
	if handled, response := respondAgentProfileError(c, err); handled {
		return response
	}
	if errors.Is(err, service.ErrViralAnalysisRequiresSeednoteProject) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, service.ErrArticleCoverPortraitUnavailable) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, service.ErrInvalidAgentInput) {
		return Error(c, fiber.StatusBadRequest, "invalid_agent_input: "+err.Error())
	}
	if isReferenceAssetError(err) {
		return respondReferenceAssetError(c, h.logger, err)
	}
	h.logger.Error().Err(err).Str("user_id", userID).Msg(logMessage)
	if errors.Is(err, service.ErrMontageInput) || errors.Is(err, service.ErrHypitInput) {
		return Error(c, fiber.StatusBadRequest, err.Error())
	}
	if errors.Is(err, service.ErrBillingInsufficientForTask) || errors.Is(err, service.ErrBillingDebtOutstanding) {
		return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
			"code": 40202,
			"msg":  "billing_task_admission_rejected",
		})
	}
	return Error(c, fiber.StatusInternalServerError, fallbackMessage)
}

func respondTaskCreationProjectError(c fiber.Ctx, logger *zerolog.Logger, err error) error {
	switch {
	case errors.Is(err, service.ErrProjectNotFound):
		return Error(c, fiber.StatusNotFound, "project not found")
	case errors.Is(err, service.ErrProjectOwnedByUser):
		return Forbidden(c, "you do not have access to this project")
	case errors.Is(err, service.ErrTaskCreationProjectInactive):
		return Error(c, fiber.StatusBadRequest, "project is not active")
	default:
		if logger != nil {
			logger.Error().Err(err).Msg("resolve task project failed")
		}
		return Error(c, fiber.StatusInternalServerError, "failed to resolve project")
	}
}

// List handles GET /api/v1/tasks.
func (h *TaskHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	offset, _ := strconv.Atoi(c.Query("offset", "0"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	status := c.Query("status", "")
	projectID := c.Query("project_id", "")
	planID := c.Query("plan_id", "")

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	tasks, total, err := h.service.List(c.Context(), userID, offset, limit, status, projectID, planID)
	if err != nil {
		h.logger.Error().Err(err).Msg("list tasks failed")
		return Error(c, fiber.StatusInternalServerError, "failed to list tasks")
	}
	if err := h.presentTaskReferences(c.Context(), userID, tasks); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	responses := taskAPIResponses(tasks, h.store)
	if err := h.enrichTaskBilling(c.Context(), userID, tasks, responses, false); err != nil {
		h.logger.Error().Err(err).Msg("enrich task billing failed")
		return Error(c, fiber.StatusInternalServerError, "failed to load task billing")
	}

	return Success(c, fiber.Map{
		"items": responses,
		"total": total,
	})
}

// GetByID handles GET /api/v1/tasks/:id.
func (h *TaskHandler) GetByID(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}

	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}
	h.service.RefreshTaskPublicationLifecycle(c.Context(), userID, task)
	if _, err := h.presentTaskReference(c.Context(), userID, task); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	resp := taskAPIResponse(task, h.store)
	if err := h.enrichTaskBilling(c.Context(), userID, []*model.Task{task}, []map[string]any{resp}, true); err != nil {
		h.logger.Error().Err(err).Str("task_id", task.ID).Msg("enrich task billing failed")
		return Error(c, fiber.StatusInternalServerError, "failed to load task billing")
	}

	return Success(c, resp)
}

func (h *TaskHandler) enrichTaskBilling(ctx context.Context, userID string, tasks []*model.Task, responses []map[string]any, includeDetails bool) error {
	if len(tasks) != len(responses) {
		return fmt.Errorf("task billing response count mismatch")
	}
	if len(tasks) == 0 || h.repo == nil {
		return nil
	}
	taskIDs := make([]string, 0, len(tasks))
	tasksByID := make(map[string]*model.Task, len(tasks))
	responsesByID := make(map[string]map[string]any, len(tasks))
	for index, task := range tasks {
		if task == nil {
			continue
		}
		taskIDs = append(taskIDs, task.ID)
		tasksByID[task.ID] = task
		responsesByID[task.ID] = responses[index]
	}
	if !includeDetails {
		totals, err := h.repo.Billing().ListTaskChargeTotals(ctx, userID, taskIDs)
		if err != nil {
			return err
		}
		totalsByTaskID := make(map[string]repository.BillingTaskChargeTotal, len(totals))
		for _, total := range totals {
			totalsByTaskID[total.TaskID] = total
		}
		for _, task := range tasks {
			if task == nil {
				continue
			}
			total := totalsByTaskID[task.ID]
			if total.TaskChargeCount == 0 {
				if task.BillingPriceCredits > 0 && total.TotalCredits > math.MaxInt64-task.BillingPriceCredits {
					return fmt.Errorf("task billing total overflow for %s", task.ID)
				}
				total.TotalCredits += task.BillingPriceCredits
			}
			responsesByID[task.ID]["billing_total_credits"] = total.TotalCredits
		}
		return nil
	}
	charges, err := h.repo.Billing().ListChargesByTaskIDs(ctx, userID, taskIDs)
	if err != nil {
		return err
	}
	chargeTaskIDs := make(map[string]string, len(charges))
	for _, charge := range charges {
		if charge.Kind == model.BillingChargeKindReversal {
			continue
		}
		taskID := ""
		if charge.TaskID != nil {
			taskID = *charge.TaskID
		} else if charge.OperationTaskID != nil {
			taskID = *charge.OperationTaskID
		}
		if _, ok := tasksByID[taskID]; ok {
			chargeTaskIDs[charge.ID] = taskID
		}
	}
	detailsByTaskID := make(map[string][]taskBillingChargeDetailResponse, len(tasks))
	totalsByTaskID := make(map[string]int64, len(tasks))
	hasTaskCharge := make(map[string]bool, len(tasks))
	for _, charge := range charges {
		taskID := chargeTaskIDs[charge.ID]
		credits := charge.PriceCredits
		if charge.Kind == model.BillingChargeKindReversal {
			if charge.ReversalOfID == nil {
				continue
			}
			taskID = chargeTaskIDs[*charge.ReversalOfID]
			credits = -credits
		}
		if taskID == "" {
			continue
		}
		if charge.Kind == model.BillingChargeKindTask {
			hasTaskCharge[taskID] = true
		}
		total := totalsByTaskID[taskID]
		if (credits > 0 && total > math.MaxInt64-credits) || (credits < 0 && total < math.MinInt64-credits) {
			return fmt.Errorf("task billing total overflow for %s", taskID)
		}
		totalsByTaskID[taskID] = total + credits
		detailsByTaskID[taskID] = append(detailsByTaskID[taskID], taskBillingChargeDetailResponse{
			ID: charge.ID, ChargeKind: charge.Kind, Policy: charge.Policy, SKUID: charge.SKUID,
			Credits: credits, PricingTier: charge.PricingTier, ListPrice: charge.ListPriceCredits, Discount: charge.DiscountCredits,
			ResourceType: charge.ResourceType, ResourceID: charge.ResourceID,
			ToolCallID: charge.ToolCallID, ReversalOfID: charge.ReversalOfID, CreatedAt: charge.CreatedAt,
		})
	}
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if !hasTaskCharge[task.ID] && task.BillingPriceCredits > 0 {
			if totalsByTaskID[task.ID] > math.MaxInt64-task.BillingPriceCredits {
				return fmt.Errorf("task billing total overflow for %s", task.ID)
			}
			detail := taskBillingChargeDetailResponse{
				ChargeKind: model.BillingChargeKindTask, Policy: "task_admission", SKUID: task.BillingSKUID,
				Credits: task.BillingPriceCredits, PricingTier: task.BillingPricingTier, ListPrice: task.BillingPriceCredits,
				ResourceType: "task", ResourceID: task.ID, CreatedAt: task.CreatedAt,
			}
			if task.BillingChargeID != nil {
				detail.ID = *task.BillingChargeID
			}
			detailsByTaskID[task.ID] = append([]taskBillingChargeDetailResponse{detail}, detailsByTaskID[task.ID]...)
			totalsByTaskID[task.ID] += task.BillingPriceCredits
		}
		response := responsesByID[task.ID]
		response["billing_total_credits"] = totalsByTaskID[task.ID]
		response["billing_charge_details"] = detailsByTaskID[task.ID]
	}
	return nil
}

// Cancel handles POST /api/v1/tasks/:id/cancel.
func (h *TaskHandler) Cancel(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before cancel.
	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	if err := h.service.CancelForUser(c.Context(), userID, id); err != nil {
		h.logger.Error().Err(err).Msg("cancel task failed")
		return Error(c, fiber.StatusInternalServerError, "failed to cancel task")
	}

	return Success(c, fiber.Map{"message": "task cancelled"})
}

// Clone handles POST /api/v1/tasks/:id/clone.
// It creates a fresh task from a terminal task's configuration and enqueues it.
// The original task is preserved and the new task is billed as a new run.
func (h *TaskHandler) Clone(c fiber.Ctx) error {
	if err := rejectRemovedRequestFields(c.Body()); err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before cloning.
	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	// Only terminal tasks can be manually cloned. Active tasks must finish or be
	// cancelled first to avoid duplicate in-flight work and surprise billing.
	if task.Status != model.TaskStatusCompleted && task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled {
		return Error(c, fiber.StatusBadRequest, "只有已完成、失败或已取消的任务可以克隆")
	}

	var req cloneTaskRequest
	if len(c.Body()) > 0 {
		if err := c.Bind().Body(&req); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid request body")
		}
	}
	req.ExecutionProfile = strings.TrimSpace(req.ExecutionProfile)
	if req.ExecutionProfile == "" {
		return Error(c, fiber.StatusBadRequest, "execution_profile is required")
	}
	if req.ProjectID != "" {
		prompt := ""
		if req.Prompt != nil {
			prompt = *req.Prompt
		}
		attachments := []model.EntryAttachment(nil)
		if req.InputAttachments != nil {
			attachments = *req.InputAttachments
		}
		creationReq := createTaskRequest{
			ProjectID:                req.ProjectID,
			ExecutionProfile:         req.ExecutionProfile,
			Prompt:                   prompt,
			Quantity:                 req.Quantity,
			ImageRatio:               req.ImageRatio,
			ImageCapabilityKey:       req.ImageCapabilityKey,
			SkipReferenceImage:       req.SkipReferenceImage,
			ReferenceImage:           req.ReferenceImage,
			InputAttachments:         attachments,
			Watermark:                req.Watermark,
			HasContentImage:          req.HasContentImage,
			HasTailImage:             req.HasTailImage,
			ArticleWithCover:         req.ArticleWithCover,
			ArticleWithContentImages: req.ArticleWithContentImages,
			ArticleCoverUsePortrait:  req.ArticleCoverUsePortrait != nil && *req.ArticleCoverUsePortrait,
			ProductPhotos:            req.ProductPhotos,
			SelectedModules:          req.SelectedModules,
			TargetPlatform:           req.TargetPlatform,
			SellingPoints:            req.SellingPoints,
			Language:                 req.Language,
			MontageInput:             req.MontageInput,
			HypitInput:               req.HypitInput,
		}
		if req.AgentInput != nil {
			creationReq.AgentInput = *req.AgentInput
		} else {
			creationReq.AgentInput = task.AgentInput.Data()
		}
		prepared, err := h.prepareTaskCreation(c, userID, &creationReq, true, task)
		if err != nil || prepared == nil {
			return err
		}
		tasks, err := h.service.Clone(c.Context(), id, service.CloneTaskParams{ExecutionProfile: req.ExecutionProfile, Overrides: &service.CloneTaskOverrides{
			ProjectID:                prepared.params.ProjectID,
			Quantity:                 prepared.params.Quantity,
			Prompt:                   prepared.params.Prompt,
			ImageRatio:               prepared.params.ImageRatio,
			ImageCapabilityKey:       prepared.params.ImageCapabilityKey,
			SkipRefImage:             prepared.params.SkipRefImage,
			ReferenceImageAssetID:    prepared.params.ReferenceImageAssetID,
			InputAttachments:         prepared.params.InputAttachments,
			AgentInput:               prepared.params.AgentInput,
			Watermark:                prepared.params.Watermark,
			HasContentImage:          prepared.params.HasContentImage,
			HasTailImage:             prepared.params.HasTailImage,
			ArticleWithCover:         prepared.params.ArticleWithCover,
			ArticleWithContentImages: prepared.params.ArticleWithContentImages,
			ArticleCoverUsePortrait:  req.ArticleCoverUsePortrait,
			Ecommerce:                prepared.params.Ecommerce,
			MontageInput:             prepared.params.MontageInput,
			HypitInput:               prepared.params.HypitInput,
		}})
		if err != nil {
			return h.respondTaskCreationServiceError(c, userID, err, "editable clone task failed", "克隆任务失败")
		}
		if len(tasks) == 0 {
			h.logger.Error().Str("task_id", id).Msg("editable clone returned no tasks")
			return Error(c, fiber.StatusInternalServerError, "克隆任务失败")
		}
		tasks[0].ReferenceImage = prepared.referenceView
		return Success(c, taskAPIResponse(tasks[0], h.store))
	}

	// Exact clones continue to use the source's frozen configuration. Re-check
	// its image-model entitlement because the user's tier may have changed.
	if err := h.validateImageCapabilityKeyForUser(c, userID, task.ImageCapabilityKey); err != nil {
		return Error(c, fiber.StatusForbidden, err.Error())
	}
	params := service.CloneTaskParams{ExecutionProfile: req.ExecutionProfile}
	if req.Prompt != nil {
		prompt := strings.TrimSpace(*req.Prompt)
		if utf8.RuneCountInString(prompt) > maxTaskPromptCharacters {
			return Error(c, fiber.StatusBadRequest, "prompt must not exceed 5120 characters")
		}
		params.Prompt = &prompt
	}
	if req.InputAttachments != nil {
		var pending repository.Repository
		if h.repo != nil {
			pending = h.repo
		}
		attachments, err := validateInputAttachments(c.Context(), h.service.Storage(), pending, userID, *req.InputAttachments, InputAttachmentValidationOptions{
			MaxCount:             maxAgentInputAttachments,
			AllowedTypes:         allAgentAttachmentTypes,
			AllowedAssetPurposes: taskInputAttachmentAssetPurposes,
		})
		if err != nil {
			return respondInputAttachmentError(c, h.logger, err)
		}
		params.InputAttachments = &attachments
	}
	params.AgentInput = req.AgentInput
	referenceView, err := h.presentCloneTaskReference(c.Context(), userID, task)
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}

	tasks, err := h.service.Clone(c.Context(), id, params)
	if err != nil {
		return h.respondTaskCreationServiceError(c, userID, err, "clone task failed", "克隆任务失败")
	}
	if len(tasks) == 0 {
		h.logger.Error().Str("task_id", id).Msg("clone returned no tasks")
		return Error(c, fiber.StatusInternalServerError, "克隆任务失败")
	}
	tasks[0].ReferenceImage = referenceView

	return Success(c, taskAPIResponse(tasks[0], h.store))
}

// Resume handles POST /api/v1/tasks/:id/resume.
// It requeues the same terminal task in its original workspace with optional
// supplemental prompt text and files.
func (h *TaskHandler) Resume(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}
	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	var req resumeTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if utf8.RuneCountInString(prompt) > maxTaskPromptCharacters {
		return Error(c, fiber.StatusBadRequest, fmt.Sprintf("补充指令不能超过 %d 个字符", maxTaskPromptCharacters))
	}
	if len(req.InputAttachments) > maxTaskResumeFiles {
		return Error(c, fiber.StatusBadRequest, fmt.Sprintf("at most %d attachments are allowed", maxTaskResumeFiles))
	}

	resumeStore := h.service.Storage()
	if len(req.InputAttachments) > 0 && resumeStore == nil {
		return Error(c, fiber.StatusServiceUnavailable, "补充文件存储暂不可用，请稍后重试")
	}
	var pending repository.Repository
	if h.repo != nil {
		pending = h.repo
	}
	validatedAttachments, err := validateInputAttachments(c.Context(), resumeStore, pending, userID, req.InputAttachments, InputAttachmentValidationOptions{
		MaxCount:             maxTaskResumeFiles,
		MaxBytes:             maxTaskResumeFileBytes,
		AllowedTypes:         allAgentAttachmentTypes,
		AllowedAssetPurposes: []string{service.DirectUploadPurposeAIEntryAttachment},
	})
	if err != nil {
		return respondInputAttachmentError(c, h.logger, err)
	}
	files := make([]service.ResumeTaskFile, 0, len(validatedAttachments))
	for i, attachment := range validatedAttachments {
		if attachment.AssetID == "" {
			return Error(c, fiber.StatusBadRequest, fmt.Sprintf("attachment %d requires asset_id", i+1))
		}
		files = append(files, service.ResumeTaskFile{
			AssetID:      attachment.AssetID,
			OriginalName: attachment.FileName,
			Label:        attachment.Instruction,
			Type:         attachment.Type,
			ContentType:  attachment.ContentType,
			Size:         attachment.Size,
		})
	}

	referenceView, err := h.presentTaskReference(c.Context(), userID, task)
	if err != nil {
		return respondReferenceAssetError(c, h.logger, err)
	}
	task, err = h.service.Resume(c.Context(), userID, id, service.ResumeTaskParams{
		Prompt: prompt,
		Files:  files,
	})
	if err != nil {
		if handled, response := respondAgentProfileError(c, err); handled {
			return response
		}
		switch {
		case errors.Is(err, service.ErrTaskResumeNoInput):
			return Error(c, fiber.StatusBadRequest, "请填写补充指令或上传补充文件")
		case errors.Is(err, service.ErrTaskResumeCompleted):
			return Error(c, fiber.StatusConflict, "已完成任务不可继续执行，请克隆任务创建新版本")
		case errors.Is(err, service.ErrTaskResumeNotTerminal):
			return Error(c, fiber.StatusConflict, "只有失败或已取消的任务可以继续执行")
		case errors.Is(err, service.ErrTaskResumeUnavailable):
			return Error(c, fiber.StatusServiceUnavailable, "继续执行仅支持 Kubernetes NAS 模式")
		case errors.Is(err, service.ErrTaskResumeStorageUnavailable):
			return Error(c, fiber.StatusServiceUnavailable, "补充文件存储暂不可用，请稍后重试")
		case errors.Is(err, service.ErrTaskResumeConflict):
			return Error(c, fiber.StatusConflict, "任务状态已变化，请刷新后重试")
		case errors.Is(err, service.ErrTaskResumeImageCapabilityMissing):
			return Error(c, fiber.StatusConflict, "任务缺少冻结的图像能力，请克隆任务创建新版本")
		default:
			h.logger.Error().Err(err).Str("task_id", id).Msg("resume task failed")
			return Error(c, fiber.StatusInternalServerError, "继续执行任务失败")
		}
	}
	task.ReferenceImage = referenceView

	return Success(c, taskAPIResponse(task, h.store))
}

// Delete handles DELETE /api/v1/tasks/:id.
func (h *TaskHandler) Delete(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	if task.Status == model.TaskStatusRunning {
		return Error(c, fiber.StatusConflict, "cannot delete a running task, cancel it first")
	}

	if err := h.service.Delete(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Msg("delete task failed")
		return Error(c, fiber.StatusInternalServerError, "failed to delete task")
	}

	return Success(c, fiber.Map{"message": "task deleted"})
}

// parseBulkTaskIDs validates the shared bulk request body: 1..100 well-formed
// UUIDs. Returns the trimmed IDs ready for lookup.
func (h *TaskHandler) parseBulkTaskIDs(c fiber.Ctx) ([]string, error) {
	var req bulkTaskIDsRequest
	if err := c.Bind().Body(&req); err != nil {
		return nil, Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	return validateBulkTaskIDs(c, req.TaskIDs)
}

func validateBulkTaskIDs(c fiber.Ctx, taskIDs []string) ([]string, error) {
	if len(taskIDs) == 0 {
		return nil, Error(c, fiber.StatusBadRequest, "task_ids is required")
	}
	if len(taskIDs) > 100 {
		return nil, Error(c, fiber.StatusBadRequest, "task_ids must not exceed 100")
	}
	ids := make([]string, 0, len(taskIDs))
	for _, id := range taskIDs {
		id = strings.TrimSpace(id)
		if _, err := uuid.Parse(id); err != nil {
			return nil, Error(c, fiber.StatusBadRequest, "invalid task_ids format")
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (h *TaskHandler) parseBulkCloneRequest(c fiber.Ctx) ([]string, string, error) {
	var req bulkCloneTasksRequest
	if err := c.Bind().Body(&req); err != nil {
		return nil, "", Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	ids, err := validateBulkTaskIDs(c, req.TaskIDs)
	if err != nil {
		return nil, "", err
	}
	req.ExecutionProfile = strings.TrimSpace(req.ExecutionProfile)
	if req.ExecutionProfile == "" {
		return nil, "", Error(c, fiber.StatusBadRequest, "execution_profile is required")
	}
	return ids, req.ExecutionProfile, nil
}

// BulkCancel handles POST /api/v1/tasks/bulk-cancel.
// Best-effort: cancels each pending/running task owned by the caller, skipping
// the rest (not found / not owned / already terminal) with a per-task reason.
func (h *TaskHandler) BulkCancel(c fiber.Ctx) error {
	ids, err := h.parseBulkTaskIDs(c)
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	results := make([]bulkTaskResult, 0, len(ids))
	succeeded := 0
	for _, id := range ids {
		task, err := h.service.GetByID(c.Context(), id)
		if err != nil {
			results = append(results, bulkTaskResult{ID: id, Reason: "not_found"})
			continue
		}
		if task.UserID != userID {
			results = append(results, bulkTaskResult{ID: id, Reason: "forbidden"})
			continue
		}
		if task.Status != model.TaskStatusPending && task.Status != model.TaskStatusRunning {
			results = append(results, bulkTaskResult{ID: id, Reason: "not_cancellable"})
			continue
		}
		if err := h.service.CancelForUser(c.Context(), userID, id); err != nil {
			h.logger.Error().Err(err).Str("task_id", id).Msg("bulk cancel: task failed")
			results = append(results, bulkTaskResult{ID: id, Reason: "failed"})
			continue
		}
		results = append(results, bulkTaskResult{ID: id, OK: true})
		succeeded++
	}
	return Success(c, bulkTasksResponse{Total: len(ids), Succeeded: succeeded, Skipped: len(ids) - succeeded, Results: results})
}

// BulkClone handles POST /api/v1/tasks/bulk-clone.
// Best-effort: clones each failed/cancelled task owned by the caller into a
// fresh billed task. Stops charging once credits are insufficient (each Clone
// re-reserves credits); remaining tasks are skipped with "insufficient_credits".
func (h *TaskHandler) BulkClone(c fiber.Ctx) error {
	ids, executionProfile, err := h.parseBulkCloneRequest(c)
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	results := make([]bulkTaskResult, 0, len(ids))
	succeeded := 0
	for _, id := range ids {
		task, err := h.service.GetByID(c.Context(), id)
		if err != nil {
			results = append(results, bulkTaskResult{ID: id, Reason: "not_found"})
			continue
		}
		if task.UserID != userID {
			results = append(results, bulkTaskResult{ID: id, Reason: "forbidden"})
			continue
		}
		if task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled {
			results = append(results, bulkTaskResult{ID: id, Reason: "not_cloneable"})
			continue
		}
		// Re-validate the image capability against the caller's current tier.
		if err := h.validateImageCapabilityKeyForUser(c, userID, task.ImageCapabilityKey); err != nil {
			results = append(results, bulkTaskResult{ID: id, Reason: "image_capability_unavailable"})
			continue
		}
		if _, err := h.presentCloneTaskReference(c.Context(), userID, task); err != nil {
			results = append(results, bulkTaskResult{ID: id, Reason: "reference_unavailable"})
			continue
		}
		newTasks, err := h.service.Clone(c.Context(), id, service.CloneTaskParams{ExecutionProfile: executionProfile})
		if err != nil {
			if errors.Is(err, service.ErrBillingInsufficientForTask) || errors.Is(err, service.ErrBillingDebtOutstanding) {
				results = append(results, bulkTaskResult{ID: id, Reason: "insufficient_credits"})
				continue
			}
			h.logger.Error().Err(err).Str("task_id", id).Msg("bulk clone: task failed")
			results = append(results, bulkTaskResult{ID: id, Reason: "failed"})
			continue
		}
		if len(newTasks) == 0 {
			h.logger.Error().Str("task_id", id).Msg("bulk clone returned no tasks")
			results = append(results, bulkTaskResult{ID: id, Reason: "failed"})
			continue
		}
		results = append(results, bulkTaskResult{ID: id, OK: true, NewTaskID: newTasks[0].ID})
		succeeded++
	}
	return Success(c, bulkTasksResponse{Total: len(ids), Succeeded: succeeded, Skipped: len(ids) - succeeded, Results: results})
}

// BulkDelete handles POST /api/v1/tasks/bulk-delete.
// Best-effort: deletes each non-running task owned by the caller, skipping the
// rest (not found / not owned / still running) with a per-task reason. Running
// tasks must be cancelled first (delete would orphan the in-flight execution).
func (h *TaskHandler) BulkDelete(c fiber.Ctx) error {
	ids, err := h.parseBulkTaskIDs(c)
	if err != nil {
		return err
	}
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	results := make([]bulkTaskResult, 0, len(ids))
	succeeded := 0
	for _, id := range ids {
		task, err := h.service.GetByID(c.Context(), id)
		if err != nil {
			results = append(results, bulkTaskResult{ID: id, Reason: "not_found"})
			continue
		}
		if task.UserID != userID {
			results = append(results, bulkTaskResult{ID: id, Reason: "forbidden"})
			continue
		}
		if task.Status == model.TaskStatusRunning {
			results = append(results, bulkTaskResult{ID: id, Reason: "running_cancel_first"})
			continue
		}
		if err := h.service.Delete(c.Context(), id); err != nil {
			h.logger.Error().Err(err).Str("task_id", id).Msg("bulk delete: task failed")
			results = append(results, bulkTaskResult{ID: id, Reason: "failed"})
			continue
		}
		results = append(results, bulkTaskResult{ID: id, OK: true})
		succeeded++
	}
	return Success(c, bulkTasksResponse{Total: len(ids), Succeeded: succeeded, Skipped: len(ids) - succeeded, Results: results})
}

// GetFiles handles GET /api/v1/tasks/:id/files.
func (h *TaskHandler) GetFiles(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before getting files.
	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	files, err := h.service.GetVisibleFiles(c.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Msg("get task files failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get task files")
	}
	return Success(c, files)
}

// Stream handles GET /api/v1/tasks/:id/stream for lifecycle and log events.
// When Redis pub/sub is available, it subscribes to typed events for immediate
// push delivery. Falls back to 1-second DB polling when Redis is unavailable.
func (h *TaskHandler) Stream(c fiber.Ctx) error {
	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before streaming.
	task, err := h.service.GetByID(c.Context(), taskID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	ctx := c.Context()

	// Try Redis pub/sub for real-time push; fall back to DB polling.
	pubsub := h.service.PubSub()
	if pubsub != nil && pubsub.Available() {
		return h.streamWithPubSub(c, ctx, taskID, pubsub)
	}
	return h.streamWithPolling(c, ctx, taskID)
}

func encodeTaskStreamEvent(event string, data any) (string, error) {
	payload, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("event: %s\ndata: %s\n\n", event, payload), nil
}

func writeTaskStreamEvent(c fiber.Ctx, event string, data any) error {
	encoded, err := encodeTaskStreamEvent(event, data)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(c, encoded)
	return err
}

func taskStreamTerminal(status string, lifecycle model.TaskLifecycle) bool {
	if status != model.TaskStatusCompleted && status != model.TaskStatusFailed && status != model.TaskStatusCancelled {
		return false
	}
	for _, stage := range lifecycle.Stages {
		if stage.Source != model.TaskLifecycleSourceServer {
			continue
		}
		switch stage.State {
		case model.TaskLifecycleStatePending, model.TaskLifecycleStateActive, model.TaskLifecycleStateBlocked:
			return false
		}
	}
	return true
}

// streamWithPubSub subscribes to distinct lifecycle and raw-log channels. A
// full lifecycle snapshot is sent first so reconnecting clients never need to
// reconstruct structured state from transient events.
func (h *TaskHandler) streamWithPubSub(c fiber.Ctx, ctx context.Context, taskID string, pubsub *service.RedisPubSub) error {
	lifecycleSub := pubsub.SubscribeLifecycle(ctx, taskID)
	logSub := pubsub.SubscribeLogs(ctx, taskID)
	if lifecycleSub == nil || logSub == nil {
		return h.streamWithPolling(c, ctx, taskID)
	}
	defer lifecycleSub.Close()
	defer logSub.Close()

	// Slow fallback poll every 5 seconds to catch any missed pub/sub events
	// and detect terminal states.
	fallbackTicker := time.NewTicker(5 * time.Second)
	defer fallbackTicker.Stop()

	// Max SSE timeout: 30 minutes.
	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()

	initial, err := h.service.GetByID(ctx, taskID)
	if err != nil {
		return nil
	}
	lastLogLen := len(initial.ProgressLog)
	lastLifecycleRevision := initial.Lifecycle.Data().Revision
	if lastLifecycleRevision > 0 {
		if err := writeTaskStreamEvent(c, "lifecycle", initial.Lifecycle.Data()); err != nil {
			return nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout.C:
			fmt.Fprintf(c, "event: timeout\ndata: {}\n\n")
			return nil
		case msg, ok := <-lifecycleSub.Events():
			if !ok {
				return h.streamWithPolling(c, ctx, taskID)
			}
			var event service.TaskLifecycleEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}
			if event.Lifecycle.Revision <= lastLifecycleRevision {
				continue
			}
			lastLifecycleRevision = event.Lifecycle.Revision
			if err := writeTaskStreamEvent(c, "lifecycle", event.Lifecycle); err != nil {
				return nil
			}
		case msg, ok := <-logSub.Events():
			if !ok {
				return h.streamWithPolling(c, ctx, taskID)
			}
			var event service.TaskLogEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}
			if err := writeTaskStreamEvent(c, "log", event.Message); err != nil {
				return nil
			}
		case <-fallbackTicker.C:
			task, err := h.service.GetByID(ctx, taskID)
			if err != nil {
				return nil
			}
			if len(task.ProgressLog) > lastLogLen {
				newLog := task.ProgressLog[lastLogLen:]
				lastLogLen = len(task.ProgressLog)
				if err := writeTaskStreamEvent(c, "log", newLog); err != nil {
					return nil
				}
			}
			if lifecycle := task.Lifecycle.Data(); lifecycle.Revision > lastLifecycleRevision {
				lastLifecycleRevision = lifecycle.Revision
				if err := writeTaskStreamEvent(c, "lifecycle", lifecycle); err != nil {
					return nil
				}
			}
			// Terminal state detection (reliable via DB).
			if taskStreamTerminal(task.Status, task.Lifecycle.Data()) {
				statusData, _ := json.Marshal(map[string]string{"status": task.Status})
				fmt.Fprintf(c, "event: %s\ndata: %s\n\n", task.Status, statusData)
				return nil
			}
		}
	}
}

// streamWithPolling falls back to 1-second DB polling for progress updates.
func (h *TaskHandler) streamWithPolling(c fiber.Ctx, ctx context.Context, taskID string) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()

	initial, err := h.service.GetByID(ctx, taskID)
	if err != nil {
		return nil
	}
	lastLogLen := len(initial.ProgressLog)
	lastLifecycleRevision := initial.Lifecycle.Data().Revision
	if lastLifecycleRevision > 0 {
		if err := writeTaskStreamEvent(c, "lifecycle", initial.Lifecycle.Data()); err != nil {
			return nil
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout.C:
			fmt.Fprintf(c, "event: timeout\ndata: {}\n\n")
			return nil
		case <-ticker.C:
			task, err := h.service.GetByID(ctx, taskID)
			if err != nil {
				return nil
			}

			if len(task.ProgressLog) > lastLogLen {
				newLog := task.ProgressLog[lastLogLen:]
				lastLogLen = len(task.ProgressLog)
				if err := writeTaskStreamEvent(c, "log", newLog); err != nil {
					return nil
				}
			}
			if lifecycle := task.Lifecycle.Data(); lifecycle.Revision > lastLifecycleRevision {
				lastLifecycleRevision = lifecycle.Revision
				if err := writeTaskStreamEvent(c, "lifecycle", lifecycle); err != nil {
					return nil
				}
			}

			if taskStreamTerminal(task.Status, task.Lifecycle.Data()) {
				statusData, _ := json.Marshal(map[string]string{
					"status": task.Status,
				})
				if _, err := fmt.Fprintf(c, "event: %s\ndata: %s\n\n", task.Status, statusData); err != nil {
					return nil
				}
				return nil
			}
		}
	}
}

// verifyTaskOwnership is a helper that fetches a task and verifies the
// authenticated user owns it. Returns the task on success, or writes an
// error response and returns nil.
func (h *TaskHandler) verifyTaskOwnership(c fiber.Ctx, taskID string) (*model.Task, error) {
	task, err := h.service.GetByID(c.Context(), taskID)
	if err != nil {
		Error(c, fiber.StatusNotFound, "task not found")
		return nil, fmt.Errorf("task not found: %w", err)
	}

	userID := GetUserID(c)
	if userID == "" {
		Error(c, fiber.StatusUnauthorized, "unauthorized")
		return nil, fmt.Errorf("unauthorized")
	}

	if task.UserID != userID {
		Forbidden(c, "you do not have access to this task")
		return nil, fmt.Errorf("ownership mismatch: user %s vs task owner %s", userID, task.UserID)
	}

	return task, nil
}

func taskFileContentDisposition(disposition, filename string) string {
	value := mime.FormatMediaType(disposition, map[string]string{"filename": filename})
	if value == "" {
		return disposition
	}
	return value
}

func setTaskFilePreviewHeaders(c fiber.Ctx, contentType, filename string) {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)
	disposition := "inline"
	if !taskFilePreviewMIMEAllowed(mediaType) {
		contentType = "application/octet-stream"
		disposition = "attachment"
	}
	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", taskFileContentDisposition(disposition, filename))
	c.Set("X-Content-Type-Options", "nosniff")

	switch mediaType {
	case "text/html", "application/xhtml+xml", "image/svg+xml":
		// Agent-produced preview content is untrusted. Keep it renderable while
		// preventing scripts, same-origin access, navigation, and form actions.
		c.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src 'self' https: data:; style-src 'unsafe-inline'")
		c.Set("Referrer-Policy", "no-referrer")
	}
}

func taskFilePreviewMIMEAllowed(mediaType string) bool {
	switch mediaType {
	case "text/plain", "text/markdown", "application/json",
		"text/html", "application/xhtml+xml", "image/svg+xml",
		"image/png", "image/jpeg", "image/gif", "image/webp",
		"audio/mpeg", "audio/mp4", "audio/wav", "audio/ogg", "audio/aac",
		"video/mp4", "video/quicktime", "video/webm":
		return true
	default:
		return false
	}
}

// DownloadFile handles GET /api/v1/tasks/:id/files/:fileId/download.
// It streams the file content as an attachment download.
func (h *TaskHandler) DownloadFile(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	fileID, err := validateUUIDParam(c, "fileId")
	if err != nil {
		return err
	}

	_, err = h.verifyTaskOwnership(c, taskID)
	if err != nil {
		return nil // error response already written
	}
	// Verify the file belongs to the task.
	if err := h.service.VerifyFileBelongsToTask(c.Context(), taskID, fileID); err != nil {
		return Error(c, fiber.StatusNotFound, "file not found")
	}
	fileForDownload, err := h.service.Repository().TaskFiles().FindByID(c.Context(), fileID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "file not found")
	}
	if err := h.service.RequireDownloadableTaskFile(c.Context(), taskID, fileForDownload); err != nil {
		if errors.Is(err, service.ErrTaskFileDownloadNotAllowed) {
			return Error(c, fiber.StatusForbidden, "过程文件不支持下载")
		}
		h.logger.Error().Err(err).Str("file_id", fileID).Msg("check task file delivery failed")
		return Error(c, fiber.StatusInternalServerError, "failed to check file delivery")
	}

	stream, file, err := h.service.GetFileStream(c.Context(), fileID)
	if err != nil {
		h.logger.Error().Err(err).Str("file_id", fileID).Msg("get file stream failed")
		return Error(c, fiber.StatusNotFound, "file not found")
	}

	contentType := file.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", taskFileContentDisposition("attachment", file.FileName))
	if file.FileSize > 0 && file.FileSize <= int64(^uint(0)>>1) {
		return c.SendStream(stream, int(file.FileSize))
	}
	return c.SendStream(stream)
}

// PreviewFile handles authenticated inline preview for both delivery and
// process files. Unlike DownloadFile, preview does not require delivery status.
func (h *TaskHandler) PreviewFile(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	fileID, err := validateUUIDParam(c, "fileId")
	if err != nil {
		return err
	}
	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil
	}
	if err := h.service.VerifyFileBelongsToTask(c.Context(), taskID, fileID); err != nil {
		return Error(c, fiber.StatusNotFound, "file not found")
	}
	stream, file, err := h.service.GetFileStream(c.Context(), fileID)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "file not found")
	}
	contentType := file.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	setTaskFilePreviewHeaders(c, contentType, file.FileName)
	return c.SendStream(stream)
}

// PreviewHTML handles GET /api/v1/tasks/:id/preview.
// It finds the HTML file for the task and returns it as the response body.
func (h *TaskHandler) PreviewHTML(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	_, err = h.verifyTaskOwnership(c, taskID)
	if err != nil {
		return nil // error response already written
	}
	// Find all files for the task and locate the HTML one.
	files, err := h.service.GetFiles(c.Context(), taskID)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("get task files failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get task files")
	}

	var htmlFile *model.TaskFile
	for _, f := range files {
		if f.Role == model.FileRoleHTML {
			htmlFile = f
			break
		}
	}

	if htmlFile == nil {
		return Error(c, fiber.StatusNotFound, "no HTML file found for this task")
	}

	stream, _, err := h.service.GetFileStream(c.Context(), htmlFile.ID)
	if err != nil {
		h.logger.Error().Err(err).Str("file_id", htmlFile.ID).Msg("get HTML file stream failed")
		return Error(c, fiber.StatusInternalServerError, "failed to read HTML file")
	}
	defer stream.Close()

	htmlContent, err := io.ReadAll(stream)
	if err != nil {
		h.logger.Error().Err(err).Msg("read HTML content failed")
		return Error(c, fiber.StatusInternalServerError, "failed to read HTML file")
	}

	// Rewrite relative image URLs to absolute storage URLs so images render in preview.
	fileMap := make(map[string]string)
	for _, f := range files {
		if (f.Role == model.FileRoleImage || f.Role == model.FileRoleCover) && f.URL != "" {
			fileMap[strings.ToLower(f.FileName)] = f.URL
		}
	}
	if len(fileMap) > 0 {
		htmlContent = service.RewriteHTMLImageURLs(htmlContent, fileMap)
	}

	setTaskFilePreviewHeaders(c, "text/html; charset=utf-8", htmlFile.FileName)
	c.Set("Content-Length", strconv.Itoa(len(htmlContent)))

	return c.Send(htmlContent)
}

// DownloadZip handles GET /api/v1/tasks/:id/files/zip.
// It creates a ZIP archive of all task files and sends it as a download.
func (h *TaskHandler) DownloadZip(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	_, err = h.verifyTaskOwnership(c, taskID)
	if err != nil {
		return nil // error response already written
	}
	stream, zipName, err := h.service.DownloadZip(c.Context(), taskID)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("download zip failed")
		if errors.Is(err, service.ErrNoDownloadableDeliveryFiles) {
			return Error(c, fiber.StatusNotFound, "没有可下载的交付文件")
		}
		return Error(c, fiber.StatusNotFound, "failed to create ZIP archive")
	}

	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", taskFileContentDisposition("attachment", zipName))

	return c.SendStream(stream)
}

// DownloadRetainedZip handles GET /api/v1/tasks/:id/files/retained/zip.
func (h *TaskHandler) DownloadRetainedZip(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}
	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil
	}
	stream, zipName, err := h.service.DownloadRetainedZip(c.Context(), taskID)
	if err != nil {
		if errors.Is(err, service.ErrNoDownloadableDeliveryFiles) {
			return Error(c, fiber.StatusNotFound, "没有可下载的已保留产物")
		}
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("download retained zip failed")
		return Error(c, fiber.StatusNotFound, "failed to create retained ZIP archive")
	}
	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", taskFileContentDisposition("attachment", zipName))
	return c.SendStream(stream)
}

// DownloadTasksZip handles POST /api/v1/tasks/files/zip.
// It creates a single ZIP archive containing all readable files from selected completed tasks.
func (h *TaskHandler) DownloadTasksZip(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var req bulkDownloadTaskFilesRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}
	if len(req.TaskIDs) == 0 {
		return Error(c, fiber.StatusBadRequest, "task_ids is required")
	}
	if len(req.TaskIDs) > 100 {
		return Error(c, fiber.StatusBadRequest, "task_ids must not exceed 100")
	}
	for _, id := range req.TaskIDs {
		id = strings.TrimSpace(id)
		if _, err := uuid.Parse(id); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid task_ids format")
		}
		task, err := h.service.GetByID(c.Context(), id)
		if err != nil {
			return Error(c, fiber.StatusNotFound, "task not found")
		}
		if task.UserID != userID {
			return Forbidden(c, "you do not have access to this task")
		}
	}

	buf, zipName, err := h.service.DownloadTasksZip(c.Context(), userID, req.TaskIDs)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("bulk download zip failed")
		if errors.Is(err, service.ErrNoDownloadableDeliveryFiles) {
			return Error(c, fiber.StatusNotFound, "没有可下载的交付文件")
		}
		return Error(c, fiber.StatusNotFound, "failed to create ZIP archive")
	}

	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	c.Set("Content-Length", strconv.Itoa(buf.Len()))

	return c.Send(buf.Bytes())
}

// GetLog handles GET /api/v1/tasks/:id/log.
// It returns the per-task agent execution log file content.
func (h *TaskHandler) GetLog(c fiber.Ctx) error {
	if h.taskLogDir == "" {
		return Error(c, fiber.StatusNotFound, "task logging is not configured")
	}

	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil // error response already written
	}

	logPath := filepath.Join(h.taskLogDir, taskID+".log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Error(c, fiber.StatusNotFound, "task log not found")
		}
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("failed to read task log")
		return Error(c, fiber.StatusInternalServerError, "failed to read task log")
	}

	c.Set("Content-Type", "text/plain; charset=utf-8")
	if c.Query("download") == "true" {
		c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="task-%s.log"`, taskID))
	}
	return c.Send(data)
}

// ServeLocalFile handles GET /api/v1/files/* for the local storage provider.
// It serves files from the local data directory with path traversal protection
// and verifies the requesting user owns the file (key prefix = userID).
func (h *TaskHandler) ServeLocalFile(c fiber.Ctx) error {
	if h.dataDir == "" {
		return Error(c, fiber.StatusNotFound, "local file serving is not configured")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	key := c.Params("*")
	if key == "" {
		return Error(c, fiber.StatusBadRequest, "file path is required")
	}

	// Prevent path traversal.
	cleanKey := filepath.Clean(key)
	if strings.Contains(cleanKey, "..") {
		return Error(c, fiber.StatusBadRequest, "invalid file path")
	}

	// Verify ownership: task workspace files live under {userID}/{taskID}/...,
	// user uploads under uploads/{projects|references|channels}/{userID}/
	// (channels is the legacy prefix from the channel→project rename).
	if !isUserOwnedStorageKey(userID, cleanKey, userID+"/") {
		return Forbidden(c, "you do not have access to this file")
	}

	filePath := filepath.Join(h.dataDir, cleanKey)

	// Ensure the resolved path is within dataDir.
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid file path")
	}
	absDataDir, err := filepath.Abs(h.dataDir)
	if err != nil {
		return Error(c, fiber.StatusInternalServerError, "server configuration error")
	}
	if !strings.HasPrefix(absPath, absDataDir+string(filepath.Separator)) {
		return Error(c, fiber.StatusForbidden, "access denied")
	}

	// Detect content type from extension.
	ext := filepath.Ext(cleanKey)
	switch strings.ToLower(ext) {
	case ".html", ".htm":
		c.Set("Content-Type", "text/html; charset=utf-8")
	case ".css":
		c.Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		c.Set("Content-Type", "application/javascript")
	case ".json":
		c.Set("Content-Type", "application/json")
	case ".png":
		c.Set("Content-Type", "image/png")
	case ".jpg", ".jpeg":
		c.Set("Content-Type", "image/jpeg")
	case ".gif":
		c.Set("Content-Type", "image/gif")
	case ".webp":
		c.Set("Content-Type", "image/webp")
	case ".svg":
		c.Set("Content-Type", "image/svg+xml")
		c.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
		c.Set("X-Content-Type-Options", "nosniff")
	case ".pdf":
		c.Set("Content-Type", "application/pdf")
	}

	return c.SendFile(absPath)
}

// validateImageCapabilityKeyForUser delegates to the package-level helper, binding
// this handler's repository and image capabilities. See
// validateImageCapabilityKeyForUser for the fail-closed tier-resolution rules.
func (h *TaskHandler) validateImageCapabilityKeyForUser(c fiber.Ctx, userID, key string) error {
	return validateImageCapabilityKeyForUser(c.Context(), h.repo, userID, key, h.imageCapabilityRoutes.Capabilities)
}

// validateUUIDParam extracts and validates that a path parameter is a valid UUID.
// Returns the UUID string or writes an error response and returns empty string.
func validateUUIDParam(c fiber.Ctx, paramName string) (string, error) {
	val := c.Params(paramName)
	if val == "" {
		return "", Error(c, fiber.StatusBadRequest, paramName+" is required")
	}
	if _, err := uuid.Parse(val); err != nil {
		return "", Error(c, fiber.StatusBadRequest, "invalid "+paramName+" format")
	}
	return val, nil
}

// UsageStats handles GET /api/v1/usage/stats.
// Returns aggregated LLM token usage and cost statistics for the authenticated user.
func (h *TaskHandler) UsageStats(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Parse optional date range. Defaults to last 30 days.
	from := time.Now().AddDate(0, 0, -30)
	to := time.Now()

	if v := c.Query("from"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid 'from' date format, use YYYY-MM-DD")
		}
		from = parsed
	}
	if v := c.Query("to"); v != "" {
		parsed, err := time.Parse("2006-01-02", v)
		if err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid 'to' date format, use YYYY-MM-DD")
		}
		// Include the entire end day.
		to = parsed.AddDate(0, 0, 1)
	}

	projectID := c.Query("project_id")

	stats, err := h.service.GetUsageStats(c.Context(), userID, from, to, projectID)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("get usage stats failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get usage stats")
	}

	return Success(c, stats)
}
