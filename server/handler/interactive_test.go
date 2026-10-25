package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/service"
	"github.com/gofiber/fiber/v3"
)

func TestInteractiveHandlerMapsAssistantErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "mode unavailable", err: service.ErrAssistantModeUnavailable, want: fiber.StatusConflict},
		{name: "checkpoint missing", err: service.ErrAssistantCheckpointNotFound, want: fiber.StatusNotFound},
		{name: "checkpoint conflict", err: service.ErrAssistantCheckpointConflict, want: fiber.StatusConflict},
		{name: "checkpoint invalid", err: service.ErrAssistantCheckpointInvalid, want: fiber.StatusBadRequest},
		{name: "creation input missing", err: service.ErrAssistantCreationInputsMissing, want: fiber.StatusBadRequest},
		{name: "creation asset unavailable", err: service.ErrAssistantCreationAssetUnavailable, want: fiber.StatusBadRequest},
		{name: "runtime context too large", err: service.ErrAssistantRuntimeContextTooLarge, want: fiber.StatusRequestEntityTooLarge},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Get("/", func(c fiber.Ctx) error { return interactiveError(c, errors.Join(test.err, errors.New("detail"))) })
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != test.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, test.want)
			}
		})
	}
}

func TestInteractiveErrorIncludesStableConflictCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "busy", err: service.ErrInteractiveBusy, want: "interactive_session_busy"},
		{name: "pending question", err: service.ErrInteractivePending, want: "interactive_question_pending"},
		{name: "mode unavailable", err: service.ErrAssistantModeUnavailable, want: "assistant_mode_unavailable"},
		{name: "checkpoint conflict", err: service.ErrAssistantCheckpointConflict, want: "assistant_checkpoint_conflict"},
		{name: "event gap", err: service.ErrInteractiveEventGap, want: "interactive_event_gap"},
		{name: "workspace busy", err: service.ErrAssistantWorkspaceBusy, want: "assistant_workspace_busy"},
		{name: "workspace mode mismatch", err: service.ErrAssistantWorkspaceModeMismatch, want: "assistant_workspace_mode_mismatch"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Post("/", func(c fiber.Ctx) error { return interactiveError(c, test.err) })
			resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body struct {
				ErrorCode string `json:"error_code"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusConflict || body.ErrorCode != test.want {
				t.Fatalf("status/error_code = %d/%q, want %d/%q", resp.StatusCode, body.ErrorCode, fiber.StatusConflict, test.want)
			}
		})
	}
}

func TestInteractiveWorkspaceExpiredErrorIncludesStableCode(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error { return interactiveError(c, service.ErrAssistantWorkspaceExpired) })
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusGone || body.ErrorCode != "assistant_workspace_expired" {
		t.Fatalf("status/error_code = %d/%q, want %d/%q", resp.StatusCode, body.ErrorCode, fiber.StatusGone, "assistant_workspace_expired")
	}
}

func TestInteractiveClosedErrorIncludesStableCode(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error { return interactiveError(c, service.ErrInteractiveSessionClosed) })
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		ErrorCode string `json:"error_code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusGone || body.ErrorCode != "interactive_session_closed" {
		t.Fatalf("status/error_code = %d/%q, want %d/%q", resp.StatusCode, body.ErrorCode, fiber.StatusGone, "interactive_session_closed")
	}
}

type handlerInteractiveRuntime struct{}

func (handlerInteractiveRuntime) Command(context.Context, string, any) error { return nil }
func (handlerInteractiveRuntime) Close() error                               { return nil }

type handlerInteractiveRuntimeFactory struct{}

func (handlerInteractiveRuntimeFactory) Start(context.Context, string, string, string, func(string, any)) (service.Runtime, error) {
	return handlerInteractiveRuntime{}, nil
}

func TestInteractiveHandlerRejectsUnknownAndTrailingJSON(t *testing.T) {
	svc := service.NewInteractiveService(nil)
	h := NewInteractiveHandler(svc)
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return c.Next()
	})
	app.Post("/create", h.Create)
	app.Post("/message/:id", h.Message)
	app.Post("/answers/:id/:callId", h.Answers)
	app.Post("/commit/:id", h.Commit)

	for _, test := range []struct {
		name string
		path string
		body string
	}{
		{name: "create unknown field", path: "/create", body: `{"agent_id":"project-creator","intent":"create-project","extra":true}`},
		{name: "message unknown field", path: "/message/session-1", body: `{"text":"hello","extra":true}`},
		{name: "answers unknown field", path: "/answers/session-1/call-1", body: `{"answers":[],"extra":true}`},
		{name: "commit trailing document", path: "/commit/session-1", body: `{"confirmation_token":"token","idempotency_key":"key"}{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, body = %s; want 400", resp.StatusCode, body)
			}
		})
	}
}

func TestInteractiveHandlerEventsStreamsPublishedEvent(t *testing.T) {
	svc := service.NewInteractiveServiceWithRuntime(nil, handlerInteractiveRuntimeFactory{})
	svc.SetEnabled(true)
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}

	h := NewInteractiveHandler(svc)
	app := fiber.New()
	app.Get("/events/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Events(c)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		_ = app.Shutdown()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Error("timed out shutting down Fiber listener")
		}
	})
	svc.Publish(sess.ID, "session.status", map[string]string{"status": "starting"})

	responseCh := make(chan *http.Response, 1)
	errorCh := make(chan error, 1)
	go func() {
		req, reqErr := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String()+"/events/"+sess.ID, nil)
		if reqErr != nil {
			errorCh <- reqErr
			return
		}
		resp, doErr := (&http.Client{}).Do(req)
		if doErr != nil {
			errorCh <- doErr
			return
		}
		responseCh <- resp
	}()

	var resp *http.Response
	select {
	case err := <-errorCh:
		t.Fatal(err)
	case resp = <-responseCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for SSE response")
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q, want event stream", got)
	}
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "id: 1\n" {
		t.Fatalf("first SSE line = %q, want id", line)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "event: session.status\n" {
		t.Fatalf("second SSE line = %q, want event", line)
	}
	svc.Publish(sess.ID, "session.status", map[string]string{"status": "running"})
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "data: {\"status\":\"starting\"}\n" {
		t.Fatalf("historical SSE data line = %q, want starting status", line)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "\n" {
		t.Fatalf("historical SSE event terminator = %q, want blank line", line)
	}
	for _, want := range []string{"id: 1\n", "event: session.snapshot\n"} {
		line, err = reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line != want {
			t.Fatalf("snapshot SSE line = %q, want %q", line, want)
		}
	}
	if line, err = reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "data: {") || !strings.Contains(line, `"status":"starting"`) {
		t.Fatalf("snapshot SSE data = %q, want current starting status", line)
	}
	if line, err = reader.ReadString('\n'); err != nil || line != "\n" {
		t.Fatalf("snapshot SSE terminator = %q, err = %v, want blank line", line, err)
	}
	line, err = reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if line != "id: 2\n" {
		t.Fatalf("live SSE line = %q, want id 2", line)
	}
}

func TestInteractiveHandlerScopesDraftEventsForAssistantWorkspace(t *testing.T) {
	app := fiber.New()
	app.Get("/", func(c fiber.Ctx) error {
		c.Locals("assistant_workspace_mode", service.AssistantModeProfile)
		event := addWorkspaceEventScope(c, service.InteractiveEvent{
			Type: "draft.ready",
			Data: service.InteractiveDraft{Ready: true, Revision: 1},
		}, "session-1")
		return c.JSON(event.Data)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var data map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatal(err)
	}
	var draft service.InteractiveDraft
	if raw, ok := data["draft"]; !ok {
		t.Fatalf("scoped draft event = %#v, want draft payload", data)
	} else if err := json.Unmarshal(raw, &draft); err != nil {
		t.Fatalf("decode scoped draft payload: %v", err)
	}
	if !draft.Ready {
		t.Fatalf("scoped draft = %#v, want ready draft", draft)
	}
	if got := string(data["mode_id"]); got != `"profile"` {
		t.Fatalf("mode_id = %s, want profile", got)
	}
}

func TestInteractiveHandlerSSEHeartbeatRenewsIdleLease(t *testing.T) {
	svc := service.NewInteractiveServiceWithRuntime(nil, handlerInteractiveRuntimeFactory{})
	svc.SetEnabled(true)
	svc.SetSessionTTL(time.Second)
	svc.SetSessionIdleTimeout(30 * time.Millisecond)
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}

	h := NewInteractiveHandler(svc)
	h.heartbeatInterval = 5 * time.Millisecond
	app := fiber.New()
	app.Get("/events/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Events(c)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() {
		_ = app.Shutdown()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Error("timed out shutting down Fiber listener")
		}
	})
	svc.Publish(sess.ID, "session.status", map[string]string{"status": "starting"})

	resp, err := (&http.Client{}).Get("http://" + listener.Addr().String() + "/events/" + sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	if line, readErr := reader.ReadString('\n'); readErr != nil || line != "id: 1\n" {
		t.Fatalf("initial SSE line = %q, err = %v", line, readErr)
	}

	time.Sleep(100 * time.Millisecond)
	status, statusErr := svc.SessionState("user-1", sess.ID)
	if statusErr != nil {
		t.Fatal(statusErr)
	}
	if status == "expired" {
		t.Fatal("active SSE connection allowed the interactive session to expire")
	}
}

func TestInteractiveHandlerMapsEventHistoryGapToConflict(t *testing.T) {
	svc := service.NewInteractiveServiceWithRuntime(nil, handlerInteractiveRuntimeFactory{})
	svc.SetEnabled(true)
	sess, err := svc.CreateSession(context.Background(), "user-1", "project-creator", "create-project")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 258; i++ {
		svc.Publish(sess.ID, "session.status", map[string]string{"status": "running"})
	}
	h := NewInteractiveHandler(svc)
	app := fiber.New()
	app.Get("/events/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", "user-1")
		return h.Events(c)
	})
	req := httptest.NewRequest(http.MethodGet, "/events/"+sess.ID, nil)
	req.Header.Set("Last-Event-ID", "1")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}
