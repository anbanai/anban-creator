package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"go.uber.org/zap"

	"github.com/royalrick/anbanwriter/app/config"
	"github.com/royalrick/anbanwriter/app/draft"
	appimage "github.com/royalrick/anbanwriter/app/image"
	"github.com/royalrick/anbanwriter/server/model"

	claudecode "github.com/severity1/claude-agent-sdk-go"
)

// BuildAppConfig constructs an app/config.Config from a UserConfig DB record.
// This bridges the multi-user server config to the single-account app config.
func BuildAppConfig(userConfig *model.UserConfig) (*config.Config, error) {
	cfg := &config.Config{
		Name:        userConfig.Name,
		Positioning: userConfig.Positioning,
	}

	// Parse keywords (comma or space separated).
	if userConfig.Keywords != "" {
		for _, kw := range strings.Split(userConfig.Keywords, ",") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				cfg.Keywords = append(cfg.Keywords, kw)
			}
		}
	}

	// WeChat credentials.
	cfg.Wechat.AppID = userConfig.WechatAppID
	cfg.Wechat.Secret = userConfig.WechatSecret

	// Scope-specific fields.
	switch userConfig.Scope {
	case model.ScopeArticle:
		cfg.Wechat.Article.Author = userConfig.Author
		cfg.Wechat.Article.Style = userConfig.Style
		cfg.Wechat.Article.Theme = userConfig.Theme
	case model.ScopeXls:
		cfg.Wechat.Xls.Style = userConfig.Style
	case model.ScopeRednote:
		if cfg.Rednote == nil {
			cfg.Rednote = &config.RednoteConfig{}
		}
		cfg.Rednote.Style = userConfig.Style
	}

	// Parse image_api_config JSON into ImageAPI structs.
	if userConfig.ImageAPIConfig != "" {
		var imageCfgs map[string]config.ImageAPI
		if err := json.Unmarshal([]byte(userConfig.ImageAPIConfig), &imageCfgs); err != nil {
			return nil, fmt.Errorf("parse image_api_config: %w", err)
		}
		if coverCfg, ok := imageCfgs["cover"]; ok {
			switch userConfig.Scope {
			case model.ScopeArticle:
				cfg.Wechat.Article.Cover.Image = coverCfg
			case model.ScopeXls:
				cfg.Wechat.Xls.Cover.Image = coverCfg
			case model.ScopeRednote:
				if cfg.Rednote == nil {
					cfg.Rednote = &config.RednoteConfig{}
				}
				cfg.Rednote.Cover.Image = coverCfg
			}
		}
		if contentCfg, ok := imageCfgs["content"]; ok {
			switch userConfig.Scope {
			case model.ScopeArticle:
				cfg.Wechat.Article.Content.Image = contentCfg
			case model.ScopeXls:
				cfg.Wechat.Xls.Content.Image = contentCfg
			case model.ScopeRednote:
				if cfg.Rednote == nil {
					cfg.Rednote = &config.RednoteConfig{}
				}
				cfg.Rednote.Content.Image = contentCfg
			}
		}
	}

	return cfg, nil
}

// toZapLogger creates a zap.Logger from a zerolog.Logger.
func toZapLogger(zlog *zerolog.Logger) *zap.Logger {
	// Use zap.NewNop as base; the zerolog logger is the real logger.
	// The app packages require zap.Logger so we provide a minimal wrapper.
	return zap.NewNop()
}

// getImageAPI returns the appropriate ImageAPI config for the given scope.
func getImageAPI(cfg *config.Config, scope string) *config.ImageAPI {
	switch scope {
	case model.ScopeArticle:
		return &cfg.Wechat.Article.Content.Image
	case model.ScopeXls:
		return &cfg.Wechat.Xls.Content.Image
	case model.ScopeRednote:
		if cfg.Rednote != nil {
			return &cfg.Rednote.Content.Image
		}
		return &cfg.Wechat.Xls.Content.Image
	default:
		return &cfg.Wechat.Article.Content.Image
	}
}

// getCoverImageAPI returns the cover ImageAPI config for the given scope.
func getCoverImageAPI(cfg *config.Config, scope string) *config.ImageAPI {
	switch scope {
	case model.ScopeArticle:
		return &cfg.Wechat.Article.Cover.Image
	case model.ScopeXls:
		return &cfg.Wechat.Xls.Cover.Image
	case model.ScopeRednote:
		if cfg.Rednote != nil {
			return &cfg.Rednote.Cover.Image
		}
		return &cfg.Wechat.Xls.Cover.Image
	default:
		return &cfg.Wechat.Article.Cover.Image
	}
}

// CreateMCPTools creates an SDK MCP server with tools that wrap the app/ packages.
func CreateMCPTools(workDir string, userConfig *model.UserConfig, logger *zerolog.Logger) (*claudecode.McpSdkServerConfig, error) {
	cfg, err := BuildAppConfig(userConfig)
	if err != nil {
		return nil, fmt.Errorf("build app config: %w", err)
	}

	scope := userConfig.Scope
	apiCfg := getImageAPI(cfg, scope)
	coverApiCfg := getCoverImageAPI(cfg, scope)
	zapLog := toZapLogger(logger)

	// --- Tool: generate_image ---
	generateImageTool := claudecode.NewTool(
		"generate_image",
		"Generate an AI image with a text prompt. Use this for content images (not covers).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "Image generation prompt (Chinese preferred)"},
				"size":   map[string]any{"type": "string", "description": "Image size in WIDTHxHEIGHT format, e.g. 1728x2304", "default": "1728x2304"},
				"output": map[string]any{"type": "string", "description": "Output file path relative to work directory, e.g. cover.png"},
			},
			"required": []string{"prompt"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			prompt := args["prompt"].(string)
			size := "1728x2304"
			if s, ok := args["size"].(string); ok && s != "" {
				size = s
			}
			output := fmt.Sprintf("img_%d.png", time.Now().UnixNano())
			if o, ok := args["output"].(string); ok && o != "" {
				output = o
			}
			outputPath := filepath.Join(workDir, output)

			processor := appimage.NewProcessor(cfg, apiCfg, zapLog)
			result, err := processor.GenerateOnlyWithSize(prompt, size, outputPath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("generate_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Image generated: %s (size: %s)", result.FilePath, result.Size),
				}},
			}, nil
		},
	)

	// --- Tool: generate_cover_image ---
	generateCoverTool := claudecode.NewTool(
		"generate_cover_image",
		"Generate a cover image for the content. Use the cover image API config (larger size, higher quality).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "Cover image generation prompt"},
				"size":   map[string]any{"type": "string", "description": "Image size, e.g. 2560x1440 for articles or 1728x2304 for vertical", "default": "2560x1440"},
				"output": map[string]any{"type": "string", "description": "Output file path relative to work directory, e.g. cover.png"},
			},
			"required": []string{"prompt"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			prompt := args["prompt"].(string)
			size := "2560x1440"
			if s, ok := args["size"].(string); ok && s != "" {
				size = s
			}
			output := "cover.png"
			if o, ok := args["output"].(string); ok && o != "" {
				output = o
			}
			outputPath := filepath.Join(workDir, output)

			processor := appimage.NewProcessor(cfg, coverApiCfg, zapLog)
			result, err := processor.GenerateOnlyWithSize(prompt, size, outputPath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("generate_cover_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Cover image generated: %s (size: %s)", result.FilePath, result.Size),
				}},
			}, nil
		},
	)

	// --- Tool: generate_batch_images ---
	generateBatchTool := claudecode.NewTool(
		"generate_batch_images",
		"Generate a batch of AI images with a shared prompt. All images are saved to the work directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "Image generation prompt (Chinese preferred)"},
				"count":  map[string]any{"type": "integer", "description": "Number of images to generate", "default": 4},
			},
			"required": []string{"prompt", "count"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			prompt := args["prompt"].(string)
			var count int
			switch v := args["count"].(type) {
			case float64:
				count = int(v)
			case int:
				count = v
			}
			if count <= 0 || count > 20 {
				count = 4
			}

			outputDir := workDir
			processor := appimage.NewProcessor(cfg, apiCfg, zapLog)
			results, err := processor.GenerateBatchOnly(prompt, count, outputDir)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("generate_batch_images failed: %v", err)}},
					IsError: true,
				}, nil
			}

			var lines []string
			for _, r := range results {
				lines = append(lines, fmt.Sprintf("Image %d: %s (size: %s)", r.Index, r.FilePath, r.Size))
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Generated %d images:\n%s", len(results), strings.Join(lines, "\n")),
				}},
			}, nil
		},
	)

	// --- Tool: upload_image ---
	uploadImageTool := claudecode.NewTool(
		"upload_image",
		"Upload a local image to WeChat CDN. Returns media_id and wechat_url.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Path to the image file (absolute or relative to work directory)"},
			},
			"required": []string{"file_path"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			filePath := args["file_path"].(string)
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(workDir, filePath)
			}

			processor := appimage.NewProcessor(cfg, apiCfg, zapLog)
			result, err := processor.UploadLocalImage(filePath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("upload_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Image uploaded. media_id: %s, wechat_url: %s", result.MediaID, result.WechatURL),
				}},
			}, nil
		},
	)

	// --- Tool: compress_image ---
	compressImageTool := claudecode.NewTool(
		"compress_image",
		"Compress an image file to reduce its size while preserving quality.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Path to the image file"},
			},
			"required": []string{"file_path"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			filePath := args["file_path"].(string)
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(workDir, filePath)
			}

			processor := appimage.NewProcessor(cfg, apiCfg, zapLog)
			resultPath, compressed, err := processor.CompressImage(filePath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("compress_image failed: %v", err)}},
					IsError: true,
				}, nil
			}
			if !compressed {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: "Image already within size limits, no compression needed."}},
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("Image compressed: %s", resultPath),
				}},
			}, nil
		},
	)

	// --- Tool: save_file ---
	saveFileTool := claudecode.NewTool(
		"save_file",
		"Write content to a file in the work directory. Creates parent directories if needed.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":     map[string]any{"type": "string", "description": "File path relative to work directory, e.g. content.md"},
				"content":  map[string]any{"type": "string", "description": "Content to write to the file"},
			},
			"required": []string{"path", "content"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			path := args["path"].(string)
			content := args["content"].(string)

			fullPath := filepath.Join(workDir, path)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("mkdir failed: %v", err)}},
					IsError: true,
				}, nil
			}
			if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("write failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: fmt.Sprintf("File saved: %s (%d bytes)", fullPath, len(content)),
				}},
			}, nil
		},
	)

	// --- Tool: read_file ---
	readFileTool := claudecode.NewTool(
		"read_file",
		"Read content from a file in the work directory.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path relative to work directory"},
			},
			"required": []string{"path"},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			path := args["path"].(string)
			fullPath := filepath.Join(workDir, path)

			data, err := os.ReadFile(fullPath)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("read failed: %v", err)}},
					IsError: true,
				}, nil
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: string(data),
				}},
			}, nil
		},
	)

	// --- Tool: list_files ---
	listFilesTool := claudecode.NewTool(
		"list_files",
		"List files and directories in the work directory (or a subdirectory).",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Subdirectory path relative to work directory (empty for root)"},
			},
		},
		func(ctx context.Context, args map[string]any) (*claudecode.McpToolResult, error) {
			dir := workDir
			if p, ok := args["path"].(string); ok && p != "" {
				dir = filepath.Join(workDir, p)
			}

			entries, err := os.ReadDir(dir)
			if err != nil {
				return &claudecode.McpToolResult{
					Content: []claudecode.McpContent{{Type: "text", Text: fmt.Sprintf("list failed: %v", err)}},
					IsError: true,
				}, nil
			}

			var lines []string
			for _, e := range entries {
				if e.IsDir() {
					lines = append(lines, e.Name()+"/")
				} else {
					info, _ := e.Info()
					size := ""
					if info != nil {
						size = fmt.Sprintf(" (%d bytes)", info.Size())
					}
					lines = append(lines, e.Name()+size)
				}
			}
			return &claudecode.McpToolResult{
				Content: []claudecode.McpContent{{
					Type: "text",
					Text: strings.Join(lines, "\n"),
				}},
			}, nil
		},
	)

	// --- Tool: create_article_draft ---
	createArticleDraftTool := claudecode.NewTool(
		"create_article_draft",
		"Create a WeChat article draft from JSON data. Requires title and HTML content.",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":        map[string]any{"type": "string", "description": "Article title"},
				"content":      map[string]any{"type": "string", "description": "HTML content of the article"},
				"author":       map[string]any{"type": "string", "description": "Author name (optional)"},
				"digest":       map[string]any{"type": "string", "description": "Article digest/summary (optional)"},
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

			draftSvc := draft.NewService(cfg, zapLog)
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
							s = filepath.Join(workDir, s)
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

			draftSvc := draft.NewService(cfg, zapLog)
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
				"name":        cfg.Name,
				"keywords":    cfg.Keywords,
				"positioning": cfg.Positioning,
				"scope":       scope,
			}
			switch scope {
			case model.ScopeArticle:
				info["author"] = cfg.Wechat.Article.Author
				info["style"] = cfg.Wechat.Article.Style
				info["theme"] = cfg.Wechat.Article.Theme
			case model.ScopeXls:
				info["style"] = cfg.Wechat.Xls.Style
			case model.ScopeRednote:
				if cfg.Rednote != nil {
					info["style"] = cfg.Rednote.Style
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
				filePath = filepath.Join(workDir, filePath)
			}

			processor := appimage.NewProcessor(cfg, coverApiCfg, zapLog)
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

			draftSvc := draft.NewService(cfg, zapLog)
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

	// Register all tools.
	return claudecode.CreateSDKMcpServer(
		"anbanwriter",
		"1.0.0",
		generateImageTool,
		generateCoverTool,
		generateBatchTool,
		uploadImageTool,
		compressImageTool,
		saveFileTool,
		readFileTool,
		listFilesTool,
		createArticleDraftTool,
		createXlsDraftTool,
		getAccountInfoTool,
		validateCoverTool,
		checkDraftHistoryTool,
	), nil
}
