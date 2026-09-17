package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeWechatDetailPlatform struct {
	response *appwechat.ArticleTotalDetailResponse
	err      error
	calls    int
	dates    []string
}

type blockingWechatRecoveryEnqueuer struct {
	entered chan struct{}
	release chan struct{}
	err     error
	mu      sync.Mutex
	calls   int
}

func (e *blockingWechatRecoveryEnqueuer) Enqueue(string, []byte) error {
	e.mu.Lock()
	e.calls++
	e.mu.Unlock()
	if e.entered != nil {
		e.entered <- struct{}{}
	}
	if e.release != nil {
		<-e.release
	}
	return e.err
}

func (e *blockingWechatRecoveryEnqueuer) EnqueueIn(string, []byte, time.Duration) error { return nil }

func (e *blockingWechatRecoveryEnqueuer) callCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls
}

func (f *fakeWechatDetailPlatform) FetchArticleTotalDetail(_ context.Context, _ *model.Project, publicationDate string) (*appwechat.ArticleTotalDetailResponse, error) {
	f.calls++
	f.dates = append(f.dates, publicationDate)
	return f.response, f.err
}

type wechatTrackingFixture struct {
	svc         *WechatTrackingService
	repo        repository.Repository
	provider    *fakeWechatDetailPlatform
	now         time.Time
	userID      string
	projectID   string
	taskID      string
	publication *model.WechatPublication
	tracking    *model.WechatArticleTracking
}

func newWechatTrackingFixture(t *testing.T, source, msgDataID, msgID, articleURL string, publishedAt time.Time) *wechatTrackingFixture {
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
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID, taskID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: uuid.NewString()[:12]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	publication := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: taskID, UserID: userID, ProjectID: projectID,
		Source: source, Status: model.WechatPublicationStatusPublished, MsgDataID: msgDataID, MsgID: msgID,
		ArticleID: "article-1", ArticleURL: articleURL, ArticleIndex: 1, PublishedAt: &publishedAt,
	}
	if err := repo.WechatPublications().Create(ctx, publication); err != nil {
		t.Fatal(err)
	}
	now := publishedAt.Add(12 * time.Hour)
	next := now
	tracking := &model.WechatArticleTracking{
		ID: uuid.NewString(), TaskID: taskID, UserID: userID, ProjectID: projectID, PublicationID: publication.ID,
		Source: source, Status: model.WechatTrackingStatusWaitingData, ArticleID: publication.ArticleID,
		MsgDataID: msgDataID, MsgID: msgID, ArticleURL: articleURL, PublishedAt: publishedAt,
		ExpiresAt: publishedAt.Add(model.WechatTrackingWindow), NextFetchAt: &next,
	}
	if err := repo.WechatTrackings().Create(ctx, tracking); err != nil {
		t.Fatal(err)
	}
	provider := &fakeWechatDetailPlatform{}
	logger := zerolog.New(io.Discard)
	svc := NewWechatTrackingService(repo, provider, nil, &logger)
	svc.now = func() time.Time { return now }
	return &wechatTrackingFixture{svc: svc, repo: repo, provider: provider, now: now, userID: userID, projectID: projectID, taskID: taskID, publication: publication, tracking: tracking}
}

func TestWechatTrackingCaptureMatchesExactAPICompositeMsgIDAndMapsAllMetrics(t *testing.T) {
	published := time.Date(2026, 8, 1, 9, 30, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg-data-1", "msg-data-1_1", "https://mp.weixin.qq.com/s/api", published)
	f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{
		{MsgID: "wrong_1", ContentURL: f.tracking.ArticleURL, Title: "same title", DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-01", ReadUser: 999}}},
		{MsgID: "msg-data-1_1", ContentURL: "https://mp.weixin.qq.com/s/other", Title: "other title", DetailList: []appwechat.ArticleTotalDetailMetric{
			{StatDate: "2026-08-01", ReadUser: 11, ShareUser: 12, CollectionUser: 13, LikeUser: 14, ZaikanUser: 15, CommentCount: 16, ReadFinishRate: 0.625, ReadAvgActiveTime: 37.25, ReadSubscribeUser: 17},
			{StatDate: "2026-08-02", ReadUser: 21, ShareUser: 22, CollectionUser: 23, LikeUser: 24, ZaikanUser: 25, CommentCount: 26, ReadFinishRate: 0.75, ReadAvgActiveTime: 48.5, ReadSubscribeUser: 27},
		}},
	}}

	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	if f.provider.calls != 1 || len(f.provider.dates) != 1 || f.provider.dates[0] != "2026-08-01" {
		t.Fatalf("detail requests = calls:%d dates:%v", f.provider.calls, f.provider.dates)
	}
	snapshots, err := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("snapshots = %d, want every detail date", len(snapshots))
	}
	first, second := snapshots[0], snapshots[1]
	if first.StatDate != "2026-08-01" || first.ReadUsers != 11 || first.ShareUsers != 12 || first.CollectionUsers != 13 || first.LikeUsers != 14 || first.ZaikanUsers != 15 || first.CommentCount != 16 || first.ReadFinishRate != 0.625 || first.AverageReadActiveTime != 37.25 || first.ReadToSubscribeUsers != 17 {
		t.Fatalf("first metric mapping = %#v", first)
	}
	if second.StatDate != "2026-08-02" || second.ReadUsers != 21 || !json.Valid(second.RawResponse) || len(second.RawResponse) == 0 {
		t.Fatalf("second metric/raw response = %#v", second)
	}
	updated, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	if updated.Status != model.WechatTrackingStatusTracking || updated.LastFetchAt == nil || updated.NextFetchAt == nil || updated.RunCount != 1 {
		t.Fatalf("tracking after capture = %#v", updated)
	}
}

func TestWechatTrackingCaptureMatchesManualPublicationByExactContentURLAndPersistsMsgID(t *testing.T) {
	published := time.Date(2026, 8, 4, 10, 0, 0, 0, wechatAnalyticsLocation)
	articleURL := "https://mp.weixin.qq.com/s/manual?mid=1&idx=1"
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", articleURL, published)
	f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{
		{MsgID: "wrong_1", ContentURL: articleURL + "&share=1", Title: "same title", DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-04", ReadUser: 999}}},
		{MsgID: "manual-msg_1", ContentURL: articleURL, Title: "different title", DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-04", ReadUser: 42}}},
	}}

	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	publication, _ := f.repo.WechatPublications().FindByID(context.Background(), f.publication.ID)
	if tracking.MsgID != "manual-msg_1" || publication.MsgID != "manual-msg_1" {
		t.Fatalf("persisted msgids = tracking:%q publication:%q", tracking.MsgID, publication.MsgID)
	}
	snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	if len(snapshots) != 1 || snapshots[0].ReadUsers != 42 {
		t.Fatalf("manual snapshots = %#v", snapshots)
	}
}

func TestWechatTrackingCaptureNeverFallsBackToTitleOrWrongIdentity(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		msgDataID  string
		msgID      string
		articleURL string
		item       appwechat.ArticleTotalDetailItem
	}{
		{name: "api URL and title cannot replace msgid", source: model.WechatPublicationSourceAnbanAPI, msgDataID: "wanted", msgID: "wanted_1", articleURL: "https://mp.weixin.qq.com/s/api", item: appwechat.ArticleTotalDetailItem{MsgID: "other_1", ContentURL: "https://mp.weixin.qq.com/s/api", Title: "same title"}},
		{name: "manual title cannot replace URL", source: model.WechatPublicationSourceWechatConsole, articleURL: "https://mp.weixin.qq.com/s/manual", item: appwechat.ArticleTotalDetailItem{MsgID: "other_1", ContentURL: "https://mp.weixin.qq.com/s/other", Title: "same title"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			published := time.Date(2026, 8, 5, 10, 0, 0, 0, wechatAnalyticsLocation)
			f := newWechatTrackingFixture(t, tt.source, tt.msgDataID, tt.msgID, tt.articleURL, published)
			tt.item.DetailList = []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-05", ReadUser: 99}}
			f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{tt.item}}
			if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
				t.Fatal(err)
			}
			snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
			tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
			if len(snapshots) != 0 || tracking.Status != model.WechatTrackingStatusWaitingData {
				t.Fatalf("fallback matched: tracking=%#v snapshots=%#v", tracking, snapshots)
			}
		})
	}
}

func TestWechatTrackingDelayedAndEmptyResponseRemainWaiting(t *testing.T) {
	published := time.Date(2026, 8, 6, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	f.provider.response = &appwechat.ArticleTotalDetailResponse{IsDelay: true, List: []appwechat.ArticleTotalDetailItem{}}

	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	if tracking.Status != model.WechatTrackingStatusWaitingData || tracking.NextFetchAt == nil || tracking.LastFetchAt == nil || tracking.LastError != "" {
		t.Fatalf("delayed tracking = %#v", tracking)
	}
}

func TestWechatTrackingDelayedResponseCannotMatchOrBindPopulatedData(t *testing.T) {
	published := time.Date(2026, 8, 6, 8, 0, 0, 0, wechatAnalyticsLocation)
	articleURL := "https://mp.weixin.qq.com/s/manual-delayed"
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", articleURL, published)
	f.provider.response = &appwechat.ArticleTotalDetailResponse{IsDelay: true, List: []appwechat.ArticleTotalDetailItem{{
		MsgID: "manual-delayed_1", ContentURL: articleURL,
		DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-06", ReadUser: 99}},
	}}}

	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	publication, _ := f.repo.WechatPublications().FindByID(context.Background(), f.publication.ID)
	snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	if tracking.Status != model.WechatTrackingStatusWaitingData || tracking.MsgID != "" || publication.MsgID != "" || len(snapshots) != 0 {
		t.Fatalf("delayed response mutated identity or metrics: tracking=%#v publication=%#v snapshots=%#v", tracking, publication, snapshots)
	}
}

func TestWechatTrackingTransientErrorRetriesBut48001BecomesUnsupported(t *testing.T) {
	published := time.Date(2026, 8, 7, 8, 0, 0, 0, wechatAnalyticsLocation)
	t.Run("transient", func(t *testing.T) {
		f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
		f.provider.err = errors.New("timeout")
		if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
			t.Fatal(err)
		}
		tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
		if tracking.Status != model.WechatTrackingStatusError || tracking.NextFetchAt == nil || tracking.LastError == "" {
			t.Fatalf("transient tracking = %#v", tracking)
		}
	})
	t.Run("unsupported", func(t *testing.T) {
		f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", "https://mp.weixin.qq.com/s/manual", published)
		f.provider.err = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}
		if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
			t.Fatal(err)
		}
		tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
		if tracking.Status != model.WechatTrackingStatusUnsupported || tracking.NextFetchAt != nil || tracking.MsgID != "" || f.provider.calls != 1 {
			t.Fatalf("unsupported tracking = %#v calls=%d", tracking, f.provider.calls)
		}
	})
}

func TestWechatTrackingFetchesAtMostOnceDailyAndExpiresAtThirtyDayBoundary(t *testing.T) {
	published := time.Date(2026, 8, 1, 10, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	current := time.Date(2026, 8, 2, 9, 0, 0, 0, wechatAnalyticsLocation)
	f.svc.now = func() time.Time { return current }
	f.provider.response = &appwechat.ArticleTotalDetailResponse{}

	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	current = current.Add(2 * time.Hour)
	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	if f.provider.calls != 1 {
		t.Fatalf("same-day provider calls = %d, want 1", f.provider.calls)
	}
	current = published.Add(model.WechatTrackingWindow)
	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	if f.provider.calls != 1 || tracking.Status != model.WechatTrackingStatusExpired || tracking.NextFetchAt != nil || tracking.ExpiredAt == nil {
		t.Fatalf("boundary tracking = %#v calls=%d", tracking, f.provider.calls)
	}
}

func TestWechatTrackingUpsertsEveryReturnedDateAndIsIdempotent(t *testing.T) {
	published := time.Date(2026, 8, 10, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	current := published.Add(12 * time.Hour)
	f.svc.now = func() time.Time { return current }
	f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{
		{StatDate: "2026-08-10", ReadUser: 10}, {StatDate: "2026-08-11", ReadUser: 20},
	}}}}
	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	first, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	firstID := first[0].ID

	current = current.Add(24 * time.Hour)
	f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{
		{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-10", ReadUser: 15}}},
		{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-12", ReadUser: 30}}},
	}}
	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	if len(snapshots) != 3 || snapshots[0].ID != firstID || snapshots[0].ReadUsers != 15 || snapshots[2].ReadUsers != 30 {
		t.Fatalf("upserted snapshots = %#v", snapshots)
	}
}

func TestWechatTrackingIdenticalDuplicateStatDateProducesOneSnapshot(t *testing.T) {
	published := time.Date(2026, 8, 11, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	metric := appwechat.ArticleTotalDetailMetric{StatDate: "2026-08-11", ReadUser: 10, ShareUser: 2, ReadFinishRate: 0.5}
	f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{
		{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{metric}},
		{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{metric}},
	}}

	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	if len(snapshots) != 1 || snapshots[0].ReadUsers != 10 || tracking.Status != model.WechatTrackingStatusTracking {
		t.Fatalf("identical duplicate result: tracking=%#v snapshots=%#v", tracking, snapshots)
	}
}

func TestWechatTrackingConflictingDuplicateStatDateIsOrderIndependentRetryableError(t *testing.T) {
	metrics := []appwechat.ArticleTotalDetailMetric{
		{StatDate: "2026-08-11", ReadUser: 10, ShareUser: 2},
		{StatDate: "2026-08-11", ReadUser: 11, ShareUser: 2},
	}
	var wantError string
	for _, order := range []struct {
		name    string
		metrics []appwechat.ArticleTotalDetailMetric
	}{
		{name: "AB", metrics: []appwechat.ArticleTotalDetailMetric{metrics[0], metrics[1]}},
		{name: "BA", metrics: []appwechat.ArticleTotalDetailMetric{metrics[1], metrics[0]}},
	} {
		t.Run(order.name, func(t *testing.T) {
			published := time.Date(2026, 8, 11, 8, 0, 0, 0, wechatAnalyticsLocation)
			f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
			f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{{MsgID: "msg_1", DetailList: order.metrics}}}

			if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
				t.Fatal(err)
			}
			snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
			tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
			if len(snapshots) != 0 || tracking.Status != model.WechatTrackingStatusError || tracking.NextFetchAt == nil || tracking.LastError == "" {
				t.Fatalf("conflicting duplicate result: tracking=%#v snapshots=%#v", tracking, snapshots)
			}
			if wantError == "" {
				wantError = tracking.LastError
			} else if tracking.LastError != wantError {
				t.Fatalf("order-dependent errors: got %q want %q", tracking.LastError, wantError)
			}
		})
	}
}

func TestWechatTrackingPersistsExactRawResponseBytesInEverySnapshot(t *testing.T) {
	published := time.Date(2026, 8, 12, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	raw := []byte(" {\n  \"is_delay\": false, \"unknown\": [1, 2], \"list\": []\n}\n")
	f.provider.response = &appwechat.ArticleTotalDetailResponse{
		RawResponse: raw,
		List: []appwechat.ArticleTotalDetailItem{{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{
			{StatDate: "2026-08-12", ReadUser: 12}, {StatDate: "2026-08-13", ReadUser: 13},
		}}},
	}

	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	if len(snapshots) != 2 {
		t.Fatalf("snapshots = %d, want 2", len(snapshots))
	}
	for _, snapshot := range snapshots {
		if !bytes.Equal(snapshot.RawResponse, raw) {
			t.Fatalf("snapshot raw response = %q, want exact %q", snapshot.RawResponse, raw)
		}
	}
}

func TestWechatTrackingManualMsgIDBindingIsTransactionalCAS(t *testing.T) {
	published := time.Date(2026, 8, 13, 8, 0, 0, 0, wechatAnalyticsLocation)
	articleURL := "https://mp.weixin.qq.com/s/manual-cas"
	tests := []struct {
		name             string
		publicationMsgID string
		missing          bool
		wantSuccess      bool
	}{
		{name: "same publication msgid binds empty tracking", publicationMsgID: "manual-cas_1", wantSuccess: true},
		{name: "conflicting publication msgid rolls back", publicationMsgID: "other_1"},
		{name: "missing publication rolls back", missing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newWechatTrackingFixture(t, model.WechatPublicationSourceWechatConsole, "", "", articleURL, published)
			if tt.missing {
				f.tracking.PublicationID = uuid.NewString()
				if err := f.repo.WechatTrackings().Update(context.Background(), f.tracking); err != nil {
					t.Fatal(err)
				}
			} else if tt.publicationMsgID != "" {
				f.publication.MsgID = tt.publicationMsgID
				if err := f.repo.WechatPublications().Update(context.Background(), f.publication); err != nil {
					t.Fatal(err)
				}
			}
			f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{{
				MsgID: "manual-cas_1", ContentURL: articleURL,
				DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-13", ReadUser: 13}},
			}}}

			if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
				t.Fatal(err)
			}
			tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
			snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
			if tt.wantSuccess {
				if tracking.MsgID != "manual-cas_1" || tracking.Status != model.WechatTrackingStatusTracking || len(snapshots) != 1 {
					t.Fatalf("idempotent CAS result: tracking=%#v snapshots=%#v", tracking, snapshots)
				}
				publication, _ := f.repo.WechatPublications().FindByID(context.Background(), f.publication.ID)
				if publication.MsgID != "manual-cas_1" {
					t.Fatalf("publication msgid = %q", publication.MsgID)
				}
				return
			}
			if tracking.MsgID != "" || tracking.Status != model.WechatTrackingStatusError || tracking.LastError == "" || tracking.NextFetchAt == nil || len(snapshots) != 0 {
				t.Fatalf("rejected CAS result: tracking=%#v snapshots=%#v", tracking, snapshots)
			}
			if !tt.missing {
				publication, _ := f.repo.WechatPublications().FindByID(context.Background(), f.publication.ID)
				if publication.MsgID != tt.publicationMsgID {
					t.Fatalf("conflict changed publication msgid to %q", publication.MsgID)
				}
			}
		})
	}
}

func TestWechatTrackingAnalyticsReadReturnsNewMetricsAndTrendForOwnerOnly(t *testing.T) {
	published := time.Date(2026, 8, 12, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{
		{StatDate: "2026-08-12", ReadUser: 10, ShareUser: 2}, {StatDate: "2026-08-13", ReadUser: 20, ShareUser: 4},
	}}}}
	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	analytics, err := f.svc.GetTaskAnalytics(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if analytics.Tracking == nil || analytics.Metrics == nil || analytics.Metrics.ReadUsers != 20 || len(analytics.Trend) != 2 || analytics.Trend[0].StatDate != "2026-08-12" {
		t.Fatalf("analytics = %#v", analytics)
	}
	if _, err := f.svc.GetTaskAnalytics(context.Background(), uuid.NewString(), f.taskID); err == nil {
		t.Fatal("non-owner read succeeded")
	}
}

func TestWechatTrackingRecoverDueCannotRegressConcurrentCapture(t *testing.T) {
	published := time.Date(2026, 8, 14, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	f.provider.response = &appwechat.ArticleTotalDetailResponse{List: []appwechat.ArticleTotalDetailItem{{MsgID: "msg_1", DetailList: []appwechat.ArticleTotalDetailMetric{{StatDate: "2026-08-14", ReadUser: 44}}}}}
	enqueuer := &blockingWechatRecoveryEnqueuer{entered: make(chan struct{}, 1), release: make(chan struct{})}
	f.svc.enqueuer = enqueuer

	recoverResult := make(chan error, 1)
	go func() { recoverResult <- f.svc.RecoverDue(context.Background(), 10) }()
	<-enqueuer.entered
	if err := f.svc.CaptureMetrics(context.Background(), f.tracking.ID); err != nil {
		t.Fatal(err)
	}
	close(enqueuer.release)
	if err := <-recoverResult; err != nil {
		t.Fatal(err)
	}

	tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	snapshots, _ := f.repo.WechatMetricSnapshots().FindByTaskID(context.Background(), f.taskID)
	if tracking.Status != model.WechatTrackingStatusTracking || tracking.RunCount != 1 || tracking.LastFetchAt == nil || tracking.RecoveryClaimToken != "" || len(snapshots) != 1 || snapshots[0].ReadUsers != 44 {
		t.Fatalf("concurrent capture was regressed: tracking=%#v snapshots=%#v", tracking, snapshots)
	}
}

func TestWechatTrackingRecoverDueConcurrentCallsEnqueueOnce(t *testing.T) {
	published := time.Date(2026, 8, 15, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	enqueuer := &blockingWechatRecoveryEnqueuer{}
	f.svc.enqueuer = enqueuer
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- f.svc.RecoverDue(context.Background(), 10)
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if enqueuer.callCount() != 1 {
		t.Fatalf("enqueue calls = %d, want 1", enqueuer.callCount())
	}
}

func TestWechatTrackingRecoverDueReleasesClaimAfterEnqueueFailure(t *testing.T) {
	published := time.Date(2026, 8, 16, 8, 0, 0, 0, wechatAnalyticsLocation)
	f := newWechatTrackingFixture(t, model.WechatPublicationSourceAnbanAPI, "msg", "msg_1", "https://mp.weixin.qq.com/s/api", published)
	enqueuer := &blockingWechatRecoveryEnqueuer{err: errors.New("redis unavailable")}
	f.svc.enqueuer = enqueuer
	if err := f.svc.RecoverDue(context.Background(), 10); err == nil {
		t.Fatal("expected enqueue failure")
	}
	tracking, _ := f.repo.WechatTrackings().FindByID(context.Background(), f.tracking.ID)
	if tracking.RecoveryClaimToken != "" || tracking.NextFetchAt == nil || tracking.NextFetchAt.After(f.now) {
		t.Fatalf("failed enqueue claim was not safely released: %#v", tracking)
	}
}
