package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerTopicPoolTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "claim_topic",
		Description: "Claim the next unused topic from the channel's topic pool. Returns the topic text and its ID, or null if the pool is empty. The topic is atomically marked as used.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
			},
			"required": []any{"channel_id"},
		},
	}, claimTopicHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_topics",
		Description: "List topics in the channel's topic pool with optional status filter.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"status":     map[string]any{"type": "string", "enum": []any{"unused", "used"}, "description": "Filter by status"},
			},
			"required": []any{"channel_id"},
		},
	}, listTopicsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "add_topic",
		Description: "Add a single topic to the channel's topic pool. Topic text must not exceed 500 characters.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID"},
				"topic":      map[string]any{"type": "string", "description": "Topic text to add (max 500 characters)"},
			},
			"required": []any{"channel_id", "topic"},
		},
	}, addTopicHandler)
}

func claimTopicHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	channelID, _ := args["channel_id"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}

	topic, topicID, err := svcs.TopicPoolSvc.Claim(ctx, userID, channelID)
	if err != nil {
		return errorResult(fmt.Sprintf("claim topic: %v", err)), nil
	}
	if topic == "" {
		return textResult(map[string]any{"topic": nil, "id": nil, "message": "topic pool is empty"})
	}
	return textResult(map[string]any{"topic": topic, "id": topicID, "claimed": true})
}

func listTopicsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	channelID, _ := args["channel_id"].(string)
	status, _ := args["status"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}

	topics, total, err := svcs.TopicPoolSvc.List(ctx, userID, channelID, status, 0, 100)
	if err != nil {
		return errorResult(fmt.Sprintf("list topics: %v", err)), nil
	}
	return textResult(map[string]any{"items": topics, "total": total})
}

func addTopicHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	channelID, _ := args["channel_id"].(string)
	topic, _ := args["topic"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if topic == "" {
		return errorResult("topic is required"), nil
	}

	result, err := svcs.TopicPoolSvc.Add(ctx, userID, channelID, []string{topic})
	if err != nil {
		return errorResult(fmt.Sprintf("add topic: %v", err)), nil
	}
	return textResult(map[string]any{"items": result, "count": len(result)})
}
