package handler

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

type runtimeArtifactSpec struct {
	Role     string `json:"role"`
	Path     string `json:"path"`
	MIMEType string `json:"mime_type"`
	Required bool   `json:"required"`
}

// buildRuntimeContext creates the deliberately small, user-facing projection
// of the task's immutable identity and latest execution state. Internal paths,
// hashes, provider configuration, and credentials never enter this response.
func buildRuntimeContext(task *model.Task, files []*model.TaskFile, project *model.Project) map[string]any {
	return buildRuntimeContextWithExecution(task, files, project, nil)
}

func buildRuntimeContextWithExecution(task *model.Task, files []*model.TaskFile, project *model.Project, currentExecution *model.TaskExecution) map[string]any {
	if task == nil {
		return map[string]any{}
	}
	snapshot := task.ProjectSnapshot.Data()
	profile := task.AgentProfileSnapshot
	profileStatus := "frozen"
	changed := false
	if project != nil {
		currentProfile := project.Profile.Data()
		snapshotProfile := snapshot.Profile
		if snapshotProfile.Version > 0 && currentProfile.Version > snapshotProfile.Version {
			profileStatus = "conflict"
			changed = true
		}
	}
	profileLabel := strings.TrimSpace(profile.DisplayName)
	if profileLabel == "" {
		profileLabel = strings.TrimSpace(profile.ProfileID)
	}
	if profileLabel == "" {
		profileLabel = "当前配置"
	}
	projectLabel := strings.TrimSpace(snapshot.ProjectName)
	if projectLabel == "" && project != nil {
		projectLabel = strings.TrimSpace(project.Name)
	}
	if projectLabel == "" {
		projectLabel = "未设置项目"
	}

	diagnostic := task.OutcomeDiagnostic()
	executionStatus := runtimeContextExecutionStatus(task, currentExecution, diagnostic)
	lifecycle := task.Lifecycle.Data()
	execution := map[string]any{
		"status":    executionStatus,
		"resumable": executionStatus == "recoverable" || task.Status == model.TaskStatusCancelled,
		"workspace": "bound",
	}
	executionID := lifecycle.ExecutionID
	if currentExecution != nil && currentExecution.ID != "" {
		executionID = currentExecution.ID
	}
	if executionID != "" {
		execution["execution_id"] = executionID
	}
	if currentExecution != nil {
		if currentExecution.RuntimeProfile != "" {
			execution["runtime_profile"] = currentExecution.RuntimeProfile
		}
		if digest := runtimeImageDigest(currentExecution.RuntimeImage); digest != "" {
			execution["runtime_image_digest"] = digest
		}
	}
	if diagnostic != nil && diagnostic.ResumePoint != "" {
		execution["recovery_stage"] = diagnostic.ResumePoint
	}
	heartbeat := task.LastHeartbeatAt
	if currentExecution != nil && currentExecution.LastHeartbeatAt != nil {
		heartbeat = currentExecution.LastHeartbeatAt
	}
	if heartbeat != nil {
		execution["last_heartbeat_at"] = heartbeat.UTC().Format(time.RFC3339)
	}

	items := make([]map[string]any, 0, len(files))
	matchedRequired := make(map[string]bool)
	requiredSpecs := runtimeRequiredArtifactSpecs(currentExecution)
	currentExecutionID := ""
	if currentExecution != nil {
		currentExecutionID = currentExecution.ID
	}
	for _, file := range files {
		if file == nil {
			continue
		}
		status := "missing"
		switch file.State {
		case model.TaskFileStateDelivered:
			status = "delivered"
		case model.TaskFileStateRetained:
			status = "retained"
		}
		items = append(items, map[string]any{"id": file.ID, "label": artifactLabel(file.Role, file.FileName), "status": status})
	}
	completed := 0
	if len(requiredSpecs) > 0 {
		for index, spec := range requiredSpecs {
			for _, file := range files {
				if file != nil && (currentExecutionID == "" || file.ExecutionID == currentExecutionID) &&
					runtimeArtifactMatches(file, spec) &&
					(file.State == model.TaskFileStateDelivered || file.State == model.TaskFileStateRetained) {
					matchedRequired[spec.Role+"\x00"+spec.Path] = true
					completed++
					break
				}
			}
			key := spec.Role + "\x00" + spec.Path
			if !matchedRequired[key] {
				items = append(items, map[string]any{
					"id":     fmt.Sprintf("required:%d:%s", index, spec.Role),
					"label":  artifactLabel(spec.Role, filepath.Base(spec.Path)),
					"status": "missing",
				})
			}
		}
	} else {
		for _, file := range files {
			if file != nil && runtimeContextFileBelongsToExecution(file, currentExecutionID, lifecycle.ExecutionID) &&
				(file.State == model.TaskFileStateDelivered || file.State == model.TaskFileStateRetained) {
				completed++
			}
		}
	}
	failed := 0
	if diagnostic != nil && strings.HasPrefix(diagnostic.Code, "artifact_") {
		failed = 1
	}
	required := len(requiredSpecs)
	if required == 0 {
		required = completed + failed
		if required == 0 && len(files) > 0 {
			required = len(files)
		}
	}
	connectivity := map[string]any{"status": "healthy", "summary": "模型和 MCP 正常"}
	if diagnostic != nil && isConnectivityDiagnostic(diagnostic) {
		connectivity["status"] = "degraded"
		connectivity["summary"] = diagnostic.Summary
		if diagnostic.Code != "" {
			connectivity["diagnostic_code"] = diagnostic.Code
		}
	}
	if task.Status == model.TaskStatusPending || task.Status == model.TaskStatusRunning {
		connectivity["status"] = "unknown"
		connectivity["summary"] = "等待连接确认"
	}
	if task.LastHeartbeatAt != nil {
		connectivity["checked_at"] = task.LastHeartbeatAt.UTC().Format(time.RFC3339)
	}

	profileProjection := map[string]any{
		"status":                 profileStatus,
		"label":                  projectLabel,
		"snapshot_id":            task.AgentProfileFingerprint,
		"summary":                profileLabel + " · 任务已冻结",
		"changed_since_snapshot": changed,
	}
	if profile.ProfileID == "" && task.AgentProfileFingerprint == "" && !changed {
		profileProjection["status"] = "missing"
	}
	missing := required - completed - failed
	if missing < 0 {
		missing = 0
	}
	return map[string]any{
		"profile":      profileProjection,
		"execution":    execution,
		"artifacts":    map[string]any{"completed": completed, "required": required, "failed": failed, "missing": missing, "items": items},
		"connectivity": connectivity,
	}
}

// runtimeContextFileBelongsToExecution keeps the fallback artifact count tied
// to the active attempt when execution identity is available. Files without an
// execution id are legacy rows, so they remain countable for older tasks.
func runtimeContextFileBelongsToExecution(file *model.TaskFile, currentExecutionID, lifecycleExecutionID string) bool {
	if file == nil {
		return false
	}
	activeExecutionID := strings.TrimSpace(currentExecutionID)
	if activeExecutionID == "" {
		activeExecutionID = strings.TrimSpace(lifecycleExecutionID)
	}
	fileExecutionID := strings.TrimSpace(file.ExecutionID)
	// Rows created before execution-scoped files were introduced have no
	// execution id; retain them for compatibility because they cannot be
	// associated with an older attempt.
	return activeExecutionID == "" || fileExecutionID == "" || fileExecutionID == activeExecutionID
}

func isConnectivityDiagnostic(diagnostic *model.ExecutionDiagnostic) bool {
	if diagnostic == nil {
		return false
	}
	if diagnostic.Provider != "" || diagnostic.ProviderCode != "" || diagnostic.HTTPStatus != 0 {
		return true
	}
	code := strings.ToLower(strings.TrimSpace(diagnostic.Code))
	for _, marker := range []string{"connection", "connectivity", "endpoint", "provider", "mcp", "model_", "service_unavailable", "timeout"} {
		if strings.Contains(code, marker) {
			return true
		}
	}
	return false
}

func runtimeContextExecutionStatus(task *model.Task, execution *model.TaskExecution, diagnostic *model.ExecutionDiagnostic) string {
	if diagnostic != nil && diagnostic.Recoverable && (task.Status == model.TaskStatusFailed || task.Status == model.TaskStatusCancelled) {
		return "recoverable"
	}
	if execution != nil {
		switch execution.Status {
		case model.TaskExecutionCreated, model.TaskExecutionDispatching, model.TaskExecutionStarting:
			return "starting"
		case model.TaskExecutionRunning, model.TaskExecutionSucceeded:
			return "ready"
		case model.TaskExecutionFailed, model.TaskExecutionTimedOut, model.TaskExecutionCancelled:
			return "failed"
		}
	}
	if task.Status == model.TaskStatusPending || task.Status == model.TaskStatusRunning {
		return "starting"
	}
	if task.Status == model.TaskStatusFailed {
		if diagnostic != nil && diagnostic.Recoverable {
			return "recoverable"
		}
		return "failed"
	}
	return "ready"
}

func runtimeRequiredArtifactSpecs(execution *model.TaskExecution) []runtimeArtifactSpec {
	if execution == nil || len(execution.AgentPackRequiredArtifactContract) == 0 {
		return nil
	}
	var specs []runtimeArtifactSpec
	if err := json.Unmarshal(execution.AgentPackRequiredArtifactContract, &specs); err != nil {
		return nil
	}
	filtered := specs[:0]
	for _, spec := range specs {
		if strings.TrimSpace(spec.Role) != "" && strings.TrimSpace(spec.Path) != "" && spec.Required {
			filtered = append(filtered, spec)
		}
	}
	return filtered
}

func runtimeArtifactMatches(file *model.TaskFile, spec runtimeArtifactSpec) bool {
	if file == nil {
		return false
	}
	if strings.TrimSpace(file.Role) != strings.TrimSpace(spec.Role) {
		return false
	}
	filePath := strings.TrimSpace(strings.ReplaceAll(file.FilePath, "\\", "/"))
	pattern := strings.TrimSpace(strings.ReplaceAll(spec.Path, "\\", "/"))
	if filePath != "" && pattern != "" {
		if matched, err := path.Match(pattern, filePath); err == nil && matched {
			return true
		}
	}
	return false
}

func artifactLabel(role, filename string) string {
	labels := map[string]string{
		model.FileRoleTopic:         "选题",
		model.FileRoleOutline:       "大纲",
		model.FileRoleDraft:         "文章初稿",
		model.FileRoleFinalMarkdown: "文章正文",
		model.FileRoleMarkdown:      "文章正文",
		model.FileRoleCover:         "封面图",
		model.FileRoleImage:         "正文配图",
		model.FileRoleImageManifest: "配图清单",
		model.FileRoleHTML:          "排版 HTML",
		model.FileRoleDraftPackage:  "草稿包",
		model.FileRoleReview:        "质量复盘",
		model.FileRoleVideo:         "视频成片",
	}
	if label := labels[role]; label != "" {
		return label
	}
	name := strings.TrimSpace(filepath.Base(filename))
	if name == "" || name == "." {
		return "未命名文件"
	}
	return name
}

func runtimeImageDigest(image string) string {
	image = strings.TrimSpace(image)
	if at := strings.LastIndex(image, "@sha256:"); at >= 0 {
		digest := image[at+1:]
		if len(digest) == len("sha256:")+64 {
			return digest
		}
	}
	if strings.HasPrefix(image, "sha256:") && len(image) == len("sha256:")+64 {
		return image
	}
	return ""
}
