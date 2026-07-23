package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestShippedWorkflowsUseCanonicalServerTaskOutput(t *testing.T) {
	root := repoRoot(t)
	paths := []string{
		"server/config.yaml",
		"server/config.example.yaml",
		"plugins/agents/seednote.md",
		"plugins/agents/ecommerce.md",
		"plugins/agents/moments.md",
		"plugins/agents/article.md",
		"plugins/skills/ecommerce/SKILL.md",
		"plugins/skills/article/SKILL.md",
		"plugins/skills/seednote-visual-design/SKILL.md",
		"plugins/skills/ecommerce-visual-design/SKILL.md",
		"plugins/skills/ecommerce-platform-specs/SKILL.md",
		"plugins/hooks/hooks.json",
		"plugins/README.md",
		"plugins/docs/plugin-development.md",
		"plugins/agents/seednote.toml",
		"plugins/agents/ecommerce.toml",
		"plugins/agents/moments.toml",
		"plugins/agents/article.toml",
		"plugins/skills/ecommerce/SKILL.md",
		"plugins/skills/article/SKILL.md",
		"plugins/skills/seednote-visual-design/SKILL.md",
		"plugins/skills/ecommerce-visual-design/SKILL.md",
		"plugins/skills/ecommerce-platform-specs/SKILL.md",
		"plugins/hooks/hooks.json",
		"plugins/CODEX.md",
		"plugins/install/agents-registration.toml",
	}
	forbidden := []string{
		"archive_workspace",
		"ARCHIVE_DIR",
		"archive-seednote-workspace.sh",
		"output/seednote/{标题}",
		"output/ecommerce/{产品名}",
		"归档全链路",
		"归档前",
		"归档目录",
		"成功归档",
		"归档整理",
		"归档与",
		"与归档",
		"归档交付",
		"→ 归档",
	}

	for _, relativePath := range paths {
		t.Run(relativePath, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			for _, term := range forbidden {
				if strings.Contains(body, term) {
					t.Errorf("%s contains legacy workspace term %q", relativePath, term)
				}
			}
		})
	}

	workflowOutputs := []struct {
		path    string
		outputs []string
	}{
		{path: "plugins/agents/article.md", outputs: []string{"output/04-article-final.md", "output/05-article.html", "output/final-review.md"}},
		{path: "plugins/agents/article.toml", outputs: []string{"output/04-article-final.md", "output/05-article.html", "output/final-review.md"}},
		{path: "plugins/agents/seednote.md", outputs: []string{"output/content.md", "output/image-plan.md", "output/failure-state.json"}},
		{path: "plugins/agents/seednote.toml", outputs: []string{"output/content.md", "output/image-plan.md", "output/failure-state.json"}},
		{path: "plugins/agents/moments.md", outputs: []string{"output/material-analysis.md", "output/content.md", "output/quality-review.md"}},
		{path: "plugins/agents/moments.toml", outputs: []string{"output/material-analysis.md", "output/content.md", "output/quality-review.md"}},
		{path: "plugins/agents/ecommerce.md", outputs: []string{"output/product-bible.md", "output/copywriting.md", "output/manifest.json"}},
		{path: "plugins/agents/ecommerce.toml", outputs: []string{"output/product-bible.md", "output/copywriting.md", "output/manifest.json"}},
		{path: "plugins/agents/designer.md", outputs: []string{"output/color-bible.md", "output/colored_00.png", "output/consistency-report.md"}},
		{path: "plugins/agents/designer.toml", outputs: []string{"output/color-bible.md", "output/colored_00.png", "output/consistency-report.md"}},
		{path: "plugins/agents/montage.md", outputs: []string{"output/montage-project.json", "output/delivery-manifest.json", "output/final.mp4"}},
		{path: "plugins/agents/montage.toml", outputs: []string{"output/montage-project.json", "output/delivery-manifest.json", "output/final.mp4"}},
		{path: "plugins/agents/live-slicer.md", outputs: []string{"output/summary.md", "output/clip-manifest.json", "output/clip-plan.json"}},
		{path: "plugins/agents/live-slicer.toml", outputs: []string{"output/summary.md", "output/clip-manifest.json", "output/clip-plan.json"}},
	}
	for _, workflow := range workflowOutputs {
		relativePath := workflow.path
		t.Run(relativePath+"/explicit-output", func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			for _, requiredOutput := range workflow.outputs {
				if !strings.Contains(body, requiredOutput) {
					t.Errorf("%s missing explicit managed-runtime artifact path %q", relativePath, requiredOutput)
				}
			}
		})
	}

	for _, relativePath := range []string{
		"plugins/agents/seednote.md",
		"plugins/agents/ecommerce.md",
		"plugins/skills/ecommerce/SKILL.md",
		"plugins/agents/seednote.toml",
		"plugins/agents/ecommerce.toml",
		"plugins/skills/ecommerce/SKILL.md",
	} {
		t.Run(relativePath+"/mode-aware-progress", func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			if regexp.MustCompile(`\[[0-9]+/[0-9]+\]`).MatchString(body) {
				t.Errorf("%s contains a mode-inaccurate fixed progress denominator", relativePath)
			}
		})
	}
}

func TestNativeAgentPairsDeclareSameExplicitOutputPaths(t *testing.T) {
	root := repoRoot(t)
	for _, agentName := range []string{
		"article",
		"designer",
		"ecommerce",
		"live-slicer",
		"moments",
		"montage",
		"seednote",
	} {
		t.Run(agentName, func(t *testing.T) {
			markdownPath := "plugins/agents/" + agentName + ".md"
			tomlPath := "plugins/agents/" + agentName + ".toml"
			markdownOutputs := explicitOutputPaths(readRepoFile(t, filepath.Join(root, filepath.FromSlash(markdownPath))))
			tomlOutputs := explicitOutputPaths(readRepoFile(t, filepath.Join(root, filepath.FromSlash(tomlPath))))
			if strings.Join(markdownOutputs, "\n") != strings.Join(tomlOutputs, "\n") {
				t.Errorf("native agent output path mismatch:\n%s: %v\n%s: %v", markdownPath, markdownOutputs, tomlPath, tomlOutputs)
			}

			if agentName != "seednote" {
				return
			}
			for _, required := range []string{
				"output/cover.png",
				"output/image_01.png",
				"output/image_02.png",
				"output/image_03.png",
				"output/tail.png",
			} {
				if !containsOutputPath(markdownOutputs, required) {
					t.Errorf("%s missing mode-dependent Seednote image path %q", markdownPath, required)
				}
				if !containsOutputPath(tomlOutputs, required) {
					t.Errorf("%s missing mode-dependent Seednote image path %q", tomlPath, required)
				}
			}
		})
	}
}

func TestSeednoteNativeAgentsDeclareModeAwareImageSemantics(t *testing.T) {
	root := repoRoot(t)
	for _, relativePath := range []string{
		"plugins/agents/seednote.md",
		"plugins/agents/seednote.toml",
	} {
		t.Run(relativePath, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			for _, required := range []string{
				"cover_only 和 cover_tail 模式的内容图数量必须为 0",
				"cover_content 和 full 模式的内容图数量必须为 1~3",
				"内容图文件必须从 output/image_01.png 开始连续编号",
				"只允许使用 output/image_01.png、output/image_02.png、output/image_03.png",
				"不得跳号或使用其他 image_*.png 文件名",
			} {
				if !strings.Contains(body, required) {
					t.Errorf("%s missing Seednote image-mode semantic %q", relativePath, required)
				}
			}
			if strings.Contains(body, "封面 1 + 内容图 1~3 + 尾图 0~1") {
				t.Errorf("%s makes 1~3 content images unconditional across zero-content modes", relativePath)
			}
		})
	}
}

func explicitOutputPaths(body string) []string {
	matches := regexp.MustCompile(`\boutput/[A-Za-z0-9._/-]*[A-Za-z0-9_-]\.[A-Za-z0-9]+\b`).FindAllString(body, -1)
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		seen[match] = struct{}{}
	}
	outputs := make([]string, 0, len(seen))
	for output := range seen {
		outputs = append(outputs, output)
	}
	sort.Strings(outputs)
	return outputs
}

func containsOutputPath(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestEcommerceWorkflowsResolveServerProductPhotoDirectory(t *testing.T) {
	root := repoRoot(t)
	for _, relativePath := range []string{
		"plugins/agents/ecommerce.md",
		"plugins/skills/ecommerce/SKILL.md",
		"plugins/agents/ecommerce.toml",
		"plugins/skills/ecommerce/SKILL.md",
	} {
		t.Run(relativePath, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			if strings.Contains(body, "$DIR/.anban-creator/products") {
				t.Fatalf("%s incorrectly resolves product photos under $DIR", relativePath)
			}
			const definition = "将 `ecommerce.product_photo_dir` 读取为 `$PRODUCT_PHOTO_DIR`"
			assertVariableDefinedBeforeUse(t, relativePath, body, "$PRODUCT_PHOTO_DIR", definition)
			definitionAt := strings.Index(body, definition)
			indexCheckAt := indexAfterText(body, "`index.json`", definitionAt)
			if indexCheckAt <= definitionAt {
				t.Fatalf("%s must assign $PRODUCT_PHOTO_DIR before checking index.json", relativePath)
			}
			for _, term := range []string{"相对路径以当前任务 CWD 为根", "$PRODUCT_PHOTO_DIR/<filename>", "缺失或全无可访问"} {
				if !strings.Contains(body[definitionAt:indexCheckAt+len("`index.json`")], term) && !strings.Contains(body[indexCheckAt:], term) {
					t.Errorf("%s missing product photo directory contract %q", relativePath, term)
				}
			}
		})
	}
}

func TestEcommerceProductAnalysisPreservesBootstrapInputDirectory(t *testing.T) {
	root := repoRoot(t)
	relativePath := "plugins/skills/ecommerce-product-analysis/SKILL.md"
	body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))

	for _, forbidden := range []string{
		"output/.anban-creator/products/index.json",
		"output/.anban-creator/products/product_01.png",
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s incorrectly relocates bootstrap input under output: %q", relativePath, forbidden)
		}
	}
	for _, required := range []string{
		"`$PRODUCT_PHOTO_DIR/index.json`",
		"`$PRODUCT_PHOTO_DIR/product_01.png`",
		"`output/product-photos.md`",
		"`output/product-bible.md`",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("%s missing product photo path contract %q", relativePath, required)
		}
	}
}

func TestCodexOverviewDocsUseCanonicalTaskDelivery(t *testing.T) {
	root := repoRoot(t)
	for _, relativePath := range []string{"plugins/CODEX.md", "plugins/docs/codex-installation.md"} {
		t.Run(relativePath, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			seednote := lineContaining(t, body, "| `seednote` |")
			ecommerce := lineContaining(t, body, "| `ecommerce` |")
			for _, pipeline := range []string{seednote, ecommerce} {
				if regexp.MustCompile(`(?i)\bArchive\b`).MatchString(pipeline) {
					t.Errorf("%s contains legacy Archive pipeline step: %s", relativePath, pipeline)
				}
				for _, required := range []string{"output/", "delivery validation"} {
					if !strings.Contains(strings.ToLower(pipeline), strings.ToLower(required)) {
						t.Errorf("%s pipeline missing canonical delivery term %q: %s", relativePath, required, pipeline)
					}
				}
			}
			for _, required := range []string{"image-plan", "runtime mode"} {
				if !strings.Contains(strings.ToLower(seednote), required) {
					t.Errorf("%s Seednote pipeline missing %q: %s", relativePath, required, seednote)
				}
			}
			if strings.Contains(seednote, "3-8 content images") {
				t.Errorf("%s Seednote pipeline contains obsolete fixed image count", relativePath)
			}
		})
	}
}

func lineContaining(t *testing.T, body, needle string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("missing line containing %q", needle)
	return ""
}

func assertVariableDefinedBeforeUse(t *testing.T, path, body, variable, definition string) {
	t.Helper()
	definitionAt := strings.Index(body, definition)
	if definitionAt < 0 {
		t.Fatalf("%s missing variable definition %q", path, definition)
	}
	if strings.Contains(body[:definitionAt], variable) {
		t.Fatalf("%s uses %s before its definition", path, variable)
	}
}

func indexAfterText(body, needle string, after int) int {
	if after < 0 || after >= len(body) {
		return -1
	}
	index := strings.Index(body[after+1:], needle)
	if index < 0 {
		return -1
	}
	return after + 1 + index
}

func TestArchiveWorkspaceImplementationIsAbsent(t *testing.T) {
	root := repoRoot(t)
	for _, relativePath := range []string{
		"plugins/scripts/archive-seednote-workspace.sh",
		"plugins/scripts/archive-seednote-workspace.sh",
	} {
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("legacy archive implementation %s still exists or could not be checked: %v", relativePath, err)
		}
	}
}
