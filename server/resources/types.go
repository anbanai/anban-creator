package resources

// Category identifies the type of resource.
type Category string

const (
	CategoryTheme       Category = "themes"
	CategoryWriter      Category = "writers"
	CategoryLayout      Category = "layouts"
	CategoryImagePreset Category = "image_presets"
)

// ResourceEntry is a resource's metadata summary for listing and display.
type ResourceEntry struct {
	Name        string   `json:"name"`
	Category    Category `json:"category"`
	Description string   `json:"description,omitempty"`

	// Theme fields
	Mood    string `json:"mood,omitempty"`
	BestFor string `json:"best_for,omitempty"`

	// Writer fields
	DisplayName string `json:"display_name,omitempty"`
	EnglishName string `json:"english_name,omitempty"`
	CategoryCn  string `json:"category_cn,omitempty"`

	// Layout fields
	LayoutCategory string   `json:"layout_category,omitempty"`
	Serves         []string `json:"serves,omitempty"`
	WhenToUse      string   `json:"when_to_use,omitempty"`
	MarkdownSyntax string   `json:"markdown_syntax,omitempty"`

	// Image preset fields
	Kind         string   `json:"kind,omitempty"`
	Archetype    string   `json:"archetype,omitempty"`
	AspectRatios []string `json:"aspect_ratios,omitempty"`
	DefaultRatio string   `json:"default_ratio,omitempty"`

	Tags []string `json:"tags,omitempty"`
}

// ValidCategories is the list of all resource categories.
var ValidCategories = []Category{CategoryTheme, CategoryWriter, CategoryLayout, CategoryImagePreset}

// IsPlatformRelevant checks if a resource category is relevant for a given platform.
func (c Category) IsPlatformRelevant(platform string) bool {
	switch c {
	case CategoryTheme, CategoryWriter:
		return platform == "article"
	case CategoryLayout:
		return platform == "article"
	case CategoryImagePreset:
		return platform == "article" || platform == "seednote"
	}
	return false
}
