package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func searchToolRequest(t *testing.T, args map[string]any) *mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: raw}}
}

func TestSearchWebHandlerRequiresExecutionIdentityBeforeProviderLookup(t *testing.T) {
	old := svcs
	svcs = nil
	t.Cleanup(func() { svcs = old })
	result, err := searchWebHandler(context.Background(), searchToolRequest(t, map[string]any{"query": "anban"}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError || !strings.Contains(toolResultText(result), "execution_identity_required") {
		t.Fatalf("result = %#v", result)
	}
}

func TestSearchWebHandlerRequiresCompleteExecutionIdentity(t *testing.T) {
	old := svcs
	svcs = nil
	t.Cleanup(func() { svcs = old })
	for name, ctx := range map[string]context.Context{
		"missing project and task": withMCPExecutionIdentity(context.Background(), "user-1", "", "", "execution-1"),
		"missing user":             withMCPExecutionIdentity(context.Background(), "", "project-1", "task-1", "execution-1"),
	} {
		t.Run(name, func(t *testing.T) {
			result, err := searchWebHandler(ctx, searchToolRequest(t, map[string]any{"query": "anban"}))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || !strings.Contains(toolResultText(result), "execution_identity_required") {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestSearchWebHandlerRejectsProviderOverrideAndInvalidInput(t *testing.T) {
	old := svcs
	svcs = nil
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{name: "provider override", args: map[string]any{"query": "q", "provider": "other"}, want: "provider_selection_forbidden"},
		{name: "empty query", args: map[string]any{"query": "  "}, want: "invalid_request"},
		{name: "fractional limit", args: map[string]any{"query": "q", "limit": 1.5}, want: "invalid_request"},
		{name: "large limit", args: map[string]any{"query": "q", "limit": 21}, want: "invalid_request"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := searchWebHandler(ctx, searchToolRequest(t, tt.args))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || !strings.Contains(toolResultText(result), tt.want) {
				t.Fatalf("result = %#v", result)
			}
		})
	}
}

func TestSearchWebHandlerReturnsProviderUnavailableForConfiguredGap(t *testing.T) {
	old := svcs
	svcs = &Services{}
	t.Cleanup(func() { svcs = old })
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	result, err := searchWebHandler(ctx, searchToolRequest(t, map[string]any{"query": "anban"}))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError || !strings.Contains(toolResultText(result), "provider_unavailable") {
		t.Fatalf("result = %#v", result)
	}
}
