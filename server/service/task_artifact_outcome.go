package service

import (
	"path"
	"strings"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
)

// A runtime failure can explain a missing file; it can never prove delivery.
func missingRequiredUploadFailure(execution *model.TaskExecution, files []*model.TaskFile, missing []string, failures []agent.ArtifactUploadFailure) *agent.ArtifactUploadFailure {
	required := make(map[string]bool)
	for _, name := range missing {
		if !strings.Contains(name, "/") {
			required["output/"+name] = true
		}
	}
	if frozen, err := resolveFrozenExecutionRequiredArtifactContract(execution); err == nil {
		for _, spec := range frozen {
			required[spec.Path] = true
		}
	}
	present := make(map[string]bool)
	for _, file := range files {
		if file != nil {
			present[file.FilePath] = true
		}
	}
	for _, failure := range failures {
		if !strings.HasPrefix(failure.Path, "output/") || path.Clean(failure.Path) != failure.Path || present[failure.Path] || !required[failure.Path] {
			continue
		}
		switch failure.Operation {
		case "prepare", "put", "stream":
		default:
			continue
		}
		if validArtifactTransferFailure(&failure.ArtifactTransferFailure) {
			return &failure
		}
	}
	return nil
}

func artifactFailureCode(result *agent.ExecutionResult) string {
	if result == nil || result.Success {
		return ""
	}
	switch result.RootErrorCode {
	case "artifact_upload_failed", "artifact_manifest_failed":
		return result.RootErrorCode
	}
	return ""
}

func executionTerminalScope(result *agent.ExecutionResult) model.LifecycleTerminalScope {
	if artifactFailureCode(result) != "" {
		return model.LifecycleTerminalInfrastructure
	}
	return model.LifecycleTerminalWork
}

func validArtifactTransferFailure(failure *agent.ArtifactTransferFailure) bool {
	if failure == nil || failure.Attempts < 0 || failure.Attempts > 4 {
		return false
	}
	switch failure.Code {
	case "deadline_exceeded", "cancelled":
		return true // A shared deadline can expire before the first attempt.
	case "connection_reset", "network_timeout", "network_unavailable", "rate_limited", "service_unavailable", "invalid_request", "unauthorized", "execution_conflict", "signature_expired", "integrity_mismatch", "protocol_error":
		return failure.Attempts > 0
	}
	return false
}

func validArtifactOperation(operation string) bool {
	switch operation {
	case "prepare", "put", "stream", "manifest":
		return true
	}
	return false
}
