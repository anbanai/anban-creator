package service

import (
	"fmt"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const openMontageTargetCloud = "cloud"

type OpenMontageExecutionTargetRequest struct {
	Config          srvconfig.OpenMontageConfig
	TaskType        string
	FromPlan        bool
	LocalAvailable  bool
	CloudAvailable  bool
	AssetsCloudSafe bool
}

func ResolveOpenMontageExecutionTarget(req OpenMontageExecutionTargetRequest) (string, error) {
	if !model.IsOpenMontagePlatform(req.TaskType) {
		return model.ExecutionTargetCloud, nil
	}
	cfg := req.Config
	if !cfg.Enabled {
		return "", fmt.Errorf("openmontage is disabled")
	}
	if len(cfg.ExecutionTargets) == 0 {
		cfg.ExecutionTargets = []string{openMontageTargetCloud}
	}
	if cfg.DefaultExecutionTarget == "" {
		cfg.DefaultExecutionTarget = openMontageTargetCloud
	}
	if req.FromPlan {
		if containsOpenMontageTarget(cfg.ExecutionTargets, openMontageTargetCloud) {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("openmontage plans require an available cloud execution target")
	}

	switch cfg.DefaultExecutionTarget {
	case openMontageTargetCloud:
		if containsOpenMontageTarget(cfg.ExecutionTargets, openMontageTargetCloud) {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("openmontage cloud execution is unavailable")
	case model.ExecutionTargetLocal:
		if req.LocalAvailable && containsOpenMontageTarget(cfg.ExecutionTargets, model.ExecutionTargetLocal) {
			return model.ExecutionTargetLocal, nil
		}
		if containsOpenMontageTarget(cfg.ExecutionTargets, openMontageTargetCloud) {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("openmontage local execution is unavailable")
	default:
		return "", fmt.Errorf("invalid openmontage execution target %q", cfg.DefaultExecutionTarget)
	}
}

func containsOpenMontageTarget(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
