package handler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"
)

type ImageCapabilityHandler struct {
	routes  config.ImageGenerationRoutesConfig
	repo    repository.Repository
	catalog *service.BillingCatalogService
	logger  *zerolog.Logger
}

func NewImageCapabilityHandler(routes config.ImageGenerationRoutesConfig, repo repository.Repository, catalog *service.BillingCatalogService, logger *zerolog.Logger) *ImageCapabilityHandler {
	return &ImageCapabilityHandler{routes: routes, repo: repo, catalog: catalog, logger: logger}
}

type ImageCapabilityOption struct {
	Key            string                              `json:"key"`
	DisplayName    string                              `json:"display_name"`
	Description    string                              `json:"description"`
	MinTier        string                              `json:"min_tier"`
	PriceCredits   int64                               `json:"price_credits"`
	PriceAvailable bool                                `json:"price_available"`
	Enabled        bool                                `json:"enabled"`
	SortOrder      int                                 `json:"sort_order"`
	Features       config.DesignerProviderCapabilities `json:"features"`
}

type ImageCapabilityRatioError struct {
	CapabilityKey string `json:"capability_key"`
	ImageRatio    string `json:"image_ratio"`
}

func (e *ImageCapabilityRatioError) Error() string {
	return fmt.Sprintf("image capability %q does not support image_ratio %q", e.CapabilityKey, e.ImageRatio)
}

func (h *ImageCapabilityHandler) List(c fiber.Ctx) error {
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
	keys := make([]string, 0, len(h.routes.Capabilities))
	for key := range h.routes.Capabilities {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := h.routes.Capabilities[keys[i]], h.routes.Capabilities[keys[j]]
		if left.SortOrder != right.SortOrder {
			return left.SortOrder < right.SortOrder
		}
		return keys[i] < keys[j]
	})
	items := make([]ImageCapabilityOption, 0, len(keys))
	for _, key := range keys {
		route := h.routes.Capabilities[key]
		requiredTier := model.NormalizeTier(route.MinTier)
		if !route.Enabled || !model.TierSatisfies(userTier, requiredTier) {
			continue
		}
		features := route.Features
		features.MaxBatch = 1
		item := ImageCapabilityOption{Key: key, DisplayName: route.Alias, Description: route.Description, MinTier: string(requiredTier), Enabled: route.Enabled, SortOrder: route.SortOrder, Features: features}
		if h.catalog != nil && strings.TrimSpace(route.BillingSKU) != "" {
			if price, err := h.catalog.ResolvePriceBySKUID(c.Context(), "", route.BillingSKU, userTier); err == nil && price != nil {
				item.PriceCredits = price.PriceCredits
				item.PriceAvailable = true
			}
		}
		items = append(items, item)
	}
	return Success(c, fiber.Map{"items": items, "tier": string(userTier), "default_capability": h.routes.DefaultCapability})
}

func ValidateImageCapabilityKey(key string, userTier model.Tier, capabilities map[string]config.ImageGenerationRouteConfig) error {
	key = strings.TrimSpace(key)
	if key == "" || key == model.ImageCapabilityKeySystemDefault {
		return nil
	}
	route, ok := capabilities[key]
	if !ok || !route.Enabled {
		return fmt.Errorf("unknown image capability key %q", key)
	}
	required := model.NormalizeTier(route.MinTier)
	if !model.TierSatisfies(userTier, required) {
		return fmt.Errorf("image capability %q requires %s tier", key, required)
	}
	return nil
}

func ValidateImageRatioForCapability(key, ratio string, routes config.ImageGenerationRoutesConfig) error {
	ratio = strings.TrimSpace(ratio)
	if ratio == "" {
		return nil
	}
	key = strings.TrimSpace(key)
	if key == "" {
		key = strings.TrimSpace(routes.DefaultCapability)
	}
	route, ok := routes.Capabilities[key]
	if !ok || !route.Enabled {
		return fmt.Errorf("unknown image capability key %q", key)
	}
	for _, supported := range route.Features.SizePresets {
		if strings.EqualFold(strings.TrimSpace(supported), ratio) {
			return nil
		}
	}
	return &ImageCapabilityRatioError{CapabilityKey: key, ImageRatio: ratio}
}

func respondImageCapabilityRatioError(c fiber.Ctx, err error) error {
	var ratioErr *ImageCapabilityRatioError
	if errors.As(err, &ratioErr) {
		return c.Status(fiber.StatusBadRequest).JSON(Response{
			Code: fiber.StatusBadRequest * 100,
			Msg:  "image_capability_ratio_unsupported",
			Data: ratioErr,
		})
	}
	return Error(c, fiber.StatusBadRequest, err.Error())
}

func validateImageCapabilityKeyForUser(ctx context.Context, repo repository.Repository, userID, key string, capabilities map[string]config.ImageGenerationRouteConfig) error {
	if strings.TrimSpace(key) == "" {
		return nil
	}
	tier := model.TierFree
	if repo != nil {
		if user, err := repo.Users().FindByID(ctx, userID); err == nil && user != nil {
			tier = model.ResolveTier(user.Tier)
		}
	}
	return ValidateImageCapabilityKey(key, tier, capabilities)
}
