package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

// stubWritingSvc is a minimal non-nil WritingService pointer used to bypass
// the nil check in handlers so argument validation tests can reach the arg parsing code.
var stubWritingSvc = &service.WritingService{}

func repositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

// ---------------------------------------------------------------------------
// parseArgs tests
// ---------------------------------------------------------------------------

func TestParseArgs_ValidJSON(t *testing.T) {
	raw := json.RawMessage(`{"project_id":"ch-1","markdown":"# Hello","theme":"autumn-warm"}`)
	args := parseArgs(raw)

	if v, _ := args["project_id"].(string); v != "ch-1" {
		t.Errorf("project_id = %q, want %q", v, "ch-1")
	}
	if v, _ := args["markdown"].(string); v != "# Hello" {
		t.Errorf("markdown = %q, want %q", v, "# Hello")
	}
	if v, _ := args["theme"].(string); v != "autumn-warm" {
		t.Errorf("theme = %q, want %q", v, "autumn-warm")
	}
}

func TestParseArgs_EmptyInput(t *testing.T) {
	args := parseArgs(nil)
	if len(args) != 0 {
		t.Errorf("expected empty map for nil input, got %d items", len(args))
	}

	args = parseArgs(json.RawMessage{})
	if len(args) != 0 {
		t.Errorf("expected empty map for empty input, got %d items", len(args))
	}
}

func TestParseArgs_InvalidJSON(t *testing.T) {
	args := parseArgs(json.RawMessage(`{invalid json`))
	if len(args) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %d items", len(args))
	}
}

func TestParseArgs_NumericArgs(t *testing.T) {
	raw := json.RawMessage(`{"count":5,"rate":3.14}`)
	args := parseArgs(raw)

	if v, ok := args["count"].(float64); !ok || int(v) != 5 {
		t.Errorf("count = %v, want 5", args["count"])
	}
	if v, ok := args["rate"].(float64); !ok || v != 3.14 {
		t.Errorf("rate = %v, want 3.14", args["rate"])
	}
}

func TestParseArgs_ArrayArgs(t *testing.T) {
	raw := json.RawMessage(`{"keywords":["专注力","效率","深度工作"]}`)
	args := parseArgs(raw)

	arr, ok := args["keywords"].([]any)
	if !ok {
		t.Fatalf("keywords type = %T, want []any", args["keywords"])
	}
	if len(arr) != 3 {
		t.Fatalf("keywords length = %d, want 3", len(arr))
	}
	if v, _ := arr[0].(string); v != "专注力" {
		t.Errorf("keywords[0] = %q, want %q", v, "专注力")
	}
}

func TestParseArgs_ChineseContent(t *testing.T) {
	raw := json.RawMessage(`{"topic":"写一篇关于专注力的文章","content":"在这个信息爆炸的时代"}`)
	args := parseArgs(raw)

	if v, _ := args["topic"].(string); v != "写一篇关于专注力的文章" {
		t.Errorf("topic = %q", v)
	}
}

// ---------------------------------------------------------------------------
// textResult tests
// ---------------------------------------------------------------------------

func TestTextResult_MapInput(t *testing.T) {
	result, err := textResult(map[string]any{"html": "<p>Hello</p>", "images": 0})
	if err != nil {
		t.Fatalf("textResult error: %v", err)
	}
	if result.IsError {
		t.Error("IsError should be false")
	}
	if len(result.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(result.Content))
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if v, _ := parsed["html"].(string); v != "<p>Hello</p>" {
		t.Errorf("html = %q", v)
	}
}

func TestTextResult_StringInput(t *testing.T) {
	result, err := textResult("plain text response")
	if err != nil {
		t.Fatalf("textResult error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if text != `"plain text response"` {
		t.Errorf("text = %q", text)
	}
}

func TestTextResult_NilInput(t *testing.T) {
	result, err := textResult(nil)
	if err != nil {
		t.Fatalf("textResult error: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if text != "null" {
		t.Errorf("text = %q, want null", text)
	}
}

func TestTextResult_ComplexStructure(t *testing.T) {
	data := map[string]any{
		"score": 85.5,
		"level": "优质内容",
		"recommendations": []string{
			"互动率偏低，建议增加互动引导",
			"分享率偏低，建议增加实用价值",
		},
	}
	result, err := textResult(data)
	if err != nil {
		t.Fatalf("textResult error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v, _ := parsed["score"].(float64); v != 85.5 {
		t.Errorf("score = %v, want 85.5", v)
	}
	recs, _ := parsed["recommendations"].([]any)
	if len(recs) != 2 {
		t.Errorf("recommendations length = %d, want 2", len(recs))
	}
}

// ---------------------------------------------------------------------------
// errorResult tests
// ---------------------------------------------------------------------------

func TestErrorResult_BasicMessage(t *testing.T) {
	result := errorResult("something went wrong")
	if !result.IsError {
		t.Error("IsError should be true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("Content length = %d, want 1", len(result.Content))
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if text != "something went wrong" {
		t.Errorf("text = %q, want %q", text, "something went wrong")
	}
}

func TestErrorResult_ChineseMessage(t *testing.T) {
	result := errorResult("积分不足，请前往充值")
	if !result.IsError {
		t.Error("IsError should be true")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if text != "积分不足，请前往充值" {
		t.Errorf("text = %q", text)
	}
}

// ---------------------------------------------------------------------------
// billingError tests
// ---------------------------------------------------------------------------

func TestBillingError_GenericError(t *testing.T) {
	result := billingError("render template", fmt.Errorf("LLM timeout"))
	if !result.IsError {
		t.Error("IsError should be true")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "render template") {
		t.Errorf("expected tool name in error, got: %q", text)
	}
	if !strings.Contains(text, "LLM timeout") {
		t.Errorf("expected original error in message, got: %q", text)
	}
	t.Logf("  [GENERIC ERROR] message: %q", text)
}

func TestScoreArticleHandler_ValidMetrics(t *testing.T) {
	args := json.RawMessage(`{
		"read_count": 10000,
		"like_count": 500,
		"share_count": 100,
		"comment_count": 50,
		"collect_count": 200,
		"topic": "专注力的秘密"
	}`)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: args,
		},
	}

	result, err := scoreArticleHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("scoreArticleHandler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content[0].(*mcp.TextContent).Text)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	t.Logf("  [SCORE] score=%.1f level=%q", parsed["score"], parsed["level"])
	t.Logf("  [SCORE] engagement_rate=%.4f share_rate=%.4f", parsed["engagement_rate"], parsed["share_rate"])
	t.Logf("  [SCORE] recommendations=%v", parsed["recommendations"])

	if parsed["score"] == nil {
		t.Error("score is missing")
	}
	if parsed["level"] == nil {
		t.Error("level is missing")
	}
	if _, ok := parsed["recommendations"].([]any); !ok {
		t.Error("recommendations should be an array")
	}
}

func TestScoreArticleHandler_ZeroReads(t *testing.T) {
	args := json.RawMessage(`{"read_count": 0, "like_count": 5}`)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: args},
	}
	result, err := scoreArticleHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for zero read_count")
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "positive") {
		t.Errorf("error should mention positive, got: %q", text)
	}
}

func TestScoreArticleHandler_MinimalArgs(t *testing.T) {
	args := json.RawMessage(`{"read_count": 1000, "like_count": 50}`)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: args},
	}

	result, err := scoreArticleHandler(context.Background(), req)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content[0].(*mcp.TextContent).Text)
	}

	var parsed map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &parsed)

	if v, ok := parsed["share_count"].(float64); !ok || v != 0 {
		t.Errorf("share_count should default to 0, got %v", parsed["share_count"])
	}
	t.Logf("  [MINIMAL] score=%.1f level=%q", parsed["score"], parsed["level"])
}

func TestScoreArticleHandler_ViralScore(t *testing.T) {
	args := json.RawMessage(`{
		"read_count": 100000,
		"like_count": 10000,
		"share_count": 5000,
		"comment_count": 2000,
		"collect_count": 8000
	}`)
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: args},
	}
	result, _ := scoreArticleHandler(context.Background(), req)

	var parsed map[string]any
	json.Unmarshal([]byte(result.Content[0].(*mcp.TextContent).Text), &parsed)

	score, _ := parsed["score"].(float64)
	level, _ := parsed["level"].(string)
	t.Logf("  [VIRAL] score=%.1f level=%q", score, level)

	if score < 60 {
		t.Errorf("viral content score should be >= 60, got %.1f", score)
	}
}

// ---------------------------------------------------------------------------
// calculateViralScore pure function tests
// ---------------------------------------------------------------------------

func TestCalculateViralScore_HighEngagement(t *testing.T) {
	score := calculateViralScore(10000, 0.15, 0.05, 0.03, 0.03)
	t.Logf("  [HIGH ENGAGEMENT] score=%.1f", score)
	if score < 30 {
		t.Errorf("high engagement score too low: %.1f", score)
	}
}

func TestCalculateViralScore_LowEngagement(t *testing.T) {
	score := calculateViralScore(100, 0.001, 0.0001, 0.0005, 0.0001)
	t.Logf("  [LOW ENGAGEMENT] score=%.1f", score)
	if score > 50 {
		t.Errorf("low engagement score too high: %.1f", score)
	}
}

func TestCalculateViralScore_CappedAt100(t *testing.T) {
	score := calculateViralScore(10000000, 1.0, 1.0, 1.0, 1.0)
	t.Logf("  [CAPPED] score=%.1f", score)
	if score > 100 {
		t.Errorf("score should be capped at 100, got %.1f", score)
	}
}

func TestCalculateViralScore_ZeroReads(t *testing.T) {
	score := calculateViralScore(0, 0, 0, 0, 0)
	t.Logf("  [ZERO READS] score=%.1f", score)
	// Log10(0) = -Inf, so read score goes negative; but score is capped at 100
	// not floored at 0. This is a known edge case — score_articleHandler
	// rejects read_count <= 0 before reaching this function.
	if score > 0 {
		t.Errorf("zero reads should not give positive score, got %.1f", score)
	}
}

// ---------------------------------------------------------------------------
// getViralLevel tests
// ---------------------------------------------------------------------------

func TestGetViralLevel_AllLevels(t *testing.T) {
	cases := []struct {
		score    float64
		expected string
	}{
		{95, "超级爆款"},
		{85, "热门爆款"},
		{75, "优质内容"},
		{65, "潜力内容"},
		{55, "普通内容"},
		{30, "待优化"},
		{0, "待优化"},
	}
	for _, tc := range cases {
		level := getViralLevel(tc.score)
		if level != tc.expected {
			t.Errorf("getViralLevel(%.0f) = %q, want %q", tc.score, level, tc.expected)
		}
		t.Logf("  [LEVEL] score=%.0f → %q", tc.score, level)
	}
}

// ---------------------------------------------------------------------------
// generateScoreRecommendations tests
// ---------------------------------------------------------------------------

func TestGenerateScoreRecommendations_LowEngagement(t *testing.T) {
	recs := generateScoreRecommendations(0.005, 0.001, 0.003, 0.0001)
	t.Logf("  [LOW ENGAGEMENT RECS] %v", recs)
	if len(recs) == 0 {
		t.Error("expected recommendations for low engagement")
	}
}

func TestGenerateScoreRecommendations_GoodMetrics(t *testing.T) {
	recs := generateScoreRecommendations(0.1, 0.05, 0.03, 0.02)
	t.Logf("  [GOOD METRICS RECS] %v", recs)
	if len(recs) != 1 {
		t.Errorf("expected 1 recommendation for good metrics, got %d", len(recs))
	}
	if recs[0] != "各项指标表现良好，继续保持！" {
		t.Errorf("expected good-metrics message, got: %q", recs[0])
	}
}

func TestGenerateScoreRecommendations_LowShareRate(t *testing.T) {
	recs := generateScoreRecommendations(0.05, 0.005, 0.03, 0.02)
	found := false
	for _, r := range recs {
		if strings.Contains(r, "分享率偏低") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected share rate recommendation, got: %v", recs)
	}
}

func TestGenerateScoreRecommendations_LowCommentRate(t *testing.T) {
	recs := generateScoreRecommendations(0.1, 0.05, 0.1, 0.001)
	found := false
	for _, r := range recs {
		if strings.Contains(r, "评论率偏低") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected comment rate recommendation, got: %v", recs)
	}
}

// ---------------------------------------------------------------------------
// MCP HTTP-level handler tests
// ---------------------------------------------------------------------------

func setupMCPHandlerWithServices(t *testing.T) (http.Handler, func()) {
	t.Helper()
	log := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()
	SetLogger(&log)

	handler := NewMCPHandler(nil, "test-api-key", &log)
	return handler, func() {
		SetServices(nil)
		SetBillingServices(nil, nil)
		SetLogger(nil)
	}
}

func callMCPTool(t *testing.T, handler http.Handler, toolName string, argsJSON string, apiKey string) string {
	t.Helper()

	// Step 1: Initialize session.
	initBody := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}`
	initReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initBody))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initReq.Header.Set("Authorization", "Bearer "+apiKey)
	initRec := httptest.NewRecorder()
	handler.ServeHTTP(initRec, initReq)
	if initRec.Code != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d: %s", initRec.Code, initRec.Body.String())
	}
	sessionID := initRec.Header().Get("Mcp-Session-Id")

	// Step 2: Send initialized notification.
	notifBody := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	notifReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(notifBody))
	notifReq.Header.Set("Content-Type", "application/json")
	notifReq.Header.Set("Authorization", "Bearer "+apiKey)
	if sessionID != "" {
		notifReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	notifRec := httptest.NewRecorder()
	handler.ServeHTTP(notifRec, notifReq)

	// Step 3: Call the tool.
	callBody := fmt.Sprintf(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"%s","arguments":%s},"id":3}`, toolName, argsJSON)
	callReq := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(callBody))
	callReq.Header.Set("Content-Type", "application/json")
	callReq.Header.Set("Accept", "application/json, text/event-stream")
	callReq.Header.Set("Authorization", "Bearer "+apiKey)
	if sessionID != "" {
		callReq.Header.Set("Mcp-Session-Id", sessionID)
	}
	callRec := httptest.NewRecorder()
	handler.ServeHTTP(callRec, callReq)

	if callRec.Code != http.StatusOK {
		t.Fatalf("tools/call %s: expected 200, got %d: %s", toolName, callRec.Code, callRec.Body.String())
	}

	// Parse response (SSE or JSON).
	body := callRec.Body.String()
	var resp map[string]any
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		for _, line := range strings.Split(body, "\n") {
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
			t.Fatalf("failed to parse tools/call response: %v\nbody: %q", err, body)
		}
	}

	// Check for JSON-RPC error first.
	if errAny, ok := resp["error"]; ok {
		errObj, _ := errAny.(map[string]any)
		msg, _ := errObj["message"].(string)
		t.Fatalf("JSON-RPC error: code=%v message=%q", errObj["code"], msg)
	}

	resultAny, ok := resp["result"]
	if !ok {
		t.Fatalf("no 'result' in response: %v", resp)
	}

	resultMap, ok := resultAny.(map[string]any)
	if !ok {
		t.Fatalf("result is not a map: %T", resultAny)
	}

	contentArr, ok := resultMap["content"].([]any)
	if !ok || len(contentArr) == 0 {
		t.Fatalf("no content in result: %v", resultMap)
	}

	textContent, ok := contentArr[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] is not a map: %T", contentArr[0])
	}

	text, _ := textContent["text"].(string)
	return text
}

func TestConvertMarkdownHandler_NoService(t *testing.T) {
	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()

	text := callMCPTool(t, handler, "convert_markdown",
		`{"project_id":"ch-1","markdown":"# Test"}`,
		"test-api-key")

	if !strings.Contains(text, "writing service not available") {
		t.Errorf("expected 'writing service not available', got: %q", text)
	}
	t.Logf("  [NO SERVICE] error: %q", text)
}

func TestConvertMarkdownHandler_MissingProjectID(t *testing.T) {
	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()

	// Need non-nil WritingSvc so the handler passes the nil check and reaches arg validation.
	SetServices(&Services{WritingSvc: stubWritingSvc})

	text := callMCPTool(t, handler, "convert_markdown",
		`{"markdown":"# Hello"}`,
		"test-api-key")

	if !strings.Contains(text, "project_id is required") {
		t.Errorf("expected 'project_id is required', got: %q", text)
	}
	t.Logf("  [MISSING PROJECT] error: %q", text)
}

func TestConvertMarkdownHandler_MissingMarkdown(t *testing.T) {
	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()

	SetServices(&Services{WritingSvc: stubWritingSvc})

	text := callMCPTool(t, handler, "convert_markdown",
		`{"project_id":"ch-1"}`,
		"test-api-key")

	if !strings.Contains(text, "markdown is required") {
		t.Errorf("expected 'markdown is required', got: %q", text)
	}
	t.Logf("  [MISSING MARKDOWN] error: %q", text)
}

func TestConvertMarkdownHandler_EmptyStrings(t *testing.T) {
	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()

	SetServices(&Services{WritingSvc: stubWritingSvc})

	text := callMCPTool(t, handler, "convert_markdown",
		`{"project_id":"","markdown":""}`,
		"test-api-key")

	t.Logf("  [EMPTY STRINGS] error: %q", text)
	if !strings.Contains(text, "project_id is required") {
		t.Errorf("expected project_id error, got: %q", text)
	}
}

func TestScoreArticleHandler_ViaMCP(t *testing.T) {
	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()

	text := callMCPTool(t, handler, "score_article",
		`{"read_count":5000,"like_count":200,"share_count":30,"comment_count":15}`,
		"test-api-key")

	t.Logf("  [SCORE VIA MCP] %s", text)

	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed["score"] == nil || parsed["level"] == nil {
		t.Error("missing score or level in result")
	}
}

func TestListProjectTitlesHandler_ReturnsTitlesOnly(t *testing.T) {
	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	log := zerolog.New(zerolog.NewTestWriter(t))

	userID := "user-title-mcp"
	projectID := "project-title-mcp"
	if err := repo.Users().Create(ctx, &model.User{
		ID:       userID,
		Email:    "title-mcp@example.com",
		Nickname: "Title MCP",
		Password: "hashed",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   "",
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	for _, task := range []*model.Task{
		{
			ID:        "task-title-mcp-1",
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Prompt:    "prompt should not leak",
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-title-mcp-2",
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Prompt:    "empty title prompt should not leak",
			Title:     "",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}

	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()
	SetServices(&Services{
		ProjectSvc: service.NewProjectService(repo, &log),
		TaskSvc:    service.NewTaskService(repo, nil, nil, nil, &log, "", nil, "", nil, nil),
	})

	text := callMCPTool(t, handler, "list_project_titles",
		fmt.Sprintf(`{"project_id":%q}`, projectID),
		"test-api-key")

	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v\ntext: %s", err, text)
	}
	if _, ok := payload["topics"]; ok {
		t.Fatalf("payload contains deprecated topics key: %v", payload)
	}
	titles, ok := payload["titles"].([]any)
	if !ok {
		t.Fatalf("payload titles type = %T, want []any: %v", payload["titles"], payload)
	}
	if len(titles) != 1 || titles[0] != "新手咖啡豆怎么选" {
		t.Fatalf("titles = %v, want [新手咖啡豆怎么选]", titles)
	}
}

func TestFinalizeTaskTitleHandler_ViaMCP(t *testing.T) {
	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	log := zerolog.New(zerolog.NewTestWriter(t))

	userID := "user-finalize-title-mcp"
	projectID := "project-finalize-title-mcp"
	if err := repo.Users().Create(ctx, &model.User{
		ID:       userID,
		Email:    "finalize-title@example.com",
		Nickname: "Finalize Title",
		Password: "hashed",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        "task-finalize-title-mcp",
		UserID:    "",
		ProjectID: projectID,
		Type:      model.ScopeSeednote,
		Status:    model.TaskStatusRunning,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()
	SetServices(&Services{
		TaskSvc: service.NewTaskService(repo, nil, nil, nil, &log, "", nil, "", nil, nil),
	})

	text := callMCPTool(t, handler, "finalize_task_title",
		`{"task_id":"task-finalize-title-mcp","title":"茶桌新手避坑指南"}`,
		"test-api-key")

	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("unmarshal payload: %v\ntext: %s", err, text)
	}
	if payload["task_id"] != "task-finalize-title-mcp" {
		t.Fatalf("task_id = %v", payload["task_id"])
	}
	if payload["title"] != "茶桌新手避坑指南" {
		t.Fatalf("title = %v", payload["title"])
	}
	if payload["updated"] != true {
		t.Fatalf("updated = %v", payload["updated"])
	}
	if _, ok := payload["topics"]; ok {
		t.Fatalf("payload contains deprecated topics key: %v", payload)
	}
}

func TestFinalizeTaskTitleHandler_RejectsDuplicateViaMCP(t *testing.T) {
	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	log := zerolog.New(zerolog.NewTestWriter(t))

	userID := "user-finalize-duplicate-mcp"
	projectID := "project-finalize-duplicate-mcp"
	for _, task := range []*model.Task{
		{
			ID:        "task-finalize-duplicate-existing",
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-finalize-duplicate-current",
			UserID:    "",
			ProjectID: projectID,
			Type:      model.ScopeSeednote,
			Status:    model.TaskStatusRunning,
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}

	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()
	SetServices(&Services{
		TaskSvc: service.NewTaskService(repo, nil, nil, nil, &log, "", nil, "", nil, nil),
	})

	text := callMCPTool(t, handler, "finalize_task_title",
		`{"task_id":"task-finalize-duplicate-current","title":" 新手 咖啡豆怎么选 "}`,
		"test-api-key")

	if !strings.Contains(text, "duplicate title") {
		t.Fatalf("text = %q, want duplicate title", text)
	}
}

func TestFinalizeTaskTitleHandler_MissingArgs(t *testing.T) {
	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()
	SetServices(&Services{TaskSvc: &service.TaskService{}})

	text := callMCPTool(t, handler, "finalize_task_title",
		`{"title":"标题"}`,
		"test-api-key")
	if !strings.Contains(text, "task_id is required") {
		t.Fatalf("text = %q, want task_id is required", text)
	}

	text = callMCPTool(t, handler, "finalize_task_title",
		`{"task_id":"task-1"}`,
		"test-api-key")
	if !strings.Contains(text, "title is required") {
		t.Fatalf("text = %q, want title is required", text)
	}
}

// ---------------------------------------------------------------------------
// MCP auth test
// ---------------------------------------------------------------------------

func TestConvertMarkdownHandler_WrongAPIKey(t *testing.T) {
	handler, cleanup := setupMCPHandlerWithServices(t)
	defer cleanup()

	initBody := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}},"id":1}`
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer wrong-key")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 with wrong key, got %d", rec.Code)
	}
}
