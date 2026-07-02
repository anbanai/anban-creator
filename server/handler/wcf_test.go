package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/anbanai/anban-creator/server/service"
)

// fakeWCFBindingService implements handler.WCFBindingService so the HTTP layer
// (auth gates, status mapping, response envelope, nil-service disabled state)
// can be exercised without a DB or a live wcfLink sidecar. Each method returns
// its configured result/error and records its args.
type fakeWCFBindingService struct {
	startBindResult service.StartBindResult
	startBindErr    error
	pollResult      service.BindStatusResult
	pollErr         error
	unbindErr       error
	getStatusResult service.BindStatusResult
	getStatusErr    error
	setDefaultErr   error

	startBindCalls int
	lastSessionID  string
	lastProjectID  string
	getStatusCalls int
}

func (f *fakeWCFBindingService) StartBind(_ context.Context, _ string) (service.StartBindResult, error) {
	f.startBindCalls++
	return f.startBindResult, f.startBindErr
}
func (f *fakeWCFBindingService) PollBindStatus(_ context.Context, _, sessionID string) (service.BindStatusResult, error) {
	f.lastSessionID = sessionID
	return f.pollResult, f.pollErr
}
func (f *fakeWCFBindingService) Unbind(_ context.Context, _ string) error { return f.unbindErr }
func (f *fakeWCFBindingService) GetStatus(_ context.Context, _ string) (service.BindStatusResult, error) {
	f.getStatusCalls++
	return f.getStatusResult, f.getStatusErr
}
func (f *fakeWCFBindingService) SetDefaultProject(_ context.Context, _, projectID string) error {
	f.lastProjectID = projectID
	return f.setDefaultErr
}

// newWCFHandlerApp mounts the wcf routes on a throwaway fiber app. A middleware
// copies X-User-ID into the "user_id" local so GetUserID resolves, mirroring the
// JWT middleware's behaviour. Pass a nil svc to exercise the disabled path.
func newWCFHandlerApp(t *testing.T, svc WCFBindingService) *fiber.App {
	t.Helper()
	h := NewWCFHandler(svc, nil) // nil logger is acceptable for tests
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", c.Get("X-User-ID"))
		return c.Next()
	})
	app.Post("/wechat/bind/start", h.StartBind)
	app.Get("/wechat/bind/status", h.PollBindStatus)
	app.Post("/wechat/unbind", h.Unbind)
	app.Get("/wechat/status", h.GetStatus)
	app.Put("/wechat/default-project", h.SetDefaultProject)
	return app
}

// wcfAuthReq builds an authenticated request for the authenticated-user tests:
// it stamps the X-User-ID header (so GetUserID resolves) and, when a body is
// present, sets the JSON content type.
func wcfAuthReq(method, target, body string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	req.Header.Set("X-User-ID", "user-1")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

func TestWCFHandler_StartBindSuccess(t *testing.T) {
	svc := &fakeWCFBindingService{
		startBindResult: service.StartBindResult{
			SessionID: "sess-123",
			QRCodeURL: "data:image/png;base64,iVBORw0KGgo=",
			Status:    "pending",
		},
	}
	app := newWCFHandlerApp(t, svc)

	resp, err := app.Test(wcfAuthReq("POST", "/wechat/bind/start", ""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, _ := json.Marshal(body.Data)
	var got service.StartBindResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got.SessionID != "sess-123" || got.Status != "pending" {
		t.Fatalf("unexpected result: %+v", got)
	}
	if !strings.HasPrefix(got.QRCodeURL, "data:image/png;base64,") {
		t.Fatalf("qrcode_url is not a png data URI: %q", got.QRCodeURL)
	}
	if svc.startBindCalls != 1 {
		t.Fatalf("StartBind called %d times, want 1", svc.startBindCalls)
	}
}

func TestWCFHandler_StartBindAlreadyBoundConflict(t *testing.T) {
	app := newWCFHandlerApp(t, &fakeWCFBindingService{startBindErr: service.ErrAlreadyBound})

	resp, err := app.Test(wcfAuthReq("POST", "/wechat/bind/start", ""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestWCFHandler_AccountConflictConflict(t *testing.T) {
	app := newWCFHandlerApp(t, &fakeWCFBindingService{startBindErr: service.ErrAccountConflict})

	resp, err := app.Test(wcfAuthReq("POST", "/wechat/bind/start", ""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

func TestWCFHandler_UnbindSuccess(t *testing.T) {
	app := newWCFHandlerApp(t, &fakeWCFBindingService{})

	resp, err := app.Test(wcfAuthReq("POST", "/wechat/unbind", ""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestWCFHandler_UnbindNotFound(t *testing.T) {
	app := newWCFHandlerApp(t, &fakeWCFBindingService{unbindErr: service.ErrNoBinding})

	resp, err := app.Test(wcfAuthReq("POST", "/wechat/unbind", ""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestWCFHandler_SetDefaultProjectRejectsOtherOwnerProject(t *testing.T) {
	// The service signals a foreign project via the codebase "does not belong"
	// convention; the handler must surface 403, never leak ownership details.
	svc := &fakeWCFBindingService{setDefaultErr: &doesNotBelongError{msg: "project does not belong to user"}}
	app := newWCFHandlerApp(t, svc)

	resp, err := app.Test(wcfAuthReq("PUT", "/wechat/default-project", `{"project_id":"proj-x"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if svc.lastProjectID != "proj-x" {
		t.Fatalf("project id not forwarded: %q", svc.lastProjectID)
	}
}

func TestWCFHandler_SetDefaultProjectRequiresBinding(t *testing.T) {
	// ErrNoBinding on set-default is a client error (bind first), not 404.
	app := newWCFHandlerApp(t, &fakeWCFBindingService{setDefaultErr: service.ErrNoBinding})

	resp, err := app.Test(wcfAuthReq("PUT", "/wechat/default-project", `{"project_id":"proj-x"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestWCFHandler_GetStatusDisabledReportsUnavailable(t *testing.T) {
	// With a nil service the handler still 200s with available:false so Studio
	// can render the "微信通知未启用" state cleanly.
	app := newWCFHandlerApp(t, nil)

	resp, err := app.Test(wcfAuthReq("GET", "/wechat/status", ""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	raw, _ := json.Marshal(body.Data)
	var got service.BindStatusResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if got.Available {
		t.Fatalf("disabled status should report available=false, got %+v", got)
	}
}

func TestWCFHandler_MutatingRoutesUnavailableWhenDisabled(t *testing.T) {
	// Mutating endpoints must 503 (not 200) when wcf is disabled.
	app := newWCFHandlerApp(t, nil)

	cases := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"start", "POST", "/wechat/bind/start", ""},
		{"unbind", "POST", "/wechat/unbind", ""},
		{"set-default", "PUT", "/wechat/default-project", `{"project_id":"p"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := app.Test(wcfAuthReq(tc.method, tc.target, tc.body))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != fiber.StatusServiceUnavailable {
				t.Fatalf("%s: status = %d, want 503", tc.name, resp.StatusCode)
			}
		})
	}
}

func TestWCFHandler_UnauthorizedWithoutUserID(t *testing.T) {
	// No X-User-ID header → 401 on every route.
	app := newWCFHandlerApp(t, &fakeWCFBindingService{})

	resp, err := app.Test(httptest.NewRequest("GET", "/wechat/status", nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWCFHandler_PollBindLoginNotPending(t *testing.T) {
	app := newWCFHandlerApp(t, &fakeWCFBindingService{pollErr: service.ErrLoginNotPending})

	resp, err := app.Test(wcfAuthReq("GET", "/wechat/bind/status?session_id=xyz", ""))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// doesNotBelongError mirrors the repository/service ownership-error convention
// matched in the handler via strings.Contains "does not belong".
type doesNotBelongError struct{ msg string }

func (e *doesNotBelongError) Error() string { return e.msg }
