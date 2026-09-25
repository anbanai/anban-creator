package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func metadataTargetRequest(t *testing.T, args map[string]any) *mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: raw}}
}

func TestCompletionMetadataTargetIsDerivedForExecutionAndExplicitForUser(t *testing.T) {
	current := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-current")
	for _, test := range []struct {
		name       string
		ctx        context.Context
		args       map[string]any
		wantTarget string
		wantError  bool
	}{
		{name: "execution token defaults to current", ctx: current, args: map[string]any{"task_id": "task-1"}, wantTarget: "execution-current"},
		{name: "execution token may name itself", ctx: current, args: map[string]any{"task_id": "task-1", "execution_id": "execution-current"}, wantTarget: "execution-current"},
		{name: "execution token cannot select another execution", ctx: current, args: map[string]any{"task_id": "task-1", "execution_id": "execution-history"}, wantError: true},
		{name: "user credential must select target", ctx: withMCPUserID(context.Background(), "user-1"), args: map[string]any{"task_id": "task-1"}, wantError: true},
		{name: "user credential may select historical target", ctx: withMCPUserID(context.Background(), "user-1"), args: map[string]any{"task_id": "task-1", "execution_id": "execution-history"}, wantTarget: "execution-history"},
	} {
		t.Run(test.name, func(t *testing.T) {
			taskID, executionID, result := parseRecomputeArgs(test.ctx, metadataTargetRequest(t, test.args))
			if test.wantError {
				if result == nil || taskID != "" || executionID != "" {
					t.Fatalf("target = %q/%q, result=%v; want rejection", taskID, executionID, result)
				}
				return
			}
			if result != nil || taskID != "task-1" || executionID != test.wantTarget {
				t.Fatalf("target = %q/%q, result=%v, want task-1/%q", taskID, executionID, result, test.wantTarget)
			}
		})
	}
}

func TestExecutionScopedMetadataToolRejectsIdentityDuplicationAndMismatch(t *testing.T) {
	for _, test := range []struct {
		name      string
		tool      string
		args      map[string]any
		wantError bool
	}{
		{name: "submit omits execution identity", tool: "submit_completion_metadata", args: map[string]any{"task_id": "task-1"}},
		{name: "submit rejects duplicated execution identity", tool: "submit_completion_metadata", args: map[string]any{"task_id": "task-1", "execution_id": "execution-1"}, wantError: true},
		{name: "task read rejects repeated identity", tool: "get_task", args: map[string]any{"task_id": "task-1", "execution_id": "execution-1"}, wantError: true},
		{name: "historical tool defaults target", tool: "recompute_content_tags", args: map[string]any{"task_id": "task-1"}},
		{name: "historical tool cannot redirect target", tool: "recompute_content_tags", args: map[string]any{"task_id": "task-1", "execution_id": "execution-2"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateExecutionToolScope(test.tool, test.args, "project-1", "task-1", "execution-1")
			if (err != nil) != test.wantError {
				t.Fatalf("validate scope error = %v, wantError=%t", err, test.wantError)
			}
		})
	}
}

func TestCompletionMetadataRejectsMalformedExplicitTarget(t *testing.T) {
	for _, identity := range []struct {
		name string
		ctx  context.Context
	}{
		{"execution", withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-current")},
		{"user", withMCPUserID(context.Background(), "user-1")},
	} {
		for _, value := range []struct {
			name  string
			value any
		}{
			{"null", nil}, {"number", 123}, {"empty", ""}, {"blank", " "},
		} {
			t.Run(identity.name+"/"+value.name, func(t *testing.T) {
				taskID, executionID, result := parseRecomputeArgs(identity.ctx, metadataTargetRequest(t, map[string]any{"task_id": "task-1", "execution_id": value.value}))
				if result == nil || !result.IsError || taskID != "" || executionID != "" {
					t.Fatalf("malformed target selected %q/%q, result=%v", taskID, executionID, result)
				}
			})
		}
	}
}
