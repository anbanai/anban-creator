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
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
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
	h := NewAgentHandler(nil, nil, &logger)
	h.SetExecutionTokenService(tokens)
	verifier := &testWorkloadVerifier{identity: &serveragent.WorkloadIdentity{Target: "docker", RuntimeIdentity: model.RuntimeIdentity{Scope: "docker", Workload: "exec-1", InstanceID: "container-id"}, ExecutionID: "execution-1"}}
	h.SetBootstrap(verifier, testBootstrapper{response: &service.AgentBootstrapResponse{ExecutionID: "execution-1", TaskID: "task-1", ExecutionToken: "execution-token"}})
	app := fiber.New()
	app.Post("/agent/scoped", h.ExecutionAuthMiddleware, func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"user": c.Locals(agentUserIDContextKey), "project": c.Locals(agentProjectIDContextKey), "task": c.Locals(agentTaskIDContextKey), "execution": c.Locals(agentExecutionIDContextKey)})
	})
	app.Post("/agent/claim", h.ExecutionAuthMiddleware, h.Claim)
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
	req.Header.Set("X-Anban-Agent-Contract-Version", "1")
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

func TestAgentBootstrapRequiresRuntimeContractHeader(t *testing.T) {
	for _, version := range []string{"", "0", "2", "v1", "1.0"} {
		t.Run("version_"+version, func(t *testing.T) {
			logger := zerolog.New(io.Discard)
			h := NewAgentHandler(nil, nil, &logger)
			h.SetBootstrap(&testWorkloadVerifier{identity: &serveragent.WorkloadIdentity{ExecutionID: "execution-1"}}, testBootstrapper{response: &service.AgentBootstrapResponse{ExecutionID: "execution-1"}})
			app := fiber.New()
			app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)

			req := httptest.NewRequest(http.MethodPost, "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
			req.Header.Set("Authorization", "Bearer workload-token")
			req.Header.Set("Content-Type", "application/json")
			if version != "" {
				req.Header.Set("X-Anban-Agent-Contract-Version", version)
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusUpgradeRequired {
				t.Fatalf("runtime contract header %q status = %d, want %d", version, resp.StatusCode, fiber.StatusUpgradeRequired)
			}
			var body struct {
				ErrorCode string `json:"error_code"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.ErrorCode != "agent_runtime_upgrade_required" {
				t.Fatalf("error_code = %q", body.ErrorCode)
			}
		})
	}
}

func TestAgentBootstrapRejectsMismatchedResponseExecutionID(t *testing.T) {
	logger := zerolog.New(io.Discard)
	h := NewAgentHandler(nil, nil, &logger)
	h.SetBootstrap(&testWorkloadVerifier{identity: &serveragent.WorkloadIdentity{ExecutionID: "execution-1"}}, testBootstrapper{response: &service.AgentBootstrapResponse{ExecutionID: "execution-2"}})
	app := fiber.New()
	app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)

	req := httptest.NewRequest(http.MethodPost, "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("X-Anban-Agent-Contract-Version", "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusConflict)
	}
}

func TestAgentExecutionAuthRejectsAPIKeysAndConflictingCredentials(t *testing.T) {
	logger := zerolog.New(io.Discard)
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	h := NewAgentHandler(nil, nil, &logger)
	h.SetExecutionTokenService(tokens)
	app := fiber.New()
	app.Post("/agent", h.ExecutionAuthMiddleware, func(c fiber.Ctx) error { return c.SendStatus(204) })
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
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("API key status = %d, want 401", resp.StatusCode)
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
		t.Fatalf("simultaneous API key credentials status = %d, want 401", resp.StatusCode)
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
			h := NewAgentHandler(nil, nil, &logger)
			h.SetBootstrap(verifier, testBootstrapper{err: tc.err})
			app := fiber.New()
			app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)
			req := httptest.NewRequest(http.MethodPost, "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
			req.Header.Set("Authorization", "Bearer workload-token")
			req.Header.Set("X-Anban-Agent-Contract-Version", "1")
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
	h := NewAgentHandler(nil, nil, &logger)
	h.SetBootstrap(verifier, testBootstrapper{})
	app := fiber.New()
	app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)
	req := httptest.NewRequest(http.MethodPost, "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("X-Anban-Agent-Contract-Version", "1")
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

func TestAgentBootstrapLogsRuntimeContractMismatch(t *testing.T) {
	var logs bytes.Buffer
	logger := zerolog.New(&logs)
	h := NewAgentHandler(nil, nil, &logger)
	h.SetBootstrap(&testWorkloadVerifier{identity: &serveragent.WorkloadIdentity{ExecutionID: "execution-1"}}, testBootstrapper{})
	app := fiber.New()
	app.Post("/agent/bootstrap", h.WorkloadAuthMiddleware, h.Bootstrap)

	req := httptest.NewRequest(http.MethodPost, "/agent/bootstrap", strings.NewReader(`{"execution_id":"execution-1"}`))
	req.Header.Set("Authorization", "Bearer workload-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d", resp.StatusCode, fiber.StatusUpgradeRequired)
	}
	line := logs.String()
	for _, field := range []string{"agent bootstrap rejected: runtime contract mismatch", `"execution_id":"execution-1"`, `"received_contract_version":""`, `"expected_contract_version":"1"`} {
		if !strings.Contains(line, field) {
			t.Fatalf("runtime contract diagnostic missing %q in %s", field, line)
		}
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
				wantRejectedStatus := fiber.StatusForbidden
				if endpoint.name == "complete" && mode == "terminal" {
					wantRejectedStatus = fiber.StatusConflict
				}
				if mode != "current" && resp.StatusCode != wantRejectedStatus {
					t.Fatalf("%s status = %d, want %d", mode, resp.StatusCode, wantRejectedStatus)
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

func TestAgentAPIKeyCannotReportProgress(t *testing.T) {
	app, repo, task, _, _, rawAPIKey, _ := setupExecutionScopedAgentApp(t)
	req := agentJSONRequest("/agent/progress", `{"task_id":"`+task.ID+`","message":"local progress","logs":["first log","second log"]}`)
	req.Header.Set("Authorization", "Bearer "+rawAPIKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("API-key progress status = %d, want 401", resp.StatusCode)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProgressLog != "" {
		t.Fatalf("rejected API-key progress wrote log %q", persisted.ProgressLog)
	}
	if persisted.LastHeartbeatAt != nil {
		t.Fatal("rejected API-key progress refreshed heartbeat")
	}
}

func TestAgentAPIKeyCannotCompleteExecutionsWithoutSideEffects(t *testing.T) {
	for _, tt := range []struct {
		name        string
		target      string
		requestedID func(string) string
		mutate      func(t *testing.T, repo repository.Repository, task *model.Task, executionID string)
	}{
		{name: "missing identity", requestedID: func(string) string { return "" }},
		{name: "unknown identity", requestedID: func(string) string { return uuid.NewString() }},
		{name: "replaced task execution", requestedID: func(current string) string { return current }, mutate: func(t *testing.T, repo repository.Repository, task *model.Task, _ string) {
			replacement := uuid.NewString()
			task.CurrentExecutionID = &replacement
			if err := repo.Tasks().Update(context.Background(), task); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "terminal task", requestedID: func(current string) string { return current }, mutate: func(t *testing.T, repo repository.Repository, task *model.Task, _ string) {
			task.Status = model.TaskStatusFailed
			if err := repo.Tasks().Update(context.Background(), task); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "completed execution", requestedID: func(current string) string { return current }, mutate: func(t *testing.T, repo repository.Repository, _ *model.Task, executionID string) {
			transitioned, err := repo.TaskExecutions().Transition(context.Background(), executionID, []string{model.TaskExecutionRunning}, model.TaskExecutionFailed, model.ExecutionTransition{TerminalReason: "already terminal"})
			if err != nil || !transitioned {
				t.Fatalf("terminal execution transition = %v/%v", transitioned, err)
			}
		}},
		{name: "wrong target", target: "kubernetes", requestedID: func(current string) string { return current }},
		{name: "missing execution record", requestedID: func(string) string { return "missing-execution" }, mutate: func(t *testing.T, repo repository.Repository, task *model.Task, _ string) {
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
			if resp.StatusCode != fiber.StatusUnauthorized {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want 401", resp.StatusCode, responseBody)
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

func TestAgentAPIKeyCannotCompleteCurrentLocalExecution(t *testing.T) {
	app, repo, task, executionID, _, rawAPIKey, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformArticle, "", model.ExecutionTargetLocalClaimed, model.TaskExecutionRunning)
	body := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","result":{"success":false,"error":"agent failed"}}`
	req := agentJSONRequest("/agent/complete", body)
	req.Header.Set("Authorization", "Bearer "+rawAPIKey)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 401", resp.StatusCode, responseBody)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.TaskStatusRunning || persisted.ErrorMessage != "" {
		t.Fatalf("rejected completion changed task: status=%q error=%q", persisted.Status, persisted.ErrorMessage)
	}
}

func TestAgentCompletionResponseLossRetryIsIdempotentForExecutionTokens(t *testing.T) {
	for _, test := range []struct {
		name   string
		target string
	}{
		{name: "cloud", target: "kubernetes"},
		{name: "local", target: model.ExecutionTargetLocalClaimed},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformArticle, "", test.target, model.TaskExecutionRunning)
			body := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","result":{"success":false,"error":"agent failed","tool_use_summary":{"Write":1}}}`
			for attempt := 1; attempt <= 2; attempt++ {
				req := agentJSONRequest("/agent/complete", body)
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != fiber.StatusOK {
					responseBody, _ := io.ReadAll(resp.Body)
					t.Fatalf("attempt %d status/body = %d/%s, want 200", attempt, resp.StatusCode, responseBody)
				}
			}

			conflict := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","result":{"success":false,"error":"different result"}}`
			req := agentJSONRequest("/agent/complete", conflict)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusConflict {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("conflict status/body = %d/%s, want 409", resp.StatusCode, responseBody)
			}
			persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil || persisted.Status != model.TaskStatusFailed || persisted.ErrorMessage != "agent failed" {
				t.Fatalf("terminal task after retries = %#v, %v", persisted, err)
			}
		})
	}
}

func TestAgentCompletionPersistsRecoverableFailureIdentity(t *testing.T) {
	app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformArticle, "", "kubernetes", model.TaskExecutionRunning)
	body := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","result":{"success":false,"error":"执行环境未建立，暂时无法生成或结算图片","terminal_reason":"platform_error","root_error_code":"execution_identity_unavailable","failure_stage":"image_generation","resume_from":"image_generation"}}`
	req := agentJSONRequest("/agent/complete", body)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 200", resp.StatusCode, responseBody)
	}

	foundTask, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundTask.Status != model.TaskStatusFailed || foundTask.ErrorMessage != "执行环境未建立，暂时无法生成或结算图片" || foundTask.Result == nil {
		t.Fatalf("task = %#v", foundTask)
	}
	var result serveragent.ExecutionResult
	if err := json.Unmarshal([]byte(*foundTask.Result), &result); err != nil {
		t.Fatal(err)
	}
	if result.RootErrorCode != "execution_identity_unavailable" || result.FailureStage != "image_generation" || result.ResumeFrom != "image_generation" {
		t.Fatalf("result = %#v", result)
	}
}

func TestAgentCompletionJWTFinalizesLocalExecutionTarget(t *testing.T) {
	app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformArticle, "", model.ExecutionTargetLocalClaimed, model.TaskExecutionRunning)
	req := agentJSONRequest("/agent/complete", `{"task_id":"`+task.ID+`","execution_id":"`+executionID+`","result":{"success":false,"error":"wrong target"}}`)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 200", resp.StatusCode, body)
	}
	persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil || persisted.Status != model.TaskStatusFailed || persisted.ErrorMessage != "wrong target" {
		t.Fatalf("local JWT completion task = %#v, %v", persisted, err)
	}
}

var errInjectedCompletionFindExecution = errors.New("injected completion execution lookup failure")

type completionCorruptResultRepository struct {
	repository.Repository
	suffix  string
	corrupt bool
}

func (r *completionCorruptResultRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&completionCorruptResultRepository{Repository: tx, suffix: r.suffix, corrupt: r.corrupt})
	})
}

func (r *completionCorruptResultRepository) TaskExecutions() repository.TaskExecutionRepository {
	return &completionCorruptResultTaskExecutions{TaskExecutionRepository: r.Repository.TaskExecutions(), parent: r}
}

type completionCorruptResultTaskExecutions struct {
	repository.TaskExecutionRepository
	parent *completionCorruptResultRepository
}

func (r *completionCorruptResultTaskExecutions) FindByID(ctx context.Context, id string) (*model.TaskExecution, error) {
	execution, err := r.TaskExecutionRepository.FindByID(ctx, id)
	if err != nil || !r.parent.corrupt {
		return execution, err
	}
	cloned := *execution
	cloned.Result = append(append([]byte(nil), execution.Result...), r.parent.suffix...)
	return &cloned, nil
}

func (r *completionCorruptResultTaskExecutions) FindByIDForUpdate(ctx context.Context, id string) (*model.TaskExecution, error) {
	execution, err := r.TaskExecutionRepository.FindByIDForUpdate(ctx, id)
	if err != nil || !r.parent.corrupt {
		return execution, err
	}
	cloned := *execution
	cloned.Result = append(append([]byte(nil), execution.Result...), r.parent.suffix...)
	return &cloned, nil
}

func TestAgentCompletionCorruptStoredResultReturns409ForExecutionTokens(t *testing.T) {
	for _, test := range []struct {
		name   string
		target string
	}{
		{name: "cloud", target: "kubernetes"},
		{name: "local", target: model.ExecutionTargetLocalClaimed},
	} {
		for _, suffix := range []string{" trailing", ` {"second":true}`} {
			t.Run(test.name+suffix, func(t *testing.T) {
				var corruptRepo *completionCorruptResultRepository
				app, _, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(
					t, model.PlatformArticle, "", test.target, model.TaskExecutionRunning,
					func(base repository.Repository) repository.Repository {
						corruptRepo = &completionCorruptResultRepository{Repository: base, suffix: suffix}
						return corruptRepo
					}, nil,
				)
				body := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","result":{"success":false,"error":"agent failed"}}`
				first := agentJSONRequest("/agent/complete", body)
				first.Header.Set("Authorization", "Bearer "+token)
				firstResp, err := app.Test(first)
				if err != nil {
					t.Fatal(err)
				}
				if firstResp.StatusCode != fiber.StatusOK {
					responseBody, _ := io.ReadAll(firstResp.Body)
					t.Fatalf("first completion status/body = %d/%s, want 200", firstResp.StatusCode, responseBody)
				}
				corruptRepo.corrupt = true
				retry := agentJSONRequest("/agent/complete", body)
				retry.Header.Set("Authorization", "Bearer "+token)
				resp, err := app.Test(retry)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != fiber.StatusConflict {
					responseBody, _ := io.ReadAll(resp.Body)
					t.Fatalf("retry status/body = %d/%s, want 409", resp.StatusCode, responseBody)
				}
			})
		}
	}
}

type completionFindExecutionErrorRepository struct {
	repository.Repository
	findCalls int
}

func (r *completionFindExecutionErrorRepository) TaskExecutions() repository.TaskExecutionRepository {
	return &completionFindExecutionErrorTaskExecutions{TaskExecutionRepository: r.Repository.TaskExecutions(), parent: r}
}

type completionFindExecutionErrorTaskExecutions struct {
	repository.TaskExecutionRepository
	parent *completionFindExecutionErrorRepository
}

func (r *completionFindExecutionErrorTaskExecutions) FindByID(ctx context.Context, id string) (*model.TaskExecution, error) {
	r.parent.findCalls++
	if r.parent.findCalls == 2 {
		return nil, errInjectedCompletionFindExecution
	}
	return r.TaskExecutionRepository.FindByID(ctx, id)
}

func TestAgentCompletionRepositoryErrorAfterAuthorizationReturns500(t *testing.T) {
	app, _, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(
		t, model.PlatformArticle, "", "kubernetes", model.TaskExecutionRunning,
		func(base repository.Repository) repository.Repository {
			return &completionFindExecutionErrorRepository{Repository: base}
		}, nil,
	)
	req := agentJSONRequest("/agent/complete", `{"task_id":"`+task.ID+`","execution_id":"`+executionID+`","result":{"success":false,"error":"agent failed"}}`)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusInternalServerError || !strings.Contains(string(body), "complete failed") {
		t.Fatalf("status/body = %d/%s, want redacted 500", resp.StatusCode, body)
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
		{name: "completion conflict", err: fmt.Errorf("finalize: %w", service.ErrTaskCompletionConflict), wantStatus: fiber.StatusConflict, wantBody: "completion result conflicts with terminal outcome"},
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
		if resp.StatusCode != fiber.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s status/body = %d/%s, want 401", endpoint.path, resp.StatusCode, body)
		}
	}
	files, _ := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if len(files) != 0 || store.uploadKey != "" {
		t.Fatalf("rejected requests caused side effects: files=%#v upload=%q", files, store.uploadKey)
	}
}

func TestAgentAPIKeyCannotUseArtifactContractWithoutExecution(t *testing.T) {
	app, repo, task, store, rawAPIKey, _ := setupAgentArtifactApp(t)
	for _, endpoint := range []struct{ path, body string }{
		{"/agent/artifacts/prepare", `{"task_id":"` + task.ID + `","relative_path":"output/content.md","content_type":"text/markdown","size":7,"sha256":"` + strings.Repeat("a", 64) + `"}`},
		{"/agent/artifacts/manifest", `{"task_id":"` + task.ID + `","files":[]}`},
	} {
		req := agentJSONRequest(endpoint.path, endpoint.body)
		req.Header.Set("Authorization", "Bearer "+rawAPIKey)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("%s status/body = %d/%s, want 401", endpoint.path, resp.StatusCode, body)
		}
	}
	files, findErr := repo.TaskFiles().FindByTaskID(context.Background(), task.ID)
	if findErr != nil || len(files) != 0 || store.uploadKey != "" {
		t.Fatalf("rejected requests caused side effects: files=%#v err=%v upload=%q", files, findErr, store.uploadKey)
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
	return setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(t, taskType, packID, target, executionStatus, nil, nil)
}

func setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(t *testing.T, taskType, packID, target, executionStatus string, decorate func(repository.Repository) repository.Repository, pubsub *service.RedisPubSub) (*fiber.App, repository.Repository, *model.Task, string, string, string, *fakeAgentArtifactStorage) {
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
	taskExecutionTarget := model.ExecutionTargetCloud
	if target == model.ExecutionTargetLocalClaimed {
		taskExecutionTarget = model.ExecutionTargetLocalClaimed
	}
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: taskType, Status: model.TaskStatusRunning, ExecutionTarget: taskExecutionTarget, CurrentExecutionID: &executionID}
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
	taskServiceRepo := repo
	if decorate != nil {
		taskServiceRepo = decorate(repo)
	}
	taskSvc := newHandlerTaskService(t, taskServiceRepo, noopTaskEnqueuer{}, store, &logger, "", pubsub, nil)
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
	h := NewAgentHandler(taskSvc, apiKeys, &logger)
	h.SetExecutionTokenService(tokens)
	h.SetDirectUploadConfig(service.DirectUploadConfig{Storage: config.StorageConfig{Provider: "oss", BucketName: "bucket", STSRoleArn: "role"}, CredentialIssuer: service.StaticUploadCredentialIssuer(func(context.Context, service.UploadCredentialRequest) (*service.UploadCredential, error) {
		return &service.UploadCredential{AccessKeyID: "ak", AccessKeySecret: "secret", SecurityToken: "token", ExpiresAt: time.Now().Add(time.Minute)}, nil
	})})
	app := fiber.New(fiber.Config{StreamRequestBody: true})
	app.Post("/agent/progress", h.ExecutionAuthMiddleware, h.Progress)
	app.Post("/agent/artifacts/prepare", h.ExecutionAuthMiddleware, h.PrepareArtifactUpload)
	app.Post("/agent/artifacts/content", h.ExecutionAuthMiddleware, h.StreamArtifactContent)
	app.Post("/agent/artifacts/manifest", h.ExecutionAuthMiddleware, h.ReportArtifactManifest)
	app.Post("/agent/complete", h.ExecutionAuthMiddleware, h.Complete)
	return app, repo, task, executionID, token, rawAPIKey, store.fakeAgentArtifactStorage
}

type beforeStructuredProgressTxRepository struct {
	repository.Repository
	once   sync.Once
	before func()
}

var errInjectedHandlerProgressCASLookup = errors.New("injected handler progress CAS lookup failure")

type progressCASLookupErrorRepository struct {
	repository.Repository
}

func (r *progressCASLookupErrorRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&progressCASLookupErrorTx{Repository: tx})
	})
}

type progressCASLookupErrorTx struct {
	repository.Repository
}

func (r *progressCASLookupErrorTx) Tasks() repository.TaskRepository {
	return &progressCASLookupErrorTasks{TaskRepository: r.Repository.Tasks()}
}

type progressCASLookupErrorTasks struct {
	repository.TaskRepository
}

func (r *progressCASLookupErrorTasks) AdvanceStructuredProgress(context.Context, string, string, int, model.ProgressPayload) (bool, model.ProgressPayload, error) {
	return false, model.ProgressPayload{}, nil
}

func (r *progressCASLookupErrorTasks) FindByIDForUpdate(context.Context, string) (*model.Task, error) {
	return nil, errInjectedHandlerProgressCASLookup
}

func newAgentProgressTestPubSub(t *testing.T) (*service.RedisPubSub, *miniredis.Miniredis) {
	t.Helper()
	miniRedis := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: miniRedis.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	logger := zerolog.New(io.Discard)
	return service.NewRedisPubSub(rdb, &logger), miniRedis
}

func subscribeAgentProgressTest(t *testing.T, ctx context.Context, pubsub *service.RedisPubSub, miniRedis *miniredis.Miniredis, taskID string) *service.ProgressSubscriber {
	t.Helper()
	subscriber := pubsub.SubscribeProgress(ctx, taskID)
	t.Cleanup(func() { _ = subscriber.Close() })
	channel := "anban:task:progress:" + taskID
	deadline := time.Now().Add(time.Second)
	for miniRedis.PubSubNumSub(channel)[channel] != 1 {
		if time.Now().After(deadline) {
			t.Fatal("timed out establishing progress subscription")
		}
	}
	return subscriber
}

func assertNoAgentProgressEvent(t *testing.T, subscriber *service.ProgressSubscriber) {
	t.Helper()
	select {
	case event := <-subscriber.Events():
		t.Fatalf("rejected structured request published SSE payload: %s", event.Payload)
	case <-time.After(500 * time.Millisecond):
	}
}

func (r *beforeStructuredProgressTxRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	r.once.Do(r.before)
	return r.Repository.WithTx(ctx, fn)
}

func TestAgentStructuredProgressRejectsExecutionThatBecomesStaleAfterAuthorization(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(context.Context, repository.Repository, *model.Task, string) error
	}{
		{
			name: "execution replaced",
			mutate: func(ctx context.Context, repo repository.Repository, task *model.Task, _ string) error {
				_, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, uuid.NewString())
				return err
			},
		},
		{
			name: "task terminal",
			mutate: func(ctx context.Context, repo repository.Repository, task *model.Task, _ string) error {
				return repo.Tasks().UpdateStatus(ctx, task.ID, model.TaskStatusCancelled)
			},
		},
		{
			name: "execution terminal",
			mutate: func(ctx context.Context, repo repository.Repository, _ *model.Task, executionID string) error {
				changed, err := repo.TaskExecutions().Transition(ctx, executionID, []string{model.TaskExecutionRunning}, model.TaskExecutionFailed, model.ExecutionTransition{})
				if err != nil {
					return err
				}
				if !changed {
					return errors.New("execution terminal transition lost")
				}
				return nil
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pubsub, miniRedis := newAgentProgressTestPubSub(t)
			var hookErr error
			var hookCalls int
			var taskForHook *model.Task
			var executionIDForHook string
			app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(
				t, model.PlatformMoments, "moments", "kubernetes", model.TaskExecutionRunning,
				func(base repository.Repository) repository.Repository {
					return &beforeStructuredProgressTxRepository{Repository: base, before: func() {
						hookCalls++
						hookErr = tt.mutate(ctx, base, taskForHook, executionIDForHook)
					}}
				}, pubsub,
			)
			taskForHook = task
			executionIDForHook = executionID
			subscriber := subscribeAgentProgressTest(t, ctx, pubsub, miniRedis, task.ID)
			body := `{"task_id":"` + task.ID + `","message":"raw message","logs":["raw log"],"stage":"writing","state":"complete","title":"朋友圈正文","description":"must not persist","progress_percent":55}`
			req := agentJSONRequest("/agent/progress", body)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if hookErr != nil || hookCalls != 1 {
				t.Fatalf("transaction hook calls/error = %d/%v", hookCalls, hookErr)
			}
			if resp.StatusCode != fiber.StatusConflict {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want 409", resp.StatusCode, responseBody)
			}
			persisted, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			persistedExecution, err := repo.TaskExecutions().FindByID(ctx, executionID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ProgressSequence != 0 || persisted.Progress != 0 || persisted.LatestProgress.Data() != (model.ProgressPayload{}) || persisted.ProgressLog != "" || persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
				t.Fatalf("stale structured request caused side effects: sequence=%d progress=%d latest=%#v log=%q heartbeats=%v/%v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
			}
			assertNoAgentProgressEvent(t, subscriber)
		})
	}
}

func TestAgentStructuredProgressCASLookupErrorReturns500WithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	pubsub, miniRedis := newAgentProgressTestPubSub(t)
	app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(
		t, model.PlatformMoments, "moments", "kubernetes", model.TaskExecutionRunning,
		func(base repository.Repository) repository.Repository {
			return &progressCASLookupErrorRepository{Repository: base}
		}, pubsub,
	)
	subscriber := subscribeAgentProgressTest(t, ctx, pubsub, miniRedis, task.ID)
	req := agentJSONRequest("/agent/progress", `{"task_id":"`+task.ID+`","message":"raw message","logs":["raw log"],"stage":"writing","state":"complete","title":"朋友圈正文","progress_percent":55}`)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 500", resp.StatusCode, responseBody)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	persistedExecution, err := repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProgressSequence != 0 || persisted.Progress != 0 || persisted.LatestProgress.Data() != (model.ProgressPayload{}) || persisted.ProgressLog != "" || persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
		t.Fatalf("CAS lookup error caused side effects: sequence=%d progress=%d latest=%#v log=%q heartbeats=%v/%v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
	}
	assertNoAgentProgressEvent(t, subscriber)
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

func TestAgentExecutionTokenStructuredProgressRequiresCurrentExecution(t *testing.T) {
	tests := []struct {
		name            string
		executionID     func(current string) string
		staleCurrent    bool
		executionStatus string
		wantStatus      int
	}{
		{name: "current", executionID: func(current string) string { return current }, wantStatus: fiber.StatusOK},
		{name: "body identity omitted", executionID: func(string) string { return "" }, wantStatus: fiber.StatusOK},
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
			app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatus(t, model.PlatformMoments, "moments", model.ExecutionTargetLocalClaimed, executionStatus)
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
			req.Header.Set("Authorization", "Bearer "+token)
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
	app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "moments")

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
	persistedExecution, err := repo.TaskExecutions().FindByID(context.Background(), executionID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
		t.Fatalf("structured validation failure refreshed heartbeat: task=%v execution=%v", persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
	}
	if persisted.Progress != 0 || persisted.LatestProgress.Data().Stage != "" || persisted.ProgressLog != "" {
		t.Fatalf("unknown stage changed progress: %d/%#v/%q", persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog)
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
		{name: "invalid title", fields: `"stage":"writing","state":"active","title":"错误标题","progress_percent":35,"message":"must not persist"`},
		{name: "invalid percent", fields: `"stage":"writing","state":"active","title":"朋友圈正文","progress_percent":36,"message":"must not persist"`},
		{name: "null stage", fields: `"stage":null,"message":"must not persist"`},
		{name: "null state", fields: `"stage":"writing","state":null,"title":"朋友圈正文","progress_percent":35,"message":"must not persist"`},
		{name: "missing title", fields: `"stage":"writing","state":"active","progress_percent":35,"message":"must not persist"`},
		{name: "null title", fields: `"stage":"writing","state":"active","title":null,"progress_percent":35,"message":"must not persist"`},
		{name: "null description", fields: `"stage":"writing","state":"active","title":"朋友圈正文","description":null,"progress_percent":35,"message":"must not persist"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPack(t, model.PlatformMoments, "moments")
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
			persistedExecution, err := repo.TaskExecutions().FindByID(context.Background(), executionID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Progress != 0 || persisted.LatestProgress.Data().Stage != "" || persisted.ProgressLog != "" || persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
				t.Fatalf("invalid structured intent changed task: progress=%d latest=%#v log=%q heartbeats=%v/%v", persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
			}
		})
	}
}

type frozenProgressContractRepository struct {
	repository.Repository
	contract datatypes.JSON
}

func (r *frozenProgressContractRepository) TaskExecutions() repository.TaskExecutionRepository {
	return &frozenProgressContractExecutions{TaskExecutionRepository: r.Repository.TaskExecutions(), contract: r.contract}
}

type frozenProgressContractExecutions struct {
	repository.TaskExecutionRepository
	contract datatypes.JSON
}

func (r *frozenProgressContractExecutions) FindByID(ctx context.Context, id string) (*model.TaskExecution, error) {
	execution, err := r.TaskExecutionRepository.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	copy := *execution
	copy.AgentPackProgressContract = append(datatypes.JSON(nil), r.contract...)
	return &copy, nil
}

func TestAgentStructuredProgressRequiresExplicitPercentForZeroPercentStage(t *testing.T) {
	contract := datatypes.JSON(`[{"id":"zero","title":"Zero","active_percent":0,"complete_percent":0}]`)
	for _, tt := range []struct {
		name         string
		percentField string
	}{
		{name: "missing"},
		{name: "null", percentField: `,"progress_percent":null`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pubsub, miniRedis := newAgentProgressTestPubSub(t)
			app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(
				t, model.PlatformMoments, "moments", "kubernetes", model.TaskExecutionRunning,
				func(base repository.Repository) repository.Repository {
					return &frozenProgressContractRepository{Repository: base, contract: contract}
				}, pubsub,
			)
			subscriber := subscribeAgentProgressTest(t, ctx, pubsub, miniRedis, task.ID)
			body := `{"task_id":"` + task.ID + `","stage":"zero","state":"active","title":"Zero","message":"must not persist"` + tt.percentField + `}`
			req := agentJSONRequest("/agent/progress", body)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want 400", resp.StatusCode, responseBody)
			}
			persisted, err := repo.Tasks().FindByID(context.Background(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			persistedExecution, err := repo.TaskExecutions().FindByID(context.Background(), executionID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ProgressSequence != 0 || persisted.ProgressLog != "" || persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
				t.Fatalf("missing/null percent caused side effects: sequence=%d log=%q heartbeats=%v/%v", persisted.ProgressSequence, persisted.ProgressLog, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
			}
			assertNoAgentProgressEvent(t, subscriber)
		})
	}
}

func TestAgentExplicitNullStageIsStructuredAndHasNoSideEffects(t *testing.T) {
	ctx := context.Background()
	pubsub, miniRedis := newAgentProgressTestPubSub(t)
	app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(
		t, model.PlatformMoments, "moments", "kubernetes", model.TaskExecutionRunning, nil, pubsub,
	)
	subscriber := subscribeAgentProgressTest(t, ctx, pubsub, miniRedis, task.ID)
	req := agentJSONRequest("/agent/progress", `{"task_id":"`+task.ID+`","stage":null,"message":"must not persist"}`)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		responseBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 400", resp.StatusCode, responseBody)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	persistedExecution, err := repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.ProgressSequence != 0 || persisted.Progress != 0 || persisted.LatestProgress.Data() != (model.ProgressPayload{}) || persisted.ProgressLog != "" || persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
		t.Fatalf("null stage caused side effects: sequence=%d progress=%d latest=%#v log=%q heartbeats=%v/%v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
	}
	assertNoAgentProgressEvent(t, subscriber)
}

func TestAgentStructuredProgressRejectsNonCanonicalOrDuplicateJSONKeys(t *testing.T) {
	for _, tt := range []struct {
		name   string
		fields string
	}{
		{
			name:   "casing only",
			fields: `"Stage":"writing","State":"active","Title":"朋友圈正文","Progress_Percent":35`,
		},
		{
			name:   "canonical null mixed-case bypass",
			fields: `"stage":null,"Stage":"writing","state":"active","title":"朋友圈正文","progress_percent":35`,
		},
		{
			name:   "exact duplicate canonical key",
			fields: `"stage":"writing","stage":"writing","state":"active","title":"朋友圈正文","progress_percent":35`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pubsub, miniRedis := newAgentProgressTestPubSub(t)
			app, repo, task, executionID, token, _, _ := setupExecutionScopedAgentAppForPackTargetAndStatusWithRepositoryDecorator(
				t, model.PlatformMoments, "moments", "kubernetes", model.TaskExecutionRunning, nil, pubsub,
			)
			subscriber := subscribeAgentProgressTest(t, ctx, pubsub, miniRedis, task.ID)
			req := agentJSONRequest("/agent/progress", `{"task_id":"`+task.ID+`","message":"must not persist",`+tt.fields+`}`)
			req.Header.Set("Authorization", "Bearer "+token)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusBadRequest {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status/body = %d/%s, want 400", resp.StatusCode, responseBody)
			}
			persisted, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			persistedExecution, err := repo.TaskExecutions().FindByID(ctx, executionID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ProgressSequence != 0 || persisted.Progress != 0 || persisted.LatestProgress.Data() != (model.ProgressPayload{}) || persisted.ProgressLog != "" || persisted.LastHeartbeatAt != nil || persistedExecution.LastHeartbeatAt != nil {
				t.Fatalf("invalid structured keys caused side effects: sequence=%d progress=%d latest=%#v log=%q heartbeats=%v/%v", persisted.ProgressSequence, persisted.Progress, persisted.LatestProgress.Data(), persisted.ProgressLog, persisted.LastHeartbeatAt, persistedExecution.LastHeartbeatAt)
			}
			assertNoAgentProgressEvent(t, subscriber)
		})
	}
}

func agentJSONRequest(path, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
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

	h := NewAgentHandler(taskSvc, apiKeySvc, &logger)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("create execution token service: %v", err)
	}
	h.SetExecutionTokenService(tokens)
	h.SetLocalExecutionTokenTTL(70 * time.Minute)
	app = fiber.New()
	app.Post("/agent/claim", h.ClaimAuthMiddleware, h.Claim)
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
	capabilityResolver := service.NewImageCapabilityResolver(repo, &config.Config{ModelRoutes: config.ModelRoutesConfig{ImageGeneration: config.ImageGenerationRoutesConfig{
		DefaultCapability: "standard", Capabilities: map[string]config.ImageGenerationRouteConfig{
			"standard": {Provider: "openai-test", Model: "image-test", BaseURL: "https://images.invalid/v1", APIKey: "test-secret", Timeout: time.Minute, Enabled: true, MinTier: string(model.TierFree), BillingSKU: "image.standard"},
		},
	}}})
	snapshot, err := capabilityResolver.FreezeImageCapability(context.Background(), userID, "standard")
	if err != nil {
		t.Fatalf("freeze image capability: %v", err)
	}
	task.ImageCapabilityKey = snapshot.Key
	task.SetImageCapabilitySnapshot(snapshot)
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
	app, _, task, executionID, token, _, store := setupExecutionScopedAgentApp(t)
	wantKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/executions/" + executionID + "/artifacts/workspace/sha256/" + strings.Repeat("a", 64) + "/output/article.md"
	request := func(body string) (int, string) {
		req := httptest.NewRequest(http.MethodPost, "/agent/artifacts/manifest", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(data)
	}
	invalidStatus, _ := request(`{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","files":[{"relative_path":"../bad","object_key":"bad","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}]}`)
	if invalidStatus != fiber.StatusBadRequest {
		t.Fatalf("invalid status = %d", invalidStatus)
	}
	store.statErr = errors.New("secret backend endpoint timed out")
	unavailableStatus, body := request(`{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","files":[{"relative_path":"output/article.md","object_key":"` + wantKey + `","size":1,"sha256":"` + strings.Repeat("a", 64) + `"}]}`)
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
	h := NewAgentHandler(taskSvc, apiKeySvc, &logger)
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
	app.Post("/agent/artifacts/prepare", h.ExecutionAuthMiddleware, h.PrepareArtifactUpload)
	app.Post("/agent/artifacts/manifest", h.ExecutionAuthMiddleware, h.ReportArtifactManifest)
	return app, repo, task, store, rawKey, otherRawKey
}

func TestAgentArtifactPrepareAndManifest(t *testing.T) {
	app, repo, task, executionID, token, _, store := setupExecutionScopedAgentApp(t)
	body := []byte("# title\n\nbody")
	sum := sha256.Sum256(body)
	hash := hex.EncodeToString(sum[:])

	prepareBody := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","relative_path":"output/article.md","filename":"article.md","content_type":"text/markdown","size":13,"sha256":"` + hash + `"}`
	req := httptest.NewRequest("POST", "/agent/artifacts/prepare", strings.NewReader(prepareBody))
	req.Header.Set("Authorization", "Bearer "+token)
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
	stagingPrefix := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/executions/" + executionID + "/artifacts/staging/sha256/" + hash + "/"
	if !strings.HasPrefix(stagingKey, stagingPrefix) || !strings.HasSuffix(stagingKey, "/output/article.md") || store.uploadKey != stagingKey || store.uploadContentType != "text/markdown" {
		t.Fatalf("prepared key/store = %q/%q/%q, want scoped staging key with prefix %q", stagingKey, store.uploadKey, store.uploadContentType, stagingPrefix)
	}
	if store.uploadMetadata[storage.ObjectMetadataSHA256] != hash {
		t.Fatalf("signed upload metadata = %#v, want sha256 %q", store.uploadMetadata, hash)
	}
	if env.Data.STSAccessKeyID == "" || env.Data.STSSecurityToken == "" || env.Data.STSAccessKeySecret == "" {
		t.Fatalf("missing sts credentials: %#v", env.Data)
	}

	store.stats = map[string]*storage.ObjectInfo{
		stagingKey: {Key: stagingKey, Size: int64(len(body)), ContentType: "text/markdown", ETag: "etag", SHA256: hash},
	}
	manifestBody := `{"task_id":"` + task.ID + `","execution_id":"` + executionID + `","files":[{"relative_path":"output/article.md","object_key":"` + stagingKey + `","content_type":"text/markdown","size":` + "13" + `,"sha256":"` + hash + `"}]}`
	req = httptest.NewRequest("POST", "/agent/artifacts/manifest", strings.NewReader(manifestBody))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatalf("manifest request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("manifest status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	files, err := repo.TaskFiles().FindByExecutionID(context.Background(), executionID)
	if err != nil {
		t.Fatalf("find task files: %v", err)
	}
	finalKey := "uploads/users/" + task.UserID + "/projects/" + task.ProjectID + "/tasks/" + task.ID + "/executions/" + executionID + "/artifacts/workspace/sha256/" + hash + "/output/article.md"
	if len(files) != 1 || files[0].OSSKey != finalKey || files[0].State != model.TaskFileStatePending {
		t.Fatalf("task files = %#v, want one pending execution file with immutable key %q", files, finalKey)
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
	if resp.StatusCode != fiber.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 401; body=%s", resp.StatusCode, body)
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
		`{"agent_pack_contract_version":2,"executor_info":{"hostname":"legacy-v2"}}`,
		`{"agent_pack_contract_version":4,"executor_info":{"hostname":"future"}}`,
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
	req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"agent_pack_contract_version":3,"executor_info":{"hostname":"mbp"}}`))
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
	if !strings.Contains(string(body), `"artifact_upload_mode":"stream"`) || !strings.Contains(string(body), `"execution_token":"`) {
		t.Fatalf("response body missing execution-scoped artifact contract: %s", body)
	}
	var envelope struct {
		Data service.LocalExecutionConfig `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	tokenClaims, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokenClaims.Validate(envelope.Data.ExecutionToken)
	if err != nil {
		t.Fatalf("validate claim execution token: %v", err)
	}
	if claims.ExpiresAt == nil || time.Until(claims.ExpiresAt.Time) < 69*time.Minute {
		t.Fatalf("execution token expires at %v, want configured 70 minute lifecycle", claims.ExpiresAt)
	}
	claimed, err := repo.Tasks().FindByID(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed.CurrentExecutionID == nil || !strings.Contains(string(body), `"execution_id":"`+*claimed.CurrentExecutionID+`"`) {
		t.Fatalf("response body does not carry current execution id %v: %s", claimed.CurrentExecutionID, body)
	}

	// Second claim: nothing claimable → 204 No Content.
	req2 := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"agent_pack_contract_version":3}`))
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
	req := httptest.NewRequest("POST", "/agent/claim", strings.NewReader(`{"agent_pack_contract_version":3}`))
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
