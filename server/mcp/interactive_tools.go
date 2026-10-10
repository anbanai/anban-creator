package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerInteractiveTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "stage_project_creator_draft",
		Description: "Stage a validated Project Creator draft from the authenticated interactive runtime.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"project":  map[string]any{"type": "object"},
				"profile":  map[string]any{"type": "object"},
				"progress": map[string]any{"type": "object"},
				"ready":    map[string]any{"type": "boolean"},
				"missing_fields": map[string]any{
					"type": "array", "maxItems": 64,
					"items": map[string]any{"type": "string", "maxLength": 300},
				},
				"inferred_fields": map[string]any{
					"type": "array", "maxItems": 64,
					"items": map[string]any{"type": "string", "maxLength": 300},
				},
			},
			"required": []any{"project", "profile", "progress", "ready", "missing_fields", "inferred_fields"},
		},
	}, stageProjectCreatorDraftHandler)
}

func stageProjectCreatorDraftHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.InteractiveSvc == nil {
		return errorResult("interactive service not available"), nil
	}
	identity, ok := getMCPInteractiveIdentity(ctx)
	if !ok {
		return errorResult("interactive runtime identity is required"), nil
	}
	if req == nil || req.Params == nil {
		return errorResult("project, profile, progress, and ready are required"), nil
	}
	args := parseArgs(req.Params.Arguments)

	var input service.InteractiveStageInput
	if err := decodeInteractiveArgument(args, "project", &input.Project); err != nil {
		return errorResult(fmt.Sprintf("project is invalid: %v", err)), nil
	}
	if err := decodeInteractiveArgument(args, "profile", &input.Profile); err != nil {
		return errorResult(fmt.Sprintf("profile is invalid: %v", err)), nil
	}
	if err := decodeInteractiveArgument(args, "progress", &input.Progress); err != nil {
		return errorResult(fmt.Sprintf("progress is invalid: %v", err)), nil
	}
	ready, ok := args["ready"].(bool)
	if !ok {
		return errorResult("ready must be a boolean"), nil
	}
	input.Ready = ready
	if err := decodeInteractiveArgument(args, "missing_fields", &input.MissingFields); err != nil {
		return errorResult(fmt.Sprintf("missing_fields is invalid: %v", err)), nil
	}
	if err := decodeInteractiveArgument(args, "inferred_fields", &input.InferredFields); err != nil {
		return errorResult(fmt.Sprintf("inferred_fields is invalid: %v", err)), nil
	}

	if err := svcs.InteractiveSvc.StageDraftForSession(identity.SessionID, input); err != nil {
		return errorResult(fmt.Sprintf("stage project creator draft: %v", err)), nil
	}
	return textResult(map[string]any{"staged": true, "session_id": identity.SessionID, "ready": input.Ready})
}

func decodeInteractiveArgument(args map[string]any, key string, target any) error {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fmt.Errorf("%s is required", key)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return err
	}
	return nil
}
