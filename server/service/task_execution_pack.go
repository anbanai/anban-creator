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
	ErrAgentPackContractMismatch = errors.New("agent execution Pack does not match frozen contract")
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
	deliveryContract, err := json.Marshal(pack.DeliveryForTaskType(taskType))
	if err != nil {
		return fmt.Errorf("marshal Agent Pack delivery contract: %w", err)
	}
	if len(pack.DeliveryForTaskType(taskType)) == 0 {
		return fmt.Errorf("managed Agent Pack %q has no delivery contract for task type %q", pack.ID, taskType)
	}
	execution.AgentPackDeliveryContract = datatypes.JSON(deliveryContract)
	requiredArtifacts, err := pack.RequiredArtifactsForTaskType(taskType)
	if err != nil {
		return fmt.Errorf("resolve Agent Pack required artifact contract: %w", err)
	}
	if len(requiredArtifacts) == 0 {
		return fmt.Errorf("managed Agent Pack %q has no required artifact contract for task type %q", pack.ID, taskType)
	}
	requiredArtifactContract, err := json.Marshal(requiredArtifacts)
	if err != nil {
		return fmt.Errorf("marshal Agent Pack required artifact contract: %w", err)
	}
	execution.AgentPackRequiredArtifactContract = datatypes.JSON(requiredArtifactContract)
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
	target.AgentPackDeliveryContract = append(datatypes.JSON(nil), source.AgentPackDeliveryContract...)
	target.AgentPackRequiredArtifactContract = append(datatypes.JSON(nil), source.AgentPackRequiredArtifactContract...)
	target.RuntimeAdapter = source.RuntimeAdapter
	target.RuntimeProfile = source.RuntimeProfile
	return true
}

func hasCompleteAgentPackIdentity(execution *model.TaskExecution) bool {
	return execution != nil &&
		strings.TrimSpace(execution.AgentPackID) != "" &&
		strings.TrimSpace(execution.AgentPackVersion) != "" &&
		strings.TrimSpace(execution.AgentPackDigest) != "" &&
		len(execution.AgentPackDeliveryContract) > 0 &&
		len(execution.AgentPackRequiredArtifactContract) > 0 &&
		strings.TrimSpace(execution.RuntimeAdapter) != "" &&
		strings.TrimSpace(execution.RuntimeProfile) != ""
}

func resolveFrozenExecutionRequiredArtifactContract(execution *model.TaskExecution) ([]agentpack.ArtifactSpec, error) {
	if execution == nil || strings.TrimSpace(execution.AgentPackID) == "" || strings.TrimSpace(execution.AgentPackVersion) == "" || strings.TrimSpace(execution.AgentPackDigest) == "" || len(execution.AgentPackRequiredArtifactContract) == 0 {
		return nil, ErrAgentPackContractMismatch
	}
	var required []agentpack.ArtifactSpec
	if err := json.Unmarshal(execution.AgentPackRequiredArtifactContract, &required); err != nil {
		return nil, fmt.Errorf("%w: decode frozen required artifact contract", ErrAgentPackContractMismatch)
	}
	if len(required) == 0 {
		return nil, fmt.Errorf("%w: empty frozen required artifact contract", ErrAgentPackContractMismatch)
	}
	for _, spec := range required {
		if strings.TrimSpace(spec.Role) == "" || strings.TrimSpace(spec.Path) == "" || strings.TrimSpace(spec.MIMEType) == "" || !spec.Required {
			return nil, fmt.Errorf("%w: invalid frozen required artifact contract", ErrAgentPackContractMismatch)
		}
	}
	return required, nil
}

func resolveFrozenExecutionDeliveryContract(execution *model.TaskExecution) ([]agentpack.DeliverySpec, error) {
	if execution == nil || strings.TrimSpace(execution.AgentPackID) == "" || strings.TrimSpace(execution.AgentPackVersion) == "" || strings.TrimSpace(execution.AgentPackDigest) == "" || len(execution.AgentPackDeliveryContract) == 0 {
		return nil, ErrAgentPackContractMismatch
	}
	var delivery []agentpack.DeliverySpec
	if err := json.Unmarshal(execution.AgentPackDeliveryContract, &delivery); err != nil {
		return nil, fmt.Errorf("%w: decode frozen delivery contract", ErrAgentPackContractMismatch)
	}
	if len(delivery) == 0 {
		return nil, fmt.Errorf("%w: empty frozen delivery contract", ErrAgentPackContractMismatch)
	}
	return delivery, nil
}
