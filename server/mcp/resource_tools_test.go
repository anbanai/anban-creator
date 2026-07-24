package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/resources"
	"github.com/anbanai/anban-creator/server/service"
)

func TestGetResourceIncludesRawArticleTemplate(t *testing.T) {
	useResourceCatalogService(t)
	args := json.RawMessage(`{"category":"article_templates","name":"long-form-essay","include_raw":true}`)
	result, err := getResourceHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: args},
	})
	if err != nil {
		t.Fatalf("getResourceHandler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("result is error: %#v", result)
	}

	text := result.Content[0].(*mcp.TextContent).Text
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed["category"] != "article_templates" {
		t.Fatalf("category = %v, want article_templates", parsed["category"])
	}
	if parsed["article_type"] != "long-form-essay" {
		t.Fatalf("article_type = %v, want long-form-essay", parsed["article_type"])
	}
	if modules, ok := parsed["modules"].([]any); !ok || len(modules) == 0 {
		t.Fatalf("modules missing from article template resource: %#v", parsed["modules"])
	}
	raw, _ := parsed["raw"].(string)
	if !strings.Contains(raw, "slot_strategy") {
		t.Fatalf("raw template YAML missing slot_strategy: %q", raw)
	}
}

func TestGetResourceExposesLayoutSchema(t *testing.T) {
	useResourceCatalogService(t)
	args := json.RawMessage(`{"category":"layouts","name":"cta"}`)
	result, err := getResourceHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: args},
	})
	if err != nil {
		t.Fatalf("getResourceHandler error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	fields, ok := parsed["fields"].(map[string]any)
	if !ok {
		t.Fatalf("fields missing from layout resource: %#v", parsed["fields"])
	}
	required, ok := fields["required"].([]any)
	if !ok || len(required) == 0 {
		t.Fatalf("required fields missing from layout resource: %#v", fields["required"])
	}
}

func TestGetResourceExposesWriterMetadata(t *testing.T) {
	useResourceCatalogService(t)
	args := json.RawMessage(`{"category":"writers","name":"dan-koe"}`)
	result, err := getResourceHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: args},
	})
	if err != nil {
		t.Fatalf("getResourceHandler error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	for _, field := range []string{"aliases", "writer_best_for", "writing_tone", "writing_voice", "writing_perspective", "title_formulas"} {
		if _, ok := parsed[field]; !ok {
			t.Fatalf("writer resource missing %s: %#v", field, parsed)
		}
	}
	if formulas, ok := parsed["title_formulas"].([]any); !ok || len(formulas) == 0 {
		t.Fatalf("writer title_formulas missing: %#v", parsed["title_formulas"])
	}
}

func useResourceCatalogService(t *testing.T) {
	t.Helper()
	old := svcs
	svcs = &Services{ResourceCatalogSvc: service.NewResourceCatalogService(resources.Manager())}
	t.Cleanup(func() { svcs = old })
}
