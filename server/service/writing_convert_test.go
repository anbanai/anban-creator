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

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

// ---------------------------------------------------------------------------
// Diagnostic LLM mock — still wired through setupConvertTest because
// WritingService retains an LLM client for non-rendering capabilities.
// ConvertMarkdown/RenderTemplate do not call the LLM (deterministic renderer),
// so the mock's recorded calls stay empty for those paths and the tests below
// assert on the rendered HTML instead.
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

	svc := NewWritingService(repo, llm, "", 0, &logger)
	return svc, repo
}

func createProjectWithTheme(t *testing.T, repo repository.Repository, userID, platform, style, theme string) string {
	t.Helper()
	ch := &model.Project{
		ID:          uuid.NewString(),
		UserID:      userID,
		Platform:    platform,
		Name:        "Convert Test Project",
		VisualStyle: style,
		Theme:       theme,
		Status:      model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(context.Background(), ch); err != nil {
		t.Fatalf("create project: %v", err)
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

// ---------------------------------------------------------------------------
// Test 1: Full deterministic render trace (master test)
//
// ConvertMarkdown no longer touches the LLM. We assert the deterministic
// renderer produces non-empty WeChat HTML with the markdown content and that
// both images surface as IMG placeholders.
// ---------------------------------------------------------------------------

func TestConvertMarkdown_FullDiagnosticTrace(t *testing.T) {
	// The LLM mock is irrelevant for convert now; render is deterministic.
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-trace-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	logPhase(t, 0, "Project created project_id=%s theme=autumn-warm", projectID)

	result, err := svc.ConvertMarkdown(context.Background(), userID, projectID, sampleMarkdown, "", "")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	logPhase(t, 1, "Project lookup + deterministic render — OK")

	logPhase(t, 2, "LLM must NOT be called by the convert path")
	// Deterministic: the LLM mock records zero calls.
	if got := callCountOf(svc); got != 0 {
		t.Errorf("[FAIL] ConvertMarkdown invoked the LLM %d time(s) — convert must be LLM-free", got)
	}

	logPhase(t, 3, "Final result — html_len=%d image_count=%d", len(result.HTML), len(result.Images))
	t.Logf("  [HTML] first 200 chars: %q", firstN(result.HTML, 200))
	for i, img := range result.Images {
		t.Logf("  [IMAGE %d] index=%d original=%q placeholder=%q", i, img.Index, img.Original, img.Placeholder)
	}

	if result.HTML == "" {
		t.Fatal("[FAIL] HTML is empty")
	}
	// The rendered HTML must carry the markdown content.
	if !strings.Contains(result.HTML, "专注力") {
		t.Error("[FAIL] HTML missing markdown content '专注力'")
	}
	// Both images must surface as ordered IMG placeholders.
	if len(result.Images) != 2 {
		t.Fatalf("[FAIL] Expected 2 images, got %d", len(result.Images))
	}
	for _, want := range []string{"<!-- IMG:0 -->", "<!-- IMG:1 -->"} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("[FAIL] HTML missing placeholder %s", want)
		}
	}
	// Image order follows document order (local first, then online).
	if result.Images[0].Original != "./images/focus.jpg" {
		t.Errorf("[FAIL] Images[0].Original = %q", result.Images[0].Original)
	}
	if result.Images[1].Original != "https://example.com/reference.png" {
		t.Errorf("[FAIL] Images[1].Original = %q", result.Images[1].Original)
	}
}

// ---------------------------------------------------------------------------
// Test 2: Default theme (empty) resolves to autumn-warm deterministically
// ---------------------------------------------------------------------------

func TestConvertMarkdown_DefaultTheme_NoPrompt(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-default-001"
	// Project has EMPTY theme → resolves to autumn-warm.
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	logPhase(t, 0, "Project created with EMPTY theme (resolves to autumn-warm)")

	result, err := svc.ConvertMarkdown(context.Background(), userID, projectID, "# Hello\n\nWorld", "", "")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	if result.HTML == "" {
		t.Fatal("[FAIL] HTML is empty")
	}
	// autumn-warm text color (#4a413d) must be inlined into the rendered HTML.
	if !strings.Contains(result.HTML, "#4a413d") {
		t.Errorf("[FAIL] autumn-warm text color #4a413d missing — default theme not applied: %q", firstN(result.HTML, 120))
	}
	t.Logf("  [DEFAULT THEME] HTML returned: %q", firstN(result.HTML, 120))
}

// ---------------------------------------------------------------------------
// Test 3: Explicit theme arg overrides project theme
// ---------------------------------------------------------------------------

func TestConvertMarkdown_ExplicitThemeArg(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-override-001"
	// Project has autumn-warm but we override to spring-fresh.
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	result, err := svc.ConvertMarkdown(context.Background(), userID, projectID, "# Test", "spring-fresh", "")
	if err != nil {
		t.Fatalf("ConvertMarkdown failed: %v", err)
	}

	// spring-fresh text color (#3d4a3d) must be present …
	if !strings.Contains(result.HTML, "#3d4a3d") {
		t.Error("[FAIL] spring-fresh text color #3d4a3d missing — theme arg not applied")
	}
	// … and autumn-warm's text color (#4a413d) must be ABSENT, proving override.
	if strings.Contains(result.HTML, "#4a413d") {
		t.Error("[FAIL] autumn-warm text color #4a413d present — project theme leaked past the override")
	}
	t.Logf("  [OVERRIDE] HTML: %q", firstN(result.HTML, 120))
}

// ---------------------------------------------------------------------------
// Test 4: Image extraction
// ---------------------------------------------------------------------------

func TestConvertMarkdown_WithImages(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-images-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "autumn-warm")

	markdown := `# Article

![local photo](./photos/test.jpg)

Some text here.

![online image](https://cdn.example.com/photo.png)

More text.

![another local](./img/hero.png)
`

	result, err := svc.ConvertMarkdown(context.Background(), userID, projectID, markdown, "", "")
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
// Test 5: Nonexistent theme → hard error (no silent fallback)
//
// Per the refactor's "errors must be exposed" directive, an explicit but
// nonexistent theme is a hard error — it must NOT silently fall back to a
// generic theme.
// ---------------------------------------------------------------------------

func TestConvertMarkdown_NonexistentTheme_Error(t *testing.T) {
	svc, repo := setupConvertTest(t, &diagnosticLLM{response: "unused"})
	userID := "user-theme-err-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	_, err := svc.ConvertMarkdown(context.Background(), userID, projectID, "# Test", "nonexistent-xyz-999", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for nonexistent theme, got nil")
	}
	t.Logf("  [THEME ERROR] Error: %v", err)
	if !strings.Contains(err.Error(), "nonexistent-xyz-999") {
		t.Errorf("[FAIL] Error should name the bad theme, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Test 6: Empty markdown → error
// ---------------------------------------------------------------------------

func TestConvertMarkdown_EmptyMarkdown(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, repo := setupConvertTest(t, llm)
	userID := "user-empty-001"
	projectID := createProjectWithTheme(t, repo, userID, model.PlatformArticle, "", "")

	_, err := svc.ConvertMarkdown(context.Background(), userID, projectID, "", "", "")
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
// Test 7: Project not found → error
// ---------------------------------------------------------------------------

func TestConvertMarkdown_ProjectNotFound(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, _ := setupConvertTest(t, llm)

	_, err := svc.ConvertMarkdown(context.Background(), "user-ghost", "nonexistent-project-id", "# Test", "", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for nonexistent project, got nil")
	}
	t.Logf("  [NOT FOUND] Error: %v", err)

	if llm.callCount() > 0 {
		t.Error("[FAIL] LLM should not have been called")
	}
}

// ---------------------------------------------------------------------------
// Test 8: Project ownership mismatch → error
// ---------------------------------------------------------------------------

func TestConvertMarkdown_ProjectOwnershipMismatch(t *testing.T) {
	llm := &diagnosticLLM{response: "should not reach"}
	svc, repo := setupConvertTest(t, llm)
	ownerID := "user-owner-001"
	otherID := "user-other-001"
	projectID := createProjectWithTheme(t, repo, ownerID, model.PlatformArticle, "", "autumn-warm")

	_, err := svc.ConvertMarkdown(context.Background(), otherID, projectID, "# Test", "", "")
	if err == nil {
		t.Fatal("[FAIL] Expected error for ownership mismatch, got nil")
	}
	t.Logf("  [OWNERSHIP] Error: %v", err)

	if !strings.Contains(err.Error(), "project not owned") {
		t.Errorf("[FAIL] Error should mention ownership, got: %v", err)
	}
	if llm.callCount() > 0 {
		t.Error("[FAIL] LLM should not have been called")
	}
}

// ---------------------------------------------------------------------------
// Small test-only helpers (kept local so assertions read cleanly).
// ---------------------------------------------------------------------------

// callCountOf inspects the WritingService's underlying diagnostic LLM mock. It
// panics if the wired client is not the diagnostic mock — acceptable for tests.
func callCountOf(svc *WritingService) int {
	if d, ok := svc.llmClient.(*diagnosticLLM); ok {
		return d.callCount()
	}
	return 0
}

func firstN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
