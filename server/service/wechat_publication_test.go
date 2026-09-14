package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type queuedWechatPublicationTask struct {
	taskType string
	payload  map[string]string
	delay    time.Duration
}

type recordingWechatPublicationEnqueuer struct {
	immediate []queuedWechatPublicationTask
	delayed   []queuedWechatPublicationTask
	err       error
}

func (e *recordingWechatPublicationEnqueuer) Enqueue(taskType string, payload []byte) error {
	if e.err != nil {
		return e.err
	}
	var decoded map[string]string
	_ = json.Unmarshal(payload, &decoded)
	e.immediate = append(e.immediate, queuedWechatPublicationTask{taskType: taskType, payload: decoded})
	return nil
}

func (e *recordingWechatPublicationEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	if e.err != nil {
		return e.err
	}
	var decoded map[string]string
	_ = json.Unmarshal(payload, &decoded)
	e.delayed = append(e.delayed, queuedWechatPublicationTask{taskType: taskType, payload: decoded, delay: delay})
	return nil
}

type fakeWechatPublicationAPI struct {
	mu sync.Mutex

	addCalls, draftListCalls, submitCalls, getCalls, publishedListCalls int
	addResponse                                                         *appwechat.DraftAddResponse
	addError                                                            error
	draftListResponse                                                   *appwechat.DraftBatchGetResponse
	draftListResponses                                                  map[int64]*appwechat.DraftBatchGetResponse
	draftListRequests                                                   []appwechat.DraftBatchGetRequest
	draftListError                                                      error
	submitResponse                                                      *appwechat.FreePublishSubmitResponse
	submitError                                                         error
	getResponses                                                        []*appwechat.FreePublishGetResponse
	getError                                                            error
	publishedListResponse                                               *appwechat.FreePublishBatchGetResponse
	publishedListResponses                                              map[int64]*appwechat.FreePublishBatchGetResponse
	publishedListRequests                                               []appwechat.FreePublishBatchGetRequest
	publishedListError                                                  error
	onAdd                                                               func()
	onSubmit                                                            func()
	onGet                                                               func()
	onPublishedList                                                     func(int)
}

func (f *fakeWechatPublicationAPI) AddDraft(context.Context, appwechat.DraftAddRequest) (*appwechat.DraftAddResponse, error) {
	f.mu.Lock()
	f.addCalls++
	f.mu.Unlock()
	if f.onAdd != nil {
		f.onAdd()
	}
	return f.addResponse, f.addError
}
func (f *fakeWechatPublicationAPI) BatchGetDrafts(_ context.Context, request appwechat.DraftBatchGetRequest) (*appwechat.DraftBatchGetResponse, error) {
	f.mu.Lock()
	f.draftListCalls++
	f.draftListRequests = append(f.draftListRequests, request)
	response := f.draftListResponse
	if f.draftListResponses != nil {
		response = f.draftListResponses[request.Offset]
	}
	f.mu.Unlock()
	return response, f.draftListError
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
	f.getCalls++
	getError := f.getError
	var response *appwechat.FreePublishGetResponse
	if len(f.getResponses) == 0 {
		f.mu.Unlock()
		if f.onGet != nil {
			f.onGet()
		}
		return nil, getError
	}
	response = f.getResponses[0]
	f.getResponses = f.getResponses[1:]
	f.mu.Unlock()
	if f.onGet != nil {
		f.onGet()
	}
	return response, getError
}
func (f *fakeWechatPublicationAPI) BatchGetFreePublishes(_ context.Context, request appwechat.FreePublishBatchGetRequest) (*appwechat.FreePublishBatchGetResponse, error) {
	f.mu.Lock()
	f.publishedListCalls++
	f.publishedListRequests = append(f.publishedListRequests, request)
	call := f.publishedListCalls
	response := f.publishedListResponse
	if f.publishedListResponses != nil {
		response = f.publishedListResponses[request.Offset]
	}
	f.mu.Unlock()
	if f.onPublishedList != nil {
		f.onPublishedList(call)
	}
	return response, f.publishedListError
}

type publicationFixture struct {
	svc         *WechatPublicationService
	repo        repository.Repository
	db          *gorm.DB
	api         *fakeWechatPublicationAPI
	now         time.Time
	userID      string
	projectID   string
	taskID      string
	executionID string
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
		repo: repo, db: db, api: &fakeWechatPublicationAPI{}, now: now,
		userID: uuid.NewString(), projectID: uuid.NewString(), taskID: uuid.NewString(), executionID: uuid.NewString(),
	}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{MediaID: "draft-1", UpdateTime: now.Unix()}}}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: f.userID, Email: f.userID + "@publication.test", Password: "x", InviteCode: f.userID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: f.projectID, UserID: f.userID, Platform: model.PlatformArticle, Name: "WeChat", Status: model.ProjectStatusActive, Config: model.ProjectConfig{WechatAppID: "app", WechatSecret: "secret", WechatPublishMode: mode}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: f.taskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &f.executionID}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	f.svc = NewWechatPublicationService(repo, func(*model.Project) (WechatPublicationAPI, error) { return f.api, nil }, &logger)
	f.svc.SetEnqueuer(&recordingWechatPublicationEnqueuer{})
	f.svc.now = func() time.Time { return f.now }
	return f
}

type publicationRepositoryOverride struct {
	repository.Repository
	publications repository.WechatPublicationRepository
}

func (r publicationRepositoryOverride) WechatPublications() repository.WechatPublicationRepository {
	return r.publications
}

type failingWechatPublicationRepository struct {
	repository.WechatPublicationRepository
	updateErr        error
	updateClaimedErr error
	findArticleErr   error
	claimPublishErr  error
}

type pausingFindPendingWechatPublicationRepository struct {
	repository.WechatPublicationRepository
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

type publishBetweenDraftRecoveryAndReloadRepository struct {
	repository.WechatPublicationRepository
	db    *gorm.DB
	mu    sync.Mutex
	calls int
	now   time.Time
}

func (r *publishBetweenDraftRecoveryAndReloadRepository) FindByTaskID(ctx context.Context, taskID string) (*model.WechatPublication, error) {
	publication, err := r.WechatPublicationRepository.FindByTaskID(ctx, taskID)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 2 && publication.Status == model.WechatPublicationStatusDrafted {
		attemptedAt := r.now
		if err := r.db.WithContext(ctx).Model(&model.WechatPublication{}).Where("id = ?", publication.ID).Updates(map[string]any{
			"status":              model.WechatPublicationStatusPublishing,
			"publish_id":          "concurrent-publish",
			"submit_attempted_at": &attemptedAt,
			"next_check_at":       &attemptedAt,
		}).Error; err != nil {
			return nil, err
		}
	}
	return publication, nil
}

func (r *pausingFindPendingWechatPublicationRepository) FindPendingByProject(ctx context.Context, projectID string) ([]*model.WechatPublication, error) {
	publications, err := r.WechatPublicationRepository.FindPendingByProject(ctx, projectID)
	r.once.Do(func() {
		close(r.entered)
		<-r.release
	})
	return publications, err
}

func (r failingWechatPublicationRepository) Update(ctx context.Context, publication *model.WechatPublication) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return r.WechatPublicationRepository.Update(ctx, publication)
}

func (r failingWechatPublicationRepository) UpdateReconciliation(ctx context.Context, publication *model.WechatPublication, expectedStatus string, expectedUpdatedAt time.Time) (bool, error) {
	if r.updateErr != nil {
		return false, r.updateErr
	}
	return r.WechatPublicationRepository.UpdateReconciliation(ctx, publication, expectedStatus, expectedUpdatedAt)
}

func (r failingWechatPublicationRepository) UpdateClaimed(ctx context.Context, publication *model.WechatPublication, token string) (bool, error) {
	if r.updateClaimedErr != nil {
		return false, r.updateClaimedErr
	}
	return r.WechatPublicationRepository.UpdateClaimed(ctx, publication, token)
}

func (r failingWechatPublicationRepository) FindByArticleID(ctx context.Context, projectID, articleID string) (*model.WechatPublication, error) {
	if r.findArticleErr != nil {
		return nil, r.findArticleErr
	}
	return r.WechatPublicationRepository.FindByArticleID(ctx, projectID, articleID)
}

func (r failingWechatPublicationRepository) ClaimPublish(ctx context.Context, id, token string, now, staleBefore time.Time, nextCheckAt *time.Time) (bool, error) {
	if r.claimPublishErr != nil {
		return false, r.claimPublishErr
	}
	return r.WechatPublicationRepository.ClaimPublish(ctx, id, token, now, staleBefore, nextCheckAt)
}

func (f *publicationFixture) overridePublications(publications repository.WechatPublicationRepository) {
	f.svc.repo = publicationRepositoryOverride{Repository: f.repo, publications: publications}
}

func (f *publicationFixture) draftInput() appwechat.DraftAddRequest {
	return appwechat.DraftAddRequest{Articles: []appwechat.DraftArticle{{
		Title: " Durable lifecycle ", Author: "Author", Digest: "Digest", ThumbMediaID: "thumb-1",
		Content:      `<section><p>Hello <strong>world</strong></p></section>`,
		ShowCoverPic: 1, NeedOpenComment: 1, OnlyFansCanComment: 1,
	}}}
}

func TestCreateDraftRejectsMultipleArticlesBeforeProviderCalls(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	request := f.draftInput()
	request.Articles = append(request.Articles, request.Articles[0])

	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request); err == nil {
		t.Fatal("CreateDraft accepted multiple articles")
	}
	if f.api.draftListCalls != 0 || f.api.addCalls != 0 {
		t.Fatalf("provider calls: list=%d add=%d, want 0/0", f.api.draftListCalls, f.api.addCalls)
	}
}

func TestCreateDraftBlocksOutboundMarketingBeforeProviderCalls(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*appwechat.DraftArticle)
	}{
		{name: "content source URL", mutate: func(article *appwechat.DraftArticle) { article.ContentSourceURL = "//marketing.example/join" }},
		{name: "provider article URL", mutate: func(article *appwechat.DraftArticle) { article.URL = "http:marketing.example/join" }},
		{name: "backslash metadata URL", mutate: func(article *appwechat.DraftArticle) { article.ContentSourceURL = `\\marketing.example/join` }},
		{name: "protocol-relative anchor", mutate: func(article *appwechat.DraftArticle) {
			article.Content = `<p><a href="//marketing.example/join">正文</a></p>`
		}},
		{name: "opaque HTTP anchor", mutate: func(article *appwechat.DraftArticle) {
			article.Content = `<p><a href="http:marketing.example/join">正文</a></p>`
		}},
		{name: "mixed-slash HTTPS anchor", mutate: func(article *appwechat.DraftArticle) {
			article.Content = `<p><a href="https:/\marketing.example/join">正文</a></p>`
		}},
		{name: "backslash-authority anchor", mutate: func(article *appwechat.DraftArticle) {
			article.Content = `<p><a href="\\marketing.example/join">正文</a></p>`
		}},
		{name: "split private contact", mutate: func(article *appwechat.DraftArticle) {
			article.Content = `<p>添加我<strong>微信</strong>即可领取资料</p>`
		}},
		{name: "split phone", mutate: func(article *appwechat.DraftArticle) {
			article.Content = `<p>138<strong>0013</strong>8000</p>`
		}},
		{name: "relative anchor inside private contact", mutate: func(article *appwechat.DraftArticle) {
			article.Content = `<p>添加我<a href="#details">微</a>信即可领取资料</p>`
		}},
		{name: "zero width private contact", mutate: func(article *appwechat.DraftArticle) {
			article.Content = "<p>添加我微\u200b信即可领取资料</p>"
		}},
		{name: "zero width phone", mutate: func(article *appwechat.DraftArticle) {
			article.Content = "<p>138\u200b00138000</p>"
		}},
		{name: "variation selector private contact", mutate: func(article *appwechat.DraftArticle) {
			article.Content = "<p>添加我微\ufe0f信即可领取资料</p>"
		}},
		{name: "variation selector phone", mutate: func(article *appwechat.DraftArticle) {
			article.Content = "<p>138\ufe0f00138000</p>"
		}},
		{name: "combining grapheme joiner private contact", mutate: func(article *appwechat.DraftArticle) {
			article.Content = "<p>添加我微\u034f信即可领取资料</p>"
		}},
		{name: "combining grapheme joiner phone", mutate: func(article *appwechat.DraftArticle) {
			article.Content = "<p>138\u034f00138000</p>"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeManual)
			f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
			f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
			request := f.draftInput()
			request.Articles[0].ContentSourceURL = ""
			request.Articles[0].URL = ""
			tt.mutate(&request.Articles[0])

			if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request); !errors.Is(err, ErrWechatPublicationMarketingBlocked) {
				t.Fatalf("CreateDraft error = %v, want ErrWechatPublicationMarketingBlocked", err)
			}
			if f.api.draftListCalls != 0 || f.api.addCalls != 0 {
				t.Fatalf("blocked metadata URL reached WeChat: list=%d add=%d", f.api.draftListCalls, f.api.addCalls)
			}
		})
	}
}

func TestCreateDraftRejectsUnsafeHTMLBeforeProviderCalls(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{name: "script", content: `<script>alert(1)</script><p>正文</p>`},
		{name: "event handler", content: `<p>正文</p><img src="https://mmbiz.qpic.cn/a.png" onerror="alert(1)">`},
		{name: "javascript link", content: `<p><a href="javascript:alert(1)">正文</a></p>`},
		{name: "CSS escaped URL", content: `<p style="background-image:\75rl(https://marketing.example/tracker.png)">正文</p>`},
		{name: "CSS image-set URL", content: `<p style="background-image:image-set(&quot;https://marketing.example/tracker.png&quot; 1x)">正文</p>`},
		{name: "untrusted HTTPS image", content: `<p>正文</p><img src="https://marketing.example/tracker.png">`},
		{name: "protocol-relative image", content: `<p>正文</p><img src="//mmbiz.qpic.cn/a.png">`},
		{name: "insecure HTTP image", content: `<p>正文</p><img src="http://mmbiz.qpic.cn/a.png">`},
		{name: "mailto image", content: `<p>正文</p><img src="mailto:contact@example.com">`},
		{name: "telephone image", content: `<p>正文</p><img src="tel:13800138000">`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeManual)
			f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
			f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
			request := f.draftInput()
			request.Articles[0].Content = tt.content

			if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request); !errors.Is(err, ErrWechatPublicationInvalidPayload) {
				t.Fatalf("CreateDraft error = %v, want ErrWechatPublicationInvalidPayload", err)
			}
			if f.api.draftListCalls != 0 || f.api.addCalls != 0 {
				t.Fatalf("unsafe HTML reached WeChat: list=%d add=%d", f.api.draftListCalls, f.api.addCalls)
			}
		})
	}
}

func TestCreateDraftAcceptsBodyImageRegisteredByCurrentExecution(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	const imageURL = "https://wechat.example/current-image.png"
	if err := f.repo.TaskFiles().Create(context.Background(), &model.TaskFile{
		ID: uuid.NewString(), TaskID: f.taskID, ExecutionID: f.executionID, State: model.TaskFileStatePending,
		FilePath: "output/current-image.png", FileName: "current-image.png", MimeType: "image/png",
		WechatURL: imageURL,
	}); err != nil {
		t.Fatal(err)
	}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
	request := f.draftInput()
	request.Articles[0].Content = `<p>正文</p><img src="` + imageURL + `">`

	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request); err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if f.api.addCalls != 1 {
		t.Fatalf("provider add calls = %d, want 1", f.api.addCalls)
	}
}

func TestCreateDraftRejectsBodyImageRegisteredByEarlierExecution(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	const imageURL = "https://wechat.example/old-image.png"
	if err := f.repo.TaskFiles().Create(context.Background(), &model.TaskFile{
		ID: uuid.NewString(), TaskID: f.taskID, ExecutionID: uuid.NewString(), State: model.TaskFileStateRetained,
		FilePath: "output/old-image.png", FileName: "old-image.png", MimeType: "image/png",
		WechatURL: imageURL,
	}); err != nil {
		t.Fatal(err)
	}
	request := f.draftInput()
	request.Articles[0].Content = `<p>正文</p><img src="` + imageURL + `">`

	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request); !errors.Is(err, ErrWechatPublicationInvalidPayload) {
		t.Fatalf("CreateDraft error = %v, want ErrWechatPublicationInvalidPayload", err)
	}
	if f.api.draftListCalls != 0 || f.api.addCalls != 0 {
		t.Fatalf("earlier execution image reached WeChat: list=%d add=%d", f.api.draftListCalls, f.api.addCalls)
	}
}

func TestCreateDraftUsesEveryMaterialFieldForIdempotency(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*appwechat.DraftArticle)
	}{
		{name: "title", mutate: func(article *appwechat.DraftArticle) { article.Title = "Different title" }},
		{name: "author", mutate: func(article *appwechat.DraftArticle) { article.Author = "Different author" }},
		{name: "digest", mutate: func(article *appwechat.DraftArticle) { article.Digest = "Different digest" }},
		{name: "content", mutate: func(article *appwechat.DraftArticle) { article.Content = "<p>Different body</p>" }},
		{name: "thumb media ID", mutate: func(article *appwechat.DraftArticle) { article.ThumbMediaID = "thumb-2" }},
		{name: "show cover pic", mutate: func(article *appwechat.DraftArticle) { article.ShowCoverPic = 0 }},
		{name: "need open comment", mutate: func(article *appwechat.DraftArticle) { article.NeedOpenComment = 0 }},
		{name: "only fans can comment", mutate: func(article *appwechat.DraftArticle) { article.OnlyFansCanComment = 0 }},
	}

	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeManual)
			f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
			f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
			request := f.draftInput()
			first, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request)
			if err != nil {
				t.Fatal(err)
			}

			changed := f.draftInput()
			tt.mutate(&changed.Articles[0])
			replayed, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, changed)
			if !errors.Is(err, ErrWechatPublicationConflict) {
				t.Fatalf("changed request returned err=%v, want conflict", err)
			}
			if replayed == nil || replayed.ID != first.ID {
				t.Fatalf("conflict publication = %#v, want ID %q", replayed, first.ID)
			}
			if f.api.draftListCalls != 1 || f.api.addCalls != 1 {
				t.Fatalf("provider calls: list=%d add=%d, want 1/1", f.api.draftListCalls, f.api.addCalls)
			}
		})
	}
}

func TestCreateDraftExactReplayIsIdempotent(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
	request := f.draftInput()

	first, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request)
	if err != nil || replayed.ID != first.ID || replayed.DraftMediaID != first.DraftMediaID {
		t.Fatalf("exact replay = %#v err=%v, want publication %#v", replayed, err, first)
	}
	if f.api.draftListCalls != 1 || f.api.addCalls != 1 {
		t.Fatalf("provider calls: list=%d add=%d, want 1/1", f.api.draftListCalls, f.api.addCalls)
	}
}

func TestCreateDraftRejectsPublicationFromEarlierExecution(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput()); err != nil {
		t.Fatal(err)
	}

	newExecutionID := uuid.NewString()
	if err := f.db.Model(&model.Task{}).Where("id = ?", f.taskID).Update("current_execution_id", newExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, newExecutionID, f.draftInput()); !errors.Is(err, ErrWechatPublicationExecutionMismatch) {
		t.Fatalf("CreateDraft error = %v, want execution mismatch", err)
	}
	if f.api.draftListCalls != 1 || f.api.addCalls != 1 {
		t.Fatalf("stale publication triggered provider calls: list=%d add=%d", f.api.draftListCalls, f.api.addCalls)
	}
}

func TestCreateDraftReusesExactPublicationFromTerminalEarlierExecution(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	if err := f.repo.TaskExecutions().Create(context.Background(), &model.TaskExecution{
		ID: f.executionID, TaskID: f.taskID, Attempt: 1, Target: "test", Status: model.TaskExecutionRunning,
		ExecutionProfile: "effective", Provider: "deepseek", ProfileEnvs: map[string]string{}, ProfileFingerprint: "fingerprint",
	}); err != nil {
		t.Fatal(err)
	}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
	first, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.db.Model(&model.TaskExecution{}).Where("id = ?", f.executionID).Update("status", model.TaskExecutionFailed).Error; err != nil {
		t.Fatal(err)
	}

	newExecutionID := uuid.NewString()
	if err := f.db.Model(&model.Task{}).Where("id = ?", f.taskID).Update("current_execution_id", newExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	reused, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, newExecutionID, f.draftInput())
	if err != nil {
		t.Fatalf("CreateDraft exact resumed replay: %v", err)
	}
	if reused.ID != first.ID || reused.ExecutionID != newExecutionID || reused.DraftMediaID != first.DraftMediaID {
		t.Fatalf("reused publication = %#v, want rebound %#v", reused, first)
	}
	if f.api.draftListCalls != 1 || f.api.addCalls != 1 {
		t.Fatalf("resumed replay triggered provider calls: list=%d add=%d", f.api.draftListCalls, f.api.addCalls)
	}
}

func TestCreateDraftRetriesExactUnsupportedPublicationAfterExecutionHandoff(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	ctx := context.Background()
	if err := f.repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: f.executionID, TaskID: f.taskID, Attempt: 1, Target: "test", Status: model.TaskExecutionFailed,
		ExecutionProfile: "effective", Provider: "deepseek", ProfileEnvs: map[string]string{}, ProfileFingerprint: "fingerprint",
	}); err != nil {
		t.Fatal(err)
	}
	request := f.draftInput()
	article, err := firstDraftArticle(request)
	if err != nil {
		t.Fatal(err)
	}
	publication := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: f.taskID, ExecutionID: f.executionID, UserID: f.userID, ProjectID: f.projectID,
		DraftTitle: article.Title, DraftAuthor: article.Author, DraftDigest: article.Digest,
		DraftThumbMediaID: article.ThumbMediaID, DraftContentFingerprint: WechatContentFingerprint(article.Content),
		DraftRequestFingerprint: wechatDraftRequestFingerprint(article), Source: model.WechatPublicationSourceAnbanAPI,
		Status: model.WechatPublicationStatusUnsupported, WechatStatusCode: 48001,
	}
	if err := f.repo.WechatPublications().Create(ctx, publication); err != nil {
		t.Fatal(err)
	}
	newExecutionID := uuid.NewString()
	if err := f.db.Model(&model.Task{}).Where("id = ?", f.taskID).Update("current_execution_id", newExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-after-handoff"}

	retried, err := f.svc.CreateDraft(ctx, f.userID, f.taskID, f.projectID, newExecutionID, request)
	if err != nil {
		t.Fatalf("CreateDraft after handoff: %v", err)
	}
	if retried.ExecutionID != newExecutionID || retried.Status != model.WechatPublicationStatusDrafted || retried.DraftMediaID != "draft-after-handoff" {
		t.Fatalf("retried publication = %#v", retried)
	}
	if f.api.addCalls != 1 {
		t.Fatalf("AddDraft calls = %d, want 1", f.api.addCalls)
	}
}

func TestCreateDraftStartsManualReconciliationSchedule(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-1"}
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)

	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatal(err)
	}
	if publication.NextCheckAt == nil || !publication.NextCheckAt.Equal(f.now.Add(10*time.Minute)) {
		t.Fatalf("next check = %v, want %v", publication.NextCheckAt, f.now.Add(10*time.Minute))
	}
	if len(enqueuer.delayed) != 1 || enqueuer.delayed[0].taskType != WechatPublicationReconcileTaskType ||
		enqueuer.delayed[0].payload["project_id"] != f.projectID || enqueuer.delayed[0].delay != 10*time.Minute {
		t.Fatalf("delayed tasks = %#v", enqueuer.delayed)
	}
}

func TestCreateDraftRecoveryRequiresAllMaterialMetadata(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*appwechat.DraftArticle)
	}{
		{name: "title", mutate: func(article *appwechat.DraftArticle) { article.Title = "Different title" }},
		{name: "author", mutate: func(article *appwechat.DraftArticle) { article.Author = "Different author" }},
		{name: "digest", mutate: func(article *appwechat.DraftArticle) { article.Digest = "Different digest" }},
		{name: "thumb media ID", mutate: func(article *appwechat.DraftArticle) { article.ThumbMediaID = "thumb-2" }},
		{name: "show cover pic", mutate: func(article *appwechat.DraftArticle) { article.ShowCoverPic = 0 }},
		{name: "need open comment", mutate: func(article *appwechat.DraftArticle) { article.NeedOpenComment = 0 }},
		{name: "only fans can comment", mutate: func(article *appwechat.DraftArticle) { article.OnlyFansCanComment = 0 }},
	}

	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeManual)
			candidate := f.draftInput().Articles[0]
			tt.mutate(&candidate)
			f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{
				MediaID: "wrong-metadata", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{candidate}},
			}}}
			f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "new-draft"}

			publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
			if err != nil {
				t.Fatal(err)
			}
			if publication.DraftMediaID != "new-draft" {
				t.Fatalf("draft media ID = %q, want new-draft", publication.DraftMediaID)
			}
			if f.api.draftListCalls != 1 || f.api.addCalls != 1 {
				t.Fatalf("provider calls: list=%d add=%d, want 1/1", f.api.draftListCalls, f.api.addCalls)
			}
		})
	}
}

func (f *publicationFixture) seedDrafted(t *testing.T) *model.WechatPublication {
	t.Helper()
	created := f.now.Add(-time.Hour)
	p := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: f.taskID, ExecutionID: f.executionID, UserID: f.userID, ProjectID: f.projectID,
		DraftMediaID: "draft-1", DraftTitle: "Durable lifecycle", DraftDigest: "Digest", DraftThumbMediaID: "thumb-1",
		DraftContentFingerprint: WechatContentFingerprint(`<section class="body"><p>Hello <strong>world</strong></p></section>`),
		Source:                  model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted, DraftCreatedAt: &created,
	}
	if err := f.repo.WechatPublications().Create(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p
}

func setPublicationCandidates(t *testing.T, publication *model.WechatPublication, articleIDs ...string) {
	t.Helper()
	candidates := make([]WechatPublicationCandidate, 0, len(articleIDs))
	for _, articleID := range articleIDs {
		candidates = append(candidates, WechatPublicationCandidate{ArticleID: articleID})
	}
	encoded, err := json.Marshal(candidates)
	if err != nil {
		t.Fatal(err)
	}
	publication.Candidates = datatypes.JSON(encoded)
}

func TestCreateDraftRecoversResponseLossBeforeRetrying(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	recoveredArticle := f.draftInput().Articles[0]
	recoveredArticle.Content = `<section> <p>Hello <strong>world</strong></p> </section>`
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{ItemCount: 1, Items: []appwechat.DraftBatchItem{{
		MediaID: "recovered-media", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{recoveredArticle}},
	}}}

	first := f.draftInput()
	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, first)
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if publication.DraftMediaID != "recovered-media" || publication.Status != model.WechatPublicationStatusDrafted {
		t.Fatalf("recovered publication = %#v", publication)
	}
	if f.api.draftListCalls != 1 || f.api.addCalls != 0 {
		t.Fatalf("provider calls: list=%d add=%d, want 1/0", f.api.draftListCalls, f.api.addCalls)
	}

	again, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, first)
	if err != nil || again.ID != publication.ID || f.api.draftListCalls != 1 || f.api.addCalls != 0 {
		t.Fatalf("idempotent replay = %#v err=%v calls=%d/%d", again, err, f.api.draftListCalls, f.api.addCalls)
	}
}

func TestCreateDraftRecoveryDoesNotOverwriteConcurrentAutomaticPublish(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	created := f.now.Add(-time.Hour)
	request := f.draftInput()
	article := normalizeDraftArticle(request.Articles[0])
	publication := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: f.taskID, ExecutionID: f.executionID, UserID: f.userID, ProjectID: f.projectID,
		DraftTitle: article.Title, DraftAuthor: article.Author, DraftDigest: article.Digest,
		DraftThumbMediaID: article.ThumbMediaID, DraftContentFingerprint: WechatContentFingerprint(article.Content),
		DraftRequestFingerprint: wechatDraftRequestFingerprint(article), Source: model.WechatPublicationSourceAnbanAPI,
		Status: model.WechatPublicationStatusDrafting, DraftCreatedAt: &created,
	}
	if err := f.repo.WechatPublications().Create(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{
		MediaID: "recovered-draft", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}},
	}}}
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "duplicate-publish"}
	f.overridePublications(&publishBetweenDraftRecoveryAndReloadRepository{
		WechatPublicationRepository: f.repo.WechatPublications(), db: f.db, now: f.now,
	})

	got, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request)
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	stored, findErr := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if got.Status != model.WechatPublicationStatusPublishing || stored.Status != model.WechatPublicationStatusPublishing ||
		stored.PublishID != "concurrent-publish" || stored.SubmitAttemptedAt == nil {
		t.Fatalf("publication overwritten: returned=%#v stored=%#v", got, stored)
	}
	if f.api.submitCalls != 0 {
		t.Fatalf("submit calls = %d, want 0", f.api.submitCalls)
	}
}

func TestCreateDraftResponseDoesNotOverwriteConcurrentAutomaticPublish(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-from-provider"}
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "duplicate-publish"}
	f.api.onAdd = func() {
		publication, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
		if err != nil {
			t.Fatal(err)
		}
		attemptedAt := f.now
		if err := f.db.Model(&model.WechatPublication{}).Where("id = ?", publication.ID).Updates(map[string]any{
			"status":              model.WechatPublicationStatusPublishing,
			"draft_media_id":      "draft-from-provider",
			"publish_id":          "concurrent-publish",
			"submit_attempted_at": &attemptedAt,
			"claim_token":         "",
			"claimed_at":          nil,
		}).Error; err != nil {
			t.Fatal(err)
		}
	}

	got, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	stored, findErr := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if got.Status != model.WechatPublicationStatusPublishing || stored.Status != model.WechatPublicationStatusPublishing ||
		stored.PublishID != "concurrent-publish" || stored.SubmitAttemptedAt == nil {
		t.Fatalf("publication overwritten: returned=%#v stored=%#v", got, stored)
	}
	if f.api.submitCalls != 0 {
		t.Fatalf("submit calls = %d, want 0", f.api.submitCalls)
	}
}

func TestCreateDraftRecoveryPaginatesAndRejectsUnsafeUpdateTimes(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	article := f.draftInput().Articles[0]
	f.api.draftListResponses = map[int64]*appwechat.DraftBatchGetResponse{
		0: {TotalCount: 3, ItemCount: 2, Items: []appwechat.DraftBatchItem{
			{MediaID: "old", UpdateTime: f.now.Add(-time.Minute).Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}}},
			{MediaID: "missing-time", UpdateTime: 0, Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}}},
		}},
		2: {TotalCount: 3, ItemCount: 1, Items: []appwechat.DraftBatchItem{
			{MediaID: "safe", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}}},
		}},
	}

	got, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatal(err)
	}
	if got.DraftMediaID != "safe" || f.api.addCalls != 0 {
		t.Fatalf("recovered=%#v addCalls=%d", got, f.api.addCalls)
	}
	if len(f.api.draftListRequests) != 2 || f.api.draftListRequests[0].Offset != 0 || f.api.draftListRequests[1].Offset != 2 {
		t.Fatalf("draft page requests=%#v", f.api.draftListRequests)
	}
}

func TestCreateDraftRecoveryAcceptsProviderTimestampInIntentSecond(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.now = f.now.Add(750 * time.Millisecond)
	article := f.draftInput().Articles[0]
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{
		MediaID: "same-second", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}},
	}}}

	got, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatal(err)
	}
	if got.DraftMediaID != "same-second" || f.api.addCalls != 0 {
		t.Fatalf("recovered=%#v addCalls=%d", got, f.api.addCalls)
	}
}

func TestCreateDraftRecoveryRejectsProviderTimestampInPriorSecond(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.now = f.now.Add(750 * time.Millisecond)
	article := f.draftInput().Articles[0]
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{
		MediaID: "prior-second", UpdateTime: f.now.Add(-time.Second).Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}},
	}}}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "new-draft"}

	got, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatal(err)
	}
	if got.DraftMediaID != "new-draft" || f.api.addCalls != 1 {
		t.Fatalf("created=%#v addCalls=%d", got, f.api.addCalls)
	}
}

func TestRecoverDraftFromItemsDoesNotOverwriteConcurrentLifecycleUpdate(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	created := f.now.Add(-time.Hour)
	request := f.draftInput()
	article := request.Articles[0]
	publication := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: f.taskID, UserID: f.userID, ProjectID: f.projectID,
		DraftTitle: "Durable lifecycle", DraftAuthor: "Author", DraftDigest: "Digest", DraftThumbMediaID: "thumb-1",
		DraftContentFingerprint: WechatContentFingerprint(article.Content),
		DraftRequestFingerprint: wechatDraftRequestFingerprint(normalizeDraftArticle(article)),
		Source:                  model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafting,
		DraftCreatedAt: &created, ClaimToken: "draft-claim",
	}
	if err := f.repo.WechatPublications().Create(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	stale, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	newClaimedAt := f.now
	if err := f.db.Model(&model.WechatPublication{}).Where("id = ?", publication.ID).Updates(map[string]any{
		"status": model.WechatPublicationStatusPublishSubmitting, "claim_token": "newer-claim", "claimed_at": &newClaimedAt,
	}).Error; err != nil {
		t.Fatal(err)
	}

	matchedItem := appwechat.DraftBatchItem{MediaID: "recovered-media", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}}}
	recovered, err := f.svc.recoverDraftFromItems(context.Background(), stale, []appwechat.DraftBatchItem{matchedItem})
	if !errors.Is(err, errWechatPublicationVersionChanged) {
		t.Fatalf("recoverDraftFromItems error = %v, want version changed", err)
	}
	if recovered {
		t.Fatal("recoverDraftFromItems reported recovery after losing its version")
	}
	stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.WechatPublicationStatusPublishSubmitting || stored.DraftMediaID != "" || stored.ClaimToken != "newer-claim" {
		t.Fatalf("concurrent lifecycle state overwritten: %#v", stored)
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

	_, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
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
	_, err = f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationPending) || f.api.draftListCalls != 2 || f.api.addCalls != 1 {
		t.Fatalf("retry err=%v calls=%d/%d; want pending and no second add", err, f.api.draftListCalls, f.api.addCalls)
	}
}

func TestCreateDraftReleasesFreshIntentAfterAPIFactoryFailure(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.svc.apiFactory = func(*model.Project) (WechatPublicationAPI, error) {
		return nil, errors.New("temporary API factory failure")
	}

	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput()); err == nil {
		t.Fatal("CreateDraft error = nil, want API factory failure")
	}
	if _, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("fresh intent remained after pre-provider failure: %v", err)
	}

	f.svc.apiFactory = func(*model.Project) (WechatPublicationAPI, error) { return f.api, nil }
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-after-factory-recovery"}
	retried, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil || retried.DraftMediaID != "draft-after-factory-recovery" || f.api.addCalls != 1 {
		t.Fatalf("retry = %#v err=%v addCalls=%d", retried, err, f.api.addCalls)
	}
}

func TestCreateDraftReleasesFreshIntentAfterReadOnlyPreflightFailure(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListError = errors.New("temporary draft-list failure")

	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput()); err == nil {
		t.Fatal("CreateDraft error = nil, want draft-list failure")
	}
	if _, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("fresh intent remained after read-only preflight failure: %v", err)
	}
	if f.api.addCalls != 0 {
		t.Fatalf("AddDraft calls = %d, want 0", f.api.addCalls)
	}

	f.api.draftListError = nil
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-after-list-recovery"}
	retried, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil || retried.DraftMediaID != "draft-after-list-recovery" || f.api.addCalls != 1 {
		t.Fatalf("retry = %#v err=%v addCalls=%d", retried, err, f.api.addCalls)
	}
}

func TestCreateDraftReclaimsStaleUnattemptedIntentAfterCrash(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	request := f.draftInput()
	article, err := firstDraftArticle(request)
	if err != nil {
		t.Fatal(err)
	}
	staleClaimedAt := f.now.Add(-wechatPublicationClaimLease - time.Second)
	intent := &model.WechatPublication{
		ID:                      uuid.NewString(),
		TaskID:                  f.taskID,
		ExecutionID:             f.executionID,
		UserID:                  f.userID,
		ProjectID:               f.projectID,
		DraftTitle:              article.Title,
		DraftAuthor:             article.Author,
		DraftDigest:             article.Digest,
		DraftThumbMediaID:       article.ThumbMediaID,
		DraftContentFingerprint: WechatContentFingerprint(article.Content),
		DraftRequestFingerprint: wechatDraftRequestFingerprint(article),
		Source:                  model.WechatPublicationSourceAnbanAPI,
		Status:                  model.WechatPublicationStatusDrafting,
		DraftCreatedAt:          &staleClaimedAt,
		ClaimToken:              uuid.NewString(),
		ClaimedAt:               &staleClaimedAt,
	}
	if err := f.repo.WechatPublications().Create(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-after-crash"}

	recovered, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, request)
	if err != nil {
		t.Fatalf("CreateDraft after crash: %v", err)
	}
	if recovered.ID != intent.ID || recovered.Status != model.WechatPublicationStatusDrafted || recovered.DraftMediaID != "draft-after-crash" {
		t.Fatalf("recovered intent = %#v", recovered)
	}
	if f.api.addCalls != 1 {
		t.Fatalf("AddDraft calls = %d, want exactly 1", f.api.addCalls)
	}
}

func TestDraftRetryPreflightFailurePreservesAmbiguousAddEvidence(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addError = errors.New("connection reset after WeChat may have accepted draft")

	first, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err == nil || first.Status != model.WechatPublicationStatusDrafting || f.api.addCalls != 1 {
		t.Fatalf("ambiguous AddDraft = %#v err=%v addCalls=%d", first, err, f.api.addCalls)
	}
	originalID := first.ID
	originalDraftCreatedAt := *first.DraftCreatedAt

	f.api.draftListError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}
	if err := f.svc.ReconcileProject(context.Background(), f.projectID); err != nil {
		t.Fatalf("48001 reconciliation: %v", err)
	}
	unsupported, err := f.repo.WechatPublications().FindByID(context.Background(), originalID)
	if err != nil || unsupported.Status != model.WechatPublicationStatusUnsupported || unsupported.WechatStatusCode != 48001 {
		t.Fatalf("unsupported = %#v err=%v", unsupported, err)
	}

	f.now = f.now.Add(time.Hour)
	f.api.draftListError = errors.New("temporary draft-list failure after permission repair")
	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput()); err == nil {
		t.Fatal("retry preflight error = nil")
	}
	preserved, err := f.repo.WechatPublications().FindByID(context.Background(), originalID)
	if err != nil || preserved.Status != model.WechatPublicationStatusUnsupported || preserved.WechatStatusCode != 48001 ||
		preserved.DraftCreatedAt == nil || !preserved.DraftCreatedAt.Equal(originalDraftCreatedAt) {
		t.Fatalf("preserved ambiguous intent = %#v err=%v", preserved, err)
	}

	f.api.draftListError = nil
	f.api.addError = nil
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "unexpected-duplicate-draft"}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	pending, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationPending) || pending.ID != originalID || f.api.addCalls != 1 {
		t.Fatalf("empty-list retry = %#v err=%v addCalls=%d; want reconciliation without another AddDraft", pending, err, f.api.addCalls)
	}

	article := f.draftInput().Articles[0]
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{
		MediaID: "original-ambiguous-draft", UpdateTime: originalDraftCreatedAt.Add(time.Minute).Unix(),
		Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{article}},
	}}}
	recovered, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil || recovered.ID != originalID || recovered.DraftMediaID != "original-ambiguous-draft" || f.api.addCalls != 1 {
		t.Fatalf("recovered = %#v err=%v addCalls=%d", recovered, err, f.api.addCalls)
	}
}

func TestCreateDraftRetriesDefinitive48001AfterPermissionRepair(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}

	first, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationDraftUnsupported) || first.Status != model.WechatPublicationStatusUnsupported {
		t.Fatalf("first CreateDraft = %#v, %v", first, err)
	}
	f.api.draftListError = nil
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-after-repair"}

	retried, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatalf("retried CreateDraft: %v", err)
	}
	if retried.Status != model.WechatPublicationStatusDrafted || retried.DraftMediaID != "draft-after-repair" || f.api.addCalls != 1 {
		t.Fatalf("retried = %#v addCalls=%d", retried, f.api.addCalls)
	}
}

func TestCreateDraftRetriesDefinitive40164AfterIPAllowlistRepair(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addError = &appwechat.WechatAPIError{ErrCode: 40164, UserMsg: "request ip is not in whitelist"}

	first, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationDraftRejected) {
		t.Fatalf("first CreateDraft = %#v err=%v, want definitive rejection", first, err)
	}
	if first.Status != model.WechatPublicationStatusUnsupported || first.WechatStatusCode != 40164 || first.DraftAddAttemptedAt != nil {
		t.Fatalf("definitively rejected draft = %#v", first)
	}

	f.api.addError = nil
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-after-allowlist-repair"}
	retried, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatalf("retried CreateDraft: %v", err)
	}
	if retried.Status != model.WechatPublicationStatusDrafted || retried.DraftMediaID != "draft-after-allowlist-repair" || f.api.addCalls != 2 {
		t.Fatalf("retried = %#v addCalls=%d", retried, f.api.addCalls)
	}
}

func TestDefinitiveDraftRetryPreflightFailurePreservesOriginalCode(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addError = &appwechat.WechatAPIError{ErrCode: 40164, UserMsg: "request ip is not in whitelist"}

	first, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationDraftRejected) || first.WechatStatusCode != 40164 {
		t.Fatalf("first CreateDraft = %#v err=%v", first, err)
	}

	f.api.addError = nil
	f.api.draftListError = errors.New("temporary draft-list failure after allowlist repair")
	if _, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput()); err == nil {
		t.Fatal("retry preflight error = nil")
	}
	preserved, err := f.repo.WechatPublications().FindByID(context.Background(), first.ID)
	if err != nil || preserved.Status != model.WechatPublicationStatusUnsupported || preserved.WechatStatusCode != 40164 {
		t.Fatalf("preserved rejection = %#v err=%v", preserved, err)
	}

	f.api.draftListError = nil
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "draft-after-transient-preflight"}
	retried, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil || retried.Status != model.WechatPublicationStatusDrafted || retried.DraftMediaID != "draft-after-transient-preflight" {
		t.Fatalf("retried = %#v err=%v", retried, err)
	}
}

func TestAmbiguousDraftAddSchedulesExactBatchRecoveryWithoutResubmission(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addError = errors.New("connection reset after WeChat accepted the draft")

	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err == nil {
		t.Fatal("CreateDraft error = nil, want ambiguous provider failure")
	}
	if publication.NextCheckAt == nil || !publication.NextCheckAt.Equal(f.now.Add(10*time.Minute)) {
		t.Fatalf("ambiguous next_check_at = %v", publication.NextCheckAt)
	}
	if len(enqueuer.delayed) != 1 || enqueuer.delayed[0].taskType != WechatPublicationReconcileTaskType {
		t.Fatalf("recovery tasks = %#v", enqueuer.delayed)
	}

	recoveredArticle := f.draftInput().Articles[0]
	f.api.addError = nil
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{
		MediaID: "recovered-draft", UpdateTime: f.now.Unix(), Content: appwechat.DraftContent{NewsItems: []appwechat.DraftArticle{recoveredArticle}},
	}}}
	if err := f.svc.ReconcileProject(context.Background(), f.projectID); err != nil {
		t.Fatal(err)
	}
	stored, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil || stored.Status != model.WechatPublicationStatusDrafted || stored.DraftMediaID != "recovered-draft" {
		t.Fatalf("recovered publication = %#v err=%v", stored, err)
	}
	if f.api.addCalls != 1 || f.api.draftListCalls != 2 {
		t.Fatalf("provider calls add=%d draft-list=%d, want 1/2", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestAmbiguousDraftRecoveryReturnsTerminalFailureAfterPermissionRepairAnd72Hours(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addError = errors.New("connection reset after request write")

	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err == nil {
		t.Fatal("CreateDraft error = nil, want ambiguous provider failure")
	}
	f.api.draftListError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}
	if err := f.svc.ReconcileProject(context.Background(), f.projectID); err != nil {
		t.Fatalf("48001 reconciliation: %v", err)
	}

	f.now = publication.DraftCreatedAt.Add(72*time.Hour + time.Second)
	f.api.draftListError = nil
	f.api.addError = nil
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "unexpected-duplicate-draft"}
	terminal, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationDraftFailed) {
		t.Fatalf("expired retry = %#v err=%v, want terminal draft failure", terminal, err)
	}
	stored, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.WechatPublicationStatusPublishFailed || stored.NextCheckAt != nil || stored.DraftAddAttemptedAt == nil || stored.LastError == "" {
		t.Fatalf("expired ambiguous draft = %#v", stored)
	}
	if f.api.addCalls != 1 {
		t.Fatalf("draft/add calls = %d, want exactly 1", f.api.addCalls)
	}

	replayed, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if !errors.Is(err, ErrWechatPublicationDraftFailed) || replayed.ID != publication.ID {
		t.Fatalf("terminal replay = %#v err=%v, want same terminal draft failure", replayed, err)
	}
	if f.api.addCalls != 1 {
		t.Fatalf("draft/add calls after replay = %d, want exactly 1", f.api.addCalls)
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

func TestFormalPublishRequiresDurableEnqueuerBeforeProviderCalls(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	f.svc.SetEnqueuer(nil)

	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationSchedulerUnavailable) {
		t.Fatalf("Publish error = %v, want scheduler unavailable", err)
	}
	if f.api.publishedListCalls != 0 || f.api.draftListCalls != 0 || f.api.submitCalls != 0 {
		t.Fatalf("provider calls before scheduler check: published=%d drafts=%d submit=%d", f.api.publishedListCalls, f.api.draftListCalls, f.api.submitCalls)
	}
}

func TestManualModeAllowsExplicitFormalPublish(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.seedDrafted(t)
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "manual-publish"}

	publication, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if publication.Status != model.WechatPublicationStatusPublishing || publication.PublishID != "manual-publish" {
		t.Fatalf("publication = %#v", publication)
	}
}

func TestAPIConfirmedModeAutomaticallyPublishesAfterDraftCreation(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "auto-draft"}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{
		TotalCount: 1,
		ItemCount:  1,
		Items:      []appwechat.DraftBatchItem{{MediaID: "auto-draft", UpdateTime: f.now.Unix()}},
	}
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "auto-publish"}

	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if publication.Status != model.WechatPublicationStatusPublishing || publication.PublishID != "auto-publish" {
		t.Fatalf("publication = %#v", publication)
	}
	if f.api.submitCalls != 1 {
		t.Fatalf("submit calls = %d, want 1", f.api.submitCalls)
	}
}

func TestFreshPublishClaimResetsPreflightReconciliationAttempts(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	lastCheckedAt := f.now.Add(-time.Minute)
	publication.CheckAttempts = 17
	publication.LastCheckedAt = &lastCheckedAt
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "fresh-publish"}

	submitted, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil || submitted.Status != model.WechatPublicationStatusPublishing || submitted.CheckAttempts != 0 || submitted.LastCheckedAt != nil {
		t.Fatalf("submitted = %#v err=%v", submitted, err)
	}
	f.api.getResponses = []*appwechat.FreePublishGetResponse{{PublishID: "fresh-publish", PublishStatus: appwechat.FreePublishStatusPublishing}}
	polled, err := f.svc.Poll(context.Background(), publication.ID)
	if err != nil || polled.CheckAttempts != 1 || polled.NextCheckAt == nil || !polled.NextCheckAt.Equal(f.now.Add(15*time.Second)) {
		t.Fatalf("first poll = %#v err=%v", polled, err)
	}
}

func TestRetryPublishResubmitsUnsupportedFormalPublishAfterPermissionRepair(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.seedDrafted(t)
	f.api.submitError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}

	unsupported, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil || unsupported.Status != model.WechatPublicationStatusUnsupported {
		t.Fatalf("first Publish = %#v, %v", unsupported, err)
	}

	f.api.submitError = nil
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "retry-publish"}
	retried, err := f.svc.RetryPublish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("RetryPublish: %v", err)
	}
	if retried.Status != model.WechatPublicationStatusPublishing || retried.PublishID != "retry-publish" {
		t.Fatalf("retried = %#v", retried)
	}
	if f.api.submitCalls != 2 {
		t.Fatalf("submit calls = %d, want 2", f.api.submitCalls)
	}
}

func TestPublishPersists48001FromPreflightAsRetryableUnsupported(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.seedDrafted(t)
	f.api.publishedListError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}

	publication, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if publication.Status != model.WechatPublicationStatusUnsupported || publication.WechatStatusCode != 48001 {
		t.Fatalf("publication = %#v", publication)
	}
	stored, findErr := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if findErr != nil || stored.Status != model.WechatPublicationStatusUnsupported {
		t.Fatalf("stored = %#v, %v", stored, findErr)
	}
}

func TestDefinitive48001ClearsSubmissionEvidenceForSafeRetry(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.seedDrafted(t)
	f.api.submitError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}

	publication, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if publication.Status != model.WechatPublicationStatusUnsupported || publication.SubmitAttemptedAt != nil {
		t.Fatalf("publication = %#v", publication)
	}
	stored, findErr := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if findErr != nil || stored.SubmitAttemptedAt != nil {
		t.Fatalf("stored = %#v, %v", stored, findErr)
	}
}

func TestDefinitiveSubmitRejectionClearsEvidenceAndCanRetry(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.seedDrafted(t)
	f.api.submitError = &appwechat.WechatAPIError{ErrCode: 40007, UserMsg: "invalid media id", Retryable: false}

	unsupported, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if unsupported.Status != model.WechatPublicationStatusUnsupported || unsupported.WechatStatusCode != 40007 ||
		unsupported.SubmitAttemptedAt != nil || unsupported.NextCheckAt != nil {
		t.Fatalf("unsupported = %#v", unsupported)
	}

	f.api.submitError = nil
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "retry-after-rejection"}
	retried, err := f.svc.RetryPublish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("RetryPublish: %v", err)
	}
	if retried.Status != model.WechatPublicationStatusPublishing || retried.PublishID != "retry-after-rejection" || f.api.submitCalls != 2 {
		t.Fatalf("retried = %#v submitCalls=%d", retried, f.api.submitCalls)
	}
}

func TestRetryPublishWithSubmissionEvidenceOnlyResumesObservation(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	attemptedAt := f.now.Add(-time.Minute)
	publication.Status = model.WechatPublicationStatusUnsupported
	publication.PublishID = "accepted-publish"
	publication.SubmitAttemptedAt = &attemptedAt
	publication.WechatStatusCode = 48001
	publication.LastError = "api unauthorized"
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)

	retried, err := f.svc.RetryPublish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("RetryPublish: %v", err)
	}
	if retried.Status != model.WechatPublicationStatusPublishing || retried.NextCheckAt == nil {
		t.Fatalf("retried = %#v", retried)
	}
	if f.api.submitCalls != 0 {
		t.Fatalf("submit calls = %d, want 0", f.api.submitCalls)
	}
	if len(enqueuer.delayed) != 1 || enqueuer.delayed[0].taskType != WechatPublicationPollTaskType {
		t.Fatalf("queued = %#v", enqueuer.delayed)
	}
}

func TestPublishDoesNotResubmitADraftWithSubmissionEvidence(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	attemptedAt := f.now.Add(-time.Minute)
	publication.SubmitAttemptedAt = &attemptedAt
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}

	result, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if !errors.Is(err, ErrWechatPublicationPending) {
		t.Fatalf("Publish error = %v, want pending", err)
	}
	if result.Status != model.WechatPublicationStatusPublishSubmitting || result.NextCheckAt == nil {
		t.Fatalf("publication = %#v", result)
	}
	if f.api.submitCalls != 0 {
		t.Fatalf("submit calls = %d, want 0", f.api.submitCalls)
	}
}

func TestAutomaticDraftCreationPreservesDraftSuccessWhenFormalPublishIsTemporarilyUnavailable(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "durable-draft"}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{
		TotalCount: 1,
		ItemCount:  1,
		Items:      []appwechat.DraftBatchItem{{MediaID: "durable-draft", UpdateTime: f.now.Unix()}},
	}
	f.api.publishedListError = errors.New("temporary published-list failure")

	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if publication.Status != model.WechatPublicationStatusDrafted || publication.DraftMediaID != "durable-draft" || publication.NextCheckAt == nil {
		t.Fatalf("publication = %#v", publication)
	}
	if len(enqueuer.delayed) == 0 || enqueuer.delayed[len(enqueuer.delayed)-1].taskType != WechatPublicationReconcileTaskType {
		t.Fatalf("queued = %#v", enqueuer.delayed)
	}
}

func TestAutomaticDraftCreationDoesNotPersistProviderURLSecrets(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "safe-draft"}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{
		TotalCount: 1,
		ItemCount:  1,
		Items:      []appwechat.DraftBatchItem{{MediaID: "safe-draft", UpdateTime: f.now.Unix()}},
	}
	f.api.publishedListError = errors.New("Post https://api.weixin.qq.com/cgi-bin/freepublish/batchget?access_token=secret-token: connection reset")

	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil {
		t.Fatalf("CreateDraft: %v", err)
	}
	if strings.Contains(publication.LastError, "access_token") || strings.Contains(publication.LastError, "secret-token") ||
		strings.Contains(publication.LastError, "api.weixin.qq.com") {
		t.Fatalf("LastError leaked provider URL: %q", publication.LastError)
	}
	if publication.LastError == "" {
		t.Fatal("LastError should retain safe user-facing context")
	}
}

func TestRetryPublishRejectsUnsupportedStatesWithoutProviderErrorCode(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	publication.Status = model.WechatPublicationStatusUnsupported
	publication.WechatStatusCode = 0
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}

	if _, err := f.svc.RetryPublish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationConflict) {
		t.Fatalf("RetryPublish error = %v, want conflict", err)
	}
	if f.api.submitCalls != 0 {
		t.Fatalf("submit calls = %d, want 0", f.api.submitCalls)
	}
}

func TestRetryPublishRequiresDurableEnqueuerBeforeStateChange(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	publication.Status = model.WechatPublicationStatusUnsupported
	publication.WechatStatusCode = 48001
	publication.LastError = "api unauthorized"
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	f.svc.SetEnqueuer(nil)

	if _, err := f.svc.RetryPublish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationSchedulerUnavailable) {
		t.Fatalf("RetryPublish error = %v, want scheduler unavailable", err)
	}
	stored, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.WechatPublicationStatusUnsupported || stored.WechatStatusCode != 48001 {
		t.Fatalf("stored = %#v, want unchanged unsupported state", stored)
	}
	if f.api.publishedListCalls != 0 || f.api.draftListCalls != 0 || f.api.submitCalls != 0 {
		t.Fatalf("provider calls before scheduler check: published=%d drafts=%d submit=%d", f.api.publishedListCalls, f.api.draftListCalls, f.api.submitCalls)
	}
}

func TestAutomaticReconciliationSubmitsAStillDraftedArticle(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "reconciled-auto-publish"}

	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	stored, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil || stored.Status != model.WechatPublicationStatusPublishing || stored.PublishID != "reconciled-auto-publish" {
		t.Fatalf("stored = %#v, %v", stored, err)
	}
	if f.api.submitCalls != 1 {
		t.Fatalf("submit calls = %d, want 1", f.api.submitCalls)
	}
}

func TestAutomaticReconciliationStopsOnDefinitiveProviderErrorAndCanRetry(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	f.api.publishedListError = &appwechat.WechatAPIError{ErrCode: 40164, UserMsg: "request rejected", Retryable: false}

	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	unsupported, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if unsupported.Status != model.WechatPublicationStatusUnsupported || unsupported.WechatStatusCode != 40164 || unsupported.NextCheckAt != nil {
		t.Fatalf("unsupported = %#v", unsupported)
	}

	f.api.publishedListError = nil
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{PublishID: "repaired-publish"}
	retried, err := f.svc.RetryPublish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("RetryPublish: %v", err)
	}
	if retried.Status != model.WechatPublicationStatusPublishing || retried.PublishID != "repaired-publish" || f.api.submitCalls != 1 {
		t.Fatalf("retried = %#v submitCalls=%d", retried, f.api.submitCalls)
	}
}

func TestTransientReconciliationFailureAdvancesCadenceAndStopsAtDeadline(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	publication := f.seedDrafted(t)
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)
	f.api.publishedListError = errors.New("temporary published-list failure")
	project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || stored.Status != model.WechatPublicationStatusDrafted || stored.NextCheckAt == nil ||
		!stored.NextCheckAt.Equal(f.now.Add(10*time.Minute)) {
		t.Fatalf("scheduled = %#v err=%v", stored, err)
	}
	if len(enqueuer.delayed) != 1 || enqueuer.delayed[0].taskType != WechatPublicationReconcileTaskType {
		t.Fatalf("queued = %#v", enqueuer.delayed)
	}

	f.now = publication.DraftCreatedAt.Add(73 * time.Hour)
	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("deadline reconcile: %v", err)
	}
	stored, err = f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || stored.Status != model.WechatPublicationStatusPublishFailed || stored.NextCheckAt != nil {
		t.Fatalf("terminal = %#v err=%v", stored, err)
	}
}

func TestManualDraftRemainsPublishableWhenBackgroundReconciliationExpires(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	oldDraftCreatedAt := f.now.Add(-73 * time.Hour)
	publication.DraftCreatedAt = &oldDraftCreatedAt
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListError = errors.New("temporary published-list failure")
	project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}

	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || stored.Status != model.WechatPublicationStatusDrafted || stored.NextCheckAt != nil {
		t.Fatalf("stored = %#v err=%v", stored, err)
	}
}

func TestManualDraftAndImmediateReconciliationDoNotRequireEnqueuer(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	f.svc.SetEnqueuer(nil)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}
	f.api.addResponse = &appwechat.DraftAddResponse{MediaID: "manual-draft"}

	publication, err := f.svc.CreateDraft(context.Background(), f.userID, f.taskID, f.projectID, f.executionID, f.draftInput())
	if err != nil || publication.DraftMediaID != "manual-draft" {
		t.Fatalf("CreateDraft = %#v, %v", publication, err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatalf("Reconcile without enqueuer: %v", err)
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

func TestAmbiguousAPIPublishSelectionPreservesResponseIdentity(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	f.api.submitResponse = &appwechat.FreePublishSubmitResponse{MsgDataID: "msg-data-from-submit"}

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if !errors.Is(err, ErrWechatPublicationPending) {
		t.Fatalf("Publish err=%v, want pending", err)
	}
	if got.MsgDataID != "msg-data-from-submit" {
		t.Fatalf("pending MsgDataID=%q", got.MsgDataID)
	}

	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "selected-after-response-loss", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
			Title: p.DraftTitle, Digest: "different", ThumbMediaID: "different", Content: "<p>different</p>", URL: "https://mp.weixin.qq.com/s/selected-after-response-loss",
		}}},
	}}}
	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	pending, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil || pending.Status != model.WechatPublicationStatusNeedsSelection {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}

	selected, err := f.svc.Select(context.Background(), f.userID, f.taskID, "selected-after-response-loss")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Source != model.WechatPublicationSourceAnbanAPI || selected.MsgDataID != "msg-data-from-submit" || selected.MsgID != "msg-data-from-submit_1" {
		t.Fatalf("selected=%#v", selected)
	}
	tracking, err := f.repo.WechatTrackings().FindByTaskID(context.Background(), f.taskID)
	if err != nil || tracking.MsgID != "msg-data-from-submit_1" {
		t.Fatalf("tracking=%#v err=%v", tracking, err)
	}
}

func TestPublishTransportAmbiguityPersistsPendingBeforeReturning(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	f.api.submitError = errors.New("connection reset after submit")

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if !errors.Is(err, ErrWechatPublicationPending) || got.Status != model.WechatPublicationStatusPublishSubmitting || got.NextCheckAt == nil {
		t.Fatalf("Publish=%#v err=%v", got, err)
	}
	stored, findErr := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if findErr != nil || stored.Status != model.WechatPublicationStatusPublishSubmitting || stored.NextCheckAt == nil {
		t.Fatalf("stored=%#v err=%v", stored, findErr)
	}
}

func TestAmbiguousSubmitOnOldDraftUsesFreshSubmissionDeadline(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	publication := f.seedDrafted(t)
	oldDraftCreatedAt := f.now.Add(-96 * time.Hour)
	publication.DraftCreatedAt = &oldDraftCreatedAt
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	f.api.submitError = errors.New("connection reset after submit")

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if !errors.Is(err, ErrWechatPublicationPending) {
		t.Fatalf("Publish error = %v, want pending", err)
	}
	if got.Status != model.WechatPublicationStatusPublishSubmitting || got.SubmitAttemptedAt == nil ||
		got.NextCheckAt == nil || !got.NextCheckAt.Equal(f.now.Add(10*time.Minute)) {
		t.Fatalf("publication = %#v", got)
	}
}

func TestFormalReconciliationUsesSubmissionDeadline(t *testing.T) {
	t.Run("recent submission on old draft keeps retrying transient list errors", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		publication := f.seedDrafted(t)
		oldDraftCreatedAt := f.now.Add(-96 * time.Hour)
		recentSubmission := f.now.Add(-time.Hour)
		publication.DraftCreatedAt = &oldDraftCreatedAt
		publication.SubmitAttemptedAt = &recentSubmission
		publication.Status = model.WechatPublicationStatusPublishSubmitting
		if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
			t.Fatal(err)
		}
		f.api.publishedListError = errors.New("temporary published-list failure")
		project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
		if err != nil {
			t.Fatal(err)
		}

		if err := f.svc.reconcileProject(context.Background(), project); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusPublishSubmitting || stored.NextCheckAt == nil ||
			!stored.NextCheckAt.Equal(f.now.Add(10*time.Minute)) {
			t.Fatalf("stored = %#v err=%v", stored, err)
		}
	})

	t.Run("successful empty list becomes terminal after submission deadline", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		publication := f.seedDrafted(t)
		oldDraftCreatedAt := f.now.Add(-96 * time.Hour)
		oldSubmission := f.now.Add(-73 * time.Hour)
		publication.DraftCreatedAt = &oldDraftCreatedAt
		publication.SubmitAttemptedAt = &oldSubmission
		publication.Status = model.WechatPublicationStatusPublishSubmitting
		if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
			t.Fatal(err)
		}
		project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
		if err != nil {
			t.Fatal(err)
		}

		if err := f.svc.reconcileProject(context.Background(), project); err != nil {
			t.Fatalf("reconcile: %v", err)
		}
		stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
		if err != nil || stored.Status != model.WechatPublicationStatusPublishFailed || stored.NextCheckAt != nil {
			t.Fatalf("stored = %#v err=%v", stored, err)
		}
	})
}

func TestAmbiguousTransportSubmitReconcilesAsAnbanAPI(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	f.api.submitError = errors.New("connection reset after submit")

	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationPending) {
		t.Fatalf("Publish err=%v, want pending", err)
	}
	stored, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil || stored.SubmitAttemptedAt == nil {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}

	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "selected-after-transport-loss", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
			Title: p.DraftTitle, Digest: "different", ThumbMediaID: "different", Content: "<p>different</p>", URL: "https://mp.weixin.qq.com/s/selected-after-transport-loss",
		}}},
	}}}
	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	selected, err := f.svc.Select(context.Background(), f.userID, f.taskID, "selected-after-transport-loss")
	if err != nil {
		t.Fatal(err)
	}
	if selected.Source != model.WechatPublicationSourceAnbanAPI || selected.MsgDataID != "" || selected.MsgID != "" {
		t.Fatalf("selected=%#v", selected)
	}
}

func TestPublishDoesNotSubmitWhenAttemptEvidenceCannotPersist(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	persistErr := errors.New("persist submit attempt failed")
	f.overridePublications(failingWechatPublicationRepository{
		WechatPublicationRepository: f.repo.WechatPublications(),
		claimPublishErr:             persistErr,
	})

	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, persistErr) {
		t.Fatalf("Publish err=%v", err)
	}
	if f.api.submitCalls != 0 {
		t.Fatalf("submit calls=%d, want 0", f.api.submitCalls)
	}
}

func TestPublicationResponseLossPathsPropagatePersistenceFailures(t *testing.T) {
	persistErr := errors.New("publication persistence failed")
	t.Run("ambiguous submit", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		f.seedDrafted(t)
		f.api.submitError = errors.New("connection reset")
		f.overridePublications(failingWechatPublicationRepository{WechatPublicationRepository: f.repo.WechatPublications(), updateClaimedErr: persistErr})
		if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, persistErr) {
			t.Fatalf("Publish err=%v", err)
		}
	})

	t.Run("poll failure", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		p := f.seedDrafted(t)
		p.Status, p.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
		if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		f.api.getError = errors.New("provider unavailable")
		f.overridePublications(failingWechatPublicationRepository{WechatPublicationRepository: f.repo.WechatPublications(), updateErr: persistErr})
		if _, err := f.svc.Poll(context.Background(), p.ID); !errors.Is(err, persistErr) {
			t.Fatalf("Poll err=%v", err)
		}
	})

	t.Run("unsupported reconcile", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeManual)
		f.seedDrafted(t)
		f.api.publishedListError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "unsupported"}
		f.overridePublications(failingWechatPublicationRepository{WechatPublicationRepository: f.repo.WechatPublications(), updateErr: persistErr})
		if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); !errors.Is(err, persistErr) {
			t.Fatalf("Reconcile err=%v", err)
		}
	})

	t.Run("binding lookup", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeManual)
		p := f.seedDrafted(t)
		f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
			ArticleID: "candidate", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: p.DraftTitle, Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/candidate"}}},
		}}}
		f.overridePublications(failingWechatPublicationRepository{WechatPublicationRepository: f.repo.WechatPublications(), findArticleErr: persistErr})
		if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); !errors.Is(err, persistErr) {
			t.Fatalf("Reconcile err=%v", err)
		}
	})
}

func TestPublishPreflightFindsPublishedArticleAcrossPagesBeforeSubmit(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	f.api.publishedListResponses = map[int64]*appwechat.FreePublishBatchGetResponse{
		0: {TotalCount: 2, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
			ArticleID: "unrelated", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: "Other", Content: "<p>other</p>", URL: "https://mp.weixin.qq.com/s/other"}}},
		}}},
		1: {TotalCount: 2, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
			ArticleID: "already-published", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: p.DraftTitle, Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/already"}}},
		}}},
	}

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.WechatPublicationStatusPublished || got.ArticleID != "already-published" || f.api.submitCalls != 0 {
		t.Fatalf("preflight publication=%#v submitCalls=%d", got, f.api.submitCalls)
	}
	if len(f.api.publishedListRequests) != 2 || f.api.publishedListRequests[1].Offset != 1 {
		t.Fatalf("published page requests=%#v", f.api.publishedListRequests)
	}
}

func TestPublishPreflightPersistsPendingWhenDraftDisappeared(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{MediaID: "different", UpdateTime: f.now.Unix()}}}

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if !errors.Is(err, ErrWechatPublicationPending) {
		t.Fatalf("Publish err=%v, want pending", err)
	}
	if got.Status != model.WechatPublicationStatusPublishSubmitting || got.NextCheckAt == nil || f.api.submitCalls != 0 {
		t.Fatalf("missing-draft publication=%#v submitCalls=%d", got, f.api.submitCalls)
	}
}

func TestMissingDraftPreflightReconcilesAsWechatConsole(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{MediaID: "different", UpdateTime: f.now.Unix()}}}

	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationPending) {
		t.Fatalf("Publish err=%v, want pending", err)
	}
	if f.api.submitCalls != 0 {
		t.Fatalf("submit calls=%d, want 0", f.api.submitCalls)
	}

	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "console-after-missing-draft", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
			Title: p.DraftTitle, Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/console-after-missing-draft",
		}}},
	}}}
	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	got, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil || got.Status != model.WechatPublicationStatusPublished || got.Source != model.WechatPublicationSourceWechatConsole {
		t.Fatalf("reconciled=%#v err=%v", got, err)
	}
}

func TestPublishPreflightCandidateDoesNotRegressConcurrentBinding(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "stale-candidate", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
			Title: p.DraftTitle, Digest: "different", ThumbMediaID: "different", Content: "<p>different</p>", URL: "https://mp.weixin.qq.com/s/stale-candidate",
		}}},
	}}}
	f.api.onPublishedList = func(call int) {
		if call != 1 {
			return
		}
		latest, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
		if err != nil {
			t.Error(err)
			return
		}
		_, err = f.svc.bindPublished(context.Background(), latest, publishedWechatArticle{
			ArticleID: "concurrent-winner", URL: "https://mp.weixin.qq.com/s/concurrent-winner", PublishedAt: f.now, Index: 1,
		}, model.WechatPublicationSourceWechatConsole)
		if err != nil {
			t.Error(err)
		}
	}

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("Publish err=%v", err)
	}
	assertConcurrentPreflightWinner(t, f, got)
}

func TestPublishPreflightMissingDraftDoesNotRegressConcurrentBinding(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.DraftBatchItem{{MediaID: "different", UpdateTime: f.now.Unix()}}}
	f.api.onPublishedList = func(call int) {
		if call != 1 {
			return
		}
		latest, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
		if err != nil {
			t.Error(err)
			return
		}
		_, err = f.svc.bindPublished(context.Background(), latest, publishedWechatArticle{
			ArticleID: "concurrent-winner", URL: "https://mp.weixin.qq.com/s/concurrent-winner", PublishedAt: f.now, Index: 1,
		}, model.WechatPublicationSourceWechatConsole)
		if err != nil {
			t.Error(err)
		}
	}

	got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if err != nil {
		t.Fatalf("Publish err=%v", err)
	}
	assertConcurrentPreflightWinner(t, f, got)
}

func assertConcurrentPreflightWinner(t *testing.T, f *publicationFixture, got *model.WechatPublication) {
	t.Helper()
	if got.Status != model.WechatPublicationStatusPublished || got.ArticleID != "concurrent-winner" || f.api.submitCalls != 0 {
		t.Fatalf("publication=%#v submitCalls=%d", got, f.api.submitCalls)
	}
	stored, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil || stored.Status != model.WechatPublicationStatusPublished || stored.ArticleID != "concurrent-winner" {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	bound, err := f.repo.WechatPublications().FindByArticleID(context.Background(), f.projectID, "concurrent-winner")
	if err != nil || bound.ID != stored.ID {
		t.Fatalf("bound=%#v err=%v", bound, err)
	}
	tracking, err := f.repo.WechatTrackings().FindByTaskID(context.Background(), f.taskID)
	if err != nil || tracking.ArticleID != "concurrent-winner" {
		t.Fatalf("tracking=%#v err=%v", tracking, err)
	}
}

func TestPollPublishMapsStatusesAndDurableSchedule(t *testing.T) {
	t.Run("publishing schedule", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		enqueuer := &recordingWechatPublicationEnqueuer{}
		f.svc.SetEnqueuer(enqueuer)
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
		if len(enqueuer.delayed) != 1 || enqueuer.delayed[0].taskType != WechatPublicationPollTaskType ||
			enqueuer.delayed[0].payload["publication_id"] != p.ID || enqueuer.delayed[0].delay != 15*time.Second {
			t.Fatalf("delayed tasks = %#v", enqueuer.delayed)
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

func TestPollFallsBackToPublishedListReconciliationAfterTwoHours(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)
	p := f.seedDrafted(t)
	p.Status, p.PublishID, p.CheckAttempts = model.WechatPublicationStatusPublishing, "publish-1", 16
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.getResponses = []*appwechat.FreePublishGetResponse{{PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusPublishing}}

	got, err := f.svc.Poll(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.WechatPublicationStatusPublishSubmitting || got.NextCheckAt == nil || got.LastError == "" {
		t.Fatalf("poll timeout fallback = %#v", got)
	}
	if len(enqueuer.delayed) != 1 || enqueuer.delayed[0].taskType != WechatPublicationReconcileTaskType || enqueuer.delayed[0].payload["project_id"] != f.projectID {
		t.Fatalf("delayed tasks = %#v", enqueuer.delayed)
	}
}

func TestProcessPollDoesNotRetryAnOutcomeAlreadyDurablyScheduled(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	p.Status, p.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.getError = errors.New("temporary provider failure")

	if err := f.svc.ProcessPoll(context.Background(), p.ID); err != nil {
		t.Fatalf("ProcessPoll returned a retryable queue error after persisting next_check_at: %v", err)
	}
	stored, err := f.repo.WechatPublications().FindByID(context.Background(), p.ID)
	if err != nil || stored.NextCheckAt == nil || stored.LastError == "" {
		t.Fatalf("durable retry state = %#v err=%v", stored, err)
	}
}

func TestWechatPublicationRecoverDueDispatchesDatabaseBackedWork(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)

	poll := f.seedDrafted(t)
	poll.Status, poll.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
	due := f.now.Add(-time.Minute)
	poll.NextCheckAt = &due
	if err := f.repo.WechatPublications().Update(context.Background(), poll); err != nil {
		t.Fatal(err)
	}

	manualTaskID := uuid.NewString()
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{ID: manualTaskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	manual := &model.WechatPublication{
		ID: uuid.NewString(), TaskID: manualTaskID, UserID: f.userID, ProjectID: f.projectID,
		DraftMediaID: "draft-2", Source: model.WechatPublicationSourceAnbanAPI,
		Status: model.WechatPublicationStatusDrafted, DraftCreatedAt: &due, NextCheckAt: &due,
	}
	if err := f.repo.WechatPublications().Create(context.Background(), manual); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RecoverDue(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if len(enqueuer.immediate) != 2 {
		t.Fatalf("immediate tasks = %#v", enqueuer.immediate)
	}
	seen := map[string]string{}
	for _, task := range enqueuer.immediate {
		seen[task.taskType] = task.payload["publication_id"] + task.payload["project_id"]
	}
	if seen[WechatPublicationPollTaskType] != poll.ID || seen[WechatPublicationReconcileTaskType] != f.projectID {
		t.Fatalf("dispatched work = %#v", seen)
	}
	for _, id := range []string{poll.ID, manual.ID} {
		claimed, err := f.repo.WechatPublications().FindByID(context.Background(), id)
		if err != nil || claimed.NextCheckAt == nil || !claimed.NextCheckAt.Equal(f.now.Add(15*time.Minute)) {
			t.Fatalf("recovery lease for %s = %#v err=%v", id, claimed, err)
		}
	}
}

func TestWechatPublicationRecoverDueReleasesDispatchLeaseAfterEnqueueFailure(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	enqueuer := &recordingWechatPublicationEnqueuer{err: errors.New("redis unavailable")}
	f.svc.SetEnqueuer(enqueuer)

	publication := f.seedDrafted(t)
	due := f.now.Add(-time.Minute)
	publication.NextCheckAt = &due
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.RecoverDue(context.Background(), 100); err == nil {
		t.Fatal("RecoverDue error = nil, want enqueue failure")
	}
	stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.NextCheckAt == nil || !stored.NextCheckAt.Equal(f.now) {
		t.Fatalf("next_check_at = %v, want immediate retry at %v", stored.NextCheckAt, f.now)
	}

	enqueuer.err = nil
	if err := f.svc.RecoverDue(context.Background(), 100); err != nil {
		t.Fatalf("RecoverDue retry: %v", err)
	}
	if len(enqueuer.immediate) != 1 || enqueuer.immediate[0].taskType != WechatPublicationReconcileTaskType {
		t.Fatalf("retry tasks = %#v", enqueuer.immediate)
	}
}

func TestPollReconciliationOutcomesDoNotOverwriteConcurrentBinding(t *testing.T) {
	tests := []struct {
		name     string
		response *appwechat.FreePublishGetResponse
		getError error
	}{
		{name: "provider error", getError: errors.New("provider unavailable")},
		{name: "unsupported", getError: &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "unauthorized"}},
		{name: "empty response"},
		{name: "still publishing", response: &appwechat.FreePublishGetResponse{PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusPublishing}},
		{name: "terminal failure", response: &appwechat.FreePublishGetResponse{PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusFailed}},
		{name: "success missing metadata", response: &appwechat.FreePublishGetResponse{PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusSucceeded}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
			publication := f.seedDrafted(t)
			publication.Status, publication.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
			if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
				t.Fatal(err)
			}
			if tt.response != nil {
				f.api.getResponses = []*appwechat.FreePublishGetResponse{tt.response}
			}
			f.api.getError = tt.getError
			var bindErr error
			f.api.onGet = func() {
				latest, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
				if err != nil {
					bindErr = err
					return
				}
				_, bindErr = f.svc.bindPublished(context.Background(), latest, publishedWechatArticle{
					ArticleID: "concurrent-poll-winner", URL: "https://mp.weixin.qq.com/s/concurrent-poll-winner", PublishedAt: f.now, Index: 1,
				}, model.WechatPublicationSourceAnbanAPI)
			}

			_, _ = f.svc.Poll(context.Background(), publication.ID)
			if bindErr != nil {
				t.Fatalf("concurrent bind: %v", bindErr)
			}
			stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != model.WechatPublicationStatusPublished || stored.ArticleID != "concurrent-poll-winner" || stored.ArticleURL != "https://mp.weixin.qq.com/s/concurrent-poll-winner" {
				t.Errorf("poll overwrote concurrent binding: %#v", stored)
			}
			binding, err := f.repo.WechatPublications().FindByArticleID(context.Background(), f.projectID, "concurrent-poll-winner")
			if err != nil || binding.ID != publication.ID {
				t.Errorf("binding=%#v err=%v", binding, err)
			}
			tracking, err := f.repo.WechatTrackings().FindByTaskID(context.Background(), f.taskID)
			if err != nil || tracking.ArticleID != "concurrent-poll-winner" {
				t.Errorf("tracking=%#v err=%v", tracking, err)
			}
		})
	}
}

func TestPollCASLossReturnsConcurrentPublishedWinner(t *testing.T) {
	tests := []struct {
		name     string
		response *appwechat.FreePublishGetResponse
		getError error
	}{
		{name: "provider error", getError: errors.New("provider unavailable")},
		{name: "empty response"},
		{
			name: "successful response binding",
			response: &appwechat.FreePublishGetResponse{
				PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusSucceeded, ArticleID: "stale-provider-result",
				ArticleDetail: appwechat.FreePublishArticleDetail{Count: 1, Items: []appwechat.FreePublishArticleItem{{Index: 1, ArticleURL: "https://mp.weixin.qq.com/s/stale-provider-result"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
			publication := f.seedDrafted(t)
			publication.Status, publication.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
			if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
				t.Fatal(err)
			}
			if tt.response != nil {
				f.api.getResponses = []*appwechat.FreePublishGetResponse{tt.response}
			}
			f.api.getError = tt.getError
			var bindErr error
			f.api.onGet = func() {
				latest, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
				if err != nil {
					bindErr = err
					return
				}
				_, bindErr = f.svc.bindPublished(context.Background(), latest, publishedWechatArticle{
					ArticleID: "concurrent-poll-winner", URL: "https://mp.weixin.qq.com/s/concurrent-poll-winner", PublishedAt: f.now, Index: 1,
				}, model.WechatPublicationSourceAnbanAPI)
			}

			got, err := f.svc.Poll(context.Background(), publication.ID)
			if bindErr != nil {
				t.Fatalf("concurrent bind: %v", bindErr)
			}
			if err != nil {
				t.Fatalf("Poll=%#v returned obsolete race error: %v (internal=%t)", got, err, errors.Is(err, errWechatPublicationVersionChanged))
			}
			if got.Status != model.WechatPublicationStatusPublished || got.ArticleID != "concurrent-poll-winner" || got.ArticleURL != "https://mp.weixin.qq.com/s/concurrent-poll-winner" {
				t.Fatalf("Poll=%#v, want durable published winner", got)
			}
		})
	}
}

func TestPollCASLossReturnsPublicErrorForDurableNonSuccessState(t *testing.T) {
	tests := []struct {
		name      string
		status    string
		wantErr   error
		lastError string
	}{
		{name: "terminal failure", status: model.WechatPublicationStatusPublishFailed, wantErr: ErrWechatPublicationConflict, lastError: "durable publication failure"},
		{name: "pending selection", status: model.WechatPublicationStatusNeedsSelection, wantErr: ErrWechatPublicationPending},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
			publication := f.seedDrafted(t)
			publication.Status, publication.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
			if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
				t.Fatal(err)
			}
			f.api.getError = errors.New("obsolete provider error")
			var updateErr error
			f.api.onGet = func() {
				latest, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
				if err != nil {
					updateErr = err
					return
				}
				expectedStatus, expectedUpdatedAt := latest.Status, latest.UpdatedAt
				latest.Status, latest.LastError = tt.status, tt.lastError
				var won bool
				won, updateErr = f.repo.WechatPublications().UpdateReconciliation(context.Background(), latest, expectedStatus, expectedUpdatedAt)
				if updateErr == nil && !won {
					updateErr = errors.New("concurrent lifecycle update did not win")
				}
			}

			got, err := f.svc.Poll(context.Background(), publication.ID)
			if updateErr != nil {
				t.Fatal(updateErr)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Poll err=%v, want %v", err, tt.wantErr)
			}
			if err != tt.wantErr {
				t.Fatalf("Poll returned non-canonical lifecycle error: %v", err)
			}
			if got.Status != tt.status || got.LastError != tt.lastError {
				t.Fatalf("Poll=%#v, want durable status %q", got, tt.status)
			}
		})
	}
}

func TestPollSuccessBindsPublicationAndCreatesTracking(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	enqueuer := &recordingWechatPublicationEnqueuer{}
	f.svc.SetEnqueuer(enqueuer)
	p := f.seedDrafted(t)
	p.Status, p.PublishID, p.MsgDataID = model.WechatPublicationStatusPublishing, "publish-1", "msg-data-1"
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.getResponses = []*appwechat.FreePublishGetResponse{{
		PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusSucceeded, ArticleID: "article-1",
		ArticleDetail: appwechat.FreePublishArticleDetail{Count: 1, Items: []appwechat.FreePublishArticleItem{{Index: 1, ArticleURL: "https://mp.weixin.qq.com/s/article-1"}}},
	}}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "article-1", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/article-1"}}},
	}}}

	got, err := f.svc.Poll(context.Background(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != model.WechatPublicationStatusPublished || got.ArticleID != "article-1" || got.MsgID != "msg-data-1_1" || got.Source != model.WechatPublicationSourceAnbanAPI || got.ArticleIndex != 1 {
		t.Fatalf("published = %#v", got)
	}
	tracking, err := f.repo.WechatTrackings().FindByTaskID(context.Background(), f.taskID)
	if err != nil || tracking.PublicationID != got.ID || tracking.Source != model.WechatPublicationSourceAnbanAPI || tracking.ArticleID != "article-1" || tracking.MsgDataID != "msg-data-1" || tracking.MsgID != "msg-data-1_1" || tracking.ArticleURL != got.ArticleURL || !tracking.PublishedAt.Equal(*got.PublishedAt) || !tracking.ExpiresAt.Equal(got.PublishedAt.Add(model.WechatTrackingWindow)) || tracking.NextFetchAt == nil {
		t.Fatalf("tracking = %#v err=%v", tracking, err)
	}
	if len(enqueuer.immediate) != 1 || enqueuer.immediate[0].taskType != WechatCaptureMetricsTaskType || enqueuer.immediate[0].payload["tracking_id"] != tracking.ID {
		t.Fatalf("initial analytics task = %#v", enqueuer.immediate)
	}
}

func TestPollSuccessRequiresAuthoritativeBatchTimestamp(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	providerPublishedAt := time.Date(2026, 8, 31, 15, 55, 0, 0, time.UTC)

	t.Run("uses provider update time across Shanghai midnight", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		f.now = time.Date(2026, 8, 31, 16, 5, 0, 0, time.UTC)
		p := f.seedDrafted(t)
		p.Status, p.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
		if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		f.api.getResponses = []*appwechat.FreePublishGetResponse{{
			PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusSucceeded, ArticleID: "article-1",
			ArticleDetail: appwechat.FreePublishArticleDetail{Items: []appwechat.FreePublishArticleItem{{Index: 1, ArticleURL: "https://mp.weixin.qq.com/s/article-1"}}},
		}}
		f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
			ArticleID: "article-1", UpdateTime: providerPublishedAt.Unix(),
			Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/article-1"}}},
		}}}

		got, err := f.svc.Poll(context.Background(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.PublishedAt == nil || !got.PublishedAt.Equal(providerPublishedAt) {
			t.Fatalf("published_at = %v, want authoritative %v", got.PublishedAt, providerPublishedAt)
		}
		if got.PublishedAt.In(shanghai).Format("2006-01-02") != "2026-08-31" || f.now.In(shanghai).Format("2006-01-02") != "2026-09-01" {
			t.Fatalf("Shanghai boundary lost: published=%s poll=%s", got.PublishedAt.In(shanghai), f.now.In(shanghai))
		}
	})

	t.Run("remains pending until article is visible in batch list", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		p := f.seedDrafted(t)
		p.Status, p.PublishID = model.WechatPublicationStatusPublishing, "publish-1"
		if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
		f.api.getResponses = []*appwechat.FreePublishGetResponse{{
			PublishID: "publish-1", PublishStatus: appwechat.FreePublishStatusSucceeded, ArticleID: "article-not-visible",
			ArticleDetail: appwechat.FreePublishArticleDetail{Items: []appwechat.FreePublishArticleItem{{Index: 1, ArticleURL: "https://mp.weixin.qq.com/s/article-not-visible"}}},
		}}
		f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}

		got, err := f.svc.Poll(context.Background(), p.ID)
		if !errors.Is(err, ErrWechatPublicationPending) {
			t.Fatalf("Poll error = %v, want pending", err)
		}
		if got.Status != model.WechatPublicationStatusPublishing || got.PublishedAt != nil || got.NextCheckAt == nil {
			t.Fatalf("premature bind = %#v", got)
		}
	})
}

func TestPublishedArticlesRejectsMissingUpdateTime(t *testing.T) {
	response := &appwechat.FreePublishBatchGetResponse{Items: []appwechat.FreePublishBatchItem{
		{ArticleID: "missing-time", UpdateTime: 0, Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/missing"}}}},
		{ArticleID: "negative-time", UpdateTime: -1, Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/negative"}}}},
		{ArticleID: "valid", UpdateTime: 1_700_000_000, Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/valid"}}}},
	}}

	articles := publishedArticles(response)
	if len(articles) != 1 || articles[0].ArticleID != "valid" || !articles[0].PublishedAt.Equal(time.Unix(1_700_000_000, 0)) {
		t.Fatalf("publishedArticles = %#v, want only valid timestamp", articles)
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

func TestDisappearedManualDraftBecomesTerminalAfterReconciliationDeadline(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}

	pending, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
	if !errors.Is(err, ErrWechatPublicationPending) || pending.Status != model.WechatPublicationStatusPublishSubmitting || hasWechatSubmissionEvidence(pending) {
		t.Fatalf("Publish = %#v err=%v", pending, err)
	}
	f.now = publication.DraftCreatedAt.Add(73 * time.Hour)
	project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || stored.Status != model.WechatPublicationStatusPublishFailed || stored.NextCheckAt != nil {
		t.Fatalf("stored = %#v err=%v", stored, err)
	}
}

func TestExpiredPublishPreflightBecomesTerminalImmediately(t *testing.T) {
	t.Run("missing draft", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeManual)
		publication := f.seedDrafted(t)
		oldDraftCreatedAt := f.now.Add(-73 * time.Hour)
		publication.DraftCreatedAt = &oldDraftCreatedAt
		if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
			t.Fatal(err)
		}
		f.api.draftListResponse = &appwechat.DraftBatchGetResponse{}

		got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
		if err != nil || got.Status != model.WechatPublicationStatusPublishFailed || got.NextCheckAt != nil {
			t.Fatalf("Publish = %#v err=%v", got, err)
		}
	})

	t.Run("existing submission evidence", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeManual)
		publication := f.seedDrafted(t)
		oldSubmission := f.now.Add(-73 * time.Hour)
		publication.SubmitAttemptedAt = &oldSubmission
		if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
			t.Fatal(err)
		}

		got, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
		if err != nil || got.Status != model.WechatPublicationStatusPublishFailed || got.NextCheckAt != nil {
			t.Fatalf("Publish = %#v err=%v", got, err)
		}
		if f.api.submitCalls != 0 {
			t.Fatalf("SubmitFreePublish calls = %d, want 0", f.api.submitCalls)
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

func TestManualReconcileEvaluatesExactUniquenessAcrossAllPages(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	p := f.seedDrafted(t)
	item := func(id string) appwechat.FreePublishBatchItem {
		return appwechat.FreePublishBatchItem{ArticleID: id, UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: p.DraftTitle, Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/" + id}}}}
	}
	f.api.publishedListResponses = map[int64]*appwechat.FreePublishBatchGetResponse{
		0: {TotalCount: 2, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{item("exact-1")}},
		1: {TotalCount: 2, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{item("exact-2")}},
	}

	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if got.Status != model.WechatPublicationStatusNeedsSelection || got.ArticleID != "" || f.api.publishedListCalls != 2 {
		t.Fatalf("cross-page reconcile=%#v calls=%d", got, f.api.publishedListCalls)
	}
}

func TestEvidencedSelectionReturnsToSubmittingAndExpiresWithoutResubmit(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	publication := f.seedDrafted(t)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	f.api.submitError = errors.New("connection reset after publish request")

	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); err == nil {
		t.Fatal("Publish error = nil, want ambiguous submission")
	}
	submitted, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || submitted.Status != model.WechatPublicationStatusPublishSubmitting || submitted.SubmitAttemptedAt == nil {
		t.Fatalf("ambiguous submission = %#v err=%v", submitted, err)
	}
	submitAttemptedAt := *submitted.SubmitAttemptedAt

	project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	f.api.submitError = nil
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{
		TotalCount: 1,
		ItemCount:  1,
		Items: []appwechat.FreePublishBatchItem{{
			ArticleID:  "weak-candidate",
			UpdateTime: f.now.Unix(),
			Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
				Title: publication.DraftTitle, Digest: "different", ThumbMediaID: "different", Content: "<p>different</p>", URL: "https://mp.weixin.qq.com/s/weak-candidate",
			}}},
		}},
	}
	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("candidate reconciliation: %v", err)
	}
	selected, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || selected.Status != model.WechatPublicationStatusNeedsSelection {
		t.Fatalf("candidate state = %#v err=%v", selected, err)
	}

	f.now = f.now.Add(time.Hour)
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}
	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("empty candidate reconciliation: %v", err)
	}
	reconciling, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || reconciling.Status != model.WechatPublicationStatusPublishSubmitting || reconciling.NextCheckAt == nil {
		t.Fatalf("reconciling state = %#v err=%v", reconciling, err)
	}

	f.now = submitAttemptedAt.Add(72*time.Hour + time.Second)
	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("expired reconciliation: %v", err)
	}
	expired, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
	if err != nil || expired.Status != model.WechatPublicationStatusPublishFailed || expired.NextCheckAt != nil {
		t.Fatalf("expired state = %#v err=%v", expired, err)
	}
	if f.api.submitCalls != 1 {
		t.Fatalf("SubmitFreePublish calls = %d, want exactly 1", f.api.submitCalls)
	}
}

func TestReconcilePreservesAPIOriginAndCompositeMsgID(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	p.Status, p.Source, p.MsgDataID = model.WechatPublicationStatusPublishSubmitting, model.WechatPublicationSourceAnbanAPI, "msg-data-1"
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "api-recovered", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/api-recovered"}}},
	}}}

	if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
		t.Fatal(err)
	}
	got, _ := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if got.Source != model.WechatPublicationSourceAnbanAPI || got.MsgID != "msg-data-1_1" {
		t.Fatalf("reconciled API publication=%#v", got)
	}
}

func TestPollRecoversStalePublishSubmittingWithoutResubmit(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	p := f.seedDrafted(t)
	p.Status = model.WechatPublicationStatusPublishSubmitting
	p.NextCheckAt = nextWechatManualCheck(f.now, *p.DraftCreatedAt)
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "recovered", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/recovered"}}},
	}}}

	got, err := f.svc.Poll(context.Background(), p.ID)
	if err != nil || got.Status != model.WechatPublicationStatusPublished || f.api.submitCalls != 0 {
		t.Fatalf("Poll=%#v err=%v submitCalls=%d", got, err, f.api.submitCalls)
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

func TestDisabledProjectReconcilesOnlyRowsWithSubmissionEvidence(t *testing.T) {
	t.Run("scheduled project reconciliation", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		evidenced := f.seedDrafted(t)
		evidenced.Status = model.WechatPublicationStatusPublishSubmitting
		evidenced.SubmitAttemptedAt = &f.now
		due := f.now.Add(-time.Minute)
		evidenced.NextCheckAt = &due
		if err := f.repo.WechatPublications().Update(context.Background(), evidenced); err != nil {
			t.Fatal(err)
		}

		ordinaryTaskID := uuid.NewString()
		if err := f.repo.Tasks().Create(context.Background(), &model.Task{ID: ordinaryTaskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
			t.Fatal(err)
		}
		ordinary := &model.WechatPublication{ID: uuid.NewString(), TaskID: ordinaryTaskID, UserID: f.userID, ProjectID: f.projectID, DraftMediaID: "ordinary-draft", Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusDrafted, DraftCreatedAt: &due, NextCheckAt: &due}
		if err := f.repo.WechatPublications().Create(context.Background(), ordinary); err != nil {
			t.Fatal(err)
		}
		project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
		if err != nil {
			t.Fatal(err)
		}
		project.Config.WechatPublishMode = model.WechatPublishModeDisabled
		if err := f.repo.Projects().Update(context.Background(), project); err != nil {
			t.Fatal(err)
		}
		f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
			ArticleID: "disabled-recovered", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/disabled-recovered"}}},
		}}}

		if err := f.svc.ReconcileProject(context.Background(), f.projectID); err != nil {
			t.Fatal(err)
		}
		gotEvidence, _ := f.repo.WechatPublications().FindByID(context.Background(), evidenced.ID)
		gotOrdinary, _ := f.repo.WechatPublications().FindByID(context.Background(), ordinary.ID)
		if gotEvidence.Status != model.WechatPublicationStatusPublished || gotEvidence.ArticleID != "disabled-recovered" {
			t.Fatalf("evidenced row was not finalized: %#v", gotEvidence)
		}
		if gotOrdinary.Status != model.WechatPublicationStatusDrafted || gotOrdinary.NextCheckAt != nil {
			t.Fatalf("ordinary disabled row kept recovery work: %#v", gotOrdinary)
		}
		if f.api.publishedListCalls != 1 {
			t.Fatalf("published list calls = %d, want one batched reconciliation", f.api.publishedListCalls)
		}
	})

	t.Run("immediate reconciliation", func(t *testing.T) {
		f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
		publication := f.seedDrafted(t)
		publication.Status, publication.PublishID = model.WechatPublicationStatusPublishSubmitting, "publish-1"
		if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
			t.Fatal(err)
		}
		project, _ := f.repo.Projects().FindByID(context.Background(), f.projectID)
		project.Config.WechatPublishMode = model.WechatPublishModeDisabled
		if err := f.repo.Projects().Update(context.Background(), project); err != nil {
			t.Fatal(err)
		}
		f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{}

		if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
			t.Fatalf("evidenced disabled reconciliation: %v", err)
		}
		if f.api.publishedListCalls != 1 {
			t.Fatalf("published list calls = %d, want 1", f.api.publishedListCalls)
		}
	})
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

func TestReconcileDoesNotOverwriteConcurrentPublishClaim(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
	drafted := f.seedDrafted(t)
	findEntered, releaseFind := make(chan struct{}), make(chan struct{})
	publications := &pausingFindPendingWechatPublicationRepository{
		WechatPublicationRepository: f.repo.WechatPublications(),
		entered:                     findEntered,
		release:                     releaseFind,
	}
	f.overridePublications(publications)

	submitEntered, releaseSubmit := make(chan struct{}), make(chan struct{})
	var releaseFindOnce, releaseSubmitOnce, submitOnce sync.Once
	releaseReconcile := func() { releaseFindOnce.Do(func() { close(releaseFind) }) }
	releaseProvider := func() { releaseSubmitOnce.Do(func() { close(releaseSubmit) }) }
	t.Cleanup(func() {
		releaseReconcile()
		releaseProvider()
	})
	f.api.onSubmit = func() {
		submitOnce.Do(func() { close(submitEntered) })
		<-releaseSubmit
	}

	reconcileDone := make(chan error, 1)
	go func() {
		reconcileDone <- f.svc.Reconcile(context.Background(), f.userID, f.taskID)
	}()
	<-findEntered

	type publishResult struct {
		publication *model.WechatPublication
		err         error
	}
	publishDone := make(chan publishResult, 1)
	go func() {
		publication, err := f.svc.Publish(context.Background(), f.userID, f.taskID)
		publishDone <- publishResult{publication: publication, err: err}
	}()
	<-submitEntered

	claimed, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Status != model.WechatPublicationStatusPublishSubmitting || claimed.ClaimToken == "" || claimed.ClaimedAt == nil || claimed.SubmitAttemptedAt == nil {
		t.Fatalf("persisted publish claim=%#v", claimed)
	}

	releaseReconcile()
	if err := <-reconcileDone; err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	afterReconcile, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	var duplicateEligible int64
	if err := f.db.Model(&model.WechatPublication{}).
		Where("id = ? AND status = ? AND draft_media_id <> ''", drafted.ID, model.WechatPublicationStatusDrafted).
		Count(&duplicateEligible).Error; err != nil {
		t.Fatal(err)
	}

	releaseProvider()
	published := <-publishDone
	if !errors.Is(published.err, ErrWechatPublicationPending) {
		t.Errorf("Publish err=%v, want pending response recovery", published.err)
	}
	if afterReconcile.Status != model.WechatPublicationStatusPublishSubmitting ||
		afterReconcile.ClaimToken != claimed.ClaimToken || afterReconcile.ClaimedAt == nil || !afterReconcile.ClaimedAt.Equal(*claimed.ClaimedAt) ||
		afterReconcile.SubmitAttemptedAt == nil || !afterReconcile.SubmitAttemptedAt.Equal(*claimed.SubmitAttemptedAt) ||
		afterReconcile.Source != model.WechatPublicationSourceAnbanAPI {
		t.Errorf("reconciliation overwrote concurrent publish claim: before=%#v after=%#v", claimed, afterReconcile)
	}
	if source := reconciledWechatPublicationSource(afterReconcile); source != model.WechatPublicationSourceAnbanAPI {
		t.Errorf("reconciliation source=%q, want %q", source, model.WechatPublicationSourceAnbanAPI)
	}
	if duplicateEligible != 0 {
		t.Errorf("publication became eligible for duplicate submission")
	}

	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "recovered-after-race", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
			Title: drafted.DraftTitle, Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/recovered-after-race",
		}}},
	}}}
	project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.reconcileProject(context.Background(), project); err != nil {
		t.Fatalf("later response recovery: %v", err)
	}
	recovered, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != model.WechatPublicationStatusPublished || recovered.ArticleID != "recovered-after-race" || recovered.Source != model.WechatPublicationSourceAnbanAPI || recovered.SubmitAttemptedAt == nil {
		t.Errorf("recovered publication=%#v", recovered)
	}
	f.api.mu.Lock()
	submitCalls := f.api.submitCalls
	f.api.mu.Unlock()
	if submitCalls != 1 {
		t.Errorf("external submit calls=%d, want 1", submitCalls)
	}
}

func TestReconciliationOutcomesRequireLoadedPublicationVersion(t *testing.T) {
	tests := []struct {
		name      string
		response  func(*publicationFixture, *model.WechatPublication) *appwechat.FreePublishBatchGetResponse
		listError error
		articleID string
	}{
		{
			name: "candidate",
			response: func(f *publicationFixture, publication *model.WechatPublication) *appwechat.FreePublishBatchGetResponse {
				return &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
					ArticleID: "stale-candidate", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
						Title: publication.DraftTitle, Content: "<p>different</p>", URL: "https://mp.weixin.qq.com/s/stale-candidate",
					}}},
				}}}
			},
		},
		{
			name: "published binding",
			response: func(f *publicationFixture, _ *model.WechatPublication) *appwechat.FreePublishBatchGetResponse {
				return &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
					ArticleID: "stale-exact", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{
						Content: `<section class="body"><p>Hello <strong>world</strong></p></section>`, URL: "https://mp.weixin.qq.com/s/stale-exact",
					}}},
				}}}
			},
			articleID: "stale-exact",
		},
		{
			name:      "unsupported",
			response:  func(*publicationFixture, *model.WechatPublication) *appwechat.FreePublishBatchGetResponse { return nil },
			listError: &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "unauthorized"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPublicationFixture(t, model.WechatPublishModeAPIConfirmed)
			publication := f.seedDrafted(t)
			f.api.publishedListResponse = tt.response(f, publication)
			f.api.publishedListError = tt.listError
			var claimErr error
			f.api.onPublishedList = func(call int) {
				if call != 1 {
					return
				}
				nextCheckAt := nextWechatManualCheck(f.now, *publication.DraftCreatedAt)
				var won bool
				won, claimErr = f.repo.WechatPublications().ClaimPublish(
					context.Background(), publication.ID, "newer-publish-claim", f.now, f.now.Add(-wechatPublicationClaimLease), nextCheckAt,
				)
				if claimErr == nil && !won {
					claimErr = errors.New("newer publish claim did not win")
				}
			}

			if err := f.svc.Reconcile(context.Background(), f.userID, f.taskID); err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if claimErr != nil {
				t.Fatal(claimErr)
			}
			stored, err := f.repo.WechatPublications().FindByID(context.Background(), publication.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != model.WechatPublicationStatusPublishSubmitting || stored.ClaimToken != "newer-publish-claim" || stored.ClaimedAt == nil || stored.SubmitAttemptedAt == nil || stored.Source != model.WechatPublicationSourceAnbanAPI {
				t.Errorf("newer publish state was overwritten: %#v", stored)
			}
			if tt.articleID != "" {
				if binding, err := f.repo.WechatPublications().FindByArticleID(context.Background(), f.projectID, tt.articleID); !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Errorf("stale binding survived CAS loss: binding=%#v err=%v", binding, err)
				}
				if tracking, err := f.repo.WechatTrackings().FindByTaskID(context.Background(), f.taskID); !errors.Is(err, gorm.ErrRecordNotFound) {
					t.Errorf("stale tracking survived CAS loss: tracking=%#v err=%v", tracking, err)
				}
			}
		})
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
	setPublicationCandidates(t, p, "owned")
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
	if f.api.publishedListCalls != 1 {
		t.Fatalf("selection must refetch each time, calls=%d", f.api.publishedListCalls)
	}
}

func TestSelectRejectsAccountArticleOutsideStoredCandidateSet(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	p := f.seedDrafted(t)
	p.Status = model.WechatPublicationStatusNeedsSelection
	p.Candidates = datatypes.JSON(`[{"article_id":"allowed","title":"Allowed"}]`)
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{Items: []appwechat.FreePublishBatchItem{
		{ArticleID: "allowed", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: "Allowed", URL: "https://mp.weixin.qq.com/s/allowed"}}}},
		{ArticleID: "not-offered", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{Title: "Other", URL: "https://mp.weixin.qq.com/s/not-offered"}}}},
	}}

	if _, err := f.svc.Select(context.Background(), f.userID, f.taskID, "not-offered"); !errors.Is(err, ErrWechatPublicationArticleNotFound) {
		t.Fatalf("Select outside candidate set err=%v", err)
	}
	stored, err := f.repo.WechatPublications().FindByTaskID(context.Background(), f.taskID)
	if err != nil || stored.Status != model.WechatPublicationStatusNeedsSelection || stored.ArticleID != "" {
		t.Fatalf("publication changed after invalid selection: %#v err=%v", stored, err)
	}
}

func TestSelectScansPublishedPagesUntilArticleIDFound(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	p := f.seedDrafted(t)
	p.Status = model.WechatPublicationStatusNeedsSelection
	setPublicationCandidates(t, p, "selected")
	if err := f.repo.WechatPublications().Update(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponses = map[int64]*appwechat.FreePublishBatchGetResponse{
		0: {TotalCount: 2, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{ArticleID: "other", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/other"}}}}}},
		1: {TotalCount: 2, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{ArticleID: "selected", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/selected"}}}}}},
	}

	got, err := f.svc.Select(context.Background(), f.userID, f.taskID, "selected")
	if err != nil || got.ArticleID != "selected" || f.api.publishedListCalls != 2 {
		t.Fatalf("Select=%#v err=%v calls=%d", got, err, f.api.publishedListCalls)
	}
}

func TestOverlappingSelectionOfSamePublicationIsIdempotent(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	publication := f.seedDrafted(t)
	publication.Status = model.WechatPublicationStatusNeedsSelection
	setPublicationCandidates(t, publication, "same-selection")
	if err := f.repo.WechatPublications().Update(context.Background(), publication); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "same-selection", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/same-selection"}}},
	}}}

	var concurrent *model.WechatPublication
	var concurrentErr error
	f.api.onPublishedList = func(call int) {
		if call == 1 {
			concurrent, concurrentErr = f.svc.Select(context.Background(), f.userID, f.taskID, "same-selection")
		}
	}

	got, err := f.svc.Select(context.Background(), f.userID, f.taskID, "same-selection")
	if concurrentErr != nil {
		t.Fatalf("concurrent Select: %v", concurrentErr)
	}
	if errors.Is(err, errWechatPublicationVersionChanged) {
		t.Fatalf("Select exposed internal version sentinel: %v", err)
	}
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if got.Status != model.WechatPublicationStatusPublished || concurrent == nil || concurrent.Status != model.WechatPublicationStatusPublished || got.ID != concurrent.ID || got.ArticleID != "same-selection" {
		t.Fatalf("selections=%#v and %#v, want the same durable publication", got, concurrent)
	}
}

func TestConcurrentSelectionHasOneAtomicArticleBindingWinner(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeManual)
	first := f.seedDrafted(t)
	first.Status = model.WechatPublicationStatusNeedsSelection
	setPublicationCandidates(t, first, "one-owner")
	if err := f.repo.WechatPublications().Update(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	secondTaskID := uuid.NewString()
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{ID: secondTaskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	created := f.now.Add(-time.Hour)
	second := &model.WechatPublication{ID: uuid.NewString(), TaskID: secondTaskID, UserID: f.userID, ProjectID: f.projectID, DraftMediaID: "draft-2", DraftContentFingerprint: first.DraftContentFingerprint, Source: model.WechatPublicationSourceAnbanAPI, Status: model.WechatPublicationStatusNeedsSelection, DraftCreatedAt: &created}
	setPublicationCandidates(t, second, "one-owner")
	if err := f.repo.WechatPublications().Create(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	f.api.publishedListResponse = &appwechat.FreePublishBatchGetResponse{TotalCount: 1, ItemCount: 1, Items: []appwechat.FreePublishBatchItem{{
		ArticleID: "one-owner", UpdateTime: f.now.Unix(), Content: appwechat.FreePublishContent{NewsItems: []appwechat.DraftArticle{{URL: "https://mp.weixin.qq.com/s/one-owner"}}},
	}}}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for _, taskID := range []string{f.taskID, secondTaskID} {
		go func(taskID string) {
			<-start
			_, err := f.svc.Select(context.Background(), f.userID, taskID, "one-owner")
			errs <- err
		}(taskID)
	}
	close(start)
	winners, conflicts := 0, 0
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			winners++
		case errors.Is(err, ErrWechatPublicationConflict):
			conflicts++
		default:
			t.Fatalf("unexpected selection error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 {
		t.Fatalf("selection results winners=%d conflicts=%d", winners, conflicts)
	}
	var bindings int64
	if err := f.db.Model(&model.WechatPublicationBinding{}).Where("project_id = ? AND article_id = ?", f.projectID, "one-owner").Count(&bindings).Error; err != nil || bindings != 1 {
		t.Fatalf("bindings=%d err=%v", bindings, err)
	}
}

func TestWechatPublicationOwnershipAndDisabledStateConflicts(t *testing.T) {
	f := newPublicationFixture(t, model.WechatPublishModeDisabled)
	f.seedDrafted(t)
	if _, err := f.svc.Get(context.Background(), uuid.NewString(), f.taskID); !errors.Is(err, ErrWechatPublicationForbidden) {
		t.Fatalf("Get foreign err=%v", err)
	}
	if _, err := f.svc.Publish(context.Background(), f.userID, f.taskID); !errors.Is(err, ErrWechatPublicationModeConflict) {
		t.Fatalf("disabled publish err=%v", err)
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
		{71 * time.Hour, time.Hour, true}, {72 * time.Hour, 0, false},
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
