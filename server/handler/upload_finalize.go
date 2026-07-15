package handler

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

func finalizePendingURLs(ctx context.Context, repo service.PendingUploadRepository, userID, purpose string, urls []string, stores ...storage.Provider) error {
	if repo == nil || len(urls) == 0 {
		return nil
	}
	var ownedURLChecks []func(string) bool
	if len(stores) > 0 && stores[0] != nil {
		ownedURLChecks = append(ownedURLChecks, stores[0].IsOwnedURL)
	}
	return service.FinalizePendingUploadURLs(ctx, repo, userID, purpose, urls, time.Now(), ownedURLChecks...)
}

func videoReferenceURLs(cfg *model.VideoTaskConfig) []string {
	if cfg == nil || len(cfg.References) == 0 {
		return nil
	}
	urls := make([]string, 0, len(cfg.References))
	for _, ref := range cfg.References {
		if ref.URL != "" {
			urls = append(urls, ref.URL)
		}
	}
	return urls
}

func videoConfigForReferenceURLs(cfg *model.VideoTaskConfig, input *model.VideoInput) *model.VideoTaskConfig {
	if input == nil {
		return cfg
	}
	merged := model.VideoTaskConfig{}
	if cfg != nil {
		merged = *cfg
	}
	merged.References = append(merged.References, input.References...)
	return &merged
}
