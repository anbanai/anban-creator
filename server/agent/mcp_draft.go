package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/royalrick/anbanwriter/app/draft"
	appimage "github.com/royalrick/anbanwriter/app/image"
	"github.com/royalrick/anbanwriter/server/model"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// createDraftTools builds the draft, account, and validation tools.
func createDraftTools(env *toolEnv) []*claudecode.McpTool {
	// --- Tool: create_article_draft ---
	createArticleDraftTool := claudecode.NewTool(
		"create_article_draft",
		"Create a WeChat article draft from JSON data. Requires title and HTML content.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":          map[string]any{"type": "string", "description": "Article title"},
				"content":        map[string]any{"type": "string", "description": "HTML content of the article"},
				"author":         map[string]any{"type": "string", "description": "Author name (optional)"},
				"digest":         map[string]any{"type": "string", "description": "Article digest/summary (optional)"},
				"thumb_media_id": map[string]any{"type": "string", "description": "Cover image media_id (optional)"},
			},
			"required": []string{"title", "content"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			title := args["title"].(string)
			htmlContent := args["content"].(string)
			author := ""
			if a, ok := args["author"].(string); ok {
				author = a
			}
			digest := ""
			if d, ok := args["digest"].(string); ok {
				digest = d
			}
			thumbMediaID := ""
			if t, ok := args["thumb_media_id"].(string); ok {
				thumbMediaID = t
			}

			draftSvc := draft.NewService(env.cfg, env.zapLog)
			articles := []draft.Article{{
				Title:        title,
				Author:       author,
				Digest:       digest,
				Content:      htmlContent,
				ThumbMediaID: thumbMediaID,
			}}
			result, err := draftSvc.CreateDraft(articles)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("create_article_draft failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Article draft created. media_id: %s, draft_url: %s", result.MediaID, result.DraftURL),
				}},
			}, nil
		},
	)

	// --- Tool: create_xls_draft ---
	createXlsDraftTool := claudecode.NewTool(
		"create_xls_draft",
		"Create a WeChat image post (xiaolvshu) draft. Requires title and image file paths or media_ids.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":     map[string]any{"type": "string", "description": "Post title (max 32 chars)"},
				"content":   map[string]any{"type": "string", "description": "Post description (plain text, no HTML)"},
				"images":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "List of local image file paths (will be uploaded automatically)"},
				"media_ids": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "List of already-uploaded media_ids"},
			},
			"required": []string{"title"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			title := args["title"].(string)
			desc := ""
			if d, ok := args["content"].(string); ok {
				desc = d
			}

			var images []string
			if imgs, ok := args["images"].([]any); ok {
				for _, img := range imgs {
					if s, ok := img.(string); ok {
						if !filepath.IsAbs(s) {
							s = filepath.Join(env.workDir, s)
						}
						images = append(images, s)
					}
				}
			}

			var mediaIDs []string
			if ids, ok := args["media_ids"].([]any); ok {
				for _, id := range ids {
					if s, ok := id.(string); ok {
						mediaIDs = append(mediaIDs, s)
					}
				}
			}

			draftSvc := draft.NewService(env.cfg, env.zapLog)
			req := &draft.ImageXlsRequest{
				Title:    title,
				Content:  desc,
				Images:   images,
				MediaIDs: mediaIDs,
			}
			result, err := draftSvc.CreateImageXls(req)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("create_xls_draft failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("XLS draft created. media_id: %s, count: %d, uploaded_ids: %v",
						result.MediaID, result.Count, result.UploadedIDs),
				}},
			}, nil
		},
	)

	// --- Tool: get_account_info ---
	getAccountInfoTool := claudecode.NewTool(
		"get_account_info",
		"Return the user's account configuration for the current scope (article, xls, or rednote).",
		map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			info := map[string]any{
				"name":        env.cfg.Name,
				"keywords":    env.cfg.Keywords,
				"positioning": env.cfg.Positioning,
				"scope":       env.scope,
			}
			switch env.scope {
			case model.ScopeArticle:
				info["author"] = env.cfg.Wechat.Article.Author
				info["style"] = env.cfg.Wechat.Article.Style
				info["theme"] = env.cfg.Wechat.Article.Theme
			case model.ScopeXls:
				info["style"] = env.cfg.Wechat.Xls.Style
			case model.ScopeRednote:
				if env.cfg.Rednote != nil {
					info["style"] = env.cfg.Rednote.Style
				}
			}

			data, _ := json.MarshalIndent(info, "", "  ")
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: string(data),
				}},
			}, nil
		},
	)

	// --- Tool: validate_cover_image ---
	validateCoverTool := claudecode.NewTool(
		"validate_cover_image",
		"Validate that a cover image meets WeChat requirements (pixel count, file size, format).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Path to the cover image file"},
			},
			"required": []string{"file_path"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			filePath := args["file_path"].(string)
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(env.workDir, filePath)
			}

			processor := appimage.NewProcessor(env.cfg, env.coverApiCfg, env.zapLog)
			if err := processor.ValidateCoverImage(filePath); err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("Cover image validation failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{Type: "text", Text: "Cover image validation passed."}},
			}, nil
		},
	)

	// --- Tool: check_draft_history ---
	checkDraftHistoryTool := claudecode.NewTool(
		"check_draft_history",
		"List recent drafts and published articles to avoid topic duplication.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count": map[string]any{"type": "integer", "description": "Number of items to retrieve (default 10)"},
			},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			count := 10
			if c, ok := args["count"].(float64); ok {
				count = int(c)
			}
			if count <= 0 {
				count = 10
			}

			draftSvc := draft.NewService(env.cfg, env.zapLog)
			var lines []string

			// List recent drafts.
			drafts, err := draftSvc.ListDrafts(0, int64(count))
			if err == nil && drafts.TotalCount > 0 {
				lines = append(lines, fmt.Sprintf("=== Recent Drafts (%d) ===", drafts.ItemCount))
				for _, d := range drafts.Items {
					lines = append(lines, fmt.Sprintf("  - %s (media_id: %s)", d.Title, d.MediaID))
				}
			}

			// List recent published articles.
			published, err := draftSvc.ListPublished(0, int64(count))
			if err == nil && published.TotalCount > 0 {
				lines = append(lines, fmt.Sprintf("\n=== Published Articles (%d) ===", published.ItemCount))
				for _, p := range published.Items {
					lines = append(lines, fmt.Sprintf("  - %s", p.Title))
				}
			}

			if len(lines) == 0 {
				lines = append(lines, "No drafts or published articles found.")
			}

			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: strings.Join(lines, "\n"),
				}},
			}, nil
		},
	)

	return []*claudecode.McpTool{
		createArticleDraftTool,
		createXlsDraftTool,
		getAccountInfoTool,
		validateCoverTool,
		checkDraftHistoryTool,
	}
}
