package resources

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEmbeddedThemesExposeColorsAndLayoutMetadata(t *testing.T) {
	type themeSpec struct {
		Colors map[string]string `yaml:"colors"`
		Layout struct {
			ContainerPadding string `yaml:"container_padding"`
			MaxWidth         string `yaml:"max_width"`
			CardPadding      string `yaml:"card_padding"`
		} `yaml:"layout"`
	}

	rawThemes := Manager().GetAllRaw(CategoryTheme)
	if len(rawThemes) == 0 {
		t.Fatal("expected embedded themes")
	}

	for name, data := range rawThemes {
		var theme themeSpec
		if err := yaml.Unmarshal(data, &theme); err != nil {
			t.Fatalf("unmarshal theme %s: %v", name, err)
		}

		entry := Manager().Get(CategoryTheme, name)
		if entry == nil {
			t.Fatalf("theme %s missing from resource manager", name)
		}
		if len(theme.Colors) == 0 {
			t.Errorf("theme %s has no YAML colors", name)
		}
		if len(entry.Colors) == 0 {
			t.Errorf("theme %s resource entry did not expose colors", name)
		}
		for key, want := range theme.Colors {
			if got := entry.Colors[key]; got != want {
				t.Errorf("theme %s color %s = %q, want %q", name, key, got, want)
			}
		}

		if theme.Layout.ContainerPadding == "" {
			t.Errorf("theme %s must declare container_padding so WeChat whitespace is theme-owned", name)
		}
		if theme.Layout.MaxWidth == "" {
			t.Errorf("theme %s must declare max_width so WeChat width is theme-owned", name)
		}
		if theme.Layout.CardPadding == "" {
			t.Errorf("theme %s must declare card_padding so WeChat whitespace is theme-owned", name)
		}
	}
}

func TestEmbeddedArticleTemplatesAreDiscoverableAndRawReadable(t *testing.T) {
	templates := Manager().List(CategoryArticleTemplate)
	if len(templates) == 0 {
		t.Fatal("expected embedded article templates")
	}

	required := map[string]bool{
		"long-form-essay": false,
		"listicle":        false,
		"tutorial":        false,
		"story-narrative": false,
	}
	for _, tpl := range templates {
		if tpl.Category != CategoryArticleTemplate {
			t.Errorf("template %s category = %q, want %q", tpl.Name, tpl.Category, CategoryArticleTemplate)
		}
		if tpl.Description == "" {
			t.Errorf("template %s should expose description", tpl.Name)
		}
		if tpl.TemplateArticleType == "" {
			t.Errorf("template %s should expose article type", tpl.Name)
		}
		if len(tpl.TemplateModules) == 0 {
			t.Errorf("template %s should expose preferred modules", tpl.Name)
		}
		if len(tpl.TemplateRhythm) == 0 {
			t.Errorf("template %s should expose rhythm metadata", tpl.Name)
		}
		if _, ok := required[tpl.Name]; ok {
			required[tpl.Name] = true
		}

		raw := Manager().GetRaw(CategoryArticleTemplate, tpl.Name)
		if len(raw) == 0 {
			t.Errorf("template %s should expose raw YAML", tpl.Name)
		}
	}
	for name, seen := range required {
		if !seen {
			t.Errorf("missing required article template %s", name)
		}
	}
}
