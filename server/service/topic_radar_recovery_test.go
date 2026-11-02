package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

type topicEnqueueFunc func(string, []byte, string) (bool, error)

func (f topicEnqueueFunc) EnqueueUnique(kind string, body []byte, key string) (bool, error) {
	return f(kind, body, key)
}

func TestTopicRadarEnqueueCannotOverwriteWorkerClaim(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, topicEnqueueFunc(func(_ string, body []byte, _ string) (bool, error) {
		var payload map[string]string
		_ = json.Unmarshal(body, &payload)
		d, err := repo.TopicDigests().FindByID(ctx, payload["digest_id"])
		if err != nil {
			return false, err
		}
		d.Status = model.TopicDigestStatusRunning
		d.AgentTaskID = topicStringPtr("task-owner")
		d.AgentExecutionID = topicStringPtr("execution-owner")
		return true, repo.TopicDigests().Update(ctx, d)
	}), nil)
	d, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := repo.TopicDigests().FindByID(ctx, d.ID)
	if err != nil || actual.Status != model.TopicDigestStatusRunning || actual.AgentTaskID == nil {
		t.Fatalf("worker claim overwritten: %+v %v", actual, err)
	}
}

func TestTopicRadarScheduleRecoversWithoutDuplicatingOccurrence(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	due := now.Add(-time.Hour)
	sub.NextRunAt = &due
	if err := repo.TopicSubscriptions().Update(ctx, sub); err != nil {
		t.Fatal(err)
	}
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, func() time.Time { return now })
	if err := svc.ScheduleTopicDigests(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := svc.ScheduleTopicDigests(ctx, 10); err != nil {
		t.Fatal(err)
	}
	runs, err := repo.TopicDigests().ListBySubscription(ctx, sub.ID, 10)
	if err != nil || len(runs) != 1 || runs[0].OccurrenceKey != model.ScheduledTopicOccurrenceKey(sub.ID, due) {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	stored, err := repo.TopicSubscriptions().FindByID(ctx, sub.ID)
	if err != nil || stored.NextRunAt == nil || !stored.NextRunAt.After(now) {
		t.Fatalf("schedule not advanced: %+v %v", stored, err)
	}
}

func TestTopicScriptRetryClearsExecutionOwnership(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	svc := NewTopicRadarService(repo, nil, nil)
	d, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	d.Status, d.ActiveKey = model.TopicDigestStatusSucceeded, nil
	if err := repo.TopicDigests().Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := repo.TopicSuggestions().CreateBatch(ctx, []*model.TopicSuggestion{{ID: "suggestion", DigestID: d.ID, ProjectID: sub.ProjectID, Title: "topic", SourceRefs: datatypes.JSON(`[]`), Scores: datatypes.JSON(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	p := RequestTopicScriptParams{UserID: sub.UserID, SuggestionID: "suggestion", TargetPlatform: "douyin"}
	job, err := svc.RequestTopicScript(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	job.Status = model.TopicScriptJobFailed
	job.TaskID = topicStringPtr("old-task")
	job.ExecutionID = topicStringPtr("old-execution")
	if err := repo.TopicScriptJobs().Update(ctx, job); err != nil {
		t.Fatal(err)
	}
	retried, err := svc.RequestTopicScript(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ID != job.ID || retried.Attempt != job.Attempt+1 || retried.TaskID != nil || retried.ExecutionID != nil {
		t.Fatalf("invalid retry: %+v", retried)
	}
	job.Status = model.TopicScriptJobSucceeded
	won, err := repo.TopicScriptJobs().UpdateIfNonterminal(ctx, job)
	if err != nil || won {
		t.Fatalf("stale attempt changed retried job: %v %v", won, err)
	}
}
