package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

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
	ProjectSvc             *service.ProjectService
	Store                  storage.Provider
	TaskSvc                *service.TaskService
	PlanSvc                *service.PlanService
	ImageSvc               *service.ImageService
	ImageModelResolver     ImageModelResolver
	ImageGenerator         ImageGenerator
	ProviderCostSvc        *service.ProviderCostService
	BillingCatalogSvc      *service.BillingCatalogService
	GenerateImageTimeout   time.Duration
	WritingSvc             *service.WritingService
	PublishingSvc          *service.PublishingService
	WorkspaceSvc           *service.WorkspaceService
	TemplateSvc            *service.TemplateService
	LiveSliceSvc           *service.LiveSliceService
	SeednoteClient         *seednote.Client
	SeednoteReadiness      service.Readiness
	TopicPoolSvc           *service.TopicPoolService
	AgentFeedbackSvc       *service.AgentFeedbackService
	AgentProjectProfileSvc *service.AgentProjectProfileService
	ArticleScoreSvc        *service.ArticleScoreService
	SeednoteExportSvc      *service.SeednoteExportService
	ResourceCatalogSvc     *service.ResourceCatalogService
	TaskImageSvc           *service.TaskImageService
	TingWuConfigured       bool
}

// RegisterTools registers all MCP tools on the server.
func RegisterTools(server *mcp.Server) {
	registerProjectTools(server)
	registerTaskTools(server)
	registerPlanTools(server)
	registerImageTools(server)
	registerWritingTools(server)
	registerPublishingTools(server)
	registerWorkspaceTools(server)
	registerSeednoteFormatTools(server)
	registerTemplateTools(server)
	registerResourceTools(server)
	registerSeednoteTools(server)
	registerMediaPipelineTools(server)
	registerFileUploadTools(server)
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
				"platform": map[string]any{"type": "string", "enum": []any{"article", "seednote", "moments", "ecommerce", "montage"}, "description": "Filter by platform type"},
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
		Description: "Get the resolved project runtime profile for AI content generation. The server applies task snapshots, sanitizes secrets, resolves style/theme/author dimensions, and returns platform-specific blocks such as ecommerce or montage. When task_id is provided, the task's frozen project_snapshot is used; old rows without a snapshot fall back to legacy task overrides/project resolution. Does NOT expose credentials.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"scope":      map[string]any{"type": "string", "enum": []any{"article", "seednote", "moments", "ecommerce", "montage"}, "description": "Legacy output hint. New agents should omit this and let the server return the platform-specific block automatically."},
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
		Description: "List terminal task files owned by the authenticated user. Returns the latest successful published deliverables followed by collected files retained from failed attempts, including names, roles, states, sizes, and download URLs. This is a post-run inspection and recovery query, not a live workspace listing or upload-completion check; pending and superseded files are excluded.",
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
	if svcs == nil || svcs.AgentProjectProfileSvc == nil {
		return errorResult("agent project profile service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	profile, err := svcs.AgentProjectProfileSvc.Get(ctx, service.AgentProjectProfileRequest{
		UserID: getUserID(ctx), ProjectID: stringArg(args, "project_id"),
		TaskID: stringArg(args, "task_id"), Scope: stringArg(args, "scope"),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(profile)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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
