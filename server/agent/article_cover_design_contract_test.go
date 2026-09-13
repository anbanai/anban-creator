package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArticleCoverDesignUsesDirectedPlanAndScopedReferences(t *testing.T) {
	root := repoRoot(t)
	skillRoot := filepath.Join(root, "harness", "skills", "article-cover-design")
	main := readRepoFile(t, filepath.Join(skillRoot, "SKILL.md"))

	for _, reference := range []string{
		"references/art-direction.md",
		"references/portrait-reference.md",
		"references/quality-gate.md",
		"references/cover-effectiveness.md",
		"references/examples.md",
	} {
		if !strings.Contains(main, reference) {
			t.Fatalf("article-cover-design SKILL.md missing progressive-disclosure reference %q", reference)
		}
		if _, err := os.Stat(filepath.Join(skillRoot, filepath.FromSlash(reference))); err != nil {
			t.Fatalf("article-cover-design reference %q is unavailable: %v", reference, err)
		}
	}

	for _, want := range []string{
		"output/cover-plan.md",
		"output/cover-prompt.md",
		"output/cover-quality.json",
		"$EFFECTIVE_ASPECT_RATIO",
		"generate_image(",
		"analyze_image(",
		"upload_image(",
		"ref_image_paths=$COVER_REFERENCE_PATHS",
		"supports_reference",
		"max_reference_images",
	} {
		if !strings.Contains(main, want) {
			t.Fatalf("article-cover-design SKILL.md missing workflow contract %q", want)
		}
	}

	for _, forbidden := range []string{
		"A cinematic WeChat article cover",
		"Photographic quality",
		"摄影级/绘画级；禁 3D 合成/卡通",
	} {
		if strings.Contains(main, forbidden) {
			t.Fatalf("article-cover-design SKILL.md retains medium-biased prompt language %q", forbidden)
		}
	}
}

func TestArticleCoverDesignDefinesEightPartMediumAdaptiveArtDirection(t *testing.T) {
	root := repoRoot(t)
	body := readRepoFile(t, filepath.Join(root, "harness", "skills", "article-cover-design", "references", "art-direction.md"))

	for _, want := range []string{
		"比例与发布派生",
		"标题策略",
		"人物或主体",
		"背景与场景",
		"色彩与光线",
		"媒介与质感",
		"视觉层级与动线",
		"禁止事项",
		"摄影",
		"编辑设计",
		"插画",
		"水墨",
		"不得固定追加",
		"cinematic",
		"Photographic quality",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("art-direction reference missing %q", want)
		}
	}
}

func TestArticleCoverDesignDefinesPortraitReferenceContract(t *testing.T) {
	root := repoRoot(t)
	body := readRepoFile(t, filepath.Join(root, "harness", "skills", "article-cover-design", "references", "portrait-reference.md"))

	for _, want := range []string{
		"默认关闭",
		".anban-creator/task-reference.png",
		".anban-creator/project-style-reference.png",
		"只用于封面",
		"不得传给正文配图",
		"required_entity",
		"ref_image_paths",
		"人物图在前",
		"身份一致性",
		"supports_reference=false",
		"max_reference_images",
		"不得静默忽略",
		"不能保证真人身份完全一致",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("portrait-reference contract missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"project-style-reference.png` 传入 `generate_image`",
		"人物参考图作为正文图",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("portrait-reference contract contains forbidden scope %q", forbidden)
		}
	}
}

func TestArticleAgentWiresPortraitReferenceOnlyIntoCoverStep(t *testing.T) {
	root := repoRoot(t)
	for _, relative := range []string{
		"harness/packs/article/agent.claude.md",
		"harness/packs/article/agent.codex.toml",
		"harness/packs/article/agent.dsh.yml",
	} {
		body := readRepoFile(t, filepath.Join(root, relative))
		for _, want := range []string{
			"task_reference_path",
			"project_style_reference_path",
			"人物参考默认关闭",
			"output/cover-plan.md",
			"ref_image_paths=$COVER_REFERENCE_PATHS",
			"正文配图不得使用人物参考图",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing article portrait-cover wiring %q", relative, want)
			}
		}
		for _, stale := range []string{
			"2.35:1",
			"`visual_quality_scorecard` + `cover_effectiveness_scorecard` 写入 `cover-prompt.md`",
		} {
			if strings.Contains(body, stale) {
				t.Fatalf("%s retains stale article cover contract %q", relative, stale)
			}
		}
	}
}

func TestArticleTemplatesDeferCoverRatioToResolvedTaskContract(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{
		"listicle.yaml",
		"long-form-essay.yaml",
		"story-narrative.yaml",
		"tutorial.yaml",
	} {
		relative := filepath.Join("harness", "templates", "article", name)
		body := readRepoFile(t, filepath.Join(root, relative))
		if !strings.Contains(body, "$EFFECTIVE_ASPECT_RATIO") {
			t.Fatalf("%s must defer the hero ratio to $EFFECTIVE_ASPECT_RATIO", relative)
		}
		if strings.Contains(body, "2.35:1") {
			t.Fatalf("%s must not override the task cover ratio with 2.35:1", relative)
		}
		if strings.Contains(body, "16:9") {
			t.Fatalf("%s must not override content-image ratios with 16:9", relative)
		}
	}
}

func TestArticleContentImagesDoNotReferencePortraitBearingCover(t *testing.T) {
	root := repoRoot(t)
	relative := filepath.Join("harness", "skills", "article-visual-design", "references", "content.md")
	body := readRepoFile(t, filepath.Join(root, relative))

	for _, want := range []string{
		"$CONTENT_STYLE_REFERENCE_PATH",
		"人物参考启用",
		"正文配图不传 `ref_image_path`",
		"文本风格块",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("%s missing portrait-isolation contract %q", relative, want)
		}
	}
	for _, forbidden := range []string{
		`ref_image_path="output/cover.png",`,
		"`ref_image_path` 必须等于 `output/cover.png`",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("%s retains unconditional cover reference %q", relative, forbidden)
		}
	}
}
