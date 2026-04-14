package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// Handler implements a minimal MCP JSON-RPC endpoint.
// It supports initialize, tools/list, and tools/call methods.
// Tools are registered via RegisterTool.
type Handler struct {
	logger    *zerolog.Logger
	apiKeySvc *service.APIKeyService
	staticKey string // fallback when no service available
	tools     []Tool
}

// Tool describes an MCP tool.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	CallFunc    func(userID string, args map[string]any) (any, error) `json:"-"`
}

// jsonRPCRequest is a JSON-RPC 2.0 request.
type jsonRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
	ID      any            `json:"id"`
}

// jsonRPCResponse is a JSON-RPC 2.0 response.
type jsonRPCResponse struct {
	JSONRPC string `json:"jsonrpc"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
	ID      any    `json:"id"`
}

// jsonRPCError is a JSON-RPC 2.0 error object.
type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// NewHandler creates a new MCP handler with per-user API key support.
func NewHandler(apiKeySvc *service.APIKeyService, staticKey string, logger *zerolog.Logger) *Handler {
	return &Handler{
		apiKeySvc: apiKeySvc,
		staticKey: staticKey,
		logger:    logger,
		tools:     []Tool{},
	}
}

// RegisterTool adds an MCP tool to the handler.
func (h *Handler) RegisterTool(tool Tool) {
	h.tools = append(h.tools, tool)
}

// Handle handles POST /mcp requests.
func (h *Handler) Handle(c fiber.Ctx) error {
	// Authenticate and extract userID.
	userID, err := h.authenticate(c)
	if err != nil {
		return c.Status(http.StatusUnauthorized).JSON(jsonRPCResponse{
			JSONRPC: "2.0",
			Error: jsonRPCError{
				Code:    -32001,
				Message: "unauthorized: " + err.Error(),
			},
			ID: nil,
		})
	}

	// Parse JSON-RPC request.
	var req jsonRPCRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.JSON(jsonRPCResponse{
			JSONRPC: "2.0",
			Error: jsonRPCError{
				Code:    -32700,
				Message: "parse error: invalid JSON",
			},
			ID: nil,
		})
	}

	if req.JSONRPC != "2.0" {
		return c.JSON(jsonRPCResponse{
			JSONRPC: "2.0",
			Error: jsonRPCError{
				Code:    -32600,
				Message: "invalid request: jsonrpc must be 2.0",
			},
			ID: req.ID,
		})
	}

	// Route to method handler.
	var result any
	var rpcErr *jsonRPCError

	switch req.Method {
	case "initialize":
		result = h.handleInitialize()
	case "tools/list":
		result = h.handleToolsList()
	case "tools/call":
		result, rpcErr = h.handleToolsCall(userID, req.Params)
	case "ping":
		result = map[string]any{}
	default:
		rpcErr = &jsonRPCError{
			Code:    -32601,
			Message: fmt.Sprintf("method not found: %s", req.Method),
		}
	}

	resp := jsonRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}
	if rpcErr != nil {
		resp.Error = *rpcErr
	} else {
		resp.Result = result
	}

	if h.logger != nil {
		h.logger.Debug().
			Str("method", req.Method).
			Str("user_id", userID).
			Interface("id", req.ID).
			Bool("error", rpcErr != nil).
			Msg("mcp request handled")
	}

	return c.JSON(resp)
}

// authenticate validates the API key and returns the userID.
// Tries per-user API keys first, falls back to static key.
func (h *Handler) authenticate(c fiber.Ctx) (string, error) {
	// Extract key from X-API-Key header or Authorization: Bearer <token>.
	rawKey := ""
	if apiKey := c.Get("X-API-Key"); apiKey != "" {
		rawKey = apiKey
	} else {
		auth := c.Get("Authorization")
		if token, ok := strings.CutPrefix(auth, "Bearer "); ok {
			rawKey = token
		}
	}

	if rawKey == "" {
		return "", fmt.Errorf("missing api key")
	}

	// Try per-user API key first.
	if h.apiKeySvc != nil {
		apiKey, err := h.apiKeySvc.Validate(c.Context(), rawKey)
		if err == nil && apiKey != nil {
			return apiKey.UserID, nil
		}
	}

	// Fallback to static key.
	if h.staticKey != "" && rawKey == h.staticKey {
		return "", nil // No userID for static key (admin mode)
	}

	return "", fmt.Errorf("invalid api key")
}

// handleInitialize returns server capabilities.
func (h *Handler) handleInitialize() map[string]any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"tools": map[string]any{
				"listChanged": false,
			},
		},
		"serverInfo": map[string]any{
			"name":    "anbanwriter-mcp",
			"version": "1.1.0",
		},
	}
}

// handleToolsList returns the list of available tools.
func (h *Handler) handleToolsList() map[string]any {
	tools := make([]map[string]any, len(h.tools))
	for i, t := range h.tools {
		tools[i] = map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		}
	}
	return map[string]any{
		"tools": tools,
	}
}

// handleToolsCall executes a tool call with the authenticated userID.
func (h *Handler) handleToolsCall(userID string, params map[string]any) (any, *jsonRPCError) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, &jsonRPCError{
			Code:    -32602,
			Message: "missing tool name",
		}
	}

	// Find the tool.
	for _, t := range h.tools {
		if t.Name == name {
			args, _ := params["arguments"].(map[string]any)
			if args == nil {
				args = map[string]any{}
			}
			result, err := t.CallFunc(userID, args)
			if err != nil {
				return nil, &jsonRPCError{
					Code:    -32000,
					Message: err.Error(),
				}
			}
			return map[string]any{
				"content": []map[string]any{
					{
						"type": "text",
						"text": fmt.Sprintf("%v", result),
					},
				},
			}, nil
		}
	}

	return nil, &jsonRPCError{
		Code:    -32602,
		Message: fmt.Sprintf("unknown tool: %s", name),
	}
}
