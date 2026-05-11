package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/resources"
)

func isValidCategory(c string) bool {
	for _, v := range resources.ValidCategories {
		if string(v) == c {
			return true
		}
	}
	return false
}

// ResourceHandler handles resource catalog endpoints.
type ResourceHandler struct {
	logger *zerolog.Logger
}

// NewResourceHandler creates a new ResourceHandler.
func NewResourceHandler(logger *zerolog.Logger) *ResourceHandler {
	return &ResourceHandler{logger: logger}
}

// List handles GET /resources/:category.
func (h *ResourceHandler) List(c fiber.Ctx) error {
	category := c.Params("category")
	platform := c.Query("platform")

	if !isValidCategory(category) {
		return Error(c, fiber.StatusBadRequest, "invalid category, must be one of: themes, writers, layouts, image_presets")
	}

	items := resources.Manager().ListByPlatform(resources.Category(category), platform)

	return Success(c, fiber.Map{
		"category": category,
		"items":    items,
	})
}

// Get handles GET /resources/:category/:name.
func (h *ResourceHandler) Get(c fiber.Ctx) error {
	category := c.Params("category")
	name := c.Params("name")

	if !isValidCategory(category) {
		return Error(c, fiber.StatusBadRequest, "invalid category, must be one of: themes, writers, layouts, image_presets")
	}

	entry := resources.Manager().Get(resources.Category(category), name)
	if entry == nil {
		return Error(c, fiber.StatusNotFound, "resource not found")
	}

	return Success(c, entry)
}
