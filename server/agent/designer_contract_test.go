package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesignerAgentKeepsMCPAndSkillLoadingContract(t *testing.T) {
	root := repoRoot(t)
	agentPath := filepath.Join(root, "claudecode", "agents", "designer.md")

	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read designer agent: %v", err)
	}
	body := string(data)

	frontmatter := frontmatterBlock(t, body)
	if strings.Contains(frontmatter, "\ntools:") {
		t.Fatal("designer agent must not define a tools allowlist; omitting tools lets Claude Code inherit MCP tools")
	}

	required := []string{
		"mcpServers:",
		"- anbanwriter",
		"skills:",
		"- line-art-coloring",
		"Claude Code subagent 的 `tools:` 字段是 allowlist",
		"不要在本 agent frontmatter 中声明 `tools:`",
		"`mcp__anbanwriter__generate_image`",
		"`mcp__anbanwriter__analyze_image`",
		"`mcp__anbanwriter__download_image`",
		"`mcp__anbanwriter__prepare_workspace`",
		"`mcp__anbanwriter__update_task_progress`",
		"ANBANWRITER_API_URL",
		"ANBANWRITER_DEFAULT_CHANNEL",
		"ANBANWRITER_API_KEY",
		"无法看到 `mcp__anbanwriter__generate_image`",
		"停止并报告 MCP 工具未注入",
		"不要绕过 MCP",
		"插件 skill 路径解析",
		"~/.claude/plugins/cache/anbanai/anbanwriter",
		"claudecode/skills/line-art-coloring/SKILL.md",
		"prepare_workspace 返回的 path 可能是相对路径",
		"如果为空，调用 `list_channels`",
	}
	for _, term := range required {
		if !strings.Contains(body, term) {
			t.Fatalf("designer agent missing required term %q", term)
		}
	}
}

func TestClaudeCodePluginDocsKeepDesignerMCPContract(t *testing.T) {
	root := repoRoot(t)
	claudePath := filepath.Join(root, "claudecode", "CLAUDE.md")

	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read claudecode CLAUDE.md: %v", err)
	}
	body := string(data)

	required := []string{
		"Do not add a `tools` allowlist to agents that need MCP tools",
		"Claude Code treats `tools` as an allowlist",
		"`mcp__anbanwriter__...` tools",
		"single-candidate by default, optional 2-candidate",
		"needs_img2img",
		"not a guaranteed line-preserving colorize tool",
		"Omit `tools` unless you intentionally want to restrict",
	}
	for _, term := range required {
		if !strings.Contains(body, term) {
			t.Fatalf("claudecode CLAUDE.md missing required term %q", term)
		}
	}

	banned := []string{
		"frontmatter block with `name`, `tools`, `skills`",
		"Progressive coloring (2-candidate)",
		"frontmatter (name, tools, skills",
	}
	for _, term := range banned {
		if strings.Contains(body, term) {
			t.Fatalf("claudecode CLAUDE.md still contains stale term %q", term)
		}
	}
}

func TestLineArtColoringSkillDocumentsRuntimeLimits(t *testing.T) {
	root := repoRoot(t)
	skillPath := filepath.Join(root, "claudecode", "skills", "line-art-coloring", "SKILL.md")

	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read line-art-coloring skill: %v", err)
	}
	body := string(data)

	required := []string{
		"当前能力边界",
		"不是专用的 `colorize_lineart`",
		"不是严格的 img2img/ControlNet 上色",
		"尽力保持线稿",
		"不能承诺 100% 保留",
		"TIF",
		"sips -s format png",
		"magick",
		"Read 返回的 CDN URL 约 30 分钟过期",
		"file_path` 方式分析有 10MB 限制",
		"compress_image",
		"upload_image",
		"output_path` 是 MCP 服务器端路径",
		"/tmp/anbanwriter-line-art",
		"prepare_workspace 返回的 path 可能是相对路径",
		"size` 是宽高比提示",
		"从原始线稿推断最接近的支持比例",
		"Prompt 控制在 500 词以内",
		"504 Gateway Timeout",
		"单候选模式",
		"专用 img2img/colorize_lineart 工具可用前",
		"收敛修正和回溯统一",
		"标记为 `needs_img2img`",
		"更简单直接的颜色指令",
	}
	for _, term := range required {
		if !strings.Contains(body, term) {
			t.Fatalf("line-art-coloring skill missing required term %q", term)
		}
	}
}

func TestLineArtColoringVerificationReferenceDocumentsBestEffortLimits(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "claudecode", "skills", "line-art-coloring", "references", "verification.md")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read verification reference: %v", err)
	}
	body := string(data)

	required := []string{
		"不是专用 img2img/colorize_lineart 工具",
		"线稿保持风险",
		"标记 `needs_img2img`",
		"10MB 限制失败",
		"先 `compress_image`",
		"`upload_image` 后用 `image_url`",
		"默认生成 1 个候选",
		"质量优先模式生成 2 个候选",
		"prompt 约束，不是能力承诺",
		"线稿风险升高",
	}
	for _, term := range required {
		if !strings.Contains(body, term) {
			t.Fatalf("verification reference missing required term %q", term)
		}
	}
}

func TestDesignerMCPToolDescriptionsDocumentPathAndSizeSemantics(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "server", "mcp", "image_tools.go"),
		filepath.Join(root, "server", "mcp", "workspace_tools.go"),
	}

	var body strings.Builder
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		body.Write(data)
		body.WriteByte('\n')
	}

	required := []string{
		"not a guaranteed line-art-only colorize tool",
		"server-local path",
		"Use a writable server path such as /tmp/",
		"not the agent client's current working directory",
		"Image aspect ratio hint",
		"providers may still return a different crop/ratio",
		"Use file_path returned by generate_image/download_image",
		"file_path analysis is limited to 10MB",
		"compress_image first or upload_image and retry with image_url",
		"relative path rooted at the agent task workspace/current working directory",
	}
	all := body.String()
	for _, term := range required {
		if !strings.Contains(all, term) {
			t.Fatalf("MCP tool descriptions missing required term %q", term)
		}
	}
}

func frontmatterBlock(t *testing.T, body string) string {
	t.Helper()
	if !strings.HasPrefix(body, "---\n") {
		t.Fatal("markdown file missing frontmatter start")
	}
	rest := strings.TrimPrefix(body, "---\n")
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		t.Fatal("markdown file missing frontmatter end")
	}
	return rest[:idx]
}
