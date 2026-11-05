package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestTopicScriptArtifactScopeReadinessAndIntegrity(t *testing.T) {
	for _, tc := range []struct {
		name, user, status, stored string
		present, wantOK            bool
	}{
		{"ready", "user-1", model.TopicScriptJobSucceeded, `{"script":"valid"}`, true, true},
		{"foreign user", "stranger", model.TopicScriptJobSucceeded, `{"script":"valid"}`, true, false},
		{"not ready", "user-1", model.TopicScriptJobQueued, `{"script":"valid"}`, true, false},
		{"changed object", "user-1", model.TopicScriptJobSucceeded, `{"script":"tampered"}`, true, false},
		{"invalid JSON", "user-1", model.TopicScriptJobSucceeded, `broken`, true, false},
		{"storage unavailable", "user-1", model.TopicScriptJobSucceeded, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			hash := sha256.Sum256([]byte(`{"script":"valid"}`))
			job := &model.TopicScriptJob{ID: "script", UserID: sub.UserID, ProjectID: sub.ProjectID, DigestID: "digest", SuggestionID: "suggestion", Fingerprint: "fingerprint", Status: tc.status, TaskID: topicStringPtr("task"), ExecutionID: topicStringPtr("execution"), ArtifactPath: "output/topic-script.json", ArtifactSHA256: fmt.Sprintf("%x", hash)}
			if err := repo.TopicScriptJobs().Create(ctx, job); err != nil {
				t.Fatal(err)
			}
			store := &fakeTaskStorage{name: "fake", files: map[string][]byte{}}
			if tc.present {
				store.files["artifact"] = []byte(tc.stored)
			}
			if err := repo.TaskFiles().Create(ctx, &model.TaskFile{ID: "artifact", TaskID: "task", ExecutionID: "execution", FilePath: "output/topic-script.json", FileName: "topic-script.json", OSSKey: "artifact", StorageProvider: store.Name()}); err != nil {
				t.Fatal(err)
			}
			svc := NewTopicRadarService(repo, nil, nil)
			svc.SetTaskService(&TaskService{repo: repo, store: store})
			got, err := svc.GetScriptArtifact(ctx, tc.user, job.ID)
			if (err == nil) != tc.wantOK || (tc.wantOK && string(got) != tc.stored) || (!tc.wantOK && got != nil) {
				t.Fatalf("artifact=%s err=%v", got, err)
			}
		})
	}
}
