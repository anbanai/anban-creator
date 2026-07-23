package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
)

func containsAnyString(values []any, want string) bool {
	for _, value := range values {
		if value, ok := value.(string); ok && value == want {
			return true
		}
	}
	return false
}

func callToolText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return ""
	}
	if content, ok := result.Content[0].(*mcp.TextContent); ok {
		return content.Text
	}
	return ""
}

func TestGenerateImageSchemaDoesNotExposeModelSelection(t *testing.T) {
	schema := generateImageInputSchema()
	properties := schema["properties"].(map[string]any)
	for _, removed := range []string{
		"image_model_key", "operation_id", "verify_with_vision", "verification_prompt", "upload_to_cdn",
	} {
		if _, ok := properties[removed]; ok {
			t.Fatalf("generate_image schema exposes %q", removed)
		}
	}
	for _, key := range []string{"ref_image_path", "ref_image_paths"} {
		property := properties[key].(map[string]any)
		description, _ := property["description"].(string)
		for _, provider := range []string{"OpenAI", "Gemini", "Volcengine", "Seedream"} {
			if strings.Contains(description, provider) {
				t.Fatalf("%s description is provider-specific: %q", key, description)
			}
		}
	}
	required := schema["required"].([]any)
	for _, name := range []string{"project_id", "task_id", "prompt", "output_path"} {
		if !containsAnyString(required, name) {
			t.Fatalf("generate_image schema must require %s, got %#v", name, required)
		}
	}
}

func TestCategorizeImageGenFailureDetectsFilesystemErrors(t *testing.T) {
	err := fmt.Errorf("create output directory: %w", os.ErrPermission)

	if got := categorizeImageGenFailure(err, ""); got != "filesystem" {
		t.Fatalf("categorizeImageGenFailure() = %q, want filesystem", got)
	}
}

func TestValidateImageToolTaskAccessRejectsForeignTask(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })

	db := repositoryTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	ctx := context.Background()
	userID := "user-image-access"
	otherUserID := "user-image-access-other"
	projectID := "project-image-access-other"
	taskID := "task-image-access-other"
	for _, user := range []*model.User{
		{ID: userID, Email: "image-access@example.com", Password: "hashed", InviteCode: "invite-image-access", Tier: model.TierFree},
		{ID: otherUserID, Email: "image-access-other@example.com", Password: "hashed", InviteCode: "invite-image-access-other", Tier: model.TierFree},
	} {
		if err := repo.Users().Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: otherUserID, Platform: model.PlatformArticle, Name: "Foreign Project", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    otherUserID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "foreign task",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svcs = &Services{TaskSvc: service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil)}

	res := validateImageToolTaskAccess(ctx, userID, taskID, "")
	if res == nil || !res.IsError {
		t.Fatalf("expected task ownership error, got %#v", res)
	}
	if !strings.Contains(callToolText(res), "task does not belong to user") {
		t.Fatalf("response = %q, want task ownership error", callToolText(res))
	}
}

func TestValidateImageToolTaskAccessRejectsProjectMismatch(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })

	db := repositoryTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	ctx := context.Background()
	userID := "user-image-project-access"
	projectID := "project-image-project-access"
	otherProjectID := "project-image-project-access-other"
	taskID := "task-image-project-access"
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "image-project-access@example.com", Password: "hashed", InviteCode: "invite-image-project-access", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	for _, project := range []*model.Project{
		{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Project", Status: model.ProjectStatusActive},
		{ID: otherProjectID, UserID: userID, Platform: model.PlatformArticle, Name: "Other Project", Status: model.ProjectStatusActive},
	} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project %s: %v", project.ID, err)
		}
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "task",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	svcs = &Services{TaskSvc: service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil)}

	res := validateImageToolTaskAccess(ctx, userID, taskID, otherProjectID)
	if res == nil || !res.IsError {
		t.Fatalf("expected project mismatch error, got %#v", res)
	}
	if !strings.Contains(callToolText(res), "task does not belong to the requested project") {
		t.Fatalf("response = %q, want project mismatch error", callToolText(res))
	}
}

func TestResolveTaskWorkspaceReadablePathRestoresTaskFileWhenWorkspaceMissing(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })

	db := repositoryTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	ctx := context.Background()
	userID := "user-image-readable"
	projectID := "project-image-readable"
	taskID := "task-image-readable"
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "image-readable@example.com", Password: "hashed", InviteCode: "invite-image-readable", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Project", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusRunning, Prompt: "task"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	store := &fakeObjectStorage{files: map[string][]byte{"user/task/output/cover.png": tinyPNGBytes()}}
	taskSvc := service.NewTaskService(repo, nil, nil, store, &logger, "", nil, t.TempDir(), nil, nil)
	svcs = &Services{TaskSvc: taskSvc, Store: store}
	if _, err := repo.TaskFiles().Upsert(ctx, &model.TaskFile{
		TaskID:          taskID,
		Role:            model.FileRoleCover,
		FileName:        "cover.png",
		MimeType:        "image/png",
		OSSKey:          "user/task/output/cover.png",
		StorageProvider: store.Name(),
		FilePath:        "output/cover.png",
	}); err != nil {
		t.Fatalf("upsert task file: %v", err)
	}

	resolved, cleanup, err := resolveTaskWorkspaceReadablePath(ctx, taskID, "output/cover.png")
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		t.Fatalf("resolveTaskWorkspaceReadablePath() error = %v", err)
	}
	if resolved == "output/cover.png" || !filepath.IsAbs(resolved) {
		t.Fatalf("resolved = %q, want server-local temp path", resolved)
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("read resolved file: %v", err)
	}
	if len(data) == 0 || data[0] != 0x89 {
		t.Fatalf("resolved data does not look like PNG: %q", data[:min(len(data), 8)])
	}
}

type fakeUploadCall struct {
	taskID, userID, relPath, mime string
	size                          int64
}

type fakeExecutionUploadCall struct {
	taskID, userID, executionID, relPath, mime string
	size                                       int64
}

type fakeRenderedImageRegistrar struct {
	uploadCalls          []fakeUploadCall
	executionUploadCalls []fakeExecutionUploadCall
	uploadResult         *model.TaskFile
	uploadErr            error
	enrichCalled         bool
	updateCalls          []fakeRenderedImageUpdateCall
}

type fakeRenderedImageUpdateCall struct {
	role, mediaID, wechatURL string
}

func (f *fakeRenderedImageRegistrar) UploadTaskFileFromReader(_ context.Context, taskID, userID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	_, _ = io.Copy(io.Discard, reader)
	f.uploadCalls = append(f.uploadCalls, fakeUploadCall{taskID, userID, relPath, mimeType, fileSize})
	return f.uploadResult, f.uploadErr
}

func (f *fakeRenderedImageRegistrar) UploadExecutionTaskFileFromReader(_ context.Context, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	_, _ = io.Copy(io.Discard, reader)
	f.executionUploadCalls = append(f.executionUploadCalls, fakeExecutionUploadCall{taskID, userID, executionID, relPath, mimeType, fileSize})
	return f.uploadResult, f.uploadErr
}

func (f *fakeRenderedImageRegistrar) EnrichFilesWithURLs(_ context.Context, _ []*model.TaskFile) {
	f.enrichCalled = true
}

func (f *fakeRenderedImageRegistrar) UpdateTaskFileMetadata(_ context.Context, file *model.TaskFile, role, mediaID, wechatURL string) (*model.TaskFile, error) {
	f.updateCalls = append(f.updateCalls, fakeRenderedImageUpdateCall{role: role, mediaID: mediaID, wechatURL: wechatURL})
	if role != "" {
		file.Role = role
	}
	file.MediaID = mediaID
	file.WechatURL = wechatURL
	return file, nil
}

func TestRegisterRenderedImageAssetRegistersTaskFileWithoutUpload(t *testing.T) {
	png := tinyPNGBytes()
	fakeReg := &fakeRenderedImageRegistrar{uploadResult: &model.TaskFile{
		ID: "task-file-1", FileName: "cover.png", FilePath: "cover.png",
		Role: model.FileRoleImage, URL: "https://files.example.com/task-file-1.png",
	}}

	got, err := registerRenderedImageAsset(context.Background(), fakeReg, "user-1", "task-1", renderedImageInput{
		Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(png),
	})
	if err != nil {
		t.Fatalf("registerRenderedImageAsset returned error: %v", err)
	}
	if got.TaskFileID != "task-file-1" || got.DownloadURL != "https://files.example.com/task-file-1.png" {
		t.Fatalf("asset = %#v", got)
	}
	if got.Role != model.FileRoleCover {
		t.Fatalf("role = %q, want cover", got.Role)
	}
	if len(fakeReg.uploadCalls) != 1 {
		t.Fatalf("task-file upload calls = %d, want 1", len(fakeReg.uploadCalls))
	}
	call := fakeReg.uploadCalls[0]
	if call.relPath != "cover.png" || call.mime != "image/png" || call.size != int64(len(png)) {
		t.Fatalf("task-file upload = %+v", call)
	}
	payload, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, removed := range []string{"wechat_url", "media_id"} {
		if strings.Contains(string(payload), removed) {
			t.Fatalf("registration result exposes %q: %s", removed, payload)
		}
	}
}

func TestRegisterRenderedImageAssetPreservesExistingUploadMetadata(t *testing.T) {
	png := tinyPNGBytes()
	fakeReg := &fakeRenderedImageRegistrar{uploadResult: &model.TaskFile{
		ID: "task-file-1", FileName: "cover.png", FilePath: "cover.png",
		Role: model.FileRoleImage, URL: "https://files.example.com/task-file-1.png",
		MediaID: "existing-media", WechatURL: "https://mmbiz.qpic.cn/existing.png",
	}}

	_, err := registerRenderedImageAsset(context.Background(), fakeReg, "user-1", "task-1", renderedImageInput{
		Name: "cover.png", Role: model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(png),
	})
	if err != nil {
		t.Fatalf("registerRenderedImageAsset returned error: %v", err)
	}
	if len(fakeReg.updateCalls) != 1 {
		t.Fatalf("metadata update calls = %d, want 1", len(fakeReg.updateCalls))
	}
	call := fakeReg.updateCalls[0]
	if call.mediaID != "existing-media" || call.wechatURL != "https://mmbiz.qpic.cn/existing.png" {
		t.Fatalf("metadata update = %+v, want existing upload metadata", call)
	}
}

func TestRegisterRenderedImageAssetRejectsInvalidMIME(t *testing.T) {
	_, err := registerRenderedImageAsset(context.Background(), &fakeRenderedImageRegistrar{}, "user-1", "task-1", renderedImageInput{
		Name: "not-image.png", ImageBase64: base64.StdEncoding.EncodeToString([]byte("this is text, not an image")),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported image MIME") {
		t.Fatalf("error = %v, want unsupported image MIME", err)
	}
}

func TestRegisterRenderedImageAssetRejectsOversizeImage(t *testing.T) {
	tooLarge := append(tinyPNGBytes(), make([]byte, maxRenderedImageBytes+1)...)
	_, err := registerRenderedImageAsset(context.Background(), &fakeRenderedImageRegistrar{}, "user-1", "task-1", renderedImageInput{
		Name: "too-large.png", ImageBase64: base64.StdEncoding.EncodeToString(tooLarge),
	})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, want size rejection", err)
	}
}

func TestRegisterRenderedImageAssetRejectsUnsafeRelativeFilePath(t *testing.T) {
	_, err := registerRenderedImageAsset(context.Background(), &fakeRenderedImageRegistrar{}, "user-1", "task-1", renderedImageInput{
		Name: "cover.png", FilePath: "../secret.png",
	})
	if err == nil || !strings.Contains(err.Error(), "absolute server-local path") {
		t.Fatalf("error = %v, want server-local absolute path rejection", err)
	}
}

func tinyPNGBytes() []byte {
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return data
}

func TestAnalyzeImagePreflightsForeignTaskBeforeCallingVision(t *testing.T) {
	oldSvcs := svcs
	oldBillSvc := billSvc
	t.Cleanup(func() {
		svcs = oldSvcs
		billSvc = oldBillSvc
	})

	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)

	userID := "user-analyze-preflight"
	projectID := "project-analyze-preflight"
	otherUserID := userID + "-other"
	foreignProjectID := projectID + "-other"
	foreignTaskID := "task-analyze-preflight-other"
	for _, user := range []*model.User{
		{
			ID:         userID,
			Email:      userID + "@example.com",
			Password:   "hashed",
			InviteCode: "invite-" + userID,
			Tier:       model.TierFree,
		},
		{
			ID:         otherUserID,
			Email:      otherUserID + "@example.com",
			Password:   "hashed",
			InviteCode: "invite-" + otherUserID,
			Tier:       model.TierFree,
		},
	} {
		if err := repo.Users().Create(ctx, user); err != nil {
			t.Fatalf("create user %s: %v", user.ID, err)
		}
	}
	for _, project := range []*model.Project{
		{
			ID:       projectID,
			UserID:   userID,
			Platform: model.PlatformArticle,
			Name:     "Analyze Project",
			Status:   model.ProjectStatusActive,
		},
		{
			ID:       foreignProjectID,
			UserID:   otherUserID,
			Platform: model.PlatformArticle,
			Name:     "Foreign Project",
			Status:   model.ProjectStatusActive,
		},
	} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project %s: %v", project.ID, err)
		}
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        foreignTaskID,
		UserID:    otherUserID,
		ProjectID: foreignProjectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "foreign task",
	}); err != nil {
		t.Fatalf("create foreign task: %v", err)
	}

	cfg := &srvconfig.Config{
		ImageUnderstanding: srvconfig.UnderstandingRuntimeConfig{
			ProviderKey: "moonshot",
			Model:       "kimi-k2.7-code-highspeed",
		},
	}
	visionClient := &fakeMCPWritingLLM{
		response: `{"overall_pass":true}`,
		usage: srvconfig.TokenUsage{
			InputTokens:  100,
			OutputTokens: 100,
			TotalTokens:  200,
		},
	}
	writingSvc := service.NewWritingService(repo, nil, "", 0, &logger)
	writingSvc.SetImageUnderstandingClient(visionClient)
	svcs = &Services{
		WritingSvc: writingSvc,
		TaskSvc:    service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil),
	}
	billSvc = &billingServices{config: cfg}

	imagePath := filepath.Join(t.TempDir(), "analyze.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}

	res, err := analyzeImageHandler(withMCPUserID(ctx, userID), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(fmt.Sprintf(`{
			"project_id": %q,
			"task_id": %q,
			"file_path": %q,
			"prompt": "verify"
		}`, projectID, foreignTaskID, imagePath))},
	})
	if err != nil {
		t.Fatalf("analyzeImageHandler returned error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error for foreign task, got %#v", res)
	}
	if !strings.Contains(callToolText(res), "task does not belong to user") {
		t.Fatalf("response = %q, want foreign task ownership error", callToolText(res))
	}
	if visionClient.calls != 0 {
		t.Fatalf("vision calls = %d, want 0 before task ownership passes", visionClient.calls)
	}
}
