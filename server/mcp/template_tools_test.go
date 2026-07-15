package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSaveTemplateIsIdempotent(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()

	first := saveTemplateForTest(t, ctx, `{
		"type":"seednote",
		"name":"通勤咖啡",
		"category":"生活方式",
		"style_prompt":"暖色晨光，干净排版",
		"tags":"[\"咖啡\",\"通勤\"]"
	}`)
	second := saveTemplateForTest(t, ctx, `{
		"type":"seednote",
		"name":" 通勤咖啡 ",
		"category":"生活方式 ",
		"style_prompt":"暖色晨光，干净排版\n",
		"tags":"[\"通勤\",\"咖啡\",\"咖啡\"]"
	}`)

	if first["status"] != "created" {
		t.Fatalf("first status = %v, want created", first["status"])
	}
	if second["status"] != "existing" {
		t.Fatalf("second status = %v, want existing", second["status"])
	}
	if first["id"] == "" || second["id"] != first["id"] {
		t.Fatalf("template IDs = %v and %v, want same non-empty ID", first["id"], second["id"])
	}
	count, err := repo.Templates().Count(ctx, "seednote", "", "", "", "public")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("template count = %d, want 1", count)
	}
}

func saveTemplateForTest(t *testing.T, ctx context.Context, args string) map[string]any {
	t.Helper()
	result, err := saveTemplateHandler(ctx, &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(args)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("save_template returned error: %#v", result.Content)
	}
	return decodeMCPMap(t, result)
}
