package mcp

import (
	"context"
	"encoding/json"
	"strings"

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
				"project_id":      map[string]any{"type": "string", "description": "Owning project ID"},
				"task_id":         map[string]any{"type": "string", "description": "Owning Anban task ID"},
				"purpose":         map[string]any{"type": "string", "enum": []any{"live_audio"}},
				"filename":        map[string]any{"type": "string", "description": "Original local filename, used to preserve its extension"},
				"content_type":    map[string]any{"type": "string", "description": "Audio MIME type, for example audio/mpeg"},
				"size":            map[string]any{"type": "integer", "description": "Exact audio size in bytes; execution uploads are bounded"},
				"expires_seconds": map[string]any{"type": "integer", "description": "Signed URL TTL in seconds; cannot exceed the execution credential lifetime", "default": 7200, "maximum": 7200},
			},
			"required": []any{"project_id", "task_id", "purpose", "filename", "content_type", "size"},
		},
	}, prepareFileUploadHandler)
}

func int64FromArg(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i
		}
	}
	return 0
}

func prepareFileUploadHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.FileUploadSvc == nil {
		return errorResult("storage provider is not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	identity, ok := getMCPExecutionIdentity(ctx)
	if !ok {
		return errorResult("prepare_file_upload requires an execution-scoped credential"), nil
	}
	projectID, _ := args["project_id"].(string)
	taskID, _ := args["task_id"].(string)
	if strings.TrimSpace(projectID) != identity.ProjectID || strings.TrimSpace(taskID) != identity.TaskID {
		return errorResult("prepare_file_upload is bound to the current task execution"), nil
	}
	purpose, _ := args["purpose"].(string)
	filename, _ := args["filename"].(string)
	contentType, _ := args["content_type"].(string)
	expires := intFromArg(args["expires_seconds"], 0)
	request := service.FileUploadPrepareRequest{
		Purpose:        purpose,
		Filename:       filename,
		ContentType:    contentType,
		Size:           int64FromArg(args["size"]),
		ExpiresSeconds: expires,
	}
	request.UserID, request.ProjectID, request.TaskID = identity.UserID, identity.ProjectID, identity.TaskID
	result, err := svcs.FileUploadSvc.PrepareForExecution(ctx, identity.UserID, identity.ProjectID, identity.TaskID, request)
	if err != nil {
		return errorResult("prepare file upload: " + err.Error()), nil
	}
	return textResult(result)
}
