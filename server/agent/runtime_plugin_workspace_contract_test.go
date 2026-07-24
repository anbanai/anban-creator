package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPluginAssetsDoNotControlManagedWorkspaceDirectories(t *testing.T) {
	root := filepath.Join(repoRoot(t), "plugins")
	forbidden := managedWorkspaceForbiddenPatterns()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == filepath.Join(root, "skills", "humanizer") {
				return filepath.SkipDir
			}
			return nil
		}
		if !ownedPluginWorkflowFile(root, path) {
			return nil
		}

		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relativePath, relErr := filepath.Rel(repoRoot(t), path)
		if relErr != nil {
			return relErr
		}
		for _, pattern := range forbidden {
			if pattern.Match(body) {
				t.Errorf("%s contains forbidden managed-workspace pattern %s", filepath.ToSlash(relativePath), pattern)
			}
		}
		pluginPath := filepath.ToSlash(relativePath)
		bareFailureState := bareFailureStatePattern.Match(body)
		if strings.HasPrefix(pluginPath, "plugins/hooks/") {
			bareFailureState = containsBareFailureStateHookInstruction(body)
		}
		if bareFailureState {
			t.Errorf("%s contains a failure-state.json instruction outside canonical output/", filepath.ToSlash(relativePath))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, taskType := range []string{"article", "seednote", "moments", "ecommerce", "designer", "montage", "live-slicer"} {
		if containsString(managedRequiredMCPTools(taskType), "prepare_workspace") {
			t.Errorf("managed runtime policy for %s names forbidden prepare_workspace tool", taskType)
		}
	}
}

func managedWorkspaceForbiddenPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)(?:^|[^[:alnum:]_])(?:mcp__[[:alnum:]_-]+__)?prepare_workspace(?:$|[^[:alnum:]_])`),
		regexp.MustCompile(`\$(?:DIR\b|\{DIR\})`),
		regexp.MustCompile(`(?m)\bmkdir[ \t]+(?:-[[:alpha:]]*p[[:alpha:]]*|--parents)[ \t]+(?:["']?(?:(?:\./|/workspace/)?output(?:$|[/ \t"';&|])|\$(?:DIR\b|\{DIR\}))|[^\n]*[ \t"'=](?:(?:\./|/workspace/)?output(?:$|[/ \t"';&|])|\$(?:DIR\b|\{DIR\})))`),
		regexp.MustCompile(`\.task-context\b`),
		regexp.MustCompile(`(?i)(?:\bCWD\b[^\n]*(?:\bTASK_ID\b|任务[ \t]*ID)|(?:\bTASK_ID\b|任务[ \t]*ID)[^\n]*\bCWD\b)`),
	}
}

var bareFailureStatePattern = regexp.MustCompile(`(?m)(?:^|[^[:alnum:]_./-])failure-state\.json`)
var (
	bareFailureStateFilenamePattern      = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]_./-])(failure-state\.json)`)
	hookFailureStateEnglishActionPattern = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]_.-])(create|emit|persist|save|write)\b`)
	hookFailureStateObservationPattern   = regexp.MustCompile(`(?i)\b(?:check(?:ed|ing)?|document(?:ed|ing)?|inspect(?:ed|ing)?|mention(?:ed|ing)?|read(?:ing)?|reference(?:d|ing)?|whether)\b`)
)

var hookFailureStateChineseActions = []string{"持久化", "写入", "创建", "生成", "保存", "写"}

var hookFailureStateChineseObservations = []string{"检查", "查看", "读取", "提及", "说明", "文档", "是否"}

func containsBareFailureStateHookInstruction(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		for _, match := range bareFailureStateFilenamePattern.FindAllStringSubmatchIndex(line, -1) {
			filenameStart := match[2]
			actionStart, actionEnd := nearestHookFailureStateAction(line[:filenameStart])
			if actionStart < 0 {
				continue
			}
			if isHookFailureStateTargetPhrase(line[actionEnd:filenameStart]) {
				return true
			}
		}
	}
	return false
}

func nearestHookFailureStateAction(prefix string) (int, int) {
	nearestStart, nearestEnd := -1, -1
	for _, match := range hookFailureStateEnglishActionPattern.FindAllStringSubmatchIndex(prefix, -1) {
		if match[2] > nearestStart || match[2] == nearestStart && match[3] > nearestEnd {
			nearestStart, nearestEnd = match[2], match[3]
		}
	}
	for _, action := range hookFailureStateChineseActions {
		start := strings.LastIndex(prefix, action)
		if start < 0 || precededByIdentifierPunctuation(prefix, start) {
			continue
		}
		end := start + len(action)
		if start > nearestStart || start == nearestStart && end > nearestEnd {
			nearestStart, nearestEnd = start, end
		}
	}
	return nearestStart, nearestEnd
}

func precededByIdentifierPunctuation(text string, index int) bool {
	if index == 0 {
		return false
	}
	previous := text[index-1]
	return previous == '.' || previous == '_' || previous >= '0' && previous <= '9' ||
		previous >= 'A' && previous <= 'Z' || previous >= 'a' && previous <= 'z'
}

func isHookFailureStateTargetPhrase(phrase string) bool {
	if len([]rune(phrase)) > 80 || strings.ContainsAny(phrase, "\n;；.!?。！？") {
		return false
	}
	if hookFailureStateObservationPattern.MatchString(phrase) {
		return false
	}
	for _, observation := range hookFailureStateChineseObservations {
		if strings.Contains(phrase, observation) {
			return false
		}
	}
	return true
}

func TestManagedWorkspaceForbiddenPatterns(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "bare workspace tool", body: "call prepare_workspace now", want: true},
		{name: "namespaced workspace tool", body: "call mcp__anban__prepare_workspace now", want: true},
		{name: "plain directory variable", body: `write $DIR/foo`, want: true},
		{name: "braced directory variable", body: `write ${DIR}/foo`, want: true},
		{name: "mkdir short option output", body: `mkdir -p output`, want: true},
		{name: "mkdir long option output", body: `mkdir --parents output`, want: true},
		{name: "mkdir relative managed output", body: `mkdir -p ./output`, want: true},
		{name: "mkdir absolute managed output", body: `mkdir --parents /workspace/output`, want: true},
		{name: "mkdir directory variable", body: `mkdir -p "$DIR/exports"`, want: true},
		{name: "cwd before task ID", body: "Use CWD directory name as TASK_ID", want: true},
		{name: "task ID before cwd", body: "TASK_ID comes from CWD", want: true},
		{name: "Chinese task ID before cwd", body: "任务 ID 来自 cwd", want: true},
		{name: "task context file", body: "read .task-context", want: true},
		{name: "unrelated directory variable", body: `write $CACHE_DIR/foo`, want: false},
		{name: "ordinary cache mkdir", body: `mkdir -p "$CACHE_DIR"`, want: false},
		{name: "nested cache output mkdir", body: `mkdir -p "$CACHE_DIR/output"`, want: false},
		{name: "ordinary temp mkdir", body: `mkdir --parents /tmp/render-cache`, want: false},
		{name: "identifier containing DIR", body: `mkdir -p "$SOME_DIR_CACHE"`, want: false},
		{name: "prose identifiers containing DIR", body: "DIRECTORY and CACHE_DIR are identifiers", want: false},
		{name: "tool identifier containing name", body: "prepare_workspace_backup", want: false},
		{name: "cwd and task ID on separate lines", body: "CWD is the task root\nTASK_ID is injected", want: false},
	}

	patterns := managedWorkspaceForbiddenPatterns()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := false
			for _, pattern := range patterns {
				if pattern.MatchString(tt.body) {
					got = true
					break
				}
			}
			if got != tt.want {
				t.Fatalf("managed workspace prohibition match for %q = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestBareFailureStatePattern(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "bare", body: "write failure-state.json", want: true},
		{name: "quoted bare", body: "write `failure-state.json`", want: true},
		{name: "canonical", body: "write output/failure-state.json", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bareFailureStatePattern.MatchString(tt.body); got != tt.want {
				t.Fatalf("bare failure-state match for %q = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestBareFailureStateHookInstructionPattern(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "bare write instruction", body: "only failures write structured failure-state.json", want: true},
		{name: "bare Chinese write instruction", body: "只有失败才写结构化 failure-state.json", want: true},
		{name: "bare create instruction", body: "On failure, create `failure-state.json` with the recovery payload.", want: true},
		{name: "bare emit instruction", body: "Emit failure-state.json when the task can be resumed.", want: true},
		{name: "bare persist instruction", body: "Persist the structured failure to failure-state.json.", want: true},
		{name: "bare save instruction", body: "Save recovery details in `failure-state.json`.", want: true},
		{name: "bare Chinese save instruction", body: "失败时将结构化状态保存到 `failure-state.json`。", want: true},
		{name: "bare Chinese generate instruction", body: "失败时生成 failure-state.json 并停止。", want: true},
		{name: "canonical write instruction", body: "write output/failure-state.json"},
		{name: "canonical save instruction", body: "save recovery details in output/failure-state.json"},
		{name: "canonical path construction", body: `failure_path = root / "output" / "failure-state.json"`},
		{name: "canonical path construction and write", body: "failure_path = root / \"output\" / \"failure-state.json\"\nfailure_path.write_text(payload)"},
		{name: "observation prose", body: "The write policy is documented; failure-state.json is only the historical basename."},
		{name: "logging code", body: `logger.write("failure-state.json missing")`},
		{name: "unrelated write and check sequence", body: "Write the report before checking whether failure-state.json exists."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsBareFailureStateHookInstruction([]byte(tt.body)); got != tt.want {
				t.Fatalf("bare hook failure-state instruction match for %q = %v, want %v", tt.body, got, tt.want)
			}
		})
	}
}

func TestParentPluginContractsDoNotRequireLegacyWorkspaceInstructions(t *testing.T) {
	tests := []struct {
		path      string
		forbidden []string
	}{
		{path: "server/agent/moments_contract_test.go", forbidden: []string{`prepare_workspace(content_type="moments"`, `"$DIR"`}},
		{path: "server/agent/designer_contract_test.go", forbidden: []string{"`prepare_workspace`", "prepare_workspace 返回的 path", "workspace_tools.go", "relative path rooted at the agent task workspace", "不能把 `download_image` 当作写入 `$DIR/colored_NN.png`", "下载 `download_url` 到 `$DIR/colored_NN.png`"}},
		{path: "server/agent/seednote_context_contract_test.go", forbidden: []string{"$DIR/topic-analysis.md", "$DIR/source-analysis.md", "$DIR/viral-template.json", "$DIR/content.md", "$DIR/image-plan.md", "$DIR/image-review.md"}},
		{path: "server/agent/article_skill_contract_test.go", forbidden: []string{"$DIR/03-article.md", "$DIR/01-research.md", "$DIR/02-outline.md", "$DIR/seo-result.md"}},
		{path: "server/agent/claude_plugin_best_practices_test.go", forbidden: []string{"$DIR/draft.json"}},
		{path: "server/mcp/seednote_hook_test.go", forbidden: []string{"$DIR/failure-state.json", "$DIR/content.md", "$DIR/viral-template.json", "$DIR/template-meta.json", "成果目录（`$DIR`）"}},
		{path: "server/mcp/live_slice_skill_test.go", forbidden: []string{"$DIR/metadata.json", "$DIR/audio.mp3", "$DIR/cover.jpg", `mkdir -p "$(dirname "$OUT")"`}},
	}

	root := repoRoot(t)
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			body := readRepoFile(t, filepath.Join(root, filepath.FromSlash(tt.path)))
			for _, forbidden := range tt.forbidden {
				if strings.Contains(body, forbidden) {
					t.Errorf("%s still requires legacy plugin workspace instruction %q", tt.path, forbidden)
				}
			}
		})
	}
}

func ownedPluginWorkflowFile(root, path string) bool {
	relativePath, err := filepath.Rel(root, path)
	if err != nil || relativePath == "." || strings.HasPrefix(relativePath, "..") {
		return false
	}
	relativePath = filepath.ToSlash(relativePath)
	extension := filepath.Ext(relativePath)
	switch {
	case relativePath == "CODEX.md", relativePath == "README.md":
		return true
	case strings.HasPrefix(relativePath, "agents/"):
		return extension == ".md" || extension == ".toml"
	case strings.HasPrefix(relativePath, "skills/"):
		return extension == ".md"
	case strings.HasPrefix(relativePath, "hooks/"):
		return extension == ".json" || extension == ".sh"
	case relativePath == "docs/plugin-development.md":
		return true
	default:
		return false
	}
}
