package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerContentMetadataTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "submit_completion_metadata",
		Description: "Submit Hook-generated content tags and execution feedback for one task execution.",
		InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"task_id":          map[string]any{"type": "string"},
			"taxonomy_version": map[string]any{"type": "string"}, "source_digest": map[string]any{"type": "string"},
			"metadata": map[string]any{"type": "string", "description": "JSON completion-metadata payload"},
		}, "required": []any{"task_id", "metadata"}},
	}, contentMetadataSubmitHandler)
	server.AddTool(&mcp.Tool{Name: "recompute_content_tags", Description: "Recompute only content tags from the stored Hook snapshot.", InputSchema: recomputeSchema()}, contentMetadataRecomputeTagsHandler)
	server.AddTool(&mcp.Tool{Name: "recompute_agent_feedback", Description: "Recompute only agent feedback from the stored Hook snapshot.", InputSchema: recomputeSchema()}, contentMetadataRecomputeFeedbackHandler)
	server.AddTool(&mcp.Tool{Name: "get_completion_metadata_status", Description: "Get completion metadata status for one task execution.", InputSchema: recomputeSchema()}, contentMetadataStatusHandler)
}

func recomputeSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"task_id":      map[string]any{"type": "string", "description": "Task whose metadata is inspected or recomputed"},
		"execution_id": map[string]any{"type": "string", "description": "Required target execution for user/API-key calls; execution-token calls default to their authenticated current execution and may only specify that same ID"},
	}, "required": []any{"task_id"}}
}

func contentMetadataSubmitHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("submit_completion_metadata request parameters are required"), nil
	}
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	executionID := getExecutionID(ctx)
	metadata, _ := args["metadata"].(string)
	if taskID == "" || metadata == "" {
		return errorResult("task_id and metadata are required"), nil
	}
	if failure := requireMCPExecutionIdentity(ctx, "submit_completion_metadata", "", taskID, ""); failure != nil {
		return failure, nil
	}
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	report, err := svcs.ContentMetadataSvc.Submit(ctx, service.ContentMetadataInput{AuthenticatedUserID: getUserID(ctx), TaskID: taskID, ExecutionID: executionID, TaxonomyVersion: stringArg(args, "taxonomy_version"), SourceDigest: stringArg(args, "source_digest"), RawMetadata: []byte(metadata)})
	if err != nil {
		return errorResult(fmt.Sprintf("submit completion metadata: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "submitted": true})
}

func parseRecomputeArgs(ctx context.Context, req *mcp.CallToolRequest) (string, string, *mcp.CallToolResult) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	requestedExecutionID, _ := args["execution_id"].(string)
	executionID := getExecutionID(ctx)
	if executionID != "" {
		if strings.TrimSpace(requestedExecutionID) != "" && strings.TrimSpace(requestedExecutionID) != executionID {
			return "", "", errorResult("execution-token calls may only target the authenticated current execution")
		}
	} else {
		executionID = strings.TrimSpace(requestedExecutionID)
	}
	if taskID == "" || executionID == "" {
		return "", "", errorResult("task_id and execution_id target are required")
	}
	return taskID, executionID, nil
}

func contentMetadataRecomputeTagsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	taskID, executionID, validation := parseRecomputeArgs(ctx, req)
	if validation != nil {
		return validation, nil
	}
	report, err := svcs.ContentMetadataSvc.RecomputeTags(ctx, getUserID(ctx), taskID, executionID)
	if err != nil {
		return errorResult(fmt.Sprintf("recompute content tags: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "recomputed": true})
}

func contentMetadataRecomputeFeedbackHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	taskID, executionID, validation := parseRecomputeArgs(ctx, req)
	if validation != nil {
		return validation, nil
	}
	report, err := svcs.ContentMetadataSvc.RecomputeFeedback(ctx, getUserID(ctx), taskID, executionID)
	if err != nil {
		return errorResult(fmt.Sprintf("recompute agent feedback: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "recomputed": true})
}

func contentMetadataStatusHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, executionID, validation := parseRecomputeArgs(ctx, req)
	if validation != nil {
		return validation, nil
	}
	if svcs.ContentMetadataSvc == nil {
		return errorResult("content metadata service not available"), nil
	}
	report, err := svcs.ContentMetadataSvc.FindAuthorized(ctx, getUserID(ctx), taskID, executionID)
	if err != nil {
		return errorResult(fmt.Sprintf("get completion metadata status: %v", err)), nil
	}
	return textResult(map[string]any{"id": report.ID, "task_id": report.TaskID, "execution_id": report.ExecutionID, "status": report.Status, "tagging_status": report.TaggingStatus, "feedback_status": report.FeedbackStatus, "attempts": report.Attempts, "error_message": report.ErrorMessage})
}
