package model

import (
	"encoding/json"
	"time"
)

// MarshalJSON keeps the undeclared-plan snapshot structurally safe for API
// clients. A zero-value lifecycle is still version 0, but stages is always an
// array rather than null.
func (lifecycle TaskLifecycle) MarshalJSON() ([]byte, error) {
	type lifecycleJSON TaskLifecycle
	snapshot := lifecycle
	if snapshot.Stages == nil {
		snapshot.Stages = []TaskLifecycleStage{}
	}
	return json.Marshal(lifecycleJSON(snapshot))
}

// NormalizeTaskLifecycleTerminal applies a task terminal outcome to the
// current lifecycle snapshot. Successful creative delivery does not finalize
// Server-owned publication stages; failed and cancelled tasks skip them.
func NormalizeTaskLifecycleTerminal(lifecycle TaskLifecycle, taskStatus, description string, now time.Time) (TaskLifecycle, bool) {
	if lifecycle.Version != TaskLifecycleVersion || len(lifecycle.Stages) == 0 {
		return lifecycle, false
	}
	next := lifecycle
	next.Stages = append([]TaskLifecycleStage(nil), lifecycle.Stages...)
	pendingTerminalIndex := -1
	if taskStatus != TaskStatusCompleted {
		hasActiveWork := false
		for i := range next.Stages {
			stage := next.Stages[i]
			if stage.Source != TaskLifecycleSourceAgent || stage.Kind != TaskLifecycleKindWork {
				continue
			}
			if stage.State == TaskLifecycleStateActive {
				hasActiveWork = true
				break
			}
			if pendingTerminalIndex < 0 && stage.State == TaskLifecycleStatePending {
				pendingTerminalIndex = i
			}
		}
		if hasActiveWork {
			pendingTerminalIndex = -1
		}
	}
	changed := false
	for i := range next.Stages {
		stage := &next.Stages[i]
		if stage.Source != TaskLifecycleSourceAgent || stage.Kind != TaskLifecycleKindWork {
			if taskStatus != TaskStatusCompleted && stage.State == TaskLifecycleStatePending {
				stage.State = TaskLifecycleStateSkipped
				stage.CompletedAt = &now
				changed = true
			}
			continue
		}
		switch stage.State {
		case TaskLifecycleStateActive:
			switch taskStatus {
			case TaskStatusCompleted:
				stage.State = TaskLifecycleStateComplete
			case TaskStatusCancelled:
				stage.State = TaskLifecycleStateCancelled
			default:
				stage.State = TaskLifecycleStateFailed
			}
			stage.CompletedAt = &now
			if description != "" {
				stage.LatestUpdate = description
			}
			changed = true
		case TaskLifecycleStatePending:
			if i == pendingTerminalIndex {
				if taskStatus == TaskStatusCancelled {
					stage.State = TaskLifecycleStateCancelled
				} else {
					stage.State = TaskLifecycleStateFailed
				}
				stage.StartedAt = &now
				if description != "" {
					stage.LatestUpdate = description
				}
			} else {
				stage.State = TaskLifecycleStateSkipped
			}
			stage.CompletedAt = &now
			changed = true
		}
	}
	if !changed {
		return lifecycle, false
	}
	next.Revision++
	next.UpdatedAt = now
	return next, true
}
