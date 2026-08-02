package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	serverauth "github.com/anbanai/anban-creator/server/auth"
)

type executionAuthorizerStub struct {
	wantUserID      string
	wantProjectID   string
	wantTaskID      string
	wantExecutionID string
	err             error
	calls           int
}

func (s *executionAuthorizerStub) ValidateAgentExecutionAccess(_ context.Context, userID, projectID, taskID, executionID string) error {
	s.calls++
	if s.err != nil {
		return s.err
	}
	if userID != s.wantUserID || projectID != s.wantProjectID || taskID != s.wantTaskID || executionID != s.wantExecutionID {
		return errors.New("unexpected execution identity")
	}
	return nil
}

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

func TestMCPHandlerExecutionTokenEnforcesToolCallScope(t *testing.T) {
	const (
		userID      = "user-1"
		projectID   = "project-1"
		taskID      = "task-1"
		executionID = "execution-1"
	)
	tokens, err := serverauth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Issue(serverauth.ExecutionClaims{
		UserID: userID, ProjectID: projectID, TaskID: taskID, ExecutionID: executionID,
	}, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &executionAuthorizerStub{
		wantUserID: userID, wantProjectID: projectID, wantTaskID: taskID, wantExecutionID: executionID,
	}
	handler := NewMCPHandler(nil, "admin-key", nil, WithExecutionAuthentication(tokens, authorizer))
	sessionID := initializeMCPExecutionSession(t, handler, token)

	for _, tt := range []struct {
		name      string
		arguments string
	}{
		{name: "other task same user", arguments: `{"project_id":"project-1","task_id":"task-2"}`},
		{name: "other project", arguments: `{"project_id":"project-2","task_id":"task-1"}`},
		{name: "other execution", arguments: `{"project_id":"project-1","task_id":"task-1","execution_id":"execution-2"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := callMCPToolForScopeTest(handler, token, sessionID, "get_task", tt.arguments)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	batch := `[{"jsonrpc":"2.0","method":"tools/call","params":{"name":"scope_probe","arguments":{"task_id":"task-1"}},"id":3},{"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_task","arguments":{"task_id":"task-2"}},"id":4}]`
	batchRec := postMCPForScopeTest(handler, token, sessionID, batch)
	if batchRec.Code != http.StatusForbidden {
		t.Fatalf("batch cross-task call: expected 403, got %d: %s", batchRec.Code, batchRec.Body.String())
	}
	if authorizer.calls != 0 {
		t.Fatalf("scope mismatch reached current-execution authorizer %d times", authorizer.calls)
	}

	// A correctly scoped request passes the central guard and reaches MCP
	// dispatch. The deliberately unknown tool avoids coupling this auth test to
	// business services while still exercising a real HTTP tools/call request.
	rec := callMCPToolForScopeTest(handler, token, sessionID, "scope_probe", `{"project_id":"project-1","task_id":"task-1","execution_id":"execution-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid scope did not reach MCP dispatch: %d: %s", rec.Code, rec.Body.String())
	}
	if authorizer.calls != 1 {
		t.Fatalf("current-execution authorizer calls = %d, want 1", authorizer.calls)
	}
}

func TestMCPHandlerExecutionTokenRejectsSupersededExecution(t *testing.T) {
	tokens, err := serverauth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	token, err := tokens.Issue(serverauth.ExecutionClaims{
		UserID: "user-1", ProjectID: "project-1", TaskID: "task-1", ExecutionID: "execution-old",
	}, time.Now().Add(10*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	authorizer := &executionAuthorizerStub{err: errors.New("not current")}
	handler := NewMCPHandler(nil, "admin-key", nil, WithExecutionAuthentication(tokens, authorizer))
	sessionID := initializeMCPExecutionSession(t, handler, token)
	rec := callMCPToolForScopeTest(handler, token, sessionID, "scope_probe", `{"project_id":"project-1","task_id":"task-1"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	if authorizer.calls != 1 {
		t.Fatalf("current-execution authorizer calls = %d, want 1", authorizer.calls)
	}
}

func initializeMCPExecutionSession(t *testing.T, handler http.Handler, token string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"execution-test","version":"1.0"}},"id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Header().Get("Mcp-Session-Id")
}

func callMCPToolForScopeTest(handler http.Handler, token, sessionID, toolName, arguments string) *httptest.ResponseRecorder {
	body := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"` + toolName + `","arguments":` + arguments + `},"id":2}`
	return postMCPForScopeTest(handler, token, sessionID, body)
}

func postMCPForScopeTest(handler http.Handler, token, sessionID, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+token)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
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
	for _, expected := range []string{"list_projects", "list_project_titles", "finalize_task_title", "generate_image", "upload_image", "get_media_pipeline_status", "upload_live_audio", "create_live_analysis_task", "build_live_clip_plan", "build_live_subject_clip_plan", "build_live_clip_manifest", "prepare_file_upload"} {
		if !toolNames[expected] {
			t.Errorf("expected tool %q not found in tools/list response", expected)
		}
	}
	for _, removed := range []string{
		"get_credit_balance", "write_article", "research_topics", "optimize_seo",
		"generate_outline", "archive_workspace", "register_rendered_image",
		"save_template", "list_templates", "get_template",
	} {
		if toolNames[removed] {
			t.Errorf("unexpected removed tool %q found in tools/list response", removed)
		}
	}
	deprecatedTool := "list_project_" + "topics"
	if toolNames[deprecatedTool] {
		t.Errorf("unexpected deprecated tool %q found in tools/list response", deprecatedTool)
	}

	if toolNames["get_project_video_profile"] {
		t.Errorf("unexpected removed tool %q found in tools/list response", "get_project_video_profile")
	}
}

func TestProjectToolSchemasIncludeMoments(t *testing.T) {
	handler := NewMCPHandler(nil, "test-key", nil)
	tools := listMCPToolsForTest(t, handler)

	assertToolEnumContains := func(toolName, property, want string) {
		t.Helper()
		for _, tool := range tools {
			tm, ok := tool.(map[string]any)
			if !ok || tm["name"] != toolName {
				continue
			}
			schema, _ := tm["inputSchema"].(map[string]any)
			props, _ := schema["properties"].(map[string]any)
			prop, _ := props[property].(map[string]any)
			values, _ := prop["enum"].([]any)
			for _, value := range values {
				if value == want {
					return
				}
			}
			t.Fatalf("%s.%s enum = %v, missing %q", toolName, property, values, want)
		}
		t.Fatalf("tool %q not found", toolName)
	}

	assertToolEnumContains("list_projects", "platform", "moments")
	assertToolEnumContains("get_project_profile", "scope", "moments")
	assertToolEnumContains("list_projects", "platform", "montage")
	assertToolEnumContains("get_project_profile", "scope", "montage")
}

func TestGetProjectSchemaOnlyAcceptsProjectID(t *testing.T) {
	handler := NewMCPHandler(nil, "test-key", nil)
	tools := listMCPToolsForTest(t, handler)
	for _, tool := range tools {
		tm, ok := tool.(map[string]any)
		if !ok || tm["name"] != "get_project" {
			continue
		}
		schema, _ := tm["inputSchema"].(map[string]any)
		properties, _ := schema["properties"].(map[string]any)
		if len(properties) != 1 || properties["project_id"] == nil {
			t.Fatalf("get_project properties = %#v, want only project_id", properties)
		}
		return
	}
	t.Fatal("get_project tool not found")
}

func TestMCPFilePathSchemaDescriptionsDeclareLocality(t *testing.T) {
	handler := NewMCPHandler(nil, "test-key", nil)
	tools := listMCPToolsForTest(t, handler)
	for _, tool := range tools {
		tm, ok := tool.(map[string]any)
		if !ok {
			continue
		}
		name, _ := tm["name"].(string)
		assertFilePathSchemaLocality(t, name, tm["inputSchema"])
	}
}

func listMCPToolsForTest(t *testing.T, handler http.Handler) []any {
	t.Helper()
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
	sessionID := initRec.Header().Get("Mcp-Session-Id")

	notifReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	notifReq.Header.Set("Content-Type", "application/json")
	notifReq.Header.Set("Authorization", "Bearer test-key")
	if sessionID != "" {
		notifReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	handler.ServeHTTP(httptest.NewRecorder(), notifReq)

	toolsReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","method":"tools/list","params":{},"id":2}`))
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
	resp := parseMCPJSONResponseForTest(t, toolsRec.Body.String())
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatal("expected result object")
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatal("expected tools array")
	}
	return tools
}

func parseMCPJSONResponseForTest(t *testing.T, body string) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err == nil {
		return resp
	}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if err := json.Unmarshal([]byte(data), &resp); err == nil {
				return resp
			}
		}
	}
	t.Fatalf("failed to parse MCP response: %q", body)
	return nil
}

func assertFilePathSchemaLocality(t *testing.T, toolName string, node any) {
	t.Helper()
	switch v := node.(type) {
	case map[string]any:
		for key, item := range v {
			if key == "file_path" {
				prop, ok := item.(map[string]any)
				if !ok {
					t.Fatalf("%s file_path schema is not an object", toolName)
				}
				desc, _ := prop["description"].(string)
				lower := strings.ToLower(desc)
				if !strings.Contains(lower, "server-local") && !strings.Contains(lower, "agent/client-local") && !strings.Contains(lower, "client-local") {
					t.Fatalf("%s file_path description must declare locality, got %q", toolName, desc)
				}
			}
			assertFilePathSchemaLocality(t, toolName, item)
		}
	case []any:
		for _, item := range v {
			assertFilePathSchemaLocality(t, toolName, item)
		}
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

func TestMCPLoggingMiddlewareRedactsSignedURLQueries(t *testing.T) {
	var buf bytes.Buffer
	log := zerolog.New(&buf)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mcpLoggingMiddleware(inner, &log)

	reqBody := `{"jsonrpc":"2.0","method":"tools/call","params":{"name":"create_live_analysis_task","arguments":{"audio_url":"https://signed.example.com/audio.mp3?Signature=secret&Expires=123","download_url":"https://signed.example.com/dl?token=secret","nested":{"image_url":"https://img.example.com/a.png?x=secret"}}},"id":1}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	output := buf.String()
	if strings.Contains(output, "Signature=secret") || strings.Contains(output, "token=secret") || strings.Contains(output, "x=secret") {
		t.Fatalf("signed URL query leaked in log output: %s", output)
	}
	for _, want := range []string{
		`\"audio_url\":\"https://signed.example.com/audio.mp3?REDACTED\"`,
		`\"download_url\":\"https://signed.example.com/dl?REDACTED\"`,
		`\"image_url\":\"https://img.example.com/a.png?REDACTED\"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("redacted log missing %q: %s", want, output)
		}
	}
}
