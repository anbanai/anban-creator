package handler

import (
	"context"
	"fmt"
	"sort"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

// ImageModelHandler exposes the list of image models the current user can select
// when creating tasks or plans. Selection is gated by user tier.
type ImageModelHandler struct {
	presets []config.ImageModelPreset
	repo    repository.Repository
	catalog *service.BillingCatalogService
	logger  *zerolog.Logger
}

// NewImageModelHandler creates a new ImageModelHandler.
// repo may be nil in degraded mode; in that case List fails closed to Free-tier
// capabilities and does not manufacture a platform-default option.
func NewImageModelHandler(presets []config.ImageModelPreset, repo repository.Repository, catalog *service.BillingCatalogService, logger *zerolog.Logger) *ImageModelHandler {
	return &ImageModelHandler{presets: presets, repo: repo, catalog: catalog, logger: logger}
}

// ImageModelOption is a selectable image model entry returned to the frontend.
type ImageModelOption struct {
	// Key is the value stored on Task/Plan.ImageModelKey. Empty and "system_default"
	// remain server-side fallback values; public entries use configurable
	// capability keys. "custom" is the enterprise-only user override.
	Key string `json:"key"`
	// DisplayName is the user-facing label.
	DisplayName string `json:"display_name"`
	Description string `json:"description,omitempty"`
	// MinTier is the minimum tier required to use this option (preset entries only).
	MinTier      string `json:"min_tier,omitempty"`
	SortOrder    int    `json:"sort_order,omitempty"`
	PriceCredits int64  `json:"price_credits,omitempty"`
	// IsCustom marks the user-override ("custom") entry.
	IsCustom bool `json:"is_custom,omitempty"`
}

// List handles GET /api/v1/image-models.
//
// Returns the list of neutral image capabilities the current user is allowed
// to select, in configured display order. The server-side empty-key fallback
// is intentionally not exposed as a selectable public entry.
func (h *ImageModelHandler) List(c fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "unauthorized")
	}

	userTier := model.TierFree
	if h.repo != nil {
		if user, err := h.repo.Users().FindByID(c.Context(), userID); err == nil && user != nil {
			userTier = model.ResolveTier(user.Tier)
		}
	}

	options := make([]ImageModelOption, 0, len(h.presets)+1)

	// Presets the user's tier satisfies. SortOrder is the public catalog order;
	// stable key order is the deterministic fallback for legacy entries.
	presets := append([]config.ImageModelPreset(nil), h.presets...)
	sort.SliceStable(presets, func(i, j int) bool {
		if presets[i].SortOrder != presets[j].SortOrder {
			if presets[i].SortOrder == 0 {
				return false
			}
			if presets[j].SortOrder == 0 {
				return true
			}
			return presets[i].SortOrder < presets[j].SortOrder
		}
		return presets[i].Key < presets[j].Key
	})
	for i := range h.presets {
		p := &presets[i]
		required := model.NormalizeTier(p.MinTier)
		if !model.TierSatisfies(userTier, required) {
			continue
		}
		option := ImageModelOption{
			Key:         p.Key,
			DisplayName: p.DisplayName,
			Description: p.Description,
			MinTier:     string(required),
			SortOrder:   p.SortOrder,
		}
		if h.catalog != nil && p.BillingSKU != "" {
			if price, err := h.catalog.ResolvePriceBySKUID(c.Context(), "", p.BillingSKU, userTier); err == nil && price != nil {
				option.PriceCredits = price.PriceCredits
			}
		}
		options = append(options, option)
	}

	// Enterprise users can opt into per-account custom config.
	if userTier == model.TierEnterprise {
		options = append(options, ImageModelOption{
			Key:         model.ImageModelKeyCustom,
			DisplayName: "自定义图像能力",
			IsCustom:    true,
		})
	}

	return Success(c, fiber.Map{
		"items": options,
		"tier":  string(userTier),
	})
}

// ValidateImageModelKey reports whether the given key is a valid selection for
// the given user tier. Used by task/plan handlers during create/update.
//
// Rules:
//   - "" / "system_default" → always valid.
//   - "custom" → valid only for Enterprise tier.
//   - any other value → valid only if it matches a preset key whose MinTier the user satisfies.
func ValidateImageModelKey(key string, userTier model.Tier, presets []config.ImageModelPreset) error {
	key = config.NormalizeImageModelKey(key)
	if key == "" || key == model.ImageModelKeySystemDefault {
		return nil
	}
	if key == model.ImageModelKeyCustom {
		if userTier != model.TierEnterprise {
			return fmt.Errorf("自定义图像模型仅对企业版可用")
		}
		return nil
	}
	for i := range presets {
		p := &presets[i]
		if p.Key != key {
			continue
		}
		required := model.NormalizeTier(p.MinTier)
		if !model.TierSatisfies(userTier, required) {
			return fmt.Errorf("当前等级 (%s) 无法使用模型 %q，需要 %s 或更高等级", userTier, p.DisplayName, required)
		}
		return nil
	}
	return fmt.Errorf("未知的图像模型 key: %q", key)
}

// validateImageModelKeyForUser resolves the caller's tier from the repository
// and validates image_model_key against it.
//
// Returns nil if the key is acceptable, an error otherwise. Fail-closed: if the
// user's tier cannot be determined (repo unavailable or lookup error), default
// to Free so a DB hiccup cannot accidentally widen access to Pro/Enterprise-only
// models.
func validateImageModelKeyForUser(ctx context.Context, repo repository.Repository, userID, key string, presets []config.ImageModelPreset) error {
	if key == "" {
		return nil
	}
	tier := model.TierFree
	if repo != nil {
		if user, err := repo.Users().FindByID(ctx, userID); err == nil && user != nil {
			tier = model.ResolveTier(user.Tier)
		}
	}
	return ValidateImageModelKey(key, tier, presets)
}
