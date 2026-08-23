package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestLegacyProgressCompatibilityToolRemainsRegistered(t *testing.T) {
	tools := listToolNames(t, RegisterTools)
	if !tools["update_task_progress"] {
		t.Fatal("update_task_progress must remain registered for Codex, DSH, and other hosts without SDK Task lifecycle hooks")
	}
}

func TestLegacyProgressCompatibilityToolSchema(t *testing.T) {
	var progressTool *mcp.Tool
	for _, tool := range listRegisteredTools(t, RegisterTools) {
		if tool.Name == "update_task_progress" {
			progressTool = tool
			break
		}
	}
	if progressTool == nil {
		t.Fatal("update_task_progress is not registered")
	}
	description := strings.ToLower(progressTool.Description)
	if !strings.Contains(description, "compatibility") || !strings.Contains(description, "hosts") || !strings.Contains(description, "without task lifecycle hooks") {
		t.Fatalf("description = %q, want compatibility host boundary", progressTool.Description)
	}

	schema, ok := progressTool.InputSchema.(map[string]any)
	if !ok {
		t.Fatalf("InputSchema type = %T, want map[string]any", progressTool.InputSchema)
	}
	if schema["type"] != "object" {
		t.Fatalf("schema type = %#v, want object", schema["type"])
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties type = %T, want map[string]any", schema["properties"])
	}
	wantTypes := map[string]string{
		"task_id":          "string",
		"stage":            "string",
		"title":            "string",
		"description":      "string",
		"progress_percent": "integer",
	}
	if len(properties) != len(wantTypes) {
		t.Fatalf("property count = %d, want %d: %#v", len(properties), len(wantTypes), properties)
	}
	for name, wantType := range wantTypes {
		property, ok := properties[name].(map[string]any)
		if !ok {
			t.Fatalf("property %q = %#v, want object", name, properties[name])
		}
		if property["type"] != wantType {
			t.Fatalf("property %q type = %#v, want %q", name, property["type"], wantType)
		}
	}

	requiredValues, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("required type = %T, want []any", schema["required"])
	}
	if len(requiredValues) != 3 {
		t.Fatalf("required count = %d, want 3: %#v", len(requiredValues), requiredValues)
	}
	required := make(map[string]bool, len(requiredValues))
	for _, value := range requiredValues {
		name, ok := value.(string)
		if !ok {
			t.Fatalf("required value = %#v, want string", value)
		}
		required[name] = true
	}
	if len(required) != 3 || !required["task_id"] || !required["stage"] || !required["title"] {
		t.Fatalf("required = %#v, want exactly task_id, stage, title", required)
	}
	if required["description"] || required["progress_percent"] {
		t.Fatalf("optional properties unexpectedly required: %#v", required)
	}
}

func TestLegacyProgressCompatibilityHandlerUsesStageFallback(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	userID := uuid.NewString()
	project := createAccountInfoProject(t, repo, userID, "")
	task := createAccountInfoTask(t, repo, userID, project.ID, "")
	arguments, err := json.Marshal(map[string]any{
		"task_id": task.ID,
		"stage":   "writing",
		"title":   "Writing",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := progressUpdateHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: arguments},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("progressUpdateHandler returned tool error: %#v", result.Content)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Progress != 40 || persisted.LatestProgress.Data().Percent != 40 {
		t.Fatalf("legacy fallback progress = %d/%#v, want seednote writing percent 40", persisted.Progress, persisted.LatestProgress.Data())
	}
}
