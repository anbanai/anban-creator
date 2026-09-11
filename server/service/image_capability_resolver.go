package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

var ErrImageCapabilityResolverUnavailable = errors.New("image capability resolver is unavailable")
var ErrTaskImageCapabilityMissing = errors.New("task requires a frozen image capability")
var ErrTaskImageCapabilityInvalid = errors.New("task image capability snapshot is invalid")
var ErrTaskImageCapabilityConflict = errors.New("task image capability snapshot conflicts with live configuration")

type ImageCapabilityResolver struct {
	repo repository.Repository
	cfg  *config.Config
}

type PublicImageCapability struct {
	Key string
}

func NewImageCapabilityResolver(repo repository.Repository, cfg *config.Config) *ImageCapabilityResolver {
	return &ImageCapabilityResolver{repo: repo, cfg: cfg}
}

func (s *ImageCapabilityResolver) ResolvePublicImageCapability(ctx context.Context, userID, capabilityKey string) (*PublicImageCapability, error) {
	_, key, err := s.resolveRoute(ctx, userID, capabilityKey)
	if err != nil {
		return nil, err
	}
	return &PublicImageCapability{Key: key}, nil
}

func (s *ImageCapabilityResolver) FreezeImageCapability(ctx context.Context, userID, capabilityKey string) (model.ImageCapabilitySnapshot, error) {
	route, key, err := s.resolveRoute(ctx, userID, capabilityKey)
	if err != nil {
		return model.ImageCapabilitySnapshot{}, err
	}
	snapshot, err := imageCapabilitySnapshot(key, route)
	if err != nil {
		return model.ImageCapabilitySnapshot{}, fmt.Errorf("freeze image capability %q: %w", key, err)
	}
	return snapshot, nil
}

func (s *ImageCapabilityResolver) ResolveImageConfigForTaskKey(ctx context.Context, userID, capabilityKey string) (*config.ImageAPIConfig, string, error) {
	if s == nil || s.cfg == nil {
		return nil, "", fmt.Errorf("image capability resolver is unavailable")
	}
	if strings.TrimSpace(capabilityKey) == "" {
		return nil, "", ErrTaskImageCapabilityMissing
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

func imageCapabilitySnapshot(key string, route config.ImageGenerationRouteConfig) (model.ImageCapabilitySnapshot, error) {
	snapshot := model.ImageCapabilitySnapshot{
		SchemaVersion: model.ImageCapabilitySnapshotSchemaVersion,
		Key:           strings.TrimSpace(key), Provider: strings.TrimSpace(route.Provider), Model: strings.TrimSpace(route.Model),
		BaseURL: strings.TrimSpace(route.BaseURL), TimeoutSeconds: int(route.Timeout / time.Second),
		ResponseFormat: strings.TrimSpace(route.ResponseFormat), BillingSKU: strings.TrimSpace(route.BillingSKU),
		MinTier: strings.TrimSpace(route.MinTier),
		GenerationFeatures: model.ImageGenerationFeaturesSnapshot{
			QualityLevels: append([]string(nil), route.GenerationFeatures.QualityLevels...),
			SizePresets:   append([]string(nil), route.GenerationFeatures.SizePresets...),
			DefaultSize:   strings.TrimSpace(route.GenerationFeatures.DefaultSize),
			MaxBatch:      route.GenerationFeatures.MaxBatch, MaxReferenceImages: route.GenerationFeatures.MaxReferenceImages,
			SupportsReference: route.GenerationFeatures.SupportsReference, SupportsMask: route.GenerationFeatures.SupportsMask,
			OutputFormats: append([]string(nil), route.GenerationFeatures.OutputFormats...),
			HasBackground: route.GenerationFeatures.HasBackground, HasCompression: route.GenerationFeatures.HasCompression,
			Watermark: route.GenerationFeatures.Watermark,
		},
	}
	if snapshot.Key == "" || snapshot.Provider == "" || snapshot.Model == "" || snapshot.BaseURL == "" ||
		snapshot.TimeoutSeconds <= 0 || snapshot.BillingSKU == "" || snapshot.MinTier == "" {
		return model.ImageCapabilitySnapshot{}, ErrTaskImageCapabilityInvalid
	}
	digest, err := snapshot.ComputedDigest()
	if err != nil {
		return model.ImageCapabilitySnapshot{}, err
	}
	snapshot.Digest = digest
	return snapshot, nil
}

func validateImageCapabilitySnapshot(snapshot model.ImageCapabilitySnapshot) error {
	if snapshot.SchemaVersion != model.ImageCapabilitySnapshotSchemaVersion || snapshot.Key == "" || snapshot.Provider == "" ||
		snapshot.Model == "" || snapshot.BaseURL == "" || snapshot.TimeoutSeconds <= 0 || snapshot.BillingSKU == "" ||
		snapshot.MinTier == "" || snapshot.Digest == "" {
		return ErrTaskImageCapabilityInvalid
	}
	digest, err := snapshot.ComputedDigest()
	if err != nil || digest != snapshot.Digest {
		return ErrTaskImageCapabilityInvalid
	}
	return nil
}

func (s *ImageCapabilityResolver) ResolveFrozenImageModelForGeneration(
	ctx context.Context,
	userID string,
	snapshot model.ImageCapabilitySnapshot,
	imageType string,
	referenceCount int,
) (*ResolvedImageModel, error) {
	if err := validateImageCapabilitySnapshot(snapshot); err != nil {
		return nil, err
	}
	imageType, err := normalizeGenerationImageType(imageType)
	if err != nil {
		return nil, err
	}
	if referenceCount < 0 {
		return nil, fmt.Errorf("reference count must not be negative")
	}
	route, key, err := s.resolveRoute(ctx, userID, snapshot.Key)
	if err != nil {
		return nil, err
	}
	live, err := imageCapabilitySnapshot(key, route)
	if err != nil {
		return nil, err
	}
	if live.Digest != snapshot.Digest {
		return nil, ErrTaskImageCapabilityConflict
	}
	if referenceCount > 0 && !snapshot.GenerationFeatures.SupportsReference {
		return nil, fmt.Errorf("selected image capability does not support reference images")
	}
	if referenceCount > snapshot.GenerationFeatures.MaxReferenceImages {
		return nil, &ImageReferenceLimitError{Requested: referenceCount, MaxReferenceImages: snapshot.GenerationFeatures.MaxReferenceImages}
	}
	runtime, ok := s.cfg.ImageAPIForCapability(snapshot.Key)
	if !ok || runtime == nil || runtime.API == nil || strings.TrimSpace(runtime.API.Key) == "" {
		return nil, fmt.Errorf("image capability %q has no runtime credential", snapshot.Key)
	}
	return &ResolvedImageModel{
		Config: runtime, Key: snapshot.Key, BillingSKU: snapshot.BillingSKU,
		Provider: imageProviderKind(snapshot.Provider), Model: snapshot.Model,
		Source: "task_snapshot:" + snapshot.Digest, SupportsReference: snapshot.GenerationFeatures.SupportsReference,
		MaxReferenceImages: snapshot.GenerationFeatures.MaxReferenceImages, SelectionReason: "task_capability_snapshot",
	}, nil
}

func (s *ImageCapabilityResolver) ValidateFrozenImageCapability(ctx context.Context, userID string, snapshot model.ImageCapabilitySnapshot) error {
	_, err := s.ResolveFrozenImageModelForGeneration(ctx, userID, snapshot, "content", 0)
	return err
}

func (s *ImageCapabilityResolver) resolveRoute(ctx context.Context, userID, capabilityKey string) (config.ImageGenerationRouteConfig, string, error) {
	if s == nil || s.cfg == nil {
		return config.ImageGenerationRouteConfig{}, "", ErrImageCapabilityResolverUnavailable
	}
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
