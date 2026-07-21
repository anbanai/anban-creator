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
		"plugins/anban/agents/seednote.md",
		"plugins/anban/agents/ecommerce.md",
		"plugins/anban/agents/moments.md",
		"plugins/anban/agents/wechatarticle.md",
		"plugins/anban/skills/ecommerce/SKILL.md",
		"plugins/anban/skills/article/SKILL.md",
		"plugins/anban/skills/seednote-visual-design/SKILL.md",
		"plugins/anban/skills/ecommerce-visual-design/SKILL.md",
		"plugins/anban/skills/ecommerce-platform-specs/SKILL.md",
		"plugins/anban/hooks/hooks.json",
		"plugins/anban/README.md",
		"plugins/anban/docs/plugin-development.md",
		"plugins/anban/agents/seednote.toml",
		"plugins/anban/agents/ecommerce.toml",
		"plugins/anban/agents/moments.toml",
		"plugins/anban/agents/wechatarticle.toml",
		"plugins/anban/skills/ecommerce/SKILL.md",
		"plugins/anban/skills/article/SKILL.md",
		"plugins/anban/skills/seednote-visual-design/SKILL.md",
		"plugins/anban/skills/ecommerce-visual-design/SKILL.md",
		"plugins/anban/skills/ecommerce-platform-specs/SKILL.md",
		"plugins/anban/hooks/hooks.json",
		"plugins/anban/CODEX.md",
		"plugins/anban/install/agents-registration.toml",
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

	workflowPaths := []string{
		"plugins/anban/agents/seednote.md",
		"plugins/anban/agents/ecommerce.md",
		"plugins/anban/agents/moments.md",
		"plugins/anban/agents/wechatarticle.md",
		"plugins/anban/skills/ecommerce/SKILL.md",
		"plugins/anban/skills/article/SKILL.md",
		"plugins/anban/agents/seednote.toml",
		"plugins/anban/agents/ecommerce.toml",
		"plugins/anban/agents/moments.toml",
		"plugins/anban/agents/wechatarticle.toml",
		"plugins/anban/skills/ecommerce/SKILL.md",
		"plugins/anban/skills/article/SKILL.md",
	}
	for _, relativePath := range workflowPaths {
		t.Run(relativePath+"/canonical-output", func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			for _, term := range []string{
				"prepare_workspace",
				"$TASK_ID",
				"$DIR",
				"task_files",
				"execution_id",
				"OSS",
			} {
				if !strings.Contains(body, term) {
					t.Errorf("%s missing canonical server task output term %q", relativePath, term)
				}
			}
			assertNoDeliverableRelocationCommands(t, relativePath, body)
		})
	}

	for _, relativePath := range []string{
		"plugins/anban/agents/seednote.md",
		"plugins/anban/agents/ecommerce.md",
		"plugins/anban/skills/ecommerce/SKILL.md",
		"plugins/anban/agents/seednote.toml",
		"plugins/anban/agents/ecommerce.toml",
		"plugins/anban/skills/ecommerce/SKILL.md",
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
		"plugins/anban/agents/ecommerce.md",
		"plugins/anban/skills/ecommerce/SKILL.md",
		"plugins/anban/agents/ecommerce.toml",
		"plugins/anban/skills/ecommerce/SKILL.md",
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
	for _, relativePath := range []string{"plugins/anban/CODEX.md", "plugins/anban/docs/codex-installation.md"} {
		t.Run(relativePath, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(relativePath)))
			seednote := lineContaining(t, body, "| `seednote` |")
			ecommerce := lineContaining(t, body, "| `ecommerce` |")
			for _, pipeline := range []string{seednote, ecommerce} {
				if regexp.MustCompile(`(?i)\bArchive\b`).MatchString(pipeline) {
					t.Errorf("%s contains legacy Archive pipeline step: %s", relativePath, pipeline)
				}
				for _, required := range []string{"$DIR", "delivery validation"} {
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

func TestDeliverableRelocationCommandDetection(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "move deliverable set to archive", body: `mv "$DIR"/* "$ARCHIVE_DIR"/`, want: true},
		{name: "copy result directory to archive", body: `cp -R "$DIR" "$ARCHIVE_DIR"`, want: true},
		{name: "rsync result directory to archive", body: `rsync -a "$DIR/" "$ARCHIVE_DIR/"`, want: true},
		{name: "move result to title suffix", body: `mv "$DIR" "$DIR-$TITLE"`, want: true},
		{name: "move result to archive suffix", body: `mv "$DIR" "$DIR_ARCHIVE"`, want: true},
		{name: "move braced result to suffix", body: `mv "${DIR}" "${DIR}-archive"`, want: true},
		{name: "copy download into result file", body: `cp "$DOWNLOAD" "$DIR/cover.png"`, want: false},
		{name: "move temporary file within result", body: `mv "$DIR/.tmp" "$DIR/final.json"`, want: false},
		{name: "normal non result command", body: `mv "$DOWNLOAD.tmp" "$DOWNLOAD"`, want: false},
		{name: "prose policy", body: "不得移动、复制或按标题重命名成果目录", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findDeliverableRelocationCommand(tt.body) != ""
			if got != tt.want {
				t.Fatalf("findDeliverableRelocationCommand(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
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

func assertNoDeliverableRelocationCommands(t *testing.T, path, body string) {
	t.Helper()
	if match := findDeliverableRelocationCommand(body); match != "" {
		t.Errorf("%s contains deliverable relocation command %q", path, match)
	}
}

func findDeliverableRelocationCommand(body string) string {
	commands := regexp.MustCompile("(?m)(?:^|[\\x60;&|]\\s*)((?:mv|cp|rsync)\\s+[^\\n\\x60]+)").FindAllStringSubmatch(body, -1)
	fields := regexp.MustCompile(`(?:"[^"]*"|'[^']*'|[^\s"']+)+`)
	for _, match := range commands {
		command := match[1]
		tokens := fields.FindAllString(command, -1)
		if len(tokens) < 3 {
			continue
		}
		args := make([]string, 0, len(tokens)-1)
		for _, token := range tokens[1:] {
			if strings.HasPrefix(token, "-") {
				continue
			}
			args = append(args, token)
		}
		if len(args) < 2 {
			continue
		}
		destination := normalizeShellPath(args[len(args)-1])
		if isUnderDIR(destination) {
			continue
		}
		for _, source := range args[:len(args)-1] {
			if isUnderDIR(normalizeShellPath(source)) {
				return command
			}
		}
	}
	return ""
}

func normalizeShellPath(path string) string {
	return strings.Trim(strings.ReplaceAll(path, `"`, ""), "' ,.;")
}

func isUnderDIR(path string) bool {
	return path == "$DIR" || strings.HasPrefix(path, "$DIR/") ||
		path == "${DIR}" || strings.HasPrefix(path, "${DIR}/")
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
		"plugins/anban/scripts/archive-seednote-workspace.sh",
		"plugins/anban/scripts/archive-seednote-workspace.sh",
	} {
		path := filepath.Join(root, filepath.FromSlash(relativePath))
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("legacy archive implementation %s still exists or could not be checked: %v", relativePath, err)
		}
	}
}
