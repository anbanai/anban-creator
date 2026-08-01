package service

import (
	"fmt"

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
	if target == nil || source == nil || source.AgentPackID == "" || source.AgentPackVersion == "" || source.AgentPackDigest == "" || source.RuntimeAdapter == "" || source.RuntimeProfile == "" {
		return false
	}
	target.AgentPackID = source.AgentPackID
	target.AgentPackVersion = source.AgentPackVersion
	target.AgentPackDigest = source.AgentPackDigest
	target.RuntimeAdapter = source.RuntimeAdapter
	target.RuntimeProfile = source.RuntimeProfile
	return true
}
