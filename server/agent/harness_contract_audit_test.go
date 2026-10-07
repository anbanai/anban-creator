package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHarnessImageToolExamplesCarryTaskIdentity(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"harness/skills/article-visual-design/SKILL.md",
		"harness/skills/article-cover-design/SKILL.md",
		"harness/skills/wechat-picture-visual-design/SKILL.md",
		"harness/skills/ecommerce-visual-design/SKILL.md",
		"harness/skills/ecommerce-product-analysis/SKILL.md",
		"harness/skills/seednote-visual-design/SKILL.md",
		"harness/skills/short-video-cover/SKILL.md",
		"harness/skills/portrait-pose-variants/SKILL.md",
		"harness/skills/moments/SKILL.md",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readArticleContractFile(t, filepath.Join(root, rel))
			for _, tool := range []string{"get_project_profile(", "generate_image(", "analyze_image("} {
				remaining := body
				for {
					at := strings.Index(remaining, tool)
					if at < 0 {
						break
					}
					call := remaining[at:]
					end := strings.Index(call, ")")
					if end < 0 {
						t.Fatalf("unterminated %s", tool)
					}
					call = call[:end]
					if !strings.Contains(call, "project_id") || !strings.Contains(call, "task_id") {
						t.Fatalf("%s missing project_id/task_id: %s", tool, call)
					}
					remaining = remaining[at+len(tool):]
				}
			}
		})
	}
}

func TestHarnessAutonomyAndFailureContracts(t *testing.T) {
	root := articleContractRepoRoot(t)
	checks := map[string][]string{
		"harness/packs/wechat-article/agent.claude.md":  {"readiness.status", "blocked", "output/draft.json"},
		"harness/packs/wechat-article/agent.codex.toml": {"readiness.status", "blocked", "output/draft.json"},
		"harness/packs/wechat-picture/agent.claude.md":  {"output/publish-package.json", "schema_version", "readiness.status", "\"content\"", "image_paths"},
		"harness/packs/wechat-picture/agent.codex.toml": {"output/publish-package.json", "schema_version", "readiness.status", "\"content\"", "image_paths"},
		"harness/packs/moments/agent.codex.toml":        {"不得询问", "结构化失败"},
		"harness/packs/seednote/agent.claude.md":        {"viral_analysis", "quality_status=failed", "generate_image"},
		"harness/packs/seednote/agent.codex.toml":       {"viral_analysis", "quality_status=failed", "generate_image"},
		"harness/packs/montage/agent.codex.toml":        {"checkpoint", "decision", "output/failure-diagnosis.md", "output/failure-state.json", "recoverable_failure", "invalid_video_aspect_ratio"},
		"harness/packs/montage/agent.claude.md":         {"checkpoint", "decision", "output/failure-diagnosis.md", "output/failure-state.json", "recoverable_failure", "invalid_video_aspect_ratio"},
	}
	for rel, terms := range checks {
		t.Run(rel, func(t *testing.T) {
			body := readArticleContractFile(t, filepath.Join(root, rel))
			for _, term := range terms {
				if !strings.Contains(body, term) {
					t.Fatalf("missing contract term %q", term)
				}
			}
		})
	}

	moments := readArticleContractFile(t, filepath.Join(root, "harness/packs/moments/agent.codex.toml"))
	if strings.Contains(moments, "无法判断时向用户列出候选") {
		t.Fatal("Moments Codex agent must not ask the user to choose a project")
	}
}

func TestVisualProductionScopesMatchGetProjectProfileSchema(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, relRoot := range []string{
		"harness/skills",
		"harness/agents",
		"harness/packs",
		"harness/dsh/presets",
	} {
		err := filepath.WalkDir(filepath.Join(root, relRoot), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(body), `scope="article"`) {
				t.Fatalf("%s uses unsupported get_project_profile scope=article; use scope=wechat", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", relRoot, err)
		}
	}
}

func TestVisualPromptBlueprintsBindSourceFactsAndReviewLayout(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, rel := range []string{
		"harness/skills/seednote-visual-design/references/prompt-blueprint.md",
		"harness/skills/article-visual-design/references/prompt-blueprint.md",
		"harness/skills/wechat-picture-visual-design/references/prompt-blueprint.md",
	} {
		body := readArticleContractFile(t, filepath.Join(root, rel))
		for _, term := range []string{"must_match_excerpts", "事实依据原句", "不得渲染为图片文字"} {
			if !strings.Contains(body, term) {
				t.Fatalf("%s missing source-fact prompt contract %q", rel, term)
			}
		}
	}
	article := readArticleContractFile(t, filepath.Join(root, "harness/skills/article-visual-design/references/generation-contract.md"))
	for _, term := range []string{"safe_zone_ok", "layout_and_reading_order_ok"} {
		if !strings.Contains(article, term) {
			t.Fatalf("article generation contract missing review field %q", term)
		}
	}
}

func TestVisualReviewUnavailableFailsClosedAtDelivery(t *testing.T) {
	root := articleContractRepoRoot(t)
	seednotePaths := []string{
		"harness/skills/seednote-visual-design/SKILL.md",
		"harness/skills/seednote-visual-design/references/reference-contract.md",
		"harness/packs/seednote/agent.claude.md",
		"harness/packs/seednote/agent.codex.toml",
		"harness/packs/seednote/agent.dsh.yml",
	}
	for _, rel := range seednotePaths {
		body := readArticleContractFile(t, filepath.Join(root, rel))
		for _, term := range []string{"quality_status=unavailable", "image_review_unavailable", "继续生成剩余计划图片", "不得报告成功"} {
			if !strings.Contains(body, term) {
				t.Fatalf("%s missing fail-closed review contract %q", rel, term)
			}
		}
	}
	picturePaths := []string{
		"harness/skills/wechat-picture-visual-design/SKILL.md",
		"harness/packs/wechat-picture/agent.claude.md",
		"harness/packs/wechat-picture/agent.codex.toml",
	}
	for _, rel := range picturePaths {
		body := readArticleContractFile(t, filepath.Join(root, rel))
		for _, term := range []string{"quality_status=unavailable", "status` 与 `readiness.status` 必须均为 `blocked`", "output/failure-state.json", "只有 `ready`"} {
			if !strings.Contains(body, term) {
				t.Fatalf("%s missing blocked picture review contract %q", rel, term)
			}
		}
	}
}

func TestArticleImageUploadIsServerOwnedAndRevalidated(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, rel := range []string{
		"harness/skills/article-visual-design/SKILL.md",
		"harness/skills/article-visual-design/references/generation-contract.md",
		"harness/agents/wechat-article.md",
		"harness/packs/wechat-article/agent.claude.md",
		"harness/packs/wechat-article/agent.codex.toml",
		"harness/packs/wechat-article/agent.dsh.yml",
	} {
		body := readArticleContractFile(t, filepath.Join(root, rel))
		for _, term := range []string{"Server-owned", "MCP", "TaskFile", "finalizer", "重新校验"} {
			if !strings.Contains(body, term) {
				t.Fatalf("%s missing Server-owned image upload boundary %q", rel, term)
			}
		}
	}
}
