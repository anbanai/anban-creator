package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newWechatTrackingRepositoryTest(t *testing.T) Repository {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return New(db)
}

func TestWechatTrackingDueDispatchClaimHasOneConcurrentWinnerAndSafeRelease(t *testing.T) {
	repo := newWechatTrackingRepositoryTest(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	tracking := &model.WechatArticleTracking{
		ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "u", ProjectID: "p", PublicationID: uuid.NewString(),
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatTrackingStatusWaitingData,
		PublishedAt: now.Add(-time.Hour), ExpiresAt: now.Add(model.WechatTrackingWindow), NextFetchAt: &due,
	}
	if err := repo.WechatTrackings().Create(ctx, tracking); err != nil {
		t.Fatal(err)
	}
	leaseUntil := now.Add(15 * time.Minute)
	tokens := []string{uuid.NewString(), uuid.NewString()}
	results := make(chan bool, len(tokens))
	errorsCh := make(chan error, len(tokens))
	var wg sync.WaitGroup
	for _, token := range tokens {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			claimed, err := repo.WechatTrackings().TryClaimDueDispatch(ctx, tracking.ID, tracking.UpdatedAt, now, leaseUntil, token)
			results <- claimed
			errorsCh <- err
		}(token)
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	winners := 0
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	for claimed := range results {
		if claimed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("claim winners = %d, want 1", winners)
	}
	claimed, err := repo.WechatTrackings().FindByID(ctx, tracking.ID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.RecoveryClaimToken == "" || claimed.NextFetchAt == nil || !claimed.NextFetchAt.Equal(leaseUntil) {
		t.Fatalf("claimed tracking = %#v", claimed)
	}
	released, err := repo.WechatTrackings().ReleaseDueDispatch(ctx, tracking.ID, claimed.RecoveryClaimToken, now)
	if err != nil || !released {
		t.Fatalf("release = %v err=%v", released, err)
	}
	after, _ := repo.WechatTrackings().FindByID(ctx, tracking.ID)
	if after.RecoveryClaimToken != "" || after.NextFetchAt == nil || !after.NextFetchAt.Equal(now) {
		t.Fatalf("released tracking = %#v", after)
	}
}

func TestWechatMetricSnapshotUpsertKeyIsTrackingIDAndStatDate(t *testing.T) {
	repo := newWechatTrackingRepositoryTest(t)
	ctx := context.Background()
	trackingID, taskID := uuid.NewString(), uuid.NewString()
	first := &model.WechatMetricSnapshot{ID: uuid.NewString(), TrackingID: trackingID, TaskID: taskID, StatDate: "2026-08-01", ReadUsers: 10, RawResponse: []byte(`{}`)}
	if err := repo.WechatMetricSnapshots().UpsertByTrackingAndDate(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatMetricSnapshots().UpsertByTrackingAndDate(ctx, &model.WechatMetricSnapshot{ID: uuid.NewString(), TrackingID: trackingID, TaskID: taskID, StatDate: "2026-08-01", ReadUsers: 20, RawResponse: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatMetricSnapshots().UpsertByTrackingAndDate(ctx, &model.WechatMetricSnapshot{ID: uuid.NewString(), TrackingID: uuid.NewString(), TaskID: taskID, StatDate: "2026-08-01", ReadUsers: 30, RawResponse: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	snapshots, err := repo.WechatMetricSnapshots().FindByTaskID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || snapshots[0].ID != first.ID || snapshots[0].ReadUsers != 20 {
		t.Fatalf("snapshots = %#v", snapshots)
	}
}

func TestWechatTrackingFindDueUsesDurableNextFetchAndActiveLifecycleStates(t *testing.T) {
	repo := newWechatTrackingRepositoryTest(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	before, after := now.Add(-time.Minute), now.Add(time.Minute)
	for _, tracking := range []*model.WechatArticleTracking{
		{ID: "due-waiting", TaskID: uuid.NewString(), UserID: "u", ProjectID: "p", PublicationID: "pub-1", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatTrackingStatusWaitingData, PublishedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), NextFetchAt: &before},
		{ID: "due-error", TaskID: uuid.NewString(), UserID: "u", ProjectID: "p", PublicationID: "pub-2", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatTrackingStatusError, PublishedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), NextFetchAt: &before},
		{ID: "future", TaskID: uuid.NewString(), UserID: "u", ProjectID: "p", PublicationID: "pub-3", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatTrackingStatusTracking, PublishedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), NextFetchAt: &after},
		{ID: "terminal", TaskID: uuid.NewString(), UserID: "u", ProjectID: "p", PublicationID: "pub-4", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatTrackingStatusUnsupported, PublishedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), NextFetchAt: &before},
	} {
		if err := repo.WechatTrackings().Create(ctx, tracking); err != nil {
			t.Fatal(err)
		}
	}
	due, err := repo.WechatTrackings().FindDue(ctx, now, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tracking := range due {
		got[tracking.ID] = true
	}
	if len(due) != 2 || !got["due-waiting"] || !got["due-error"] {
		t.Fatalf("due = %#v", due)
	}
}

func TestWechatTrackingDailyFetchClaimHasOneWinner(t *testing.T) {
	repo := newWechatTrackingRepositoryTest(t)
	ctx := context.Background()
	publicationTime := time.Date(2026, 8, 1, 8, 0, 0, 0, time.UTC)
	tracking := &model.WechatArticleTracking{
		ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "u", ProjectID: "p", PublicationID: uuid.NewString(),
		Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatTrackingStatusWaitingData,
		PublishedAt: publicationTime, ExpiresAt: publicationTime.Add(model.WechatTrackingWindow),
	}
	if err := repo.WechatTrackings().Create(ctx, tracking); err != nil {
		t.Fatal(err)
	}
	dayStart := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	claimedAt := dayStart.Add(time.Hour)
	first, err := repo.WechatTrackings().TryClaimDailyFetch(ctx, tracking.ID, claimedAt, dayStart, claimedAt.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.WechatTrackings().TryClaimDailyFetch(ctx, tracking.ID, claimedAt.Add(time.Minute), dayStart, claimedAt.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !first || second {
		t.Fatalf("claims = first:%v second:%v, want one winner", first, second)
	}
}

func TestWechatPublicationBindMsgIDAllowsEmptyOrSameAndRejectsMissingOrConflict(t *testing.T) {
	repo := newWechatTrackingRepositoryTest(t)
	ctx := context.Background()
	publication := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: uuid.NewString(), UserID: "u", ProjectID: "p",
		Source: model.WechatPublicationSourceWechatConsole, Status: model.WechatPublicationStatusPublished,
		ArticleURL: "https://mp.weixin.qq.com/s/manual", ArticleIndex: 1,
	}
	if err := repo.WechatPublications().Create(ctx, publication); err != nil {
		t.Fatal(err)
	}
	bound, err := repo.WechatPublications().BindMsgID(ctx, publication.ID, "manual_1")
	if err != nil || !bound {
		t.Fatalf("empty bind = %v err=%v", bound, err)
	}
	bound, err = repo.WechatPublications().BindMsgID(ctx, publication.ID, "manual_1")
	if err != nil || !bound {
		t.Fatalf("idempotent bind = %v err=%v", bound, err)
	}
	bound, err = repo.WechatPublications().BindMsgID(ctx, publication.ID, "other_1")
	if err != nil || bound {
		t.Fatalf("conflicting bind = %v err=%v", bound, err)
	}
	stored, _ := repo.WechatPublications().FindByID(ctx, publication.ID)
	if stored.MsgID != "manual_1" {
		t.Fatalf("conflicting bind changed msgid to %q", stored.MsgID)
	}
	if _, err := repo.WechatPublications().BindMsgID(ctx, uuid.NewString(), "missing_1"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("missing bind error = %v, want record not found", err)
	}
}
