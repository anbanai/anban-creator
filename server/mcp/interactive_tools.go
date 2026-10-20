package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerInteractiveTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "stage_assistant_draft",
		Description: "Stage a structured creation plan or postmortem review draft for the authenticated Assistant session. The Server selects the schema from the session mode.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "maxLength": 200},
				"payload": map[string]any{"type": "object", "additionalProperties": true},
				"ready":   map[string]any{"type": "boolean"},
			},
			"required": []any{"payload", "ready"},
		},
	}, stageAssistantDraftHandler)
	server.AddTool(&mcp.Tool{
		Name:        "stage_project_creator_draft",
		Description: "Stage a validated Project Creator draft from the authenticated interactive runtime.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"project":  projectCreatorProjectSchema(),
				"profile":  projectCreatorProfileSchema(),
				"progress": projectCreatorProgressSchema(),
				"ready":    map[string]any{"type": "boolean"},
				"missing_fields": map[string]any{
					"type": "array", "maxItems": 64,
					"items": map[string]any{"type": "string", "maxLength": 300},
				},
				"inferred_fields": map[string]any{
					"type": "array", "maxItems": 64,
					"items": map[string]any{"type": "string", "maxLength": 300},
				},
				"memory_append": map[string]any{"type": "string", "maxLength": 65536},
			},
			"required": []any{"project", "profile", "progress", "ready", "missing_fields", "inferred_fields"},
		},
	}, stageProjectCreatorDraftHandler)
}

func stageAssistantDraftHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.InteractiveSvc == nil {
		return errorResult("interactive service not available"), nil
	}
	identity, ok := getMCPInteractiveIdentity(ctx)
	if !ok {
		return errorResult("interactive runtime identity is required"), nil
	}
	if req == nil || req.Params == nil {
		return errorResult("payload and ready are required"), nil
	}
	args := parseArgs(req.Params.Arguments)
	// Mode and Agent selection are Server-owned. Prefer this explicit error
	// when a caller attempts to smuggle mode metadata alongside other fields so
	// the contract remains deterministic despite Go map iteration order.
	if _, exists := args["mode_id"]; exists {
		return errorResult(`unknown assistant draft field "mode_id"`), nil
	}
	for key := range args {
		if key != "name" && key != "payload" && key != "ready" {
			return errorResult(fmt.Sprintf("unknown assistant draft field %q", key)), nil
		}
	}
	var input service.AssistantDraftStageInput
	if raw, exists := args["name"]; exists && raw != nil {
		if err := decodeInteractiveArgumentStrict(args, "name", &input.Name); err != nil {
			return errorResult(fmt.Sprintf("name is invalid: %v", err)), nil
		}
	}
	if err := decodeInteractiveArgumentStrict(args, "payload", &input.Payload); err != nil {
		return errorResult(fmt.Sprintf("payload is invalid: %v", err)), nil
	}
	if err := decodeInteractiveArgumentStrict(args, "ready", &input.Ready); err != nil {
		return errorResult(fmt.Sprintf("ready is invalid: %v", err)), nil
	}
	if err := svcs.InteractiveSvc.StageAssistantDraftForSessionContext(ctx, identity.SessionID, input); err != nil {
		return errorResult(fmt.Sprintf("stage assistant draft: %v", err)), nil
	}
	return textResult(map[string]any{"staged": true, "session_id": identity.SessionID, "ready": input.Ready})
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
	if err := decodeInteractiveArgumentStrict(args, "profile", &input.Profile); err != nil {
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
	if raw, exists := args["memory_append"]; exists && raw != nil {
		if err := decodeInteractiveArgument(args, "memory_append", &input.MemoryAppend); err != nil {
			return errorResult(fmt.Sprintf("memory_append is invalid: %v", err)), nil
		}
	}

	if err := svcs.InteractiveSvc.StageDraftForSessionContext(ctx, identity.SessionID, input); err != nil {
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

func decodeInteractiveArgumentStrict(args map[string]any, key string, target any) error {
	raw, ok := args[key]
	if !ok || raw == nil {
		return fmt.Errorf("%s is required", key)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func projectCreatorProfileSchema() map[string]any {
	dimension := map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"content", "sources", "evidence", "missing_fields"},
		"properties": map[string]any{
			"content":        map[string]any{"type": "object"},
			"sources":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"evidence":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"missing_fields": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
	return map[string]any{
		"type": "object", "additionalProperties": false,
		"required": []any{"schema_version", "status", "initialization_status", "dimensions"},
		"properties": map[string]any{
			"schema_version":        map[string]any{"type": "integer", "const": 1},
			"status":                map[string]any{"type": "string", "enum": []any{"draft", "confirmed"}},
			"initialization_status": map[string]any{"type": "string", "enum": []any{"not_started", "queued", "running", "ready", "failed"}},
			"last_error":            map[string]any{"type": "string"}, "version": map[string]any{"type": "integer"},
			"application_source": map[string]any{"type": "string", "enum": []any{"", "system_analysis", "user_confirmation", "user_edit", "user_restore"}}, "analysis_task_id": map[string]any{"type": "string"},
			"dimensions": map[string]any{
				"type": "object", "additionalProperties": false,
				"required":   []any{"identity", "style", "audience", "platforms", "preferences", "memory"},
				"properties": map[string]any{"identity": dimension, "style": dimension, "audience": dimension, "platforms": dimension, "preferences": dimension, "memory": dimension},
			},
			"analysis_limits":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"follow_up_questions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
	}
}

func projectCreatorProgressSchema() map[string]any {
	status := map[string]any{"type": "string", "enum": []any{"not_started", "collected", "pending_confirmation", "confirmed"}}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"stage": map[string]any{"type": "string"}, "collected_dimensions": map[string]any{"type": "string"},
		"current_dimension": map[string]any{"type": "string"}, "notes": map[string]any{"type": "string"},
		"identity": status, "style": status, "audience": status, "platforms": status,
		"preferences": status, "memory": status,
	}, "required": []any{"identity", "style", "audience", "platforms", "preferences", "memory"}}
}

func projectCreatorProjectSchema() map[string]any {
	return map[string]any{"type": "object", "additionalProperties": false, "required": []any{"name", "platform"}, "properties": map[string]any{
		"name":         map[string]any{"type": "string", "maxLength": 100},
		"platform":     map[string]any{"type": "string", "enum": []any{"wechat", "seednote", "moments", "ecommerce", "montage", "whiteboard-animation", "hypit"}},
		"instructions": map[string]any{"type": "string", "maxLength": 20000},
		"keywords":     map[string]any{"type": "string", "maxLength": 10000},
		"visual_style": map[string]any{"type": "string", "maxLength": 10000},
	}}
}
