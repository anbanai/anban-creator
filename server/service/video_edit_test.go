package service

import (
	"path"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestValidateAndFreezeVideoEditAssetsUsesOwnedAssetMetadata(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	asset := &model.Asset{
		ID: "asset-1", UserID: "user-1", Purpose: DirectUploadPurposeVideoEditAsset,
		StorageKey: "assets/users/user-1/asset-1/clip.mp4", FileName: "clip.mp4",
		ContentType: "video/mp4", Size: 1024, ETag: "verified-etag",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	input := &model.VideoEditInput{SourceAssets: []model.VideoEditAsset{{Type: "video", AssetID: asset.ID}}}
	if err := validateAndFreezeVideoEditAssets(t.Context(), repo, "oss", "user-1", input, config.VideoEditConfig{MaxAssetBytes: 1 << 20, MaxTotalInputBytes: 1 << 20}); err != nil {
		t.Fatalf("validateAndFreezeVideoEditAssets: %v", err)
	}
	got := input.SourceAssets[0]
	if got.FileName != asset.FileName || got.MimeType != asset.ContentType || got.FileSize != asset.Size {
		t.Fatalf("frozen source metadata = %#v, want asset metadata %#v", got, asset)
	}
}

func TestValidateAndFreezeVideoEditAssetsAcceptsAssistantOwnedMedia(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	asset := &model.Asset{
		ID: "assistant-video", UserID: "user-1", Purpose: DirectUploadPurposeAIEntryAttachment,
		StorageKey: "assets/users/user-1/assistant-video/clip.mp4", FileName: "clip.mp4",
		ContentType: "video/mp4", Size: 2048, ETag: "verified-etag",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	input := &model.VideoEditInput{SourceAssets: []model.VideoEditAsset{{Type: "video", AssetID: asset.ID}}}
	if err := validateAndFreezeVideoEditAssets(t.Context(), repo, "oss", "user-1", input, config.VideoEditConfig{MaxAssetBytes: 1 << 20, MaxTotalInputBytes: 1 << 20}); err != nil {
		t.Fatalf("validateAndFreezeVideoEditAssets assistant asset: %v", err)
	}
	if got := input.SourceAssets[0].FileName; got != asset.FileName {
		t.Fatalf("frozen assistant filename = %q, want %q", got, asset.FileName)
	}
}

func TestValidateAndFreezeVideoEditAssetsRejectsForeignAssetsAndTotalOverflow(t *testing.T) {
	t.Run("foreign asset", func(t *testing.T) {
		repo := openBootstrapTestRepository(t)
		asset := &model.Asset{
			ID: "foreign-asset", UserID: "user-2", Purpose: DirectUploadPurposeVideoEditAsset,
			StorageKey: "assets/users/user-2/foreign-asset/clip.mp4", FileName: "clip.mp4",
			ContentType: "video/mp4", Size: 1024, ETag: "verified-etag",
		}
		if err := repo.Assets().Create(t.Context(), asset); err != nil {
			t.Fatal(err)
		}
		input := &model.VideoEditInput{SourceAssets: []model.VideoEditAsset{{Type: "video", AssetID: asset.ID}}}
		if err := validateAndFreezeVideoEditAssets(t.Context(), repo, "oss", "user-1", input, config.VideoEditConfig{}); err == nil {
			t.Fatal("foreign asset was accepted")
		}
	})

	t.Run("aggregate size", func(t *testing.T) {
		repo := openBootstrapTestRepository(t)
		assets := make([]model.VideoEditAsset, 5)
		for i := range assets {
			id := "asset-" + string(rune('1'+i))
			fileName := id + ".mp4"
			asset := &model.Asset{
				ID: id, UserID: "user-1", Purpose: DirectUploadPurposeVideoEditAsset,
				StorageKey: path.Join("assets/users/user-1", id, fileName), FileName: fileName,
				ContentType: "video/mp4", Size: 256 << 20, ETag: "verified-etag",
			}
			if err := repo.Assets().Create(t.Context(), asset); err != nil {
				t.Fatal(err)
			}
			assets[i] = model.VideoEditAsset{Type: "video", AssetID: id}
		}
		input := &model.VideoEditInput{SourceAssets: assets}
		err := validateAndFreezeVideoEditAssets(t.Context(), repo, "oss", "user-1", input, config.VideoEditConfig{MaxAssetBytes: 256 << 20, MaxTotalInputBytes: 1 << 30})
		if err == nil || !strings.Contains(err.Error(), "exceeds 1073741824 bytes in total") {
			t.Fatalf("validateAndFreezeVideoEditAssets error = %v, want aggregate-size rejection", err)
		}
	})
}
