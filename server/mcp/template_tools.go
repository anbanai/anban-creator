package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/model"
)

// validTemplateTypes defines the allowed template type values.
var validTemplateTypes = map[string]bool{
	"poster":   true,
	"seednote": true,
	"article":  true,
}

func registerTemplateTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "save_template",
		Description: "Save a content template extracted from a viral note or poster design. Templates are global (not user-scoped).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":            map[string]any{"type": "string", "description": "Template type: poster, seednote, or article"},
				"name":            map[string]any{"type": "string", "description": "Template name (optional, auto-derived from style_prompt if empty)"},
				"category":        map[string]any{"type": "string", "description": "Industry/category tag (optional)"},
				"structure":       map[string]any{"type": "string", "description": "JSON string of template structure (required)"},
				"style_prompt":    map[string]any{"type": "string", "description": "AI visual style prompt (optional)"},
				"writing_style":   map[string]any{"type": "string", "description": "Content writing voice/tonality scaffold (optional), distinct from visual style_prompt; delivered to the agent as template_writing_style via get_channel_profile(task_id)"},
				"example_content": map[string]any{"type": "string", "description": "JSON string of example content (optional)"},
				"tags":            map[string]any{"type": "string", "description": "JSON array string of tags (optional)"},
			},
			"required": []any{"type", "structure"},
		},
	}, saveTemplateHandler)

	server.AddTool(&mcp.Tool{
		Name:        "list_templates",
		Description: "List content templates with optional filtering by type and category.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type":     map[string]any{"type": "string", "description": "Filter by template type (optional)"},
				"category": map[string]any{"type": "string", "description": "Filter by category (optional)"},
				"limit":    map[string]any{"type": "integer", "description": "Max results (default 20)"},
			},
		},
	}, listTemplatesHandler)

	server.AddTool(&mcp.Tool{
		Name:        "get_template",
		Description: "Get a template by ID.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string", "description": "Template ID"},
			},
			"required": []any{"id"},
		},
	}, getTemplateHandler)
}

func saveTemplateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.TemplateSvc == nil {
		return errorResult("template service not available"), nil
	}
	_ = getUserID(ctx) // Templates are global (not user-scoped); user ID is intentionally unused.
	args := parseArgs(req.Params.Arguments)

	tmplType, _ := args["type"].(string)
	name, _ := args["name"].(string)
	structure, _ := args["structure"].(string)
	category, _ := args["category"].(string)
	stylePrompt, _ := args["style_prompt"].(string)
	writingStyle, _ := args["writing_style"].(string)
	exampleContent, _ := args["example_content"].(string)
	tags, _ := args["tags"].(string)

	if tmplType == "" {
		return errorResult("type is required"), nil
	}
	if !validTemplateTypes[tmplType] {
		return errorResult(fmt.Sprintf("invalid type: %s (must be one of: poster, seednote, article)", tmplType)), nil
	}
	if structure == "" {
		return errorResult("structure is required"), nil
	}

	if tags == "" {
		tags = "[]"
	}

	var structureMap map[string]any
	if err := json.Unmarshal([]byte(structure), &structureMap); err != nil {
		return errorResult(fmt.Sprintf("invalid structure JSON: %v", err)), nil
	}

	var exampleMap map[string]any
	if exampleContent != "" {
		if err := json.Unmarshal([]byte(exampleContent), &exampleMap); err != nil {
			return errorResult(fmt.Sprintf("invalid example_content JSON: %v", err)), nil
		}
	}

	var tagsSlice []string
	if err := json.Unmarshal([]byte(tags), &tagsSlice); err != nil {
		return errorResult(fmt.Sprintf("invalid tags JSON: %v", err)), nil
	}

	template := model.Template{
		ID:             uuid.New().String(),
		Type:           tmplType,
		Name:           name,
		Category:       category,
		Structure:      structureMap,
		StylePrompt:    stylePrompt,
		WritingStyle:   writingStyle,
		ExampleContent: exampleMap,
		Tags:           tagsSlice,
		IsActive:       true,
	}

	created, err := svcs.TemplateSvc.Create(ctx, &template, "")
	if err != nil {
		return errorResult(fmt.Sprintf("save template: %v", err)), nil
	}

	return textResult(map[string]any{
		"id":      created.ID,
		"name":    created.Name,
		"type":    created.Type,
		"status":  "created",
		"message": fmt.Sprintf("Template '%s' saved successfully.", created.Name),
	})
}

func listTemplatesHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.TemplateSvc == nil {
		return errorResult("template service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)

	tmplType, _ := args["type"].(string)
	category, _ := args["category"].(string)
	limit := 20
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}

	templates, total, err := svcs.TemplateSvc.List(ctx, tmplType, category, "", "", "public", 0, limit)
	if err != nil {
		return errorResult(fmt.Sprintf("list templates: %v", err)), nil
	}

	return textResult(map[string]any{
		"items": templates,
		"total": total,
	})
}

func getTemplateHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.TemplateSvc == nil {
		return errorResult("template service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)

	id, _ := args["id"].(string)
	if id == "" {
		return errorResult("id is required"), nil
	}

	template, err := svcs.TemplateSvc.GetByID(ctx, id)
	if err != nil {
		return errorResult(fmt.Sprintf("get template: %v", err)), nil
	}

	return textResult(template)
}
