package service

import (
	"context"
	"errors"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestUnifiedTopicLiveLinkProjectsUsageAndCanonicalTask(t *testing.T) {
	for _, tc := range []struct {
		name, typ, relation string
		used, associated    bool
	}{
		{"article", model.TaskTypeWechatArticle, model.ContentAssetLinkDerivative, true, true},
		{"reference", model.TaskTypeWechatArticle, model.ContentAssetLinkReference, false, false},
		{"research", model.TaskTypeContentResearch, model.ContentAssetLinkDerivative, false, true},
		{"script", model.TaskTypeContentScript, model.ContentAssetLinkDerivative, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			db := repo.Analytics().DB()
			project := createTestProject(t, repo, "owner", model.PlatformWechat)
			svc := NewContentAssetService(repo)
			asset := model.NewContentAsset("owner", "canonical")
			if err := repo.ContentAssets().Create(ctx, asset); err != nil {
				t.Fatal(err)
			}
			historical := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)
			task := &model.Task{ID: "result", UserID: "owner", ProjectID: project, Type: tc.typ, Status: model.TaskStatusCompleted, CreatedAt: historical}
			if err := db.Create(task).Error; err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if _, err := svc.LinkContent(ctx, LinkContentAssetParams{UserID: "owner", AssetID: asset.ID, ContentKind: "task", ContentID: task.ID, Relation: tc.relation}); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.ContentAssets().PopulateTopicSummaries(ctx, "owner", []*model.ContentAsset{asset}); err != nil {
				t.Fatal(err)
			}
			want := model.TopicStatusUnused
			if tc.used {
				want = model.TopicStatusUsed
			}
			if len(asset.ProjectUses) != 1 || asset.ProjectUses[0].Status != want {
				t.Fatalf("uses=%+v", asset.ProjectUses)
			}
			if tc.used && (asset.ProjectUses[0].UsedAt == nil || !asset.ProjectUses[0].UsedAt.Equal(historical)) {
				t.Fatalf("historical use timestamp lost: %+v", asset.ProjectUses)
			}
			got, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (got.ContentAssetID == asset.ID) != tc.associated {
				t.Fatalf("task association=%s", got.ContentAssetID)
			}
			if tc.associated && len(asset.Outputs) != 1 {
				t.Fatalf("outputs=%+v", asset.Outputs)
			}
		})
	}
}

func TestUnifiedTopicUnusedFilterUsesEffectivePersonaStatus(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	db := repo.Analytics().DB()
	p := createTestProject(t, repo, "owner", model.PlatformWechat)
	other := createTestProject(t, repo, "owner", model.PlatformWechat)
	asset := model.NewContentAsset("owner", "duplicates")
	if err := db.Create(asset).Error; err != nil {
		t.Fatal(err)
	}
	used := &model.TopicPool{UserID: "owner", ProjectID: p, ContentAssetID: &asset.ID, Topic: asset.Title, Status: model.TopicStatusUsed}
	if err := db.Create(used).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(used).UpdateColumn("dedupe_key", nil).Error; err != nil {
		t.Fatal(err)
	}
	unused := &model.TopicPool{UserID: "owner", ProjectID: p, ContentAssetID: &asset.ID, Topic: asset.Title, Status: model.TopicStatusUnused}
	if err := db.Create(unused).Error; err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{p, ""} {
		_, n, err := repo.ContentAssets().ListWithFilters(ctx, repository.ContentAssetListFilter{UserID: "owner", ProjectID: project, Usage: "unused", Limit: 25})
		if err != nil || n != 0 {
			t.Fatalf("project=%s count=%d err=%v", project, n, err)
		}
	}
	if err := db.Create(&model.TopicPool{UserID: "owner", ProjectID: other, ContentAssetID: &asset.ID, Topic: asset.Title, Status: model.TopicStatusUnused}).Error; err != nil {
		t.Fatal(err)
	}
	_, n, err := repo.ContentAssets().ListWithFilters(ctx, repository.ContentAssetListFilter{UserID: "owner", Usage: "unused", Limit: 25})
	if err != nil || n != 1 {
		t.Fatalf("mixed personas count=%d err=%v", n, err)
	}
}

func TestUnifiedTopicDiscoveryKeepsCanonicalTaskAndSourceAfterRename(t *testing.T) {
	for _, mode := range []string{"task", "source"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			db := repo.Analytics().DB()
			project := createTestProject(t, repo, "owner", model.PlatformWechat)
			asset := model.NewContentAsset("owner", "topic before publication")
			if err := db.Create(asset).Error; err != nil {
				t.Fatal(err)
			}
			task := &model.Task{ID: "published-task", UserID: "owner", ProjectID: project, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusCompleted, ContentAssetID: asset.ID}
			if mode == "task" {
				if err := db.Create(task).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Create(&model.ContentAssetLink{ID: "original-source", UserID: "owner", ProjectID: project, AssetID: asset.ID, ContentKind: "wechat_publication", ContentID: "publication", Relation: model.ContentAssetLinkOrigin}).Error; err != nil {
					t.Fatal(err)
				}
			}
			for _, value := range []any{
				&model.WechatPublication{Source: model.WechatPublicationSourceAnbanAPI, ID: "publication", UserID: "owner", ProjectID: project, TaskID: task.ID, Status: model.WechatPublicationStatusPublished},
				&model.AnalyticsState{ProjectID: project, Revision: 4, ActiveGeneration: 1, Status: "ready"},
				&model.AnalyticsContent{ID: "analytics", ProjectID: project, PublicationID: "publication", TaskID: task.ID, Channel: model.ChannelArticle, Title: "completely different final headline", Status: "published"},
			} {
				if err := db.Create(value).Error; err != nil {
					t.Fatal(err)
				}
			}
			date := time.Now().UTC().Add(-24 * time.Hour)
			reads := int64(100)
			for _, grain := range []string{"day", "month"} {
				bucket := date.Format("2006-01-02")
				if grain == "month" {
					bucket = date.Format("2006-01") + "-01"
				}
				if err := db.Create(&model.AnalyticsBucket{ProjectID: project, Generation: 1, ContentID: "analytics", MetricBasis: "cumulative", Granularity: grain, BucketStart: bucket, LastStatDate: date.Format("2006-01-02"), AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &reads}}).Error; err != nil {
					t.Fatal(err)
				}
			}
			svc := NewContentAssetService(repo)
			for i := 0; i < 2; i++ {
				created, err := svc.DiscoverPublishedContentAssets(ctx, "owner", project, 4)
				if err != nil || created != 0 {
					t.Fatalf("created=%d err=%v", created, err)
				}
				asset.Title = "canonical renamed"
				if err := repo.ContentAssets().Update(ctx, asset); err != nil {
					t.Fatal(err)
				}
			}
			var count int64
			if err := db.Model(&model.ContentAsset{}).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("assets=%d %v", count, err)
			}
			evidence, err := repo.ContentAssets().ListEvidence(ctx, "owner", asset.ID)
			if err != nil || len(evidence) != 1 {
				t.Fatalf("evidence=%+v %v", evidence, err)
			}
			rows, n, err := repo.ContentAssets().ListWithFilters(ctx, repository.ContentAssetListFilter{UserID: "owner", ProjectID: project, Usage: "used", Limit: 25})
			if err != nil || n != 1 || len(rows[0].ProjectUses) != 1 {
				t.Fatalf("usage rows=%+v count=%d err=%v", rows, n, err)
			}
			if claim, err := repo.TopicPools().ClaimAssetWithTask(ctx, "owner", project, asset.ID, "again"); err != nil || claim != nil {
				t.Fatalf("published topic remained eligible: %+v %v", claim, err)
			}
		})
	}
}

func TestUnifiedTopicResearchCandidatesAttachOriginatingPersonaUnused(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	project := createTestProject(t, repo, "owner", model.PlatformWechat)
	result := model.PlanningResearchResult{PlanningIdentity: model.PlanningIdentity{Status: "ready"}, Candidates: []model.PlanningProposal{{ID: "one", Title: "new research topic"}, {ID: "two", Title: "another research topic"}}}
	evidence := model.PlanningResearchEvidence{Sources: []model.PlanningSource{{ID: "external", URL: "https://example.com/source"}}}
	for i := 0; i < 2; i++ {
		if err := repo.WithTx(ctx, func(tx repository.Repository) error {
			return applyResearchResult(ctx, tx, "owner", project, "", result, evidence)
		}); err != nil {
			t.Fatal(err)
		}
	}
	rows, n, err := repo.ContentAssets().ListWithFilters(ctx, repository.ContentAssetListFilter{UserID: "owner", ProjectID: project, Usage: "unused", Limit: 25})
	if err != nil || n != 2 {
		t.Fatalf("candidates=%+v n=%d err=%v", rows, n, err)
	}
	for _, row := range rows {
		if len(row.ProjectUses) != 1 || row.ProjectUses[0].Status != "unused" {
			t.Fatalf("used research candidate %+v", row)
		}
	}
}

func TestUnifiedTopicMigrationRepairsReverseLineageIdempotently(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	db := repo.Analytics().DB()
	project := createTestProject(t, repo, "owner", model.PlatformWechat)
	historical := time.Now().UTC().Add(-72 * time.Hour).Truncate(time.Second)
	for _, kind := range []string{"task", "link", "reference", "script", "published"} {
		asset := model.NewContentAsset("owner", kind)
		if err := db.Create(asset).Error; err != nil {
			t.Fatal(err)
		}
		task := &model.Task{ID: kind, UserID: "owner", ProjectID: project, Type: model.TaskTypeWechatArticle, CreatedAt: historical}
		if kind == "task" || kind == "script" {
			task.ContentAssetID = asset.ID
		}
		if kind == "script" {
			task.Type = model.TaskTypeContentScript
		}
		if kind != "published" {
			if err := db.Create(task).Error; err != nil {
				t.Fatal(err)
			}
		}
		if kind == "link" || kind == "reference" || kind == "published" {
			link := &model.ContentAssetLink{ID: kind, UserID: "owner", ProjectID: project, AssetID: asset.ID, ContentKind: "task", ContentID: task.ID, Relation: model.ContentAssetLinkDerivative, CreatedAt: historical}
			if kind == "reference" {
				link.Relation = model.ContentAssetLinkReference
			}
			if kind == "link" {
				link.ProjectID = ""
			}
			if kind == "published" {
				link.ContentKind = "wechat_publication"
				if err := db.Create(&model.WechatPublication{Source: model.WechatPublicationSourceAnbanAPI, ID: kind, UserID: "owner", ProjectID: project, TaskID: "imported", Status: "published", PublishedAt: &historical}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := db.Create(link).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	var original []model.TopicPool
	for i := 0; i < 2; i++ {
		if err := MigrateTopicPoolToContentAssets(ctx, db, nil); err != nil {
			t.Fatal(err)
		}
		var rows []model.TopicPool
		if err := db.Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		if len(rows) != 5 {
			t.Fatalf("queues=%+v", rows)
		}
		for n, row := range rows {
			used := row.Topic != "reference" && row.Topic != "script"
			if (row.Status == "used") != used {
				t.Fatalf("wrong use %+v", row)
			}
			if used && (row.UsedAt == nil || !row.UsedAt.Equal(historical)) {
				t.Fatalf("wrong time %+v", row)
			}
			if i == 1 && (row.ID != original[n].ID || !row.CreatedAt.Equal(original[n].CreatedAt) || !row.UpdatedAt.Equal(original[n].UpdatedAt)) {
				t.Fatalf("history changed %+v %+v", original[n], row)
			}
		}
		original = rows
	}
}

func TestUnifiedTopicReuseAdmissionRecordsUseWithoutBlockingAnotherReuse(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	project := createTestProject(t, repo, "owner", model.PlatformWechat)
	asset := model.NewContentAsset("owner", "reusable")
	if err := repo.ContentAssets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	svc := NewContentAssetService(repo)
	svc.SetEnqueuer(&contentAssetTestEnqueuer{})
	taskSvc := &TaskService{repo: repo}
	var first model.TopicProjectUse
	for _, key := range []string{"first", "second"} {
		run, err := svc.CreateReuseRun(ctx, CreateReuseRunParams{UserID: "owner", AssetID: asset.ID, TargetProjectID: project, TargetPlatform: model.PlatformWechat, IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		if key == "first" {
			if err := repo.ContentAssets().PopulateTopicSummaries(ctx, "owner", []*model.ContentAsset{asset}); err != nil {
				t.Fatal(err)
			}
			if len(asset.ProjectUses) != 1 || asset.ProjectUses[0].Status != "unused" {
				t.Fatalf("pre-production use=%+v", asset.ProjectUses)
			}
		}
		run.Status = model.ContentReuseRunStatusRunning
		if err := repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
			t.Fatal(err)
		}
		task := &model.Task{ID: key, UserID: "owner", ProjectID: project, Type: model.TaskTypeWechatArticle, AgentID: model.AgentIDArticle, ContentAssetID: asset.ID, ReuseRunID: run.ID}
		if err := taskSvc.persistTaskWithFixedAdmission(ctx, task); err != nil {
			t.Fatal(err)
		}
		if err := repo.ContentAssets().PopulateTopicSummaries(ctx, "owner", []*model.ContentAsset{asset}); err != nil {
			t.Fatal(err)
		}
		if len(asset.ProjectUses) != 1 || asset.ProjectUses[0].Status != "used" {
			t.Fatalf("production use=%+v", asset.ProjectUses)
		}
		if key == "first" {
			first = asset.ProjectUses[0]
		} else if asset.ProjectUses[0].TaskID == nil || first.TaskID == nil || *asset.ProjectUses[0].TaskID != *first.TaskID || !asset.ProjectUses[0].UsedAt.Equal(*first.UsedAt) {
			t.Fatalf("repeated reuse rewrote history: %+v", asset.ProjectUses)
		}
	}
	task := &model.Task{ID: "ordinary", UserID: "owner", ProjectID: project, Type: model.TaskTypeWechatArticle, AgentID: model.AgentIDArticle, ContentAssetID: asset.ID}
	if err := taskSvc.persistTaskWithFixedAdmission(ctx, task); err == nil {
		t.Fatal("ordinary repeat production admitted")
	}
}

func TestUnifiedTopicLinkFailureRollsBackAssociationAndUsage(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	db := repo.Analytics().DB()
	project := createTestProject(t, repo, "owner", model.PlatformWechat)
	asset := model.NewContentAsset("owner", "atomic link")
	if err := db.Create(asset).Error; err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "atomic-task", UserID: "owner", ProjectID: project, Type: model.TaskTypeWechatArticle}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("reject_lineage", func(tx *gorm.DB) {
		if tx.Statement.Table == "content_asset_links" {
			tx.AddError(errors.New("lineage persistence unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	defer db.Callback().Create().Remove("reject_lineage")
	if _, err := NewContentAssetService(repo).LinkContent(ctx, LinkContentAssetParams{UserID: "owner", AssetID: asset.ID, ContentKind: "task", ContentID: task.ID}); err == nil {
		t.Fatal("expected link failure")
	}
	got, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil || got.ContentAssetID != "" {
		t.Fatalf("task association escaped rollback: %+v %v", got, err)
	}
	var count int64
	if err := db.Model(&model.TopicPool{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("use escaped rollback: %d %v", count, err)
	}
}
