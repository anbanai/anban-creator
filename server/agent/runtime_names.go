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
	ContainerRuntimeUser          = "1000:1000"
	ContainerHomePath             = "/home/node"
	DockerRuntimeHomeDirName      = ".anban-runtime-home"
	MontageSubmoduleEnvName       = "ANBAN_MONTAGE_SUBMODULE_PATH"
	ContainerMontageSubmodulePath = "/app/third_party/OpenMontage"
	OrphanedContainerNameFilter   = "^/" + EphemeralContainerNamePrefix
)

func DefaultWorkspaceDir(taskID string) string {
	return filepath.Join(os.TempDir(), DefaultWorkspaceBaseName, taskID)
}

func EphemeralContainerName(taskID string) string {
	return EphemeralContainerNamePrefix + taskID
}

func OrphanedContainerNameFilters() []string {
	return []string{OrphanedContainerNameFilter}
}
