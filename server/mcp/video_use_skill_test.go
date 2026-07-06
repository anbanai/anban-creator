package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func requireValidSkillFrontmatter(t *testing.T, plugin, skillName, body string) {
	t.Helper()
	const marker = "---\n"
	if !strings.HasPrefix(body, marker) {
		t.Fatalf("%s %s SKILL.md missing YAML frontmatter", plugin, skillName)
	}
	rest := strings.TrimPrefix(body, marker)
	end := strings.Index(rest, marker)
	if end < 0 {
		t.Fatalf("%s %s SKILL.md frontmatter is not closed", plugin, skillName)
	}
	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(rest[:end]), &fm); err != nil {
		t.Fatalf("%s %s SKILL.md has invalid YAML frontmatter: %v", plugin, skillName, err)
	}
	if strings.TrimSpace(fm.Name) == "" || strings.TrimSpace(fm.Description) == "" {
		t.Fatalf("%s %s SKILL.md frontmatter requires non-empty name and description", plugin, skillName)
	}
}

func TestVideoUseSkillFiles(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	var firstBody string
	for _, plugin := range []string{"claudecode", "codex", "openclaw"} {
		skillDir := filepath.Join(root, plugin, "skills", "video-use")
		skillPath := filepath.Join(skillDir, "SKILL.md")
		raw, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("%s video-use SKILL.md missing: %v", plugin, err)
		}
		body := string(raw)
		requireValidSkillFrontmatter(t, plugin, "video-use", body)
		for _, want := range []string{
			"name: video-use",
			"prepare_file_upload",
			"create_video_asr_task",
			"query_video_asr_task",
			"prepare_video_transcript_download",
			"pack_video_transcripts",
			"anban video",
			"media-manifest.json",
			"display rotation",
			"draft",
			"preview",
			"final",
			"file-based transcript",
			"output_width/output_height",
			"overlay dimensions",
			"FunASR",
			"Aliyun FunASR HTTP",
			"Source Han Sans",
			"思源黑体",
			"subtitles are applied LAST",
			"master.srt",
			"setpts=PTS-STARTPTS+T/TB",
			"takes_packed.md",
			"edl.json",
			"timeline_view",
			"render.py",
			"grade.py",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s video-use SKILL.md missing %q", plugin, want)
			}
		}
		for _, banned := range []string{
			"ElevenLabs",
			"Scribe",
			"ELEVENLABS_API_KEY",
			"api.elevenlabs.io",
			"xi-api-key",
		} {
			if strings.Contains(body, banned) {
				t.Fatalf("%s video-use SKILL.md should not mention %q", plugin, banned)
			}
		}
		if firstBody == "" {
			firstBody = body
		} else if body != firstBody {
			t.Fatalf("video-use SKILL.md differs between plugins")
		}
		for _, helper := range []string{"render.py", "grade.py", "timeline_view.py"} {
			if _, err := os.Stat(filepath.Join(skillDir, "scripts", helper)); err != nil {
				t.Fatalf("%s video-use helper %s missing: %v", plugin, helper, err)
			}
		}
	}
}

func TestClaudeCodeSkillFrontmatterParses(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	skillRoot := filepath.Join(root, "claudecode", "skills")
	entries, err := os.ReadDir(skillRoot)
	if err != nil {
		t.Fatalf("read claudecode skills: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skill := entry.Name()
		raw, err := os.ReadFile(filepath.Join(skillRoot, skill, "SKILL.md"))
		if err != nil {
			t.Fatalf("%s SKILL.md missing: %v", skill, err)
		}
		requireValidSkillFrontmatter(t, "claudecode", skill, string(raw))
	}
}

func TestVideoOverlaySkillFiles(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	skills := []string{
		"hyperframes-video-overlays",
		"remotion-video-overlays",
		"manim-video-overlays",
		"pil-video-overlays",
	}
	for _, skill := range skills {
		var firstBody string
		for _, plugin := range []string{"claudecode", "codex", "openclaw"} {
			skillPath := filepath.Join(root, plugin, "skills", skill, "SKILL.md")
			raw, err := os.ReadFile(skillPath)
			if err != nil {
				t.Fatalf("%s %s SKILL.md missing: %v", plugin, skill, err)
			}
			body := string(raw)
			requireValidSkillFrontmatter(t, plugin, skill, body)
			for _, want := range []string{
				"name: " + skill,
				"edit/animations/slot_<id>/",
				"render.webm",
				"alpha",
				"edl.json",
				"overlays",
				"file",
				"start",
				"end",
				"x",
				"y",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s %s missing %q", plugin, skill, want)
				}
			}
			if firstBody == "" {
				firstBody = body
			} else if body != firstBody {
				t.Fatalf("%s SKILL.md differs between plugins", skill)
			}
		}
	}
}

func TestServerDockerfileInstallsOfficialVideoOverlaySkills(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "server", "Dockerfile"))
	if err != nil {
		t.Fatalf("server Dockerfile missing: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"ca-certificates curl git jq fontconfig fonts-noto-cjk python3",
		"git config --global http.version HTTP/1.1",
		"npx -y skills@1.5.14 add heygen-com/hyperframes",
		"--skill music-to-video",
		"--skill slideshow",
		"npx -y skills@1.5.14 add remotion-dev/skills",
		"--skill remotion-best-practices",
		"--agent claude-code",
		"--copy",
		"-g",
		"-y",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("server Dockerfile should install official video overlay skills, missing %q", want)
		}
	}
	if strings.Index(body, "USER node") > strings.Index(body, "npx -y skills@1.5.14 add heygen-com/hyperframes") {
		t.Fatalf("server Dockerfile should install official skills as the node user")
	}
}

func TestVideoAgentReplacesShortVideoStudio(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	claudeAgent := filepath.Join(root, "claudecode", "agents", "video.md")
	raw, err := os.ReadFile(claudeAgent)
	if err != nil {
		t.Fatalf("video agent missing: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"name: video",
		"skills:",
		"- seedance-20",
		"- video-use",
		"- hyperframes-video-overlays",
		"- remotion-video-overlays",
		"- manim-video-overlays",
		"- pil-video-overlays",
		"- short-video-cover",
		"- portrait-pose-variants",
		"- capcut-draft",
		"插件级 `.mcp.json`",
		"prepare_file_upload",
		"create_video_asr_task",
		"query_video_asr_task",
		"prepare_video_transcript_download",
		"pack_video_transcripts",
		"anban video",
		"media-manifest.json",
		"display rotation",
		"save-asr-result",
		"pack-transcripts",
		"match-script",
		"draft",
		"preview",
		"final",
		"普通素材剪辑不得调用",
		"register_video_reference",
		"create_video_generation_job",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("video agent missing %q", want)
		}
	}
	frontmatter := body
	if end := strings.Index(body[len("---\n"):], "\n---"); end >= 0 {
		frontmatter = body[:len("---\n")+end+len("\n---")]
	}
	if strings.Contains(frontmatter, "\nmcpServers:") {
		t.Fatal("video agent must not define mcpServers; plugin subagents inherit the plugin-level MCP server")
	}
	for _, banned := range []string{"short-video-studio", "upload_live_audio", "create_live_analysis_task"} {
		if strings.Contains(body, banned) {
			t.Fatalf("video agent should not mention %q", banned)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "claudecode", "agents", "short-video-studio.md")); !os.IsNotExist(err) {
		t.Fatalf("old claudecode short-video-studio agent should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "codex", "agents", "short-video-studio.toml")); !os.IsNotExist(err) {
		t.Fatalf("old codex short-video-studio agent should be removed")
	}

	codexAgentRaw, err := os.ReadFile(filepath.Join(root, "codex", "agents", "video.toml"))
	if err != nil {
		t.Fatalf("codex video agent missing: %v", err)
	}
	codexAgent := string(codexAgentRaw)
	for _, want := range []string{
		`name = "video"`,
		"music-to-video",
		"slideshow",
		"remotion-best-practices",
		"skills/seedance-20/SKILL.md",
		"skills/video-use/SKILL.md",
		"skills/hyperframes-video-overlays/SKILL.md",
		"skills/remotion-video-overlays/SKILL.md",
		"skills/manim-video-overlays/SKILL.md",
		"skills/pil-video-overlays/SKILL.md",
		"anban video",
		"media-manifest.json",
		"display rotation",
		"prepare_video_transcript_download",
		"save-asr-result",
		"pack-transcripts",
		"match-script",
		"draft",
		"preview",
		"final",
	} {
		if !strings.Contains(codexAgent, want) {
			t.Fatalf("codex video agent missing %q", want)
		}
	}
	for _, banned := range []string{
		"保存返回的 normalized JSON",
		"调用 pack_video_transcripts，保存 edit/takes_packed.md",
	} {
		if strings.Contains(codexAgent, banned) {
			t.Fatalf("codex video agent still contains old flow %q", banned)
		}
	}

	regRaw, err := os.ReadFile(filepath.Join(root, "codex", "install", "agents-registration.toml"))
	if err != nil {
		t.Fatalf("codex registration missing: %v", err)
	}
	reg := string(regRaw)
	if !strings.Contains(reg, "[agents.video]") || strings.Contains(reg, "short-video-studio") {
		t.Fatalf("codex registration should contain video and remove short-video-studio:\n%s", reg)
	}
}

func TestVideoUseOverlayHandoffContract(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "claudecode", "skills", "video-use", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{
		"Use official `music-to-video` or `slideshow` skills when the brief matches their HyperFrames workflows",
		"then use `hyperframes-video-overlays` skill",
		"Use official `remotion-best-practices` skill for Remotion implementation guidance",
		"then use `remotion-video-overlays` skill",
		"Use `manim-video-overlays` skill",
		"Use `pil-video-overlays` skill",
		"edit/animations/slot_<id>/render.webm",
		"overlays[]",
		"subtitles are applied LAST",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("video-use overlay handoff missing %q", want)
		}
	}
}
