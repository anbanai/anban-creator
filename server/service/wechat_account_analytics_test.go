package service

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	value, reason := sumWechatMetric([]string{"2026-09-01", "2026-09-03"}, days, "read_users")
	if value == nil || *value != 1 || reason != "" {
		t.Fatalf("sum = %v reason=%q", value, reason)
	}
	missing, reason := sumWechatMetric([]string{"2026-09-01", "2026-09-02", "2026-09-03"}, days, "read_users")
	if missing != nil || reason == "" {
		t.Fatalf("incomplete coverage sum = %v reason=%q", missing, reason)
	}
	missing, reason = sumWechatMetric([]string{"2026-09-01"}, []*model.WechatAccountAnalyticsDay{{StatDate: "2026-09-01"}}, "read_users")
	if missing != nil || reason == "" {
		t.Fatalf("missing metric = %v reason=%q", missing, reason)
	}
}

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

func TestNormalizeWechatAccountAnalyticsIdempotencyKey(t *testing.T) {
	got, err := normalizeWechatAccountAnalyticsIdempotencyKey("  replay-key  ")
	if err != nil || got != "replay-key" {
		t.Fatalf("normalized key = %q, err = %v", got, err)
	}
	if _, err := normalizeWechatAccountAnalyticsIdempotencyKey(" \t\n "); err == nil {
		t.Fatal("whitespace-only idempotency key must be rejected")
	}
	if _, err := normalizeWechatAccountAnalyticsIdempotencyKey(strings.Repeat("a", 129)); err == nil {
		t.Fatal("oversized ASCII idempotency key must be rejected")
	}
	if _, err := normalizeWechatAccountAnalyticsIdempotencyKey(strings.Repeat("界", 43)); err == nil {
		t.Fatal("oversized UTF-8 idempotency key must be rejected by byte length")
	}
}

func TestWechatTrafficSourcesRejectUnsupportedMetric(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	project := &model.Project{ID: "project-traffic-metric", UserID: "user-traffic-metric", Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatal(err)
	}
	_, err = NewWechatAccountAnalyticsService(repo).TrafficSources(context.Background(), project.UserID, project.ID, "2026-10-08", "2026-10-08", "share_users", "all")
	if !errors.Is(err, ErrAnalyticsInvalidQuery) || !strings.Contains(err.Error(), "read_users") {
		t.Fatalf("unsupported source metric error = %v", err)
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
	batch := &model.WechatAccountAnalyticsBatch{ID: "batch-traffic-zero", UserID: project.UserID, ProjectID: project.ID, Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Now().UTC(), EffectiveAt: time.Now().UTC()}
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

func TestWechatTrafficSourcesExposeSourceTotalsWithoutAccountDays(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	project := &model.Project{ID: "project-source-only", UserID: "user-source-only", Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	batch := &model.WechatAccountAnalyticsBatch{ID: "batch-source-only", UserID: project.UserID, ProjectID: project.ID, Status: model.WechatAccountAnalyticsBatchCompleted, ReceivedAt: time.Now().UTC(), EffectiveAt: time.Now().UTC()}
	if err := repo.WechatAccountAnalytics().CreateBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatAccountAnalytics().CreateSources(ctx, []*model.WechatTrafficSourceDay{
		{ID: "source-only-1", ProjectID: project.ID, BatchID: batch.ID, StatDate: "2026-10-08", Source: model.WechatTrafficSourceMessage, ReadUsers: ptrInt64(100)},
		{ID: "source-only-2", ProjectID: project.ID, BatchID: batch.ID, StatDate: "2026-10-09", Source: model.WechatTrafficSourceMessage, ReadUsers: ptrInt64(120)},
	}); err != nil {
		t.Fatal(err)
	}
	response, err := NewWechatAccountAnalyticsService(repo).TrafficSources(ctx, project.UserID, project.ID, "2026-10-08", "2026-10-09", "read_users", "all")
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Items) != 1 || response.Items[0].Value == nil || *response.Items[0].Value != 220 {
		t.Fatalf("items = %#v, want source total 220", response.Items)
	}
	if response.Items[0].Share != nil || response.DataQuality == nil {
		t.Fatalf("source-only share/data quality = %#v / %#v, want unavailable share and warning", response.Items[0].Share, response.DataQuality)
	}
}

func timePtr(value time.Time) *time.Time { return &value }

func ptrInt64(value int64) *int64 { return &value }
