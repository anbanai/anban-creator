package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

			// Authentication diagnostics deliberately record no credential bytes or
			// derivatives that could be correlated across requests.
			if sw.status == 401 {
				authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
				parts := strings.SplitN(authHeader, " ", 2)
				bearerFormat := len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && strings.TrimSpace(parts[1]) != ""
				evt = evt.
					Bool("authorization_present", authHeader != "").
					Bool("bearer_format", bearerFormat)
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
			JSONResponse:   true,
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
			zlog.Warn().
				Str("remote_addr", r.RemoteAddr).
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
			if err := validateExecutionToolScope(request.Params.Name, request.Params.Arguments, projectID, taskID, executionID); err != nil {
				http.Error(w, err.Error(), http.StatusForbidden)
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
		next.ServeHTTP(w, r.WithContext(withMCPExecutionIdentity(r.Context(), info.UserID, projectID, taskID, executionID)))
	})
}

type scopeRequest struct {
	Method string `json:"method"`
	Params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	} `json:"params"`
}

// executionToolScope describes the identity fields an execution-scoped MCP
// call must carry. The table is deliberately fail-closed: adding a new MCP
// tool without an explicit entry cannot silently widen an execution token.
type executionToolScope struct {
	Denied           bool
	RequireProjectID bool
	RequireTaskID    bool
	RequireExecution bool
}

var executionToolScopes = map[string]executionToolScope{
	// User-wide enumeration and scheduling are never part of one task's
	// execution authority.
	"list_projects":     {Denied: true},
	"list_tasks":        {Denied: true},
	"list_plans":        {Denied: true},
	"create_plan":       {Denied: true},
	"add_topic":         {Denied: true},
	"upload_live_audio": {Denied: true},

	// Project-scoped reads and publication listings are restricted to the
	// project frozen into the execution token.
	"get_project":             {RequireProjectID: true},
	"get_project_profile":     {RequireProjectID: true, RequireTaskID: true},
	"list_project_titles":     {RequireProjectID: true},
	"list_drafts":             {RequireProjectID: true},
	"list_published_articles": {RequireProjectID: true},
	"list_topics":             {RequireProjectID: true},
	"claim_topic":             {RequireProjectID: true, RequireTaskID: true},
	"convert_markdown":        {RequireProjectID: true, RequireTaskID: true},
	"render_template":         {RequireProjectID: true, RequireTaskID: true},

	// Task-scoped reads and mutations must name the exact current task.
	"get_task":                       {RequireTaskID: true},
	"cancel_task":                    {RequireTaskID: true},
	"finalize_task_title":            {RequireTaskID: true},
	"list_task_files":                {RequireTaskID: true},
	"update_task_progress":           {RequireTaskID: true},
	"submit_agent_feedback":          {RequireTaskID: true},
	"submit_completion_metadata":     {RequireTaskID: true, RequireExecution: true},
	"recompute_content_tags":         {RequireTaskID: true, RequireExecution: true},
	"recompute_agent_feedback":       {RequireTaskID: true, RequireExecution: true},
	"get_completion_metadata_status": {RequireTaskID: true, RequireExecution: true},
	"create_draft":                   {RequireProjectID: true, RequireTaskID: true},
	"generate_image":                 {RequireProjectID: true, RequireTaskID: true},
	"crop_image":                     {RequireTaskID: true},
	"upload_image":                   {RequireProjectID: true, RequireTaskID: true},
	"compress_image":                 {RequireTaskID: true},
	"download_image":                 {RequireProjectID: true, RequireTaskID: true},
	"analyze_image":                  {RequireProjectID: true, RequireTaskID: true},
	"analyze_video":                  {RequireProjectID: true, RequireTaskID: true},

	// These tools are deterministic, read-only, or operate on the external
	// live/Seednote capability already selected for the current execution. The
	// central authorizer still runs before dispatch, so stale executions cannot
	// use them.
	"score_article":                {},
	"export_seednote":              {},
	"list_resources":               {},
	"get_resource":                 {},
	"search_seednote_feeds":        {},
	"get_seednote_feed_detail":     {},
	"get_seednote_user_profile":    {},
	"get_media_pipeline_status":    {},
	"prepare_file_upload":          {RequireProjectID: true, RequireTaskID: true},
	"create_live_analysis_task":    {},
	"query_live_analysis_task":     {},
	"build_live_clip_plan":         {},
	"build_live_subject_clip_plan": {},
	"build_live_clip_manifest":     {},
}

func validateExecutionToolScope(toolName string, arguments map[string]any, projectID, taskID, executionID string) error {
	rule, ok := executionToolScopes[strings.TrimSpace(toolName)]
	if !ok || rule.Denied {
		return fmt.Errorf("execution token cannot call MCP tool %q", strings.TrimSpace(toolName))
	}
	for _, field := range []struct {
		name     string
		expected string
		required bool
	}{
		{name: "project_id", expected: projectID, required: rule.RequireProjectID},
		{name: "task_id", expected: taskID, required: rule.RequireTaskID},
		{name: "execution_id", expected: executionID, required: rule.RequireExecution},
	} {
		requested, present := stringArgument(arguments, field.name)
		if field.required && (!present || strings.TrimSpace(requested) == "") {
			return fmt.Errorf("execution-scoped MCP tool %q requires %s", toolName, field.name)
		}
		if present && requested != field.expected {
			return fmt.Errorf("execution token cannot access requested %s", field.name)
		}
	}
	return nil
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

func redactMCPLogValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, item := range x {
			if isMCPLogSensitiveKey(k) {
				out[k] = "REDACTED"
				continue
			}
			if isMCPLogURLKey(k) {
				out[k] = redactMCPLogURLValue(item)
				continue
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

func isMCPLogSensitiveKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return false
	}
	for _, marker := range []string{"token", "secret", "password", "authorization", "api_key", "apikey"} {
		if strings.Contains(key, marker) {
			return true
		}
	}
	return false
}

func isMCPLogURLKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return key == "url" || strings.HasSuffix(key, "_url") || strings.HasSuffix(key, "_urls")
}

func redactMCPLogURLValue(value any) any {
	switch x := value.(type) {
	case string:
		return redactURLQuery(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			if raw, ok := item.(string); ok {
				out[i] = redactURLQuery(raw)
			} else {
				out[i] = redactMCPLogValue(item)
			}
		}
		return out
	case []string:
		out := make([]string, len(x))
		for i, item := range x {
			out[i] = redactURLQuery(item)
		}
		return out
	default:
		return redactMCPLogValue(value)
	}
}

func redactURLQuery(raw string) string {
	if raw == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.RawQuery != "" {
		parsed.RawQuery = "REDACTED"
		return parsed.String()
	}
	// url.Parse can reject malformed percent escapes, but a malformed URL may
	// still carry a credential in its query string. Redact conservatively rather
	// than returning that value to logs.
	if index := strings.IndexByte(raw, '?'); index >= 0 {
		fragment := ""
		if fragmentIndex := strings.IndexByte(raw[index+1:], '#'); fragmentIndex >= 0 {
			fragment = raw[index+1+fragmentIndex:]
		}
		return raw[:index] + "?REDACTED" + fragment
	}
	return raw
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

type mcpExecutionIDContextKey struct{}

type mcpExecutionIdentityContextKey struct{}

type mcpExecutionIdentity struct {
	UserID      string
	ProjectID   string
	TaskID      string
	ExecutionID string
}

func withMCPUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, mcpUserIDContextKey{}, userID)
}

func withMCPExecutionID(ctx context.Context, executionID string) context.Context {
	return context.WithValue(ctx, mcpExecutionIDContextKey{}, strings.TrimSpace(executionID))
}

func withMCPExecutionIdentity(ctx context.Context, userID, projectID, taskID, executionID string) context.Context {
	identity := mcpExecutionIdentity{
		UserID: strings.TrimSpace(userID), ProjectID: strings.TrimSpace(projectID),
		TaskID: strings.TrimSpace(taskID), ExecutionID: strings.TrimSpace(executionID),
	}
	ctx = context.WithValue(ctx, mcpExecutionIdentityContextKey{}, identity)
	return withMCPExecutionID(ctx, identity.ExecutionID)
}

func getMCPExecutionIdentity(ctx context.Context) (mcpExecutionIdentity, bool) {
	identity, ok := ctx.Value(mcpExecutionIdentityContextKey{}).(mcpExecutionIdentity)
	if !ok || identity.ExecutionID == "" {
		return mcpExecutionIdentity{}, false
	}
	return identity, true
}

func hasMCPTokenInfo(ctx context.Context) bool {
	return mcpauth.TokenInfoFromContext(ctx) != nil
}

func getExecutionID(ctx context.Context) string {
	executionID, _ := ctx.Value(mcpExecutionIDContextKey{}).(string)
	return strings.TrimSpace(executionID)
}

// requireMCPExecutionIdentity prevents fixed-SKU operations from falling back
// to API-key or static-key authentication. The execution scope is installed by
// executionScopeMiddleware after the JWT has been authorized for the task.
func requireMCPExecutionIdentity(ctx context.Context, operation, projectID, taskID, executionID string) *mcp.CallToolResult {
	identity, ok := getMCPExecutionIdentity(ctx)
	if !ok || identity.ProjectID == "" || identity.TaskID == "" || identity.ExecutionID == "" {
		return errorResult(fmt.Sprintf(`{"code":"execution_identity_required","message":"%s requires an execution-scoped credential"}`, operation))
	}
	if (projectID != "" && strings.TrimSpace(projectID) != identity.ProjectID) ||
		(taskID != "" && strings.TrimSpace(taskID) != identity.TaskID) ||
		(executionID != "" && strings.TrimSpace(executionID) != identity.ExecutionID) {
		return errorResult(fmt.Sprintf(`{"code":"execution_identity_mismatch","message":"%s is bound to the current task execution"}`, operation))
	}
	return nil
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
