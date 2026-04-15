package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerImageTools registers image generation, upload, and compression tools.
func registerImageTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "generate_image",
		Description: "Generate a single image using the channel's configured image provider (OpenAI DALL-E, Google Gemini, Volcengine Seedream, or OpenRouter). The server handles API key management and credit deduction. Returns the local file path of the generated image.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id":    map[string]any{"type": "string", "description": "Channel ID (determines which image API config to use)"},
				"prompt":        map[string]any{"type": "string", "description": "Image generation prompt"},
				"image_type":    map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Whether to use the cover or content image API config"},
				"output_path":   map[string]any{"type": "string", "description": "Local file path to save the image (optional, generates to temp dir if empty)"},
				"ref_image_path": map[string]any{"type": "string", "description": "Path to a reference image for style consistency (optional)"},
			},
			"required": []any{"channel_id", "prompt"},
		},
	}, generateImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "generate_batch_images",
		Description: "Generate multiple images at once using the channel's configured image provider. Returns an array of local file paths for the generated images.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id":     map[string]any{"type": "string", "description": "Channel ID"},
				"prompt":         map[string]any{"type": "string", "description": "Base prompt for all images"},
				"count":          map[string]any{"type": "integer", "description": "Number of images to generate (1-20)", "minimum": 1, "maximum": 20},
				"image_type":     map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Cover or content image API config"},
				"output_dir":     map[string]any{"type": "string", "description": "Directory to save generated images"},
				"ref_image_path": map[string]any{"type": "string", "description": "Path to reference image for style consistency (optional)"},
			},
			"required": []any{"channel_id", "prompt", "count", "output_dir"},
		},
	}, generateBatchImagesHandler)

	server.AddTool(&mcp.Tool{
		Name:        "upload_image",
		Description: "Upload a local image to the WeChat CDN (or configured storage). Returns the CDN URL and media ID.",
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
	refPath, _ := args["ref_image_path"].(string)

	result, err := svcs.ImageSvc.GenerateImage(ctx, userID, channelID, prompt, imageType, outputPath, refPath)
	if err != nil {
		return errorResult(fmt.Sprintf("generate image: %v", err)), nil
	}

	return textResult(result)
}

func generateBatchImagesHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	channelID, _ := args["channel_id"].(string)
	prompt, _ := args["prompt"].(string)
	count := 1
	if v, ok := args["count"].(float64); ok && int(v) > 0 {
		count = int(v)
	}
	outputDir, _ := args["output_dir"].(string)
	imageType, _ := args["image_type"].(string)
	refPath, _ := args["ref_image_path"].(string)

	if channelID == "" {
		return errorResult("channel_id is required"), nil
	}
	if prompt == "" {
		return errorResult("prompt is required"), nil
	}
	if outputDir == "" {
		return errorResult("output_dir is required"), nil
	}
	if imageType == "" {
		imageType = "content"
	}

	result, err := svcs.ImageSvc.GenerateBatch(ctx, userID, channelID, prompt, imageType, count, outputDir, refPath)
	if err != nil {
		return errorResult(fmt.Sprintf("batch generate: %v", err)), nil
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
