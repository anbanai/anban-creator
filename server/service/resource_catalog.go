package service

import (
	"errors"

	"github.com/anbanai/anban-creator/server/resources"
)

var ErrResourceNotFound = errors.New("resource not found")

type ResourceCatalogRequest struct {
	Category   string
	Platform   string
	Name       string
	IncludeRaw bool
}

type ResourceCatalogService struct {
	manager *resources.ResourceManager
}

func NewResourceCatalogService(manager *resources.ResourceManager) *ResourceCatalogService {
	return &ResourceCatalogService{manager: manager}
}

func (s *ResourceCatalogService) Query(req ResourceCatalogRequest) (any, error) {
	if s == nil || s.manager == nil {
		return nil, errors.New("resource catalog not available")
	}
	category := resources.Category(req.Category)
	if req.Name == "" {
		return map[string]any{
			"category": req.Category,
			"items":    s.manager.ListByPlatform(category, req.Platform),
		}, nil
	}

	entry := s.manager.Get(category, req.Name)
	if entry == nil {
		return nil, ErrResourceNotFound
	}
	result := map[string]any{
		"name": entry.Name, "category": entry.Category, "description": entry.Description,
	}
	switch category {
	case resources.CategoryTheme:
		result["mood"], result["best_for"] = entry.Mood, entry.BestFor
	case resources.CategoryWriter:
		result["display_name"], result["english_name"] = entry.DisplayName, entry.EnglishName
		result["category_cn"], result["aliases"] = entry.CategoryCn, entry.Aliases
		result["writer_best_for"], result["writing_tone"] = entry.WriterBestFor, entry.WritingTone
		result["writing_voice"], result["writing_perspective"] = entry.WritingVoice, entry.WritingPerspective
		result["title_formulas"] = entry.TitleFormulas
	case resources.CategoryLayout:
		result["layout_category"], result["serves"] = entry.LayoutCategory, entry.Serves
		result["when_to_use"], result["markdown_syntax"] = entry.WhenToUse, entry.MarkdownSyntax
		result["body_format"], result["fields"], result["rows"] = entry.BodyFormat, entry.Fields, entry.Rows
	case resources.CategoryArticleTemplate:
		result["article_type"], result["article_types"] = entry.TemplateArticleType, entry.TemplateArticleTypes
		result["best_for"], result["rhythm"] = entry.TemplateBestFor, entry.TemplateRhythm
		result["image_count"], result["modules"] = entry.TemplateImageCount, entry.TemplateModules
		result["composition_guidance"] = entry.CompositionGuidance
	}
	if req.IncludeRaw {
		if raw := s.manager.GetRaw(category, req.Name); len(raw) > 0 {
			result["raw"] = string(raw)
		}
	}
	return result, nil
}
