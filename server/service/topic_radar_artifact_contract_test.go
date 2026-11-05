package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTopicArtifactRejectsInvalidEnvelope(t *testing.T) {
	cases := []struct {
		name, field string
		value       any
		omit        bool
	}{
		{name: "wrong task", field: "task_id", value: "another-task"},
		{name: "wrong execution", field: "execution_id", value: "another-execution"},
		{name: "wrong kind", field: "kind", value: "topic_script"},
		{name: "blocked", field: "status", value: "blocked"},
		{name: "insufficient", field: "status", value: "data_insufficient"},
		{name: "blocked quality", field: "quality_status", value: "blocked"},
		{name: "wrong source", field: "source", value: "untrusted"},
		{name: "invalid data timestamp", field: "data_at", value: "yesterday"},
		{name: "invalid generated timestamp", field: "generated_at", value: "yesterday"},
		{name: "null missing evidence", field: "missing", value: nil},
	}
	for _, field := range []string{"task_id", "execution_id", "kind", "status", "source", "data_at", "missing", "quality_status", "generated_at"} {
		cases = append(cases, struct {
			name, field string
			value       any
			omit        bool
		}{name: "missing " + field, field: field, omit: true})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			svc := NewTopicRadarService(repo, nil, nil)
			digest, err := svc.Run(context.Background(), RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
			if err != nil {
				t.Fatal(err)
			}
			freezeTestTopicRefs(t, repo, digest)
			raw := topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"title","introduction":"intro","source_summary":"source","source_refs":[{"type":"trend","ref_id":"1"}],"scores":{},"video_fit":"good"}]}`))
			var data map[string]any
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatal(err)
			}
			if tc.omit {
				delete(data, tc.field)
			} else {
				data[tc.field] = tc.value
			}
			body, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.FinalizeDigest(context.Background(), digest.ID, body); err == nil {
				t.Fatal("invalid envelope accepted")
			}
			got, err := repo.TopicDigests().FindByID(context.Background(), digest.ID)
			if err != nil || got.Status != model.TopicDigestStatusFailed {
				t.Fatalf("digest=%+v err=%v", got, err)
			}
		})
	}
}

func TestTopicScriptRejectsTargetTimingAndSourceMismatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"timing", func(data map[string]any) { data["scenes"].([]any)[0].(map[string]any)["duration_seconds"] = 5 }},
		{"target", func(data map[string]any) { data["target"].(map[string]any)["platform"] = "bilibili" }},
		{"missing target", func(data map[string]any) { delete(data, "target") }},
		{"source", func(data map[string]any) { data["source_refs"].([]any)[0].(map[string]any)["ref_id"] = "invented" }},
		{"missing source", func(data map[string]any) { delete(data, "source_refs") }},
		{"empty sources", func(data map[string]any) { data["source_refs"] = []any{} }},
		{"scene source", func(data map[string]any) {
			data["scenes"].([]any)[0].(map[string]any)["source_refs"] = []map[string]any{{"type": "trend", "ref_id": "invented"}}
		}},
		{"missing identity", func(data map[string]any) { delete(data, "task_id") }},
		{"wrong execution", func(data map[string]any) { data["execution_id"] = "another-execution" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := topicRadarTestRepository(t)
			ctx := context.Background()
			sub := seedTopicRadarSubscription(t, repo)
			svc := NewTopicRadarService(repo, nil, nil)
			digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
			if err != nil {
				t.Fatal(err)
			}
			freezeTestTopicRefs(t, repo, digest)
			job := &model.TopicScriptJob{ID: "script", DigestID: digest.ID, ProjectID: sub.ProjectID, UserID: sub.UserID, SuggestionID: "suggestion", Fingerprint: "script", Status: model.TopicScriptJobRunning, TargetPlatform: "douyin", DurationSeconds: 60, AspectRatio: "9:16"}
			if err := repo.TopicScriptJobs().Create(ctx, job); err != nil {
				t.Fatal(err)
			}
			raw := topicScriptTestArtifact(t, repo, job, []byte(`{"schema_version":"1.0","title":"title","hook":"hook","scenes":[{"duration_seconds":60,"visual":"visual","voiceover":"voice"}],"cta":"cta"}`))
			var data map[string]any
			if err := json.Unmarshal(raw, &data); err != nil {
				t.Fatal(err)
			}
			tc.mutate(data)
			body, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.FinalizeTopicScript(ctx, job.ID, body, "output/topic-script.json"); err == nil {
				t.Fatal("invalid script accepted")
			}
		})
	}
}
