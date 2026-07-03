package agent

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

const NestedAgentDelegationError = "agent delegated to nested Agent tool and produced no required deliverables"

var seednoteContentImagePattern = regexp.MustCompile(`^image_\d+\.(png|jpe?g|webp)$`)

type ArtifactValidation struct {
	Valid               bool
	MeaningfulFileCount int
	Missing             []string
	Reason              string
}

func (v ArtifactValidation) Error() string {
	if v.Valid {
		return ""
	}
	if v.Reason != "" {
		return v.Reason
	}
	if len(v.Missing) > 0 {
		return "missing required deliverables: " + strings.Join(v.Missing, ", ")
	}
	return "no required deliverables found"
}

func ValidateTaskArtifactsFromWorkDir(task *model.Task, workDir string) ArtifactValidation {
	files, count := collectWorkDirArtifacts(workDir)
	return validateTaskArtifacts(task, files, count)
}

func ValidateTaskArtifactsFromTaskFiles(task *model.Task, taskFiles []*model.TaskFile) ArtifactValidation {
	files := make(map[string]bool, len(taskFiles)*2)
	meaningful := 0
	for _, file := range taskFiles {
		if file == nil {
			continue
		}
		addArtifactPath(files, file.FileName)
		addArtifactPath(files, file.FilePath)
		if isDeliverableArtifact(file.FileName) || isDeliverableArtifact(file.FilePath) {
			meaningful++
		}
	}
	return validateTaskArtifacts(task, files, meaningful)
}

func IsNestedAgentDelegationOnly(summary map[string]int) bool {
	if summary["Agent"] <= 0 {
		return false
	}
	for name, count := range summary {
		if count <= 0 || name == "Agent" || isNonSubstantiveTool(name) {
			continue
		}
		return false
	}
	return true
}

func isNonSubstantiveTool(name string) bool {
	switch name {
	case "TaskCreate", "TaskUpdate", "TodoWrite":
		return true
	default:
		return false
	}
}

func validateTaskArtifacts(task *model.Task, files map[string]bool, meaningful int) ArtifactValidation {
	result := ArtifactValidation{MeaningfulFileCount: meaningful}
	if task == nil || task.Type != model.PlatformSeednote {
		result.Valid = meaningful > 0
		if !result.Valid {
			result.Reason = "no meaningful output files"
		}
		return result
	}

	var missing []string
	for _, name := range []string{"content.md", "image-plan.md", "cover.png"} {
		if !files[name] {
			missing = append(missing, name)
		}
	}
	if task.HasContentImage && !hasSeednoteContentImage(files) {
		missing = append(missing, "image_01.png")
	}
	if task.HasTailImage && !files["tail.png"] {
		missing = append(missing, "tail.png")
	}
	if len(missing) > 0 {
		result.Missing = missing
		result.Reason = "seednote missing required deliverables: " + strings.Join(missing, ", ")
		return result
	}
	result.Valid = true
	return result
}

func hasSeednoteContentImage(files map[string]bool) bool {
	for name := range files {
		if seednoteContentImagePattern.MatchString(name) {
			return true
		}
	}
	return false
}

func collectWorkDirArtifacts(workDir string) (map[string]bool, int) {
	files := make(map[string]bool)
	if workDir == "" {
		return files, 0
	}
	scanDir := workDir
	if info, err := os.Stat(filepath.Join(workDir, "output")); err == nil && info.IsDir() {
		scanDir = filepath.Join(workDir, "output")
	}

	meaningful := 0
	_ = filepath.WalkDir(scanDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".anban-creator", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(scanDir, path)
		if relErr != nil {
			rel = path
		}
		addArtifactPath(files, rel)
		if isDeliverableArtifact(rel) {
			meaningful++
		}
		return nil
	})
	return files, meaningful
}

func addArtifactPath(files map[string]bool, path string) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	if path == "" {
		return
	}
	base := strings.ToLower(filepath.Base(path))
	if base != "" {
		files[base] = true
	}
	files[strings.ToLower(path)] = true
}

func isDeliverableArtifact(path string) bool {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(path)))
	if base == "" || strings.HasPrefix(base, ".") {
		return false
	}
	switch base {
	case "claude.md", ".task-context", "settings.json", ".mcp.json":
		return false
	default:
		return true
	}
}
