package service

import (
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestAggregateSeednotePostSummariesUsesLatestDailyVersionsAndSortsByExposure(t *testing.T) {
	day1 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	day2 := day1.AddDate(0, 0, 1)
	importedAt := day1.Add(time.Hour)
	laterImportedAt := importedAt.Add(time.Hour)
	count := func(value int64) *int64 { return &value }
	metric := func(postID string, date, imported time.Time, exposure, likes, comments, collects int64, click, duration float64) *model.SeednoteMetricVersion {
		return &model.SeednoteMetricVersion{PostID: postID, DataAsOfAt: date, ImportedAt: imported, ExposureCount: count(exposure), LikeCount: count(likes), CommentCount: count(comments), CollectCount: count(collects), CoverClickRate: &click, AvgWatchDuration: &duration}
	}
	posts := []*model.SeednotePost{
		{ID: "post-low", Title: "低曝光", CreatedAt: day1},
		{ID: "post-high", Title: "高曝光", CreatedAt: day1.Add(time.Minute)},
	}
	versions := []*model.SeednoteMetricVersion{
		metric("post-low", day1, importedAt, 100, 10, 2, 3, 0.10, 5),
		metric("post-low", day1, laterImportedAt, 120, 12, 4, 5, 0.20, 7),
		metric("post-low", day2, importedAt, 80, 8, 1, 2, 0.30, 9),
		metric("post-high", day1, importedAt, 500, 20, 8, 12, 0.40, 10),
	}

	got := aggregateSeednotePostSummaries(posts, versions)
	if len(got) != 2 {
		t.Fatalf("aggregateSeednotePostSummaries() returned %d summaries, want 2", len(got))
	}
	if got[0].ID != "post-high" || got[1].ID != "post-low" {
		t.Fatalf("summaries sorted IDs = %q, %q, want post-high, post-low", got[0].ID, got[1].ID)
	}
	if got[1].ExposureCount != 200 || got[1].LikeCount != 20 || got[1].CommentCount != 5 || got[1].CollectCount != 7 {
		t.Fatalf("post-low totals = %+v, want exposure 200 / likes 20 / comments 5 / collects 7", got[1])
	}
	if got[1].CoverClickRate != 0.25 || got[1].AvgWatchDuration != 8 {
		t.Fatalf("post-low averages = click %.2f duration %.1f, want .25 and 8", got[1].CoverClickRate, got[1].AvgWatchDuration)
	}
}
