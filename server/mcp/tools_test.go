package mcp

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

func setupAccountInfoTest(t *testing.T) (*service.TaskService, *service.ChannelService, repository.Repository, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "account_info_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Task{}, &model.Template{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.Nop()
	channelSvc := service.NewChannelService(repo, &logger)
	taskSvc := service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
	templateSvc := service.NewTemplateService(repo, &logger)

	old := svcs
	svcs = &Services{ChannelSvc: channelSvc, TaskSvc: taskSvc, TemplateSvc: templateSvc}

	cleanup := func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		svcs = old
	}
	return taskSvc, channelSvc, repo, cleanup
}

func createAccountInfoChannel(t *testing.T, repo repository.Repository, userID, style string) *model.Channel {
	t.Helper()
	ctx := context.Background()
	ch := &model.Channel{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "test-channel",
		Style:    style,
	}
	if err := repo.Channels().Create(ctx, ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	return ch
}

func createAccountInfoTask(t *testing.T, repo repository.Repository, userID, channelID, style string) *model.Task {
	t.Helper()
	ctx := context.Background()
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ChannelID: channelID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusPending,
		Style:     style,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

// TestBuildAccountInfo_NoTaskID_FallsBackToChannel: without task_id the channel's
// own style is returned with style_source="channel".
func TestBuildAccountInfo_NoTaskID_FallsBackToChannel(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "seednote",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["style"]; got != "极简扁平，蓝白配色" {
		t.Errorf("style = %v, want 极简扁平，蓝白配色", got)
	}
	if got := info["style_source"]; got != "channel" {
		t.Errorf("style_source = %v, want channel", got)
	}
}

// TestBuildAccountInfo_TaskStyleOverride: a task with its own style overrides
// the channel's style when task_id is supplied.
func TestBuildAccountInfo_TaskStyleOverride(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")
	task := createAccountInfoTask(t, repo, userID, ch.ID, "温暖治愈系，柔光摄影")

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "seednote",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["style"]; got != "温暖治愈系，柔光摄影" {
		t.Errorf("style = %v, want 温暖治愈系，柔光摄影", got)
	}
	if got := info["style_source"]; got != "task" {
		t.Errorf("style_source = %v, want task", got)
	}
	// seednote branch must still surface the channel's reference_image_url unchanged.
	imgCfg, ok := info["image_config"].(map[string]any)
	if !ok {
		t.Fatalf("image_config missing or wrong type: %T", info["image_config"])
	}
	if _, present := imgCfg["reference_image_url"]; !present {
		t.Errorf("image_config.reference_image_url missing")
	}
}

// TestBuildAccountInfo_TaskStyleEmpty_FallsBackToChannel: task_id supplied but
// task.Style is empty → channel style still wins, source is "channel".
func TestBuildAccountInfo_TaskStyleEmpty_FallsBackToChannel(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "seednote",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["style"]; got != "极简扁平，蓝白配色" {
		t.Errorf("style = %v, want 极简扁平，蓝白配色", got)
	}
	if got := info["style_source"]; got != "channel" {
		t.Errorf("style_source = %v, want channel (empty task.Style falls back)", got)
	}
}

// TestBuildAccountInfo_CrossChannel_Rejected: a task that belongs to a different
// channel must not leak its style into this profile response.
func TestBuildAccountInfo_CrossChannel_Rejected(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	chA := createAccountInfoChannel(t, repo, userID, "A风格")
	chB := createAccountInfoChannel(t, repo, userID, "B风格")
	// task belongs to channel B, but we query channel A.
	taskOnB := createAccountInfoTask(t, repo, userID, chB.ID, "B的task风格")

	_, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": chA.ID,
		"scope":      "seednote",
		"task_id":    taskOnB.ID,
	})
	if errMsg == "" {
		t.Fatal("expected error for cross-channel task, got nil")
	}
	if !strings.Contains(errMsg, "does not belong") {
		t.Errorf("error message = %q, want it to mention 'does not belong'", errMsg)
	}
}

// TestBuildAccountInfo_ArticleScope_TaskStyleOverride: effectiveStyle is computed
// before the scope switch, so the task-style override applies to article scope too.
// This guards against a future refactor that moves the override inside `case "seednote":`.
func TestBuildAccountInfo_ArticleScope_TaskStyleOverride(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	// Use a wechat-article channel so article scope is meaningful. The setup helper
	// creates seednote channels by default; override the platform here.
	ch := &model.Channel{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "article-channel",
		Style:    "channel-article-style",
	}
	if err := repo.Channels().Create(ctx, ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}
	task := createAccountInfoTask(t, repo, userID, ch.ID, "task-article-style")
	// Task.Type defaults to seednote in the helper; flip to article for realism.
	task.Type = model.PlatformArticle
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task type: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["style"]; got != "task-article-style" {
		t.Errorf("style = %v, want task-article-style", got)
	}
	if got := info["style_source"]; got != "task" {
		t.Errorf("style_source = %v, want task", got)
	}
	// Article scope must NOT include seednote's image_config block.
	if _, present := info["image_config"]; present {
		t.Errorf("article scope should not include image_config, got %v", info["image_config"])
	}
}

// TestBuildAccountInfo_CrossUser_RejectedAtChannelLookup: a request from a
// different user is rejected at the channel-ownership check (ChannelSvc.Get
// enforces ch.UserID == userID before we ever reach the task lookup). This means
// the inner `task.UserID != userID` guard in buildAccountInfo is defense-in-depth
// — it cannot be exercised through the public API surface today because
// ChannelSvc.Get short-circuits first. We keep the test to lock in the
// user-facing guarantee and to flag the day ChannelSvc.Get changes shape.
func TestBuildAccountInfo_CrossUser_RejectedAtChannelLookup(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherUserID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, ownerID, "owner风格")
	task := createAccountInfoTask(t, repo, ownerID, ch.ID, "owner的task风格")

	_, errMsg := buildAccountInfo(ctx, otherUserID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "seednote",
		"task_id":    task.ID,
	})
	if errMsg == "" {
		t.Fatal("expected error for cross-user access, got nil")
	}
	// The rejection surfaces as the channel-ownership error, NOT the task-belong
	// error, because ChannelSvc.Get runs first.
	if !strings.Contains(errMsg, "channel not owned") && !strings.Contains(errMsg, "owned by user") {
		t.Errorf("error message = %q, want it to mention channel ownership rejection", errMsg)
	}
}

// createAccountInfoTemplate inserts a template row directly via the repository
// (bypassing the service's name-derivation so we control every field).
func createAccountInfoTemplate(t *testing.T, repo repository.Repository, userID string) *model.Template {
	t.Helper()
	ctx := context.Background()
	tmpl := &model.Template{
		ID:             uuid.New().String(),
		UserID:         userID,
		Type:           model.PlatformArticle,
		Name:           "测试脚手架模板",
		Visibility:     "public",
		WritingStyle:   "犀利、接地气、像朋友聊天",
		Structure:      map[string]any{"text": "开头钩子 → 3 个论点 → 行动号召"},
		ExampleContent: map[string]any{"text": "示例正文片段……"},
		IsActive:       true,
	}
	if err := repo.Templates().Create(ctx, tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	return tmpl
}

// TestBuildAccountInfo_TemplateScaffold: a task carrying a template_id surfaces
// the template's writing style / structure / example in the profile response, so
// the agent can apply the content scaffold. Visual style (style/style_source) is
// unaffected — the scaffold rides on separate template_* keys.
func TestBuildAccountInfo_TemplateScaffold(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")
	tmpl := createAccountInfoTemplate(t, repo, userID)
	task := createAccountInfoTask(t, repo, userID, ch.ID, "温暖治愈系，柔光摄影")
	task.TemplateID = &tmpl.ID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task template_id: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["template_id"]; got != tmpl.ID {
		t.Errorf("template_id = %v, want %s", got, tmpl.ID)
	}
	if got := info["template_name"]; got != "测试脚手架模板" {
		t.Errorf("template_name = %v, want 测试脚手架模板", got)
	}
	if got := info["template_writing_style"]; got != "犀利、接地气、像朋友聊天" {
		t.Errorf("template_writing_style = %v, want 犀利、接地气、像朋友聊天", got)
	}
	if got := info["template_structure"]; got != "开头钩子 → 3 个论点 → 行动号召" {
		t.Errorf("template_structure = %v, want scaffold text", got)
	}
	if got := info["template_example"]; got != "示例正文片段……" {
		t.Errorf("template_example = %v, want example text", got)
	}
	// Visual style channel is untouched by the scaffold.
	if got := info["style"]; got != "温暖治愈系，柔光摄影" {
		t.Errorf("style = %v, want task visual style (unchanged)", got)
	}
}

// TestBuildAccountInfo_AuthorBylineAndWritingStyle: a 公众号 template carries TWO
// independent dimensions — 作者 (AuthorName, the published byline) and 写作风格
// (AuthorStyleIntro + optional AuthorAvatarURL, the writing imitation). The template
// AuthorName OVERRIDES the channel byline and surfaces as the resolved top-level
// `author` (precedence template > channel). AuthorStyleIntro surfaces as
// template_writing_style (writing direction); AuthorAvatarURL as template_author_avatar
// (persona avatar, NOT the byline). The two never derive from each other.
func TestBuildAccountInfo_AuthorBylineAndWritingStyle(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")

	// Template WITH 作者 (byline) + 写作风格 (imitation + avatar).
	tmpl := &model.Template{
		ID:               uuid.New().String(),
		UserID:           userID,
		Type:             model.PlatformArticle,
		Name:             "作者+写作风格模板",
		Visibility:       "public",
		AuthorName:       "老李",
		AuthorAvatarURL:  "https://example.com/avatar.png",
		AuthorStyleIntro: "犀利、接地气、像朋友聊天",
		IsActive:         true,
	}
	if err := repo.Templates().Create(ctx, tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	task := createAccountInfoTask(t, repo, userID, ch.ID, "温暖治愈系，柔光摄影")
	task.TemplateID = &tmpl.ID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task template_id: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	// 作者 (byline): template AuthorName overrides the (empty) channel author and
	// surfaces as the resolved top-level `author` that the agent passes to publish_draft.
	if got := info["author"]; got != "老李" {
		t.Errorf("author = %v, want 老李 (template byline overrides channel)", got)
	}
	if got := info["template_author_name"]; got != "老李" {
		t.Errorf("template_author_name = %v, want 老李", got)
	}
	// 写作风格 (imitation): AuthorStyleIntro drives template_writing_style (NOT the
	// empty WritingStyle). It is the writing direction, independent of the byline.
	if got := info["template_writing_style"]; got != "犀利、接地气、像朋友聊天" {
		t.Errorf("template_writing_style = %v, want the writing-style intro", got)
	}
	if got := info["template_author_avatar"]; got != "https://example.com/avatar.png" {
		t.Errorf("template_author_avatar = %v, want avatar url", got)
	}
}

// TestBuildAccountInfo_BylineFallbackToChannel: when the linked template has NO
// AuthorName, the channel's own byline (channel.Author) must be preserved as the
// top-level `author` — a template must never clobber the channel byline with an
// empty value. (precedence template > channel, but only when the template defines one.)
func TestBuildAccountInfo_BylineFallbackToChannel(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")
	ch.Author = "频道作者"
	if err := repo.Channels().Update(ctx, ch); err != nil {
		t.Fatalf("update channel author: %v", err)
	}
	// Template WITHOUT an 作者 (AuthorName) — only a writing-style intro.
	tmpl := &model.Template{
		ID:               uuid.New().String(),
		UserID:           userID,
		Type:             model.PlatformArticle,
		Name:             "无作者模板",
		Visibility:       "public",
		AuthorStyleIntro: "犀利、接地气",
		IsActive:         true,
	}
	if err := repo.Templates().Create(ctx, tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")
	task.TemplateID = &tmpl.ID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task template_id: %v", err)
	}
	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["author"]; got != "频道作者" {
		t.Errorf("author = %v, want 频道作者 (template without AuthorName keeps channel byline)", got)
	}
}

// TestBuildAccountInfo_TaskAuthorOverridesTemplateByline: the TOP rung of the author
// precedence chain — a task carrying its OWN Author wins over both the linked
// template's AuthorName and the channel byline. This rung was never asserted before
// (the test helpers create tasks with empty Author), so a refactor dropping the task
// rung of firstNonEmptyStr(task, template, channel) would slip through.
func TestBuildAccountInfo_TaskAuthorOverridesTemplateByline(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")
	ch.Author = "频道作者"
	if err := repo.Channels().Update(ctx, ch); err != nil {
		t.Fatalf("update channel author: %v", err)
	}
	tmpl := &model.Template{
		ID:         uuid.New().String(),
		UserID:     userID,
		Type:       model.PlatformArticle,
		Name:       "作者模板",
		Visibility: "public",
		AuthorName: "老李",
		IsActive:   true,
	}
	if err := repo.Templates().Create(ctx, tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")
	task.TemplateID = &tmpl.ID
	task.Author = "任务作者"
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task author: %v", err)
	}
	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["author"]; got != "任务作者" {
		t.Errorf("author = %v, want 任务作者 (task byline wins over template and channel)", got)
	}
}

// TestBuildAccountInfo_TemplateBylineOverridesChannelWhenTaskEmpty: the MIDDLE rung —
// when the task has no Author of its own but links a template that does, the template's
// AuthorName wins over the channel byline. This is the exact case the centralization
// fixed: persona used to be folded only in the !templateFromTask branch, so a task with
// template_id silently fell back to the channel byline instead of the template's.
// Together with _TaskAuthorOverridesTemplateByline this pins the full chain.
func TestBuildAccountInfo_TemplateBylineOverridesChannelWhenTaskEmpty(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")
	ch.Author = "频道作者"
	if err := repo.Channels().Update(ctx, ch); err != nil {
		t.Fatalf("update channel author: %v", err)
	}
	tmpl := &model.Template{
		ID:         uuid.New().String(),
		UserID:     userID,
		Type:       model.PlatformArticle,
		Name:       "作者模板",
		Visibility: "public",
		AuthorName: "老李",
		IsActive:   true,
	}
	if err := repo.Templates().Create(ctx, tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")
	task.TemplateID = &tmpl.ID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task template_id: %v", err)
	}
	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["author"]; got != "老李" {
		t.Errorf("author = %v, want 老李 (template byline wins over channel when task author is empty)", got)
	}
}

// TestBuildAccountInfo_TemplateDeleted_NoScaffold: a task whose template_id points
// at a deleted/non-existent template must not fail the whole profile — the
// template_* keys are simply absent. Guards against stale template_id rows.
func TestBuildAccountInfo_TemplateDeleted_NoScaffold(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoChannel(t, repo, userID, "极简扁平，蓝白配色")
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")
	ghostID := uuid.New().String() // no template row exists for this id
	task.TemplateID = &ghostID
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task template_id: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("expected no error for stale template_id, got: %s", errMsg)
	}
	for _, key := range []string{"template_id", "template_name", "template_writing_style", "template_structure", "template_example"} {
		if _, present := info[key]; present {
			t.Errorf("stale template should not surface %q, got %v", key, info[key])
		}
	}
}

// TestBuildAccountInfo_ChannelTemplateFallback: a 公众号 channel that BINDS a template
// (channel.TemplateID) — with no task-level template — resolves the persona (byline +
// writing style + avatar) and theme from the channel's bound template. This is the
// channel-template precedence rung: task > task-template > plan > channel-template >
// channel own fields. The bound template's theme is folded into the resolved `theme`
// with source "channel-template" so convert_markdown uses it.
func TestBuildAccountInfo_ChannelTemplateFallback(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()

	tmpl := &model.Template{
		ID:               uuid.New().String(),
		UserID:           userID,
		Type:             model.PlatformArticle,
		Name:             "频道绑定模板",
		Visibility:       "public",
		AuthorName:       "老李",
		AuthorStyleIntro: "犀利、接地气、像朋友聊天",
		AuthorAvatarURL:  "https://example.com/li.png",
		Theme:            "autumn-warm",
		IsActive:         true,
	}
	if err := repo.Templates().Create(ctx, tmpl); err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Article channel bound to the template; no task and no own persona/theme.
	ch := &model.Channel{
		ID:         uuid.New().String(),
		UserID:     userID,
		Platform:   model.PlatformArticle,
		Name:       "article-channel",
		TemplateID: tmpl.ID,
	}
	if err := repo.Channels().Create(ctx, ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	// 作者 (byline): channel-template AuthorName overrides the empty channel byline.
	if got := info["author"]; got != "老李" {
		t.Errorf("author = %v, want 老李 (channel-template byline)", got)
	}
	if got := info["template_writing_style"]; got != "犀利、接地气、像朋友聊天" {
		t.Errorf("template_writing_style = %v, want intro from channel-template", got)
	}
	if got := info["template_author_avatar"]; got != "https://example.com/li.png" {
		t.Errorf("template_author_avatar = %v, want avatar url from channel-template", got)
	}
	// Theme is folded into the resolved theme (channel-template rung).
	if got := info["theme"]; got != "autumn-warm" {
		t.Errorf("theme = %v, want autumn-warm (folded from channel-template)", got)
	}
	if got := info["theme_source"]; got != "channel-template" {
		t.Errorf("theme_source = %v, want channel-template", got)
	}
}

// TestBuildAccountInfo_NoTemplate_ChannelPersonaFallback: a channel with its OWN persona
// (author_style_intro / author_avatar_url) but no bound template surfaces that persona so
// a writing direction defined directly on the channel still reaches the agent. The
// channel's own byline (channel.Author) is the top-level `author` (lowest precedence rung).
func TestBuildAccountInfo_NoTemplate_ChannelPersonaFallback(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()

	ch := &model.Channel{
		ID:               uuid.New().String(),
		UserID:           userID,
		Platform:         model.PlatformArticle,
		Name:             "article-channel",
		Author:           "频道作者",
		AuthorStyleIntro: "平实、克制、重数据",
		AuthorAvatarURL:  "https://example.com/ch.png",
	}
	if err := repo.Channels().Create(ctx, ch); err != nil {
		t.Fatalf("create channel: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"channel_id": ch.ID,
		"scope":      "article",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	// Channel's own byline (no template to override it).
	if got := info["author"]; got != "频道作者" {
		t.Errorf("author = %v, want 频道作者 (channel own byline)", got)
	}
	// Channel's own persona surfaces as the writing direction / avatar fallback.
	if got := info["template_writing_style"]; got != "平实、克制、重数据" {
		t.Errorf("template_writing_style = %v, want channel author_style_intro fallback", got)
	}
	if got := info["template_author_avatar"]; got != "https://example.com/ch.png" {
		t.Errorf("template_author_avatar = %v, want channel avatar fallback", got)
	}
}
