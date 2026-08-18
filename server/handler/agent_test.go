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
	"gorm.io/datatypes"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type executionScopeTestStore struct {
	*fakeAgentArtifactStorage
	uploadedKey  string
	uploadedBody []byte
}

func (s *executionScopeTestStore) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	s.uploadedKey = key
	s.uploadedBody = append([]byte(nil), data...)
	s.fakeAgentArtifactStorage.uploadedKey = key
	s.fakeAgentArtifactStorage.uploadedBody = append([]byte(nil), data...)
	return &storage.UploadResult{Key: key, URL: s.GetURL(key), Size: int64(len(data)), MimeType: contentType}, nil
}

type testWorkloadVerifier struct {
	gotToken, gotExecutionID string
	identity                 *serveragent.WorkloadIdentity
	err                      error
}

func (v *testWorkloadVerifier) Verify(_ context.Context, token, executionID string) (*serveragent.WorkloadIdentity, error) {
	v.gotToken, v.gotExecutionID = token, executionID
	return v.identity, v.err
}

type testBootstrapper struct {
	response *service.AgentBootstrapResponse
	err      error
}

func (b testBootstrapper) Bootstrap(context.Context, *serveragent.WorkloadIdentity) (*service.AgentBootstrapResponse, error) {
	return b.response, b.err
}

func TestAgentHandlerExecutionTokenAndWorkloadBootstrap(t *testing.T) {
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
	verifier := &testWorkloadVerifier{identity: &serveragent.WorkloadIdentity{Target: "docker", RuntimeIdentity: model.RuntimeIdentity{Scope: "docker", Workload: "exec-1", InstanceID: "container-id"}, ExecutionID: "execution-1"}}
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
			verifier := &testWorkloadVerifier{identity: &serveragent.WorkloadIdentity{ExecutionID: "execution-1"}}
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
			return agentJSONRequest("/agent/artifacts/prepare", `{"task_id":"`+taskID+`","execution_id":"`+executionID+`","relative_path":"output/content.md","filename":"content.md","content_type":"text/markdown","size":7,"sha256":"`+strings.Repeat("a", 64)+`"}`)
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
	app, repo, task, _, _, rawAPIKey, _ := setupExecutionScopedAgentApp(t)
	req := agentJSONRequest("/agent/progress", `{"task_id":"`+task.ID+`","message":"local progress","logs":["first log","second log"]}`)
	req.Header.Set("Authorization", "Bearer "+rawAPIKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("API-key progress status = %d, want 200", resp.StatusCode)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := persisted.ProgressLog, "first log\nsecond log\nlocal progress\n"; got != want {
		t.Fatalf("API-key progress log = %q, want %q", got, want)
	}
}

func TestAgentAPIKeyCompletionRequiresCurrentExecutionWithoutSideEffects(t *testing.T) {
	for _, tt := range []struct {
		name        string
		target      string
		requestedID func(string) string
		mutate      func(t *testing.T, repo repository.Repository, task *model.Task, executionID string)
		wantStatus  int
	}{
		{name: "missing identity", requestedID: func(string) string { return "" }, wantStatus: fiber.StatusBadRequest},
		{name: "unknown identity", requestedID: func(string) string { return uuid.NewString() }, wantStatus: fiber.StatusForbidden},
		{name: "replaced task execution", requestedID: func(current string) string { return current }, wantStatus: fiber.StatusForbidden, mutate: func(t *testing.T, repo repository.Repository, task *model.Task, _ string) {
			replacement := uuid.NewString()
			task.CurrentExecutionID = &replacement
			if err := repo.Tasks().Update(context.Background(), task); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "terminal task", requestedID: func(current string) string { return current }, wantStatus: fiber.StatusForbidden, mutate: func(t *testing.T, repo repository.Repository, task *model.Task, _ string) {
			task.Status = model.TaskStatusFailed
			if err := repo.Tasks().Update(context.Background(), task); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "completed execution", requestedID: func(current string) string { return current }, wantStatus: fiber.StatusForbidden, mutate: func(t *testing.T, repo repository.Repository, _ *model.Task, executionID string) {
			transitioned, err := repo.TaskExecutions().Transition(context.Background(), executionID, []string{model.TaskExecutionRunning}, model.TaskExecutionFailed, model.ExecutionTransition{TerminalReason: "already terminal"})
			if err != nil || !transitioned {
				t.Fatalf("terminal execution transition = %v/%v", transitioned, err)
			}
		}},
		{name: "wrong target", target: "kubernetes", requestedID: func(current string) string { return current }, wantStatus: fiber.StatusForbidden},
		{name: "missing execution record", requestedID: func(string) string { return "missing-execution" }, wantStatus: fiber.StatusForbidden, mutate: func(t *testing.T, repo repository.Repository, task *model.Task, _ string) {
			missing := "missing-execution"
			task.CurrentExecutionID = &missing
			if err := repo.Tasks().Update(context.Background(), task); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target := tt.target
			if target == "" {
				target = model.ExecutionTargetLocalClaimed
			}
			app, repo, task, executionID, _, rawAPIKey, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformArticle, "", target, model.TaskExecutionRunning)
			if tt.mutate != nil {
				tt.mutate(t, repo, task, executionID)
			}
			beforeTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			beforeExecution, err := repo.TaskExecutions().FindByID(context.Background(), executionID)
			if err != nil {
				t.Fatal(err)
			}
			requestedID := tt.requestedID(executionID)
			body := `{"task_id":"` + task.ID + `"`
			if requestedID != "" {
				body += `,"execution_id":"` + requestedID + `"`
			}
			body += `,"result":{"success":false,"error":"late"}}`
			req := agentJSONRequest("/agent/complete", body)
			req.Header.Set("Authorization", "Bearer "+rawAPIKey)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want %d", resp.StatusCode, responseBody, tt.wantStatus)
			}
			persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != beforeTask.Status || !sameStringPointer(persisted.Result, beforeTask.Result) || !sameTimePointer(persisted.CompletedAt, beforeTask.CompletedAt) || persisted.ErrorMessage != beforeTask.ErrorMessage || persisted.BillingTerminalReason != beforeTask.BillingTerminalReason || !sameStringPointer(persisted.CurrentExecutionID, beforeTask.CurrentExecutionID) {
				t.Fatalf("rejected completion changed task: before=%#v after=%#v", beforeTask, persisted)
			}
			persistedExecution, err := repo.TaskExecutions().FindByID(context.Background(), executionID)
			if err != nil {
				t.Fatal(err)
			}
			if persistedExecution.Status != beforeExecution.Status || !sameTimePointer(persistedExecution.CompletedAt, beforeExecution.CompletedAt) || string(persistedExecution.Result) != string(beforeExecution.Result) || persistedExecution.FinalizationStatus != beforeExecution.FinalizationStatus {
				t.Fatalf("rejected completion changed execution: before=%#v after=%#v", beforeExecution, persistedExecution)
			}
		})
	}
}

func sameStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameTimePointer(left, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func TestAgentExecutionJWTRejectsConflictingCompletionBodyExecution(t *testing.T) {
	app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformArticle, "", model.ExecutionTargetLocalClaimed, model.TaskExecutionRunning)
	body := `{"task_id":"` + task.ID + `","execution_id":"` + uuid.NewString() + `","result":{"success":false,"error":"must reject"}}`
	req := agentJSONRequest("/agent/complete", body)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.TaskStatusRunning || persisted.Result != nil || persisted.CompletedAt != nil {
		t.Fatalf("conflicting JWT completion changed task: status=%q result=%v completed_at=%v", persisted.Status, persisted.Result, persisted.CompletedAt)
	}
}

func TestAgentAPIKeyCurrentLocalCompletionFailureFinalizesTask(t *testing.T) {
	app, repo, task, executionID, _, rawAPIKey, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformArticle, "", model.ExecutionTargetLocalClaimed, model.TaskExecutionRunning)
	body := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","result":{"success":false,"error":"agent failed"}}`
	req := agentJSONRequest("/agent/complete", body)
	req.Header.Set("Authorization", "Bearer "+rawAPIKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 200", resp.StatusCode, responseBody)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.TaskStatusFailed || persisted.ErrorMessage != "agent failed" {
		t.Fatalf("task = status:%q error:%q, want failed/agent failed", persisted.Status, persisted.ErrorMessage)
	}
}

func TestAgentCompletionErrorResponseDistinguishesStaleFromInternal(t *testing.T) {
	for _, tt := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "stale CAS loss", err: fmt.Errorf("finalize: %w", service.ErrStaleTaskExecution), wantStatus: fiber.StatusConflict, wantBody: "task execution is no longer current"},
		{name: "ordinary repository error", err: errors.New("database unavailable"), wantStatus: fiber.StatusInternalServerError, wantBody: "complete failed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status, body := agentCompletionErrorResponse(tt.err)
			if status != tt.wantStatus || body != tt.wantBody {
				t.Fatalf("response = %d/%q, want %d/%q", status, body, tt.wantStatus, tt.wantBody)
			}
		})
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
	key := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/executions/" + executionID + "/artifacts/workspace/sha256/" + strings.Repeat("a", 64) + "/output/content.md"
	store.stats = map[string]*storage.ObjectInfo{key: {Key: key, Size: 7, ContentType: "text/markdown", SHA256: strings.Repeat("a", 64)}}
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
	return setupExecutionScopedAgentAppForPack(t, model.PlatformArticle, "")
}

func setupExecutionScopedAgentAppForPack(t *testing.T, taskType, packID string) (*fiber.App, repository.Repository, *model.Task, string, string, string, *fakeAgentArtifactStorage) {
	return setupExecutionScopedAgentAppForPackAndTarget(t, taskType, packID, "kubernetes")
}

func setupExecutionScopedAgentAppForPackAndTarget(t *testing.T, taskType, packID, target string) (*fiber.App, repository.Repository, *model.Task, string, string, string, *fakeAgentArtifactStorage) {
	return setupExecutionScopedAgentAppForPackTargetAndStatus(t, taskType, packID, target, model.TaskExecutionRunning)
}

func setupExecutionScopedAgentAppForPackTargetAndStatus(t *testing.T, taskType, packID, target, executionStatus string) (*fiber.App, repository.Repository, *model.Task, string, string, string, *fakeAgentArtifactStorage) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID, taskID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: uuid.NewString() + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: taskType, Name: "P", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: taskType, Status: model.TaskStatusRunning, ExecutionTarget: target, CurrentExecutionID: &executionID}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	execution := &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: target, Status: executionStatus, Started: true, StartedAt: &now, RuntimeInstanceID: "pod-1"}
	if packID != "" {
		pack, ok := agentpack.Default().Pack(packID)
		if !ok {
			t.Fatalf("embedded %s Pack missing", packID)
		}
		execution.AgentPackID = pack.ID
		execution.AgentPackVersion = pack.Version
		execution.AgentPackDigest = pack.Digest
		execution.RuntimeAdapter = pack.Runtime.Adapter
		execution.RuntimeProfile = pack.Runtime.Profile
		progressContract, err := json.Marshal(pack.Progress)
		if err != nil {
			t.Fatalf("marshal progress contract: %v", err)
		}
		execution.AgentPackProgressContract = datatypes.JSON(progressContract)
	}
	if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	store := &executionScopeTestStore{fakeAgentArtifactStorage: &fakeAgentArtifactStorage{}}
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
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
	app := fiber.New(fiber.Config{StreamRequestBody: true})
	app.Post("/agent/progress", h.AuthMiddleware, h.Progress)
	app.Post("/agent/upload", h.AuthMiddleware, h.Upload)
	app.Post("/agent/artifacts/prepare", h.AuthMiddleware, h.PrepareArtifactUpload)
	app.Post("/agent/artifacts/content", h.AuthMiddleware, h.StreamArtifactContent)
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

func TestAgentStructuredProgressPersistsFrozenPackStage(t *testing.T) {
	app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "moments")

	body := `{"task_id":"` + task.ID + `","stage":"writing","state":"complete","title":"朋友圈正文","description":"正文已生成","progress_percent":55}`
	req := agentJSONRequest("/agent/progress", body)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("structured progress status/body = %d/%s", resp.StatusCode, responseBody)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	latest := persisted.LatestProgress.Data()
	if persisted.LastHeartbeatAt == nil || persisted.Progress != 55 || latest.Stage != "writing" || latest.State != "complete" || latest.Title != "朋友圈正文" || latest.Description != "正文已生成" || latest.Percent != 55 {
		t.Fatalf("persisted structured progress = heartbeat:%v progress:%d latest:%#v", persisted.LastHeartbeatAt, persisted.Progress, latest)
	}
}

func TestAgentAPIKeyStructuredProgressRequiresCurrentLocalExecution(t *testing.T) {
	tests := []struct {
		name            string
		executionID     func(current string) string
		staleCurrent    bool
		executionStatus string
		wantStatus      int
	}{
		{name: "current", executionID: func(current string) string { return current }, wantStatus: fiber.StatusOK},
		{name: "missing", executionID: func(string) string { return "" }, wantStatus: fiber.StatusBadRequest},
		{name: "mismatched", executionID: func(string) string { return uuid.NewString() }, wantStatus: fiber.StatusForbidden},
		{name: "stale", executionID: func(current string) string { return current }, staleCurrent: true, wantStatus: fiber.StatusForbidden},
		{name: "non-running execution", executionID: func(current string) string { return current }, executionStatus: model.TaskExecutionFailed, wantStatus: fiber.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executionStatus := tt.executionStatus
			if executionStatus == "" {
				executionStatus = model.TaskExecutionRunning
			}
			app, repo, task, executionID, _, rawAPIKey, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformMoments, "moments", model.ExecutionTargetLocalClaimed, executionStatus)
			if tt.staleCurrent {
				stale := uuid.NewString()
				task.CurrentExecutionID = &stale
			}
			if err := repo.Tasks().Update(context.Background(), task); err != nil {
				t.Fatal(err)
			}
			requestedExecutionID := tt.executionID(executionID)
			body := `{"task_id":"` + task.ID + `","execution_id":"` + requestedExecutionID + `","message":"must not persist when rejected","logs":["must not persist when rejected"],"stage":"writing","state":"complete","title":"朋友圈正文","description":"正文已生成","progress_percent":55}`
			req := agentJSONRequest("/agent/progress", body)
			req.Header.Set("Authorization", "Bearer "+rawAPIKey)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want %d", resp.StatusCode, responseBody, tt.wantStatus)
			}
			persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			persistedExecution, err := repo.TaskExecutions().FindByID(context.Background(), executionID)
			if err != nil {
				t.Fatal(err)
			}
			latestProgress := persisted.LatestProgress.Data()
			if tt.wantStatus == fiber.StatusOK {
				if persisted.Progress != 55 || persisted.LastHeartbeatAt == nil || persistedExecution.LastHeartbeatAt == nil {
					t.Fatalf("progress/heartbeats = %d/%v/%v, want 55 and both heartbeats", persisted.Progress, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
				}
			} else if persisted.Progress != 0 || persisted.ProgressSequence != 0 || latestProgress != (model.ProgressPayload{}) || persisted.ProgressLog != "" || persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
				t.Fatalf("rejected request caused side effects: progress=%d sequence=%d latest=%#v log=%q heartbeats=%v/%v", persisted.Progress, persisted.ProgressSequence, latestProgress, persisted.ProgressLog, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
			}
		})
	}
}

func TestAgentExecutionJWTRejectsConflictingProgressBodyExecution(t *testing.T) {
	app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "moments")
	body := `{"task_id":"` + task.ID + `","execution_id":"` + uuid.NewString() + `","stage":"writing","state":"complete","title":"朋友圈正文","progress_percent":55}`
	req := agentJSONRequest("/agent/progress", body)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 403", resp.StatusCode, responseBody)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Progress != 0 || persisted.LastHeartbeatAt != nil {
		t.Fatalf("conflicting JWT body caused side effects: progress=%d heartbeat=%v", persisted.Progress, persisted.LastHeartbeatAt)
	}
}

func TestAgentStructuredProgressRejectsUnknownFrozenPackStage(t *testing.T) {
	app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "moments")

	body := `{"task_id":"` + task.ID + `","stage":"not_declared","state":"complete","title":"未知","progress_percent":70}`
	req := agentJSONRequest("/agent/progress", body)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode < 400 || resp.StatusCode >= 500 {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("unknown stage status/body = %d/%s, want 4xx", resp.StatusCode, responseBody)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LastHeartbeatAt == nil {
		t.Fatal("structured validation failure did not refresh heartbeat")
	}
	if persisted.Progress != 0 || persisted.LatestProgress.Data().Stage != "" {
		t.Fatalf("unknown stage changed progress: %d/%#v", persisted.Progress, persisted.LatestProgress.Data())
	}
}

func TestAgentStructuredProgressIntentRequiresStage(t *testing.T) {
	tests := []struct {
		name   string
		fields string
	}{
		{name: "whitespace stage", fields: `"stage":"   ","state":"active","message":"must not persist"`},
		{name: "state only", fields: `"state":"active","message":"must not persist"`},
		{name: "title only", fields: `"title":"朋友圈正文","message":"must not persist"`},
		{name: "description only", fields: `"description":"正文已生成","message":"must not persist"`},
		{name: "percent only", fields: `"progress_percent":55,"message":"must not persist"`},
		{name: "missing state", fields: `"stage":"writing","title":"朋友圈正文","progress_percent":35,"message":"must not persist"`},
		{name: "invalid state", fields: `"stage":"writing","state":"paused","title":"朋友圈正文","progress_percent":35,"message":"must not persist"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo, task, _, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "moments")
			body := `{"task_id":"` + task.ID + `",` + tt.fields + `}`
			req := agentJSONRequest("/agent/progress", body)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("structured intent status/body = %d/%s, want 400", resp.StatusCode, responseBody)
			}
			persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Progress != 0 || persisted.LatestProgress.Data().Stage != "" || persisted.ProgressLog != "" {
				t.Fatalf("invalid structured intent changed task: progress=%d latest=%#v log=%q", persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog)
			}
		})
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
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
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
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetExecutorMaxTurns(nil)
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
	uploadMetadata    map[string]string
	uploadedKey       string
	uploadedBody      []byte
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
func (f *fakeAgentArtifactStorage) UploadURLWithMetadata(_ context.Context, key, contentType string, metadata map[string]string, _ int) (string, error) {
	f.uploadKey = key
	f.uploadContentType = contentType
	f.uploadMetadata = metadata
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
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectNotFound, key)
	}
	cp := *f.stats[key]
	return &cp, nil
}

func (f *fakeAgentArtifactStorage) PromoteObject(_ context.Context, sourceKey, finalKey, expectedETag string) (*storage.ObjectInfo, error) {
	source := f.stats[sourceKey]
	if source == nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectNotFound, sourceKey)
	}
	if source.ETag != expectedETag {
		return nil, fmt.Errorf("%w: %s", storage.ErrPromotionPreconditionFailed, sourceKey)
	}
	if f.stats[finalKey] != nil {
		return nil, fmt.Errorf("%w: %s", storage.ErrObjectAlreadyExists, finalKey)
	}
	copy := *source
	copy.Key = finalKey
	f.stats[finalKey] = &copy
	return &copy, nil
}

func TestAgentArtifactManifestErrorTaxonomy(t *testing.T) {
	app, _, task, store, rawKey, _ := setupAgentArtifactApp(t)
	wantKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/workspace/sha256/" + strings.Repeat("a", 64) + "/output/article.md"
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
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
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
	body := []byte("# title\n\nbody")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	prepareBody := `{"task_id":"` + task.ID + `","relative_path":"output/article.md","filename":"article.md","content_type":"text/markdown","size":13,"sha256":"` + hash + `"}`
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
	stagingKey := env.Data.Key
	stagingPrefix := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/staging/sha256/" + hash + "/"
	if !strings.HasPrefix(stagingKey, stagingPrefix) || !strings.HasSuffix(stagingKey, "/output/article.md") || store.uploadKey != stagingKey || store.uploadContentType != "text/markdown" {
		t.Fatalf("prepared key/store = %q/%q/%q, want scoped staging key with prefix %q", stagingKey, store.uploadKey, store.uploadContentType, stagingPrefix)
	}
	if store.uploadMetadata[storage.ObjectMetadataSHA256] != hash {
		t.Fatalf("signed upload metadata = %#v, want sha256 %q", store.uploadMetadata, hash)
	}
	if env.Data.STSAccessKeyID != "sts-ak" || env.Data.STSSecurityToken != "sts-token" || env.Data.STSAccessKeySecret == "" {
		t.Fatalf("missing sts credentials: %#v", env.Data)
	}

	store.stats = map[string]*storage.ObjectInfo{
		stagingKey: {Key: stagingKey, Size: int64(len(body)), ContentType: "text/markdown", ETag: "etag", SHA256: hash},
	}
	manifestBody := `{"task_id":"` + task.ID + `","files":[{"relative_path":"output/article.md","object_key":"` + stagingKey + `","content_type":"text/markdown","size":` + "13" + `,"sha256":"` + hash + `"}]}`
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
	finalKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/artifacts/workspace/sha256/" + hash + "/output/article.md"
	if len(files) != 1 || files[0].OSSKey != finalKey {
		t.Fatalf("task files = %#v, want one file with immutable key %q", files, finalKey)
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

func TestAgentClaim_RequiresCurrentAgentPackContract(t *testing.T) {
	app, _, rawKey, _, _ := setupAgentClaimApp(t)
	for _, body := range []string{
		`{"executor_info":{"hostname":"legacy"}}`,
		`{"agent_pack_contract_version":1,"executor_info":{"hostname":"legacy-v1"}}`,
		`{"agent_pack_contract_version":3,"executor_info":{"hostname":"future"}}`,
	} {
		req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+rawKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("claim failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusUpgradeRequired {
			responseBody, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d, want 426; body=%s", resp.StatusCode, responseBody)
		}
	}
}

// TestAgentClaim_ReturnsConfigThenNoContent confirms a valid claim returns the
// full task config, and a follow-up claim returns 204 (nothing left).
func TestAgentClaim_ReturnsConfigThenNoContent(t *testing.T) {
	app, repo, rawKey, userID, projectID := setupAgentClaimApp(t)
	taskID := seedClaimableLocalTask(t, repo, userID, projectID)

	// First claim: should return 200 + config carrying the task id.
	req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"agent_pack_contract_version":2,"executor_info":{"hostname":"mbp"}}`))
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
	claimed, err := repo.Tasks().FindByID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.CurrentExecutionID == nil || !strings.Contains(string(body), `"execution_id":"`+*claimed.CurrentExecutionID+`"`) {
		t.Fatalf("response body does not carry current execution id %v: %s", claimed.CurrentExecutionID, body)
	}

	// Second claim: nothing claimable → 204 No Content.
	req2 := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"agent_pack_contract_version":2}`))
	req2.Header.Set("Authorization", "Bearer "+rawKey)
	req2.Header.Set("Content-Type", "application/json")
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
	req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"agent_pack_contract_version":2}`))
	req.Header.Set("Authorization", "Bearer "+rawKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("claim failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}
