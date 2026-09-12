package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

// registerImageTools registers image generation, upload, and compression tools.
func registerImageTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "generate_image",
		Description: "Generate one durable task image from a creative prompt and optional ordered references. This is not a guaranteed line-art-only colorize tool. The server resolves the configured route, registers the generated task file to the current execution, and settles the operation atomically. Terminal file collection occurs after the final workspace manifest and terminal finalization; download_url is the immediate durable handle. The returned task-relative file_path is a durable logical path, not a server-local path. Use the download_url across runtime boundaries.",
		InputSchema: generateImageInputSchema(),
	}, generateImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "crop_image",
		Description: "Crop one existing task image to an exact width and height using an explicit anchor, then register the output as a durable task file. This tool performs no platform-specific decisions.",
		InputSchema: cropImageInputSchema(),
	}, cropImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "upload_image",
		Description: "Upload a task-relative image file owned by the current task execution to WeChat CDN or configured storage. Returns upload metadata.",
		InputSchema: map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines WeChat credentials)"},
				"file_path":  map[string]any{"type": "string", "description": "Task-relative path returned by an image tool for the current execution"},
				"task_id":    map[string]any{"type": "string", "description": "Task ID that owns the image"},
			},
			"required": []any{"project_id", "task_id", "file_path"},
		},
	}, uploadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "compress_image",
		Description: "Compress one authorized task-relative image for the current execution and register the result as a durable task file. No server-local path is accepted or returned.",
		InputSchema: compressImageInputSchema(),
	}, compressImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "download_image",
		Description: "Download one public HTTPS raster image with bounded network access and register it as a durable task-relative file for the current execution.",
		InputSchema: downloadImageInputSchema(),
	}, downloadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "analyze_image",
		Description: "Analyze an HTTPS image URL or an authorized task-relative image with the configured image-understanding model route. Task-relative paths may identify a current/published task image, a frozen task input image, or the task reference path. Returns the AI analysis and usage; file_path analysis is limited to 10MB.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines model route context)"},
				"image_url":  map[string]any{"type": "string", "description": "Remote HTTPS URL of the image to analyze"},
				"file_path":  map[string]any{"type": "string", "description": "Authorized task-relative image path; task_id is required; limited to 10MB"},
				"prompt":     map[string]any{"type": "string", "description": "Detailed analysis prompt describing what to analyze"},
				"task_id":    map[string]any{"type": "string", "description": "Optional task ID to associate the image-understanding credit charge with"},
			},
			"required": []any{"project_id", "prompt"},
		},
	}, analyzeImageHandler)
}

func generateImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("generate_image request parameters are required"), nil
	}
	args := parseArgs(req.Params.Arguments)
	if failure := requireMCPExecutionIdentity(ctx, "generate_image", stringArg(args, "project_id"), stringArg(args, "task_id"), ""); failure != nil {
		return failure, nil
	}
	if svcs == nil || svcs.TaskImageSvc == nil {
		return errorResult("task image service not available"), nil
	}
	operationTimeout := 10 * time.Minute
	if svcs.GenerateImageTimeout > 0 {
		operationTimeout = svcs.GenerateImageTimeout
	}
	parentCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	stopHeartbeat := startProgressHeartbeat(ctx, req.Session, req.Params.GetProgressToken(), "generate_image", longTextHeartbeatInterval)
	defer stopHeartbeat()

	referencePaths := parseStringArray(args, "ref_image_paths")
	if len(referencePaths) == 0 {
		if referencePath := stringArg(args, "ref_image_path"); strings.TrimSpace(referencePath) != "" {
			referencePaths = []string{referencePath}
		}
	}
	var watermark *bool
	if value, ok := args["watermark"].(bool); ok {
		watermark = &value
	}
	asset, err := svcs.TaskImageSvc.Generate(ctx, service.GenerateTaskImageRequest{
		UserID: getUserID(ctx), ExecutionID: getExecutionID(ctx),
		TaskID: stringArg(args, "task_id"), ProjectID: stringArg(args, "project_id"),
		Prompt: stringArg(args, "prompt"), ImageType: stringArg(args, "image_type"),
		OutputPath: stringArg(args, "output_path"), AspectRatio: stringArg(args, "aspect_ratio"),
		ReferencePaths: referencePaths, Watermark: watermark,
	})
	if err != nil {
		failure := classifyImageToolFailure(parentCtx, ctx, err, "generate", operationTimeout, false)
		if isImageTimeoutFailure(failure.Code) || failure.Code == "image_ratio_not_allowed" || failure.Code == "image_ratio_mismatch" {
			return imageFailureResult(failure), nil
		}
		return billingError("generate image", err), nil
	}
	if timeoutResult := imageContextFailureResult(parentCtx, ctx, "generate", operationTimeout, true); timeoutResult != nil {
		return timeoutResult, nil
	}
	return textResult(asset)
}

type imageToolFailure struct {
	Code               string   `json:"code"`
	Message            string   `json:"message"`
	Stage              string   `json:"stage"`
	TimeoutMS          int64    `json:"timeout_ms,omitempty"`
	Durable            bool     `json:"durable_task_file"`
	RequestedRatio     string   `json:"requested_ratio,omitempty"`
	AllowedImageRatios []string `json:"allowed_image_ratios,omitempty"`
	TaskImageRatio     string   `json:"task_image_ratio,omitempty"`
	ActualWidth        int      `json:"actual_width,omitempty"`
	ActualHeight       int      `json:"actual_height,omitempty"`
	CapabilityKey      string   `json:"capability_key,omitempty"`
}

func imageFailureResult(failure imageToolFailure) *mcp.CallToolResult {
	payload, err := json.Marshal(failure)
	if err != nil {
		return errorResult(failure.Message)
	}
	return errorResult(string(payload))
}

func classifyImageToolFailure(parentCtx, operationCtx context.Context, err error, stage string, timeout time.Duration, durable bool) imageToolFailure {
	code := categorizeImageGenFailure(err, "")
	var ratioErr *service.ImageRatioNotAllowedError
	if errors.As(err, &ratioErr) {
		return imageToolFailure{
			Code: "image_ratio_not_allowed", Message: ratioErr.Error(), Stage: stage, Durable: durable,
			RequestedRatio: ratioErr.RequestedRatio, AllowedImageRatios: append([]string(nil), ratioErr.AllowedImageRatios...), TaskImageRatio: ratioErr.TaskImageRatio,
		}
	}
	var mismatch *service.ImageRatioMismatchError
	if errors.As(err, &mismatch) {
		return imageToolFailure{
			Code: "image_ratio_mismatch", Message: mismatch.Error(), Stage: stage, Durable: durable,
			RequestedRatio: mismatch.RequestedRatio, ActualWidth: mismatch.ActualWidth, ActualHeight: mismatch.ActualHeight, CapabilityKey: mismatch.CapabilityKey,
		}
	}
	switch {
	case errors.Is(parentCtx.Err(), context.Canceled):
		code = "request_cancelled"
	case errors.Is(operationCtx.Err(), context.DeadlineExceeded):
		code = "operation_timeout"
	case errors.Is(err, context.DeadlineExceeded):
		code = "provider_timeout"
	}
	message := "image generation failed"
	switch code {
	case "request_cancelled":
		message = "generate_image request was cancelled"
	case "operation_timeout":
		message = fmt.Sprintf("generate_image exceeded the server operation timeout of %s", timeout)
	case "provider_timeout":
		message = "image provider attempt exceeded its server timeout"
	case "":
		if err != nil {
			message = err.Error()
		}
	}
	return imageToolFailure{
		Code: code, Message: message, Stage: stage,
		TimeoutMS: timeout.Milliseconds(), Durable: durable,
	}
}

func isImageTimeoutFailure(code string) bool {
	return code == "provider_timeout" || code == "operation_timeout" || code == "request_cancelled"
}

func imageContextFailureResult(parentCtx, operationCtx context.Context, stage string, timeout time.Duration, durable bool) *mcp.CallToolResult {
	err := operationCtx.Err()
	if err == nil {
		return nil
	}
	return imageFailureResult(classifyImageToolFailure(parentCtx, operationCtx, err, stage, timeout, durable))
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
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "create output directory") {
		return "filesystem"
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

func uploadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("upload_image request parameters are required"), nil
	}
	args := parseArgs(req.Params.Arguments)
	if failure := requireMCPExecutionIdentity(ctx, "upload_image", stringArg(args, "project_id"), stringArg(args, "task_id"), ""); failure != nil {
		return failure, nil
	}
	if svcs == nil || svcs.TaskImageOperationsSvc == nil {
		return errorResult("image service not available"), nil
	}
	result, err := svcs.TaskImageOperationsSvc.Upload(ctx, service.UploadTaskImageRequest{
		UserID: getUserID(ctx), ExecutionID: getExecutionID(ctx), ProjectID: stringArg(args, "project_id"),
		TaskID: stringArg(args, "task_id"), FilePath: stringArg(args, "file_path"),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func compressImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("compress_image request parameters are required"), nil
	}
	if svcs == nil || svcs.TaskImageOperationsSvc == nil {
		return errorResult("image service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	maxWidth := 0
	if v, ok := args["max_width"].(float64); ok {
		maxWidth = int(v)
	}
	result, err := svcs.TaskImageOperationsSvc.Compress(ctx, service.CompressTaskImageRequest{
		UserID: getUserID(ctx), ExecutionID: getExecutionID(ctx), TaskID: stringArg(args, "task_id"),
		InputPath: stringArg(args, "input_path"), OutputPath: stringArg(args, "output_path"), MaxWidth: maxWidth,
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func cropImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("crop_image request parameters are required"), nil
	}
	if svcs == nil || svcs.TaskImageOperationsSvc == nil {
		return errorResult("task image service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	targetWidth, targetHeight := 0, 0
	if value, ok := args["target_width"].(float64); ok {
		targetWidth = int(value)
	}
	if value, ok := args["target_height"].(float64); ok {
		targetHeight = int(value)
	}
	result, err := svcs.TaskImageOperationsSvc.Crop(ctx, service.CropTaskImageRequest{
		UserID: getUserID(ctx), ExecutionID: getExecutionID(ctx), TaskID: stringArg(args, "task_id"),
		InputPath: stringArg(args, "input_path"), OutputPath: stringArg(args, "output_path"),
		TargetWidth: targetWidth, TargetHeight: targetHeight, Anchor: stringArg(args, "anchor"),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func generateImageInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"project_id":      map[string]any{"type": "string", "description": "Project ID that owns the task"},
			"task_id":         map[string]any{"type": "string", "description": "Task ID that owns the generated image"},
			"prompt":          map[string]any{"type": "string", "description": "Creative image prompt"},
			"image_type":      map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Semantic asset role"},
			"output_path":     map[string]any{"type": "string", "description": "Task-relative output path, such as output/cover.png"},
			"aspect_ratio":    map[string]any{"type": "string", "description": "Concrete business aspect ratio such as 3:4, 16:9, or 1:1; auto and pixel sizes are not accepted"},
			"ref_image_path":  map[string]any{"type": "string", "description": "Optional single authorized task-relative reference image path"},
			"ref_image_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional ordered authorized task-relative reference image paths"},
			"watermark":       map[string]any{"type": "boolean", "description": "Whether the generated image should include a watermark", "default": false},
		},
		"required": []any{"project_id", "task_id", "prompt", "output_path", "aspect_ratio"},
	}
}

func cropImageInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"task_id":       map[string]any{"type": "string", "description": "Task that owns the input and output files"},
			"input_path":    map[string]any{"type": "string", "description": "Task-relative path of an existing image"},
			"output_path":   map[string]any{"type": "string", "description": "Distinct task-relative output path"},
			"target_width":  map[string]any{"type": "integer", "minimum": 1, "maximum": 8192},
			"target_height": map[string]any{"type": "integer", "minimum": 1, "maximum": 8192},
			"anchor": map[string]any{
				"type": "string", "default": "center",
				"enum": []any{"center", "top", "bottom", "left", "right", "top_left", "top_right", "bottom_left", "bottom_right"},
			},
		},
		"required": []any{"task_id", "input_path", "output_path", "target_width", "target_height"},
	}
}

func compressImageInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"task_id":    map[string]any{"type": "string", "description": "Task that owns the input and output images"},
			"input_path": map[string]any{"type": "string", "description": "Authorized task-relative input image path"},
			"output_path": map[string]any{
				"type": "string", "description": "Distinct task-relative output image path with the same image format as the input",
			},
			"max_width": map[string]any{"type": "integer", "minimum": 0, "maximum": 8192, "description": "Maximum width in pixels (0 = server default)", "default": 0},
		},
		"required": []any{"task_id", "input_path", "output_path"},
	}
}

func downloadImageInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"project_id": map[string]any{"type": "string", "description": "Project that owns the task"},
			"task_id":    map[string]any{"type": "string", "description": "Task that will own the downloaded image"},
			"url":        map[string]any{"type": "string", "description": "Public HTTPS raster image URL"},
			"output_path": map[string]any{
				"type": "string", "description": "Task-relative output path whose extension matches the downloaded image format",
			},
		},
		"required": []any{"project_id", "task_id", "url", "output_path"},
	}
}

func downloadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("download_image request parameters are required"), nil
	}
	if svcs == nil || svcs.TaskImageOperationsSvc == nil {
		return errorResult("image service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	result, err := svcs.TaskImageOperationsSvc.Download(ctx, service.DownloadTaskImageRequest{
		UserID: getUserID(ctx), ExecutionID: getExecutionID(ctx),
		ProjectID: stringArg(args, "project_id"), TaskID: stringArg(args, "task_id"),
		URL: stringArg(args, "url"), OutputPath: stringArg(args, "output_path"),
	})
	if err != nil {
		return errorResult(fmt.Sprintf("download image: %v", err)), nil
	}

	return textResult(result)
}

func analyzeImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req == nil || req.Params == nil {
		return errorResult("analyze_image request parameters are required"), nil
	}
	args := parseArgs(req.Params.Arguments)
	if failure := requireMCPExecutionIdentity(ctx, "analyze_image", stringArg(args, "project_id"), stringArg(args, "task_id"), ""); failure != nil {
		return failure, nil
	}
	if svcs == nil || svcs.TaskImageOperationsSvc == nil {
		return errorResult("image understanding service not available"), nil
	}
	result, err := svcs.TaskImageOperationsSvc.Analyze(ctx, service.AnalyzeTaskImageRequest{
		UserID: getUserID(ctx), ExecutionID: getExecutionID(ctx), ProjectID: stringArg(args, "project_id"), TaskID: stringArg(args, "task_id"),
		ImageURL: stringArg(args, "image_url"), FilePath: stringArg(args, "file_path"), Prompt: stringArg(args, "prompt"),
	})
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}
