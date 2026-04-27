package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/service"
)

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// mcpLoggingMiddleware wraps an http.Handler to log all MCP requests.
// For POST requests, it reads and logs the JSON-RPC tool call details.
func mcpLoggingMiddleware(next http.Handler, zlog *zerolog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}

		// Log tool call details from JSON-RPC request body.
		if r.Method == http.MethodPost && r.Body != nil && zlog != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 100000))
			if err == nil && len(body) > 0 {
				r.Body = io.NopCloser(bytes.NewReader(body))
				var msg map[string]any
				if json.Unmarshal(body, &msg) == nil {
					if method, _ := msg["method"].(string); method == "tools/call" {
						if params, ok := msg["params"].(map[string]any); ok {
							toolName, _ := params["name"].(string)
							argsJSON, _ := json.Marshal(params["arguments"])
							argsStr := string(argsJSON)
							if len(argsStr) > 1000 {
								argsStr = argsStr[:1000] + "...(truncated)"
							}
							zlog.Debug().
								Str("tool", toolName).
								Str("args", argsStr).
								Str("remote_addr", r.RemoteAddr).
								Msg("mcp tool call")
						}
					}
				}
			}
		}

		next.ServeHTTP(sw, r)
		if zlog != nil {
			zlog.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", sw.status).
				Dur("duration", time.Since(start)).
				Str("remote_addr", r.RemoteAddr).
				Msg("mcp request")
		}
	})
}

// NewMCPHandler creates an http.Handler for the MCP endpoint using the official
// MCP Go SDK. It supports per-user API keys (via APIKeyService) with optional
// fallback to a static API key.
//
// The returned handler handles:
//   - POST /mcp — client-to-server JSON-RPC messages
//   - GET /mcp  — server-to-client SSE stream
//   - DELETE /mcp — terminate session
func NewMCPHandler(apiKeySvc *service.APIKeyService, staticKey string, zlog *zerolog.Logger) http.Handler {
	// Create MCP server.
	mcServer := mcp.NewServer(&mcp.Implementation{
		Name:    "anbanwriter-mcp",
		Version: "1.2.0",
	}, &mcp.ServerOptions{
		Instructions: "Content creation assistant for WeChat, Xiaolvshu, and Xiaohongshu publishing.",
	})

	// Register all tools.
	RegisterTools(mcServer)

	// Create streamable HTTP handler (supports POST/GET/DELETE).
	mcpHTTP := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			return mcServer
		},
		&mcp.StreamableHTTPOptions{
			SessionTimeout: 10 * time.Minute,
		},
	)

	// Auth middleware: validate bearer token via per-user API keys or static key.
	verifier := newTokenVerifier(apiKeySvc, staticKey, zlog)
	protected := auth.RequireBearerToken(verifier, nil)(mcpHTTP)

	return mcpLoggingMiddleware(protected, zlog)
}

// tokenVerifier validates per-user API keys via APIKeyService, with static key fallback.
func newTokenVerifier(apiKeySvc *service.APIKeyService, staticKey string, zlog *zerolog.Logger) auth.TokenVerifier {
	return func(ctx context.Context, token string, r *http.Request) (*auth.TokenInfo, error) {
		// 1. Try per-user API key.
		if apiKeySvc != nil {
			apiKey, err := apiKeySvc.Validate(ctx, token)
			if err == nil && apiKey != nil {
				scopes := []string{"mcp"}
				if apiKey.IsManaged {
					scopes = append(scopes, "managed")
				}
				return &auth.TokenInfo{
					UserID:     apiKey.UserID,
					Scopes:     scopes,
					Expiration: time.Now().Add(10 * 365 * 24 * time.Hour), // API keys don't expire
				}, nil
			}
		}

		// 2. Fallback to static key (admin mode, no userID).
		if staticKey != "" && token == staticKey {
			return &auth.TokenInfo{
				UserID:     "",
				Scopes:     []string{"mcp", "admin"},
				Expiration: time.Now().Add(10 * 365 * 24 * time.Hour),
			}, nil
		}

		if zlog != nil {
			zlog.Warn().
				Str("remote_addr", r.RemoteAddr).
				Int("token_len", len(token)).
				Bool("static_key_set", staticKey != "").
				Msg("mcp auth failed: invalid token")
		}
		return nil, auth.ErrInvalidToken
	}
}

// getUserID extracts the authenticated user ID from the MCP request context.
// Returns empty string for static key / admin mode.
func getUserID(ctx context.Context) string {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil {
		return ""
	}
	return info.UserID
}

// textResult creates a CallToolResult with JSON text content.
func textResult(data any) (*mcp.CallToolResult, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: string(b)},
		},
	}, nil
}

// errorResult creates a CallToolResult indicating a tool error.
func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: msg},
		},
		IsError: true,
	}
}

// noopLogger returns a discard slog.Logger if zlog is nil.
func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(ioDiscard{}, nil))
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

// isManagedCall returns true if the MCP call is from a managed key (agent task execution).
func isManagedCall(ctx context.Context) bool {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil {
		return false
	}
	for _, s := range info.Scopes {
		if s == "managed" {
			return true
		}
	}
	return false
}
