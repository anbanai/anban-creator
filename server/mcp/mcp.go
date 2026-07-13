package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	serverauth "github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/service"
)

// ExecutionAccessAuthorizer verifies that a JWT still represents the task's
// current, active execution. *service.TaskService implements this interface.
type ExecutionAccessAuthorizer interface {
	ValidateAgentExecutionAccess(ctx context.Context, userID, projectID, taskID, executionID string) error
}

type mcpHandlerConfig struct {
	executionTokens     *serverauth.ExecutionTokenService
	executionAuthorizer ExecutionAccessAuthorizer
}

// MCPHandlerOption configures optional MCP authentication modes.
type MCPHandlerOption func(*mcpHandlerConfig)

// WithExecutionAuthentication lets an agent execution JWT authenticate to MCP.
// Both dependencies are required: token validation alone is insufficient because
// a superseded execution must stop working immediately, before JWT expiry.
func WithExecutionAuthentication(tokens *serverauth.ExecutionTokenService, authorizer ExecutionAccessAuthorizer) MCPHandlerOption {
	return func(cfg *mcpHandlerConfig) {
		cfg.executionTokens = tokens
		cfg.executionAuthorizer = authorizer
	}
}

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
								argsJSON, _ := json.Marshal(redactMCPLogValue(params["arguments"]))
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
// MCP Go SDK. It supports per-user API keys (via APIKeyService), short-lived
// execution JWTs when configured, and optional fallback to a static API key.
//
// The returned handler handles:
//   - POST /mcp — client-to-server JSON-RPC messages
//   - GET /mcp  — server-to-client SSE stream
//   - DELETE /mcp — terminate session
func NewMCPHandler(apiKeySvc *service.APIKeyService, staticKey string, zlog *zerolog.Logger, options ...MCPHandlerOption) http.Handler {
	cfg := &mcpHandlerConfig{}
	for _, option := range options {
		if option != nil {
			option(cfg)
		}
	}
	// Create MCP server.
	mcServer := mcp.NewServer(&mcp.Implementation{
		Name:    "anban-mcp",
		Version: "1.2.0",
	}, &mcp.ServerOptions{
		Instructions: "Content creation assistant for WeChat and Seednote publishing.",
	})

	// Register all tools.
	RegisterTools(mcServer)

	// Create streamable HTTP handler (supports POST/GET/DELETE).
	// SessionTimeout must exceed the longest tool call: convert_markdown can run
	// up to ~10min on slow LLM responses, plugin .mcp.json hard-caps the call at
	// 15min, and WriteTimeout is 16min. 16min here keeps sessions alive across
	// the full window so progress notifications have a routing target.
	mcpHTTP := mcp.NewStreamableHTTPHandler(
		func(r *http.Request) *mcp.Server {
			return mcServer
		},
		&mcp.StreamableHTTPOptions{
			SessionTimeout: 16 * time.Minute,
		},
	)

	// Auth middleware: validate bearer token via per-user API keys or static key.
	verifier := newTokenVerifier(apiKeySvc, staticKey, cfg.executionTokens, cfg.executionAuthorizer, zlog)
	scoped := executionScopeMiddleware(mcpHTTP, cfg.executionAuthorizer)
	protected := mcpauth.RequireBearerToken(verifier, nil)(scoped)

	return mcpLoggingMiddleware(protected, zlog)
}

// tokenVerifier validates execution JWTs and per-user API keys, with static key fallback.
func newTokenVerifier(apiKeySvc *service.APIKeyService, staticKey string, executionTokens *serverauth.ExecutionTokenService, executionAuthorizer ExecutionAccessAuthorizer, zlog *zerolog.Logger) mcpauth.TokenVerifier {
	return func(ctx context.Context, token string, r *http.Request) (*mcpauth.TokenInfo, error) {
		if token == "" {
			if zlog != nil {
				zlog.Warn().
					Str("remote_addr", r.RemoteAddr).
					Msg("mcp auth failed: empty bearer token (ANBAN_API_KEY env var may not be set)")
			}
			return nil, mcpauth.ErrInvalidToken
		}

		// 1. Short-lived execution JWT. Do not accept these unless current
		// execution authorization is also configured for dispatch-time checks.
		if executionTokens != nil && executionAuthorizer != nil {
			claims, err := executionTokens.Validate(token)
			if err == nil {
				return &mcpauth.TokenInfo{
					UserID:     claims.UserID,
					Scopes:     []string{"mcp", "managed", "execution"},
					Expiration: claims.ExpiresAt.Time,
					Extra: map[string]any{
						"project_id":   claims.ProjectID,
						"task_id":      claims.TaskID,
						"execution_id": claims.ExecutionID,
					},
				}, nil
			}
		}

		// 2. Try per-user API key.
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
				return &mcpauth.TokenInfo{
					UserID:     apiKey.UserID,
					Scopes:     scopes,
					Expiration: time.Now().Add(10 * 365 * 24 * time.Hour),
				}, nil
			}
		}

		// 3. Fallback to static key (admin mode, no userID).
		if staticKey != "" && token == staticKey {
			if zlog != nil {
				zlog.Debug().Msg("mcp auth succeeded via static key (admin)")
			}
			return &mcpauth.TokenInfo{
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
		return nil, mcpauth.ErrInvalidToken
	}
}

func executionScopeMiddleware(next http.Handler, authorizer ExecutionAccessAuthorizer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info := mcpauth.TokenInfoFromContext(r.Context())
		if info == nil || !slices.Contains(info.Scopes, "execution") || r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		const maximumScopeRequestBytes = 1 << 20
		body, err := io.ReadAll(io.LimitReader(r.Body, maximumScopeRequestBytes+1))
		if err != nil {
			http.Error(w, "invalid MCP request", http.StatusBadRequest)
			return
		}
		if len(body) > maximumScopeRequestBytes {
			http.Error(w, "MCP request is too large", http.StatusRequestEntityTooLarge)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		requests, err := parseScopeRequests(body)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}

		projectID, _ := info.Extra["project_id"].(string)
		taskID, _ := info.Extra["task_id"].(string)
		executionID, _ := info.Extra["execution_id"].(string)
		toolCallFound := false
		for _, request := range requests {
			if request.Method != "tools/call" {
				continue
			}
			toolCallFound = true
			if requested, ok := stringArgument(request.Params.Arguments, "project_id"); ok && requested != projectID {
				http.Error(w, "execution token cannot access requested project", http.StatusForbidden)
				return
			}
			if requested, ok := stringArgument(request.Params.Arguments, "task_id"); ok && requested != taskID {
				http.Error(w, "execution token cannot access requested task", http.StatusForbidden)
				return
			}
			if requested, ok := stringArgument(request.Params.Arguments, "execution_id"); ok && requested != executionID {
				http.Error(w, "execution token cannot access requested execution", http.StatusForbidden)
				return
			}
		}
		if !toolCallFound {
			next.ServeHTTP(w, r)
			return
		}
		if authorizer == nil || authorizer.ValidateAgentExecutionAccess(r.Context(), info.UserID, projectID, taskID, executionID) != nil {
			http.Error(w, "execution token is not authorized for the current task execution", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

type scopeRequest struct {
	Method string `json:"method"`
	Params struct {
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

func parseScopeRequests(body []byte) ([]scopeRequest, error) {
	var single scopeRequest
	if err := json.Unmarshal(body, &single); err == nil {
		return []scopeRequest{single}, nil
	}
	var batch []scopeRequest
	if err := json.Unmarshal(body, &batch); err != nil {
		return nil, err
	}
	return batch, nil
}

func stringArgument(arguments map[string]any, key string) (string, bool) {
	value, exists := arguments[key]
	if !exists {
		return "", false
	}
	valueString, ok := value.(string)
	return valueString, ok
}

var mcpLogURLArgKeys = map[string]bool{
	"audio_url":    true,
	"image_url":    true,
	"url":          true,
	"upload_url":   true,
	"download_url": true,
}

func redactMCPLogValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if mcpLogURLArgKeys[k] {
				if s, ok := item.(string); ok {
					out[k] = redactURLQuery(s)
					continue
				}
			}
			out[k] = redactMCPLogValue(item)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = redactMCPLogValue(item)
		}
		return out
	default:
		return v
	}
}

func redactURLQuery(raw string) string {
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.RawQuery == "" {
		return raw
	}
	parsed.RawQuery = "REDACTED"
	return parsed.String()
}

// getUserID extracts the authenticated user ID from the MCP request context.
// Returns empty string for static key / admin mode.
func getUserID(ctx context.Context) string {
	info := mcpauth.TokenInfoFromContext(ctx)
	if info != nil {
		return info.UserID
	}
	if userID, ok := ctx.Value(mcpUserIDContextKey{}).(string); ok {
		return userID
	}
	return ""
}

type mcpUserIDContextKey struct{}

func withMCPUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, mcpUserIDContextKey{}, userID)
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

// isManagedCall returns true if the MCP call is from a managed key (agent task execution).
func isManagedCall(ctx context.Context) bool {
	info := mcpauth.TokenInfoFromContext(ctx)
	if info == nil {
		return false
	}
	return slices.Contains(info.Scopes, "managed")
}

func isAdminCall(ctx context.Context) bool {
	info := mcpauth.TokenInfoFromContext(ctx)
	if info == nil {
		return false
	}
	return slices.Contains(info.Scopes, "admin")
}
