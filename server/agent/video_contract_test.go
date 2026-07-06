package agent

import (
	"os"
	"strings"
	"testing"
)

func TestVideoCreatorAgentStaysOnGenerationWorkflow(t *testing.T) {
	text := readRepoFile(t, "../../claudecode/agents/videocreator.md")
	if strings.Contains(frontmatterBlock(t, text), "\ntools:") {
		t.Fatal("videocreator agent must not define a tools allowlist; omitting tools lets Claude Code inherit MCP tools")
	}
	for _, want := range []string{
		"禁止调用 Claude `Agent` 工具",
		"videocreator",
		`agent_name="videocreator"`,
		"不得用 dreamina-video 作为 agent_name",
		"目标成片时长",
		"单次生成片段",
		"create_video_generation_job",
		"query_video_generation_job",
		"download_video_generation_results",
		"compose_video_segments",
		"validate_video_delivery",
		"generate_image",
		"anchor-strategy.md",
		"visual-anchor-pack.md",
		"visual-anchors/",
		"verify_with_vision",
		"register_video_reference",
		"不得自动进入字幕",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("videocreator.md missing %q", want)
		}
	}
	for _, forbidden := range []string{"video-use", "Remotion", "片头"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("videocreator.md should not include default editing workflow %q", forbidden)
		}
	}
}

func TestVideoEditorAgentStaysOnEditingWorkflow(t *testing.T) {
	text := readRepoFile(t, "../../claudecode/agents/videoeditor.md")
	if strings.Contains(frontmatterBlock(t, text), "\ntools:") {
		t.Fatal("videoeditor agent must not define a tools allowlist; omitting tools lets Claude Code inherit MCP tools")
	}
	for _, want := range []string{
		"videoeditor",
		"video-use",
		"final.mp4",
		"preview.mp4",
		"不得调用 Seedance",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("videoeditor.md missing %q", want)
		}
	}
	for _, forbidden := range []string{"create_video_generation_task", "download_video_generation_result"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("videoeditor.md should not include video generation tool %q", forbidden)
		}
	}
}

func TestVideoHookQualityGateIsRegistered(t *testing.T) {
	text := readRepoFile(t, "../../claudecode/hooks/hooks.json")
	if !strings.Contains(text, "video-quality-gate.sh") {
		t.Fatal("video-quality-gate.sh is not registered in hooks.json")
	}
}

func TestCodexVideoHookQualityGateIsRegistered(t *testing.T) {
	hooks := readRepoFile(t, "../../codex/hooks/hooks.json")
	for _, want := range []string{
		`"matcher": "videocreator"`,
		`"matcher": "videoeditor"`,
		"${PLUGIN_ROOT}/hooks/video-quality-gate.sh",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("codex hooks missing %q", want)
		}
	}

	script := readRepoFile(t, "../../codex/hooks/video-quality-gate.sh")
	for _, want := range []string{"videocreator", "videoeditor", "dreamina-video", "video-use"} {
		if !strings.Contains(script, want) {
			t.Fatalf("codex video quality gate missing %q", want)
		}
	}
}

func TestCodexDocsCountAllNativeSubagents(t *testing.T) {
	for _, path := range []string{
		"../../codex/README.md",
		"../../codex/CODEX.md",
		"../../codex/install/agents-registration.toml",
	} {
		text := readRepoFile(t, path)
		if strings.Contains(strings.ToLower(text), "six subagents") ||
			strings.Contains(strings.ToLower(text), "six anban-creator") ||
			strings.Contains(text, "6 native Codex subagents") {
			t.Fatalf("%s still documents six subagents", path)
		}
	}
}

func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
