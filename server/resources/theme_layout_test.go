package resources

import (
	"encoding/json"
	"strings"
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
		if len(tpl.TemplateBestFor) == 0 {
			t.Errorf("template %s should expose best_for metadata", tpl.Name)
		}
		if len(tpl.CompositionGuidance) == 0 {
			t.Errorf("template %s should expose composition guidance", tpl.Name)
		}
		if _, ok := required[tpl.Name]; ok {
			required[tpl.Name] = true
		}

		raw := Manager().GetRaw(CategoryArticleTemplate, tpl.Name)
		if len(raw) == 0 {
			t.Errorf("template %s should expose raw YAML", tpl.Name)
		}
		var spec struct {
			ExampleLayoutPlan string `yaml:"example_layout_plan"`
		}
		if err := yaml.Unmarshal(raw, &spec); err != nil {
			t.Fatalf("unmarshal article template %s raw: %v", tpl.Name, err)
		}
		if strings.TrimSpace(spec.ExampleLayoutPlan) == "" {
			t.Errorf("template %s should include example_layout_plan", tpl.Name)
		} else {
			var parsed map[string]any
			if err := json.Unmarshal([]byte(spec.ExampleLayoutPlan), &parsed); err != nil {
				t.Errorf("template %s example_layout_plan should be valid JSON: %v", tpl.Name, err)
			}
		}
	}
	for name, seen := range required {
		if !seen {
			t.Errorf("missing required article template %s", name)
		}
	}
}

func TestEmbeddedWritersExposeAgentSelectionMetadata(t *testing.T) {
	writers := Manager().List(CategoryWriter)
	if len(writers) == 0 {
		t.Fatal("expected embedded writers")
	}
	for _, writer := range writers {
		if writer.EnglishName == "" {
			t.Errorf("writer %s missing english_name", writer.Name)
		}
		if len(writer.Aliases) < 2 {
			t.Errorf("writer %s should expose aliases for lookup", writer.EnglishName)
		}
		if len(writer.WriterBestFor) == 0 {
			t.Errorf("writer %s should expose writer_best_for", writer.EnglishName)
		}
		if writer.WritingTone == "" || writer.WritingVoice == "" || writer.WritingPerspective == "" {
			t.Errorf("writer %s should expose tone/voice/perspective", writer.EnglishName)
		}
		if len(writer.TitleFormulas) == 0 {
			t.Errorf("writer %s should expose title formulas", writer.EnglishName)
		}
		for _, formula := range writer.TitleFormulas {
			if formula.Type == "" || formula.Template == "" || len(formula.Examples) == 0 {
				t.Errorf("writer %s has incomplete title formula: %#v", writer.EnglishName, formula)
			}
		}
		raw := string(Manager().GetRaw(CategoryWriter, writer.EnglishName))
		for _, forbidden := range []string{"cover_style", "cover_prompt", "visual_style", "image_style"} {
			if strings.Contains(raw, forbidden) {
				t.Errorf("writer %s must not carry visual field %q", writer.EnglishName, forbidden)
			}
		}
	}
}
