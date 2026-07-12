package agent

import (
	"os"
	"path/filepath"
)

const (
	AgentBinaryName               = "anban"
	DefaultWorkspaceBaseName      = "anban-creator"
	EphemeralContainerNamePrefix  = "creator-agent-task-"
	DockerAgentImageDefault       = "creator-agent:latest"
	MontageSubmoduleEnvName       = "ANBAN_MONTAGE_SUBMODULE_PATH"
	ContainerMontageSubmodulePath = "/app/third_party/OpenMontage"
	OrphanedContainerNameFilter   = "^/" + EphemeralContainerNamePrefix
	// LegacyOrphanedContainerNameFilter is retained only for cleanup during upgrades.
	LegacyOrphanedContainerNameFilter = "^/anban-creator-task-"
)

func DefaultWorkspaceDir(taskID string) string {
	return filepath.Join(os.TempDir(), DefaultWorkspaceBaseName, taskID)
}

func EphemeralContainerName(taskID string) string {
	return EphemeralContainerNamePrefix + taskID
}

func OrphanedContainerNameFilters() []string {
	return []string{OrphanedContainerNameFilter, LegacyOrphanedContainerNameFilter}
}
