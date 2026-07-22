package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

// These tests cover the Batch 3A bulk endpoints (BulkCancel / BulkClone /
// BulkDelete): request validation, per-task status routing, ownership, and the
// summary counts. They deliberately exercise the handler layer against a
// nil-credit / nil-pubsub TaskService — Cancel and Delete are nil-safe (every
// credit/pubsub access is guarded; cancelFuncs is a sync.Map), so the cancel
// and delete success paths run for real. Clone's success path re-reserves
// credits via CreateManual and is therefore NOT exercised here (a real credit
// service would be required); only its status-gating is tested.

// bulkTestApp seeds a user + project on a fresh sqlite DB and wires the three
// bulk routes with the caller's userID injected via c.Locals("user_id").
func bulkTestApp(t *testing.T, userID string) (app *fiber.App, repo repository.Repository, projectID string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo = repository.New(db)
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "bulk@example.com",
		Password:   "hashed",
		InviteCode: "bulktest",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID = uuid.New().String()
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Bulk",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := service.NewTaskService(repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)

	app = fiber.New()
	app.Post("/tasks/bulk-cancel", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.BulkCancel(c) })
	app.Post("/tasks/bulk-clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.BulkClone(c) })
	app.Post("/tasks/bulk-delete", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.BulkDelete(c) })
	return app, repo, projectID
}

// seedBulkTask inserts a task in the given status owned by userID/projectID and
// returns its id. Planted directly via the repo so terminal/active states are
// available without running the execution path.
func seedBulkTask(t *testing.T, repo repository.Repository, userID, projectID, status string) string {
	t.Helper()
	id := uuid.New().String()
	if err := repo.Tasks().Create(context.Background(), &model.Task{
		ID:        id,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    status,
	}); err != nil {
		t.Fatalf("seed task (%s): %v", status, err)
	}
	return id
}

type bulkTestResult struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Reason string `json:"reason"`
}

type bulkTestResp struct {
	Total     int              `json:"total"`
	Succeeded int              `json:"succeeded"`
	Skipped   int              `json:"skipped"`
	Results   []bulkTestResult `json:"results"`
}

// callBulk POSTs {task_ids} to path and returns the decoded summary. Fails the
// test on non-200 (used only for success-path assertions).
func callBulk(t *testing.T, app *fiber.App, path string, ids []string) bulkTestResp {
	t.Helper()
	body, _ := json.Marshal(map[string][]string{"task_ids": ids})
	req := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("%s status = %d, want 200", path, resp.StatusCode)
	}
	var env struct {
		Code int          `json:"code"`
		Data bulkTestResp `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return env.Data
}

func bulkStatusFor(app *fiber.App, path, body string) int {
	req := httptest.NewRequest("POST", path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		return -1
	}
	return resp.StatusCode
}

func bulkResultByID(r bulkTestResp) map[string]bulkTestResult {
	m := make(map[string]bulkTestResult, len(r.Results))
	for _, res := range r.Results {
		m[res.ID] = res
	}
	return m
}

func TestBulk_RequestValidation(t *testing.T) {
	userID := uuid.New().String()
	app, _, _ := bulkTestApp(t, userID)

	tests := []struct {
		name string
		path string
		body string
		want int
	}{
		{"cancel empty ids", "/tasks/bulk-cancel", `{"task_ids":[]}`, fiber.StatusBadRequest},
		{"cancel missing field", "/tasks/bulk-cancel", `{}`, fiber.StatusBadRequest},
		{"clone bad uuid", "/tasks/bulk-clone", `{"task_ids":["not-a-uuid"]}`, fiber.StatusBadRequest},
		{"delete over 100", "/tasks/bulk-delete", `{"task_ids":["` + strings.Repeat(uuid.New().String()+`","`, 101) + `"]}`, fiber.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bulkStatusFor(app, tt.path, tt.body); got != tt.want {
				t.Fatalf("%s status = %d, want %d", tt.path, got, tt.want)
			}
		})
	}
}

func TestBulkCancel_RoutesByStatusAndOwnership(t *testing.T) {
	userID := uuid.New().String()
	app, repo, projectID := bulkTestApp(t, userID)

	pending := seedBulkTask(t, repo, userID, projectID, model.TaskStatusPending)
	running := seedBulkTask(t, repo, userID, projectID, model.TaskStatusRunning)
	completed := seedBulkTask(t, repo, userID, projectID, model.TaskStatusCompleted)
	failed := seedBulkTask(t, repo, userID, projectID, model.TaskStatusFailed)
	cancelled := seedBulkTask(t, repo, userID, projectID, model.TaskStatusCancelled)

	// A task owned by a different user — ownership must be checked before status.
	foreignUser := uuid.New().String()
	foreignProject := uuid.New().String()
	if err := repo.Users().Create(context.Background(), &model.User{ID: foreignUser, Email: "other@example.com", Password: "x", InviteCode: "other"}); err != nil {
		t.Fatalf("create foreign user: %v", err)
	}
	if err := repo.Projects().Create(context.Background(), &model.Project{ID: foreignProject, UserID: foreignUser, Platform: model.PlatformSeednote, Name: "Other", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create foreign project: %v", err)
	}
	foreignPending := seedBulkTask(t, repo, foreignUser, foreignProject, model.TaskStatusPending)

	missingID := uuid.New().String()
	ids := []string{pending, running, completed, failed, cancelled, foreignPending, missingID}

	res := callBulk(t, app, "/tasks/bulk-cancel", ids)
	if res.Total != len(ids) {
		t.Fatalf("total = %d, want %d", res.Total, len(ids))
	}
	if res.Succeeded != 2 || res.Skipped != 5 {
		t.Fatalf("succeeded/skipped = %d/%d, want 2/5", res.Succeeded, res.Skipped)
	}
	byID := bulkResultByID(res)
	cases := map[string]string{ // id-prefix → expected reason ("": ok)
		pending:        "",
		running:        "",
		completed:      "not_cancellable",
		failed:         "not_cancellable",
		cancelled:      "not_cancellable",
		foreignPending: "forbidden",
		missingID:      "not_found",
	}
	for id, wantReason := range cases {
		got, ok := byID[id]
		if !ok {
			t.Fatalf("result for %s missing", id)
		}
		if wantReason == "" {
			if !got.OK || got.Reason != "" {
				t.Fatalf("task %s = %+v, want OK", id, got)
			}
		} else if got.OK || got.Reason != wantReason {
			t.Fatalf("task %s = %+v, want reason %q", id, got, wantReason)
		}
	}
}

func TestBulkDelete_RoutesByStatus(t *testing.T) {
	userID := uuid.New().String()
	app, repo, projectID := bulkTestApp(t, userID)

	completed := seedBulkTask(t, repo, userID, projectID, model.TaskStatusCompleted)
	failed := seedBulkTask(t, repo, userID, projectID, model.TaskStatusFailed)
	cancelled := seedBulkTask(t, repo, userID, projectID, model.TaskStatusCancelled)
	pending := seedBulkTask(t, repo, userID, projectID, model.TaskStatusPending)
	running := seedBulkTask(t, repo, userID, projectID, model.TaskStatusRunning)
	foreignCompleted := seedBulkTask(t, repo, uuid.New().String(), projectID, model.TaskStatusCompleted)
	missingID := uuid.New().String()

	res := callBulk(t, app, "/tasks/bulk-delete", []string{completed, failed, cancelled, pending, running, foreignCompleted, missingID})
	// completed/failed/cancelled/pending delete OK (pending cancels first, nil-safe);
	// running skipped as running_cancel_first; foreign forbidden; missing not_found.
	if res.Succeeded != 4 || res.Skipped != 3 {
		t.Fatalf("succeeded/skipped = %d/%d, want 4/3", res.Succeeded, res.Skipped)
	}
	byID := bulkResultByID(res)
	if got := byID[running]; got.OK || got.Reason != "running_cancel_first" {
		t.Fatalf("running task = %+v, want reason running_cancel_first", got)
	}
	if got := byID[foreignCompleted]; got.OK || got.Reason != "forbidden" {
		t.Fatalf("foreign task = %+v, want reason forbidden", got)
	}
	if got := byID[missingID]; got.OK || got.Reason != "not_found" {
		t.Fatalf("missing task = %+v, want reason not_found", got)
	}
	for _, id := range []string{completed, failed, cancelled, pending} {
		if got := byID[id]; !got.OK {
			t.Fatalf("terminal task %s = %+v, want OK", id, got)
		}
	}

	// Verify the deleted tasks are actually gone from the DB.
	for _, id := range []string{completed, failed, cancelled, pending} {
		if _, err := repo.Tasks().FindByID(context.Background(), id); err == nil {
			t.Fatalf("task %s still present after delete", id)
		}
	}
}

func TestBulkClone_StatusGatingOnly(t *testing.T) {
	userID := uuid.New().String()
	app, repo, projectID := bulkTestApp(t, userID)

	// Non-cloneable own tasks: each must be skipped as not_cloneable BEFORE the
	// handler calls service.Clone (which would need a credit service). We do not
	// seed any failed/cancelled own task, so Clone's body is never entered.
	pending := seedBulkTask(t, repo, userID, projectID, model.TaskStatusPending)
	running := seedBulkTask(t, repo, userID, projectID, model.TaskStatusRunning)
	completed := seedBulkTask(t, repo, userID, projectID, model.TaskStatusCompleted)

	// A failed task owned by someone else → forbidden (ownership precedes status).
	foreignFailed := seedBulkTask(t, repo, uuid.New().String(), projectID, model.TaskStatusFailed)
	missingID := uuid.New().String()

	res := callBulk(t, app, "/tasks/bulk-clone", []string{pending, running, completed, foreignFailed, missingID})
	if res.Succeeded != 0 || res.Skipped != 5 {
		t.Fatalf("succeeded/skipped = %d/%d, want 0/5", res.Succeeded, res.Skipped)
	}
	byID := bulkResultByID(res)
	for _, id := range []string{pending, running, completed} {
		if got := byID[id]; got.OK || got.Reason != "not_cloneable" {
			t.Fatalf("non-cloneable task %s = %+v, want reason not_cloneable", id, got)
		}
	}
	if got := byID[foreignFailed]; got.OK || got.Reason != "forbidden" {
		t.Fatalf("foreign failed task = %+v, want reason forbidden", got)
	}
	if got := byID[missingID]; got.OK || got.Reason != "not_found" {
		t.Fatalf("missing task = %+v, want reason not_found", got)
	}
}
