package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerResourceTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "list_resources",
		Description: "List available embedded resources (themes, writers, layouts, image presets, article templates). Returns metadata for each resource including name, description, and category-specific fields.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category": map[string]any{"type": "string", "enum": []any{"themes", "writers", "layouts", "image_presets", "article_templates"}, "description": "Resource category to list"},
				"platform": map[string]any{"type": "string", "description": "Filter by platform: article or seednote (optional)"},
			},
			"required": []any{"category"},
		},
	}, listResourcesHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_resource",
		Description: "Get detailed metadata for a specific resource including usage guidance, schema, syntax examples, and optional raw YAML.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category":    map[string]any{"type": "string", "enum": []any{"themes", "writers", "layouts", "image_presets", "article_templates"}, "description": "Resource category"},
				"name":        map[string]any{"type": "string", "description": "Resource name"},
				"include_raw": map[string]any{"type": "boolean", "description": "Include read-only raw YAML for exact agent consumption"},
			},
			"required": []any{"category", "name"},
		},
	}, getResourceHandler)
}

func listResourcesHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ResourceCatalogSvc == nil {
		return errorResult("resource catalog service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	category := stringArg(args, "category")
	if category == "" {
		return errorResult("category is required"), nil
	}
	result, err := svcs.ResourceCatalogSvc.Query(service.ResourceCatalogRequest{
		Category: category,
		Platform: stringArg(args, "platform"),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func getResourceHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ResourceCatalogSvc == nil {
		return errorResult("resource catalog service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	category, name := stringArg(args, "category"), stringArg(args, "name")
	if category == "" || name == "" {
		return errorResult("category and name are required"), nil
	}
	includeRaw, _ := args["include_raw"].(bool)
	result, err := svcs.ResourceCatalogSvc.Query(service.ResourceCatalogRequest{
		Category: category, Name: name, IncludeRaw: includeRaw,
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}
