package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	srvconfig "github.com/anbanai/anban-creator/server/config"
)

const (
	AgentBinaryName                  = "anban"
	ContainerRuntimeUser             = "1000:1000"
	ContainerHomePath                = "/home/node"
	RuntimeHomeDirName               = ".anban-runtime-home"
	ContainerOpenMontageVenvPath     = "/opt/openmontage-venv"
	ContainerContentRuntimePath      = "/usr/local/bin:/usr/bin:/bin"
	ContainerMontageRuntimePath      = ContainerOpenMontageVenvPath + "/bin:" + ContainerContentRuntimePath
	MontageSubmoduleEnvName          = "ANBAN_MONTAGE_SUBMODULE_PATH"
	MontageTemplateEnvName           = "ANBAN_MONTAGE_TEMPLATE_PATH"
	MontageRuntimeDirName            = "openmontage"
	ContainerMontageTemplatePath     = "/opt/montage-template"
	dockerRuntimeContainerNamePrefix = "creator-agent-job"
	dockerTaskWorkspaceNamePrefix    = "creator-agent-workspace"
	dockerRuntimeNameMaxLength       = 128
)

var dockerNameUnsafe = regexp.MustCompile(`[^a-z0-9_.-]+`)

// containerAgentMemoryMountPath follows the frozen adapter's Claude Code CWD.
func containerAgentMemoryMountPath(runtimeAdapter string) string {
	if strings.TrimSpace(runtimeAdapter) == agentpack.AdapterOpenMontage {
		return "/workspace/" + MontageRuntimeDirName + "/.claude/agent-memory"
	}
	return "/workspace/.claude/agent-memory"
}

func containerRuntimePath(taskType string) string {
	pack, ok := agentpack.Default().ForTaskType(taskType)
	if !ok {
		return ContainerContentRuntimePath
	}
	if pack.Runtime.Adapter == agentpack.AdapterOpenMontage {
		return ContainerMontageRuntimePath
	}
	return ContainerContentRuntimePath
}

func runtimeAdapterForTaskType(taskType string) string {
	if pack, ok := agentpack.Default().ForTaskType(taskType); ok {
		return pack.Runtime.Adapter
	}
	return agentpack.AdapterStandard
}

func RuntimeImageForTask(images srvconfig.RuntimeImages, taskType string) srvconfig.RuntimeImageSelection {
	return images.ForTask(taskType)
}

func dockerRuntimeContainerName(executionID string) string {
	return dockerIdentityName(dockerRuntimeContainerNamePrefix, executionID)
}

func dockerTaskWorkspaceVolumeName(taskID string) string {
	return dockerIdentityName(dockerTaskWorkspaceNamePrefix, taskID)
}

func dockerIdentityName(prefix, identity string) string {
	digest := sha256.Sum256([]byte(identity))
	hash := hex.EncodeToString(digest[:8])
	part := strings.ToLower(strings.TrimSpace(identity))
	part = dockerNameUnsafe.ReplaceAllString(part, "-")
	part = strings.Trim(part, "-._")
	maxPartLength := dockerRuntimeNameMaxLength - len(prefix) - len(hash) - 2
	if len(part) > maxPartLength {
		part = strings.Trim(part[:maxPartLength], "-._")
	}
	if part == "" {
		part = "unknown"
	}
	return prefix + "-" + part + "-" + hash
}
