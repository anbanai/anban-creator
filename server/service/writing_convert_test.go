package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
)

// ---------------------------------------------------------------------------
// Diagnostic LLM mock — records everything and logs it
// ---------------------------------------------------------------------------

type llmCall struct {
	SystemPrompt string
	UserPrompt   string
	Timestamp    time.Time
}

type diagnosticLLM struct {
	mu          sync.Mutex
	calls       []llmCall
	response    string
	responseErr error
}

func (d *diagnosticLLM) Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, llmCall{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		Timestamp:    time.Now(),
	})
	return d.response, d.responseErr
}

func (d *diagnosticLLM) CompleteWithImage(_ context.Context, _, _, _ string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (d *diagnosticLLM) lastCall() *llmCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) == 0 {
		return nil
	}
	return &d.calls[len(d.calls)-1]
}

func (d *diagnosticLLM) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func setupConvertTest(t *testing.T, llm *diagnosticLLM) (*WritingService, repository.Repository) {
	t.Helper()
	db := setupTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger()

	svc := NewWritingService(repo, llm, "", 0, 0, &logger)
	return svc, repo
}

func createChannelWithTheme(t *testing.T, repo repository.Repository, userID, platform, style, theme string) string {
	t.Helper()
	ch := &model.Channel{
		ID:       uuid.NewString(),
		UserID:   userID,
		Platform: platform,
		Name:     "Convert Test Channel",
		Style:    style,
		Theme:    theme,
		Status:   model.ChannelStatusActive,
	}
	if err := repo.Channels().Create(context.Background(), ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch.ID
}

// sampleMarkdown is a realistic Chinese markdown with headings, bold, lists, blockquotes, and images.
const sampleMarkdown = `# 专注力的秘密

在这个信息爆炸的时代，专注力已经成为最稀缺的资源。

## 为什么专注力如此重要？

研究表明，持续的多任务处理会**降低 IQ 高达 15 分**。这不是危言耸听，而是有充分科学依据的事实。

### 三个核心原因

1. **认知切换成本**：每次切换任务，大脑需要 23 分钟重新进入状态
2. **深度工作减少**：碎片化时间无法产生突破性想法
3. **记忆力下降**：信息过载导致海马体功能受损

> 专注不是一种天赋，而是一种可以训练的技能。—— Cal Newport

## 如何提升专注力？

- 早上 9-11 点安排最重要的工作
- 关闭手机通知
- 使用番茄工作法（25 分钟专注 + 5 分钟休息）

![专注力示意图](./images/focus.jpg)

![参考图](https://example.com/reference.png)

## 总结

专注力是现代人最重要的竞争力。从小事开始，每天进步一点点。

*本文约 500 字，阅读时间约 3 分钟*
`

func logPhase(t *testing.T, phase int, msg string, args ...any) {
	t.Helper()
	t.Logf("=== PHASE %d: %s ===", phase, fmt.Sprintf(msg, args...))
}

func logPrompt(t *testing.T, prompt string) {
	t.Helper()
	t.Logf("  [PROMPT] length=%d", len(prompt))
	if len(prompt) > 200 {
		t.Logf("  [PROMPT] first 200 chars: %q", prompt[:200])
		t.Logf("  [PROMPT] last 200 chars: %q", prompt[len(prompt)-200:])
	} else {
		t.Logf("  [PROMPT] full content: %q", prompt)
	}
}

// ---------------------------------------------------------------------------
// Test 1: Full diagnostic trace (master test)
// ---------------------------------------------------------------------------

func TestConvertMarkdown_FullDiagnosticTrace(t *testing.T) {
	llm := &diagnosticLLM{
		response: `<section style="background:#faf9f5;"><p style="color:#4a413d;">专注力的秘密</p></section>`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-trace-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	logPhase(t, 0, "Channel created channel_id=%s theme=autumn-warm", channelID)

	result, err := svc.ConvertMarkdown(context.Background(), userID, channelID, sampleMarkdown, "")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	// Phase 1: Channel lookup
	logPhase(t, 1, "Channel lookup — OK (user=%s, channel=%s)", userID, channelID)

	// Phase 2: LLM call inspection
	call := llm.lastCall()
	if call == nil {
		t.Fatal("LLM was never called — converter may not have produced an AI_MODE_REQUEST sentinel")
	}
	logPhase(t, 2, "LLM call received — system_prompt_empty=%v user_prompt_len=%d",
		call.SystemPrompt == "", len(call.UserPrompt))

	logPrompt(t, call.UserPrompt)

	// Verify prompt contains theme-specific content
	if !strings.Contains(call.UserPrompt, "秋日暖光") {
		t.Logf("  [WARN] Prompt does NOT contain '秋日暖光' theme name")
	}
	if !strings.Contains(call.UserPrompt, "#faf9f5") {
		t.Logf("  [WARN] Prompt does NOT contain autumn-warm background color #faf9f5")
	}
	if !strings.Contains(call.UserPrompt, "专注力") {
		t.Logf("  [WARN] Prompt does NOT contain the markdown content '专注力'")
	}

	// Phase 3: Final result
	logPhase(t, 3, "Final result — html_len=%d image_count=%d", len(result.HTML), len(result.Images))
	t.Logf("  [HTML] first 200 chars: %q", result.HTML)
	for i, img := range result.Images {
		t.Logf("  [IMAGE %d] index=%d original=%q placeholder=%q", i, img.Index, img.Original, img.Placeholder)
	}

	if result.HTML == "" {
		t.Error("[FAIL] HTML is empty")
	}
	if len(result.Images) != 2 {
		t.Errorf("[FAIL] Expected 2 images, got %d", len(result.Images))
	}
}

// ---------------------------------------------------------------------------
// Test 2: Default theme (no prompt) — exposes the bug
// ---------------------------------------------------------------------------

func TestConvertMarkdown_DefaultTheme_NoPrompt(t *testing.T) {
	llm := &diagnosticLLM{
		response: `<p>Hello</p>`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-default-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	logPhase(t, 0, "Channel created with EMPTY theme (will resolve to 'autumn-warm' as new default)")

	result, err := svc.ConvertMarkdown(context.Background(), userID, channelID, "# Hello\n\nWorld", "")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	call := llm.lastCall()
	if call == nil {
		t.Fatal("LLM was never called")
	}

	t.Logf("  [DEFAULT THEME] Prompt length: %d", len(call.UserPrompt))

	// Now that default resolves to autumn-warm, the prompt should contain autumn-warm content
	if strings.Contains(call.UserPrompt, "秋日暖光") {
		t.Log("  [OK] Default resolved to autumn-warm — prompt contains theme content")
	} else if strings.Contains(call.UserPrompt, "微信公众号排版助手") {
		t.Log("  [FALLBACK] Theme not found, using generic prompt")
	} else {
		t.Logf("  [WARN] Unexpected prompt content, first 200 chars: %q", call.UserPrompt[:min(200, len(call.UserPrompt))])
	}

	t.Logf("  [DEFAULT THEME] HTML returned: %q", result.HTML)
}

// ---------------------------------------------------------------------------
// Test 3: Explicit theme arg override
// ---------------------------------------------------------------------------

func TestConvertMarkdown_ExplicitThemeArg(t *testing.T) {
	llm := &diagnosticLLM{
		response: `<section><p>Minimal</p></section>`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-override-001"
	// Channel has autumn-warm but we override to minimal-blue
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	result, err := svc.ConvertMarkdown(context.Background(), userID, channelID, "# Test", "minimal-blue")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	call := llm.lastCall()
	t.Logf("  [OVERRIDE] Prompt length: %d", len(call.UserPrompt))

	// Check the prompt is NOT using autumn-warm content
	if strings.Contains(call.UserPrompt, "秋日暖光") {
		t.Error("[FAIL] Prompt contains autumn-warm content — theme arg was not overridden")
	}

	t.Logf("  [OVERRIDE] HTML: %q", result.HTML)
}

// ---------------------------------------------------------------------------
// Test 4: Image extraction
// ---------------------------------------------------------------------------

func TestConvertMarkdown_WithImages(t *testing.T) {
	llm := &diagnosticLLM{
		response: `<p>Content</p><!-- IMG:0 --><p>More</p><!-- IMG:1 -->`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-images-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	markdown := `# Article

![local photo](./photos/test.jpg)

Some text here.

![online image](https://cdn.example.com/photo.png)

More text.

![another local](./img/hero.png)
`

	result, err := svc.ConvertMarkdown(context.Background(), userID, channelID, markdown, "")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	t.Logf("  [IMAGES] Total images extracted: %d", len(result.Images))
	for i, img := range result.Images {
		t.Logf("  [IMAGE %d] index=%d original=%q type=%s",
			i, img.Index, img.Original, classifyImage(img.Original))
	}

	if len(result.Images) != 3 {
		t.Errorf("[FAIL] Expected 3 images, got %d", len(result.Images))
	}
}

func classifyImage(original string) string {
	if strings.HasPrefix(original, "./") {
		return "local"
	}
	if strings.HasPrefix(original, "http") {
		return "online"
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// Test 5: Nonexistent theme → generic fallback
// ---------------------------------------------------------------------------

func TestConvertMarkdown_NonexistentTheme_Fallback(t *testing.T) {
	llm := &diagnosticLLM{
		response: `<p>Fallback</p>`,
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-fallback-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	result, err := svc.ConvertMarkdown(context.Background(), userID, channelID, "# Test", "nonexistent-xyz-999")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	call := llm.lastCall()
	t.Logf("  [FALLBACK] Prompt length: %d", len(call.UserPrompt))
	t.Logf("  [FALLBACK] Full prompt:\n%s", call.UserPrompt)

	if !strings.Contains(call.UserPrompt, "微信公众号排版助手") {
		t.Error("[FAIL] Prompt should contain generic prompt '微信公众号排版助手'")
	}
	t.Logf("  [FALLBACK] HTML: %q", result.HTML)
}

// ---------------------------------------------------------------------------
// Test 6: Empty markdown → error
// ---------------------------------------------------------------------------

func TestConvertMarkdown_EmptyMarkdown(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-empty-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	_, err := svc.ConvertMarkdown(context.Background(), userID, channelID, "", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for empty markdown, got nil")
	}
	t.Logf("  [EMPTY] Error: %v", err)

	if !strings.Contains(err.Error(), "markdown") {
		t.Errorf("[FAIL] Error should mention markdown, got: %v", err)
	}
	if llm.callCount() > 0 {
		t.Error("[FAIL] LLM should not have been called for empty markdown")
	}
}

// ---------------------------------------------------------------------------
// Test 7: Channel not found → error
// ---------------------------------------------------------------------------

func TestConvertMarkdown_ChannelNotFound(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, _ := setupConvertTest(t, llm)

	_, err := svc.ConvertMarkdown(context.Background(), "user-ghost", "nonexistent-channel-id", "# Test", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for nonexistent channel, got nil")
	}
	t.Logf("  [NOT FOUND] Error: %v", err)

	if llm.callCount() > 0 {
		t.Error("[FAIL] LLM should not have been called")
	}
}

// ---------------------------------------------------------------------------
// Test 8: Channel ownership mismatch → error
// ---------------------------------------------------------------------------

func TestConvertMarkdown_ChannelOwnershipMismatch(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, repo := setupConvertTest(t, llm)
	ownerID := "user-owner-001"
	otherID := "user-other-001"
	channelID := createChannelWithTheme(t, repo, ownerID, model.PlatformArticle, "", "autumn-warm")

	_, err := svc.ConvertMarkdown(context.Background(), otherID, channelID, "# Test", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for ownership mismatch, got nil")
	}
	t.Logf("  [OWNERSHIP] Error: %v", err)

	if !strings.Contains(err.Error(), "channel not owned") {
		t.Errorf("[FAIL] Error should mention ownership, got: %v", err)
	}
	if llm.callCount() > 0 {
		t.Error("[FAIL] LLM should not have been called")
	}
}

// ---------------------------------------------------------------------------
// Test 9: LLM returns error
// ---------------------------------------------------------------------------

func TestConvertMarkdown_LLMError(t *testing.T) {
	llm := &diagnosticLLM{
		responseErr: fmt.Errorf("API rate limit exceeded"),
	}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-llmerr-001"
	channelID := createChannelWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	_, err := svc.ConvertMarkdown(context.Background(), userID, channelID, "# Test\n\nHello", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error when LLM fails, got nil")
	}
	t.Logf("  [LLM ERROR] Error: %v", err)

	if !strings.Contains(err.Error(), "llm convert markdown") {
		t.Errorf("[FAIL] Error should be wrapped by 'llm convert markdown', got: %v", err)
	}

	// But the LLM should have been called with the prompt
	call := llm.lastCall()
	if call == nil {
		t.Error("[WARN] LLM was not called at all")
	} else {
		t.Logf("  [LLM ERROR] Prompt length before error: %d", len(call.UserPrompt))
	}
}
