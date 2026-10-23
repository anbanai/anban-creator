package service

import (
	"errors"

	"github.com/anbanai/anban-creator/server/resources"
)

var ErrResourceNotFound = errors.New("resource not found")

type ResourceCatalogRequest struct {
	Category      string
	Platform      string
	Name          string
	IncludeRaw    bool
	LifecycleMode string
}

type ResourceCatalogService struct {
	manager        *resources.ResourceManager
	configuredMode resources.LifecycleMode
}

func NewResourceCatalogService(manager *resources.ResourceManager, configuredMode ...resources.LifecycleMode) *ResourceCatalogService {
	mode := resources.LifecycleModeLegacy
	if len(configuredMode) > 0 && configuredMode[0] != "" {
		mode = configuredMode[0]
	}
	if mode != resources.LifecycleModeLegacy && mode != resources.LifecycleModeCompatibility && mode != resources.LifecycleModeNative {
		mode = resources.LifecycleModeLegacy
	}
	return &ResourceCatalogService{manager: manager, configuredMode: mode}
}

func (s *ResourceCatalogService) Query(req ResourceCatalogRequest) (any, error) {
	if s == nil || s.manager == nil {
		return nil, errors.New("resource catalog not available")
	}
	mode, err := resources.ResolveLifecycleMode(resources.LifecycleMode(req.LifecycleMode), s.configuredMode)
	if err != nil {
		return nil, err
	}
	category := resources.Category(req.Category)
	if req.Name == "" {
		items, err := s.manager.ListBySelectionMode(category, req.Platform, mode)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"category":       req.Category,
			"lifecycle_mode": string(mode),
			"items":          items,
		}, nil
	}

	entry := s.manager.Get(category, req.Name)
	if entry == nil || !resources.LifecycleAllowed(entry.Lifecycle, mode) {
		return nil, ErrResourceNotFound
	}
	result := map[string]any{
		"name": entry.Name, "category": entry.Category, "description": entry.Description,
		"lifecycle_mode": string(mode),
		"schema_version": entry.SchemaVersion, "lifecycle": string(entry.Lifecycle),
		"when_not_to_use": entry.WhenNotToUse, "pairs_well_with": entry.PairsWellWith,
		"canonical_example": entry.CanonicalExample, "provenance": entry.Provenance,
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
		result["when_not_to_use"], result["pairs_well_with"] = entry.WhenNotToUse, entry.PairsWellWith
		result["canonical_example"], result["provenance"] = entry.CanonicalExample, entry.Provenance
		if entry.ModuleContract != nil && resources.LifecycleAllowed(entry.ModuleContract.Lifecycle, mode) {
			result["module_contract"] = entry.ModuleContract
		}
	case resources.CategoryArticleTemplate:
		result["article_type"], result["article_types"] = entry.TemplateArticleType, entry.TemplateArticleTypes
		result["best_for"], result["rhythm"] = entry.TemplateBestFor, entry.TemplateRhythm
		result["image_count"], result["modules"] = entry.TemplateImageCount, entry.TemplateModules
		result["forbidden_modules"] = entry.TemplateForbidden
		result["composition_guidance"] = entry.CompositionGuidance
	}
	if req.IncludeRaw {
		if raw := s.manager.GetRaw(category, req.Name); len(raw) > 0 {
			result["raw"] = string(raw)
		}
	}
	return result, nil
}
