package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/seednote"
	"github.com/anbanai/anban-creator/server/service"
)

type projectReadiness bool

func (r projectReadiness) Ready() bool { return bool(r) }

func TestProjectRequestMapsRequirePublishApproval(t *testing.T) {
	req := projectRequest{
		Platform:               "article",
		Name:                   "Article",
		EnablePublishing:       true,
		RequirePublishApproval: true,
	}

	project := req.toProject()
	if !project.Config.EnablePublishing {
		t.Fatal("EnablePublishing was not mapped")
	}
	if !project.Config.RequirePublishApproval {
		t.Fatal("RequirePublishApproval was not mapped")
	}
	if got := req.getFieldValue("require_publish_approval"); got != "true" {
		t.Fatalf("getFieldValue(require_publish_approval) = %q, want true", got)
	}
}

type projectReferenceStore struct {
	*fakeStorageProvider
	signedKeys  []string
	downloadErr error
}

type hookedProjectRepository struct {
	repository.ProjectRepository
	findCalls  int
	casCalls   int
	beforeFind func(int)
	beforeCAS  func(int)
}

func (r *hookedProjectRepository) FindByID(ctx context.Context, id string) (*model.Project, error) {
	r.findCalls++
	if r.beforeFind != nil {
		r.beforeFind(r.findCalls)
	}
	return r.ProjectRepository.FindByID(ctx, id)
}

func (r *hookedProjectRepository) UpdateIfReferenceImageAssetID(ctx context.Context, project *model.Project, expectedID string) (bool, error) {
	r.casCalls++
	if r.beforeCAS != nil {
		r.beforeCAS(r.casCalls)
	}
	return r.ProjectRepository.UpdateIfReferenceImageAssetID(ctx, project, expectedID)
}

type projectHandlerRepositoryOverride struct {
	repository.Repository
	projects repository.ProjectRepository
}

func (r projectHandlerRepositoryOverride) Projects() repository.ProjectRepository {
	return r.projects
}

func (s *projectReferenceStore) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	s.signedKeys = append(s.signedKeys, key)
	if s.downloadErr != nil {
		return "", s.downloadErr
	}
	return "https://signed.example/" + key, nil
}

func setupProjectHandlerTest(t *testing.T) (*fiber.App, repository.Repository, *projectReferenceStore) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "projects.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}

	repo := repository.New(db)
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(repo.UploadSessions())}
	return newProjectHandlerTestApp(repo, store), repo, store
}

func newProjectHandlerTestApp(repo repository.Repository, store *projectReferenceStore) *fiber.App {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	projectSvc := service.NewProjectService(repo, &logger)
	h := NewProjectHandler(projectSvc, &logger)
	h.SetStore(store)
	h.SetUploadRepository(repo)
	h.SetReferenceAssetService(service.NewReferenceAssetService(repo, store, time.Now))

	app := fiber.New()
	injectUser := func(c fiber.Ctx) error {
		if uid := c.Get("X-User-ID"); uid != "" {
			c.Locals("user_id", uid)
		}
		return c.Next()
	}
	app.Delete("/api/v1/projects/:id", injectUser, h.Delete)
	app.Get("/api/v1/projects", injectUser, h.List)
	app.Get("/api/v1/projects/:id", injectUser, h.Get)
	app.Post("/api/v1/projects", injectUser, h.Create)
	app.Put("/api/v1/projects/:id", injectUser, h.Update)
	return app
}

func setupProjectDeleteHandlerTest(t *testing.T) (*fiber.App, repository.Repository) {
	t.Helper()
	app, repo, _ := setupProjectHandlerTest(t)
	return app, repo
}

func TestProjectHandler_SeednoteLoginStatusUnavailableWhenSidecarNotReady(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewProjectHandler(nil, &logger)
	h.SetSeednoteClient(seednote.NewClient("http://127.0.0.1:1", time.Second))
	h.SetSeednoteReadiness(projectReadiness(false))

	app := fiber.New()
	app.Get("/seednote/login-status", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.SeednoteLoginStatus(c)
	})

	req := httptest.NewRequest("GET", "/seednote/login-status", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var payload struct {
		Data struct {
			Available bool   `json:"available"`
			LoggedIn  bool   `json:"logged_in"`
			Message   string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response %q: %v", string(body), err)
	}
	if payload.Data.Available || payload.Data.LoggedIn || !strings.Contains(payload.Data.Message, "后台连接") {
		t.Fatalf("unexpected response: %s", string(body))
	}
}

func TestProjectHandler_FetchProfileReturnsUnavailableWhenSeednoteNotReady(t *testing.T) {
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewProjectHandler(nil, &logger)
	h.SetSeednoteClient(seednote.NewClient("http://127.0.0.1:1", time.Second))
	h.SetSeednoteReadiness(projectReadiness(false))
	if h.seednoteReady == nil || h.seednoteReady.Ready() {
		t.Fatalf("seednote readiness not installed or unexpectedly ready: %#v", h.seednoteReady)
	}

	app := fiber.New()
	app.Post("/projects/fetch-profile", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.FetchProfile(c)
	})

	body := strings.NewReader(`{"platform":"seednote","profile_url":"https://www.xiaohongshu.com/user/profile/abc?xsec_token=test"}`)
	req := httptest.NewRequest(http.MethodPost, "/projects/fetch-profile", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", resp.StatusCode, string(got))
	}
	if !strings.Contains(string(got), "后台连接") {
		t.Fatalf("body = %s, want background connection hint", string(got))
	}
}

func TestProjectCreateFinalizesReferenceSessionAndPersistsAssetID(t *testing.T) {
	app, repo, store := setupProjectHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	refID := "reference-upload"
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: refID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/" + userID + "/" + refID + "/ref.png",
		FileName:   "ref.png", ContentType: "image/png", Size: 456,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session: %v", err)
	}

	resp := doRequest(t, app, "POST", "/api/v1/projects", userID, map[string]any{
		"platform": model.PlatformArticle,
		"name":     "公众号项目",
		"reference_image": map[string]any{
			"upload_session_id": refID,
		},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	responseBody := decodeBody(t, resp)
	responseJSON, _ := json.Marshal(responseBody)
	if strings.Contains(string(responseJSON), "reference_image_url") {
		t.Fatalf("response exposed legacy reference URL: %s", responseJSON)
	}
	projects, err := repo.Projects().ListByUserID(ctx, userID, repository.ProjectListOptions{})
	if err != nil || len(projects) != 1 {
		t.Fatalf("list persisted project: projects=%#v err=%v", projects, err)
	}
	if projects[0].ReferenceImageAssetID != refID {
		t.Fatalf("reference asset id = %q, want %q", projects[0].ReferenceImageAssetID, refID)
	}
	assertFinalizedAsset(t, repo, refID, "assets/users/"+userID+"/"+refID+"/ref.png")
	if len(store.signedKeys) != 1 || store.signedKeys[0] != "assets/users/"+userID+"/"+refID+"/ref.png" {
		t.Fatalf("signed keys = %#v", store.signedKeys)
	}
}

func TestProjectRejectsLegacyReferenceImageURL(t *testing.T) {
	legacyKeys := []string{"reference_image_url", "REFERENCE_IMAGE_URL", "Reference_Image_Url"}
	legacyValues := []string{`null`, `""`, `"https://example.com/ref.png"`}
	for _, key := range legacyKeys {
		for _, value := range legacyValues {
			t.Run(key+value, func(t *testing.T) {
				app, repo, _ := setupProjectHandlerTest(t)
				body := strings.NewReader(`{"platform":"article","name":"brand","` + key + `":` + value + `}`)
				req := httptest.NewRequest(http.MethodPost, "/api/v1/projects", body)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-User-ID", uuid.NewString())
				resp, err := app.Test(req)
				if err != nil {
					t.Fatalf("request: %v", err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != fiber.StatusBadRequest {
					t.Fatalf("status = %d, want 400; body=%v", resp.StatusCode, decodeBody(t, resp))
				}
				projects, err := repo.Projects().ListByUserID(t.Context(), req.Header.Get("X-User-ID"), repository.ProjectListOptions{})
				if err != nil || len(projects) != 0 {
					t.Fatalf("legacy request persisted projects=%#v err=%v", projects, err)
				}
			})
		}
	}
}

func TestProjectUpdateReferenceNullClearsAndOmissionPreserves(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	projectID := uuid.NewString()
	asset := &model.Asset{
		ID: "asset-1", UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StorageKey: "assets/users/" + userID + "/asset-1/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 3, ETag: "etag-1",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "brand",
		ReferenceImageAssetID: asset.ID, Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "renamed"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("omit status = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	got, _ := repo.Projects().FindByID(t.Context(), projectID)
	if got.ReferenceImageAssetID != asset.ID {
		t.Fatalf("omission changed reference to %q", got.ReferenceImageAssetID)
	}

	resp = doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"reference_image": nil})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("clear status = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	got, _ = repo.Projects().FindByID(t.Context(), projectID)
	if got.ReferenceImageAssetID != "" {
		t.Fatalf("explicit null left reference %q", got.ReferenceImageAssetID)
	}
}

func TestProjectUpdateReferenceSelectionReplacesAndFinalizes(t *testing.T) {
	app, repo, _ := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	projectID := uuid.NewString()
	sessionID := "replacement-session"
	if err := repo.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "brand",
		ReferenceImageAssetID: "old-asset", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID: sessionID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StagingKey: "uploads/pending/" + userID + "/" + sessionID + "/ref.jpg",
		FileName:   "ref.jpg", ContentType: "image/jpeg", Size: 12,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{
		"reference_image": map[string]any{"upload_session_id": sessionID},
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	got, _ := repo.Projects().FindByID(t.Context(), projectID)
	if got.ReferenceImageAssetID != sessionID {
		t.Fatalf("reference id = %q, want %q", got.ReferenceImageAssetID, sessionID)
	}
}

func TestProjectReferenceSelectionMapsErrorsBeforePersistence(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name       string
		session    *model.UploadSession
		asset      *model.Asset
		selection  map[string]any
		storeError error
		wantStatus int
	}{
		{name: "missing asset is opaque", selection: map[string]any{"asset_id": "missing"}, wantStatus: fiber.StatusForbidden},
		{name: "foreign asset is opaque", asset: &model.Asset{ID: "foreign", UserID: "other", Purpose: service.DirectUploadPurposeProjectReference, StorageKey: "assets/users/other/foreign/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, ETag: "etag"}, selection: map[string]any{"asset_id": "foreign"}, wantStatus: fiber.StatusForbidden},
		{name: "asset purpose", asset: &model.Asset{ID: "purpose", Purpose: service.DirectUploadPurposeTaskReference, StorageKey: "assets/users/user/purpose/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, ETag: "etag"}, selection: map[string]any{"asset_id": "purpose"}, wantStatus: fiber.StatusBadRequest},
		{name: "asset metadata", asset: &model.Asset{ID: "metadata", Purpose: service.DirectUploadPurposeProjectReference, StorageKey: "assets/users/user/metadata/ref.txt", FileName: "ref.txt", ContentType: "text/plain", Size: 3, ETag: "etag"}, selection: map[string]any{"asset_id": "metadata"}, wantStatus: fiber.StatusBadRequest},
		{name: "expired session", session: &model.UploadSession{ID: "expired", Purpose: service.DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user/expired/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, Status: model.UploadSessionPending, ExpiresAt: now.Add(-time.Minute)}, selection: map[string]any{"upload_session_id": "expired"}, wantStatus: fiber.StatusGone},
		{name: "conflicting session", session: &model.UploadSession{ID: "conflict", Purpose: service.DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user/conflict/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, Status: model.UploadSessionFinalizing, ExpiresAt: now.Add(time.Hour), FinalizationToken: "other", FinalizationClaimedAt: &now}, selection: map[string]any{"upload_session_id": "conflict"}, wantStatus: fiber.StatusConflict},
		{name: "storage dependency", session: &model.UploadSession{ID: "storage", Purpose: service.DirectUploadPurposeProjectReference, StagingKey: "uploads/pending/user/storage/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, Status: model.UploadSessionPending, ExpiresAt: now.Add(time.Hour)}, selection: map[string]any{"upload_session_id": "storage"}, storeError: errors.New("storage unavailable"), wantStatus: fiber.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo, store := setupProjectHandlerTest(t)
			userID := "user"
			if tt.asset != nil {
				asset := *tt.asset
				if asset.UserID == "" {
					asset.UserID = userID
				}
				if err := repo.Assets().Create(t.Context(), &asset); err != nil {
					t.Fatal(err)
				}
			}
			if tt.session != nil {
				session := *tt.session
				session.UserID = userID
				if err := repo.UploadSessions().Create(t.Context(), &session); err != nil {
					t.Fatal(err)
				}
			}
			store.statErr = tt.storeError
			resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
				"platform": model.PlatformArticle, "name": "brand", "reference_image": tt.selection,
			})
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d want %d body=%v", resp.StatusCode, tt.wantStatus, decodeBody(t, resp))
			}
			projects, err := repo.Projects().ListByUserID(t.Context(), userID, repository.ProjectListOptions{})
			if err != nil || len(projects) != 0 {
				t.Fatalf("failed selection persisted project=%#v err=%v", projects, err)
			}
		})
	}
}

func TestProjectReferenceViewsAreSignedForCreateUpdateGetAndList(t *testing.T) {
	app, repo, store := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	asset := &model.Asset{
		ID: "asset-view", UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StorageKey: "assets/users/" + userID + "/asset-view/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 3, ETag: "etag",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	created := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
		"platform": model.PlatformArticle, "name": "brand", "reference_image": map[string]any{"asset_id": asset.ID},
	})
	if created.StatusCode != fiber.StatusOK {
		t.Fatalf("create status=%d body=%v", created.StatusCode, decodeBody(t, created))
	}
	projects, _ := repo.Projects().ListByUserID(t.Context(), userID, repository.ProjectListOptions{})
	projectID := projects[0].ID
	for _, request := range []struct {
		method, path string
		body         any
	}{
		{http.MethodPut, "/api/v1/projects/" + projectID, map[string]any{"name": "renamed"}},
		{http.MethodGet, "/api/v1/projects/" + projectID, nil},
		{http.MethodGet, "/api/v1/projects", nil},
	} {
		resp := doRequest(t, app, request.method, request.path, userID, request.body)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("%s %s status=%d body=%v", request.method, request.path, resp.StatusCode, decodeBody(t, resp))
		}
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"asset_id":"asset-view"`) || !strings.Contains(string(body), `"download_url":"https://signed.example/`) {
			t.Fatalf("%s %s missing asset view: %s", request.method, request.path, body)
		}
		if strings.Contains(string(body), "reference_image_url") || strings.Contains(string(body), `"storage_key"`) {
			t.Fatalf("%s %s leaked persisted reference detail: %s", request.method, request.path, body)
		}
	}
	persisted, _ := repo.Projects().FindByID(t.Context(), projectID)
	if persisted.ReferenceImage != nil {
		t.Fatalf("transient view was persisted: %#v", persisted.ReferenceImage)
	}
	if len(store.signedKeys) != 4 {
		t.Fatalf("signed keys=%#v, want one per response", store.signedKeys)
	}
}

func TestProjectCreateSigningFailureDoesNotPersist(t *testing.T) {
	app, repo, store := setupProjectHandlerTest(t)
	userID := uuid.NewString()
	asset := &model.Asset{
		ID: "asset-sign-failure", UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
		StorageKey: "assets/users/" + userID + "/asset-sign-failure/ref.png", FileName: "ref.png",
		ContentType: "image/png", Size: 3, ETag: "etag",
	}
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	store.downloadErr = errors.New("signer unavailable")

	resp := doRequest(t, app, http.MethodPost, "/api/v1/projects", userID, map[string]any{
		"platform": model.PlatformArticle, "name": "must-not-persist",
		"reference_image": map[string]any{"asset_id": asset.ID},
	})
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status=%d want 503 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	projects, err := repo.Projects().ListByUserID(t.Context(), userID, repository.ProjectListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 0 {
		t.Fatalf("signing failure persisted projects: %#v", projects)
	}
}

func TestProjectUpdateSigningFailureDoesNotMutate(t *testing.T) {
	tests := []struct {
		name     string
		body     map[string]any
		targetID string
	}{
		{name: "omission presents current asset", body: map[string]any{"name": "after"}, targetID: "asset-current"},
		{name: "replacement presents selected asset", body: map[string]any{"name": "after", "reference_image": map[string]any{"asset_id": "asset-replacement"}}, targetID: "asset-replacement"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo, store := setupProjectHandlerTest(t)
			userID := uuid.NewString()
			projectID := uuid.NewString()
			for _, assetID := range []string{"asset-current", "asset-replacement"} {
				if err := repo.Assets().Create(t.Context(), &model.Asset{
					ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
					StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
					ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := repo.Projects().Create(t.Context(), &model.Project{
				ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "before",
				ReferenceImageAssetID: "asset-current", Status: model.ProjectStatusActive,
			}); err != nil {
				t.Fatal(err)
			}
			store.downloadErr = errors.New("signer unavailable")

			resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, tt.body)
			if resp.StatusCode != fiber.StatusServiceUnavailable {
				t.Fatalf("status=%d want 503 body=%v", resp.StatusCode, decodeBody(t, resp))
			}
			if len(store.signedKeys) != 1 || store.signedKeys[0] != "assets/users/"+userID+"/"+tt.targetID+"/ref.png" {
				t.Fatalf("signed keys=%#v, want target %q", store.signedKeys, tt.targetID)
			}
			persisted, err := repo.Projects().FindByID(t.Context(), projectID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Name != "before" || persisted.ReferenceImageAssetID != "asset-current" {
				t.Fatalf("signing failure mutated project: name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
			}
		})
	}
}

func TestProjectUpdateReferenceOmissionRetriesCASAndReturnsMatchingView(t *testing.T) {
	base := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	projectID := uuid.NewString()
	for _, assetID := range []string{"asset-a", "asset-b"} {
		if err := base.Assets().Create(t.Context(), &model.Asset{
			ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
			StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := base.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "before",
		ReferenceImageAssetID: "asset-a", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	hooked := &hookedProjectRepository{ProjectRepository: base.Projects()}
	hooked.beforeFind = func(call int) {
		if call != 2 {
			return
		}
		project, err := base.Projects().FindByID(t.Context(), projectID)
		if err != nil {
			t.Fatal(err)
		}
		project.ReferenceImageAssetID = "asset-b"
		if err := base.Projects().Update(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	repo := projectHandlerRepositoryOverride{Repository: base, projects: hooked}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newProjectHandlerTestApp(repo, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "after"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"asset_id":"asset-b"`) || strings.Contains(string(body), `"asset_id":"asset-a"`) {
		t.Fatalf("response does not match retried reference: %s", body)
	}
	wantSigned := []string{
		"assets/users/" + userID + "/asset-a/ref.png",
		"assets/users/" + userID + "/asset-b/ref.png",
	}
	if strings.Join(store.signedKeys, "|") != strings.Join(wantSigned, "|") {
		t.Fatalf("signed keys=%#v want %#v", store.signedKeys, wantSigned)
	}
	persisted, err := base.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "after" || persisted.ReferenceImageAssetID != "asset-b" {
		t.Fatalf("persisted project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
	}
}

func TestProjectUpdateReferenceOmissionReturnsConflictAfterBoundedCASRetries(t *testing.T) {
	base := repository.New(setupTaskHandlerTestDB(t))
	userID := uuid.NewString()
	projectID := uuid.NewString()
	assetIDs := []string{"asset-a", "asset-b", "asset-c", "asset-d"}
	for _, assetID := range assetIDs {
		if err := base.Assets().Create(t.Context(), &model.Asset{
			ID: assetID, UserID: userID, Purpose: service.DirectUploadPurposeProjectReference,
			StorageKey: "assets/users/" + userID + "/" + assetID + "/ref.png", FileName: "ref.png",
			ContentType: "image/png", Size: 3, ETag: "etag-" + assetID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := base.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "before",
		ReferenceImageAssetID: assetIDs[0], Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	hooked := &hookedProjectRepository{ProjectRepository: base.Projects()}
	hooked.beforeCAS = func(call int) {
		project, err := base.Projects().FindByID(t.Context(), projectID)
		if err != nil {
			t.Fatal(err)
		}
		project.ReferenceImageAssetID = assetIDs[call]
		if err := base.Projects().Update(t.Context(), project); err != nil {
			t.Fatal(err)
		}
	}
	repo := projectHandlerRepositoryOverride{Repository: base, projects: hooked}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newProjectHandlerTestApp(repo, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "must-not-write"})
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status=%d want 409 body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	if hooked.casCalls != 3 {
		t.Fatalf("CAS calls=%d want 3", hooked.casCalls)
	}
	wantSigned := []string{
		"assets/users/" + userID + "/asset-a/ref.png",
		"assets/users/" + userID + "/asset-b/ref.png",
		"assets/users/" + userID + "/asset-c/ref.png",
	}
	if strings.Join(store.signedKeys, "|") != strings.Join(wantSigned, "|") {
		t.Fatalf("signed keys=%#v want %#v", store.signedKeys, wantSigned)
	}
	persisted, err := base.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "before" || persisted.ReferenceImageAssetID != "asset-d" {
		t.Fatalf("request mutated project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
	}
}

func TestProjectUpdateReferenceOmissionMatchesNullReferenceRow(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	base := repository.New(db)
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := base.Projects().Create(t.Context(), &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle,
		Name: "before", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("UPDATE projects SET reference_image_asset_id = NULL WHERE id = ?", projectID).Error; err != nil {
		t.Fatal(err)
	}
	store := &projectReferenceStore{fakeStorageProvider: uploadSessionStatStore(base.UploadSessions())}
	app := newProjectHandlerTestApp(base, store)

	resp := doRequest(t, app, http.MethodPut, "/api/v1/projects/"+projectID, userID, map[string]any{"name": "after"})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status=%d body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	persisted, err := base.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Name != "after" || persisted.ReferenceImageAssetID != "" {
		t.Fatalf("persisted project name=%q reference=%q", persisted.Name, persisted.ReferenceImageAssetID)
	}
	if len(store.signedKeys) != 0 {
		t.Fatalf("empty reference signed keys=%#v", store.signedKeys)
	}
}

func TestProjectHandler_UpdateFinalizesAvatarUploadSession(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	uploadID := "updated-avatar-upload"
	key := "uploads/pending/" + userID + "/" + uploadID + "/avatar.png"
	stagingURL := "https://cdn.example.com/" + key

	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "公众号项目",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID:         uploadID,
		UserID:     userID,
		Purpose:    service.DirectUploadPurposeProjectReference,
		StagingKey: key,

		FileName:    "avatar.png",
		ContentType: "image/png",
		Size:        123,
		Status:      model.UploadSessionPending,
		ExpiresAt:   time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session: %v", err)
	}

	resp := doRequest(t, app, "PUT", "/api/v1/projects/"+projectID, userID, map[string]any{
		"avatar_url": stagingURL,
	})
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200; body=%v", resp.StatusCode, decodeBody(t, resp))
	}
	responseBody := decodeBody(t, resp)
	responseJSON, _ := json.Marshal(responseBody)
	if strings.Contains(string(responseJSON), "uploads/pending/") || !strings.Contains(string(responseJSON), "assets/users/") {
		t.Fatalf("updated project persisted non-final URL: %s", responseJSON)
	}

	assertFinalizedAsset(t, repo, uploadID, "assets/users/"+userID+"/"+uploadID+"/avatar.png")
}

func TestProjectHandler_DeleteWithAssociatedTasksReturnsConflict(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()

	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	resp := doRequest(t, app, "DELETE", "/api/v1/projects/"+projectID, userID, nil)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}

	body := decodeBody(t, resp)
	msg, _ := body["msg"].(string)
	if !strings.Contains(msg, "cannot delete project with 1 associated tasks") ||
		!strings.Contains(msg, "archive it instead") {
		t.Fatalf("msg = %q, want associated task archive guidance", msg)
	}
}

func TestProjectHandler_DeleteWithAssociatedPlansReturnsConflict(t *testing.T) {
	app, repo := setupProjectDeleteHandlerTest(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()

	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Plans().Create(ctx, &model.Plan{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Title:     "Daily ideas",
		Status:    model.PlanStatusActive,
	}); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	resp := doRequest(t, app, "DELETE", "/api/v1/projects/"+projectID, userID, nil)
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}

	body := decodeBody(t, resp)
	msg, _ := body["msg"].(string)
	if !strings.Contains(msg, "cannot delete project with 1 associated plans") ||
		!strings.Contains(msg, "archive it instead") {
		t.Fatalf("msg = %q, want associated plan archive guidance", msg)
	}
}
