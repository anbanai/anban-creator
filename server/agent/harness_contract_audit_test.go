package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessImageToolExamplesCarryTaskIdentity(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"harness/skills/article-visual-design/SKILL.md",
		"harness/skills/article-cover-design/SKILL.md",
		"harness/skills/ecommerce-visual-design/SKILL.md",
		"harness/skills/ecommerce-product-analysis/SKILL.md",
		"harness/skills/seednote-visual-design/SKILL.md",
		"harness/skills/short-video-cover/SKILL.md",
		"harness/skills/portrait-pose-variants/SKILL.md",
		"harness/skills/moments/SKILL.md",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readArticleContractFile(t, filepath.Join(root, rel))
			for _, tool := range []string{"get_project_profile(", "generate_image(", "analyze_image("} {
				remaining := body
				for {
					at := strings.Index(remaining, tool)
					if at < 0 {
						break
					}
					call := remaining[at:]
					end := strings.Index(call, ")")
					if end < 0 {
						t.Fatalf("unterminated %s", tool)
					}
					call = call[:end]
					if !strings.Contains(call, "project_id") || !strings.Contains(call, "task_id") {
						t.Fatalf("%s missing project_id/task_id: %s", tool, call)
					}
					remaining = remaining[at+len(tool):]
				}
			}
		})
	}
}

func TestHarnessAutonomyAndFailureContracts(t *testing.T) {
	root := articleContractRepoRoot(t)
	checks := map[string][]string{
		"harness/packs/article/agent.claude.md":      {"readiness.status", "blocked", "output/draft.json"},
		"harness/packs/article/agent.codex.toml":     {"readiness.status", "blocked", "output/draft.json"},
		"harness/packs/moments/agent.codex.toml":     {"不得询问", "结构化失败"},
		"harness/packs/live-slicer/agent.claude.md":  {"curl", "ffmpeg", "ffprobe"},
		"harness/packs/live-slicer/agent.codex.toml": {"curl", "ffmpeg", "ffprobe"},
		"harness/packs/seednote/agent.claude.md":     {"viral_analysis", "quality_status=failed", "generate_image"},
		"harness/packs/seednote/agent.codex.toml":    {"viral_analysis", "quality_status=failed", "generate_image"},
		"harness/packs/montage/agent.codex.toml":     {"checkpoint", "decision", "output/failure-diagnosis.md", "output/failure-state.json", "recoverable_failure", "invalid_video_aspect_ratio"},
		"harness/packs/montage/agent.claude.md":      {"checkpoint", "decision", "output/failure-diagnosis.md", "output/failure-state.json", "recoverable_failure", "invalid_video_aspect_ratio"},
	}
	for rel, terms := range checks {
		t.Run(rel, func(t *testing.T) {
			body := readArticleContractFile(t, filepath.Join(root, rel))
			for _, term := range terms {
				if !strings.Contains(body, term) {
					t.Fatalf("missing contract term %q", term)
				}
			}
		})
	}

	moments := readArticleContractFile(t, filepath.Join(root, "harness/packs/moments/agent.codex.toml"))
	if strings.Contains(moments, "无法判断时向用户列出候选") {
		t.Fatal("Moments Codex agent must not ask the user to choose a project")
	}
}

func TestCapCutTemplatesAreRealAndReferenced(t *testing.T) {
	root := articleContractRepoRoot(t)
	body := readArticleContractFile(t, filepath.Join(root, "harness/skills/capcut-draft/SKILL.md"))
	if strings.Contains(body, "aux_sound_project.json") {
		t.Fatal("CapCut skill references a missing aux_sound_project.json template")
	}
	for _, name := range []string{"draft_info.json", "draft_meta_info.json", "aux_sound_channel.json"} {
		if !strings.Contains(body, name) {
			t.Fatalf("CapCut skill does not reference %s", name)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "harness/skills/capcut-draft/templates"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, "harness/skills/capcut-draft/templates", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatalf("template %s is not valid JSON: %v", entry.Name(), err)
		}
	}
}
