package service

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/datatypes"
)

func TestTopicRadarFreezesScopedSources(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	sub := seedTopicRadarSubscription(t, repo)
	sub.SourceConfig = datatypes.JSON(`{"platforms":["douyin"]}`)
	if err := repo.TopicSubscriptions().Update(ctx, sub); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	trend := &model.TrendSnapshot{Platform: "douyin", Items: datatypes.JSON(`[{"title":"热点证据","url":"https://example.test/trend","rank":1,"hot":"123"}]`), FetchedAt: now, LastAttemptedAt: now}
	if err := repo.TrendSnapshots().Upsert(ctx, trend); err != nil {
		t.Fatal(err)
	}
	for _, item := range []*model.TopicPool{
		{UserID: sub.UserID, ProjectID: sub.ProjectID, Topic: "本项目历史选题", Status: model.TopicStatusUnused, ContentAssetID: topicStringPtr("source-asset")},
		{UserID: "other-user", ProjectID: "other-project", Topic: "不能泄露的选题", Status: model.TopicStatusUnused, ContentAssetID: topicStringPtr("source-asset")},
	} {
		if err := repo.TopicPools().Create(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []*model.SearchOperation{
		{ID: "search-owned", UserID: sub.UserID, ProjectID: sub.ProjectID, TaskID: "t", ExecutionID: "e", Operation: "mcp.search_web", Status: model.SearchOperationSucceeded, RequestFingerprint: "owned", ResponseSnapshot: datatypes.JSON(`{"results":[{"title":"检索证据","url":"https://example.test/search"}]}`)},
		{ID: "search-other", UserID: "other-user", ProjectID: "other-project", TaskID: "t2", ExecutionID: "e2", Operation: "mcp.search_web", Status: model.SearchOperationSucceeded, RequestFingerprint: "other", ResponseSnapshot: datatypes.JSON(`{"results":[{"title":"不应泄露","url":"https://private.example.test"}]}`)},
	} {
		if err := repo.Analytics().DB().Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, func() time.Time { return now })
	digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID, RequestKey: "freeze"})
	if err != nil {
		t.Fatal(err)
	}
	var frozen struct {
		SourceRefs    []model.TopicSourceRef `json:"source_refs"`
		MissingFields []string               `json:"missing_fields"`
	}
	if err := json.Unmarshal(digest.InputSnapshot, &frozen); err != nil {
		t.Fatal(err)
	}
	if len(frozen.SourceRefs) != 3 {
		t.Fatalf("frozen refs = %+v; want trend, owned pool and owned search", frozen.SourceRefs)
	}
	for _, ref := range frozen.SourceRefs {
		if ref.Title == "不应泄露" || ref.Title == "不能泄露的选题" {
			t.Fatal("cross-project source leaked")
		}
	}
	original := string(digest.InputSnapshot)
	trend.Items = datatypes.JSON(`[{"title":"后来修改的热点","url":"https://example.test/changed"}]`)
	if err := repo.TrendSnapshots().Upsert(ctx, trend); err != nil {
		t.Fatal(err)
	}
	again, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID, RequestKey: "freeze"})
	if err != nil || string(again.InputSnapshot) != original {
		t.Fatalf("snapshot changed on replay: %v", err)
	}
}

func TestTopicRadarSubscriptionInitialScheduleAndValidation(t *testing.T) {
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	now := time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, func() time.Time { return now })
	valid := CreateTopicSubscriptionParams{UserID: sub.UserID, ProjectID: sub.ProjectID, Name: "daily", Enabled: true, Timezone: "Asia/Shanghai", Schedule: datatypes.JSON(`{"hour":9,"minute":30}`)}
	got, err := svc.CreateSubscription(context.Background(), valid)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)
	if got.NextRunAt == nil || !got.NextRunAt.Equal(want) {
		t.Fatalf("next run = %v, want %v", got.NextRunAt, want)
	}
	for _, raw := range []string{`{"hour":24}`, `{"hour":-1}`, `{"minute":60}`, `{"hour":"nine"}`, `[]`} {
		bad := valid
		bad.Schedule = datatypes.JSON(raw)
		if _, err := svc.CreateSubscription(context.Background(), bad); err == nil {
			t.Errorf("accepted invalid schedule %s", raw)
		}
	}
}

func TestTopicRadarHistoryScopesBeforeLimitAndMarksMissing(t *testing.T) {
	for _, ownHistory := range []bool{false, true} {
		t.Run(fmt.Sprintf("owned=%t", ownHistory), func(t *testing.T) {
			repo := topicRadarTestRepository(t)
			ctx := context.Background()
			sub := seedTopicRadarSubscription(t, repo)
			for i := 0; i < 101; i++ {
				if err := repo.TopicPools().Create(ctx, &model.TopicPool{UserID: "another-user", ProjectID: sub.ProjectID, Topic: fmt.Sprintf("foreign-%d", i), Status: model.TopicStatusUnused, ContentAssetID: topicStringPtr(fmt.Sprintf("foreign-asset-%d", i))}); err != nil {
					t.Fatal(err)
				}
			}
			if ownHistory {
				if err := repo.TopicPools().Create(ctx, &model.TopicPool{UserID: sub.UserID, ProjectID: sub.ProjectID, Topic: "owned-history", Status: model.TopicStatusUnused, ContentAssetID: topicStringPtr("source-asset")}); err != nil {
					t.Fatal(err)
				}
			}
			svc := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
			digest, err := svc.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID, RequestKey: "scoped-history"})
			if err != nil {
				t.Fatal(err)
			}
			var frozen topicFrozenContext
			if err := json.Unmarshal(digest.InputSnapshot, &frozen); err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, ref := range frozen.SourceRefs {
				if ref.Type == "asset" {
					count++
					if ref.Title != "owned-history" {
						t.Fatalf("foreign history leaked: %+v", ref)
					}
				}
			}
			want := 0
			if ownHistory {
				want = 1
			}
			if count != want {
				t.Fatalf("owned history count=%d want=%d", count, want)
			}
			if missing := slices.Contains(frozen.MissingFields, "historical_topics"); missing == ownHistory {
				t.Fatalf("historical_topics missing=%t owned=%t", missing, ownHistory)
			}
		})
	}
}
