package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGuizangSocialCardSkillMirrorsAndContracts(t *testing.T) {
	root := repoRoot(t)
	claudeExamples := readRepoFile(t, filepath.Join(root, "claudecode", "skills", "guizang-social-card", "references", "examples.md"))
	for _, plugin := range []string{"claudecode", "openclaw", "codex"} {
		t.Run(plugin, func(t *testing.T) {
			skillPath := filepath.Join(root, plugin, "skills", "guizang-social-card", "SKILL.md")
			body := readRepoFile(t, skillPath)
			for _, want := range []string{
				"guizang-social-card",
				"references/examples.md",
				"图片比例固定规则",
				"Seednote/XLS/移动信息流默认 `3:4`",
				"微信公众号",
				"图文笔记",
				"1080x1440",
				"2100x900",
				"1080x1080",
				"21:9",
				"1:1",
				"Playwright",
				"HTML/CSS",
				"register_rendered_image",
				"wechat-21x9-cover.png",
				"wechat-1x1-cover.png",
				"wechat-cover-pair-preview.png",
				"cover.png",
				"image_01.png",
				"tail.png",
				"op7418/guizang-social-card-skill",
				"AGPL-3.0",
				"不直接复制",
				"不要绕过 MCP",
				"不要自写 HTTP 上传客户端",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Guizang contract term %q", skillPath, want)
				}
			}

			examplesPath := filepath.Join(root, plugin, "skills", "guizang-social-card", "references", "examples.md")
			examples := readRepoFile(t, examplesPath)
			if plugin != "claudecode" && examples != claudeExamples {
				t.Fatalf("%s must match claudecode Guizang examples", examplesPath)
			}
		})
	}
}

func TestGuizangSocialCardRoutingIsDeclaredForSeednoteAndWechat(t *testing.T) {
	root := repoRoot(t)
	files := []string{
		filepath.Join(root, "claudecode", "agents", "seednote.md"),
		filepath.Join(root, "codex", "agents", "seednote.toml"),
		filepath.Join(root, "claudecode", "agents", "wechatarticle.md"),
		filepath.Join(root, "codex", "agents", "wechatarticle.toml"),
	}
	for _, plugin := range []string{"claudecode", "openclaw", "codex"} {
		for _, skill := range []string{"seednote-visual-design", "article-visual-design", "article-cover-design"} {
			files = append(files, filepath.Join(root, plugin, "skills", skill, "SKILL.md"))
		}
	}

	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			body := readRepoFile(t, file)
			for _, want := range []string{
				"guizang-social-card",
				"归藏",
				"Guizang",
				"social card",
				"小红书组图",
				"Swiss",
				"editorial card",
				"visual_style",
				"register_rendered_image",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Guizang routing term %q", file, want)
				}
			}
		})
	}
}

func TestGuizangSocialCardSkillDoesNotVendorUpstreamAGPLAssets(t *testing.T) {
	root := repoRoot(t)
	for _, plugin := range []string{"claudecode", "openclaw", "codex"} {
		dir := filepath.Join(root, plugin, "skills", "guizang-social-card")
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			base := filepath.Base(path)
			for _, forbidden := range []string{
				"validate-social-deck.mjs",
				"template-editorial-card.html",
				"template-swiss-card.html",
				"magazine-bg-webgl.js",
			} {
				if base == forbidden {
					t.Fatalf("%s vendors upstream AGPL asset %q", dir, forbidden)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
