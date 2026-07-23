package mcp

import (
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

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

// registerImageTools registers image generation, upload, and compression tools.
func registerImageTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "generate_image",
		Description: "Generate one durable task image from a creative prompt and optional ordered references. The server resolves the configured route, registers the generated task file to the current execution, and settles the operation atomically. Terminal file collection occurs after the final workspace manifest and terminal finalization; download_url is the immediate durable handle. Returns the durable asset name, role, download_url, and task-relative file_path.",
		InputSchema: generateImageInputSchema(),
	}, generateImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "upload_image",
		Description: "Upload a server-local image to WeChat CDN or configured storage. When task_id is provided, relative file_path values are resolved against that task workspace. Returns upload metadata.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string", "description": "Project ID (determines WeChat credentials)"},
				"file_path":  map[string]any{"type": "string", "description": "Server-local file path of the image to upload"},
				"task_id":    map[string]any{"type": "string", "description": "Optional task ID; when provided, relative file_path is resolved from the task workspace"},
			},
			"required": []any{"project_id", "file_path"},
		},
	}, uploadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "register_rendered_image",
		Description: "Register one PNG, JPEG, or WebP rendered by an agent as a durable task file. Use image_base64 for agent/client-local bytes or file_path for an absolute server-local file.",
		InputSchema: registerRenderedImageInputSchema(),
	}, registerRenderedImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "compress_image",
		Description: "Compress a server-local image file (resize and re-encode). Returns the server-local path to the compressed file. No credit deduction. When task_id is provided, relative file_path values are resolved against that task workspace.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path": map[string]any{"type": "string", "description": "Server-local file path of the image to compress"},
				"max_width": map[string]any{"type": "integer", "description": "Maximum width in pixels (0 = use server default)", "default": 0},
				"task_id":   map[string]any{"type": "string", "description": "Optional task ID; when provided, relative file_path is resolved from the task workspace"},
			},
			"required": []any{"file_path"},
		},
	}, compressImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "download_image",
		Description: "Download one image URL to a server-local file and return its file_path.",
		InputSchema: downloadImageInputSchema(),
	}, downloadImageHandler)

	server.AddTool(&mcp.Tool{
		Name:        "analyze_image",
		Description: "Analyze an HTTPS image URL or server-local image file with the configured image-understanding model route. Returns the AI analysis and usage; file_path analysis is limited to 10MB.",
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
	if req == nil || req.Params == nil {
		return errorResult("generate_image request parameters are required"), nil
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

	args := parseArgs(req.Params.Arguments)
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
		OutputPath: stringArg(args, "output_path"), Size: stringArg(args, "size"),
		ReferencePaths: referencePaths, Watermark: watermark,
	})
	if err != nil {
		failure := classifyImageToolFailure(parentCtx, ctx, err, "generate", operationTimeout, false)
		if isImageTimeoutFailure(failure.Code) {
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
	Code      string `json:"code"`
	Message   string `json:"message"`
	Stage     string `json:"stage"`
	TimeoutMS int64  `json:"timeout_ms,omitempty"`
	Durable   bool   `json:"durable_task_file"`
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

func resolveTaskWorkspacePath(taskID, filePath string) (string, bool) {
	if svcs == nil || svcs.TaskSvc == nil {
		return filePath, false
	}
	return svcs.TaskSvc.ResolveWorkspacePath(taskID, filePath)
}

func resolveTaskWorkspaceReadablePath(ctx context.Context, taskID, filePath string) (string, func(), error) {
	filePath = strings.TrimSpace(filePath)
	if taskID == "" || filePath == "" || filepath.IsAbs(filePath) {
		return filePath, nil, nil
	}
	if resolved, ok := resolveTaskWorkspacePath(taskID, filePath); ok {
		return resolved, nil, nil
	}
	if svcs == nil || svcs.TaskSvc == nil || svcs.Store == nil || svcs.TaskSvc.Repository() == nil {
		return filePath, nil, nil
	}
	cleanPath, err := service.CleanTaskFileRelativePath(filePath)
	if err != nil {
		return filePath, nil, nil
	}
	taskFile, err := svcs.TaskSvc.Repository().TaskFiles().FindExisting(ctx, taskID, cleanPath)
	if err != nil {
		return "", nil, fmt.Errorf("find task file %s: %w", cleanPath, err)
	}
	if taskFile == nil || strings.TrimSpace(taskFile.OSSKey) == "" {
		return filePath, nil, nil
	}
	data, err := svcs.Store.Read(ctx, taskFile.OSSKey)
	if err != nil {
		return "", nil, fmt.Errorf("read task file %s: %w", cleanPath, err)
	}
	return writeTaskFileTemp(data, cleanPath)
}

func resolveTaskWorkspaceReadablePaths(ctx context.Context, taskID string, filePaths []string) ([]string, func(), error) {
	if len(filePaths) == 0 {
		return nil, nil, nil
	}
	out := make([]string, 0, len(filePaths))
	cleanups := make([]func(), 0)
	for _, filePath := range filePaths {
		resolved, cleanup, err := resolveTaskWorkspaceReadablePath(ctx, taskID, filePath)
		if err != nil {
			for _, fn := range cleanups {
				fn()
			}
			return nil, nil, err
		}
		out = append(out, resolved)
		if cleanup != nil {
			cleanups = append(cleanups, cleanup)
		}
	}
	if len(cleanups) == 0 {
		return out, nil, nil
	}
	return out, func() {
		for _, fn := range cleanups {
			fn()
		}
	}, nil
}

func writeTaskFileTemp(data []byte, logicalPath string) (string, func(), error) {
	ext := strings.ToLower(filepath.Ext(logicalPath))
	if ext == "" {
		ext = ".bin"
	}
	f, err := os.CreateTemp("", "anban-task-file-*"+ext)
	if err != nil {
		return "", nil, fmt.Errorf("create task file temp: %w", err)
	}
	path := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("write task file temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("close task file temp: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func validateImageToolTaskAccess(ctx context.Context, userID, taskID, projectID string) *mcp.CallToolResult {
	if taskID == "" {
		return nil
	}
	if svcs == nil || svcs.TaskSvc == nil {
		return errorResult("task service not available")
	}
	task, err := svcs.TaskSvc.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return errorResult("task not found")
	}
	if userID != "" && task.UserID != userID {
		return errorResult("task does not belong to user")
	}
	if projectID != "" && task.ProjectID != projectID {
		return errorResult("task does not belong to the requested project")
	}
	return nil
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
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}
	if name == "" {
		return errorResult("name is required"), nil
	}
	result, err := svcs.TaskSvc.RegisterRenderedImage(ctx, service.RegisterRenderedImageRequest{
		UserID: userID, ProjectID: projectID, TaskID: taskID, ExecutionID: getExecutionID(ctx),
		Name: name, Role: role, ImageBase64: imageBase64, FilePath: filePath,
	})
	if err != nil {
		if errors.Is(err, service.ErrRenderedImageTaskNotFound) {
			return errorResult("task not found"), nil
		}
		if errors.Is(err, service.ErrRenderedImageProjectMismatch) {
			return errorResult("task does not belong to the requested project"), nil
		}
		return errorResult(fmt.Sprintf("register rendered image: %v", err)), nil
	}
	return textResult(result)
}

func uploadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	userID := getUserID(ctx)
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	filePath, _ := args["file_path"].(string)
	taskID, _ := args["task_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if filePath == "" {
		return errorResult("file_path is required"), nil
	}
	if errResult := validateImageToolTaskAccess(ctx, userID, taskID, projectID); errResult != nil {
		return errResult, nil
	}
	filePath, _ = resolveTaskWorkspacePath(taskID, filePath)

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
	taskID, _ := args["task_id"].(string)
	maxWidth := 0
	if v, ok := args["max_width"].(float64); ok {
		maxWidth = int(v)
	}

	if filePath == "" {
		return errorResult("file_path is required"), nil
	}
	if errResult := validateImageToolTaskAccess(ctx, getUserID(ctx), taskID, ""); errResult != nil {
		return errResult, nil
	}
	filePath, _ = resolveTaskWorkspacePath(taskID, filePath)

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
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"project_id":      map[string]any{"type": "string", "description": "Project ID that owns the task"},
			"task_id":         map[string]any{"type": "string", "description": "Task ID that owns the generated image"},
			"prompt":          map[string]any{"type": "string", "description": "Creative image prompt"},
			"image_type":      map[string]any{"type": "string", "enum": []any{"cover", "content"}, "description": "Semantic asset role"},
			"output_path":     map[string]any{"type": "string", "description": "Task-relative output path, such as output/cover.png"},
			"size":            map[string]any{"type": "string", "description": "Requested aspect ratio, such as 3:4, 16:9, or 1:1"},
			"ref_image_path":  map[string]any{"type": "string", "description": "Optional single task or server-local reference image path"},
			"ref_image_paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional ordered reference image paths"},
			"watermark":       map[string]any{"type": "boolean", "description": "Whether the generated image should include a watermark", "default": false},
		},
		"required": []any{"project_id", "task_id", "prompt", "output_path"},
	}
}

func registerRenderedImageInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"project_id":   map[string]any{"type": "string", "description": "Project ID that owns the task"},
			"task_id":      map[string]any{"type": "string", "description": "Task ID that owns the rendered image"},
			"name":         map[string]any{"type": "string", "description": "Task-relative image name or path"},
			"role":         map[string]any{"type": "string", "enum": []any{"cover", "image", "other"}, "description": "Semantic task-file role"},
			"image_base64": map[string]any{"type": "string", "description": "Base64 image bytes or a data:image URL"},
			"file_path":    map[string]any{"type": "string", "description": "Absolute server-local image path"},
		},
		"required": []any{"project_id", "task_id", "name"},
	}
}

func downloadImageInputSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"project_id": map[string]any{"type": "string", "description": "Project ID used to resolve image download settings"},
			"url":        map[string]any{"type": "string", "description": "Image URL to download"},
		},
		"required": []any{"project_id", "url"},
	}
}

func downloadImageHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.ImageSvc == nil {
		return errorResult("image service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)

	projectID, _ := args["project_id"].(string)
	url, _ := args["url"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	if url == "" {
		return errorResult("url is required"), nil
	}

	result, err := svcs.ImageSvc.DownloadImage(ctx, projectID, url)
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
	if errResult := validateImageToolTaskAccess(ctx, userID, taskID, projectID); errResult != nil {
		return errResult, nil
	}
	var imageSource string

	if filePath != "" {
		filePath, _ = resolveTaskWorkspacePath(taskID, filePath)
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

	providerRequestID := newUnderstandingProviderRequestID(model.OperationImageUnderstanding)
	result, err := svcs.WritingSvc.AnalyzeImageDetailed(ctx, userID, imageSource, prompt)
	if err != nil {
		recordUnderstandingProviderCost(ctx, taskID, model.OperationImageUnderstanding, providerRequestID, nil)
		return errorResult(fmt.Sprintf("analyze image: %v", err)), nil
	}
	recordUnderstandingProviderCost(ctx, taskID, model.OperationImageUnderstanding, providerRequestID, &result.Usage)
	return textResult(map[string]any{
		"analysis": result.Text,
		"usage":    result.Usage,
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
