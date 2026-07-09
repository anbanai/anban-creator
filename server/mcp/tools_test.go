package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/app/writer"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
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
	planSvc := service.NewPlanService(repo, &logger)
	templateSvc := service.NewTemplateService(repo, &logger)

	old := svcs
	svcs = &Services{ProjectSvc: projectSvc, TaskSvc: taskSvc, PlanSvc: planSvc, TemplateSvc: templateSvc}

	cleanup := func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		svcs = old
	}
	return taskSvc, projectSvc, repo, cleanup
}

func decodeMCPMap(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	if result == nil || len(result.Content) == 0 {
		t.Fatal("empty MCP result")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("unexpected MCP content type: %T", result.Content[0])
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(text.Text), &data); err != nil {
		t.Fatalf("decode MCP JSON %q: %v", text.Text, err)
	}
	return data
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

func TestTaskListHandlerSplitsVideoFields(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()

	userID := uuid.New().String()
	ctx := context.Background()
	creatorProject := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
		Name:     "creator",
		Status:   model.ProjectStatusActive,
	}
	editorProject := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideoEditor,
		Name:     "editor",
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(ctx, creatorProject); err != nil {
		t.Fatalf("create creator project: %v", err)
	}
	if err := repo.Projects().Create(ctx, editorProject); err != nil {
		t.Fatalf("create editor project: %v", err)
	}
	creatorTask := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: creatorProject.ID,
		Type:      model.PlatformVideoCreator,
		Status:    model.TaskStatusPending,
		Prompt:    "生成产品短片",
	}
	creatorTask.SetVideoInput(model.VideoInput{Brief: "生成产品短片"})
	creatorTask.SetVideoConfig(model.VideoTaskConfig{Ratio: "9:16"})
	editorTask := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: editorProject.ID,
		Type:      model.PlatformVideoEditor,
		Status:    model.TaskStatusPending,
		Prompt:    "剪辑源视频",
	}
	editorTask.SetVideoInput(model.VideoInput{Brief: "剪辑源视频", References: []model.VideoReferenceAsset{{Type: "video_url", URL: "https://cdn.example.com/source.mp4"}}})
	if err := repo.Tasks().Create(ctx, creatorTask); err != nil {
		t.Fatalf("create creator task: %v", err)
	}
	if err := repo.Tasks().Create(ctx, editorTask); err != nil {
		t.Fatalf("create editor task: %v", err)
	}

	result, err := taskListHandler(withMCPUserID(context.Background(), userID), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{"limit":10}`)}})
	if err != nil {
		t.Fatalf("taskListHandler: %v", err)
	}
	data := decodeMCPMap(t, result)
	items, ok := data["items"].([]any)
	if !ok || len(items) != 2 {
		t.Fatalf("items = %#v, want two tasks", data["items"])
	}
	for _, item := range items {
		task, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("task item type = %T", item)
		}
		if _, ok := task["video_input"]; ok {
			t.Fatalf("MCP task_list exposed generic video_input: %#v", task)
		}
		if _, ok := task["video_config"]; ok {
			t.Fatalf("MCP task_list exposed generic video_config: %#v", task)
		}
		switch task["type"] {
		case model.PlatformVideoCreator:
			if _, ok := task["video_creator_input"]; !ok {
				t.Fatalf("creator task missing video_creator_input: %#v", task)
			}
			if _, ok := task["video_editor_input"]; ok {
				t.Fatalf("creator task exposed editor input: %#v", task)
			}
		case model.PlatformVideoEditor:
			if _, ok := task["video_editor_input"]; !ok {
				t.Fatalf("editor task missing video_editor_input: %#v", task)
			}
			if _, ok := task["video_creator_input"]; ok {
				t.Fatalf("editor task exposed creator input: %#v", task)
			}
		default:
			t.Fatalf("unexpected task type: %#v", task)
		}
	}
}

func TestPlanHandlersSplitVideoFields(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()

	userID := uuid.New().String()
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
		Name:     "creator",
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(context.Background(), project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	reqBody := json.RawMessage(`{"project_id":"` + project.ID + `","cron_expr":"0 9 * * *","prompt":"每日生成新品短视频"}`)
	result, err := planCreateHandler(withMCPUserID(context.Background(), userID), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: reqBody}})
	if err != nil {
		t.Fatalf("planCreateHandler: %v", err)
	}
	created := decodeMCPMap(t, result)
	if _, ok := created["video_creator_input"]; !ok {
		t.Fatalf("MCP plan_create missing video_creator_input: %#v", created)
	}
	if _, ok := created["video_input"]; ok {
		t.Fatalf("MCP plan_create exposed generic video_input: %#v", created)
	}
	if _, ok := created["video_config"]; ok {
		t.Fatalf("MCP plan_create exposed generic video_config: %#v", created)
	}

	listResult, err := planListHandler(withMCPUserID(context.Background(), userID), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)}})
	if err != nil {
		t.Fatalf("planListHandler: %v", err)
	}
	list := decodeMCPMap(t, listResult)
	items, ok := list["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one plan", list["items"])
	}
	plan := items[0].(map[string]any)
	if _, ok := plan["video_creator_input"]; !ok {
		t.Fatalf("MCP plan_list missing video_creator_input: %#v", plan)
	}
	if _, ok := plan["video_input"]; ok {
		t.Fatalf("MCP plan_list exposed generic video_input: %#v", plan)
	}
	if _, ok := plan["video_config"]; ok {
		t.Fatalf("MCP plan_list exposed generic video_config: %#v", plan)
	}
}

func TestBuildAccountInfoSplitsVideoProjectProfiles(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New().String()
	creatorProject := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
		Name:     "creator",
		Status:   model.ProjectStatusActive,
	}
	editorProject := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideoEditor,
		Name:     "editor",
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(ctx, creatorProject); err != nil {
		t.Fatalf("create creator project: %v", err)
	}
	if err := repo.Projects().Create(ctx, editorProject); err != nil {
		t.Fatalf("create editor project: %v", err)
	}

	creator, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": creatorProject.ID,
		"scope":      model.PlatformVideoCreator,
	})
	if errMsg != "" {
		t.Fatalf("creator profile error: %s", errMsg)
	}
	if _, ok := creator["videocreator"].(map[string]any); !ok {
		t.Fatalf("creator profile missing videocreator block: %#v", creator)
	}
	if _, ok := creator["videoeditor"]; ok {
		t.Fatalf("creator profile exposed videoeditor block: %#v", creator)
	}
	if _, ok := creator["video"]; ok {
		t.Fatalf("creator profile exposed generic video block: %#v", creator)
	}
	brief, ok := creator["agent_brief"].(string)
	if !ok || !strings.Contains(brief, "video_creator_input") || !strings.Contains(brief, "videocreator.model_catalog") {
		t.Fatalf("creator agent_brief = %#v, want videocreator contract", creator["agent_brief"])
	}

	editor, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": editorProject.ID,
		"scope":      model.PlatformVideoEditor,
	})
	if errMsg != "" {
		t.Fatalf("editor profile error: %s", errMsg)
	}
	editorBlock, ok := editor["videoeditor"].(map[string]any)
	if !ok {
		t.Fatalf("editor profile missing videoeditor block: %#v", editor)
	}
	if _, ok := editor["videocreator"]; ok {
		t.Fatalf("editor profile exposed videocreator block: %#v", editor)
	}
	if _, ok := editor["video"]; ok {
		t.Fatalf("editor profile exposed generic video block: %#v", editor)
	}
	if _, ok := editor["agent_brief"]; ok {
		t.Fatalf("editor profile should not expose videocreator agent_brief: %#v", editor["agent_brief"])
	}
	if editorBlock["requires_source_media"] != true {
		t.Fatalf("editor requires_source_media = %#v, want true", editorBlock["requires_source_media"])
	}
	delivery, _ := editorBlock["delivery"].([]string)
	if strings.Join(delivery, ",") != "final.mp4,preview.mp4,capcut draft" {
		t.Fatalf("editor delivery = %#v, want final/preview/capcut", editorBlock["delivery"])
	}
}

func taskToolRequest(t *testing.T, taskID string) *mcp.CallToolRequest {
	t.Helper()
	args, err := json.Marshal(map[string]string{"task_id": taskID})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: args}}
}

func TestTaskGetHandlerRejectsForeignTask(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()

	ownerID := uuid.New().String()
	intruderID := uuid.New().String()
	project := createAccountInfoProject(t, repo, ownerID, "")
	task := createAccountInfoTask(t, repo, ownerID, project.ID, "")

	result, err := taskGetHandler(withMCPUserID(context.Background(), intruderID), taskToolRequest(t, task.ID))
	if err != nil {
		t.Fatalf("taskGetHandler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "task not found") {
		t.Fatalf("expected ownership error, got: %s", text)
	}
}

func TestTaskGetHandlerReturnsSplitVideoFields(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()

	ctx := context.Background()
	userID := uuid.New().String()
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformVideoCreator,
		Name:     "video-project",
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformVideoCreator,
		Status:    model.TaskStatusPending,
		Prompt:    "生成产品视频",
	}
	task.SetVideoInput(model.VideoInput{Brief: "使用用户素材生成短视频"})
	task.SetVideoConfig(model.VideoTaskConfig{Ratio: "9:16", Duration: 12})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create video task: %v", err)
	}

	result, err := taskGetHandler(withMCPUserID(ctx, userID), taskToolRequest(t, task.ID))
	if err != nil {
		t.Fatalf("taskGetHandler: %v", err)
	}
	data := decodeMCPMap(t, result)
	if _, ok := data["video_input"]; ok {
		t.Fatalf("task_get exposed generic video_input: %#v", data)
	}
	if _, ok := data["video_config"]; ok {
		t.Fatalf("task_get exposed generic video_config: %#v", data)
	}
	input, ok := data["video_creator_input"].(map[string]any)
	if !ok {
		t.Fatalf("video_creator_input missing or wrong type: %T", data["video_creator_input"])
	}
	if got := input["brief"]; got != "使用用户素材生成短视频" {
		t.Fatalf("video_creator_input.brief = %v", got)
	}
	cfg, ok := data["video_creator_config"].(map[string]any)
	if !ok {
		t.Fatalf("video_creator_config missing or wrong type: %T", data["video_creator_config"])
	}
	if got := cfg["ratio"]; got != "9:16" {
		t.Fatalf("video_creator_config.ratio = %v", got)
	}
	if got := cfg["duration"]; got != float64(12) {
		t.Fatalf("video_creator_config.duration = %v", got)
	}
}

func TestTaskCancelHandlerRejectsForeignTaskWithoutStatusChange(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()

	ownerID := uuid.New().String()
	intruderID := uuid.New().String()
	project := createAccountInfoProject(t, repo, ownerID, "")
	task := createAccountInfoTask(t, repo, ownerID, project.ID, "")
	task.Status = model.TaskStatusRunning
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatalf("update task: %v", err)
	}

	result, err := taskCancelHandler(withMCPUserID(context.Background(), intruderID), taskToolRequest(t, task.ID))
	if err != nil {
		t.Fatalf("taskCancelHandler: %v", err)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "task not found") {
		t.Fatalf("expected ownership error, got: %s", text)
	}
	reloaded, err := repo.Tasks().FindByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if reloaded.Status != model.TaskStatusRunning {
		t.Fatalf("foreign cancel changed status to %q", reloaded.Status)
	}
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

func TestBuildAccountInfo_MomentsProfileIncludesDeliveryContract(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:                uuid.New().String(),
		UserID:            userID,
		Platform:          model.PlatformMoments,
		Name:              "私域朋友圈",
		Status:            model.ProjectStatusActive,
		Instructions:      "高信任成交内容",
		Keywords:          "私域,成交,生活方式",
		VisualStyle:       "真实手机随拍，自然光",
		ReferenceImageURL: "/api/v1/files/ref-card.png",
		ImageRatio:        "3:4",
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create moments project: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: ch.ID,
		Type:      model.PlatformMoments,
		Status:    model.TaskStatusPending,
	}
	task.SetProjectSnapshot(model.SnapshotProject(ch))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create moments task: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"scope":      "moments",
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	if got := info["platform"]; got != model.PlatformMoments {
		t.Fatalf("platform = %v, want moments", got)
	}
	moments, ok := info["moments"].(map[string]any)
	if !ok {
		t.Fatalf("moments block missing or wrong type: %T", info["moments"])
	}
	artifacts, _ := moments["required_artifacts"].([]string)
	if strings.Join(artifacts, ",") != "material-analysis.md,content.md,quality-review.md" {
		t.Fatalf("required artifacts = %#v", artifacts)
	}
	if _, ok := moments["image_skill"]; ok {
		t.Fatalf("image_skill should not be advertised for moments profile: %v", moments["image_skill"])
	}
	imgCfg := info["image_config"].(map[string]any)
	if got := imgCfg["default_ratio"]; got != "3:4" {
		t.Fatalf("default_ratio = %v, want 3:4", got)
	}
	if _, ok := imgCfg["optional_skill"]; ok {
		t.Fatalf("optional_skill should not be advertised for moments image_config: %v", imgCfg["optional_skill"])
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

func TestBuildAccountInfo_EcommerceProjectAutoReturnsEcommerceBlockWithoutScope(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:           uuid.New().String(),
		UserID:       userID,
		Platform:     model.PlatformEcommerce,
		Name:         "ecommerce-project",
		Instructions: "茶品牌电商项目",
		Keywords:     "茶叶,礼盒",
	}
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:            uuid.New().String(),
		UserID:        userID,
		ProjectID:     ch.ID,
		Type:          model.PlatformEcommerce,
		Status:        model.TaskStatusPending,
		ImageModelKey: "openai-gpt-image",
	}
	task.SetEcommerce(model.EcommerceConfig{
		SelectedModules: map[string]int{"main_images": 3},
		ProductPhotos:   []string{"https://cdn.example.com/tea.png"},
		TargetPlatform:  "tmall",
		SellingPoints:   "高山春茶",
		Language:        "zh-CN",
		BrandBrief:      "年轻化茶品牌",
	})
	task.SetProjectSnapshot(model.SnapshotProject(ch))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	ec, ok := info["ecommerce"].(map[string]any)
	if !ok {
		t.Fatalf("ecommerce block missing without scope: %#v", info)
	}
	if got := ec["product_photo_count"]; got != 1 {
		t.Fatalf("product_photo_count = %v, want 1", got)
	}
	if got := ec["target_platform"]; got != "tmall" {
		t.Fatalf("target_platform = %v, want tmall", got)
	}
}

func TestBuildAccountInfo_MontageProjectReturnsMontageBlock(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	oldBillSvc := billSvc
	defer func() { billSvc = oldBillSvc }()
	SetBillingServices(nil, nil, &srvconfig.Config{Montage: srvconfig.MontageConfig{
		ProviderEnv: map[string]string{
			"FAL_KEY":        "fal-secret",
			"RUNWAY_API_KEY": "",
		},
		ToolPolicy: map[string]srvconfig.MontageToolCapabilityPolicy{
			"video_generation": {Preferred: []string{"fal"}},
		},
		PipelineDefaults: map[string]map[string]any{
			"social-short": {"budget_usd": 2.0, "video_generation": "auto"},
		},
	}})
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:           uuid.New().String(),
		UserID:       userID,
		Platform:     model.PlatformMontage,
		Name:         "montage-project",
		Instructions: "短视频自动剪辑项目",
	}
	ch.SetMontageDefaults(model.MontageDefaults{
		DefaultPipeline: "social-short",
		Preferences: model.MontagePreferences{
			AspectRatio:     "9:16",
			DurationSeconds: 45,
		},
		AssetGuidance:   "优先使用用户上传的视频素材",
		DeliveryTargets: []string{"final_video"},
	})
	if err := repo.Projects().Create(ctx, ch); err != nil {
		t.Fatalf("create project: %v", err)
	}
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: ch.ID,
		Type:      model.PlatformMontage,
		Status:    model.TaskStatusPending,
		Prompt:    "生成发布预告短片",
	}
	task.SetMontageInput(model.MontageInput{
		Brief:       "生成发布预告短片",
		PipelineKey: "social-short",
		SourceAssets: []model.MontageAsset{{
			Type: "video_url",
			URL:  "/api/v1/files/source.mp4",
		}},
		Preferences: model.MontagePreferences{
			AspectRatio:     "9:16",
			DurationSeconds: 30,
		},
	})
	task.SetProjectSnapshot(model.SnapshotProject(ch))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	info, errMsg := buildAccountInfo(ctx, userID, map[string]any{
		"project_id": ch.ID,
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	montage, ok := info["montage"].(map[string]any)
	if !ok {
		t.Fatalf("montage block missing: %#v", info)
	}
	if got := montage["workspace_input_file"]; got != "montage-input.json" {
		t.Fatalf("workspace_input_file = %v, want montage-input.json", got)
	}
	if got := montage["project_manifest_file"]; got != "montage-project.json" {
		t.Fatalf("project_manifest_file = %v, want montage-project.json", got)
	}
	if got := montage["source_asset_count"]; got != 1 {
		t.Fatalf("source_asset_count = %v, want 1", got)
	}
	input, ok := montage["input"].(model.MontageInput)
	if !ok || input.Brief != "生成发布预告短片" || input.PipelineKey != "social-short" {
		t.Fatalf("montage.input = %#v, want task montage input", montage["input"])
	}
	defaults, ok := montage["defaults"].(model.MontageDefaults)
	if !ok || defaults.DefaultPipeline != "social-short" || defaults.Preferences.DurationSeconds != 45 {
		t.Fatalf("montage.defaults = %#v, want project montage defaults", montage["defaults"])
	}
	contract, ok := montage["runner_contract"].(string)
	if !ok || !strings.Contains(contract, "ANBAN_MONTAGE_SUBMODULE_PATH") {
		t.Fatalf("runner_contract = %#v, want Montage runtime env path hint", montage["runner_contract"])
	}
	providerEnv, ok := montage["provider_env"].(map[string]bool)
	if !ok {
		t.Fatalf("provider_env = %#v, want redacted map", montage["provider_env"])
	}
	if providerEnv["FAL_KEY"] != true || providerEnv["RUNWAY_API_KEY"] != false {
		t.Fatalf("provider_env = %#v, want configured statuses", providerEnv)
	}
	toolPolicy, ok := montage["tool_policy"].(map[string]any)
	if !ok {
		t.Fatalf("tool_policy = %#v, want configured Montage tool policy", montage["tool_policy"])
	}
	videoPolicy, ok := toolPolicy["video_generation"].(srvconfig.MontageToolCapabilityPolicy)
	if !ok || len(videoPolicy.Preferred) != 1 || videoPolicy.Preferred[0] != "fal" {
		t.Fatalf("tool_policy.video_generation = %#v, want configured preference", toolPolicy["video_generation"])
	}
	pipelineDefaults, ok := montage["pipeline_defaults"].(map[string]any)
	if !ok {
		t.Fatalf("pipeline_defaults = %#v, want configured Montage pipeline defaults", montage["pipeline_defaults"])
	}
	socialShort, ok := pipelineDefaults["social-short"].(map[string]any)
	if !ok || socialShort["video_generation"] != "auto" {
		t.Fatalf("pipeline_defaults.social-short = %#v, want configured defaults", pipelineDefaults["social-short"])
	}
	if strings.Contains(strings.Join([]string{
		contract,
		toJSONForTest(t, montage["provider_env"]),
		toJSONForTest(t, montage["tool_policy"]),
		toJSONForTest(t, montage["pipeline_defaults"]),
	}, "\n"), "fal-secret") {
		t.Fatalf("montage profile leaked provider secret: %#v", montage)
	}
}

func TestBuildMontageProfileBlockReturnsEmptyObjectsForUnsetRuntimeConfig(t *testing.T) {
	oldBillSvc := billSvc
	defer func() { billSvc = oldBillSvc }()
	SetBillingServices(nil, nil, &srvconfig.Config{Montage: srvconfig.MontageConfig{}})

	block := buildMontageProfileBlock(&model.Project{Platform: model.PlatformMontage}, &model.Task{Type: model.PlatformMontage})
	for _, key := range []string{"provider_env", "tool_policy", "pipeline_defaults"} {
		data, err := json.Marshal(block[key])
		if err != nil {
			t.Fatalf("marshal %s: %v", key, err)
		}
		if string(data) != "{}" {
			t.Fatalf("%s marshals to %s, want {}", key, data)
		}
	}
}

func toJSONForTest(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal test value: %v", err)
	}
	return string(data)
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
