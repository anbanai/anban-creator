package service

import (
	"context"

	"github.com/anbanai/anban-creator/server/storage"
)

type MediaPipelineStatusRequest struct{}

type MediaPipelineStatusResult struct {
	StorageConfigured bool              `json:"storage_configured"`
	OSSDirectUpload   bool              `json:"oss_direct_upload"`
	TingWuConfigured  bool              `json:"tingwu_configured"`
	Missing           []string          `json:"missing"`
	Hints             map[string]string `json:"hints"`
}

type MediaPipelineService struct {
	store            storage.Provider
	tingwuConfigured bool
}

func NewMediaPipelineService(store storage.Provider, tingwuConfigured bool) *MediaPipelineService {
	return &MediaPipelineService{store: store, tingwuConfigured: tingwuConfigured}
}

func (s *MediaPipelineService) Status(context.Context, MediaPipelineStatusRequest) *MediaPipelineStatusResult {
	storageConfigured := s != nil && s.store != nil
	ossDirectUpload := storageConfigured && s.store.Name() == "oss"
	tingwuConfigured := s != nil && s.tingwuConfigured
	missing := []string{}
	if !ossDirectUpload {
		missing = append(missing, "oss storage for prepare_file_upload direct uploads")
	}
	if !tingwuConfigured {
		missing = append(missing, "tingwu endpoint/region/app_key/access_key/access_secret for live-slicer")
	}
	return &MediaPipelineStatusResult{
		StorageConfigured: storageConfigured,
		OSSDirectUpload:   ossDirectUpload,
		TingWuConfigured:  tingwuConfigured,
		Missing:           missing,
		Hints: map[string]string{
			"live_audio_upload": "Use prepare_file_upload(purpose=\"live_audio\"), PUT the agent-local audio file to upload_url, then pass audio_key to create_live_analysis_task.",
		},
	}
}
