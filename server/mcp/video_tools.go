package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

func registerVideoTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "get_project_video_profile",
		Description: "Return the project's video defaults, model policy, global Seedance model catalog, and credit multiplier. Agents must read this before planning video generation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string"},
			},
			"required": []any{"project_id"},
		},
	}, getProjectVideoProfileHandler)

	server.AddTool(&mcp.Tool{
		Name:        "register_video_reference",
		Description: "Register a publicly accessible HTTPS image/audio/video/text reference for Seedance video generation. Local/private URLs are rejected because Ark cannot fetch them; upload media to OSS/CDN first.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":     map[string]any{"type": "string"},
				"type":           map[string]any{"type": "string", "enum": []any{"text", "image_url", "audio_url", "video_url"}},
				"url":            map[string]any{"type": "string"},
				"file_path":      map[string]any{"type": "string"},
				"task_id":        map[string]any{"type": "string"},
				"task_file_id":   map[string]any{"type": "string"},
				"text":           map[string]any{"type": "string"},
				"reference_role": map[string]any{"type": "string"},
				"input_duration_seconds": map[string]any{
					"type":        "number",
					"description": "Ignored for video_url task files; the server measures input video duration from the registered task file.",
				},
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
		Name:        "validate_video_generation_params",
		Description: "Validate video generation parameters against the project video profile and return resolved parameters, suggested params, and estimated credits without calling Ark.",
		InputSchema: videoGenerationInputSchema(),
	}, validateVideoGenerationParamsHandler)

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
		Description: "Download a succeeded provider video URL to server temp storage, upload it to OSS, and register it as a task file when task_id is provided. Agents should not provide local absolute output paths.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":  map[string]any{"type": "string"},
				"task_id":     map[string]any{"type": "string"},
				"video_url":   map[string]any{"type": "string"},
				"output_path": map[string]any{"type": "string"},
				"file_name":   map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "video_url"},
		},
	}, downloadVideoGenerationResultHandler)
}

func getProjectVideoProfileHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if strings.TrimSpace(projectID) == "" {
		return errorResult("project_id is required"), nil
	}
	project, err := mcpVideoProject(ctx, projectID)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	if project.Platform != model.PlatformVideo {
		return errorResult("project is not a video generation project"), nil
	}
	return textResult(map[string]any{
		"video_defaults":       project.VideoDefaults.Data(),
		"video_model_policy":   project.VideoModelPolicy.Data(),
		"model_catalog":        videoModelCatalog(),
		"credit_multiplier":    videoCreditMultiplier(),
		"persistent_file_rule": "all server-persistent references and generated results must be OSS-backed task files; local agent files are temporary only",
	})
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
	taskID, _ := args["task_id"].(string)
	taskFileID, _ := args["task_file_id"].(string)
	textValue, _ := args["text"].(string)
	referenceRole, _ := args["reference_role"].(string)
	var measuredDuration float64
	var taskFile *model.TaskFile
	if strings.TrimSpace(taskFileID) != "" {
		resolvedURL, duration, tf, err := videoReferenceURLFromTaskFile(ctx, taskID, taskFileID, refType)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		urlValue = resolvedURL
		measuredDuration = duration
		taskFile = tf
	}
	if strings.TrimSpace(filePath) != "" {
		if refType == service.VideoReferenceText {
			return errorResult("file_path is only supported for image_url, audio_url, and video_url references"), nil
		}
		uploadedURL, tf, duration, err := uploadVideoReferenceFile(ctx, refType, filePath, taskID)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		urlValue = uploadedURL
		if tf != nil {
			taskFile = tf
			taskFileID = tf.ID
		}
		measuredDuration = duration
	}
	ref := service.VideoReferenceInput{Type: refType, URL: urlValue, Text: textValue, TaskFileID: taskFileID, ReferenceRole: referenceRole, InputDurationSeconds: measuredDuration}
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
	resp := map[string]any{"reference": ref, "ark_url": ref.URL}
	if taskFile != nil {
		resp["task_file"] = taskFile
		resp["task_file_id"] = taskFile.ID
	}
	return textResult(resp)
}

func videoReferenceURLFromTaskFile(ctx context.Context, taskID, taskFileID, refType string) (string, float64, *model.TaskFile, error) {
	if strings.TrimSpace(taskID) == "" {
		return "", 0, nil, fmt.Errorf("task_id is required when task_file_id is used")
	}
	if svcs == nil || svcs.TaskSvc == nil {
		return "", 0, nil, fmt.Errorf("task service not available")
	}
	if err := svcs.TaskSvc.VerifyFileBelongsToTask(ctx, taskID, taskFileID); err != nil {
		return "", 0, nil, err
	}
	stream, tf, err := svcs.TaskSvc.GetFileStream(ctx, taskFileID)
	if err != nil {
		return "", 0, nil, err
	}
	var duration float64
	if refType == service.VideoReferenceVideo {
		duration, err = probeVideoDurationFromReader(ctx, stream, filepath.Base(tf.FileName))
		if closeErr := stream.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			return "", 0, nil, fmt.Errorf("measure input video duration from task file: %w", err)
		}
	} else {
		_ = stream.Close()
	}
	svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
	if strings.TrimSpace(tf.URL) == "" || !strings.HasPrefix(tf.URL, "https://") {
		return "", 0, nil, fmt.Errorf("task file URL is not publicly accessible HTTPS; configure OSS/CDN storage before using it as a video reference")
	}
	return tf.URL, duration, tf, nil
}

func uploadVideoReferenceFile(ctx context.Context, refType, filePath, taskID string) (string, *model.TaskFile, float64, error) {
	if svcs == nil || svcs.Store == nil {
		return "", nil, 0, fmt.Errorf("storage provider not configured: upload the reference to OSS/CDN and pass a public HTTPS URL")
	}
	cleanPath := filepath.Clean(filePath)
	info, err := os.Stat(cleanPath)
	if err != nil {
		return "", nil, 0, fmt.Errorf("read reference file: %w", err)
	}
	if info.IsDir() {
		return "", nil, 0, fmt.Errorf("file_path must point to a file")
	}
	if strings.TrimSpace(taskID) != "" && svcs.TaskSvc != nil {
		f, err := os.Open(cleanPath)
		if err != nil {
			return "", nil, 0, fmt.Errorf("read reference file: %w", err)
		}
		defer f.Close()
		mimeType := service.DetectTaskFileMIME(cleanPath)
		tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, taskID, getUserID(ctx), filepath.Join("references", filepath.Base(cleanPath)), f, mimeType, info.Size())
		if err != nil {
			return "", nil, 0, fmt.Errorf("register video reference task file: %w", err)
		}
		var duration float64
		if refType == service.VideoReferenceVideo {
			stream, _, err := svcs.TaskSvc.GetFileStream(ctx, tf.ID)
			if err != nil {
				return "", nil, 0, fmt.Errorf("read video reference task file: %w", err)
			}
			duration, err = probeVideoDurationFromReader(ctx, stream, filepath.Base(tf.FileName))
			if closeErr := stream.Close(); closeErr != nil && err == nil {
				err = closeErr
			}
			if err != nil {
				return "", nil, 0, fmt.Errorf("measure input video duration from task file: %w", err)
			}
		}
		svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
		if strings.TrimSpace(tf.URL) == "" || !strings.HasPrefix(tf.URL, "https://") {
			return "", nil, 0, fmt.Errorf("uploaded reference URL is not publicly accessible HTTPS; configure OSS/CDN storage or pass an existing public HTTPS URL")
		}
		return tf.URL, tf, duration, nil
	}
	key := videoReferenceStorageKey(getUserID(ctx), refType, filepath.Base(cleanPath), time.Now())
	upload, err := svcs.Store.UploadFile(ctx, key, cleanPath, service.DetectTaskFileMIME(cleanPath))
	if err != nil {
		return "", nil, 0, fmt.Errorf("upload video reference: %w", err)
	}
	if upload == nil || strings.TrimSpace(upload.URL) == "" {
		return "", nil, 0, fmt.Errorf("upload video reference returned no URL")
	}
	if !strings.HasPrefix(upload.URL, "https://") {
		return "", nil, 0, fmt.Errorf("uploaded reference URL is not publicly accessible HTTPS; configure OSS/CDN storage or pass an existing public HTTPS URL")
	}
	return upload.URL, nil, 0, nil
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
	plan, err := resolveMCPVideoPlan(ctx, projectID, videoReq)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(map[string]any{"generation_plan": plan, "sdk_payload_preview": plan.SDKPayloadPreview, "estimated_credits": plan.EstimatedCredits})
}

func validateVideoGenerationParamsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if strings.TrimSpace(projectID) == "" {
		return errorResult("project_id is required"), nil
	}
	videoReq, err := parseVideoGenerationRequest(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	plan, err := resolveMCPVideoPlan(ctx, projectID, videoReq)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(map[string]any{
		"valid":             true,
		"resolved_params":   plan,
		"estimated_credits": plan.EstimatedCredits,
		"pricing_breakdown": plan.PricingBreakdown,
	})
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
	plan, err := resolveMCPVideoPlan(ctx, projectID, videoReq)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	videoReq.Model = plan.Model
	videoReq.Purpose = plan.Purpose
	videoReq.Resolution = plan.Resolution
	videoReq.Ratio = plan.Ratio
	videoReq.Duration = plan.Duration
	videoReq.Watermark = plan.Watermark
	userID := getUserID(ctx)
	if err := maybeDeductVideo(ctx, userID, videoReq.TaskID, plan.EstimatedCredits); err != nil {
		return billingError("create video generation task", err), nil
	}
	result, err := svcs.VideoSvc.CreateTask(ctx, videoReq)
	if err != nil {
		_ = maybeRefundVideoOperation(ctx, videoReq.TaskID)
		return billingError("create video generation task", err), nil
	}
	if err := persistVideoGenerationSubmitted(ctx, projectID, userID, videoReq.TaskID, result, plan); err != nil {
		_ = maybeRefundVideoOperation(ctx, videoReq.TaskID)
		return billingError("persist video generation", err), nil
	}
	return textResult(map[string]any{"task": result, "estimated_credits": plan.EstimatedCredits, "pricing_breakdown": plan.PricingBreakdown})
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
	_ = persistVideoGenerationQuery(ctx, videoTaskID, result)
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
	if strings.TrimSpace(taskID) == "" {
		return errorResult("task_id is required so the generated video can be registered as an OSS task file"), nil
	}
	if svcs == nil || svcs.TaskSvc == nil {
		return errorResult("task service not available: generated videos must be registered as OSS task files"), nil
	}
	if !strings.HasPrefix(videoURL, "https://") {
		return errorResult("video_url must be a publicly accessible HTTPS URL"), nil
	}
	if fileName == "" {
		fileName = fmt.Sprintf("generated-video-%d.mp4", time.Now().Unix())
	}
	if outputPath == "" {
		outputPath = filepath.Join(os.TempDir(), "anban-video-results", fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(fileName)))
	} else if strings.HasSuffix(outputPath, string(os.PathSeparator)) || filepath.Ext(outputPath) == "" {
		outputPath = filepath.Join(outputPath, fileName)
	}
	if err := downloadFile(ctx, videoURL, outputPath, 500<<20); err != nil {
		return errorResult(err.Error()), nil
	}
	resp := map[string]any{"file_path": outputPath}
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
	resp["file_url"] = tf.URL
	_ = persistVideoGenerationDownload(ctx, taskID, videoURL, tf)
	return textResult(resp)
}

func activeVideoService() *service.VideoService {
	if svcs != nil && svcs.VideoSvc != nil {
		return svcs.VideoSvc
	}
	return service.NewVideoService(&srvconfig.VideoAPIConfig{
		BaseURL: srvconfig.DefaultVideoAPIBaseURL,
	})
}

func resolveMCPVideoPlan(ctx context.Context, projectID string, videoReq service.VideoGenerationRequest) (*service.VideoGenerationPlan, error) {
	project, err := mcpVideoProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.Platform != model.PlatformVideo {
		return nil, fmt.Errorf("project is not a video generation project")
	}
	if err := requireMeasuredVideoReferences(videoReq.ReferenceSet); err != nil {
		return nil, err
	}
	plan, err := service.ResolveVideoGenerationPlan(
		videoReq,
		project.VideoDefaults.Data(),
		project.VideoModelPolicy.Data(),
		videoModelCatalog(),
		videoCreditMultiplier(),
	)
	if err != nil {
		return nil, err
	}
	plan.ProjectID = projectID
	sdkPlan, err := activeVideoService().BuildPlan(service.VideoGenerationRequest{
		Prompt:       plan.Prompt,
		Purpose:      plan.Purpose,
		Model:        plan.Model,
		Resolution:   plan.Resolution,
		Ratio:        plan.Ratio,
		Duration:     plan.Duration,
		Seed:         plan.Seed,
		CameraFixed:  plan.CameraFixed,
		Watermark:    plan.Watermark,
		ServiceTier:  plan.ServiceTier,
		ReferenceSet: plan.References,
	}, projectID)
	if err != nil {
		return nil, err
	}
	plan.RequiredArtifacts = sdkPlan.RequiredArtifacts
	plan.SDKPayloadPreview = sdkPlan.SDKPayloadPreview
	return &plan, nil
}

func mcpVideoProject(ctx context.Context, projectID string) (*model.Project, error) {
	if svcs == nil || svcs.ProjectSvc == nil {
		return nil, fmt.Errorf("project service not available")
	}
	project, _, err := svcs.ProjectSvc.Get(ctx, getUserID(ctx), projectID)
	if err != nil {
		return nil, err
	}
	return project, nil
}

func videoCreditMultiplier() int {
	if billSvc != nil && billSvc.config != nil {
		return billSvc.config.VideoAPI.CreditMultiplierOrDefault()
	}
	return 1000
}

func videoModelCatalog() service.VideoModelCatalog {
	if billSvc != nil && billSvc.config != nil {
		return service.VideoModelCatalogFromConfig(billSvc.config.VideoAPI.ModelCatalogOrDefault())
	}
	return service.DefaultVideoModelCatalog()
}

func maybeDeductVideo(ctx context.Context, userID, taskID string, credits int) error {
	if credits <= 0 || billSvc == nil || billSvc.creditSvc == nil || isManagedCall(ctx) {
		return nil
	}
	if taskID != "" && svcs != nil && svcs.TaskSvc != nil {
		if task, err := svcs.TaskSvc.GetByID(ctx, taskID); err == nil && task.Type == model.PlatformVideo && task.VideoCreditsCharged > 0 {
			return nil
		}
	}
	_, err := billSvc.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeVideoGen, credits, videoOperationID(taskID))
	return err
}

func maybeRefundVideoOperation(ctx context.Context, taskID string) error {
	if billSvc == nil || billSvc.creditSvc == nil || taskID == "" {
		return nil
	}
	return billSvc.creditSvc.RefundForOperationByID(ctx, videoOperationID(taskID), "视频生成提交失败退还")
}

func videoOperationID(taskID string) string {
	if strings.TrimSpace(taskID) == "" {
		return fmt.Sprintf("video_gen:%d", time.Now().UnixNano())
	}
	return "video_gen:" + taskID
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
	if v, ok := args["preflight"].(bool); ok {
		req.Preflight = &v
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
		taskFileID, _ := m["task_file_id"].(string)
		referenceRole, _ := m["reference_role"].(string)
		ref := service.VideoReferenceInput{Type: refType, URL: urlValue, Text: textValue, TaskFileID: taskFileID, ReferenceRole: referenceRole}
		if v, ok := numberAsFloat64(m["input_duration_seconds"]); ok {
			ref.InputDurationSeconds = v
		}
		refs = append(refs, ref)
	}
	return refs
}

func requireMeasuredVideoReferences(refs []service.VideoReferenceInput) error {
	for _, ref := range refs {
		if ref.Type != service.VideoReferenceVideo {
			continue
		}
		if strings.TrimSpace(ref.TaskFileID) == "" || ref.InputDurationSeconds <= 0 {
			return fmt.Errorf("video_url references must be registered with register_video_reference using task_file_id so the server can measure input video duration")
		}
	}
	return nil
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

func numberAsFloat64(raw any) (float64, bool) {
	switch v := raw.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

func probeVideoDurationFromReader(ctx context.Context, reader io.Reader, fileName string) (float64, error) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0, fmt.Errorf("ffprobe is required to measure video references")
	}
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext == "" {
		ext = ".mp4"
	}
	tmp, err := os.CreateTemp("", "anban-video-reference-*"+ext)
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := io.Copy(tmp, io.LimitReader(reader, 2<<30)); err != nil {
		_ = tmp.Close()
		return 0, err
	}
	if err := tmp.Close(); err != nil {
		return 0, err
	}
	cmd := exec.CommandContext(ctx, ffprobe, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", tmpPath)
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}
	duration, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("invalid ffprobe duration %q", strings.TrimSpace(string(out)))
	}
	return duration, nil
}

func persistVideoGenerationSubmitted(ctx context.Context, projectID, userID, taskID string, result *service.VideoGenerationCreateResult, plan *service.VideoGenerationPlan) error {
	if result == nil || plan == nil || svcs == nil || svcs.TaskSvc == nil {
		return nil
	}
	repo := svcs.TaskSvc.Repository()
	if repo == nil || repo.VideoGenerations() == nil {
		return nil
	}
	references, err := json.Marshal(plan.References)
	if err != nil {
		return err
	}
	cfg := model.VideoTaskConfig{
		Purpose:          plan.Purpose,
		ModelKey:         plan.ModelKey,
		Model:            plan.Model,
		Resolution:       plan.Resolution,
		Ratio:            plan.Ratio,
		Duration:         plan.Duration,
		Watermark:        plan.Watermark,
		Preflight:        plan.Preflight,
		EstimatedCredits: plan.EstimatedCredits,
		PricingBreakdown: plan.PricingBreakdown,
	}
	gen := &model.VideoGeneration{
		UserID:           userID,
		ProjectID:        projectID,
		TaskID:           taskID,
		ArkTaskID:        result.VideoTaskID,
		Status:           "submitted",
		ResolvedParams:   datatypes.NewJSONType(cfg),
		References:       datatypes.JSON(references),
		PricingBreakdown: datatypes.NewJSONType(*plan.PricingBreakdown),
		CreditsCharged:   plan.EstimatedCredits,
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		return err
	}
	if strings.TrimSpace(taskID) != "" {
		task, err := repo.Tasks().FindByID(ctx, taskID)
		if err == nil {
			task.VideoGenerationID = gen.ID
			task.SetVideoConfig(cfg)
			task.VideoEstimatedCredits = plan.EstimatedCredits
			task.VideoCreditsCharged = plan.EstimatedCredits
			if updateErr := repo.Tasks().Update(ctx, task); updateErr != nil {
				return updateErr
			}
		}
	}
	return nil
}

func persistVideoGenerationQuery(ctx context.Context, arkTaskID string, result *service.VideoGenerationTaskResult) error {
	if result == nil || svcs == nil || svcs.TaskSvc == nil {
		return nil
	}
	repo := svcs.TaskSvc.Repository()
	if repo == nil || repo.VideoGenerations() == nil {
		return nil
	}
	gen, err := repo.VideoGenerations().FindByArkTaskID(ctx, arkTaskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	gen.Status = result.Status
	if result.Error != nil {
		gen.ErrorMessage = result.Error.Message
	}
	urls := map[string]string{
		"video_url":      result.VideoURL,
		"file_url":       result.FileURL,
		"last_frame_url": result.LastFrameURL,
	}
	b, err := json.Marshal(urls)
	if err != nil {
		return err
	}
	gen.ProviderURLs = datatypes.JSON(b)
	return repo.VideoGenerations().Update(ctx, gen)
}

func persistVideoGenerationDownload(ctx context.Context, taskID, providerURL string, tf *model.TaskFile) error {
	if tf == nil || svcs == nil || svcs.TaskSvc == nil {
		return nil
	}
	repo := svcs.TaskSvc.Repository()
	if repo == nil || repo.VideoGenerations() == nil {
		return nil
	}
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		return nil
	}
	var gen *model.VideoGeneration
	if strings.TrimSpace(task.VideoGenerationID) != "" {
		gen, err = repo.VideoGenerations().FindByID(ctx, task.VideoGenerationID)
	} else {
		gen, err = repo.VideoGenerations().FindLatestByTaskID(ctx, taskID)
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	ids := map[string]any{
		"generated_video": tf.ID,
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	gen.TaskFileIDs = datatypes.JSON(b)
	if gen.Status == "" || gen.Status == "submitted" {
		gen.Status = "archived"
	}
	return repo.VideoGenerations().Update(ctx, gen)
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
