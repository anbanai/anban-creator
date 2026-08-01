package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

const (
	AIEntryStatusCreated            = "created"
	AIEntryStatusNeedsConfiguration = "needs_configuration"
	AIEntryStatusError              = "error"
)

type AIEntrySubmitRequest struct {
	UserID           string                  `json:"-"`
	Channel          string                  `json:"channel"`
	ProjectID        string                  `json:"project_id"`
	ExecutionProfile string                  `json:"execution_profile"`
	Text             string                  `json:"text"`
	Attachments      []model.EntryAttachment `json:"attachments,omitempty"`
	ExecutionTarget  string                  `json:"execution_target,omitempty"`
}

type AIEntrySubmitResult struct {
	Status    string      `json:"status"`
	Task      *model.Task `json:"task,omitempty"`
	Message   string      `json:"message,omitempty"`
	ActionURL string      `json:"action_url,omitempty"`
}

type AIEntrySubmitter interface {
	Submit(ctx context.Context, req AIEntrySubmitRequest) (*AIEntrySubmitResult, error)
}

type AIEntryModelConfig struct {
	ProviderKey string
	Model       string
}

type ProviderTokenCostRecorder interface {
	CatalogID() string
	RecordProviderTokenUsage(context.Context, RecordProviderTokenCostRequest) (*model.BillingProviderCostEvent, error)
	RecordProviderTokenUnreconciled(context.Context, RecordProviderTokenUnreconciledRequest) (*model.BillingProviderCostEvent, error)
}

type AIEntryService struct {
	repo            repository.Repository
	taskSvc         *TaskService
	llm             ResultLLMClient
	costs           ProviderTokenCostRecorder
	config          AIEntryModelConfig
	logger          *zerolog.Logger
	referenceAssets *ReferenceAssetService
}

func (s *AIEntryService) SetReferenceAssetService(referenceAssets *ReferenceAssetService) {
	if s != nil {
		s.referenceAssets = referenceAssets
	}
}

type aiEntryIntent struct {
	Prompt             string         `json:"prompt"`
	Notes              string         `json:"notes"`
	SellingPoints      string         `json:"selling_points"`
	SelectedModules    map[string]int `json:"selected_modules"`
	TargetPlatform     string         `json:"target_platform"`
	Language           string         `json:"language"`
	ImageRatio         string         `json:"image_ratio"`
	ImageCapabilityKey string         `json:"image_capability_key"`
}

var aiEntryEcommerceModuleMax = map[string]int{
	"main_images":  10,
	"detail_page":  20,
	"cover_banner": 5,
	"share_image":  5,
	"sku_images":   20,
}

func NewAIEntryService(repo repository.Repository, taskSvc *TaskService, llm ResultLLMClient, costs ProviderTokenCostRecorder, cfg AIEntryModelConfig, logger *zerolog.Logger) *AIEntryService {
	if logger == nil {
		nop := zerolog.Nop()
		logger = &nop
	}
	return &AIEntryService{repo: repo, taskSvc: taskSvc, llm: llm, costs: costs, config: cfg, logger: logger}
}

func (s *AIEntryService) Submit(ctx context.Context, req AIEntrySubmitRequest) (*AIEntrySubmitResult, error) {
	if s == nil || s.repo == nil || s.taskSvc == nil {
		return aiEntryError("AI 入口服务暂不可用。"), nil
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ExecutionProfile = strings.TrimSpace(req.ExecutionProfile)
	req.Text = strings.TrimSpace(req.Text)
	if req.UserID == "" {
		return aiEntryError("用户未登录。"), nil
	}
	if req.ProjectID == "" {
		return aiEntryNeedsConfiguration("请选择一个项目后再创建任务。", "/projects"), nil
	}
	if req.ExecutionProfile == "" {
		return aiEntryNeedsConfiguration("请选择执行模型后再创建任务。", "/"), nil
	}
	project, err := s.repo.Projects().FindByID(ctx, req.ProjectID)
	if err != nil {
		return aiEntryNeedsConfiguration("未找到项目，请先选择或创建一个可用项目。", "/projects"), nil
	}
	if project.UserID != req.UserID {
		return aiEntryNeedsConfiguration("当前项目不可用，请切换到自己的项目。", "/projects"), nil
	}
	if project.Status != model.ProjectStatusActive {
		return aiEntryNeedsConfiguration("当前项目已归档，请切换到活跃项目。", "/projects"), nil
	}
	if s.llm == nil {
		return aiEntryNeedsConfiguration("AI 入口意图解析模型暂不可用，请联系管理员。", ""), nil
	}

	intent, parseErr := s.parseIntent(ctx, project, req)
	if parseErr != nil {
		if s.logger != nil {
			s.logger.Warn().Err(parseErr).Str("user_id", req.UserID).Str("project_id", req.ProjectID).Msg("ai entry intent parse failed")
		}
		return aiEntryError("AI 意图解析失败，请换一种更明确的描述后重试。"), nil
	}
	prompt := firstNonEmptyString(intent.Prompt, req.Text)
	if prompt == "" {
		return aiEntryNeedsConfiguration("请先描述你想创建的内容。", "/"), nil
	}

	projectSnapshot := model.SnapshotProject(project)
	params := CreateManualParams{
		UserID:           req.UserID,
		ProjectID:        req.ProjectID,
		ExecutionProfile: req.ExecutionProfile,
		Prompt:           prompt,
		Quantity:         1,
		ImageRatio:       normalizeAIEntryImageRatio(intent.ImageRatio),
		InputAttachments: normalizeEntryAttachments(req.Attachments),
		ExecutionTarget:  normalizeAIEntryExecutionTarget(req.ExecutionTarget),
		ProjectSnapshot:  &projectSnapshot,
	}
	var referenceView *model.AssetView

	switch project.Platform {
	case model.PlatformArticle, model.PlatformMoments:
		if uploadSessionID := firstImageAttachmentUploadSessionID(req.Attachments); uploadSessionID != "" {
			if s.referenceAssets == nil {
				return nil, ErrReferenceAssetUnavailable
			}
			assetID, err := s.referenceAssets.ResolveSelection(ctx, req.UserID, ReferenceImageSelection{UploadSessionID: uploadSessionID}, []string{DirectUploadPurposeAIEntryAttachment})
			if err != nil {
				return nil, err
			}
			params.ReferenceImageAssetID = assetID
			referenceView, err = s.referenceAssets.Present(ctx, req.UserID, assetID, []string{DirectUploadPurposeAIEntryAttachment})
			if err != nil {
				return nil, err
			}
		}
	case model.PlatformSeednote:
		// Seednote uses InputAttachments as its only new per-run reference source.
	case model.PlatformEcommerce:
		photos := imageAttachmentURLs(req.Attachments)
		if len(photos) == 0 {
			return aiEntryNeedsConfiguration("创建电商任务需要至少上传一张产品图。", aiEntryTaskCreateActionURL(model.PlatformEcommerce, project.ID)), nil
		}
		modules := normalizeAIEntrySelectedModules(intent.SelectedModules)
		if len(modules) == 0 {
			modules = project.EcommerceDefaults.Data().DefaultSelectedModules
		}
		if len(modules) == 0 {
			modules = map[string]int{"main_images": 1}
		}
		params.Ecommerce = &model.EcommerceConfig{
			SelectedModules: modules,
			ProductPhotos:   photos,
			TargetPlatform:  firstNonEmptyString(intent.TargetPlatform, project.EcommerceDefaults.Data().TargetPlatform),
			SellingPoints:   firstNonEmptyString(intent.SellingPoints, req.Text),
			Language:        intent.Language,
		}
	default:
		return aiEntryNeedsConfiguration("当前项目平台暂不支持 AI 入口创建任务。", "/projects/"+project.ID), nil
	}
	if referenceView == nil && project.ReferenceImageAssetID != "" {
		if s.referenceAssets == nil {
			return nil, ErrReferenceAssetUnavailable
		}
		referenceView, err = s.referenceAssets.Present(ctx, req.UserID, project.ReferenceImageAssetID, []string{DirectUploadPurposeProjectReference})
		if err != nil {
			return nil, err
		}
	}

	tasks, err := s.taskSvc.CreateManual(ctx, params)
	if err != nil {
		if IsReferenceAssetError(err) {
			return nil, err
		}
		if isAIEntryTaskCreationProfileError(err) {
			return nil, err
		}
		return aiEntryError("创建任务失败：" + cleanErr(err.Error())), nil
	}
	if len(tasks) == 0 {
		return aiEntryError("创建任务失败：未生成任务。"), nil
	}
	tasks[0].ReferenceImage = referenceView
	return &AIEntrySubmitResult{
		Status:  AIEntryStatusCreated,
		Task:    tasks[0],
		Message: "已创建任务。",
	}, nil
}

func isAIEntryTaskCreationProfileError(err error) bool {
	for _, target := range []error{
		ErrAgentProfileNotFound,
		ErrAgentProfileUnavailable,
		ErrAgentProfileAccessDenied,
		ErrAgentProfileSnapshotInvalid,
		ErrAgentProfileSnapshotConflict,
		ErrAgentProviderUnavailable,
		ErrAgentModelCostUnmapped,
		ErrBillingProfileSKUNotFound,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	return false
}

func (s *AIEntryService) parseIntent(ctx context.Context, project *model.Project, req AIEntrySubmitRequest) (aiEntryIntent, error) {
	systemPrompt := strings.TrimSpace(`你是 Anban 的 AI 创作入口意图解析器。
只做参数解析，不要写正文，不要解释。
必须只返回严格 JSON 对象，不能包含 Markdown、代码围栏或注释。
可用字段：
{
  "prompt": "最终给创作 agent 的简洁任务描述",
  "selling_points": "电商卖点，可选",
  "selected_modules": {"main_images": 1},
  "target_platform": "电商平台，可选",
  "language": "语言，可选",
  "image_ratio": "3:4|1:1|4:3|16:9|3:2|2:3|9:16|21:9，可选"
}
缺失字段请省略。`)
	userPrompt := aiEntryPrompt(project, req)
	raw, err := s.completeIntent(ctx, systemPrompt, userPrompt)
	if err != nil {
		return aiEntryIntent{}, err
	}
	intent, err := parseAIEntryIntentJSON(raw)
	if err == nil {
		return intent, nil
	}
	repairPrompt := "只返回合法 JSON。修复下面这段意图解析结果，不要解释，不要代码围栏。\n\n原始用户需求：\n" + req.Text + "\n\n待修复内容：\n" + raw
	repaired, repairErr := s.completeIntent(ctx, systemPrompt, repairPrompt)
	if repairErr != nil {
		return aiEntryIntent{}, repairErr
	}
	intent, err = parseAIEntryIntentJSON(repaired)
	if err != nil {
		return aiEntryIntent{}, err
	}
	return intent, nil
}

func (s *AIEntryService) completeIntent(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	result, err := s.llm.CompleteResult(ctx, systemPrompt, userPrompt)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("server internal model returned no result")
	}
	s.recordIntentCost(ctx, result.Usage)
	return result.Text, nil
}

func (s *AIEntryService) recordIntentCost(ctx context.Context, usage srvconfig.TokenUsage) {
	if s == nil || s.costs == nil || strings.TrimSpace(s.config.ProviderKey) == "" || strings.TrimSpace(s.config.Model) == "" {
		return
	}
	providerRequestID := "internal:server_internal:" + uuid.NewString()
	var err error
	if usage.InputTokens <= 0 && usage.CachedInputTokens <= 0 && usage.CacheReadInputTokens <= 0 && usage.CacheCreationInputTokens <= 0 && usage.OutputTokens <= 0 {
		_, err = s.costs.RecordProviderTokenUnreconciled(ctx, RecordProviderTokenUnreconciledRequest{
			Provider: s.config.ProviderKey, Model: s.config.Model, ProviderRequestID: providerRequestID,
			ReasonCode: model.BillingExecutionCostReasonMissingProviderUsage,
		})
	} else {
		cacheRead := usage.CacheReadInputTokens
		if cacheRead == 0 {
			cacheRead = usage.CachedInputTokens
		}
		_, err = s.costs.RecordProviderTokenUsage(ctx, RecordProviderTokenCostRequest{
			Provider: s.config.ProviderKey, Model: s.config.Model, ProviderRequestID: providerRequestID,
			CatalogID: s.costs.CatalogID(), IdempotencyKey: providerRequestID,
			Usage:  TokenUsage{Input: usage.InputTokens, CacheRead: cacheRead, CacheCreation: usage.CacheCreationInputTokens, Output: usage.OutputTokens},
			Source: string(model.BillingProviderCostSourceProviderResponse),
		})
	}
	if err != nil && s.logger != nil {
		s.logger.Error().Err(err).Str("provider_request_id", providerRequestID).Msg("record server internal provider cost; AI entry result remains valid")
	}
}

func aiEntryPrompt(project *model.Project, req AIEntrySubmitRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "channel: %s\n", req.Channel)
	if project != nil {
		fmt.Fprintf(&b, "project_id: %s\nproject_name: %s\nproject_platform: %s\nproject_instructions: %s\n", project.ID, project.Name, project.Platform, project.Instructions)
	}
	fmt.Fprintf(&b, "user_text: %s\n", req.Text)
	if len(req.Attachments) > 0 {
		b.WriteString("attachments:\n")
		for i, a := range req.Attachments {
			fmt.Fprintf(&b, "- index=%d type=%s file_name=%s content_type=%s size=%d url=%s text=%s\n", i, a.Type, a.FileName, a.ContentType, a.Size, a.URL, a.Text)
			if instruction := strings.TrimSpace(a.Instruction); instruction != "" {
				fmt.Fprintf(&b, "- attachment %d instruction: %s\n", i+1, instruction)
			}
		}
	}
	return b.String()
}

func parseAIEntryIntentJSON(raw string) (aiEntryIntent, error) {
	var intent aiEntryIntent
	candidate, extractErr := extractJSONObject(raw)
	if extractErr != nil {
		candidate = strings.TrimSpace(raw)
	}
	if err := json.Unmarshal([]byte(candidate), &intent); err != nil {
		return aiEntryIntent{}, err
	}
	intent.Prompt = strings.TrimSpace(intent.Prompt)
	intent.SellingPoints = strings.TrimSpace(intent.SellingPoints)
	intent.TargetPlatform = strings.TrimSpace(intent.TargetPlatform)
	intent.Language = strings.TrimSpace(intent.Language)
	intent.ImageRatio = strings.TrimSpace(intent.ImageRatio)
	intent.ImageCapabilityKey = strings.TrimSpace(intent.ImageCapabilityKey)
	return intent, nil
}

func normalizeEntryAttachments(in []model.EntryAttachment) []model.EntryAttachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]model.EntryAttachment, 0, len(in))
	for _, a := range in {
		a.Type = normalizeEntryAttachmentType(a.Type, a.ContentType)
		a.URL = strings.TrimSpace(a.URL)
		a.Text = strings.TrimSpace(a.Text)
		a.FileName = strings.TrimSpace(a.FileName)
		a.ContentType = strings.TrimSpace(a.ContentType)
		a.Role = strings.TrimSpace(a.Role)
		a.UploadID = strings.TrimSpace(a.UploadID)
		a.Key = strings.TrimSpace(a.Key)
		a.Instruction = strings.TrimSpace(a.Instruction)
		out = append(out, a)
	}
	return out
}

func normalizeEntryAttachmentType(t, contentType string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	if t != "" {
		return t
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(ct, "image/"):
		return "image"
	case strings.HasPrefix(ct, "audio/"):
		return "audio"
	case strings.HasPrefix(ct, "video/"):
		return "video"
	case strings.HasPrefix(ct, "text/"):
		return "text"
	default:
		return "document"
	}
}

func normalizeAIEntryExecutionTarget(target string) string {
	switch strings.TrimSpace(target) {
	case model.ExecutionTargetLocal:
		return model.ExecutionTargetLocal
	default:
		return model.ExecutionTargetCloud
	}
}

func normalizeAIEntryImageRatio(ratio string) string {
	ratio = strings.TrimSpace(ratio)
	if model.ValidImageRatios[ratio] {
		return ratio
	}
	return ""
}

func normalizeAIEntrySelectedModules(modules map[string]int) map[string]int {
	if len(modules) == 0 {
		return nil
	}
	out := map[string]int{}
	for key, qty := range modules {
		key = strings.TrimSpace(key)
		maxQty, ok := aiEntryEcommerceModuleMax[key]
		if !ok || qty <= 0 {
			continue
		}
		if qty > maxQty {
			qty = maxQty
		}
		out[key] = qty
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func firstImageAttachmentUploadSessionID(attachments []model.EntryAttachment) string {
	for _, attachment := range attachments {
		if normalizeEntryAttachmentType(attachment.Type, attachment.ContentType) == "image" {
			return strings.TrimSpace(attachment.UploadID)
		}
	}
	return ""
}

func imageAttachmentURLs(attachments []model.EntryAttachment) []string {
	urls := []string{}
	for _, a := range attachments {
		if normalizeEntryAttachmentType(a.Type, a.ContentType) == "image" {
			if source := entryAttachmentStorageSource(a); source != "" {
				urls = append(urls, source)
			}
		}
	}
	return urls
}

func entryAttachmentStorageSource(attachment model.EntryAttachment) string {
	if rawURL := strings.TrimSpace(attachment.URL); rawURL != "" {
		return rawURL
	}
	return strings.TrimSpace(attachment.Key)
}

func aiEntryNeedsConfiguration(message, actionURL string) *AIEntrySubmitResult {
	return &AIEntrySubmitResult{Status: AIEntryStatusNeedsConfiguration, Message: message, ActionURL: actionURL}
}

func aiEntryError(message string) *AIEntrySubmitResult {
	return &AIEntrySubmitResult{Status: AIEntryStatusError, Message: message}
}

func aiEntryTaskCreateActionURL(taskType, projectID string) string {
	taskType = strings.TrimSpace(taskType)
	projectID = strings.TrimSpace(projectID)
	if taskType == "" {
		return "/tasks?create=true&intent=new"
	}
	if projectID == "" {
		return "/tasks?create=true&type=" + taskType + "&intent=new"
	}
	return "/tasks?create=true&type=" + taskType + "&project_id=" + projectID + "&intent=new"
}
