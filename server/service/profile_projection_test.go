package service

import (
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestProfileDimensionMarkdownIncludesStructuredContentAndSources(t *testing.T) {
	profile := model.NewProjectProfile()
	profile.Dimensions.Identity.Content = map[string]any{"summary": "科技写作"}
	profile.Dimensions.Identity.Sources = []string{"[用户确认]"}
	profile.Dimensions.Identity.MissingFields = []string{"代表作品"}

	content, err := ProfileDimensionMarkdown("identity", profile.Dimensions.Identity)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# identity", "科技写作", "[用户确认]", "代表作品"} {
		if !strings.Contains(content, want) {
			t.Fatalf("markdown %q does not contain %q", content, want)
		}
	}
}

func TestProfileAgentsMarkdownReferencesAllSixDimensions(t *testing.T) {
	content := ProfileAgentsMarkdown()
	for _, name := range model.ProfileDimensions() {
		if !strings.Contains(content, "profile/"+name+".md") {
			t.Fatalf("AGENTS.md does not reference %s", name)
		}
	}
	if !strings.Contains(content, "preferences.md") || !strings.Contains(content, "硬约束") {
		t.Fatalf("AGENTS.md is missing preference rule: %s", content)
	}
}
