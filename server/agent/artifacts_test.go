package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestValidateSeednoteArtifactsFromWorkDir(t *testing.T) {
	tests := []struct {
		name  string
		task  model.Task
		files []string
		valid bool
	}{
		{
			name:  "only runtime file fails",
			task:  model.Task{Type: model.PlatformSeednote, HasContentImage: true},
			files: []string{"CLAUDE.md"},
			valid: false,
		},
		{
			name:  "missing content fails",
			task:  model.Task{Type: model.PlatformSeednote, HasContentImage: true},
			files: []string{"output/image-plan.md", "output/cover.png", "output/image_01.png"},
			valid: false,
		},
		{
			name:  "missing image plan fails",
			task:  model.Task{Type: model.PlatformSeednote, HasContentImage: true},
			files: []string{"output/content.md", "output/cover.png", "output/image_01.png"},
			valid: false,
		},
		{
			name:  "missing cover fails",
			task:  model.Task{Type: model.PlatformSeednote, HasContentImage: true},
			files: []string{"output/content.md", "output/image-plan.md", "output/image_01.png"},
			valid: false,
		},
		{
			name:  "cover content mode succeeds with required files",
			task:  model.Task{Type: model.PlatformSeednote, HasContentImage: true},
			files: append(seednoteRequiredArtifactPaths("output"), "output/cover.png", "output/image_01.png"),
			valid: true,
		},
		{
			name:  "extra tail does not fail cover content mode",
			task:  model.Task{Type: model.PlatformSeednote, HasContentImage: true},
			files: append(seednoteRequiredArtifactPaths("output"), "output/cover.png", "output/image_01.png", "output/tail.png"),
			valid: true,
		},
		{
			name:  "tail mode requires tail",
			task:  model.Task{Type: model.PlatformSeednote, HasTailImage: true},
			files: append(seednoteRequiredArtifactPaths("output"), "output/cover.png"),
			valid: false,
		},
		{
			name:  "cover only mode does not require content image",
			task:  model.Task{Type: model.PlatformSeednote},
			files: append(seednoteRequiredArtifactPaths("output"), "output/cover.png"),
			valid: true,
		},
		{
			name:  "structured recoverable failure is not success",
			task:  model.Task{Type: model.PlatformSeednote},
			files: []string{"output/failure-state.json"},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			got := ValidateTaskArtifactsFromWorkDir(&tt.task, dir)
			if got.Valid != tt.valid {
				t.Fatalf("valid = %v, want %v; result=%#v", got.Valid, tt.valid, got)
			}
		})
	}
}

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

func TestDockerRuntimeHomeIsNotMeaningfulOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DockerRuntimeHomeDirName, ".claude", "plugins", "installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := ValidateTaskArtifactsFromWorkDir(&model.Task{Type: model.PlatformArticle}, dir)
	if got.Valid || got.MeaningfulFileCount != 0 {
		t.Fatalf("runtime home validation = %#v, want no meaningful output", got)
	}
}

func TestValidateMomentsArtifactsFromWorkDir(t *testing.T) {
	tests := []struct {
		name  string
		files []string
		valid bool
	}{
		{
			name:  "missing quality review fails",
			files: []string{"output/material-analysis.md", "output/content.md"},
			valid: false,
		},
		{
			name:  "fixed markdown package succeeds",
			files: []string{"output/material-analysis.md", "output/content.md", "output/quality-review.md"},
			valid: true,
		},
		{
			name:  "optional extra file does not change required package",
			files: []string{"output/material-analysis.md", "output/content.md", "output/quality-review.md", "output/extra.png"},
			valid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range tt.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			got := ValidateTaskArtifactsFromWorkDir(&model.Task{Type: model.PlatformMoments}, dir)
			if got.Valid != tt.valid {
				t.Fatalf("valid = %v, want %v; result=%#v", got.Valid, tt.valid, got)
			}
		})
	}
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
