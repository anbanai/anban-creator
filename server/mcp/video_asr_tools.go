package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/royalrick/anbanwriter/server/service"
)

func registerVideoASRTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "upload_video_audio",
		Description: "Upload a local audio file extracted from source video to configured OSS/CDN storage. Optional for video-use; OpenAI-compatible FunASR transcription normally uses local file_path directly.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path":       map[string]any{"type": "string", "description": "Local audio file path generated with ffmpeg by the video-use workflow"},
				"expires_seconds": map[string]any{"type": "integer", "description": "Signed URL TTL in seconds when no custom OSS domain is configured", "default": 86400},
			},
			"required": []any{"file_path"},
		},
	}, uploadVideoAudioHandler)

	server.AddTool(&mcp.Tool{
		Name:        "create_video_asr_task",
		Description: "Transcribe a local audio file through the server-side OpenAI-compatible FunASR API and return normalized video-use transcript JSON. API keys stay on the server.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file_path":     map[string]any{"type": "string", "description": "Local audio file path generated with ffmpeg by the video-use workflow"},
				"language_hint": map[string]any{"type": "string", "description": "Optional language hint such as zh or en"},
				"speaker_count": map[string]any{"type": "integer", "description": "Optional expected speaker count for diarization"},
			},
			"required": []any{"file_path"},
		},
	}, createVideoASRTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "query_video_asr_task",
		Description: "Return a completed local FunASR transcription result by task_id. create_video_asr_task is synchronous; this is a compatibility cache lookup.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Completed local FunASR result ID returned by create_video_asr_task"},
			},
			"required": []any{"task_id"},
		},
	}, queryVideoASRTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "pack_video_transcripts",
		Description: "Pack normalized word-level video transcripts into the video-use takes_packed.md markdown view. Does not read or write files.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"transcripts":       map[string]any{"type": "object", "description": "Map of take/source name to normalized transcript JSON"},
				"silence_threshold": map[string]any{"type": "number", "default": 0.5},
			},
			"required": []any{"transcripts"},
		},
	}, packVideoTranscriptsHandler)
}

func uploadVideoAudioHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.VideoASRSvc == nil {
		return errorResult("video ASR service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	filePath, _ := args["file_path"].(string)
	expires := 0
	if v, ok := numberAsInt64(args["expires_seconds"]); ok {
		expires = int(v)
	}
	result, err := svcs.VideoASRSvc.UploadAudio(ctx, filePath, expires)
	if err != nil {
		return errorResult("upload video audio: " + err.Error()), nil
	}
	return textResult(result)
}

func createVideoASRTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.VideoASRSvc == nil {
		return errorResult("video ASR service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	filePath, _ := args["file_path"].(string)
	languageHint, _ := args["language_hint"].(string)
	speakerCount := 0
	if v, ok := numberAsInt64(args["speaker_count"]); ok {
		speakerCount = int(v)
	}
	result, err := svcs.VideoASRSvc.CreateTask(ctx, service.VideoASRTaskRequest{
		FilePath:     filePath,
		LanguageHint: languageHint,
		SpeakerCount: speakerCount,
	})
	if err != nil {
		return errorResult("create video ASR task: " + err.Error()), nil
	}
	return textResult(result)
}

func queryVideoASRTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.VideoASRSvc == nil {
		return errorResult("video ASR service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	result, err := svcs.VideoASRSvc.QueryTask(ctx, taskID)
	if err != nil {
		return errorResult("query video ASR task: " + err.Error()), nil
	}
	return textResult(result)
}

func packVideoTranscriptsHandler(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := parseArgs(req.Params.Arguments)
	transcripts, err := parseVideoTranscriptMap(args["transcripts"])
	if err != nil {
		return errorResult(err.Error()), nil
	}
	threshold := 0.0
	if v, ok := args["silence_threshold"].(float64); ok {
		threshold = v
	}
	md, err := service.PackVideoTranscripts(transcripts, threshold)
	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(map[string]any{"takes_packed_markdown": md})
}

func parseVideoTranscriptMap(raw any) (map[string]service.VideoTranscript, error) {
	items, ok := raw.(map[string]any)
	if !ok || len(items) == 0 {
		return nil, fmt.Errorf("transcripts is required")
	}
	out := make(map[string]service.VideoTranscript, len(items))
	for name, item := range items {
		rawJSON, err := json.Marshal(item)
		if err != nil {
			return nil, fmt.Errorf("marshal transcript %s: %w", name, err)
		}
		var transcript service.VideoTranscript
		if err := json.Unmarshal(rawJSON, &transcript); err != nil {
			return nil, fmt.Errorf("decode transcript %s: %w", name, err)
		}
		out[name] = transcript
	}
	return out, nil
}
