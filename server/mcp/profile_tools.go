package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerProfileTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "submit_profile_result",
		Description: "Submit one validated structured six-dimensional project profile result. The Server persists the revision and generates project memory files; the Agent must not write persistent profile files.",
		InputSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
			"project_id": map[string]any{"type": "string"}, "task_id": map[string]any{"type": "string"},
			"expected_revision": map[string]any{"type": "integer", "minimum": 0},
			"dimensions":        map[string]any{"type": "object"},
			"analysis_limits":   map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"missing_fields":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		}, "required": []any{"project_id", "task_id", "expected_revision", "dimensions"}},
	}, submitProfileResultHandler)
}

func submitProfileResultHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	taskID, _ := args["task_id"].(string)
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(taskID) == "" {
		return errorResult("project_id and task_id are required"), nil
	}
	if failure := requireMCPExecutionIdentity(ctx, "submit_profile_result", projectID, taskID, ""); failure != nil {
		return failure, nil
	}
	if svcs.ProjectSvc == nil {
		return errorResult("project profile service not available"), nil
	}
	expected, ok := args["expected_revision"].(float64)
	if !ok || expected < 0 || expected != float64(int64(expected)) {
		return errorResult("expected_revision must be a non-negative integer"), nil
	}
	rawDimensions, ok := args["dimensions"].(map[string]any)
	if !ok {
		return errorResult("dimensions must be an object"), nil
	}
	want := map[string]bool{"identity": true, "style": true, "audience": true, "platforms": true, "preferences": true, "memory": true}
	if len(rawDimensions) != len(want) {
		return errorResult("dimensions must contain exactly the six required dimensions"), nil
	}
	for key := range rawDimensions {
		if !want[key] {
			return errorResult("dimensions contain an unsupported dimension"), nil
		}
	}
	dimensionsBytes, err := json.Marshal(rawDimensions)
	if err != nil {
		return errorResult("dimensions are invalid"), nil
	}
	var dimensions model.ProjectProfileDimensions
	if err := json.Unmarshal(dimensionsBytes, &dimensions); err != nil {
		return errorResult(fmt.Sprintf("dimensions are invalid: %v", err)), nil
	}
	limits := parseStringArray(args, "analysis_limits")
	missing := parseStringArray(args, "missing_fields")
	profile, err := svcs.ProjectSvc.ApplyAgentProfileResult(ctx, getUserID(ctx), projectID, taskID, int64(expected), dimensions, limits, missing)
	if err != nil {
		return errorResult(fmt.Sprintf("submit profile result: %v", err)), nil
	}
	return textResult(map[string]any{"submitted": true, "project_id": projectID, "task_id": taskID, "revision": profile.Version, "status": profile.InitializationStatus})
}
