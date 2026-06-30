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
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing conditional image requirement term %q", path, term)
				}
			}
			for _, stale := range []string{
				"任一硬性项失败时停止发布：内容不贴题、缺少封面 `media_id`、章节缺图",
				"图文并茂**：每个 `##` 章节至少一张配图",
				"图片生成要求",
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
		filepath.Join(root, "claudecode", "skills", "article-visual-design", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "article", "SKILL.md"),
		filepath.Join(root, "openclaw", "skills", "article-visual-design", "SKILL.md"),
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
				"链首图",
				"不存在的 `$DIR/cover.png`",
			} {
				if !strings.Contains(text, term) {
					t.Fatalf("%s missing content-only ref_image_path guard term %q", path, term)
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
