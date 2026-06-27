package mcp

import (
	"context"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// progressNotifier is the subset of *mcp.ServerSession needed to push progress
// notifications back to the client. Defined as an interface so heartbeat tests
// can substitute a fake notifier without spinning up a real session.
type progressNotifier interface {
	NotifyProgress(ctx context.Context, params *mcp.ProgressNotificationParams) error
}

// startProgressHeartbeat periodically pushes NotifyProgress notifications during
// a long-running tool call to keep the SSE response stream active.
//
// Why this exists: Claude Code's HTTP MCP client has a 60-second first-byte
// budget. The two long-text tool handlers (write_article, convert_markdown) block on
// an LLM call for 1–8 minutes without writing any
// bytes to the SSE stream, so the client gives up at 60s and the tool times
// out. Pushing a progress notification every 15s rolls the stream and keeps
// the connection alive. See https://code.claude.com/docs/en/mcp.
//
// The returned stop function is safe to call multiple times and MUST be
// deferred by the caller. When token is nil or sess is nil (no progress
// routing target), the helper is a no-op — clients that do not send
// _meta.progressToken cannot be notified, but the call still works via the
// plugin's per-server hard `timeout` budget.
func startProgressHeartbeat(ctx context.Context, sess progressNotifier, token any, toolName string, interval time.Duration) func() {
	if token == nil || sess == nil || interval <= 0 {
		return func() {}
	}

	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		elapsed := time.Duration(0)
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				elapsed += interval
				// NotifyProgress only carries a human-readable Message; we
				// intentionally omit Progress/Total because the LLM call
				// duration varies 1–10min and any fixed Total would mislead
				// the client's progress bar. The byte stream itself is what
				// defeats the 60s first-byte budget.
				_ = sess.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: token,
					Message:       toolName + " 生成中…",
				})
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}

// logLongTextToolStart emits diagnostic fields at the entry of a long-text
// tool handler so we can verify whether clients actually send a progress
// token (without which the heartbeat is a no-op). Safe to call with a nil
// mcpLog — falls back to silent in that case.
func logLongTextToolStart(toolName string, req *mcp.CallToolRequest) {
	if mcpLog == nil {
		return
	}
	var (
		token     any
		sessionID string
	)
	if req != nil {
		if req.Params != nil {
			token = req.Params.GetProgressToken()
		}
		if req.Session != nil {
			sessionID = req.Session.ID()
		}
	}
	mcpLog.Info().
		Str("tool", toolName).
		Bool("has_progress_token", token != nil).
		Interface("progress_token", token).
		Str("mcp_session_id", sessionID).
		Msg("mcp long-text tool start")
}

// logLongTextToolEnd emits duration at handler exit. Pair with
// logLongTextToolStart via `defer logLongTextToolEnd(name, time.Now())`.
func logLongTextToolEnd(toolName string, start time.Time) {
	if mcpLog == nil {
		return
	}
	mcpLog.Info().
		Str("tool", toolName).
		Dur("duration", time.Since(start)).
		Msg("mcp long-text tool end")
}
