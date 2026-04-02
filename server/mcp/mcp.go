package mcp

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// NewMCPHttpHandler returns a Fiber handler that exposes MCP tool definitions
// for external Claude Code connections. Currently returns a static tool list;
// full MCP streamable HTTP implementation comes in Phase 5.
func NewMCPHttpHandler(repo repository.Repository, logger *zerolog.Logger) fiber.Handler {
	return func(c fiber.Ctx) error {
		// user_id is set by MCPAuthMiddleware. It may be "mcp-api-key" for
		// API key authentication, in which case we list all available tools.
		userID, _ := c.Locals("user_id").(string)

		// List user configs to determine available scopes.
		ctx := c.Context()
		configs, err := repo.UserConfigs().ListByUserID(ctx, userID)
		if err != nil {
			logger.Error().Err(err).Str("user_id", userID).Msg("failed to list user configs")
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "failed to load user configs",
			})
		}

		// If authenticated via API key, include all scope tools regardless of config.
		authMethod, _ := c.Locals("auth_method").(string)
		if authMethod == "api_key" {
			configs = append(configs,
				&model.UserConfig{Scope: model.ScopeArticle},
				&model.UserConfig{Scope: model.ScopeXls},
				&model.UserConfig{Scope: model.ScopeRednote},
			)
		}

		// Build tool list based on configured scopes.
		tools := buildToolList(configs)

		return c.JSON(fiber.Map{
			"server": "anbanwriter",
			"tools":  tools,
		})
	}
}

// toolDefinition represents a tool for the MCP tool list response.
type toolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// buildToolList returns the list of available tools based on user's configured scopes.
func buildToolList(configs []*model.UserConfig) []toolDefinition {
	// Core tools available for all scopes.
	tools := []toolDefinition{
		{
			Name:        "get_account_info",
			Description: "Return the user's account configuration for the current scope.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		{
			Name:        "save_file",
			Description: "Write content to a file in the work directory.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path":    map[string]any{"type": "string"},
					"content": map[string]any{"type": "string"},
				},
				"required": []string{"path", "content"},
			},
		},
		{
			Name:        "read_file",
			Description: "Read content from a file in the work directory.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
				"required": []string{"path"},
			},
		},
		{
			Name:        "list_files",
			Description: "List files and directories in the work directory.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"path": map[string]any{"type": "string"},
				},
			},
		},
	}

	// Scope-specific tools.
	for _, cfg := range configs {
		switch cfg.Scope {
		case model.ScopeArticle:
			tools = append(tools, articleTools()...)
		case model.ScopeXls:
			tools = append(tools, xlsTools()...)
		case model.ScopeRednote:
			tools = append(tools, rednoteTools()...)
		}
	}

	return tools
}

func articleTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "generate_cover_image",
			Description: "Generate a cover image for a WeChat article.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{"type": "string"},
					"size":   map[string]any{"type": "string", "default": "2560x1440"},
					"output": map[string]any{"type": "string"},
				},
				"required": []string{"prompt"},
			},
		},
		{
			Name:        "create_article_draft",
			Description: "Create a WeChat article draft.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":         map[string]any{"type": "string"},
					"content":       map[string]any{"type": "string"},
					"author":        map[string]any{"type": "string"},
					"digest":        map[string]any{"type": "string"},
					"thumb_media_id": map[string]any{"type": "string"},
				},
				"required": []string{"title", "content"},
			},
		},
	}
}

func xlsTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "generate_cover_image",
			Description: "Generate a cover image for a WeChat image post.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{"type": "string"},
					"size":   map[string]any{"type": "string", "default": "1728x2304"},
					"output": map[string]any{"type": "string"},
				},
				"required": []string{"prompt"},
			},
		},
		{
			Name:        "create_xls_draft",
			Description: "Create a WeChat image post (xiaolvshu) draft.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"title":     map[string]any{"type": "string"},
					"content":   map[string]any{"type": "string"},
					"images":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					"media_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				},
				"required": []string{"title"},
			},
		},
	}
}

func rednoteTools() []toolDefinition {
	return []toolDefinition{
		{
			Name:        "generate_cover_image",
			Description: "Generate a cover image for a RedNote post.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{"type": "string"},
					"size":   map[string]any{"type": "string", "default": "1728x2304"},
					"output": map[string]any{"type": "string"},
				},
				"required": []string{"prompt"},
			},
		},
	}
}

// MarshalToolListJSON is a convenience for testing.
func MarshalToolListJSON(tools []toolDefinition) (string, error) {
	data, err := json.MarshalIndent(tools, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
