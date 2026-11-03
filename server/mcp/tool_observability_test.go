package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	serverauth "github.com/anbanai/anban-creator/server/auth"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/service"
)

func decodeToolCallLog(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var event map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(buf.Bytes()), &event); err != nil {
		t.Fatalf("decode tool call log %q: %v", buf.String(), err)
	}
	return event
}

func TestMCPToolCallLoggingRecordsDurationAndTokenUsage(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		time.Sleep(20 * time.Millisecond)
		usage := service.ToolUsageFromContext(ctx)
		if usage == nil {
			return nil, errors.New("tool usage collector missing from context")
		}
		usage.RecordModelUsage("vision-route", srvconfig.TokenUsage{
			InputTokens:       11,
			CachedInputTokens: 2,
			OutputTokens:      3,
			TotalTokens:       16,
		})
		return &mcp.CallToolResult{}, nil
	})

	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: "analyze_image"},
	}
	ctx := withMCPExecutionIdentity(context.Background(), "user-1", "project-1", "task-1", "execution-1")
	if _, err := handler(ctx, "tools/call", req); err != nil {
		t.Fatalf("tool call: %v", err)
	}

	event := decodeToolCallLog(t, &buf)
	if event["message"] != "mcp tool call" || event["tool"] != "analyze_image" {
		t.Fatalf("log identity = %#v, want mcp tool call for analyze_image", event)
	}
	if event["status"] != "ok" || event["level"] != "info" {
		t.Fatalf("log status = %#v, want ok/info", event)
	}
	if event["user_id"] != "user-1" || event["project_id"] != "project-1" ||
		event["task_id"] != "task-1" || event["execution_id"] != "execution-1" {
		t.Fatalf("log identity = %#v, want execution identity fields", event)
	}
	if duration, ok := event["duration_ms"].(float64); !ok || duration < 15 {
		t.Fatalf("duration_ms = %#v, want measured duration of at least 15ms", event["duration_ms"])
	}
	if event["provider_calls"] != float64(1) || event["usage_reported"] != true {
		t.Fatalf("provider fields = %#v, want one call with usage_reported=true", event)
	}
	if event["input_tokens"] != float64(11) || event["cached_input_tokens"] != float64(2) ||
		event["output_tokens"] != float64(3) || event["total_tokens"] != float64(16) {
		t.Fatalf("token fields = %#v, want input 11, cached 2, output 3, total 16", event)
	}
	models, ok := event["models"].([]any)
	if !ok || len(models) != 1 || models[0] != "vision-route" {
		t.Fatalf("models = %#v, want [vision-route]", event["models"])
	}
}

func TestMCPToolCallLoggingMarksToolErrors(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{IsError: true}, nil
	})
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "analyze_image"}}

	if _, err := handler(context.Background(), "tools/call", req); err != nil {
		t.Fatalf("tool call: %v", err)
	}
	event := decodeToolCallLog(t, &buf)
	if event["status"] != "tool_error" || event["is_error"] != true || event["level"] != "warn" {
		t.Fatalf("log = %#v, want warned tool_error", event)
	}
}

func TestMCPToolCallLoggingOmitsUsageWhenNoProviderWasCalled(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "crop_image"}}

	if _, err := handler(context.Background(), "tools/call", req); err != nil {
		t.Fatalf("tool call: %v", err)
	}
	event := decodeToolCallLog(t, &buf)
	if _, ok := event["provider_calls"]; ok {
		t.Fatalf("provider_calls should be omitted when no provider was called: %#v", event)
	}
	if _, ok := event["input_tokens"]; ok {
		t.Fatalf("token fields should be omitted when no provider was called: %#v", event)
	}
}

func TestMCPToolCallLoggingIgnoresNonToolMethods(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.ResultBase{}, nil
	})
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "analyze_image"}}

	if _, err := handler(context.Background(), "tools/list", req); err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("non-tool method was logged: %s", buf.String())
	}
}

func TestMCPToolCallLoggingOmitsTokensWhenProviderDidNotReportUsage(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		usage := service.ToolUsageFromContext(ctx)
		if usage == nil {
			return nil, errors.New("tool usage collector missing from context")
		}
		usage.RecordModelUsage("image-route", srvconfig.TokenUsage{})
		return &mcp.CallToolResult{}, nil
	})
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "generate_image"}}

	if _, err := handler(context.Background(), "tools/call", req); err != nil {
		t.Fatalf("tool call: %v", err)
	}
	event := decodeToolCallLog(t, &buf)
	if event["provider_calls"] != float64(1) || event["usage_reported"] != false {
		t.Fatalf("provider fields = %#v, want one call with usage_reported=false", event)
	}
	for _, field := range []string{"input_tokens", "cached_input_tokens", "output_tokens", "total_tokens"} {
		if _, ok := event[field]; ok {
			t.Fatalf("%s should be omitted when the provider did not report usage: %#v", field, event)
		}
	}
}

func TestMCPToolCallLoggingMarksInternalErrors(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, errors.New("provider unavailable")
	})
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "analyze_image"}}

	if _, err := handler(context.Background(), "tools/call", req); err == nil {
		t.Fatal("tool call error = nil, want propagated error")
	}
	event := decodeToolCallLog(t, &buf)
	if event["status"] != "error" || event["level"] != "error" || event["is_error"] != true {
		t.Fatalf("log = %#v, want error status and level", event)
	}
	if event["error"] != "provider unavailable" {
		t.Fatalf("error field = %#v, want provider unavailable", event["error"])
	}
}

func TestMCPToolCallLoggingRecordsInteractiveIdentity(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{Name: "convert_markdown"}}
	ctx := withMCPInteractiveIdentity(context.Background(), "user-2", "session-2", "project-2")

	if _, err := handler(ctx, "tools/call", req); err != nil {
		t.Fatalf("tool call: %v", err)
	}
	event := decodeToolCallLog(t, &buf)
	if event["user_id"] != "user-2" || event["project_id"] != "project-2" || event["interactive_session_id"] != "session-2" {
		t.Fatalf("log identity = %#v, want interactive identity", event)
	}
	if _, ok := event["task_id"]; ok {
		t.Fatalf("interactive log should not carry task_id: %#v", event)
	}
}

type loggingImageOperationsStub struct {
	fixedSKUImageOperationsSpy
}

func (s *loggingImageOperationsStub) Analyze(ctx context.Context, _ service.AnalyzeTaskImageRequest) (*service.AnalyzeTaskImageResult, error) {
	s.analyzeCalls++
	service.RecordModelUsage(ctx, "vision-route", srvconfig.TokenUsage{InputTokens: 11, OutputTokens: 3, TotalTokens: 14})
	return &service.AnalyzeTaskImageResult{Analysis: "ok"}, nil
}

func TestMCPHandlerLogsToolCallWithExecutionIdentity(t *testing.T) {
	old := svcs
	svcs = &Services{TaskImageOperationsSvc: &loggingImageOperationsStub{}}
	t.Cleanup(func() { svcs = old })

	tokens, err := serverauth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Issue(serverauth.ExecutionClaims{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-1",
	}, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &executionAuthorizerStub{
		wantUserID: "user-1", wantProjectID: "project-1",
		wantTaskID: "task-1", wantExecutionID: "execution-1",
	}

	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := NewMCPHandler(nil, "admin-key", &log, WithExecutionAuthentication(tokens, authorizer))
	sessionID := initializeMCPExecutionSession(t, handler, token)
	rec := callMCPToolForScopeTest(handler, token, sessionID, "analyze_image", `{"project_id":"project-1","task_id":"task-1","prompt":"secret-prompt"}`)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"isError":true`) {
		t.Fatalf("analyze_image failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	var event map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var candidate map[string]any
		if json.Unmarshal([]byte(line), &candidate) == nil && candidate["message"] == "mcp tool call" {
			event = candidate
			break
		}
	}
	if event == nil {
		t.Fatalf("no mcp tool call log line: %s", buf.String())
	}
	if event["tool"] != "analyze_image" || event["user_id"] != "user-1" ||
		event["project_id"] != "project-1" || event["task_id"] != "task-1" || event["execution_id"] != "execution-1" {
		t.Fatalf("log identity = %#v, want execution identity from the handler context", event)
	}
	if event["provider_calls"] != float64(1) || event["usage_reported"] != true ||
		event["input_tokens"] != float64(11) || event["output_tokens"] != float64(3) || event["total_tokens"] != float64(14) {
		t.Fatalf("log usage = %#v, want one reported provider call", event)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "secret-prompt") {
		t.Fatalf("tool call log leaked the prompt: %s", encoded)
	}
}

func TestMCPToolCallLoggingFallsBackToVerifiedTokenInfo(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)
	handler := mcpToolCallLoggingMiddleware(&log)(func(context.Context, string, mcp.Request) (mcp.Result, error) {
		return nil, errors.New("execution superseded")
	})
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Params: &mcp.CallToolParamsRaw{Name: "analyze_image"},
		Extra: &mcp.RequestExtra{TokenInfo: &mcpauth.TokenInfo{
			UserID: "user-1",
			Scopes: []string{"mcp", "execution"},
			Extra: map[string]any{
				"project_id": "project-1", "task_id": "task-1", "execution_id": "execution-1",
			},
		}},
	}

	if _, err := handler(context.Background(), "tools/call", req); err == nil {
		t.Fatal("tool call error = nil, want identity rejection")
	}
	event := decodeToolCallLog(t, &buf)
	if event["status"] != "error" || event["user_id"] != "user-1" ||
		event["project_id"] != "project-1" || event["task_id"] != "task-1" || event["execution_id"] != "execution-1" {
		t.Fatalf("log = %#v, want rejected call attributed from TokenInfo", event)
	}
}
