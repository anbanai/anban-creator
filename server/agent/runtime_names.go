package agent

import (
	"os"
	"path/filepath"
)

const (
	AgentBinaryName              = "anban"
	DefaultWorkspaceBaseName     = "anban-creator"
	EphemeralContainerNamePrefix = "anban-creator-task-"
	DockerAgentImageDefault      = "anban-creator-agent:latest"
	KubernetesAgentImageDefault  = "anban-creator-server:latest"
	OrphanedContainerNameFilter  = "^/" + EphemeralContainerNamePrefix
)

func DefaultWorkspaceDir(taskID string) string {
	return filepath.Join(os.TempDir(), DefaultWorkspaceBaseName, taskID)
}

func EphemeralContainerName(taskID string) string {
	return EphemeralContainerNamePrefix + taskID
}
