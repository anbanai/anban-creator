package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/model"
)

// registerImageTools registers image generation, upload, and compression tools.
func registerImageTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "generate_image",
		Description: "Generate a single image using the channel's configured image provider (OpenAI DALL-E, Google Gemini, Volcengine Seedream). The server handles API key management and credit deduction. Returns the download URL (remote CDN URL or data URL) of the generated image. If output_path is provided, the server also saves the image to that path and returns file_path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id":     map[string]any{"type": "string", "description": "Channel ID (determines which image API config to use)"},
				"prompt":         map[string]any{"type": "string", "description": "Image generation prompt"},
				"image_type":     map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Whether to use the cover or content image API config"},
				"output_path":    map[string]any{"type": "string", "description": "Local file path to save the generated image (optional, server will download and save)"},
				"size":           map[string]any{"type": "string", "description": "Image size/aspect ratio (e.g., '3:4', '16:9', '1:1'). Overrides channel default when provided."},
				"ref_image_path": map[string]any{"type": "string", "description": "Path to a reference image for style consistency (optional)"},
				"task_id":        map[string]any{"type": "string", "description": "Task ID (for logging and credit tracking)"},
			},
			"required": []any{"channel_id", "prompt"},
		},
	}, generateImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "upload_image",
		Description: "Upload a local image. For WeChat channels, uploads to WeChat CDN; for other platforms, uploads to configured storage. Returns the URL.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID (determines WeChat credentials)"},
				"file_path":  map[string]any{"type": "string", "description": "Local file path of the image to upload"},
			},
			"required": []any{"channel_id", "file_path"},
		},
	}, uploadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "compress_image",
		Description: "Compress a local image file (resize and re-encode). Returns the path to the compressed file. No credit deduction.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Local file path of the image to compress"},
				"max_width": map[string]any{"type": "integer", "description": "Maximum width in pixels (0 = use server default)", "default": 0},
			},
			"required": []any{"file_path"},
		},
	}, compressImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "download_image",
		Description: "Download an image from a URL, optionally uploading to WeChat CDN. Returns the local file path (download-only) or WeChat CDN URL (with upload).",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID (determines WeChat credentials)"},
				"url":        map[string]any{"type": "string", "description": "URL of the image to download"},
				"upload":     map[string]any{"type": "boolean", "description": "Upload to WeChat CDN after download", "default": false},
			},
			"required": []any{"channel_id", "url"},
		},
	}, downloadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "generate_images_from_markdown",
		Description: "Extract AI image placeholders (__generate:prompt__) from Markdown content, generate all images, and optionally upload them. Returns download URLs (remote CDN URLs or data URLs) and/or CDN URLs. Requires task_id for logging and credit tracking.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id":   map[string]any{"type": "string", "description": "Channel ID (determines image API config)"},
				"markdown":     map[string]any{"type": "string", "description": "Markdown content containing AI image placeholders"},
				"image_type":   map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Whether to use cover or content image API config", "default": "content"},
				"style_prompt": map[string]any{"type": "string", "description": "Style prompt prepended to all image prompts (optional)"},
				"upload":       map[string]any{"type": "boolean", "description": "Upload generated images to WeChat CDN", "default": false},
				"task_id":      map[string]any{"type": "string", "description": "Task ID (required, for logging and credit tracking)"},
			},
			"required": []any{"channel_id", "markdown", "task_id"},
		},
	}, batchGenerateFromMarkdownHandler)
}

func generateImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	prompt, _ := args["prompt"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if prompt == "" {
		return errorResult("prompt is required"), nil
	}

	imageType, _ := args["image_type"].(string)
	if imageType == "" {
		imageType = "content"
	}
	outputPath, _ := args["output_path"].(string)
	size, _ := args["size"].(string)
	refPath, _ := args["ref_image_path"].(string)
	taskID, _ := args["task_id"].(string)

	provider, mdl := resolveImageModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeImageGen, provider, mdl, 1); err != nil {
		return billingError("generate image", err), nil
	}

	result, err := svcs.ImageSvc.GenerateImage(ctx, userID, channelID, prompt, imageType, outputPath, refPath, taskID, size)
	if err != nil {
		return billingError("generate image", err), nil
	}

	return textResult(result)
}

func uploadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	filePath, _ := args["file_path"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if filePath == "" {
		return errorResult("file_path is required"), nil
	}

	result, err := svcs.ImageSvc.UploadImage(ctx, userID, channelID, filePath)
	if err != nil {
		return errorResult(fmt.Sprintf("upload image: %v", err)), nil
	}

	return textResult(result)
}

func compressImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)

	filePath, _ := args["file_path"].(string)
	maxWidth := 0
	if v, ok := args["max_width"].(float64); ok {
		maxWidth = int(v)
	}

	if filePath == "" {
		return errorResult("file_path is required"), nil
	}

	compressedPath, compressed, err := svcs.ImageSvc.CompressImage(filePath, maxWidth)
	if err != nil {
		return errorResult(fmt.Sprintf("compress image: %v", err)), nil
	}

	return textResult(map[string]any{
		"file_path":  compressedPath,
		"compressed": compressed,
	})
}

func downloadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	url, _ := args["url"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if url == "" {
		return errorResult("url is required"), nil
	}

	upload := "false"
	if v, ok := args["upload"].(bool); ok && v {
		upload = "true"
	}

	result, err := svcs.ImageSvc.DownloadImage(ctx, userID, channelID, url, upload)
	if err != nil {
		return errorResult(fmt.Sprintf("download image: %v", err)), nil
	}

	return textResult(result)
}

func batchGenerateFromMarkdownHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	markdown, _ := args["markdown"].(string)
	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if markdown == "" {
		return errorResult("markdown is required"), nil
	}

	imageType, _ := args["image_type"].(string)
	if imageType == "" {
		imageType = "content"
	}
	stylePrompt, _ := args["style_prompt"].(string)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	upload := false
	if v, ok := args["upload"].(bool); ok {
		upload = v
	}

	result, err := svcs.ImageSvc.BatchGenerateFromMarkdown(ctx, userID, channelID, markdown, imageType, stylePrompt, upload, taskID)
	if err != nil {
		return errorResult(fmt.Sprintf("batch generate from markdown: %v", err)), nil
	}

	return textResult(result)
}
