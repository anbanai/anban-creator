package agent

import (
	"os"
	"strings"
	"testing"
)

func TestVideoAgentUsesUnifiedSkillDrivenIntake(t *testing.T) {
	text := readRepoFile(t, "../../claudecode/agents/video.md")
	if strings.Contains(frontmatterBlock(t, text), "\ntools:") {
		t.Fatal("video agent must not define a tools allowlist; omitting tools lets Claude Code inherit MCP tools")
	}
	for _, want := range []string{
		"禁止调用 Claude `Agent` 工具",
		"video",
		"video_input",
		"CLAUDE.md",
		"Studio 不再提供",
		"业务玩法",
		"制作模式",
		"seedance-20",
		"video-use",
		"create_video_generation_job",
		"query_video_generation_job",
		"download_video_generation_results",
		"compose_video_segments",
		"validate_video_delivery",
		`agent_name="video"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("video.md missing %q", want)
		}
	}

	codexAgent := readRepoFile(t, "../../codex/agents/video.toml")
	for _, want := range []string{
		`name = "video"`,
		"video_input",
		"CLAUDE.md",
		"Studio 不再提供",
		"业务玩法",
		"制作模式",
		"seedance-20",
		"skills/video-use/SKILL.md",
		"create_video_generation_job",
		"submit_agent_feedback(agent_name=\"video\"",
	} {
		if !strings.Contains(codexAgent, want) {
			t.Fatalf("codex video.toml missing %q", want)
		}
	}
}

func TestVideoSkillContractsUseVideoInputReferences(t *testing.T) {
	for _, path := range []string{
		"../../claudecode/skills/seedance-20/references/mcp-contract.md",
		"../../codex/skills/seedance-20/references/mcp-contract.md",
		"../../openclaw/skills/seedance-20/references/mcp-contract.md",
		"../../claudecode/skills/seedance-20/references/anban-mcp-contract.md",
		"../../codex/skills/seedance-20/references/anban-mcp-contract.md",
		"../../openclaw/skills/seedance-20/references/anban-mcp-contract.md",
		"../../claudecode/skills/dreamina-video/references/mcp-contract.md",
		"../../codex/skills/dreamina-video/references/mcp-contract.md",
		"../../openclaw/skills/dreamina-video/references/mcp-contract.md",
	} {
		text := readRepoFile(t, path)
		for _, want := range []string{
			"video.input",
			"video_input.references",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
		for _, forbidden := range []string{
			"task/plan `video_config.references`",
			"task/plan video_config.references",
			"task or plan has `video_config.references`",
			"metadata from task/plan `video_config.references`",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s still points user references at video_config: %q", path, forbidden)
			}
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
		`"matcher": "video"`,
		"${PLUGIN_ROOT}/hooks/video-quality-gate.sh",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("codex hooks missing %q", want)
		}
	}

	script := readRepoFile(t, "../../codex/hooks/video-quality-gate.sh")
	for _, want := range []string{"video", "seedance-20", "dreamina-video", "video-use"} {
		if !strings.Contains(script, want) {
			t.Fatalf("codex video quality gate missing %q", want)
		}
	}
}

func TestCodexVideoAgentsUseSeedance20Skill(t *testing.T) {
	text := readRepoFile(t, "../../codex/agents/video.toml")
	for _, want := range []string{
		"seedance-20",
		"__PLUGIN_ROOT__/skills/seedance-20/SKILL.md",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("codex video.toml missing %q", want)
		}
	}
	for _, forbidden := range []string{
		"using dreamina-video skill",
		"path = \"__PLUGIN_ROOT__/skills/dreamina-video/SKILL.md\"",
		"workflow=dreamina-video",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("codex video.toml should not keep dreamina-video as primary workflow: %q", forbidden)
		}
	}
}

func TestVideoDistributionDoesNotExposeLegacySplitAgents(t *testing.T) {
	for _, path := range []string{
		"../../claudecode/agents/videocreator.md",
		"../../claudecode/agents/videoeditor.md",
		"../../codex/agents/videocreator.toml",
		"../../codex/agents/videoeditor.toml",
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("legacy split video agent should not be distributed: %s", path)
		}
	}

	for _, path := range []string{
		"../../claudecode/hooks/hooks.json",
		"../../codex/hooks/hooks.json",
		"../../codex/install/agents-registration.toml",
		"../../codex/README.md",
		"../../codex/CODEX.md",
		"../../claudecode/docs/plugin-development.md",
	} {
		text := readRepoFile(t, path)
		for _, forbidden := range []string{"videocreator", "videoeditor", "VideoCreator", "VideoEditor"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s still exposes legacy split video agent %q", path, forbidden)
			}
		}
	}
}

func TestDockerfilesInstallPluginWithSeedance20Skill(t *testing.T) {
	for _, path := range []string{"../../agent/Dockerfile", "../../server/Dockerfile"} {
		text := readRepoFile(t, path)
		for _, want := range []string{
			"claude plugin marketplace add /anbanai",
			"claude plugin install --scope user anban@anbanai",
		} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
		if !strings.Contains(text, "COPY claudecode/") || !strings.Contains(text, "/anbanai/") {
			t.Fatalf("%s must copy claudecode plugin assets into /anbanai", path)
		}
	}

	if _, err := os.Stat("../../claudecode/skills/seedance-20/SKILL.md"); err != nil {
		t.Fatalf("Docker-installed Claude plugin must include seedance-20 skill: %v", err)
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
