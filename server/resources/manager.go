package resources

import (
	"fmt"
	"io/fs"
	"maps"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ResourceManager provides read-only access to embedded YAML resources.
type ResourceManager struct {
	themes           map[string]*ResourceEntry
	writers          map[string]*ResourceEntry
	layouts          map[string]*ResourceEntry
	imagePresets     map[string]*ResourceEntry
	articleTemplates map[string]*ResourceEntry
	rawContent       map[Category]map[string][]byte
}

var globalManager *ResourceManager

func init() {
	m, err := NewResourceManager()
	if err != nil {
		panic("failed to load embedded resources: " + err.Error())
	}
	globalManager = m
}

// Manager returns the global singleton ResourceManager.
func Manager() *ResourceManager { return globalManager }

// NewResourceManager loads all embedded resources.
func NewResourceManager() (*ResourceManager, error) {
	rm := &ResourceManager{
		themes:           make(map[string]*ResourceEntry),
		writers:          make(map[string]*ResourceEntry),
		layouts:          make(map[string]*ResourceEntry),
		imagePresets:     make(map[string]*ResourceEntry),
		articleTemplates: make(map[string]*ResourceEntry),
		rawContent:       make(map[Category]map[string][]byte),
	}
	rm.rawContent[CategoryTheme] = make(map[string][]byte)
	rm.rawContent[CategoryWriter] = make(map[string][]byte)
	rm.rawContent[CategoryLayout] = make(map[string][]byte)
	rm.rawContent[CategoryImagePreset] = make(map[string][]byte)
	rm.rawContent[CategoryArticleTemplate] = make(map[string][]byte)

	if err := rm.loadThemes(); err != nil {
		return nil, fmt.Errorf("load themes: %w", err)
	}
	if err := rm.loadWriters(); err != nil {
		return nil, fmt.Errorf("load writers: %w", err)
	}
	if err := rm.loadLayouts(); err != nil {
		return nil, fmt.Errorf("load layouts: %w", err)
	}
	if err := rm.loadImagePresets(); err != nil {
		return nil, fmt.Errorf("load image presets: %w", err)
	}
	if err := rm.loadArticleTemplates(); err != nil {
		return nil, fmt.Errorf("load article templates: %w", err)
	}
	return rm, nil
}

// List returns all resources in a category. Returns empty slice for unknown categories.
func (rm *ResourceManager) List(category Category) []ResourceEntry {
	items := make([]ResourceEntry, 0)
	switch category {
	case CategoryTheme:
		for _, e := range rm.themes {
			items = append(items, *e)
		}
	case CategoryWriter:
		for _, e := range rm.writers {
			items = append(items, *e)
		}
	case CategoryLayout:
		for _, e := range rm.layouts {
			items = append(items, *e)
		}
	case CategoryImagePreset:
		for _, e := range rm.imagePresets {
			items = append(items, *e)
		}
	case CategoryArticleTemplate:
		for _, e := range rm.articleTemplates {
			items = append(items, *e)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

// Get returns a single resource by category and name. Returns nil for unknown resources.
func (rm *ResourceManager) Get(category Category, name string) *ResourceEntry {
	switch category {
	case CategoryTheme:
		if e, ok := rm.themes[name]; ok {
			cp := *e
			return &cp
		}
	case CategoryWriter:
		if e, ok := rm.writers[name]; ok {
			cp := *e
			return &cp
		}
	case CategoryLayout:
		if e, ok := rm.layouts[name]; ok {
			cp := *e
			return &cp
		}
	case CategoryImagePreset:
		if e, ok := rm.imagePresets[name]; ok {
			cp := *e
			return &cp
		}
	case CategoryArticleTemplate:
		if e, ok := rm.articleTemplates[name]; ok {
			cp := *e
			return &cp
		}
	}
	return nil
}

// GetRaw returns the raw YAML bytes for a resource. Used when full prompt content is needed.
func (rm *ResourceManager) GetRaw(category Category, name string) []byte {
	if catMap, ok := rm.rawContent[category]; ok {
		return catMap[name]
	}
	return nil
}

// GetAllRaw returns all raw YAML bytes for a category.
func (rm *ResourceManager) GetAllRaw(category Category) map[string][]byte {
	if catMap, ok := rm.rawContent[category]; ok {
		result := make(map[string][]byte, len(catMap))
		maps.Copy(result, catMap)
		return result
	}
	return make(map[string][]byte)
}

// ListByPlatform returns resources filtered by platform relevance.
func (rm *ResourceManager) ListByPlatform(category Category, platform string) []ResourceEntry {
	if platform == "" || category.IsPlatformRelevant(platform) {
		return rm.List(category)
	}
	return make([]ResourceEntry, 0)
}

func (rm *ResourceManager) loadThemes() error {
	entries, err := fs.ReadDir(ThemesFS, "themes")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := fs.ReadFile(ThemesFS, "themes/"+e.Name())
		if err != nil {
			return err
		}
		var raw struct {
			Name        string `yaml:"name"`
			Description string `yaml:"description"`
			StyleInfo   struct {
				Mood    string `yaml:"mood"`
				Colors  string `yaml:"colors"`
				BestFor string `yaml:"best_for"`
			} `yaml:"style_info"`
			Colors map[string]string `yaml:"colors"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		if raw.Name == "" {
			continue
		}
		entry := &ResourceEntry{
			Name:        raw.Name,
			Category:    CategoryTheme,
			Description: raw.Description,
			Mood:        raw.StyleInfo.Mood,
			BestFor:     raw.StyleInfo.BestFor,
			Colors:      raw.Colors,
		}
		rm.themes[raw.Name] = entry
		rm.rawContent[CategoryTheme][raw.Name] = data
	}
	return nil
}

func (rm *ResourceManager) loadWriters() error {
	entries, err := fs.ReadDir(WritersFS, "writers")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := fs.ReadFile(WritersFS, "writers/"+e.Name())
		if err != nil {
			return err
		}
		var raw struct {
			Name        string `yaml:"name"`
			EnglishName string `yaml:"english_name"`
			Category    string `yaml:"category"`
			Description string `yaml:"description"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		if raw.EnglishName == "" {
			continue
		}
		entry := &ResourceEntry{
			Name:        raw.Name,
			Category:    CategoryWriter,
			Description: raw.Description,
			DisplayName: raw.Name,
			EnglishName: raw.EnglishName,
			CategoryCn:  raw.Category,
		}
		rm.writers[raw.EnglishName] = entry
		rm.rawContent[CategoryWriter][raw.EnglishName] = data
	}
	return nil
}

func (rm *ResourceManager) loadLayouts() error {
	entries, err := fs.ReadDir(LayoutsFS, "layouts")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := fs.ReadFile(LayoutsFS, "layouts/"+e.Name())
		if err != nil {
			return err
		}
		var raw struct {
			Name           string      `yaml:"name"`
			Category       string      `yaml:"category"`
			Serves         []string    `yaml:"serves"`
			Description    string      `yaml:"description"`
			WhenToUse      string      `yaml:"when_to_use"`
			MarkdownSyntax string      `yaml:"markdown_syntax"`
			BodyFormat     string      `yaml:"body_format"`
			Fields         *FieldsSpec `yaml:"fields"`
			Rows           *RowsSpec   `yaml:"rows"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		if raw.Name == "" {
			continue
		}
		entry := &ResourceEntry{
			Name:           raw.Name,
			Category:       CategoryLayout,
			Description:    raw.Description,
			LayoutCategory: raw.Category,
			Serves:         raw.Serves,
			WhenToUse:      raw.WhenToUse,
			MarkdownSyntax: raw.MarkdownSyntax,
			BodyFormat:     raw.BodyFormat,
			Fields:         raw.Fields,
			Rows:           raw.Rows,
		}
		rm.layouts[raw.Name] = entry
		rm.rawContent[CategoryLayout][raw.Name] = data
	}
	return nil
}

func (rm *ResourceManager) loadArticleTemplates() error {
	entries, err := fs.ReadDir(ArticleTemplatesFS, "article_templates")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := fs.ReadFile(ArticleTemplatesFS, "article_templates/"+e.Name())
		if err != nil {
			return err
		}
		var raw struct {
			Name         string         `yaml:"name"`
			Description  string         `yaml:"description"`
			ArticleTypes []string       `yaml:"article_types"`
			BestFor      []string       `yaml:"best_for"`
			Rhythm       map[string]any `yaml:"rhythm"`
			ImageCount   map[string]int `yaml:"image_count"`
			Modules      struct {
				Preferred []string `yaml:"preferred"`
			} `yaml:"modules"`
			CompositionGuidance []string `yaml:"composition_guidance"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		if raw.Name == "" {
			continue
		}
		entry := &ResourceEntry{
			Name:                 raw.Name,
			Category:             CategoryArticleTemplate,
			Description:          raw.Description,
			TemplateArticleType:  raw.Name,
			TemplateArticleTypes: raw.ArticleTypes,
			TemplateBestFor:      raw.BestFor,
			TemplateRhythm:       raw.Rhythm,
			TemplateImageCount:   raw.ImageCount,
			TemplateModules:      raw.Modules.Preferred,
			CompositionGuidance:  raw.CompositionGuidance,
		}
		rm.articleTemplates[raw.Name] = entry
		rm.rawContent[CategoryArticleTemplate][raw.Name] = data
	}
	return nil
}

func (rm *ResourceManager) loadImagePresets() error {
	entries, err := fs.ReadDir(ImagePresetsFS, "image_presets")
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") && !strings.HasSuffix(e.Name(), ".yml") {
			continue
		}
		data, err := fs.ReadFile(ImagePresetsFS, "image_presets/"+e.Name())
		if err != nil {
			return err
		}
		var raw struct {
			Name                    string   `yaml:"name"`
			Kind                    string   `yaml:"kind"`
			Description             string   `yaml:"description"`
			Archetype               string   `yaml:"archetype"`
			RecommendedAspectRatios []string `yaml:"recommended_aspect_ratios"`
			DefaultAspectRatio      string   `yaml:"default_aspect_ratio"`
			Tags                    []string `yaml:"tags"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}
		if raw.Name == "" {
			continue
		}
		entry := &ResourceEntry{
			Name:         raw.Name,
			Category:     CategoryImagePreset,
			Description:  raw.Description,
			Kind:         raw.Kind,
			Archetype:    raw.Archetype,
			AspectRatios: raw.RecommendedAspectRatios,
			DefaultRatio: raw.DefaultAspectRatio,
			Tags:         raw.Tags,
		}
		rm.imagePresets[raw.Name] = entry
		rm.rawContent[CategoryImagePreset][raw.Name] = data
	}
	return nil
}
