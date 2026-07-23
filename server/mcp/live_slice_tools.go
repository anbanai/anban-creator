package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

// registerLiveSliceTools registers direct live video slicing tools.
func registerLiveSliceTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "upload_live_audio",
		Description: "Upload one server-local live audio file and return its storage URL and object key.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path":       map[string]any{"type": "string", "description": "Server-local audio file path only. Do not pass an agent/client-local path such as /Users/... unless the MCP server runs on that same filesystem."},
				"expires_seconds": map[string]any{"type": "integer", "description": "Signed URL TTL in seconds when no custom OSS domain is configured", "default": 86400},
			},
			"required": []any{"file_path"},
		},
	}, uploadLiveAudioHandler)

	server.AddTool(&mcp.Tool{
		Name:        "create_live_analysis_task",
		Description: "Create one Alibaba TingWu offline analysis task for a live audio URL or object key. Returns the TingWu task ID.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"audio_key":                  map[string]any{"type": "string", "description": "OSS object key returned by prepare_file_upload for purpose=live_audio. Preferred for agent/client-local files."},
				"audio_url":                  map[string]any{"type": "string", "description": "Public or signed audio URL accessible by TingWu"},
				"auto_chapters_enabled":      map[string]any{"type": "boolean", "description": "Enable auto chapters", "default": true},
				"summarization_enabled":      map[string]any{"type": "boolean", "description": "Enable summary, Q&A, and mind map", "default": true},
				"meeting_assistance_enabled": map[string]any{"type": "boolean", "description": "Enable keywords, actions, and key information", "default": true},
				"diarization_enabled":        map[string]any{"type": "boolean", "description": "Enable speaker diarization", "default": false},
				"script_template_enable":     map[string]any{"type": "boolean", "description": "Ask TingWu to generate reusable live-script structure", "default": false},
			},
		},
	}, createLiveAnalysisTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "query_live_analysis_task",
		Description: "Query a TingWu live analysis task and return normalized JSON with chapters, sentences, topics, Q&A, words, silents, and optional live-script template.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "TingWu task ID"},
			},
			"required": []any{"task_id"},
		},
	}, queryLiveAnalysisTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "recognize_live_subjects",
		Description: "Analyze live transcript sentences and return short-video topic candidates as strict JSON subjects.",
		InputSchema: liveSentenceSchema(nil),
	}, recognizeLiveSubjectsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "recognize_live_invalid_sentences",
		Description: "Identify transcript sentences that are entirely unsuitable for short-video slicing, such as greetings, thanks, filler, and live-only promotions.",
		InputSchema: liveSentenceSchema(nil),
	}, recognizeLiveInvalidSentencesHandler)

	server.AddTool(&mcp.Tool{
		Name:        "recognize_live_segments",
		Description: "Split live transcript sentences into coherent slicing ranges. Returns segment title, description, thoughts, start index, and end index.",
		InputSchema: liveSentenceSchema(map[string]any{
			"ask": map[string]any{"type": "string", "description": "Optional slicing goal or constraints"},
		}),
	}, recognizeLiveSegmentsHandler)

	server.AddTool(&mcp.Tool{
		Name:        "complete_live_subject",
		Description: "Create a short-video script from live transcript sentences, optional ask, selected subject, and thoughts. Returns title, subtitle, reasoning, and chosen sentences.",
		InputSchema: liveSentenceSchema(map[string]any{
			"ask":      map[string]any{"type": "string", "description": "Optional slicing goal or constraints"},
			"subject":  map[string]any{"type": "string", "description": "Selected short-video topic"},
			"thoughts": map[string]any{"type": "string", "description": "Planning notes for the selected topic"},
		}),
	}, completeLiveSubjectHandler)

	server.AddTool(&mcp.Tool{
		Name:        "build_live_clip_plan",
		Description: "Deterministically map live transcript sentences and LLM segment indexes to clip timings, safe output paths, and ffmpeg command strings. Does not execute commands or touch files.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sentences":                liveSentenceSchema(nil)["properties"].(map[string]any)["sentences"],
				"segments":                 liveSegmentArraySchema(),
				"video_path":               map[string]any{"type": "string", "description": "Source video path used in generated ffmpeg commands"},
				"output_dir":               map[string]any{"type": "string", "description": "Working directory; clips are planned under output_dir/exports"},
				"invalid":                  liveInvalidArraySchema(),
				"min_duration_seconds":     map[string]any{"type": "number", "default": 5},
				"max_duration_seconds":     map[string]any{"type": "number", "default": 180},
				"head_padding_seconds":     map[string]any{"type": "number", "default": 0},
				"tail_padding_seconds":     map[string]any{"type": "number", "default": 0},
				"target_mode":              map[string]any{"type": "string", "description": "Output orientation target: auto/vertical (short-video 9:16, default), horizontal, original (keep source)", "default": "auto"},
				"vertical_fill":            map[string]any{"type": "string", "description": "How to fit a landscape source into a vertical canvas: blur (default, blurred bg + centered foreground, loses nothing), crop (center 9:16 column, loses sides), none", "default": "blur"},
				"source_width":             map[string]any{"type": "integer", "description": "Source video width in px from ffprobe; supplying width+height enables orientation conversion"},
				"source_height":            map[string]any{"type": "integer", "description": "Source video height in px from ffprobe; supplying width+height enables orientation conversion"},
				"target_width":             map[string]any{"type": "integer", "description": "Target vertical canvas width", "default": 1080},
				"target_height":            map[string]any{"type": "integer", "description": "Target vertical canvas height", "default": 1920},
				"normalize_audio_loudness": map[string]any{"type": "boolean", "description": "Apply EBU R128 loudnorm on re-encode paths so clip audio is broadcast/Douyin-safe (default true)", "default": true},
			},
			"required": []any{"sentences", "segments", "video_path", "output_dir"},
		},
	}, buildLiveClipPlanHandler)

	server.AddTool(&mcp.Tool{
		Name:        "build_live_subject_clip_plan",
		Description: "Deterministically map completed live subject scripts to clip parts, re-encode commands, concat list content, and final ffmpeg concat commands. Does not execute commands or touch files.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sentences":                liveSentenceSchema(nil)["properties"].(map[string]any)["sentences"],
				"completions":              liveSubjectCompletionArraySchema(),
				"video_path":               map[string]any{"type": "string", "description": "Source video path used in generated ffmpeg commands"},
				"output_dir":               map[string]any{"type": "string", "description": "Working directory; clips are planned under output_dir/exports"},
				"invalid":                  liveInvalidArraySchema(),
				"min_duration_seconds":     map[string]any{"type": "number", "default": 5},
				"max_duration_seconds":     map[string]any{"type": "number", "default": 180},
				"head_padding_seconds":     map[string]any{"type": "number", "default": 0},
				"tail_padding_seconds":     map[string]any{"type": "number", "default": 0},
				"target_mode":              map[string]any{"type": "string", "description": "Output orientation target: auto/vertical (short-video 9:16, default), horizontal, original (keep source)", "default": "auto"},
				"vertical_fill":            map[string]any{"type": "string", "description": "How to fit a landscape source into a vertical canvas: blur (default, blurred bg + centered foreground, loses nothing), crop (center 9:16 column, loses sides), none", "default": "blur"},
				"source_width":             map[string]any{"type": "integer", "description": "Source video width in px from ffprobe; supplying width+height enables orientation conversion"},
				"source_height":            map[string]any{"type": "integer", "description": "Source video height in px from ffprobe; supplying width+height enables orientation conversion"},
				"target_width":             map[string]any{"type": "integer", "description": "Target vertical canvas width", "default": 1080},
				"target_height":            map[string]any{"type": "integer", "description": "Target vertical canvas height", "default": 1920},
				"normalize_audio_loudness": map[string]any{"type": "boolean", "description": "Apply EBU R128 loudnorm on re-encode paths so clip audio is broadcast/Douyin-safe (default true)", "default": true},
			},
			"required": []any{"sentences", "completions", "video_path", "output_dir"},
		},
	}, buildLiveSubjectClipPlanHandler)

	server.AddTool(&mcp.Tool{
		Name:        "build_live_clip_manifest",
		Description: "Build a JSON-array clip manifest plus transcript.md, summary.md, and clip_notes_markdown from a clip plan and local ffmpeg execution results. Strictly validates one result per clip, successful file size, and actual duration. Does not read or write files.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source_video":   map[string]any{"type": "string"},
				"tingwu_task_id": map[string]any{"type": "string"},
				"analysis_title": map[string]any{"type": "string"},
				"sentences":      liveSentenceSchema(nil)["properties"].(map[string]any)["sentences"],
				"invalid":        liveInvalidArraySchema(),
				"warnings":       liveClipNoticeArraySchema(),
				"rejected":       liveRejectedArraySchema(),
				"clips":          liveClipArraySchema(),
				"clip_results":   liveClipResultArraySchema(),
			},
			"required": []any{"source_video", "tingwu_task_id", "sentences", "clips", "clip_results"},
		},
	}, buildLiveClipManifestHandler)
}

func liveSentenceSchema(extra map[string]any) map[string]any {
	props := map[string]any{
		"sentences": map[string]any{
			"type":        "array",
			"description": "Transcript sentences with index/text and optional start/end seconds",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"index": map[string]any{"type": "integer"},
					"start": map[string]any{"type": "number"},
					"end":   map[string]any{"type": "number"},
					"text":  map[string]any{"type": "string"},
				},
				"required": []any{"index", "text"},
			},
		},
		"task_id": map[string]any{"type": "string", "description": "Optional Anban task ID for task-scoped credit tracking"},
	}
	for k, v := range extra {
		props[k] = v
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []any{"sentences"},
	}
}

func liveSegmentArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":       map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"thoughts":    map[string]any{"type": "string"},
				"start":       map[string]any{"type": "integer"},
				"end":         map[string]any{"type": "integer"},
			},
			"required": []any{"start", "end"},
		},
	}
}

func liveSubjectCompletionArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"title":     map[string]any{"type": "string"},
				"subtitle":  map[string]any{"type": "string"},
				"thoughts":  map[string]any{"type": "string"},
				"sentences": liveSentenceSchema(nil)["properties"].(map[string]any)["sentences"],
			},
			"required": []any{"sentences"},
		},
	}
}

func liveInvalidArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"index":  map[string]any{"type": "integer"},
				"reason": map[string]any{"type": "string"},
			},
			"required": []any{"index"},
		},
	}
}

func liveClipNoticeArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"index":  map[string]any{"type": "integer"},
				"title":  map[string]any{"type": "string"},
				"reason": map[string]any{"type": "string"},
			},
			"required": []any{"index", "reason"},
		},
	}
}

func liveRejectedArraySchema() map[string]any {
	return liveClipNoticeArraySchema()
}

func liveClipPartArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"part_index":         map[string]any{"type": "integer"},
				"sentence_start":     map[string]any{"type": "integer"},
				"sentence_end":       map[string]any{"type": "integer"},
				"start":              map[string]any{"type": "number"},
				"end":                map[string]any{"type": "number"},
				"duration":           map[string]any{"type": "number"},
				"output":             map[string]any{"type": "string"},
				"accurate_cut_shell": map[string]any{"type": "string"},
				"accurate_cut_args":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"transcript":         liveSentenceSchema(nil)["properties"].(map[string]any)["sentences"],
			},
			"required": []any{"part_index", "sentence_start", "sentence_end", "start", "end", "duration", "output"},
		},
	}
}

func liveClipArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"index":               map[string]any{"type": "integer"},
				"title":               map[string]any{"type": "string"},
				"description":         map[string]any{"type": "string"},
				"thoughts":            map[string]any{"type": "string"},
				"sentence_start":      map[string]any{"type": "integer"},
				"sentence_end":        map[string]any{"type": "integer"},
				"start":               map[string]any{"type": "number"},
				"end":                 map[string]any{"type": "number"},
				"duration":            map[string]any{"type": "number"},
				"output":              map[string]any{"type": "string"},
				"method":              map[string]any{"type": "string"},
				"status":              map[string]any{"type": "string"},
				"parts":               liveClipPartArraySchema(),
				"concat_list_path":    map[string]any{"type": "string"},
				"concat_list_content": map[string]any{"type": "string"},
				"concat_shell":        map[string]any{"type": "string"},
				"concat_args":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"script_notes":        liveSentenceSchema(nil)["properties"].(map[string]any)["sentences"],
			},
			"required": []any{"index", "title", "sentence_start", "sentence_end", "start", "end", "duration", "output"},
		},
	}
}

func liveClipResultArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"index":                   map[string]any{"type": "integer"},
				"status":                  map[string]any{"type": "string"},
				"method":                  map[string]any{"type": "string"},
				"output":                  map[string]any{"type": "string"},
				"exit_code":               map[string]any{"type": "integer"},
				"error":                   map[string]any{"type": "string"},
				"size":                    map[string]any{"type": "integer"},
				"actual_duration_seconds": map[string]any{"type": "number"},
				"part_results":            liveClipPartResultArraySchema(),
			},
			"required": []any{"index", "status"},
		},
	}
}

func liveClipPartResultArraySchema() map[string]any {
	return map[string]any{
		"type": "array",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"part_index":              map[string]any{"type": "integer"},
				"status":                  map[string]any{"type": "string"},
				"method":                  map[string]any{"type": "string"},
				"output":                  map[string]any{"type": "string"},
				"exit_code":               map[string]any{"type": "integer"},
				"error":                   map[string]any{"type": "string"},
				"size":                    map[string]any{"type": "integer"},
				"actual_duration_seconds": map[string]any{"type": "number"},
			},
			"required": []any{"part_index", "status"},
		},
	}
}

func uploadLiveAudioHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	filePath, _ := args["file_path"].(string)
	if filePath == "" {
		return errorResult("file_path is required"), nil
	}
	expires := intFromArg(args["expires_seconds"], 0)

	result, err := svcs.LiveSliceSvc.UploadLiveAudio(ctx, filePath, expires)
	if err != nil {
		return errorResult("upload live audio: " + err.Error()), nil
	}
	return textResult(result)
}

func createLiveAnalysisTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	audioURL, _ := args["audio_url"].(string)
	audioKey, _ := args["audio_key"].(string)
	if audioKey == "" && audioURL == "" {
		return errorResult("audio_key or audio_url is required"), nil
	}

	task, err := svcs.LiveSliceSvc.CreateLiveAnalysisTask(ctx, service.LiveAnalysisTaskRequest{
		AudioKey:                 audioKey,
		AudioURL:                 audioURL,
		AutoChaptersEnabled:      boolFromArg(args["auto_chapters_enabled"], true),
		SummarizationEnabled:     boolFromArg(args["summarization_enabled"], true),
		MeetingAssistanceEnabled: boolFromArg(args["meeting_assistance_enabled"], true),
		DiarizationEnabled:       boolFromArg(args["diarization_enabled"], false),
		ScriptTemplateEnable:     boolFromArg(args["script_template_enable"], false),
	})
	if err != nil {
		return errorResult("create live analysis task: " + err.Error()), nil
	}
	return textResult(task)
}

func queryLiveAnalysisTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	if taskID == "" {
		return errorResult("task_id is required"), nil
	}

	result, err := svcs.LiveSliceSvc.QueryLiveAnalysisTask(ctx, taskID)
	if err != nil {
		return errorResult("query live analysis task: " + err.Error()), nil
	}
	return textResult(result)
}

func recognizeLiveSubjectsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	sentences, err := parseLiveSentences(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	subjects, err := svcs.LiveSliceSvc.RecognizeLiveSubjects(ctx, sentences)
	if err != nil {
		return billingError("recognize live subjects", err), nil
	}
	return textResult(map[string]any{"subjects": subjects})
}

func recognizeLiveInvalidSentencesHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	sentences, err := parseLiveSentences(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	invalid, err := svcs.LiveSliceSvc.RecognizeLiveInvalidSentences(ctx, sentences)
	if err != nil {
		return billingError("recognize live invalid sentences", err), nil
	}
	return textResult(map[string]any{"invalid": invalid})
}

func recognizeLiveSegmentsHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	sentences, err := parseLiveSentences(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	ask, _ := args["ask"].(string)
	segments, err := svcs.LiveSliceSvc.RecognizeLiveSegments(ctx, sentences, ask)
	if err != nil {
		return billingError("recognize live segments", err), nil
	}
	return textResult(map[string]any{"segments": segments})
}

func completeLiveSubjectHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	sentences, err := parseLiveSentences(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	ask, _ := args["ask"].(string)
	subject, _ := args["subject"].(string)
	thoughts, _ := args["thoughts"].(string)
	completion, err := svcs.LiveSliceSvc.CompleteLiveSubject(ctx, sentences, ask, subject, thoughts)
	if err != nil {
		return billingError("complete live subject", err), nil
	}
	return textResult(completion)
}

func buildLiveClipPlanHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	videoPath, _ := args["video_path"].(string)
	if videoPath == "" {
		return errorResult("video_path is required"), nil
	}
	outputDir, _ := args["output_dir"].(string)
	if outputDir == "" {
		return errorResult("output_dir is required"), nil
	}
	sentences, err := parseLiveSentences(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	segments, err := parseLiveSegments(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	invalid, err := parseLiveInvalids(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	plan, err := svcs.LiveSliceSvc.BuildLiveClipPlan(service.LiveClipPlanRequest{
		VideoPath:              videoPath,
		OutputDir:              outputDir,
		Sentences:              sentences,
		Segments:               segments,
		Invalid:                invalid,
		MinDurationSeconds:     floatFromArg(args["min_duration_seconds"], 0),
		MaxDurationSeconds:     floatFromArg(args["max_duration_seconds"], 0),
		HeadPaddingSeconds:     floatFromArg(args["head_padding_seconds"], 0),
		TailPaddingSeconds:     floatFromArg(args["tail_padding_seconds"], 0),
		TargetMode:             stringFromArg(args["target_mode"]),
		VerticalFill:           stringFromArg(args["vertical_fill"]),
		SourceWidth:            intFromArg(args["source_width"], 0),
		SourceHeight:           intFromArg(args["source_height"], 0),
		TargetWidth:            intFromArg(args["target_width"], 0),
		TargetHeight:           intFromArg(args["target_height"], 0),
		NormalizeAudioLoudness: boolPtrFromArg(args["normalize_audio_loudness"]),
	})
	if err != nil {
		return errorResult("build live clip plan: " + err.Error()), nil
	}
	return textResult(plan)
}

func buildLiveSubjectClipPlanHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	videoPath, _ := args["video_path"].(string)
	if videoPath == "" {
		return errorResult("video_path is required"), nil
	}
	outputDir, _ := args["output_dir"].(string)
	if outputDir == "" {
		return errorResult("output_dir is required"), nil
	}
	sentences, err := parseLiveSentences(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	completions, err := parseLiveSubjectCompletions(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	invalid, err := parseLiveInvalids(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	plan, err := svcs.LiveSliceSvc.BuildLiveSubjectClipPlan(service.LiveSubjectClipPlanRequest{
		VideoPath:              videoPath,
		OutputDir:              outputDir,
		Sentences:              sentences,
		Completions:            completions,
		Invalid:                invalid,
		MinDurationSeconds:     floatFromArg(args["min_duration_seconds"], 0),
		MaxDurationSeconds:     floatFromArg(args["max_duration_seconds"], 0),
		HeadPaddingSeconds:     floatFromArg(args["head_padding_seconds"], 0),
		TailPaddingSeconds:     floatFromArg(args["tail_padding_seconds"], 0),
		TargetMode:             stringFromArg(args["target_mode"]),
		VerticalFill:           stringFromArg(args["vertical_fill"]),
		SourceWidth:            intFromArg(args["source_width"], 0),
		SourceHeight:           intFromArg(args["source_height"], 0),
		TargetWidth:            intFromArg(args["target_width"], 0),
		TargetHeight:           intFromArg(args["target_height"], 0),
		NormalizeAudioLoudness: boolPtrFromArg(args["normalize_audio_loudness"]),
	})
	if err != nil {
		return errorResult("build live subject clip plan: " + err.Error()), nil
	}
	return textResult(plan)
}

func buildLiveClipManifestHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.LiveSliceSvc == nil {
		return errorResult("live slice service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	sourceVideo, _ := args["source_video"].(string)
	if sourceVideo == "" {
		return errorResult("source_video is required"), nil
	}
	tingwuTaskID, _ := args["tingwu_task_id"].(string)
	if tingwuTaskID == "" {
		return errorResult("tingwu_task_id is required"), nil
	}
	sentences, err := parseLiveSentences(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	invalid, err := parseLiveInvalids(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	warnings, err := parseLiveNotices(args, "warnings")
	if err != nil {
		return errorResult(err.Error()), nil
	}
	rejectedNotices, err := parseLiveNotices(args, "rejected")
	if err != nil {
		return errorResult(err.Error()), nil
	}
	rejected := make([]service.LiveRejected, 0, len(rejectedNotices))
	for _, notice := range rejectedNotices {
		rejected = append(rejected, service.LiveRejected{Index: notice.Index, Title: notice.Title, Reason: notice.Reason})
	}
	clips, err := parseLiveClips(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	results, err := parseLiveClipResults(args)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	analysisTitle, _ := args["analysis_title"].(string)
	manifest, err := svcs.LiveSliceSvc.BuildLiveClipManifest(service.LiveClipManifestRequest{
		SourceVideo:   sourceVideo,
		TingWuTaskID:  tingwuTaskID,
		AnalysisTitle: analysisTitle,
		Sentences:     sentences,
		Invalid:       invalid,
		Warnings:      warnings,
		Rejected:      rejected,
		Clips:         clips,
		ClipResults:   results,
	})
	if err != nil {
		return errorResult("build live clip manifest: " + err.Error()), nil
	}
	return textResult(manifest)
}

func parseLiveSentences(args map[string]any) ([]service.LiveSentence, error) {
	raw, ok := args["sentences"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("sentences is required")
	}
	sentences := make([]service.LiveSentence, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("sentences[%d] must be an object", i)
		}
		text, _ := obj["text"].(string)
		if text == "" {
			return nil, fmt.Errorf("sentences[%d].text is required", i)
		}
		index := intFromArg(obj["index"], 0)
		if index <= 0 {
			return nil, fmt.Errorf("sentences[%d].index must be positive", i)
		}
		sentences = append(sentences, service.LiveSentence{
			Index: int64(index),
			Start: floatFromArg(obj["start"], 0),
			End:   floatFromArg(obj["end"], 0),
			Text:  text,
		})
	}
	return sentences, nil
}

func parseLiveSegments(args map[string]any) ([]service.LiveSegment, error) {
	raw, ok := args["segments"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("segments is required")
	}
	segments := make([]service.LiveSegment, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("segments[%d] must be an object", i)
		}
		segments = append(segments, service.LiveSegment{
			Title:       stringFromArg(obj["title"]),
			Description: stringFromArg(obj["description"]),
			Thoughts:    stringFromArg(obj["thoughts"]),
			Start:       int64(intFromArg(obj["start"], 0)),
			End:         int64(intFromArg(obj["end"], 0)),
		})
	}
	return segments, nil
}

func parseLiveSubjectCompletions(args map[string]any) ([]service.LiveSubjectCompletion, error) {
	raw, ok := args["completions"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("completions is required")
	}
	completions := make([]service.LiveSubjectCompletion, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("completions[%d] must be an object", i)
		}
		sentences, err := parseLiveCompletionSentences(obj["sentences"], fmt.Sprintf("completions[%d].sentences", i))
		if err != nil {
			return nil, err
		}
		completions = append(completions, service.LiveSubjectCompletion{
			Title:     stringFromArg(obj["title"]),
			Subtitle:  stringFromArg(obj["subtitle"]),
			Thoughts:  stringFromArg(obj["thoughts"]),
			Sentences: sentences,
		})
	}
	return completions, nil
}

func parseLiveCompletionSentences(value any, name string) ([]service.LiveSentence, error) {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("%s is required", name)
	}
	sentences := make([]service.LiveSentence, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", name, i)
		}
		index := intFromArg(obj["index"], -1)
		if index < 0 {
			return nil, fmt.Errorf("%s[%d].index must be >= 0", name, i)
		}
		text := stringFromArg(obj["text"])
		if index == 0 && text == "" {
			return nil, fmt.Errorf("%s[%d].text is required when index is 0", name, i)
		}
		sentences = append(sentences, service.LiveSentence{
			Index:  int64(index),
			Start:  floatFromArg(obj["start"], 0),
			End:    floatFromArg(obj["end"], 0),
			Text:   text,
			Reason: stringFromArg(obj["reason"]),
		})
	}
	return sentences, nil
}

func parseLiveInvalids(args map[string]any) ([]service.LiveInvalid, error) {
	raw, ok := args["invalid"].([]any)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	invalid := make([]service.LiveInvalid, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid[%d] must be an object", i)
		}
		index := intFromArg(obj["index"], 0)
		if index <= 0 {
			return nil, fmt.Errorf("invalid[%d].index must be positive", i)
		}
		invalid = append(invalid, service.LiveInvalid{Index: int64(index), Reason: stringFromArg(obj["reason"])})
	}
	return invalid, nil
}

func parseLiveNotices(args map[string]any, field string) ([]service.LiveClipNotice, error) {
	raw, ok := args[field].([]any)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	notices := make([]service.LiveClipNotice, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", field, i)
		}
		index := intFromArg(obj["index"], 0)
		if index <= 0 {
			return nil, fmt.Errorf("%s[%d].index must be positive", field, i)
		}
		notices = append(notices, service.LiveClipNotice{
			Index:  index,
			Title:  stringFromArg(obj["title"]),
			Reason: stringFromArg(obj["reason"]),
		})
	}
	return notices, nil
}

func parseLiveClips(args map[string]any) ([]service.LiveClip, error) {
	raw, ok := args["clips"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("clips is required")
	}
	clips := make([]service.LiveClip, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("clips[%d] must be an object", i)
		}
		transcript, err := parseLiveSentencesFromValue(obj["transcript"], fmt.Sprintf("clips[%d].transcript", i), false)
		if err != nil {
			return nil, err
		}
		parts, err := parseLiveClipParts(obj["parts"], fmt.Sprintf("clips[%d].parts", i))
		if err != nil {
			return nil, err
		}
		scriptNotes, err := parseLiveSentencesFromValue(obj["script_notes"], fmt.Sprintf("clips[%d].script_notes", i), false)
		if err != nil {
			return nil, err
		}
		concatArgs := stringArrayFromArg(obj["concat_args"])
		clips = append(clips, service.LiveClip{
			Index:             intFromArg(obj["index"], 0),
			Title:             stringFromArg(obj["title"]),
			Description:       stringFromArg(obj["description"]),
			Thoughts:          stringFromArg(obj["thoughts"]),
			SentenceStart:     int64(intFromArg(obj["sentence_start"], 0)),
			SentenceEnd:       int64(intFromArg(obj["sentence_end"], 0)),
			Start:             floatFromArg(obj["start"], 0),
			End:               floatFromArg(obj["end"], 0),
			Duration:          floatFromArg(obj["duration"], 0),
			Output:            stringFromArg(obj["output"]),
			Method:            stringFromArg(obj["method"]),
			Status:            stringFromArg(obj["status"]),
			Parts:             parts,
			ConcatListPath:    stringFromArg(obj["concat_list_path"]),
			ConcatListContent: stringFromArg(obj["concat_list_content"]),
			ConcatShell:       stringFromArg(obj["concat_shell"]),
			ConcatArgs:        concatArgs,
			Transcript:        transcript,
			ScriptNotes:       scriptNotes,
		})
	}
	return clips, nil
}

func parseLiveClipParts(value any, name string) ([]service.LiveClipPart, error) {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	parts := make([]service.LiveClipPart, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", name, i)
		}
		transcript, err := parseLiveSentencesFromValue(obj["transcript"], fmt.Sprintf("%s[%d].transcript", name, i), false)
		if err != nil {
			return nil, err
		}
		parts = append(parts, service.LiveClipPart{
			PartIndex:        intFromArg(obj["part_index"], 0),
			SentenceStart:    int64(intFromArg(obj["sentence_start"], 0)),
			SentenceEnd:      int64(intFromArg(obj["sentence_end"], 0)),
			Start:            floatFromArg(obj["start"], 0),
			End:              floatFromArg(obj["end"], 0),
			Duration:         floatFromArg(obj["duration"], 0),
			Output:           stringFromArg(obj["output"]),
			AccurateCutShell: stringFromArg(obj["accurate_cut_shell"]),
			AccurateCutArgs:  stringArrayFromArg(obj["accurate_cut_args"]),
			Transcript:       transcript,
		})
	}
	return parts, nil
}

func parseLiveClipResults(args map[string]any) ([]service.LiveClipExecutionResult, error) {
	raw, ok := args["clip_results"].([]any)
	if !ok || len(raw) == 0 {
		return nil, fmt.Errorf("clip_results is required")
	}
	results := make([]service.LiveClipExecutionResult, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("clip_results[%d] must be an object", i)
		}
		index := intFromArg(obj["index"], 0)
		if index <= 0 {
			return nil, fmt.Errorf("clip_results[%d].index must be positive", i)
		}
		status := stringFromArg(obj["status"])
		if status == "" {
			return nil, fmt.Errorf("clip_results[%d].status is required", i)
		}
		partResults, err := parseLiveClipPartResults(obj["part_results"], fmt.Sprintf("clip_results[%d].part_results", i))
		if err != nil {
			return nil, err
		}
		results = append(results, service.LiveClipExecutionResult{
			Index:                 index,
			Status:                status,
			Method:                stringFromArg(obj["method"]),
			Output:                stringFromArg(obj["output"]),
			ExitCode:              intFromArg(obj["exit_code"], 0),
			Error:                 stringFromArg(obj["error"]),
			Size:                  int64(intFromArg(obj["size"], 0)),
			ActualDurationSeconds: floatFromArg(obj["actual_duration_seconds"], 0),
			PartResults:           partResults,
		})
	}
	return results, nil
}

func parseLiveClipPartResults(value any, name string) ([]service.LiveClipPartExecutionResult, error) {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	results := make([]service.LiveClipPartExecutionResult, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", name, i)
		}
		partIndex := intFromArg(obj["part_index"], 0)
		if partIndex <= 0 {
			return nil, fmt.Errorf("%s[%d].part_index must be positive", name, i)
		}
		status := stringFromArg(obj["status"])
		if status == "" {
			return nil, fmt.Errorf("%s[%d].status is required", name, i)
		}
		results = append(results, service.LiveClipPartExecutionResult{
			PartIndex:             partIndex,
			Status:                status,
			Method:                stringFromArg(obj["method"]),
			Output:                stringFromArg(obj["output"]),
			ExitCode:              intFromArg(obj["exit_code"], 0),
			Error:                 stringFromArg(obj["error"]),
			Size:                  int64(intFromArg(obj["size"], 0)),
			ActualDurationSeconds: floatFromArg(obj["actual_duration_seconds"], 0),
		})
	}
	return results, nil
}

func parseLiveSentencesFromValue(value any, name string, required bool) ([]service.LiveSentence, error) {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		if required {
			return nil, fmt.Errorf("%s is required", name)
		}
		return nil, nil
	}
	args := map[string]any{"sentences": raw}
	return parseLiveSentences(args)
}

func stringArrayFromArg(value any) []string {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func stringFromArg(v any) string {
	s, _ := v.(string)
	return s
}

func boolFromArg(v any, fallback bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return fallback
}

// boolPtrFromArg returns nil when the argument is absent so request structs can default
// (a nil *bool is interpreted as the documented default, e.g. loudnorm enabled).
func boolPtrFromArg(v any) *bool {
	if b, ok := v.(bool); ok {
		return &b
	}
	return nil
}

func intFromArg(v any, fallback int) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case jsonNumber:
		i, err := n.Int64()
		if err == nil {
			return int(i)
		}
	}
	return fallback
}

func floatFromArg(v any, fallback float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case jsonNumber:
		f, err := n.Float64()
		if err == nil {
			return f
		}
	}
	return fallback
}

type jsonNumber interface {
	Int64() (int64, error)
	Float64() (float64, error)
}
