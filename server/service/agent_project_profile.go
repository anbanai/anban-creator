package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/resources"
)

type AgentProjectProfileRequest struct {
	UserID    string
	ProjectID string
	TaskID    string
	Scope     string
}

type AgentProjectProfile map[string]any

var ErrWechatDeliveryContextRequired = errors.New("公众号文章或贴图交付判断必须绑定当前任务上下文")
var ErrWechatDeliveryDecisionUnavailable = errors.New("公众号任务缺少有效的冻结交付决策")

func validateWechatDeliveryContext(req AgentProjectProfileRequest, task *model.Task) error {
	scope := strings.TrimSpace(req.Scope)
	if task == nil {
		if scope == model.ScopeWechat || scope == "wechat-picture" {
			return ErrWechatDeliveryContextRequired
		}
		return nil
	}
	articleTask := task.Type == model.TaskTypeWechatArticle && taskUsesChannel(task, model.ChannelArticle)
	pictureTask := task.Type == model.TaskTypeWechatPicture && taskUsesChannel(task, model.ChannelWechatPicture)
	hasWechatContext := task.Type == model.TaskTypeWechatArticle || task.Type == model.TaskTypeWechatPicture ||
		taskUsesChannel(task, model.ChannelArticle) || taskUsesChannel(task, model.ChannelWechatPicture)
	if !hasWechatContext {
		return nil
	}
	if !articleTask && !pictureTask {
		return fmt.Errorf("公众号 profile scope 与当前任务类型不匹配")
	}
	if scope == "wechat-picture" && !pictureTask {
		return fmt.Errorf("公众号贴图 profile scope 与当前任务类型不匹配")
	}
	return nil
}

func validateWechatDeliveryDecision(task *model.Task, decision model.WechatDeliveryDecision) error {
	if task == nil || (task.Type != model.TaskTypeWechatArticle && task.Type != model.TaskTypeWechatPicture) {
		return nil
	}
	switch decision.Mode {
	case model.WechatDeliveryModeAutomatic:
		if !decision.AutomaticEnabled || !decision.AppIDConfigured || !decision.SecretConfigured {
			return ErrWechatDeliveryDecisionUnavailable
		}
	case model.WechatDeliveryModeUserAssisted:
	default:
		return ErrWechatDeliveryDecisionUnavailable
	}
	return nil
}

// wechatDeliveryInstructions is the semantic, agent-facing rendering contract.
// The server decides the branch from the frozen task snapshot; agents only
// follow this instruction and must not reconstruct it from credentials.
func wechatDeliveryInstructions(task *model.Task, decision model.WechatDeliveryDecision) string {
	if task == nil || (task.Type != model.TaskTypeWechatArticle && task.Type != model.TaskTypeWechatPicture) {
		return ""
	}
	automatic := decision.Mode == model.WechatDeliveryModeAutomatic && decision.AutomaticEnabled && decision.AppIDConfigured && decision.SecretConfigured
	if task.Type == model.TaskTypeWechatPicture {
		if pictureTaskDraftDisabled(task) {
			automatic = false
		}
		if automatic {
			return "当前任务是微信公众号贴图，采用自动草稿交付。请生成符合公众号官方贴图发布要求的标题、caption 和按顺序排列的图片发布包，图片以当前任务内的相对文件路径提供；Server 会上传这些图片并创建公众号贴图草稿。不要直接调用微信发布能力。任一必需图片缺失或审核未通过时，不得报告交付完成，必须保留 blocked 结果。"
		}
		return "当前任务是微信公众号贴图，采用手动复制交付。请生成包含标题、caption 和全部图片的完整图文内容，图片必须按视觉顺序排列并使用稳定公开的图片地址；Studio 会一次复制完整图文供用户粘贴到公众号后台。不能只交付文案，也不能要求用户逐张复制或重新组装图片。任一必需图片缺失或无法公开访问时，不得报告交付完成，必须保留 blocked 结果。"
	}
	if automatic {
		return "当前任务是微信公众号文章，采用自动草稿交付。请生成包含全部必需正文配图的公众号文章 HTML，并使用微信素材图片地址；Server 会负责创建公众号草稿。不要直接调用微信发布能力。任一必需图片没有进入最终 HTML 时，不得报告交付完成，必须保留 blocked 结果。"
	}
	return "当前任务是微信公众号文章，采用手动复制交付。请生成包含正文和全部必需配图的完整 HTML，并使用稳定公开的 Anban 图片地址；Studio 会一次复制完整图文供用户粘贴到公众号后台。不能只交付文章文字，也不能要求用户重新上传或组装图片。任一必需图片没有进入最终 HTML 时，不得报告交付完成，必须保留 blocked 结果。"
}

type AgentProjectProfileService struct {
	projects          *ProjectService
	tasks             *TaskService
	resources         *resources.ResourceManager
	montage           config.MontageConfig
	videoEdit         config.VideoEditConfig
	talkingHeadEditor config.TalkingHeadEditorConfig
	imageCapabilities *ImageCapabilityResolver
	repo              repository.Repository
}

func NewAgentProjectProfileService(projects *ProjectService, tasks *TaskService, manager *resources.ResourceManager, montage config.MontageConfig, imageCapabilities *ImageCapabilityResolver, repos ...repository.Repository) *AgentProjectProfileService {
	var repo repository.Repository
	if len(repos) > 0 {
		repo = repos[0]
	}
	return &AgentProjectProfileService{projects: projects, tasks: tasks, resources: manager, montage: montage, imageCapabilities: imageCapabilities, repo: repo}
}

func (s *AgentProjectProfileService) SetVideoEditConfig(cfg config.VideoEditConfig) {
	if s == nil {
		return
	}
	cfg.ApplyDefaults()
	s.videoEdit = cfg
}

func (s *AgentProjectProfileService) SetTalkingHeadEditorConfig(cfg config.TalkingHeadEditorConfig) {
	if s == nil {
		return
	}
	cfg.ApplyDefaults()
	s.talkingHeadEditor = cfg
}

func (s *AgentProjectProfileService) Get(ctx context.Context, req AgentProjectProfileRequest) (*AgentProjectProfile, error) {
	if strings.TrimSpace(req.ProjectID) == "" {
		return nil, errors.New("project_id is required")
	}
	if s == nil || s.projects == nil {
		return nil, errors.New("project service not available")
	}
	project, _, err := s.projects.Get(ctx, req.UserID, req.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}
	SanitizeProject(project)

	var task *model.Task
	usesProjectSnapshot := false
	if req.TaskID != "" {
		if s.tasks == nil {
			return nil, errors.New("task service not available")
		}
		task, err = s.tasks.GetByID(ctx, req.TaskID)
		if err != nil {
			return nil, fmt.Errorf("get task: %w", err)
		}
		if task.UserID != req.UserID || task.ProjectID != req.ProjectID {
			return nil, errors.New("task does not belong to the requested project")
		}
		if snapshot := task.ProjectSnapshot.Data(); snapshot.HasData() {
			project = model.ProjectFromSnapshot(project, snapshot)
			usesProjectSnapshot = true
		}
	}
	if err := validateWechatDeliveryContext(req, task); err != nil {
		return nil, err
	}

	outputType := project.Platform
	if task != nil {
		outputType = task.Type
	}
	resourcePlatform := outputType
	if task != nil {
		if platform := model.PlatformForChannel(task.Channel); platform != "" {
			resourcePlatform = platform
		}
	}
	projectProfile := project.Profile.Data()
	if projectProfile.SchemaVersion == 0 {
		projectProfile = model.NewProjectProfile()
	}
	if model.IsHypitPlatform(outputType) {
		in := model.HypitInput{}
		if task != nil {
			in = task.HypitInput.Data()
		}
		p := AgentProjectProfile{"name": project.Name, "platform": project.Platform, "instructions": project.Instructions, "hypit": map[string]any{"defaults": project.HypitDefaults.Data(), "input": in, "workspace_input_file": "input.json", "runtime_profile_file": "runtime-profile.json", "project_root": "/workspace/project", "required_artifacts": []string{"output/final.mp4", "output/cover.png", "output/project.json", "output/project.zip", "output/delivery-manifest.json", "output/quality-report.json"}}}
		resolved := map[string]any{
			"id": project.ID, "name": project.Name, "platform": project.Platform,
			"instructions": project.Instructions, "visual_style": project.VisualStyle,
			"image_ratio":           hypitPortraitCoverRatio(project.HypitDefaults.Data().Preferences),
			"allowed_image_ratios":  model.SupportedImageRatios(resourcePlatform),
			"uses_project_snapshot": usesProjectSnapshot,
		}
		if task != nil {
			resolved["image_ratio"] = task.ImageRatio
			resolved["image_capability_key"] = task.ImageCapabilityKey
		}
		if projectPortraitReferenceAssetID(task) != "" {
			resolved["project_portrait_reference_path"] = serveragent.ProjectPortraitReferenceImagePath
		}
		if taskReferenceAssetID(task) != "" {
			resolved["task_reference_path"] = serveragent.TaskReferenceImagePath
		}
		if projectStyleReferenceAssetID(task) != "" {
			resolved["project_style_reference_path"] = serveragent.ProjectStyleReferenceImagePath
		}
		p["account_profile"] = projectProfile
		resolved["account_profile"] = projectProfile
		if !projectProfile.IsConfirmed() {
			resolved["account_profile_status"] = projectProfile.Status
		}
		p["resolved_profile"] = resolved
		return &p, nil
	}
	style := ResolveStyle(project, task)
	effectiveImageRatio := strings.TrimSpace(project.ImageRatio)
	if task != nil && strings.TrimSpace(task.ImageRatio) != "" {
		effectiveImageRatio = strings.TrimSpace(task.ImageRatio)
	}
	videoEditOutput := model.IsVideoEditPlatform(outputType)
	if s.imageCapabilities == nil && !videoEditOutput {
		return nil, errors.New("image capability resolver not available")
	}
	imageCapabilityKey := ""
	if task != nil {
		imageCapabilityKey = strings.TrimSpace(task.ImageCapabilityKey)
		if taskUsesFrozenImageCapability(task.Type) && imageCapabilityKey == "" {
			return nil, ErrTaskImageCapabilityMissing
		}
	}
	imageCapabilityKeyResolved := ""
	if !videoEditOutput {
		resolved, err := s.imageCapabilities.ResolvePublicImageCapability(ctx, req.UserID, imageCapabilityKey)
		if err != nil {
			return nil, fmt.Errorf("resolve image capability: %w", err)
		}
		imageCapabilityKeyResolved = resolved.Key
	}
	profile := AgentProjectProfile{
		"name": project.Name, "positioning": project.Instructions,
		"instructions": project.Instructions, "keywords": project.Keywords,
		"platform": project.Platform, "visual_style": style.VisualStyle,
		"writer": style.Writer, "author": style.Author, "theme": style.Theme,
		"visual_style_source": style.VisualStyleSource, "writer_source": style.WriterSource,
		"author_source": style.AuthorSource, "theme_source": style.ThemeSource,
	}
	if task != nil && (taskUsesChannel(task, model.ChannelArticle) || taskUsesChannel(task, model.ChannelWechatPicture)) {
		decision := task.ProjectSnapshot.Data().WechatDelivery
		if err := validateWechatDeliveryDecision(task, decision); err != nil {
			return nil, err
		}
		if instructions := wechatDeliveryInstructions(task, decision); instructions != "" {
			profile["wechat_delivery_instructions"] = instructions
		}
	}
	// Preserve source labels so inferred content cannot be treated as verified facts.
	profile["account_profile"] = projectProfile
	resolvedProfile := map[string]any{
		"id": project.ID, "name": project.Name, "platform": project.Platform,
		"profile_url": project.ProfileURL, "avatar_url": project.AvatarURL,
		"instructions": project.Instructions, "positioning": project.Instructions,
		"keywords": project.Keywords, "visual_style": style.VisualStyle,
		"creative_constraints": style.VisualStyle, "visual_style_label": "图片视觉",
		"image_ratio": effectiveImageRatio, "uses_project_snapshot": usesProjectSnapshot,
		"image_capability_key": imageCapabilityKeyResolved,
		"allowed_image_ratios": model.SupportedImageRatios(resourcePlatform),
		"sources": map[string]any{
			"visual_style": style.VisualStyleSource,
			"instructions": agentProfileSource(usesProjectSnapshot),
			"keywords":     agentProfileSource(usesProjectSnapshot),
		},
	}
	resolvedProfile["account_profile"] = projectProfile
	if !projectProfile.IsConfirmed() {
		resolvedProfile["account_profile_status"] = projectProfile.Status
	}
	hasTaskReference := taskReferenceAssetID(task) != ""
	hasProjectStyleReference := projectStyleReferenceAssetID(task) != ""
	if hasTaskReference {
		resolvedProfile["task_reference_path"] = serveragent.TaskReferenceImagePath
	}
	if hasProjectStyleReference {
		resolvedProfile["project_style_reference_path"] = serveragent.ProjectStyleReferenceImagePath
	}
	if projectPortraitReferenceAssetID(task) != "" {
		resolvedProfile["project_portrait_reference_path"] = serveragent.ProjectPortraitReferenceImagePath
	}
	profile["resolved_profile"] = resolvedProfile
	if outputType == model.TaskTypeWechatArticle || outputType == model.PlatformWechat || outputType == model.PlatformSeednote {
		feedbackStrategy, err := s.feedbackStrategyProfile(ctx, project, task)
		if err != nil {
			return nil, fmt.Errorf("load feedback strategy: %w", err)
		}
		profile["feedback_strategy"] = feedbackStrategy
	}

	switch outputType {
	case model.PlatformSeednote:
		imageConfig := map[string]any{}
		if hasTaskReference {
			imageConfig["task_reference_path"] = serveragent.TaskReferenceImagePath
		}
		if hasProjectStyleReference {
			imageConfig["project_style_reference_path"] = serveragent.ProjectStyleReferenceImagePath
		}
		profile["image_config"] = imageConfig
	case model.PlatformMoments:
		imageConfig := map[string]any{"default_ratio": firstAgentProfileValue(effectiveImageRatio, "3:4")}
		if hasTaskReference {
			imageConfig["task_reference_path"] = serveragent.TaskReferenceImagePath
		}
		if hasProjectStyleReference {
			imageConfig["project_style_reference_path"] = serveragent.ProjectStyleReferenceImagePath
		}
		profile["image_config"] = imageConfig
		profile["moments"] = map[string]any{
			"required_artifacts": []string{"material-analysis.md", "content.md", "image-prompts.md", "moments-image.png", "quality-review.md"},
			"method":             []string{"六类素材：发售、人设、产品、案例、生活、认知", "四层提炼：观点层、框架层、风格层、人设层"},
			"auto_publish":       false, "scheduled_plans": false,
			"falsification_ban": "不伪造客户案例、成交数据、用户反馈",
		}
	case model.PlatformEcommerce:
		ecommerce := map[string]any{"input_attachment_index": ".anban-creator/input-attachments/index.json"}
		if task != nil {
			cfg := task.Ecommerce.Data()
			ecommerce["selected_modules"] = cfg.SelectedModules
			ecommerce["product_photo_count"] = len(cfg.ProductPhotos)
			if cfg.TargetPlatform != "" {
				ecommerce["target_platform"] = cfg.TargetPlatform
			}
			if cfg.SellingPoints != "" {
				ecommerce["selling_points"] = cfg.SellingPoints
			}
			if cfg.Language != "" {
				ecommerce["language"] = cfg.Language
			}
			if cfg.BrandBrief != "" {
				ecommerce["brand_brief"] = cfg.BrandBrief
			}
		}
		profile["ecommerce"] = ecommerce
	case model.PlatformMontage:
		profile["montage"] = s.montageProfile(project, task)
	case model.PlatformVideoEdit:
		profile["video_edit"] = s.videoEditProfile(project, task)
	case model.PlatformTalkingHeadEditor:
		profile["talking_head_editor"] = s.talkingHeadEditorProfile(project, task)
	}

	if s.resources != nil {
		profile["available_themes"] = s.resources.ListByPlatform(resources.CategoryTheme, resourcePlatform)
		profile["available_writers"] = s.resources.ListByPlatform(resources.CategoryWriter, resourcePlatform)
		profile["available_article_templates"] = s.resources.ListByPlatform(resources.CategoryArticleTemplate, resourcePlatform)
		if style.Theme != "" {
			if entry := s.resources.Get(resources.CategoryTheme, style.Theme); entry != nil {
				profile["theme_description"] = entry.Description
			}
		}
		if style.Writer != "" {
			if entry := s.resources.Get(resources.CategoryWriter, style.Writer); entry != nil {
				profile["writer_description"] = entry.Description
			}
		}
	}
	return &profile, nil
}

func (s *AgentProjectProfileService) videoEditProfile(project *model.Project, task *model.Task) map[string]any {
	defaults := model.VideoEditDefaults{}
	if project != nil {
		defaults = project.VideoEditDefaults.Data()
	}
	input := model.VideoEditInput{}
	if task != nil && model.IsVideoEditPlatform(task.Type) {
		input = task.VideoEditInput.Data()
	}
	return map[string]any{
		"defaults":       defaults,
		"input":          input,
		"default_policy": s.videoEdit.DefaultPolicy,
		"limits": map[string]any{
			"max_duration_seconds":  s.videoEdit.MaxDurationSeconds,
			"max_assets":            s.videoEdit.MaxAssets,
			"max_asset_bytes":       s.videoEdit.MaxAssetBytes,
			"max_total_input_bytes": s.videoEdit.MaxTotalInputBytes,
			"timeout_minutes":       s.videoEdit.TimeoutMinutes,
		},
		"env":                  s.videoEdit.RedactedEnv(),
		"upstream_revision":    "43cfc566833548093455c9d4a46bd69913b7e552",
		"workspace_input_file": "video-edit-input.json",
		"policy_file":          "video-edit-policy.json",
		"project_root":         "/workspace",
		"required_artifacts": []string{
			"output/final.mp4",
			"output/edit-decision-list.json",
			"output/edit-strategy.md",
			"output/delivery-manifest.json",
			"output/quality-evidence.json",
			"output/content-tags.json",
		},
		"optional_artifacts": []string{"output/subtitles.srt", "output/failure-state.json", "output/failure-diagnosis.md", "output/progress-state.json"},
		"runtime_contract":   "Use browser-use/video-use at the pinned Server-provided commit in the standard /workspace adapter. Execute autonomously, never request interactive edit approval, and never invoke OpenMontage.",
	}
}

func (s *AgentProjectProfileService) talkingHeadEditorProfile(project *model.Project, task *model.Task) map[string]any {
	defaults := model.TalkingHeadEditorDefaults{}
	if project != nil {
		defaults = project.TalkingHeadEditorDefaults.Data()
	}
	input := model.TalkingHeadEditorInput{}
	if task != nil && model.IsTalkingHeadEditorPlatform(task.Type) {
		input = task.TalkingHeadEditorInput.Data()
	}
	return map[string]any{
		"defaults":       defaults,
		"input":          input,
		"default_policy": s.talkingHeadEditor.DefaultPolicy,
		"limits": map[string]any{
			"max_duration_seconds": s.talkingHeadEditor.MaxDurationSeconds,
			"max_assets":           s.talkingHeadEditor.MaxAssets,
			"timeout_minutes":      s.talkingHeadEditor.TimeoutMinutes,
		},
		"upstream_revisions": map[string]string{
			"auto-editor":                  "28.1.0",
			"auto-editor-source":           "c6d755e590fa1fd3db0cdb3747bc92cf8af1a0bd",
			"video-use":                    "43cfc566833548093455c9d4a46bd69913b7e552",
			"erduo-broll-loop-engineering": "managed-release",
		},
		"workspace_input_file": "talking-head-editor-input.json",
		"policy_file":          "talking-head-editor-policy.json",
		"project_root":         "/workspace",
		"required_artifacts": []string{
			"output/final.mp4",
			"output/edit-decision-list.json",
			"output/transcript.json",
			"output/edit-strategy.md",
			"output/visual-plan.json",
			"output/sfx-cues.json",
			"output/bgm-plan.json",
			"output/audio-source-manifest.json",
			"output/delivery-manifest.json",
			"output/quality-evidence.json",
			"output/content-tags.json",
		},
		"optional_artifacts": []string{"output/rough-cut.mp4", "output/semantic-cut.mp4", "output/motion-sample.mp4", "output/subtitles.srt", "output/failure-state.json", "output/failure-diagnosis.md", "output/progress-state.json"},
		"runtime_contract":   "Use only the three Server-pinned talking-head-editor Skills. Execute rough cut and semantic cut continuously, stop at the motion-sample review gate, and add sound only after approval. Never load video-edit or silently downgrade to it.",
	}
}

func (s *AgentProjectProfileService) feedbackStrategyProfile(ctx context.Context, project *model.Project, task *model.Task) (map[string]any, error) {
	payload := map[string]any{"mode": "advisory", "available": false, "strategy_unavailable": "no_active_strategy"}
	if s == nil || s.repo == nil || project == nil {
		return payload, nil
	}
	channel, taskKind := feedbackIdentity(project, task)
	if channel == "" || taskKind == "" {
		return payload, nil
	}
	snapshot, err := s.repo.FeedbackLoop().FindActiveStrategy(ctx, project.ID, channel)
	if err != nil {
		return nil, err
	}

	if !feedbackStrategyUsable(snapshot, taskKind, channel, time.Now().UTC()) {
		return payload, nil
	}
	recommendations := json.RawMessage(snapshot.Recommendations)
	evidence := json.RawMessage(snapshot.Evidence)
	if !json.Valid(recommendations) || !json.Valid(evidence) {
		payload["strategy_unavailable"] = "invalid_strategy_payload"
		return payload, nil
	}
	payload["available"] = true
	payload["strategy_snapshot_id"] = snapshot.ID
	payload["strategy_revision"] = snapshot.Revision
	payload["strategy_digest"] = snapshot.Digest
	payload["recommendations"] = recommendations
	payload["evidence"] = evidence
	payload["limitations"] = snapshot.Limitations
	return payload, nil
}

func feedbackIdentity(_ *model.Project, task *model.Task) (string, string) {
	if task == nil {
		return "", ""
	}
	return strings.TrimSpace(task.Channel), strings.TrimSpace(task.TaskKind)
}

func (s *AgentProjectProfileService) montageProfile(project *model.Project, task *model.Task) map[string]any {
	defaults := model.MontageDefaults{}
	if project != nil {
		defaults = project.MontageDefaults.Data()
	}
	input := model.MontageInput{}
	if task != nil && model.IsMontagePlatform(task.Type) {
		input = task.MontageInput.Data()
	}
	toolPolicy := make(map[string]any, len(s.montage.ToolPolicy))
	for key, value := range s.montage.ToolPolicy {
		toolPolicy[key] = value
	}
	pipelineDefaults := make(map[string]any, len(s.montage.PipelineDefaults))
	for key, value := range s.montage.PipelineDefaults {
		pipelineDefaults[key] = value
	}
	return map[string]any{
		"defaults": defaults, "input": input, "source_asset_count": len(input.SourceAssets),
		"workspace_input_file": "montage-input.json", "tool_policy_file": "montage-tool-policy.json",
		"pipeline_defaults_file": "montage-pipeline-defaults.json", "project_manifest_file": "montage-project.json",
		"output_dir": "output/montage", "required_artifacts": []string{"final.mp4", "delivery-manifest.json"},
		"artifact_roles": []string{"final_video", "delivery_manifest", "source_manifest", "timeline", "subtitles", "audio", "run_log", "failure_diagnosis"},
		"env":            s.montage.RedactedEnv(), "tool_policy": toolPolicy, "pipeline_defaults": pipelineDefaults,
		"runner_contract": "Agent prepares montage-input.json and montage-project.json, runs the Montage adapter from the runtime-provided /workspace/openmontage project root exposed through $ANBAN_MONTAGE_SUBMODULE_PATH, then validates local output files against delivery-manifest.json; the Runtime uploads and registers task files by artifact role after execution.",
	}
}

func agentProfileSource(usesProjectSnapshot bool) string {
	if usesProjectSnapshot {
		return "snapshot"
	}
	return "project"
}

func firstAgentProfileValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
