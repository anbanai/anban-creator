package service

import (
	"context"
	"fmt"
	"strings"

	appconfig "github.com/anbanai/anban-creator/app/config"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type ImageCapabilityResolver struct {
	repo repository.Repository
	cfg  *config.Config
}

func NewImageCapabilityResolver(repo repository.Repository, cfg *config.Config) *ImageCapabilityResolver {
	return &ImageCapabilityResolver{repo: repo, cfg: cfg}
}

func (s *ImageCapabilityResolver) ResolveImageConfigForTaskKey(ctx context.Context, userID, capabilityKey string) (*config.ImageAPIConfig, string, error) {
	if s == nil || s.cfg == nil {
		return nil, "", fmt.Errorf("image capability resolver is unavailable")
	}
	route, key, err := s.resolveRoute(ctx, userID, capabilityKey)
	if err != nil {
		return nil, "", err
	}
	runtime, ok := s.cfg.ImageAPIForCapability(key)
	if !ok || runtime == nil {
		return nil, "", fmt.Errorf("image capability %q has no runtime configuration", key)
	}
	_ = route
	return runtime, "capability:" + key, nil
}

type ImageReferenceLimitError struct {
	Requested          int `json:"requested"`
	MaxReferenceImages int `json:"max_reference_images"`
}

func (e *ImageReferenceLimitError) Error() string {
	if e == nil {
		return "image reference limit exceeded"
	}
	return fmt.Sprintf("requested %d reference images exceeds capability limit %d", e.Requested, e.MaxReferenceImages)
}

type ResolvedImageModel struct {
	Config             *config.ImageAPIConfig `json:"-"`
	Key                string                 `json:"-"`
	BillingSKU         string                 `json:"-"`
	Provider           string                 `json:"-"`
	Model              string                 `json:"-"`
	Source             string                 `json:"-"`
	SupportsReference  bool                   `json:"-"`
	MaxReferenceImages int                    `json:"-"`
	SelectionReason    string                 `json:"-"`
}

func (s *ImageCapabilityResolver) ResolveImageModelForGeneration(
	ctx context.Context,
	userID string,
	capabilityKey string,
	imageType string,
	referenceCount int,
) (*ResolvedImageModel, error) {
	imageType, err := normalizeGenerationImageType(imageType)
	if err != nil {
		return nil, err
	}
	if referenceCount < 0 {
		return nil, fmt.Errorf("reference count must not be negative")
	}
	route, key, err := s.resolveRoute(ctx, userID, capabilityKey)
	if err != nil {
		return nil, err
	}
	if referenceCount > 0 && !route.Features.SupportsReference {
		return nil, fmt.Errorf("selected image capability does not support reference images")
	}
	if referenceCount > route.Features.MaxReferenceImages {
		return nil, &ImageReferenceLimitError{Requested: referenceCount, MaxReferenceImages: route.Features.MaxReferenceImages}
	}
	runtime, ok := s.cfg.ImageAPIForCapability(key)
	if !ok || runtime == nil {
		return nil, fmt.Errorf("image capability %q has no runtime configuration", key)
	}
	api := imageAPIForType(runtime, imageType)
	if api == nil || strings.TrimSpace(api.Provider) == "" || strings.TrimSpace(api.Model) == "" {
		return nil, fmt.Errorf("image capability %q is incomplete", key)
	}
	return &ResolvedImageModel{
		Config: runtime, Key: key, BillingSKU: strings.TrimSpace(route.BillingSKU),
		Provider: strings.TrimSpace(api.Provider), Model: strings.TrimSpace(api.Model),
		Source: "capability:" + key, SupportsReference: route.Features.SupportsReference,
		MaxReferenceImages: route.Features.MaxReferenceImages, SelectionReason: "task_capability",
	}, nil
}

func (s *ImageCapabilityResolver) resolveRoute(ctx context.Context, userID, capabilityKey string) (config.ImageGenerationRouteConfig, string, error) {
	key := strings.TrimSpace(capabilityKey)
	if key == "" {
		key = strings.TrimSpace(s.cfg.ModelRoutes.ImageGeneration.DefaultCapability)
	}
	route, ok := s.cfg.ImageCapability(key)
	if !ok || !route.Enabled {
		return config.ImageGenerationRouteConfig{}, "", fmt.Errorf("unknown image capability key %q", key)
	}
	tier := model.TierFree
	if s.repo != nil {
		user, err := s.repo.Users().FindByID(ctx, userID)
		if err != nil {
			return config.ImageGenerationRouteConfig{}, "", fmt.Errorf("resolve image capability user tier: %w", err)
		}
		if user != nil {
			tier = model.ResolveTier(user.Tier)
		}
	}
	required := model.NormalizeTier(route.MinTier)
	if !model.TierSatisfies(tier, required) {
		return config.ImageGenerationRouteConfig{}, "", fmt.Errorf("image capability %q requires %s tier", key, required)
	}
	return route, key, nil
}

func normalizeGenerationImageType(imageType string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(imageType)); normalized {
	case "", "content":
		return "content", nil
	case "cover":
		return "cover", nil
	default:
		return "", fmt.Errorf("unsupported image_type %q", imageType)
	}
}

func imageAPIForType(cfg *config.ImageAPIConfig, imageType string) *appconfig.ImageAPI {
	if cfg == nil {
		return nil
	}
	if imageType == "cover" {
		return cfg.Cover
	}
	return cfg.Content
}

func imageProviderKind(provider string) string {
	key := strings.ToLower(strings.TrimSpace(provider))
	switch {
	case strings.Contains(key, "volc"):
		return "volcengine"
	case strings.Contains(key, "gemini") || strings.Contains(key, "google"):
		return "gemini"
	case strings.Contains(key, "openai"), strings.Contains(key, "moonshot"), strings.Contains(key, "kimi"), strings.Contains(key, "wangcai"):
		return "openai"
	default:
		return key
	}
}
