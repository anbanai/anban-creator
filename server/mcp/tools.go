package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

// Services holds the service instances needed by MCP tools.
type Services struct {
	ChannelSvc *service.ChannelService
	TaskSvc    *service.TaskService
	CreditSvc  *service.CreditService
	PlanSvc    *service.PlanService
}

// RegisterTools registers all MCP tools on the handler.
func RegisterTools(h *Handler, svcs *Services) {
	registerChannelTools(h, svcs)
	registerTaskTools(h, svcs)
	registerCreditTools(h, svcs)
	registerPlanTools(h, svcs)
}

func registerChannelTools(h *Handler, svcs *Services) {
	h.RegisterTool(Tool{
		Name:        "list_channels",
		Description: "List the authenticated user's channels (WeChat accounts). Each channel represents a WeChat Official Account or Xiaohongshu account.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":   map[string]any{"type": "string", "enum": []any{"active", "archived"}, "description": "Filter by status"},
				"platform": map[string]any{"type": "string", "enum": []any{"article", "xls", "rednote"}, "description": "Filter by platform type"},
			},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			opts := repository.ChannelListOptions{}
			if v, ok := args["status"].(string); ok {
				opts.Status = v
			}
			if v, ok := args["platform"].(string); ok {
				opts.Platform = v
			}
			channels, err := svcs.ChannelSvc.List(newCtx(), userID, opts)
			if err != nil {
				return nil, fmt.Errorf("list channels: %w", err)
			}
			for _, ch := range channels {
				service.SanitizeChannel(ch)
			}
			return channels, nil
		},
	})

	h.RegisterTool(Tool{
		Name:        "get_channel",
		Description: "Get details of a specific channel by ID, including its configuration (WeChat AppID, positioning, style, theme, etc.).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
			},
			"required": []any{"channel_id"},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			channelID, _ := args["channel_id"].(string)
			if channelID == "" {
				return nil, fmt.Errorf("channel_id is required")
			}
			ch, stats, err := svcs.ChannelSvc.Get(newCtx(), userID, channelID)
			if err != nil {
				return nil, fmt.Errorf("get channel: %w", err)
			}
			service.SanitizeChannel(ch)
			return map[string]any{"channel": ch, "stats": stats}, nil
		},
	})
}

func registerTaskTools(h *Handler, svcs *Services) {
	h.RegisterTool(Tool{
		Name:        "create_task",
		Description: "Create one or more content creation tasks for a channel. The task type is derived from the channel's platform (article/xls/rednote). Credits are deducted automatically.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID to create task for"},
				"topic":      map[string]any{"type": "string", "description": "Content topic or title"},
				"quantity":   map[string]any{"type": "integer", "description": "Number of tasks (1-5, default 1)", "minimum": 1, "maximum": 5, "default": 1},
			},
			"required": []any{"channel_id", "topic"},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			channelID, _ := args["channel_id"].(string)
			topic, _ := args["topic"].(string)
			if channelID == "" {
				return nil, fmt.Errorf("channel_id is required")
			}
			if topic == "" {
				return nil, fmt.Errorf("topic is required")
			}
			quantity := 1
			if v, ok := args["quantity"].(float64); ok && int(v) > 0 {
				quantity = int(v)
			}

			tasks, err := svcs.TaskSvc.CreateManual(newCtx(), userID, channelID, topic, quantity)
			if err != nil {
				return nil, fmt.Errorf("create task: %w", err)
			}

			ids := make([]string, len(tasks))
			for i, t := range tasks {
				ids[i] = t.ID
			}
			return map[string]any{
				"task_ids": ids,
				"count":    len(tasks),
				"status":   "pending",
				"message":  fmt.Sprintf("Created %d task(s). Tasks will execute asynchronously.", len(tasks)),
			}, nil
		},
	})

	h.RegisterTool(Tool{
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
		CallFunc: func(userID string, args map[string]any) (any, error) {
			limit := 20
			if v, ok := args["limit"].(float64); ok && int(v) > 0 && int(v) <= 100 {
				limit = int(v)
			}
			status, _ := args["status"].(string)
			channelID, _ := args["channel_id"].(string)

			tasks, total, err := svcs.TaskSvc.List(newCtx(), userID, 0, limit, status, channelID)
			if err != nil {
				return nil, fmt.Errorf("list tasks: %w", err)
			}
			return map[string]any{
				"items": tasks,
				"total": total,
			}, nil
		},
	})

	h.RegisterTool(Tool{
		Name:        "get_task",
		Description: "Get detailed information about a specific task including status, progress log, and result.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
			},
			"required": []any{"task_id"},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			taskID, _ := args["task_id"].(string)
			if taskID == "" {
				return nil, fmt.Errorf("task_id is required")
			}
			task, err := svcs.TaskSvc.GetByID(newCtx(), taskID)
			if err != nil {
				return nil, fmt.Errorf("get task: %w", err)
			}
			// Parse result JSON if present.
			var result any
			if task.Result != nil && *task.Result != "" {
				_ = json.Unmarshal([]byte(*task.Result), &result)
			}
			return map[string]any{
				"id":            task.ID,
				"type":          task.Type,
				"status":        task.Status,
				"topic":         task.Topic,
				"progress_log":  task.ProgressLog,
				"result":        result,
				"error_message": task.ErrorMessage,
				"retry_count":   task.RetryCount,
				"created_at":    task.CreatedAt,
				"started_at":    task.StartedAt,
				"completed_at":  task.CompletedAt,
			}, nil
		},
	})

	h.RegisterTool(Tool{
		Name:        "cancel_task",
		Description: "Cancel a pending or running task.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID to cancel"},
			},
			"required": []any{"task_id"},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			taskID, _ := args["task_id"].(string)
			if taskID == "" {
				return nil, fmt.Errorf("task_id is required")
			}
			if err := svcs.TaskSvc.Cancel(newCtx(), taskID); err != nil {
				return nil, fmt.Errorf("cancel task: %w", err)
			}
			return map[string]any{"cancelled": true, "task_id": taskID}, nil
		},
	})

	h.RegisterTool(Tool{
		Name:        "get_task_files",
		Description: "List output files for a completed task. Returns file names, roles (cover, html, markdown, image), and sizes.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Task ID"},
			},
			"required": []any{"task_id"},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			taskID, _ := args["task_id"].(string)
			if taskID == "" {
				return nil, fmt.Errorf("task_id is required")
			}
			files, err := svcs.TaskSvc.GetFiles(newCtx(), taskID)
			if err != nil {
				return nil, fmt.Errorf("get task files: %w", err)
			}
			return map[string]any{"files": files, "count": len(files)}, nil
		},
	})
}

func registerCreditTools(h *Handler, svcs *Services) {
	h.RegisterTool(Tool{
		Name:        "get_credits",
		Description: "Get the authenticated user's current credit balance.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			balance, err := svcs.CreditSvc.GetBalance(newCtx(), userID)
			if err != nil {
				return nil, fmt.Errorf("get credits: %w", err)
			}
			return map[string]any{"balance": balance}, nil
		},
	})
}

func registerPlanTools(h *Handler, svcs *Services) {
	h.RegisterTool(Tool{
		Name:        "list_plans",
		Description: "List scheduled content plans for the authenticated user.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Filter by channel ID"},
			},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			channelID, _ := args["channel_id"].(string)
			plans, total, err := svcs.PlanSvc.List(newCtx(), userID, 0, 50, channelID)
			if err != nil {
				return nil, fmt.Errorf("list plans: %w", err)
			}
			return map[string]any{"items": plans, "total": total}, nil
		},
	})

	h.RegisterTool(Tool{
		Name:        "create_plan",
		Description: "Create a scheduled content plan that automatically generates tasks on a cron schedule.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"title":      map[string]any{"type": "string", "description": "Plan title"},
				"cron_expr":  map[string]any{"type": "string", "description": "Cron expression (e.g. '0 9 * * *' for daily at 9am)"},
				"topic_hint": map[string]any{"type": "string", "description": "Topic hint for auto-generated content"},
			},
			"required": []any{"channel_id", "title", "cron_expr"},
		},
		CallFunc: func(userID string, args map[string]any) (any, error) {
			channelID, _ := args["channel_id"].(string)
			title, _ := args["title"].(string)
			cronExpr, _ := args["cron_expr"].(string)
			topicHint, _ := args["topic_hint"].(string)

			if channelID == "" || title == "" || cronExpr == "" {
				return nil, fmt.Errorf("channel_id, title, and cron_expr are required")
			}

			plan, err := svcs.PlanSvc.Create(newCtx(), userID, channelID, title, "", cronExpr, topicHint)
			if err != nil {
				return nil, fmt.Errorf("create plan: %w", err)
			}
			return plan, nil
		},
	})
}

// newCtx returns a background context for service calls.
func newCtx() context.Context {
	return context.Background()
}
