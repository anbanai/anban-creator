package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestWechatProjectReconcileLeaseHasOneConcurrentWinnerAndExpires(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repos := []Repository{New(db), New(db)}
	start := make(chan struct{})
	results := make(chan bool, len(repos))
	errs := make(chan error, len(repos))
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Add(1)
		go func(repo Repository) {
			defer wg.Done()
			<-start
			won, err := repo.WechatPublications().ClaimProjectReconcile(context.Background(), "project-1", time.Minute)
			results <- won
			errs <- err
		}(repo)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	winners := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for won := range results {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("lease winners=%d, want 1", winners)
	}
	if err := db.Model(&model.WechatProjectReconcileLease{}).Where("project_id = ?", "project-1").Update("lease_until_micros", int64(0)).Error; err != nil {
		t.Fatal(err)
	}
	if won, err := repos[0].WechatPublications().ClaimProjectReconcile(context.Background(), "project-1", time.Minute); err != nil || !won {
		t.Fatalf("expired lease claim=%v, %v", won, err)
	}
}

func TestWechatProjectReconcileLeasePreservesSQLiteSubsecondDuration(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	deadline := time.Now().Add(2 * time.Second)
	for {
		nanosecond := time.Now().Nanosecond()
		if nanosecond >= 600_000_000 && nanosecond <= 800_000_000 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("did not reach subsecond lease test window")
		}
		time.Sleep(2 * time.Millisecond)
	}

	before := time.Now()
	won, err := repo.WechatPublications().ClaimProjectReconcile(context.Background(), "precision-project", time.Minute)
	after := time.Now()
	if err != nil || !won {
		t.Fatalf("claim=%v err=%v", won, err)
	}
	var lease model.WechatProjectReconcileLease
	if err := db.Where("project_id = ?", "precision-project").First(&lease).Error; err != nil {
		t.Fatal(err)
	}
	leaseUntil := time.UnixMicro(lease.LeaseUntilMicros)
	if leaseUntil.Before(before.Add(59*time.Second + 800*time.Millisecond)) {
		t.Fatalf("lease expires early: before=%v until=%v duration=%v", before, leaseUntil, leaseUntil.Sub(before))
	}
	if leaseUntil.After(after.Add(60*time.Second + 200*time.Millisecond)) {
		t.Fatalf("lease expires too late: after=%v until=%v duration=%v", after, leaseUntil, leaseUntil.Sub(after))
	}
}

func TestWechatArticleBindingHasOneConcurrentWinnerPerProject(t *testing.T) {
	db := setupTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	repoA, repoB := New(db), New(db)
	publications := []*model.WechatPublication{
		{ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted},
		{ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted},
		{ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-2", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted},
	}
	for _, publication := range publications {
		if err := repoA.WechatPublications().Create(context.Background(), publication); err != nil {
			t.Fatal(err)
		}
	}

	start := make(chan struct{})
	results := make(chan bool, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for index, repo := range []Repository{repoA, repoB} {
		wg.Add(1)
		go func(repo Repository, publicationID string) {
			defer wg.Done()
			<-start
			won, err := repo.WechatPublications().ClaimArticleBinding(context.Background(), "project-1", "article-1", publicationID)
			results <- won
			errs <- err
		}(repo, publications[index].ID)
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	winners := 0
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for won := range results {
		if won {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("binding winners=%d, want 1", winners)
	}
	bound, err := repoA.WechatPublications().FindByArticleID(context.Background(), "project-1", "article-1")
	if err != nil || (bound.ID != publications[0].ID && bound.ID != publications[1].ID) {
		t.Fatalf("bound publication=%#v, %v", bound, err)
	}
	if won, err := repoA.WechatPublications().ClaimArticleBinding(context.Background(), "project-2", "article-1", publications[2].ID); err != nil || !won {
		t.Fatalf("cross-project binding=%v, %v", won, err)
	}
}

func TestWechatPreflightTransitionsRequireExpectedStatusAndVersion(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	t.Run("candidate status mismatch", func(t *testing.T) {
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		stale, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		publication.Status = model.WechatPublicationStatusPublished
		publication.ArticleID = "winner"
		if err := repo.WechatPublications().Update(ctx, publication); err != nil {
			t.Fatal(err)
		}

		nextCheckAt := time.Now()
		won, err := repo.WechatPublications().TransitionToNeedsSelection(
			ctx, stale.ID, stale.Status, stale.UpdatedAt, model.WechatPublicationSourceWechatConsole, []byte(`[{"article_id":"candidate"}]`), &nextCheckAt,
		)
		if err != nil || won {
			t.Fatalf("transition won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusPublished || stored.ArticleID != "winner" {
			t.Fatalf("stored=%#v err=%v", stored, err)
		}
	})

	t.Run("missing draft version mismatch", func(t *testing.T) {
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		stale, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		newVersion := stale.UpdatedAt.Add(time.Second)
		if err := db.Model(&model.WechatPublication{}).Where("id = ?", stale.ID).UpdateColumn("updated_at", newVersion).Error; err != nil {
			t.Fatal(err)
		}

		nextCheckAt := time.Now()
		won, err := repo.WechatPublications().TransitionToPublishSubmitting(
			ctx, stale.ID, stale.Status, stale.UpdatedAt, "draft disappeared", &nextCheckAt,
		)
		if err != nil || won {
			t.Fatalf("transition won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusDrafted || stored.LastError != "" {
			t.Fatalf("stored=%#v err=%v", stored, err)
		}
	})
}

func TestWechatReconciliationTransitionsAreNarrowAndVersionGuarded(t *testing.T) {
	db := setupTestDB(t)
	repo := New(db)
	ctx := context.Background()

	t.Run("draft recovery records provider media and releases its claim", func(t *testing.T) {
		claimedAt := time.Now().Add(-time.Minute)
		retryAuthorizedAt := claimedAt.Add(-time.Minute)
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafting,
			LastError: "ambiguous draft/add outcome", ClaimToken: "draft-claim", ClaimedAt: &claimedAt,
			DraftRetryAuthorizedAt: &retryAuthorizedAt,
			WechatStatusCode:       48001,
			PublishID:              "preserved-publish-id", MsgDataID: "preserved-msg-data-id",
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		lastCheckedAt := time.Now()
		nextCheckAt := lastCheckedAt.Add(10 * time.Minute)
		won, err := repo.WechatPublications().TransitionDraftRecovered(
			ctx, stored.ID, stored.UpdatedAt, "recovered-draft-media", &nextCheckAt, &lastCheckedAt,
		)
		if err != nil || !won {
			t.Fatalf("TransitionDraftRecovered won=%v err=%v", won, err)
		}
		stored, err = repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.DraftMediaID != "recovered-draft-media" || stored.Status != model.WechatPublicationStatusDrafted || stored.LastError != "" ||
			stored.WechatStatusCode != 0 ||
			stored.NextCheckAt == nil || !stored.NextCheckAt.Equal(nextCheckAt) || stored.LastCheckedAt == nil || !stored.LastCheckedAt.Equal(lastCheckedAt) ||
			stored.ClaimToken != "" || stored.ClaimedAt != nil || stored.DraftRetryAuthorizedAt != nil ||
			stored.PublishID != "preserved-publish-id" || stored.MsgDataID != "preserved-msg-data-id" {
			t.Fatalf("stored=%#v", stored)
		}
	})

	t.Run("draft recovery rejects a stale version", func(t *testing.T) {
		claimedAt := time.Now().Add(-time.Minute)
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafting,
			LastError: "ambiguous draft/add outcome", ClaimToken: "newer-draft-claim", ClaimedAt: &claimedAt,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		stale, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.WechatPublication{}).Where("id = ?", publication.ID).Update("last_error", "newer reconciliation state").Error; err != nil {
			t.Fatal(err)
		}
		lastCheckedAt := time.Now()
		nextCheckAt := lastCheckedAt.Add(10 * time.Minute)
		won, err := repo.WechatPublications().TransitionDraftRecovered(
			ctx, stale.ID, stale.UpdatedAt, "stale-draft-media", &nextCheckAt, &lastCheckedAt,
		)
		if err != nil || won {
			t.Fatalf("TransitionDraftRecovered won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.DraftMediaID != "" || stored.Status != model.WechatPublicationStatusDrafting || stored.LastError != "newer reconciliation state" ||
			stored.ClaimToken != "newer-draft-claim" || stored.ClaimedAt == nil {
			t.Fatalf("stored=%#v", stored)
		}
	})

	t.Run("unsupported draft retry is version guarded and has one winner", func(t *testing.T) {
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusUnsupported,
			WechatStatusCode: 48001, LastError: "missing draft permission",
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		stale, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		claimedAt := time.Now()
		won, err := repo.WechatPublications().RetryUnsupportedDraft(ctx, stale.ID, stale.UpdatedAt, claimedAt, "retry-claim")
		if err != nil || !won {
			t.Fatalf("RetryUnsupportedDraft won=%v err=%v", won, err)
		}
		won, err = repo.WechatPublications().RetryUnsupportedDraft(ctx, stale.ID, stale.UpdatedAt, claimedAt.Add(time.Second), "stale-claim")
		if err != nil || won {
			t.Fatalf("stale RetryUnsupportedDraft won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusDrafting || stored.WechatStatusCode != 0 || stored.LastError != "" ||
			stored.ClaimToken != "retry-claim" || stored.ClaimedAt == nil || !stored.ClaimedAt.Equal(claimedAt) {
			t.Fatalf("stored=%#v err=%v", stored, err)
		}
	})

	t.Run("draft add write-ahead marker includes its recovery boundary", func(t *testing.T) {
		claimedAt := time.Now().Add(-time.Minute)
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafting,
			ClaimToken: "draft-add-claim", ClaimedAt: &claimedAt,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		attemptedAt := time.Now()
		nextCheckAt := attemptedAt.Add(10 * time.Minute)
		won, err := repo.WechatPublications().MarkDraftAddAttempted(ctx, publication.ID, "draft-add-claim", attemptedAt, nextCheckAt)
		if err != nil || !won {
			t.Fatalf("MarkDraftAddAttempted won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.DraftAddAttemptedAt == nil || !stored.DraftAddAttemptedAt.Equal(attemptedAt) ||
			stored.DraftAddAttempts != 1 ||
			stored.NextCheckAt == nil || !stored.NextCheckAt.Equal(nextCheckAt) ||
			stored.DraftCreatedAt == nil || !stored.DraftCreatedAt.Equal(attemptedAt) {
			t.Fatalf("write-ahead state = %#v err=%v", stored, err)
		}
	})

	t.Run("authorized draft retry preserves the first attempt and is bounded", func(t *testing.T) {
		firstAttempt := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
		retryAttempt := firstAttempt.Add(time.Hour)
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafting,
			DraftAddAttemptedAt: &firstAttempt, DraftAddAttempts: 1, DraftRetryAuthorizedAt: &retryAttempt,
			ClaimToken: "retry-claim", ClaimedAt: &retryAttempt,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}

		won, err := repo.WechatPublications().MarkDraftAddAttempted(ctx, publication.ID, "retry-claim", retryAttempt, retryAttempt.Add(10*time.Minute))
		if err != nil || !won {
			t.Fatalf("MarkDraftAddAttempted retry won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.DraftAddAttemptedAt == nil || !stored.DraftAddAttemptedAt.Equal(firstAttempt) || stored.DraftAddAttempts != 2 {
			t.Fatalf("stored retry evidence = %#v", stored)
		}
		won, err = repo.WechatPublications().MarkDraftAddAttempted(ctx, publication.ID, "retry-claim", retryAttempt.Add(time.Minute), retryAttempt.Add(11*time.Minute))
		if err != nil || won {
			t.Fatalf("third MarkDraftAddAttempted won=%v err=%v", won, err)
		}
	})

	t.Run("reconciliation CAS loss preserves newer publish claim", func(t *testing.T) {
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			DraftMediaID: "draft-1", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		stale, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if won, err := repo.WechatPublications().ClaimPublish(ctx, publication.ID, "newer-claim", now, now.Add(-time.Minute), &now); err != nil || !won {
			t.Fatalf("ClaimPublish won=%v err=%v", won, err)
		}
		stale.Status = model.WechatPublicationStatusNeedsSelection
		stale.Source = model.WechatPublicationSourceWechatConsole
		stale.Candidates = []byte(`[{"article_id":"stale"}]`)
		won, err := repo.WechatPublications().UpdateReconciliation(ctx, stale, model.WechatPublicationStatusDrafted, stale.UpdatedAt)
		if err != nil || won {
			t.Fatalf("UpdateReconciliation won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusPublishSubmitting || stored.ClaimToken != "newer-claim" || stored.SubmitAttemptedAt == nil || stored.Source != model.WechatPublicationSourceAnbanAPI {
			t.Fatalf("stored=%#v err=%v", stored, err)
		}
	})

	t.Run("reconciliation update cannot clear provider identity or claim evidence", func(t *testing.T) {
		now := time.Now()
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			DraftMediaID: "draft-2", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusPublishSubmitting,
			PublishID: "publish-2", MsgDataID: "msg-data-2", ClaimToken: "claim-2", ClaimedAt: &now, SubmitAttemptedAt: &now,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		desired, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		expectedUpdatedAt := desired.UpdatedAt
		desired.Status, desired.Source = model.WechatPublicationStatusNeedsSelection, model.WechatPublicationSourceWechatConsole
		desired.PublishID, desired.MsgDataID, desired.ClaimToken, desired.ClaimedAt, desired.SubmitAttemptedAt = "", "", "", nil, nil
		desired.Candidates = []byte(`[{"article_id":"candidate"}]`)
		won, err := repo.WechatPublications().UpdateReconciliation(ctx, desired, model.WechatPublicationStatusPublishSubmitting, expectedUpdatedAt)
		if err != nil || !won {
			t.Fatalf("UpdateReconciliation won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusNeedsSelection || stored.PublishID != "publish-2" || stored.MsgDataID != "msg-data-2" || stored.ClaimToken != "claim-2" || stored.ClaimedAt == nil || stored.SubmitAttemptedAt == nil {
			t.Fatalf("stored=%#v err=%v", stored, err)
		}
	})

	t.Run("published transition is narrow and version guarded", func(t *testing.T) {
		now := time.Now()
		publication := &model.WechatPublication{
			ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1",
			DraftMediaID: "draft-3", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusPublishSubmitting,
			PublishID: "publish-3", MsgDataID: "msg-data-3", ClaimToken: "claim-3", ClaimedAt: &now, SubmitAttemptedAt: &now,
		}
		if err := repo.WechatPublications().Create(ctx, publication); err != nil {
			t.Fatal(err)
		}
		desired, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil {
			t.Fatal(err)
		}
		expectedUpdatedAt := desired.UpdatedAt
		desired.Status, desired.Source = model.WechatPublicationStatusPublished, model.WechatPublicationSourceAnbanAPI
		desired.ArticleID, desired.ArticleURL, desired.ArticleIndex, desired.PublishedAt = "article-3", "https://mp.weixin.qq.com/s/article-3", 1, &now
		desired.PublishID, desired.MsgDataID, desired.ClaimToken, desired.ClaimedAt, desired.SubmitAttemptedAt = "", "", "", nil, nil
		won, err := repo.WechatPublications().TransitionToPublished(ctx, desired, model.WechatPublicationStatusPublishSubmitting, expectedUpdatedAt)
		if err != nil || !won {
			t.Fatalf("TransitionToPublished won=%v err=%v", won, err)
		}
		stored, err := repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusPublished || stored.ArticleID != "article-3" || stored.PublishID != "publish-3" || stored.MsgDataID != "msg-data-3" || stored.ClaimToken != "claim-3" || stored.ClaimedAt == nil || stored.SubmitAttemptedAt == nil {
			t.Fatalf("stored=%#v err=%v", stored, err)
		}

		stale := *desired
		stale.UpdatedAt = expectedUpdatedAt
		stale.ArticleID = "stale-article"
		won, err = repo.WechatPublications().TransitionToPublished(ctx, &stale, model.WechatPublicationStatusPublishSubmitting, expectedUpdatedAt)
		if err != nil || won {
			t.Fatalf("stale TransitionToPublished won=%v err=%v", won, err)
		}
		stored, err = repo.WechatPublications().FindByID(ctx, publication.ID)
		if err != nil || stored.ArticleID != "article-3" {
			t.Fatalf("stale transition changed stored=%#v err=%v", stored, err)
		}
	})
}
