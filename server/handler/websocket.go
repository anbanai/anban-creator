package handler

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// In production, check against allowed origins
		// For now, allow all during development
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
}

// NewWebSocketHub creates a new WebSocketHub and starts its event loop.
func NewWebSocketHub() *WebSocketHub {
	hub := &WebSocketHub{
		clients:    make(map[string]map[*websocket.Conn]bool),
		register:   make(chan *WSClient),
		unregister: make(chan *WSClient),
		broadcast:  make(chan *WSMessage, 256),
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
			defer h.mu.Unlock()
			conns := h.clients[msg.Scene]
			if conns == nil {
				continue
			}
			data, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			var toClose []*websocket.Conn
			for conn := range conns {
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					toClose = append(toClose, conn)
				}
			}
			for _, conn := range toClose {
				delete(conns, conn)
				conn.Close()
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
func (h *WebSocketHub) HandleWebSocket() fiber.Handler {
	return func(c fiber.Ctx) error {
		scene := c.Query("scene")
		if scene == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "scene query parameter is required",
			})
		}

		return adaptor.HTTPHandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			client := &WSClient{Scene: scene, Conn: conn}
			h.Register(client)

			// Read loop to detect disconnect.
			defer func() {
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
