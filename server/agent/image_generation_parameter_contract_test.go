package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratingAgentsUseEffectiveImageRatioAndCapabilitySizes(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"plugins/agents/article.md",
		"plugins/agents/article.toml",
		"plugins/agents/seednote.md",
		"plugins/agents/seednote.toml",
		"plugins/agents/ecommerce.md",
		"plugins/agents/ecommerce.toml",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, want := range []string{
				"resolved_profile.image_ratio",
				"resolved_profile.supported_sizes",
				"用户明确比例",
				"智能适配",
				"显式传 `size`",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing image parameter contract %q", rel, want)
				}
			}
		})
	}
}

func TestGeneratingSkillsDoNotOverrideImageRatioOrRelyOnImplicitCrop(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"plugins/skills/article/SKILL.md",
		"plugins/skills/article-visual-design/SKILL.md",
		"plugins/skills/article-visual-design/references/content.md",
		"plugins/skills/article-cover-design/SKILL.md",
		"plugins/skills/seednote-visual-design/SKILL.md",
		"plugins/skills/ecommerce-visual-design/SKILL.md",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, want := range []string{
				"resolved_profile.image_ratio",
				"resolved_profile.supported_sizes",
				"用户明确比例",
				"智能适配",
				"显式传 `size`",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing image parameter contract %q", rel, want)
				}
			}
			for _, stale := range []string{
				"服务端强制中心裁剪",
				"服务端强制精确裁剪",
				"服务端强制裁剪",
				"不依赖项目级/任务级 image ratio",
				"size=<按 slot 固定",
				`size="4:3"`,
			} {
				if strings.Contains(body, stale) {
					t.Fatalf("%s still contains stale image parameter rule %q", rel, stale)
				}
			}
		})
	}

	cover := readImageGenerationContractFile(t, filepath.Join(root, "plugins/skills/article-cover-design/SKILL.md"))
	for _, want := range []string{"crop_image(", "目标宽高", "锚点"} {
		if !strings.Contains(cover, want) {
			t.Fatalf("article cover skill missing explicit crop contract %q", want)
		}
	}
}

func TestEveryDocumentedGenerateImageCallPassesSize(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"plugins/agents/article.md",
		"plugins/agents/article.toml",
		"plugins/agents/seednote.md",
		"plugins/agents/seednote.toml",
		"plugins/agents/ecommerce.md",
		"plugins/agents/ecommerce.toml",
		"plugins/skills/article-visual-design/SKILL.md",
		"plugins/skills/article-cover-design/SKILL.md",
		"plugins/skills/seednote-visual-design/SKILL.md",
		"plugins/skills/ecommerce-visual-design/SKILL.md",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			remaining := body
			for call := 1; ; call++ {
				start := strings.Index(remaining, "generate_image(")
				if start < 0 {
					break
				}
				remaining = remaining[start:]
				end := strings.Index(remaining, ")")
				if end < 0 {
					t.Fatalf("%s generate_image call %d is not closed", rel, call)
				}
				invocation := remaining[:end+1]
				if !strings.Contains(invocation, "size") {
					t.Fatalf("%s generate_image call %d does not pass size: %s", rel, call, invocation)
				}
				remaining = remaining[end+1:]
			}
		})
	}
}

func TestArticleExactCoverIsCroppedBeforeTheUploadedCoverIsSelected(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"plugins/agents/article.md",
		"plugins/agents/article.toml",
		"plugins/packs/article/agent.claude.md",
		"plugins/packs/article/agent.codex.toml",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			cropAt := strings.Index(body, "crop_image")
			uploadAt := strings.Index(body, "file_path=$COVER_PATH")
			if cropAt < 0 || uploadAt < 0 || cropAt > uploadAt {
				t.Fatalf("%s must select any explicit crop before uploading $COVER_PATH", rel)
			}
			if strings.Contains(body, `upload_image(project_id=$PROJECT_ID, task_id=$TASK_ID, file_path="output/cover.png")`) {
				t.Fatalf("%s uploads the uncropped cover before selecting $COVER_PATH", rel)
			}
		})
	}
}

func TestImageGuidanceDoesNotDescribeImplicitPlatformCropping(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"plugins/skills/article/SKILL.md",
		"plugins/skills/article-visual-design/references/cover.md",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, stale := range []string{
				"硬编码官方比例",
				"服务端按 `platform=article + image_type=cover` 精确中心裁剪",
			} {
				if strings.Contains(body, stale) {
					t.Fatalf("%s still describes removed implicit image behavior %q", rel, stale)
				}
			}
		})
	}
}

func readImageGenerationContractFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}
