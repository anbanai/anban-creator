package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appwechat "github.com/anbanai/anban-creator/server/app/wechat"
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
	addError       error
}

func (f *mcpWechatPublicationAPI) AddDraft(context.Context, appwechat.DraftAddRequest) (*appwechat.DraftAddResponse, error) {
	f.addCalls++
	if f.addError != nil {
		return nil, f.addError
	}
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
	repo        repository.Repository
	db          *gorm.DB
	api         *mcpWechatPublicationAPI
	userID      string
	projectID   string
	taskID      string
	executionID string
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
		repo: repo, db: db, api: &mcpWechatPublicationAPI{},
		userID: uuid.NewString(), projectID: uuid.NewString(), taskID: uuid.NewString(), executionID: uuid.NewString(),
	}
	ctx := context.Background()
	if err := repo.Users().Create(ctx, &model.User{ID: f.userID, Email: f.userID + "@mcp.test", Password: "x", InviteCode: f.userID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: f.projectID, UserID: f.userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: f.taskID, UserID: f.userID, ProjectID: f.projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted, CurrentExecutionID: &f.executionID}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: f.executionID, TaskID: f.taskID, Attempt: 1, Target: "test", Status: model.TaskExecutionRunning,
		ExecutionProfile: "effective", Provider: "deepseek", ProfileEnvs: map[string]string{}, ProfileFingerprint: "fingerprint",
	}); err != nil {
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

func interactivePublishingContext(f *publishingToolFixture) context.Context {
	return withMCPUserID(context.Background(), f.userID)
}

func managedPublishingContext(f *publishingToolFixture) context.Context {
	return withMCPExecutionIdentity(interactivePublishingContext(f), f.userID, f.projectID, f.taskID, f.executionID)
}

type createDraftToolFailure struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Hint      string `json:"hint"`
	Retryable bool   `json:"retryable"`
}

func decodeCreateDraftToolFailure(t *testing.T, result *mcp.CallToolResult) createDraftToolFailure {
	t.Helper()
	if result == nil || !result.IsError || len(result.Content) != 1 {
		t.Fatalf("result = %#v, want one tool error", result)
	}
	text := result.Content[0].(*mcp.TextContent).Text
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatalf("create_draft error is not JSON: %q: %v", text, err)
	}
	for _, field := range []string{"code", "message", "hint", "retryable"} {
		if _, ok := raw[field]; !ok {
			t.Fatalf("create_draft error = %#v, missing %q", raw, field)
		}
	}
	var failure createDraftToolFailure
	if err := json.Unmarshal([]byte(text), &failure); err != nil {
		t.Fatal(err)
	}
	if failure.Code == "" || failure.Message == "" || failure.Hint == "" {
		t.Fatalf("incomplete create_draft error = %#v", failure)
	}
	return failure
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
	articles := schema["properties"].(map[string]any)["articles"].(map[string]any)
	if fmt.Sprint(articles["minItems"]) != "1" || fmt.Sprint(articles["maxItems"]) != "1" {
		t.Fatalf("articles cardinality = min:%#v max:%#v, want exactly one", articles["minItems"], articles["maxItems"])
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
		filepath.Join("..", "app", "draft", "service.go"),
		filepath.Join("..", "app", "wechat", "service.go"),
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
		code string
	}{
		{name: "authenticated user", args: validCreateDraftArgs(f), code: "create_draft_auth_required"},
		{name: "task ID", user: f.userID, args: map[string]any{"project_id": f.projectID, "articles": []any{map[string]any{"title": "Title", "content": "Body"}}}, code: "create_draft_invalid_payload"},
		{name: "project ID", user: f.userID, args: map[string]any{"task_id": f.taskID, "articles": []any{map[string]any{"title": "Title", "content": "Body"}}}, code: "create_draft_invalid_payload"},
		{name: "missing article", user: f.userID, args: map[string]any{"task_id": f.taskID, "project_id": f.projectID, "articles": []any{}}, code: "create_draft_invalid_payload"},
		{name: "multiple articles", user: f.userID, args: map[string]any{"task_id": f.taskID, "project_id": f.projectID, "articles": []any{map[string]any{"title": "One", "content": "Body"}, map[string]any{"title": "Two", "content": "Body"}}}, code: "create_draft_invalid_payload"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.user != "" {
				ctx = interactivePublishingContext(f)
			}
			result, err := createDraftHandler(ctx, createDraftToolRequest(t, tt.args))
			if err != nil {
				t.Fatal(err)
			}
			failure := decodeCreateDraftToolFailure(t, result)
			if failure.Code != tt.code || failure.Retryable {
				t.Fatalf("failure = %#v, want code %q and retryable=false", failure, tt.code)
			}
		})
	}
	if f.api.addCalls != 0 || f.api.draftListCalls != 0 {
		t.Fatalf("invalid inputs reached WeChat: add=%d list=%d", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestCreateDraftHandlerAllowsExplicitInteractiveUser(t *testing.T) {
	f := newPublishingToolFixture(t)
	result, err := createDraftHandler(interactivePublishingContext(f), createDraftToolRequest(t, validCreateDraftArgs(f)))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("interactive create_draft = %#v, want success", result)
	}
	if f.api.addCalls != 1 {
		t.Fatalf("interactive draft/add calls = %d, want 1", f.api.addCalls)
	}
}

func TestCreateDraftHandlerRejectsManagedExecutionAuthority(t *testing.T) {
	f := newPublishingToolFixture(t)
	result, err := createDraftHandler(managedPublishingContext(f), createDraftToolRequest(t, validCreateDraftArgs(f)))
	if err != nil {
		t.Fatal(err)
	}
	failure := decodeCreateDraftToolFailure(t, result)
	if failure.Code != "create_draft_managed_forbidden" || failure.Retryable {
		t.Fatalf("failure = %#v, want managed authority rejection", failure)
	}
	if f.api.addCalls != 0 || f.api.draftListCalls != 0 {
		t.Fatalf("managed draft request reached WeChat: add=%d list=%d", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestCreateDraftHandlerBlocksMarketingRiskBeforeWechat(t *testing.T) {
	f := newPublishingToolFixture(t)
	args := validCreateDraftArgs(f)
	args["articles"] = []any{map[string]any{
		"title":   "添加我微信即可领取资料",
		"content": `<p>正文内容。</p>`,
	}}
	result, err := createDraftHandler(interactivePublishingContext(f), createDraftToolRequest(t, args))
	if err != nil {
		t.Fatal(err)
	}
	failure := decodeCreateDraftToolFailure(t, result)
	if failure.Code != "create_draft_marketing_blocked" || failure.Retryable {
		t.Fatalf("failure = %#v, want deterministic marketing block", failure)
	}
	if f.api.addCalls != 0 || f.api.draftListCalls != 0 {
		t.Fatalf("blocked draft reached WeChat: add=%d list=%d", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestCreateDraftHandlerAllowsBodyImageURLsAndWarningOnlyClaims(t *testing.T) {
	f := newPublishingToolFixture(t)
	args := validCreateDraftArgs(f)
	args["articles"] = []any{map[string]any{
		"title":   "Title",
		"content": `<p>这款茶被称为全网第一，仍需读者自行判断。</p><img src="https://mmbiz.qpic.cn/example.png" alt="春茶茶园">`,
	}}
	result, err := createDraftHandler(interactivePublishingContext(f), createDraftToolRequest(t, args))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.IsError {
		t.Fatalf("warning-only article was blocked: %#v", result)
	}
	if f.api.addCalls != 1 {
		t.Fatalf("WeChat add calls = %d, want 1", f.api.addCalls)
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
		code      string
	}{
		{name: "foreign task", user: uuid.NewString(), taskID: f.taskID, projectID: f.projectID, code: "create_draft_forbidden"},
		{name: "foreign project", user: f.userID, taskID: foreignProjectTaskID, projectID: foreignProjectID, code: "create_draft_forbidden"},
		{name: "task project mismatch", user: f.userID, taskID: f.taskID, projectID: otherProjectID, code: "create_draft_project_mismatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := validCreateDraftArgs(f)
			args["task_id"] = tt.taskID
			args["project_id"] = tt.projectID
			ctx := withMCPUserID(context.Background(), tt.user)
			result, err := createDraftHandler(ctx, createDraftToolRequest(t, args))
			if err != nil {
				t.Fatal(err)
			}
			failure := decodeCreateDraftToolFailure(t, result)
			if failure.Code != tt.code || failure.Retryable {
				t.Fatalf("failure = %#v, want code %q and retryable=false", failure, tt.code)
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
	result, err := createDraftHandler(interactivePublishingContext(f), createDraftToolRequest(t, args))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("result = %#v, want duplicate-image error", result)
	}
	if failure := decodeCreateDraftToolFailure(t, result); failure.Code != "create_draft_invalid_payload" || failure.Retryable {
		t.Fatalf("failure = %#v, want invalid payload", failure)
	}
	if f.api.addCalls != 0 || f.api.draftListCalls != 0 {
		t.Fatalf("duplicate images reached WeChat: add=%d list=%d", f.api.addCalls, f.api.draftListCalls)
	}
}

func TestClassifyCreateDraftFailureMarksExpiredReconciliationTerminal(t *testing.T) {
	failure := classifyCreateDraftFailure(service.ErrWechatPublicationDraftFailed)

	if failure.Code != "create_draft_reconciliation_failed" || failure.Retryable {
		t.Fatalf("failure = %#v, want terminal reconciliation failure", failure)
	}
	hint := strings.ToLower(failure.Hint)
	if !strings.Contains(hint, "stopped") || !strings.Contains(hint, "new task") || !strings.Contains(hint, "wechat") {
		t.Fatalf("hint = %q, want stopped reconciliation and recovery action", failure.Hint)
	}
}

func TestClassifyCreateDraftFailureExplainsDefinitiveIPRejection(t *testing.T) {
	err := errors.Join(
		service.ErrWechatPublicationDraftRejected,
		&appwechat.WechatAPIError{ErrCode: 40164, UserMsg: "request ip is not in whitelist"},
	)
	failure := classifyCreateDraftFailure(err)

	if failure.Code != "create_draft_rejected" || failure.Retryable {
		t.Fatalf("failure = %#v, want nonretryable definitive rejection", failure)
	}
	message, hint := strings.ToLower(failure.Message), strings.ToLower(failure.Hint)
	if !strings.Contains(message, "ip") || !strings.Contains(hint, "allowlist") || !strings.Contains(hint, "rerun") {
		t.Fatalf("failure = %#v, want actionable IP allowlist guidance", failure)
	}
}

func TestCreateDraftHandlerReturnsStructuredLifecycleErrors(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		f := newPublishingToolFixture(t)
		args := validCreateDraftArgs(f)
		args["task_id"] = uuid.NewString()
		ctx := withMCPUserID(context.Background(), f.userID)
		result, err := createDraftHandler(ctx, createDraftToolRequest(t, args))
		if err != nil {
			t.Fatal(err)
		}
		failure := decodeCreateDraftToolFailure(t, result)
		if failure.Code != "create_draft_not_found" || failure.Retryable {
			t.Fatalf("failure = %#v", failure)
		}
	})

	t.Run("request conflict", func(t *testing.T) {
		f := newPublishingToolFixture(t)
		ctx := interactivePublishingContext(f)
		if result, err := createDraftHandler(ctx, createDraftToolRequest(t, validCreateDraftArgs(f))); err != nil || result.IsError {
			t.Fatalf("first create = %#v err=%v", result, err)
		}
		changed := validCreateDraftArgs(f)
		changed["articles"] = []any{map[string]any{"title": "Different", "content": "<p>Body</p>"}}
		result, err := createDraftHandler(ctx, createDraftToolRequest(t, changed))
		if err != nil {
			t.Fatal(err)
		}
		failure := decodeCreateDraftToolFailure(t, result)
		if failure.Code != "create_draft_conflict" || failure.Retryable {
			t.Fatalf("failure = %#v", failure)
		}
	})

	t.Run("pending reconciliation", func(t *testing.T) {
		f := newPublishingToolFixture(t)
		ctx := interactivePublishingContext(f)
		f.api.addError = errors.New("ambiguous provider response")
		if _, err := createDraftHandler(ctx, createDraftToolRequest(t, validCreateDraftArgs(f))); err != nil {
			t.Fatal(err)
		}
		f.api.addError = nil
		result, err := createDraftHandler(ctx, createDraftToolRequest(t, validCreateDraftArgs(f)))
		if err != nil {
			t.Fatal(err)
		}
		failure := decodeCreateDraftToolFailure(t, result)
		if failure.Code != "create_draft_pending_reconciliation" || failure.Retryable {
			t.Fatalf("failure = %#v", failure)
		}
	})

	t.Run("provider failure is sanitized", func(t *testing.T) {
		f := newPublishingToolFixture(t)
		f.api.addError = errors.New("provider secret token abc")
		result, err := createDraftHandler(interactivePublishingContext(f), createDraftToolRequest(t, validCreateDraftArgs(f)))
		if err != nil {
			t.Fatal(err)
		}
		failure := decodeCreateDraftToolFailure(t, result)
		if failure.Code != "create_draft_pending_reconciliation" || failure.Retryable {
			t.Fatalf("failure = %#v", failure)
		}
		if strings.Contains(failure.Message, "secret token") || strings.Contains(failure.Hint, "secret token") {
			t.Fatalf("provider details leaked: %#v", failure)
		}
	})
}

func TestCreateDraftHandlerRetriesUnsupportedCapabilityAfterRepair(t *testing.T) {
	f := newPublishingToolFixture(t)
	ctx := interactivePublishingContext(f)
	f.api.addError = &appwechat.WechatAPIError{ErrCode: 48001, UserMsg: "api unauthorized"}

	result, err := createDraftHandler(ctx, createDraftToolRequest(t, validCreateDraftArgs(f)))
	if err != nil {
		t.Fatal(err)
	}
	failure := decodeCreateDraftToolFailure(t, result)
	if failure.Code != "create_draft_unsupported" || failure.Retryable || !strings.Contains(strings.ToLower(failure.Hint), "rerun") {
		t.Fatalf("failure = %#v, want nonretryable account capability hint", failure)
	}
	firstAddCalls, firstListCalls := f.api.addCalls, f.api.draftListCalls

	f.api.addError = nil
	if err := f.db.Model(&model.TaskExecution{}).Where("id = ?", f.executionID).Update("status", model.TaskExecutionFailed).Error; err != nil {
		t.Fatal(err)
	}
	newExecutionID := uuid.NewString()
	if err := f.db.Model(&model.Task{}).Where("id = ?", f.taskID).Update("current_execution_id", newExecutionID).Error; err != nil {
		t.Fatal(err)
	}
	if err := f.repo.TaskExecutions().Create(context.Background(), &model.TaskExecution{
		ID: newExecutionID, TaskID: f.taskID, Attempt: 2, Target: "test", Status: model.TaskExecutionRunning,
		ExecutionProfile: "effective", Provider: "deepseek", ProfileEnvs: map[string]string{}, ProfileFingerprint: "fingerprint",
	}); err != nil {
		t.Fatal(err)
	}
	ctx = interactivePublishingContext(f)
	result, err = createDraftHandler(ctx, createDraftToolRequest(t, validCreateDraftArgs(f)))
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("retry failed: %#v", result)
	}
	data := decodeMCPMap(t, result)
	if data["draft_media_id"] != "draft-media-1" || data["status"] != model.WechatPublicationStatusDrafted {
		t.Fatalf("retry response = %#v", data)
	}
	if f.api.addCalls != firstAddCalls+1 || f.api.draftListCalls != firstListCalls+1 {
		t.Fatalf("retry provider calls: add=%d/%d list=%d/%d", f.api.addCalls, firstAddCalls+1, f.api.draftListCalls, firstListCalls+1)
	}
}

func TestCreateDraftHandlerReturnsOnlyLifecycleDraftFields(t *testing.T) {
	f := newPublishingToolFixture(t)
	result, err := createDraftHandler(interactivePublishingContext(f), createDraftToolRequest(t, validCreateDraftArgs(f)))
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
