package resources

import (
	"fmt"
	"strings"
)

// Category identifies the type of resource.
type Category string

const (
	CategoryTheme           Category = "themes"
	CategoryWriter          Category = "writers"
	CategoryLayout          Category = "layouts"
	CategoryArticleTemplate Category = "article_templates"
)

// ResourceEntry is a resource's metadata summary for listing and display.
type ResourceEntry struct {
	Name        string   `json:"name"`
	Category    Category `json:"category"`
	Description string   `json:"description,omitempty"`

	SchemaVersion           string             `json:"schema_version,omitempty"`
	Lifecycle               ResourceLifecycle  `json:"lifecycle"`
	ModuleContractVersion   string             `json:"module_contract_version,omitempty"`
	ModuleContractLifecycle ResourceLifecycle  `json:"module_contract_lifecycle,omitempty"`
	WhenNotToUse            string             `json:"-"`
	PairsWellWith           []string           `json:"-"`
	CanonicalExample        map[string]string  `json:"-"`
	Provenance              ResourceProvenance `json:"-"`
	ModuleContract          *ModuleContract    `json:"-"`

	// Theme fields
	Mood    string            `json:"mood,omitempty"`
	BestFor string            `json:"best_for,omitempty"`
	Colors  map[string]string `json:"colors,omitempty"`

	// Writer fields
	DisplayName        string             `json:"display_name,omitempty"`
	EnglishName        string             `json:"english_name,omitempty"`
	CategoryCn         string             `json:"category_cn,omitempty"`
	Aliases            []string           `json:"aliases,omitempty"`
	WriterBestFor      []string           `json:"writer_best_for,omitempty"`
	WritingTone        string             `json:"writing_tone,omitempty"`
	WritingVoice       string             `json:"writing_voice,omitempty"`
	WritingPerspective string             `json:"writing_perspective,omitempty"`
	TitleFormulas      []TitleFormulaSpec `json:"title_formulas,omitempty"`

	// Layout fields
	LayoutCategory string      `json:"layout_category,omitempty"`
	Serves         []string    `json:"serves,omitempty"`
	WhenToUse      string      `json:"when_to_use,omitempty"`
	MarkdownSyntax string      `json:"markdown_syntax,omitempty"`
	BodyFormat     string      `json:"body_format,omitempty"`
	Fields         *FieldsSpec `json:"fields,omitempty"`
	Rows           *RowsSpec   `json:"rows,omitempty"`

	// Article template fields
	TemplateArticleType  string         `json:"template_article_type,omitempty"`
	TemplateArticleTypes []string       `json:"template_article_types,omitempty"`
	TemplateBestFor      []string       `json:"template_best_for,omitempty"`
	TemplateRhythm       map[string]any `json:"template_rhythm,omitempty"`
	TemplateImageCount   map[string]int `json:"template_image_count,omitempty"`
	TemplateModules      []string       `json:"template_modules,omitempty"`
	TemplateForbidden    []string       `json:"-"`
	CompositionGuidance  []string       `json:"composition_guidance,omitempty"`
}

type ResourceLifecycle string

const (
	LifecycleLegacy        ResourceLifecycle = "legacy"
	LifecycleCompatibility ResourceLifecycle = "compatibility"
	LifecycleRecommended   ResourceLifecycle = "recommended"
)

type ResourceProvenance struct {
	Source     string `yaml:"source" json:"source,omitempty"`
	Version    string `yaml:"version" json:"version,omitempty"`
	Reference  string `yaml:"reference" json:"reference,omitempty"`
	ImportedAt string `yaml:"imported_at" json:"imported_at,omitempty"`
}

type ModuleContract struct {
	Lifecycle      ResourceLifecycle            `yaml:"lifecycle" json:"lifecycle"`
	Version        string                       `yaml:"version" json:"version"`
	Required       []FieldSpec                  `yaml:"required,omitempty" json:"required,omitempty"`
	Optional       []FieldSpec                  `yaml:"optional,omitempty" json:"optional,omitempty"`
	VariantField   string                       `yaml:"variant_field,omitempty" json:"variant_field,omitempty"`
	DefaultVariant string                       `yaml:"default_variant,omitempty" json:"default_variant,omitempty"`
	Variants       map[string]ModuleVariantSpec `yaml:"variants,omitempty" json:"variants,omitempty"`
}

type ModuleVariantSpec struct {
	Required    []string   `yaml:"required,omitempty" json:"required,omitempty"`
	RequiredAny [][]string `yaml:"required_any,omitempty" json:"required_any,omitempty"`
	UseWhen     string     `yaml:"use_when,omitempty" json:"use_when,omitempty"`
}

func normalizeLifecycle(value string) (ResourceLifecycle, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return LifecycleLegacy, nil
	}
	lifecycle := ResourceLifecycle(value)
	switch lifecycle {
	case LifecycleLegacy, LifecycleCompatibility, LifecycleRecommended:
		return lifecycle, nil
	default:
		return "", fmt.Errorf("unsupported resource lifecycle %q", value)
	}
}

func ResolveLifecycleMode(requested, configured LifecycleMode) (LifecycleMode, error) {
	if configured == "" {
		configured = LifecycleModeLegacy
	}
	if !validLifecycleMode(configured) {
		return "", fmt.Errorf("unsupported configured lifecycle mode %q", configured)
	}
	if requested == "" {
		return configured, nil
	}
	if !validLifecycleMode(requested) {
		return "", fmt.Errorf("unsupported lifecycle mode %q", requested)
	}
	if lifecycleModeRank(requested) > lifecycleModeRank(configured) {
		return configured, nil
	}
	return requested, nil
}

func LifecycleAllowed(lifecycle ResourceLifecycle, mode LifecycleMode) bool {
	if lifecycle == "" {
		lifecycle = LifecycleLegacy
	}
	return lifecycleRank(lifecycle) <= lifecycleModeRank(mode)
}

func validLifecycleMode(mode LifecycleMode) bool {
	switch mode {
	case LifecycleModeLegacy, LifecycleModeCompatibility, LifecycleModeNative:
		return true
	default:
		return false
	}
}

func lifecycleModeRank(mode LifecycleMode) int {
	switch mode {
	case LifecycleModeCompatibility:
		return 1
	case LifecycleModeNative:
		return 2
	default:
		return 0
	}
}

func lifecycleRank(lifecycle ResourceLifecycle) int {
	switch lifecycle {
	case LifecycleCompatibility:
		return 1
	case LifecycleRecommended:
		return 2
	default:
		return 0
	}
}

// ValidCategories is the list of all resource categories.
var ValidCategories = []Category{CategoryTheme, CategoryWriter, CategoryLayout, CategoryArticleTemplate}

// IsPlatformRelevant checks if a resource category is relevant for a given platform.
func (c Category) IsPlatformRelevant(platform string) bool {
	switch c {
	case CategoryTheme, CategoryWriter:
		return platform == "wechat"
	case CategoryLayout:
		return platform == "wechat"
	case CategoryArticleTemplate:
		return platform == "wechat"
	}
	return false
}

type FieldSpec struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`
	Default     string   `yaml:"default,omitempty" json:"default,omitempty"`
	Example     string   `yaml:"example,omitempty" json:"example,omitempty"`
}

type FieldsSpec struct {
	Required []FieldSpec `yaml:"required,omitempty" json:"required,omitempty"`
	Optional []FieldSpec `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type RowsSpec struct {
	Delimiter   string      `yaml:"delimiter,omitempty" json:"delimiter,omitempty"`
	MinColumns  int         `yaml:"min_columns,omitempty" json:"min_columns,omitempty"`
	Schema      []FieldSpec `yaml:"schema,omitempty" json:"schema,omitempty"`
	Description string      `yaml:"description,omitempty" json:"description,omitempty"`
}

type TitleFormulaSpec struct {
	Type     string   `yaml:"type" json:"type"`
	Template string   `yaml:"template" json:"template"`
	Examples []string `yaml:"examples,omitempty" json:"examples,omitempty"`
}
