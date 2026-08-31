package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appwechat "github.com/anbanai/anban-creator/app/wechat"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mcpWechatPublicationAPI struct {
	addCalls       int
	draftListCalls int
}

func (f *mcpWechatPublicationAPI) AddDraft(context.Context, appwechat.DraftAddRequest) (*appwechat.DraftAddResponse, error) {
	f.addCalls++
	return &appwechat.DraftAddResponse{MediaID: "draft-media-1"}, nil
}

func (f *mcpWechatPublicationAPI) BatchGetDrafts(context.Context, appwechat.DraftBatchGetRequest) (*appwechat.DraftBatchGetResponse, error) {
	f.draftListCalls++
	return &appwechat.DraftBatchGetResponse{}, nil
}

func (*mcpWechatPublicationAPI) SubmitFreePublish(context.Context, appwechat.FreePublishSubmitRequest) (*appwechat.FreePublishSubmitResponse, error) {
	return nil, nil
}

func (*mcpWechatPublicationAPI) GetFreePublish(context.Context, appwechat.FreePublishGetRequest) (*appwechat.FreePublishGetResponse, error) {
	return nil, nil
}

func (*mcpWechatPublicationAPI) BatchGetFreePublishes(context.Context, appwechat.FreePublishBatchGetRequest) (*appwechat.FreePublishBatchGetResponse, error) {
	return &appwechat.FreePublishBatchGetResponse{}, nil
}

type publishingToolFixture struct {
	repo      repository.Repository
	api       *mcpWechatPublicationAPI
	userID    string
	projectID string
	taskID    string
}

func newPublishingToolFixture(t *testing.T) *publishingToolFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	f := &publishingToolFixture{
		repo: repo, api: &mcpWechatPublicationAPI{},
		userID: uuid.NewString(), projectID: uuid.NewString(), taskID: uuid.NewString(),
	}
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: f.userID, Email: f.userID + "@mcp.test", Password: "x", InviteCode: f.userID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: f.projectID, UserID: f.userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: f.taskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	svc := service.NewWechatPublicationService(repo, func(*model.Project) (service.WechatPublicationAPI, error) { return f.api, nil }, &logger)
	old := svcs
	svcs = &Services{WechatPublicationSvc: svc}
	t.Cleanup(func() { svcs = old })
	return f
}

func createDraftToolRequest(t *testing.T, args map[string]any) *mcp.CallToolRequest {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: raw}}
}

func validCreateDraftArgs(f *publishingToolFixture) map[string]any {
	return map[string]any{
		"task_id": f.taskID, "project_id": f.projectID,
		"articles": []any{map[string]any{"title": "Title", "content": "<p>Body</p>"}},
	}
}

func TestCreateDraftToolIsCleanCutover(t *testing.T) {
	tools := listRegisteredTools(t, registerPublishingTools)
	var create *mcp.Tool
	for _, tool := range tools {
		switch tool.Name {
		case "create_draft":
			create = tool
		case "publish_draft":
			t.Fatal("legacy publish_draft tool remains registered")
		}
	}
	if create == nil {
		t.Fatal("create_draft tool is not registered")
	}
	schema := create.InputSchema.(map[string]any)
	required := schema["required"].([]any)
	for _, field := range []string{"task_id", "project_id", "articles"} {
		found := false
		for _, item := range required {
			found = found || item == field
		}
		if !found {
			t.Errorf("create_draft required = %#v, missing %q", required, field)
		}
	}
}

func TestLegacyDraftPublicationProductionSymbolsAreRemoved(t *testing.T) {
	legacyTool := "Publish" + "Draft"
	legacyURL := "draft" + "_url"
	fakeEditURL := "appmsg" + "_edit_v2"
	paths := []string{
		"publishing_tools.go",
		filepath.Join("..", "service", "publishing.go"),
		filepath.Join("..", "service", "task.go"),
		filepath.Join("..", "..", "app", "draft", "service.go"),
		filepath.Join("..", "..", "app", "wechat", "service.go"),
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, removed := range []string{legacyTool, legacyURL, fakeEditURL} {
			if strings.Contains(string(data), removed) {
				t.Errorf("%s retains obsolete draft publication symbol %q", path, removed)
			}
		}
	}
}

func TestCreateDraftHandlerRequiresAuthenticatedIDsAndArticle(t *testing.T) {
	f := newPublishingToolFixture(t)
	tests := []struct {
		name string
		user string
		args map[string]any
		want string
	}{
		{name: "authenticated user", args: validCreateDraftArgs(f), want: "authenticated user is required"},
		{name: "task ID", user: f.userID, args: map[string]any{"project_id": f.projectID, "articles": []any{map[string]any{"title": "Title", "content": "Body"}}}, want: "task_id is required"},
		{name: "project ID", user: f.userID, args: map[string]any{"task_id": f.taskID, "articles": []any{map[string]any{"title": "Title", "content": "Body"}}}, want: "project_id is required"},
		{name: "article", user: f.userID, args: map[string]any{"task_id": f.taskID, "project_id": f.projectID, "articles": []any{}}, want: "at least one article is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != "" {
				ctx = withMCPUserID(ctx, tt.user)
			}
			result, err := createDraftHandler(ctx, createDraftToolRequest(t, tt.args))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError || len(result.Content) == 0 {
				t.Fatalf("result = %#v, want tool error", result)
			}
			if got := result.Content[0].(*mcp.TextContent).Text; got != tt.want {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
		})
	}
	if f.api.addCalls != 0 || f.api.draftListCalls != 0 {
		t.Fatalf("invalid inputs reached WeChat: add=%d list=%d", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestCreateDraftHandlerRejectsOwnershipAndProjectMismatchBeforeWechat(t *testing.T) {
	f := newPublishingToolFixture(t)
	otherProjectID := uuid.NewString()
	if err := f.repo.Projects().Create(context.Background(), &model.Project{ID: otherProjectID, UserID: f.userID, Platform: model.PlatformArticle, Name: "Other", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	foreignProjectID := uuid.NewString()
	if err := f.repo.Projects().Create(context.Background(), &model.Project{ID: foreignProjectID, UserID: uuid.NewString(), Platform: model.PlatformArticle, Name: "Foreign", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	foreignProjectTaskID := uuid.NewString()
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{ID: foreignProjectTaskID, UserID: f.userID, ProjectID: foreignProjectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		user      string
		taskID    string
		projectID string
	}{
		{name: "foreign task", user: uuid.NewString(), taskID: f.taskID, projectID: f.projectID},
		{name: "foreign project", user: f.userID, taskID: foreignProjectTaskID, projectID: foreignProjectID},
		{name: "task project mismatch", user: f.userID, taskID: f.taskID, projectID: otherProjectID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := validCreateDraftArgs(f)
			args["task_id"] = tt.taskID
			args["project_id"] = tt.projectID
			result, err := createDraftHandler(withMCPUserID(context.Background(), tt.user), createDraftToolRequest(t, args))
			if err != nil {
				t.Fatal(err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("result = %#v, want ownership error", result)
			}
		})
	}
	if f.api.addCalls != 0 || f.api.draftListCalls != 0 {
		t.Fatalf("ownership failure reached WeChat: add=%d list=%d", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestCreateDraftHandlerRejectsDuplicateContentImagesBeforeWechat(t *testing.T) {
	f := newPublishingToolFixture(t)
	args := validCreateDraftArgs(f)
	args["articles"] = []any{map[string]any{
		"title":   "Title",
		"content": `<p>Body</p><img src="https://cdn/same.png"><img src="https://cdn/same.png">`,
	}}
	result, err := createDraftHandler(withMCPUserID(context.Background(), f.userID), createDraftToolRequest(t, args))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("result = %#v, want duplicate-image error", result)
	}
	if f.api.addCalls != 0 || f.api.draftListCalls != 0 {
		t.Fatalf("duplicate images reached WeChat: add=%d list=%d", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestCreateDraftHandlerReturnsOnlyLifecycleDraftFields(t *testing.T) {
	f := newPublishingToolFixture(t)
	result, err := createDraftHandler(withMCPUserID(context.Background(), f.userID), createDraftToolRequest(t, validCreateDraftArgs(f)))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("create draft failed: %#v", result)
	}
	data := decodeMCPMap(t, result)
	if len(data) != 2 || data["draft_media_id"] != "draft-media-1" || data["status"] != model.WechatPublicationStatusDrafted {
		t.Fatalf("response = %#v, want only draft_media_id/status", data)
	}
	for _, removed := range []string{"media_id", "draft_url"} {
		if _, ok := data[removed]; ok {
			t.Fatalf("response exposes removed field %q: %#v", removed, data)
		}
	}
	if f.api.addCalls != 1 {
		t.Fatalf("WeChat add calls = %d, want 1", f.api.addCalls)
	}
}
