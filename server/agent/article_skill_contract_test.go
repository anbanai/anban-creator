package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArticleSkillContracts_ImageControlsSizesAndTextPolicy(t *testing.T) {
	root := articleContractRepoRoot(t)
	cases := []struct {
		name     string
		path     string
		required []string
	}{
		{
			name: "claudecode article visual skill",
			path: filepath.Join(root, "claudecode", "skills", "article-visual-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
				`size="4:3"`,
				`size="1:1"`,
				"受控文字策略",
				"article_image_mode",
				"cover_only",
				"text_only",
				"不依赖项目级/任务级 image ratio",
			},
		},
		{
			name: "openclaw article visual skill",
			path: filepath.Join(root, "openclaw", "skills", "article-visual-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
				`size="4:3"`,
				`size="1:1"`,
				"受控文字策略",
				"article_image_mode",
				"cover_only",
				"text_only",
				"不依赖项目级/任务级 image ratio",
			},
		},
		{
			name: "codex article visual skill",
			path: filepath.Join(root, "codex", "skills", "article-visual-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
				`size="4:3"`,
				`size="1:1"`,
				"受控文字策略",
				"article_image_mode",
				"cover_only",
				"text_only",
				"不依赖项目级/任务级 image ratio",
			},
		},
		{
			name: "claudecode cover skill",
			path: filepath.Join(root, "claudecode", "skills", "article-cover-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
				"受控文字策略",
				"article_image_mode",
				"content_only",
			},
		},
		{
			name: "openclaw cover skill",
			path: filepath.Join(root, "openclaw", "skills", "article-cover-design", "SKILL.md"),
			required: []string{
				`size="21:9"`,
				"受控文字策略",
				"article_image_mode",
				"content_only",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}
			text := string(data)
			for _, term := range tc.required {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing required article image contract term %q", tc.path, term)
				}
			}
		})
	}
}

func TestArticleSkillContracts_NoUnconditionalImageRequirements(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "skills", "article", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "article", "SKILL.md"),
		filepath.Join(root, "codex", "skills", "article", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"图片运行控制前置",
				"article_image_mode",
				"仅在对应图片模式开启该产物时",
				"纯文字文章",
				`size="4:3"`,
				`size="1:1"`,
				"render_template",
				"正文配图开启时",
				"封面开关开启时",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing conditional image requirement term %q", path, term)
				}
			}
			for _, stale := range []string{
				"任一硬性项失败时停止发布：内容不贴题、缺少封面 `media_id`、章节缺图",
				"图文并茂**：每个 `##` 章节至少一张配图",
				"图片生成要求",
				"所有配图使用 `ref_image_path=\"$DIR/cover.png\"` 保持风格一致",
				"将文件内容作为 `markdown` 参数传给 `convert_markdown",
				"`$DIR/images.json` 仅作为审计记录，`convert_markdown` 不会读取该文件",
				"| 6 | `article-visual-design` | `cover.png`, `image-plan.md` |",
				"| 7 | `article-visual-design` | `images.json`, 更新 `04-article-final.md` |",
				"| 8 | `content-writing` | `05-article.html` |",
			} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s still contains stale unconditional image requirement %q", path, stale)
				}
			}
		})
	}
}

func TestArticleSkillContracts_ContentOnlyDoesNotRequireCoverReference(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "agents", "wechatarticle.md"),
		filepath.Join(root, "claudecode", "skills", "article", "SKILL.md"),
		filepath.Join(root, "claudecode", "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "article", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "codex", "skills", "article", "SKILL.md"),
		filepath.Join(root, "codex", "skills", "article-visual-design", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"封面关·配图开",
				"不传",
				"链到首张已生成图",
				"不存在的 `$DIR/cover.png`",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing content-only ref_image_path guard term %q", path, term)
				}
			}
		})
	}
}

func TestContentWritingSkillContracts_RenderTemplateMainPath(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		filepath.Join(root, "claudecode", "skills", "content-writing", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "content-writing", "SKILL.md"),
		filepath.Join(root, "codex", "skills", "content-writing", "SKILL.md"),
	}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(data)
			for _, term := range []string{
				"render_template",
				"inspect_article",
				"article_templates",
				"主路径",
				"convert_markdown` 只用于旧版 server 兼容降级",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing content-writing render contract term %q", path, term)
				}
			}
			for _, stale := range []string{
				"Markdown 转微信 HTML：调用 `convert_markdown` MCP 工具",
				"排版模块使用标准 Markdown 语法，由 `convert_markdown` 工具中的 LLM 自动渲染",
				"保存为 `$DIR/05-article.html`。",
			} {
				if strings.Contains(text, stale) {
					t.Fatalf("%s still contains stale content-writing render path %q", path, stale)
				}
			}
		})
	}
}

func articleContractRepoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
