package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
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
	"github.com/anbanai/anban-creator/server/resources"
	"github.com/anbanai/anban-creator/server/service"
)

func listToolNames(t *testing.T, register func(*mcp.Server)) map[string]bool {
	t.Helper()
	ctx := context.Background()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-server", Version: "1.0.0"}, nil)
	register(server)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	result, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(result.Tools))
	for _, tool := range result.Tools {
		names[tool.Name] = true
	}
	return names
}

func setupAccountInfoTest(t *testing.T) (*service.TaskService, *service.ProjectService, repository.Repository, func()) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "account_info_test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Task{}, &model.TaskFile{}, &model.Template{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.New(db)
	logger := zerolog.Nop()
	projectSvc := service.NewProjectService(repo, &logger)
	taskSvc := service.NewTaskService(repo, nil, nil, &logger, "", nil, nil)
	planSvc := service.NewPlanService(repo, &logger)
	templateSvc := service.NewTemplateService(repo, &logger)
	profileSvc := service.NewAgentProjectProfileService(projectSvc, taskSvc, resources.Manager(), srvconfig.MontageConfig{}, accountInfoImageCapabilityResolver())

	old := svcs
	svcs = &Services{
		ProjectSvc: projectSvc, TaskSvc: taskSvc, PlanSvc: planSvc, TemplateSvc: templateSvc,
		AgentProjectProfileSvc: profileSvc,
		ArticleScoreSvc:        service.NewArticleScoreService(),
		SeednoteExportSvc:      service.NewSeednoteExportService(),
		ResourceCatalogSvc:     service.NewResourceCatalogService(resources.Manager()),
	}

	cleanup := func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
		svcs = old
	}
	return taskSvc, projectSvc, repo, cleanup
}

func accountInfoImageCapabilityResolver() *service.ImageCapabilityResolver {
	capability := func(sizes ...string) srvconfig.ImageGenerationRouteConfig {
		return srvconfig.ImageGenerationRouteConfig{
			Enabled: true, MinTier: "free",
			DesignerFeatures: srvconfig.DesignerProviderCapabilities{SizePresets: sizes},
		}
	}
	preferred := capability("1:1", "16:9")
	preferred.Provider = "private-provider"
	preferred.Model = "private-model"
	preferred.BaseURL = "https://private-route.invalid/v1"
	preferred.APIKey = "private-api-key"
	preferred.BillingSKU = "private-billing-sku"
	return service.NewImageCapabilityResolver(nil, &srvconfig.Config{ModelRoutes: srvconfig.ModelRoutesConfig{
		ImageGeneration: srvconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "default-route",
			Capabilities: map[string]srvconfig.ImageGenerationRouteConfig{
				"default-route":    capability("1:1", "3:4"),
				"preferred-key":    preferred,
				"openai-gpt-image": capability("1:1", "3:4", "16:9"),
			},
		},
	}})
}

func getAgentProjectProfileForTest(ctx context.Context, userID string, args map[string]any) (map[string]any, string) {
	profile, err := svcs.AgentProjectProfileSvc.Get(ctx, service.AgentProjectProfileRequest{
		UserID: userID, ProjectID: stringArg(args, "project_id"),
		TaskID: stringArg(args, "task_id"), Scope: stringArg(args, "scope"),
	})
	if err != nil {
		return nil, err.Error()
	}
	return map[string]any(*profile), ""
}

func TestMCPToolListDoesNotContainPrepareWorkspace(t *testing.T) {
	tools := listMCPToolsForTest(t, NewMCPHandler(nil, "test-key", nil))
	for _, rawTool := range tools {
		tool, ok := rawTool.(map[string]any)
		if !ok {
			continue
		}
		if tool["name"] == "prepare_workspace" {
			t.Fatal("MCP tool list contains forbidden prepare_workspace tool")
		}
	}
}

func TestListTaskFilesReturnsCollectedFiles(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	userID := uuid.NewString()
	project := createAccountInfoProject(t, repo, userID, "")
	task := createAccountInfoTask(t, repo, userID, project.ID, "")
	if err := repo.TaskFiles().BatchCreate(context.Background(), []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: "successful", State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md"},
		{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: "failed", State: model.TaskFileStateCollected, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json"},
		{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: "running", State: model.TaskFileStatePending, Role: model.FileRoleOther, FilePath: "output/pending.md", FileName: "pending.md"},
		{ID: uuid.NewString(), TaskID: task.ID, ExecutionID: "old", State: model.TaskFileStateSuperseded, Role: model.FileRoleOther, FilePath: "output/old.md", FileName: "old.md"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := taskFilesHandler(withMCPUserID(context.Background(), userID), taskToolRequest(t, task.ID))
	if err != nil {
		t.Fatal(err)
	}
	data := decodeMCPMap(t, result)
	files, ok := data["files"].([]any)
	if !ok || len(files) != 2 {
		t.Fatalf("files = %#v", data["files"])
	}
	states := []any{files[0].(map[string]any)["state"], files[1].(map[string]any)["state"]}
	if states[0] != model.TaskFileStatePublished || states[1] != model.TaskFileStateCollected {
		t.Fatalf("states = %#v", states)
	}
	for _, raw := range files {
		state := raw.(map[string]any)["state"]
		if state == model.TaskFileStatePending || state == model.TaskFileStateSuperseded {
			t.Fatalf("hidden task-file state leaked: %#v", raw)
		}
	}
}

func TestListTaskFilesRejectsForeignTask(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ownerID, intruderID := uuid.NewString(), uuid.NewString()
	project := createAccountInfoProject(t, repo, ownerID, "")
	task := createAccountInfoTask(t, repo, ownerID, project.ID, "")
	if err := repo.TaskFiles().Create(context.Background(), &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, ExecutionID: "failed", State: model.TaskFileStateCollected,
		Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json",
	}); err != nil {
		t.Fatal(err)
	}

	result, err := taskFilesHandler(withMCPUserID(context.Background(), intruderID), taskToolRequest(t, task.ID))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("foreign list result = %#v, want tool error", result)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	if strings.Contains(text, "failure-state.json") {
		t.Fatalf("foreign list leaked collected file: %s", text)
	}
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

func TestTaskGetHandlerExposesAgentInput(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()

	userID := uuid.NewString()
	project := createAccountInfoProject(t, repo, userID, "")
	task := createAccountInfoTask(t, repo, userID, project.ID, "")
	task.SetAgentInput(map[string]any{"format": "brief"})
	if err := repo.Tasks().Update(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	result, err := taskGetHandler(withMCPUserID(context.Background(), userID), taskToolRequest(t, task.ID))
	if err != nil {
		t.Fatalf("taskGetHandler: %v", err)
	}
	response := decodeMCPMap(t, result)
	input, ok := response["agent_input"].(map[string]any)
	if !ok || input["format"] != "brief" {
		t.Fatalf("agent_input = %#v, want task extension payload", response["agent_input"])
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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
	imgCfg, ok := info["image_config"].(map[string]any)
	if !ok {
		t.Fatalf("image_config missing or wrong type: %T", info["image_config"])
	}
	if _, present := imgCfg["reference_image_path"]; present {
		t.Errorf("image_config unexpectedly contains a reference path: %#v", imgCfg)
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
	ch.ReferenceImageAssetID = "current-asset"
	if err := repo.Projects().Update(ctx, ch); err != nil {
		t.Fatalf("update project: %v", err)
	}
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")
	task.SetProjectSnapshot(model.ProjectSnapshot{
		ProjectName:           "快照项目名",
		Platform:              model.PlatformSeednote,
		Instructions:          "快照定位",
		VisualStyle:           "快照视觉",
		ReferenceImageAssetID: "snapshot-asset",
	})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task snapshot: %v", err)
	}

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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
	if got := imgCfg["reference_image_path"]; got != ".anban-creator/reference.png" {
		t.Fatalf("reference_image_path = %v, want runtime path", got)
	}
}

func TestBuildAccountInfoReferenceAssetOnlyExposesRuntimePath(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.NewString()
	ch := createAccountInfoProject(t, repo, userID, "clean")
	task := createAccountInfoTask(t, repo, userID, ch.ID, "")
	task.ReferenceImageAssetID = "asset-task"
	task.SkipReferenceImage = true
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{"project_id": ch.ID, "scope": "seednote", "task_id": task.ID})
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	raw, _ := json.Marshal(info)
	if strings.Contains(string(raw), "reference_image_url") || strings.Contains(string(raw), "assets/users/") {
		t.Fatalf("profile leaked reference storage identity: %s", raw)
	}
	profile := info["resolved_profile"].(map[string]any)
	if got := profile["reference_image_path"]; got != ".anban-creator/reference.png" {
		t.Fatalf("reference_image_path = %v", got)
	}
	imageConfig := info["image_config"].(map[string]any)
	if got := imageConfig["reference_image_path"]; got != ".anban-creator/reference.png" {
		t.Fatalf("image_config.reference_image_path = %v", got)
	}

	task.ReferenceImageAssetID = ""
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: task.Type, ReferenceImageAssetID: "asset-project"})
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	info, errMsg = getAgentProjectProfileForTest(ctx, userID, map[string]any{"project_id": ch.ID, "scope": "seednote", "task_id": task.ID})
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if _, ok := info["resolved_profile"].(map[string]any)["reference_image_path"]; ok {
		t.Fatal("skip_reference_image exposed inherited runtime path")
	}

	task.SkipReferenceImage = false
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	info, errMsg = getAgentProjectProfileForTest(ctx, userID, map[string]any{"project_id": ch.ID, "scope": "seednote", "task_id": task.ID})
	if errMsg != "" {
		t.Fatal(errMsg)
	}
	if got := info["resolved_profile"].(map[string]any)["reference_image_path"]; got != ".anban-creator/reference.png" {
		t.Fatalf("inherited reference path = %v", got)
	}
}

func TestBuildAccountInfo_MomentsProfileIncludesDeliveryContract(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	ch := &model.Project{
		ID:                    uuid.New().String(),
		UserID:                userID,
		Platform:              model.PlatformMoments,
		Name:                  "私域朋友圈",
		Status:                model.ProjectStatusActive,
		Instructions:          "高信任成交内容",
		Keywords:              "私域,成交,生活方式",
		VisualStyle:           "真实手机随拍，自然光",
		ReferenceImageAssetID: "moments-asset",
		ImageRatio:            "3:4",
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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
	if strings.Join(artifacts, ",") != "material-analysis.md,content.md,image-prompts.md,moments-image.png,quality-review.md" {
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

	_, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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
// The profile service's task ownership guard is defense-in-depth — it cannot
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

	_, errMsg := getAgentProjectProfileForTest(ctx, otherUserID, map[string]any{
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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

func TestAgentProjectProfileDoesNotExposeImageRouteMetadata(t *testing.T) {
	_, _, repo, cleanup := setupAccountInfoTest(t)
	defer cleanup()
	ctx := context.Background()
	userID := uuid.New().String()
	project := createAccountInfoProject(t, repo, userID, "clean editorial collage")
	task := &model.Task{
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          project.ID,
		Type:               model.PlatformSeednote,
		Status:             model.TaskStatusPending,
		ImageCapabilityKey: "preferred-key",
	}
	task.SetProjectSnapshot(model.SnapshotProject(project))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
		"project_id": project.ID,
		"task_id":    task.ID,
	})
	if errMsg != "" {
		t.Fatalf("unexpected error: %s", errMsg)
	}
	raw := mustJSON(t, info)
	resolved := info["resolved_profile"].(map[string]any)
	if got := resolved["image_capability_key"]; got != "preferred-key" {
		t.Fatalf("image_capability_key = %v, want preferred-key", got)
	}
	if got, want := resolved["allowed_image_ratios"], []string{"3:4", "1:1", "4:3"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allowed_image_ratios = %#v, want %#v", got, want)
	}
	for _, forbidden := range []string{
		"image_generation", "image_model", "provider", "selection_reason", "base_url", "api_key", "billing_sku",
		"private-provider", "private-model", "https://private-route.invalid/v1", "private-api-key", "private-billing-sku",
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("profile exposes route metadata %q: %s", forbidden, raw)
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
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          ch.ID,
		Type:               model.PlatformEcommerce,
		Status:             model.TaskStatusPending,
		ImageCapabilityKey: "openai-gpt-image",
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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
	montageConfig := srvconfig.MontageConfig{
		Env: map[string]string{
			"NEW_PROVIDER_TOKEN": "future-secret",
			"RUNWAY_API_KEY":     "",
		},
		ToolPolicy: map[string]srvconfig.MontageToolCapabilityPolicy{
			"video_generation": {Preferred: []string{"fal"}},
		},
		PipelineDefaults: map[string]map[string]any{
			"social-short": {"budget_usd": 2.0, "video_generation": "auto"},
		},
	}
	SetBillingServices(nil, &srvconfig.Config{Montage: montageConfig})
	svcs.AgentProjectProfileSvc = service.NewAgentProjectProfileService(
		svcs.ProjectSvc, svcs.TaskSvc, resources.Manager(), montageConfig, accountInfoImageCapabilityResolver(),
	)
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

	info, errMsg := getAgentProjectProfileForTest(ctx, userID, map[string]any{
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
	env, ok := montage["env"].(map[string]bool)
	if !ok {
		t.Fatalf("env = %#v, want redacted map", montage["env"])
	}
	if env["NEW_PROVIDER_TOKEN"] != true || env["RUNWAY_API_KEY"] != false {
		t.Fatalf("env = %#v, want configured statuses", env)
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
		toJSONForTest(t, montage["env"]),
		toJSONForTest(t, montage["tool_policy"]),
		toJSONForTest(t, montage["pipeline_defaults"]),
	}, "\n"), "future-secret") {
		t.Fatalf("montage profile leaked environment secret: %#v", montage)
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

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return string(data)
}
