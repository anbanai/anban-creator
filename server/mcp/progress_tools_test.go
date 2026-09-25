package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/model"
)

func findProgressTool(t *testing.T, name string) *mcp.Tool {
	t.Helper()
	for _, tool := range listRegisteredTools(t, RegisterTools) {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("%s is not registered", name)
	return nil
}

func TestLifecycleProgressToolsExposeOnlyServerOwnedContract(t *testing.T) {
	tests := []struct {
		name       string
		properties map[string]string
		required   map[string]bool
	}{
		{
			name: "set_task_progress_plan",
			properties: map[string]string{
				"task_id": "string", "stages": "array",
			},
			required: map[string]bool{"task_id": true, "stages": true},
		},
		{
			name: "update_task_progress",
			properties: map[string]string{
				"task_id": "string", "stage": "string", "state": "string", "description": "string",
			},
			required: map[string]bool{"task_id": true, "stage": true, "state": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := findProgressTool(t, tt.name)
			schema := tool.InputSchema.(map[string]any)
			if schema["additionalProperties"] != false {
				t.Fatalf("additionalProperties = %#v, want false", schema["additionalProperties"])
			}
			properties := schema["properties"].(map[string]any)
			if len(properties) != len(tt.properties) {
				t.Fatalf("properties = %#v", properties)
			}
			for name, wantType := range tt.properties {
				property, ok := properties[name].(map[string]any)
				if !ok || property["type"] != wantType {
					t.Fatalf("property %s = %#v, want type %s", name, properties[name], wantType)
				}
			}
			for _, removed := range []string{"title", "progress_percent", "percent"} {
				if _, exists := properties[removed]; exists {
					t.Fatalf("removed property %q remains in schema", removed)
				}
			}
			required := map[string]bool{}
			for _, raw := range schema["required"].([]any) {
				required[raw.(string)] = true
			}
			if len(required) != len(tt.required) {
				t.Fatalf("required = %#v", required)
			}
			for name := range tt.required {
				if !required[name] {
					t.Fatalf("required missing %s: %#v", name, required)
				}
			}
		})
	}
}

func setupMCPTaskLifecycle(t *testing.T) (context.Context, string, string, func()) {
	t.Helper()
	_, _, repo, cleanup := setupAccountInfoTest(t)
	userID := uuid.NewString()
	project := createAccountInfoProject(t, repo, userID, "")
	task := createAccountInfoTask(t, repo, userID, project.ID, "")
	if err := repo.Tasks().UpdateStatus(context.Background(), task.ID, model.TaskStatusRunning); err != nil {
		t.Fatal(err)
	}
	executionID := uuid.NewString()
	if err := repo.TaskExecutions().Create(context.Background(), &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Target: "test", Status: model.TaskExecutionRunning, Started: true,
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(context.Background(), task.ID, executionID); err != nil || !ok {
		t.Fatalf("set current execution = %v, %v", ok, err)
	}
	return withMCPExecutionIdentity(context.Background(), userID, project.ID, task.ID, executionID), task.ID, executionID, cleanup
}

func lifecycleToolRequest(t *testing.T, arguments map[string]any) *mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(arguments)
	if err != nil {
		t.Fatal(err)
	}
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: raw}}
}

func TestLifecycleProgressHandlersPersistPlanAndSequentialUpdate(t *testing.T) {
	ctx, taskID, executionID, cleanup := setupMCPTaskLifecycle(t)
	defer cleanup()
	planResult, err := progressPlanHandler(ctx, lifecycleToolRequest(t, map[string]any{
		"task_id": taskID,
		"stages":  []map[string]string{{"id": "research", "title": "研究素材"}, {"id": "writing", "title": "撰写内容"}},
	}))
	if err != nil || planResult.IsError {
		t.Fatalf("set plan = %#v, %v", planResult, err)
	}
	updateResult, err := progressUpdateHandler(ctx, lifecycleToolRequest(t, map[string]any{
		"task_id": taskID, "stage": "research", "state": "active", "description": "正在核验来源",
	}))
	if err != nil || updateResult.IsError {
		t.Fatalf("update stage = %#v, %v", updateResult, err)
	}
	data := decodeMCPMap(t, updateResult)
	lifecycle := data["lifecycle"].(map[string]any)
	if lifecycle["revision"] != float64(2) {
		t.Fatalf("lifecycle = %#v", lifecycle)
	}
	if data["execution_id"] != executionID {
		t.Fatalf("execution_id = %#v, want token-bound %q", data["execution_id"], executionID)
	}
}

func TestLifecycleProgressHandlersRequireMatchingExecutionIdentity(t *testing.T) {
	ctx, taskID, executionID, cleanup := setupMCPTaskLifecycle(t)
	defer cleanup()
	for _, test := range []struct {
		name    string
		ctx     context.Context
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{
			name: "missing identity", ctx: context.Background(), handler: progressPlanHandler,
			args: map[string]any{"task_id": taskID, "execution_id": executionID, "stages": []map[string]string{{"id": "one", "title": "One"}, {"id": "two", "title": "Two"}}},
		},
		{
			name: "task mismatch", ctx: ctx, handler: progressPlanHandler,
			args: map[string]any{"task_id": "wrong", "stages": []map[string]string{{"id": "one", "title": "One"}, {"id": "two", "title": "Two"}}},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := test.handler(test.ctx, lifecycleToolRequest(t, test.args))
			if err != nil {
				t.Fatal(err)
			}
			if !result.IsError {
				t.Fatalf("result = %#v, want tool error", result)
			}
		})
	}
}
