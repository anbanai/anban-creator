package resources

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
	CompositionGuidance  []string       `json:"composition_guidance,omitempty"`
}

// ValidCategories is the list of all resource categories.
var ValidCategories = []Category{CategoryTheme, CategoryWriter, CategoryLayout, CategoryArticleTemplate}

// IsPlatformRelevant checks if a resource category is relevant for a given platform.
func (c Category) IsPlatformRelevant(platform string) bool {
	switch c {
	case CategoryTheme, CategoryWriter:
		return platform == "article"
	case CategoryLayout:
		return platform == "article"
	case CategoryArticleTemplate:
		return platform == "article"
	}
	return false
}

type FieldSpec struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Enum        []string `yaml:"enum,omitempty" json:"enum,omitempty"`
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
