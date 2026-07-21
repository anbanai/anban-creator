package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type executionScopeTestStore struct{ *fakeAgentArtifactStorage }

func (s *executionScopeTestStore) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	return &storage.UploadResult{Key: key, URL: s.GetURL(key), Size: int64(len(data)), MimeType: contentType}, nil
}

type testWorkloadVerifier struct {
	gotToken, gotExecutionID string
	identity                 *serveragent.KubernetesWorkloadIdentity
	err                      error
}

func (v *testWorkloadVerifier) Verify(_ context.Context, token, executionID string) (*serveragent.KubernetesWorkloadIdentity, error) {
	v.gotToken, v.gotExecutionID = token, executionID
	return v.identity, v.err
}

type testBootstrapper struct {
	response *service.AgentBootstrapResponse
	err      error
}

func (b testBootstrapper) Bootstrap(context.Context, *serveragent.KubernetesWorkloadIdentity) (*service.AgentBootstrapResponse, error) {
	return b.response, b.err
}

func TestAgentExecutionTokenAndWorkloadBootstrap(t *testing.T) {
	logger := zerolog.New(io.Discard)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := tokens.Issue(auth.ExecutionClaims{UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1"}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	h := NewAgentHandler(nil, nil, nil, "", &logger)
	h.SetExecutionTokenService(tokens)
	verifier := &testWorkloadVerifier{identity: &serveragent.KubernetesWorkloadIdentity{ExecutionID: "execution-1"}}
	h.SetBootstrap(verifier, testBootstrapper{response: &service.AgentBootstrapResponse{TaskID: "task-1", ExecutionToken: "execution-token"}})
	app := fiber.New()
	app.Post("/agent/scoped", h.AuthMiddleware, func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"user": c.Locals(agentUserIDContextKey), "project": c.Locals(agentProjectIDContextKey), "task": c.Locals(agentTaskIDContextKey), "execution": c.Locals(agentExecutionIDContextKey)})
	})
	app.Post("/agent/claim", h.AuthMiddleware, h.Claim)
	app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)

	req := httptest.NewRequest("POST", "/agent/scoped", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("execution auth status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"execution":"execution-1"`) || !strings.Contains(string(body), `"task":"task-1"`) {
		t.Fatalf("locals body = %s", body)
	}
	req = httptest.NewRequest("POST", "/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("execution JWT claim status = %d, want 403", resp.StatusCode)
	}

	req = httptest.NewRequest("POST", "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || verifier.gotToken != "workload-token" || verifier.gotExecutionID != "execution-1" {
		t.Fatalf("bootstrap status/token/id = %d/%q/%q", resp.StatusCode, verifier.gotToken, verifier.gotExecutionID)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("bootstrap Cache-Control = %q, want no-store", got)
	}
}

func TestAgentInvalidJWTDoesNotDowngradeToStaticKey(t *testing.T) {
	logger := zerolog.New(io.Discard)
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	h := NewAgentHandler(nil, nil, nil, "independent-key", &logger)
	h.SetExecutionTokenService(tokens)
	app := fiber.New()
	app.Post("/agent", h.AuthMiddleware, func(c fiber.Ctx) error { return c.SendStatus(204) })
	req := httptest.NewRequest("POST", "/agent", nil)
	req.Header.Set("Authorization", "Bearer bad.jwt.token")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	req = httptest.NewRequest("POST", "/agent", nil)
	req.Header.Set("Authorization", "Bearer bad.jwt.token")
	req.Header.Set("X-Agent-API-Key", "independent-key")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("conflicting invalid JWT/API key status = %d, want 401", resp.StatusCode)
	}

	raw, err := tokens.Issue(auth.ExecutionClaims{UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1"}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest("POST", "/agent", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("X-Agent-API-Key", "independent-key")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("conflicting valid JWT/API key status = %d, want 401", resp.StatusCode)
	}

	for _, setCredential := range []func(*http.Request){
		func(req *http.Request) { req.Header.Set("Authorization", "Bearer independent-key") },
		func(req *http.Request) { req.Header.Set("X-Agent-API-Key", "independent-key") },
	} {
		req = httptest.NewRequest("POST", "/agent", nil)
		setCredential(req)
		resp, err = app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusNoContent {
			t.Fatalf("single legacy API key status = %d, want 204", resp.StatusCode)
		}
	}
	req = httptest.NewRequest("POST", "/agent", nil)
	req.Header.Set("X-Agent-API-Key", "independent-key")
	req.Header.Set("X-API-Key", "independent-key")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("simultaneous valid legacy credentials status = %d, want 401", resp.StatusCode)
	}
}

func TestAgentBootstrapRedactsInternalErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "state conflict", err: fmt.Errorf("%w: secret-task-id", service.ErrAgentBootstrapConflict), wantStatus: fiber.StatusConflict, wantBody: "agent bootstrap state conflict"},
		{name: "dependency unavailable", err: fmt.Errorf("%w: sign key uploads/private/secret", service.ErrAgentBootstrapUnavailable), wantStatus: fiber.StatusServiceUnavailable, wantBody: "agent bootstrap dependency unavailable"},
		{name: "internal", err: errors.New("database password and object key"), wantStatus: fiber.StatusInternalServerError, wantBody: "agent bootstrap failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			verifier := &testWorkloadVerifier{identity: &serveragent.KubernetesWorkloadIdentity{ExecutionID: "execution-1"}}
			h := NewAgentHandler(nil, nil, nil, "", &logger)
			h.SetBootstrap(verifier, testBootstrapper{err: tc.err})
			app := fiber.New()
			app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)
			req := httptest.NewRequest(http.MethodPost, "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
			req.Header.Set("Authorization", "Bearer workload-token")
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.wantStatus || !strings.Contains(string(body), tc.wantBody) {
				t.Fatalf("status/body = %d/%s, want %d/%q", resp.StatusCode, body, tc.wantStatus, tc.wantBody)
			}
			for _, leaked := range []string{"secret-task-id", "uploads/private/secret", "database password", "object key"} {
				if strings.Contains(string(body), leaked) {
					t.Fatalf("internal detail %q leaked in %s", leaked, body)
				}
			}
		})
	}
}

func TestAgentBootstrapLogsWorkloadVerificationErrorWithoutLeakingIt(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	verifier := &testWorkloadVerifier{err: errors.New("token review failed for private pod uid")}
	h := NewAgentHandler(nil, nil, nil, "", &logger)
	h.SetBootstrap(verifier, testBootstrapper{})
	app := fiber.New()
	app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)
	req := httptest.NewRequest(http.MethodPost, "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusUnauthorized || !strings.Contains(string(body), "workload identity verification failed") {
		t.Fatalf("status/body = %d/%s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "private pod uid") {
		t.Fatalf("verification detail leaked in response: %s", body)
	}
	if !strings.Contains(logs.String(), "private pod uid") || !strings.Contains(logs.String(), "execution-1") {
		t.Fatalf("verification failure missing from server logs: %s", logs.String())
	}
}

func TestAgentExecutionJWTScopesAllTaskEndpoints(t *testing.T) {
	endpoints := []struct {
		name    string
		path    string
		request func(string, string) *http.Request
	}{
		{"progress", "/agent/progress", func(taskID, _ string) *http.Request {
			return agentJSONRequest("/agent/progress", `{"task_id":"`+taskID+`","message":"working"}`)
		}},
		{"upload", "/agent/upload", func(taskID, _ string) *http.Request { return agentMultipartUploadRequest(taskID) }},
		{"prepare", "/agent/artifacts/prepare", func(taskID, executionID string) *http.Request {
			return agentJSONRequest("/agent/artifacts/prepare", `{"task_id":"`+taskID+`","execution_id":"`+executionID+`","relative_path":"output/content.md","filename":"content.md","content_type":"text/markdown","size":7}`)
		}},
		{"manifest", "/agent/artifacts/manifest", func(taskID, executionID string) *http.Request {
			return agentJSONRequest("/agent/artifacts/manifest", `{"task_id":"`+taskID+`","execution_id":"`+executionID+`","files":[]}`)
		}},
		{"complete", "/agent/complete", func(taskID, _ string) *http.Request {
			return agentJSONRequest("/agent/complete", `{"task_id":"`+taskID+`"}`)
		}},
	}
	for _, endpoint := range endpoints {
		for _, mode := range []string{"current", "cross_task", "stale", "terminal"} {
			t.Run(endpoint.name+"/"+mode, func(t *testing.T) {
				app, repo, task, executionID, token, _, store := setupExecutionScopedAgentApp(t)
				requestTaskID := task.ID
				expectedTaskStatus := model.TaskStatusRunning
				switch mode {
				case "cross_task":
					requestTaskID = uuid.NewString()
				case "stale":
					other := uuid.NewString()
					persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
					if err != nil {
						t.Fatal(err)
					}
					persisted.CurrentExecutionID = &other
					if err := repo.Tasks().Update(context.Background(), persisted); err != nil {
						t.Fatal(err)
					}
				case "terminal":
					persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
					if err != nil {
						t.Fatal(err)
					}
					persisted.Status = model.TaskStatusFailed
					expectedTaskStatus = model.TaskStatusFailed
					if err := repo.Tasks().Update(context.Background(), persisted); err != nil {
						t.Fatal(err)
					}
				}
				req := endpoint.request(requestTaskID, executionID)
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				if mode == "current" && resp.StatusCode != fiber.StatusOK {
					t.Fatalf("current execution %s status = %d, want 200", executionID, resp.StatusCode)
				}
				if mode != "current" && resp.StatusCode != fiber.StatusForbidden {
					t.Fatalf("%s status = %d, want 403", mode, resp.StatusCode)
				}
				if mode != "current" {
					persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
					if err != nil {
						t.Fatal(err)
					}
					files, err := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
					if err != nil {
						t.Fatal(err)
					}
					if persisted.ProgressLog != "" || persisted.Status != expectedTaskStatus || len(files) != 0 || store.uploadKey != "" {
						t.Fatalf("rejected request caused side effect: task=%#v files=%d upload=%q", persisted, len(files), store.uploadKey)
					}
				}
			})
		}
	}
}

func TestAgentAPIKeyProgressBehaviorIsPreserved(t *testing.T) {
	app, _, task, _, _, rawAPIKey, _ := setupExecutionScopedAgentApp(t)
	req := agentJSONRequest("/agent/progress", `{"task_id":"`+task.ID+`","message":"local progress"}`)
	req.Header.Set("Authorization", "Bearer "+rawAPIKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("API-key progress status = %d, want 200", resp.StatusCode)
	}
}

func TestAgentProgressResultPayloadCannotWriteTerminalEvidence(t *testing.T) {
	app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentApp(t)
	body := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","message":"still running","result":{"success":true,"model_usage":[{"provider":"provider","model":"early","input_tokens":17}],"cost_status":"reconciled"}}`
	req := agentJSONRequest("/agent/progress", body)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("progress status/body = %d/%s", resp.StatusCode, responseBody)
	}

	found, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Result != nil || found.CostStatus != "" || len(found.TerminalModelUsage.Data()) != 0 {
		t.Fatalf("non-terminal progress persisted evidence: result=%v cost_status=%q usage=%+v", found.Result, found.CostStatus, found.TerminalModelUsage.Data())
	}
}

func TestAgentAPIKeyCannotUseLegacyArtifactContractForCloudTask(t *testing.T) {
	app, repo, task, _, _, rawAPIKey, store := setupExecutionScopedAgentApp(t)
	for _, endpoint := range []struct{ path, body string }{
		{"/agent/artifacts/prepare", `{"task_id":"` + task.ID + `","relative_path":"output/content.md","size":7}`},
		{"/agent/artifacts/manifest", `{"task_id":"` + task.ID + `","files":[]}`},
	} {
		req := agentJSONRequest(endpoint.path, endpoint.body)
		req.Header.Set("Authorization", "Bearer "+rawAPIKey)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusConflict {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s status/body = %d/%s", endpoint.path, resp.StatusCode, body)
		}
	}
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if len(files) != 0 || store.uploadKey != "" {
		t.Fatalf("rejected requests caused side effects: files=%#v upload=%q", files, store.uploadKey)
	}
}

func TestAgentArtifactManifestPersistenceFailureIsRedacted(t *testing.T) {
	app, _, task, executionID, token, _, store := setupExecutionScopedAgentApp(t)
	key := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/executions/" + executionID + "/artifacts/output/content.md"
	store.stats = map[string]*storage.ObjectInfo{key: {Key: key, Size: 7, ContentType: "text/markdown"}}
	file := `{"relative_path":"output/content.md","object_key":"` + key + `","size":7,"sha256":"` + strings.Repeat("a", 64) + `"}`
	req := agentJSONRequest("/agent/artifacts/manifest", `{"task_id":"`+task.ID+`","execution_id":"`+executionID+`","files":[`+file+`,`+file+`]}`)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusInternalServerError || !strings.Contains(string(body), "failed to persist") {
		t.Fatalf("status/body = %d/%s", resp.StatusCode, body)
	}
	if strings.Contains(string(body), "UNIQUE") || strings.Contains(string(body), "constraint") {
		t.Fatalf("database detail leaked: %s", body)
	}
}

func setupExecutionScopedAgentApp(t *testing.T) (*fiber.App, repository.Repository, *model.Task, string, string, string, *fakeAgentArtifactStorage) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID, taskID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: uuid.NewString() + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "P", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &executionID}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true, StartedAt: &now, PodUID: "pod-1"}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	store := &executionScopeTestStore{fakeAgentArtifactStorage: &fakeAgentArtifactStorage{}}
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	apiKeys := service.NewAPIKeyService(repo, &logger)
	_, rawAPIKey, err := apiKeys.Create(ctx, userID, "local")
	if err != nil {
		t.Fatal(err)
	}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	token, err := tokens.Issue(auth.ExecutionClaims{UserID: userID, ProjectID: projectID, TaskID: taskID, ExecutionID: executionID}, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	h := NewAgentHandler(taskSvc, apiKeys, store, "", &logger)
	h.SetExecutionTokenService(tokens)
	h.SetDirectUploadConfig(service.DirectUploadConfig{Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", STSRoleArn: "role"}, CredentialIssuer: service.StaticUploadCredentialIssuer(func(context.Context, service.UploadCredentialRequest) (*service.UploadCredential, error) {
		return &service.UploadCredential{AccessKeyID: "ak", AccessKeySecret: "secret", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})})
	app := fiber.New()
	app.Post("/agent/progress", h.AuthMiddleware, h.Progress)
	app.Post("/agent/upload", h.AuthMiddleware, h.Upload)
	app.Post("/agent/artifacts/prepare", h.AuthMiddleware, h.PrepareArtifactUpload)
	app.Post("/agent/artifacts/manifest", h.AuthMiddleware, h.ReportArtifactManifest)
	app.Post("/agent/complete", h.AuthMiddleware, h.Complete)
	return app, repo, task, executionID, token, rawAPIKey, store.fakeAgentArtifactStorage
}

func TestAgentProgressRefreshesTaskAndExecutionHeartbeats(t *testing.T) {
	app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentApp(t)
	req := agentJSONRequest("/agent/progress", `{"task_id":"`+task.ID+`"}`)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("heartbeat status = %d, want 200", resp.StatusCode)
	}
	foundTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundExecution, err := repo.TaskExecutions().FindByID(context.Background(), executionID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.LastHeartbeatAt == nil || foundExecution.LastHeartbeatAt == nil {
		t.Fatalf("heartbeats task=%v execution=%v", foundTask.LastHeartbeatAt, foundExecution.LastHeartbeatAt)
	}
}

func agentJSONRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestResolvePublishingRequiresAdminKeyAndResumesFinalization(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, taskID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: uuid.NewString() + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12]}); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: taskID, UserID: userID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, CurrentExecutionID: &executionID}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	execution := &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionSucceeded, Started: true, FinalizationStatus: model.TaskExecutionFinalizationWorkflow, PublishingStatus: model.TaskExecutionPublishingAmbiguous}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
	h := NewAgentHandler(taskSvc, nil, nil, "", &logger)
	h.SetAdminAPIKey("operator-secret")
	app := fiber.New()
	app.Post("/admin/executions/:executionID/publishing", h.ResolvePublishing)

	unauthorized := agentJSONRequest("/admin/executions/"+executionID+"/publishing", `{"published":true}`)
	unauthorized.Header.Set("Authorization", "Bearer execution-jwt")
	resp, err := app.Test(unauthorized)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d", resp.StatusCode)
	}

	authorized := agentJSONRequest("/admin/executions/"+executionID+"/publishing", `{"published":true}`)
	authorized.Header.Set("X-Admin-API-Key", "operator-secret")
	resp, err = app.Test(authorized)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("authorized status=%d", resp.StatusCode)
	}
	foundExecution, _ := repo.TaskExecutions().FindByID(ctx, executionID)
	foundTask, _ := repo.Tasks().FindByID(ctx, taskID)
	if foundExecution.PublishingStatus != model.TaskExecutionPublishingSucceeded || foundExecution.FinalizationStatus != model.TaskExecutionFinalizationDone || !foundTask.Published || foundTask.Status != model.TaskStatusCompleted {
		t.Fatalf("execution=%+v task=%+v", foundExecution, foundTask)
	}
}

func agentMultipartUploadRequest(taskID string) *http.Request {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	_ = w.WriteField("task_id", taskID)
	_ = w.WriteField("relative_path", "output/content.md")
	file, _ := w.CreateFormFile("file", "content.md")
	_, _ = file.Write([]byte("content"))
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/agent/upload", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// setupAgentClaimApp wires an AgentHandler (real API-key auth via APIKeyService)
// over a fresh sqlite DB seeded with a user + project, and returns the app, the
// repo handle (so callers can seed tasks against the same DB), the user's raw
// API key, and the user/project IDs.
func setupAgentClaimApp(t *testing.T) (app *fiber.App, repo repository.Repository, rawKey, userID, projectID string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo = repository.New(db)
	ctx := context.Background()

	userID = uuid.New().String()
	projectID = uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "claim@example.com", Password: "x", InviteCode: "ic"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "P", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, nil, &logger, "", nil, "", nil, nil)
	taskSvc.SetExecutorDefaults("claude-test", nil)
	apiKeySvc := service.NewAPIKeyService(repo, &logger)
	_, rawKey, err := apiKeySvc.Create(ctx, userID, "test")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}

	h := NewAgentHandler(taskSvc, apiKeySvc, nil, "", &logger)
	app = fiber.New()
	app.Post("/agent/claim", h.AuthMiddleware, h.Claim)
	return app, repo, rawKey, userID, projectID
}

// seedClaimableLocalTask persists a pending local-target task and returns its ID.
func seedClaimableLocalTask(t *testing.T, repo repository.Repository, userID, projectID string) string {
	t.Helper()
	task := &model.Task{
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformSeednote,
		Status:             model.TaskStatusPending,
		Prompt:             "测试选题",
		ExecutionTarget:    model.ExecutionTargetLocal,
		LocalClaimDeadline: ptrTime(time.Now().Add(service.LocalClaimWindow)),
	}
	if err := repo.Tasks().Create(context.Background(), task); err != nil {
		t.Fatalf("create seed task: %v", err)
	}
	return task.ID
}

func ptrTime(t time.Time) *time.Time { return &t }

type fakeAgentArtifactStorage struct {
	uploadKey         string
	uploadContentType string
	stats             map[string]*storage.ObjectInfo
	statErr           error
}

func (f *fakeAgentArtifactStorage) Name() string { return "oss" }
func (f *fakeAgentArtifactStorage) Upload(context.Context, string, io.Reader, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (f *fakeAgentArtifactStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, nil
}
func (f *fakeAgentArtifactStorage) UploadURL(_ context.Context, key string, contentType string, _ int) (string, error) {
	f.uploadKey = key
	f.uploadContentType = contentType
	return "https://upload.example.com/" + key, nil
}
func (f *fakeAgentArtifactStorage) GetURL(key string) string { return "https://cdn.example.com/" + key }
func (f *fakeAgentArtifactStorage) Read(context.Context, string) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (f *fakeAgentArtifactStorage) Delete(context.Context, string) error { return nil }
func (f *fakeAgentArtifactStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	return "https://download.example.com/" + key, nil
}
func (f *fakeAgentArtifactStorage) HasCustomDomain() bool { return true }
func (f *fakeAgentArtifactStorage) IsOwnedURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "https://cdn.example.com/")
}
func (f *fakeAgentArtifactStorage) StatObject(_ context.Context, key string) (*storage.ObjectInfo, error) {
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.stats == nil || f.stats[key] == nil {
		return nil, os.ErrNotExist
	}
	cp := *f.stats[key]
	return &cp, nil
}

func TestAgentArtifactManifestErrorTaxonomy(t *testing.T) {
	app, _, task, store, rawKey, _ := setupAgentArtifactApp(t)
	wantKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/output/article.md"
	request := func(body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/agent/artifacts/manifest", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	invalidStatus, _ := request(`{"task_id":"` + task.ID + `","files":[{"relative_path":"../bad","object_key":"bad","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}]}`)
	if invalidStatus != fiber.StatusBadRequest {
		t.Fatalf("invalid status = %d", invalidStatus)
	}
	store.statErr = errors.New("secret backend endpoint timed out")
	unavailableStatus, body := request(`{"task_id":"` + task.ID + `","files":[{"relative_path":"output/article.md","object_key":"` + wantKey + `","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}]}`)
	if unavailableStatus != fiber.StatusServiceUnavailable {
		t.Fatalf("unavailable status/body = %d/%s", unavailableStatus, body)
	}
	if strings.Contains(body, "secret backend") || !strings.Contains(body, "temporarily unavailable") {
		t.Fatalf("backend detail leaked: %s", body)
	}
}

func setupAgentArtifactApp(t *testing.T) (*fiber.App, repository.Repository, *model.Task, *fakeAgentArtifactStorage, string, string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	projectID := uuid.New().String()
	for _, user := range []*model.User{
		{ID: userID, Email: "artifact@example.com", Password: "x", InviteCode: "artifact"},
		{ID: otherUserID, Email: "other-artifact@example.com", Password: "x", InviteCode: "other-artifact"},
	} {
		if err := repo.Users().Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "P", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	store := &fakeAgentArtifactStorage{}
	taskSvc := service.NewTaskService(repo, nil, noopTaskEnqueuer{}, store, &logger, "", nil, "", nil, nil)
	apiKeySvc := service.NewAPIKeyService(repo, &logger)
	_, rawKey, err := apiKeySvc.Create(ctx, userID, "artifact")
	if err != nil {
		t.Fatalf("create api key: %v", err)
	}
	_, otherRawKey, err := apiKeySvc.Create(ctx, otherUserID, "artifact-other")
	if err != nil {
		t.Fatalf("create other api key: %v", err)
	}
	h := NewAgentHandler(taskSvc, apiKeySvc, store, "", &logger)
	h.SetDirectUploadConfig(service.DirectUploadConfig{
		Storage: config.StorageConfig{
			Provider:       "oss",
			Endpoint:       "oss-cn-hangzhou.aliyuncs.com",
			BucketName:     "anban-test",
			Region:         "oss-cn-hangzhou",
			STSRoleArn:     "acs:ram::1:role/upload",
			STSSessionName: "agent-artifact-upload",
		},
		CredentialIssuer: service.StaticUploadCredentialIssuer(func(context.Context, service.UploadCredentialRequest) (*service.UploadCredential, error) {
			return &service.UploadCredential{
				AccessKeyID:     "sts-ak",
				AccessKeySecret: "sts-secret",
				SecurityToken:   "sts-token",
				ExpiresAt:       time.Now().Add(15 * time.Minute),
			}, nil
		}),
	})

	app := fiber.New()
	app.Post("/agent/artifacts/prepare", h.AuthMiddleware, h.PrepareArtifactUpload)
	app.Post("/agent/artifacts/manifest", h.AuthMiddleware, h.ReportArtifactManifest)
	return app, repo, task, store, rawKey, otherRawKey
}

func TestAgentArtifactPrepareAndManifest(t *testing.T) {
	app, repo, task, store, rawKey, _ := setupAgentArtifactApp(t)

	prepareBody := `{"task_id":"` + task.ID + `","relative_path":"output/article.md","filename":"article.md","content_type":"text/markdown","size":123}`
	req := httptest.NewRequest("POST", "/agent/artifacts/prepare", strings.NewReader(prepareBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("prepare request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("prepare status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var env struct {
		Data struct {
			Key                string `json:"key"`
			STSAccessKeyID     string `json:"sts_access_key_id"`
			STSSecurityToken   string `json:"sts_security_token"`
			STSAccessKeySecret string `json:"sts_access_key_secret"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode prepare: %v", err)
	}
	wantKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/output/article.md"
	if env.Data.Key != wantKey || store.uploadKey != wantKey || store.uploadContentType != "text/markdown" {
		t.Fatalf("prepared key/store = %q/%q/%q, want %q", env.Data.Key, store.uploadKey, store.uploadContentType, wantKey)
	}
	if env.Data.STSAccessKeyID != "sts-ak" || env.Data.STSSecurityToken != "sts-token" || env.Data.STSAccessKeySecret == "" {
		t.Fatalf("missing sts credentials: %#v", env.Data)
	}

	body := []byte("# title\n\nbody")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])
	store.stats = map[string]*storage.ObjectInfo{
		wantKey: {Key: wantKey, Size: int64(len(body)), ContentType: "text/markdown", ETag: "etag"},
	}
	manifestBody := `{"task_id":"` + task.ID + `","files":[{"relative_path":"output/article.md","object_key":"` + wantKey + `","content_type":"text/markdown","size":` + "13" + `,"sha256":"` + hash + `","etag":"etag"}]}`
	req = httptest.NewRequest("POST", "/agent/artifacts/manifest", strings.NewReader(manifestBody))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("manifest request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("manifest status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	files, err := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("find task files: %v", err)
	}
	if len(files) != 1 || files[0].OSSKey != wantKey {
		t.Fatalf("task files = %#v, want one file with key %q", files, wantKey)
	}
}

func TestAgentArtifactPrepareRejectsCrossUser(t *testing.T) {
	app, _, task, _, _, otherRawKey := setupAgentArtifactApp(t)

	body := `{"task_id":"` + task.ID + `","relative_path":"output/article.md","filename":"article.md","content_type":"text/markdown","size":123}`
	req := httptest.NewRequest("POST", "/agent/artifacts/prepare", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+otherRawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 403; body=%s", resp.StatusCode, body)
	}
}

// TestAgentClaim_RequiresAuth confirms a missing token is rejected.
func TestAgentClaim_RequiresAuth(t *testing.T) {
	app, _, _, _, _ := setupAgentClaimApp(t)
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestAgentClaim_RejectsInvalidToken confirms an unknown token is rejected.
func TestAgentClaim_RejectsInvalidToken(t *testing.T) {
	app, _, _, _, _ := setupAgentClaimApp(t)
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer not-a-real-key")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// TestAgentClaim_ReturnsConfigThenNoContent confirms a valid claim returns the
// full task config, and a follow-up claim returns 204 (nothing left).
func TestAgentClaim_ReturnsConfigThenNoContent(t *testing.T) {
	app, repo, rawKey, userID, projectID := setupAgentClaimApp(t)
	taskID := seedClaimableLocalTask(t, repo, userID, projectID)

	// First claim: should return 200 + config carrying the task id.
	req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"executor_info":{"hostname":"mbp"}}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), taskID) {
		t.Fatalf("response body does not contain task id %q: %s", taskID, body)
	}
	if !strings.Contains(string(body), `"agent_flag"`) {
		t.Fatalf("response body missing agent_flag field: %s", body)
	}

	// Second claim: nothing claimable → 204 No Content.
	req2 := httptest.NewRequest("POST", "/agent/claim", nil)
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	resp2, err := app.Test(req2)
	if err != nil {
		t.Fatalf("second claim failed: %v", err)
	}
	if resp2.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp2.StatusCode)
	}
}

// TestAgentClaim_NoContentWhenEmpty confirms an unauthenticated-of-tasks claim
// returns 204 even with a valid token.
func TestAgentClaim_NoContentWhenEmpty(t *testing.T) {
	app, _, rawKey, _, _ := setupAgentClaimApp(t)
	// No task seeded.
	req := httptest.NewRequest("POST", "/agent/claim", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}
