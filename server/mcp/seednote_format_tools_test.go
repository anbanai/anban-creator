package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func TestExportSeednoteHandlerDelegatesMarkdownParsing(t *testing.T) {
	old := svcs
	svcs = &Services{SeednoteExportSvc: service.NewSeednoteExportService()}
	defer func() { svcs = old }()

	raw := json.RawMessage(`{"markdown":"# Test Title\n\n![cover](cover.png)\n\nBody #标签一 #标签二"}`)
	result, err := exportSeednoteHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("result = %#v", result)
	}
	parsed := decodeMCPMap(t, result)
	if parsed["title"] != "Test Title" || !strings.Contains(parsed["content"].(string), "[图片:1]") {
		t.Fatalf("parsed = %#v", parsed)
	}
	if images, ok := parsed["images"].([]any); !ok || len(images) != 1 {
		t.Fatalf("images = %#v", parsed["images"])
	}
}

func TestExportSeednoteHandlerReturnsMarkdownFormat(t *testing.T) {
	old := svcs
	svcs = &Services{SeednoteExportSvc: service.NewSeednoteExportService()}
	defer func() { svcs = old }()

	raw := json.RawMessage(`{"format":"markdown","title":"Title","content":"Body","tags":[" tag ","tag",""]}`)
	result, err := exportSeednoteHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed := decodeMCPMap(t, result)
	if parsed["format"] != "markdown" || !strings.Contains(parsed["output"].(string), "# Seednote Publishing Content") {
		t.Fatalf("parsed = %#v", parsed)
	}
	if _, exists := parsed["images"]; exists {
		t.Fatalf("markdown export unexpectedly contains images field: %#v", parsed)
	}
	if tags, ok := parsed["tags"].([]any); !ok || len(tags) != 3 || tags[0] != "tag" || tags[1] != "tag" || tags[2] != "" {
		t.Fatalf("tags = %#v", parsed["tags"])
	}
}

func TestExportSeednoteHandlerJSONIncludesEmptyImages(t *testing.T) {
	old := svcs
	svcs = &Services{SeednoteExportSvc: service.NewSeednoteExportService()}
	defer func() { svcs = old }()

	raw := json.RawMessage(`{"title":"Title","content":"Body"}`)
	result, err := exportSeednoteHandler(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: raw},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed := decodeMCPMap(t, result)
	images, exists := parsed["images"]
	if !exists {
		t.Fatalf("JSON export missing images field: %#v", parsed)
	}
	if values, ok := images.([]any); !ok || len(values) != 0 {
		t.Fatalf("images = %#v, want empty array", images)
	}
}
