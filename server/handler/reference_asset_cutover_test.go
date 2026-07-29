package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type referencePresentationStore struct {
	*fakeStorageProvider
	signedKeys  []string
	downloadErr error
}

func (s *referencePresentationStore) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	s.signedKeys = append(s.signedKeys, key)
	if s.downloadErr != nil {
		return "", s.downloadErr
	}
	return "https://download.example.com/" + key, nil
}

func TestTaskHandlerRejectsInvalidReferenceBeforePersistenceOrCredits(t *testing.T) {
	tests := []struct {
		name       string
		asset      *model.Asset
		assetID    string
		wantStatus int
	}{
		{name: "missing", assetID: "missing", wantStatus: fiber.StatusForbidden},
		{name: "foreign", assetID: "foreign", asset: cutoverAsset("foreign", "other", service.DirectUploadPurposeTaskReference, "ref.png", "image/png"), wantStatus: fiber.StatusForbidden},
		{name: "purpose", assetID: "purpose", asset: cutoverAsset("purpose", "user", service.DirectUploadPurposeProjectReference, "ref.png", "image/png"), wantStatus: fiber.StatusBadRequest},
		{name: "metadata", assetID: "metadata", asset: cutoverAsset("metadata", "user", service.DirectUploadPurposeTaskReference, "ref.txt", "text/plain"), wantStatus: fiber.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskHandlerTestDB(t)
			repo := repository.New(db)
			logger := zerolog.New(io.Discard)
			userID := "user"
			if err := repo.Users().Create(t.Context(), &model.User{ID: userID, OpenID: "openid-" + tt.name}); err != nil {
				t.Fatal(err)
			}
			project := &model.Project{ID: uuid.NewString(), UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}
			if err := repo.Projects().Create(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			if tt.asset != nil {
				if err := repo.Assets().Create(t.Context(), tt.asset); err != nil {
					t.Fatal(err)
				}
			}
			store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
			taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
			referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			h := NewTaskHandler(taskSvc, &logger)
			h.SetRepository(repo)
			h.SetStore(store)
			h.SetReferenceAssetService(referenceSvc)
			app := fiber.New()
			app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })
			resp := postJSON(t, app, "/tasks", `{"execution_profile":"cost_effective","project_id":"`+project.ID+`","prompt":"write","reference_image":{"asset_id":"`+tt.assetID+`"}}`)
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			_, total, _ := taskSvc.List(t.Context(), userID, 0, 10, "", "", "")
			if total != 0 {
				t.Fatalf("tasks = %d, want 0", total)
			}
		})
	}
}

func TestPlanAndTaskHandlersRejectLegacyReferenceImageURLPresence(t *testing.T) {
	for _, body := range []string{
		`{"project_id":"p","reference_image_url":"https://example.com/ref.png"}`,
		`{"project_id":"p","reference_image_url":null}`,
		`{"project_id":"p","reference_image_url":""}`,
		`{"project_id":"p","Reference_Image_URL":""}`,
	} {
		for _, route := range []string{"plan", "task"} {
			logger := zerolog.New(io.Discard)
			app := fiber.New()
			if route == "plan" {
				h := NewPlanHandler(nil, &logger)
				app.Post("/test", h.Create)
			} else {
				h := NewTaskHandler(nil, &logger)
				app.Post("/test", h.Create)
			}
			req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("%s body %s status=%d", route, body, resp.StatusCode)
			}
		}
	}
}

func TestPlanAndTaskReadResponsesPresentRepositoryAssets(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "read-user"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	planAsset := cutoverAsset("plan-asset", userID, service.DirectUploadPurposeTaskReference, "plan.png", "image/png")
	taskAsset := cutoverAsset("task-asset", userID, service.DirectUploadPurposeAIEntryAttachment, "task.png", "image/png")
	for _, asset := range []*model.Asset{planAsset, taskAsset} {
		if err := repo.Assets().Create(ctx, asset); err != nil {
			t.Fatal(err)
		}
	}
	plan := &model.Plan{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.PlanStatusActive, ReferenceImageAssetID: planAsset.ID}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted, ReferenceImageAssetID: taskAsset.ID}
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	logger := zerolog.New(io.Discard)
	planSvc := newHandlerPlanService(t, repo, &logger)
	planSvc.SetReferenceAssetService(referenceSvc)
	planHandler := NewPlanHandler(planSvc, &logger)
	planHandler.SetReferenceAssetService(referenceSvc)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	taskSvc.SetReferenceAssetService(referenceSvc)
	taskHandler := NewTaskHandler(taskSvc, &logger)
	taskHandler.SetRepository(repo)
	taskHandler.SetStore(store)
	taskHandler.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error { c.Locals("user_id", userID); return c.Next() })
	app.Get("/plans", planHandler.List)
	app.Get("/plans/:id", planHandler.GetByID)
	app.Get("/tasks", taskHandler.List)
	app.Get("/tasks/:id", taskHandler.GetByID)

	for _, path := range []string{"/plans", "/plans/" + plan.ID, "/tasks", "/tasks/" + task.ID} {
		resp, err := app.Test(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != fiber.StatusOK || !strings.Contains(string(body), `"reference_image":{"asset_id"`) || !strings.Contains(string(body), "https://download.example.com/assets/users/") {
			t.Fatalf("%s status/body = %d/%s", path, resp.StatusCode, body)
		}
		if strings.Contains(string(body), "reference_image_url") || strings.Contains(string(body), "storage_key") {
			t.Fatalf("%s leaked legacy reference data: %s", path, body)
		}
	}

	store.downloadErr = errors.New("signer unavailable")
	resp, err := app.Test(httptest.NewRequest("GET", "/tasks/"+task.ID, nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("signing failure status/body = %d/%s", resp.StatusCode, body)
	}
}

func TestBulkCloneSigningFailureDoesNotCreateOrCharge(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "bulk-clone-user"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	asset := cutoverAsset("bulk-asset", userID, service.DirectUploadPurposeTaskReference, "ref.png", "image/png")
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, ExecutionProfile: "cost_effective", Status: model.TaskStatusFailed, ExecutionTarget: model.ExecutionTargetCloud, ReferenceImageAssetID: asset.ID}
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}, downloadErr: errors.New("signer unavailable")}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Post("/tasks/bulk-clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.BulkClone(c) })
	resp := postJSON(t, app, "/tasks/bulk-clone", `{"task_ids":["`+source.ID+`"]}`)
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s", resp.StatusCode, body)
	}
	_, total, err := taskSvc.List(ctx, userID, 0, 10, "", "", "")
	if err != nil || total != 1 {
		t.Fatalf("tasks = %d, %v; want source only", total, err)
	}
}

func TestPlanMutationSigningFailureDoesNotPersist(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "plan-sign-user"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	asset := cutoverAsset("plan-sign-asset", userID, service.DirectUploadPurposeTaskReference, "ref.png", "image/png")
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}, downloadErr: errors.New("signer unavailable")}
	logger := zerolog.New(io.Discard)
	planSvc := newHandlerPlanService(t, repo, &logger)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	planSvc.SetReferenceAssetService(referenceSvc)
	h := NewPlanHandler(planSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Post("/plans", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })
	app.Put("/plans/:id", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Update(c) })

	resp := postJSON(t, app, "/plans", `{"execution_profile":"cost_effective","project_id":"`+projectID+`","cron_expr":"0 9 * * *","prompt":"create","reference_image":{"asset_id":"`+asset.ID+`"}}`)
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create status/body = %d/%s", resp.StatusCode, body)
	}
	_, total, err := planSvc.List(ctx, userID, 0, 10, projectID)
	if err != nil || total != 0 {
		t.Fatalf("plans after failed create = %d, %v", total, err)
	}

	baseline := &model.Plan{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.PlanStatusActive, Prompt: "before"}
	if err := repo.Plans().Create(ctx, baseline); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("PUT", "/plans/"+baseline.ID, strings.NewReader(`{"execution_profile":"cost_effective","prompt":"after","reference_image":{"asset_id":"`+asset.ID+`"}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("update status/body = %d/%s", resp.StatusCode, body)
	}
	persisted, err := repo.Plans().FindByID(ctx, baseline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Prompt != "before" || persisted.ReferenceImageAssetID != "" {
		t.Fatalf("failed update persisted: %#v", persisted)
	}
}

func TestPlanUpdateReferenceImageOmissionNullReplaceAndInvalidEmptySelection(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "plan-update-reference"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	first := cutoverAsset("plan-first", userID, service.DirectUploadPurposeTaskReference, "first.png", "image/png")
	second := cutoverAsset("plan-second", userID, service.DirectUploadPurposeTaskReference, "second.png", "image/png")
	for _, asset := range []*model.Asset{first, second} {
		if err := repo.Assets().Create(ctx, asset); err != nil {
			t.Fatal(err)
		}
	}
	plan := &model.Plan{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, ExecutionProfile: "cost_effective", Status: model.PlanStatusActive, Prompt: "before", ReferenceImageAssetID: first.ID}
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatal(err)
	}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	logger := zerolog.New(io.Discard)
	planSvc := newHandlerPlanService(t, repo, &logger)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	planSvc.SetReferenceAssetService(referenceSvc)
	h := NewPlanHandler(planSvc, &logger)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Put("/plans/:id", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Update(c) })

	update := func(body string) *http.Response {
		t.Helper()
		req := httptest.NewRequest("PUT", "/plans/"+plan.ID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	assertID := func(want string) {
		t.Helper()
		persisted, err := repo.Plans().FindByID(ctx, plan.ID)
		if err != nil {
			t.Fatal(err)
		}
		if persisted.ReferenceImageAssetID != want {
			t.Fatalf("reference asset = %q, want %q", persisted.ReferenceImageAssetID, want)
		}
	}

	if resp := update(`{"execution_profile":"cost_effective","prompt":"omitted"}`); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("omitted status = %d", resp.StatusCode)
	}
	assertID(first.ID)
	if resp := update(`{"execution_profile":"cost_effective","prompt":"cleared","reference_image":null}`); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("null status = %d", resp.StatusCode)
	}
	assertID("")
	if resp := update(`{"execution_profile":"cost_effective","prompt":"replaced","reference_image":{"asset_id":"` + second.ID + `"}}`); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("replace status = %d", resp.StatusCode)
	}
	assertID(second.ID)
	if resp := update(`{"execution_profile":"cost_effective","prompt":"invalid","reference_image":{}}`); resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("empty selection status = %d", resp.StatusCode)
	}
	assertID(second.ID)
}

func TestTaskCreateInheritedReferenceSigningFailureDoesNotCreateOrCharge(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "task-sign-user"}); err != nil {
		t.Fatal(err)
	}
	asset := cutoverAsset("project-sign-asset", userID, service.DirectUploadPurposeProjectReference, "ref.png", "image/png")
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive, ReferenceImageAssetID: asset.ID}); err != nil {
		t.Fatal(err)
	}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}, downloadErr: errors.New("signer unavailable")}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })
	resp := postJSON(t, app, "/tasks", `{"execution_profile":"cost_effective","project_id":"`+projectID+`","prompt":"write"}`)
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s", resp.StatusCode, body)
	}
	_, total, err := taskSvc.List(ctx, userID, 0, 10, "", "", "")
	if err != nil || total != 0 {
		t.Fatalf("tasks after signing failure = %d, %v", total, err)
	}
}

type taskHandlerProjectRepository struct {
	repository.ProjectRepository
	findCalls int
	find      func(context.Context, string) (*model.Project, error)
}

func (r *taskHandlerProjectRepository) FindByID(ctx context.Context, id string) (*model.Project, error) {
	r.findCalls++
	if r.find != nil {
		return r.find(ctx, id)
	}
	return r.ProjectRepository.FindByID(ctx, id)
}

type taskHandlerRepositoryOverride struct {
	repository.Repository
	projects repository.ProjectRepository
}

func (r *taskHandlerRepositoryOverride) Projects() repository.ProjectRepository { return r.projects }

func TestTaskCreateProjectLookupFailsClosedBeforeMutation(t *testing.T) {
	rootCause := errors.New("project repository unavailable")
	tests := []struct {
		name          string
		failOnDefense bool
		find          func(string, string) (*model.Project, error)
		wantStatus    int
	}{
		{name: "missing", find: func(string, string) (*model.Project, error) { return nil, gorm.ErrRecordNotFound }, wantStatus: fiber.StatusNotFound},
		{name: "repository error", find: func(string, string) (*model.Project, error) { return nil, rootCause }, wantStatus: fiber.StatusInternalServerError},
		{name: "foreign", find: func(projectID, _ string) (*model.Project, error) {
			return &model.Project{ID: projectID, UserID: "other-user", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}, nil
		}, wantStatus: fiber.StatusForbidden},
		{name: "missing on service defense", failOnDefense: true, find: func(string, string) (*model.Project, error) { return nil, gorm.ErrRecordNotFound }, wantStatus: fiber.StatusNotFound},
		{name: "repository error on service defense", failOnDefense: true, find: func(string, string) (*model.Project, error) { return nil, rootCause }, wantStatus: fiber.StatusInternalServerError},
		{name: "foreign on service defense", failOnDefense: true, find: func(projectID, _ string) (*model.Project, error) {
			return &model.Project{ID: projectID, UserID: "other-user", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}, nil
		}, wantStatus: fiber.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := repository.New(setupTaskHandlerTestDB(t))
			ctx := t.Context()
			userID := uuid.NewString()
			projectID := uuid.NewString()
			if err := base.Users().Create(ctx, &model.User{ID: userID, OpenID: "project-lookup-" + tt.name}); err != nil {
				t.Fatal(err)
			}
			projects := &taskHandlerProjectRepository{ProjectRepository: base.Projects()}
			projects.find = func(context.Context, string) (*model.Project, error) {
				if tt.failOnDefense && projects.findCalls == 1 {
					return &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Status: model.ProjectStatusActive}, nil
				}
				return tt.find(projectID, userID)
			}
			repo := &taskHandlerRepositoryOverride{Repository: base, projects: projects}
			logger := zerolog.New(io.Discard)
			taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
			taskSvc.SetReferenceAssetService(service.NewReferenceAssetService(repo, nil, time.Now))
			h := NewTaskHandler(taskSvc, &logger)
			h.SetRepository(repo)
			h.SetReferenceAssetService(service.NewReferenceAssetService(repo, nil, time.Now))
			app := fiber.New()
			app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })

			resp := postJSON(t, app, "/tasks", `{"execution_profile":"cost_effective","project_id":"`+projectID+`","prompt":"write"}`)
			if resp.StatusCode != tt.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want %d", resp.StatusCode, body, tt.wantStatus)
			}
			wantCalls := 1
			if tt.failOnDefense {
				wantCalls = 2
			}
			if projects.findCalls != wantCalls {
				t.Fatalf("project lookups = %d, want %d", projects.findCalls, wantCalls)
			}
			_, total, listErr := taskSvc.List(ctx, userID, 0, 10, "", "", "")
			if listErr != nil || total != 0 {
				t.Fatalf("tasks = %d, %v", total, listErr)
			}
		})
	}
}

func TestTaskCreateInheritedProjectReferenceFreezesPreflightSnapshotAndAttachesView(t *testing.T) {
	base := repository.New(setupTaskHandlerTestDB(t))
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := base.Users().Create(ctx, &model.User{ID: userID, OpenID: "single-project-lookup"}); err != nil {
		t.Fatal(err)
	}
	asset := cutoverAsset("project-inherited", userID, service.DirectUploadPurposeProjectReference, "ref.png", "image/png")
	if err := base.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	if err := base.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive, ReferenceImageAssetID: asset.ID}); err != nil {
		t.Fatal(err)
	}
	projects := &taskHandlerProjectRepository{ProjectRepository: base.Projects()}
	repo := &taskHandlerRepositoryOverride{Repository: base, projects: projects}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })

	resp := postJSON(t, app, "/tasks", `{"execution_profile":"cost_effective","project_id":"`+projectID+`","prompt":"write"}`)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK || !strings.Contains(string(body), `"reference_image":{"asset_id":"`+asset.ID+`"`) {
		t.Fatalf("status/body = %d/%s", resp.StatusCode, body)
	}
	if projects.findCalls != 3 {
		t.Fatalf("project lookups = %d, want preflight, service defense, and cloud dispatch", projects.findCalls)
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
		t.Fatalf("signed keys = %#v", store.signedKeys)
	}
}

func TestCloneResponseAttachesSignedReferenceView(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "clone-view-user"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	asset := cutoverAsset("clone-view-asset", userID, service.DirectUploadPurposeTaskReference, "ref.png", "image/png")
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, ExecutionProfile: "cost_effective", Status: model.TaskStatusCompleted, ExecutionTarget: model.ExecutionTargetCloud, ReferenceImageAssetID: asset.ID}
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })
	resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{"execution_profile":"cost_effective"}`)
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusOK || !strings.Contains(string(body), `"reference_image":{"asset_id":"`+asset.ID+`"`) || !strings.Contains(string(body), "https://download.example.com/"+asset.StorageKey) {
		t.Fatalf("status/body = %d/%s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "reference_image_url") || strings.Contains(string(body), "storage_key") {
		t.Fatalf("clone response leaked reference storage identity: %s", body)
	}
}

func TestResumeSigningFailureDoesNotMutateTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "resume-sign-user"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Name: "Article", Platform: model.PlatformArticle, Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	asset := cutoverAsset("resume-sign-asset", userID, service.DirectUploadPurposeTaskReference, "ref.png", "image/png")
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted, ReferenceImageAssetID: asset.ID}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}, downloadErr: errors.New("signer unavailable")}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Resume(c) })
	resp := postJSON(t, app, "/tasks/"+task.ID+"/resume", `{"prompt":"continue"}`)
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s", resp.StatusCode, body)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.TaskStatusCompleted {
		t.Fatalf("task status mutated after signing failure: %q", persisted.Status)
	}
}

func cutoverAsset(id, userID, purpose, fileName, contentType string) *model.Asset {
	return &model.Asset{ID: id, UserID: userID, Purpose: purpose, StorageKey: "assets/users/" + userID + "/" + id + "/" + fileName, FileName: fileName, ContentType: contentType, Size: 3, ETag: "etag"}
}
