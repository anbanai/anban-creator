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

func TestAgentDockerfileInstallsOfficialVideoOverlaySkills(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	raw, err := os.ReadFile(filepath.Join(root, "Dockerfile.agent"))
	if err != nil {
		t.Fatalf("agent Dockerfile missing: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"ca-certificates curl gh git jq fontconfig fonts-noto-cjk python3 python3-venv",
		"git config --global http.version HTTP/1.1",
		"npx -y skills@latest add heygen-com/hyperframes",
		"--skill music-to-video",
		"--skill slideshow",
		"npx -y skills@latest add remotion-dev/skills",
		"--skill remotion-best-practices",
		"--agent claude-code",
		"--copy",
		"-g",
		"-y",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("agent Dockerfile should install official video overlay skills, missing %q", want)
		}
	}
	if strings.Index(body, "USER node") > strings.Index(body, "npx -y skills@latest add heygen-com/hyperframes") {
		t.Fatalf("agent Dockerfile should install official skills as the node user")
	}
}

func TestSplitVideoAgentsReplaceShortVideoStudio(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	creatorRaw, err := os.ReadFile(filepath.Join(root, "claudecode", "agents", "videocreator.md"))
	if err != nil {
		t.Fatalf("videocreator agent missing: %v", err)
	}
	creator := string(creatorRaw)
	for _, want := range []string{
		"name: videocreator",
		"skills:",
		"- seedance-20",
		"register_video_reference",
		"prepare_video_generation_inputs",
		"create_video_generation_job",
		"video-input-contract.json",
		"generated visual anchors can supplement user media but cannot replace it",
		"compose_video_segments",
		`submit_agent_feedback(agent_name="videocreator"`,
	} {
		if !strings.Contains(creator, want) {
			t.Fatalf("videocreator agent missing %q", want)
		}
	}
	creatorFrontmatter := creator
	if end := strings.Index(creator[len("---\n"):], "\n---"); end >= 0 {
		creatorFrontmatter = creator[:len("---\n")+end+len("\n---")]
	}
	if strings.Contains(creatorFrontmatter, "\nmcpServers:") || strings.Contains(creatorFrontmatter, "\ntools:") {
		t.Fatal("videocreator agent must not define tools or mcpServers")
	}
	for _, banned := range []string{"short-video-studio", "upload_live_audio", "create_live_analysis_task", "create_video_asr_task", "pack_video_transcripts", "video-use"} {
		if strings.Contains(creator, banned) {
			t.Fatalf("videocreator agent should not mention %q", banned)
		}
	}

	editorRaw, err := os.ReadFile(filepath.Join(root, "claudecode", "agents", "videoeditor.md"))
	if err != nil {
		t.Fatalf("videoeditor agent missing: %v", err)
	}
	editor := string(editorRaw)
	for _, want := range []string{
		"name: videoeditor",
		"skills:",
		"- video-use",
		"- hyperframes-video-overlays",
		"- remotion-video-overlays",
		"- manim-video-overlays",
		"- pil-video-overlays",
		"- capcut-draft",
		"prepare_file_upload",
		"create_video_asr_task",
		"prepare_video_transcript_download",
		"anban video",
		"edit/media-manifest.json",
		"display rotation",
		"save-asr-result",
		"pack-transcripts",
		"match-script",
		"preview.mp4",
		"final.mp4",
		"普通素材剪辑不得调用",
		`submit_agent_feedback(agent_name="videoeditor"`,
	} {
		if !strings.Contains(editor, want) {
			t.Fatalf("videoeditor agent missing %q", want)
		}
	}
	editorFrontmatter := editor
	if end := strings.Index(editor[len("---\n"):], "\n---"); end >= 0 {
		editorFrontmatter = editor[:len("---\n")+end+len("\n---")]
	}
	if strings.Contains(editorFrontmatter, "\nmcpServers:") || strings.Contains(editorFrontmatter, "\ntools:") {
		t.Fatal("videoeditor agent must not define tools or mcpServers")
	}
	for _, banned := range []string{"short-video-studio", "upload_live_audio", "create_live_analysis_task", "create_video_generation_job", "prepare_video_generation_inputs", "seedance-20"} {
		if strings.Contains(editor, banned) {
			t.Fatalf("videoeditor agent should not mention %q", banned)
		}
	}

	codexCreatorRaw, err := os.ReadFile(filepath.Join(root, "codex", "agents", "videocreator.toml"))
	if err != nil {
		t.Fatalf("codex videocreator agent missing: %v", err)
	}
	codexCreator := string(codexCreatorRaw)
	for _, want := range []string{
		`name = "videocreator"`,
		"skills/seedance-20/SKILL.md",
		"prepare_video_generation_inputs",
		"video-input-contract.json",
		"compose_video_segments",
		`submit_agent_feedback(agent_name="videocreator"`,
	} {
		if !strings.Contains(codexCreator, want) {
			t.Fatalf("codex videocreator agent missing %q", want)
		}
	}

	codexEditorRaw, err := os.ReadFile(filepath.Join(root, "codex", "agents", "videoeditor.toml"))
	if err != nil {
		t.Fatalf("codex videoeditor agent missing: %v", err)
	}
	codexEditor := string(codexEditorRaw)
	for _, want := range []string{
		`name = "videoeditor"`,
		"skills/video-use/SKILL.md",
		"skills/hyperframes-video-overlays/SKILL.md",
		"skills/remotion-video-overlays/SKILL.md",
		"skills/manim-video-overlays/SKILL.md",
		"skills/pil-video-overlays/SKILL.md",
		"anban video",
		"edit/media-manifest.json",
		"display rotation",
		"prepare_video_transcript_download",
		"save-asr-result",
		"pack-transcripts",
		"match-script",
		"preview.mp4",
		"final.mp4",
		`submit_agent_feedback(agent_name="videoeditor"`,
	} {
		if !strings.Contains(codexEditor, want) {
			t.Fatalf("codex videoeditor agent missing %q", want)
		}
	}
	for _, banned := range []string{
		"create_video_generation_job",
		"prepare_video_generation_inputs",
		"seedance-20",
	} {
		if strings.Contains(codexEditor, banned) {
			t.Fatalf("codex videoeditor agent should not contain creator flow %q", banned)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "claudecode", "agents", "video.md")); !os.IsNotExist(err) {
		t.Fatalf("unified claudecode video agent should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "codex", "agents", "video.toml")); !os.IsNotExist(err) {
		t.Fatalf("unified codex video agent should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "claudecode", "agents", "short-video-studio.md")); !os.IsNotExist(err) {
		t.Fatalf("old claudecode short-video-studio agent should be removed")
	}
	if _, err := os.Stat(filepath.Join(root, "codex", "agents", "short-video-studio.toml")); !os.IsNotExist(err) {
		t.Fatalf("old codex short-video-studio agent should be removed")
	}

	regRaw, err := os.ReadFile(filepath.Join(root, "codex", "install", "agents-registration.toml"))
	if err != nil {
		t.Fatalf("codex registration missing: %v", err)
	}
	reg := string(regRaw)
	if !strings.Contains(reg, "[agents.videocreator]") || !strings.Contains(reg, "[agents.videoeditor]") || strings.Contains(reg, "[agents.video]") || strings.Contains(reg, "short-video-studio") {
		t.Fatalf("codex registration should contain split video agents and remove unified/short-video entries:\n%s", reg)
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
