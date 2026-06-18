package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/model"
)

// registerImageTools registers image generation, upload, and compression tools.
func registerImageTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "generate_image",
		Description: "Generate a single image using the channel's configured image provider (OpenAI DALL-E, Google Gemini, Volcengine Seedream). This is image generation/reference-image generation, not a guaranteed line-art-only colorize tool. The server handles API key management and credit deduction. Returns the download URL (remote CDN URL or data URL), generation metadata (prompt, image_type, provider, model, revised_prompt, response_type, output_mime), and if output_path is provided also saves the image to that server-local path and returns file_path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id":      map[string]any{"type": "string", "description": "Channel ID (determines which image API config to use)"},
				"prompt":          map[string]any{"type": "string", "description": "Image generation prompt"},
				"image_type":      map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Whether to use the cover or content image API config"},
				"output_path":     map[string]any{"type": "string", "description": "Server-local file path to save the generated image (optional, server will download and save). Use a writable server path such as /tmp/...; this is not the agent client's current working directory."},
				"size":            map[string]any{"type": "string", "description": "Image aspect ratio hint (e.g., '3:4', '16:9', '1:1', optionally ':1K/:2K/:4K' where supported). Overrides channel default when provided; providers may still return a different crop/ratio."},
				"ref_image_path":  map[string]any{"type": "string", "description": "Server-local path to a reference image for style consistency (optional). Use file_path returned by generate_image/download_image, not a client-local path."},
				"task_id":         map[string]any{"type": "string", "description": "Task ID (for logging, credit tracking, and per-task image model lookup)"},
				"image_model_key": map[string]any{"type": "string", "description": "Optional image model key selected at task creation time. When provided, overrides user/server defaults for this single call. Resolution: '' = server default; 'custom' = user model-config override (Enterprise only); any other value must match a server-managed image preset key. If task_id is also provided and task_id has its own image_model_key, the explicit image_model_key parameter takes precedence."},
				"watermark":       map[string]any{"type": "boolean", "description": "Enable watermark on generated image (only supported by Volcengine/Seedream)", "default": false},
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
		Name:        "analyze_image",
		Description: "Analyze an image using a vision AI model. Accepts a remote image URL (https://) or a server-local file path (from generate_image/download_image file_path). Returns the AI's analysis as text. Use this for: identifying entities in line art, evaluating coloring quality, auditing cross-image color consistency, verifying line art preservation. file_path analysis is limited to 10MB; for larger images compress_image first or upload_image and retry with image_url. No credit deduction.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"channel_id": map[string]any{"type": "string", "description": "Channel ID (determines vision model config)"},
				"image_url":  map[string]any{"type": "string", "description": "Remote HTTPS URL of the image to analyze"},
				"file_path":  map[string]any{"type": "string", "description": "Server-local file path (from generate_image/download_image file_path result), max 10MB"},
				"prompt":     map[string]any{"type": "string", "description": "Detailed analysis prompt describing what to analyze"},
			},
			"required": []any{"channel_id", "prompt"},
		},
	}, analyzeImageHandler)
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
	imageModelKey, _ := args["image_model_key"].(string)
	var watermark *bool
	if v, ok := args["watermark"].(bool); ok {
		watermark = &v
	}

	// Resolve image_model_key: explicit parameter wins, else fall back to task-level setting.
	// Ownership check prevents leaking another user's task existence via timing/error
	// differences; also stops a caller from inheriting another user's watermark flag.
	if imageModelKey == "" && taskID != "" {
		if t, err := svcs.TaskSvc.GetByID(ctx, taskID); err == nil {
			if t.UserID != userID {
				return errorResult("task not found"), nil
			}
			imageModelKey = t.ImageModelKey
			// Fall back to task-level watermark if not explicitly set.
			if watermark == nil && t.Watermark {
				wm := true
				watermark = &wm
			}
		}
	}

	if mcpLog != nil {
		evt := mcpLog.Info().
			Str("tool", "generate_image").
			Str("task_id", taskID).
			Str("user_id", userID).
			Str("channel_id", channelID).
			Str("image_type", imageType).
			Str("size", size).
			Str("image_model_key", imageModelKey).
			Str("ref_image_path", refPath).
			Str("output_path", outputPath)
		if watermark != nil {
			evt = evt.Bool("watermark", *watermark)
		}
		// prompt may carry the full visual style description; cap at 500 runes
		// to keep logs bounded (style mismatches are still visible in the head).
		promptSnippet := prompt
		if r := []rune(promptSnippet); len(r) > 500 {
			promptSnippet = string(r[:500]) + "...(truncated)"
		}
		evt.Str("prompt_snippet", promptSnippet).
			Msg("MCP generate_image called")
	}

	provider, mdl := resolveImageModel(ctx, userID)
	if err := maybeDeduct(ctx, userID, model.CreditTypeImageGen, provider, mdl, 1); err != nil {
		if mcpLog != nil {
			// billingError below also logs the err with tool name; this entry
			// adds task_id/channel_id/stage so concurrent-task greps can land.
			mcpLog.Warn().
				Str("tool", "generate_image").
				Str("task_id", taskID).
				Str("channel_id", channelID).
				Str("user_id", userID).
				Str("stage", "deduct").
				Err(err).
				Msg("MCP generate_image failed")
		}
		return billingError("generate image", err), nil
	}

	result, err := svcs.ImageSvc.GenerateImage(ctx, userID, channelID, prompt, imageType, outputPath, refPath, taskID, size, imageModelKey, watermark)
	if err != nil {
		if mcpLog != nil {
			mcpLog.Warn().
				Str("tool", "generate_image").
				Str("task_id", taskID).
				Str("channel_id", channelID).
				Str("user_id", userID).
				Str("stage", "generate").
				Str("image_model_key", imageModelKey).
				Err(err).
				Msg("MCP generate_image failed")
		}
		return billingError("generate image", err), nil
	}

	if mcpLog != nil {
		// result.Provider/Model reflect what the provider actually ran (built
		// from its response in image.go buildImageResult), which may differ
		// from resolveImageModel() used for billing at line 168 — e.g. when
		// the channel overrides the user-level config. result.* is the source
		// of truth for "what generated this image".
		// DownloadURL may be a multi-MB base64 data URL; only log its head so
		// log volume stays sane while still identifying provider/protocol.
		urlSnippet := result.DownloadURL
		if r := []rune(urlSnippet); len(r) > 100 {
			urlSnippet = string(r[:100]) + "...(truncated)"
		}
		mcpLog.Info().
			Str("tool", "generate_image").
			Str("task_id", taskID).
			Str("channel_id", channelID).
			Str("provider", result.Provider).
			Str("model", result.Model).
			Str("size", result.Size).
			Str("response_type", result.ResponseType).
			Str("output_mime", result.OutputMIME).
			Str("file_path", result.FilePath).
			Str("revised_prompt", result.RevisedPrompt).
			Str("download_url_snippet", urlSnippet).
			Msg("MCP generate_image succeeded")
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

func analyzeImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing/vision service not available"), nil
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

	imageURL, _ := args["image_url"].(string)
	filePath, _ := args["file_path"].(string)

	var imageSource string

	if filePath != "" {
		info, err := os.Stat(filePath)
		if err != nil {
			return errorResult(fmt.Sprintf("read image file: %v", err)), nil
		}
		if info.Size() > 10<<20 {
			return errorResult("image file is too large for analysis (max 10MB)"), nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return errorResult(fmt.Sprintf("read image file: %v", err)), nil
		}
		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			return errorResult("file is not an image"), nil
		}
		imageSource = fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	} else if imageURL != "" {
		if !strings.HasPrefix(imageURL, "https://") {
			return errorResult("image_url must be an HTTPS URL"), nil
		}
		data, err := downloadHTTPSImage(ctx, imageURL, 10<<20)
		if err != nil {
			return errorResult(fmt.Sprintf("download image: %v", err)), nil
		}
		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			return errorResult("downloaded file is not an image"), nil
		}
		imageSource = fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	} else {
		return errorResult("either image_url or file_path is required"), nil
	}

	result, err := svcs.WritingSvc.AnalyzeImage(ctx, userID, imageSource, prompt)
	if err != nil {
		return errorResult(fmt.Sprintf("analyze image: %v", err)), nil
	}

	return textResult(map[string]any{
		"analysis": result,
	})
}

// downloadHTTPSImage downloads an image from a public HTTPS URL.
func downloadHTTPSImage(ctx context.Context, imageURL string, maxSize int64) ([]byte, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			DialContext: publicOnlyDialContext,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("external image redirects must use https")
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if req.URL.Scheme != "https" || req.URL.Hostname() == "" || req.URL.User != nil {
		return nil, fmt.Errorf("invalid external image URL")
	}
	req.Header.Set("Accept", "image/*")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download external image: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download external image: unexpected status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSize+1))
	if err != nil {
		return nil, fmt.Errorf("read external image: %w", err)
	}
	if int64(len(data)) > maxSize {
		return nil, fmt.Errorf("external image exceeds max size")
	}
	return data, nil
}

// publicOnlyDialContext prevents SSRF by only connecting to public IP addresses.
func publicOnlyDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid address: %w", err)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve host: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("host did not resolve")
	}
	for _, addr := range ips {
		if !isPublicIP(addr.IP) {
			return nil, fmt.Errorf("external image host resolves to a non-public address")
		}
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}

func isPublicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() &&
		!ip.IsPrivate() &&
		!ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() &&
		!ip.IsLinkLocalMulticast() &&
		!ip.IsUnspecified() &&
		!ip.IsMulticast()
}
