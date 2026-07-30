package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerVideoUnderstandingTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "analyze_video",
		Description: "Analyze one authorized complete video with the configured native video-understanding route. No frame, audio, or text fallback is performed.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"project_id":   map[string]any{"type": "string"},
				"prompt":       map[string]any{"type": "string"},
				"task_id":      map[string]any{"type": "string"},
				"video_url":    map[string]any{"type": "string"},
				"task_file_id": map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "prompt"},
		},
	}, analyzeVideoHandler)
}

func analyzeVideoHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.TaskVideoOperationsSvc == nil {
		return errorResult("video understanding service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	result, err := svcs.TaskVideoOperationsSvc.Analyze(ctx, service.AnalyzeTaskVideoRequest{
		UserID: getUserID(ctx), ProjectID: stringArg(args, "project_id"), TaskID: stringArg(args, "task_id"),
		TaskFileID: stringArg(args, "task_file_id"), VideoURL: stringArg(args, "video_url"), Prompt: stringArg(args, "prompt"),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}
