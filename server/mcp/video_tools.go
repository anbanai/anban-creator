package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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
		Name:        "register_video_reference",
		Description: "Register a publicly accessible HTTPS image/audio/video/text reference for Seedance video generation. Local/private URLs are rejected because Ark cannot fetch them; upload media to OSS/CDN first. file_path is server-local only.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":     map[string]any{"type": "string"},
				"type":           map[string]any{"type": "string", "enum": []any{"text", "image_url", "audio_url", "video_url"}},
				"url":            map[string]any{"type": "string"},
				"file_path":      map[string]any{"type": "string", "description": "Server-local media path only; agent/client-local files should be uploaded or registered as task files first."},
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
		Name:        "analyze_video_reference",
		Description: "Analyze an OSS/CDN video reference with the configured native video understanding model before video creation. The model must understand the whole video directly; no sampled-frame fallback is performed. Returns structured video-understanding JSON and registers video-understanding.json when task_id is provided.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":      map[string]any{"type": "string"},
				"task_id":         map[string]any{"type": "string"},
				"task_file_id":    map[string]any{"type": "string"},
				"video_url":       map[string]any{"type": "string"},
				"reference_role":  map[string]any{"type": "string"},
				"purpose_hint":    map[string]any{"type": "string"},
				"analysis_prompt": map[string]any{"type": "string"},
			},
			"required": []any{"project_id"},
		},
	}, analyzeVideoReferenceHandler)

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
		Name:        "create_video_generation_job",
		Description: "Create a target-duration video generation job. The server resolves target duration, splits provider-bounded segments, deducts total credits once, submits one provider task per segment, and records segment state.",
		InputSchema: videoGenerationInputSchema(),
	}, createVideoGenerationJobHandler)

	server.AddTool(&mcp.Tool{
		Name:        "query_video_generation_job",
		Description: "Query all provider segments for a server-created video generation job. Accepts video_generation_id or task_id and returns aggregate status plus segment statuses.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":          map[string]any{"type": "string"},
				"task_id":             map[string]any{"type": "string"},
				"video_generation_id": map[string]any{"type": "string"},
			},
		},
	}, queryVideoGenerationJobHandler)

	server.AddTool(&mcp.Tool{
		Name:        "download_video_generation_results",
		Description: "Download one or more succeeded segment video URLs, register each segment file, and mark a single-segment result as final_video. Multi-segment jobs should call compose_video_segments after all segments are downloaded.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":          map[string]any{"type": "string"},
				"task_id":             map[string]any{"type": "string"},
				"video_generation_id": map[string]any{"type": "string"},
				"segments":            map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			},
			"required": []any{"project_id", "task_id", "segments"},
		},
	}, downloadVideoGenerationResultsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "compose_video_segments",
		Description: "Register an already composed final.mp4 for a multi-segment job as the final_video task file. The agent composes locally with ffmpeg and passes file_path.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":          map[string]any{"type": "string"},
				"task_id":             map[string]any{"type": "string"},
				"video_generation_id": map[string]any{"type": "string"},
				"file_path":           map[string]any{"type": "string", "description": "Server-local or agent-workspace local final.mp4 path to upload; not a browser/client path."},
				"file_name":           map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "task_id", "file_path"},
		},
	}, composeVideoSegmentsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "validate_video_delivery",
		Description: "Validate that a video generation job has a registered final_video task file and return segment/final delivery metadata.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id":          map[string]any{"type": "string"},
				"task_id":             map[string]any{"type": "string"},
				"video_generation_id": map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "task_id"},
		},
	}, validateVideoDeliveryHandler)
}

func videoGenerationInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project_id":               map[string]any{"type": "string"},
			"prompt":                   map[string]any{"type": "string"},
			"purpose":                  map[string]any{"type": "string", "enum": []any{"planting", "ecommerce", "lead_gen", "promotion"}},
			"creative_type":            map[string]any{"type": "string", "enum": []any{"personal_ip", "high_efficiency_joke", "product_demo", "brand_promo", "custom"}},
			"subject_profile":          map[string]any{"type": "string"},
			"audience":                 map[string]any{"type": "string"},
			"single_message":           map[string]any{"type": "string"},
			"references":               map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
			"duration":                 map[string]any{"type": "integer"},
			"planned_duration_seconds": map[string]any{"type": "integer"},
			"target_duration_reason":   map[string]any{"type": "string"},
			"ratio":                    map[string]any{"type": "string"},
			"resolution":               map[string]any{"type": "string"},
			"seed":                     map[string]any{"type": "integer"},
			"camera_fixed":             map[string]any{"type": "boolean"},
			"watermark":                map[string]any{"type": "boolean"},
			"service_tier":             map[string]any{"type": "string"},
			"task_id":                  map[string]any{"type": "string"},
		},
		"required": []any{"project_id", "task_id", "prompt"},
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
	if duration, ok := numberAsFloat64(args["input_duration_seconds"]); ok && duration > 0 {
		measuredDuration = duration
	}
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

func analyzeVideoReferenceHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.WritingSvc == nil {
		return errorResult("writing/vision service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	if strings.TrimSpace(projectID) == "" {
		return errorResult("project_id is required"), nil
	}
	taskID, _ := args["task_id"].(string)
	taskFileID, _ := args["task_file_id"].(string)
	videoURL, _ := args["video_url"].(string)
	referenceRole, _ := args["reference_role"].(string)
	purposeHint, _ := args["purpose_hint"].(string)
	analysisPrompt, _ := args["analysis_prompt"].(string)
	if strings.TrimSpace(taskFileID) != "" {
		resolved, _, _, err := videoReferenceURLFromTaskFile(ctx, taskID, taskFileID, service.VideoReferenceVideo)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		videoURL = resolved
	}
	if err := service.ValidatePublicHTTPSURLForVideoReference(videoURL); err != nil {
		return errorResult(err.Error()), nil
	}
	userID := getUserID(ctx)
	if err := preflightUnderstandingTokenBilling(ctx, userID, taskID, model.CreditTypeVideoUnderstanding); err != nil {
		return billingError("analyze video reference", err), nil
	}
	prompt := buildVideoUnderstandingPrompt(referenceRole, purposeHint, analysisPrompt)
	analysis, err := svcs.WritingSvc.AnalyzeVideoURLDetailed(ctx, userID, videoURL, prompt)
	analysisMode := "native_video"
	metadata := map[string]any{"source_url": videoURL}
	if err != nil {
		return errorResult("analyze video reference: " + err.Error()), nil
	}
	understanding := normalizeVideoUnderstanding(analysis.Text)
	understanding["metadata"] = metadata
	understanding["model"] = analysis.Model
	understanding["analysis_mode"] = analysisMode
	understanding["source_url"] = videoURL
	understanding["reference_role"] = strings.TrimSpace(referenceRole)
	understanding["purpose_hint"] = strings.TrimSpace(purposeHint)
	understanding["usage"] = analysis.Usage
	creditsCharged, err := maybeDeductUnderstandingTokens(ctx, userID, taskID, model.CreditTypeVideoUnderstanding, analysis.Usage)
	if err != nil {
		return errorResult("bill video understanding: " + err.Error()), nil
	}
	understanding["credits_charged"] = creditsCharged

	resp := map[string]any{
		"analysis_mode":            analysisMode,
		"video_understanding":      understanding,
		"video_understanding_file": "video-understanding.json",
		"usage":                    analysis.Usage,
		"credits_charged":          creditsCharged,
	}
	if strings.TrimSpace(taskID) != "" && svcs.TaskSvc != nil {
		payload, _ := json.MarshalIndent(understanding, "", "  ")
		reader := strings.NewReader(string(payload))
		tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, taskID, getUserID(ctx), "video-understanding.json", reader, "application/json", int64(len(payload)))
		if err != nil {
			return errorResult("register video-understanding.json: " + err.Error()), nil
		}
		svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
		resp["task_file"] = tf
		resp["task_file_id"] = tf.ID
	}
	return textResult(resp)
}

func buildVideoUnderstandingPrompt(referenceRole, purposeHint, extra string) string {
	var b strings.Builder
	b.WriteString("你是资深短视频导演和多模态视频理解专家。请完整理解这个参考视频，不要只总结文案或录音。")
	b.WriteString("从画面、人物、表情、动作、场景、镜头运动、节奏、主体一致性、可复刻点和不可改变点分析。")
	if strings.TrimSpace(referenceRole) != "" {
		fmt.Fprintf(&b, "\nreference_role: %s", referenceRole)
	}
	if strings.TrimSpace(purposeHint) != "" {
		fmt.Fprintf(&b, "\npurpose_hint: %s", purposeHint)
	}
	if strings.TrimSpace(extra) != "" {
		fmt.Fprintf(&b, "\nuser_analysis_prompt: %s", extra)
	}
	b.WriteString(`

Return strict JSON only with these fields:
{
  "visual_summary": "one concise but specific visual summary",
  "timeline": [{"time_range":"0-3s","visual":"","action":"","expression":"","camera":"","rhythm":"","creative_function":""}],
  "subjects": [],
  "people": [{"role":"","appearance":"","expression":"","gesture":"","consistency_anchors":[]}],
  "expressions": [],
  "actions": [],
  "scenes": [],
  "camera_motion": [],
  "rhythm": [],
  "must_keep": [],
  "can_change": [],
  "must_not_change": [],
  "planning_hints": []
}`)
	return b.String()
}

func normalizeVideoUnderstanding(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil && parsed != nil {
		return parsed
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(raw[start:end+1]), &parsed); err == nil && parsed != nil {
			return parsed
		}
	}
	return map[string]any{
		"visual_summary":  raw,
		"timeline":        []any{},
		"subjects":        []any{},
		"people":          []any{},
		"expressions":     []any{},
		"actions":         []any{},
		"scenes":          []any{},
		"camera_motion":   []any{},
		"rhythm":          []any{},
		"must_keep":       []any{},
		"can_change":      []any{},
		"must_not_change": []any{},
		"planning_hints":  []any{"vision response was not valid JSON; raw text preserved in visual_summary"},
	}
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
	if err := rejectMCPModelSelection(args, "model"); err != nil {
		return errorResult(err.Error()), nil
	}
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
	if err := rejectMCPModelSelection(args, "model"); err != nil {
		return errorResult(err.Error()), nil
	}
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
	args := parseArgs(req.Params.Arguments)
	if err := rejectMCPModelSelection(args, "model"); err != nil {
		return errorResult(err.Error()), nil
	}
	if svcs == nil || svcs.VideoSvc == nil {
		return errorResult("video service not available"), nil
	}
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

func createVideoGenerationJobHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	if err := rejectMCPModelSelection(args, "model"); err != nil {
		return errorResult(err.Error()), nil
	}
	if svcs == nil || svcs.VideoSvc == nil {
		return errorResult("video service not available"), nil
	}
	if svcs.TaskSvc == nil || svcs.TaskSvc.Repository() == nil || svcs.TaskSvc.Repository().VideoGenerations() == nil {
		return errorResult("task/video generation repository not available"), nil
	}
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
	userID := getUserID(ctx)
	if err := maybeDeductVideo(ctx, userID, videoReq.TaskID, plan.EstimatedCredits); err != nil {
		return billingError("create video generation job", err), nil
	}
	gen, err := persistVideoGenerationJobPlanned(ctx, projectID, userID, videoReq.TaskID, plan)
	if err != nil {
		_ = maybeRefundVideoOperation(ctx, videoReq.TaskID)
		return billingError("persist video generation job", err), nil
	}
	repo := svcs.TaskSvc.Repository().VideoGenerations()
	submitted := make([]map[string]any, 0, len(plan.Segments))
	for _, seg := range plan.Segments {
		segmentReq := videoReq
		segmentReq.Model = seg.Model
		segmentReq.Purpose = plan.Purpose
		segmentReq.Resolution = seg.Resolution
		segmentReq.Ratio = seg.Ratio
		segmentReq.Duration = seg.Duration
		segmentReq.Prompt = seg.Prompt
		segmentReq.Watermark = plan.Watermark
		result, err := svcs.VideoSvc.CreateTask(ctx, segmentReq)
		if err != nil {
			gen.Status = "failed"
			gen.ErrorMessage = err.Error()
			_ = repo.Update(ctx, gen)
			_ = maybeRefundVideoOperation(ctx, videoReq.TaskID)
			return billingError("create video generation segment", err), nil
		}
		segment := &model.VideoGenerationSegment{
			VideoGenerationID: gen.ID,
			UserID:            userID,
			ProjectID:         projectID,
			TaskID:            videoReq.TaskID,
			Index:             seg.Index,
			ArkTaskID:         result.VideoTaskID,
			Status:            "submitted",
			Prompt:            seg.Prompt,
			StartSecond:       seg.StartSecond,
			EndSecond:         seg.EndSecond,
			Duration:          seg.Duration,
			EstimatedCredits:  seg.EstimatedCredits,
			CreditsCharged:    seg.EstimatedCredits,
		}
		if err := repo.CreateSegment(ctx, segment); err != nil {
			gen.Status = "failed"
			gen.ErrorMessage = err.Error()
			_ = repo.Update(ctx, gen)
			_ = maybeRefundVideoOperation(ctx, videoReq.TaskID)
			return billingError("persist video generation segment", err), nil
		}
		submitted = append(submitted, map[string]any{
			"index":         seg.Index,
			"video_task_id": result.VideoTaskID,
			"duration":      seg.Duration,
			"credits":       seg.EstimatedCredits,
		})
	}
	gen.Status = "submitted"
	_ = repo.Update(ctx, gen)
	return textResult(map[string]any{
		"video_generation_id": gen.ID,
		"generation_plan":     plan,
		"segments":            submitted,
		"estimated_credits":   plan.EstimatedCredits,
		"pricing_breakdown":   plan.PricingBreakdown,
	})
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

func queryVideoGenerationJobHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.VideoSvc == nil || svcs.TaskSvc == nil || svcs.TaskSvc.Repository() == nil {
		return errorResult("video job services not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	gen, err := videoGenerationFromArgs(ctx, args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	repo := svcs.TaskSvc.Repository().VideoGenerations()
	segments, err := repo.ListSegments(ctx, gen.ID)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	allSucceeded := len(segments) > 0
	anyFailed := false
	respSegments := make([]map[string]any, 0, len(segments))
	for _, segment := range segments {
		queryError := ""
		if strings.TrimSpace(segment.ArkTaskID) != "" {
			result, err := svcs.VideoSvc.QueryTask(ctx, segment.ArkTaskID)
			if err == nil {
				segment.Status = result.Status
				if result.Error != nil {
					segment.ErrorMessage = result.Error.Message
				}
				urls := map[string]string{"video_url": result.VideoURL, "file_url": result.FileURL, "last_frame_url": result.LastFrameURL}
				if b, marshalErr := json.Marshal(urls); marshalErr == nil {
					segment.ProviderURLs = datatypes.JSON(b)
				}
				_ = repo.UpdateSegment(ctx, segment)
			} else {
				queryError = err.Error()
			}
		}
		status := strings.ToLower(segment.Status)
		if status != "succeeded" || queryError != "" {
			allSucceeded = false
		}
		if status == "failed" || status == "cancelled" {
			anyFailed = true
		}
		respSegment := map[string]any{
			"index":         segment.Index,
			"video_task_id": segment.ArkTaskID,
			"status":        segment.Status,
			"duration":      segment.Duration,
			"task_file_id":  segment.TaskFileID,
			"error":         segment.ErrorMessage,
			"provider_urls": segment.ProviderURLs,
		}
		if queryError != "" {
			respSegment["query_error"] = queryError
		}
		respSegments = append(respSegments, respSegment)
	}
	switch {
	case anyFailed:
		gen.Status = "failed"
	case allSucceeded:
		gen.Status = "succeeded"
	default:
		gen.Status = "submitted"
	}
	_ = repo.Update(ctx, gen)
	return textResult(map[string]any{
		"video_generation_id": gen.ID,
		"status":              gen.Status,
		"segments":            respSegments,
	})
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

func downloadVideoGenerationResultsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return errorResult("task_id is required"), nil
	}
	gen, err := videoGenerationFromArgs(ctx, args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	rawSegments, ok := args["segments"].([]any)
	if !ok || len(rawSegments) == 0 {
		return errorResult("segments are required"), nil
	}
	repo := svcs.TaskSvc.Repository().VideoGenerations()
	persistedSegments, err := repo.ListSegments(ctx, gen.ID)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	segmentsByIndex := make(map[int]*model.VideoGenerationSegment, len(persistedSegments))
	for _, segment := range persistedSegments {
		if segment != nil {
			segmentsByIndex[segment.Index] = segment
		}
	}
	files := make([]*model.TaskFile, 0, len(rawSegments))
	segmentIDs := make([]string, 0, len(rawSegments))
	var ids map[string]any
	if len(gen.TaskFileIDs) > 0 {
		_ = json.Unmarshal(gen.TaskFileIDs, &ids)
	}
	if ids == nil {
		ids = map[string]any{}
	}
	if existing, ok := ids["segments"].([]any); ok {
		for _, value := range existing {
			if id, ok := value.(string); ok && strings.TrimSpace(id) != "" {
				segmentIDs = append(segmentIDs, id)
			}
		}
	}
	for i, raw := range rawSegments {
		item, ok := raw.(map[string]any)
		if !ok {
			return errorResult(fmt.Sprintf("segments[%d] must be an object", i)), nil
		}
		index := 0
		if v, ok := numberAsInt64(item["index"]); ok {
			index = int(v)
		}
		if index <= 0 {
			return errorResult(fmt.Sprintf("segments[%d].index is required", i)), nil
		}
		segment, ok := segmentsByIndex[index]
		if !ok {
			return errorResult(fmt.Sprintf("segments[%d].index %d does not belong to video_generation_id %s", i, index, gen.ID)), nil
		}
		videoURL, _ := item["video_url"].(string)
		if strings.TrimSpace(videoURL) == "" {
			videoURL, _ = item["file_url"].(string)
		}
		if !strings.HasPrefix(videoURL, "https://") {
			return errorResult("segment video_url must be a publicly accessible HTTPS URL"), nil
		}
		fileName := fmt.Sprintf("segment-%02d.mp4", index)
		tmpPath := filepath.Join(os.TempDir(), "anban-video-results", fmt.Sprintf("%d-%s", time.Now().UnixNano(), fileName))
		if err := downloadFile(ctx, videoURL, tmpPath, 500<<20); err != nil {
			return errorResult(err.Error()), nil
		}
		tf, err := uploadLocalVideoTaskFile(ctx, taskID, filepath.ToSlash(filepath.Join("video-segments", fileName)), tmpPath)
		_ = os.Remove(tmpPath)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		files = append(files, tf)
		segmentIDs = append(segmentIDs, tf.ID)
		segment.TaskFileID = tf.ID
		segment.Status = "archived"
		_ = repo.UpdateSegment(ctx, segment)
	}
	ids["segments"] = segmentIDs
	if len(persistedSegments) == 1 && len(segmentIDs) == 1 {
		ids["final_video"] = segmentIDs[0]
		gen.Status = "archived"
	}
	if b, err := json.Marshal(ids); err == nil {
		gen.TaskFileIDs = datatypes.JSON(b)
		_ = repo.Update(ctx, gen)
	}
	return textResult(map[string]any{"video_generation_id": gen.ID, "task_files": files, "task_file_ids": ids})
}

func composeVideoSegmentsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	filePath, _ := args["file_path"].(string)
	fileName, _ := args["file_name"].(string)
	if strings.TrimSpace(fileName) == "" {
		fileName = "final.mp4"
	}
	gen, err := videoGenerationFromArgs(ctx, args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	tf, err := uploadLocalVideoTaskFile(ctx, taskID, fileName, filePath)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	repo := svcs.TaskSvc.Repository().VideoGenerations()
	var ids map[string]any
	if len(gen.TaskFileIDs) > 0 {
		_ = json.Unmarshal(gen.TaskFileIDs, &ids)
	}
	if ids == nil {
		ids = map[string]any{}
	}
	ids["final_video"] = tf.ID
	b, _ := json.Marshal(ids)
	gen.TaskFileIDs = datatypes.JSON(b)
	gen.Status = "archived"
	if err := repo.Update(ctx, gen); err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(map[string]any{"video_generation_id": gen.ID, "final_video": tf, "task_file_ids": ids})
}

func validateVideoDeliveryHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	gen, err := videoGenerationFromArgs(ctx, args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	var ids map[string]any
	if len(gen.TaskFileIDs) > 0 {
		_ = json.Unmarshal(gen.TaskFileIDs, &ids)
	}
	finalID, _ := ids["final_video"].(string)
	if strings.TrimSpace(finalID) == "" {
		return textResult(map[string]any{"valid": false, "reason": "video missing final_video task file", "video_generation_id": gen.ID})
	}
	if strings.TrimSpace(taskID) == "" {
		taskID = gen.TaskID
	}
	if strings.TrimSpace(taskID) == "" || svcs == nil || svcs.TaskSvc == nil || svcs.TaskSvc.Repository() == nil || svcs.TaskSvc.Repository().TaskFiles() == nil {
		return textResult(map[string]any{"valid": false, "reason": "task file repository is not available", "video_generation_id": gen.ID, "final_video_task_file_id": finalID})
	}
	files, err := svcs.TaskSvc.Repository().TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	for _, file := range files {
		if file != nil && file.ID == finalID && isMCPVideoTaskFile(file) {
			return textResult(map[string]any{"valid": true, "video_generation_id": gen.ID, "final_video_task_file_id": finalID, "task_file_ids": ids})
		}
	}
	return textResult(map[string]any{"valid": false, "reason": "final_video task file is not registered or is not a video", "video_generation_id": gen.ID, "final_video_task_file_id": finalID, "task_file_ids": ids})
}

func isMCPVideoTaskFile(file *model.TaskFile) bool {
	if file == nil || file.FileSize <= 0 {
		return false
	}
	mime := strings.ToLower(strings.TrimSpace(file.MimeType))
	name := strings.ToLower(strings.TrimSpace(file.FileName))
	path := strings.ToLower(strings.TrimSpace(file.FilePath))
	if strings.HasPrefix(mime, "video/") {
		return true
	}
	for _, value := range []string{name, path} {
		switch filepath.Ext(value) {
		case ".mp4", ".mov", ".webm", ".m4v":
			return true
		}
	}
	return false
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
	task, err := mcpTaskForProject(ctx, videoReq.TaskID, projectID)
	if err != nil {
		return nil, err
	}
	if err := validateMCPPersistedVideoModels(project, task, videoModelCatalog()); err != nil {
		return nil, err
	}
	if cfg := task.VideoConfig.Data(); strings.TrimSpace(cfg.ModelKey) != "" {
		videoReq.Model = cfg.ModelKey
	}
	if err := requireMeasuredVideoReferences(ctx, videoReq.ReferenceSet, videoReq.TaskID); err != nil {
		return nil, err
	}
	plan, err := service.ResolveVideoGenerationPlanWithBilling(
		videoReq,
		project.VideoDefaults.Data(),
		project.VideoModelPolicy.Data(),
		videoModelCatalog(),
		videoBillingOptions(ctx, getUserID(ctx)),
	)
	if err != nil {
		return nil, err
	}
	plan.ProjectID = projectID
	previewDuration := plan.Duration
	previewPrompt := plan.Prompt
	if len(plan.Segments) > 0 {
		previewDuration = plan.Segments[0].Duration
		previewPrompt = plan.Segments[0].Prompt
	}
	sdkPlan, err := activeVideoService().BuildPlan(service.VideoGenerationRequest{
		Prompt:       previewPrompt,
		Purpose:      plan.Purpose,
		Model:        plan.Model,
		Resolution:   plan.Resolution,
		Ratio:        plan.Ratio,
		Duration:     previewDuration,
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
	segmentPreviews := make([]map[string]any, 0, len(plan.Segments))
	for _, seg := range plan.Segments {
		segmentPreviews = append(segmentPreviews, map[string]any{
			"index":             seg.Index,
			"start_second":      seg.StartSecond,
			"end_second":        seg.EndSecond,
			"duration":          seg.Duration,
			"model":             seg.Model,
			"model_key":         seg.ModelKey,
			"estimated_credits": seg.EstimatedCredits,
		})
	}
	plan.SDKPayloadPreview["segments"] = segmentPreviews
	return &plan, nil
}

func mcpTaskForProject(ctx context.Context, taskID, projectID string) (*model.Task, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if svcs == nil || svcs.TaskSvc == nil {
		return nil, fmt.Errorf("task service not available")
	}
	task, err := svcs.TaskSvc.GetByID(ctx, taskID)
	if err != nil || task == nil {
		return nil, fmt.Errorf("task not found")
	}
	userID := getUserID(ctx)
	if userID != "" && task.UserID != userID {
		return nil, fmt.Errorf("task not found")
	}
	if task.ProjectID != projectID {
		return nil, fmt.Errorf("task does not belong to the requested project")
	}
	return task, nil
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

func validateMCPPersistedVideoModels(project *model.Project, task *model.Task, catalog service.VideoModelCatalog) error {
	if catalog == nil {
		catalog = service.VideoModelCatalog{}
	}
	check := func(key string) error {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil
		}
		if _, ok := catalog[key]; !ok {
			return fmt.Errorf("模型未配置或不可用: %s", key)
		}
		return nil
	}
	if project != nil {
		defaults := project.VideoDefaults.Data()
		if err := check(defaults.ModelKey); err != nil {
			return err
		}
		policy := project.VideoModelPolicy.Data()
		if err := check(policy.DefaultModel); err != nil {
			return err
		}
		for _, key := range policy.AllowedModels {
			if err := check(key); err != nil {
				return err
			}
		}
	}
	if task != nil {
		cfg := task.VideoConfig.Data()
		if err := check(cfg.ModelKey); err != nil {
			return err
		}
	}
	return nil
}

func videoCreditMultiplier() int {
	if billSvc != nil && billSvc.config != nil && billSvc.config.Billing.CreditsPerCNY > 0 {
		return billSvc.config.Billing.CreditsPerCNY
	}
	if billSvc != nil && billSvc.config != nil {
		return billSvc.config.VideoAPI.CreditMultiplierOrDefault()
	}
	return 1000
}

func videoModelCatalog() service.VideoModelCatalog {
	if billSvc != nil && billSvc.config != nil {
		return service.VideoModelCatalogFromConfig(billSvc.config.VideoAPI.ModelCatalog)
	}
	return service.VideoModelCatalog{}
}

func videoBillingOptions(ctx context.Context, userID string) service.VideoBillingOptions {
	fallback := videoCreditMultiplier()
	var billing srvconfig.BillingConfig
	if billSvc != nil && billSvc.config != nil {
		billing = billSvc.config.Billing
	}
	tier := model.TierFree
	userMultiplier := 1.0
	if billSvc == nil || billSvc.creditSvc == nil || userID == "" || userID == "system" || isAdminCall(ctx) {
		return service.VideoBillingOptionsFromConfig(billing, fallback, tier, userMultiplier)
	}
	if foundTier, err := billSvc.creditSvc.GetUserTier(ctx, userID); err == nil {
		tier = foundTier
	} else {
		logBillingSkip(userID, model.CreditTypeVideoGen, "tier_lookup_failed")
	}
	foundMultiplier, err := billSvc.creditSvc.GetUserBillingMultiplier(ctx, userID)
	if err != nil {
		logBillingSkip(userID, model.CreditTypeVideoGen, "billing_multiplier_lookup_failed")
	} else if foundMultiplier > 0 {
		userMultiplier = foundMultiplier
	}
	return service.VideoBillingOptionsFromConfig(billing, fallback, tier, userMultiplier)
}

func maybeDeductVideo(ctx context.Context, userID, taskID string, credits int) error {
	if credits <= 0 {
		logBillingSkip(userID, model.CreditTypeVideoGen, "unpriced")
		return nil
	}
	if billSvc == nil || billSvc.creditSvc == nil {
		logBillingSkip(userID, model.CreditTypeVideoGen, "no_credit_service")
		return nil
	}
	if userID == "" || isAdminCall(ctx) {
		logBillingSkip(userID, model.CreditTypeVideoGen, "admin_static_key")
		return nil
	}
	if userID == "system" {
		logBillingSkip(userID, model.CreditTypeVideoGen, "system_user")
		return nil
	}
	if err := validateBillingTask(ctx, userID, taskID); err != nil {
		return err
	}
	if taskID != "" && svcs != nil && svcs.TaskSvc != nil {
		if task, err := svcs.TaskSvc.GetByID(ctx, taskID); err == nil && task.Type == model.PlatformVideo && task.VideoCreditsCharged > 0 {
			return nil
		}
	}
	_, err := billSvc.creditSvc.DeductForOperation(ctx, userID, model.CreditTypeVideoGen, credits, videoOperationID(taskID), taskID)
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

func rejectMCPModelSelection(args map[string]any, keys ...string) error {
	for _, key := range keys {
		if _, ok := args[key]; ok {
			return fmt.Errorf("%s is not accepted by MCP generation tools; the server resolves models from task/project configuration", key)
		}
	}
	return nil
}

func parseVideoGenerationRequest(args map[string]any) (service.VideoGenerationRequest, error) {
	prompt, _ := args["prompt"].(string)
	purpose, _ := args["purpose"].(string)
	creativeType, _ := args["creative_type"].(string)
	subjectProfile, _ := args["subject_profile"].(string)
	audience, _ := args["audience"].(string)
	singleMessage, _ := args["single_message"].(string)
	resolution, _ := args["resolution"].(string)
	ratio, _ := args["ratio"].(string)
	serviceTier, _ := args["service_tier"].(string)
	taskID, _ := args["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return service.VideoGenerationRequest{}, fmt.Errorf("task_id is required")
	}
	req := service.VideoGenerationRequest{
		Prompt:         prompt,
		Purpose:        purpose,
		CreativeType:   creativeType,
		SubjectProfile: subjectProfile,
		Audience:       audience,
		SingleMessage:  singleMessage,
		Resolution:     resolution,
		Ratio:          ratio,
		ServiceTier:    serviceTier,
		TaskID:         taskID,
		ReferenceSet:   parseVideoReferences(args["references"]),
	}
	if v, ok := numberAsInt64(args["duration"]); ok {
		req.Duration = v
	}
	if v, ok := numberAsInt64(args["planned_duration_seconds"]); ok {
		req.PlannedDurationSeconds = v
	}
	if v, ok := args["target_duration_reason"].(string); ok {
		req.TargetDurationReason = v
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

func requireMeasuredVideoReferences(ctx context.Context, refs []service.VideoReferenceInput, taskID string) error {
	for _, ref := range refs {
		if ref.Type != service.VideoReferenceVideo {
			continue
		}
		if strings.TrimSpace(ref.TaskFileID) != "" && ref.InputDurationSeconds > 0 {
			continue
		}
		if ref.InputDurationSeconds > 0 && videoReferenceMatchesTaskConfig(ctx, taskID, ref) {
			continue
		}
		if ref.InputDurationSeconds <= 0 {
			return fmt.Errorf("video_url references must be registered with register_video_reference using task_file_id so the server can measure input video duration")
		}
		return fmt.Errorf("video_url references with saved input duration must match the current task video_config; use register_video_reference with task_file_id for new raw video references")
	}
	return nil
}

func videoReferenceMatchesTaskConfig(ctx context.Context, taskID string, ref service.VideoReferenceInput) bool {
	if strings.TrimSpace(taskID) == "" || svcs == nil || svcs.TaskSvc == nil {
		return false
	}
	task, err := svcs.TaskSvc.GetByID(ctx, taskID)
	if err != nil || task.Type != model.PlatformVideo {
		return false
	}
	cfg := task.VideoConfig.Data()
	for _, asset := range cfg.References {
		if asset.Type != service.VideoReferenceVideo {
			continue
		}
		if strings.TrimSpace(asset.URL) != strings.TrimSpace(ref.URL) {
			continue
		}
		if asset.InputDurationSeconds <= 0 {
			continue
		}
		return math.Abs(asset.InputDurationSeconds-ref.InputDurationSeconds) < 0.01
	}
	return false
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
		Purpose:                   plan.Purpose,
		ModelKey:                  plan.ModelKey,
		Model:                     plan.Model,
		Resolution:                plan.Resolution,
		Ratio:                     plan.Ratio,
		Duration:                  plan.Duration,
		TargetDurationSeconds:     plan.TargetDurationSeconds,
		TargetDurationSource:      plan.TargetDurationSource,
		TargetDurationReason:      plan.TargetDurationReason,
		SegmentMaxDurationSeconds: plan.SegmentMaxDurationSeconds,
		SegmentMinDurationSeconds: plan.SegmentMinDurationSeconds,
		Watermark:                 plan.Watermark,
		Preflight:                 plan.Preflight,
		EstimatedCredits:          plan.EstimatedCredits,
		PricingBreakdown:          plan.PricingBreakdown,
	}
	for _, seg := range plan.Segments {
		cfg.Segments = append(cfg.Segments, model.VideoTaskSegmentConfig{
			Index:            seg.Index,
			StartSecond:      seg.StartSecond,
			EndSecond:        seg.EndSecond,
			Duration:         seg.Duration,
			Prompt:           seg.Prompt,
			ModelKey:         seg.ModelKey,
			Model:            seg.Model,
			Resolution:       seg.Resolution,
			Ratio:            seg.Ratio,
			EstimatedCredits: seg.EstimatedCredits,
		})
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

func persistVideoGenerationJobPlanned(ctx context.Context, projectID, userID, taskID string, plan *service.VideoGenerationPlan) (*model.VideoGeneration, error) {
	if plan == nil || svcs == nil || svcs.TaskSvc == nil {
		return nil, fmt.Errorf("task service not available")
	}
	repo := svcs.TaskSvc.Repository()
	if repo == nil || repo.VideoGenerations() == nil {
		return nil, fmt.Errorf("video generation repository not available")
	}
	references, err := json.Marshal(plan.References)
	if err != nil {
		return nil, err
	}
	cfg := model.VideoTaskConfig{
		Purpose:                   plan.Purpose,
		ModelKey:                  plan.ModelKey,
		Model:                     plan.Model,
		Resolution:                plan.Resolution,
		Ratio:                     plan.Ratio,
		Duration:                  plan.Duration,
		TargetDurationSeconds:     plan.TargetDurationSeconds,
		TargetDurationSource:      plan.TargetDurationSource,
		TargetDurationReason:      plan.TargetDurationReason,
		SegmentMaxDurationSeconds: plan.SegmentMaxDurationSeconds,
		SegmentMinDurationSeconds: plan.SegmentMinDurationSeconds,
		Watermark:                 plan.Watermark,
		Preflight:                 plan.Preflight,
		EstimatedCredits:          plan.EstimatedCredits,
		PricingBreakdown:          plan.PricingBreakdown,
	}
	for _, seg := range plan.Segments {
		cfg.Segments = append(cfg.Segments, model.VideoTaskSegmentConfig{
			Index:            seg.Index,
			StartSecond:      seg.StartSecond,
			EndSecond:        seg.EndSecond,
			Duration:         seg.Duration,
			Prompt:           seg.Prompt,
			ModelKey:         seg.ModelKey,
			Model:            seg.Model,
			Resolution:       seg.Resolution,
			Ratio:            seg.Ratio,
			EstimatedCredits: seg.EstimatedCredits,
		})
	}
	gen := &model.VideoGeneration{
		UserID:           userID,
		ProjectID:        projectID,
		TaskID:           taskID,
		Status:           "planned",
		ResolvedParams:   datatypes.NewJSONType(cfg),
		References:       datatypes.JSON(references),
		PricingBreakdown: datatypes.NewJSONType(*plan.PricingBreakdown),
		CreditsCharged:   plan.EstimatedCredits,
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		return nil, err
	}
	if strings.TrimSpace(taskID) != "" {
		task, err := repo.Tasks().FindByID(ctx, taskID)
		if err == nil {
			task.VideoGenerationID = gen.ID
			task.SetVideoConfig(cfg)
			task.VideoEstimatedCredits = plan.EstimatedCredits
			task.VideoCreditsCharged = plan.EstimatedCredits
			if updateErr := repo.Tasks().Update(ctx, task); updateErr != nil {
				return nil, updateErr
			}
		}
	}
	return gen, nil
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

func videoGenerationFromArgs(ctx context.Context, args map[string]any) (*model.VideoGeneration, error) {
	if svcs == nil || svcs.TaskSvc == nil || svcs.TaskSvc.Repository() == nil || svcs.TaskSvc.Repository().VideoGenerations() == nil {
		return nil, fmt.Errorf("video generation repository not available")
	}
	repo := svcs.TaskSvc.Repository()
	videoGenerationID, _ := args["video_generation_id"].(string)
	if strings.TrimSpace(videoGenerationID) != "" {
		return repo.VideoGenerations().FindByID(ctx, videoGenerationID)
	}
	taskID, _ := args["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("video_generation_id or task_id is required")
	}
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err == nil && strings.TrimSpace(task.VideoGenerationID) != "" {
		return repo.VideoGenerations().FindByID(ctx, task.VideoGenerationID)
	}
	return repo.VideoGenerations().FindLatestByTaskID(ctx, taskID)
}

func uploadLocalVideoTaskFile(ctx context.Context, taskID, fileName, filePath string) (*model.TaskFile, error) {
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if svcs == nil || svcs.TaskSvc == nil {
		return nil, fmt.Errorf("task service not available")
	}
	cleanPath := filepath.Clean(filePath)
	f, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("read video file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat video file: %w", err)
	}
	tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, taskID, getUserID(ctx), filepath.Base(fileName), f, "video/mp4", info.Size())
	if err != nil {
		return nil, err
	}
	svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
	return tf, nil
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
		"final_video": tf.ID,
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
