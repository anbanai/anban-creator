package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func TestWorkspaceToolSurface(t *testing.T) {
	tools := listMCPToolsForTest(t, NewMCPHandler(nil, "test-key", nil))
	var prepareTool map[string]any
	for _, tool := range tools {
		tool, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		switch tool["name"] {
		case "prepare_workspace":
			prepareTool = tool
		case "archive_workspace":
			t.Fatal("archive_workspace must not be registered")
		}
	}
	if prepareTool == nil {
		t.Fatal("prepare_workspace must be registered")
	}

	schema, ok := prepareTool["inputSchema"].(map[string]any)
	if !ok {
		t.Fatalf("prepare_workspace inputSchema = %#v, want object", prepareTool["inputSchema"])
	}
	wantRequired := []any{"content_type", "task_id"}
	if got := schema["required"]; !reflect.DeepEqual(got, wantRequired) {
		t.Fatalf("prepare_workspace required = %#v, want %#v", got, wantRequired)
	}

	description, _ := prepareTool["description"].(string)
	for _, want := range []string{
		"canonical task-relative output",
		"managed server tasks must provide task_id",
	} {
		if !strings.Contains(strings.ToLower(description), want) {
			t.Fatalf("prepare_workspace description = %q, missing %q", description, want)
		}
	}
}

func TestPrepareWorkspaceHandlerRejectsTaskless(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	svcs = &Services{WorkspaceSvc: service.NewWorkspaceService()}

	result, err := prepareWorkspaceHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"content_type":"seednote"}`)},
	})
	if err != nil {
		t.Fatalf("prepareWorkspaceHandler returned error: %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("prepareWorkspaceHandler result = %#v, want tool error", result)
	}
	if len(result.Content) == 0 {
		t.Fatal("prepareWorkspaceHandler returned tool error without content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || !strings.Contains(text.Text, "task_id is required") {
		t.Fatalf("prepareWorkspaceHandler result = %#v, want task_id is required", result)
	}
}
