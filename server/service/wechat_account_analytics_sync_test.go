package service

import (
	"context"
	"errors"
	"testing"
	"time"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/glebarez/sqlite"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeWechatUserSummaryAPI struct {
	rows  []appwechat.UserSummaryItem
	calls []appwechat.UserSummaryRequest
}

func (f *fakeWechatUserSummaryAPI) GetUserSummary(_ context.Context, request appwechat.UserSummaryRequest) (*appwechat.UserSummaryResponse, error) {
	f.calls = append(f.calls, request)
	rows := make([]appwechat.UserSummaryItem, 0)
	for _, row := range f.rows {
		if row.RefDate >= request.BeginDate && row.RefDate <= request.EndDate {
			rows = append(rows, row)
		}
	}
	return &appwechat.UserSummaryResponse{List: rows}, nil
}

func TestWechatAccountAnalyticsSyncPersistsDailyGrowthAndSourceFactsIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatOpenPlatformAuthorizations().Upsert(ctx, &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: project.ID, UserID: project.UserID, AuthorizerAppID: "wx-authorizer",
		GrantedScopes: []int{2}, Status: model.WechatAuthorizationStatusConnected, AuthorizedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	api := &fakeWechatUserSummaryAPI{rows: []appwechat.UserSummaryItem{
		{RefDate: "2026-10-01", UserSource: 1, NewUser: 3, CancelUser: 1},
		{RefDate: "2026-10-01", UserSource: 30, NewUser: 4, CancelUser: 0},
		{RefDate: "2026-10-02", UserSource: 1},
		{RefDate: "2026-10-03", UserSource: 1},
		{RefDate: "2026-10-04", UserSource: 1},
		{RefDate: "2026-10-05", UserSource: 1},
		{RefDate: "2026-10-06", UserSource: 1},
		{RefDate: "2026-10-07", UserSource: 1},
		{RefDate: "2026-10-08", UserSource: 1},
		{RefDate: "2026-10-09", UserSource: 1, NewUser: 2, CancelUser: 1},
		{RefDate: "2026-10-10", UserSource: 1},
	}}
	svc := NewWechatAccountAnalyticsSyncService(repo, func(context.Context, *model.Project, *model.WechatOpenPlatformAuthorization) (WechatUserSummaryAPI, error) {
		return api, nil
	})
	svc.now = func() time.Time { return time.Date(2026, 10, 11, 4, 0, 0, 0, time.UTC) }

	first, err := svc.Sync(ctx, project.UserID, project.ID, "2026-10-01", "2026-10-10")
	if err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != 2 || api.calls[0].BeginDate != "2026-10-01" || api.calls[0].EndDate != "2026-10-07" || api.calls[1].BeginDate != "2026-10-08" || api.calls[1].EndDate != "2026-10-10" {
		t.Fatalf("API date windows = %#v, want inclusive 7-day chunks", api.calls)
	}
	if first.AccountDays != 10 || first.From != "2026-10-01" || first.To != "2026-10-10" {
		t.Fatalf("sync result = %#v", first)
	}
	overview, err := NewWechatAccountAnalyticsService(repo).Overview(ctx, project.UserID, project.ID, first.From, first.To)
	if err != nil {
		t.Fatal(err)
	}
	if !overview.Coverage.Complete || overview.Totals["follower_gain_count"] == nil || *overview.Totals["follower_gain_count"] != 9 ||
		overview.Totals["follower_cancel_count"] == nil || *overview.Totals["follower_cancel_count"] != 2 ||
		overview.Totals["follower_net_gain_count"] == nil || *overview.Totals["follower_net_gain_count"] != 7 {
		t.Fatalf("daily follower totals = %#v coverage=%#v", overview.Totals, overview.Coverage)
	}
	traffic, err := NewWechatAccountAnalyticsService(repo).TrafficSources(ctx, project.UserID, project.ID, first.From, first.To, "follower_gain_count", "all")
	if err != nil {
		t.Fatal(err)
	}
	if traffic.Total == nil || *traffic.Total != 9 || len(traffic.Items) != 2 {
		t.Fatalf("follower acquisition sources = %#v", traffic)
	}
	values := map[string]int64{}
	for _, item := range traffic.Items {
		if item.Value != nil {
			values[item.Source] = *item.Value
		}
	}
	if values["search"] != 5 || values["qr_code"] != 4 {
		t.Fatalf("source values = %#v, want search=5 and QR=4", values)
	}

	second, err := svc.Sync(ctx, project.UserID, project.ID, first.From, first.To)
	if err != nil {
		t.Fatal(err)
	}
	if second.Batch.ID != first.Batch.ID || second.Revision != first.Revision {
		t.Fatalf("replayed sync = %#v, want same batch/revision as %#v", second, first)
	}
	batches, total, err := repo.WechatAccountAnalytics().ListBatches(ctx, project.ID, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(batches) != 1 {
		t.Fatalf("sync batches = %d %#v, want one idempotent batch", total, batches)
	}
}

func TestWechatAccountAnalyticsSyncRequiresConnectedAccountScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status string
		scopes []int
	}{
		{name: "revoked", status: model.WechatAuthorizationStatusRevoked, scopes: []int{2}},
		{name: "missing user management scope", status: model.WechatAuthorizationStatusConnected, scopes: []int{7}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			if err := model.AutoMigrate(db); err != nil {
				t.Fatal(err)
			}
			repo := repository.New(db)
			ctx := context.Background()
			project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformWechat, Name: "公众号"}
			if err := repo.Projects().Create(ctx, project); err != nil {
				t.Fatal(err)
			}
			if err := repo.WechatOpenPlatformAuthorizations().Upsert(ctx, &model.WechatOpenPlatformAuthorization{
				ID: uuid.NewString(), ProjectID: project.ID, UserID: project.UserID, AuthorizerAppID: "wx-authorizer",
				GrantedScopes: tc.scopes, Status: tc.status, AuthorizedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatal(err)
			}
			factoryCalls := 0
			svc := NewWechatAccountAnalyticsSyncService(repo, func(context.Context, *model.Project, *model.WechatOpenPlatformAuthorization) (WechatUserSummaryAPI, error) {
				factoryCalls++
				return &fakeWechatUserSummaryAPI{}, nil
			})
			_, err = svc.Sync(ctx, project.UserID, project.ID, "2026-10-01", "2026-10-02")
			if err == nil || factoryCalls != 0 {
				t.Fatalf("Sync error=%v, API factory calls=%d", err, factoryCalls)
			}
		})
	}
}

func TestWechatAccountAnalyticsSyncCapsRequestedWindowToYesterday(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	ctx := context.Background()
	project := &model.Project{ID: uuid.NewString(), UserID: uuid.NewString(), Platform: model.PlatformWechat, Name: "公众号"}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := repo.WechatOpenPlatformAuthorizations().Upsert(ctx, &model.WechatOpenPlatformAuthorization{
		ID: uuid.NewString(), ProjectID: project.ID, UserID: project.UserID, AuthorizerAppID: "wx-authorizer",
		GrantedScopes: []int{2}, Status: model.WechatAuthorizationStatusConnected, AuthorizedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	api := &fakeWechatUserSummaryAPI{}
	svc := NewWechatAccountAnalyticsSyncService(repo, func(context.Context, *model.Project, *model.WechatOpenPlatformAuthorization) (WechatUserSummaryAPI, error) {
		return api, nil
	})
	svc.now = func() time.Time { return time.Date(2026, 10, 11, 4, 0, 0, 0, time.UTC) }
	result, err := svc.Sync(ctx, project.UserID, project.ID, "2026-10-10", "2026-10-11")
	if err != nil {
		t.Fatal(err)
	}
	if result.To != "2026-10-10" || result.AccountDays != 0 || len(api.calls) != 1 || api.calls[0].EndDate != "2026-10-10" {
		t.Fatalf("sync result = %#v API calls=%#v, expected to stop at yesterday", result, api.calls)
	}
	overview, err := NewWechatAccountAnalyticsService(repo).Overview(ctx, project.UserID, project.ID, result.From, result.To)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Coverage.Complete || overview.Totals["follower_gain_count"] != nil || overview.Series[0]["follower_gain_count"] != nil {
		t.Fatalf("missing API rows were presented as zero: coverage=%#v totals=%#v series=%#v", overview.Coverage, overview.Totals, overview.Series)
	}
	_, err = svc.Sync(ctx, project.UserID, project.ID, "2026-09-01", "2026-10-11")
	if !errors.Is(err, ErrWechatAccountAnalyticsSyncRange) || len(api.calls) != 1 {
		t.Fatalf("overlong sync error=%v API calls=%d, want validation before external requests", err, len(api.calls))
	}
}
