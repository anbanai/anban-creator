package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
)

const (
	kubernetesAgentAppName       = "creator-agent"
	kubernetesAgentNamePrefix    = kubernetesAgentAppName
	kubernetesUserIDLabel        = "anban.ai/user-id"
	kubernetesProjectIDLabel     = "anban.ai/project-id"
	kubernetesTaskIDLabel        = "anban.ai/task-id"
	kubernetesWorkspaceMountName = "workspace"
)

var kubernetesNameUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

func kubernetesAgentPodName(task *model.Task) string {
	userID, projectID := "", ""
	if task != nil {
		userID = task.UserID
		projectID = task.ProjectID
	}
	user := kubernetesSafeNamePart(userID)
	project := kubernetesSafeNamePart(projectID)
	hash := kubernetesHashSuffix(userID + "\x00" + projectID)
	base := fmt.Sprintf("%s-%s-%s", kubernetesAgentNamePrefix, user, project)
	maxBaseLen := 63 - len(hash) - 1
	if len(base) > maxBaseLen {
		base = strings.Trim(base[:maxBaseLen], "-")
	}
	if base == "" {
		base = kubernetesAgentNamePrefix
	}
	return strings.Trim(base+"-"+hash, "-")
}

func kubernetesAgentLabels(task *model.Task) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name": kubernetesAgentAppName,
	}
	if task == nil {
		return labels
	}
	if task.UserID != "" {
		labels[kubernetesUserIDLabel] = kubernetesLabelValue(task.UserID)
	}
	if task.ProjectID != "" {
		labels[kubernetesProjectIDLabel] = kubernetesLabelValue(task.ProjectID)
	}
	if task.ID != "" {
		labels[kubernetesTaskIDLabel] = kubernetesLabelValue(task.ID)
	}
	return labels
}

func kubernetesWorkspacePath(mountPath string, task *model.Task) string {
	userID, projectID, taskID := "", "", ""
	if task != nil {
		userID = task.UserID
		projectID = task.ProjectID
		taskID = task.ID
	}
	return path.Join(
		strings.TrimSpace(mountPath),
		"users", kubernetesSafePathPart(userID),
		"projects", kubernetesSafePathPart(projectID),
		"tasks", kubernetesSafePathPart(taskID),
		"workspace",
	)
}

func kubernetesSafeNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = kubernetesNameUnsafe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "unknown"
	}
	if len(value) > 24 {
		return strings.Trim(value[:24], "-")
	}
	return value
}

func kubernetesSafePathPart(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "\\", "-")
	value = strings.Trim(value, ". ")
	if value == "" {
		return "unknown"
	}
	return value
}

func kubernetesLabelValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	value = kubernetesNameUnsafe.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "unknown"
	}
	if len(value) <= 63 {
		return value
	}
	hash := kubernetesHashSuffix(value)
	return strings.Trim(value[:52], "-") + "-" + hash
}

func kubernetesHashSuffix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:10]
}
