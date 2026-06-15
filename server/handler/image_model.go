package handler

import (
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/config"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// ImageModelHandler exposes the list of image models the current user can select
// when creating tasks or plans. Selection is gated by user tier.
type ImageModelHandler struct {
	presets []config.ImageModelPreset
	repo    repository.Repository
	logger  *zerolog.Logger
}

// NewImageModelHandler creates a new ImageModelHandler.
// repo may be nil in degraded mode; in that case List returns only the system-default option.
func NewImageModelHandler(presets []config.ImageModelPreset, repo repository.Repository, logger *zerolog.Logger) *ImageModelHandler {
	return &ImageModelHandler{presets: presets, repo: repo, logger: logger}
}

// ImageModelOption is a selectable image model entry returned to the frontend.
type ImageModelOption struct {
	// Key is the value stored on Task/Plan.ImageModelKey. "" = system default,
	// "custom" = user override, any other = preset key.
	Key string `json:"key"`
	// DisplayName is the user-facing label.
	DisplayName string `json:"display_name"`
	// Provider is the underlying image provider (volcengine/gemini/openai).
	Provider string `json:"provider,omitempty"`
	// Model is the concrete model id, included so the frontend can show a hint.
	Model string `json:"model,omitempty"`
	// MinTier is the minimum tier required to use this option (preset entries only).
	MinTier string `json:"min_tier,omitempty"`
	// IsCustom marks the user-override ("custom") entry.
	IsCustom bool `json:"is_custom,omitempty"`
}

// List handles GET /api/v1/image-models.
//
// Returns the list of image models the current user is allowed to select, in display order:
//  1. Always: the "system default" entry (Key = "").
//  2. Each preset whose MinTier the user satisfies, in the order declared in config.
//  3. For Enterprise users only: a "custom" entry that uses the user's per-account model-config.
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

	options := make([]ImageModelOption, 0, len(h.presets)+2)

	// 1. System default is always available.
	options = append(options, ImageModelOption{
		Key:         model.ImageModelKeySystemDefault,
		DisplayName: "系统默认",
	})

	// 2. Presets the user's tier satisfies, in declared order.
	for i := range h.presets {
		p := &h.presets[i]
		required := model.NormalizeTier(p.MinTier)
		if !model.TierSatisfies(userTier, required) {
			continue
		}
		display := p.DisplayName
		if display == "" {
			display = fmt.Sprintf("%s / %s", p.Provider, p.Model)
		}
		options = append(options, ImageModelOption{
			Key:         p.Key,
			DisplayName: display,
			Provider:    p.Provider,
			Model:       p.Model,
			MinTier:     string(required),
		})
	}

	// 3. Enterprise users can opt into per-account custom config.
	if userTier == model.TierEnterprise {
		options = append(options, ImageModelOption{
			Key:         model.ImageModelKeyCustom,
			DisplayName: "自定义 (使用账号配置)",
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
