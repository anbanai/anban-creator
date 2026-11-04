package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestUnifiedTopicMergePreservesConsumptionAndFrozenLineage(t *testing.T) {
	for _, consumed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unused", true: "used"}[consumed], func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			db := repo.Analytics().DB()
			user := "owner"
			project := createTestProject(t, repo, user, model.PlatformWechat)
			svc := NewContentAssetService(repo)
			canonical, err := svc.Create(ctx, CreateContentAssetParams{UserID: user, Title: "canonical", ProjectIDs: []string{project}})
			if err != nil {
				t.Fatal(err)
			}
			source, err := svc.Create(ctx, CreateContentAssetParams{UserID: user, Title: "source", ProjectIDs: []string{project}})
			if err != nil {
				t.Fatal(err)
			}
			task := &model.Task{ID: "historical-task", UserID: user, ProjectID: project, ContentAssetID: source.ID, Prompt: "source at consumption", Type: model.TaskTypeWechatArticle}
			if err := db.Create(task).Error; err != nil {
				t.Fatal(err)
			}
			if consumed {
				if _, err := repo.TopicPools().ClaimAssetWithTask(ctx, user, project, source.ID, task.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: "lineage", UserID: user, ProjectID: project, AssetID: source.ID, ContentKind: "task", ContentID: task.ID, Relation: model.ContentAssetLinkDerivative}); err != nil {
				t.Fatal(err)
			}
			radar := NewTopicRadarService(repo, nil, nil)
			job, err := radar.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: user, ProjectID: project, AssetID: source.ID, TargetPlatform: "douyin"})
			if err != nil {
				t.Fatal(err)
			}
			frozen := string(job.InputSnapshot)
			job.Status = model.TopicScriptJobFailed
			if err := repo.TopicScriptJobs().Update(ctx, job); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				if err := repo.ContentAssets().MergeAsset(ctx, user, canonical.ID, source.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.ContentAssets().PopulateTopicSummaries(ctx, user, []*model.ContentAsset{canonical}); err != nil {
				t.Fatal(err)
			}
			want := model.TopicStatusUnused
			if consumed {
				want = model.TopicStatusUsed
			}
			if len(canonical.ProjectUses) != 1 || canonical.ProjectUses[0].Status != want {
				t.Fatalf("uses=%+v", canonical.ProjectUses)
			}
			if len(canonical.Outputs) != 2 {
				t.Fatalf("lost outputs: %+v", canonical.Outputs)
			}
			got, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil || got.ContentAssetID != canonical.ID || got.Prompt != task.Prompt {
				t.Fatalf("task=%+v err=%v", got, err)
			}
			links, err := repo.ContentAssets().ListLinks(ctx, user, canonical.ID)
			if err != nil || len(links) != 1 || links[0].ContentID != task.ID {
				t.Fatalf("links=%+v %v", links, err)
			}
			retried, err := radar.RetryScript(ctx, user, job.ID)
			if err != nil || retried.ID != job.ID || retried.ContentAssetID != canonical.ID || string(retried.InputSnapshot) != frozen {
				t.Fatalf("retry=%+v %v", retried, err)
			}
			claim, err := repo.TopicPools().ClaimAssetWithTask(ctx, user, project, canonical.ID, "second-task")
			if consumed && claim != nil {
				t.Fatalf("used topic claimed: %+v", claim)
			}
			if !consumed && (err != nil || claim == nil) {
				t.Fatalf("unused supply lost: %+v %v", claim, err)
			}
			var count int64
			if err := db.Model(&model.TopicPool{}).Where("project_id=? AND dedupe_key IS NOT NULL", project).Count(&count).Error; err != nil || count != 1 {
				t.Fatalf("dedupe rows=%d %v", count, err)
			}
		})
	}
}

func TestUnifiedTopicRenameThenReaddKeepsResearchEvidenceSeparate(t *testing.T) {
	for _, research := range []bool{false, true} {
		t.Run(map[bool]string{false: "manual", true: "research"}[research], func(t *testing.T) {
			ctx := context.Background()
			repo := topicRadarTestRepository(t)
			sub := seedTopicRadarSubscription(t, repo)
			svc := NewContentAssetService(repo)
			old, err := svc.Create(ctx, CreateContentAssetParams{UserID: sub.UserID, Title: "Old topic", ProjectIDs: []string{sub.ProjectID}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := repo.TopicPools().ClaimAssetWithTask(ctx, sub.UserID, sub.ProjectID, old.ID, "old-task"); err != nil {
				t.Fatal(err)
			}
			old.Title = "New topic"
			if err := repo.ContentAssets().Update(ctx, old); err != nil {
				t.Fatal(err)
			}
			var replacement *model.ContentAsset
			if research {
				radar := NewTopicRadarService(repo, nil, nil)
				digest, err := radar.Run(ctx, RunTopicDigestParams{UserID: sub.UserID, SubscriptionID: sub.ID})
				if err != nil {
					t.Fatal(err)
				}
				freezeTestTopicRefs(t, repo, digest)
				if err := radar.FinalizeDigest(ctx, digest.ID, topicDigestTestArtifact(t, repo, digest, []byte(`{"schema_version":"1.0","suggestions":[{"title":"Old topic","introduction":"fresh angle","source_summary":"fresh evidence","source_refs":[{"type":"search","ref_id":"trend-1"}],"scores":{},"video_fit":"video"}]}`))); err != nil {
					t.Fatal(err)
				}
				suggestions, err := repo.TopicSuggestions().ListByDigest(ctx, digest.ID)
				if err != nil || len(suggestions) != 1 {
					t.Fatalf("suggestions %v %v", suggestions, err)
				}
				replacement, err = repo.ContentAssets().FindByID(ctx, sub.UserID, suggestions[0].ContentAssetID)
				if err != nil {
					t.Fatal(err)
				}
				evidence, err := repo.ContentAssets().ListEvidence(ctx, sub.UserID, replacement.ID)
				if err != nil || len(evidence) == 0 {
					t.Fatalf("evidence=%+v %v", evidence, err)
				}
				wrong, err := repo.ContentAssets().ListEvidence(ctx, sub.UserID, old.ID)
				if err != nil || len(wrong) != 0 {
					t.Fatalf("research attached to renamed asset: %+v %v", wrong, err)
				}
			} else {
				replacement, err = svc.Create(ctx, CreateContentAssetParams{UserID: sub.UserID, Title: "Old topic", ProjectIDs: []string{sub.ProjectID}})
				if err != nil {
					t.Fatal(err)
				}
			}
			if replacement.ID == old.ID || replacement.Title != "Old topic" {
				t.Fatalf("replacement=%+v", replacement)
			}
			if _, err := repo.TopicPools().ClaimAssetWithTask(ctx, sub.UserID, sub.ProjectID, replacement.ID, "new-task"); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnifiedTopicPreTaskScriptFailureSurvivesRefresh(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	sub := seedTopicRadarSubscription(t, repo)
	asset, err := NewContentAssetService(repo).Create(ctx, CreateContentAssetParams{UserID: sub.UserID, Title: "idea", ProjectIDs: []string{sub.ProjectID}})
	if err != nil {
		t.Fatal(err)
	}
	radar := NewTopicRadarService(repo, nil, nil)
	job, err := radar.RequestTopicScript(ctx, RequestTopicScriptParams{UserID: sub.UserID, ProjectID: sub.ProjectID, AssetID: asset.ID, TargetPlatform: "douyin"})
	if err != nil {
		t.Fatal(err)
	}
	// No configured admission service: failure happens before a Task can exist.
	_ = radar.ProcessTopicScript(ctx, job.ID)
	job, err = repo.TopicScriptJobs().FindByID(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != model.TopicScriptJobFailed || job.TaskID != nil {
		t.Fatalf("job=%+v", job)
	}
	refreshed, err := NewContentAssetService(repository.New(repo.Analytics().DB())).Get(ctx, sub.UserID, asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(refreshed.Asset.Outputs) != 1 {
		t.Fatalf("outputs=%+v", refreshed.Asset.Outputs)
	}
	output := refreshed.Asset.Outputs[0]
	if output.JobID != job.ID || output.Status != "failed" || output.Error == "" || output.TaskID != "" {
		t.Fatalf("output=%+v", output)
	}
	if _, err := radar.RetryScript(ctx, sub.UserID, output.JobID); err != nil {
		t.Fatal(err)
	}
	// Once dispatch binds a task, the job remains the single script output.
	task := &model.Task{ID: "bound-script-task", UserID: sub.UserID, ProjectID: sub.ProjectID, ContentAssetID: asset.ID, Type: model.TaskTypeTopicScript}
	if err := repo.Analytics().DB().Create(task).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.Analytics().DB().Model(&model.TopicScriptJob{}).Where("id = ?", job.ID).UpdateColumn("task_id", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	refreshed, err = NewContentAssetService(repo).Get(ctx, sub.UserID, asset.ID)
	if err != nil || len(refreshed.Asset.Outputs) != 1 || refreshed.Asset.Outputs[0].TaskID != task.ID || refreshed.Asset.Outputs[0].JobID != job.ID {
		t.Fatalf("duplicated script output: %+v %v", refreshed, err)
	}
}

func TestUnifiedTopicMigrationPreservesConsumedHistoryAndExistingScriptSnapshot(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	db := repo.Analytics().DB()
	sub := seedTopicRadarSubscription(t, repo)
	now := time.Now().UTC().Truncate(time.Second)
	task := &model.Task{ID: "legacy-task", UserID: sub.UserID, ProjectID: sub.ProjectID, Prompt: "original frozen prompt", CreatedAt: now}
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: model.PlatformWechat})
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	row := &model.TopicPool{UserID: sub.UserID, ProjectID: sub.ProjectID, Topic: "legacy topic", Status: model.TopicStatusUsed, TaskID: &task.ID, UsedAt: &now, CreatedAt: now}
	if err := db.Create(row).Error; err != nil {
		t.Fatal(err)
	}
	digest := &model.TopicDigest{ID: "legacy-digest", UserID: sub.UserID, ProjectID: sub.ProjectID, SubscriptionID: sub.ID, Status: model.TopicDigestStatusSucceeded, OccurrenceKey: "legacy", RequestKey: "legacy", InputSnapshot: []byte(`{"original":true}`)}
	if err := db.Create(digest).Error; err != nil {
		t.Fatal(err)
	}
	suggestion := &model.TopicSuggestion{ID: "legacy-suggestion", DigestID: digest.ID, ProjectID: sub.ProjectID, Title: "legacy topic", SourceRefs: []byte(`[]`), Scores: []byte(`{}`)}
	if err := db.Create(suggestion).Error; err != nil {
		t.Fatal(err)
	}
	frozen := []byte(`{"selected_topic":{"title":"original script input"},"snapshot":{"snapshot_hash":"existing-hash"}}`)
	job := &model.TopicScriptJob{ID: "legacy-script", UserID: sub.UserID, ProjectID: sub.ProjectID, DigestID: digest.ID, SuggestionID: suggestion.ID, TaskID: &task.ID, Fingerprint: "legacy-fingerprint", Status: model.TopicScriptJobSucceeded, InputSnapshot: frozen, CreatedAt: now}
	if err := db.Create(job).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := MigrateTopicPoolToContentAssets(ctx, db, nil); err != nil {
			t.Fatal(err)
		}
	}
	migrated, err := repo.TopicPools().FindByID(ctx, row.ID)
	if err != nil || migrated.Status != model.TopicStatusUsed || migrated.TaskID == nil || *migrated.TaskID != task.ID || !migrated.UsedAt.Equal(now) || migrated.Topic != row.Topic {
		t.Fatalf("history=%+v %v", migrated, err)
	}
	got, err := repo.TopicScriptJobs().FindByID(ctx, job.ID)
	if err != nil || string(got.InputSnapshot) != string(frozen) || got.ContentAssetID != *migrated.ContentAssetID || *got.TaskID != task.ID {
		t.Fatalf("job=%+v %v", got, err)
	}
	gotTask, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil || gotTask.Prompt != task.Prompt || gotTask.ContentAssetID != got.ContentAssetID || gotTask.ProjectSnapshot.Data().Platform != model.PlatformWechat {
		t.Fatalf("task=%+v %v", gotTask, err)
	}
	var snap map[string]any
	if err := json.Unmarshal(got.InputSnapshot, &snap); err != nil {
		t.Fatal(err)
	}
	var assets, links, queues int64
	db.Model(&model.ContentAsset{}).Count(&assets)
	db.Model(&model.ContentAssetLink{}).Count(&links)
	db.Model(&model.TopicPool{}).Count(&queues)
	if assets != 1 || links != 1 || queues != 1 {
		t.Fatalf("migration duplicated history: assets=%d links=%d queues=%d", assets, links, queues)
	}
}

func TestUnifiedTopicMergePreservesFrozenResearchAndReuseBindings(t *testing.T) {
	ctx := context.Background()
	repo := topicRadarTestRepository(t)
	db := repo.Analytics().DB()
	user := "owner"
	project := createTestProject(t, repo, user, model.PlatformWechat)
	svc := NewContentAssetService(repo)
	source, err := svc.Create(ctx, CreateContentAssetParams{UserID: user, Title: "source"})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := svc.Create(ctx, CreateContentAssetParams{UserID: user, Title: "canonical"})
	if err != nil {
		t.Fatal(err)
	}
	frozen := map[string]any{"candidates": []map[string]any{{"asset_id": source.ID, "title": "frozen title"}}}
	research, err := svc.EnsureResearchJob(ctx, user, project, source.ID, "frozen-research-key", 4, 2, frozen)
	if err != nil {
		t.Fatal(err)
	}
	originalResearch := string(research.FrozenContext)
	originalHash := research.FrozenContextSHA256
	run := model.NewContentReuseRun(user, source.ID, project, model.PlatformWechat)
	run.RequestFingerprint = "frozen-original-fingerprint"
	run.PolicySnapshot = []byte(`{"revision":2}`)
	if err := db.Create(run).Error; err != nil {
		t.Fatal(err)
	}
	stage := &model.ContentReuseStageExecution{ID: "frozen-stage", RunID: run.ID, Stage: model.ContentReuseStageResearch, InputSnapshot: []byte(`{"candidates":[{"asset_id":"historical-source"}]}`), InputSHA256: "frozen-stage-hash"}
	if err := db.Create(stage).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ContentAssets().MergeAsset(ctx, user, canonical.ID, source.ID); err != nil {
		t.Fatal(err)
	}
	again, err := svc.EnsureResearchJob(ctx, user, project, source.ID, "frozen-research-key", 4, 2, frozen)
	if err != nil || again.ID != research.ID || string(again.FrozenContext) != originalResearch || again.FrozenContextSHA256 != originalHash {
		t.Fatalf("frozen request changed: %+v %v", again, err)
	}
	var gotStage model.ContentReuseStageExecution
	if err := db.First(&gotStage, "id = ?", stage.ID).Error; err != nil {
		t.Fatal(err)
	}
	if string(gotStage.InputSnapshot) != string(stage.InputSnapshot) || gotStage.InputSHA256 != stage.InputSHA256 {
		t.Fatalf("stage rewritten: %+v", gotStage)
	}
	gotRun, err := repo.ContentAssets().FindReuseRunByID(ctx, run.ID)
	if err != nil || gotRun.AssetID != canonical.ID || gotRun.RequestFingerprint != run.RequestFingerprint || string(gotRun.PolicySnapshot) != string(run.PolicySnapshot) {
		t.Fatalf("run=%+v %v", gotRun, err)
	}
}
