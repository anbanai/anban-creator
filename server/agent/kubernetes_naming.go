package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/anbanai/anban-creator/server/model"
)

const (
	kubernetesAgentAppName        = "creator-agent"
	kubernetesUserIDLabel         = "anban.ai/user-id"
	kubernetesProjectIDLabel      = "anban.ai/project-id"
	kubernetesTaskIDLabel         = "anban.ai/task-id"
	kubernetesExecutionIDLabel    = "anban.ai/execution-id"
	kubernetesWorkspaceMountName  = "workspace"
	kubernetesJobNamePrefix       = "creator-agent-job"
	kubernetesWorkspaceNamePrefix = "creator-agent-workspace"
)

var kubernetesNameUnsafe = regexp.MustCompile(`[^a-z0-9-]+`)

func kubernetesJobName(executionID string) string {
	return kubernetesIdentityName(kubernetesJobNamePrefix, executionID)
}

func kubernetesTaskWorkspacePVCName(taskID string) string {
	return kubernetesIdentityName(kubernetesWorkspaceNamePrefix, taskID)
}

func kubernetesExecutionWorkspacePVCName(execution *model.TaskExecution, task *model.Task) string {
	identity := taskID(task)
	if execution != nil && execution.Purpose == model.TaskExecutionPurposePublicationRecovery {
		identity += "-publication-recovery-" + executionID(execution)
	}
	return kubernetesTaskWorkspacePVCName(identity)
}

func kubernetesIdentityName(prefix, identity string) string {
	part := kubernetesSafeNamePart(identity)
	hash := kubernetesHashSuffix(identity)
	maxPartLen := 63 - len(prefix) - len(hash) - 2
	if len(part) > maxPartLen {
		part = strings.Trim(part[:maxPartLen], "-")
	}
	if part == "" {
		part = "unknown"
	}
	return prefix + "-" + part + "-" + hash
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

func kubernetesLabelValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(validation.IsValidLabelValue(value)) == 0 {
		return value
	}
	safe := strings.ToLower(value)
	safe = strings.ReplaceAll(safe, "_", "-")
	safe = kubernetesNameUnsafe.ReplaceAllString(safe, "-")
	safe = strings.Trim(safe, "-")
	hash := kubernetesHashSuffix(value)
	if len(safe) > 52 {
		safe = strings.Trim(safe[:52], "-")
	}
	if safe == "" {
		return hash
	}
	return safe + "-" + hash
}

func kubernetesHashSuffix(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:10]
}
