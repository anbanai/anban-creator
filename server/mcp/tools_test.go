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

	"github.com/royalrick/anbanwriter/app/writer"
	"github.com/royalrick/anbanwriter/server/model"
	"github.com/royalrick/anbanwriter/server/repository"
	"github.com/royalrick/anbanwriter/server/service"
)

func setupAccountInfoTest(t *testing.T) (*service.TaskService, *service.ProjectService, repository.Repository, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "account_info_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Task{}, &model.Template{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.Nop()
	projectSvc := service.NewProjectService(repo, &logger)
	taskSvc := service.NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
	templateSvc := service.NewTemplateService(repo, &logger)

	old := svcs
	svcs = &Services{ProjectSvc: projectSvc, TaskSvc: taskSvc, TemplateSvc: templateSvc}

	cleanup := func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		svcs = old
	}
	return taskSvc, projectSvc, repo, cleanup
}

func createAccountInfoProject(t *testing.T, repo repository.Repository, userID, style string) *model.Project {
	t.Helper()
	ctx := context.Background()
	ch := &model.Project{
		ID:          uuid.New().String(),
		UserID:      userID,
		Platform:    model.PlatformSeednote,
		Name:        "test-project",
		VisualStyle: style,
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return ch
}

// createAccountInfoTask persists a seednote task. When visualStyle is non-empty it
// is stored as a per-task VisualStyle OVERRIDE (the only thing a task carries);
// empty means "inherit the project verbatim" (no override).
func createAccountInfoTask(t *testing.T, repo repository.Repository, userID, projectID, visualStyle string) *model.Task {
	t.Helper()
	ctx := context.Background()
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusPending,
	}
	if visualStyle != "" {
		task.SetOverrides(model.StyleOverrides{VisualStyle: visualStyle})
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	return task
}

// TestBuildAccountInfo_NoTaskID_FallsBackToProject: without task_id the project's
// own visual_style is returned with visual_style_source="project".
func TestBuildAccountInfo_NoTaskID_FallsBackToProject(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoProject(t, repo, userID, "极简扁平，蓝白配色")
	ch.Positioning = "旧定位不应进入 MCP profile"
	ch.Instructions = "面向独立开发者的效率工具项目"
	if err := repo.Projects().Update(ctx, ch); err != nil {
		t.Fatalf("update project instructions: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "seednote",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["visual_style"]; got != "极简扁平，蓝白配色" {
		t.Errorf("visual_style = %v, want 极简扁平，蓝白配色", got)
	}
	if got := info["visual_style_source"]; got != "project" {
		t.Errorf("visual_style_source = %v, want project", got)
	}
	if got := info["instructions"]; got != "面向独立开发者的效率工具项目" {
		t.Errorf("instructions = %v, want canonical instructions", got)
	}
	if got := info["positioning"]; got != "面向独立开发者的效率工具项目" {
		t.Errorf("positioning = %v, want canonical instructions compatibility value", got)
	}
}

// TestBuildAccountInfo_TaskStyleOverride: a task carrying a per-task VisualStyle
// override wins over the project's visual_style when task_id is supplied.
func TestBuildAccountInfo_TaskStyleOverride(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoProject(t, repo, userID, "极简扁平，蓝白配色")
	task := createAccountInfoTask(t, repo, userID, ch.ID, "温暖治愈系，柔光摄影")

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "seednote",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["visual_style"]; got != "温暖治愈系，柔光摄影" {
		t.Errorf("visual_style = %v, want 温暖治愈系，柔光摄影", got)
	}
	if got := info["visual_style_source"]; got != "task" {
		t.Errorf("visual_style_source = %v, want task", got)
	}
	// seednote branch must still surface the project's reference_image_url unchanged.
	imgCfg, ok := info["image_config"].(map[string]any)
	if !ok {
		t.Fatalf("image_config missing or wrong type: %T", info["image_config"])
	}
	if _, present := imgCfg["reference_image_url"]; !present {
		t.Errorf("image_config.reference_image_url missing")
	}
}

func TestBuildAccountInfo_TaskProjectSnapshotWinsOverCurrentProject(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoProject(t, repo, userID, "当前视觉")
	ch.Name = "当前项目名"
	ch.Instructions = "当前定位"
	ch.ReferenceImageURL = "/api/v1/files/current-ref"
	if err := repo.Projects().Update(ctx, ch); err != nil {
		t.Fatalf("update project: %v", err)
	}
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")
	task.SetProjectSnapshot(model.ProjectSnapshot{
		ProjectName:       "快照项目名",
		Platform:          model.PlatformSeednote,
		Instructions:      "快照定位",
		VisualStyle:       "快照视觉",
		ReferenceImageURL: "/api/v1/files/snapshot-ref",
	})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task snapshot: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "seednote",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["name"]; got != "快照项目名" {
		t.Fatalf("name = %v, want snapshot name", got)
	}
	if got := info["instructions"]; got != "快照定位" {
		t.Fatalf("instructions = %v, want snapshot instructions", got)
	}
	if got := info["visual_style"]; got != "快照视觉" {
		t.Fatalf("visual_style = %v, want snapshot visual style", got)
	}
	if got := info["visual_style_source"]; got != "snapshot" {
		t.Fatalf("visual_style_source = %v, want snapshot", got)
	}
	imgCfg := info["image_config"].(map[string]any)
	if got := imgCfg["reference_image_url"]; got != "/api/v1/files/snapshot-ref" {
		t.Fatalf("reference_image_url = %v, want snapshot ref", got)
	}
}

// TestBuildAccountInfo_TaskStyleEmpty_FallsBackToProject: task_id supplied but the
// task carries no VisualStyle override → project visual_style wins, source "project".
func TestBuildAccountInfo_TaskStyleEmpty_FallsBackToProject(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoProject(t, repo, userID, "极简扁平，蓝白配色")
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "seednote",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["visual_style"]; got != "极简扁平，蓝白配色" {
		t.Errorf("visual_style = %v, want 极简扁平，蓝白配色", got)
	}
	if got := info["visual_style_source"]; got != "project" {
		t.Errorf("visual_style_source = %v, want project (empty override falls back)", got)
	}
}

// TestBuildAccountInfo_CrossProject_Rejected: a task that belongs to a different
// project must not leak its overrides into this profile response.
func TestBuildAccountInfo_CrossProject_Rejected(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	chA := createAccountInfoProject(t, repo, userID, "A风格")
	chB := createAccountInfoProject(t, repo, userID, "B风格")
	// task belongs to project B, but we query project A.
	taskOnB := createAccountInfoTask(t, repo, userID, chB.ID, "B的task风格")

	_, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": chA.ID,
		"scope":      "seednote",
		"task_id":    taskOnB.ID,
	})
	if errMsg == "" {
		t.Fatal("expected error for cross-project task, got nil")
	}
	if !strings.Contains(errMsg, "does not belong") {
		t.Errorf("error message = %q, want it to mention 'does not belong'", errMsg)
	}
}

// TestBuildAccountInfo_ArticleScope_TaskStyleOverride: resolution happens before the
// scope switch, so a per-task VisualStyle override applies to article scope too. This
// guards against a future refactor that moves the override inside `case "seednote":`.
func TestBuildAccountInfo_ArticleScope_TaskStyleOverride(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:          uuid.New().String(),
		UserID:      userID,
		Platform:    model.PlatformArticle,
		Name:        "article-project",
		VisualStyle: "project-article-style",
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := createAccountInfoTask(t, repo, userID, ch.ID, "task-article-style")
	task.Type = model.PlatformArticle
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task type: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["visual_style"]; got != "task-article-style" {
		t.Errorf("visual_style = %v, want task-article-style", got)
	}
	if got := info["visual_style_source"]; got != "task" {
		t.Errorf("visual_style_source = %v, want task", got)
	}
	// Article scope must NOT include seednote's image_config block.
	if _, present := info["image_config"]; present {
		t.Errorf("article scope should not include image_config, got %v", info["image_config"])
	}
}

// TestBuildAccountInfo_ArticleWriterDefault: an article project with no writer key
// resolves to the platform default (writer.DefaultStyleName) at resolution time, with
// source "project". The default is NOT stored on the project — it surfaces only via
// ResolveStyle, the single place every consumer reads it.
func TestBuildAccountInfo_ArticleWriterDefault(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "article-project",
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "article",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["writer"]; got != writer.DefaultStyleName {
		t.Errorf("writer = %v, want %q (article platform default)", got, writer.DefaultStyleName)
	}
	if got := info["writer_source"]; got != "project" {
		t.Errorf("writer_source = %v, want project", got)
	}
}

// TestBuildAccountInfo_CrossUser_RejectedAtProjectLookup: a request from a different
// user is rejected at the project-ownership check (ProjectSvc.Get enforces
// ch.UserID == userID before we ever reach the task lookup). This means the inner
// `task.UserID != userID` guard in buildAccountInfo is defense-in-depth — it cannot
// be exercised through the public API surface today because ProjectSvc.Get
// short-circuits first. We keep the test to lock in the user-facing guarantee.
func TestBuildAccountInfo_CrossUser_RejectedAtProjectLookup(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherUserID := uuid.New().String()
	ch := createAccountInfoProject(t, repo, ownerID, "owner风格")
	task := createAccountInfoTask(t, repo, ownerID, ch.ID, "owner的task风格")

	_, errMsg := buildAccountInfo(ctx, otherUserID, map[string]any{
		"project_id": ch.ID,
		"scope":      "seednote",
		"task_id":    task.ID,
	})
	if errMsg == "" {
		t.Fatal("expected error for cross-user access, got nil")
	}
	// The rejection surfaces as the project-ownership error, NOT the task-belong
	// error, because ProjectSvc.Get runs first.
	if !strings.Contains(errMsg, "project not owned") && !strings.Contains(errMsg, "owned by user") {
		t.Errorf("error message = %q, want it to mention project ownership rejection", errMsg)
	}
}

// TestBuildAccountInfo_AuthorAndWriter: runtime profile exposes only the
// dimensions the agent actually consumes. author (公众号发布署名) and writer
// (写作风格 key) surface independently; persona avatar is Studio-only display
// metadata and must not leak to Agent/MCP.
func TestBuildAccountInfo_AuthorAndWriter(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "article-project",
		Author:   "老李",
		Writer:   "dan-koe",
		Theme:    "autumn-warm",
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "article",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["author"]; got != "老李" {
		t.Errorf("author = %v, want 老李", got)
	}
	if got := info["author_source"]; got != "project" {
		t.Errorf("author_source = %v, want project", got)
	}
	if got := info["writer"]; got != "dan-koe" {
		t.Errorf("writer = %v, want dan-koe", got)
	}
	if got := info["writer_source"]; got != "project" {
		t.Errorf("writer_source = %v, want project", got)
	}
	if _, present := info["persona_avatar"]; present {
		t.Errorf("persona_avatar must not be exposed to agent profile: %v", info["persona_avatar"])
	}
	if _, present := info["persona_avatar_source"]; present {
		t.Errorf("persona_avatar_source must not be exposed to agent profile: %v", info["persona_avatar_source"])
	}
	if got := info["theme"]; got != "autumn-warm" {
		t.Errorf("theme = %v, want autumn-warm", got)
	}
	if got := info["theme_source"]; got != "project" {
		t.Errorf("theme_source = %v, want project", got)
	}
}

// TestBuildAccountInfo_TaskAuthorOverridesProject: a task carrying its OWN author
// override wins over the project author (the top rung of the two-layer resolution).
func TestBuildAccountInfo_TaskAuthorOverridesProject(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "article-project",
		Author:   "项目作者",
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: ch.ID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusPending,
	}
	task.SetOverrides(model.StyleOverrides{Author: "任务作者"})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["author"]; got != "任务作者" {
		t.Errorf("author = %v, want 任务作者 (task override wins over project)", got)
	}
	if got := info["author_source"]; got != "task" {
		t.Errorf("author_source = %v, want task", got)
	}
}

// TestBuildAccountInfo_AuthorFallbackToProject: when a task carries no author
// override, the project's own author is preserved with source "project" — an empty
// override never clobbers the project value.
func TestBuildAccountInfo_AuthorFallbackToProject(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "article-project",
		Author:   "项目作者",
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}
	// Task with no overrides at all — fully inherits the project.
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: ch.ID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusPending,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "article",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["author"]; got != "项目作者" {
		t.Errorf("author = %v, want 项目作者 (no override keeps project author)", got)
	}
	if got := info["author_source"]; got != "project" {
		t.Errorf("author_source = %v, want project", got)
	}
}

// TestBuildAccountInfo_NoTemplateNamespace: the old template_* namespace is GONE.
// A profile response never carries template_id / template_name / template_writing_style
// / template_structure / template_example / template_author_avatar — templates are
// project-creation starters only and no longer ride on the resolved profile.
func TestBuildAccountInfo_NoTemplateNamespace(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := createAccountInfoProject(t, repo, userID, "极简扁平，蓝白配色")

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "article",
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	for _, key := range []string{
		"template_id", "template_name", "template_writing_style",
		"template_structure", "template_example", "template_author_avatar",
		"template_author", "template_theme", "style", "writing_style",
		"author_name", "writer_key", "byline", "writing_voice",
	} {
		if _, present := info[key]; present {
			t.Errorf("profile should not surface legacy key %q, got %v", key, info[key])
		}
	}
}

func TestParseStringArray(t *testing.T) {
	args := map[string]any{
		"refs":   []any{"/a.png", "/b.png", "", 123, "/c.png"},
		"raw":    []string{"/x.png", "/y.png"},
		"scalar": "/not-array.png",
	}
	got := parseStringArray(args, "refs")
	if len(got) != 3 || got[0] != "/a.png" || got[1] != "/b.png" || got[2] != "/c.png" {
		t.Errorf("refs = %v, want [/a /b /c] (empty + non-string dropped)", got)
	}
	gotRaw := parseStringArray(args, "raw")
	if len(gotRaw) != 2 || gotRaw[1] != "/y.png" {
		t.Errorf("raw = %v, want [/x /y]", gotRaw)
	}
	if gotNil := parseStringArray(args, "absent"); gotNil != nil {
		t.Errorf("absent key = %v, want nil", gotNil)
	}
	if gotScalar := parseStringArray(args, "scalar"); gotScalar != nil {
		t.Errorf("scalar (non-array) = %v, want nil", gotScalar)
	}
}
