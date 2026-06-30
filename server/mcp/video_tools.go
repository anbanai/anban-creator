package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	srvconfig "github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/service"
)

func registerVideoTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "register_video_reference",
		Description: "Register a publicly accessible HTTPS image/audio/video/text reference for Seedance video generation. Local/private URLs are rejected because Ark cannot fetch them; upload media to OSS/CDN first.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string"},
				"type":       map[string]any{"type": "string", "enum": []any{"text", "image_url", "audio_url", "video_url"}},
				"url":        map[string]any{"type": "string"},
				"file_path":  map[string]any{"type": "string"},
				"text":       map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "type"},
		},
	}, registerVideoReferenceHandler)

	server.AddTool(&mcp.Tool{
		Name:        "build_video_generation_plan",
		Description: "Validate commercial video generation inputs and return a deterministic generation-plan artifact plus an Ark SDK payload preview. Does not call the provider or deduct credits.",
		InputSchema: videoGenerationInputSchema(),
	}, buildVideoGenerationPlanHandler)

	server.AddTool(&mcp.Tool{
		Name:        "create_video_generation_task",
		Description: "Create an asynchronous Seedance/Dreamina video generation task through the server-side Volcengine Ark SDK. The server manages API keys, credits, logging, and parameter validation.",
		InputSchema: videoGenerationInputSchema(),
	}, createVideoGenerationTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "query_video_generation_task",
		Description: "Query a server-created Ark content-generation video task by video_task_id. Returns status, generated video URLs, revised prompt, and generation metadata.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":    map[string]any{"type": "string"},
				"video_task_id": map[string]any{"type": "string"},
				"task_id":       map[string]any{"type": "string"},
				"download_dir":  map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "video_task_id"},
		},
	}, queryVideoGenerationTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "download_video_generation_result",
		Description: "Download a succeeded video generation result URL to a server-local path and optionally register it as a task file when task_id is provided.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":  map[string]any{"type": "string"},
				"task_id":     map[string]any{"type": "string"},
				"video_url":   map[string]any{"type": "string"},
				"output_path": map[string]any{"type": "string"},
				"file_name":   map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "video_url", "output_path"},
		},
	}, downloadVideoGenerationResultHandler)
}

func videoGenerationInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project_id":   map[string]any{"type": "string"},
			"prompt":       map[string]any{"type": "string"},
			"purpose":      map[string]any{"type": "string", "enum": []any{"planting", "ecommerce", "lead_gen", "promotion"}},
			"references":   map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"duration":     map[string]any{"type": "integer"},
			"ratio":        map[string]any{"type": "string"},
			"resolution":   map[string]any{"type": "string"},
			"model":        map[string]any{"type": "string"},
			"seed":         map[string]any{"type": "integer"},
			"camera_fixed": map[string]any{"type": "boolean"},
			"watermark":    map[string]any{"type": "boolean"},
			"service_tier": map[string]any{"type": "string"},
			"task_id":      map[string]any{"type": "string"},
		},
		"required": []any{"project_id", "prompt"},
	}
}

func registerVideoReferenceHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if strings.TrimSpace(projectID) == "" {
		return errorResult("project_id is required"), nil
	}
	refType, _ := args["type"].(string)
	urlValue, _ := args["url"].(string)
	filePath, _ := args["file_path"].(string)
	textValue, _ := args["text"].(string)
	if strings.TrimSpace(filePath) != "" {
		if refType == service.VideoReferenceText {
			return errorResult("file_path is only supported for image_url, audio_url, and video_url references"), nil
		}
		uploadedURL, err := uploadVideoReferenceFile(ctx, refType, filePath)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		urlValue = uploadedURL
	}
	ref := service.VideoReferenceInput{Type: refType, URL: urlValue, Text: textValue}
	_, err := activeVideoService().BuildPlan(service.VideoGenerationRequest{
		Prompt:       "reference validation",
		Model:        "validation-model",
		Resolution:   "1080p",
		Ratio:        "9:16",
		Duration:     15,
		ReferenceSet: []service.VideoReferenceInput{ref},
	}, "validation")
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(map[string]any{"reference": ref, "ark_url": ref.URL})
}

func uploadVideoReferenceFile(ctx context.Context, refType, filePath string) (string, error) {
	if svcs == nil || svcs.Store == nil {
		return "", fmt.Errorf("storage provider not configured: upload the reference to OSS/CDN and pass a public HTTPS URL")
	}
	cleanPath := filepath.Clean(filePath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return "", fmt.Errorf("read reference file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("file_path must point to a file")
	}
	key := videoReferenceStorageKey(getUserID(ctx), refType, filepath.Base(cleanPath), time.Now())
	upload, err := svcs.Store.UploadFile(ctx, key, cleanPath, service.DetectTaskFileMIME(cleanPath))
	if err != nil {
		return "", fmt.Errorf("upload video reference: %w", err)
	}
	if upload == nil || strings.TrimSpace(upload.URL) == "" {
		return "", fmt.Errorf("upload video reference returned no URL")
	}
	if !strings.HasPrefix(upload.URL, "https://") {
		return "", fmt.Errorf("uploaded reference URL is not publicly accessible HTTPS; configure OSS/CDN storage or pass an existing public HTTPS URL")
	}
	return upload.URL, nil
}

func videoReferenceStorageKey(userID, refType, fileName string, now time.Time) string {
	if strings.TrimSpace(userID) == "" {
		userID = "anonymous"
	}
	cleanType := strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(refType)
	cleanName := filepath.Base(fileName)
	if cleanName == "." || cleanName == string(os.PathSeparator) || strings.TrimSpace(cleanName) == "" {
		cleanName = "reference"
	}
	return filepath.ToSlash(filepath.Join("uploads", "video-references", userID, cleanType, fmt.Sprintf("%d-%s", now.UnixNano(), cleanName)))
}

func buildVideoGenerationPlanHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	videoReq, err := parseVideoGenerationRequest(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	plan, err := activeVideoService().BuildPlan(videoReq, projectID)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(map[string]any{"generation_plan": plan, "sdk_payload_preview": plan.SDKPayloadPreview})
}

func createVideoGenerationTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.VideoSvc == nil {
		return errorResult("video service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if projectID == "" {
		return errorResult("project_id is required"), nil
	}
	videoReq, err := parseVideoGenerationRequest(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	if err := svcs.VideoSvc.ValidateRequest(videoReq); err != nil {
		return errorResult(err.Error()), nil
	}
	userID := getUserID(ctx)
	if err := maybeDeduct(ctx, userID, model.CreditTypeVideoGen, "volcengine", videoReq.Model, 1); err != nil {
		return billingError("create video generation task", err), nil
	}
	result, err := svcs.VideoSvc.CreateTask(ctx, videoReq)
	if err != nil {
		return billingError("create video generation task", err), nil
	}
	return textResult(result)
}

func queryVideoGenerationTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.VideoSvc == nil {
		return errorResult("video service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	videoTaskID, _ := args["video_task_id"].(string)
	result, err := svcs.VideoSvc.QueryTask(ctx, videoTaskID)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(result)
}

func downloadVideoGenerationResultHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	videoURL, _ := args["video_url"].(string)
	outputPath, _ := args["output_path"].(string)
	taskID, _ := args["task_id"].(string)
	fileName, _ := args["file_name"].(string)
	if videoURL == "" {
		return errorResult("video_url is required"), nil
	}
	if outputPath == "" {
		return errorResult("output_path is required"), nil
	}
	if !strings.HasPrefix(videoURL, "https://") {
		return errorResult("video_url must be a publicly accessible HTTPS URL"), nil
	}
	if strings.HasSuffix(outputPath, string(os.PathSeparator)) || filepath.Ext(outputPath) == "" {
		if fileName == "" {
			fileName = fmt.Sprintf("generated-video-%d.mp4", time.Now().Unix())
		}
		outputPath = filepath.Join(outputPath, fileName)
	}
	if err := downloadFile(ctx, videoURL, outputPath, 500<<20); err != nil {
		return errorResult(err.Error()), nil
	}
	resp := map[string]any{"file_path": outputPath}
	if taskID != "" && svcs != nil && svcs.TaskSvc != nil {
		userID := getUserID(ctx)
		f, err := os.Open(outputPath)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		defer f.Close()
		info, _ := f.Stat()
		tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, taskID, userID, filepath.Base(outputPath), f, "video/mp4", info.Size())
		if err != nil {
			return errorResult(err.Error()), nil
		}
		svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
		resp["task_file"] = tf
	}
	return textResult(resp)
}

func activeVideoService() *service.VideoService {
	if svcs != nil && svcs.VideoSvc != nil {
		return svcs.VideoSvc
	}
	return service.NewVideoService(&srvconfig.VideoAPIConfig{
		Model: "doubao-seedance-2-0",
		Defaults: srvconfig.VideoDefaultsConfig{
			Resolution: "1080p",
			Ratio:      "9:16",
			Duration:   15,
		},
	})
}

func parseVideoGenerationRequest(args map[string]any) (service.VideoGenerationRequest, error) {
	prompt, _ := args["prompt"].(string)
	purpose, _ := args["purpose"].(string)
	modelName, _ := args["model"].(string)
	resolution, _ := args["resolution"].(string)
	ratio, _ := args["ratio"].(string)
	serviceTier, _ := args["service_tier"].(string)
	taskID, _ := args["task_id"].(string)
	req := service.VideoGenerationRequest{
		Prompt:       prompt,
		Purpose:      purpose,
		Model:        modelName,
		Resolution:   resolution,
		Ratio:        ratio,
		ServiceTier:  serviceTier,
		TaskID:       taskID,
		ReferenceSet: parseVideoReferences(args["references"]),
	}
	if v, ok := numberAsInt64(args["duration"]); ok {
		req.Duration = v
	}
	if v, ok := numberAsInt64(args["seed"]); ok {
		req.Seed = &v
	}
	if v, ok := args["camera_fixed"].(bool); ok {
		req.CameraFixed = &v
	}
	if v, ok := args["watermark"].(bool); ok {
		req.Watermark = &v
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return req, fmt.Errorf("prompt is required")
	}
	return req, nil
}

func parseVideoReferences(raw any) []service.VideoReferenceInput {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	refs := make([]service.VideoReferenceInput, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		refType, _ := m["type"].(string)
		urlValue, _ := m["url"].(string)
		textValue, _ := m["text"].(string)
		refs = append(refs, service.VideoReferenceInput{Type: refType, URL: urlValue, Text: textValue})
	}
	return refs
}

func numberAsInt64(raw any) (int64, bool) {
	switch v := raw.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

func downloadFile(ctx context.Context, rawURL, outputPath string, maxBytes int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download video: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download video returned HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}
	f, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer f.Close()
	written, err := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return err
	}
	if written > maxBytes {
		return fmt.Errorf("download video exceeds max size of %d bytes", maxBytes)
	}
	return nil
}
