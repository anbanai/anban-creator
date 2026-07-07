package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

// registerImageTools registers image generation, upload, and compression tools.
func registerImageTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "generate_image",
		Description: "Generate a single image using the project's configured image provider (OpenAI GPT Image, Google Gemini, Volcengine Seedream). This is image generation/reference-image generation, not a guaranteed line-art-only colorize tool. The server handles API key management and credit deduction. Returns a fetchable download_url (always a storage URL — never an inline base64 data URL), generation metadata (prompt, image_type, provider, model, revised_prompt, response_type, output_mime), and file_path when output_path is provided. When task_id is provided the image is also registered as a task_file in the same call, so list_task_files returns it immediately. When verify_with_vision=true, also runs a post-generation vision check using verification_prompt and returns a verification object {passed, score, missing_entities, notes, raw}. When upload_to_cdn=true, the server ALSO uploads the saved image to the project's CDN (WeChat material library for article projects, returning wechat_url + media_id) in the SAME call, right after a passing vision check — this makes each image durable the moment it is generated and removes the need for a separate, interruptible upload_image step. Upload is skipped when verify_with_vision=true but verification fails (so a rejected image never consumes a material slot); on a post-generation upload failure the response carries upload_error instead of wechat_url so the caller can retry just the upload via upload_image without regenerating.",
		InputSchema: generateImageInputSchema(),
	}, generateImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "upload_image",
		Description: "Upload a server-local image. For WeChat projects, uploads to WeChat CDN; for other platforms, uploads to configured storage. Agent/client-local files must first be made available to the MCP server or generated/downloaded by server-side tools.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines WeChat credentials)"},
				"file_path":  map[string]any{"type": "string", "description": "Server-local file path of the image to upload"},
			},
			"required": []any{"project_id", "file_path"},
		},
	}, uploadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "register_rendered_image",
		Description: "Register an image rendered by an agent from HTML/CSS/Playwright as a task file, optionally uploading it to the project's CDN. Use image_base64 for agent/client-local PNG/JPEG/WebP bytes. Use file_path only for an absolute server-local file path readable by the MCP server. For WeChat article projects, upload_to_cdn=true uploads to the WeChat material library and returns wechat_url + media_id.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":    map[string]any{"type": "string", "description": "Project ID (must match task_id)"},
				"task_id":       map[string]any{"type": "string", "description": "Task ID that owns the rendered image"},
				"name":          map[string]any{"type": "string", "description": "Task-file name/path to store, e.g. cover.png, image_01.png, wechat-21x9-cover.png"},
				"role":          map[string]any{"type": "string", "enum": []any{"cover", "image", "other"}, "description": "Task-file role. Use cover for cover.png/wechat-21x9-cover.png, image for body cards."},
				"image_base64":  map[string]any{"type": "string", "description": "Base64 image bytes or data:image/... URL. Preferred for agent/client-local rendered files."},
				"file_path":     map[string]any{"type": "string", "description": "Absolute server-local file path readable by the MCP server. Do not pass an agent/client-local path; use image_base64 instead."},
				"upload_to_cdn": map[string]any{"type": "boolean", "description": "When true, upload the registered image to the project's CDN. Article projects return wechat_url + media_id.", "default": false},
			},
			"required": []any{"project_id", "task_id", "name"},
		},
	}, registerRenderedImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "compress_image",
		Description: "Compress a server-local image file (resize and re-encode). Returns the server-local path to the compressed file. No credit deduction.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Server-local file path of the image to compress"},
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
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines WeChat credentials)"},
				"url":        map[string]any{"type": "string", "description": "URL of the image to download"},
				"upload":     map[string]any{"type": "boolean", "description": "Upload to WeChat CDN after download", "default": false},
			},
			"required": []any{"project_id", "url"},
		},
	}, downloadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "analyze_image",
		Description: "Analyze an image using the configured image-understanding model route. Accepts a remote image URL (https://) or a server-local file path (from generate_image/download_image file_path). Returns the AI's analysis, token usage, and charged credits. Use this for: identifying entities in line art, evaluating coloring quality, auditing cross-image color consistency, verifying line art preservation. file_path analysis is limited to 10MB; for larger images compress_image first or upload_image and retry with image_url.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines model route context)"},
				"image_url":  map[string]any{"type": "string", "description": "Remote HTTPS URL of the image to analyze"},
				"file_path":  map[string]any{"type": "string", "description": "Server-local file path (from generate_image/download_image file_path result), max 10MB"},
				"prompt":     map[string]any{"type": "string", "description": "Detailed analysis prompt describing what to analyze"},
				"task_id":    map[string]any{"type": "string", "description": "Optional task ID to associate the image-understanding credit charge with"},
			},
			"required": []any{"project_id", "prompt"},
		},
	}, analyzeImageHandler)
}

func generateImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)
	if _, ok := args["image_model_key"]; ok {
		return errorResult("image_model_key is not accepted by generate_image; the server resolves image models from task/project configuration"), nil
	}
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}

	projectID, _ := args["project_id"].(string)
	prompt, _ := args["prompt"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
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
	refPaths := parseStringArray(args, "ref_image_paths")
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	verifyWithVision, _ := args["verify_with_vision"].(bool)
	verificationPrompt, _ := args["verification_prompt"].(string)
	uploadToCDN, _ := args["upload_to_cdn"].(bool)
	var watermark *bool
	if v, ok := args["watermark"].(bool); ok {
		watermark = &v
	}

	if svcs.TaskSvc == nil {
		return errorResult("task service not available"), nil
	}
	t, err := svcs.TaskSvc.GetByID(ctx, taskID)
	if err != nil || t == nil {
		return errorResult("task not found"), nil
	}
	if userID != "" && t.UserID != userID {
		return errorResult("task not found"), nil
	}
	if t.ProjectID != projectID {
		return errorResult("task does not belong to the requested project"), nil
	}
	imageModelKey := t.ImageModelKey
	if watermark == nil && t.Watermark {
		wm := true
		watermark = &wm
	}

	// upload_to_cdn requires a saved local file to upload; validate before
	// billing/generation so a contract violation never wastes a generation.
	if uploadToCDN && outputPath == "" {
		return errorResult("output_path is required when upload_to_cdn is true"), nil
	}

	if mcpLog != nil {
		evt := mcpLog.Info().
			Str("tool", "generate_image").
			Str("task_id", taskID).
			Str("user_id", userID).
			Str("project_id", projectID).
			Str("image_type", imageType).
			Str("size", size).
			Str("image_model_key", imageModelKey).
			Str("ref_image_path", refPath).
			Int("ref_image_paths_count", len(refPaths)).
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

	billingProvider, billingModel, billingSource, err := resolveImageBillingModel(ctx, userID, imageModelKey)
	if err != nil {
		return errorResult(fmt.Sprintf("image model unavailable: %v", err)), nil
	}
	dynamicProvider, dynamicModel, dynamicRoute, dynamicBilling := resolveDynamicImageGenerationBillingRoute(imageType, billingModel, billingSource)
	if !dynamicBilling {
		if err := maybeDeductForResolvedModel(ctx, userID, model.CreditTypeImageGen, billingProvider, billingModel, 1, taskID, billingSource); err != nil {
			if mcpLog != nil {
				// billingError below also logs the err with tool name; this entry
				// adds task_id/project_id/stage so concurrent-task greps can land.
				mcpLog.Warn().
					Str("tool", "generate_image").
					Str("task_id", taskID).
					Str("project_id", projectID).
					Str("user_id", userID).
					Str("stage", "deduct").
					Str("image_model_key", imageModelKey).
					Str("billing_provider", billingProvider).
					Str("billing_model", billingModel).
					Str("billing_source", billingSource).
					Err(err).
					Msg("MCP generate_image failed")
			}
			return billingError("generate image", err), nil
		}
	} else if mcpLog != nil {
		mcpLog.Debug().
			Str("tool", "generate_image").
			Str("task_id", taskID).
			Str("user_id", userID).
			Str("billing_provider", dynamicProvider).
			Str("billing_model", dynamicModel).
			Msg("MCP generate_image defers dynamic usage billing until provider response")
	}

	result, err := svcs.ImageSvc.GenerateImage(ctx, userID, projectID, prompt, imageType, outputPath, refPath, refPaths, taskID, size, imageModelKey, watermark)
	if err != nil {
		if mcpLog != nil {
			mcpLog.Warn().
				Str("tool", "generate_image").
				Str("task_id", taskID).
				Str("project_id", projectID).
				Str("user_id", userID).
				Str("stage", "generate").
				Str("image_model_key", imageModelKey).
				Str("failure_reason", categorizeImageGenFailure(err, refPath)).
				Bool("ref_image_mode", refPath != "").
				Err(err).
				Msg("MCP generate_image failed")
		}
		return billingError("generate image", err), nil
	}

	if dynamicBilling {
		if result.Usage == nil {
			return billingError("generate image", fmt.Errorf("%s usage is required for billing", dynamicModel)), nil
		}
		usage := srvconfig.ImageGenerationUsage{
			Size:                   firstNonEmpty(result.Size, size),
			Count:                  1,
			TextInputTokens:        result.Usage.TextInputTokens,
			TextCachedInputTokens:  result.Usage.TextCachedInputTokens,
			ImageInputTokens:       result.Usage.ImageInputTokens,
			ImageCachedInputTokens: result.Usage.ImageCachedInputTokens,
			ImageOutputTokens:      result.Usage.ImageOutputTokens,
			TotalTokens:            result.Usage.TotalTokens,
			ReferenceImageCount:    len(refPaths),
		}
		if refPath != "" {
			usage.ReferenceImageCount++
		}
		if _, err := maybeDeductImageGenerationUsage(ctx, userID, taskID, dynamicRoute, dynamicProvider, dynamicModel, usage); err != nil {
			return billingError("generate image", err), nil
		}
	}

	// Register the generated image as a task_file so list_task_files returns
	// it mid-run (P2), and rewrite download_url to the file's fetchable
	// storage URL so the LLM-facing response never carries a multi-MB base64
	// data URL for OpenAI/Gemini providers (P1). Skipped for ad-hoc generation
	// (no task_id) and when no file was persisted (no file_path). Soft failure:
	// a registration error is logged but never fails the call — the image is
	// already durable on disk and uploadMissingTaskFiles (task_execution.go)
	// will index it post-execution.
	if taskID != "" && result.FilePath != "" && svcs.TaskSvc != nil {
		if url, tfErr := registerGeneratedImageTaskFile(ctx, svcs.TaskSvc, taskID, userID, result); tfErr != nil {
			if mcpLog != nil {
				mcpLog.Warn().
					Str("tool", "generate_image").
					Str("task_id", taskID).
					Str("user_id", userID).
					Str("file_path", result.FilePath).
					Err(tfErr).
					Msg("MCP generate_image task_file registration failed (download_url left as provider value)")
			}
		} else if url != "" {
			result.DownloadURL = url
		}
	}

	if mcpLog != nil {
		// result.Provider/Model reflect what the provider actually ran (built
		// from its response in image.go buildImageResult), which may differ
		// from resolveImageModel() used for billing at line 168 — e.g. when
		// the project overrides the user-level config. result.* is the source
		// of truth for "what generated this image".
		// On the task path DownloadURL is now a short fetchable storage URL
		// (rewritten above by registerGeneratedImageTaskFile); for ad-hoc
		// generation (no task_id) it may still be a multi-MB base64 data URL.
		// Cap at 100 runes either way so log volume stays bounded.
		urlSnippet := result.DownloadURL
		if r := []rune(urlSnippet); len(r) > 100 {
			urlSnippet = string(r[:100]) + "...(truncated)"
		}
		mcpLog.Info().
			Str("tool", "generate_image").
			Str("task_id", taskID).
			Str("project_id", projectID).
			Str("image_model_key", imageModelKey).
			Str("billing_provider", billingProvider).
			Str("billing_model", billingModel).
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

	// Optional post-generation vision verification. Runs only when the caller
	// explicitly requests it; failures do not block the response — the agent
	// decides whether to retry based on the returned verification object.
	if verifyWithVision {
		if verificationPrompt == "" {
			return errorResult("verification_prompt is required when verify_with_vision is true"), nil
		}
		if svcs.WritingSvc == nil {
			return errorResult("writing/vision service not available for verification"), nil
		}
		verification, vErr := runImageVerification(ctx, userID, taskID, result, verificationPrompt)
		if vErr != nil {
			// Verification failed for operational reasons (vision API down,
			// file unreadable, etc.). Surface as a soft failure: return the
			// image with an error note rather than dropping the generation.
			if mcpLog != nil {
				mcpLog.Warn().
					Str("tool", "generate_image").
					Str("task_id", taskID).
					Str("project_id", projectID).
					Err(vErr).
					Msg("MCP generate_image vision verification failed")
			}
			verification = &service.VisionVerification{
				Passed: false,
				Score:  "unknown",
				Notes:  "verification call failed: " + vErr.Error(),
			}
		}
		result.Verification = verification
		if mcpLog != nil {
			mcpLog.Info().
				Str("tool", "generate_image").
				Str("task_id", taskID).
				Str("project_id", projectID).
				Bool("verification_passed", verification.Passed).
				Str("verification_score", verification.Score).
				Strs("missing_entities", verification.MissingEntities).
				Msg("MCP generate_image vision verification completed")
		}
	}

	// upload_to_cdn: make image upload atomic with generation. Each image
	// becomes durable on the project's CDN the instant it is generated,
	// eliminating the lost-results window of the old separate upload_image
	// batch. Upload is gated on a passing vision check (when requested) so a
	// rejected image never consumes a material slot; a post-generation upload
	// failure surfaces as upload_error so the caller retries just the upload
	// via upload_image without paying for regeneration.
	if shouldUploadAfterVerification(uploadToCDN, verifyWithVision, result.Verification) {
		uploaded, upErr := svcs.ImageSvc.UploadImage(ctx, userID, projectID, result.FilePath)
		if upErr != nil {
			result.UploadError = upErr.Error()
			if mcpLog != nil {
				mcpLog.Warn().
					Str("tool", "generate_image").
					Str("task_id", taskID).
					Str("project_id", projectID).
					Str("file_path", result.FilePath).
					Err(upErr).
					Msg("MCP generate_image CDN upload failed (generation kept; retry upload via upload_image)")
			}
		} else {
			result.WeChatURL = uploaded.WechatURL
			result.MediaID = uploaded.MediaID
			if mcpLog != nil {
				mcpLog.Info().
					Str("tool", "generate_image").
					Str("task_id", taskID).
					Str("project_id", projectID).
					Str("wechat_url", uploaded.WechatURL).
					Msg("MCP generate_image uploaded to CDN")
			}
		}
	} else if uploadToCDN {
		// upload_to_cdn requested but gated off (vision check failed or
		// unavailable). Log so the skip is observable; the caller regenerates
		// with a sharper prompt or re-verifies before retrying the upload.
		if mcpLog != nil {
			mcpLog.Info().
				Str("tool", "generate_image").
				Str("task_id", taskID).
				Str("project_id", projectID).
				Msg("MCP generate_image skipped CDN upload (vision verification did not pass)")
		}
	}

	return textResult(result)
}

// categorizeImageGenFailure classifies a generate-stage error into an actionable
// failure_reason for logs, distinguishing the three failure modes that need
// different operator responses: timeouts (extend/retry), ref-image / i2i mode
// failures (the reference image pins the scene — see Seedream strong-i2i), and
// generic provider errors. Timeout takes precedence since it is the most
// actionable. Used only for diagnostics; it never changes the returned error.
func categorizeImageGenFailure(err error, refPath string) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	if refPath != "" {
		return "ref_image_mode"
	}
	return "provider"
}

// taskFileRegistrar is the subset of TaskService used to register a generated
// image as a task_file. Declared as an interface so generate_image's P1/P2
// logic (rewrite download_url to a fetchable URL + index the file) is unit-
// testable without a full TaskService + storage + repo. *service.TaskService
// satisfies it.
type taskFileRegistrar interface {
	UploadTaskFileFromReader(ctx context.Context, taskID, userID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error)
	EnrichFilesWithURLs(ctx context.Context, files []*model.TaskFile)
}

// registerGeneratedImageTaskFile records the generated image (already saved at
// result.FilePath) as a task_files row, uploading the bytes to storage once
// (UploadTaskFileFromReader dedupes by (taskID, filePath) + content hash), then
// enriches the row to a fetchable URL (signed for OSS without a custom domain,
// or the local /api/v1/files path) and returns it. The caller assigns it to
// ImageResult.DownloadURL, guaranteeing no inline base64 reaches the LLM.
func registerGeneratedImageTaskFile(ctx context.Context, reg taskFileRegistrar, taskID, userID string, result *service.ImageResult) (string, error) {
	info, err := os.Stat(result.FilePath)
	if err != nil {
		return "", fmt.Errorf("stat generated image: %w", err)
	}
	f, err := os.Open(result.FilePath)
	if err != nil {
		return "", fmt.Errorf("open generated image: %w", err)
	}
	defer f.Close()

	mimeType := result.OutputMIME
	if mimeType == "" {
		mimeType = service.DetectTaskFileMIME(result.FilePath)
	}

	tf, err := reg.UploadTaskFileFromReader(ctx, taskID, userID, result.FilePath, f, mimeType, info.Size())
	if err != nil {
		return "", fmt.Errorf("register task file: %w", err)
	}

	reg.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
	if tf.URL != "" {
		return tf.URL, nil
	}
	return tf.OSSURL, nil
}

const maxRenderedImageBytes = 10 * 1024 * 1024

type renderedImageInput struct {
	Name        string
	Role        string
	ImageBase64 string
	FilePath    string
	UploadToCDN bool
}

type renderedImageRegistrationResult struct {
	TaskFileID  string `json:"task_file_id"`
	Name        string `json:"name"`
	Role        string `json:"role"`
	FilePath    string `json:"file_path"`
	MimeType    string `json:"mime_type"`
	FileSize    int64  `json:"file_size"`
	DownloadURL string `json:"download_url,omitempty"`
	WeChatURL   string `json:"wechat_url,omitempty"`
	MediaID     string `json:"media_id,omitempty"`
}

type renderedImageRegistrar interface {
	UploadTaskFileFromReader(ctx context.Context, taskID, userID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error)
	EnrichFilesWithURLs(ctx context.Context, files []*model.TaskFile)
	UpdateTaskFileMetadata(ctx context.Context, file *model.TaskFile, role, mediaID, wechatURL string) (*model.TaskFile, error)
}

type renderedImageUploader interface {
	UploadImage(ctx context.Context, userID, projectID, filePath string) (*service.UploadImageResult, error)
}

func registerRenderedImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.TaskSvc == nil {
		return errorResult("task service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	taskID, _ := args["task_id"].(string)
	name, _ := args["name"].(string)
	role, _ := args["role"].(string)
	imageBase64, _ := args["image_base64"].(string)
	filePath, _ := args["file_path"].(string)
	uploadToCDN, _ := args["upload_to_cdn"].(bool)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if name == "" {
		return errorResult("name is required"), nil
	}
	if uploadToCDN && svcs.ImageSvc == nil {
		return errorResult("image service not available for CDN upload"), nil
	}

	t, err := svcs.TaskSvc.GetByID(ctx, taskID)
	if err != nil || t == nil {
		return errorResult("task not found"), nil
	}
	if userID != "" && t.UserID != userID {
		return errorResult("task not found"), nil
	}
	if t.ProjectID != projectID {
		return errorResult("task does not belong to the requested project"), nil
	}

	result, err := registerRenderedImageAsset(ctx, svcs.TaskSvc, svcs.ImageSvc, userID, projectID, taskID, renderedImageInput{
		Name:        name,
		Role:        role,
		ImageBase64: imageBase64,
		FilePath:    filePath,
		UploadToCDN: uploadToCDN,
	})
	if err != nil {
		return errorResult(fmt.Sprintf("register rendered image: %v", err)), nil
	}
	return textResult(result)
}

func registerRenderedImageAsset(ctx context.Context, reg renderedImageRegistrar, uploader renderedImageUploader, userID, projectID, taskID string, input renderedImageInput) (*renderedImageRegistrationResult, error) {
	if reg == nil {
		return nil, fmt.Errorf("task file registrar is required")
	}
	name, err := service.CleanTaskFileRelativePath(input.Name)
	if err != nil {
		return nil, fmt.Errorf("invalid name: %w", err)
	}
	if strings.Contains(filepath.ToSlash(name), "/../") {
		return nil, fmt.Errorf("invalid name")
	}
	role, err := normalizeRenderedImageRole(input.Role)
	if err != nil {
		return nil, err
	}

	payload, sourcePath, cleanup, err := loadRenderedImagePayload(input)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return nil, err
	}
	if len(payload.data) == 0 {
		return nil, fmt.Errorf("image data is required")
	}
	if len(payload.data) > maxRenderedImageBytes {
		return nil, fmt.Errorf("rendered image is too large (max %d bytes)", maxRenderedImageBytes)
	}
	if err := validateRenderedImageMIME(name, payload.mimeType); err != nil {
		return nil, err
	}
	if role == "" {
		role = service.DetermineTaskFileRole(name, payload.mimeType)
		if role == "" {
			role = model.FileRoleImage
		}
	}

	tf, err := reg.UploadTaskFileFromReader(ctx, taskID, userID, name, bytes.NewReader(payload.data), payload.mimeType, int64(len(payload.data)))
	if err != nil {
		return nil, fmt.Errorf("register task file: %w", err)
	}
	reg.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})

	wechatURL := tf.WechatURL
	mediaID := tf.MediaID
	if input.UploadToCDN {
		if uploader == nil {
			return nil, fmt.Errorf("image uploader is required when upload_to_cdn is true")
		}
		uploaded, err := uploader.UploadImage(ctx, userID, projectID, sourcePath)
		if err != nil {
			return nil, fmt.Errorf("upload rendered image to CDN: %w", err)
		}
		if uploaded != nil {
			if uploaded.WechatURL != "" {
				wechatURL = uploaded.WechatURL
			}
			if uploaded.MediaID != "" {
				mediaID = uploaded.MediaID
			}
		}
	}

	updated, err := reg.UpdateTaskFileMetadata(ctx, tf, role, mediaID, wechatURL)
	if err != nil {
		return nil, err
	}
	if updated != nil {
		tf = updated
	}
	reg.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})

	return &renderedImageRegistrationResult{
		TaskFileID:  tf.ID,
		Name:        name,
		Role:        tf.Role,
		FilePath:    firstNonEmpty(tf.FilePath, name),
		MimeType:    payload.mimeType,
		FileSize:    int64(len(payload.data)),
		DownloadURL: firstNonEmpty(tf.URL, tf.OSSURL),
		WeChatURL:   wechatURL,
		MediaID:     mediaID,
	}, nil
}

type renderedImagePayload struct {
	data     []byte
	mimeType string
}

func loadRenderedImagePayload(input renderedImageInput) (*renderedImagePayload, string, func(), error) {
	hasBase64 := strings.TrimSpace(input.ImageBase64) != ""
	hasPath := strings.TrimSpace(input.FilePath) != ""
	if hasBase64 == hasPath {
		return nil, "", nil, fmt.Errorf("provide exactly one of image_base64 or file_path")
	}
	if hasBase64 {
		data, err := decodeRenderedImageBase64(input.ImageBase64)
		if err != nil {
			return nil, "", nil, err
		}
		if len(data) > maxRenderedImageBytes {
			return nil, "", nil, fmt.Errorf("rendered image is too large (max %d bytes)", maxRenderedImageBytes)
		}
		mimeType := http.DetectContentType(data)
		tempPath, cleanup, err := writeRenderedImageTempFile(data, mimeType)
		if err != nil {
			return nil, "", nil, err
		}
		return &renderedImagePayload{data: data, mimeType: mimeType}, tempPath, cleanup, nil
	}

	cleanPath, err := cleanRenderedServerLocalPath(input.FilePath)
	if err != nil {
		return nil, "", nil, err
	}
	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("stat file_path: %w", err)
	}
	if info.IsDir() {
		return nil, "", nil, fmt.Errorf("file_path must be a file")
	}
	if info.Size() > maxRenderedImageBytes {
		return nil, "", nil, fmt.Errorf("rendered image is too large (max %d bytes)", maxRenderedImageBytes)
	}
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, "", nil, fmt.Errorf("read file_path: %w", err)
	}
	return &renderedImagePayload{data: data, mimeType: http.DetectContentType(data)}, cleanPath, nil, nil
}

func decodeRenderedImageBase64(raw string) ([]byte, error) {
	body := strings.TrimSpace(raw)
	if strings.HasPrefix(body, "data:") {
		comma := strings.Index(body, ",")
		if comma < 0 {
			return nil, fmt.Errorf("invalid data URL")
		}
		body = body[comma+1:]
	}
	data, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("decode image_base64: %w", err)
	}
	return data, nil
}

func cleanRenderedServerLocalPath(raw string) (string, error) {
	path := strings.TrimSpace(raw)
	if path == "" {
		return "", fmt.Errorf("file_path is required")
	}
	if strings.ContainsRune(path, 0) || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "data:") {
		return "", fmt.Errorf("file_path must be an absolute server-local path")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("file_path must be an absolute server-local path")
	}
	return filepath.Clean(path), nil
}

func writeRenderedImageTempFile(data []byte, mimeType string) (string, func(), error) {
	ext := renderedImageExt(mimeType)
	if ext == "" {
		ext = ".img"
	}
	f, err := os.CreateTemp("", "anban-rendered-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("create temp rendered image: %w", err)
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("write temp rendered image: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("close temp rendered image: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func normalizeRenderedImageRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	switch role {
	case "":
		return "", nil
	case model.FileRoleCover, model.FileRoleImage, model.FileRoleOther:
		return role, nil
	default:
		return "", fmt.Errorf("unsupported role %q", role)
	}
}

func validateRenderedImageMIME(name, mimeType string) error {
	if !isAllowedRenderedImageMIME(mimeType) {
		return fmt.Errorf("unsupported image MIME %q", mimeType)
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".webp":
		return nil
	default:
		return fmt.Errorf("unsupported image extension %q", ext)
	}
}

func isAllowedRenderedImageMIME(mimeType string) bool {
	switch mimeType {
	case "image/png", "image/jpeg", "image/webp":
		return true
	default:
		return false
	}
}

func renderedImageExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	default:
		return ""
	}
}

// VisionVerification lives in the service package so it can be a field on
// ImageResult; the mcp handler constructs it via runImageVerification below.

// runImageVerification calls AnalyzeImage on the just-generated image with the
// user-supplied verification_prompt and parses the JSON response into a
// VisionVerification struct. Non-fatal parse issues fall back to score=unknown.
func runImageVerification(ctx context.Context, userID, taskID string, result *service.ImageResult, prompt string) (*service.VisionVerification, error) {
	if svcs.WritingSvc == nil {
		return nil, fmt.Errorf("writing service unavailable")
	}
	if err := preflightUnderstandingTokenBilling(ctx, userID, taskID, model.CreditTypeImageUnderstanding); err != nil {
		return nil, fmt.Errorf("bill image understanding: %w", err)
	}

	var imageSource string
	// Prefer the saved file_path (cheaper, no re-download). Fall back to the
	// download URL for providers that return remote CDN URLs without saving.
	if result.FilePath != "" {
		data, err := os.ReadFile(result.FilePath)
		if err != nil {
			return nil, fmt.Errorf("read generated image: %w", err)
		}
		if len(data) > 10<<20 {
			return nil, fmt.Errorf("generated image too large for vision check (max 10MB)")
		}
		mimeType := http.DetectContentType(data)
		if !strings.HasPrefix(mimeType, "image/") {
			return nil, fmt.Errorf("generated file is not an image")
		}
		imageSource = fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	} else if strings.HasPrefix(result.DownloadURL, "https://") {
		data, err := downloadHTTPSImage(ctx, result.DownloadURL, 10<<20)
		if err != nil {
			return nil, fmt.Errorf("download generated image for verification: %w", err)
		}
		mimeType := http.DetectContentType(data)
		imageSource = fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))
	} else if strings.HasPrefix(result.DownloadURL, "data:image/") {
		// Cap the same 10MB limit as the file_path branch. Base64 encoding
		// inflates by ~33%, so a 10MB image is ~13MB as a data URL.
		if len(result.DownloadURL) > 14<<20 {
			return nil, fmt.Errorf("generated image too large for vision check (data URL exceeds 10MB image equivalent)")
		}
		imageSource = result.DownloadURL
	} else {
		return nil, fmt.Errorf("no accessible image source for verification (need file_path or download_url)")
	}

	analysis, err := svcs.WritingSvc.AnalyzeImageDetailed(ctx, userID, imageSource, prompt)
	if err != nil {
		return nil, fmt.Errorf("analyze image: %w", err)
	}
	if _, err := maybeDeductUnderstandingTokens(ctx, userID, taskID, model.CreditTypeImageUnderstanding, analysis.Usage); err != nil {
		return nil, fmt.Errorf("bill image understanding: %w", err)
	}

	return parseVisionVerificationJSON(analysis.Text), nil
}

// shouldUploadAfterVerification decides whether a generate_image call with
// upload_to_cdn=true proceeds to upload the generated image to the CDN. Upload
// is gated on a passing vision check so a rejected image never consumes a
// material slot. When verify_with_vision is true but no verification object is
// attached (e.g. the vision call errored operationally), we default to NOT
// uploading — the safer of the two options, since the image bytes are still
// saved locally and the caller can re-verify / re-upload explicitly.
func shouldUploadAfterVerification(uploadToCDN, verifyWithVision bool, verification *service.VisionVerification) bool {
	if !uploadToCDN {
		return false
	}
	if !verifyWithVision {
		return true
	}
	if verification == nil {
		return false
	}
	return verification.Passed
}

// parseVisionVerificationJSON extracts the structured fields from the vision
// model's response. Tolerates JSON wrapped in markdown fences or surrounded by
// prose; missing fields default to zero values.
func parseVisionVerificationJSON(raw string) *service.VisionVerification {
	v := &service.VisionVerification{Score: "unknown", Raw: raw}

	// Strip markdown code fences if present.
	body := strings.TrimSpace(raw)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimPrefix(body, "```")
	body = strings.TrimSuffix(body, "```")
	body = strings.TrimSpace(body)

	// Find the first '{' and last '}' to tolerate leading/trailing prose.
	start := strings.Index(body, "{")
	end := strings.LastIndex(body, "}")
	if start < 0 || end <= start {
		// Could not find a JSON object — treat as soft failure.
		v.Notes = "vision response was not valid JSON; raw preserved"
		return v
	}

	var parsed struct {
		AllEntitiesPresent  any      `json:"all_entities_present"`
		MissingEntities     []string `json:"missing_entities"`
		RelevanceScore      string   `json:"relevance_score"`
		HasForbiddenContent bool     `json:"has_forbidden_content"`
		OverallPass         any      `json:"overall_pass"`
		ForbiddenNotes      string   `json:"forbidden_notes"`
		SharperPromptHint   string   `json:"sharper_prompt_hint"`
	}
	if err := json.Unmarshal([]byte(body[start:end+1]), &parsed); err != nil {
		v.Notes = "vision response JSON parse error: " + err.Error()
		return v
	}

	v.MissingEntities = parsed.MissingEntities
	if parsed.RelevanceScore != "" {
		v.Score = strings.ToLower(parsed.RelevanceScore)
	}
	v.Passed = coerceBool(parsed.OverallPass, parsed.AllEntitiesPresent)
	notes := parsed.ForbiddenNotes
	if parsed.SharperPromptHint != "" {
		if notes != "" {
			notes += "; "
		}
		notes += "sharper_prompt_hint: " + parsed.SharperPromptHint
	}
	if parsed.HasForbiddenContent && parsed.ForbiddenNotes != "" {
		v.Passed = false
	}
	v.Notes = notes
	return v
}

// coerceBool returns true if any of the supplied values is truthy. Accepts
// native bool, the strings "true"/"yes", and the integer 1 — vision models
// occasionally wrap a boolean in a string and we don't want to silently flag
// a passing image as failed because of JSON typing drift.
func coerceBool(values ...any) bool {
	for _, v := range values {
		switch x := v.(type) {
		case bool:
			if x {
				return true
			}
		case string:
			if s := strings.ToLower(strings.TrimSpace(x)); s == "true" || s == "yes" {
				return true
			}
		case float64:
			if x == 1 {
				return true
			}
		case int:
			if x == 1 {
				return true
			}
		case int64:
			if x == 1 {
				return true
			}
		}
	}
	return false
}

func uploadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	filePath, _ := args["file_path"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if filePath == "" {
		return errorResult("file_path is required"), nil
	}

	result, err := svcs.ImageSvc.UploadImage(ctx, userID, projectID, filePath)
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

func generateImageInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project_id":          map[string]any{"type": "string", "description": "Project ID (must match task_id; determines project context)"},
			"prompt":              map[string]any{"type": "string", "description": "Image generation prompt"},
			"image_type":          map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Whether to use the cover or content image API config"},
			"output_path":         map[string]any{"type": "string", "description": "Server-local file path to save the generated image (optional, but required when upload_to_cdn=true since the upload reads this file). Use a writable server path such as /tmp/...; this server-local path lives on the MCP server host, not the agent client's current working directory."},
			"size":                map[string]any{"type": "string", "description": "Image aspect ratio hint (e.g., '3:4', '16:9', '1:1', optionally ':1K/:2K/:4K' where supported). Overrides project default when provided; providers may still return a different crop/ratio."},
			"ref_image_path":      map[string]any{"type": "string", "description": "Server-local path to a reference image for style consistency (optional). Use file_path returned by generate_image/download_image, not a client-local path."},
			"ref_image_paths":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Additional server-local reference image paths for multi-reference fidelity (optional). OpenAI/Gemini merge these with ref_image_path (up to ~16 total) as a multi-image edit request. For e-commerce product-photo consistency, pass only the product photos relevant to THIS image's depicted part (per the product-photo list / 产品图清单), not all photos; pair with a named-fidelity prompt naming the exact list index. Volcengine/Seedream only use ref_image_path (or paths[0] if no single ref). Use file_path values returned by generate_image/download_image."},
			"task_id":             map[string]any{"type": "string", "description": "Task ID. The server resolves the image model from this task; agents must not pass model keys."},
			"watermark":           map[string]any{"type": "boolean", "description": "Enable watermark on generated image (only supported by Volcengine/Seedream)", "default": false},
			"verify_with_vision":  map[string]any{"type": "boolean", "description": "When true, after generation the server runs a vision check using verification_prompt against the generated image and returns a verification object. Use this to confirm the image contains the intended entities/matches the chapter content. The image-understanding call is billed by actual usage and associated with task_id.", "default": false},
			"verification_prompt": map[string]any{"type": "string", "description": "Prompt for the post-generation vision check (required when verify_with_vision=true). Should ask the vision model to verify required entities are present and return JSON {all_entities_present, missing_entities, relevance_entities, relevance_score, overall_pass}."},
			"upload_to_cdn":       map[string]any{"type": "boolean", "description": "When true (requires output_path), upload the saved image to the project's CDN in the same call and return wechat_url + media_id on the result. For article projects this uploads to the WeChat material library. Upload runs only after a passing vision check (or when verify_with_vision is false), so rejected images are never uploaded. On upload failure the result carries upload_error instead — retry the upload alone via upload_image, no regeneration needed.", "default": false},
		},
		"required": []any{"project_id", "task_id", "prompt"},
	}
}

func downloadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	url, _ := args["url"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if url == "" {
		return errorResult("url is required"), nil
	}

	upload := "false"
	if v, ok := args["upload"].(bool); ok && v {
		upload = "true"
	}

	result, err := svcs.ImageSvc.DownloadImage(ctx, userID, projectID, url, upload)
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

	projectID, _ := args["project_id"].(string)
	prompt, _ := args["prompt"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if prompt == "" {
		return errorResult("prompt is required"), nil
	}

	imageURL, _ := args["image_url"].(string)
	filePath, _ := args["file_path"].(string)
	taskID, _ := args["task_id"].(string)
	if imageURL == "" && filePath == "" {
		return errorResult("either image_url or file_path is required"), nil
	}
	if err := preflightUnderstandingTokenBilling(ctx, userID, taskID, model.CreditTypeImageUnderstanding); err != nil {
		return billingError("analyze image", err), nil
	}

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
	}

	result, err := svcs.WritingSvc.AnalyzeImageDetailed(ctx, userID, imageSource, prompt)
	if err != nil {
		return errorResult(fmt.Sprintf("analyze image: %v", err)), nil
	}
	creditsCharged, err := maybeDeductUnderstandingTokens(ctx, userID, taskID, model.CreditTypeImageUnderstanding, result.Usage)
	if err != nil {
		return errorResult(fmt.Sprintf("bill image understanding: %v", err)), nil
	}

	return textResult(map[string]any{
		"analysis":        result.Text,
		"usage":           result.Usage,
		"credits_charged": creditsCharged,
	})
}

func resolveDynamicImageGenerationBillingRoute(imageType, modelName, billingSource string) (providerKey, resolvedModel, routeName string, ok bool) {
	if billSvc == nil || billSvc.config == nil {
		return "", "", "", false
	}
	var route srvconfig.ImageGenerationRouteConfig
	switch {
	case strings.HasPrefix(billingSource, "preset:"):
		presetKey := strings.TrimPrefix(billingSource, "preset:")
		for _, preset := range billSvc.config.ImagePresets {
			if preset.Key != presetKey || preset.ProviderRoute == "" {
				continue
			}
			var found bool
			route, found = imageGenerationRouteByPathForMCP(preset.ProviderRoute)
			if !found {
				return "", "", "", false
			}
			routeName = strings.TrimPrefix(strings.TrimSpace(preset.ProviderRoute), "model_routes.")
			break
		}
	case billingSource == "system_default" || billingSource == "":
		if imageType == "cover" {
			route = billSvc.config.ModelRoutes.ImageGeneration.Cover
			routeName = "image_generation.cover"
		} else {
			route = billSvc.config.ModelRoutes.ImageGeneration.Content
			routeName = "image_generation.content"
		}
	default:
		return "", "", "", false
	}
	if route.Provider == "" {
		return "", "", "", false
	}
	if route.Model == "" {
		route.Model = modelName
	}
	if route.Model == "" {
		return "", "", "", false
	}
	price, exists := billSvc.config.ModelPrices.ImageGeneration[route.Provider+"/"+route.Model]
	if !exists || price.PricingType != srvconfig.ImagePricingTypeOpenAIUsage {
		return "", "", "", false
	}
	return route.Provider, route.Model, routeName, true
}

func imageGenerationRouteByPathForMCP(path string) (srvconfig.ImageGenerationRouteConfig, bool) {
	if billSvc == nil || billSvc.config == nil {
		return srvconfig.ImageGenerationRouteConfig{}, false
	}
	path = strings.TrimPrefix(strings.TrimSpace(path), "model_routes.")
	switch path {
	case "image_generation.cover":
		return billSvc.config.ModelRoutes.ImageGeneration.Cover, true
	case "image_generation.content":
		return billSvc.config.ModelRoutes.ImageGeneration.Content, true
	}
	const designerPrefix = "image_generation.designer."
	if strings.HasPrefix(path, designerPrefix) {
		route, ok := billSvc.config.ModelRoutes.ImageGeneration.Designer[strings.TrimPrefix(path, designerPrefix)]
		return route, ok
	}
	return srvconfig.ImageGenerationRouteConfig{}, false
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
