package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestUnifiedTopicAdmissionRollsBackClaimAndPreservesLineage(t *testing.T) {
	svc, pool := setupTaskServiceWithTopicPool(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, svc.repo, userID, model.PlatformWechat)
	rows, err := pool.Add(ctx, userID, projectID, []string{"canonical topic"})
	if err != nil {
		t.Fatal(err)
	}
	assetID := *rows[0].ContentAssetID
	p := CreateManualParams{UserID: userID, ProjectID: projectID, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective", ContentAssetID: assetID, Quantity: 1}
	db := svc.repo.Analytics().DB()
	if err := db.Callback().Create().Before("gorm:create").Register("fail_topic_task", func(tx *gorm.DB) {
		if tx.Statement.Table == "tasks" {
			tx.AddError(errors.New("task save failed"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateManual(ctx, p); err == nil {
		t.Fatal("expected persistence failure")
	}
	db.Callback().Create().Remove("fail_topic_task")
	pending, total, err := pool.List(ctx, userID, projectID, model.TopicStatusUnused, 0, 10)
	if err != nil || total != 1 || pending[0].TaskID != nil {
		t.Fatalf("failed save consumed topic: %+v %d %v", pending, total, err)
	}
	tasks, err := svc.CreateManual(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if tasks[0].ContentAssetID != assetID || tasks[0].Prompt != "canonical topic" {
		t.Fatalf("lost topic: %+v", tasks[0])
	}
	links, err := svc.repo.ContentAssets().ListLinks(ctx, userID, assetID)
	if err != nil || len(links) != 1 || links[0].ContentID != tasks[0].ID {
		t.Fatalf("lineage=%+v err=%v", links, err)
	}
	// Renaming the canonical topic must not reset persona consumption.
	asset, err := svc.repo.ContentAssets().FindByID(ctx, userID, assetID)
	if err != nil {
		t.Fatal(err)
	}
	asset.Title = "renamed topic"
	if err := svc.repo.ContentAssets().Update(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateManual(ctx, p); err == nil {
		t.Fatal("second request consumed the renamed asset again")
	}
	claimed, err := pool.ClaimTopic(ctx, ClaimTopicRequest{UserID: userID, ProjectID: projectID, TaskID: tasks[0].ID})
	if err != nil || claimed.AssetID != assetID || claimed.Topic != "canonical topic" {
		t.Fatalf("frozen repeated claim=%+v %v", claimed, err)
	}
	p.UserID = uuid.NewString()
	p.ProjectID = createTestProject(t, svc.repo, p.UserID, model.PlatformWechat)
	if _, err := svc.CreateManual(ctx, p); err == nil {
		t.Fatal("foreign asset accepted")
	}
}

func TestUnifiedTopicUsageIsPersonaScopedAndArchiveStopsSupply(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	userID := "topic-user"
	p1 := createTestProject(t, repo, userID, model.PlatformWechat)
	p2 := createTestProject(t, repo, userID, model.PlatformSeednote)
	assets := NewContentAssetService(repo)
	asset, err := assets.Create(ctx, CreateContentAssetParams{UserID: userID, Title: "shared", ProjectIDs: []string{p1, p2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.TopicPools().ClaimAssetWithTask(ctx, userID, p1, asset.ID, "task-one"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		project, usage string
		want           int64
	}{{"", "unused", 1}, {"", "used", 1}, {p1, "unused", 0}, {p1, "used", 1}, {p2, "unused", 1}, {p2, "used", 0}} {
		rows, total, err := assets.ListFiltered(ctx, ContentAssetListParams{UserID: userID, ProjectID: tc.project, Usage: tc.usage, Limit: 25})
		if err != nil || total != tc.want {
			t.Fatalf("%+v: %d %v", tc, total, err)
		}
		if total > 0 && len(rows[0].ProjectUses) != 2 {
			t.Fatalf("missing persona summaries: %+v", rows[0])
		}
	}
	if _, err := assets.Archive(ctx, userID, asset.ID); err != nil {
		t.Fatal(err)
	}
	if row, err := repo.TopicPools().ClaimWithTask(ctx, userID, p2, "task-two"); err != nil || row != nil {
		t.Fatalf("archived topic claimed: %+v %v", row, err)
	}
	stats, err := repo.Projects().GetStatsByProjectIDs(ctx, []string{p1, p2})
	if err != nil || stats[p2].UnusedTopics != 0 {
		t.Fatalf("counts=%+v %v", stats, err)
	}
}

func TestUnifiedTopicInsufficientAdmissionDoesNotConsume(t *testing.T) {
	ctx := context.Background()
	svc, fixture, _ := newFixedTaskBillingFixture(t, 0, 0)
	projectID := createTestProject(t, fixture.repo, billingWalletUserID, model.PlatformWechat)
	pool := NewTopicPoolService(fixture.repo, nil)
	svc.SetTopicPoolService(pool)
	if _, err := pool.Add(ctx, billingWalletUserID, projectID, []string{"reserved after payment only"}); err != nil {
		t.Fatal(err)
	}
	plan := &model.Plan{ID: uuid.NewString(), UserID: billingWalletUserID, ProjectID: projectID, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective", Status: model.PlanStatusActive}
	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil || task != nil {
		t.Fatalf("insufficient balance: %+v %v", task, err)
	}
	_, total, err := pool.List(ctx, billingWalletUserID, projectID, model.TopicStatusUnused, 0, 10)
	if err != nil || total != 1 {
		t.Fatalf("balance rejection consumed topic: %d %v", total, err)
	}
}

func TestUnifiedManualTopicScriptFreezesInputWithoutConsuming(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	sub := seedTopicRadarSubscription(t, repo)
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: sub.UserID, ProjectIDs: []string{sub.ProjectID}, Title: "manual idea", CoreQuestion: "original angle"})
	if err != nil {
		t.Fatal(err)
	}
	radar := NewTopicRadarService(repo, &topicRadarTestEnqueuer{}, nil)
	request := RequestTopicScriptParams{UserID: sub.UserID, ProjectID: sub.ProjectID, AssetID: asset.ID, TargetPlatform: "douyin"}
	job, err := radar.RequestTopicScript(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	frozen := string(job.InputSnapshot)
	asset.CoreQuestion = "edited"
	if err := repo.ContentAssets().Update(ctx, asset); err != nil {
		t.Fatal(err)
	}
	job.Status = model.TopicScriptJobFailed
	if err := repo.TopicScriptJobs().Update(ctx, job); err != nil {
		t.Fatal(err)
	}
	again, err := radar.RequestTopicScript(ctx, request)
	if err != nil || again.ID != job.ID || string(again.InputSnapshot) != frozen {
		t.Fatalf("script retry replaced snapshot: %+v %v", again, err)
	}
	rows, total, err := repo.TopicPools().FindByProject(ctx, sub.ProjectID, model.TopicStatusUnused, 0, 10)
	if err != nil || total != 1 || *rows[0].ContentAssetID != asset.ID {
		t.Fatalf("script consumed topic: %+v %v", rows, err)
	}
}

func TestTopicResearchAcceptsReceiptBeyondFirstHundred(t *testing.T) {
	repo := topicRadarTestRepository(t)
	ctx := context.Background()
	sub := seedTopicRadarSubscription(t, repo)
	radar := NewTopicRadarService(repo, nil, nil)
	digest, err := radar.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
	if err != nil {
		t.Fatal(err)
	}
	body := topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"last receipt","introduction":"angle","source_summary":"source","source_refs":[{"type":"search","ref_id":"receipt-100","url":"https://example.test/latest"}],"scores":{},"video_fit":"video"}]}`))
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("receipt-%03d", i)
		if err := repo.Analytics().DB().Create(&model.SearchOperation{ID: id, UserID: sub.UserID, ProjectID: sub.ProjectID, TaskID: *digest.AgentTaskID, ExecutionID: *digest.AgentExecutionID, Operation: "mcp.search_web", Status: model.SearchOperationSucceeded, RequestFingerprint: id, ResponseSnapshot: []byte(`{"results":[{"title":"source","url":"https://example.test/latest"}]}`)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := radar.FinalizeDigest(ctx, digest.ID, body); err != nil {
		t.Fatal(err)
	}
}
