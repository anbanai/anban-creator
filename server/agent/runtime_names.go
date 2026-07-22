package agent

import (
	"os"
	"path/filepath"
)

const (
	AgentBinaryName              = "anban"
	DefaultWorkspaceBaseName     = "anban-creator"
	EphemeralContainerNamePrefix = "creator-agent-task-"
	DockerArticleImageDefault    = "creator-agent-article:latest"
	ContainerRuntimeUser         = "1000:1000"
	ContainerHomePath            = "/home/node"
	DockerRuntimeHomeDirName     = ".anban-runtime-home"
	ContainerAgentReachVenvPath  = "/opt/agent-reach-venv"
	ContainerOpenMontageVenvPath = "/opt/openmontage-venv"
	ContainerContentRuntimePath  = "/usr/local/bin:/usr/bin:/bin"
	ContainerSeednoteRuntimePath = ContainerAgentReachVenvPath + "/bin:" + ContainerContentRuntimePath
	ContainerMontageRuntimePath  = ContainerOpenMontageVenvPath + "/bin:" + ContainerContentRuntimePath
	MontageSubmoduleEnvName      = "ANBAN_MONTAGE_SUBMODULE_PATH"
	MontageTemplateEnvName       = "ANBAN_MONTAGE_TEMPLATE_PATH"
	MontageRuntimeDirName        = "montage"
	ContainerMontageTemplatePath = "/opt/montage-template"
	OrphanedContainerNameFilter  = "^/" + EphemeralContainerNamePrefix
)

func containerRuntimePath(taskType string) string {
	switch taskType {
	case "seednote":
		return ContainerSeednoteRuntimePath
	case "montage":
		return ContainerMontageRuntimePath
	default:
		return ContainerContentRuntimePath
	}
}

func DefaultWorkspaceDir(taskID string) string {
	return filepath.Join(os.TempDir(), DefaultWorkspaceBaseName, taskID)
}

func EphemeralContainerName(taskID string) string {
	return EphemeralContainerNamePrefix + taskID
}

func OrphanedContainerNameFilters() []string {
	return []string{OrphanedContainerNameFilter}
}
