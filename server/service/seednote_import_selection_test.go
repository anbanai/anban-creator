package service

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"github.com/tealeg/xlsx/v3"
)

func seednoteSelectionFixture(t *testing.T) (*SeednoteImportService, repository.Repository, SeednoteImportRequest, string) {
	t.Helper()
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, taskID := createSeednoteTrackingFixtures(t, repo)
	file := xlsx.NewFile()
	sheet, err := file.AddSheet("数据")
	if err != nil {
		t.Fatal(err)
	}
	header := sheet.AddRow()
	for _, value := range seednoteImportHeaders {
		header.AddCell().SetString(value)
	}
	for _, values := range [][]string{
		{"早起效率翻倍的方法", "2026-09-01 12:00:00", "图文", "100", "20", "0.2", "5", "2", "3", "1", "1", "4", "0"},
		{"无关联笔记", "2026-09-02 12:00:00", "视频", "200", "30", "0.3", "6", "2", "3", "1", "1", "4", "0"},
		{"错误数据", "2026-09-02 12:00:00", "图文", "bad"},
	} {
		row := sheet.AddRow()
		for _, value := range values {
			row.AddCell().SetString(value)
		}
	}
	var buf bytes.Buffer
	if err := file.Write(&buf); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := uuid.NewString() + ".xlsx"
	if _, err := store.Upload(context.Background(), key, bytes.NewReader(buf.Bytes()), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"); err != nil {
		t.Fatal(err)
	}
	asset := &model.Asset{ID: uuid.NewString(), UserID: userID, Purpose: DirectUploadPurposeSeednoteImport, StorageKey: key, FileName: "笔记数据.xlsx"}
	if err := repo.Assets().Create(context.Background(), asset); err != nil {
		t.Fatal(err)
	}
	return NewSeednoteImportService(repo, store), repo, SeednoteImportRequest{UserID: userID, ProjectID: projectID, UploadID: asset.ID}, taskID
}

func TestSeednotePreviewAndSelectedImportPreserveTaskLifecycle(t *testing.T) {
	ctx := context.Background()
	svc, repo, req, taskID := seednoteSelectionFixture(t)
	before, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TotalRows != 3 || preview.Rows[0].Target == nil || preview.Rows[0].Target.ID != taskID || preview.Rows[0].ContentType != "图文" || preview.Rows[1].MatchStatus != "unmatched" || preview.Rows[2].MatchStatus != "invalid" {
		t.Fatalf("preview = %+v", preview)
	}
	_, batches, err := repo.SeednoteImports().ListBatches(ctx, req.ProjectID, 0, 100)
	if err != nil || batches != 0 {
		t.Fatalf("preview wrote batches: %d, %v", batches, err)
	}
	_, posts, err := repo.SeednotePosts().ListByProject(ctx, req.ProjectID, "", 0, 100)
	if err != nil || posts != 0 {
		t.Fatalf("preview wrote posts: %d, %v", posts, err)
	}
	req.Selections = []AnalyticsSelection{{SourceRow: 2, Target: AnalyticsTarget{Kind: "task", ID: taskID}}}
	imported, err := svc.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Batch.ResolvedRows != 1 || imported.Batch.ReviewRows != 0 || len(imported.Rows) != 1 || imported.Batch.TotalRows != 1 || imported.Batch.InvalidRows != 0 {
		t.Fatalf("import = %+v", imported)
	}
	post, err := repo.SeednotePosts().FindByID(ctx, req.ProjectID, imported.Rows[0].PostID)
	if err != nil || post.TaskID != taskID || post.Genre != "图文" {
		t.Fatalf("post=%+v err=%v", post, err)
	}
	// Reimport through a platform alias must reuse the same task-backed identity.
	req.Selections[0].Target = AnalyticsTarget{Kind: "seednote_post", ID: post.ID}
	repeated, err := svc.Import(ctx, req)
	if err != nil || repeated.Rows[0].PostID != post.ID {
		t.Fatalf("repeat=%+v err=%v", repeated, err)
	}
	after, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("task changed: before=%+v after=%+v err=%v", before, after, err)
	}
	trackings, err := repo.SeednoteTrackings().FindByTaskIDs(ctx, req.UserID, req.ProjectID, []string{taskID})
	if err != nil || len(trackings) != 0 {
		t.Fatalf("import wrote tracking: %+v %v", trackings, err)
	}
	overview, err := svc.Overview(ctx, req.UserID, req.ProjectID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.PostSummaries) != 1 || overview.PostSummaries[0].Genre != "图文" || overview.PostSummaries[0].ContentType != "图文" {
		t.Fatalf("summary=%+v", overview.PostSummaries)
	}
	raw, _ := json.Marshal(overview.Posts[0])
	if !bytes.Contains(raw, []byte(`"content_type":"图文"`)) {
		t.Fatalf("native post missing type: %s", raw)
	}
}

func TestSeednoteImportRejectsInvalidSelectionsWithoutWrites(t *testing.T) {
	for _, tc := range []string{"empty", "foreign task", "unknown row", "invalid row", "duplicate row", "duplicate canonical target"} {
		t.Run(tc, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, req, taskID := seednoteSelectionFixture(t)
			target := AnalyticsTarget{Kind: "task", ID: taskID}
			req.Selections = []AnalyticsSelection{{SourceRow: 2, Target: target}}
			switch tc {
			case "empty":
				req.Selections = nil
			case "foreign task":
				foreignTask := uuid.NewString()
				if err := repo.Tasks().Create(ctx, &model.Task{ID: foreignTask, UserID: req.UserID, ProjectID: uuid.NewString(), Type: model.PlatformSeednote, Status: model.TaskStatusPending}); err != nil {
					t.Fatal(err)
				}
				req.Selections[0].Target.ID = foreignTask
			case "unknown row":
				req.Selections[0].SourceRow = 999
			case "invalid row":
				req.Selections[0].SourceRow = 4
			case "duplicate row":
				req.Selections = append(req.Selections, req.Selections[0])
			case "duplicate canonical target":
				post := &model.SeednotePost{ID: uuid.NewString(), UserID: req.UserID, ProjectID: req.ProjectID, TaskID: taskID, Title: "关联笔记"}
				if err := repo.SeednotePosts().Create(ctx, post); err != nil {
					t.Fatal(err)
				}
				req.Selections = append(req.Selections, AnalyticsSelection{SourceRow: 3, Target: AnalyticsTarget{Kind: "seednote_post", ID: post.ID}})
			}
			if _, err := svc.Import(ctx, req); err == nil {
				t.Fatal("invalid selection accepted")
			}
			_, total, err := repo.SeednoteImports().ListBatches(ctx, req.ProjectID, 0, 100)
			if err != nil || total != 0 {
				t.Fatalf("rejection wrote batches: %d %v", total, err)
			}
			versions, err := repo.SeednoteMetricVersions().FindByProject(ctx, req.ProjectID, nil, nil)
			if err != nil || len(versions) != 0 {
				t.Fatalf("rejection wrote metrics: %d %v", len(versions), err)
			}
		})
	}
}

func TestSeednotePreviewUsesSameProjectAliases(t *testing.T) {
	ctx := context.Background()
	svc, repo, req, _ := seednoteSelectionFixture(t)
	post := &model.SeednotePost{ID: uuid.NewString(), UserID: req.UserID, ProjectID: req.ProjectID, Title: "已改标题"}
	if err := repo.SeednotePosts().Create(ctx, post); err != nil {
		t.Fatal(err)
	}
	if err := repo.SeednotePostAliases().Create(ctx, &model.SeednotePostAlias{ID: uuid.NewString(), BatchID: seednoteActiveAliasBatch(t, repo, post), PostID: post.ID, NormalizedTitle: "无关联笔记"}); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(ctx, req)
	if err != nil || preview.Rows[1].Target == nil || preview.Rows[1].Target.ID != post.ID {
		t.Fatalf("preview=%+v err=%v", preview, err)
	}
}

func TestSeednoteImportExplicitSelectionCanOverridePreview(t *testing.T) {
	for _, status := range []string{model.TaskStatusPending, model.TaskStatusRunning, model.TaskStatusFailed} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			svc, repo, req, taskID := seednoteSelectionFixture(t)
			task, err := repo.Tasks().FindByID(ctx, taskID)
			if err != nil {
				t.Fatal(err)
			}
			task.Status = status
			if err := repo.Tasks().Update(ctx, task); err != nil {
				t.Fatal(err)
			}
			req.Selections = []AnalyticsSelection{{SourceRow: 3, Target: AnalyticsTarget{Kind: "task", ID: taskID}}}
			imported, err := svc.Import(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			if len(imported.Rows) != 1 || imported.Rows[0].SourceRow != 3 || imported.Rows[0].PostID == "" {
				t.Fatalf("rows=%+v", imported.Rows)
			}
			after, err := repo.Tasks().FindByID(ctx, taskID)
			if err != nil || after.Status != status {
				t.Fatalf("import changed task status: %+v %v", after, err)
			}
		})
	}
}

func TestSeednotePreviewConflictingAliasNeedsReview(t *testing.T) {
	ctx := context.Background()
	svc, repo, req, _ := seednoteSelectionFixture(t)
	for _, title := range []string{"无关联笔记", "更名笔记"} {
		post := &model.SeednotePost{ID: uuid.NewString(), UserID: req.UserID, ProjectID: req.ProjectID, Title: title}
		if err := repo.SeednotePosts().Create(ctx, post); err != nil {
			t.Fatal(err)
		}
		if title != "无关联笔记" {
			if err := repo.SeednotePostAliases().Create(ctx, &model.SeednotePostAlias{ID: uuid.NewString(), BatchID: seednoteActiveAliasBatch(t, repo, post), PostID: post.ID, NormalizedTitle: "无关联笔记"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	preview, err := svc.Preview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Rows[1].Target != nil || preview.Rows[1].MatchStatus != "needs_review" {
		t.Fatalf("ambiguous row=%+v", preview.Rows[1])
	}
}

func seednoteActiveAliasBatch(t *testing.T, repo repository.Repository, post *model.SeednotePost) string {
	t.Helper()
	batch := &model.SeednoteImportBatch{ID: uuid.NewString(), UserID: post.UserID, ProjectID: post.ProjectID, Status: model.SeednoteImportBatchStatusCompleted, FileName: "data.xlsx"}
	if err := repo.SeednoteImports().CreateBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	return batch.ID
}

func TestSeednoteRevokedImportCannotTeachFutureMatching(t *testing.T) {
	ctx := context.Background()
	svc, repo, req, taskID := seednoteSelectionFixture(t)
	// The source row has a different title; explicitly associate it to task A.
	req.Selections = []AnalyticsSelection{{SourceRow: 3, Target: AnalyticsTarget{Kind: "task", ID: taskID}}}
	imported, err := svc.Import(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	post, err := repo.SeednotePosts().FindByID(ctx, req.ProjectID, imported.Rows[0].PostID)
	if err != nil {
		t.Fatal(err)
	}
	if post.FirstPublishedAtBatchID != imported.Batch.ID {
		t.Fatalf("missing publication-date provenance: %+v", post)
	}
	activeDate, err := reliableSeednotePostPublishedAt(ctx, repo, post)
	if err != nil || activeDate == nil {
		t.Fatalf("active date=%v err=%v", activeDate, err)
	}
	before, err := svc.Preview(ctx, req)
	if err != nil || before.Rows[1].Target == nil || before.Rows[1].Target.ID != taskID {
		t.Fatalf("active alias not matched: %+v err=%v", before, err)
	}
	if _, err := svc.Revoke(ctx, req.UserID, req.ProjectID, imported.Batch.ID); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Preview(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if after.Rows[1].Target != nil || after.Rows[1].MatchStatus != "unmatched" {
		t.Fatalf("revoked alias still matches task A: %+v", after.Rows[1])
	}
	revokedDate, err := reliableSeednotePostPublishedAt(ctx, repo, post)
	if err != nil || revokedDate != nil {
		t.Fatalf("revoked date still evidence: %v err=%v", revokedDate, err)
	}
	candidates, err := loadAnalyticsCandidates(ctx, repo, req.UserID, req.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range candidates {
		if candidate.Target.ID == taskID && candidate.matchDate != nil {
			t.Fatalf("candidate retained revoked date: %+v", candidate)
		}
	}
}
