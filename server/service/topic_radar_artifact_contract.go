package service

import (
	"errors"
	"fmt"
	"time"
)

type topicArtifactEnvelope struct {
	TaskID        string    `json:"task_id"`
	ExecutionID   string    `json:"execution_id"`
	Kind          string    `json:"kind"`
	Status        string    `json:"status"`
	Source        string    `json:"source"`
	DataAt        time.Time `json:"data_at"`
	Missing       []string  `json:"missing"`
	QualityStatus string    `json:"quality_status"`
}

func (e topicArtifactEnvelope) validate(kind string, taskID, executionID *string) error {
	if e.TaskID == "" || taskID == nil || e.TaskID != *taskID {
		return errors.New("artifact task identity mismatch")
	}
	if e.ExecutionID == "" || executionID == nil || e.ExecutionID != *executionID {
		return errors.New("artifact execution identity mismatch")
	}
	if e.Kind != kind {
		return errors.New("artifact kind mismatch")
	}
	if e.Source != "topic-radar" {
		return errors.New("artifact source mismatch")
	}
	if e.DataAt.IsZero() || e.Missing == nil {
		return errors.New("artifact data timestamp and missing evidence are required")
	}
	if e.Status != "ready" && e.Status != "warning" {
		return fmt.Errorf("artifact is not usable: status %q", e.Status)
	}
	if e.QualityStatus != "ready" && e.QualityStatus != "warning" {
		return errors.New("artifact quality is blocked or missing")
	}
	return nil
}
