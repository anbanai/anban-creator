package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type fakeArchestraClient struct {
	agents []service.ArchestraAgent
	sent   []service.ApprovalDecision
	err    error
}

func (f *fakeArchestraClient) ListAgents(context.Context) ([]service.ArchestraAgent, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.agents, nil
}
func (f *fakeArchestraClient) SendMessage(_ context.Context, _ string, _, _ string, _ string, decisions []service.ApprovalDecision) (service.ArchestraSendResult, error) {
	if f.err != nil {
		return service.ArchestraSendResult{}, f.err
	}
	f.sent = append(f.sent, decisions...)
	return service.ArchestraSendResult{Message: &service.ArchestraMessage{Role: "ROLE_AGENT", Text: "ok"}}, nil
}
func (f *fakeArchestraClient) GetTask(context.Context, string, string) (service.ArchestraTask, error) {
	if f.err != nil {
		return service.ArchestraTask{}, f.err
	}
	return service.ArchestraTask{ID: "task-1", ContextID: "ctx-1", State: "TASK_STATE_INPUT_REQUIRED", Response: "done", ApprovalRequests: []service.ArchestraApprovalRequest{{ID: "appr-1", Resolved: false}}}, nil
}
func (f *fakeArchestraClient) CancelTask(context.Context, string, string) (service.ArchestraTask, error) {
	if f.err != nil {
		return service.ArchestraTask{}, f.err
	}
	return service.ArchestraTask{ID: "task-1", State: "TASK_STATE_CANCELED"}, nil
}

func archestraAdminTestRepo(t *testing.T) (repository.Repository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{ID: "admin-1", Email: "admin@example.com", InviteCode: "ADMIN1", IsAdmin: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{ID: "user-1", Email: "user@example.com", InviteCode: "USER01", IsAdmin: false}).Error; err != nil {
		t.Fatal(err)
	}
	return repository.New(db), db
}

func archestraAdminTestApp(t *testing.T, client archestraClient) *fiber.App {
	repo, _ := archestraAdminTestRepo(t)
	logger := zerolog.Nop()
	h := NewArchestraAdminHandler(repo, client, &logger)
	app := fiber.New()
	app.Get("/agents", func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return h.Agents(c) })
	app.Post("/agents/:agentId/messages", func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return h.Message(c) })
	app.Get("/tasks/:taskId", func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return h.Task(c) })
	app.Post("/tasks/:taskId/approval", func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return h.Approval(c) })
	app.Post("/tasks/:taskId/cancel", func(c fiber.Ctx) error { c.Locals("user_id", c.Get("X-User-ID")); return h.Cancel(c) })
	return app
}

func TestArchestraAdminHandlerRejectsUnauthenticatedAndOrdinaryUsers(t *testing.T) {
	app := archestraAdminTestApp(t, &fakeArchestraClient{})
	routes := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "agents", method: "GET", path: "/agents"},
		{name: "message", method: "POST", path: "/agents/a1/messages", body: `{"text":"hello"}`},
		{name: "task", method: "GET", path: "/tasks/task-1?agent_id=a1"},
		{name: "approval", method: "POST", path: "/tasks/task-1/approval", body: `{"agent_id":"a1","approval_id":"appr-1","approved":true}`},
		{name: "cancel", method: "POST", path: "/tasks/task-1/cancel", body: `{"agent_id":"a1"}`},
	}
	for _, route := range routes {
		for _, tc := range []struct {
			name, user string
			status     int
		}{
			{name: "missing", status: fiber.StatusUnauthorized},
			{name: "ordinary", user: "user-1", status: fiber.StatusForbidden},
		} {
			t.Run(route.name+"/"+tc.name, func(t *testing.T) {
				req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
				if route.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				if tc.user != "" {
					req.Header.Set("X-User-ID", tc.user)
				}
				resp, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != tc.status {
					t.Fatalf("status=%d want=%d", resp.StatusCode, tc.status)
				}
				_ = resp.Body.Close()
			})
		}
	}
}

func TestArchestraAdminHandlerSupportsProxyOperationsForAdmin(t *testing.T) {
	client := &fakeArchestraClient{agents: []service.ArchestraAgent{{ID: "a1", Name: "Writer"}}}
	app := archestraAdminTestApp(t, client)

	resp, err := app.Test(httptest.NewRequest("GET", "/agents", nil), fiber.TestConfig{Timeout: 0})
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	req := httptest.NewRequest("GET", "/agents", nil)
	req.Header.Set("X-User-ID", "admin-1")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK || body.Code != 0 {
		t.Fatalf("agents status=%d body=%#v", resp.StatusCode, body)
	}

	req = httptest.NewRequest("POST", "/agents/a1/messages", strings.NewReader(`{"text":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "admin-1")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("message status=%d", resp.StatusCode)
	}

	req = httptest.NewRequest("POST", "/tasks/task-1/approval", strings.NewReader(`{"agent_id":"a1","approval_id":"appr-1","approved":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-ID", "admin-1")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusOK || len(client.sent) != 1 || !client.sent[0].Approved {
		t.Fatalf("approval status=%d decisions=%#v", resp.StatusCode, client.sent)
	}

	for _, tc := range []struct {
		name   string
		method string
		path   string
	}{
		{name: "task", method: "GET", path: "/tasks/task-1?agent_id=a1"},
		{name: "cancel", method: "POST", path: "/tasks/task-1/cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-User-ID", "admin-1")
			if tc.name == "cancel" {
				req = httptest.NewRequest(tc.method, tc.path, strings.NewReader(`{"agent_id":"a1"}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-User-ID", "admin-1")
			}
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusOK {
				t.Fatalf("status=%d", resp.StatusCode)
			}
			_ = resp.Body.Close()
		})
	}
}

func TestArchestraAdminHandlerRechecksAdminAndRedactsUnavailableService(t *testing.T) {
	repo, db := archestraAdminTestRepo(t)
	logger := zerolog.Nop()
	client := &fakeArchestraClient{agents: []service.ArchestraAgent{{ID: "a1"}}}
	h := NewArchestraAdminHandler(repo, client, &logger)
	app := fiber.New()
	app.Get("/agents", func(c fiber.Ctx) error { c.Locals("user_id", "admin-1"); return h.Agents(c) })

	initialReq := httptest.NewRequest("GET", "/agents", nil)
	initialReq.Header.Set("X-User-ID", "admin-1")
	resp, err := app.Test(initialReq)
	if err != nil || resp.StatusCode != fiber.StatusOK {
		t.Fatalf("initial admin status=%d err=%v", resp.StatusCode, err)
	}
	_ = resp.Body.Close()

	if err := db.Model(&model.User{}).Where("id = ?", "admin-1").Update("is_admin", false).Error; err != nil {
		t.Fatal(err)
	}
	revokedReq := httptest.NewRequest("GET", "/agents", nil)
	revokedReq.Header.Set("X-User-ID", "admin-1")
	resp, err = app.Test(revokedReq)
	if err != nil || resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("revoked admin status=%d err=%v", resp.StatusCode, err)
	}
	_ = resp.Body.Close()

	configuredApp := archestraAdminTestApp(t, &fakeArchestraClient{err: service.ErrArchestraNotConfigured})
	req := httptest.NewRequest("GET", "/agents", nil)
	req.Header.Set("X-User-ID", "admin-1")
	resp, err = configuredApp.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("unconfigured status=%d err=%v", resp.StatusCode, err)
	}
	_ = resp.Body.Close()

	errorApp := archestraAdminTestApp(t, &fakeArchestraClient{err: errors.New("upstream secret prompt")})
	resp, err = errorApp.Test(req)
	if err != nil || resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("upstream error status=%d err=%v", resp.StatusCode, err)
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil || strings.Contains(string(body), "secret prompt") {
		t.Fatalf("upstream response leaked: %s", body)
	}
}
