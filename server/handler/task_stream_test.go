package handler

import (
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestEncodeTaskStreamEventUsesDistinctLifecycleAndLogEvents(t *testing.T) {
	lifecycle := model.TaskLifecycle{
		Version: 1, Revision: 4, ExecutionID: "execution-1",
		Stages: []model.TaskLifecycleStage{{ID: "writing", Title: "撰写内容", Source: "agent", Kind: "work", State: "active"}},
	}
	encoded, err := encodeTaskStreamEvent("lifecycle", lifecycle)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "event: lifecycle\n") || !strings.Contains(encoded, `"revision":4`) || strings.Contains(encoded, "percent") {
		t.Fatalf("lifecycle SSE = %q", encoded)
	}
	logEvent, err := encodeTaskStreamEvent("log", "Using tool: Read")
	if err != nil {
		t.Fatal(err)
	}
	if logEvent != "event: log\ndata: \"Using tool: Read\"\n\n" {
		t.Fatalf("log SSE = %q", logEvent)
	}
}

func TestTaskStreamTerminalWaitsForServerLifecycleStages(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		lifecycle model.TaskLifecycle
		want      bool
	}{
		{name: "running task", status: model.TaskStatusRunning, want: false},
		{name: "completed creative task", status: model.TaskStatusCompleted, lifecycle: model.TaskLifecycle{Version: 1, Stages: []model.TaskLifecycleStage{{ID: "writing", Source: model.TaskLifecycleSourceAgent, Kind: model.TaskLifecycleKindWork, State: model.TaskLifecycleStateComplete}}}, want: true},
		{name: "manual publication pending", status: model.TaskStatusCompleted, lifecycle: model.TaskLifecycle{Version: 1, Stages: []model.TaskLifecycleStage{{ID: "system_publication", Source: model.TaskLifecycleSourceServer, Kind: model.TaskLifecycleKindPublication, State: model.TaskLifecycleStatePending}}}, want: false},
		{name: "publication processing", status: model.TaskStatusCompleted, lifecycle: model.TaskLifecycle{Version: 1, Stages: []model.TaskLifecycleStage{{ID: "system_publication", Source: model.TaskLifecycleSourceServer, Kind: model.TaskLifecycleKindPublication, State: model.TaskLifecycleStateActive}}}, want: false},
		{name: "publication blocked", status: model.TaskStatusCompleted, lifecycle: model.TaskLifecycle{Version: 1, Stages: []model.TaskLifecycleStage{{ID: "system_publication", Source: model.TaskLifecycleSourceServer, Kind: model.TaskLifecycleKindPublication, State: model.TaskLifecycleStateBlocked}}}, want: false},
		{name: "publication completed", status: model.TaskStatusCompleted, lifecycle: model.TaskLifecycle{Version: 1, Stages: []model.TaskLifecycleStage{{ID: "system_publication", Source: model.TaskLifecycleSourceServer, Kind: model.TaskLifecycleKindPublication, State: model.TaskLifecycleStateComplete}}}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskStreamTerminal(tt.status, tt.lifecycle); got != tt.want {
				t.Fatalf("taskStreamTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}
