package agent

import (
	"os"
	"path/filepath"
	"regexp"
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
