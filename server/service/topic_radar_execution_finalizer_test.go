package service

import (
	"context"
	"io"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

func TestTopicRadarExecutionFinalizerEnforcesOwnershipAndSettlesFailure(t *testing.T) {
	for _, kind := range []string{model.TaskTypeTopicDigest, model.TaskTypeTopicScript} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			svc := NewTopicRadarService(repo, nil, nil)
			logger := zerolog.New(io.Discard)
			svc.SetTaskService(newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, t.TempDir(), nil, nil))
			task := &model.Task{ID: "owner", UserID: sub.UserID, ProjectID: sub.ProjectID, Channel: model.ChannelTopicRadar, Type: kind, CurrentExecutionID: topicStringPtr("execution")}
			execution := &model.TaskExecution{ID: "execution", TaskID: task.ID, Status: model.TaskExecutionFailed}
			digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
			if err != nil {
				t.Fatal(err)
			}
			id := digest.ID
			if kind == model.TaskTypeTopicDigest {
				digest.AgentTaskID, digest.AgentExecutionID = &task.ID, &execution.ID
				if err := repo.TopicDigests().Update(ctx, digest); err != nil {
					t.Fatal(err)
				}
			} else {
				id = "script"
				if err := repo.TopicScriptJobs().Create(ctx, &model.TopicScriptJob{ID: id, UserID: sub.UserID, ProjectID: sub.ProjectID, DigestID: digest.ID, SuggestionID: "suggestion", Fingerprint: "failure", Status: model.TopicScriptJobRunning, TaskID: &task.ID, ExecutionID: &execution.ID}); err != nil {
					t.Fatal(err)
				}
			}
			task.SetAgentInput(map[string]any{"snapshot": map[string]any{"snapshot_id": id}})
			stale := *execution
			stale.ID = "stale"
			if err := svc.FinalizeTopicRadarExecution(ctx, task, &stale); err == nil {
				t.Fatal("stale execution accepted")
			}
			if err := svc.FinalizeTopicRadarExecution(ctx, task, execution); err != nil {
				t.Fatal(err)
			}
			if err := svc.FinalizeTopicRadarExecution(ctx, task, execution); err != nil {
				t.Fatalf("terminal replay: %v", err)
			}
			if kind == model.TaskTypeTopicDigest {
				actual, err := repo.TopicDigests().FindByID(ctx, id)
				if err != nil || actual.Status != model.TopicDigestStatusFailed || actual.ActiveKey != nil {
					t.Fatalf("digest: %+v %v", actual, err)
				}
			} else {
				actual, err := repo.TopicScriptJobs().FindByID(ctx, id)
				if err != nil || actual.Status != model.TopicScriptJobFailed {
					t.Fatalf("script: %+v %v", actual, err)
				}
			}
		})
	}
}
