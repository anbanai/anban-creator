package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRepositoryInstructionsDefineMCPAsCapabilityTransport(t *testing.T) {
	for _, path := range []string{"../../CLAUDE.md", "../../AGENTS.md"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, want := range []string{
			"MCP is a stateless capability transport",
			"Agents and Skills own business workflow orchestration",
			"one application capability",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}

func TestExtractedMCPHandlersRequireApplicationCapabilities(t *testing.T) {
	old := svcs
	defer func() { svcs = old }()
	svcs = &Services{}

	tests := []struct {
		name    string
		args    map[string]any
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
		want    string
	}{
		{name: "project profile", args: map[string]any{"project_id": "project"}, handler: accountInfoHandler, want: "agent project profile service not available"},
		{name: "article score", args: map[string]any{"read_count": 100.0, "like_count": 1.0}, handler: scoreArticleHandler, want: "article score service not available"},
		{name: "seednote export", args: map[string]any{"title": "title", "content": "body"}, handler: exportSeednoteHandler, want: "seednote export service not available"},
		{name: "resource catalog", args: map[string]any{"category": "themes"}, handler: listResourcesHandler, want: "resource catalog service not available"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			result, err := tt.handler(context.Background(), &mcp.CallToolRequest{
				Params: &mcp.CallToolParamsRaw{Arguments: raw},
			})
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || len(result.Content) == 0 {
				t.Fatalf("result = %#v, want tool error", result)
			}
			text := result.Content[0].(*mcp.TextContent).Text
			if !strings.Contains(text, tt.want) {
				t.Fatalf("error = %q, want %q", text, tt.want)
			}
		})
	}
}
