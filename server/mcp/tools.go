package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/resources"
	"github.com/royalrick/anbanwriter/server/seednote"
	"github.com/royalrick/anbanwriter/server/service"
)

// Services holds the service instances needed by MCP tools.
type Services struct {
	ProjectSvc       *service.ProjectService
	TaskSvc          *service.TaskService
	CreditSvc        *service.CreditService
	PlanSvc          *service.PlanService
	ImageSvc         *service.ImageService
	WritingSvc       *service.WritingService
	PublishingSvc    *service.PublishingService
	WorkspaceSvc     *service.WorkspaceService
	TemplateSvc      *service.TemplateService
	LiveSliceSvc     *service.LiveSliceService
	SeednoteClient   *seednote.Client
	TopicPoolSvc     *service.TopicPoolService
	AgentFeedbackSvc *service.AgentFeedbackService
}

// RegisterTools registers all MCP tools on the server.
func RegisterTools(server *mcp.Server) {
	registerProjectTools(server)
	registerTaskTools(server)
	registerCreditTools(server)
	registerPlanTools(server)
	registerImageTools(server)
	registerWritingTools(server)
	registerPublishingTools(server)
	registerWorkspaceTools(server)
	registerSeednoteFormatTools(server)
	registerTemplateTools(server)
	registerResourceTools(server)
	registerSeednoteTools(server)
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
				"platform": map[string]any{"type": "string", "enum": []any{"article", "seednote"}, "description": "Filter by platform type"},
			},
		},
	}, projectListHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_project",
		Description: "Get details of a specific project by ID, including its configuration (WeChat AppID, positioning, style, theme, etc.).",
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
		Description: "Get formatted account information for AI content creation context. Returns positioning, keywords, and three INDEPENDENT style dimensions: `style` (图片视觉 image visual style, free text), `writing_style` (写作风格 writer resource key e.g. dan-koe), `theme` (排版样式 theme resource key e.g. autumn-warm). These three never derive from each other. Does NOT expose sensitive credentials.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"scope":      map[string]any{"type": "string", "enum": []any{"article", "seednote", "ecommerce"}, "description": "Filter output by content type"},
				"task_id":    map[string]any{"type": "string", "description": "Optional task UUID. When provided, the task is the single source of truth: its resolved `style`/`writing_style`/`theme` (precedence task > template > plan > project) are returned with *_source=\"task\"; otherwise the project's values are returned with *_source=\"project\". The task must belong to the same project and user, otherwise the call is rejected. Always pass task_id when one exists so template-derived dimensions surface correctly."},
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
		Description: "Record the hook-selected final content title for a task. Hooks call this before final delivery so future tasks can deduplicate by canonical title.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
				"title":   map[string]any{"type": "string", "description": "Final content title selected by the completion hook"},
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

func registerCreditTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "get_credit_balance",
		Description: "Get the authenticated user's current credit balance.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, creditsGetHandler)
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

// buildAccountInfo is the testable core of get_project_profile. It resolves the
// three orthogonal style dimensions (图片视觉 / 写作风格 / 排版样式). When a task_id
// is supplied and the task belongs to the requesting user+project, the task is the
// single source of truth (its Style/WritingStyle/Theme are already resolved at
// creation); otherwise the project's values are used. The three dimensions never
// derive from one another.
func buildAccountInfo(ctx context.Context, userID string, args map[string]any) (map[string]any, string) {
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return nil, "project_id is required"
	}
	scope, _ := args["scope"].(string)
	taskID, _ := args["task_id"].(string)

	if svcs.ProjectSvc == nil {
		return nil, "project service not available"
	}
	ch, _, err := svcs.ProjectSvc.Get(ctx, userID, projectID)
	if err != nil {
		return nil, fmt.Sprintf("get project: %v", err)
	}
	service.SanitizeProject(ch)

	// Resolve the three orthogonal style dimensions. Each is independent — the
	// writer never drives the visual style. When a task_id is supplied, the task is
	// the single source of truth: Task.Style / Task.WritingStyle / Task.Theme are
	// already the resolved effective values (precedence task > template > plan >
	// project, computed at creation). Without a task_id we fall back to the project.
	effectiveVisual := ch.Style
	effectiveWriter := ch.WritingStyle
	effectiveTheme := ch.Theme
	visualSource := "project"
	writerSource := "project"
	themeSource := "project"
	// 公众号人设维度（作者署名 + 写作风格模仿 + 可选头像），与视觉/写作key/排版正交，
	// 同样按 task > template > project 解析。
	effectiveAuthor := ch.Author
	effectiveAuthorIntro := ch.AuthorStyleIntro
	effectiveAuthorAvatar := ch.AuthorAvatarURL
	var taskTemplateID *string
	var task *model.Task
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
		// Task fields hold the resolved effective values (precedence
		// task > template > plan > project, computed at creation). We still fall
		// back to the project PER DIMENSION when a task field is empty, and
		// report each dimension's source honestly — so a task that only set its
		// writer doesn't silently clobber the project's visual/theme.
		// (Persona Author/AuthorStyleIntro/AuthorAvatarURL is resolved centrally
		// below via task > template > project, so it is not folded here.)
		if task.Style != "" {
			effectiveVisual = task.Style
			visualSource = "task"
		}
		if task.WritingStyle != "" {
			effectiveWriter = task.WritingStyle
			writerSource = "task"
		}
		if task.Theme != "" {
			effectiveTheme = task.Theme
			themeSource = "task"
		}
		taskTemplateID = task.TemplateID
	}

	// Base account info (always included). The three style dimensions are exposed
	// as independent fields so the agent never conflates them:
	//   - style         (图片视觉): free-text image visual style; empty = none
	//   - writing_style (写作风格): writer resource key, e.g. "dan-koe"
	//   - theme         (排版样式): theme resource key, e.g. "autumn-warm"
	info := map[string]any{
		"name":                 ch.Name,
		"author":               effectiveAuthor,
		"positioning":          ch.Positioning,
		"keywords":             ch.Keywords,
		"style":                effectiveVisual,
		"writing_style":        effectiveWriter,
		"theme":                effectiveTheme,
		"style_source":         visualSource,
		"writing_style_source": writerSource,
		"theme_source":         themeSource,
		"platform":             ch.Platform,
	}

	switch scope {
	case "seednote":
		// For seednote, style is a visual/image style description used for image prompt generation.
		info["image_config"] = map[string]any{
			"reference_image_url": ch.ReferenceImageURL,
		}
	case "ecommerce":
		// E-commerce: surface the package config (selected modules, target
		// platform, brand brief, language) plus the resolved image model and the
		// workspace path where the executor materialized the product photos.
		// The image model is chosen by the user at task creation (Task.ImageModelKey)
		// and resolved here to a concrete provider/model so the agent can adapt its
		// reference-image strategy: OpenAI/Gemini accept multiple refs (≤16 via
		// generate_image's ref_image_paths) for max product fidelity; Volcengine/
		// Seedream take a single ref (strong i2i), so the agent uses one anchor ref
		// + product-bible text block. Product photos are downloaded by the executor
		// into .anbanwriter/products/ (see agent.DownloadProductImages); the agent
		// reads index.json there for the exact filenames.
		ec := map[string]any{
			"product_photo_dir": ".anbanwriter/products",
			"consistency_audit": true, // verify_with_vision self-check loop
		}
		if task != nil {
			provider, mdl := resolveEcommerceImageProvider(ctx, userID, task)
			ec["image_model"] = map[string]any{
				"provider": provider,
				"model":    mdl,
				"key":      task.ImageModelKey,
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
	}

	// Add available resource options for the platform.
	info["available_themes"] = resources.Manager().ListByPlatform(resources.CategoryTheme, ch.Platform)
	info["available_writers"] = resources.Manager().ListByPlatform(resources.CategoryWriter, ch.Platform)

	// Descriptions for the RESOLVED theme / writer (not the raw project fields).
	if effectiveTheme != "" {
		if e := resources.Manager().Get(resources.CategoryTheme, effectiveTheme); e != nil {
			info["theme_description"] = e.Description
		}
	}
	if effectiveWriter != "" {
		if e := resources.Manager().Get(resources.CategoryWriter, effectiveWriter); e != nil {
			// writing_style_description describes the writing VOICE (tone/人设/调性)
			// of the resolved writer resource key. It is independent of `style`
			// (visual). The two describe different dimensions and must not be conflated.
			info["writing_style_description"] = e.Description
		}
	}

	// Surface the effective template's content. The effective template is the
	// task's template if present (precedence task > task-template > plan), else
	// the project's bound 公众号 template (project-template). Three things are
	// delivered, kept as STRICTLY INDEPENDENT concepts (the article 作者/byline
	// must never be conflated with 写作风格/writing imitation):
	//
	//   - 作者 (byline): the template's AuthorName overrides the project byline
	//     and surfaces as the top-level `author` (passed to publish_draft).
	//   - 写作风格 (writing imitation, free text): AuthorStyleIntro for article,
	//     falling back to the writer-key scaffold WritingStyle (poster). Surfaced
	//     as template_writing_style.
	//   - template_author_avatar: the optional 写作风格 persona avatar.
	//   - For a project-level template, its theme is folded into the resolved
	//     `theme` so convert_markdown uses it (a task-level template already had
	//     its theme folded into Task.Theme at creation).
	//
	// When NO template is bound (task or project), the project's own persona
	// (author_style_intro / author_avatar_url) is surfaced instead, so a writing
	// direction defined directly on the project still reaches the agent. Errors
	// (template deleted) are logged-and-skipped so a stale id never breaks the
	// profile — the keys simply stay absent.
	effectiveTemplateID := ""
	templateFromTask := false
	if taskTemplateID != nil && *taskTemplateID != "" {
		effectiveTemplateID = *taskTemplateID
		templateFromTask = true
	} else if ch.TemplateID != "" {
		effectiveTemplateID = ch.TemplateID
	}

	var tmpl *model.Template
	if effectiveTemplateID != "" && svcs.TemplateSvc != nil {
		if t, terr := svcs.TemplateSvc.GetByID(ctx, effectiveTemplateID); terr == nil {
			tmpl = t
			info["template_id"] = tmpl.ID
			info["template_name"] = tmpl.Name
			if tmpl.AuthorName != "" {
				info["template_author_name"] = tmpl.AuthorName
			}
			info["template_theme"] = tmpl.Theme
			info["template_structure"] = extractScaffoldText(tmpl.Structure)
			info["template_example"] = extractScaffoldText(tmpl.ExampleContent)
			// Project-level template (no task template): fold its theme into the
			// resolved theme so convert_markdown uses it. A task template's theme was
			// already folded into Task.Theme at creation (and set effectiveTheme above),
			// so we only fold for the project-template path — never clobbering a task's
			// explicit theme override. Persona is resolved centrally below.
			if !templateFromTask && tmpl.Theme != "" {
				effectiveTheme = tmpl.Theme
				themeSource = "project-template"
				info["theme"] = effectiveTheme
				info["theme_source"] = themeSource
				if e := resources.Manager().Get(resources.CategoryTheme, effectiveTheme); e != nil {
					info["theme_description"] = e.Description
				}
			}
		} else if mcpLog != nil {
			mcpLog.Warn().Err(terr).Str("template_id", effectiveTemplateID).
				Msg("linked template not found; skipping content scaffold")
		}
	}

	// 人设维度（作者署名 + 写作风格模仿 + 可选头像）按 task > template > project 解析。
	// 这对 task-template 与 project-template 两条路径都成立：task 字段在创建时已解析
	// （task > template > project），tmpl 为有效模板（缺失则为 nil）。写作风格模仿优先取
	// AuthorStyleIntro，回退 writer-key scaffold WritingStyle（poster）。逐维度独立，作者
	// 署名绝不与写作模仿混用。author 透传给 publish_draft；template_writing_style 供写作
	// 模仿；template_author_avatar 为人设参考（不入署名）。
	var tmplAuthor, tmplIntro, tmplAvatar string
	if tmpl != nil {
		tmplAuthor = tmpl.AuthorName
		tmplIntro = tmpl.AuthorStyleIntro
		if tmplIntro == "" {
			tmplIntro = tmpl.WritingStyle
		}
		tmplAvatar = tmpl.AuthorAvatarURL
	}
	if task != nil {
		effectiveAuthor = firstNonEmptyStr(task.Author, tmplAuthor, ch.Author)
		effectiveAuthorIntro = firstNonEmptyStr(task.AuthorStyleIntro, tmplIntro, ch.AuthorStyleIntro)
		effectiveAuthorAvatar = firstNonEmptyStr(task.AuthorAvatarURL, tmplAvatar, ch.AuthorAvatarURL)
	} else {
		effectiveAuthor = firstNonEmptyStr(tmplAuthor, ch.Author)
		effectiveAuthorIntro = firstNonEmptyStr(tmplIntro, ch.AuthorStyleIntro)
		effectiveAuthorAvatar = firstNonEmptyStr(tmplAvatar, ch.AuthorAvatarURL)
	}
	info["author"] = effectiveAuthor
	if effectiveAuthorIntro != "" {
		info["template_writing_style"] = effectiveAuthorIntro
	}
	if effectiveAuthorAvatar != "" {
		info["template_author_avatar"] = effectiveAuthorAvatar
	}

	return info, ""
}

// firstNonEmptyStr returns the first non-empty argument, or "" when all are empty.
// Local mirror of service.firstNonEmpty (which is unexported) so package mcp can
// resolve persona dimensions with task > template > project precedence without an
// export cycle. Pure helper; all args must be plain strings.
func firstNonEmptyStr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// extractScaffoldText pulls the human-editable text out of a template scaffold
// JSON column. The Studio form stores {"text": "<markdown>"}; the legacy MCP
// save_template path may store richer JSON. We prefer .text, fall back to a
// compact stringification of whatever is there, and return "" for empty/nil so
// the profile omits the key naturally.
func extractScaffoldText(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	if v, ok := m["text"].(string); ok {
		return v
	}
	out, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("%v", m)
	}
	return string(out)
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

	tasks, total, err := svcs.TaskSvc.List(context.Background(), userID, 0, limit, status, projectID)
	if err != nil {
		return errorResult(fmt.Sprintf("list tasks: %v", err)), nil
	}
	return textResult(map[string]any{"items": tasks, "total": total})
}

func taskGetHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}

	task, err := svcs.TaskSvc.GetByID(context.Background(), taskID)
	if err != nil {
		return errorResult(fmt.Sprintf("get task: %v", err)), nil
	}
	var result any
	if task.Result != nil && *task.Result != "" {
		_ = json.Unmarshal([]byte(*task.Result), &result)
	}
	return textResult(map[string]any{
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
	})
}

func taskCancelHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}

	if err := svcs.TaskSvc.Cancel(context.Background(), taskID); err != nil {
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
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}

	files, err := svcs.TaskSvc.GetFiles(context.Background(), taskID)
	if err != nil {
		return errorResult(fmt.Sprintf("get task files: %v", err)), nil
	}
	return textResult(map[string]any{"files": files, "count": len(files)})
}

func creditsGetHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	balance, err := svcs.CreditSvc.GetBalance(context.Background(), userID)
	if err != nil {
		return errorResult(fmt.Sprintf("get credits: %v", err)), nil
	}
	return textResult(map[string]any{"balance": balance})
}

func planListHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)

	plans, total, err := svcs.PlanSvc.List(context.Background(), userID, 0, 50, projectID)
	if err != nil {
		return errorResult(fmt.Sprintf("list plans: %v", err)), nil
	}
	return textResult(map[string]any{"items": plans, "total": total})
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
	return textResult(plan)
}
