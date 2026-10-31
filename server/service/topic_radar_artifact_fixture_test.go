package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func topicDigestTestArtifact(t *testing.T, repo repository.Repository, digest *model.TopicDigest, raw []byte) []byte {
	t.Helper()
	bind := digest.AgentTaskID == nil || digest.AgentExecutionID == nil
	if digest.AgentTaskID == nil {
		digest.AgentTaskID = topicStringPtr("task-" + digest.ID)
	}
	if digest.AgentExecutionID == nil {
		digest.AgentExecutionID = topicStringPtr("execution-" + digest.ID)
	}
	if bind {
		if err := repo.TopicDigests().Update(context.Background(), digest); err != nil {
			t.Fatal(err)
		}
	}
	return topicArtifactFixture(t, raw, map[string]any{
		"task_id": *digest.AgentTaskID, "execution_id": *digest.AgentExecutionID,
		"kind": model.TaskKindTopicDigest, "generated_at": "2026-10-31T03:00:00Z",
	})
}

func topicScriptTestArtifact(t *testing.T, repo repository.Repository, job *model.TopicScriptJob, raw []byte) []byte {
	t.Helper()
	bind := job.TaskID == nil || job.ExecutionID == nil
	if job.TaskID == nil {
		job.TaskID = topicStringPtr("task-" + job.ID)
	}
	if job.ExecutionID == nil {
		job.ExecutionID = topicStringPtr("execution-" + job.ID)
	}
	if bind {
		if err := repo.TopicScriptJobs().Update(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	return topicArtifactFixture(t, raw, map[string]any{
		"task_id": *job.TaskID, "execution_id": *job.ExecutionID, "kind": model.TaskKindTopicScript,
		"target":      map[string]any{"platform": job.TargetPlatform, "duration_seconds": job.DurationSeconds, "aspect_ratio": job.AspectRatio},
		"source_refs": []map[string]any{{"type": "trend", "ref_id": "1"}},
	})
}

func topicArtifactFixture(t *testing.T, raw []byte, fields map[string]any) []byte {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	fields["status"] = "ready"
	fields["source"] = "topic-radar"
	fields["quality_status"] = "ready"
	fields["data_at"] = "2026-10-31T03:00:00Z"
	fields["missing"] = []string{}
	for key, value := range fields {
		if _, exists := body[key]; !exists {
			body[key] = value
		}
	}
	result, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
