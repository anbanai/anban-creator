package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestWechatAccountRangeUsesInclusiveShanghaiDates(t *testing.T) {
	start, finish, err := validateWechatAccountRange("2026-09-01", "2026-09-03")
	if err != nil {
		t.Fatal(err)
	}
	got := dateList(start, finish)
	if len(got) != 3 || got[0] != "2026-09-01" || got[2] != "2026-09-03" {
		t.Fatalf("dates = %#v", got)
	}
	if _, _, err := validateWechatAccountRange("2026-09-03", "2026-09-01"); err == nil {
		t.Fatal("expected descending range to fail")
	}
}

func TestWechatAccountCoverageAndNullZeroSemantics(t *testing.T) {
	zero := int64(0)
	one := int64(1)
	days := []*model.WechatAccountAnalyticsDay{
		{StatDate: "2026-09-01", ReadUsers: &zero},
		{StatDate: "2026-09-03", ReadUsers: &one},
	}
	coverage, covered := coverageFor([]string{"2026-09-01", "2026-09-02", "2026-09-03"}, days)
	if coverage.Complete || coverage.CoveredDays != 2 || len(covered) != 2 {
		t.Fatalf("coverage = %#v dates=%#v", coverage, covered)
	}
	value, reason := sumWechatMetric(days, "read_users")
	if value == nil || *value != 1 || reason != "" {
		t.Fatalf("sum = %v reason=%q", value, reason)
	}
	missing, reason := sumWechatMetric([]*model.WechatAccountAnalyticsDay{{StatDate: "2026-09-01"}}, "read_users")
	if missing != nil || reason == "" {
		t.Fatalf("missing metric = %v reason=%q", missing, reason)
	}
}

func TestWechatAccountOverviewKeepsPartialMetricTotalsAndCoverage(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	project := &model.Project{ID: "project-partial", UserID: "user-partial", Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	for i, item := range []struct {
		date  string
		read  *int64
		share *int64
	}{
		{date: "2026-10-01", read: testWechatInt64Ptr(10), share: testWechatInt64Ptr(0)},
		{date: "2026-10-03", read: testWechatInt64Ptr(20)},
	} {
		batchID := fmt.Sprintf("batch-partial-%d", i)
		batch := &model.WechatAccountAnalyticsBatch{ID: batchID, UserID: project.UserID, ProjectID: project.ID, IdempotencyKey: batchID, Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Date(2026, 10, 4, i, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 10, 4, i, 0, 0, 0, time.UTC)}
		if err := repo.WechatAccountAnalytics().CreateBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
		if err := repo.WechatAccountAnalytics().CreateDays(ctx, []*model.WechatAccountAnalyticsDay{{ID: fmt.Sprintf("day-partial-%d", i), ProjectID: project.ID, BatchID: batchID, StatDate: item.date, ReadUsers: item.read, ShareUsers: item.share}}); err != nil {
			t.Fatal(err)
		}
	}

	overview, err := NewWechatAccountAnalyticsService(repo).Overview(ctx, project.UserID, project.ID, "2026-10-01", "2026-10-03")
	if err != nil {
		t.Fatal(err)
	}
	if overview.Totals["read_users"] == nil || *overview.Totals["read_users"] != 30 {
		t.Fatalf("read total = %v, want partial total 30", overview.Totals["read_users"])
	}
	if overview.Totals["share_users"] == nil || *overview.Totals["share_users"] != 0 {
		t.Fatalf("share total = %v, want observed zero", overview.Totals["share_users"])
	}
	if overview.MetricCoverage["read_users"].CoveredDays != 2 || overview.MetricCoverage["share_users"].CoveredDays != 1 {
		t.Fatalf("metric coverage = %#v", overview.MetricCoverage)
	}
	if overview.Series[1]["read_users"] != nil {
		t.Fatalf("missing date was filled: %#v", overview.Series[1])
	}
	if overview.Comparisons["read_users"].PreviousPeriod.Value != nil {
		t.Fatalf("partial period comparison = %#v, want unavailable", overview.Comparisons["read_users"].PreviousPeriod)
	}
}

func testWechatInt64Ptr(value int64) *int64 { return &value }

func TestWechatAccountPreviousRangeAndZeroDenominator(t *testing.T) {
	start, _ := time.ParseInLocation("2006-01-02", "2026-09-07", repository.AnalyticsLocation)
	finish := start.AddDate(0, 0, 6)
	previousStart, previousFinish := previousWechatRange(start, finish)
	if formatWechatAccountDate(previousStart) != "2026-08-31" || formatWechatAccountDate(previousFinish) != "2026-09-06" {
		t.Fatalf("previous = %s..%s", formatWechatAccountDate(previousStart), formatWechatAccountDate(previousFinish))
	}
	if percentChange(5, 0) != nil {
		t.Fatal("zero previous denominator must be unavailable")
	}
}

func TestWechatAccountOverviewReturnsPreviousDayAndWeekComparisons(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	project := &model.Project{ID: "project-comparisons", UserID: "user-comparisons", Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	for i, item := range []struct {
		date  string
		value int64
	}{
		{date: "2026-10-07", value: 5},
		{date: "2026-10-01", value: 2},
		{date: "2026-10-08", value: 10},
	} {
		batchID := fmt.Sprintf("batch-comparison-%d", i)
		batch := &model.WechatAccountAnalyticsBatch{ID: batchID, UserID: project.UserID, ProjectID: project.ID, IdempotencyKey: fmt.Sprintf("comparison-%d", i), Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Date(2026, 10, 9, i, 0, 0, 0, time.UTC), EffectiveAt: time.Date(2026, 10, 9, i, 0, 0, 0, time.UTC)}
		if err := repo.WechatAccountAnalytics().CreateBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
		value := item.value
		if err := repo.WechatAccountAnalytics().CreateDays(ctx, []*model.WechatAccountAnalyticsDay{{ID: fmt.Sprintf("day-comparison-%d", i), ProjectID: project.ID, BatchID: batchID, StatDate: item.date, ReadUsers: &value}}); err != nil {
			t.Fatal(err)
		}
	}
	previousDays, previousErr := repo.WechatAccountAnalytics().ListWinningDays(ctx, project.ID, "2026-10-07", "2026-10-07")
	weekDays, weekErr := repo.WechatAccountAnalytics().ListWinningDays(ctx, project.ID, "2026-10-01", "2026-10-01")
	if previousErr != nil || weekErr != nil || len(previousDays) != 1 || len(weekDays) != 1 {
		t.Fatalf("previous days=%#v err=%v week=%#v err=%v", previousDays, previousErr, weekDays, weekErr)
	}
	overview, err := NewWechatAccountAnalyticsService(repo).Overview(ctx, project.UserID, project.ID, "2026-10-08", "2026-10-08")
	if err != nil {
		t.Fatal(err)
	}
	comparisons := overview.Comparisons["read_users"]
	if comparisons == nil || comparisons.PreviousDay == nil || comparisons.PreviousWeek == nil {
		t.Fatalf("comparisons = %#v, want previous day and week", comparisons)
	}
	if comparisons.PreviousDay.Value == nil || *comparisons.PreviousDay.Value != 1 {
		t.Fatalf("previous day = %#v, want 100%%", comparisons.PreviousDay)
	}
	if comparisons.PreviousWeek.Value == nil || *comparisons.PreviousWeek.Value != 4 {
		t.Fatalf("previous week = %#v, want 400%%", comparisons.PreviousWeek)
	}
}

func TestWechatAccountImportFingerprintIncludesDataAsOfAndClientModifiedAt(t *testing.T) {
	data := []byte("workbook")
	base := WechatAccountAnalyticsImportRequest{
		UploadID:             "upload-1",
		IdempotencyKey:       "same-key",
		Timezone:             "Asia/Shanghai",
		DataAsOfAt:           timePtr(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)),
		ClientFileModifiedAt: timePtr(time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)),
	}
	first := marshalWechatAccountImportFingerprint(base, data)
	base.DataAsOfAt = timePtr(time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	second := marshalWechatAccountImportFingerprint(base, data)
	if string(first) == string(second) {
		t.Fatal("changing data_as_of_at must change the idempotency fingerprint")
	}
	var decoded wechatAccountImportFingerprint
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.DataAsOfAt == "" || decoded.ClientFileModifiedAt == "" || !strings.Contains(decoded.FileSHA256, hex.EncodeToString(sha256Bytes(data))) {
		t.Fatalf("fingerprint omitted provenance: %#v", decoded)
	}
}

func TestWechatTrafficSourcesZeroTotalLeavesSharesUnavailable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	project := &model.Project{ID: "project-traffic-zero", UserID: "user-traffic-zero", Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	batch := &model.WechatAccountAnalyticsBatch{ID: "batch-traffic-zero", UserID: project.UserID, ProjectID: project.ID, IdempotencyKey: "traffic-zero", Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Now().UTC(), EffectiveAt: time.Now().UTC()}
	if err := repo.WechatAccountAnalytics().CreateBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatAccountAnalytics().CreateDays(context.Background(), []*model.WechatAccountAnalyticsDay{{ID: "day-traffic-zero", ProjectID: project.ID, BatchID: batch.ID, StatDate: "2026-10-08", ReadUsers: &zero}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatAccountAnalytics().CreateSources(context.Background(), []*model.WechatTrafficSourceDay{{ID: "source-traffic-zero", ProjectID: project.ID, BatchID: batch.ID, StatDate: "2026-10-08", Source: model.WechatTrafficSourceMessage, ReadUsers: &zero}}); err != nil {
		t.Fatal(err)
	}
	response, err := NewWechatAccountAnalyticsService(repo).TrafficSources(context.Background(), project.UserID, project.ID, "2026-10-08", "2026-10-08", "read_users", "all")
	if err != nil {
		t.Fatal(err)
	}
	if response.DataQuality == nil || !strings.Contains(*response.DataQuality, "0") {
		t.Fatalf("data quality = %#v, want zero-total warning", response.DataQuality)
	}
	if len(response.Items) != 1 || response.Items[0].Share != nil {
		t.Fatalf("items = %#v, shares must remain unavailable", response.Items)
	}
}

func timePtr(value time.Time) *time.Time { return &value }
