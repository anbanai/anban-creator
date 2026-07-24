package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerMediaPipelineTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "get_media_pipeline_status",
		Description: "Return safe readiness diagnostics for media upload and live-slice TingWu analysis. Does not return secrets or signed URLs.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, getMediaPipelineStatusHandler)
}

func getMediaPipelineStatusHandler(ctx context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.MediaPipelineSvc == nil {
		return errorResult("media pipeline service not available"), nil
	}
	return textResult(svcs.MediaPipelineSvc.Status(ctx, service.MediaPipelineStatusRequest{}))
}
