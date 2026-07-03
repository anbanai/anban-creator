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
