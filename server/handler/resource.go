package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/app/converter"
	"github.com/anbanai/anban-creator/server/resources"
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
		return Error(c, fiber.StatusBadRequest, "invalid category, must be one of: themes, writers, layouts, article_templates")
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
		return Error(c, fiber.StatusBadRequest, "invalid category, must be one of: themes, writers, layouts, article_templates")
	}

	entry := resources.Manager().Get(resources.Category(category), name)
	if entry == nil {
		return Error(c, fiber.StatusNotFound, "resource not found")
	}

	return Success(c, entry)
}

// sampleThemeMarkdown is a representative article snippet used to preview a
// theme's typography, colors, and block styles (headings, quote, list, emphasis).
// Rendered deterministically — no LLM — so previews are fast and stable.
const sampleThemeMarkdown = `# 排版预览示例

这是一段正文，用来展示主题的字号、行距与正文配色。好的排版让读者在**关键信息**与*细节描述*之间自然过渡，长时间阅读也不易疲劳。

> 这是一段引用：用来呈现引用块的左边框、背景色与字体的差异，常用于金句或重点提示。

## 一、小节标题

- 列表项：展示项目符号样式与行间距
- 另一项：包含**加粗**与*斜体*的混排效果

## 二、另一个小节

正文继续，验证段落间距、对齐方式与分隔线的表现。

---

收尾段落，确认主题整体的视觉收口。`

// PreviewTheme handles GET /api/v1/resources/themes/:name/preview.
//
// Renders the built-in sample markdown with the requested theme via the
// deterministic renderer and returns the WeChat-safe inline-CSS HTML — meant
// for display inside a sandboxed iframe in the template editor/preview. Public
// (no auth): it renders only static theme styling, no user data. Returns 404
// for an unknown theme.
func (h *ResourceHandler) PreviewTheme(c fiber.Ctx) error {
	name := c.Params("name")
	if name == "" {
		return Error(c, fiber.StatusBadRequest, "theme name is required")
	}
	if resources.Manager().Get(resources.CategoryTheme, name) == nil {
		return Error(c, fiber.StatusNotFound, "theme not found")
	}

	nopLog := zerolog.Nop()
	cvt := converter.NewConverterWithThemes(&nopLog, resources.Manager().GetAllRaw(resources.CategoryTheme))
	result := cvt.Convert(&converter.ConvertRequest{Markdown: sampleThemeMarkdown, Theme: name})
	if !result.Success {
		h.logger.Error().Str("theme", name).Str("err", result.Error).Msg("preview theme render failed")
		return Error(c, fiber.StatusInternalServerError, "failed to render theme preview")
	}

	// Wrap in a minimal HTML document so the srcDoc iframe always renders UTF-8.
	// (A srcDoc document otherwise relies on the parent page's charset, which is
	// fragile.) Preview-only — the production converter output stays bare
	// WeChat-safe <section> fragments with no <head>.
	previewDoc := "<!DOCTYPE html><html lang=\"zh\"><head><meta charset=\"utf-8\"></head><body style=\"margin:0;\">" + result.HTML + "</body></html>"
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(previewDoc)
}
