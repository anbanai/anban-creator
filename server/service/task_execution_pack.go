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

func taskExecutionIdentity(task *model.Task) (agentID, channel, taskKind string) {
	if task == nil {
		return "", "", ""
	}
	agentID = strings.TrimSpace(task.AgentID)
	channel = strings.TrimSpace(task.Channel)
	taskKind = strings.TrimSpace(task.TaskKind)
	if agentID == "" {
		switch task.Type {
		case model.PlatformWechat, model.TaskTypeWechatArticle:
			agentID = model.AgentIDArticle
		case model.TaskTypeWechatPicture:
			agentID = model.AgentIDWechatPicture
		case model.PlatformSeednote:
			agentID = model.AgentIDSeednote
		case model.PlatformMontage:
			agentID = model.AgentIDMontage
		case model.PlatformHypit:
			agentID = model.AgentIDHypit
		case model.PlatformWhiteboardAnimation:
			agentID = model.AgentIDWhiteboard
		}
	}
	if channel == "" {
		channel, _ = model.AgentChannel(agentID)
	}
	switch taskKind {
	case "":
		switch agentID {
		case model.AgentIDArticle, model.AgentIDWechatPicture, model.AgentIDSeednote:
			taskKind = model.TaskKindContentGeneration
		case model.AgentIDProfile:
			taskKind = model.TaskKindProfileAnalysis
		default:
			taskKind = model.TaskKindContentGeneration
		}
	}
	return agentID, channel, taskKind
}

func applyAgentPackIdentity(execution *model.TaskExecution, agentID string) error {
	if execution == nil {
		return fmt.Errorf("task execution is required")
	}
	requestedKind := strings.TrimSpace(agentID)
	pack, ok := agentpack.Default().ForAgent(agentID)
	if !ok || pack.Kind != agentpack.KindManaged {
		// Some internal callers provide a workflow kind while constructing a
		// frozen managed execution. Resolve that kind to its owning Pack without
		// accepting the retired public article identity.
		if candidate, found := agentpack.Default().ForTaskType(agentID); found && candidate.Kind == agentpack.KindManaged {
			pack, ok = candidate, true
			agentID = pack.Agent.Name
		} else if agentID == model.PlatformMontage {
			pack, ok = agentpack.Default().ForAgent(model.AgentIDMontage)
			agentID = model.AgentIDMontage
		}
	}
	legacyPlugin := ok && pack.Kind == agentpack.KindPlugin && (agentID == model.AgentIDHypit || agentID == model.AgentIDMontage)
	if !ok || pack.Kind != agentpack.KindManaged && !legacyPlugin {
		return fmt.Errorf("managed Agent %q has no Agent Pack", agentID)
	}
	execution.AgentPackID = pack.ID
	execution.AgentID = agentID
	execution.Channel = pack.Channel
	if execution.Channel == "" {
		execution.Channel = agentID
	}
	if execution.TaskKind == "" {
		switch agentID {
		case model.AgentIDArticle:
			execution.TaskKind = model.TaskTypeWechatArticle
		case model.AgentIDWechatPicture:
			execution.TaskKind = model.TaskTypeWechatPicture
		case model.AgentIDHypit:
			execution.TaskKind = model.PlatformHypit
		case model.AgentIDSeednote:
			if requestedKind == model.TaskTypeViralAnalysis {
				execution.TaskKind = model.TaskTypeViralAnalysis
			} else {
				execution.TaskKind = model.TaskKindContentGeneration
			}
		case model.AgentIDFeedback:
			execution.TaskKind = model.TaskKindFeedbackAnalysis
		case model.AgentIDWhiteboard:
			execution.TaskKind = model.PlatformWhiteboardAnimation
		case model.AgentIDProfile:
			execution.TaskKind = model.TaskKindProfileAnalysis
		default:
			execution.TaskKind = model.TaskKindContentGeneration
		}
	}
	execution.AgentPackVersion = pack.Version
	execution.AgentPackDigest = pack.Digest
	taskKind := ""
	if execution != nil {
		taskKind = execution.TaskKind
	}
	deliveryContract, err := json.Marshal(pack.DeliveryForTaskKind(taskKind))
	if err != nil {
		return fmt.Errorf("marshal Agent Pack delivery contract: %w", err)
	}
	if len(pack.DeliveryForTaskKind(taskKind)) == 0 && taskKind != model.TaskKindProfileAnalysis {
		return fmt.Errorf("managed Agent Pack %q has no delivery contract for task kind %q", pack.ID, taskKind)
	}
	execution.AgentPackDeliveryContract = datatypes.JSON(deliveryContract)
	requiredArtifacts, err := pack.RequiredArtifactsForTaskKind(taskKind)
	if err != nil {
		return fmt.Errorf("resolve Agent Pack required artifact contract: %w", err)
	}
	if len(requiredArtifacts) == 0 && taskKind != model.TaskKindProfileAnalysis {
		return fmt.Errorf("managed Agent Pack %q has no required artifact contract for task kind %q", pack.ID, taskKind)
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
	target.AgentID = source.AgentID
	target.Channel = source.Channel
	target.TaskKind = source.TaskKind
	target.AgentPackVersion = source.AgentPackVersion
	target.AgentPackDigest = source.AgentPackDigest
	target.AgentPackDeliveryContract = append(datatypes.JSON(nil), source.AgentPackDeliveryContract...)
	target.AgentPackRequiredArtifactContract = append(datatypes.JSON(nil), source.AgentPackRequiredArtifactContract...)
	target.RuntimeAdapter = source.RuntimeAdapter
	target.RuntimeProfile = source.RuntimeProfile
	return true
}

func hasCompleteAgentPackIdentity(execution *model.TaskExecution) bool {
	if execution == nil {
		return false
	}
	dataOnlyProfile := execution.TaskKind == model.TaskKindProfileAnalysis
	return strings.TrimSpace(execution.AgentPackID) != "" &&
		strings.TrimSpace(execution.AgentPackVersion) != "" &&
		strings.TrimSpace(execution.AgentPackDigest) != "" &&
		(dataOnlyProfile || (len(execution.AgentPackDeliveryContract) > 0 && len(execution.AgentPackRequiredArtifactContract) > 0)) &&
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
