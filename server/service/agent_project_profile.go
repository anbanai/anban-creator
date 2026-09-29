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

type AgentProjectProfileService struct {
	projects          *ProjectService
	tasks             *TaskService
	resources         *resources.ResourceManager
	montage           config.MontageConfig
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
		if snapshot := task.ProjectSnapshot.Data(); snapshot.Platform != "" {
			project = model.ProjectFromSnapshot(project, snapshot)
			usesProjectSnapshot = true
		}
	}

	if model.IsHypitPlatform(project.Platform) {
		in := model.HypitInput{}
		if task != nil {
			in = task.HypitInput.Data()
		}
		p := AgentProjectProfile{"name": project.Name, "platform": project.Platform, "instructions": project.Instructions, "hypit": map[string]any{"defaults": project.HypitDefaults.Data(), "input": in, "workspace_input_file": "input.json", "runtime_profile_file": "runtime-profile.json", "project_root": "/workspace/project", "required_artifacts": []string{"output/final.mp4", "output/cover.png", "output/project.json", "output/project.zip", "output/delivery-manifest.json", "output/quality-report.json"}}}
		resolved := map[string]any{
			"id": project.ID, "name": project.Name, "platform": project.Platform,
			"instructions": project.Instructions, "visual_style": project.VisualStyle,
			"image_ratio":           hypitPortraitCoverRatio(project.HypitDefaults.Data().Preferences),
			"allowed_image_ratios":  model.SupportedImageRatios(project.Platform),
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
		p["resolved_profile"] = resolved
		return &p, nil
	}
	style := ResolveStyle(project, task)
	effectiveImageRatio := strings.TrimSpace(project.ImageRatio)
	if task != nil && strings.TrimSpace(task.ImageRatio) != "" {
		effectiveImageRatio = strings.TrimSpace(task.ImageRatio)
	}
	if s.imageCapabilities == nil {
		return nil, errors.New("image capability resolver not available")
	}
	imageCapabilityKey := ""
	if task != nil {
		imageCapabilityKey = strings.TrimSpace(task.ImageCapabilityKey)
		if taskUsesFrozenImageCapability(task.Type) && imageCapabilityKey == "" {
			return nil, ErrTaskImageCapabilityMissing
		}
	}
	imageCapability, err := s.imageCapabilities.ResolvePublicImageCapability(ctx, req.UserID, imageCapabilityKey)
	if err != nil {
		return nil, fmt.Errorf("resolve image capability: %w", err)
	}
	agentConfig := project.AgentConfig.Data()
	if agentConfig == nil {
		agentConfig = map[string]any{}
	}
	profile := AgentProjectProfile{
		"name": project.Name, "positioning": project.Instructions,
		"instructions": project.Instructions, "keywords": project.Keywords,
		"platform": project.Platform, "visual_style": style.VisualStyle,
		"writer": style.Writer, "author": style.Author, "theme": style.Theme,
		"visual_style_source": style.VisualStyleSource, "writer_source": style.WriterSource,
		"author_source": style.AuthorSource, "theme_source": style.ThemeSource,
		"agent_config": agentConfig,
	}
	projectProfile := project.Profile.Data()
	if projectProfile.SchemaVersion == 0 {
		projectProfile = model.NewProjectProfile()
	}
	// Drafts are review material for Studio only. Downstream Agents may consume
	// an account profile after the user confirms it through the versioned
	// project-profile API; exposing an unconfirmed draft here would let a
	// creative workflow silently treat inference as project truth.
	if projectProfile.IsConfirmed() {
		profile["account_profile"] = projectProfile
	}
	resolvedProfile := map[string]any{
		"id": project.ID, "name": project.Name, "platform": project.Platform,
		"profile_url": project.ProfileURL, "avatar_url": project.AvatarURL,
		"instructions": project.Instructions, "positioning": project.Instructions,
		"keywords": project.Keywords, "visual_style": style.VisualStyle,
		"creative_constraints": style.VisualStyle, "visual_style_label": "图片视觉",
		"image_ratio": effectiveImageRatio, "uses_project_snapshot": usesProjectSnapshot,
		"image_capability_key": imageCapability.Key,
		"allowed_image_ratios": model.SupportedImageRatios(project.Platform),
		"agent_config":         agentConfig,
		"sources": map[string]any{
			"visual_style": style.VisualStyleSource,
			"instructions": agentProfileSource(usesProjectSnapshot),
			"keywords":     agentProfileSource(usesProjectSnapshot),
		},
	}
	if projectProfile.IsConfirmed() {
		resolvedProfile["account_profile"] = projectProfile
	} else {
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
	if project.Platform == model.PlatformArticle || project.Platform == model.PlatformSeednote {
		feedbackStrategy, err := s.feedbackStrategyProfile(ctx, project, task)
		if err != nil {
			return nil, fmt.Errorf("load feedback strategy: %w", err)
		}
		profile["feedback_strategy"] = feedbackStrategy
	}

	switch project.Platform {
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
	}

	if s.resources != nil {
		profile["available_themes"] = s.resources.ListByPlatform(resources.CategoryTheme, project.Platform)
		profile["available_writers"] = s.resources.ListByPlatform(resources.CategoryWriter, project.Platform)
		profile["available_article_templates"] = s.resources.ListByPlatform(resources.CategoryArticleTemplate, project.Platform)
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

func (s *AgentProjectProfileService) feedbackStrategyProfile(ctx context.Context, project *model.Project, task *model.Task) (map[string]any, error) {
	payload := map[string]any{"mode": "advisory", "available": false, "strategy_unavailable": "no_active_strategy"}
	if s == nil || s.repo == nil || project == nil {
		return payload, nil
	}
	taskType := project.Platform
	if task != nil && strings.TrimSpace(task.Type) != "" {
		taskType = task.Type
	}
	snapshot, err := s.repo.FeedbackLoop().FindActiveStrategy(ctx, project.ID, project.Platform)
	if err != nil {
		return nil, err
	}
	if !feedbackStrategyUsable(snapshot, taskType, project.Platform, time.Now().UTC()) {
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
