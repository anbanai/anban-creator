package service

import (
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestBootstrapMaterializesFrozenTopicRadarInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		taskType string
		taskKind string
	}{
		{name: "digest", taskType: model.TaskTypeTopicDigest, taskKind: model.TaskKindTopicDigest},
		{name: "script", taskType: model.TaskTypeTopicScript, taskKind: model.TaskKindTopicScript},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{ID: "task-1", Type: tc.taskType, TaskKind: tc.taskKind}
			task.SetAgentInput(map[string]any{
				"schema_version": "1.0", "task_id": "task-1", "execution_id": "", "task_kind": tc.taskKind,
				"snapshot":       map[string]any{"snapshot_id": "snapshot-1", "project_id": "project-1"},
				"source_records": []any{},
			})
			files, err := (&AgentBootstrapService{}).buildTopicRadarFiles(&model.TaskExecution{ID: "execution-1"}, task)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || files[0].Path != "input/topic-radar.json" || files[0].Mode != 0444 || files[0].Text == "" {
				t.Fatalf("files = %+v", files)
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(files[0].Text), &payload); err != nil {
				t.Fatal(err)
			}
			if payload["task_id"] != "task-1" || payload["execution_id"] != "execution-1" || payload["task_kind"] != tc.taskKind {
				t.Fatalf("identity = %#v", payload)
			}
			snapshot, ok := payload["snapshot"].(map[string]any)
			if !ok || snapshot["snapshot_id"] != "snapshot-1" {
				t.Fatalf("snapshot = %#v", payload["snapshot"])
			}
		})
	}
}

func TestBootstrapSkipsTopicRadarInputForOtherTasks(t *testing.T) {
	task := &model.Task{ID: "task-1", Type: model.TaskTypeWechatArticle, TaskKind: model.TaskKindContentGeneration}
	files, err := (&AgentBootstrapService{}).buildTopicRadarFiles(&model.TaskExecution{ID: "execution-1"}, task)
	if err != nil || len(files) != 0 {
		t.Fatalf("files = %+v, err = %v", files, err)
	}
}
