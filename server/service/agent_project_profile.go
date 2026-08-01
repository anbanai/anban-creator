package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/resources"
)

const agentReferenceImageRuntimePath = ".anban-creator/reference.png"

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
}

func NewAgentProjectProfileService(projects *ProjectService, tasks *TaskService, manager *resources.ResourceManager, montage config.MontageConfig, imageCapabilities *ImageCapabilityResolver) *AgentProjectProfileService {
	return &AgentProjectProfileService{projects: projects, tasks: tasks, resources: manager, montage: montage, imageCapabilities: imageCapabilities}
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
	}
	imageCapability, err := s.imageCapabilities.ResolvePublicImageCapability(ctx, req.UserID, imageCapabilityKey)
	if err != nil {
		return nil, fmt.Errorf("resolve image capability: %w", err)
	}
	profile := AgentProjectProfile{
		"name": project.Name, "positioning": project.Instructions,
		"instructions": project.Instructions, "keywords": project.Keywords,
		"platform": project.Platform, "visual_style": style.VisualStyle,
		"writer": style.Writer, "author": style.Author, "theme": style.Theme,
		"visual_style_source": style.VisualStyleSource, "writer_source": style.WriterSource,
		"author_source": style.AuthorSource, "theme_source": style.ThemeSource,
	}
	resolvedProfile := map[string]any{
		"id": project.ID, "name": project.Name, "platform": project.Platform,
		"profile_url": project.ProfileURL, "avatar_url": project.AvatarURL,
		"instructions": project.Instructions, "positioning": project.Instructions,
		"keywords": project.Keywords, "visual_style": style.VisualStyle,
		"creative_constraints": style.VisualStyle, "visual_style_label": "图片视觉",
		"image_ratio": effectiveImageRatio, "uses_project_snapshot": usesProjectSnapshot,
		"image_capability_key": imageCapability.Key,
		"supported_sizes":      append([]string(nil), imageCapability.SupportedSizes...),
		"sources": map[string]any{
			"visual_style": style.VisualStyleSource,
			"instructions": agentProfileSource(usesProjectSnapshot),
			"keywords":     agentProfileSource(usesProjectSnapshot),
		},
	}
	hasReference := EffectiveReferenceAssetID(task) != ""
	if hasReference {
		resolvedProfile["reference_image_path"] = agentReferenceImageRuntimePath
	}
	profile["resolved_profile"] = resolvedProfile

	switch project.Platform {
	case model.PlatformSeednote:
		imageConfig := map[string]any{}
		if hasReference {
			imageConfig["reference_image_path"] = agentReferenceImageRuntimePath
		}
		profile["image_config"] = imageConfig
	case model.PlatformMoments:
		imageConfig := map[string]any{"default_ratio": firstAgentProfileValue(effectiveImageRatio, "3:4")}
		if hasReference {
			imageConfig["reference_image_path"] = agentReferenceImageRuntimePath
		}
		profile["image_config"] = imageConfig
		profile["moments"] = map[string]any{
			"required_artifacts": []string{"material-analysis.md", "content.md", "quality-review.md"},
			"method":             []string{"六类素材：发售、人设、产品、案例、生活、认知", "四层提炼：观点层、框架层、风格层、人设层"},
			"auto_publish":       false, "scheduled_plans": false,
			"falsification_ban": "不伪造客户案例、成交数据、用户反馈",
		}
	case model.PlatformEcommerce:
		ecommerce := map[string]any{"product_photo_dir": ".anban-creator/products"}
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
		"runner_contract": "Agent prepares montage-input.json and montage-project.json, runs the Montage adapter from $ANBAN_MONTAGE_SUBMODULE_PATH when set, otherwise third_party/OpenMontage, then registers task files by artifact role.",
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
