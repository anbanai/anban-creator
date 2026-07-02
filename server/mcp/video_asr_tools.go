package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/anbanai/anban-creator/server/service"
)

func registerVideoASRTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "prepare_file_upload",
		Description: "Prepare a policy-controlled OSS direct upload for agent/client-local files. Use purpose=video_audio for video-use ASR or purpose=live_audio for live-slicer TingWu, upload the local file to upload_url with PUT, then pass audio_key to the matching task tool.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"purpose":         map[string]any{"type": "string", "description": "Upload purpose. Supported: video_audio, live_audio"},
				"filename":        map[string]any{"type": "string", "description": "Original local filename, used only to preserve extension"},
				"content_type":    map[string]any{"type": "string", "description": "MIME type for the object, e.g. audio/wav"},
				"expires_seconds": map[string]any{"type": "integer", "description": "Signed upload/download URL TTL in seconds", "default": 86400},
			},
			"required": []any{"purpose", "filename", "content_type"},
		},
	}, prepareFileUploadHandler)

	server.AddTool(&mcp.Tool{
		Name:        "create_video_asr_task",
		Description: "Transcribe an OSS-backed audio object or HTTPS audio URL through server-side Aliyun FunASR HTTP and return a compact receipt with transcript_object_key/download_url. API keys stay on the server.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"audio_key":     map[string]any{"type": "string", "description": "OSS object key returned by prepare_file_upload for purpose=video_audio"},
				"audio_url":     map[string]any{"type": "string", "description": "Existing public or signed HTTPS audio URL"},
				"language_hint": map[string]any{"type": "string", "description": "Optional language hint such as zh or en"},
				"speaker_count": map[string]any{"type": "integer", "description": "Optional expected speaker count for diarization"},
			},
		},
	}, createVideoASRTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "query_video_asr_task",
		Description: "Return a completed Aliyun Fun-ASR transcription result by task_id. create_video_asr_task is synchronous; this is a compatibility cache lookup.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id": map[string]any{"type": "string", "description": "Completed FunASR result ID returned by create_video_asr_task"},
			},
			"required": []any{"task_id"},
		},
	}, queryVideoASRTaskHandler)

	server.AddTool(&mcp.Tool{
		Name:        "prepare_video_transcript_download",
		Description: "Return a signed download URL for a normalized video-use transcript JSON object. Agents should save it locally with anban video save-asr-result.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"task_id":               map[string]any{"type": "string", "description": "Completed ASR task ID"},
				"transcript_object_key": map[string]any{"type": "string", "description": "Transcript object key returned by create_video_asr_task"},
				"expires_seconds":       map[string]any{"type": "integer", "default": 86400},
			},
		},
	}, prepareVideoTranscriptDownloadHandler)

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

func prepareFileUploadHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.Store == nil {
		return errorResult("storage provider is not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	purpose, _ := args["purpose"].(string)
	filename, _ := args["filename"].(string)
	contentType, _ := args["content_type"].(string)
	expires := 0
	if v, ok := numberAsInt64(args["expires_seconds"]); ok {
		expires = int(v)
	}
	result, err := service.PrepareFileUpload(ctx, svcs.Store, service.FileUploadPrepareRequest{
		Purpose:        purpose,
		Filename:       filename,
		ContentType:    contentType,
		ExpiresSeconds: expires,
	})
	if err != nil {
		return errorResult("prepare file upload: " + err.Error()), nil
	}
	return textResult(result)
}

func createVideoASRTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	audioASRSvc := currentAudioASRService()
	if audioASRSvc == nil {
		return errorResult("audio ASR service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	filePath, _ := args["file_path"].(string)
	audioKey, _ := args["audio_key"].(string)
	audioURL, _ := args["audio_url"].(string)
	languageHint, _ := args["language_hint"].(string)
	speakerCount := 0
	if v, ok := numberAsInt64(args["speaker_count"]); ok {
		speakerCount = int(v)
	}
	result, err := audioASRSvc.CreateTask(ctx, service.AudioASRTaskRequest{
		FilePath:     filePath,
		AudioKey:     audioKey,
		AudioURL:     audioURL,
		LanguageHint: languageHint,
		SpeakerCount: speakerCount,
	})
	if err != nil {
		return errorResult("create video ASR task: " + err.Error()), nil
	}
	return textResult(audioASRReceipt(result))
}

func queryVideoASRTaskHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	audioASRSvc := currentAudioASRService()
	if audioASRSvc == nil {
		return errorResult("audio ASR service not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	result, err := audioASRSvc.QueryTask(ctx, taskID)
	if err != nil {
		return errorResult("query video ASR task: " + err.Error()), nil
	}
	return textResult(audioASRReceipt(result))
}

func prepareVideoTranscriptDownloadHandler(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if svcs == nil || svcs.Store == nil {
		return errorResult("storage provider is not available"), nil
	}
	args := parseArgs(req.Params.Arguments)
	taskID, _ := args["task_id"].(string)
	key, _ := args["transcript_object_key"].(string)
	expires := 86400
	if v, ok := numberAsInt64(args["expires_seconds"]); ok && v > 0 {
		expires = int(v)
	}
	if strings.TrimSpace(key) == "" && strings.TrimSpace(taskID) != "" {
		if svc := currentAudioASRService(); svc != nil {
			result, err := svc.QueryTask(ctx, taskID)
			if err != nil {
				return errorResult("prepare video transcript download: " + err.Error()), nil
			}
			key = result.TranscriptKey
		}
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return errorResult("transcript_object_key or task_id is required"), nil
	}
	if !strings.HasPrefix(key, "uploads/video-transcripts/") {
		return errorResult("transcript_object_key must be under uploads/video-transcripts/"), nil
	}
	url, err := svcs.Store.DownloadURL(ctx, key, expires)
	if err != nil {
		return errorResult("prepare video transcript download: " + err.Error()), nil
	}
	return textResult(map[string]any{
		"transcript_object_key": key,
		"download_url":          url,
		"expires_seconds":       expires,
	})
}

func currentAudioASRService() *service.AudioASRService {
	if svcs == nil {
		return nil
	}
	if svcs.AudioASRSvc != nil {
		return svcs.AudioASRSvc
	}
	return svcs.VideoASRSvc
}

func audioASRReceipt(result *service.AudioASRTaskResult) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	receipt := map[string]any{
		"task_id":          result.TaskID,
		"status":           result.Status,
		"word_count":       result.WordCount,
		"duration_seconds": result.DurationSeconds,
	}
	if result.TranscriptKey != "" {
		receipt["transcript_object_key"] = result.TranscriptKey
	}
	if result.TranscriptURL != "" {
		receipt["download_url"] = result.TranscriptURL
	}
	if result.TranscriptionURL != "" {
		receipt["provider_transcription_url"] = result.TranscriptionURL
	}
	if result.Error != "" {
		receipt["error"] = result.Error
	}
	return receipt
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
