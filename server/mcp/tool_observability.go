package mcp

import (
	"context"
	"slices"
	"strings"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/service"
)

// mcpToolCallLoggingMiddleware emits one structured log line per completed
// tools/call. Wall-clock duration covers the whole tool call; token usage is
// whatever shared provider clients reported while the call was running. It is
// installed outside requestIdentityMiddleware so rejected calls are logged too;
// identity falls back to the transport's verified TokenInfo when the inner
// middleware has not populated the context yet.
func mcpToolCallLoggingMiddleware(zlog *zerolog.Logger) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			tool := mcpToolCallName(req)
			usage := service.NewToolUsage()
			ctx = service.WithToolUsage(ctx, usage)
			started := time.Now()
			result, err := next(ctx, method, req)
			logMCPToolCall(zlog, ctx, req, tool, time.Since(started), result, err, usage)
			return result, err
		}
	}
}

func mcpToolCallName(req mcp.Request) string {
	if req == nil {
		return ""
	}
	params, _ := req.GetParams().(*mcp.CallToolParamsRaw)
	if params == nil {
		return ""
	}
	return strings.TrimSpace(params.Name)
}

type mcpToolCallIdentity struct {
	UserID                 string
	ProjectID              string
	TaskID                 string
	ExecutionID            string
	InteractiveSessionID   string
	AssistantInteractionID string
}

func mcpCallIdentity(ctx context.Context, req mcp.Request) mcpToolCallIdentity {
	if identity, ok := getMCPExecutionIdentity(ctx); ok {
		return mcpToolCallIdentity{
			UserID: identity.UserID, ProjectID: identity.ProjectID,
			TaskID: identity.TaskID, ExecutionID: identity.ExecutionID,
		}
	}
	if identity, ok := getMCPInteractiveIdentity(ctx); ok {
		return mcpToolCallIdentity{
			UserID: identity.UserID, ProjectID: identity.ProjectID,
			InteractiveSessionID: identity.SessionID, AssistantInteractionID: identity.InteractionID,
		}
	}
	if req != nil {
		if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
			return mcpIdentityFromTokenInfo(extra.TokenInfo)
		}
	}
	return mcpToolCallIdentity{UserID: getUserID(ctx)}
}

// mcpIdentityFromTokenInfo mirrors requestIdentityMiddleware's extraction so
// the outer logging middleware can attribute calls without relying on the
// inner middleware having run.
func mcpIdentityFromTokenInfo(info *mcpauth.TokenInfo) mcpToolCallIdentity {
	identity := mcpToolCallIdentity{UserID: info.UserID}
	if slices.Contains(info.Scopes, "execution") {
		identity.ProjectID, _ = info.Extra["project_id"].(string)
		identity.TaskID, _ = info.Extra["task_id"].(string)
		identity.ExecutionID, _ = info.Extra["execution_id"].(string)
	}
	if slices.Contains(info.Scopes, "interactive") {
		identity.ProjectID, _ = info.Extra["project_id"].(string)
		identity.InteractiveSessionID, _ = info.Extra["interactive_session_id"].(string)
		identity.AssistantInteractionID, _ = info.Extra["assistant_interaction_id"].(string)
	}
	return identity
}

func logMCPToolCall(zlog *zerolog.Logger, ctx context.Context, req mcp.Request, tool string, duration time.Duration, result mcp.Result, callErr error, usage *service.ToolUsage) {
	if zlog == nil {
		zlog = mcpLog
	}
	if zlog == nil {
		return
	}

	toolResult, _ := result.(*mcp.CallToolResult)
	isToolError := toolResult != nil && toolResult.IsError
	status := "ok"
	event := zlog.Info()
	switch {
	case callErr != nil:
		status = "error"
		event = zlog.Error().Err(callErr)
	case isToolError:
		status = "tool_error"
		event = zlog.Warn()
	}

	event = event.
		Str("tool", tool).
		Int64("duration_ms", duration.Milliseconds()).
		Str("status", status).
		Bool("is_error", isToolError || callErr != nil)

	identity := mcpCallIdentity(ctx, req)
	if identity.UserID != "" {
		event = event.Str("user_id", identity.UserID)
	}
	if identity.ProjectID != "" {
		event = event.Str("project_id", identity.ProjectID)
	}
	if identity.TaskID != "" {
		event = event.Str("task_id", identity.TaskID)
	}
	if identity.ExecutionID != "" {
		event = event.Str("execution_id", identity.ExecutionID)
	}
	if identity.InteractiveSessionID != "" {
		event = event.Str("interactive_session_id", identity.InteractiveSessionID)
	}
	if identity.AssistantInteractionID != "" {
		event = event.Str("assistant_interaction_id", identity.AssistantInteractionID)
	}

	if usage != nil {
		snapshot := usage.Snapshot()
		if snapshot.ProviderCalls > 0 {
			event = event.
				Int("provider_calls", snapshot.ProviderCalls).
				Bool("usage_reported", snapshot.UsageReported)
			if len(snapshot.Models) > 0 {
				event = event.Strs("models", snapshot.Models)
			}
			if snapshot.UsageReported {
				event = event.
					Int64("input_tokens", snapshot.InputTokens).
					Int64("cached_input_tokens", snapshot.CachedInputTokens).
					Int64("cache_read_input_tokens", snapshot.CacheReadInputTokens).
					Int64("cache_creation_input_tokens", snapshot.CacheCreationInputTokens).
					Int64("output_tokens", snapshot.OutputTokens).
					Int64("total_tokens", snapshot.TotalTokens)
			}
		}
	}

	event.Msg("mcp tool call")
}
