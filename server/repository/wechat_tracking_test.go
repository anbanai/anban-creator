package repository

import (
	"context"
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
