package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
)

const (
	maxTaskPromptCharacters         = 5120
	maxGoalTextCharacters           = 4000
	maxTaskResumeFiles              = 10
	maxTaskResumeFileBytes          = 25 * 1024 * 1024
	maxVideoProductionArtifactBytes = 512 * 1024
)

// TaskHandler handles task-related HTTP endpoints.
type TaskHandler struct {
	service      *service.TaskService
	logger       *zerolog.Logger
	dataDir      string // local storage data directory (for ServeLocalFile)
	taskLogDir   string // task log directory (for GetLog)
	imagePresets []config.ImageModelPreset
	repo         repository.Repository
}

// NewTaskHandler creates a new TaskHandler.
//
// Optional variadic options:
//   - first string arg: local data directory (for ServeLocalFile).
//   - WithImagePresets / WithRepository: configure tier-gated image model validation.
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

// SetImagePresets wires the system-managed image model presets for tier-gated
// validation of createTaskRequest.ImageModelKey.
func (h *TaskHandler) SetImagePresets(presets []config.ImageModelPreset) {
	h.imagePresets = presets
}

// SetRepository wires the user repository so the handler can resolve the caller's
// tier for image-model validation.
func (h *TaskHandler) SetRepository(repo repository.Repository) {
	h.repo = repo
}

// Request types.

type createTaskRequest struct {
	ProjectID          string `json:"project_id"`
	Prompt             string `json:"prompt"`
	Quantity           int    `json:"quantity"`
	ImageRatio         string `json:"image_ratio"`
	ImageModelKey      string `json:"image_model_key"`
	SkipReferenceImage *bool  `json:"skip_reference_image"`
	ReferenceImageURL  string `json:"reference_image_url"`
	Watermark          *bool  `json:"watermark"`
	Goal               string `json:"goal"`
	GoalMode           bool   `json:"goal_mode"`
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
	// E-commerce package fields (project platform = "ecommerce"). SelectedModules
	// maps a module key (main_images / detail_page / cover_banner / share_image /
	// sku_images) to its quantity; the task cost = sum(unit price × quantity).
	// ProductPhotos are server-owned URLs (from /files/upload) materialized into
	// the agent workspace by the executor.
	ProductPhotos            []string               `json:"product_photos,omitempty"`
	SelectedModules          map[string]int         `json:"selected_modules,omitempty"`
	TargetPlatform           string                 `json:"target_platform,omitempty"`
	SellingPoints            string                 `json:"selling_points,omitempty"`
	Language                 string                 `json:"language,omitempty"`
	ProviderStrategyOverride string                 `json:"provider_strategy_override,omitempty"`
	VideoConfig              *model.VideoTaskConfig `json:"video_config,omitempty"`
	// ExecutionTarget, when "local", routes the task to the caller's desktop
	// local executor instead of cloud execution. Set by the desktop studio build
	// when a local executor is available. Empty = cloud (default). See
	// model.ExecutionTarget*.
	ExecutionTarget string `json:"execution_target,omitempty"`
}

type bulkDownloadTaskFilesRequest struct {
	TaskIDs []string `json:"task_ids"`
}

// bulkTaskIDsRequest is the shared request body for bulk cancel / clone / delete.
type bulkTaskIDsRequest struct {
	TaskIDs []string `json:"task_ids"`
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

type taskDetailResponse struct {
	*model.Task
	CreditsCharged     *int                       `json:"credits_charged,omitempty"`
	CreditsSummary     *taskCreditsSummary        `json:"credits_summary,omitempty"`
	CreditTransactions []*model.CreditTransaction `json:"credit_transactions,omitempty"`
}

type taskCreditsSummary struct {
	TaskConsumed      int `json:"task_consumed"`
	OperationConsumed int `json:"operation_consumed"`
	Refunded          int `json:"refunded"`
	NetConsumed       int `json:"net_consumed"`
}

type videoProductionResponse struct {
	TaskID         string                             `json:"task_id"`
	ScenarioKey    string                             `json:"scenario_key,omitempty"`
	ProductionMode string                             `json:"production_mode,omitempty"`
	Artifacts      map[string]videoProductionArtifact `json:"artifacts"`
	RetakeActions  []string                           `json:"retake_actions"`
	NextActions    []string                           `json:"next_actions"`
}

type videoProductionArtifact struct {
	Status     string         `json:"status"`
	FileID     string         `json:"file_id,omitempty"`
	FileName   string         `json:"file_name"`
	URL        string         `json:"url,omitempty"`
	Content    string         `json:"content,omitempty"`
	ParsedJSON map[string]any `json:"parsed_json,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// Create handles POST /api/v1/tasks.
func (h *TaskHandler) Create(c fiber.Ctx) error {
	var req createTaskRequest
	if err := c.Bind().Body(&req); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if req.ProjectID == "" {
		return Error(c, fiber.StatusBadRequest, "project_id is required")
	}

	prompt := strings.TrimSpace(req.Prompt)
	if utf8.RuneCountInString(prompt) > maxTaskPromptCharacters {
		return Error(c, fiber.StatusBadRequest, "prompt must not exceed 5120 characters")
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	quantity := req.Quantity
	if quantity <= 0 {
		quantity = 1
	}
	if quantity > 5 {
		return Error(c, fiber.StatusBadRequest, "quantity must be between 1 and 5")
	}

	if req.ImageRatio != "" && !model.ValidImageRatios[req.ImageRatio] {
		return Error(c, fiber.StatusBadRequest, "image_ratio must be one of: 3:4, 1:1, 4:3, 16:9")
	}

	if !validReferenceImageURL(req.ReferenceImageURL) {
		return Error(c, fiber.StatusBadRequest, "reference_image_url must be an internal file path or an http(s) URL")
	}
	for _, u := range req.ProductPhotos {
		if !validReferenceImageURL(u) {
			return Error(c, fiber.StatusBadRequest, "product_photos must be internal file paths or http(s) URLs")
		}
	}

	// Validate image_model_key against the caller's tier.
	if err := h.validateImageModelKeyForUser(c, userID, req.ImageModelKey); err != nil {
		return Error(c, fiber.StatusForbidden, err.Error())
	}

	// Validate goal text length when goal mode is enabled.
	if req.GoalMode {
		goalText := strings.TrimSpace(req.Goal)
		if goalText == "" {
			return Error(c, fiber.StatusBadRequest, "goal must not be empty when goal_mode is true")
		}
		if utf8.RuneCountInString(goalText) > maxGoalTextCharacters {
			return Error(c, fiber.StatusBadRequest, "goal must not exceed 4000 characters")
		}
	}
	if h.repo != nil {
		if err := finalizePendingURLs(c.Context(), h.repo.PendingUploads(), userID, service.DirectUploadPurposeTaskReference, []string{req.ReferenceImageURL}); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := finalizePendingURLs(c.Context(), h.repo.PendingUploads(), userID, service.DirectUploadPurposeEcommercePhoto, req.ProductPhotos); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if err := finalizePendingURLs(c.Context(), h.repo.PendingUploads(), userID, service.DirectUploadPurposeVideoReference, videoReferenceURLs(req.VideoConfig)); err != nil {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
	}

	// Build the e-commerce package config when any e-commerce field is present.
	// The service only consults it when the project platform is "ecommerce".
	var ecommerceCfg *model.EcommerceConfig
	if len(req.SelectedModules) > 0 || len(req.ProductPhotos) > 0 || req.TargetPlatform != "" || req.SellingPoints != "" {
		ecommerceCfg = &model.EcommerceConfig{
			ProductPhotos:            req.ProductPhotos,
			SelectedModules:          req.SelectedModules,
			TargetPlatform:           req.TargetPlatform,
			SellingPoints:            req.SellingPoints,
			Language:                 req.Language,
			ProviderStrategyOverride: req.ProviderStrategyOverride,
		}
	}

	tasks, err := h.service.CreateManual(c.Context(), service.CreateManualParams{
		UserID:                   userID,
		ProjectID:                req.ProjectID,
		Prompt:                   prompt,
		Quantity:                 quantity,
		ImageRatio:               req.ImageRatio,
		ImageModelKey:            req.ImageModelKey,
		SkipRefImage:             req.SkipReferenceImage,
		ReferenceImageURL:        req.ReferenceImageURL,
		Watermark:                req.Watermark,
		Goal:                     req.Goal,
		GoalMode:                 req.GoalMode,
		HasContentImage:          req.HasContentImage,
		HasTailImage:             req.HasTailImage,
		ArticleWithCover:         req.ArticleWithCover,
		ArticleWithContentImages: req.ArticleWithContentImages,
		Ecommerce:                ecommerceCfg,
		Video:                    req.VideoConfig,
		ExecutionTarget:          req.ExecutionTarget,
	})
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("create task failed")
		if errors.Is(err, service.ErrMinimumVideoBalance) {
			return Error(c, fiber.StatusPaymentRequired, "视频任务需至少 100000 积分余额")
		}
		if errors.Is(err, service.ErrVideoGenerationConfig) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, service.ErrInsufficientCredits) {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
				"code": 40200,
				"msg":  "insufficient_credits",
			})
		}
		return Error(c, fiber.StatusInternalServerError, "failed to create task")
	}

	// Return single task for quantity=1 (frontend expects Task, not Task[]).
	if quantity == 1 && len(tasks) > 0 {
		return Success(c, tasks[0])
	}
	return Success(c, tasks)
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

	return Success(c, fiber.Map{
		"items": tasks,
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

	resp := taskDetailResponse{Task: task}
	if h.repo != nil {
		if tx, err := h.repo.Credits().FindDeductionByTaskID(c.Context(), task.ID); err == nil && tx != nil && tx.Amount < 0 {
			charged := -tx.Amount
			resp.CreditsCharged = &charged
		}
		if transactions, err := h.repo.Credits().FindByTaskIDAndUserID(c.Context(), task.ID, userID); err == nil {
			resp.CreditTransactions = transactions
			summary := summarizeTaskCredits(transactions)
			resp.CreditsSummary = &summary
		}
	}

	return Success(c, resp)
}

func summarizeTaskCredits(transactions []*model.CreditTransaction) taskCreditsSummary {
	var summary taskCreditsSummary
	for _, tx := range transactions {
		if tx == nil {
			continue
		}
		if tx.Amount < 0 {
			consumed := -tx.Amount
			if tx.Type == model.CreditTypeTaskDeduct {
				summary.TaskConsumed += consumed
			} else {
				summary.OperationConsumed += consumed
			}
			continue
		}
		if tx.Amount > 0 {
			summary.Refunded += tx.Amount
		}
	}
	summary.NetConsumed = summary.TaskConsumed + summary.OperationConsumed - summary.Refunded
	if summary.NetConsumed < 0 {
		summary.NetConsumed = 0
	}
	return summary
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

	// Re-validate the image model against the caller's current tier (tier may
	// have changed since the original task was created).
	if err := h.validateImageModelKeyForUser(c, userID, task.ImageModelKey); err != nil {
		return Error(c, fiber.StatusForbidden, err.Error())
	}

	newTask, err := h.service.Clone(c.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", id).Msg("clone task failed")
		if errors.Is(err, service.ErrMinimumVideoBalance) {
			return Error(c, fiber.StatusPaymentRequired, "视频任务需至少 100000 积分余额")
		}
		if errors.Is(err, service.ErrVideoGenerationConfig) {
			return Error(c, fiber.StatusBadRequest, err.Error())
		}
		if errors.Is(err, service.ErrInsufficientCredits) {
			return c.Status(fiber.StatusPaymentRequired).JSON(fiber.Map{
				"code": 40200,
				"msg":  "insufficient_credits",
			})
		}
		return Error(c, fiber.StatusInternalServerError, "克隆任务失败")
	}

	return Success(c, newTask)
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

	prompt := strings.TrimSpace(c.FormValue("prompt"))
	if utf8.RuneCountInString(prompt) > maxTaskPromptCharacters {
		return Error(c, fiber.StatusBadRequest, fmt.Sprintf("补充指令不能超过 %d 个字符", maxTaskPromptCharacters))
	}

	var labels []string
	if raw := strings.TrimSpace(c.FormValue("file_labels")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &labels); err != nil {
			return Error(c, fiber.StatusBadRequest, "file_labels must be a JSON string array")
		}
	}

	form, _ := c.MultipartForm()
	fileHeaders := formFiles(form, "files")
	if len(fileHeaders) > maxTaskResumeFiles {
		return Error(c, fiber.StatusBadRequest, fmt.Sprintf("补充文件最多上传 %d 个", maxTaskResumeFiles))
	}

	files := make([]service.ResumeTaskFile, 0, len(fileHeaders))
	opened := make([]io.Closer, 0, len(fileHeaders))
	defer func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}()
	for i, header := range fileHeaders {
		if header == nil {
			continue
		}
		if header.Size > maxTaskResumeFileBytes {
			return Error(c, fiber.StatusBadRequest, fmt.Sprintf("补充文件不能超过 %dMB", maxTaskResumeFileBytes/(1024*1024)))
		}
		src, err := header.Open()
		if err != nil {
			return Error(c, fiber.StatusBadRequest, "读取补充文件失败")
		}
		opened = append(opened, src)
		label := ""
		if i < len(labels) {
			label = labels[i]
		}
		files = append(files, service.ResumeTaskFile{
			OriginalName: header.Filename,
			Label:        label,
			Reader:       src,
			Size:         header.Size,
		})
	}

	task, err = h.service.Resume(c.Context(), userID, id, service.ResumeTaskParams{
		Prompt: prompt,
		Files:  files,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTaskResumeNoInput):
			return Error(c, fiber.StatusBadRequest, "请填写补充指令或上传补充文件")
		case errors.Is(err, service.ErrTaskResumeNotTerminal):
			return Error(c, fiber.StatusConflict, "只有已完成、失败或已取消的任务可以继续执行")
		case errors.Is(err, service.ErrTaskResumeWorkspaceMissing):
			return Error(c, fiber.StatusConflict, "原任务工作目录已清理，无法继续执行。可以使用“克隆任务”创建新任务。")
		case errors.Is(err, service.ErrTaskResumeConflict):
			return Error(c, fiber.StatusConflict, "任务状态已变化，请刷新后重试")
		default:
			h.logger.Error().Err(err).Str("task_id", id).Msg("resume task failed")
			return Error(c, fiber.StatusInternalServerError, "继续执行任务失败")
		}
	}

	return Success(c, task)
}

func formFiles(form *multipart.Form, key string) []*multipart.FileHeader {
	if form == nil || form.File == nil {
		return nil
	}
	return form.File[key]
}

// PublishApprove handles POST /api/v1/tasks/:id/publish-approve: resumes a held
// publish-approval gate (Batch 4A) and publishes the frozen draft to the WeChat
// draft box. The actual publish runs asynchronously; this returns once the task
// is marked approved.
func (h *TaskHandler) PublishApprove(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	// Verify ownership before resuming the publish.
	task, err := h.service.GetByID(c.Context(), id)
	if err != nil {
		return Error(c, fiber.StatusNotFound, "task not found")
	}
	if task.UserID != userID {
		return Forbidden(c, "you do not have access to this task")
	}

	if err := h.service.ApprovePublish(c.Context(), id); err != nil {
		h.logger.Error().Err(err).Str("task_id", id).Msg("approve publish failed")
		switch {
		case errors.Is(err, service.ErrPublishApprovalNotPending):
			return Error(c, fiber.StatusConflict, "该任务不在待审核发布状态")
		case errors.Is(err, service.ErrPublishApprovalUnavailable):
			return Error(c, fiber.StatusConflict, "无法发布：项目未启用发布或草稿数据缺失")
		}
		return Error(c, fiber.StatusInternalServerError, "放行发布失败")
	}

	return Success(c, fiber.Map{"approved": true})
}

// PublishReject handles POST /api/v1/tasks/:id/publish-reject: closes the
// publish-approval gate without publishing. Accepts an optional {"reason": "..."}
// body; the reason is surfaced to the user via the progress event.
func (h *TaskHandler) PublishReject(c fiber.Ctx) error {
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

	var req struct {
		Reason string `json:"reason"`
	}
	// Body is optional — a POST with no body simply rejects with no note.
	_ = c.Bind().Body(&req)

	if err := h.service.RejectPublish(c.Context(), id, req.Reason); err != nil {
		h.logger.Error().Err(err).Str("task_id", id).Msg("reject publish failed")
		if errors.Is(err, service.ErrPublishApprovalNotPending) {
			return Error(c, fiber.StatusConflict, "该任务不在待审核发布状态")
		}
		return Error(c, fiber.StatusInternalServerError, "驳回发布失败")
	}

	return Success(c, fiber.Map{"rejected": true})
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
	if len(req.TaskIDs) == 0 {
		return nil, Error(c, fiber.StatusBadRequest, "task_ids is required")
	}
	if len(req.TaskIDs) > 100 {
		return nil, Error(c, fiber.StatusBadRequest, "task_ids must not exceed 100")
	}
	ids := make([]string, 0, len(req.TaskIDs))
	for _, id := range req.TaskIDs {
		id = strings.TrimSpace(id)
		if _, err := uuid.Parse(id); err != nil {
			return nil, Error(c, fiber.StatusBadRequest, "invalid task_ids format")
		}
		ids = append(ids, id)
	}
	return ids, nil
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
		if task.Status != model.TaskStatusFailed && task.Status != model.TaskStatusCancelled {
			results = append(results, bulkTaskResult{ID: id, Reason: "not_cloneable"})
			continue
		}
		// Re-validate the image model against the caller's current tier.
		if err := h.validateImageModelKeyForUser(c, userID, task.ImageModelKey); err != nil {
			results = append(results, bulkTaskResult{ID: id, Reason: "image_model_unavailable"})
			continue
		}
		newTask, err := h.service.Clone(c.Context(), id)
		if err != nil {
			if errors.Is(err, service.ErrInsufficientCredits) {
				results = append(results, bulkTaskResult{ID: id, Reason: "insufficient_credits"})
				continue
			}
			h.logger.Error().Err(err).Str("task_id", id).Msg("bulk clone: task failed")
			results = append(results, bulkTaskResult{ID: id, Reason: "failed"})
			continue
		}
		results = append(results, bulkTaskResult{ID: id, OK: true, NewTaskID: newTask.ID})
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

// MarkPublished handles PATCH /api/v1/tasks/:id/published.
func (h *TaskHandler) MarkPublished(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	var body struct {
		Published bool `json:"published"`
	}
	if err := c.Bind().Body(&body); err != nil {
		return Error(c, fiber.StatusBadRequest, "invalid request body")
	}

	if err := h.service.SetPublished(c.Context(), userID, id, body.Published); err != nil {
		h.logger.Error().Err(err).Msg("mark published failed")
		return Error(c, fiber.StatusInternalServerError, "failed to update published status")
	}

	return Success(c, fiber.Map{"published": body.Published})
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

	files, err := h.service.GetFiles(c.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Msg("get task files failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get task files")
	}

	return Success(c, files)
}

// GetVideoProduction handles GET /api/v1/tasks/:id/video-production.
func (h *TaskHandler) GetVideoProduction(c fiber.Ctx) error {
	id, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	task, err := h.verifyTaskOwnership(c, id)
	if err != nil {
		return nil
	}
	if task.Type != model.PlatformVideo {
		return Error(c, fiber.StatusBadRequest, "task is not a video task")
	}

	files, err := h.service.GetFiles(c.Context(), id)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", id).Msg("get task files for video production failed")
		return Error(c, fiber.StatusInternalServerError, "failed to get task files")
	}

	filesByArtifact := videoProductionFilesByName(files)
	artifacts := make(map[string]videoProductionArtifact, len(service.VideoProductionArtifactNames()))
	for _, artifactName := range service.VideoProductionArtifactNames() {
		file := filesByArtifact[artifactName]
		if file == nil {
			artifacts[artifactName] = videoProductionArtifact{
				Status:   "missing",
				FileName: artifactName,
			}
			continue
		}
		artifacts[artifactName] = h.readVideoProductionArtifact(c.Context(), file, artifactName)
	}

	cfg := task.VideoConfig.Data()
	return Success(c, videoProductionResponse{
		TaskID:         task.ID,
		ScenarioKey:    cfg.ScenarioKey,
		ProductionMode: cfg.ProductionMode,
		Artifacts:      artifacts,
		RetakeActions:  service.VideoRetakeActions(),
		NextActions:    service.VideoNextActions(),
	})
}

func videoProductionFilesByName(files []*model.TaskFile) map[string]*model.TaskFile {
	result := map[string]*model.TaskFile{}
	expected := map[string]bool{}
	for _, name := range service.VideoProductionArtifactNames() {
		expected[name] = true
	}
	for _, file := range files {
		if file == nil {
			continue
		}
		candidates := []string{
			file.FileName,
			file.FilePath,
			filepath.Base(file.FilePath),
		}
		for _, candidate := range candidates {
			clean := strings.TrimSpace(candidate)
			if !expected[clean] || result[clean] != nil {
				continue
			}
			result[clean] = file
		}
	}
	return result
}

func (h *TaskHandler) readVideoProductionArtifact(ctx context.Context, file *model.TaskFile, artifactName string) videoProductionArtifact {
	artifact := videoProductionArtifact{
		Status:   "available",
		FileID:   file.ID,
		FileName: artifactName,
		URL:      file.URL,
	}
	stream, _, err := h.service.GetFileStream(ctx, file.ID)
	if err != nil {
		artifact.Status = "error"
		artifact.Error = "failed to read artifact"
		return artifact
	}
	defer stream.Close()

	data, err := io.ReadAll(io.LimitReader(stream, maxVideoProductionArtifactBytes+1))
	if err != nil {
		artifact.Status = "error"
		artifact.Error = "failed to read artifact"
		return artifact
	}
	if len(data) > maxVideoProductionArtifactBytes {
		artifact.Content = string(data[:maxVideoProductionArtifactBytes])
		artifact.Error = "artifact content truncated at 512KB"
		return artifact
	}
	artifact.Content = string(data)
	if strings.EqualFold(filepath.Ext(artifactName), ".json") {
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			artifact.Error = "failed to parse JSON artifact"
		} else {
			artifact.ParsedJSON = parsed
		}
	}
	return artifact
}

// Stream handles GET /api/v1/tasks/:id/stream — SSE endpoint for real-time progress.
// When Redis pub/sub is available, it subscribes to progress events for immediate
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

// streamWithPubSub uses Redis pub/sub for real-time progress delivery.
// It also runs a slow DB poll as a fallback for any events missed by pub/sub
// and for terminal state detection.
func (h *TaskHandler) streamWithPubSub(c fiber.Ctx, ctx context.Context, taskID string, pubsub *service.RedisPubSub) error {
	sub := pubsub.SubscribeProgress(ctx, taskID)
	if sub == nil {
		return h.streamWithPolling(c, ctx, taskID)
	}
	defer sub.Close()

	// Slow fallback poll every 5 seconds to catch any missed pub/sub events
	// and detect terminal states.
	fallbackTicker := time.NewTicker(5 * time.Second)
	defer fallbackTicker.Stop()

	// Max SSE timeout: 30 minutes.
	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()

	// Fetch initial progress log length from DB.
	lastLogLen := 0
	if task, err := h.service.GetByID(ctx, taskID); err == nil {
		lastLogLen = len(task.ProgressLog)
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timeout.C:
			fmt.Fprintf(c, "event: timeout\ndata: {}\n\n")
			return nil
		case msg, ok := <-sub.Events():
			if !ok {
				// Subscription closed, fall back to polling.
				return h.streamWithPolling(c, ctx, taskID)
			}
			var event service.ProgressEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				continue
			}
			if event.Stage != "" || event.Percent > 0 {
				payload := map[string]any{
					"stage":       event.Stage,
					"title":       event.Title,
					"description": event.Description,
					"percent":     event.Percent,
				}
				data, _ := json.Marshal(payload)
				if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", data); err != nil {
					return nil
				}
			} else {
				escaped, _ := json.Marshal(event.Message)
				if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", escaped); err != nil {
					return nil
				}
			}
		case <-fallbackTicker.C:
			task, err := h.service.GetByID(ctx, taskID)
			if err != nil {
				return nil
			}
			// Send any progress entries missed by pub/sub.
			if len(task.ProgressLog) > lastLogLen {
				newLog := task.ProgressLog[lastLogLen:]
				lastLogLen = len(task.ProgressLog)
				escaped, _ := json.Marshal(newLog)
				if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", escaped); err != nil {
					return nil
				}
			}
			// Terminal state detection (reliable via DB).
			if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
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

	lastLogLen := 0

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
				escaped, err := json.Marshal(newLog)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(c, "event: progress\ndata: %s\n\n", escaped); err != nil {
					return nil
				}
			}

			if task.Status == model.TaskStatusCompleted || task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled {
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

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil // error response already written
	}

	// Verify the file belongs to the task.
	if err := h.service.VerifyFileBelongsToTask(c.Context(), taskID, fileID); err != nil {
		return Error(c, fiber.StatusNotFound, "file not found")
	}

	stream, file, err := h.service.GetFileStream(c.Context(), fileID)
	if err != nil {
		h.logger.Error().Err(err).Str("file_id", fileID).Msg("get file stream failed")
		return Error(c, fiber.StatusNotFound, "file not found")
	}
	defer stream.Close()

	contentType := file.MimeType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, file.FileName))
	c.Set("Content-Length", strconv.FormatInt(file.FileSize, 10))

	if _, err := io.Copy(c.Response().BodyWriter(), stream); err != nil {
		h.logger.Error().Err(err).Str("file_id", fileID).Msg("stream file failed")
	}

	return nil
}

// PreviewHTML handles GET /api/v1/tasks/:id/preview.
// It finds the HTML file for the task and returns it as the response body.
func (h *TaskHandler) PreviewHTML(c fiber.Ctx) error {
	taskID, err := validateUUIDParam(c, "id")
	if err != nil {
		return err
	}

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
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

	c.Set("Content-Type", "text/html; charset=utf-8")
	c.Set("X-Content-Type-Options", "nosniff")
	// Allow loading images from external storage (OSS) in the sandboxed preview.
	cspValue := "sandbox allow-scripts"
	if h.service.StorageProviderName() == "oss" {
		cspValue += "; img-src https: data:"
	}
	c.Set("Content-Security-Policy", cspValue)
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

	if _, err := h.verifyTaskOwnership(c, taskID); err != nil {
		return nil // error response already written
	}

	buf, zipName, err := h.service.DownloadZip(c.Context(), taskID)
	if err != nil {
		h.logger.Error().Err(err).Str("task_id", taskID).Msg("download zip failed")
		return Error(c, fiber.StatusNotFound, "failed to create ZIP archive")
	}

	c.Set("Content-Type", "application/zip")
	c.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipName))
	c.Set("Content-Length", strconv.Itoa(buf.Len()))

	return c.Send(buf.Bytes())
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
		if _, err := uuid.Parse(strings.TrimSpace(id)); err != nil {
			return Error(c, fiber.StatusBadRequest, "invalid task_ids format")
		}
	}

	buf, zipName, err := h.service.DownloadTasksZip(c.Context(), userID, req.TaskIDs)
	if err != nil {
		h.logger.Error().Err(err).Str("user_id", userID).Msg("bulk download zip failed")
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

// validateImageModelKeyForUser delegates to the package-level helper, binding
// this handler's repository and image presets. See validateImageModelKeyForUser
// in image_model.go for the fail-closed tier-resolution rules.
func (h *TaskHandler) validateImageModelKeyForUser(c fiber.Ctx, userID, key string) error {
	return validateImageModelKeyForUser(c.Context(), h.repo, userID, key, h.imagePresets)
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
