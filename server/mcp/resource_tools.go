package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/resources"
)

// registerResourceTools registers resource discovery MCP tools.
func registerResourceTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "list_resources",
		Description: "List available embedded resources (themes, writers, layouts, image presets). Returns metadata for each resource including name, description, and category-specific fields.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category": map[string]any{
					"type":        "string",
					"enum":        []any{"themes", "writers", "layouts", "image_presets"},
					"description": "Resource category to list",
				},
				"platform": map[string]any{
					"type":        "string",
					"description": "Filter by platform: article or seednote (optional)",
				},
			},
			"required": []any{"category"},
		},
	}, listResourcesHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_resource",
		Description: "Get detailed metadata for a specific resource including usage guidance and syntax examples (for layouts).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category": map[string]any{
					"type":        "string",
					"enum":        []any{"themes", "writers", "layouts", "image_presets"},
					"description": "Resource category",
				},
				"name": map[string]any{
					"type":        "string",
					"description": "Resource name (e.g. 'autumn-warm', 'dan-koe', 'hero', 'cover-default')",
				},
			},
			"required": []any{"category", "name"},
		},
	}, getResourceHandler)
}

func listResourcesHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	category, _ := args["category"].(string)
	platform, _ := args["platform"].(string)

	if category == "" {
		return errorResult("category is required"), nil
	}

	items := resources.Manager().ListByPlatform(resources.Category(category), platform)

	return textResult(map[string]any{
		"category": category,
		"items":    items,
	})
}

func getResourceHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	category, _ := args["category"].(string)
	name, _ := args["name"].(string)

	if category == "" || name == "" {
		return errorResult("category and name are required"), nil
	}

	entry := resources.Manager().Get(resources.Category(category), name)
	if entry == nil {
		return errorResult("resource not found"), nil
	}

	result := map[string]any{
		"name":        entry.Name,
		"category":    entry.Category,
		"description": entry.Description,
	}

	switch resources.Category(category) {
	case resources.CategoryTheme:
		result["mood"] = entry.Mood
		result["best_for"] = entry.BestFor
	case resources.CategoryWriter:
		result["display_name"] = entry.DisplayName
		result["english_name"] = entry.EnglishName
		result["category_cn"] = entry.CategoryCn
	case resources.CategoryLayout:
		result["layout_category"] = entry.LayoutCategory
		result["serves"] = entry.Serves
		result["when_to_use"] = entry.WhenToUse
		result["markdown_syntax"] = entry.MarkdownSyntax
	case resources.CategoryImagePreset:
		result["archetype"] = entry.Archetype
		result["aspect_ratios"] = entry.AspectRatios
		result["default_ratio"] = entry.DefaultRatio
	}

	return textResult(result)
}
