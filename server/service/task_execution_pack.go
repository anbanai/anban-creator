package service

import (
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
)

func applyAgentPackIdentity(execution *model.TaskExecution, taskType string) error {
	if execution == nil {
		return fmt.Errorf("task execution is required")
	}
	pack, ok := agentpack.Default().ForTaskType(taskType)
	if !ok || pack.Kind != agentpack.KindManaged {
		return fmt.Errorf("managed task type %q has no Agent Pack", taskType)
	}
	execution.AgentPackID = pack.ID
	execution.AgentPackVersion = pack.Version
	execution.AgentPackDigest = pack.Digest
	execution.RuntimeAdapter = pack.Runtime.Adapter
	execution.RuntimeProfile = pack.Runtime.Profile
	return nil
}

func inheritAgentPackIdentity(target, source *model.TaskExecution) bool {
	if target == nil || !hasCompleteAgentPackIdentity(source) {
		return false
	}
	target.AgentPackID = source.AgentPackID
	target.AgentPackVersion = source.AgentPackVersion
	target.AgentPackDigest = source.AgentPackDigest
	target.RuntimeAdapter = source.RuntimeAdapter
	target.RuntimeProfile = source.RuntimeProfile
	return true
}

func hasCompleteAgentPackIdentity(execution *model.TaskExecution) bool {
	return execution != nil &&
		strings.TrimSpace(execution.AgentPackID) != "" &&
		strings.TrimSpace(execution.AgentPackVersion) != "" &&
		strings.TrimSpace(execution.AgentPackDigest) != "" &&
		strings.TrimSpace(execution.RuntimeAdapter) != "" &&
		strings.TrimSpace(execution.RuntimeProfile) != ""
}
