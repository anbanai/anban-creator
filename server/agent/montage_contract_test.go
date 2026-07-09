package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestMontageTaskMapsToDedicatedAgent(t *testing.T) {
	if got := TaskTypeToAgent(model.PlatformMontage); got != "montage" {
		t.Fatalf("TaskTypeToAgent(montage) = %q, want montage", got)
	}
}

func TestMontageIsCanonicalAgentName(t *testing.T) {
	if got := TaskTypeToAgent("montage"); got != "montage" {
		t.Fatalf("TaskTypeToAgent(montage) = %q, want montage", got)
	}
	if got := TaskTypeToAgent("open" + "montage"); got == "montage" {
		t.Fatalf("TaskTypeToAgent(%s) = %q, old %s name must not remain canonical", "open"+"montage", got, "open"+"montage")
	}
}

func TestMontageWorkspaceInputFileIsWritten(t *testing.T) {
	workDir := t.TempDir()
	task := &model.Task{Type: model.PlatformMontage}
	task.SetMontageInput(model.MontageInput{
		Brief:       "make a launch video",
		PipelineKey: "social-short",
		Preferences: model.MontagePreferences{
			AspectRatio:     "9:16",
			DurationSeconds: 30,
		},
	})

	if err := writeMontageInputJSON(workDir, task); err != nil {
		t.Fatalf("writeMontageInputJSON: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workDir, "montage-input.json"))
	if err != nil {
		t.Fatalf("read montage-input.json: %v", err)
	}
	var got model.MontageInput
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal montage-input.json: %v", err)
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

func TestMontageWorkDirArtifactsRequireFinalVideoAndManifest(t *testing.T) {
	task := &model.Task{Type: model.PlatformMontage}

	t.Run("missing manifest", func(t *testing.T) {
		workDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(workDir, "final.mp4"), []byte("video"), 0o644); err != nil {
			t.Fatalf("write final video: %v", err)
		}

		got := ValidateTaskArtifactsFromWorkDir(task, workDir)
		if got.Valid {
			t.Fatal("ValidateTaskArtifactsFromWorkDir valid = true, want false")
		}
		if got.Reason != "montage missing required deliverables: delivery-manifest.json" {
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

func TestMontagePluginContractsAreDistributed(t *testing.T) {
	root := repoRoot(t)
	claudeAgent := readRepoFile(t, filepath.Join(root, "claudecode", "agents", "montage.md"))
	for _, want := range []string{
		"name: montage",
		"skills:",
		"- montage",
		"montage-input.json",
		"montage-project.json",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"delivery-manifest.json",
		"final.mp4",
		"submit_agent_feedback",
	} {
		if !strings.Contains(claudeAgent, want) {
			t.Fatalf("claudecode montage agent missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"create_video_generation_job",
		"validate_video_delivery",
		"- video-use",
		"skills/video-use/SKILL.md",
	} {
		if strings.Contains(claudeAgent, forbidden) {
			t.Fatalf("claudecode montage agent must not reference existing video workflow %q", forbidden)
		}
	}

	codexAgent := readRepoFile(t, filepath.Join(root, "codex", "agents", "montage.toml"))
	for _, want := range []string{
		`name = "montage"`,
		"montage-input.json",
		"montage-project.json",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"delivery-manifest.json",
		"final_video",
		`submit_agent_feedback(agent_name=\"montage\"`,
		"__PLUGIN_ROOT__/skills/montage/SKILL.md",
	} {
		if !strings.Contains(codexAgent, want) {
			t.Fatalf("codex montage agent missing %q", want)
		}
	}

	reg := readRepoFile(t, filepath.Join(root, "codex", "install", "agents-registration.toml"))
	for _, want := range []string{
		"[agents.montage]",
		"montage.toml",
		"Montage 视频生产引擎",
	} {
		if !strings.Contains(reg, want) {
			t.Fatalf("codex agents registration missing %q", want)
		}
	}
}

func TestMontageSkillMirrorsStayInSync(t *testing.T) {
	root := repoRoot(t)
	canonical := readRepoFile(t, filepath.Join(root, "claudecode", "skills", "montage", "SKILL.md"))
	for _, distro := range []string{"codex", "openclaw"} {
		path := filepath.Join(root, distro, "skills", "montage", "SKILL.md")
		if got := readRepoFile(t, path); got != canonical {
			t.Fatalf("%s must match claudecode montage skill", path)
		}
	}
	for _, want := range []string{
		"name: montage",
		"montage-input.json",
		"montage-project.json",
		"ANBAN_MONTAGE_SUBMODULE_PATH",
		"delivery-manifest.json",
		"third_party/OpenMontage",
		"Do not call `create_video_generation_job`",
		"Do not modify files under `third_party/OpenMontage`",
	} {
		if !strings.Contains(canonical, want) {
			t.Fatalf("montage skill missing %q", want)
		}
	}
}

func TestMontagePluginManifestsAdvertiseSupport(t *testing.T) {
	root := repoRoot(t)
	for _, path := range []string{
		filepath.Join(root, "claudecode", ".claude-plugin", "plugin.json"),
		filepath.Join(root, "codex", ".codex-plugin", "plugin.json"),
		filepath.Join(root, "openclaw", "openclaw.plugin.json"),
	} {
		body := readRepoFile(t, path)
		if !strings.Contains(body, "Montage") {
			t.Fatalf("%s must advertise Montage support", path)
		}
	}
}

func TestMontageSubmodulePathIsDeclared(t *testing.T) {
	root := repoRoot(t)
	gitmodules := readRepoFile(t, filepath.Join(root, ".gitmodules"))
	if !strings.Contains(gitmodules, "third_party/OpenMontage") {
		t.Fatal(".gitmodules missing third_party/OpenMontage submodule")
	}
	if !strings.Contains(gitmodules, "https://github.com/calesthio/OpenMontage.git") {
		t.Fatal(".gitmodules missing Montage upstream URL")
	}
}
