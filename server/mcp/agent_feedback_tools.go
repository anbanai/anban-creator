package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerAgentFeedbackTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "submit_agent_feedback",
		Description: "Submit post-execution feedback from an agent including scores, errors, and optimization suggestions.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":       map[string]any{"type": "string", "description": "Task ID"},
				"agent_name":    map[string]any{"type": "string", "description": "Agent name (designer, ecommerce, live-slicer, moments, montage, seednote, videocreator, videoeditor, or wechatarticle)"},
				"scores":        map[string]any{"type": "string", "description": "JSON object with score dimensions, e.g. {\"quality\":8,\"completeness\":9,\"efficiency\":7}"},
				"errors":        map[string]any{"type": "string", "description": "Errors encountered during execution (optional)"},
				"optimizations": map[string]any{"type": "string", "description": "Optimization suggestions for future runs (optional)"},
				"summary":       map[string]any{"type": "string", "description": "Brief execution summary (optional)"},
			},
			"required": []any{"task_id", "agent_name"},
		},
	}, agentFeedbackSubmitHandler)
}

func agentFeedbackSubmitHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	agentName, _ := args["agent_name"].(string)
	scores, _ := args["scores"].(string)
	errors, _ := args["errors"].(string)
	optimizations, _ := args["optimizations"].(string)
	summary, _ := args["summary"].(string)

	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if agentName == "" {
		return errorResult("agent_name is required"), nil
	}

	if svcs.AgentFeedbackSvc == nil {
		return errorResult("agent feedback service not available"), nil
	}

	feedback, err := svcs.AgentFeedbackSvc.Create(ctx, taskID, agentName, scores, errors, optimizations, summary)
	if err != nil {
		return errorResult(fmt.Sprintf("submit feedback: %v", err)), nil
	}
	return textResult(map[string]any{"id": feedback.ID, "task_id": taskID, "submitted": true})
}
