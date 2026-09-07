package mcp

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerContentMetadataTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "submit_completion_metadata",
		Description: "Submit Hook-generated content tags and execution feedback for one task execution.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"task_id": map[string]any{"type": "string"}, "execution_id": map[string]any{"type": "string"},
			"taxonomy_version": map[string]any{"type": "string"}, "source_digest": map[string]any{"type": "string"},
			"metadata": map[string]any{"type": "string", "description": "JSON completion-metadata payload"},
		}, "required": []any{"task_id", "execution_id", "metadata"}},
	}, contentMetadataSubmitHandler)
	server.AddTool(&mcp.Tool{Name: "recompute_content_tags", Description: "Recompute only content tags from the stored Hook snapshot.", InputSchema: recomputeSchema()}, contentMetadataRecomputeTagsHandler)
	server.AddTool(&mcp.Tool{Name: "recompute_agent_feedback", Description: "Recompute only agent feedback from the stored Hook snapshot.", InputSchema: recomputeSchema()}, contentMetadataRecomputeFeedbackHandler)
	server.AddTool(&mcp.Tool{Name: "get_completion_metadata_status", Description: "Get completion metadata status for one task execution.", InputSchema: recomputeSchema()}, contentMetadataStatusHandler)
}

func recomputeSchema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"task_id": map[string]any{"type": "string"}, "execution_id": map[string]any{"type": "string"}}, "required": []any{"task_id", "execution_id"}}
}

func contentMetadataSubmitHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	executionID, _ := args["execution_id"].(string)
	metadata, _ := args["metadata"].(string)
	if taskID == "" || executionID == "" || metadata == "" {
		return errorResult("task_id, execution_id and metadata are required"), nil
	}
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	report, err := svcs.ContentMetadataSvc.Submit(ctx, service.ContentMetadataInput{TaskID: taskID, ExecutionID: executionID, TaxonomyVersion: stringArg(args, "taxonomy_version"), SourceDigest: stringArg(args, "source_digest"), RawMetadata: []byte(metadata)})
	if err != nil {
		return errorResult(fmt.Sprintf("submit completion metadata: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "submitted": true})
}

func parseRecomputeArgs(req *mcp.CallToolRequest) (string, string, *mcp.CallToolResult) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	executionID, _ := args["execution_id"].(string)
	if taskID == "" || executionID == "" {
		return "", "", errorResult("task_id and execution_id are required")
	}
	return taskID, executionID, nil
}

func contentMetadataRecomputeTagsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	taskID, executionID, validation := parseRecomputeArgs(req)
	if validation != nil {
		return validation, nil
	}
	report, err := svcs.ContentMetadataSvc.RecomputeTags(ctx, taskID, executionID)
	if err != nil {
		return errorResult(fmt.Sprintf("recompute content tags: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "recomputed": true})
}

func contentMetadataRecomputeFeedbackHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	taskID, executionID, validation := parseRecomputeArgs(req)
	if validation != nil {
		return validation, nil
	}
	report, err := svcs.ContentMetadataSvc.RecomputeFeedback(ctx, taskID, executionID)
	if err != nil {
		return errorResult(fmt.Sprintf("recompute agent feedback: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "recomputed": true})
}

func contentMetadataStatusHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	executionID, _ := args["execution_id"].(string)
	if taskID == "" || executionID == "" {
		return errorResult("task_id and execution_id are required"), nil
	}
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	report, err := svcs.ContentMetadataSvc.Find(ctx, taskID, executionID)
	if err != nil {
		return errorResult(fmt.Sprintf("get completion metadata status: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "tagging_status": report.TaggingStatus, "feedback_status": report.FeedbackStatus, "attempts": report.Attempts, "error_message": report.ErrorMessage})
}
