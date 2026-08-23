package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

var (
	ErrAgentProgressExecutionMismatch = errors.New("agent progress execution does not belong to task")
	ErrAgentProgressPackMismatch      = errors.New("agent progress execution Pack does not match frozen contract")
	ErrAgentProgressUnknownStage      = errors.New("agent progress stage is not declared by frozen Pack")
	ErrAgentProgressStateMismatch     = errors.New("agent progress state does not match frozen Pack")
	ErrAgentProgressTitleMismatch     = errors.New("agent progress title does not match frozen Pack")
	ErrAgentProgressPercentMismatch   = errors.New("agent progress percent does not match frozen Pack")
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
	progressContract, err := json.Marshal(pack.ProgressForTaskType(taskType))
	if err != nil {
		return fmt.Errorf("marshal Agent Pack progress contract: %w", err)
	}
	execution.AgentPackProgressContract = datatypes.JSON(progressContract)
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
	target.AgentPackProgressContract = append(datatypes.JSON(nil), source.AgentPackProgressContract...)
	target.RuntimeAdapter = source.RuntimeAdapter
	target.RuntimeProfile = source.RuntimeProfile
	return true
}

func hasCompleteAgentPackIdentity(execution *model.TaskExecution) bool {
	return execution != nil &&
		strings.TrimSpace(execution.AgentPackID) != "" &&
		strings.TrimSpace(execution.AgentPackVersion) != "" &&
		strings.TrimSpace(execution.AgentPackDigest) != "" &&
		len(execution.AgentPackProgressContract) > 0 &&
		strings.TrimSpace(execution.RuntimeAdapter) != "" &&
		strings.TrimSpace(execution.RuntimeProfile) != ""
}

func resolveFrozenExecutionProgressContract(execution *model.TaskExecution) ([]agentpack.ProgressStage, error) {
	if execution == nil || strings.TrimSpace(execution.AgentPackID) == "" || strings.TrimSpace(execution.AgentPackVersion) == "" || strings.TrimSpace(execution.AgentPackDigest) == "" || len(execution.AgentPackProgressContract) == 0 {
		return nil, ErrAgentProgressPackMismatch
	}
	var progress []agentpack.ProgressStage
	if err := json.Unmarshal(execution.AgentPackProgressContract, &progress); err != nil {
		return nil, fmt.Errorf("%w: decode frozen progress contract", ErrAgentProgressPackMismatch)
	}
	return progress, nil
}
