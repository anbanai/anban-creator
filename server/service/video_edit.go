package service

import (
	"context"
	"fmt"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func validateAndFreezeVideoEditAssets(ctx context.Context, repo repository.Repository, storageProvider, userID string, input *model.VideoEditInput, limits srvconfig.VideoEditConfig) error {
	if repo == nil || input == nil {
		return fmt.Errorf("video-edit source asset repository is unavailable")
	}
	maxAssetBytes := limits.MaxAssetBytes
	if maxAssetBytes <= 0 {
		maxAssetBytes = 256 << 20
	}
	maxTotalInputBytes := limits.MaxTotalInputBytes
	if maxTotalInputBytes <= 0 {
		maxTotalInputBytes = 1 << 30
	}
	assets := make([]*model.Asset, len(input.SourceAssets))
	totalBytes := int64(0)
	for i := range input.SourceAssets {
		assetInput := &input.SourceAssets[i]
		asset, err := repo.Assets().FindOwnedByID(ctx, assetInput.AssetID, userID)
		if err != nil || asset == nil {
			return fmt.Errorf("video_edit_input.source_assets[%d].asset_id is unavailable", i)
		}
		if err := validateVideoEditSourceAssetMaterializable(asset, userID, storageProvider, assetInput, maxAssetBytes); err != nil {
			return fmt.Errorf("video_edit_input.source_assets[%d] %v", i, err)
		}
		if asset.Size > maxTotalInputBytes-totalBytes {
			return fmt.Errorf("video_edit_input.source_assets exceeds %d bytes in total", maxTotalInputBytes)
		}
		totalBytes += asset.Size
		assets[i] = asset
	}
	for i, asset := range assets {
		input.SourceAssets[i].AssetID = asset.ID
		input.SourceAssets[i].FileName = asset.FileName
		input.SourceAssets[i].MimeType = asset.ContentType
		input.SourceAssets[i].FileSize = asset.Size
	}
	return nil
}
