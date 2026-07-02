package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime/debug"
	"sync"
	"time"

	ws "github.com/fasthttp/websocket"
	"github.com/gofiber/contrib/v3/websocket"
	"github.com/gofiber/fiber/v3"

	"github.com/anbanai/anban-creator/server/auth"
)

// Frame write cadence used when pushing data to connected clients.
const (
	broadcastWriteTimeout = 5 * time.Second
	pingWriteTimeout      = 10 * time.Second
	pingInterval          = 30 * time.Second
)

// WSClient represents a connected WebSocket client in a scene group.
type WSClient struct {
	Scene string
	// Conn is the underlying fasthttp/websocket connection, captured at upgrade
	// time. It is intentionally NOT the fiber-contrib *websocket.Conn wrapper:
	// that wrapper is pooled and its embedded conn is nilled (and returned to a
	// sync.Pool) once the websocket.New handler returns, so holding it in the
	// long-lived hub map would race the pool release. The raw conn is not pooled
	// and stays valid until Close().
	Conn *ws.Conn
	// mu serializes data-frame writes. fasthttp/websocket (like gorilla) allows
	// a single concurrent writer; the hub broadcast loop and the keepalive
	// pinger both write to this connection, so they must be serialized.
	mu sync.Mutex
}

// writeWithDeadline sends a data frame under the connection's write mutex with
// the given timeout. Safe to call concurrently from the broadcast loop and the
// keepalive pinger.
func (c *WSClient) writeWithDeadline(messageType int, data []byte, timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.Conn.SetWriteDeadline(time.Now().Add(timeout))
	return c.Conn.WriteMessage(messageType, data)
}

// close closes the underlying connection.
func (c *WSClient) close() {
	_ = c.Conn.Close()
}

// WSMessage is a message to be broadcast to all clients in a scene.
type WSMessage struct {
	Scene string `json:"scene"`
	Type  string `json:"type"`
	Data  any    `json:"data"`
}

// WebSocketHub manages WebSocket connections grouped by scene for QR login flow.
type WebSocketHub struct {
	clients    map[string]map[*WSClient]bool // scene -> clients
	mu         sync.RWMutex
	register   chan *WSClient
	unregister chan *WSClient
	broadcast  chan *WSMessage
	quit       chan struct{}
	quitOnce   sync.Once
	jwtService *auth.JWTService
}

// NewWebSocketHub creates a new WebSocketHub and starts its event loop.
func NewWebSocketHub(jwtSvc *auth.JWTService) *WebSocketHub {
	hub := &WebSocketHub{
		clients:    make(map[string]map[*WSClient]bool),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		broadcast:  make(chan *WSMessage, 256),
		quit:       make(chan struct{}),
		jwtService: jwtSvc,
	}
	go hub.run()
	return hub
}

// run is the main event loop for the WebSocketHub.
func (h *WebSocketHub) run() {
	for {
		select {
		case <-h.quit:
			h.closeAllClients()
			return

		case client := <-h.register:
			h.mu.Lock()
			if h.clients[client.Scene] == nil {
				h.clients[client.Scene] = make(map[*WSClient]bool)
			}
			h.clients[client.Scene][client] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.removeClient(client)

		case msg := <-h.broadcast:
			h.mu.Lock()
			clients := h.clients[msg.Scene]
			if clients == nil {
				h.mu.Unlock()
				continue
			}
			// Copy client list under lock to avoid holding lock during I/O.
			clientsCopy := make([]*WSClient, 0, len(clients))
			for c := range clients {
				clientsCopy = append(clientsCopy, c)
			}
			h.mu.Unlock()

			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			// Write to clients outside the lock so one slow client
			// does not block all broadcasts.
			var toClose []*WSClient
			for _, c := range clientsCopy {
				if err := c.writeWithDeadline(ws.TextMessage, data, broadcastWriteTimeout); err != nil {
					toClose = append(toClose, c)
				}
			}

			// Remove failed clients under lock.
			if len(toClose) > 0 {
				h.mu.Lock()
				for _, c := range toClose {
					h.removeClientLocked(c)
				}
				h.mu.Unlock()
			}
		}
	}
}

// closeAllClients closes every registered connection so the per-connection
// serve() read loops exit during shutdown. Caller must not hold h.mu.
func (h *WebSocketHub) closeAllClients() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, clients := range h.clients {
		for c := range clients {
			c.close()
		}
	}
	h.clients = make(map[string]map[*WSClient]bool)
}

// removeClient removes a client from its scene and closes its connection.
// It must not be called with h.mu held.
func (h *WebSocketHub) removeClient(client *WSClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeClientLocked(client)
}

// removeClientLocked is removeClient without acquiring the lock.
func (h *WebSocketHub) removeClientLocked(client *WSClient) {
	if clients, ok := h.clients[client.Scene]; ok {
		if _, exists := clients[client]; exists {
			delete(clients, client)
			client.close()
			if len(clients) == 0 {
				delete(h.clients, client.Scene)
			}
		}
	}
}

// Register adds a client connection to a scene. It returns once the hub has
// recorded the client, or immediately if the hub is shutting down.
func (h *WebSocketHub) Register(client *WSClient) {
	select {
	case h.register <- client:
	case <-h.quit:
	}
}

// Unregister removes a client connection from a scene. It never blocks, even
// after Shutdown (the event loop may already have exited).
func (h *WebSocketHub) Unregister(client *WSClient) {
	select {
	case h.unregister <- client:
	case <-h.quit:
	}
}

// Broadcast sends a message to all clients in a scene. It blocks only if the
// inbound queue is full; delivery is guaranteed for QR-login notifications, so
// it intentionally does not drop messages. After Shutdown it becomes a no-op.
func (h *WebSocketHub) Broadcast(scene, msgType string, data any) {
	select {
	case h.broadcast <- &WSMessage{Scene: scene, Type: msgType, Data: data}:
	case <-h.quit:
	}
}

// Shutdown stops the hub event loop and closes all client connections. It is
// idempotent and safe to call concurrently.
func (h *WebSocketHub) Shutdown() {
	h.quitOnce.Do(func() { close(h.quit) })
}

// serve runs the per-connection lifecycle: register, keepalive pings, and a
// read loop that blocks until the client disconnects. It must be invoked from
// inside a websocket.New handler, where the connection is already upgraded.
func (h *WebSocketHub) serve(conn *ws.Conn, scene string) {
	client := &WSClient{Scene: scene, Conn: conn}
	h.Register(client)

	// Ping goroutine for keepalive.
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := client.writeWithDeadline(ws.PingMessage, nil, pingWriteTimeout); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Read loop to detect disconnect. Inbound messages are not expected — this
	// is a server-push channel for QR-login status — and are discarded.
	defer func() {
		close(done)
		h.Unregister(client)
	}()
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}

// recoverHandler logs a panic from a connection handler without writing a
// frame. Writing from the recovery path (the fiber-contrib default) would race
// the per-connection write mutex; the connection is being torn down anyway.
func recoverHandler(_ *websocket.Conn) {
	if r := recover(); r != nil {
		fmt.Fprintf(os.Stderr, "websocket: handler panic recovered: %v\n%s\n", r, debug.Stack())
	}
}

// HandleWebSocket returns a Fiber handler that upgrades HTTP to WebSocket.
// The scene is read from the "scene" query parameter.
// The JWT token is read from the "token" query parameter.
//
// Validation runs in a Fiber handler before the upgrade so invalid requests
// receive a normal HTTP error response instead of a failed WebSocket handshake.
// On success the request is delegated to the fiber-contrib websocket handler.
func (h *WebSocketHub) HandleWebSocket() fiber.Handler {
	upgrade := websocket.New(func(c *websocket.Conn) {
		// c.Conn is the underlying fasthttp/websocket connection (not pooled);
		// c.Query reads scene from the pre-upgrade query snapshot.
		h.serve(c.Conn, c.Query("scene"))
	}, websocket.Config{RecoverHandler: recoverHandler})
	return func(c fiber.Ctx) error {
		scene := c.Query("scene")
		if scene == "" || !isValidScene(scene) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "valid scene query parameter is required",
			})
		}

		// Validate JWT token from query parameter.
		tokenStr := c.Query("token")
		if tokenStr == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "authentication required",
			})
		}
		claims, err := h.jwtService.ValidateToken(tokenStr)
		if err != nil {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "invalid or expired token",
			})
		}
		_ = claims.UserID // available for future authorization checks

		return upgrade(c)
	}
}

// HandleLoginWebSocket returns a Fiber handler for unauthenticated WebSocket
// connections used during QR code login. The scene must match a valid QR state.
func (h *WebSocketHub) HandleLoginWebSocket(authHandler *AuthHandler) fiber.Handler {
	upgrade := websocket.New(func(c *websocket.Conn) {
		h.serve(c.Conn, c.Query("scene"))
	}, websocket.Config{RecoverHandler: recoverHandler})
	return func(c fiber.Ctx) error {
		scene := c.Query("scene")
		if scene == "" || !isValidScene(scene) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "valid scene query parameter is required",
			})
		}

		if authHandler == nil || !authHandler.HasValidQRScene(c.Context(), scene) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid or expired QR code scene",
			})
		}

		return upgrade(c)
	}
}

// isValidScene checks if the scene parameter matches allowed patterns.
// Allowed: lowercase alphanumeric with hyphens, 3-128 chars, must start/end with alphanumeric.
func isValidScene(scene string) bool {
	if len(scene) > 128 || len(scene) < 3 {
		return false
	}
	matched, _ := regexp.MatchString(`^[a-z0-9][a-z0-9\-]+[a-z0-9]$`, scene)
	return matched
}
