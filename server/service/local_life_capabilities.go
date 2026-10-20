package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrLocalLifeDisabled = errors.New("素材混剪当前未启用")

type LocalLifeCapabilityCatalog struct {
	Enabled             bool                     `json:"enabled"`
	DefaultMixType      string                   `json:"default_mix_type"`
	DefaultVariantCount int                      `json:"default_variant_count"`
	MaxVariantCount     int                      `json:"max_variant_count"`
	DefaultDuration     int64                    `json:"default_duration_seconds"`
	MaxDuration         int64                    `json:"max_duration_seconds"`
	AspectRatios        []string                 `json:"aspect_ratios"`
	Types               []model.LocalLifeMixType `json:"types"`
}

type LocalLifeCapabilityService struct {
	enabled bool
}

func NewLocalLifeCapabilityService(enabled ...bool) *LocalLifeCapabilityService {
	value := false
	if len(enabled) > 0 {
		value = enabled[0]
	}
	return &LocalLifeCapabilityService{enabled: value}
}

func (s *LocalLifeCapabilityService) Catalog() LocalLifeCapabilityCatalog {
	enabled := s != nil && s.enabled
	return LocalLifeCapabilityCatalog{
		Enabled: enabled, DefaultMixType: "general-store", DefaultVariantCount: 3, MaxVariantCount: 10,
		DefaultDuration: 30, MaxDuration: 60, AspectRatios: []string{"9:16", "16:9", "1:1"}, Types: model.LocalLifeMixTypes(),
	}
}

func (s *LocalLifeCapabilityService) Enabled() bool {
	return s != nil && s.enabled
}

func validateMaterializableLocalLifeSourceTaskFiles(ctx context.Context, repo repository.Repository, expectedStorageProvider, userID, projectID string, assets []model.LocalLifeAsset, trustedSources ...montageSourceTaskTrust) error {
	converted := make([]model.MontageAsset, 0, len(assets))
	for _, asset := range assets {
		converted = append(converted, model.MontageAsset{Type: asset.Type, TaskFileID: asset.TaskFileID})
	}
	if err := ValidateMaterializableMontageSourceTaskFiles(ctx, repo, expectedStorageProvider, userID, projectID, converted, trustedSources...); err != nil {
		return fmt.Errorf("%w: %v", ErrMontageInput, err)
	}
	return nil
}
