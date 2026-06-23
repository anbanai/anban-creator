package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/resources"
	"github.com/royalrick/anbanwriter/server/seednote"
	"github.com/royalrick/anbanwriter/server/service"
)

// Services holds the service instances needed by MCP tools.
type Services struct {
	ChannelSvc       *service.ChannelService
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
	registerChannelTools(server)
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

func registerChannelTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "list_channels",
		Description: "List the authenticated user's channels (WeChat accounts). Each channel represents a WeChat Official Account or Seednote account.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":   map[string]any{"type": "string", "enum": []any{"active", "archived"}, "description": "Filter by status"},
				"platform": map[string]any{"type": "string", "enum": []any{"article", "seednote"}, "description": "Filter by platform type"},
			},
		},
	}, channelListHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_channel",
		Description: "Get details of a specific channel by ID, including its configuration (WeChat AppID, positioning, style, theme, etc.).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
			},
			"required": []any{"channel_id"},
		},
	}, channelGetHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_channel_profile",
		Description: "Get formatted account information for AI content creation context. Returns positioning, keywords, and three INDEPENDENT style dimensions: `style` (图片视觉 image visual style, free text), `writing_style` (写作风格 writer resource key e.g. dan-koe), `theme` (排版样式 theme resource key e.g. autumn-warm). These three never derive from each other. Does NOT expose sensitive credentials.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"scope":      map[string]any{"type": "string", "enum": []any{"article", "seednote"}, "description": "Filter output by content type"},
				"task_id":    map[string]any{"type": "string", "description": "Optional task UUID. When provided, the task is the single source of truth: its resolved `style`/`writing_style`/`theme` (precedence task > template > plan > channel) are returned with *_source=\"task\"; otherwise the channel's values are returned with *_source=\"channel\". The task must belong to the same channel and user, otherwise the call is rejected. Always pass task_id when one exists so template-derived dimensions surface correctly."},
			},
			"required": []any{"channel_id"},
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
				"channel_id": map[string]any{"type": "string", "description": "Filter by channel ID"},
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
		Name:        "list_channel_titles",
		Description: "List all recorded content titles for a channel. Use this before selecting a new title to avoid duplicates within the same channel.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
			},
			"required": []any{"channel_id"},
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
				"channel_id": map[string]any{"type": "string", "description": "Filter by channel ID"},
			},
		},
	}, planListHandler)

	server.AddTool(&mcp.Tool{
		Name:        "create_plan",
		Description: "Create a scheduled content plan that automatically generates tasks on a cron schedule.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"cron_expr":  map[string]any{"type": "string", "description": "Cron expression (e.g. '0 9 * * *' for daily at 9am)"},
				"prompt":     map[string]any{"type": "string", "description": "Optional prompt/instructions for auto-generated content"},
			},
			"required": []any{"channel_id", "cron_expr"},
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

func channelListHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	opts := repository.ChannelListOptions{}
	if v, ok := args["status"].(string); ok {
		opts.Status = v
	}
	if v, ok := args["platform"].(string); ok {
		opts.Platform = v
	}
	channels, err := svcs.ChannelSvc.List(context.Background(), userID, opts)
	if err != nil {
		return errorResult(fmt.Sprintf("list channels: %v", err)), nil
	}
	for _, ch := range channels {
		service.SanitizeChannel(ch)
	}
	return textResult(channels)
}

func channelGetHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}

	ch, stats, err := svcs.ChannelSvc.Get(context.Background(), userID, channelID)
	if err != nil {
		return errorResult(fmt.Sprintf("get channel: %v", err)), nil
	}
	service.SanitizeChannel(ch)
	return textResult(map[string]any{"channel": ch, "stats": stats})
}

func accountInfoHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	info, errMsg := buildAccountInfo(ctx, userID, parseArgs(req.Params.Arguments))
	if errMsg != "" {
		return errorResult(errMsg), nil
	}
	return textResult(info)
}

// buildAccountInfo is the testable core of get_channel_profile. It resolves the
// three orthogonal style dimensions (图片视觉 / 写作风格 / 排版样式). When a task_id
// is supplied and the task belongs to the requesting user+channel, the task is the
// single source of truth (its Style/WritingStyle/Theme are already resolved at
// creation); otherwise the channel's values are used. The three dimensions never
// derive from one another.
func buildAccountInfo(ctx context.Context, userID string, args map[string]any) (map[string]any, string) {
	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return nil, "channel_id is required"
	}
	scope, _ := args["scope"].(string)
	taskID, _ := args["task_id"].(string)

	if svcs.ChannelSvc == nil {
		return nil, "channel service not available"
	}
	ch, _, err := svcs.ChannelSvc.Get(ctx, userID, channelID)
	if err != nil {
		return nil, fmt.Sprintf("get channel: %v", err)
	}
	service.SanitizeChannel(ch)

	// Resolve the three orthogonal style dimensions. Each is independent — the
	// writer never drives the visual style. When a task_id is supplied, the task is
	// the single source of truth: Task.Style / Task.WritingStyle / Task.Theme are
	// already the resolved effective values (precedence task > template > plan >
	// channel, computed at creation). Without a task_id we fall back to the channel.
	effectiveVisual := ch.Style
	effectiveWriter := ch.WritingStyle
	effectiveTheme := ch.Theme
	visualSource := "channel"
	writerSource := "channel"
	themeSource := "channel"
	var taskTemplateID *string
	if taskID != "" {
		if svcs.TaskSvc == nil {
			return nil, "task service not available"
		}
		task, terr := svcs.TaskSvc.GetByID(ctx, taskID)
		if terr != nil {
			return nil, fmt.Sprintf("get task: %v", terr)
		}
		// Guard against cross-channel/cross-user injection.
		if task.UserID != userID || task.ChannelID != channelID {
			return nil, "task does not belong to the requested channel"
		}
		// Task fields hold the resolved effective values (precedence
		// task > template > plan > channel, computed at creation). We still fall
		// back to the channel PER DIMENSION when a task field is empty, and
		// report each dimension's source honestly — so a task that only set its
		// writer doesn't silently clobber the channel's visual/theme.
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
		"author":               ch.Author,
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
	}

	// Add available resource options for the platform.
	info["available_themes"] = resources.Manager().ListByPlatform(resources.CategoryTheme, ch.Platform)
	info["available_writers"] = resources.Manager().ListByPlatform(resources.CategoryWriter, ch.Platform)
	info["available_layouts"] = resources.Manager().ListByPlatform(resources.CategoryLayout, ch.Platform)
	info["available_image_presets"] = resources.Manager().ListByPlatform(resources.CategoryImagePreset, ch.Platform)

	// Descriptions for the RESOLVED theme / writer (not the raw channel fields).
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
	if ch.Layout != "" {
		if e := resources.Manager().Get(resources.CategoryLayout, ch.Layout); e != nil {
			info["layout_description"] = e.Description
		}
	}
	if ch.ImagePreset != "" {
		if e := resources.Manager().Get(resources.CategoryImagePreset, ch.ImagePreset); e != nil {
			info["image_preset_description"] = e.Description
		}
	}

	// Surface the linked template's content. Three things are delivered, kept as
	// STRICTLY INDEPENDENT concepts (the article 作者/byline must never be conflated
	// with 写作风格/writing imitation):
	//
	//   - 作者 (byline): the template's AuthorName overrides the channel byline
	//     (precedence template > channel) and surfaces as the top-level `author`
	//     field, which the agent passes to publish_draft. A template without
	//     AuthorName leaves the channel byline untouched.
	//   - 写作风格 (writing imitation, free text): AuthorStyleIntro for article,
	//     falling back to the writer-key scaffold WritingStyle (poster). Surfaced
	//     as template_writing_style; the skill follows it for 框架/写作方式/笔迹.
	//   - template_author_avatar: the optional 写作风格 persona avatar (part of the
	//     writing persona, NOT the byline).
	//
	// Only present when the task carries a template_id (manual task or spawned
	// from a plan). Errors (template deleted) are logged-and-skipped so a stale
	// template_id never breaks the profile — the keys simply stay absent.
	if taskTemplateID != nil && *taskTemplateID != "" && svcs.TemplateSvc != nil {
		if tmpl, terr := svcs.TemplateSvc.GetByID(ctx, *taskTemplateID); terr == nil {
			info["template_id"] = tmpl.ID
			info["template_name"] = tmpl.Name
			// 作者（署名 byline）: the template AuthorName overrides the channel byline
			// (precedence template > channel). It surfaces as the resolved top-level
			// `author`, which the agent passes to publish_draft.
			if tmpl.AuthorName != "" {
				info["author"] = tmpl.AuthorName
				info["template_author_name"] = tmpl.AuthorName
			}
			// 写作风格（模仿写作）: article inline intro (free-text 框架/写作方式/笔迹),
			// else the writer-key scaffold WritingStyle (poster). Independent of the byline.
			if tmpl.AuthorStyleIntro != "" {
				info["template_writing_style"] = tmpl.AuthorStyleIntro
			} else {
				info["template_writing_style"] = tmpl.WritingStyle
			}
			// 写作风格的可选人设头像（仅作人设参考，不入署名）。
			if tmpl.AuthorAvatarURL != "" {
				info["template_author_avatar"] = tmpl.AuthorAvatarURL
			}
			info["template_theme"] = tmpl.Theme
			info["template_structure"] = extractScaffoldText(tmpl.Structure)
			info["template_example"] = extractScaffoldText(tmpl.ExampleContent)
		} else if mcpLog != nil {
			mcpLog.Warn().Err(terr).Str("template_id", *taskTemplateID).
				Msg("template linked to task not found; skipping content scaffold")
		}
	}

	return info, ""
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
	channelID, _ := args["channel_id"].(string)

	tasks, total, err := svcs.TaskSvc.List(context.Background(), userID, 0, limit, status, channelID)
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
	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}

	if _, _, err := svcs.ChannelSvc.Get(context.Background(), userID, channelID); err != nil {
		return errorResult(fmt.Sprintf("get channel: %v", err)), nil
	}

	titles, err := svcs.TaskSvc.ListTitles(context.Background(), channelID)
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
	channelID, _ := args["channel_id"].(string)

	plans, total, err := svcs.PlanSvc.List(context.Background(), userID, 0, 50, channelID)
	if err != nil {
		return errorResult(fmt.Sprintf("list plans: %v", err)), nil
	}
	return textResult(map[string]any{"items": plans, "total": total})
}

func planCreateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	channelID, _ := args["channel_id"].(string)
	cronExpr, _ := args["cron_expr"].(string)
	prompt, _ := args["prompt"].(string)

	if channelID == "" || cronExpr == "" {
		return errorResult("channel_id and cron_expr are required"), nil
	}

	plan, err := svcs.PlanSvc.Create(context.Background(), service.CreatePlanParams{
		UserID:    userID,
		ChannelID: channelID,
		CronExpr:  cronExpr,
		Prompt:    prompt,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("create plan: %v", err)), nil
	}
	return textResult(plan)
}
