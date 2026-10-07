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

func TestValidateWechatPictureArtifactsRequiresConcreteContentImage(t *testing.T) {
	task := &model.Task{Type: model.TaskTypeWechatPicture}
	required := []string{"topic-analysis.md", "content-dna.json", "content-script.md", "content.md", "image-plan.md", "image-prompts.md", "publish-package.json", "quality-review.md", "cover.png"}
	files := make([]*model.TaskFile, 0, len(required)+1)
	for _, name := range required {
		files = append(files, &model.TaskFile{FileName: name, FilePath: filepath.Join("output", name)})
	}
	files = append(files, &model.TaskFile{FileName: "image_*.png", FilePath: "output/image_*.png"})
	got := ValidateTaskArtifactsFromTaskFiles(task, files)
	if got.Valid || !containsArtifactName(got.Missing, "concrete image_NN.png") {
		t.Fatalf("literal wildcard placeholder accepted: %#v", got)
	}
	files = append(files, &model.TaskFile{FileName: "image_01.png", FilePath: "output/image_01.png"})
	if got = ValidateTaskArtifactsFromTaskFiles(task, files); !got.Valid {
		t.Fatalf("concrete picture image rejected: %#v", got)
	}
}

func TestValidateViralAnalysisArtifactsFromTaskFiles(t *testing.T) {
	required := []string{"source-analysis.md", "viral-template.json"}
	tests := []struct {
		name         string
		missing      string
		failureState bool
		valid        bool
	}{
		{name: "complete", valid: true},
		{name: "failure state blocks complete deliverables", failureState: true},
		{name: "missing source analysis", missing: "source-analysis.md"},
		{name: "missing viral template", missing: "viral-template.json"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var files []*model.TaskFile
			for _, name := range required {
				if name != tt.missing {
					files = append(files, &model.TaskFile{FileName: name, FilePath: filepath.Join("output", name)})
				}
			}
			if tt.failureState {
				files = append(files, &model.TaskFile{FileName: "failure-state.json", FilePath: "output/failure-state.json"})
			}
			got := ValidateTaskArtifactsFromTaskFiles(&model.Task{Type: model.TaskTypeViralAnalysis}, files)
			if got.Valid != tt.valid {
				t.Fatalf("validation = %#v, want valid=%v", got, tt.valid)
			}
			if tt.missing != "" && !containsArtifactName(got.Missing, tt.missing) {
				t.Fatalf("missing = %#v, want %q", got.Missing, tt.missing)
			}
			if tt.failureState && !containsArtifactName(got.Missing, "successful seednote completion") {
				t.Fatalf("missing = %#v, want failure-state rejection", got.Missing)
			}
		})
	}
}

func TestValidateMomentsArtifactsRequiresGeneratedImage(t *testing.T) {
	required := []string{
		"material-analysis.md",
		"content.md",
		"image-prompts.md",
		"moments-image.png",
		"quality-review.md",
	}
	for _, missing := range append([]string{""}, required...) {
		name := "complete"
		if missing != "" {
			name = "missing " + missing
		}
		t.Run(name, func(t *testing.T) {
			var files []*model.TaskFile
			for _, artifact := range required {
				if artifact != missing {
					files = append(files, &model.TaskFile{FileName: artifact, FilePath: filepath.Join("output", artifact)})
				}
			}
			got := ValidateTaskArtifactsFromTaskFiles(&model.Task{Type: model.PlatformMoments}, files)
			if got.Valid != (missing == "") {
				t.Fatalf("validation = %#v, missing=%q", got, missing)
			}
			if missing != "" && !containsArtifactName(got.Missing, missing) {
				t.Fatalf("missing = %#v, want %q", got.Missing, missing)
			}
		})
	}
}

func TestValidateWhiteboardAnimationArtifactsRequiresCompleteScenePairs(t *testing.T) {
	required := []string{"output/final.mp4", "output/storyboard.json", "output/quality-report.json", "output/delivery-manifest.json"}
	tests := []struct {
		name       string
		files      []string
		valid      bool
		wantMissed string
	}{
		{name: "complete scene", files: append(append([]string{}, required...), "output/scenes/scene-01.png", "output/scenes/scene-01.annotation.json"), valid: true},
		{name: "missing scene pair", files: append(append([]string{}, required...), "output/scenes/scene-01.png"), wantMissed: "scene_annotations"},
		{name: "mismatched scene pair", files: append(append([]string{}, required...), "output/scenes/scene-01.png", "output/scenes/scene-02.annotation.json"), wantMissed: "scene_pairs"},
		{name: "no scene", files: required, wantMissed: "scene_images"},
		{name: "missing final video", files: []string{"output/storyboard.json", "output/quality-report.json", "output/delivery-manifest.json", "output/scenes/scene-01.png", "output/scenes/scene-01.annotation.json"}, wantMissed: "output/final.mp4"},
		{name: "failure state", files: append(append([]string{}, required...), "output/scenes/scene-01.png", "output/scenes/scene-01.annotation.json", "output/failure-state.json"), wantMissed: "successful whiteboard-animation completion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			files := make([]*model.TaskFile, 0, len(test.files))
			for _, path := range test.files {
				files = append(files, &model.TaskFile{FileName: filepath.Base(path), FilePath: path, FileSize: 1, State: model.TaskFileStateDelivered})
			}
			got := ValidateTaskArtifactsFromTaskFiles(&model.Task{Type: model.PlatformWhiteboardAnimation}, files)
			if got.Valid != test.valid {
				t.Fatalf("validation = %#v, want valid=%t", got, test.valid)
			}
			if test.wantMissed != "" && !containsArtifactName(got.Missing, test.wantMissed) {
				t.Fatalf("missing = %#v, want %q", got.Missing, test.wantMissed)
			}
		})
	}
}

func containsArtifactName(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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
