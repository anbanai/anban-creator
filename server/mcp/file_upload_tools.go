package mcp

import (
	"context"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerFileUploadTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "prepare_file_upload",
		Description: "Create an OSS direct-upload target for live-slicer audio. Returns the object key, signed PUT URL, download URL, method, headers, and expiry.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"purpose":         map[string]any{"type": "string", "enum": []any{"live_audio"}},
				"filename":        map[string]any{"type": "string", "description": "Original local filename, used to preserve its extension"},
				"content_type":    map[string]any{"type": "string", "description": "Audio MIME type, for example audio/mpeg"},
				"expires_seconds": map[string]any{"type": "integer", "description": "Signed URL TTL in seconds", "default": 86400},
			},
			"required": []any{"purpose", "filename", "content_type"},
		},
	}, prepareFileUploadHandler)
}

func prepareFileUploadHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.FileUploadSvc == nil {
		return errorResult("storage provider is not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	purpose, _ := args["purpose"].(string)
	filename, _ := args["filename"].(string)
	contentType, _ := args["content_type"].(string)
	expires := intFromArg(args["expires_seconds"], 0)
	result, err := svcs.FileUploadSvc.Prepare(ctx, service.FileUploadPrepareRequest{
		Purpose:        purpose,
		Filename:       filename,
		ContentType:    contentType,
		ExpiresSeconds: expires,
	})
	if err != nil {
		return errorResult("prepare file upload: " + err.Error()), nil
	}
	return textResult(result)
}
