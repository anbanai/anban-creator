package handler

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

// ArticleAssetHandler serves only explicitly published, immutable article images.
type ArticleAssetHandler struct {
	repo   repository.Repository
	store  storage.Provider
	logger *zerolog.Logger
}

func NewArticleAssetHandler(repo repository.Repository, store storage.Provider, logger *zerolog.Logger) *ArticleAssetHandler {
	return &ArticleAssetHandler{repo: repo, store: store, logger: logger}
}

func (h *ArticleAssetHandler) Get(c fiber.Ctx) error {
	asset, err := h.repo.Assets().FindByID(c.Context(), c.Params("id"))
	if err != nil || asset.Purpose != model.AssetPurposeArticleImage {
		return Error(c, fiber.StatusNotFound, "article image not found")
	}
	if !strings.Contains("|image/png|image/jpeg|image/gif|image/webp|", "|"+strings.ToLower(asset.ContentType)+"|") {
		return Error(c, fiber.StatusNotFound, "article image not found")
	}
	var body io.ReadCloser
	if streamer, ok := h.store.(storage.ObjectStreamProvider); ok {
		body, err = streamer.OpenObject(c.Context(), asset.StorageKey)
	} else {
		var data []byte
		data, err = storage.ReadObject(c.Context(), h.store, asset.StorageKey, 20<<20)
		if err == nil {
			body = io.NopCloser(bytes.NewReader(data))
		}
	}
	if err != nil {
		h.logger.Error().Err(err).Str("asset_id", asset.ID).Msg("article image asset unavailable")
		return Error(c, fiber.StatusNotFound, "article image not found")
	}
	c.Set("Content-Type", asset.ContentType)
	c.Set("Cache-Control", "public, max-age=86400, immutable")
	c.Set("X-Content-Type-Options", "nosniff")
	return c.SendStream(body)
}

func (h *ArticleAssetHandler) Delete(c fiber.Ctx) error {
	userID, _ := c.Locals("user_id").(string)
	if userID == "" {
		return Error(c, fiber.StatusUnauthorized, "authentication required")
	}
	asset, err := h.repo.Assets().FindOwnedByID(c.Context(), c.Params("id"), userID)
	if err != nil || asset.Purpose != model.AssetPurposeArticleImage {
		return Error(c, fiber.StatusNotFound, "article image not found")
	}
	if err := h.repo.Assets().DeleteOwnedArticleImage(c.Context(), asset.ID, userID); err != nil {
		if errors.Is(err, model.ErrAssetNotFound) {
			return Error(c, fiber.StatusNotFound, "article image not found")
		}
		h.logger.Error().Err(err).Str("asset_id", asset.ID).Msg("delete article image asset record")
		return Error(c, fiber.StatusInternalServerError, "failed to delete article image")
	}
	if err := h.store.Delete(c.Context(), asset.StorageKey); err != nil {
		h.logger.Error().Err(err).Str("asset_id", asset.ID).Msg("delete article image object")
		return Error(c, fiber.StatusInternalServerError, "article image was removed from delivery but storage cleanup failed")
	}
	return c.SendStatus(fiber.StatusNoContent)
}
