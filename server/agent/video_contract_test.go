package agent

import (
	"os"
	"strings"
	"testing"
)

func TestVideoAgentForbidsNestedAgentForMainWorkflow(t *testing.T) {
	text := readRepoFile(t, "../../claudecode/agents/video.md")
	if strings.Contains(frontmatterBlock(t, text), "\ntools:") {
		t.Fatal("video agent must not define a tools allowlist; omitting tools lets Claude Code inherit MCP tools")
	}
	for _, want := range []string{
		"禁止调用 Claude `Agent` 工具",
		"必须在当前 video agent 上下文内完成",
		"create_video_generation_task",
		"query_video_generation_task",
		"download_video_generation_result",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("video.md missing %q", want)
		}
	}
}

func TestVideoHookQualityGateIsRegistered(t *testing.T) {
	text := readRepoFile(t, "../../claudecode/hooks/hooks.json")
	if !strings.Contains(text, "video-quality-gate.sh") {
		t.Fatal("video-quality-gate.sh is not registered in hooks.json")
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
