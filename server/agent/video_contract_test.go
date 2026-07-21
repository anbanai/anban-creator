package agent

import (
	"os"
	"strings"
	"testing"
)

func TestVideoAgentsUseDedicatedCreatorAndEditorContracts(t *testing.T) {
	creator := readRepoFile(t, "../../claudecode/agents/videocreator.md")
	editor := readRepoFile(t, "../../claudecode/agents/videoeditor.md")
	for path, text := range map[string]string{
		"claudecode/agents/videocreator.md": creator,
		"claudecode/agents/videoeditor.md":  editor,
	} {
		if strings.Contains(frontmatterBlock(t, text), "\ntools:") {
			t.Fatalf("%s must not define a tools allowlist; omitting tools lets Claude Code inherit MCP tools", path)
		}
		if strings.Contains(frontmatterBlock(t, text), "mcpServers") {
			t.Fatalf("%s must not define mcpServers; plugin agents inherit plugin-level MCP", path)
		}
		if !strings.Contains(text, "禁止调用 Claude `Agent` 工具") {
			t.Fatalf("%s must forbid nested Agent delegation", path)
		}
	}
	for _, want := range []string{
		"name: videocreator",
		"workflow=`videocreator`",
		"analyze_video_reference",
		"video-understanding.json",
		"深层意图",
		"create_video_generation_job",
		"query_video_generation_job",
		"download_video_generation_results",
		"compose_video_segments",
		"validate_video_delivery",
		`agent_name="videocreator"`,
	} {
		if !strings.Contains(creator, want) {
			t.Fatalf("videocreator.md missing %q", want)
		}
	}
	for _, forbidden := range []string{"video-use", "capcut-draft", `agent_name="video"`} {
		if strings.Contains(creator, forbidden) {
			t.Fatalf("videocreator.md must not contain editor/unified contract %q", forbidden)
		}
	}
	for _, want := range []string{
		"name: videoeditor",
		"video-use",
		"prepare_file_upload",
		"create_video_asr_task",
		"edl.json",
		"final.mp4",
		`agent_name="videoeditor"`,
	} {
		if !strings.Contains(editor, want) {
			t.Fatalf("videoeditor.md missing %q", want)
		}
	}
	for _, forbidden := range []string{"create_video_generation_job", "seedance-20", `agent_name="video"`} {
		if strings.Contains(editor, forbidden) {
			t.Fatalf("videoeditor.md must not contain creator/unified contract %q", forbidden)
		}
	}

	codexCreator := readRepoFile(t, "../../codex/agents/videocreator.toml")
	codexEditor := readRepoFile(t, "../../codex/agents/videoeditor.toml")
	for _, want := range []string{`name = "videocreator"`, "workflow=videocreator", "analyze_video_reference", "video-understanding.json", "深层意图", "submit_agent_feedback(agent_name=\"videocreator\""} {
		if !strings.Contains(codexCreator, want) {
			t.Fatalf("codex videocreator.toml missing %q", want)
		}
	}
	for _, want := range []string{`name = "videoeditor"`, "skills/video-use/SKILL.md", "submit_agent_feedback(agent_name=\"videoeditor\""} {
		if !strings.Contains(codexEditor, want) {
			t.Fatalf("codex videoeditor.toml missing %q", want)
		}
	}
}

func TestSplitVideoHookQualityGatesAreRegistered(t *testing.T) {
	text := readRepoFile(t, "../../claudecode/hooks/hooks.json")
	for _, want := range []string{
		`"matcher": "^anban:videocreator$"`,
		`"matcher": "^anban:videoeditor$"`,
		"videocreator-quality-gate.sh",
		"videoeditor-quality-gate.sh",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("claudecode hooks missing %q", want)
		}
	}
}

func TestCodexSplitVideoHookQualityGatesAreRegistered(t *testing.T) {
	hooks := readRepoFile(t, "../../codex/hooks/hooks.json")
	for _, want := range []string{
		`"matcher": "videocreator"`,
		`"matcher": "videoeditor"`,
		"${PLUGIN_ROOT}/hooks/videocreator-quality-gate.sh",
		"${PLUGIN_ROOT}/hooks/videoeditor-quality-gate.sh",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("codex hooks missing %q", want)
		}
	}

	creatorScript := readRepoFile(t, "../../codex/hooks/videocreator-quality-gate.sh")
	for _, want := range []string{"videocreator", "视频生成工作流", "task_id not in text"} {
		if !strings.Contains(creatorScript, want) {
			t.Fatalf("codex videocreator quality gate missing %q", want)
		}
	}
	for _, forbidden := range []string{"output/video/$task_id", "seedance-20", "dreamina-video\" \"$manifest\""} {
		if strings.Contains(creatorScript, forbidden) {
			t.Fatalf("codex videocreator quality gate must not use legacy/generic manifest discovery %q", forbidden)
		}
	}
	editorScript := readRepoFile(t, "../../codex/hooks/videoeditor-quality-gate.sh")
	for _, want := range []string{"videoeditor", "video-use", "edl.json", "draft_info.json", "draft_meta_info.json", "task_id not in text"} {
		if !strings.Contains(editorScript, want) {
			t.Fatalf("codex videoeditor quality gate missing %q", want)
		}
	}
	if strings.Contains(editorScript, "output/video/$task_id") || strings.Contains(editorScript, "workflow=.*video-use") {
		t.Fatalf("codex videoeditor quality gate must not use legacy/generic manifest discovery")
	}
}

func TestVideoCreatorAgentsOwnWorkflowWithoutSkill(t *testing.T) {
	for _, path := range []string{"../../claudecode/agents/videocreator.md", "../../codex/agents/videocreator.toml"} {
		text := readRepoFile(t, path)
		for _, want := range []string{"workflow", "videocreator", "create_video_generation_job", "validate_video_delivery"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s missing agent-owned workflow term %q", path, want)
			}
		}
		for _, forbidden := range []string{"seedance-20", "skills/dreamina-video/SKILL.md"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s still depends on removed video Skill %q", path, forbidden)
			}
		}
	}
}

func TestVideoDistributionDoesNotExposeUnifiedVideoAgent(t *testing.T) {
	for _, path := range []string{
		"../../claudecode/agents/video.md",
		"../../codex/agents/video.toml",
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unified video agent should not be distributed: %s", path)
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
		for _, forbidden := range []string{`name = "video"`, `"matcher": "anban:video"`, `"matcher": "video"`, "统一视频", "统一入口"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("%s still exposes unified video agent %q", path, forbidden)
			}
		}
	}
}

func TestAgentDockerfileInstallsPluginWithoutRemovedVideoSkills(t *testing.T) {
	path := "../../Dockerfile.agent"
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

	for _, path := range []string{"../../claudecode/skills/seedance-20", "../../codex/skills/seedance-20"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("removed video Skill must not be distributed at %s", path)
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
