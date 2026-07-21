package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	serverbilling "github.com/anbanai/anban-creator/server/billing"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type fakeImageModelResolver struct {
	resolved       *service.ResolvedImageModel
	err            error
	calls          int
	userID         string
	imageModelKey  string
	imageType      string
	referenceCount int
}

func (f *fakeImageModelResolver) ResolveImageModelForGeneration(
	_ context.Context,
	userID string,
	imageModelKey string,
	imageType string,
	referenceCount int,
) (*service.ResolvedImageModel, error) {
	f.calls++
	f.userID = userID
	f.imageModelKey = imageModelKey
	f.imageType = imageType
	f.referenceCount = referenceCount
	return f.resolved, f.err
}

type fakeImageGenerator struct {
	result          *service.ImageResult
	err             error
	calls           int
	imageType       string
	refPath         string
	refPaths        []string
	resolved        *service.ResolvedImageModel
	waitForContext  bool
	returnAfterWait bool
	started         chan struct{}
	ctxErr          error
}

func (f *fakeImageGenerator) GenerateImage(
	ctx context.Context,
	_, _, _, imageType, _, refPath string,
	refPaths []string,
	_, _ string,
	resolved *service.ResolvedImageModel,
	_ *bool,
) (*service.ImageResult, error) {
	f.calls++
	f.imageType = imageType
	f.refPath = refPath
	f.refPaths = append([]string(nil), refPaths...)
	f.resolved = resolved
	if f.started != nil {
		close(f.started)
	}
	if f.waitForContext {
		<-ctx.Done()
		f.ctxErr = ctx.Err()
		if f.returnAfterWait {
			return f.result, f.err
		}
		return nil, ctx.Err()
	}
	return f.result, f.err
}

func installImageFixedBilling(t *testing.T, repo repository.Repository, taskSvc *service.TaskService, taskID string) *service.BillingCatalogService {
	t.Helper()
	ctx := context.Background()
	executionID := uuid.NewString()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true}); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}
	bundle := &serverbilling.Bundle{Products: serverbilling.ProductCatalog{CatalogID: "retail-image-test-" + taskID, Currency: "credits", SKUs: []serverbilling.SKUConfig{
		{ID: "image.cover.v1", Operation: "mcp.generate_image", ChargePolicy: "accepted_task_operation", PriceCredits: 500, Route: "image_generation.cover", Delivery: "persisted_image"},
		{ID: "image.content.v1", Operation: "mcp.generate_image", ChargePolicy: "accepted_task_operation", PriceCredits: 500, Route: "image_generation.content", Delivery: "persisted_image"},
	}}}
	catalog := service.NewBillingCatalogService(repo, bundle, service.BillingCatalogOptions{})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	taskSvc.SetBillingWalletService(service.NewBillingWalletService(repo, bundle, service.BillingWalletOptions{}))
	return catalog
}

func setupTimedGenerateImageHandlerTest(t *testing.T) (context.Context, string, *mcp.CallToolRequest, *fakeImageModelResolver, *service.TaskService, *service.BillingCatalogService) {
	t.Helper()
	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	userID := "user-image-timeout"
	projectID := "project-image-timeout"
	taskID := "task-image-timeout"
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID,
		Type: model.PlatformSeednote, Status: model.TaskStatusRunning,
	}); err != nil {
		t.Fatal(err)
	}
	resolver := &fakeImageModelResolver{resolved: &service.ResolvedImageModel{
		Provider: "volcengine", Model: "seedream", SelectionReason: "preferred",
	}}
	request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(fmt.Sprintf(`{
		"project_id": %q,
		"task_id": %q,
		"operation_id": "timeout-operation",
		"prompt": "cover",
		"output_path": "output/cover.png",
		"image_type": "cover"
	}`, projectID, taskID))}}
	taskSvc := service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil)
	return ctx, userID, request, resolver, taskSvc, installImageFixedBilling(t, repo, taskSvc, taskID)
}

func TestGenerateImageHandlerReturnsOperationTimeout(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	ctx, userID, request, resolver, taskSvc, catalog := setupTimedGenerateImageHandlerTest(t)
	generator := &fakeImageGenerator{waitForContext: true}
	svcs = &Services{
		TaskSvc: taskSvc, ImageSvc: &service.ImageService{},
		ImageModelResolver: resolver,
		ImageGenerator:     generator, GenerateImageTimeout: 500 * time.Millisecond,
		BillingCatalogSvc: catalog,
	}
	res, err := generateImageHandler(withMCPUserID(ctx, userID), request)
	if err != nil || !res.IsError || !strings.Contains(callToolText(res), `"code":"operation_timeout"`) {
		t.Fatalf("result/error = %#v/%v text=%s", res, err, callToolText(res))
	}
	if !errors.Is(generator.ctxErr, context.DeadlineExceeded) {
		t.Fatalf("generator context error = %v", generator.ctxErr)
	}
}

func TestGenerateImageHandlerClassifiesCallerCancellation(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	_, userID, request, resolver, taskSvc, catalog := setupTimedGenerateImageHandlerTest(t)
	started := make(chan struct{})
	generator := &fakeImageGenerator{waitForContext: true, started: started}
	svcs = &Services{
		TaskSvc: taskSvc, ImageSvc: &service.ImageService{},
		ImageModelResolver: resolver,
		ImageGenerator:     generator, GenerateImageTimeout: time.Minute,
		BillingCatalogSvc: catalog,
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	res, err := generateImageHandler(withMCPUserID(ctx, userID), request)
	if err != nil || !res.IsError || !strings.Contains(callToolText(res), `"code":"request_cancelled"`) {
		t.Fatalf("result/error = %#v/%v text=%s", res, err, callToolText(res))
	}
}

func TestGenerateImageHandlerRejectsLateProviderSuccess(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	ctx, userID, request, resolver, taskSvc, catalog := setupTimedGenerateImageHandlerTest(t)
	generator := &fakeImageGenerator{
		waitForContext:  true,
		returnAfterWait: true,
		result:          &service.ImageResult{DownloadURL: "https://example.com/late.png"},
	}
	svcs = &Services{
		TaskSvc: taskSvc, ImageSvc: &service.ImageService{},
		ImageModelResolver: resolver,
		ImageGenerator:     generator, GenerateImageTimeout: 500 * time.Millisecond,
		BillingCatalogSvc: catalog,
	}
	res, err := generateImageHandler(withMCPUserID(ctx, userID), request)
	if err != nil || !res.IsError || !strings.Contains(callToolText(res), `"code":"operation_timeout"`) {
		t.Fatalf("result/error = %#v/%v text=%s", res, err, callToolText(res))
	}
}

func TestGenerateImageHandlerReturnsProviderTimeout(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	ctx, userID, request, resolver, taskSvc, catalog := setupTimedGenerateImageHandlerTest(t)
	generator := &fakeImageGenerator{err: context.DeadlineExceeded}
	svcs = &Services{
		TaskSvc: taskSvc, ImageSvc: &service.ImageService{},
		ImageModelResolver: resolver,
		ImageGenerator:     generator, GenerateImageTimeout: time.Minute,
		BillingCatalogSvc: catalog,
	}
	res, err := generateImageHandler(withMCPUserID(ctx, userID), request)
	if err != nil || !res.IsError || !strings.Contains(callToolText(res), `"code":"provider_timeout"`) {
		t.Fatalf("result/error = %#v/%v text=%s", res, err, callToolText(res))
	}
}

func TestGenerateImageSchemaDoesNotExposeModelSelection(t *testing.T) {
	schema := generateImageInputSchema()
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing or wrong type: %#v", schema["properties"])
	}
	if _, ok := props["image_model_key"]; ok {
		t.Fatalf("generate_image schema must not expose image_model_key")
	}
	for _, forbidden := range []string{"auto", "required", "excluded"} {
		if _, ok := props[forbidden]; ok {
			t.Fatalf("generate_image schema must not expose %q reference policy", forbidden)
		}
	}
	for _, key := range []string{"ref_image_path", "ref_image_paths"} {
		prop, ok := props[key].(map[string]any)
		if !ok {
			t.Fatalf("%s schema missing or wrong type: %#v", key, props[key])
		}
		description, _ := prop["description"].(string)
		for _, provider := range []string{"OpenAI", "Gemini", "Volcengine", "Seedream"} {
			if strings.Contains(description, provider) {
				t.Fatalf("%s description must be provider-neutral, got %q", key, description)
			}
		}
	}
	required, ok := schema["required"].([]any)
	if !ok {
		t.Fatalf("schema required missing or wrong type: %#v", schema["required"])
	}
	for _, name := range []string{"task_id", "operation_id", "output_path"} {
		if !containsAnyString(required, name) {
			t.Fatalf("generate_image schema must require %s, got %#v", name, required)
		}
	}
}

func TestGenerateImageResolvesModelOnce(t *testing.T) {
	oldSvcs := svcs
	oldLog := mcpLog
	t.Cleanup(func() {
		svcs = oldSvcs
		mcpLog = oldLog
	})

	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	userID := "user-resolve-image-once"
	projectID := "project-resolve-image-once"
	taskID := "task-resolve-image-once"
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusRunning, ImageModelKey: "preferred-key",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	resolved := &service.ResolvedImageModel{
		Key:                "server-only-key",
		Provider:           "openai",
		Model:              "gpt-image-2",
		Source:             "preset:openai-gpt-image",
		SupportsReference:  true,
		MaxReferenceImages: 16,
		SelectionReason:    "reference_compatible_fallback",
	}
	resolver := &fakeImageModelResolver{resolved: resolved}
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	generatedPath := filepath.Join(t.TempDir(), "resolved.png")
	if err := os.WriteFile(generatedPath, tinyPNGBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	generator := &fakeImageGenerator{result: &service.ImageResult{FilePath: "output/cover.png", LocalFilePath: generatedPath, OutputMIME: "image/png"}}
	taskSvc := service.NewTaskService(repo, nil, nil, store, &logger, "", nil, "", nil, nil)
	svcs = &Services{
		TaskSvc:            taskSvc,
		ImageSvc:           &service.ImageService{},
		ImageModelResolver: resolver,
		ImageGenerator:     generator,
		BillingCatalogSvc:  installImageFixedBilling(t, repo, taskSvc, taskID),
	}
	var logBuffer bytes.Buffer
	log := zerolog.New(&logBuffer)
	mcpLog = &log

	ref1 := filepath.Join(t.TempDir(), "ref-1.png")
	ref2 := filepath.Join(t.TempDir(), "ref-2.png")
	res, err := generateImageHandler(withMCPUserID(ctx, userID), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(fmt.Sprintf(`{
			"project_id": %q,
			"task_id": %q,
			"operation_id": "resolve-once-operation",
			"prompt": "generate a cover",
			"output_path": "output/cover.png",
			"image_type": "cover",
			"ref_image_paths": [%q, %q]
		}`, projectID, taskID, ref1, ref2))},
	})
	if err != nil {
		t.Fatalf("generateImageHandler returned error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got %#v (%s)", res, callToolText(res))
	}
	if resolver.calls != 1 {
		t.Fatalf("resolver calls = %d, want 1", resolver.calls)
	}
	if resolver.userID != userID || resolver.imageModelKey != "preferred-key" || resolver.imageType != "cover" || resolver.referenceCount != 2 {
		t.Fatalf("resolver args = user %q key %q type %q refs %d", resolver.userID, resolver.imageModelKey, resolver.imageType, resolver.referenceCount)
	}
	if generator.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", generator.calls)
	}
	if generator.resolved != resolved {
		t.Fatalf("descriptor pointer was not shared: resolver=%p generator=%p", resolved, generator.resolved)
	}

	payload := map[string]any{}
	if err := json.Unmarshal([]byte(callToolText(res)), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["provider"] != resolved.Provider || payload["model"] != resolved.Model || payload["selection_reason"] != resolved.SelectionReason {
		t.Fatalf("response model metadata = %#v, want descriptor metadata", payload)
	}
	if payload["supports_reference"] != true || payload["max_reference_images"] != float64(16) {
		t.Fatalf("response capabilities = %#v", payload)
	}
	if _, ok := payload["key"]; ok || strings.Contains(callToolText(res), resolved.Key) {
		t.Fatalf("response must not expose resolved key: %s", callToolText(res))
	}
	logs := logBuffer.String()
	for _, want := range []string{`"provider":"openai"`, `"model":"gpt-image-2"`, `"selection_reason":"reference_compatible_fallback"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs missing %s: %s", want, logs)
		}
	}
	if strings.Contains(logs, resolved.Key) {
		t.Fatalf("logs must not expose resolved key: %s", logs)
	}
	if strings.Contains(logs, "must-not-drive-response") {
		t.Fatalf("logs must use resolved descriptor metadata, not a second billing decision: %s", logs)
	}
}

func TestGenerateImageUsesMultiReferenceArrayAsAuthoritativeInput(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })

	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	userID := "user-authoritative-refs"
	projectID := "project-authoritative-refs"
	taskID := "task-authoritative-refs"
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	resolved := &service.ResolvedImageModel{Provider: "openai", Model: "gpt-image-2", SupportsReference: true, MaxReferenceImages: 16}
	resolver := &fakeImageModelResolver{resolved: resolved}
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	generatedPath := filepath.Join(t.TempDir(), "multi-ref.png")
	if err := os.WriteFile(generatedPath, tinyPNGBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	generator := &fakeImageGenerator{result: &service.ImageResult{FilePath: "output/generated.png", LocalFilePath: generatedPath, OutputMIME: "image/png"}}
	taskSvc := service.NewTaskService(repo, nil, nil, store, &logger, "", nil, "", nil, nil)
	svcs = &Services{
		TaskSvc:            taskSvc,
		ImageModelResolver: resolver,
		ImageGenerator:     generator,
		BillingCatalogSvc:  installImageFixedBilling(t, repo, taskSvc, taskID),
	}

	legacyRef := filepath.Join(t.TempDir(), "legacy.png")
	arrayRef1 := filepath.Join(t.TempDir(), "array-1.png")
	arrayRef2 := filepath.Join(t.TempDir(), "array-2.png")
	args, err := json.Marshal(map[string]any{
		"project_id":      projectID,
		"task_id":         taskID,
		"operation_id":    "multi-reference-operation",
		"prompt":          "generate",
		"output_path":     "output/generated.png",
		"ref_image_path":  legacyRef,
		"ref_image_paths": []string{arrayRef1, arrayRef2},
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	res, err := generateImageHandler(withMCPUserID(ctx, userID), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: args}})
	if err != nil {
		t.Fatalf("generateImageHandler returned error: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("expected successful tool result, got %#v (%s)", res, callToolText(res))
	}
	if resolver.referenceCount != 2 {
		t.Fatalf("resolver reference count = %d, want 2", resolver.referenceCount)
	}
	if generator.refPath != "" {
		t.Fatalf("legacy ref path = %q, want ignored when ref_image_paths is supplied", generator.refPath)
	}
	if len(generator.refPaths) != 2 || generator.refPaths[0] != arrayRef1 || generator.refPaths[1] != arrayRef2 {
		t.Fatalf("generator refs = %#v, want authoritative array", generator.refPaths)
	}
}

func TestGenerateImageValidatesVisionPreconditionsBeforeBillingOrGeneration(t *testing.T) {
	for _, tt := range []struct {
		name               string
		verificationPrompt string
		wantError          string
	}{
		{name: "missing prompt", wantError: "verification_prompt is required"},
		{name: "missing service", verificationPrompt: "verify product identity", wantError: "writing/vision service not available"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			oldSvcs := svcs
			t.Cleanup(func() { svcs = oldSvcs })

			db := repositoryTestDB(t)
			repo := repository.New(db)
			ctx := context.Background()
			logger := zerolog.New(io.Discard)
			suffix := strings.ReplaceAll(tt.name, " ", "-")
			userID := "user-vision-preflight-" + suffix
			projectID := "project-vision-preflight-" + suffix
			taskID := "task-vision-preflight-" + suffix
			if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote"}); err != nil {
				t.Fatalf("create project: %v", err)
			}
			if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
				t.Fatalf("create task: %v", err)
			}
			resolver := &fakeImageModelResolver{resolved: &service.ResolvedImageModel{Provider: "openai", Model: "gpt-image-2"}}
			generator := &fakeImageGenerator{result: &service.ImageResult{DownloadURL: "https://example.com/generated.png"}}
			svcs = &Services{
				TaskSvc:            service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil),
				ImageModelResolver: resolver,
				ImageGenerator:     generator,
			}
			args, err := json.Marshal(map[string]any{
				"project_id":          projectID,
				"task_id":             taskID,
				"operation_id":        "vision-preflight-" + suffix,
				"prompt":              "generate",
				"output_path":         "output/generated.png",
				"verify_with_vision":  true,
				"verification_prompt": tt.verificationPrompt,
			})
			if err != nil {
				t.Fatalf("marshal args: %v", err)
			}
			res, err := generateImageHandler(withMCPUserID(ctx, userID), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: args}})
			if err != nil {
				t.Fatalf("generateImageHandler returned error: %v", err)
			}
			if res == nil || !res.IsError || !strings.Contains(callToolText(res), tt.wantError) {
				t.Fatalf("result = %#v (%s), want error containing %q", res, callToolText(res), tt.wantError)
			}
			if resolver.calls != 0 || generator.calls != 0 {
				t.Fatalf("resolver/generator calls = %d/%d, want 0/0", resolver.calls, generator.calls)
			}
		})
	}
}

type countingImageStorage struct {
	storage.Provider
	taskUploads int
	cdnUploads  int
}

func (s *countingImageStorage) Upload(ctx context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	if strings.HasPrefix(key, "uploads/images/") {
		s.cdnUploads++
	} else {
		s.taskUploads++
	}
	return s.Provider.Upload(ctx, key, reader, contentType)
}

func TestGenerateImageReplaysFinalSanitizedSettlementResult(t *testing.T) {
	oldSvcs, oldBillSvc := svcs, billSvc
	t.Cleanup(func() { svcs, billSvc = oldSvcs, oldBillSvc })

	ctx := context.Background()
	db := repositoryTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	userID, projectID, taskID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	executionID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: uuid.NewString()[:8]}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionRunning, Started: true}); err != nil {
		t.Fatal(err)
	}
	if won, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !won {
		t.Fatalf("set current execution: won=%v err=%v", won, err)
	}

	bundle := &serverbilling.Bundle{Products: serverbilling.ProductCatalog{
		CatalogID: "retail-image-replay-v1", Currency: "credits",
		SKUs: []serverbilling.SKUConfig{{
			ID: "image.content.v1", Operation: "mcp.generate_image", Route: "image_generation.content",
			ChargePolicy: "accepted_task_operation", PriceCredits: 500, Delivery: "persisted_image",
		}},
	}, Policy: serverbilling.PolicyCatalog{AcceptedTask: serverbilling.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true}}}
	catalogSvc := service.NewBillingCatalogService(repo, bundle, service.BillingCatalogOptions{})
	if _, err := catalogSvc.Publish(ctx); err != nil {
		t.Fatalf("publish catalog: %v", err)
	}
	walletSvc := service.NewBillingWalletService(repo, bundle, service.BillingWalletOptions{})
	local, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &countingImageStorage{Provider: local}
	taskSvc := service.NewTaskService(repo, nil, nil, store, &logger, "", nil, "", nil, nil)
	taskSvc.SetBillingWalletService(walletSvc)
	imageSvc := service.NewImageService(nil, store, repo, &logger)
	vision := &fakeMCPWritingLLM{response: `{"overall_pass":true,"relevance_score":"high","missing_entities":[],"notes":"secret raw note"}`}
	writingSvc := service.NewWritingService(repo, nil, "", 0, &logger)
	writingSvc.SetImageUnderstandingClient(vision)
	generatedPath := filepath.Join(t.TempDir(), "generated.png")
	if err := os.WriteFile(generatedPath, tinyPNGBytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	resolved := &service.ResolvedImageModel{Provider: "volcengine_ark", Model: "doubao-seedream", SelectionReason: "quality_rank", SupportsReference: true, MaxReferenceImages: 10}
	generator := &fakeImageGenerator{result: &service.ImageResult{
		ProviderRequestID: "provider-secret-request", FilePath: "output/generated.png", LocalFilePath: generatedPath,
		Size: "1:1", Width: 1024, Height: 1024, Prompt: "secret prompt", RevisedPrompt: "secret revised prompt",
		ResponseType: "url", ResponsePreview: "secret preview", OutputMIME: "image/png",
	}}
	svcs = &Services{
		TaskSvc: taskSvc, ImageSvc: imageSvc, WritingSvc: writingSvc,
		ImageModelResolver: &fakeImageModelResolver{resolved: resolved}, ImageGenerator: generator,
		BillingCatalogSvc: catalogSvc,
	}
	billSvc = &billingServices{config: &srvconfig.Config{ImageUnderstanding: srvconfig.UnderstandingRuntimeConfig{ProviderKey: "moonshot", Model: "kimi-test"}}}

	call := func(operationID, prompt string) *mcp.CallToolResult {
		t.Helper()
		args, marshalErr := json.Marshal(map[string]any{
			"project_id": projectID, "task_id": taskID, "operation_id": operationID,
			"prompt": prompt, "output_path": "output/generated.png", "image_type": "content",
			"verify_with_vision": true, "verification_prompt": "verify", "upload_to_cdn": true,
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		res, callErr := generateImageHandler(withMCPUserID(ctx, userID), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: args}})
		if callErr != nil {
			t.Fatal(callErr)
		}
		return res
	}

	first := call("image-op-1", "generate a product image")
	if first == nil || first.IsError {
		t.Fatalf("first result = %#v text=%s", first, callToolText(first))
	}
	firstText := callToolText(first)
	for _, forbidden := range []string{"secret prompt", "secret revised prompt", "secret preview", "provider-secret-request", "secret raw note", `"usage"`} {
		if strings.Contains(firstText, forbidden) {
			t.Fatalf("sanitized result contains %q: %s", forbidden, firstText)
		}
	}
	if generator.calls != 1 || vision.calls != 1 || store.taskUploads != 1 || store.cdnUploads != 1 {
		t.Fatalf("first calls generator=%d vision=%d task_upload=%d cdn_upload=%d", generator.calls, vision.calls, store.taskUploads, store.cdnUploads)
	}
	second := call("image-op-1", "generate a product image")
	if second == nil || second.IsError || callToolText(second) != firstText {
		t.Fatalf("replay mismatch: first=%s second=%s", firstText, callToolText(second))
	}
	if generator.calls != 1 || vision.calls != 1 || store.taskUploads != 1 || store.cdnUploads != 1 {
		t.Fatalf("replay reran side effects generator=%d vision=%d task_upload=%d cdn_upload=%d", generator.calls, vision.calls, store.taskUploads, store.cdnUploads)
	}

	conflict := call("image-op-1", "changed prompt")
	if conflict == nil || !conflict.IsError || !strings.Contains(callToolText(conflict), "conflict") {
		t.Fatalf("changed request result = %#v text=%s", conflict, callToolText(conflict))
	}
	if generator.calls != 1 {
		t.Fatalf("conflict reran provider: %d", generator.calls)
	}
	third := call("image-op-2", "generate a product image")
	if third == nil || third.IsError || generator.calls != 2 || vision.calls != 2 || store.taskUploads != 2 || store.cdnUploads != 2 {
		t.Fatalf("new operation result=%#v generator=%d vision=%d task_upload=%d cdn_upload=%d", third, generator.calls, vision.calls, store.taskUploads, store.cdnUploads)
	}
	var settlementCount int64
	if err := db.Model(&model.BillingSettlementOutbox{}).Count(&settlementCount).Error; err != nil || settlementCount != 2 {
		t.Fatalf("settlement count=%d err=%v, want 2", settlementCount, err)
	}
}

func TestGenerateImageReturnsActualReferenceLimit(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })

	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)
	userID := "user-image-limit"
	projectID := "project-image-limit"
	taskID := "task-image-limit"
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning, ImageModelKey: "preferred-key"}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	resolver := &fakeImageModelResolver{err: &service.ImageReferenceLimitError{Requested: 17, MaxReferenceImages: 16}}
	generator := &fakeImageGenerator{}
	svcs = &Services{
		TaskSvc:            service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil),
		ImageSvc:           &service.ImageService{},
		ImageModelResolver: resolver,
		ImageGenerator:     generator,
	}

	refs := make([]string, 17)
	for i := range refs {
		refs[i] = filepath.Join(t.TempDir(), fmt.Sprintf("ref-%02d.png", i+1))
	}
	args, err := json.Marshal(map[string]any{
		"project_id":      projectID,
		"task_id":         taskID,
		"operation_id":    "reference-limit-operation",
		"prompt":          "generate",
		"output_path":     "output/generated.png",
		"ref_image_paths": refs,
	})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	res, err := generateImageHandler(withMCPUserID(ctx, userID), &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Arguments: args}})
	if err != nil {
		t.Fatalf("generateImageHandler returned error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected limit error, got %#v", res)
	}
	if got := callToolText(res); !strings.Contains(got, "requested 17") || !strings.Contains(got, "limit of 16") {
		t.Fatalf("limit response = %q, want actual 17/16 limit", got)
	}
	if resolver.calls != 1 || resolver.referenceCount != 17 {
		t.Fatalf("resolver calls/count = %d/%d, want 1/17", resolver.calls, resolver.referenceCount)
	}
	if generator.calls != 0 {
		t.Fatalf("generator must not run after preflight failure: %d", generator.calls)
	}
}

func TestGenerateImageRejectsExplicitImageModelKey(t *testing.T) {
	oldSvcs := svcs
	t.Cleanup(func() { svcs = oldSvcs })
	svcs = &Services{}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Arguments: json.RawMessage(`{
			"project_id":"project-1",
			"task_id":"task-1",
			"prompt":"test prompt",
			"image_model_key":"seedream-4.0"
		}`)},
	}

	res, err := generateImageHandler(withMCPUserID(context.Background(), "user-1"), req)
	if err != nil {
		t.Fatalf("generateImageHandler returned error: %v", err)
	}
	if res == nil || !res.IsError {
		t.Fatalf("expected tool error, got %#v", res)
	}
	if !strings.Contains(callToolText(res), "image_model_key is not accepted") {
		t.Fatalf("error text = %q, want image_model_key rejection", callToolText(res))
	}
}

func TestParseVisionVerificationJSON_Pass(t *testing.T) {
	raw := `{"all_entities_present": true, "missing_entities": [], "relevance_score": "high", "overall_pass": true}`
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Errorf("[FAIL] Passed = false, want true")
	}
	if v.Score != "high" {
		t.Errorf("[FAIL] Score = %q, want high", v.Score)
	}
	if len(v.MissingEntities) != 0 {
		t.Errorf("[FAIL] MissingEntities = %v, want empty", v.MissingEntities)
	}
	if v.Raw != raw {
		t.Error("[FAIL] Raw not preserved")
	}
}

func TestParseVisionVerificationJSON_MissingEntities(t *testing.T) {
	raw := `{"all_entities_present": false, "missing_entities": ["green shoots", "stone crack"], "relevance_score": "medium", "overall_pass": false}`
	v := parseVisionVerificationJSON(raw)
	if v.Passed {
		t.Errorf("[FAIL] Passed = true, want false")
	}
	if v.Score != "medium" {
		t.Errorf("[FAIL] Score = %q, want medium", v.Score)
	}
	if len(v.MissingEntities) != 2 {
		t.Fatalf("[FAIL] MissingEntities len = %d, want 2", len(v.MissingEntities))
	}
	if v.MissingEntities[0] != "green shoots" {
		t.Errorf("[FAIL] MissingEntities[0] = %q", v.MissingEntities[0])
	}
}

func TestParseVisionVerificationJSON_MarkdownFenced(t *testing.T) {
	raw := "```json\n" + `{"all_entities_present": true, "relevance_score": "high", "overall_pass": true}` + "\n```"
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Errorf("[FAIL] Passed = false, want true (fenced JSON)")
	}
	if v.Score != "high" {
		t.Errorf("[FAIL] Score = %q, want high", v.Score)
	}
}

func TestParseVisionVerificationJSON_SurroundedByProse(t *testing.T) {
	raw := `Sure, here is my analysis: {"all_entities_present": true, "relevance_score": "high", "overall_pass": true} Hope that helps!`
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Errorf("[FAIL] Passed = false, want true (JSON surrounded by prose)")
	}
}

func TestParseVisionVerificationJSON_NotJSON(t *testing.T) {
	raw := "I cannot analyze this image."
	v := parseVisionVerificationJSON(raw)
	if v.Passed {
		t.Error("[FAIL] Passed = true for non-JSON response")
	}
	if v.Score != "unknown" {
		t.Errorf("[FAIL] Score = %q, want unknown", v.Score)
	}
	if v.Notes == "" {
		t.Error("[FAIL] Notes should explain the failure")
	}
}

func TestParseVisionVerificationJSON_ForbiddenContentFails(t *testing.T) {
	raw := `{"all_entities_present": true, "missing_entities": [], "relevance_score": "high", "overall_pass": true, "has_forbidden_content": true, "forbidden_notes": "text watermark visible in corner"}`
	v := parseVisionVerificationJSON(raw)
	if v.Passed {
		t.Error("[FAIL] Passed should be false when forbidden content detected")
	}
	if !strings.Contains(v.Notes, "watermark") {
		t.Errorf("[FAIL] Notes should include forbidden details, got: %q", v.Notes)
	}
}

func TestParseVisionVerificationJSON_OverallPassOverridesAllEntities(t *testing.T) {
	// overall_pass is authoritative; AllEntitiesPresent false but overall_pass true.
	// (e.g. missing a minor entity but the composition still matches.)
	raw := `{"all_entities_present": false, "missing_entities": ["optional detail"], "relevance_score": "high", "overall_pass": true}`
	v := parseVisionVerificationJSON(raw)
	if !v.Passed {
		t.Error("[FAIL] OverallPass=true should make Passed=true")
	}
}

func TestCoerceBool(t *testing.T) {
	cases := []struct {
		name string
		args []any
		want bool
	}{
		{"native true", []any{true}, true},
		{"native false", []any{false}, false},
		{"any true wins", []any{false, true}, true},
		{"string true", []any{"true"}, true},
		{"string yes", []any{"yes"}, true},
		{"string YES capitalized", []any{"YES"}, true},
		{"string false", []any{"false"}, false},
		{"int 1", []any{1}, true},
		{"int 0", []any{0}, false},
		{"float64 1.0", []any{float64(1)}, true},
		{"garbage string", []any{"maybe"}, false},
		{"nil alone", []any{nil}, false},
		{"all nil", []any{nil, nil, nil}, false},
		// Real-world case: LLM returns string instead of bool.
		{"string true among nils", []any{nil, "true", nil}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := coerceBool(c.args...)
			if got != c.want {
				t.Errorf("[FAIL] coerceBool(%v) = %v, want %v", c.args, got, c.want)
			}
		})
	}
}

func TestParseVisionVerificationJSON_RawPreservedOnParseError(t *testing.T) {
	raw := "{broken"
	v := parseVisionVerificationJSON(raw)
	if v.Raw != raw {
		t.Errorf("[FAIL] Raw not preserved on parse error: got %q", v.Raw)
	}
	if v.Passed {
		t.Error("[FAIL] Should not pass on parse error")
	}
}

func TestVisionVerification_JSONTags(t *testing.T) {
	// Smoke test the JSON tags match the documented schema.
	v := &service.VisionVerification{
		Passed:          true,
		Score:           "high",
		MissingEntities: []string{"a", "b"},
		Notes:           "ok",
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("[FAIL] marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"passed"`, `"score"`, `"missing_entities"`, `"notes"`, `"high"`, `"a"`} {
		if !strings.Contains(s, want) {
			t.Errorf("[FAIL] JSON missing %q in: %s", want, s)
		}
	}
}

func TestShouldUploadAfterVerification(t *testing.T) {
	// Every combination of the three inputs. This is the gate that makes
	// generate_image(upload_to_cdn=true) upload atomically: a rejected image
	// (passed=false) must never consume a material slot, and a missing
	// verification object when one was requested must default to skip.
	passed := &service.VisionVerification{Passed: true}
	failed := &service.VisionVerification{Passed: false, Score: "medium"}

	cases := []struct {
		name             string
		uploadToCDN      bool
		verifyWithVision bool
		verification     *service.VisionVerification
		want             bool
	}{
		{"upload off, no verify", false, false, nil, false},
		{"upload off, verify passed", false, true, passed, false},
		{"upload off, verify failed", false, true, failed, false},
		{"upload on, no verify (always upload)", true, false, nil, true},
		{"upload on, verify passed", true, true, passed, true},
		{"upload on, verify failed -> skip", true, true, failed, false},
		{"upload on, verify requested but nil -> skip (safe default)", true, true, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := shouldUploadAfterVerification(c.uploadToCDN, c.verifyWithVision, c.verification)
			if got != c.want {
				t.Errorf("[FAIL] shouldUploadAfterVerification(%v, %v, %+v) = %v, want %v",
					c.uploadToCDN, c.verifyWithVision, c.verification, got, c.want)
			}
		})
	}
}

func TestImageResult_UploadFields_JSONTags(t *testing.T) {
	// The atomic-upload contract: wechat_url + media_id appear on success;
	// upload_error appears (and the URL fields stay omitempty) on failure.
	// Verify the omitempty so a normal (non-upload) generate result doesn't
	// leak empty "wechat_url":"" / "media_id":"" keys to the agent.
	success := &service.ImageResult{
		FilePath:  "/tmp/img.png",
		WeChatURL: "https://cdn.example/img.png",
		MediaID:   "media_123",
	}
	b, err := json.Marshal(success)
	if err != nil {
		t.Fatalf("[FAIL] marshal success: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"wechat_url":"https://cdn.example/img.png"`, `"media_id":"media_123"`} {
		if !strings.Contains(s, want) {
			t.Errorf("[FAIL] success JSON missing %q in: %s", want, s)
		}
	}

	// A result with no upload fields must not emit empty upload keys.
	plain := &service.ImageResult{FilePath: "/tmp/img.png"}
	bp, err := json.Marshal(plain)
	if err != nil {
		t.Fatalf("[FAIL] marshal plain: %v", err)
	}
	sp := string(bp)
	for _, unwanted := range []string{`"wechat_url"`, `"media_id"`, `"upload_error"`} {
		if strings.Contains(sp, unwanted) {
			t.Errorf("[FAIL] plain JSON should omit %q, got: %s", unwanted, sp)
		}
	}

	// Upload-failure result carries upload_error and omits the URL fields.
	failedUp := &service.ImageResult{
		FilePath:    "/tmp/img.png",
		UploadError: "boom",
	}
	bf, err := json.Marshal(failedUp)
	if err != nil {
		t.Fatalf("[FAIL] marshal failed: %v", err)
	}
	sf := string(bf)
	if !strings.Contains(sf, `"upload_error":"boom"`) {
		t.Errorf("[FAIL] failed JSON missing upload_error in: %s", sf)
	}
	if strings.Contains(sf, `"wechat_url"`) || strings.Contains(sf, `"media_id"`) {
		t.Errorf("[FAIL] failed JSON should not carry URL fields when upload errored: %s", sf)
	}
}

func containsAnyString(values []any, want string) bool {
	for _, v := range values {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}

func callToolText(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if text, ok := res.Content[0].(*mcp.TextContent); ok {
		return text.Text
	}
	return ""
}

// fakeTaskFileRegistrar implements taskFileRegistrar for unit tests. Its Enrich
// is a no-op so tests can exercise both the URL and OSSURL branches of the
// helper (the real EnrichFilesWithURLs only sets URL when OSSKey != "").
type fakeTaskFileRegistrar struct {
	uploadCalls          []fakeUploadCall
	executionUploadCalls []fakeExecutionUploadCall
	uploadResult         *model.TaskFile
	uploadErr            error
	enrichCalled         bool
}

type fakeUploadCall struct {
	taskID, userID, relPath, mime string
	size                          int64
}

type fakeExecutionUploadCall struct {
	taskID, userID, executionID, relPath, mime string
	size                                       int64
}

func (f *fakeTaskFileRegistrar) UploadTaskFileFromReader(_ context.Context, taskID, userID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	io.Copy(io.Discard, reader) // drain so the helper's file handle closes cleanly
	f.uploadCalls = append(f.uploadCalls, fakeUploadCall{taskID, userID, relPath, mimeType, fileSize})
	return f.uploadResult, f.uploadErr
}

func (f *fakeTaskFileRegistrar) UploadExecutionTaskFileFromReader(_ context.Context, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	io.Copy(io.Discard, reader)
	f.executionUploadCalls = append(f.executionUploadCalls, fakeExecutionUploadCall{taskID, userID, executionID, relPath, mimeType, fileSize})
	return f.uploadResult, f.uploadErr
}

func (f *fakeTaskFileRegistrar) UploadExecutionTaskFileWithSettlementFromReader(_ context.Context, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64, _ service.TaskFileOperationSettlement) (*model.TaskFile, error) {
	io.Copy(io.Discard, reader)
	f.executionUploadCalls = append(f.executionUploadCalls, fakeExecutionUploadCall{taskID, userID, executionID, relPath, mimeType, fileSize})
	return f.uploadResult, f.uploadErr
}

func registerGeneratedImageTaskFileTest(ctx context.Context, fake *fakeTaskFileRegistrar, taskID, userID string, result *service.ImageResult) (string, error) {
	if getExecutionID(ctx) == "" {
		ctx = withMCPExecutionID(ctx, "execution-test")
	}
	return registerGeneratedImageTaskFile(ctx, fake, taskID, userID, "operation-test", strings.Repeat("a", 64), &model.BillingSKU{CatalogID: "retail-test", SKUID: "image-test"}, &service.ImageOperationResultSnapshot{}, result)
}

func (f *fakeTaskFileRegistrar) EnrichFilesWithURLs(_ context.Context, _ []*model.TaskFile) {
	f.enrichCalled = true
}

func TestRegisterGeneratedImageTaskFile_RegistersAndReturnsFetchableURL(t *testing.T) {
	// P1 + P2 at the unit level: the helper must (a) register with the correct
	// (taskID, relPath, mime, size) and (b) return a fetchable URL that is NOT
	// an inline base64 data URL — the value the handler will assign to
	// ImageResult.DownloadURL.
	tmp := filepath.Join(t.TempDir(), "cover.png")
	body := []byte("fake-png-bytes")
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	fake := &fakeTaskFileRegistrar{
		uploadResult: &model.TaskFile{URL: "https://cdn.example.com/u/t/output/cover.png"},
	}
	res := &service.ImageResult{FilePath: tmp, OutputMIME: "image/png"}

	url, err := registerGeneratedImageTaskFileTest(context.Background(), fake, "task-1", "user-1", res)
	if err != nil {
		t.Fatalf("[FAIL] unexpected error: %v", err)
	}
	if url != "https://cdn.example.com/u/t/output/cover.png" {
		t.Errorf("[FAIL] url = %q, want the enriched fetchable URL", url)
	}
	if strings.HasPrefix(url, "data:") {
		t.Errorf("[FAIL] returned url must never be an inline base64 data URL, got %q", url)
	}
	if len(fake.executionUploadCalls) != 1 {
		t.Fatalf("[FAIL] expected exactly 1 execution upload call, got %d", len(fake.executionUploadCalls))
	}
	c := fake.executionUploadCalls[0]
	if c.taskID != "task-1" || c.userID != "user-1" {
		t.Errorf("[FAIL] ids = (%q,%q), want (task-1,user-1)", c.taskID, c.userID)
	}
	if c.relPath != tmp {
		t.Errorf("[FAIL] relPath = %q, want %q (result.FilePath passed through)", c.relPath, tmp)
	}
	if c.mime != "image/png" {
		t.Errorf("[FAIL] mime = %q, want image/png", c.mime)
	}
	if c.size != int64(len(body)) {
		t.Errorf("[FAIL] size = %d, want %d", c.size, len(body))
	}
	if !fake.enrichCalled {
		t.Error("[FAIL] EnrichFilesWithURLs was not called")
	}
}

func TestRegisterGeneratedImageTaskFileScopesManagedCallToExecution(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "image_01.png")
	if err := os.WriteFile(tmp, []byte("fake-png-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := &fakeTaskFileRegistrar{uploadResult: &model.TaskFile{URL: "https://cdn.example.com/image_01.png"}}
	ctx := withMCPExecutionID(context.Background(), "execution-1")
	result := &service.ImageResult{FilePath: "output/seednote/title/image_01.png", LocalFilePath: tmp, OutputMIME: "image/png"}

	if _, err := registerGeneratedImageTaskFileTest(ctx, fake, "task-1", "user-1", result); err != nil {
		t.Fatal(err)
	}
	if len(fake.uploadCalls) != 0 || len(fake.executionUploadCalls) != 1 {
		t.Fatalf("legacy calls=%d execution calls=%d", len(fake.uploadCalls), len(fake.executionUploadCalls))
	}
	call := fake.executionUploadCalls[0]
	if call.executionID != "execution-1" || call.relPath != "output/seednote/title/image_01.png" {
		t.Fatalf("execution upload = %#v", call)
	}
}

func TestRegisterGeneratedImageTaskFile_UsesLocalFileButRegistersLogicalPath(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "generated-cover.png")
	body := []byte("fake-png-bytes")
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}

	fake := &fakeTaskFileRegistrar{
		uploadResult: &model.TaskFile{URL: "https://cdn.example.com/u/t/output/cover.png"},
	}
	res := &service.ImageResult{
		FilePath:      "output/seednote/cover.png",
		LocalFilePath: tmp,
		OutputMIME:    "image/png",
	}

	_, err := registerGeneratedImageTaskFileTest(context.Background(), fake, "task-1", "user-1", res)
	if err != nil {
		t.Fatalf("[FAIL] unexpected error: %v", err)
	}
	if len(fake.executionUploadCalls) != 1 {
		t.Fatalf("[FAIL] expected exactly 1 Upload call, got %d", len(fake.executionUploadCalls))
	}
	c := fake.executionUploadCalls[0]
	if c.relPath != "output/seednote/cover.png" {
		t.Fatalf("[FAIL] relPath = %q, want logical output path", c.relPath)
	}
	if c.size != int64(len(body)) {
		t.Fatalf("[FAIL] size = %d, want %d from local temp file", c.size, len(body))
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

func TestRegisterGeneratedImageTaskFile_FallsBackToOSSURL(t *testing.T) {
	// When Enrich leaves URL empty (OSSKey == "" branch), the helper must fall
	// back to OSSURL so the caller still gets a fetchable download_url.
	tmp := filepath.Join(t.TempDir(), "image_01.png")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	fake := &fakeTaskFileRegistrar{
		uploadResult: &model.TaskFile{OSSURL: "https://oss.example.com/u/t/output/image_01.png"},
	}
	res := &service.ImageResult{FilePath: tmp, OutputMIME: "image/png"}

	url, err := registerGeneratedImageTaskFileTest(context.Background(), fake, "task-1", "user-1", res)
	if err != nil {
		t.Fatalf("[FAIL] unexpected error: %v", err)
	}
	if url != "https://oss.example.com/u/t/output/image_01.png" {
		t.Errorf("[FAIL] url = %q, want OSSURL fallback when URL unset", url)
	}
}

func TestRegisterGeneratedImageTaskFile_RejectsMissingFetchableURL(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "image_01.png")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	fake := &fakeTaskFileRegistrar{uploadResult: &model.TaskFile{}}
	res := &service.ImageResult{FilePath: tmp, OutputMIME: "image/png"}

	_, err := registerGeneratedImageTaskFileTest(context.Background(), fake, "task-1", "user-1", res)
	if err == nil || !strings.Contains(err.Error(), "no fetchable URL") {
		t.Fatalf("error = %v, want missing fetchable URL", err)
	}
}

func TestSanitizeTaskImageDownloadURLRemovesInlineBase64(t *testing.T) {
	result := &service.ImageResult{DownloadURL: "data:image/png;base64," + strings.Repeat("A", 2*1024*1024)}
	sanitizeTaskImageDownloadURL(result)
	if result.DownloadURL != "" {
		t.Fatalf("DownloadURL retained %d inline bytes", len(result.DownloadURL))
	}

	httpsURL := "https://cdn.example.com/image.png"
	result.DownloadURL = httpsURL
	sanitizeTaskImageDownloadURL(result)
	if result.DownloadURL != httpsURL {
		t.Fatalf("DownloadURL = %q, want %q", result.DownloadURL, httpsURL)
	}
}

func TestImageOperationResultSnapshotKeepsMCPResponseCompact(t *testing.T) {
	large := strings.Repeat("provider-output-", 128*1024)
	result := &service.ImageResult{
		FilePath:        "output/seednote/cover.png",
		DownloadURL:     "data:image/png;base64," + large,
		Size:            "3:4",
		Width:           1536,
		Height:          2048,
		ImageType:       "cover",
		Provider:        "volcengine",
		Model:           "doubao-seedream-4-0",
		SelectionReason: "configured_default",
		Prompt:          large,
		RevisedPrompt:   large,
		ResponsePreview: large,
		Verification: &service.VisionVerification{
			Passed: true,
			Score:  "high",
			Notes:  large,
			Raw:    large,
		},
	}

	snapshot := imageOperationResultSnapshot("operation-1", &model.BillingSKU{
		CatalogID: "retail-v1", SKUID: "image.cover.v1", PriceCredits: 500,
	}, result)
	snapshot.DownloadURL = "https://cdn.example.com/tasks/task-1/cover.png"
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) > 4*1024 {
		t.Fatalf("generate_image customer response is %d bytes, want <= 4096", len(payload))
	}
	for _, secret := range []string{"data:image", "provider-output-"} {
		if bytes.Contains(payload, []byte(secret)) {
			t.Fatalf("generate_image customer response leaked %q", secret)
		}
	}
}

func TestRegisterGeneratedImageTaskFile_StatErrorSkipsUpload(t *testing.T) {
	fake := &fakeTaskFileRegistrar{}
	res := &service.ImageResult{FilePath: "/does/not/exist/cover.png", OutputMIME: "image/png"}

	if _, err := registerGeneratedImageTaskFileTest(context.Background(), fake, "task-1", "user-1", res); err == nil {
		t.Fatal("[FAIL] expected error for missing file, got nil")
	}
	if len(fake.uploadCalls) != 0 {
		t.Errorf("[FAIL] Upload must not be called when stat fails, got %d calls", len(fake.uploadCalls))
	}
}

func TestRegisterGeneratedImageTaskFile_UploadErrorPropagates(t *testing.T) {
	// The helper must surface the error so the handler can decide whether the
	// generated bytes are already durable or the task-local temp file must fail.
	tmp := filepath.Join(t.TempDir(), "cover.png")
	if err := os.WriteFile(tmp, []byte("x"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	fake := &fakeTaskFileRegistrar{uploadErr: fmt.Errorf("storage down")}
	res := &service.ImageResult{FilePath: tmp, OutputMIME: "image/png"}

	_, err := registerGeneratedImageTaskFileTest(context.Background(), fake, "task-1", "user-1", res)
	if err == nil {
		t.Fatal("[FAIL] expected error when Upload fails, got nil")
	}
	if !strings.Contains(err.Error(), "storage down") {
		t.Errorf("[FAIL] error should wrap the upload error, got %q", err.Error())
	}
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
	io.Copy(io.Discard, reader)
	f.uploadCalls = append(f.uploadCalls, fakeUploadCall{taskID, userID, relPath, mimeType, fileSize})
	return f.uploadResult, f.uploadErr
}

func (f *fakeRenderedImageRegistrar) UploadExecutionTaskFileFromReader(_ context.Context, taskID, userID, executionID, relPath string, reader io.Reader, mimeType string, fileSize int64) (*model.TaskFile, error) {
	io.Copy(io.Discard, reader)
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

type fakeRenderedImageUploader struct {
	result *service.UploadImageResult
	err    error
	calls  []fakeRenderedImageUploadCall
}

type fakeRenderedImageUploadCall struct {
	userID, projectID, filePath string
}

func (f *fakeRenderedImageUploader) UploadImage(_ context.Context, userID, projectID, filePath string) (*service.UploadImageResult, error) {
	f.calls = append(f.calls, fakeRenderedImageUploadCall{userID: userID, projectID: projectID, filePath: filePath})
	return f.result, f.err
}

func TestRegisterRenderedImageAssetFromBase64RegistersTaskFileAndWechatUpload(t *testing.T) {
	png := tinyPNGBytes()
	fakeReg := &fakeRenderedImageRegistrar{
		uploadResult: &model.TaskFile{
			ID:       "task-file-1",
			FileName: "wechat-21x9-cover.png",
			FilePath: "wechat-21x9-cover.png",
			Role:     model.FileRoleImage,
			URL:      "https://files.example.com/task-file-1.png",
		},
	}
	fakeUpload := &fakeRenderedImageUploader{
		result: &service.UploadImageResult{WechatURL: "https://mmbiz.qpic.cn/cover.png", MediaID: "media-123"},
	}

	got, err := registerRenderedImageAsset(context.Background(), fakeReg, fakeUpload, "user-1", "project-1", "task-1", renderedImageInput{
		Name:        "wechat-21x9-cover.png",
		Role:        model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(png),
		UploadToCDN: true,
	})
	if err != nil {
		t.Fatalf("registerRenderedImageAsset returned error: %v", err)
	}
	if got.TaskFileID != "task-file-1" {
		t.Fatalf("task_file_id = %q, want task-file-1", got.TaskFileID)
	}
	if got.DownloadURL != "https://files.example.com/task-file-1.png" {
		t.Fatalf("download_url = %q", got.DownloadURL)
	}
	if got.WeChatURL != "https://mmbiz.qpic.cn/cover.png" || got.MediaID != "media-123" {
		t.Fatalf("wechat upload fields = (%q,%q), want URL/media", got.WeChatURL, got.MediaID)
	}
	if got.Role != model.FileRoleCover {
		t.Fatalf("role = %q, want cover", got.Role)
	}
	if len(fakeReg.uploadCalls) != 1 {
		t.Fatalf("upload calls = %d, want 1", len(fakeReg.uploadCalls))
	}
	call := fakeReg.uploadCalls[0]
	if call.relPath != "wechat-21x9-cover.png" || call.mime != "image/png" || call.size != int64(len(png)) {
		t.Fatalf("upload call = %+v, want name/png/%d bytes", call, len(png))
	}
	if len(fakeUpload.calls) != 1 || fakeUpload.calls[0].projectID != "project-1" {
		t.Fatalf("cdn upload calls = %+v, want project-1", fakeUpload.calls)
	}
	if len(fakeReg.updateCalls) != 1 {
		t.Fatalf("metadata update calls = %d, want 1", len(fakeReg.updateCalls))
	}
	if fakeReg.updateCalls[0].role != model.FileRoleCover || fakeReg.updateCalls[0].mediaID != "media-123" {
		t.Fatalf("metadata update = %+v", fakeReg.updateCalls[0])
	}
}

func TestRegisterRenderedImageAssetWithoutCDNUploadPreservesExistingWechatMetadata(t *testing.T) {
	png := tinyPNGBytes()
	fakeReg := &fakeRenderedImageRegistrar{
		uploadResult: &model.TaskFile{
			ID:        "task-file-1",
			FileName:  "wechat-21x9-cover.png",
			FilePath:  "wechat-21x9-cover.png",
			Role:      model.FileRoleCover,
			URL:       "https://files.example.com/task-file-1.png",
			MediaID:   "existing-media",
			WechatURL: "https://mmbiz.qpic.cn/existing.png",
		},
	}

	got, err := registerRenderedImageAsset(context.Background(), fakeReg, nil, "user-1", "project-1", "task-1", renderedImageInput{
		Name:        "wechat-21x9-cover.png",
		Role:        model.FileRoleCover,
		ImageBase64: base64.StdEncoding.EncodeToString(png),
		UploadToCDN: false,
	})
	if err != nil {
		t.Fatalf("registerRenderedImageAsset returned error: %v", err)
	}
	if got.MediaID != "existing-media" || got.WeChatURL != "https://mmbiz.qpic.cn/existing.png" {
		t.Fatalf("wechat metadata = (%q,%q), want existing values", got.MediaID, got.WeChatURL)
	}
	if len(fakeReg.updateCalls) != 1 {
		t.Fatalf("metadata update calls = %d, want 1", len(fakeReg.updateCalls))
	}
	if fakeReg.updateCalls[0].mediaID != "existing-media" || fakeReg.updateCalls[0].wechatURL != "https://mmbiz.qpic.cn/existing.png" {
		t.Fatalf("metadata update = %+v, want existing WeChat fields", fakeReg.updateCalls[0])
	}
}

func TestRegisterRenderedImageAssetRejectsInvalidMIME(t *testing.T) {
	_, err := registerRenderedImageAsset(context.Background(), &fakeRenderedImageRegistrar{}, nil, "user-1", "project-1", "task-1", renderedImageInput{
		Name:        "not-image.png",
		ImageBase64: base64.StdEncoding.EncodeToString([]byte("this is text, not an image")),
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported image MIME") {
		t.Fatalf("error = %v, want unsupported image MIME", err)
	}
}

func TestRegisterRenderedImageAssetRejectsOversizeImage(t *testing.T) {
	tooLarge := append(tinyPNGBytes(), make([]byte, maxRenderedImageBytes+1)...)
	_, err := registerRenderedImageAsset(context.Background(), &fakeRenderedImageRegistrar{}, nil, "user-1", "project-1", "task-1", renderedImageInput{
		Name:        "too-large.png",
		ImageBase64: base64.StdEncoding.EncodeToString(tooLarge),
	})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("error = %v, want size rejection", err)
	}
}

func TestRegisterRenderedImageAssetRejectsUnsafeRelativeFilePath(t *testing.T) {
	_, err := registerRenderedImageAsset(context.Background(), &fakeRenderedImageRegistrar{}, nil, "user-1", "project-1", "task-1", renderedImageInput{
		Name:     "cover.png",
		FilePath: "../secret.png",
	})
	if err == nil || !strings.Contains(err.Error(), "absolute server-local path") {
		t.Fatalf("error = %v, want server-local absolute path rejection", err)
	}
}

func tinyPNGBytes() []byte {
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return data
}

func TestRunImageVerificationRecordsProviderCostWithoutUserWallet(t *testing.T) {
	oldSvcs := svcs
	oldBillSvc := billSvc
	t.Cleanup(func() {
		svcs = oldSvcs
		billSvc = oldBillSvc
	})

	db := repositoryTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := "image-understanding-task-user"
	projectID := "image-understanding-task-project"
	taskID := "image-understanding-task"
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "imgtask",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Image Task Project",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "generate image",
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}

	cfg := &srvconfig.Config{
		ImageUnderstanding: srvconfig.UnderstandingRuntimeConfig{
			ProviderKey: "moonshot",
			Model:       "kimi-k2.7-code-highspeed",
		},
	}
	logger := zerolog.New(io.Discard)
	writingSvc := service.NewWritingService(repo, nil, "", 0, &logger)
	writingSvc.SetImageUnderstandingClient(&fakeMCPWritingLLM{
		response: `{"overall_pass":true}`,
		usage: srvconfig.TokenUsage{
			InputTokens:  100,
			OutputTokens: 100,
			TotalTokens:  200,
		},
	})
	taskSvc := service.NewTaskService(repo, nil, nil, nil, &logger, "", nil, "", nil, nil)
	bundle, err := serverbilling.LoadBundle("../billing")
	if err != nil {
		t.Fatalf("load billing bundle: %v", err)
	}
	svcs = &Services{
		WritingSvc: writingSvc, TaskSvc: taskSvc,
		ProviderCostSvc: service.NewProviderCostService(repository.NewBillingCostRepository(db), bundle),
	}
	billSvc = &billingServices{config: cfg}

	imagePath := filepath.Join(t.TempDir(), "verified.png")
	if err := os.WriteFile(imagePath, []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR"), 0o644); err != nil {
		t.Fatalf("write image: %v", err)
	}
	_, err = runImageVerification(ctx, userID, taskID, &service.ImageResult{FilePath: imagePath}, "verify")
	if err != nil {
		t.Fatalf("runImageVerification: %v", err)
	}
	var event model.BillingProviderCostEvent
	if err := db.First(&event).Error; err != nil {
		t.Fatalf("find provider cost event: %v", err)
	}
	if event.TaskID != taskID || event.Provider != "moonshot" || event.Model != "kimi-k2.7-code-highspeed" || event.CostMicroCNY <= 0 {
		t.Fatalf("provider cost event = %#v", event)
	}
	if _, err := repo.Billing().FindAccount(ctx, userID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("image verification created user wallet: %v", err)
	}
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
