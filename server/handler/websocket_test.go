package handler

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/fasthttp/websocket"
	"github.com/gofiber/fiber/v3"

	"github.com/royalrick/anbanwriter/server/auth"
)

// setupWSApp starts a minimal Fiber app exposing the authenticated /ws route
// on an ephemeral port. It returns the hub (to drive broadcasts), the JWT
// service (to mint tokens) and the dial address. The fiber-contrib websocket
// handler is the system under test.
func setupWSApp(t *testing.T) (hub *WebSocketHub, jwtSvc *auth.JWTService, addr string, cleanup func()) {
	t.Helper()

	jwtSvc, err := auth.NewJWTService("test-secret-key", "1h", "24h")
	if err != nil {
		t.Fatalf("failed to create JWT service: %v", err)
	}
	hub = NewWebSocketHub(jwtSvc)

	app := fiber.New()
	app.Get("/ws", hub.HandleWebSocket())

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr = ln.Addr().String()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true})
	}()

	// Surface an immediate bind failure before any dial attempt.
	select {
	case err := <-serveErr:
		t.Fatalf("listener failed to start: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cleanup = func() {
		_ = app.Shutdown()
		select {
		case <-serveErr:
		case <-time.After(2 * time.Second):
		}
	}
	return hub, jwtSvc, addr, cleanup
}

// waitForClient blocks until the hub has at least one client registered for the
// scene, proving the server-side serve() goroutine has completed registration
// before the test broadcasts.
func waitForClient(t *testing.T, hub *WebSocketHub, scene string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.clients[scene])
		hub.mu.RUnlock()
		if n >= 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for client registration on scene %q", scene)
}

func TestWebSocket_AuthenticatedBroadcast(t *testing.T) {
	hub, jwtSvc, addr, cleanup := setupWSApp(t)
	defer cleanup()

	const scene = "qr-login-scene-1"
	token, err := jwtSvc.GenerateAccessToken("user-abc")
	if err != nil {
		t.Fatalf("failed to mint token: %v", err)
	}

	url := "ws://" + addr + "/ws?scene=" + scene + "&token=" + token
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v (status=%v)", err, statusOf(resp))
	}
	if resp != nil {
		resp.Body.Close()
	}
	defer conn.Close()

	// Ensure the server has registered the connection before broadcasting.
	waitForClient(t, hub, scene)

	hub.Broadcast(scene, "login_success", fiber.Map{"scene": scene, "user_id": "user-abc"})

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	_, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read broadcast failed: %v", err)
	}

	var got WSMessage
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal broadcast: %v (body=%q)", err, payload)
	}
	if got.Scene != scene || got.Type != "login_success" {
		t.Fatalf("unexpected message: %+v", got)
	}
	data, ok := got.Data.(map[string]any)
	if !ok || data["user_id"] != "user-abc" {
		t.Fatalf("unexpected data: %+v", got.Data)
	}
}

func TestWebSocket_InvalidTokenRejected(t *testing.T) {
	_, _, addr, cleanup := setupWSApp(t)
	defer cleanup()

	url := "ws://" + addr + "/ws?scene=qr-login-scene-1&token=bogus-token"
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(url, nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected dial to fail for an invalid token")
	}
	if !errors.Is(err, websocket.ErrBadHandshake) {
		t.Fatalf("expected ErrBadHandshake, got %v", err)
	}
	if statusOf(resp) != fiber.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", statusOf(resp))
	}
}

func TestWebSocket_InvalidSceneRejected(t *testing.T) {
	_, jwtSvc, addr, cleanup := setupWSApp(t)
	defer cleanup()

	token, err := jwtSvc.GenerateAccessToken("user-abc")
	if err != nil {
		t.Fatalf("failed to mint token: %v", err)
	}

	// "X" is too short to satisfy isValidScene (3-128 chars).
	url := "ws://" + addr + "/ws?scene=X&token=" + token
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(url, nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("expected dial to fail for an invalid scene")
	}
	if statusOf(resp) != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", statusOf(resp))
	}
}

// TestWebSocket_ClientDisconnectCleansUp exercises the disconnect teardown path
// (read loop exits -> Unregister -> removeClient), which was the data-race site
// before storing the raw (non-pooled) fasthttp/websocket conn. Under -race this
// guards against regressions of that class.
func TestWebSocket_ClientDisconnectCleansUp(t *testing.T) {
	hub, jwtSvc, addr, cleanup := setupWSApp(t)
	defer cleanup()

	const scene = "qr-login-scene-2"
	token, err := jwtSvc.GenerateAccessToken("user-disconnect")
	if err != nil {
		t.Fatalf("failed to mint token: %v", err)
	}

	url := "ws://" + addr + "/ws?scene=" + scene + "&token=" + token
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v (status=%d)", err, statusOf(resp))
	}
	if resp != nil {
		resp.Body.Close()
	}

	waitForClient(t, hub, scene)

	// Client disconnects.
	if err := conn.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}

	// The hub must drop the client. Poll rather than sleep.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		n := len(hub.clients[scene])
		hub.mu.RUnlock()
		if n == 0 {
			return // success
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("client was not removed from hub after disconnect")
}

// TestWebSocket_ShutdownClosesClients verifies Shutdown() tears down the event
// loop and closes live connections without deadlocking serve() goroutines.
func TestWebSocket_ShutdownClosesClients(t *testing.T) {
	hub, jwtSvc, addr, cleanup := setupWSApp(t)
	defer cleanup() // cleanup also calls Shutdown; it is idempotent.

	const scene = "qr-login-scene-3"
	token, err := jwtSvc.GenerateAccessToken("user-shutdown")
	if err != nil {
		t.Fatalf("failed to mint token: %v", err)
	}

	url := "ws://" + addr + "/ws?scene=" + scene + "&token=" + token
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial failed: %v (status=%d)", err, statusOf(resp))
	}
	if resp != nil {
		resp.Body.Close()
	}
	defer conn.Close()

	waitForClient(t, hub, scene)

	hub.Shutdown()

	// The server closes the connection as part of shutdown, so the client read
	// must surface an error (not block forever).
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("expected read error after server shutdown, got nil")
	}
}

// statusOf safely extracts the HTTP status code from a dial response.
func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	defer resp.Body.Close()
	return resp.StatusCode
}
