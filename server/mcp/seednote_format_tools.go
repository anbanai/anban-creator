package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerSeednoteFormatTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "export_seednote",
		Description: "Format content for Seednote publishing. Parses Markdown or accepts direct parameters, extracts tags, cleans formatting, and returns structured content ready for publishing.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"markdown": map[string]any{"type": "string", "description": "Markdown content to parse"},
				"title":    map[string]any{"type": "string", "description": "Title (used when not parsing markdown)"},
				"content":  map[string]any{"type": "string", "description": "Body text (used when not parsing markdown)"},
				"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tags (used when not parsing markdown)"},
				"format":   map[string]any{"type": "string", "enum": []any{"json", "markdown"}, "description": "Output format (default: json)"},
			},
		},
	}, exportSeednoteHandler)
}

func exportSeednoteHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.SeednoteExportSvc == nil {
		return errorResult("seednote export service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	result, err := svcs.SeednoteExportSvc.Export(service.SeednoteExportRequest{
		Format:   stringArg(args, "format"),
		Markdown: stringArg(args, "markdown"),
		Title:    stringArg(args, "title"),
		Content:  stringArg(args, "content"),
		Tags:     parseSeednoteTagsArgument(args),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func parseSeednoteTagsArgument(args map[string]any) []string {
	values, ok := args["tags"].([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			result = append(result, text)
		}
	}
	return result
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}
