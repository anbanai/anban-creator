package service

import (
	"fmt"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const openMontageTargetCloud = "cloud"

type MontageExecutionTargetRequest struct {
	Config          srvconfig.MontageConfig
	TaskType        string
	FromPlan        bool
	LocalAvailable  bool
	CloudAvailable  bool
	AssetsCloudSafe bool
}

func ResolveMontageExecutionTarget(req MontageExecutionTargetRequest) (string, error) {
	if !model.IsMontagePlatform(req.TaskType) {
		return model.ExecutionTargetCloud, nil
	}
	cfg := req.Config
	if !cfg.Enabled {
		return "", fmt.Errorf("montage is disabled")
	}
	if len(cfg.ExecutionTargets) == 0 {
		cfg.ExecutionTargets = []string{openMontageTargetCloud}
	}
	if cfg.DefaultExecutionTarget == "" {
		cfg.DefaultExecutionTarget = openMontageTargetCloud
	}
	if req.FromPlan {
		if containsMontageTarget(cfg.ExecutionTargets, openMontageTargetCloud) {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("montage plans require an available cloud execution target")
	}

	switch cfg.DefaultExecutionTarget {
	case openMontageTargetCloud:
		if containsMontageTarget(cfg.ExecutionTargets, openMontageTargetCloud) {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("montage cloud execution is unavailable")
	case model.ExecutionTargetLocal:
		if req.LocalAvailable && containsMontageTarget(cfg.ExecutionTargets, model.ExecutionTargetLocal) {
			return model.ExecutionTargetLocal, nil
		}
		if containsMontageTarget(cfg.ExecutionTargets, openMontageTargetCloud) {
			return model.ExecutionTargetCloud, nil
		}
		return "", fmt.Errorf("montage local execution is unavailable")
	default:
		return "", fmt.Errorf("invalid montage execution target %q", cfg.DefaultExecutionTarget)
	}
}

func containsMontageTarget(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
