package agent

import (
	"path/filepath"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestValidateSeednoteArtifactsFromTaskFiles(t *testing.T) {
	task := &model.Task{Type: model.PlatformSeednote, HasContentImage: true}
	files := []*model.TaskFile{
		{FileName: "CLAUDE.md", FilePath: "CLAUDE.md"},
		{FileName: "cover.png", FilePath: "output/seednote/title/cover.png"},
		{FileName: "image_01.png", FilePath: "output/seednote/title/image_01.png"},
	}
	for _, path := range seednoteRequiredArtifactPaths("output/seednote/title") {
		files = append(files, &model.TaskFile{FileName: filepath.Base(path), FilePath: path})
	}

	got := ValidateTaskArtifactsFromTaskFiles(task, files)
	if !got.Valid {
		t.Fatalf("expected task_files artifacts to be valid: %#v", got)
	}
}

func seednoteRequiredArtifactPaths(prefix string) []string {
	names := []string{
		"content.md",
		"request-analysis.json",
		"request-analysis.md",
		"reference-analysis.json",
		"reference-analysis.md",
		"image-plan.md",
		"image-prompts.md",
		"image-review.md",
		"reference-usage-summary.json",
	}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(prefix, name))
	}
	return paths
}

func TestNestedAgentDelegationOnly(t *testing.T) {
	if !IsNestedAgentDelegationOnly(map[string]int{"Agent": 1}) {
		t.Fatal("Agent-only summary should be treated as nested delegation")
	}
	if !IsNestedAgentDelegationOnly(map[string]int{"Agent": 1, "TaskCreate": 1, "TaskUpdate": 1}) {
		t.Fatal("Agent with only tracking tools should be treated as nested delegation")
	}
	if IsNestedAgentDelegationOnly(map[string]int{"Agent": 1, "generate_image": 1}) {
		t.Fatal("Agent plus substantive tool should not be treated as Agent-only delegation")
	}
}
