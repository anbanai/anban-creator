package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
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

func TestGenerateImageSchemaContainsOnlySemanticInputs(t *testing.T) {
	properties := generateImageInputSchema()["properties"].(map[string]any)
	for _, removed := range []string{"operation_id", "verify_with_vision", "verification_prompt", "upload_to_cdn"} {
		if _, ok := properties[removed]; ok {
			t.Fatalf("generate_image exposes removed field %q", removed)
		}
	}
	required := generateImageInputSchema()["required"].([]any)
	for _, value := range required {
		if value == "operation_id" {
			t.Fatal("generate_image still requires operation_id")
		}
	}
}

func TestRegisterRenderedImageSchemaDoesNotUpload(t *testing.T) {
	properties := registerRenderedImageInputSchema()["properties"].(map[string]any)
	if _, ok := properties["upload_to_cdn"]; ok {
		t.Fatal("register_rendered_image still exposes upload_to_cdn")
	}
}

func TestDownloadImageSchemaDoesNotUpload(t *testing.T) {
	properties := downloadImageInputSchema()["properties"].(map[string]any)
	if _, ok := properties["upload"]; ok {
		t.Fatal("download_image still exposes upload")
	}
}

func TestGenerateImagePublicResultContainsOnlyDurableAssetFields(t *testing.T) {
	raw, err := json.Marshal(service.TaskImageAsset{
		Name: "cover.png", Role: "cover", DownloadURL: "/files/cover.png", FilePath: "output/cover.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{
		"provider", "model", "selection_reason", "response_type", "revised_prompt",
		"output_mime", "verification", "wechat_url", "media_id", "billing",
	} {
		if strings.Contains(string(raw), removed) {
			t.Fatalf("generate_image public result exposes %q: %s", removed, raw)
		}
	}
}

func TestGenerateImageFailureDoesNotExposeRouteMetadata(t *testing.T) {
	raw, err := json.Marshal(classifyImageToolFailure(
		context.Background(), context.Background(), context.DeadlineExceeded,
		"generate", time.Minute, false,
	))
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"provider", "model"} {
		if _, ok := fields[removed]; ok {
			t.Fatalf("generate_image failure exposes %q: %s", removed, raw)
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
