package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
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
// For POST requests, it reads and logs the JSON-RPC method and tool call details.
func mcpLoggingMiddleware(next http.Handler, zlog *zerolog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}

		// Log JSON-RPC method from POST request body.
		if r.Method == http.MethodPost && r.Body != nil && zlog != nil {
			body, err := io.ReadAll(io.LimitReader(r.Body, 100000))
			if err == nil && len(body) > 0 {
				r.Body = io.NopCloser(bytes.NewReader(body))
				var msg map[string]any
				if json.Unmarshal(body, &msg) == nil {
					if rpcMethod, _ := msg["method"].(string); rpcMethod != "" {
						evt := zlog.Info().
							Str("rpc_method", rpcMethod).
							Str("remote_addr", r.RemoteAddr)
						if rpcMethod == "tools/call" {
							if params, ok := msg["params"].(map[string]any); ok {
								toolName, _ := params["name"].(string)
								argsJSON, _ := json.Marshal(params["arguments"])
								argsStr := string(argsJSON)
								if len(argsStr) > 1000 {
									argsStr = argsStr[:1000] + "...(truncated)"
								}
								evt = evt.Str("tool", toolName).Str("args", argsStr)
							}
						}
						if id, ok := msg["id"]; ok {
							evt = evt.Interface("id", id)
						}
						evt.Msg("mcp rpc request")
					}
				}
			}
		}

		next.ServeHTTP(sw, r)
		if zlog != nil {
			evt := zlog.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", sw.status).
				Dur("duration", time.Since(start)).
				Str("remote_addr", r.RemoteAddr)

			// Log Authorization header for auth debugging (strips "Bearer " prefix).
			if sw.status == 401 {
				authHeader := r.Header.Get("Authorization")
				var tokenPreview string
				if len(authHeader) > 7 && strings.HasPrefix(authHeader, "Bearer ") {
					token := strings.TrimPrefix(authHeader, "Bearer ")
					if len(token) > 8 {
						tokenPreview = token[:8] + "..."
					} else {
						tokenPreview = token
					}
				} else if len(authHeader) > 0 {
					tokenPreview = "(non-bearer)"
				} else {
					tokenPreview = "(empty)"
				}
				evt = evt.
					Str("auth_token_preview", tokenPreview)
			}
			evt.Msg("mcp request")
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
		Instructions: "Content creation assistant for WeChat, Xiaolvshu, and Seednote publishing.",
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
		if token == "" {
			if zlog != nil {
				zlog.Warn().
					Str("remote_addr", r.RemoteAddr).
					Msg("mcp auth failed: empty bearer token (ANBANWRITER_API_KEY env var may not be set)")
			}
			return nil, auth.ErrInvalidToken
		}

		// 1. Try per-user API key.
		if apiKeySvc != nil {
			apiKey, err := apiKeySvc.Validate(ctx, token)
			if err == nil && apiKey != nil {
				if zlog != nil {
					zlog.Debug().
						Str("user_id", apiKey.UserID).
						Bool("managed", apiKey.IsManaged).
						Msg("mcp auth succeeded via API key")
				}
				scopes := []string{"mcp"}
				if apiKey.IsManaged {
					scopes = append(scopes, "managed")
				}
				return &auth.TokenInfo{
					UserID:     apiKey.UserID,
					Scopes:     scopes,
					Expiration: time.Now().Add(10 * 365 * 24 * time.Hour),
				}, nil
			}
		}

		// 2. Fallback to static key (admin mode, no userID).
		if staticKey != "" && token == staticKey {
			if zlog != nil {
				zlog.Debug().Msg("mcp auth succeeded via static key (admin)")
			}
			return &auth.TokenInfo{
				UserID:     "",
				Scopes:     []string{"mcp", "admin"},
				Expiration: time.Now().Add(10 * 365 * 24 * time.Hour),
			}, nil
		}

		if zlog != nil {
			tokenHashBytes := sha256.Sum256([]byte(token))
			tokenHash := hex.EncodeToString(tokenHashBytes[:])[:16]
			zlog.Warn().
				Str("remote_addr", r.RemoteAddr).
				Int("token_len", len(token)).
				Str("token_hash_prefix", tokenHash).
				Bool("static_key_set", staticKey != "").
				Bool("api_key_svc_available", apiKeySvc != nil).
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
