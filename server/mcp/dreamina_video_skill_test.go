package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDreaminaVideoSkillFiles(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	var firstBody string
	for _, plugin := range []string{"claudecode", "codex", "openclaw"} {
		skillDir := filepath.Join(root, plugin, "skills", "dreamina-video")
		skillPath := filepath.Join(skillDir, "SKILL.md")
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("%s dreamina-video SKILL.md missing: %v", plugin, err)
		}
		body := string(raw)
		for _, want := range []string{
			"name: dreamina-video",
			"即梦",
			"Seedance",
			"种草",
			"带货",
			"获客",
			"推广",
			"register_video_reference",
			"build_video_generation_plan",
			"create_video_generation_task",
			"query_video_generation_task",
			"download_video_generation_result",
			"reference-anchors.md",
			"script.md",
			"shot-plan.md",
			"quality-review.md",
			"references/methodology.md",
			"references/stability.md",
			"references/prompt-templates.md",
			"references/mcp-contract.md",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s SKILL.md missing %q", plugin, want)
			}
		}
		for _, ref := range []string{"methodology.md", "stability.md", "prompt-templates.md", "mcp-contract.md"} {
			refPath := filepath.Join(skillDir, "references", ref)
			refRaw, err := os.ReadFile(refPath)
			if err != nil {
				t.Fatalf("%s reference %s missing: %v", plugin, ref, err)
			}
			refBody := string(refRaw)
			if len(strings.TrimSpace(refBody)) < 300 {
				t.Fatalf("%s reference %s is too thin", plugin, ref)
			}
			if ref == "mcp-contract.md" {
				for _, want := range []string{"file_path", "ark_url", "OSS/CDN"} {
					if !strings.Contains(refBody, want) {
						t.Fatalf("%s reference %s missing %q", plugin, ref, want)
					}
				}
			}
		}
		for _, banned := range []string{
			"curl https://ark.cn-beijing.volces.com",
			"VOLCENGINE_ARK_API_KEY",
			"直接调用火山",
			"dreamina text2video",
			"dreamina image2video",
		} {
			if strings.Contains(body, banned) {
				t.Fatalf("%s SKILL.md should not mention %q", plugin, banned)
			}
		}
		if firstBody == "" {
			firstBody = body
		} else if body != firstBody {
			t.Fatalf("dreamina-video SKILL.md differs between plugins")
		}
	}
}
