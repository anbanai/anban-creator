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
			files: []string{"output/content.md", "output/image-plan.md", "output/cover.png", "output/image_01.png"},
			valid: true,
		},
		{
			name:  "extra tail does not fail cover content mode",
			task:  model.Task{Type: model.PlatformSeednote, HasContentImage: true},
			files: []string{"output/content.md", "output/image-plan.md", "output/cover.png", "output/image_01.png", "output/tail.png"},
			valid: true,
		},
		{
			name:  "tail mode requires tail",
			task:  model.Task{Type: model.PlatformSeednote, HasTailImage: true},
			files: []string{"output/content.md", "output/image-plan.md", "output/cover.png"},
			valid: false,
		},
		{
			name:  "cover only mode does not require content image",
			task:  model.Task{Type: model.PlatformSeednote},
			files: []string{"output/content.md", "output/image-plan.md", "output/cover.png"},
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
		{FileName: "content.md", FilePath: "output/seednote/title/content.md"},
		{FileName: "image-plan.md", FilePath: "output/seednote/title/image-plan.md"},
		{FileName: "cover.png", FilePath: "output/seednote/title/cover.png"},
		{FileName: "image_01.png", FilePath: "output/seednote/title/image_01.png"},
	}

	got := ValidateTaskArtifactsFromTaskFiles(task, files)
	if !got.Valid {
		t.Fatalf("expected task_files artifacts to be valid: %#v", got)
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
