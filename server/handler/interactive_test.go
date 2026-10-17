package handler

import (
	"bufio"
	"context"
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
