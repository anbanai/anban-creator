package service

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeWechatPublicationAPI struct {
	mu sync.Mutex

	addCalls, draftListCalls, submitCalls, getCalls, publishedListCalls int
	addResponse                                                         *appwechat.DraftAddResponse
	addError                                                            error
	draftListResponse                                                   *appwechat.DraftBatchGetResponse
	draftListError                                                      error
	submitResponse                                                      *appwechat.FreePublishSubmitResponse
	submitError                                                         error
	getResponses                                                        []*appwechat.FreePublishGetResponse
	getError                                                            error
	publishedListResponse                                               *appwechat.FreePublishBatchGetResponse
	publishedListError                                                  error
	onSubmit                                                            func()
	onPublishedList                                                     func(int)
}

func (f *fakeWechatPublicationAPI) AddDraft(context.Context, appwechat.DraftAddRequest) (*appwechat.DraftAddResponse, error) {
	f.mu.Lock()
	f.addCalls++
	f.mu.Unlock()
	return f.addResponse, f.addError
}
func (f *fakeWechatPublicationAPI) BatchGetDrafts(context.Context, appwechat.DraftBatchGetRequest) (*appwechat.DraftBatchGetResponse, error) {
	f.mu.Lock()
	f.draftListCalls++
	f.mu.Unlock()
	return f.draftListResponse, f.draftListError
}
func (f *fakeWechatPublicationAPI) SubmitFreePublish(context.Context, appwechat.FreePublishSubmitRequest) (*appwechat.FreePublishSubmitResponse, error) {
	f.mu.Lock()
	f.submitCalls++
	f.mu.Unlock()
	if f.onSubmit != nil {
		f.onSubmit()
	}
	return f.submitResponse, f.submitError
}
func (f *fakeWechatPublicationAPI) GetFreePublish(context.Context, appwechat.FreePublishGetRequest) (*appwechat.FreePublishGetResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getCalls++
	if len(f.getResponses) == 0 {
		return nil, f.getError
	}
	response := f.getResponses[0]
	f.getResponses = f.getResponses[1:]
	return response, f.getError
}
func (f *fakeWechatPublicationAPI) BatchGetFreePublishes(context.Context, appwechat.FreePublishBatchGetRequest) (*appwechat.FreePublishBatchGetResponse, error) {
	f.mu.Lock()
	f.publishedListCalls++
	call := f.publishedListCalls
	f.mu.Unlock()
	if f.onPublishedList != nil {
		f.onPublishedList(call)
	}
	return f.publishedListResponse, f.publishedListError
}

type publicationFixture struct {
	svc       *WechatPublicationService
	repo      repository.Repository
	api       *fakeWechatPublicationAPI
	now       time.Time
	userID    string
	projectID string
	taskID    string
}

func newPublicationFixture(t *testing.T, mode string) *publicationFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	now := time.Date(2026, 8, 31, 4, 0, 0, 0, time.UTC)
	f := &publicationFixture{
		repo: repo, api: &fakeWechatPublicationAPI{}, now: now,
		userID: uuid.NewString(), projectID: uuid.NewString(), taskID: uuid.NewString(),
	}
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: f.userID, Email: f.userID + "@publication.test", Password: "x", InviteCode: f.userID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: f.projectID, UserID: f.userID, Platform: model.PlatformArticle, Name: "WeChat", Status: model.ProjectStatusActive, Config: model.ProjectConfig{WechatAppID: "app", WechatSecret: "secret", WechatPublishMode: mode}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: f.taskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	f.svc = NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return f.api, nil }, &logger)
	f.svc.now = func() time.Time { return f.now }
	return f
}

func (f *publicationFixture) draftInput() appwechat.DraftAddRequest {
	return appwechat.DraftAddRequest{Articles: []appwechat.DraftArticle{{
		Title: " Durable lifecycle ", Author: "Author", Digest: "Digest", ThumbMediaID: "thumb-1",
		Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`,
	}}}
}

func (f *publicationFixture) seedDrafted(t *testing.T) *model.WechatPublication {
	t.Helper()
	created := f.now.Add(-time.Hour)
	p := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: f.taskID, UserID: f.userID, ProjectID: f.projectID,
		DraftMediaID: "draft-1", DraftTitle: "Durable lifecycle", DraftDigest: "Digest", DraftThumbMediaID: "thumb-1",
		DraftContentFingerprint: WechatContentFingerprint(`<section class="body"><p>Hello <strong>world</strong></p></section>`),
		Source:                  model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted, DraftCreatedAt: &created,
	}
	if err := f.repo.WechatPublications().Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCreateDraftRecoversResponseLossBeforeRetrying(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{ItemCount: 1, Items: []appwechat.DraftBatchItem{{
		MediaID: "recovered-media", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{{
			Title: "Durable lifecycle", Author: "Author", Digest: "Digest", ThumbMediaID: "thumb-1",
			Content: `<section class="body"> <p>Hello <strong>world</strong></p> </section>`,
		}}},
	}}}

	first := f.draftInput()
	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, first)
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if publication.DraftMediaID != "recovered-media" || publication.Status != model.WechatPublicationStatusDrafted {
		t.Fatalf("recovered publication = %#v", publication)
	}
	if f.api.draftListCalls != 1 || f.api.addCalls != 0 {
		t.Fatalf("provider calls: list=%d add=%d, want 1/0", f.api.draftListCalls, f.api.addCalls)
	}

	again, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, first)
	if err != nil || again.ID != publication.ID || f.api.draftListCalls != 1 || f.api.addCalls != 0 {
		t.Fatalf("idempotent replay = %#v err=%v calls=%d/%d", again, err, f.api.draftListCalls, f.api.addCalls)
	}
}

func TestWechatContentFingerprintPreservesMeaningfulInlineSpaces(t *testing.T) {
	formatted := `<section>
  <p>Hello <strong>world</strong></p>
</section>`
	compact := `<section><p>Hello <strong>world</strong></p></section>`
	if WechatContentFingerprint(formatted) != WechatContentFingerprint(compact) {
		t.Fatal("formatting-only whitespace changed the fingerprint")
	}
	withoutWordSpace := `<section><p>Hello<strong>world</strong></p></section>`
	if WechatContentFingerprint(compact) == WechatContentFingerprint(withoutWordSpace) {
		t.Fatal("meaningful inline whitespace was discarded")
	}
}

func TestCreateDraftPersistsIntentBeforeExternalCallAndDoesNotBlindRetry(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addError = errors.New("connection reset after response")

	_, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.draftInput())
	if err == nil {
		t.Fatal("CreateDraft succeeded after ambiguous provider response")
	}
	stored, findErr := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if findErr != nil || stored.Status != model.WechatPublicationStatusDrafting || stored.DraftContentFingerprint == "" {
		t.Fatalf("durable intent = %#v err=%v", stored, findErr)
	}
	if f.api.draftListCalls != 1 || f.api.addCalls != 1 {
		t.Fatalf("first calls = %d/%d", f.api.draftListCalls, f.api.addCalls)
	}

	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	_, err = f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationPending) || f.api.draftListCalls != 2 || f.api.addCalls != 1 {
		t.Fatalf("retry err=%v calls=%d/%d; want pending and no second add", err, f.api.draftListCalls, f.api.addCalls)
	}
}

func TestPublishUsesSingleCASWinnerAndKeepsStringIdentifiers(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "90071992547409931234", MsgDataID: "80071992547409931234"}
	f.api.onSubmit = func() { once.Do(func() { close(entered) }); <-release }

	firstDone := make(chan error, 1)
	go func() { _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); firstDone <- err }()
	<-entered
	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationConflict) {
		t.Fatalf("second Publish err=%v, want conflict", err)
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Publish: %v", err)
	}
	stored, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if f.api.submitCalls != 1 || stored.PublishID != "90071992547409931234" || stored.MsgDataID != "80071992547409931234" || stored.Status != model.WechatPublicationStatusPublishing {
		t.Fatalf("stored=%#v submitCalls=%d", stored, f.api.submitCalls)
	}
	if stored.NextCheckAt == nil || !stored.NextCheckAt.Equal(f.now.Add(5*time.Second)) {
		t.Fatalf("first next_check_at = %v", stored.NextCheckAt)
	}
}

func TestPublishResponseLossSchedulesReconciliationWithoutResubmitting(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if !errors.Is(err, ErrWechatPublicationPending) {
		t.Fatalf("Publish err=%v, want pending", err)
	}
	if got.Status != model.WechatPublicationStatusPublishSubmitting || got.NextCheckAt == nil || !got.NextCheckAt.Equal(f.now.Add(10*time.Minute)) {
		t.Fatalf("ambiguous publish = %#v", got)
	}
	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationConflict) {
		t.Fatalf("second Publish err=%v, want conflict", err)
	}
	if f.api.submitCalls != 1 {
		t.Fatalf("submit calls=%d, want 1", f.api.submitCalls)
	}
}

func TestPollPublishMapsStatusesAndDurableSchedule(t *testing.T) {
	t.Run("publishing schedule", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		p := f.seedDrafted(t)
		p.Status, p.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
		if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		f.api.getResponses = []*appwechat.FreePublishGetResponse{{PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusPublishing}}
		if _, err := f.svc.Poll(context.Background(), p.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
		if got.CheckAttempts != 1 || got.NextCheckAt == nil || !got.NextCheckAt.Equal(f.now.Add(15*time.Second)) {
			t.Fatalf("poll schedule = %#v", got)
		}
	})

	t.Run("empty response remains durably scheduled", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		p := f.seedDrafted(t)
		p.Status, p.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
		if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Poll(context.Background(), p.ID); err == nil {
			t.Fatal("Poll unexpectedly accepted an empty response")
		}
		got, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
		if got.CheckAttempts != 1 || got.LastCheckedAt == nil || got.NextCheckAt == nil || !got.NextCheckAt.Equal(f.now.Add(15*time.Second)) || got.LastError == "" {
			t.Fatalf("empty-response schedule = %#v", got)
		}
	})

	for _, tc := range []struct {
		name string
		code int
		want string
	}{
		{"original failed", 2, "publish_failed"}, {"failed", 3, "publish_failed"}, {"audit rejected", 4, "publish_failed"},
		{"user deleted", 5, "publish_failed"}, {"system banned", 6, "publish_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
			p := f.seedDrafted(t)
			p.Status, p.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
			if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
				t.Fatal(err)
			}
			f.api.getResponses = []*appwechat.FreePublishGetResponse{{PublishID: "publish-1", PublishStatus: tc.code}}
			got, err := f.svc.Poll(context.Background(), p.ID)
			if err != nil || got.Status != tc.want || got.WechatStatusCode != tc.code || got.NextCheckAt != nil {
				t.Fatalf("Poll = %#v, %v", got, err)
			}
		})
	}
}

func TestPollSuccessBindsPublicationAndCreatesTracking(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	p.Status, p.PublishID, p.MsgDataID = model.WechatPublicationStatusPublishing, "publish-1", "msg-data-1"
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.getResponses = []*appwechat.FreePublishGetResponse{{
		PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusSucceeded, ArticleID: "article-1",
		ArticleDetail: appwechat.FreePublishArticleDetail{Count: 1, Items: []appwechat.FreePublishArticleItem{{Index: 1, ArticleURL: "https://mp.weixin.qq.com/s/article-1"}}},
	}}

	got, err := f.svc.Poll(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.WechatPublicationStatusPublished || got.ArticleID != "article-1" || got.MsgID != "msg-data-1_1" || got.Source != model.WechatPublicationSourceAnbanAPI || got.ArticleIndex != 1 {
		t.Fatalf("published = %#v", got)
	}
	tracking, err := f.repo.WechatTrackings().FindByTaskID(context.Background(), f.taskID)
	if err != nil || tracking.ArticleID != "article-1" || tracking.MsgID != "msg-data-1_1" || tracking.ArticleURL != got.ArticleURL {
		t.Fatalf("tracking = %#v err=%v", tracking, err)
	}
}

func TestPublishUnsupportedAndMissingDraftNeverBlindSubmit(t *testing.T) {
	t.Run("48001", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		f.seedDrafted(t)
		f.api.submitError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "unauthorized"}
		got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
		if err != nil || got.Status != model.WechatPublicationStatusUnsupported || got.WechatStatusCode != 48001 {
			t.Fatalf("Publish = %#v, %v", got, err)
		}
	})
	t.Run("missing draft", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		created := f.now.Add(-time.Hour)
		p := &model.WechatPublication{ID: uuid.NewString(), TaskID: f.taskID, UserID: f.userID, ProjectID: f.projectID, DraftTitle: "Durable lifecycle", DraftContentFingerprint: WechatContentFingerprint("<p>body</p>"), Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted, DraftCreatedAt: &created}
		if err := f.repo.WechatPublications().Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
		got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
		if !errors.Is(err, ErrWechatPublicationPending) || got.Status != model.WechatPublicationStatusDrafted || f.api.submitCalls != 0 || f.api.draftListCalls != 1 {
			t.Fatalf("Publish = %#v, %v calls=%d/%d", got, err, f.api.submitCalls, f.api.draftListCalls)
		}
	})
}

func TestManualReconcileBatchesProjectAndUsesOnlyHighConfidenceMatches(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	first := f.seedDrafted(t)
	secondTaskID := uuid.NewString()
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{ID: secondTaskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	created := f.now.Add(-time.Hour)
	second := &model.WechatPublication{ID: uuid.NewString(), TaskID: secondTaskID, UserID: f.userID, ProjectID: f.projectID, DraftMediaID: "draft-2", DraftTitle: "Weak title", DraftDigest: "other", DraftThumbMediaID: "other-thumb", DraftContentFingerprint: WechatContentFingerprint("<p>different body</p>"), Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted, DraftCreatedAt: &created}
	if err := f.repo.WechatPublications().Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{ItemCount: 2, Items: []appwechat.FreePublishBatchItem{
		{ArticleID: "exact", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: first.DraftTitle, Digest: "changed", ThumbMediaID: "changed", Content: `<section class="body"> <p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/exact"}}}},
		{ArticleID: "weak", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: second.DraftTitle, Digest: "not exact", ThumbMediaID: "not exact", Content: "<p>not exact</p>", URL: "https://mp.weixin.qq.com/s/weak"}}}},
	}}

	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	if f.api.publishedListCalls != 1 {
		t.Fatalf("batch calls=%d, want 1", f.api.publishedListCalls)
	}
	bound, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if bound.Status != model.WechatPublicationStatusPublished || bound.ArticleID != "exact" || bound.Source != model.WechatPublicationSourceWechatConsole {
		t.Fatalf("exact = %#v", bound)
	}
	weak, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), secondTaskID)
	if weak.Status != model.WechatPublicationStatusNeedsSelection || len(weak.Candidates) == 0 {
		t.Fatalf("weak = %#v", weak)
	}
}

func TestManualReconcileRateLimitAndSchedule(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	p := f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if got.NextCheckAt == nil || !got.NextCheckAt.Equal(f.now.Add(10*time.Minute)) {
		t.Fatalf("next check = %v", got.NextCheckAt)
	}
	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationRateLimited) {
		t.Fatalf("second reconcile err=%v", err)
	}
	if f.api.publishedListCalls != 1 {
		t.Fatalf("rate-limited provider calls=%d", f.api.publishedListCalls)
	}
	_ = p
}

func TestManualReconcileRateLimitIsAtomicAcrossConcurrentRequests(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	entered, release := make(chan struct{}), make(chan struct{})
	f.api.onPublishedList = func(call int) {
		if call == 1 {
			close(entered)
			<-release
		}
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- f.svc.Reconcile(context.Background(), f.userID, f.taskID) }()
	<-entered
	secondErr := f.svc.Reconcile(context.Background(), f.userID, f.taskID)
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	if !errors.Is(secondErr, ErrWechatPublicationRateLimited) {
		t.Fatalf("second Reconcile err=%v, want rate limited", secondErr)
	}
	if f.api.publishedListCalls != 1 {
		t.Fatalf("concurrent provider calls=%d, want 1", f.api.publishedListCalls)
	}
}

func TestManualReconcileRequiresOneUniqueArticleAcrossAllExactSignals(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	p := f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{Items: []appwechat.FreePublishBatchItem{
		{ArticleID: "body-match", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: "Different", Digest: "Different", ThumbMediaID: "different", Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/body-match"}}}},
		{ArticleID: "metadata-match", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: p.DraftTitle, Digest: p.DraftDigest, ThumbMediaID: p.DraftThumbMediaID, Content: `<p>different</p>`, URL: "https://mp.weixin.qq.com/s/metadata-match"}}}},
	}}

	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if got.Status != model.WechatPublicationStatusNeedsSelection || got.ArticleID != "" {
		t.Fatalf("conflicting exact signals auto-bound: %#v", got)
	}
}

func TestManualReconcileDoesNotBindArticleWithoutProviderURL(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	p := f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "missing-url", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
			Title: p.DraftTitle, Digest: p.DraftDigest, ThumbMediaID: p.DraftThumbMediaID, Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`,
		}}},
	}}}
	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if got.Status == model.WechatPublicationStatusPublished || got.ArticleID != "" {
		t.Fatalf("article without URL was bound: %#v", got)
	}
}

func TestSelectRefetchesProviderAndRejectsForeignOrBoundArticle(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	p := f.seedDrafted(t)
	p.Status = model.WechatPublicationStatusNeedsSelection
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{Items: []appwechat.FreePublishBatchItem{{ArticleID: "owned", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: "Selected", URL: "https://mp.weixin.qq.com/s/owned"}}}}}}

	if _, err := f.svc.Select(context.Background(), f.userID, f.taskID, "foreign"); !errors.Is(err, ErrWechatPublicationArticleNotFound) {
		t.Fatalf("foreign err=%v", err)
	}
	got, err := f.svc.Select(context.Background(), f.userID, f.taskID, "owned")
	if err != nil || got.ArticleID != "owned" || got.Status != model.WechatPublicationStatusPublished {
		t.Fatalf("Select = %#v, %v", got, err)
	}
	if f.api.publishedListCalls != 2 {
		t.Fatalf("selection must refetch each time, calls=%d", f.api.publishedListCalls)
	}
}

func TestWechatPublicationOwnershipAndStateConflicts(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.seedDrafted(t)
	if _, err := f.svc.Get(context.Background(), uuid.NewString(), f.taskID); !errors.Is(err, ErrWechatPublicationForbidden) {
		t.Fatalf("Get foreign err=%v", err)
	}
	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationModeConflict) {
		t.Fatalf("manual publish err=%v", err)
	}
}

func TestPublicationSchedulesUseTheContractedDurableCadence(t *testing.T) {
	now := time.Date(2026, 8, 31, 4, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		attempts  int
		after     time.Duration
		scheduled bool
	}{
		{0, 5 * time.Second, true}, {1, 15 * time.Second, true}, {2, 30 * time.Second, true},
		{3, time.Minute, true}, {4, 2 * time.Minute, true}, {5, 5 * time.Minute, true},
		{6, 10 * time.Minute, true}, {16, 10 * time.Minute, true}, {17, 0, false},
	} {
		got := nextWechatAPICheck(now, tc.attempts)
		if !tc.scheduled {
			if got != nil {
				t.Fatalf("attempt %d scheduled %v", tc.attempts, got)
			}
			continue
		}
		if got == nil || !got.Equal(now.Add(tc.after)) {
			t.Fatalf("attempt %d = %v, want +%s", tc.attempts, got, tc.after)
		}
	}
	for _, tc := range []struct {
		age, after time.Duration
		scheduled  bool
	}{
		{time.Hour, 10 * time.Minute, true}, {2 * time.Hour, time.Hour, true},
		{23 * time.Hour, time.Hour, true}, {24 * time.Hour, 6 * time.Hour, true},
		{71 * time.Hour, 6 * time.Hour, true}, {72 * time.Hour, 0, false},
	} {
		got := nextWechatManualCheck(now, now.Add(-tc.age))
		if !tc.scheduled {
			if got != nil {
				t.Fatalf("age %s scheduled %v", tc.age, got)
			}
			continue
		}
		if got == nil || !got.Equal(now.Add(tc.after)) {
			t.Fatalf("age %s = %v, want +%s", tc.age, got, tc.after)
		}
	}
}
