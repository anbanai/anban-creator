package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestMCPHandlerInitialize(t *testing.T) {
	handler := NewMCPHandler(nil, "test-key", nil)

	reqBody := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test-client","version":"1.0"}},"id":1}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer test-key")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// The StreamableHTTPHandler may return SSE or plain JSON.
	// Try parsing directly first; if that fails, look for JSON within SSE data.
	respBody := rec.Body.String()
	var resp map[string]any

	err := json.Unmarshal([]byte(respBody), &resp)
	if err == nil {
		// Parsed as plain JSON.
	} else {
		// Try extracting from SSE format (data: {...}\n\n).
		for _, line := range strings.Split(respBody, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if jsonErr := json.Unmarshal([]byte(data), &resp); jsonErr == nil {
					err = nil
					break
				}
			}
		}
		if err != nil {
			t.Fatalf("failed to parse response as JSON or SSE: %v\nbody: %q", err, respBody)
		}
	}
	if resp["result"] == nil {
		t.Fatalf("expected 'result' field in initialize response, got: %v", resp)
	}
}

func TestMCPHandlerAuthRejection(t *testing.T) {
	handler := NewMCPHandler(nil, "test-key", nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	// No Authorization header.

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMCPHandlerInvalidToken(t *testing.T) {
	handler := NewMCPHandler(nil, "test-key", nil)

	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-key")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMCPHandlerToolsList(t *testing.T) {
	handler := NewMCPHandler(nil, "test-key", nil)

	// First, initialize to get a session.
	initBody := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}`
	initReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initReq.Header.Set("Authorization", "Bearer test-key")
	initRec := httptest.NewRecorder()
	handler.ServeHTTP(initRec, initReq)

	if initRec.Code != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d: %s", initRec.Code, initRec.Body.String())
	}

	// Extract session ID from response header.
	sessionID := initRec.Header().Get("Mcp-Session-Id")

	// Send initialized notification.
	notifBody := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	notifReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(notifBody))
	notifReq.Header.Set("Content-Type", "application/json")
	notifReq.Header.Set("Authorization", "Bearer test-key")
	if sessionID != "" {
		notifReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	notifRec := httptest.NewRecorder()
	handler.ServeHTTP(notifRec, notifReq)

	// Now request tools/list.
	toolsBody := `{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}`
	toolsReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(toolsBody))
	toolsReq.Header.Set("Content-Type", "application/json")
	toolsReq.Header.Set("Accept", "application/json, text/event-stream")
	toolsReq.Header.Set("Authorization", "Bearer test-key")
	if sessionID != "" {
		toolsReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	toolsRec := httptest.NewRecorder()
	handler.ServeHTTP(toolsRec, toolsReq)

	if toolsRec.Code != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d: %s", toolsRec.Code, toolsRec.Body.String())
	}

	// Parse response (may be SSE or plain JSON).
	toolsRespBody := toolsRec.Body.String()
	var resp map[string]any
	if err := json.Unmarshal([]byte(toolsRespBody), &resp); err != nil {
		// Try SSE format.
		for _, line := range strings.Split(toolsRespBody, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				if jsonErr := json.Unmarshal([]byte(data), &resp); jsonErr == nil {
					err = nil
					break
				}
			}
		}
		if err != nil {
			t.Fatalf("failed to parse tools/list response: %v\nbody: %q", err, toolsRespBody)
		}
	}

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatal("expected 'result' object in tools/list response")
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected 'tools' array in tools/list response")
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool in tools/list response")
	}

	// Verify expected tool names exist.
	toolNames := map[string]bool{}
	for _, tool := range tools {
		if t, ok := tool.(map[string]any); ok {
			if name, ok := t["name"].(string); ok {
				toolNames[name] = true
			}
		}
	}
	for _, expected := range []string{"list_channels", "create_task", "list_channel_titles", "finalize_task_title", "get_credit_balance", "upload_live_audio", "create_live_analysis_task", "build_live_clip_plan", "build_live_subject_clip_plan", "build_live_clip_manifest"} {
		if !toolNames[expected] {
			t.Errorf("expected tool %q not found in tools/list response", expected)
		}
	}
	deprecatedTool := "list_channel_" + "topics"
	if toolNames[deprecatedTool] {
		t.Errorf("unexpected deprecated tool %q found in tools/list response", deprecatedTool)
	}
}

func TestMCPLoggingMiddleware(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	handler := mcpLoggingMiddleware(inner, &log)

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	output := buf.String()
	if !strings.Contains(output, `"method":"POST"`) {
		t.Errorf("expected method POST in log output, got: %s", output)
	}
	if !strings.Contains(output, `"path":"/mcp"`) {
		t.Errorf("expected path /mcp in log output, got: %s", output)
	}
	if !strings.Contains(output, `"status":418`) {
		t.Errorf("expected status 418 in log output, got: %s", output)
	}
}
