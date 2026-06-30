package resources

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestEmbeddedThemesUseCompactWeChatWidth(t *testing.T) {
	type themeLayout struct {
		Layout struct {
			ContainerPadding string `yaml:"container_padding"`
			CardPadding      string `yaml:"card_padding"`
		} `yaml:"layout"`
	}

	rawThemes := Manager().GetAllRaw(CategoryTheme)
	if len(rawThemes) == 0 {
		t.Fatal("expected embedded themes")
	}

	for name, data := range rawThemes {
		var theme themeLayout
		if err := yaml.Unmarshal(data, &theme); err != nil {
			t.Fatalf("unmarshal theme %s: %v", name, err)
		}
		if !hasZeroHorizontalPadding(theme.Layout.ContainerPadding) {
			t.Errorf("theme %s container_padding = %q, want zero horizontal padding", name, theme.Layout.ContainerPadding)
		}
		if !hasCompactCardHorizontalPadding(theme.Layout.CardPadding) {
			t.Errorf("theme %s card_padding = %q, want 16px horizontal padding", name, theme.Layout.CardPadding)
		}
	}
}

func hasZeroHorizontalPadding(value string) bool {
	parts := strings.Fields(value)
	switch len(parts) {
	case 2:
		return parts[1] == "0"
	case 4:
		return parts[1] == "0" && parts[3] == "0"
	default:
		return value == "0"
	}
}

func hasCompactCardHorizontalPadding(value string) bool {
	parts := strings.Fields(value)
	switch len(parts) {
	case 2:
		return parts[1] == "16px"
	case 4:
		return parts[1] == "16px" && parts[3] == "16px"
	default:
		return false
	}
}
