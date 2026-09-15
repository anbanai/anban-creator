package mcp

import (
	"os"
	"strings"
	"testing"
)

func TestArticlePackHasOnlyCoreDeliveryRequirements(t *testing.T) {
	raw, err := os.ReadFile("../../harness/packs/article/agent-pack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pack := string(raw)
	for _, required := range []string{
		"path: output/04-article-final.md, mime_type: text/markdown, required: true",
		"path: output/05-article.html, mime_type: text/html, required: true",
	} {
		if !strings.Contains(pack, required) {
			t.Errorf("article pack missing core requirement %q", required)
		}
	}
	if strings.Contains(pack, "path: output/final-review.md, mime_type: text/markdown, required: true") {
		t.Fatal("final review must be optional")
	}
	if got := strings.Count(pack, "required: true"); got != 2 {
		t.Fatalf("required artifacts = %d, want exactly 2", got)
	}
}

func TestArticleAgentUsesOnlyThreeFormalProgressTasks(t *testing.T) {
	for _, path := range []string{
		"../../harness/packs/article/agent.claude.md",
		"../../harness/agents/article.md",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		agent := string(raw)
		if strings.Contains(agent, "十步细粒度") || strings.Contains(agent, "十步业务任务可以另建") {
			t.Fatalf("%s still asks for fine-grained TaskCreate calls", path)
		}
		if !strings.Contains(agent, "只创建 research、writing、delivery 三个正式 Task") {
			t.Fatalf("%s does not declare the three-task contract", path)
		}
	}
}

func TestArticleAgentKeepsDraftPublicationIndependent(t *testing.T) {
	for _, path := range []string{
		"../../harness/packs/article/agent.claude.md",
		"../../harness/agents/article.md",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		agent := string(raw)
		if strings.Contains(agent, "`create_draft` 成功、最终 feedback") ||
			strings.Contains(agent, "草稿创建成功，可通过公众号后台查看") ||
			strings.Contains(agent, "create_draft` 实际调用失败立即写") {
			t.Fatalf("%s still makes draft publication part of core delivery success", path)
		}
		for _, required := range []string{"output/draft-result.json", "草稿创建失败不影响 Markdown/HTML 交付", "marketing-scan.json.status=block_publish"} {
			if !strings.Contains(agent, required) {
				t.Fatalf("%s missing independent publication rule %q", path, required)
			}
		}
	}
}

func TestArticleDraftRetryIsBoundedByStructuredFailure(t *testing.T) {
	for _, path := range []string{
		"../../harness/packs/article/agent.claude.md",
		"../../harness/packs/article/agent.codex.toml",
		"../../harness/packs/article/agent.dsh.yml",
		"../../harness/skills/article-publishing/SKILL.md",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		contract := string(raw)
		for _, required := range []string{
			"`retryable=true`",
			"完全相同",
			"最多重试一次",
			"`retryable=false`",
			"结果不明确",
			"不得重试",
		} {
			if !strings.Contains(contract, required) {
				t.Errorf("%s missing bounded create_draft retry rule %q", path, required)
			}
		}
	}
}

func TestArticleAgentKeepsVisualFailuresIndependent(t *testing.T) {
	for _, path := range []string{
		"../../harness/packs/article/agent.claude.md",
		"../../harness/agents/article.md",
		"../../harness/skills/article/SKILL.md",
		"../../harness/skills/article-visual-design/SKILL.md",
		"../../harness/skills/article-cover-design/SKILL.md",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		for _, forbidden := range []string{
			"article_image_upload_failed\",\"message\":\"图片上传在限定重试后仍失败\",\"resume_from\":\"image_generation\"}` 并结束",
			"article_cover_generation_failed\",\"message\":\"封面生成在限定重试后仍失败\",\"resume_from\":\"image_generation\"}`，结束",
			"article_content_images_failed\",\"message\":\"超过一半章节配图在限定重试后仍失败\",\"resume_from\":\"image_generation\"}`。",
		} {
			if strings.Contains(content, forbidden) {
				t.Fatalf("%s still terminates core delivery for a visual failure", path)
			}
		}
		if !strings.Contains(content, "视觉失败不得阻止核心 Markdown 与 HTML 继续生成") {
			t.Fatalf("%s does not declare visual failure independence", path)
		}
	}
}

func TestArticleImageModeHasNoCompatibilityDefault(t *testing.T) {
	for _, path := range []string{
		"../../harness/packs/article/agent.claude.md",
		"../../harness/skills/article/SKILL.md",
		"../../harness/skills/article-publishing/SKILL.md",
		"../../harness/skills/article-visual-design/SKILL.md",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		content := string(raw)
		if strings.Contains(content, "缺失时按 `cover_and_content`") ||
			strings.Contains(content, "缺失时也按此处理") ||
			strings.Contains(content, "以兼容旧任务") {
			t.Fatalf("%s still defaults a missing article_image_mode", path)
		}
		if !strings.Contains(content, "article_image_mode_missing") {
			t.Fatalf("%s does not fail explicitly when article_image_mode is missing", path)
		}
	}
}

func TestContentWritingDoesNotExposeGeneralBlacklistToModel(t *testing.T) {
	raw, err := os.ReadFile("../../harness/skills/content-writing/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)
	if strings.Contains(skill, "prohibited-words.md") {
		t.Fatal("content-writing still directs the model to read the prohibited-word list")
	}
	if !strings.Contains(skill, "scan-article-marketing.mjs") {
		t.Fatal("content-writing does not invoke the deterministic marketing scanner")
	}
}
