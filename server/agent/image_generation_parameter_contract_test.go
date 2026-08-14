package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func generatingSkillContractPaths(t *testing.T, root string) []string {
	t.Helper()
	skillRoot := filepath.Join(root, "plugins", "skills")
	var paths []string
	err := filepath.WalkDir(skillRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != "SKILL.md" {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "generate_image(") {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(paths)
	return paths
}

func TestGeneratingAgentsUseBusinessAspectRatios(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"plugins/agents/article.md",
		"plugins/agents/article.toml",
		"plugins/agents/seednote.md",
		"plugins/agents/seednote.toml",
		"plugins/agents/moments.md",
		"plugins/agents/moments.toml",
		"plugins/agents/ecommerce.md",
		"plugins/agents/ecommerce.toml",
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, want := range []string{
				"resolved_profile.image_ratio",
				"resolved_profile.allowed_image_ratios",
				"用户明确比例",
				"智能适配",
				"显式传 `aspect_ratio`",
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
	paths := generatingSkillContractPaths(t, root)
	if len(paths) != 8 {
		t.Fatalf("generating Skill count = %d (%v), want 8", len(paths), paths)
	}
	for _, rel := range paths {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, want := range []string{
				"resolved_profile.image_ratio",
				"resolved_profile.allowed_image_ratios",
				"用户明确比例",
				"智能适配",
				"显式传 `aspect_ratio`",
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
				"resolved_profile.supported_sizes",
				"image_capability_ratio_unsupported",
				"$EFFECTIVE_IMAGE_SIZE",
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

func TestEveryDocumentedGenerateImageCallPassesAspectRatio(t *testing.T) {
	root := articleContractRepoRoot(t)
	paths := []string{
		"plugins/agents/article.md",
		"plugins/agents/article.toml",
		"plugins/agents/seednote.md",
		"plugins/agents/seednote.toml",
		"plugins/agents/moments.md",
		"plugins/agents/moments.toml",
		"plugins/agents/ecommerce.md",
		"plugins/agents/ecommerce.toml",
	}
	paths = append(paths, generatingSkillContractPaths(t, root)...)
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
				if !strings.Contains(invocation, "aspect_ratio") {
					t.Fatalf("%s generate_image call %d does not pass aspect_ratio: %s", rel, call, invocation)
				}
				for _, stale := range []string{"size=", "size =", "supported_sizes", "$EFFECTIVE_IMAGE_SIZE", ":2K", ":4K"} {
					if strings.Contains(invocation, stale) {
						t.Fatalf("%s generate_image call %d contains fixed or mixed size %q: %s", rel, call, stale, invocation)
					}
				}
				remaining = remaining[end+1:]
			}
		})
	}
}

func TestGeneratingSkillProseDoesNotDescribeTaskMCPSizeParameter(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, rel := range generatingSkillContractPaths(t, root) {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, stale := range []string{
				"MCP `size`",
				"`size` 是宽高比",
				"重选 `size`",
			} {
				if strings.Contains(body, stale) {
					t.Fatalf("%s still describes task MCP parameter as size via %q", rel, stale)
				}
			}
		})
	}
}

func TestEcommercePlatformGuidanceDoesNotPassFixedSizePresetsToTaskGeneration(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, rel := range []string{
		"plugins/skills/ecommerce-platform-specs/SKILL.md",
		"plugins/skills/ecommerce-platform-specs/references/platforms.md",
	} {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, stale := range []string{"ratio:tier", "1:1:2K", "3:4:2K", "16:9:2K"} {
				if strings.Contains(body, stale) {
					t.Fatalf("%s still mixes fixed size presets into task guidance via %q", rel, stale)
				}
			}
		})
	}
}

func TestMomentsGeneratesSemanticRatioImageArtifacts(t *testing.T) {
	root := articleContractRepoRoot(t)
	for _, rel := range []string{
		"plugins/agents/moments.md",
		"plugins/agents/moments.toml",
		"plugins/skills/moments/SKILL.md",
	} {
		t.Run(rel, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, rel))
			for _, want := range []string{
				"output/image-prompts.md",
				"output/moments-image.png",
				"image_capability_key",
				"generate_image(",
				"aspect_ratio=$EFFECTIVE_ASPECT_RATIO",
			} {
				if !strings.Contains(body, want) {
					t.Fatalf("%s missing Moments image workflow term %q", rel, want)
				}
			}
		})
	}
}

func TestImageSkillGuidanceDoesNotOverrideEffectiveRatioOrRestoreLegacyRoutes(t *testing.T) {
	root := articleContractRepoRoot(t)
	tests := []struct {
		path  string
		stale []string
	}{
		{path: "plugins/skills/article-cover-design/SKILL.md", stale: []string{"宽银幕叙事构图", "A cinematic 2.35:1 wide banner", "the 2.35:1 hero"}},
		{path: "plugins/skills/article-visual-design/references/cover.md", stale: []string{"硬编码 900×383 / 2.35:1", "image_size=full-bleed`, `2.35:1", "A 2.35:1 horizontal image"}},
		{path: "plugins/skills/ecommerce-visual-design/SKILL.md", stale: []string{"1:1:2K", "3:4:2K", "16:9:2K", "默认 cover 用更高质量"}},
		{path: "plugins/skills/portrait-pose-variants/SKILL.md", stale: []string{"size=\"9:16\""}},
		{path: "plugins/skills/short-video-cover/SKILL.md", stale: []string{"size=\"9:16\"", "size` 参数固定传 `\"9:16\"`"}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			body := readImageGenerationContractFile(t, filepath.Join(root, tt.path))
			for _, stale := range tt.stale {
				if strings.Contains(body, stale) {
					t.Errorf("%s still contains stale image guidance %q", tt.path, stale)
				}
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
