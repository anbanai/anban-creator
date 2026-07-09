package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestOpenMontageTaskMapsToDedicatedAgent(t *testing.T) {
	if got := TaskTypeToAgent(model.PlatformOpenMontage); got != "openmontage" {
		t.Fatalf("TaskTypeToAgent(openmontage) = %q, want openmontage", got)
	}
}

func TestOpenMontageWorkspaceInputFileIsWritten(t *testing.T) {
	workDir := t.TempDir()
	task := &model.Task{Type: model.PlatformOpenMontage}
	task.SetOpenMontageInput(model.OpenMontageInput{
		Brief:       "make a launch video",
		PipelineKey: "social-short",
		Preferences: model.OpenMontagePreferences{
			AspectRatio:     "9:16",
			DurationSeconds: 30,
		},
	})

	if err := writeOpenMontageInputJSON(workDir, task); err != nil {
		t.Fatalf("writeOpenMontageInputJSON: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workDir, "openmontage-input.json"))
	if err != nil {
		t.Fatalf("read openmontage-input.json: %v", err)
	}
	var got model.OpenMontageInput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal openmontage-input.json: %v", err)
	}
	if got.Brief != "make a launch video" {
		t.Fatalf("Brief = %q, want make a launch video", got.Brief)
	}
	if got.PipelineKey != "social-short" {
		t.Fatalf("PipelineKey = %q, want social-short", got.PipelineKey)
	}
	if got.Preferences.AspectRatio != "9:16" || got.Preferences.DurationSeconds != 30 {
		t.Fatalf("Preferences = %+v, want aspect 9:16 duration 30", got.Preferences)
	}
}

func TestOpenMontageWorkDirArtifactsRequireFinalVideoAndManifest(t *testing.T) {
	task := &model.Task{Type: model.PlatformOpenMontage}

	t.Run("missing manifest", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "final.mp4"), []byte("video"), 0o644); err != nil {
			t.Fatalf("write final video: %v", err)
		}

		got := ValidateTaskArtifactsFromWorkDir(task, workDir)
		if got.Valid {
			t.Fatal("ValidateTaskArtifactsFromWorkDir valid = true, want false")
		}
		if got.Reason != "openmontage missing required deliverables: delivery-manifest.json" {
			t.Fatalf("Reason = %q, want missing delivery manifest", got.Reason)
		}
	})

	t.Run("accepts final video aliases with manifest", func(t *testing.T) {
		for _, name := range []string{"final.mp4", "final_video.mp4", "final-video.mp4"} {
			t.Run(name, func(t *testing.T) {
				workDir := t.TempDir()
				if err := os.WriteFile(filepath.Join(workDir, name), []byte("video"), 0o644); err != nil {
					t.Fatalf("write final video: %v", err)
				}
				if err := os.WriteFile(filepath.Join(workDir, "delivery-manifest.json"), []byte("{}"), 0o644); err != nil {
					t.Fatalf("write delivery manifest: %v", err)
				}

				got := ValidateTaskArtifactsFromWorkDir(task, workDir)
				if !got.Valid {
					t.Fatalf("ValidateTaskArtifactsFromWorkDir valid = false, reason=%q missing=%v", got.Reason, got.Missing)
				}
			})
		}
	})
}
