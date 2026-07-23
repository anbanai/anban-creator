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
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`\bprepare_workspace\b`),
		regexp.MustCompile(`\$DIR\b`),
		regexp.MustCompile(`mkdir\s+-p[^\n]*(output|DIR)`),
		regexp.MustCompile(`\.task-context`),
		regexp.MustCompile(`CWD[^\n]*(TASK_ID|任务 ID)`),
	}

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
