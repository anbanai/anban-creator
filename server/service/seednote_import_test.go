package service

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
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
	if *got[1].ExposureCount != 200 || *got[1].LikeCount != 20 || *got[1].CommentCount != 5 || *got[1].CollectCount != 7 {
		t.Fatalf("post-low totals = %+v, want exposure 200 / likes 20 / comments 5 / collects 7", got[1])
	}
	if *got[1].CoverClickRate != 0.25 || *got[1].AvgWatchDuration != 8 {
		t.Fatalf("post-low averages = click %.2f duration %.1f, want .25 and 8", *got[1].CoverClickRate, *got[1].AvgWatchDuration)
	}
}

func TestSeednotePostSummaryIncludesPublicIdentity(t *testing.T) {
	summaries := aggregateSeednotePostSummaries([]*model.SeednotePost{{ID: "internal-id", NoteID: "public-note", NoteURL: "https://www.xiaohongshu.com/explore/public-note?xsec_token=example"}}, []*model.SeednoteMetricVersion{{PostID: "internal-id", DataAsOfAt: time.Now()}})
	raw, err := json.Marshal(summaries)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got[0]["note_id"] != "public-note" || got[0]["note_url"] != "https://www.xiaohongshu.com/explore/public-note?xsec_token=example" {
		t.Fatalf("public identity missing: %s", raw)
	}
}

func TestSeednoteImportReadsReuseExplicitlyLinkedTaskIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, noteID, noteURL string
		linked, foreign       bool
	}{
		{name: "bound task supplies full public URL", linked: true},
		{name: "post URL takes precedence", linked: true, noteID: "own-note", noteURL: "https://www.xiaohongshu.com/explore/own-note"},
		{name: "different note ID cannot take task URL", linked: true, noteID: "own-note"},
		{name: "unlinked post does not infer identity"},
		{name: "cross project tracking is not reused", linked: true, foreign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			trackingService, repo, _ := setupSeednoteTrackingServiceTest(t)
			userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
			publicURL := "https://www.xiaohongshu.com/explore/note-1?xsec_token=example"
			if err := trackingService.BindTask(ctx, userID, taskID, SeednotePublicationIdentity{NoteURL: publicURL}); err != nil {
				t.Fatal(err)
			}
			if tc.foreign {
				tracking, err := repo.SeednoteTrackings().FindByTaskID(ctx, taskID)
				if err != nil {
					t.Fatal(err)
				}
				tracking.ProjectID = "another-project"
				if err := repo.SeednoteTrackings().Update(ctx, tracking); err != nil {
					t.Fatal(err)
				}
			}
			post := &model.SeednotePost{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Title: "笔记", NoteID: tc.noteID, NoteURL: tc.noteURL}
			if tc.linked {
				post.TaskID = taskID
			}
			if err := repo.SeednotePosts().Create(ctx, post); err != nil {
				t.Fatal(err)
			}
			if err := repo.SeednoteMetricVersions().Create(ctx, &model.SeednoteMetricVersion{ID: uuid.NewString(), PostID: post.ID, BatchID: uuid.NewString(), ImportRowID: uuid.NewString(), DataAsOfAt: time.Now(), ImportedAt: time.Now(), RawData: "{}"}); err != nil {
				t.Fatal(err)
			}
			svc := NewSeednoteImportService(repo, nil)
			wantID, wantURL := tc.noteID, tc.noteURL
			if tc.linked && !tc.foreign && tc.noteID == "" && tc.noteURL == "" {
				wantID, wantURL = "note-1", publicURL
			}
			posts, _, err := svc.ListPosts(ctx, userID, projectID, "", 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			detail, _, err := svc.GetPost(ctx, userID, projectID, post.ID, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			overview, err := svc.Overview(ctx, userID, projectID, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range []*model.SeednotePost{posts[0], detail, overview.Posts[0]} {
				if got.NoteID != wantID || got.NoteURL != wantURL {
					t.Fatalf("identity = (%q, %q), want (%q, %q)", got.NoteID, got.NoteURL, wantID, wantURL)
				}
			}
			if overview.PostSummaries[0].NoteID != wantID || overview.PostSummaries[0].NoteURL != wantURL {
				t.Fatalf("summary identity = %+v", overview.PostSummaries[0])
			}
		})
	}
}

func TestSeednoteOverviewPreservesMissingMetricsAndNewestDataDate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		supplied bool
		value    int64
	}{
		{name: "all missing"}, {name: "real zero", supplied: true}, {name: "partial input", supplied: true, value: 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			_, repo, _ := setupSeednoteTrackingServiceTest(t)
			userID, projectID, _ := createSeednoteTrackingFixtures(t, repo)
			post := &model.SeednotePost{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Title: "笔记"}
			if err := repo.SeednotePosts().Create(ctx, post); err != nil {
				t.Fatal(err)
			}
			date := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
			laterDate := date.AddDate(0, 0, 1)
			for i, asOf := range []time.Time{date, laterDate} {
				version := &model.SeednoteMetricVersion{ID: uuid.NewString(), PostID: post.ID, BatchID: uuid.NewString(), ImportRowID: uuid.NewString(), DataAsOfAt: asOf, ImportedAt: date.Add(time.Duration(2-i) * time.Hour), RawData: "{}"}
				if tc.supplied && i == 1 {
					value, rate := tc.value, float64(tc.value)/100
					version.ExposureCount, version.CoverClickRate = &value, &rate
				}
				if err := repo.SeednoteMetricVersions().Create(ctx, version); err != nil {
					t.Fatal(err)
				}
			}
			got, err := NewSeednoteImportService(repo, nil).Overview(ctx, userID, projectID, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(got.PostSummaries) != 1 || len(got.Series) != 2 {
				t.Fatalf("overview = %+v", got)
			}
			summary := got.PostSummaries[0]
			if summary.DataAsOfAt == nil || !summary.DataAsOfAt.Equal(laterDate) {
				t.Fatalf("data_as_of_at = %v", summary.DataAsOfAt)
			}
			// Missing stays explicit null in both native API views, including means.
			for _, value := range []any{summary, got.Series[0], got.Series[1]} {
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				var fields map[string]any
				if err := json.Unmarshal(raw, &fields); err != nil {
					t.Fatal(err)
				}
				for _, metric := range []string{"view_count", "like_count", "comment_count", "collect_count", "follower_gain_count", "share_count", "avg_watch_duration", "barrage_count"} {
					field, exists := fields[metric]
					if !exists || field != nil {
						t.Fatalf("%s must be explicit null: %s", metric, raw)
					}
				}
			}
			if got.Series[0].ExposureCount != nil || got.Series[0].CoverClickRate != nil {
				t.Fatalf("missing day became zero: %+v", got.Series[0])
			}
			if tc.supplied {
				if summary.ExposureCount == nil || *summary.ExposureCount != tc.value || summary.CoverClickRate == nil || *summary.CoverClickRate != float64(tc.value)/100 {
					t.Fatalf("summary lost supplied metrics: %+v", summary)
				}
				if got.Series[1].ExposureCount == nil || *got.Series[1].ExposureCount != tc.value {
					t.Fatalf("series lost supplied count: %+v", got.Series[1])
				}
			} else if summary.ExposureCount != nil || summary.CoverClickRate != nil || got.Series[1].ExposureCount != nil {
				t.Fatalf("missing totals became zero: %+v", summary)
			}
		})
	}
}

func TestSeednoteOverviewUsesShanghaiDayForUTCVersions(t *testing.T) {
	ctx := context.Background()
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, _ := createSeednoteTrackingFixtures(t, repo)
	post := &model.SeednotePost{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Title: "跨时区数据"}
	if err := repo.SeednotePosts().Create(ctx, post); err != nil {
		t.Fatal(err)
	}
	// Both observations belong to September 24 in Shanghai, despite UTC dates
	// straddling midnight. The earlier observation was imported last and wins.
	olderData := time.Date(2026, 9, 23, 17, 0, 0, 0, time.UTC)
	laterData := time.Date(2026, 9, 24, 1, 0, 0, 0, time.UTC)
	imported := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	for i, at := range []time.Time{laterData, olderData} {
		count := int64(100 * (i + 1))
		version := &model.SeednoteMetricVersion{ID: uuid.NewString(), PostID: post.ID, BatchID: uuid.NewString(), ImportRowID: uuid.NewString(), DataAsOfAt: at, ImportedAt: imported.Add(time.Duration(i) * time.Hour), ExposureCount: &count, RawData: "{}"}
		if err := repo.SeednoteMetricVersions().Create(ctx, version); err != nil {
			t.Fatal(err)
		}
	}
	// Use UTC instants equivalent to the Shanghai day's query boundaries.
	from := time.Date(2026, 9, 23, 16, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	svc := NewSeednoteImportService(repo, nil)
	overview, err := svc.Overview(ctx, userID, projectID, &from, &to)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Dates) != 1 || overview.Dates[0] != "2026-09-24" || len(overview.Series) != 1 {
		t.Fatalf("wrong Shanghai daily grouping: %+v", overview)
	}
	if overview.Series[0].ExposureCount == nil || *overview.Series[0].ExposureCount != 200 {
		t.Fatalf("same Shanghai day did not select newest import: %+v", overview.Series)
	}
	if len(overview.PostSummaries) != 1 || overview.PostSummaries[0].ExposureCount == nil || *overview.PostSummaries[0].ExposureCount != 200 {
		t.Fatalf("summary double counted UTC dates in same Shanghai day: %+v", overview.PostSummaries)
	}
	_, versions, err := svc.GetPost(ctx, userID, projectID, post.ID, &from, &to)
	if err != nil || len(versions) != 2 {
		t.Fatalf("detail lost source observations: %+v %v", versions, err)
	}
	if overview.PostSummaries[0].DataAsOfAt == nil || !overview.PostSummaries[0].DataAsOfAt.Equal(olderData) {
		t.Fatalf("summary does not describe selected observation: %+v", overview.PostSummaries[0])
	}
}
