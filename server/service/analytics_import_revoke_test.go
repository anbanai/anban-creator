package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

func TestSeednoteRevokeImportRestoresPreviousMetricsAndRetainsHistory(t *testing.T) {
	ctx := context.Background()
	_, repo, _ := setupSeednoteTrackingServiceTest(t)
	userID, projectID, _ := createSeednoteTrackingFixtures(t, repo)
	svc := NewSeednoteImportService(repo, nil)
	post := &model.SeednotePost{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Title: "笔记"}
	if err := repo.SeednotePosts().Create(ctx, post); err != nil {
		t.Fatal(err)
	}
	var batches []*model.SeednoteImportBatch
	var rows []*model.SeednoteImportRow
	date := time.Now()
	for i, count := range []int64{10, 99} {
		batch := &model.SeednoteImportBatch{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Status: "completed", FileName: "数据.xlsx", DataAsOfAt: date}
		if err := repo.SeednoteImports().CreateBatch(ctx, batch); err != nil {
			t.Fatal(err)
		}
		row := &model.SeednoteImportRow{ID: uuid.NewString(), BatchID: batch.ID, ProjectID: projectID, PostID: post.ID, RawData: "{}", MatchStatus: "matched"}
		if err := repo.SeednoteImports().CreateRows(ctx, []*model.SeednoteImportRow{row}); err != nil {
			t.Fatal(err)
		}
		if err := repo.SeednoteMetricVersions().Create(ctx, &model.SeednoteMetricVersion{ID: uuid.NewString(), PostID: post.ID, BatchID: batch.ID, ImportRowID: row.ID, ExposureCount: &count, DataAsOfAt: date, ImportedAt: date.Add(time.Duration(i) * time.Hour), RawData: "{}"}); err != nil {
			t.Fatal(err)
		}
		batches, rows = append(batches, batch), append(rows, row)
	}
	before, err := svc.Overview(ctx, userID, projectID, nil, nil)
	if err != nil || before.PostSummaries[0].ExposureCount != 99 {
		t.Fatalf("before=%+v err=%v", before, err)
	}
	if _, err := svc.Revoke(ctx, "other-user", projectID, batches[1].ID); err == nil {
		t.Fatal("unauthorized revoke succeeded")
	}
	if _, err := svc.Revoke(ctx, userID, "other-project", batches[1].ID); err == nil {
		t.Fatal("cross-project revoke succeeded")
	}
	result, err := svc.Revoke(ctx, userID, projectID, batches[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Batch.Status != "revoked" || result.Batch.RevokedAt == nil || len(result.Rows) != 1 {
		t.Fatalf("history=%+v", result)
	}
	repeated, err := svc.Revoke(ctx, userID, projectID, batches[1].ID)
	if err != nil || !repeated.Batch.RevokedAt.Equal(*result.Batch.RevokedAt) {
		t.Fatalf("repeat err=%v result=%+v", err, repeated)
	}
	if _, err := svc.Resolve(ctx, userID, projectID, batches[1].ID, []SeednoteResolveAction{{RowID: rows[1].ID, Action: "create_new"}}); !errors.Is(err, ErrAnalyticsImportRevoked) {
		t.Fatalf("resolve err=%v", err)
	}
	after, err := svc.Overview(ctx, userID, projectID, nil, nil)
	if err != nil || after.PostSummaries[0].ExposureCount != 10 {
		t.Fatalf("after=%+v err=%v", after, err)
	}
	_, versions, err := svc.GetPost(ctx, userID, projectID, post.ID, nil, nil)
	if err != nil || len(versions) != 1 || versions[0].BatchID != batches[0].ID {
		t.Fatalf("versions=%+v err=%v", versions, err)
	}
	if _, err := svc.Revoke(ctx, userID, projectID, batches[0].ID); err != nil {
		t.Fatal(err)
	}
	empty, err := svc.Overview(ctx, userID, projectID, nil, nil)
	if err != nil || len(empty.Series) != 0 || len(empty.PostSummaries) != 0 {
		t.Fatalf("empty=%+v err=%v", empty, err)
	}
}

func TestWechatRevokeImportRestoresMetricsAndOwnedLink(t *testing.T) {
	for _, tc := range []struct {
		name, existingURL string
		first             int
	}{
		{name: "transfer URL to remaining batch", first: 0},
		{name: "restore previous metrics", first: 1},
		{name: "preserve manual URL", existingURL: "https://mp.weixin.qq.com/s/manual", first: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existingURL := tc.existingURL
			ctx := context.Background()
			f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", existingURL, time.Now())
			svc := NewWechatAnalyticsImportService(f.repo, nil)
			var batches []*model.WechatAnalyticsImportBatch
			var rows []*model.WechatAnalyticsImportRow
			for i, count := range []int64{10, 99} {
				batch := &model.WechatAnalyticsImportBatch{ID: uuid.NewString(), UserID: f.userID, ProjectID: f.projectID, Status: "completed", DataAsOfAt: time.Now().Add(time.Duration(i) * time.Hour)}
				if err := f.repo.WechatAnalyticsImports().CreateBatch(ctx, batch); err != nil {
					t.Fatal(err)
				}
				row := &model.WechatAnalyticsImportRow{ID: uuid.NewString(), ProjectID: f.projectID, BatchID: batch.ID, PublicationID: f.publication.ID, ArticleURL: "https://mp.weixin.qq.com/s/imported", RawData: `{"内容url":"https://mp.weixin.qq.com/s/imported"}`, ReadUsers: &count, MatchStatus: "matched"}
				if err := f.repo.WechatAnalyticsImports().CreateRows(ctx, []*model.WechatAnalyticsImportRow{row}); err != nil {
					t.Fatal(err)
				}
				if err := f.repo.WechatAnalyticsImports().CreateSnapshot(ctx, snapshotFromWechatImport(batch, row, time.Now())); err != nil {
					t.Fatal(err)
				}
				if err := f.repo.WechatPublications().RecordImportedAnalytics(ctx, f.projectID, f.publication.ID, batch.ID, row.ArticleURL); err != nil {
					t.Fatal(err)
				}
				batches, rows = append(batches, batch), append(rows, row)
			}
			staleTracking, err := f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.Revoke(ctx, "other-user", f.projectID, batches[1].ID); err == nil {
				t.Fatal("unauthorized revoke succeeded")
			}
			if _, err := svc.Revoke(ctx, f.userID, "other-project", batches[1].ID); err == nil {
				t.Fatal("cross-project revoke succeeded")
			}
			// Exercise both revocation orders while another valid observation remains.
			if _, err := svc.Revoke(ctx, f.userID, f.projectID, batches[tc.first].ID); err != nil {
				t.Fatal(err)
			}
			p, _ := f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
			if p.ArticleURL == "" {
				t.Fatal("remaining import URL was lost")
			}
			remaining, err := svc.Overview(ctx, f.userID, f.projectID)
			wantReads := int64(99)
			if tc.first == 1 {
				wantReads = 10
			}
			if err != nil || remaining.ReadUsers != wantReads {
				t.Fatalf("remaining=%+v err=%v", remaining, err)
			}
			result, err := svc.Revoke(ctx, f.userID, f.projectID, batches[1-tc.first].ID)
			if err != nil {
				t.Fatal(err)
			}
			if result.Batch.Status != "revoked" || result.Batch.RevokedAt == nil || len(result.Rows) != 1 {
				t.Fatalf("history=%+v", result)
			}
			if _, err := svc.Resolve(ctx, f.userID, f.projectID, batches[1].ID, []WechatAnalyticsResolveAction{{RowID: rows[1].ID, Action: "link_existing", PublicationID: f.publication.ID}}); !errors.Is(err, ErrAnalyticsImportRevoked) {
				t.Fatalf("resolve err=%v", err)
			}
			if _, err := svc.Revoke(ctx, f.userID, f.projectID, batches[1].ID); err != nil {
				t.Fatal(err)
			}
			p, _ = f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
			tracking, _ := f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
			if p.ArticleURL != existingURL || tracking.ArticleURL != existingURL || p.Status != "published" || p.AnalyticsStatus == "import_available" {
				t.Fatalf("publication=%+v tracking=%+v", p, tracking)
			}
			// A metrics request started before revocation must not restore the URL.
			staleTracking.RunCount++
			if err := f.repo.WechatTrackings().Update(ctx, staleTracking); err != nil {
				t.Fatal(err)
			}
			tracking, _ = f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
			if tracking.ArticleURL != existingURL {
				t.Fatalf("stale metrics restored URL: %s", tracking.ArticleURL)
			}
			overview, err := svc.Overview(ctx, f.userID, f.projectID)
			if err != nil || overview.WithData != 0 || overview.ReadUsers != 0 {
				t.Fatalf("overview=%+v err=%v", overview, err)
			}
			// Official observations are independent of withdrawn spreadsheet batches.
			if err := f.repo.WechatMetricSnapshots().Create(ctx, &model.WechatMetricSnapshot{ID: uuid.NewString(), TrackingID: f.tracking.ID, TaskID: f.taskID, StatDate: "2026-09-20", ReadUsers: 7, RawResponse: []byte("{}")}); err != nil {
				t.Fatal(err)
			}
			overview, err = svc.Overview(ctx, f.userID, f.projectID)
			if err != nil || overview.ReadUsers != 7 {
				t.Fatalf("official=%+v err=%v", overview, err)
			}
		})
	}
}

func TestWechatRevokePreservesUserConfirmedImportedURL(t *testing.T) {
	ctx := context.Background()
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", "", time.Now())
	batch := &model.WechatAnalyticsImportBatch{ID: uuid.NewString(), UserID: f.userID, ProjectID: f.projectID, Status: "completed", DataAsOfAt: time.Now()}
	if err := f.repo.WechatAnalyticsImports().CreateBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	articleURL := "https://mp.weixin.qq.com/s/confirmed"
	if err := f.repo.WechatPublications().RecordImportedAnalytics(ctx, f.projectID, f.publication.ID, batch.ID, articleURL); err != nil {
		t.Fatal(err)
	}
	publicationService := NewWechatPublicationService(f.repo, nil, nil)
	if _, err := publicationService.BindManualPublication(ctx, f.userID, f.taskID, articleURL); err != nil {
		t.Fatal(err)
	}
	svc := NewWechatAnalyticsImportService(f.repo, nil)
	if _, err := svc.Revoke(ctx, f.userID, f.projectID, batch.ID); err != nil {
		t.Fatal(err)
	}
	publication, err := f.repo.WechatPublications().FindByID(ctx, f.publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	tracking, err := f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if publication.ArticleURL != articleURL || tracking.ArticleURL != articleURL || publication.ArticleURLImportBatchID != "" || tracking.ArticleURLImportBatchID != "" {
		t.Fatalf("confirmed URLs changed: publication=%+v tracking=%+v", publication, tracking)
	}
}

type revokeBeforeConfirmationRepository struct {
	repository.Repository
	before func()
}

func (r *revokeBeforeConfirmationRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	if r.before != nil {
		before := r.before
		r.before = nil
		before()
	}
	return r.Repository.WithTx(ctx, fn)
}

func TestWechatManualBindingAfterRevoke(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		name := "bind after revoke"
		if concurrent {
			name = "revoke during confirmation"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", "", time.Now())
			f.publication.DraftMediaID = "draft-1"
			if err := f.repo.WechatPublications().Update(ctx, f.publication); err != nil {
				t.Fatal(err)
			}
			batch := &model.WechatAnalyticsImportBatch{ID: uuid.NewString(), UserID: f.userID, ProjectID: f.projectID, Status: "completed", DataAsOfAt: time.Now()}
			if err := f.repo.WechatAnalyticsImports().CreateBatch(ctx, batch); err != nil {
				t.Fatal(err)
			}
			articleURL := "https://mp.weixin.qq.com/s/imported"
			if err := f.repo.WechatPublications().RecordImportedAnalytics(ctx, f.projectID, f.publication.ID, batch.ID, articleURL); err != nil {
				t.Fatal(err)
			}
			svc := NewWechatAnalyticsImportService(f.repo, nil)
			revoke := func() {
				if _, err := svc.Revoke(ctx, f.userID, f.projectID, batch.ID); err != nil {
					t.Fatal(err)
				}
			}
			publicationService := NewWechatPublicationService(f.repo, nil, nil)
			if concurrent {
				publicationService.repo = &revokeBeforeConfirmationRepository{Repository: f.repo, before: revoke}
			} else {
				revoke()
				articleURL = "https://mp.weixin.qq.com/s/correct"
			}
			_, err := publicationService.BindManualPublication(ctx, f.userID, f.taskID, articleURL)
			if concurrent {
				if !errors.Is(err, ErrWechatPublicationConflict) {
					t.Fatalf("stale confirmation err=%v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tracking, err := f.repo.WechatTrackings().FindByTaskID(ctx, f.taskID)
			if err != nil {
				t.Fatal(err)
			}
			if tracking.ArticleURL != articleURL || tracking.ArticleURLImportBatchID != "" {
				t.Fatalf("tracking identity not restored: %+v", tracking)
			}
		})
	}
}
