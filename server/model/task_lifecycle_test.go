package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"
)

func TestEmptyTaskLifecycleSerializesStagesAsArray(t *testing.T) {
	raw, err := json.Marshal(datatypes.NewJSONType(TaskLifecycle{}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"stages":[]`) {
		t.Fatalf("empty lifecycle JSON = %s, want stages array", raw)
	}
}

func TestNormalizeTaskLifecycleTerminalMarksTheNextPendingStageAsTheFailurePoint(t *testing.T) {
	now := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		taskStatus string
		wantState  string
	}{
		{name: "failed", taskStatus: TaskStatusFailed, wantState: TaskLifecycleStateFailed},
		{name: "cancelled", taskStatus: TaskStatusCancelled, wantState: TaskLifecycleStateCancelled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lifecycle := TaskLifecycle{Version: TaskLifecycleVersion, Revision: 2, Stages: []TaskLifecycleStage{
				{ID: "research", Source: TaskLifecycleSourceAgent, Kind: TaskLifecycleKindWork, State: TaskLifecycleStateComplete},
				{ID: "writing", Source: TaskLifecycleSourceAgent, Kind: TaskLifecycleKindWork, State: TaskLifecycleStatePending},
				{ID: "review", Source: TaskLifecycleSourceAgent, Kind: TaskLifecycleKindWork, State: TaskLifecycleStatePending},
			}}

			normalized, changed := NormalizeTaskLifecycleTerminal(lifecycle, tt.taskStatus, "执行已终止", now, LifecycleTerminalWork)

			if !changed {
				t.Fatal("terminal normalization reported no change")
			}
			if normalized.Stages[1].State != tt.wantState || normalized.Stages[1].LatestUpdate != "执行已终止" || normalized.Stages[1].StartedAt == nil || normalized.Stages[1].CompletedAt == nil {
				t.Fatalf("terminal stage = %#v, want state %q with failure context", normalized.Stages[1], tt.wantState)
			}
			if normalized.Stages[2].State != TaskLifecycleStateSkipped {
				t.Fatalf("tail stage = %#v, want skipped", normalized.Stages[2])
			}
		})
	}
}

func TestInfrastructureFailurePreservesAgentProgress(t *testing.T) {
	now := time.Now()
	current := TaskLifecycle{Version: TaskLifecycleVersion, Revision: 3, Stages: []TaskLifecycleStage{
		{ID: "research", Source: TaskLifecycleSourceAgent, Kind: TaskLifecycleKindWork, State: TaskLifecycleStateActive},
		{ID: "writing", Source: TaskLifecycleSourceAgent, Kind: TaskLifecycleKindWork, State: TaskLifecycleStatePending},
	}}
	got, changed := NormalizeTaskLifecycleTerminal(current, TaskStatusFailed, "产物上传失败", now, LifecycleTerminalInfrastructure)
	if changed || got.Stages[0].State != TaskLifecycleStateActive || got.Stages[1].State != TaskLifecycleStatePending {
		t.Fatalf("infrastructure failure falsely changed work evidence: %#v", got)
	}
}
