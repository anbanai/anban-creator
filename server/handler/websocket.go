package handler

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gorilla/websocket"

	"github.com/royalrick/anbanwriter/server/auth"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// Allow all origins; CORS enforcement is handled by the Fiber CORS middleware
	// before the request reaches this upgrader.
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// WSClient represents a connected WebSocket client in a scene group.
type WSClient struct {
	Scene string
	Conn  *websocket.Conn
}

// WSMessage is a message to be broadcast to all clients in a scene.
type WSMessage struct {
	Scene string      `json:"scene"`
	Type  string      `json:"type"`
	Data  interface{} `json:"data"`
}

// WebSocketHub manages WebSocket connections grouped by scene for QR login flow.
type WebSocketHub struct {
	clients    map[string]map[*websocket.Conn]bool // scene -> connections
	mu         sync.RWMutex
	register   chan *WSClient
	unregister chan *WSClient
	broadcast  chan *WSMessage
	jwtService *auth.JWTService
}

// NewWebSocketHub creates a new WebSocketHub and starts its event loop.
func NewWebSocketHub(jwtSvc *auth.JWTService) *WebSocketHub {
	hub := &WebSocketHub{
		clients:    make(map[string]map[*websocket.Conn]bool),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		broadcast:  make(chan *WSMessage, 256),
		jwtService: jwtSvc,
	}
	go hub.run()
	return hub
}

// run is the main event loop for the WebSocketHub.
func (h *WebSocketHub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if h.clients[client.Scene] == nil {
				h.clients[client.Scene] = make(map[*websocket.Conn]bool)
			}
			h.clients[client.Scene][client.Conn] = true
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if conns, ok := h.clients[client.Scene]; ok {
				if _, exists := conns[client.Conn]; exists {
					delete(conns, client.Conn)
					client.Conn.Close()
					if len(conns) == 0 {
						delete(h.clients, client.Scene)
					}
				}
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.Lock()
			conns := h.clients[msg.Scene]
			if conns == nil {
				h.mu.Unlock()
				continue
			}
			// Copy connection list under lock to avoid holding lock during I/O.
			connsCopy := make([]*websocket.Conn, 0, len(conns))
			for conn := range conns {
				connsCopy = append(connsCopy, conn)
			}
			h.mu.Unlock()

			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}

			// Write to connections outside the lock so one slow client
			// does not block all broadcasts.
			var toClose []*websocket.Conn
			for _, conn := range connsCopy {
				conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					toClose = append(toClose, conn)
				}
			}

			// Remove failed connections under lock.
			if len(toClose) > 0 {
				h.mu.Lock()
				for _, conn := range toClose {
					if conns := h.clients[msg.Scene]; conns != nil {
						delete(conns, conn)
						conn.Close()
						if len(conns) == 0 {
							delete(h.clients, msg.Scene)
						}
					}
				}
				h.mu.Unlock()
			}
		}
	}
}

// Register adds a client connection to a scene.
func (h *WebSocketHub) Register(client *WSClient) {
	h.register <- client
}

// Unregister removes a client connection from a scene.
func (h *WebSocketHub) Unregister(client *WSClient) {
	h.unregister <- client
}

// Broadcast sends a message to all connections in a scene.
func (h *WebSocketHub) Broadcast(scene, msgType string, data interface{}) {
	h.broadcast <- &WSMessage{Scene: scene, Type: msgType, Data: data}
}

// HandleWebSocket returns a Fiber handler that upgrades HTTP to WebSocket.
// The scene is read from the "scene" query parameter.
// The JWT token is read from the "token" query parameter.
func (h *WebSocketHub) HandleWebSocket() fiber.Handler {
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

		return adaptor.HTTPHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			client := &WSClient{Scene: scene, Conn: conn}
			h.Register(client)

			// Ping goroutine for keepalive.
			done := make(chan struct{})
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
						if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
							return
						}
					case <-done:
						return
					}
				}
			}()

			// Read loop to detect disconnect.
			defer func() {
				close(done)
				h.Unregister(client)
			}()
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					break
				}
			}
		})(c)
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

// HandleLoginWebSocket returns a Fiber handler for unauthenticated WebSocket
// connections used during QR code login. The scene must match a valid QR state.
func (h *WebSocketHub) HandleLoginWebSocket(authHandler *AuthHandler) fiber.Handler {
	return func(c fiber.Ctx) error {
		scene := c.Query("scene")
		if scene == "" || !isValidScene(scene) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "valid scene query parameter is required",
			})
		}

		if authHandler == nil || !authHandler.HasValidQRScene(scene) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid or expired QR code scene",
			})
		}

		return adaptor.HTTPHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			client := &WSClient{Scene: scene, Conn: conn}
			h.Register(client)

			done := make(chan struct{})
			go func() {
				ticker := time.NewTicker(30 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
						if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
							return
						}
					case <-done:
						return
					}
				}
			}()

			defer func() {
				close(done)
				h.Unregister(client)
			}()
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					break
				}
			}
		})(c)
	}
}
