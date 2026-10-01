package service

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openFeedbackJobTestRepo(t *testing.T) (repository.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "feedback-job.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.AnalyticsContent{}, &model.AnalyticsObservation{}, &model.FeedbackJob{}, &model.FeedbackLease{}, &model.FeedbackInsight{}, &model.StrategySnapshot{}); err != nil {
		t.Fatal(err)
	}
	return repository.New(db), db
}

func TestMarkManagedFeedbackFailureReconcilesTerminalTaskFailure(t *testing.T) {
	repo, _ := openFeedbackJobTestRepo(t)
	job := &model.FeedbackJob{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Platform: model.PlatformWechat,
		AccountID: "wechat:wx-1", Operation: "content_postmortem", Cadence: FeedbackCadenceWeekly,
		PeriodStart: "2026-09-01", PeriodEnd: "2026-09-07", Fingerprint: "managed-failure-fingerprint",
		Status: model.FeedbackJobQueued,
	}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := MarkManagedFeedbackFailure(context.Background(), repo, job.ID, errors.New("runtime preparation failed")); err != nil {
		t.Fatalf("MarkManagedFeedbackFailure() error = %v", err)
	}
	stored, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.FeedbackJobFailed || stored.Attempts != 1 || stored.CompletedAt == nil {
		t.Fatalf("stored = %#v, want failed attempt 1 with completion time", stored)
	}
	if err := MarkManagedFeedbackFailure(context.Background(), repo, job.ID, errors.New("late retry")); err != nil {
		t.Fatalf("late MarkManagedFeedbackFailure() error = %v", err)
	}
	stored, err = repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Attempts != 1 || stored.LastError != "runtime preparation failed" {
		t.Fatalf("late retry overwrote terminal failure: %#v", stored)
	}
}

func TestProcessFeedbackJobReclaimsFailedJobForBoundedRetry(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	job := &model.FeedbackJob{ID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Platform: model.PlatformWechat, AccountID: "wechat:wx-1", Operation: "publish_analytics", Cadence: FeedbackCadenceWeekly, PeriodStart: "2026-09-01", PeriodEnd: "2026-09-07", AnalyticsRevision: 1, Fingerprint: "retry-fingerprint", Status: model.FeedbackJobQueued}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("DROP TABLE analytics_observations").Error; err != nil {
		t.Fatal(err)
	}
	if err := ProcessFeedbackJob(context.Background(), repo, job.ID); err == nil {
		t.Fatal("expected transient database error")
	}
	stored, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.FeedbackJobFailed || stored.Attempts != 1 {
		t.Fatalf("stored = %#v, want failed attempt 1", stored)
	}
	if err := db.Exec("CREATE TABLE analytics_observations (id varchar(36) primary key)").Error; err != nil {
		t.Fatal(err)
	}
	if err := ProcessFeedbackJob(context.Background(), repo, job.ID); err == nil {
		t.Fatal("expected schema mismatch on retry")
	}
	stored, err = repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Attempts != 2 || stored.Status != model.FeedbackJobFailed {
		t.Fatalf("stored after retry = %#v, want failed attempt 2", stored)
	}
	if err := db.Model(&model.FeedbackJob{}).Where("id = ?", job.ID).Update("attempts", model.FeedbackMaxAttempts-1).Error; err != nil {
		t.Fatal(err)
	}
	if err := ProcessFeedbackJob(context.Background(), repo, job.ID); err == nil {
		t.Fatal("expected final schema error")
	}
	stored, err = repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Attempts != model.FeedbackMaxAttempts || stored.Status != model.FeedbackJobBlocked {
		t.Fatalf("stored after final retry = %#v, want blocked at max attempts", stored)
	}
}

func TestProcessFeedbackJobDefersWhenAccountLeaseIsHeld(t *testing.T) {
	repo, _ := openFeedbackJobTestRepo(t)
	firstID, secondID := uuid.NewString(), uuid.NewString()
	first := &model.FeedbackJob{ID: firstID, UserID: "user-1", ProjectID: "project-1", Platform: model.PlatformWechat, AccountID: "wechat:wx-1", Operation: "publish_analytics", Cadence: FeedbackCadenceWeekly, PeriodStart: "2026-09-01", PeriodEnd: "2026-09-07", Fingerprint: "lease-first", Status: model.FeedbackJobQueued}
	second := &model.FeedbackJob{ID: secondID, UserID: "user-1", ProjectID: "project-2", Platform: model.PlatformWechat, AccountID: "wechat:wx-1", Operation: "publish_analytics", Cadence: FeedbackCadenceWeekly, PeriodStart: "2026-09-01", PeriodEnd: "2026-09-07", Fingerprint: "lease-second", Status: model.FeedbackJobQueued}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	acquired, err := repo.FeedbackLoop().AcquireFeedbackLease(context.Background(), "feedback:account:wechat:wx-1", firstID, time.Now().UTC(), feedbackLeaseTTL)
	if err != nil || !acquired {
		t.Fatalf("AcquireFeedbackLease() = %v, %v", acquired, err)
	}
	if err := ProcessFeedbackJob(context.Background(), repo, secondID); err == nil {
		t.Fatal("expected account lease contention")
	}
	stored, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), secondID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.FeedbackJobQueued || stored.Attempts != 0 {
		t.Fatalf("stored = %#v, want queued without consuming an attempt", stored)
	}
}

func TestProcessFeedbackJobSkipsWhenQueuedDataIsNoLongerValid(t *testing.T) {
	repo, _ := openFeedbackJobTestRepo(t)
	job := &model.FeedbackJob{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Platform: model.PlatformWechat,
		Operation: "publish_analytics", Cadence: FeedbackCadenceWeekly, PeriodStart: "2026-09-01", PeriodEnd: "2026-09-07",
		AnalyticsRevision: 4, ContentSetDigest: "digest", Fingerprint: "fingerprint-empty", Status: model.FeedbackJobQueued,
	}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	if err := ProcessFeedbackJob(context.Background(), repo, job.ID); err != nil {
		t.Fatalf("ProcessFeedbackJob() error = %v", err)
	}
	stored, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.FeedbackJobSkipped || stored.SkipReason != model.FeedbackSkipNoValidObservations {
		t.Fatalf("job = %#v, want skipped with no_valid_observations", stored)
	}
	var insightCount int64
	if err := repo.Analytics().DB().Model(&model.FeedbackInsight{}).Count(&insightCount).Error; err != nil {
		t.Fatal(err)
	}
	if insightCount != 0 {
		t.Fatalf("insight count = %d, want 0", insightCount)
	}
}

func TestProcessFeedbackJobDataTrackerCompletesWithoutInsight(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	now := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	contentID := "content-tracker"
	if err := db.Create(&model.AnalyticsContent{ID: contentID, ProjectID: "project-1", Platform: model.PlatformWechat, Date: ptrTime(now.Add(-48 * time.Hour))}).Error; err != nil {
		t.Fatal(err)
	}
	readUsers := int64(100)
	if err := db.Create(&model.AnalyticsObservation{ID: "observation-tracker", ProjectID: "project-1", ContentID: contentID, MetricBasis: "cumulative", StatDate: "2026-09-07", Source: "manual", EffectiveAt: now, ReceivedAt: now, AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &readUsers}}).Error; err != nil {
		t.Fatal(err)
	}
	job := &model.FeedbackJob{ID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Platform: model.PlatformWechat, AccountID: "wechat:wx-1", Operation: "data_tracker", Cadence: FeedbackCadenceDaily, PeriodStart: "2026-09-07", PeriodEnd: "2026-09-07", AnalyticsRevision: 4, Fingerprint: "fingerprint-tracker", Status: model.FeedbackJobQueued}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if err := ProcessFeedbackJob(context.Background(), repo, job.ID); err != nil {
		t.Fatalf("ProcessFeedbackJob() error = %v", err)
	}
	stored, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil || stored.Status != model.FeedbackJobSucceeded || stored.SampleCount != 1 {
		t.Fatalf("job = %#v, err=%v, want succeeded tracker", stored, err)
	}
	var insightCount int64
	if err := db.Model(&model.FeedbackInsight{}).Count(&insightCount).Error; err != nil {
		t.Fatal(err)
	}
	if insightCount != 0 {
		t.Fatalf("insight count = %d, want 0 for deterministic data tracker", insightCount)
	}
}

func TestProcessFeedbackJobPersistsExplainableInsightFromSQLiteObservation(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	now := time.Date(2026, 8, 31, 4, 0, 0, 0, time.UTC)
	contentID := "content-1"
	if err := db.Create(&model.AnalyticsContent{ID: contentID, ProjectID: "project-1", Platform: model.PlatformWechat, Date: ptrTime(now.Add(-8 * 24 * time.Hour))}).Error; err != nil {
		t.Fatal(err)
	}
	readUsers, shareUsers := int64(100), int64(10)
	if err := db.Create(&model.AnalyticsObservation{ID: "observation-1", ProjectID: "project-1", ContentID: contentID, MetricBasis: "cumulative", StatDate: "2026-09-07", Source: "manual", EffectiveAt: now, ReceivedAt: now, AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &readUsers, ShareUsers: &shareUsers}}).Error; err != nil {
		t.Fatal(err)
	}
	job := &model.FeedbackJob{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Platform: model.PlatformWechat,
		Operation: "publish_analytics", Cadence: FeedbackCadenceWeekly, PeriodStart: "2026-09-01", PeriodEnd: "2026-09-07",
		AnalyticsRevision: 4, ContentSetDigest: "digest", Fingerprint: "fingerprint-valid", Status: model.FeedbackJobQueued,
	}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	if err := ProcessFeedbackJob(context.Background(), repo, job.ID); err != nil {
		t.Fatalf("ProcessFeedbackJob() error = %v", err)
	}
	stored, err := repo.FeedbackLoop().FindJobByIDOrFingerprint(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.FeedbackJobSucceeded || stored.SampleCount != 1 {
		t.Fatalf("job = %#v, want succeeded with one content sample", stored)
	}
	var insight model.FeedbackInsight
	if err := db.Where("job_id = ?", job.ID).First(&insight).Error; err != nil {
		t.Fatal(err)
	}
	if insight.AnalyticsRevision != 4 || insight.EvidenceJSON == "" || insight.Summary == "" {
		t.Fatalf("insight = %#v, want revision/evidence/summary", insight)
	}
}

func TestProcessFeedbackJobStrategyRecommendationsUseObservedEngagement(t *testing.T) {
	repo, db := openFeedbackJobTestRepo(t)
	now := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	for i := 0; i < 10; i++ {
		contentID := "content-" + uuid.NewString()
		if err := db.Create(&model.AnalyticsContent{ID: contentID, ProjectID: "project-1", Platform: model.PlatformWechat, Date: ptrTime(now.Add(-8 * 24 * time.Hour))}).Error; err != nil {
			t.Fatal(err)
		}
		readUsers, shareUsers := int64(100), int64(10)
		if err := db.Create(&model.AnalyticsObservation{ID: "observation-" + uuid.NewString(), ProjectID: "project-1", ContentID: contentID, MetricBasis: "cumulative", StatDate: "2026-08-31", Source: "manual", EffectiveAt: now, ReceivedAt: now, AnalyticsMetrics: model.AnalyticsMetrics{ReadUsers: &readUsers, ShareUsers: &shareUsers}}).Error; err != nil {
			t.Fatal(err)
		}
	}
	job := &model.FeedbackJob{
		ID: uuid.NewString(), UserID: "user-1", ProjectID: "project-1", Platform: model.PlatformWechat,
		Operation: "strategy_advisor", Cadence: FeedbackCadenceMonthly, PeriodStart: "2026-08-01", PeriodEnd: "2026-08-31",
		AnalyticsRevision: 5, ContentSetDigest: "digest", Fingerprint: "fingerprint-strategy", Status: model.FeedbackJobQueued,
	}
	if _, err := repo.FeedbackLoop().CreateJob(context.Background(), job); err != nil {
		t.Fatal(err)
	}

	if err := ProcessFeedbackJob(context.Background(), repo, job.ID); err != nil {
		t.Fatalf("ProcessFeedbackJob() error = %v", err)
	}
	var snapshot model.StrategySnapshot
	if err := db.Where("project_id = ?", "project-1").First(&snapshot).Error; err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "active" || snapshot.SourceRevision != 5 || snapshot.Recommendations == "" {
		t.Fatalf("snapshot = %#v, want active observed strategy", snapshot)
	}
	if snapshot.Recommendations == `{"keep":"keep the current mix while the sample is below the strategy threshold"}` {
		t.Fatal("strategy recommendations must reflect the observed engagement band")
	}
}

func ptrTime(value time.Time) *time.Time { return &value }
