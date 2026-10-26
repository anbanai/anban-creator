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
		Description: "Stage a structured creation plan or postmortem review draft for the authenticated Assistant session. For creation, send payload.plan with only the documented fields: goal, content_type, expected_result, requirements, constraints, assets, missing_inputs, agent_decisions, target_audience, angle, outline (string array), style_constraints, asset_plan, acceptance_criteria, cost_estimate, execution_options, and optional image/video fields. Keep all metadata inside plan; do not invent nested objects or fields.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"name":           map[string]any{"type": "string", "maxLength": 200},
				"payload":        assistantDraftPayloadSchema(),
				"ready":          map[string]any{"type": "boolean"},
				"interaction_id": map[string]any{"type": "string", "minLength": 1},
				"generation":     map[string]any{"type": "integer", "minimum": 1},
				"attempt_id":     map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
				"mode_id":        map[string]any{"type": "string", "enum": []any{"profile", "creation", "review"}},
				"payload_digest": map[string]any{"type": "string", "minLength": 64, "maxLength": 64},
			},
			"required": []any{"interaction_id", "generation", "attempt_id", "mode_id", "payload", "payload_digest", "ready"},
		},
	}, stageAssistantDraftHandler)
	server.AddTool(&mcp.Tool{
		Name:        "stage_project_creator_draft",
		Description: "Stage a validated Project Creator draft from the authenticated interactive runtime.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"interaction_id": map[string]any{"type": "string", "minLength": 1},
				"generation":     map[string]any{"type": "integer", "minimum": 1},
				"attempt_id":     map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
				"mode_id":        map[string]any{"type": "string", "enum": []any{"profile", "creation", "review"}},
				"payload_digest": map[string]any{"type": "string", "minLength": 64, "maxLength": 64},
				"payload":        map[string]any{"type": "object"},
				"project":        projectCreatorProjectSchema(),
				"profile":        projectCreatorProfileSchema(),
				"progress":       projectCreatorProgressSchema(),
				"ready":          map[string]any{"type": "boolean"},
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
			"required": []any{"interaction_id", "generation", "attempt_id", "mode_id", "payload_digest", "project", "profile", "progress", "ready", "missing_fields", "inferred_fields"},
		},
	}, stageProjectCreatorDraftHandler)
}

func assistantDraftPayloadSchema() map[string]any {
	stringArray := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	asset := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"type": map[string]any{"type": "string"}, "asset_id": map[string]any{"type": "string"},
		"upload_id": map[string]any{"type": "string"}, "url": map[string]any{"type": "string"},
		"task_file_id": map[string]any{"type": "string"}, "text": map[string]any{"type": "string"},
		"file_name": map[string]any{"type": "string"}, "mime_type": map[string]any{"type": "string"},
		"file_size": map[string]any{"type": "integer", "minimum": 0}, "instruction": map[string]any{"type": "string"},
	}}
	plan := map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"goal": map[string]any{"type": "string"}, "content_type": map[string]any{"type": "string"},
		"execution_profile": map[string]any{"type": "string"}, "image_ratio": map[string]any{"type": "string"},
		"image_capability_key": map[string]any{"type": "string"}, "expected_result": map[string]any{"type": "string"},
		"requirements": stringArray, "constraints": stringArray, "assets": map[string]any{"type": "array", "items": map[string]any{"oneOf": []any{asset, map[string]any{"type": "string"}}}},
		"video_edit_brief": map[string]any{"type": "string"}, "video_edit_preferences": map[string]any{"type": "object"},
		"missing_inputs": stringArray, "agent_decisions": stringArray, "target_audience": map[string]any{"type": "string"},
		"angle": map[string]any{"type": "string"}, "outline": stringArray, "style_constraints": map[string]any{"type": "string"},
		"asset_plan": map[string]any{"type": "string"}, "acceptance_criteria": map[string]any{"type": "string"},
		"cost_estimate": map[string]any{"type": "string"}, "execution_options": stringArray,
	}}
	return map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{
		"plan": plan,
		// Review payloads are validated by the mode-specific Server decoder.
		"review": map[string]any{"type": "object", "additionalProperties": true},
	}}
}

func stageAssistantDraftHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || (svcs.InteractiveSvc == nil && svcs.InteractionSvc == nil) {
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
	if svcs.InteractionSvc != nil {
		interactionID, _ := args["interaction_id"].(string)
		if strings.TrimSpace(interactionID) == "" || identity.InteractionID == "" {
			return errorResult("interaction capability is required"), nil
		}
		generation, ok := interactionGeneration(args["generation"])
		if !ok {
			return errorResult("generation must be an integer"), nil
		}
		attemptID, ok := args["attempt_id"].(string)
		if !ok || strings.TrimSpace(attemptID) == "" {
			return errorResult("attempt_id is required"), nil
		}
		modeID, ok := args["mode_id"].(string)
		if !ok || strings.TrimSpace(modeID) == "" {
			return errorResult("mode_id is required"), nil
		}
		payloadDigest, ok := args["payload_digest"].(string)
		if !ok || strings.TrimSpace(payloadDigest) == "" {
			return errorResult("payload_digest is required"), nil
		}
		var payload json.RawMessage
		if err := decodeInteractiveArgumentStrict(args, "payload", &payload); err != nil {
			return errorResult(fmt.Sprintf("payload is invalid: %v", err)), nil
		}
		ready, ok := args["ready"].(bool)
		if !ok {
			return errorResult("ready must be a boolean"), nil
		}
		if identity.InteractionID != interactionID || identity.Generation != generation || (identity.ModeID != "" && identity.ModeID != modeID) {
			return errorResult(service.InteractionErrorGenerationChanged), nil
		}
		result, err := svcs.InteractionSvc.StageDraftForInteraction(ctx, identity.UserID, service.InteractionStageInput{InteractionID: interactionID, Generation: generation, AttemptID: attemptID, ModeID: modeID, Payload: payload, PayloadDigest: payloadDigest, Ready: ready})
		if err != nil {
			return errorResult(fmt.Sprintf("stage assistant draft: %v", err)), nil
		}
		return textResult(result)
	}
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
	if err := stageLegacyAssistantDraft(ctx, svcs.InteractiveSvc, identity.SessionID, input); err != nil {
		return errorResult(fmt.Sprintf("stage assistant draft: %v", err)), nil
	}
	return textResult(map[string]any{"staged": true, "session_id": identity.SessionID, "ready": input.Ready})
}

func stageProjectCreatorDraftHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || (svcs.InteractiveSvc == nil && svcs.InteractionSvc == nil) {
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
	if svcs.InteractionSvc != nil {
		interactionID, _ := args["interaction_id"].(string)
		if strings.TrimSpace(interactionID) == "" || identity.InteractionID == "" {
			return errorResult("interaction capability is required"), nil
		}
		generation, ok := interactionGeneration(args["generation"])
		if !ok {
			return errorResult("generation must be an integer"), nil
		}
		attemptID, ok := args["attempt_id"].(string)
		if !ok || strings.TrimSpace(attemptID) == "" {
			return errorResult("attempt_id is required"), nil
		}
		modeID, ok := args["mode_id"].(string)
		if !ok || strings.TrimSpace(modeID) == "" {
			return errorResult("mode_id is required"), nil
		}
		payloadDigest, ok := args["payload_digest"].(string)
		if !ok || strings.TrimSpace(payloadDigest) == "" {
			return errorResult("payload_digest is required"), nil
		}
		var payload json.RawMessage
		if raw, exists := args["payload"]; exists {
			encoded, encodeErr := json.Marshal(raw)
			if encodeErr != nil {
				return errorResult(fmt.Sprintf("payload is invalid: %v", encodeErr)), nil
			}
			payload = encoded
		} else {
			encoded, encodeErr := json.Marshal(map[string]any{"project": args["project"], "profile": args["profile"], "progress": args["progress"], "missing_fields": args["missing_fields"], "inferred_fields": args["inferred_fields"], "memory_append": args["memory_append"], "ready": args["ready"]})
			if encodeErr != nil {
				return errorResult(fmt.Sprintf("payload is invalid: %v", encodeErr)), nil
			}
			payload = encoded
		}
		ready, ok := args["ready"].(bool)
		if !ok {
			return errorResult("ready must be a boolean"), nil
		}
		if identity.InteractionID != interactionID || identity.Generation != generation || (identity.ModeID != "" && identity.ModeID != modeID) {
			return errorResult(service.InteractionErrorGenerationChanged), nil
		}
		result, err := svcs.InteractionSvc.StageDraftForInteraction(ctx, identity.UserID, service.InteractionStageInput{InteractionID: interactionID, Generation: generation, AttemptID: attemptID, ModeID: modeID, Payload: payload, PayloadDigest: payloadDigest, Ready: ready})
		if err != nil {
			return errorResult(fmt.Sprintf("stage project creator draft: %v", err)), nil
		}
		return textResult(result)
	}

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

	if err := stageLegacyProjectCreatorDraft(ctx, svcs.InteractiveSvc, identity.SessionID, input); err != nil {
		return errorResult(fmt.Sprintf("stage project creator draft: %v", err)), nil
	}
	return textResult(map[string]any{"staged": true, "session_id": identity.SessionID, "ready": input.Ready})
}

func stageLegacyAssistantDraft(ctx context.Context, interactiveSvc *service.InteractiveService, sessionID string, input service.AssistantDraftStageInput) error {
	return interactiveSvc.StageAssistantDraftForSessionContext(ctx, sessionID, input)
}

func stageLegacyProjectCreatorDraft(ctx context.Context, interactiveSvc *service.InteractiveService, sessionID string, input service.InteractiveStageInput) error {
	return interactiveSvc.StageDraftForSessionContext(ctx, sessionID, input)
}

func interactionGeneration(raw any) (int64, bool) {
	switch value := raw.(type) {
	case int:
		return int64(value), value > 0
	case int64:
		return value, value > 0
	case float64:
		return int64(value), value >= 1 && value == float64(int64(value))
	default:
		return 0, false
	}
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
