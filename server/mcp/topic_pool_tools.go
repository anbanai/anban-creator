package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerTopicPoolTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "claim_topic",
		Description: "Claim the next unused topic from the project's topic pool. Returns the topic text and its ID, or null if the pool is empty. The topic is atomically marked as used. Pass task_id to associate the claimed topic with the calling task for provenance.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"task_id":    map[string]any{"type": "string", "description": "Optional task ID — when set, the claimed topic is linked to this task for provenance."},
			},
			"required": []any{"project_id"},
		},
	}, claimTopicHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_topics",
		Description: "List topics in the project's topic pool with optional status filter.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"status":     map[string]any{"type": "string", "enum": []any{"unused", "used"}, "description": "Filter by status"},
			},
			"required": []any{"project_id"},
		},
	}, listTopicsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "add_topic",
		Description: "Add a single topic to the project's topic pool. Topic text must not exceed 500 characters.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID"},
				"topic":      map[string]any{"type": "string", "description": "Topic text to add (max 500 characters)"},
			},
			"required": []any{"project_id", "topic"},
		},
	}, addTopicHandler)
}

func claimTopicHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	taskID, _ := args["task_id"].(string)

	result, err := svcs.TopicPoolSvc.ClaimTopic(ctx, service.ClaimTopicRequest{
		UserID: userID, ProjectID: projectID, TaskID: taskID,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("claim topic: %v", err)), nil
	}
	if result.Topic == "" {
		payload := map[string]any{"topic": nil, "id": nil, "message": "topic pool is empty"}
		if result.TaskID != "" {
			payload["task_id"] = result.TaskID
		}
		return textResult(payload)
	}
	payload := map[string]any{"topic": result.Topic, "id": result.TopicID, "claimed": true}
	if result.TaskID != "" {
		payload["id"] = nil
		payload["task_id"] = result.TaskID
	}
	return textResult(payload)
}

func listTopicsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	status, _ := args["status"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}

	topics, total, err := svcs.TopicPoolSvc.List(ctx, userID, projectID, status, 0, 100)
	if err != nil {
		return errorResult(fmt.Sprintf("list topics: %v", err)), nil
	}
	return textResult(map[string]any{"items": topics, "total": total})
}

func addTopicHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	topic, _ := args["topic"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if topic == "" {
		return errorResult("topic is required"), nil
	}

	result, err := svcs.TopicPoolSvc.Add(ctx, userID, projectID, []string{topic})
	if err != nil {
		return errorResult(fmt.Sprintf("add topic: %v", err)), nil
	}
	return textResult(map[string]any{"items": result, "count": len(result)})
}
