package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesignerAgentKeepsMCPAndSkillContract(t *testing.T) {
	root := repoRoot(t)
	agentPath := filepath.Join(root, "plugins", "agents", "designer.md")

	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("read designer agent: %v", err)
	}
	body := string(data)

	frontmatter := frontmatterBlock(t, body)
	if strings.Contains(frontmatter, "\ntools:") {
		t.Fatal("designer agent must not define a tools allowlist; omitting tools lets Claude Code inherit MCP tools")
	}
	if strings.Contains(frontmatter, "\nmcpServers:") {
		t.Fatal("designer agent must not define mcpServers; plugin subagent frontmatter ignores that field, so MCP is provided by plugin-level .mcp.json")
	}

	required := []string{
		"  - line-art-coloring",
		"插件级 `.mcp.json`",
		"Claude Code subagent 的 `tools:` 字段是 allowlist",
		"不要在本 agent frontmatter 中声明 `tools:`",
		"`generate_image`",
		"`analyze_image`",
		"`download_image`",
		"`prepare_workspace`",
		"`update_task_progress`",
		"ANBAN_API_URL",
		"ANBAN_DEFAULT_PROJECT",
		"ANBAN_API_KEY",
		"无法看到 `generate_image` 等 MCP 能力",
		"停止并报告 MCP 工具未注入",
		"不要绕过 MCP",
		"prepare_workspace 返回的 path 可能是相对路径",
		"如果为空，调用 `list_projects`",
	}
	if !strings.Contains(frontmatter, "\nskills:") {
		t.Fatal("designer agent must preload line-art-coloring")
	}
	for _, term := range required {
		if !strings.Contains(body, term) {
			t.Fatalf("designer agent missing required term %q", term)
		}
	}
	for _, forbidden := range []string{"`Skill` 工具", "anban:line-art-coloring", "不要在 Agent frontmatter 预加载"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("designer agent contains obsolete Skill loading instruction %q", forbidden)
		}
	}
}

func TestClaudeCodePluginDocsKeepDesignerMCPContract(t *testing.T) {
	root := repoRoot(t)
	claudePath := filepath.Join(root, "plugins", "docs", "plugin-development.md")

	data, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("read claudecode plugin development docs: %v", err)
	}
	body := string(data)

	required := []string{
		"Do not add a `tools` allowlist to agents that need MCP tools",
		"Claude Code treats `tools` as an allowlist",
		"hide inherited MCP tools from subagents",
		"Do not add `mcpServers` to plugin agent frontmatter",
		"Plugin subagents receive MCP servers from the plugin-level `.mcp.json`",
		"single-candidate by default, optional 2-candidate",
		"needs_img2img",
		"not a guaranteed line-preserving colorize tool",
		"Omit `tools` unless you intentionally want to restrict",
	}
	for _, term := range required {
		if !strings.Contains(body, term) {
			t.Fatalf("claudecode plugin development docs missing required term %q", term)
		}
	}

	banned := []string{
		"frontmatter block with `name`, `tools`, `skills`",
		"frontmatter block with `name`, `skills`, `mcpServers`",
		"Progressive coloring (2-candidate)",
		"frontmatter (name, tools, skills",
	}
	for _, term := range banned {
		if strings.Contains(body, term) {
			t.Fatalf("claudecode plugin development docs still contains stale term %q", term)
		}
	}
}

func TestLineArtColoringSkillDocumentsRuntimeLimits(t *testing.T) {
	root := repoRoot(t)
	skillPath := filepath.Join(root, "plugins", "skills", "line-art-coloring", "SKILL.md")

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
		"/tmp/anban-creator-line-art",
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
	path := filepath.Join(root, "plugins", "skills", "line-art-coloring", "references", "verification.md")

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

func TestLineArtColoringDocsMatchAnalyzeImageSingleImageSemantics(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		filepath.Join(root, "plugins", "agents", "designer.md"),
		filepath.Join(root, "plugins", "skills", "line-art-coloring", "SKILL.md"),
		filepath.Join(root, "plugins", "skills", "line-art-coloring", "references", "verification.md"),
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
	all := body.String()

	required := []string{
		"analyze_image 一次只分析一张图片",
		"先为原始线稿生成线稿指纹",
		"将上色图审计结果与线稿指纹逐项比对",
		"同时传 `file_path` 和 `image_url` 时服务端只会使用 `file_path`",
		"不能把 `download_image` 当作写入 `$DIR/colored_NN.png` 的本地归档步骤",
		"下载 `download_url` 到 `$DIR/colored_NN.png`",
	}
	for _, term := range required {
		if !strings.Contains(all, term) {
			t.Fatalf("line-art-coloring docs missing single-image analyze semantics term %q", term)
		}
	}

	banned := []string{
		"file_path=上色图服务器端路径, image_url=原始线稿CDN_URL",
		"用 `download_image` 或返回的 `download_url` 下载到 `$DIR/colored_NN.png`",
	}
	for _, term := range banned {
		if strings.Contains(all, term) {
			t.Fatalf("line-art-coloring docs still contain non-executable term %q", term)
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
		"Generate one durable task image",
		"settles the operation atomically",
		"Task-relative output path",
		"Requested aspect ratio",
		"Optional ordered reference image paths",
		"relative file_path values are resolved against that task workspace",
		"image_base64 for agent/client-local bytes",
		"file_path for an absolute server-local file",
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
