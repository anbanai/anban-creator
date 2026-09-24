package service

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestContentAnalyticsMatchPrecedenceAndConflictingIdentity(t *testing.T) {
	now := time.Now()
	earlier := now.AddDate(0, 0, -1)
	candidates := []AnalyticsCandidate{{Target: AnalyticsTarget{"task", "a"}, Title: "同题", Date: &now, matchDate: &now, URL: "https://mp.weixin.qq.com/s/first"}, {Target: AnalyticsTarget{"task", "b"}, Title: "同题", Date: &earlier, matchDate: &earlier, URL: "https://mp.weixin.qq.com/s/second"}, {Target: AnalyticsTarget{"task", "c"}, Title: "唯一标题", Date: &earlier}}
	for _, tc := range []struct {
		name, title, url, want string
		date                   *time.Time
		ambiguous              bool
	}{
		{name: "url first", title: "其他标题", url: "https://mp.weixin.qq.com/s/first?scene=1#rd", want: "a"},
		{name: "title and date", title: "同题", date: &now, want: "a"},
		{name: "unique title", title: "唯一标题", date: &now, want: "c"},
		{name: "ambiguous title", title: "同题", ambiguous: true},
		{name: "conflicting url", title: "同题", date: &now, url: "https://mp.weixin.qq.com/s/third"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ambiguous := matchAnalyticsCandidate(tc.title, tc.date, tc.url, candidates)
			id := ""
			if got != nil {
				id = got.Target.ID
			}
			if id != tc.want || ambiguous != tc.ambiguous {
				t.Fatalf("match %q ambiguous=%v", id, ambiguous)
			}
		})
	}
}

func TestContentAnalyticsConflictingHistoricalIdentityRequiresManualSelection(t *testing.T) {
	candidate := AnalyticsCandidate{Target: AnalyticsTarget{"task", "task"}, Title: "同题", conflictingIdentity: true}
	if matched, _ := matchAnalyticsCandidate("同题", nil, "https://mp.weixin.qq.com/s/new", []AnalyticsCandidate{candidate}); matched != nil {
		t.Fatal("conflicting history must not fall back to title")
	}
	selected, err := resolveAnalyticsSelections([]AnalyticsSelection{{2, candidate.Target}}, []AnalyticsCandidate{candidate})
	if err != nil || len(selected) != 1 {
		t.Fatalf("explicit correction rejected: %v", err)
	}
}

func TestAnalyticsPublicIdentityIgnoresWechatTrackingQuery(t *testing.T) {
	first := "https://mp.weixin.qq.com/s/abc?scene=1#rd"
	second := "https://mp.weixin.qq.com/s/abc?scene=2#rd"
	if analyticsPublicIdentity(first) != analyticsPublicIdentity(second) {
		t.Fatalf("tracking query changed article identity: %q vs %q", analyticsPublicIdentity(first), analyticsPublicIdentity(second))
	}
}

func TestContentAnalyticsCandidatesAllStatesAndIsolation(t *testing.T) {
	ctx := context.Background()
	f, _, _ := newWechatSelectionImport(t, [][]string{wechatSelectionRow("文章", "")})
	for _, status := range []string{"pending", "running", "failed", "cancelled", "completed"} {
		task := &model.Task{ID: uuid.NewString(), UserID: f.userID, ProjectID: f.projectID, Type: "article", Status: status, Title: "筛选 " + status}
		if err := f.repo.Tasks().Create(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	svc := NewContentAnalyticsService(f.repo)
	items, total, err := svc.Candidates(ctx, f.userID, f.projectID, "", 0, 100)
	if err != nil || total != 6 || len(items) != 6 {
		t.Fatalf("candidates=%d total=%d err=%v", len(items), total, err)
	}
	for _, item := range items {
		if item.Target.Kind != "task" {
			t.Fatalf("publication was not canonicalized: %+v", item)
		}
	}
	items, total, err = svc.Candidates(ctx, f.userID, f.projectID, "筛选", 1, 2)
	if err != nil || total != 5 || len(items) != 2 {
		t.Fatalf("search pagination items=%d total=%d err=%v", len(items), total, err)
	}
	if _, _, err := svc.Candidates(ctx, uuid.NewString(), f.projectID, "", 0, 25); err == nil {
		t.Fatal("foreign user accessed project")
	}
}

func TestWechatAnalyticsTaskOnlySnapshotsAndRevocation(t *testing.T) {
	ctx := context.Background()
	f, svc, req := newWechatSelectionImport(t, [][]string{wechatSelectionRow("待处理任务", "https://mp.weixin.qq.com/s/task")})
	task := &model.Task{ID: uuid.NewString(), UserID: f.userID, ProjectID: f.projectID, Type: "article", Status: "pending", Title: "待处理任务"}
	if err := f.repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	before, _ := f.repo.Tasks().FindByID(ctx, task.ID)
	preview, err := svc.Preview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rows[0].Target == nil || preview.Rows[0].Target.ID != task.ID {
		t.Fatalf("task match missing: %+v", preview.Rows[0])
	}
	_, total, _ := f.repo.WechatAnalyticsImports().ListBatches(ctx, f.projectID, 0, 20)
	if total != 0 {
		t.Fatal("preview persisted analytics")
	}
	req.Selections = []AnalyticsSelection{{2, AnalyticsTarget{"task", task.ID}}}
	first, err := svc.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	req.DataAsOfAt = &later
	second, err := svc.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	views, err := svc.ListArticles(ctx, f.userID, f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, view := range views {
		if view.Task != nil && view.Task.ID == task.ID {
			found = true
			if view.Publication != nil || view.Task.Status != "pending" || len(view.Snapshots) != 2 || view.Latest.BatchID != second.Batch.ID {
				t.Fatalf("task-only view: %+v", view)
			}
		}
	}
	if !found {
		t.Fatal("task-only analytics missing")
	}
	after, _ := f.repo.Tasks().FindByID(ctx, task.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("import mutated task")
	}
	pubs, _ := f.repo.WechatPublications().ListByProject(ctx, f.projectID)
	if len(pubs) != 1 {
		t.Fatal("import fabricated publication")
	}
	if _, err := svc.Revoke(ctx, f.userID, f.projectID, second.Batch.ID); err != nil {
		t.Fatal(err)
	}
	snapshots, err := svc.Snapshots(ctx, f.userID, f.projectID, task.ID)
	if err != nil || len(snapshots) != 1 || snapshots[0].BatchID != first.Batch.ID {
		t.Fatalf("revocation did not restore prior observation: %+v %v", snapshots, err)
	}
	if _, err := svc.Revoke(ctx, f.userID, f.projectID, first.Batch.ID); err != nil {
		t.Fatal(err)
	}
	overview, err := svc.Overview(ctx, f.userID, f.projectID)
	if err != nil || overview.WithData != 0 {
		t.Fatalf("revoked observation included: %+v %v", overview, err)
	}
}

func TestContentAnalyticsOnlyPublishedDateIsMatchingEvidence(t *testing.T) {
	date := time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC)
	other := date.AddDate(0, 0, -1)
	tasks := []AnalyticsCandidate{{Target: AnalyticsTarget{"task", "first"}, Title: "同题", Date: &date}, {Target: AnalyticsTarget{"task", "second"}, Title: "同题", Date: &other}}
	if match, ambiguous := matchAnalyticsCandidate("同题", &date, "", tasks); match != nil || !ambiguous {
		t.Fatalf("creation date disambiguated task: %+v %v", match, ambiguous)
	}
	published := AnalyticsCandidate{Target: AnalyticsTarget{"wechat_publication", "publication"}, Title: "同题", Date: &other, matchDate: &other}
	if match, _ := matchAnalyticsCandidate("同题", &date, "", []AnalyticsCandidate{published}); match != nil {
		t.Fatal("conflicting publication date fell back to unique title")
	}
	if _, err := resolveAnalyticsSelections([]AnalyticsSelection{{2, published.Target}}, []AnalyticsCandidate{published}); err != nil {
		t.Fatalf("explicit correction rejected: %v", err)
	}
	if analyticsPublicIdentity("https://mp.weixin.qq.com/s?foo=1") == analyticsPublicIdentity("https://mp.weixin.qq.com/s?foo=2") {
		t.Fatal("unknown URL queries collapsed into one identity")
	}
}

func TestWechatAnalyticsTaskAndPublicationDetailHaveContinuousHistory(t *testing.T) {
	ctx := context.Background()
	f, svc, _ := newWechatSelectionImport(t, [][]string{wechatSelectionRow("文章", "")})
	for _, taskOnly := range []bool{true, false} {
		snapshot := &model.WechatAnalyticsSnapshot{ID: uuid.NewString(), ProjectID: f.projectID, BatchID: uuid.NewString(), ImportRowID: uuid.NewString(), DataAsOfAt: time.Now(), ImportedAt: time.Now(), RawData: "{}"}
		if taskOnly {
			snapshot.TaskID = f.taskID
		} else {
			snapshot.PublicationID = f.publication.ID
		}
		if err := f.repo.WechatAnalyticsImports().CreateSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
	}
	official := &model.WechatMetricSnapshot{ID: uuid.NewString(), TrackingID: f.tracking.ID, TaskID: f.taskID, StatDate: "2026-09-12", CapturedAt: time.Now(), RawResponse: []byte("{}")}
	if err := f.repo.WechatMetricSnapshots().Create(ctx, official); err != nil {
		t.Fatal(err)
	}
	byTask, err := svc.Snapshots(ctx, f.userID, f.projectID, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	byPublication, err := svc.Snapshots(ctx, f.userID, f.projectID, f.publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(byTask) != 3 || !reflect.DeepEqual(byTask, byPublication) {
		t.Fatalf("task and publication history diverged: %+v %+v", byTask, byPublication)
	}
}
