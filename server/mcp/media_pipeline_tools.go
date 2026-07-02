package mcp

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerMediaPipelineTools(server *mcp.Server) {
	server.AddTool(&mcp.Tool{
		Name:        "get_media_pipeline_status",
		Description: "Return safe readiness diagnostics for media upload, live-slice TingWu analysis, and audio ASR through Aliyun FunASR HTTP. Does not return secrets or signed URLs.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, getMediaPipelineStatusHandler)
}

func getMediaPipelineStatusHandler(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	storageConfigured := svcs != nil && svcs.Store != nil
	ossDirectUpload := storageConfigured && svcs.Store.Name() == "oss"
	tingwuConfigured := svcs != nil && svcs.TingWuConfigured
	funasrConfigured := svcs != nil && svcs.FunASRConfigured

	missing := []string{}
	if !ossDirectUpload {
		missing = append(missing, "oss storage for prepare_file_upload direct uploads")
	}
	if !tingwuConfigured {
		missing = append(missing, "tingwu endpoint/region/app_key/access_key/access_secret for live-slicer")
	}
	if !funasrConfigured {
		missing = append(missing, "funasr base_url/api_key for audio ASR")
	}

	return textResult(map[string]any{
		"storage_configured": storageConfigured,
		"oss_direct_upload":  ossDirectUpload,
		"tingwu_configured":  tingwuConfigured,
		"funasr_configured":  funasrConfigured,
		"missing":            missing,
		"hints": map[string]string{
			"live_audio_upload":  "Use prepare_file_upload(purpose=\"live_audio\"), PUT the agent-local audio file to upload_url, then pass audio_key to create_live_analysis_task.",
			"video_audio_upload": "Use prepare_file_upload(purpose=\"video_audio\"), PUT the agent-local wav to upload_url, then pass audio_key to create_video_asr_task.",
			"funasr_endpoint":    "funasr.base_url must be an Aliyun MaaS regional host such as https://{WorkspaceId}.cn-beijing.maas.aliyuncs.com.",
		},
	})
}
