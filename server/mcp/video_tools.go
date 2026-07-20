package mcp

import (
	"bytes"
	"context"
	"encoding/json"
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

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
)

var videoDownloadHTTPClient = &http.Client{Transport: http.DefaultTransport}

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
		Name:        "prepare_video_generation_inputs",
		Description: "Normalize Studio video_creator_input.references into a fail-closed video input contract before Seedance generation. Infers reference roles, validates public media, preserves measured video duration, registers video-input-contract.json, and returns the references that must be used by validate/build/create.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project_id": map[string]any{"type": "string"},
				"task_id":    map[string]any{"type": "string"},
			},
			"required": []any{"project_id", "task_id"},
		},
	}, prepareVideoGenerationInputsHandler)

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
		Description: "Validate video generation parameters against the project videocreator profile and return resolved parameters without calling Ark or calculating retail charges.",
		InputSchema: videoGenerationInputSchema(),
	}, validateVideoGenerationParamsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "create_video_generation_job",
		Description: "Create a target-duration video generation job. The server resolves target duration, splits provider-bounded segments, pins one fixed retail SKU per segment before dispatch, submits one provider task per segment, and records segment state. No operation charge occurs at provider submission.",
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
		Description: "Download persisted provider outputs for succeeded segments, atomically register each durable task file with its pinned fixed-SKU settlement, and mark a single-segment result as final_video. The URL must exactly match query_video_generation_job output. Multi-segment jobs should call compose_video_segments after all segments are downloaded.",
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

type videoInputContract struct {
	ProjectID               string                           `json:"project_id"`
	TaskID                  string                           `json:"task_id"`
	InferredMode            string                           `json:"inferred_mode"`
	StrictRemake            bool                             `json:"strict_remake"`
	References              []videoInputContractReference    `json:"references"`
	RequiredReferenceRoles  []string                         `json:"required_reference_roles,omitempty"`
	RequiredReferenceCount  int                              `json:"required_reference_count"`
	RequiredVideoReference  bool                             `json:"required_video_reference"`
	TargetDurationSeconds   int64                            `json:"target_duration_seconds,omitempty"`
	TargetDurationSource    string                           `json:"target_duration_source,omitempty"`
	TargetDurationReason    string                           `json:"target_duration_reason,omitempty"`
	VideoInputContractFile  string                           `json:"video_creator_input_contract_file"`
	VideoUnderstandingFile  string                           `json:"video_understanding_file,omitempty"`
	VideoUnderstandingFiles []videoUnderstandingContractFile `json:"video_understanding_files,omitempty"`
	PreparationWarnings     []string                         `json:"preparation_warnings,omitempty"`
}

type videoUnderstandingContractFile struct {
	FileName      string `json:"file_name"`
	TaskFileID    string `json:"task_file_id,omitempty"`
	SourceURL     string `json:"source_url"`
	ReferenceRole string `json:"reference_role,omitempty"`
	AnalysisMode  string `json:"analysis_mode"`
}

type videoInputContractReference struct {
	Type                 string   `json:"type"`
	URL                  string   `json:"url,omitempty"`
	Text                 string   `json:"text,omitempty"`
	TaskFileID           string   `json:"task_file_id,omitempty"`
	ReferenceRole        string   `json:"reference_role,omitempty"`
	MustKeep             []string `json:"must_keep,omitempty"`
	CanChange            []string `json:"can_change,omitempty"`
	MustNotTransfer      []string `json:"must_not_transfer,omitempty"`
	FileName             string   `json:"file_name,omitempty"`
	MimeType             string   `json:"mime_type,omitempty"`
	FileSize             int64    `json:"file_size,omitempty"`
	InputDurationSeconds float64  `json:"input_duration_seconds,omitempty"`
	Required             bool     `json:"required"`
}

func prepareVideoGenerationInputsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	projectID, _ := args["project_id"].(string)
	taskID, _ := args["task_id"].(string)
	if strings.TrimSpace(projectID) == "" {
		return errorResult("project_id is required"), nil
	}
	task, err := mcpTaskForProject(ctx, taskID, projectID)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	contract, err := buildVideoInputContract(ctx, projectID, task, true)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	if svcs == nil || svcs.TaskSvc == nil {
		return errorResult("task service not available"), nil
	}
	if err := materializeOwnedVideoInputReferences(ctx, task, contract); err != nil {
		return errorResult(err.Error()), nil
	}
	if err := analyzeRequiredVideoReferencesDuringPrepare(ctx, task, contract); err != nil {
		return errorResult(err.Error()), nil
	}
	payload, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		return errorResult("marshal video input contract: " + err.Error()), nil
	}
	tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, task.ID, getUserID(ctx), "video-input-contract.json", strings.NewReader(string(payload)), "application/json", int64(len(payload)))
	if err != nil {
		return errorResult("register video-input-contract.json: " + err.Error()), nil
	}
	svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
	resp := map[string]any{
		"video_creator_input_contract":       contract,
		"video_creator_input_contract_file":  "video-input-contract.json",
		"video_creator_input_contract_id":    tf.ID,
		"video_creator_input_contract_asset": tf,
		"normalized_references":              contract.References,
		"required_reference_roles":           contract.RequiredReferenceRoles,
		"inferred_mode":                      contract.InferredMode,
		"target_duration_seconds":            contract.TargetDurationSeconds,
		"target_duration_source":             contract.TargetDurationSource,
		"video_understanding_files":          contract.VideoUnderstandingFiles,
	}
	return textResult(resp)
}

func materializeOwnedVideoInputReferences(ctx context.Context, task *model.Task, contract *videoInputContract) error {
	if task == nil || contract == nil || svcs == nil || svcs.TaskSvc == nil || svcs.Store == nil {
		return nil
	}
	for i := range contract.References {
		ref := &contract.References[i]
		if !ref.Required || ref.Type == service.VideoReferenceText || strings.TrimSpace(ref.TaskFileID) != "" || strings.TrimSpace(ref.URL) == "" {
			continue
		}
		if !svcs.Store.IsOwnedURL(ref.URL) {
			continue
		}
		source, err := service.ResolveMediaSourceBytes(ctx, svcs.Store, nil, service.MediaSourceRequest{
			RawURL:      ref.URL,
			MaxBytes:    50 << 20,
			ContentType: ref.MimeType,
		})
		if err != nil {
			return fmt.Errorf("materialize video_creator_input.references %s: %w", contractReferenceLabel(*ref), err)
		}
		fileName := videoInputContractFileName(*ref, source.Filename)
		mimeType := videoInputContractMimeType(*ref, source.ContentType)
		tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, task.ID, getUserID(ctx), filepath.ToSlash(filepath.Join("video-inputs", fileName)), bytes.NewReader(source.Bytes), mimeType, int64(len(source.Bytes)))
		if err != nil {
			return fmt.Errorf("register video_creator_input.references %s as task file: %w", contractReferenceLabel(*ref), err)
		}
		svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
		ref.TaskFileID = tf.ID
		if strings.TrimSpace(tf.URL) != "" {
			ref.URL = tf.URL
		}
		ref.FileName = tf.FileName
		ref.MimeType = tf.MimeType
		ref.FileSize = tf.FileSize
	}
	return nil
}

func videoInputContractFileName(ref videoInputContractReference, fallback string) string {
	for _, value := range []string{ref.FileName, fallback, ref.URL, ref.Type} {
		base := filepath.Base(strings.TrimSpace(value))
		if base != "." && base != "/" && base != "" {
			return base
		}
	}
	switch ref.Type {
	case service.VideoReferenceImage:
		return "reference.png"
	case service.VideoReferenceAudio:
		return "reference.mp3"
	case service.VideoReferenceVideo:
		return "reference.mp4"
	default:
		return "reference.bin"
	}
}

func videoInputContractMimeType(ref videoInputContractReference, fallback string) string {
	if strings.TrimSpace(ref.MimeType) != "" {
		return strings.TrimSpace(ref.MimeType)
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	ext := strings.ToLower(filepath.Ext(ref.FileName))
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".mov":
		return "video/quicktime"
	case ".webm":
		return "video/webm"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}

func analyzeRequiredVideoReferencesDuringPrepare(ctx context.Context, task *model.Task, contract *videoInputContract) error {
	if task == nil || contract == nil || !contract.RequiredVideoReference {
		return nil
	}
	if svcs == nil || svcs.WritingSvc == nil || svcs.TaskSvc == nil {
		return fmt.Errorf("required video references must be understood with model_routes.video_understanding via native_video; writing/video understanding service not available")
	}
	analyzed := 0
	for _, ref := range contract.References {
		if !ref.Required || ref.Type != service.VideoReferenceVideo {
			continue
		}
		analyzed++
		fileName := "video-understanding.json"
		if analyzed > 1 {
			fileName = fmt.Sprintf("video-understanding-%02d.json", analyzed)
		}
		understandingFile, err := analyzeAndRegisterVideoUnderstanding(ctx, task.ID, ref, fileName, contract.InferredMode)
		if err != nil {
			return err
		}
		if contract.VideoUnderstandingFile == "" {
			contract.VideoUnderstandingFile = fileName
		}
		contract.VideoUnderstandingFiles = append(contract.VideoUnderstandingFiles, understandingFile)
		if understandingFile.TaskFileID != "" {
			contract.PreparationWarnings = append(contract.PreparationWarnings, fmt.Sprintf("registered %s task_file_id=%s", fileName, understandingFile.TaskFileID))
		}
	}
	return nil
}

func analyzeAndRegisterVideoUnderstanding(ctx context.Context, taskID string, ref videoInputContractReference, fileName, inferredMode string) (videoUnderstandingContractFile, error) {
	if err := service.ValidatePublicHTTPSURLForVideoReference(ref.URL); err != nil {
		return videoUnderstandingContractFile{}, err
	}
	userID := getUserID(ctx)
	inferredMode = strings.TrimSpace(inferredMode)
	if inferredMode == "" {
		inferredMode = "standard"
	}
	purposeHint := ""
	if strings.TrimSpace(ref.ReferenceRole) != "" {
		purposeHint = ref.ReferenceRole
	}
	prompt := buildVideoUnderstandingPrompt(ref.ReferenceRole, purposeHint, "Extract the full reference timeline, surface facts, deep intent, business intent, latent subtext, joke or reversal structure, visual beats, camera, action, expression, rhythm, must_keep, must_keep_meaning, can_change, can_adapt_meaning, must_not_change, and must_not_break_meaning for reference-timeline.json and shot-plan.md.")
	providerRequestID := newUnderstandingProviderRequestID(model.CreditTypeVideoUnderstanding)
	analysis, err := svcs.WritingSvc.AnalyzeVideoURLDetailed(ctx, userID, ref.URL, prompt)
	if err != nil {
		recordUnderstandingProviderCost(ctx, taskID, model.CreditTypeVideoUnderstanding, providerRequestID, nil)
		return videoUnderstandingContractFile{}, fmt.Errorf("analyze video reference during preparation: %w", err)
	}
	recordUnderstandingProviderCost(ctx, taskID, model.CreditTypeVideoUnderstanding, providerRequestID, &analysis.Usage)
	understanding := normalizeVideoUnderstanding(analysis.Text)
	understanding["metadata"] = map[string]any{"source_url": ref.URL}
	understanding["model"] = analysis.Model
	understanding["analysis_mode"] = "native_video"
	understanding["source_url"] = ref.URL
	understanding["reference_role"] = strings.TrimSpace(ref.ReferenceRole)
	understanding["purpose_hint"] = inferredMode
	understanding["usage"] = analysis.Usage
	if err := validateDeepVideoUnderstanding(understanding); err != nil {
		return videoUnderstandingContractFile{}, fmt.Errorf("analyze video reference during preparation returned insufficient native video understanding: %w", err)
	}
	payload, _ := json.MarshalIndent(understanding, "", "  ")
	tf, err := svcs.TaskSvc.UploadTaskFileFromReader(ctx, taskID, userID, fileName, strings.NewReader(string(payload)), "application/json", int64(len(payload)))
	if err != nil {
		return videoUnderstandingContractFile{}, fmt.Errorf("register %s: %w", fileName, err)
	}
	svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
	return videoUnderstandingContractFile{
		FileName:      fileName,
		TaskFileID:    tf.ID,
		SourceURL:     ref.URL,
		ReferenceRole: strings.TrimSpace(ref.ReferenceRole),
		AnalysisMode:  "native_video",
	}, nil
}

func buildVideoInputContract(ctx context.Context, projectID string, task *model.Task, allowNativeAnalysis bool) (*videoInputContract, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	input := task.VideoInput.Data()
	promptContext := strings.TrimSpace(strings.Join([]string{task.Prompt, input.Brief}, "\n"))
	strict := isStrictVideoRemakePrompt(promptContext)
	contract := &videoInputContract{
		ProjectID:              projectID,
		TaskID:                 task.ID,
		InferredMode:           "standard",
		StrictRemake:           strict,
		VideoInputContractFile: "video-input-contract.json",
	}
	if strict {
		contract.InferredMode = "strict_remake"
	}
	seenRoles := map[string]bool{}
	for _, asset := range input.References {
		ref, err := normalizeVideoInputContractReference(ctx, task, promptContext, asset, strict)
		if err != nil {
			return nil, err
		}
		if ref.Type == "" {
			continue
		}
		contract.References = append(contract.References, ref)
		if ref.Required {
			contract.RequiredReferenceCount++
			if strings.TrimSpace(ref.ReferenceRole) != "" && !seenRoles[ref.ReferenceRole] {
				contract.RequiredReferenceRoles = append(contract.RequiredReferenceRoles, ref.ReferenceRole)
				seenRoles[ref.ReferenceRole] = true
			}
			if ref.Type == service.VideoReferenceVideo {
				contract.RequiredVideoReference = true
				if ref.InputDurationSeconds > 0 && contract.TargetDurationSeconds == 0 {
					contract.TargetDurationSeconds = int64(math.Round(ref.InputDurationSeconds))
					contract.TargetDurationSource = service.VideoDurationSourceReferenceVideo
					contract.TargetDurationReason = "matched measured reference video duration"
				}
			}
		}
	}
	if input.HardConstraints.Duration > 0 {
		contract.TargetDurationSeconds = input.HardConstraints.Duration
		contract.TargetDurationSource = service.VideoDurationSourceUser
		contract.TargetDurationReason = "user requested explicit target duration"
	}
	return contract, nil
}

func normalizeVideoInputContractReference(ctx context.Context, task *model.Task, promptContext string, asset model.VideoReferenceAsset, strict bool) (videoInputContractReference, error) {
	refType := strings.TrimSpace(asset.Type)
	urlValue := strings.TrimSpace(asset.URL)
	taskFileID := strings.TrimSpace(asset.TaskFileID)
	duration := asset.InputDurationSeconds
	if taskFileID != "" {
		resolvedURL, measuredDuration, tf, err := videoReferenceURLFromTaskFile(ctx, task.ID, taskFileID, refType)
		if err != nil {
			return videoInputContractReference{}, err
		}
		urlValue = resolvedURL
		if measuredDuration > 0 {
			duration = measuredDuration
		}
		if tf != nil {
			asset.FileName = tf.FileName
			asset.MimeType = tf.MimeType
			asset.FileSize = tf.FileSize
		}
	}
	if refType == "" {
		refType = inferVideoReferenceType(urlValue, asset.Text)
	}
	role := strings.TrimSpace(asset.ReferenceRole)
	if role == "" {
		role = inferVideoInputReferenceRole(promptContext, refType, strict)
	}
	required := isRequiredVideoInputReference(refType, urlValue, asset.Text)
	if required && refType != service.VideoReferenceText {
		if err := service.ValidatePublicHTTPSURLForVideoReference(urlValue); err != nil {
			return videoInputContractReference{}, fmt.Errorf("video_creator_input.references %s is not usable: %w", videoInputReferenceLabel(asset), err)
		}
	}
	if refType == service.VideoReferenceVideo && duration <= 0 {
		return videoInputContractReference{}, fmt.Errorf("video_creator_input.references %s is a video reference but has no measured input_duration_seconds; upload/register it before generation", videoInputReferenceLabel(asset))
	}
	return videoInputContractReference{
		Type:                 refType,
		URL:                  urlValue,
		Text:                 strings.TrimSpace(asset.Text),
		TaskFileID:           taskFileID,
		ReferenceRole:        role,
		MustKeep:             append([]string(nil), asset.MustKeep...),
		CanChange:            append([]string(nil), asset.CanChange...),
		MustNotTransfer:      append([]string(nil), asset.MustNotTransfer...),
		FileName:             strings.TrimSpace(asset.FileName),
		MimeType:             strings.TrimSpace(asset.MimeType),
		FileSize:             asset.FileSize,
		InputDurationSeconds: duration,
		Required:             required,
	}, nil
}

func inferVideoReferenceType(urlValue, textValue string) string {
	if strings.TrimSpace(textValue) != "" && strings.TrimSpace(urlValue) == "" {
		return service.VideoReferenceText
	}
	lower := strings.ToLower(strings.TrimSpace(urlValue))
	switch {
	case strings.HasSuffix(lower, ".mp4"), strings.HasSuffix(lower, ".mov"), strings.HasSuffix(lower, ".webm"), strings.HasSuffix(lower, ".m4v"):
		return service.VideoReferenceVideo
	case strings.HasSuffix(lower, ".mp3"), strings.HasSuffix(lower, ".wav"), strings.HasSuffix(lower, ".m4a"), strings.HasSuffix(lower, ".aac"):
		return service.VideoReferenceAudio
	case lower != "":
		return service.VideoReferenceImage
	default:
		return ""
	}
}

func inferVideoInputReferenceRole(promptContext, refType string, strict bool) string {
	switch refType {
	case service.VideoReferenceVideo:
		if strict {
			if strings.Contains(promptContext, "段子") || strings.Contains(promptContext, "时间轴") || strings.Contains(promptContext, "照着") {
				return "joke timeline"
			}
			return "full remake reference"
		}
		return "motion reference"
	case service.VideoReferenceImage:
		if strict || strings.Contains(promptContext, "主体不变") || strings.Contains(promptContext, "主体") {
			return "subject identity"
		}
		return "visual reference"
	case service.VideoReferenceAudio:
		return "audio reference"
	case service.VideoReferenceText:
		return "text constraint"
	default:
		return ""
	}
}

func isStrictVideoRemakePrompt(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return false
	}
	for _, phrase := range []string{
		"主体不变",
		"完全一样",
		"同款",
		"复刻",
		"参考视频",
		"照着这个段子",
		"照着段子",
		"时间轴结构",
		"strict remake",
		"full remake",
		"same as reference",
		"copy this video",
	} {
		if strings.Contains(text, strings.ToLower(phrase)) {
			return true
		}
	}
	return false
}

func isRequiredVideoInputReference(refType, urlValue, textValue string) bool {
	switch refType {
	case service.VideoReferenceImage, service.VideoReferenceAudio, service.VideoReferenceVideo:
		return strings.TrimSpace(urlValue) != ""
	case service.VideoReferenceText:
		return strings.TrimSpace(textValue) != ""
	default:
		return strings.TrimSpace(urlValue) != "" || strings.TrimSpace(textValue) != ""
	}
}

func videoInputReferenceLabel(asset model.VideoReferenceAsset) string {
	for _, value := range []string{asset.FileName, asset.URL, asset.Text, asset.ReferenceRole, asset.Type} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "reference"
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
	userID := getUserID(ctx)
	if strings.TrimSpace(taskID) != "" {
		if svcs.TaskSvc == nil {
			return errorResult("task service not available"), nil
		}
		task, err := svcs.TaskSvc.GetByID(ctx, taskID)
		if err != nil || task == nil {
			return errorResult("task not found"), nil
		}
		if task.UserID != userID {
			return errorResult("task does not belong to user"), nil
		}
		if task.ProjectID != projectID {
			return errorResult("task does not belong to the requested project"), nil
		}
	}
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
	prompt := buildVideoUnderstandingPrompt(referenceRole, purposeHint, analysisPrompt)
	providerRequestID := newUnderstandingProviderRequestID(model.CreditTypeVideoUnderstanding)
	analysis, err := svcs.WritingSvc.AnalyzeVideoURLDetailed(ctx, userID, videoURL, prompt)
	analysisMode := "native_video"
	metadata := map[string]any{"source_url": videoURL}
	if err != nil {
		recordUnderstandingProviderCost(ctx, taskID, model.CreditTypeVideoUnderstanding, providerRequestID, nil)
		return errorResult("analyze video reference: " + err.Error()), nil
	}
	recordUnderstandingProviderCost(ctx, taskID, model.CreditTypeVideoUnderstanding, providerRequestID, &analysis.Usage)
	understanding := normalizeVideoUnderstanding(analysis.Text)
	understanding["metadata"] = metadata
	understanding["model"] = analysis.Model
	understanding["analysis_mode"] = analysisMode
	understanding["source_url"] = videoURL
	understanding["reference_role"] = strings.TrimSpace(referenceRole)
	understanding["purpose_hint"] = strings.TrimSpace(purposeHint)
	understanding["usage"] = analysis.Usage
	if err := validateDeepVideoUnderstanding(understanding); err != nil {
		return errorResult("analyze video reference returned insufficient native video understanding: " + err.Error()), nil
	}
	resp := map[string]any{
		"analysis_mode":            analysisMode,
		"video_understanding":      understanding,
		"video_understanding_file": "video-understanding.json",
		"usage":                    analysis.Usage,
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
	b.WriteString("更重要的是，请判断视频的潜在内涵和真实意图：创作者想让观众产生什么误会、共情、发笑、信任、焦虑、向往或购买冲动；笑点、反转、隐喻、社会语境、商业转化暗线和必须保留的潜台词是什么。")
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
  "surface_facts": {"people":[],"visuals":[],"actions":[],"expressions":[],"scenes":[],"camera":[],"rhythm":[]},
  "deep_intent": {"creator_intent":"","subtext":"","emotional_arc":[],"audience_expectation":"","joke_or_twist_mechanism":"","metaphor_or_social_context":""},
  "business_intent": {"trust_building":[],"pain_points":[],"conversion_triggers":[],"cta_subtext":""},
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
  "must_keep_meaning": [],
  "can_adapt_meaning": [],
  "must_not_break_meaning": [],
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

func validateDeepVideoUnderstanding(understanding map[string]any) error {
	if understanding == nil {
		return fmt.Errorf("video understanding JSON is empty")
	}
	var missing []string
	for _, field := range []string{"deep_intent", "must_keep_meaning"} {
		if !meaningfulJSONValue(understanding[field]) {
			missing = append(missing, field)
		}
	}
	if mode, _ := understanding["analysis_mode"].(string); strings.TrimSpace(mode) != "native_video" {
		missing = append(missing, "analysis_mode=native_video")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing or empty %s; do not continue with frame sampling, screenshots, image understanding, transcript-only analysis, or marketing-copy guesses", strings.Join(missing, ", "))
	}
	return nil
}

func meaningfulJSONValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case []any:
		for _, item := range v {
			if meaningfulJSONValue(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range v {
			if meaningfulJSONValue(item) {
				return true
			}
		}
		return false
	default:
		return true
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
	return textResult(map[string]any{"generation_plan": plan, "sdk_payload_preview": plan.SDKPayloadPreview})
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
		"valid":           true,
		"resolved_params": plan,
	})
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
	if svcs.BillingCatalogSvc == nil {
		return errorResult("fixed video SKU catalog is not available"), nil
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
	gen, err := persistVideoGenerationJobPlanned(ctx, projectID, userID, videoReq.TaskID, plan)
	if err != nil {
		return billingError("persist video generation job", err), nil
	}
	repo := svcs.TaskSvc.Repository().VideoGenerations()
	submitted := make([]map[string]any, 0, len(plan.Segments))
	inputMode := videoRetailInputMode(plan.References)
	for _, seg := range plan.Segments {
		durationTier, err := videoRetailDurationTier(seg.Duration)
		if err != nil {
			gen.Status = "failed"
			gen.ErrorMessage = err.Error()
			_ = repo.Update(ctx, gen)
			return billingError("resolve video duration SKU tier", err), nil
		}
		sku, err := svcs.BillingCatalogSvc.ResolveVideoSKU(ctx, "", serverbilling.SKUSelectors{
			ModelKey: seg.ModelKey, Resolution: seg.Resolution, DurationTier: durationTier, InputMode: inputMode,
		})
		if err != nil {
			gen.Status = "failed"
			gen.ErrorMessage = err.Error()
			_ = repo.Update(ctx, gen)
			return billingError("resolve fixed video SKU", err), nil
		}
		pinnedSKU := model.VideoSegmentRetailSKU{
			CatalogID: sku.CatalogID, SKUID: sku.SKUID, PriceCredits: sku.PriceCredits,
			ModelKey: seg.ModelKey, Resolution: strings.ToLower(seg.Resolution), DurationTier: durationTier, InputMode: inputMode,
		}
		segment := &model.VideoGenerationSegment{
			VideoGenerationID: gen.ID,
			UserID:            userID,
			ProjectID:         projectID,
			TaskID:            videoReq.TaskID,
			Index:             seg.Index,
			Status:            "planned",
			Prompt:            seg.Prompt,
			StartSecond:       seg.StartSecond,
			EndSecond:         seg.EndSecond,
			Duration:          seg.Duration,
			RetailSKU:         datatypes.NewJSONType(pinnedSKU),
			EstimatedCredits:  0,
			CreditsCharged:    0,
		}
		if err := repo.CreateSegment(ctx, segment); err != nil {
			gen.Status = "failed"
			gen.ErrorMessage = err.Error()
			_ = repo.Update(ctx, gen)
			return billingError("pin video generation segment SKU", err), nil
		}
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
			segment.Status = "failed"
			segment.ErrorMessage = err.Error()
			_ = repo.UpdateSegment(ctx, segment)
			gen.Status = "failed"
			gen.ErrorMessage = err.Error()
			_ = repo.Update(ctx, gen)
			return billingError("create video generation segment", err), nil
		}
		segment.ArkTaskID = result.VideoTaskID
		segment.Status = "submitted"
		if err := repo.UpdateSegment(ctx, segment); err != nil {
			gen.Status = "failed"
			gen.ErrorMessage = err.Error()
			_ = repo.Update(ctx, gen)
			return billingError("persist video generation segment", err), nil
		}
		submitted = append(submitted, map[string]any{
			"index":         seg.Index,
			"video_task_id": result.VideoTaskID,
			"duration":      seg.Duration,
			"retail_sku":    pinnedSKU,
		})
	}
	gen.Status = "submitted"
	_ = repo.Update(ctx, gen)
	return textResult(map[string]any{
		"video_generation_id": gen.ID,
		"generation_plan":     plan,
		"segments":            submitted,
	})
}

func videoRetailDurationTier(duration int64) (string, error) {
	switch {
	case duration >= 1 && duration <= 5:
		return "1-5", nil
	case duration >= 6 && duration <= 10:
		return "6-10", nil
	case duration >= 11 && duration <= 15:
		return "11-15", nil
	default:
		return "", fmt.Errorf("video segment duration %d has no fixed retail tier", duration)
	}
}

func videoRetailInputMode(references []service.VideoReferenceInput) string {
	for _, reference := range references {
		if reference.Type == service.VideoReferenceVideo || reference.Type == service.VideoReferenceAudio {
			return "media_input"
		}
	}
	return "no_input"
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
				recordVideoGenerationProviderCost(ctx, gen, result)
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
		videoURL, err = validateVideoSegmentProviderURL(segment, videoURL)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		identity, err := resolveVideoSegmentSettlementIdentity(ctx, taskID, gen, segment, videoURL)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		if replayFile, _, replayErr := svcs.TaskSvc.FindExecutionTaskFileSettlement(ctx, taskID, identity.ExecutionID, segment.ArkTaskID, identity.RequestFingerprint, "video", "mcp-video-settlement"); replayErr != nil {
			return errorResult(fmt.Sprintf("replay fixed video operation: %v", replayErr)), nil
		} else if replayFile != nil {
			svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{replayFile})
			files = append(files, replayFile)
			segmentIDs = append(segmentIDs, replayFile.ID)
			segment.TaskFileID = replayFile.ID
			segment.Status = "archived"
			if err := repo.UpdateSegment(ctx, segment); err != nil {
				return errorResult(err.Error()), nil
			}
			continue
		}
		fileName := fmt.Sprintf("segment-%02d.mp4", index)
		tmpPath := filepath.Join(os.TempDir(), "anban-video-results", fmt.Sprintf("%d-%s", time.Now().UnixNano(), fileName))
		if err := downloadFile(ctx, videoURL, tmpPath, 500<<20); err != nil {
			return errorResult(err.Error()), nil
		}
		tf, err := uploadLocalVideoSegmentTaskFile(ctx, taskID, gen, segment, identity, filepath.ToSlash(filepath.Join("video-segments", fileName)), tmpPath)
		_ = os.Remove(tmpPath)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		files = append(files, tf)
		segmentIDs = append(segmentIDs, tf.ID)
		segment.TaskFileID = tf.ID
		segment.Status = "archived"
		if err := repo.UpdateSegment(ctx, segment); err != nil {
			return errorResult(err.Error()), nil
		}
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
		return textResult(map[string]any{"valid": false, "reason": "videocreator missing final_video task file", "video_generation_id": gen.ID})
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

func applyVideoInputContractToRequest(ctx context.Context, projectID string, task *model.Task, req *service.VideoGenerationRequest) (*videoInputContract, error) {
	if task == nil || req == nil {
		return nil, nil
	}
	input := task.VideoInput.Data()
	if strings.TrimSpace(req.Ratio) == "" && strings.TrimSpace(input.HardConstraints.Ratio) != "" {
		req.Ratio = strings.TrimSpace(input.HardConstraints.Ratio)
	}
	if req.Duration <= 0 && input.HardConstraints.Duration > 0 {
		req.Duration = input.HardConstraints.Duration
	}
	if req.Watermark == nil && input.HardConstraints.Watermark != nil {
		watermark := *input.HardConstraints.Watermark
		req.Watermark = &watermark
	}
	currentContract, err := buildVideoInputContract(ctx, projectID, task, false)
	if err != nil {
		return nil, err
	}
	if currentContract == nil || currentContract.RequiredReferenceCount == 0 {
		return currentContract, nil
	}
	contract, loaded, err := loadPreparedVideoInputContract(ctx, task.ID)
	if err != nil {
		return nil, err
	}
	if !loaded {
		return nil, fmt.Errorf("video_creator_input.references require prepare_video_generation_inputs before generation; missing video-input-contract.json")
	}
	if contract == nil || contract.RequiredReferenceCount == 0 {
		return nil, fmt.Errorf("video-input-contract.json has no required video_creator_input.references")
	}
	for _, required := range contract.References {
		if !required.Required {
			continue
		}
		index := findVideoReferenceInRequest(req.ReferenceSet, required)
		if index < 0 {
			return nil, fmt.Errorf("video_creator_input.references required reference is missing from final generation plan: %s; generated visual anchors may supplement user media but cannot replace it", contractReferenceLabel(required))
		}
		mergeVideoContractReferenceIntoRequest(&req.ReferenceSet[index], required)
	}
	if contract.RequiredVideoReference {
		hasVideo := false
		for _, ref := range req.ReferenceSet {
			if ref.Type == service.VideoReferenceVideo {
				hasVideo = true
				if ref.InputDurationSeconds <= 0 {
					return nil, fmt.Errorf("video_creator_input.references video reference %s is not measured; call prepare_video_generation_inputs/register_video_reference before generation", strings.TrimSpace(ref.URL))
				}
			}
		}
		if !hasVideo {
			return nil, fmt.Errorf("video_creator_input.references contains a required video reference, but final references[] contains no video_url")
		}
		if err := validatePreparedVideoUnderstandingFiles(ctx, task.ID, contract); err != nil {
			return nil, err
		}
	}
	return contract, nil
}

func loadPreparedVideoInputContract(ctx context.Context, taskID string) (*videoInputContract, bool, error) {
	if svcs == nil || svcs.TaskSvc == nil || svcs.TaskSvc.Repository() == nil || svcs.TaskSvc.Repository().TaskFiles() == nil {
		return nil, false, fmt.Errorf("video_creator_input.references require prepare_video_generation_inputs before generation, but task file repository is not available")
	}
	files, err := svcs.TaskSvc.Repository().TaskFiles().FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, false, err
	}
	for _, file := range files {
		if file == nil {
			continue
		}
		for _, value := range []string{file.FileName, file.FilePath} {
			if filepath.Base(strings.TrimSpace(value)) == "video-input-contract.json" {
				stream, _, err := svcs.TaskSvc.GetFileStream(ctx, file.ID)
				if err != nil {
					return nil, false, fmt.Errorf("load video-input-contract.json: %w", err)
				}
				defer stream.Close()
				payload, err := io.ReadAll(stream)
				if err != nil {
					return nil, false, fmt.Errorf("read video-input-contract.json: %w", err)
				}
				var contract videoInputContract
				if err := json.Unmarshal(payload, &contract); err != nil {
					return nil, false, fmt.Errorf("parse video-input-contract.json: %w", err)
				}
				return &contract, true, nil
			}
		}
	}
	return nil, false, nil
}

func validateVideoInputContractPlan(contract *videoInputContract, plan *service.VideoGenerationPlan) error {
	if contract == nil || contract.RequiredReferenceCount == 0 || plan == nil {
		return nil
	}
	for _, required := range contract.References {
		if !required.Required {
			continue
		}
		if findVideoReferenceInRequest(plan.References, required) < 0 {
			return fmt.Errorf("video_creator_input.references required reference disappeared from resolved generation plan: %s", contractReferenceLabel(required))
		}
	}
	if contract.RequiredVideoReference {
		hasVideoReference := false
		for _, reference := range plan.References {
			if reference.Type == service.VideoReferenceVideo {
				hasVideoReference = true
				break
			}
		}
		if !hasVideoReference {
			return fmt.Errorf("video_creator_input.references includes a required video reference but the resolved plan lost it")
		}
	}
	return nil
}

func validatePreparedVideoUnderstandingFiles(ctx context.Context, taskID string, contract *videoInputContract) error {
	if contract == nil || !contract.RequiredVideoReference {
		return nil
	}
	if strings.TrimSpace(taskID) == "" {
		return fmt.Errorf("video-understanding native_video validation requires task_id")
	}
	files, err := videoUnderstandingFilesForValidation(ctx, taskID, contract)
	if err != nil {
		return err
	}
	for _, required := range contract.References {
		if !required.Required || required.Type != service.VideoReferenceVideo {
			continue
		}
		info, ok := findUnderstandingFileForReference(files, required)
		if !ok {
			return fmt.Errorf("video_creator_input.references video reference %s requires a matching native_video video-understanding artifact before generation", contractReferenceLabel(required))
		}
		if strings.TrimSpace(info.AnalysisMode) != "native_video" {
			return fmt.Errorf("video-understanding artifact for %s must have analysis_mode=native_video", contractReferenceLabel(required))
		}
		understanding, err := loadVideoUnderstandingArtifact(ctx, taskID, info)
		if err != nil {
			return err
		}
		if err := validateDeepVideoUnderstanding(understanding); err != nil {
			return fmt.Errorf("video-understanding artifact for %s is insufficient: %w", contractReferenceLabel(required), err)
		}
	}
	return nil
}

func videoUnderstandingFilesForValidation(ctx context.Context, taskID string, contract *videoInputContract) ([]videoUnderstandingContractFile, error) {
	if len(contract.VideoUnderstandingFiles) > 0 {
		return contract.VideoUnderstandingFiles, nil
	}
	if strings.TrimSpace(contract.VideoUnderstandingFile) == "" {
		return nil, fmt.Errorf("video_creator_input.references include a required video reference but native_video video-understanding.json is missing; call prepare_video_generation_inputs/analyze_video_reference with model_routes.video_understanding")
	}
	var videoRefs []videoInputContractReference
	for _, ref := range contract.References {
		if ref.Required && ref.Type == service.VideoReferenceVideo {
			videoRefs = append(videoRefs, ref)
		}
	}
	if len(videoRefs) != 1 {
		return nil, fmt.Errorf("video-understanding files must be source_url matched for multiple video references")
	}
	return []videoUnderstandingContractFile{{
		FileName:      contract.VideoUnderstandingFile,
		SourceURL:     videoRefs[0].URL,
		ReferenceRole: videoRefs[0].ReferenceRole,
		AnalysisMode:  "native_video",
	}}, nil
}

func findUnderstandingFileForReference(files []videoUnderstandingContractFile, required videoInputContractReference) (videoUnderstandingContractFile, bool) {
	for _, file := range files {
		if strings.TrimSpace(file.SourceURL) != "" && strings.TrimSpace(file.SourceURL) == strings.TrimSpace(required.URL) {
			return file, true
		}
	}
	return videoUnderstandingContractFile{}, false
}

func loadVideoUnderstandingArtifact(ctx context.Context, taskID string, info videoUnderstandingContractFile) (map[string]any, error) {
	if svcs == nil || svcs.TaskSvc == nil || svcs.TaskSvc.Repository() == nil || svcs.TaskSvc.Repository().TaskFiles() == nil {
		return nil, fmt.Errorf("task file repository is required to validate video-understanding artifacts")
	}
	taskFileID := strings.TrimSpace(info.TaskFileID)
	if taskFileID == "" {
		files, err := svcs.TaskSvc.Repository().TaskFiles().FindByTaskID(ctx, taskID)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file == nil {
				continue
			}
			if filepath.Base(strings.TrimSpace(file.FileName)) == filepath.Base(strings.TrimSpace(info.FileName)) ||
				filepath.Base(strings.TrimSpace(file.FilePath)) == filepath.Base(strings.TrimSpace(info.FileName)) {
				taskFileID = file.ID
				break
			}
		}
	}
	if taskFileID == "" {
		return nil, fmt.Errorf("video-understanding artifact %s is not registered as a task file", strings.TrimSpace(info.FileName))
	}
	stream, _, err := svcs.TaskSvc.GetFileStream(ctx, taskFileID)
	if err != nil {
		return nil, fmt.Errorf("load video-understanding artifact %s: %w", strings.TrimSpace(info.FileName), err)
	}
	defer stream.Close()
	payload, err := io.ReadAll(stream)
	if err != nil {
		return nil, fmt.Errorf("read video-understanding artifact %s: %w", strings.TrimSpace(info.FileName), err)
	}
	var understanding map[string]any
	if err := json.Unmarshal(payload, &understanding); err != nil {
		return nil, fmt.Errorf("parse video-understanding artifact %s: %w", strings.TrimSpace(info.FileName), err)
	}
	return understanding, nil
}

func findVideoReferenceInRequest(refs []service.VideoReferenceInput, required videoInputContractReference) int {
	for i, ref := range refs {
		if videoReferenceMatchesContract(ref, required) {
			return i
		}
	}
	return -1
}

func videoReferenceMatchesContract(ref service.VideoReferenceInput, required videoInputContractReference) bool {
	if strings.TrimSpace(required.TaskFileID) != "" && strings.TrimSpace(ref.TaskFileID) == strings.TrimSpace(required.TaskFileID) {
		return true
	}
	if strings.TrimSpace(required.URL) != "" && strings.TrimSpace(ref.URL) == strings.TrimSpace(required.URL) {
		return true
	}
	if required.Type == service.VideoReferenceText && strings.TrimSpace(required.Text) != "" && strings.TrimSpace(ref.Text) == strings.TrimSpace(required.Text) {
		return true
	}
	return false
}

func mergeVideoContractReferenceIntoRequest(ref *service.VideoReferenceInput, required videoInputContractReference) {
	if ref == nil {
		return
	}
	if strings.TrimSpace(ref.Type) == "" {
		ref.Type = required.Type
	}
	if strings.TrimSpace(ref.URL) == "" {
		ref.URL = required.URL
	}
	if strings.TrimSpace(ref.Text) == "" {
		ref.Text = required.Text
	}
	if strings.TrimSpace(ref.TaskFileID) == "" {
		ref.TaskFileID = required.TaskFileID
	}
	if strings.TrimSpace(ref.ReferenceRole) == "" {
		ref.ReferenceRole = required.ReferenceRole
	}
	if len(ref.MustKeep) == 0 {
		ref.MustKeep = append([]string(nil), required.MustKeep...)
	}
	if len(ref.CanChange) == 0 {
		ref.CanChange = append([]string(nil), required.CanChange...)
	}
	if len(ref.MustNotTransfer) == 0 {
		ref.MustNotTransfer = append([]string(nil), required.MustNotTransfer...)
	}
	if ref.InputDurationSeconds <= 0 {
		ref.InputDurationSeconds = required.InputDurationSeconds
	}
}

func contractReferenceLabel(ref videoInputContractReference) string {
	for _, value := range []string{ref.FileName, ref.URL, ref.Text, ref.ReferenceRole, ref.Type} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "reference"
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
	if !model.IsVideoCreatorPlatform(project.Platform) {
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
	contract, err := applyVideoInputContractToRequest(ctx, projectID, task, &videoReq)
	if err != nil {
		return nil, err
	}
	if err := requireMeasuredVideoReferences(ctx, videoReq.ReferenceSet, videoReq.TaskID); err != nil {
		return nil, err
	}
	plan, err := service.ResolveVideoGenerationPlan(
		videoReq,
		project.VideoDefaults.Data(),
		project.VideoModelPolicy.Data(),
		videoModelCatalog(),
	)
	if err != nil {
		return nil, err
	}
	plan.ProjectID = projectID
	if err := validateVideoInputContractPlan(contract, &plan); err != nil {
		return nil, err
	}
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
			"index":        seg.Index,
			"start_second": seg.StartSecond,
			"end_second":   seg.EndSecond,
			"duration":     seg.Duration,
			"model":        seg.Model,
			"model_key":    seg.ModelKey,
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

func videoModelCatalog() service.VideoModelCatalog {
	if billSvc != nil && billSvc.config != nil {
		return service.VideoModelCatalogFromConfig(billSvc.config.VideoAPI.ModelCatalog)
	}
	return service.VideoModelCatalog{}
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
		ref := service.VideoReferenceInput{
			Type:            refType,
			URL:             urlValue,
			Text:            textValue,
			TaskFileID:      taskFileID,
			ReferenceRole:   referenceRole,
			MustKeep:        stringSliceFromAny(m["must_keep"]),
			CanChange:       stringSliceFromAny(m["can_change"]),
			MustNotTransfer: stringSliceFromAny(m["must_not_transfer"]),
		}
		if v, ok := numberAsFloat64(m["input_duration_seconds"]); ok {
			ref.InputDurationSeconds = v
		}
		refs = append(refs, ref)
	}
	return refs
}

func stringSliceFromAny(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		value, ok := item.(string)
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		if value != "" {
			values = append(values, value)
		}
	}
	return values
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
		return fmt.Errorf("video_url references with saved input duration must match the current task video_creator_config; use register_video_reference with task_file_id for new raw video references")
	}
	return nil
}

func videoReferenceMatchesTaskConfig(ctx context.Context, taskID string, ref service.VideoReferenceInput) bool {
	if strings.TrimSpace(taskID) == "" || svcs == nil || svcs.TaskSvc == nil {
		return false
	}
	task, err := svcs.TaskSvc.GetByID(ctx, taskID)
	if err != nil || !model.IsVideoCreatorPlatform(task.Type) {
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
	input := task.VideoInput.Data()
	for _, asset := range input.References {
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
	}
	for _, seg := range plan.Segments {
		cfg.Segments = append(cfg.Segments, model.VideoTaskSegmentConfig{
			Index:       seg.Index,
			StartSecond: seg.StartSecond,
			EndSecond:   seg.EndSecond,
			Duration:    seg.Duration,
			Prompt:      seg.Prompt,
			ModelKey:    seg.ModelKey,
			Model:       seg.Model,
			Resolution:  seg.Resolution,
			Ratio:       seg.Ratio,
		})
	}
	gen := &model.VideoGeneration{
		UserID:           userID,
		ProjectID:        projectID,
		TaskID:           taskID,
		Status:           "planned",
		ResolvedParams:   datatypes.NewJSONType(cfg),
		References:       datatypes.JSON(references),
		PricingBreakdown: datatypes.NewJSONType(model.VideoPricingBreakdown{}),
		CreditsCharged:   0,
	}
	if err := repo.VideoGenerations().Create(ctx, gen); err != nil {
		return nil, err
	}
	if strings.TrimSpace(taskID) != "" {
		task, err := repo.Tasks().FindByID(ctx, taskID)
		if err == nil {
			task.VideoGenerationID = gen.ID
			task.SetVideoConfig(cfg)
			task.VideoEstimatedCredits = 0
			task.VideoCreditsCharged = 0
			if updateErr := repo.Tasks().Update(ctx, task); updateErr != nil {
				return nil, updateErr
			}
		}
	}
	return gen, nil
}

func recordVideoGenerationProviderCost(ctx context.Context, gen *model.VideoGeneration, result *service.VideoGenerationTaskResult) {
	if svcs == nil || svcs.ProviderCostSvc == nil || gen == nil || result == nil || !strings.EqualFold(result.Status, "succeeded") {
		return
	}
	providerRequestID := strings.TrimSpace(result.VideoTaskID)
	if providerRequestID == "" {
		providerRequestID = strings.TrimSpace(gen.ArkTaskID)
	}
	modelID := strings.TrimSpace(result.Model)
	if modelID == "" {
		modelID = strings.TrimSpace(gen.ResolvedParams.Data().Model)
	}
	hasVideoInput, hasAudioInput := videoGenerationInputFlags(gen.References)
	var err error
	if result.Duration <= 0 || strings.TrimSpace(result.Resolution) == "" {
		_, err = svcs.ProviderCostSvc.RecordMediaUnreconciled(ctx, service.RecordMediaUnreconciledRequest{
			TaskID: gen.TaskID, Provider: "volcengine_ark", Model: modelID, ProviderRequestID: providerRequestID,
			MediaKind: "video", ReasonCode: model.BillingExecutionCostReasonMissingOutputMetadata,
			DurationSeconds: result.Duration, Resolution: result.Resolution, HasVideoInput: hasVideoInput, HasAudioInput: hasAudioInput,
		})
	} else {
		_, err = svcs.ProviderCostSvc.RecordVideoOutputCost(ctx, service.RecordVideoOutputCostRequest{
			TaskID: gen.TaskID, Provider: "volcengine_ark", Model: modelID, ProviderRequestID: providerRequestID,
			CatalogID: svcs.ProviderCostSvc.CatalogID(), IdempotencyKey: providerRequestID,
			DurationSeconds: result.Duration, Resolution: result.Resolution,
			HasVideoInput: hasVideoInput, HasAudioInput: hasAudioInput,
			Source: string(model.BillingProviderCostSourceProviderResponse),
		})
	}
	if err != nil && mcpLog != nil {
		mcpLog.Error().Err(err).Str("video_generation_id", gen.ID).Str("provider_request_id", providerRequestID).
			Msg("record video provider cost; provider result remains valid")
	}
}

func videoGenerationInputFlags(raw datatypes.JSON) (bool, bool) {
	var references []service.VideoReferenceInput
	if len(raw) == 0 || json.Unmarshal(raw, &references) != nil {
		return false, false
	}
	var video, audio bool
	for _, reference := range references {
		kind := strings.ToLower(strings.TrimSpace(reference.Type))
		video = video || strings.Contains(kind, "video")
		audio = audio || strings.Contains(kind, "audio")
	}
	return video, audio
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

type videoSegmentSettlementIdentity struct {
	ExecutionID        string
	RequestFingerprint string
	Pinned             model.VideoSegmentRetailSKU
}

func resolveVideoSegmentSettlementIdentity(ctx context.Context, taskID string, gen *model.VideoGeneration, segment *model.VideoGenerationSegment, providerURL string) (videoSegmentSettlementIdentity, error) {
	if gen == nil || segment == nil || strings.TrimSpace(segment.ArkTaskID) == "" {
		return videoSegmentSettlementIdentity{}, fmt.Errorf("persisted Ark segment identity is required for fixed video settlement")
	}
	pinned := segment.RetailSKU.Data()
	if pinned.CatalogID == "" || pinned.SKUID == "" || pinned.PriceCredits <= 0 || pinned.ModelKey == "" || pinned.Resolution == "" || pinned.DurationTier == "" || pinned.InputMode == "" {
		return videoSegmentSettlementIdentity{}, fmt.Errorf("video segment %d has no pinned fixed retail SKU", segment.Index)
	}
	executionID := getExecutionID(ctx)
	if executionID == "" && svcs != nil && svcs.TaskSvc != nil {
		if task, err := svcs.TaskSvc.GetByID(ctx, taskID); err == nil && task.CurrentExecutionID != nil {
			executionID = strings.TrimSpace(*task.CurrentExecutionID)
		}
	}
	if executionID == "" {
		return videoSegmentSettlementIdentity{}, fmt.Errorf("current execution identity is required for fixed-SKU video settlement")
	}
	fingerprint := imageOperationFingerprint(
		"video-segment", taskID, executionID, gen.ID, segment.ID, segment.ArkTaskID,
		pinned.CatalogID, pinned.SKUID, pinned.PriceCredits, pinned.ModelKey, pinned.Resolution, pinned.DurationTier, pinned.InputMode,
		providerURL,
	)
	return videoSegmentSettlementIdentity{ExecutionID: executionID, RequestFingerprint: fingerprint, Pinned: pinned}, nil
}

func validateVideoSegmentProviderURL(segment *model.VideoGenerationSegment, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if !strings.HasPrefix(requested, "https://") {
		return "", fmt.Errorf("segment video_url must be a publicly accessible HTTPS URL")
	}
	if segment == nil || len(segment.ProviderURLs) == 0 {
		return "", fmt.Errorf("segment provider output is not persisted; query the video generation job before downloading")
	}
	var urls map[string]string
	if err := json.Unmarshal(segment.ProviderURLs, &urls); err != nil {
		return "", fmt.Errorf("decode persisted segment provider output: %w", err)
	}
	for _, key := range []string{"video_url", "file_url"} {
		if authoritative := strings.TrimSpace(urls[key]); authoritative != "" && requested == authoritative {
			return authoritative, nil
		}
	}
	return "", fmt.Errorf("segment video_url does not match the persisted provider output")
}

func uploadLocalVideoSegmentTaskFile(ctx context.Context, taskID string, gen *model.VideoGeneration, segment *model.VideoGenerationSegment, identity videoSegmentSettlementIdentity, fileName, filePath string) (*model.TaskFile, error) {
	if svcs == nil || svcs.TaskSvc == nil {
		return nil, fmt.Errorf("task service not available")
	}
	f, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("read video file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat video file: %w", err)
	}
	snapshot, err := json.Marshal(map[string]any{
		"video_generation_id": gen.ID,
		"segment_index":       segment.Index,
	})
	if err != nil {
		return nil, err
	}
	tf, err := svcs.TaskSvc.UploadExecutionTaskFileWithOperationSettlementFromReader(ctx, taskID, getUserID(ctx), identity.ExecutionID, filepath.Base(fileName), f, "video/mp4", info.Size(), service.GenericTaskFileOperationSettlement{
		ResourceType: "video", IdempotencyScope: "mcp-video-settlement",
		CatalogID: identity.Pinned.CatalogID, SKUID: identity.Pinned.SKUID, PriceCredits: identity.Pinned.PriceCredits,
		OperationID: segment.ArkTaskID, RequestFingerprint: identity.RequestFingerprint, ResultSnapshot: snapshot,
	})
	if err != nil {
		return nil, err
	}
	svcs.TaskSvc.EnrichFilesWithURLs(ctx, []*model.TaskFile{tf})
	return tf, nil
}

func downloadFile(ctx context.Context, rawURL, outputPath string, maxBytes int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := videoDownloadHTTPClient.Do(req)
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
