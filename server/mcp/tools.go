package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/resources"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

const referenceImageRuntimePath = ".anban-creator/reference.png"

// ImageModelResolver selects one immutable provider/model descriptor before an
// image request reaches billing or generation.
type ImageModelResolver interface {
	ResolveImageModelForGeneration(
		ctx context.Context,
		userID string,
		imageModelKey string,
		imageType string,
		referenceCount int,
	) (*service.ResolvedImageModel, error)
}

// ImageGenerator is the narrow generation surface used by generate_image.
// ImageSvc remains concrete because the other image tools need upload,
// compression, and download methods that are intentionally not part of this
// request-scoped interface.
type ImageGenerator interface {
	GenerateImage(
		ctx context.Context,
		userID, projectID, prompt, imageType, outputPath, refPath string,
		refPaths []string,
		taskID, size string,
		resolved *service.ResolvedImageModel,
		watermark *bool,
	) (*service.ImageResult, error)
}

// Services holds the service instances needed by MCP tools.
type Services struct {
	ProjectSvc           *service.ProjectService
	Store                storage.Provider
	TaskSvc              *service.TaskService
	PlanSvc              *service.PlanService
	ImageSvc             *service.ImageService
	ImageModelResolver   ImageModelResolver
	ImageGenerator       ImageGenerator
	ProviderCostSvc      *service.ProviderCostService
	BillingCatalogSvc    *service.BillingCatalogService
	GenerateImageTimeout time.Duration
	VideoSvc             *service.VideoService
	AudioASRSvc          *service.AudioASRService
	VideoASRSvc          *service.VideoASRService
	WritingSvc           *service.WritingService
	PublishingSvc        *service.PublishingService
	WorkspaceSvc         *service.WorkspaceService
	TemplateSvc          *service.TemplateService
	LiveSliceSvc         *service.LiveSliceService
	SeednoteClient       *seednote.Client
	SeednoteReadiness    service.Readiness
	TopicPoolSvc         *service.TopicPoolService
	AgentFeedbackSvc     *service.AgentFeedbackService
	TingWuConfigured     bool
	FunASRConfigured     bool
}

// RegisterTools registers all MCP tools on the server.
func RegisterTools(server *mcp.Server) {
	registerProjectTools(server)
	registerTaskTools(server)
	registerPlanTools(server)
	registerImageTools(server)
	registerVideoTools(server)
	registerVideoASRTools(server)
	registerWritingTools(server)
	registerPublishingTools(server)
	registerWorkspaceTools(server)
	registerSeednoteFormatTools(server)
	registerTemplateTools(server)
	registerResourceTools(server)
	registerSeednoteTools(server)
	registerMediaPipelineTools(server)
	registerLiveSliceTools(server)
	registerTopicPoolTools(server)
	registerProgressTools(server)
	registerAgentFeedbackTools(server)
}

// parseArgs unmarshals raw JSON arguments into a map.
func parseArgs(raw json.RawMessage) map[string]any {
	args := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &args)
	}
	return args
}

// parseStringArray reads a JSON string-array argument (e.g. ["a","b"]) into a
// []string. It accepts both []any (the shape json.Unmarshal produces for a JSON
// array) and []string; empty/non-string items are silently dropped. Returns nil
// when the key is absent or not an array, so callers can treat "unset" and
// "empty" uniformly via len().
func parseStringArray(args map[string]any, key string) []string {
	v, ok := args[key]
	if !ok {
		return nil
	}
	var out []string
	switch vv := v.(type) {
	case []any:
		for _, item := range vv {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
	case []string:
		for _, s := range vv {
			if s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func registerProjectTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "list_projects",
		Description: "List the authenticated user's projects (WeChat accounts). Each project represents a WeChat Official Account or Seednote account.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":   map[string]any{"type": "string", "enum": []any{"active", "archived"}, "description": "Filter by status"},
				"platform": map[string]any{"type": "string", "enum": []any{"article", "seednote", "moments", "ecommerce", "videocreator", "videoeditor", "montage"}, "description": "Filter by platform type"},
			},
		},
	}, projectListHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_project",
		Description: "Get details of a specific project by ID, including its configuration (WeChat AppID, instructions positioning, style, theme, etc.).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
			},
			"required": []any{"project_id"},
		},
	}, projectGetHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_project_profile",
		Description: "Get the resolved project runtime profile for AI content generation. This is the single project facts entrypoint: the server applies task snapshots, sanitizes secrets, resolves style/theme/author dimensions, and returns platform-specific blocks such as videocreator, videoeditor, ecommerce, or montage. Videocreator projects include resolved_profile, agent_brief, defaults, policy, model_catalog, pricing, and references; videoeditor projects expose editing-source and delivery requirements; montage projects expose montage defaults and task input. When task_id is provided, the task's frozen project_snapshot is used; old rows without a snapshot fall back to legacy task overrides/project resolution. Does NOT expose credentials or unavailable models.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"scope":      map[string]any{"type": "string", "enum": []any{"article", "seednote", "moments", "ecommerce", "videocreator", "videoeditor", "montage"}, "description": "Legacy output hint. New agents should omit this and let the server return the platform-specific block automatically."},
				"task_id":    map[string]any{"type": "string", "description": "Optional task UUID. When provided, reads the task's frozen project_snapshot so historical tasks stay reproducible. The task must belong to the same project and user, otherwise the call is rejected. Always pass task_id when one exists."},
			},
			"required": []any{"project_id"},
		},
	}, accountInfoHandler)
}

func registerTaskTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "list_tasks",
		Description: "List tasks for the authenticated user with optional filters.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":     map[string]any{"type": "string", "enum": []any{"pending", "running", "completed", "failed", "cancelled"}, "description": "Filter by status"},
				"project_id": map[string]any{"type": "string", "description": "Filter by project ID"},
				"plan_id":    map[string]any{"type": "string", "description": "Filter by plan ID"},
				"limit":      map[string]any{"type": "integer", "description": "Max results (default 20, max 100)", "default": 20},
			},
		},
	}, taskListHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_task",
		Description: "Get detailed information about a specific task including status, progress log, and result.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
			},
			"required": []any{"task_id"},
		},
	}, taskGetHandler)

	server.AddTool(&mcp.Tool{
		Name:        "cancel_task",
		Description: "Cancel a pending or running task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID to cancel"},
			},
			"required": []any{"task_id"},
		},
	}, taskCancelHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_project_titles",
		Description: "List all recorded content titles for a project. Use this before selecting a new title to avoid duplicates within the same project.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
			},
			"required": []any{"project_id"},
		},
	}, titleListHandler)

	server.AddTool(&mcp.Tool{
		Name:        "finalize_task_title",
		Description: "Record the Agent-selected final content title before title-dependent artifacts are generated, so future tasks can deduplicate by canonical title.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
				"title":   map[string]any{"type": "string", "description": "Final content title selected by the owning Agent"},
			},
			"required": []any{"task_id", "title"},
		},
	}, titleFinalizeHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_task_files",
		Description: "List output files for a completed task. Returns file names, roles (cover, html, markdown, image), and sizes.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
			},
			"required": []any{"task_id"},
		},
	}, taskFilesHandler)
}

func registerPlanTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "list_plans",
		Description: "List scheduled content plans for the authenticated user.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Filter by project ID"},
			},
		},
	}, planListHandler)

	server.AddTool(&mcp.Tool{
		Name:        "create_plan",
		Description: "Create a scheduled content plan that automatically generates tasks on a cron schedule.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"cron_expr":  map[string]any{"type": "string", "description": "Cron expression (e.g. '0 9 * * *' for daily at 9am)"},
				"prompt":     map[string]any{"type": "string", "description": "Optional prompt/instructions for auto-generated content"},
			},
			"required": []any{"project_id", "cron_expr"},
		},
	}, planCreateHandler)
}

// ---------------------------------------------------------------------------
// Tool handler implementations (closures over Services, set via SetServices)
// ---------------------------------------------------------------------------

var svcs *Services

// SetServices stores the service instances for use by tool handlers.
func SetServices(s *Services) {
	svcs = s
}

func projectListHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	opts := repository.ProjectListOptions{}
	if v, ok := args["status"].(string); ok {
		opts.Status = v
	}
	if v, ok := args["platform"].(string); ok {
		opts.Platform = v
	}
	projects, err := svcs.ProjectSvc.List(context.Background(), userID, opts)
	if err != nil {
		return errorResult(fmt.Sprintf("list projects: %v", err)), nil
	}
	for _, ch := range projects {
		service.SanitizeProject(ch)
	}
	return textResult(projects)
}

func projectGetHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}

	ch, stats, err := svcs.ProjectSvc.Get(context.Background(), userID, projectID)
	if err != nil {
		return errorResult(fmt.Sprintf("get project: %v", err)), nil
	}
	service.SanitizeProject(ch)
	return textResult(map[string]any{"project": ch, "stats": stats})
}

func accountInfoHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	info, errMsg := buildAccountInfo(ctx, userID, parseArgs(req.Params.Arguments))
	if errMsg != "" {
		return errorResult(errMsg), nil
	}
	return textResult(info)
}

// buildAccountInfo is the testable core of get_project_profile. It returns a
// flat, fully resolved creation profile. For new rows, a supplied task_id applies
// the task's frozen project_snapshot; old rows without snapshots fall back to
// legacy task overrides over the current project. The same ResolveStyle primitive
// feeds prompt/settings/MCP channels so they cannot disagree.
func buildAccountInfo(ctx context.Context, userID string, args map[string]any) (map[string]any, string) {
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return nil, "project_id is required"
	}
	taskID, _ := args["task_id"].(string)

	if svcs.ProjectSvc == nil {
		return nil, "project service not available"
	}
	ch, _, err := svcs.ProjectSvc.Get(ctx, userID, projectID)
	if err != nil {
		return nil, fmt.Sprintf("get project: %v", err)
	}
	service.SanitizeProject(ch)

	// Load the requested task (if any) so its frozen project snapshot surfaces.
	// The task must belong to the same user+project, otherwise the call is rejected.
	var task *model.Task
	usesProjectSnapshot := false
	if taskID != "" {
		if svcs.TaskSvc == nil {
			return nil, "task service not available"
		}
		var terr error
		task, terr = svcs.TaskSvc.GetByID(ctx, taskID)
		if terr != nil {
			return nil, fmt.Sprintf("get task: %v", terr)
		}
		// Guard against cross-project/cross-user injection.
		if task.UserID != userID || task.ProjectID != projectID {
			return nil, "task does not belong to the requested project"
		}
		if snap := task.ProjectSnapshot.Data(); snap.Platform != "" {
			ch = model.ProjectFromSnapshot(ch, snap)
			usesProjectSnapshot = true
		}
	}
	if model.IsVideoCreatorPlatform(ch.Platform) {
		service.SanitizeProjectVideoProfile(ch, videoModelCatalog())
	}

	// The dimensions are independent — the writer key never drives the visual
	// style, the author never equals the writer persona — and each carries its
	// provenance.
	r := service.ResolveStyle(ch, task)

	// Flat profile. Every runtime style/theme/author dimension is exposed directly (no
	// template_* namespace) with its own *_source provenance tag:
	//   - visual_style   (图片视觉): free-text image visual style; empty = none
	//   - writer     (写作者):   writer YAML resource key, e.g. "dan-koe"
	//   - author         (作者署名): published author name; pass to publish_draft's author
	//   - theme          (排版样式): theme resource key, e.g. "autumn-warm"
	// Writer avatars/nicknames are Studio-only display metadata and must never
	// enter Agent/MCP runtime profiles.
	info := map[string]any{
		"name":                ch.Name,
		"positioning":         ch.Instructions,
		"instructions":        ch.Instructions,
		"keywords":            ch.Keywords,
		"platform":            ch.Platform,
		"visual_style":        r.VisualStyle,
		"writer":              r.Writer,
		"author":              r.Author,
		"theme":               r.Theme,
		"visual_style_source": r.VisualStyleSource,
		"writer_source":       r.WriterSource,
		"author_source":       r.AuthorSource,
		"theme_source":        r.ThemeSource,
	}
	resolvedProfile := map[string]any{
		"id":                    ch.ID,
		"name":                  ch.Name,
		"platform":              ch.Platform,
		"profile_url":           ch.ProfileURL,
		"avatar_url":            ch.AvatarURL,
		"instructions":          ch.Instructions,
		"positioning":           ch.Instructions,
		"keywords":              ch.Keywords,
		"visual_style":          r.VisualStyle,
		"creative_constraints":  creativeConstraintsForProfile(ch.Platform, r.VisualStyle),
		"visual_style_label":    "图片视觉",
		"image_ratio":           ch.ImageRatio,
		"uses_project_snapshot": usesProjectSnapshot,
		"sources": map[string]any{
			"visual_style": r.VisualStyleSource,
			"instructions": profileSource(usesProjectSnapshot),
			"keywords":     profileSource(usesProjectSnapshot),
		},
	}
	hasReference := service.EffectiveReferenceAssetID(task) != ""
	if hasReference {
		resolvedProfile["reference_image_path"] = referenceImageRuntimePath
	}
	info["resolved_profile"] = resolvedProfile

	switch ch.Platform {
	case "seednote":
		// For seednote, style is a visual/image style description used for image prompt generation.
		imageConfig := map[string]any{}
		if hasReference {
			imageConfig["reference_image_path"] = referenceImageRuntimePath
		}
		info["image_config"] = imageConfig
		if svcs.ImageModelResolver != nil {
			imageModelKey := ""
			if task != nil {
				imageModelKey = task.ImageModelKey
			}
			resolved, resolveErr := svcs.ImageModelResolver.ResolveImageModelForGeneration(ctx, userID, imageModelKey, "content", 0)
			if resolveErr != nil {
				return nil, fmt.Sprintf("resolve image generation capability: %v", resolveErr)
			}
			if resolved == nil {
				return nil, "resolve image generation capability: resolver returned no descriptor"
			}
			info["image_generation"] = map[string]any{
				"provider":             resolved.Provider,
				"model":                resolved.Model,
				"supports_reference":   resolved.SupportsReference,
				"max_reference_images": resolved.MaxReferenceImages,
				"selection_reason":     resolved.SelectionReason,
			}
		}
	case "moments":
		imageConfig := map[string]any{"default_ratio": firstNonEmpty(ch.ImageRatio, "3:4")}
		if hasReference {
			imageConfig["reference_image_path"] = referenceImageRuntimePath
		}
		info["image_config"] = imageConfig
		info["moments"] = map[string]any{
			"required_artifacts": []string{
				"material-analysis.md",
				"content.md",
				"quality-review.md",
			},
			"method": []string{
				"六类素材：发售、人设、产品、案例、生活、认知",
				"四层提炼：观点层、框架层、风格层、人设层",
			},
			"auto_publish":      false,
			"scheduled_plans":   false,
			"falsification_ban": "不伪造客户案例、成交数据、用户反馈",
		}
	case "ecommerce":
		// E-commerce: surface the package config (selected modules, target
		// platform, brand brief, language) plus the resolved image model and the
		// workspace path where the executor materialized the product photos.
		// The agent uses the concrete provider/model only to plan its independent
		// product-photo workflow; task model keys remain server-owned. Product
		// photos are downloaded by the executor into .anban-creator/products/ (see
		// agent.DownloadProductImages); the agent reads index.json there for the
		// exact filenames.
		ec := map[string]any{
			"product_photo_dir": ".anban-creator/products",
			"consistency_audit": true, // verify_with_vision self-check loop
		}
		if task != nil {
			provider, mdl := resolveEcommerceImageProvider(ctx, userID, task)
			ec["image_model"] = map[string]any{
				"provider": provider,
				"model":    mdl,
			}
			cfg := task.Ecommerce.Data()
			ec["selected_modules"] = cfg.SelectedModules
			ec["product_photo_count"] = len(cfg.ProductPhotos)
			if cfg.TargetPlatform != "" {
				ec["target_platform"] = cfg.TargetPlatform
			}
			if cfg.SellingPoints != "" {
				ec["selling_points"] = cfg.SellingPoints
			}
			if cfg.Language != "" {
				ec["language"] = cfg.Language
			}
			if cfg.BrandBrief != "" {
				ec["brand_brief"] = cfg.BrandBrief
			}
		}
		info["ecommerce"] = ec
	case model.PlatformMontage:
		info["montage"] = buildMontageProfileBlock(ch, task)
	}
	if model.IsVideoCreatorPlatform(ch.Platform) {
		videoBlock := buildVideoProfileBlock(ch, task)
		info["videocreator"] = videoBlock
		info["agent_brief"] = buildProjectAgentBrief(ch, usesProjectSnapshot, videoBlock)
	} else if model.IsVideoEditorPlatform(ch.Platform) {
		info["videoeditor"] = map[string]any{
			"input_attachment_dir":  ".anban-creator/input-attachments",
			"requires_source_media": true,
			"delivery":              []string{"final.mp4", "preview.mp4", "capcut draft"},
		}
	}

	// Available resource options for the platform (best-effort; the embedded
	// resource manager may be nil in some test contexts).
	mgr := resources.Manager()
	if mgr != nil {
		info["available_themes"] = mgr.ListByPlatform(resources.CategoryTheme, ch.Platform)
		info["available_writers"] = mgr.ListByPlatform(resources.CategoryWriter, ch.Platform)
		info["available_article_templates"] = mgr.ListByPlatform(resources.CategoryArticleTemplate, ch.Platform)

		// Descriptions for the RESOLVED theme / writer key (not the raw project fields).
		if r.Theme != "" {
			if e := mgr.Get(resources.CategoryTheme, r.Theme); e != nil {
				info["theme_description"] = e.Description
			}
		}
		if r.Writer != "" {
			if e := mgr.Get(resources.CategoryWriter, r.Writer); e != nil {
				// writer_description describes the writing VOICE (tone/人设/调性) of
				// the resolved writer resource key. It is independent of visual_style;
				// the two describe different dimensions and must not be conflated.
				info["writer_description"] = e.Description
			}
		}
	}

	return info, ""
}

func buildMontageProfileBlock(ch *model.Project, task *model.Task) map[string]any {
	defaults := model.MontageDefaults{}
	if ch != nil {
		defaults = ch.MontageDefaults.Data()
	}
	input := model.MontageInput{}
	if task != nil && model.IsMontagePlatform(task.Type) {
		input = task.MontageInput.Data()
	}
	env := map[string]bool{}
	toolPolicy := map[string]any{}
	pipelineDefaults := map[string]any{}
	if billSvc != nil && billSvc.config != nil {
		env = billSvc.config.Montage.RedactedEnv()
		for key, value := range billSvc.config.Montage.ToolPolicy {
			toolPolicy[key] = value
		}
		for key, value := range billSvc.config.Montage.PipelineDefaults {
			pipelineDefaults[key] = value
		}
	}
	return map[string]any{
		"defaults":               defaults,
		"input":                  input,
		"source_asset_count":     len(input.SourceAssets),
		"workspace_input_file":   "montage-input.json",
		"tool_policy_file":       "montage-tool-policy.json",
		"pipeline_defaults_file": "montage-pipeline-defaults.json",
		"project_manifest_file":  "montage-project.json",
		"output_dir":             "output/montage",
		"required_artifacts":     []string{"final.mp4", "delivery-manifest.json"},
		"artifact_roles":         []string{"final_video", "delivery_manifest", "source_manifest", "timeline", "subtitles", "audio", "run_log", "failure_diagnosis"},
		"env":                    env,
		"tool_policy":            toolPolicy,
		"pipeline_defaults":      pipelineDefaults,
		"runner_contract":        "Agent prepares montage-input.json and montage-project.json, runs the Montage adapter from $ANBAN_MONTAGE_SUBMODULE_PATH when set, otherwise third_party/OpenMontage, then registers task files by artifact role.",
	}
}

func profileSource(usesProjectSnapshot bool) string {
	if usesProjectSnapshot {
		return "snapshot"
	}
	return "project"
}

func creativeConstraintsForProfile(platform, visualStyle string) string {
	if model.IsVideoPlatform(platform) {
		return ""
	}
	return visualStyle
}

func buildVideoProfileBlock(ch *model.Project, task *model.Task) map[string]any {
	defaults := ch.VideoDefaults.Data()
	policy := ch.VideoModelPolicy.Data()
	catalog := filterVideoCatalogForPolicy(videoModelCatalog(), policy)
	taskConfig := model.VideoTaskConfig{}
	input := model.VideoInput{}
	if task != nil && model.IsVideoCreatorPlatform(task.Type) {
		taskConfig = task.VideoConfig.Data()
		input = task.VideoInput.Data()
	}
	resolvedDefaults := map[string]any{
		"purpose":         firstNonEmpty(taskConfig.Purpose, defaults.Purpose),
		"creative_type":   firstNonEmpty(taskConfig.CreativeType, defaults.CreativeType),
		"subject_profile": firstNonEmpty(taskConfig.SubjectProfile, defaults.SubjectProfile),
		"audience":        firstNonEmpty(taskConfig.Audience, defaults.Audience),
		"single_message":  firstNonEmpty(taskConfig.SingleMessage, defaults.SingleMessage),
		"model_key":       firstNonEmpty(taskConfig.ModelKey, defaults.ModelKey, policy.DefaultModel),
		"resolution":      firstNonEmpty(taskConfig.Resolution, defaults.Resolution),
		"ratio":           firstNonEmpty(taskConfig.Ratio, defaults.Ratio),
		"duration":        firstPositiveInt64(taskConfig.Duration, defaults.Duration),
		"watermark":       firstBoolPtr(taskConfig.Watermark, defaults.Watermark),
		"preflight":       taskOrDefaultPreflight(taskConfig, defaults, task != nil && model.IsVideoCreatorPlatform(task.Type)),
	}
	return map[string]any{
		"defaults": resolvedDefaults,
		"policy": map[string]any{
			"allowed_models":       policy.AllowedModels,
			"default_model":        policy.DefaultModel,
			"allow_auto_downgrade": policy.AllowAutoDowngrade,
			"max_resolution":       policy.MaxResolution,
			"max_duration":         policy.MaxDuration,
			"model_selection_rule": "Only keys present in model_catalog and allowed_models are usable; the server rejects unavailable models.",
			"auto_downgrade_label": "参数不支持时自动降到可用分辨率",
		},
		"model_catalog": catalog,
		"input":         input,
		"references":    input.References,
		"task_config":   taskConfig,
		"pricing": map[string]any{
			"retail_model":             "fixed_sku",
			"base_task_fee_rule":       "VideoCreator task admission uses the fixed task SKU.",
			"operation_billing_rule":   "Each provider segment pins a fixed SKU before dispatch; its accepted-task charge is enqueued only after the matching provider output is durably persisted as a task file.",
			"operation_debt_rule":      "Accepted task segment charges may create debt and never invalidate a durable successful video result.",
			"sku_selectors":            []string{"model_key", "resolution", "duration_tier", "input_mode"},
			"provider_cost_separation": "Provider usage is internal cost evidence and never determines retail credits.",
		},
		"persistent_file_rule": "all server-persistent references and generated results must be OSS-backed task files; local agent files are temporary only",
		"visual_anchor_generation": map[string]any{
			"available":           true,
			"default_image_type":  "content",
			"max_auto_anchors":    3,
			"verify_with_vision":  "Required for generated visual anchors; accept only verification.passed=true and score >= 0.75 when a score is present.",
			"register_tool":       "After a generated anchor passes vision verification, call register_video_reference(type=\"image_url\", file_path=<generated file_path>, reference_role=\"subject identity\" | \"product appearance\" | \"first frame\").",
			"fallback":            "If generate_image is unavailable or the main anchor fails two verification attempts, use text-only anchors for ordinary videos or stop and request user reference media for high-consistency tasks.",
			"derived_anchor_rule": "When generating 2-3 anchors, derive later anchors from the approved main anchor with ref_image_path; do not independently regenerate the same subject.",
		},
	}
}

func filterVideoCatalogForPolicy(catalog service.VideoModelCatalog, policy model.VideoModelPolicy) service.VideoModelCatalog {
	if catalog == nil {
		catalog = service.VideoModelCatalog{}
	}
	if len(policy.AllowedModels) == 0 {
		return catalog
	}
	filtered := service.VideoModelCatalog{}
	for _, key := range policy.AllowedModels {
		if spec, ok := catalog[key]; ok {
			filtered[key] = spec
		}
	}
	return filtered
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstBoolPtr(values ...*bool) any {
	for _, value := range values {
		if value != nil {
			return *value
		}
	}
	return nil
}

func taskOrDefaultPreflight(taskConfig model.VideoTaskConfig, defaults model.VideoDefaults, hasVideoTask bool) bool {
	if hasVideoTask && taskConfig.Preflight {
		return true
	}
	return defaults.Preflight
}

func buildProjectAgentBrief(ch *model.Project, usesProjectSnapshot bool, videoBlock map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "项目：%s\n", ch.Name)
	fmt.Fprintf(&b, "平台：%s\n", ch.Platform)
	if ch.Instructions != "" {
		fmt.Fprintf(&b, "项目定位：%s\n", ch.Instructions)
	}
	if ch.Keywords != "" {
		fmt.Fprintf(&b, "关键词：%s\n", ch.Keywords)
	}
	b.WriteString("分析入口：项目长期定位只读取工作区 CLAUDE.md / project.instructions；本次需求读取 task.prompt、video_creator_input（profile 中为 videocreator.input）的 brief、references 与 hard_constraints。\n")
	b.WriteString("Studio 不再提供视频玩法、商业目标、制作模式、内容类型、主体、受众或核心信息；这些业务判断必须由 videocreator agent 自主分析并落盘到 video_creator_config。\n")
	if usesProjectSnapshot {
		b.WriteString("配置来源：任务创建时冻结的项目快照\n")
	}
	if defaults, ok := videoBlock["defaults"].(map[string]any); ok {
		fmt.Fprintf(&b, "视频默认参数：model=%v, resolution=%v, ratio=%v, duration=%v, watermark=%v\n",
			defaults["model_key"], defaults["resolution"], defaults["ratio"], defaults["duration"], defaults["watermark"])
	}
	if pricing, ok := videoBlock["pricing"].(map[string]any); ok {
		fmt.Fprintf(&b, "积分规则：任务准入使用固定任务 SKU；每个视频片段在提交前固定操作 SKU，仅在对应 provider 输出持久化为任务文件后排队扣费；已接受任务允许形成欠款且不影响成功视频结果（%v）。\n", pricing["operation_billing_rule"])
	}
	b.WriteString("模型规则：只能使用本 profile 返回的 videocreator.model_catalog 与 videocreator.policy.allowed_models 中的模型 key；未返回的模型不可使用。")
	return b.String()
}

func taskListHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	limit := 20
	if v, ok := args["limit"].(float64); ok && int(v) > 0 && int(v) <= 100 {
		limit = int(v)
	}
	status, _ := args["status"].(string)
	projectID, _ := args["project_id"].(string)
	planID, _ := args["plan_id"].(string)

	tasks, total, err := svcs.TaskSvc.List(context.Background(), userID, 0, limit, status, projectID, planID)
	if err != nil {
		return errorResult(fmt.Sprintf("list tasks: %v", err)), nil
	}
	return textResult(map[string]any{"items": mcpTaskResponses(tasks), "total": total})
}

func taskGetHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}

	task, err := svcs.TaskSvc.GetByID(context.Background(), taskID)
	if err != nil {
		return errorResult(fmt.Sprintf("get task: %v", err)), nil
	}
	if task.UserID != userID {
		return errorResult("task not found"), nil
	}
	var result any
	if task.Result != nil && *task.Result != "" {
		_ = json.Unmarshal([]byte(*task.Result), &result)
	}
	resp := map[string]any{
		"id":            task.ID,
		"type":          task.Type,
		"status":        task.Status,
		"prompt":        task.Prompt,
		"progress_log":  task.ProgressLog,
		"result":        result,
		"error_message": task.ErrorMessage,
		"retry_count":   task.RetryCount,
		"created_at":    task.CreatedAt,
		"started_at":    task.StartedAt,
		"completed_at":  task.CompletedAt,
	}
	rewriteMCPVideoFields(resp, task.Type, task.VideoInput.Data(), task.VideoConfig.Data())
	return textResult(resp)
}

func taskCancelHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}

	if err := svcs.TaskSvc.CancelForUser(context.Background(), userID, taskID); err != nil {
		return errorResult(fmt.Sprintf("cancel task: %v", err)), nil
	}
	return textResult(map[string]any{"cancelled": true, "task_id": taskID})
}

func titleListHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}

	if _, _, err := svcs.ProjectSvc.Get(context.Background(), userID, projectID); err != nil {
		return errorResult(fmt.Sprintf("get project: %v", err)), nil
	}

	titles, err := svcs.TaskSvc.ListTitles(context.Background(), projectID)
	if err != nil {
		return errorResult(fmt.Sprintf("list titles: %v", err)), nil
	}
	return textResult(map[string]any{"titles": titles, "count": len(titles)})
}

func titleFinalizeHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	title, _ := args["title"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if strings.TrimSpace(title) == "" {
		return errorResult("title is required"), nil
	}

	finalTitle, err := svcs.TaskSvc.FinalizeTitle(context.Background(), userID, taskID, title)
	if err != nil {
		return errorResult(fmt.Sprintf("finalize title: %v", err)), nil
	}
	return textResult(map[string]any{"task_id": taskID, "title": finalTitle, "updated": true})
}

func taskFilesHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if _, err := svcs.TaskSvc.ValidateAgentTaskAccess(context.Background(), taskID, userID); err != nil {
		return errorResult("task not found"), nil
	}

	files, err := svcs.TaskSvc.GetVisibleFiles(context.Background(), taskID)
	if err != nil {
		return errorResult(fmt.Sprintf("get task files: %v", err)), nil
	}
	return textResult(map[string]any{"files": files, "count": len(files)})
}

func planListHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)

	plans, total, err := svcs.PlanSvc.List(context.Background(), userID, 0, 50, projectID)
	if err != nil {
		return errorResult(fmt.Sprintf("list plans: %v", err)), nil
	}
	return textResult(map[string]any{"items": mcpPlanResponses(plans), "total": total})
}

func planCreateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	cronExpr, _ := args["cron_expr"].(string)
	prompt, _ := args["prompt"].(string)

	if projectID == "" || cronExpr == "" {
		return errorResult("project_id and cron_expr are required"), nil
	}

	plan, err := svcs.PlanSvc.Create(context.Background(), service.CreatePlanParams{
		UserID:    userID,
		ProjectID: projectID,
		CronExpr:  cronExpr,
		Prompt:    prompt,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("create plan: %v", err)), nil
	}
	return textResult(mcpPlanResponse(plan))
}
