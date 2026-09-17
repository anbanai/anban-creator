package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/billing"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/service"
	"github.com/anbanai/anban-creator/server/storage"
)

type noopTaskEnqueuer struct{}

type capturedTaskEnqueue struct {
	taskType string
	payload  []byte
}

type capturingTaskEnqueuer struct {
	items []capturedTaskEnqueue
}

type availableRuntimeDispatcher struct{}

type readTrackingTaskStorage struct {
	*storage.LocalProvider
	readCalls int
}

type faultingTaskDownloadStorage struct {
	*storage.LocalProvider
	lastStream *faultingTaskDownloadStream
}

type faultingTaskDownloadStream struct {
	data   []byte
	err    error
	closed bool
}

func (s *faultingTaskDownloadStorage) OpenObject(context.Context, string) (io.ReadCloser, error) {
	stream := &faultingTaskDownloadStream{data: []byte("partial"), err: errors.New("storage stream interrupted")}
	s.lastStream = stream
	return stream, nil
}

func (s *faultingTaskDownloadStream) Read(p []byte) (int, error) {
	if len(s.data) > 0 {
		n := copy(p, s.data)
		s.data = s.data[n:]
		return n, nil
	}
	return 0, s.err
}

func (s *faultingTaskDownloadStream) Close() error {
	s.closed = true
	return nil
}

func (s *readTrackingTaskStorage) Read(ctx context.Context, key string) ([]byte, error) {
	s.readCalls++
	return s.LocalProvider.Read(ctx, key)
}

func handlerTestAgentProfileRegistry(t *testing.T) *service.AgentProfileRegistry {
	t.Helper()
	registry, err := service.NewAgentProfileRegistry([]service.AgentExecutionProfile{
		handlerTestProfile("effective", "Cost effective", "", "deepseek", "deepseek-v4-pro", model.TierFree),
		handlerTestProfile("balanced", "Balanced", "", "volcengine_ark", "doubao-seed-evolving", model.TierPro),
	})
	if err != nil {
		t.Fatalf("NewAgentProfileRegistry: %v", err)
	}
	return registry
}

func freezeHandlerTaskProfile(t *testing.T, task *model.Task) {
	t.Helper()
	profile := handlerTestProfile("effective", "Cost effective", "", "deepseek", "deepseek-v4-pro", model.TierFree)
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatalf("freeze handler task profile: %v", err)
	}
	task.ExecutionProfile = profile.ID
	task.AgentProfileSnapshot = snapshot
	task.AgentProfileFingerprint = fingerprint
	if task.Type != model.TaskTypeViralAnalysis && task.ImageCapabilityKey == "" {
		freezeHandlerTaskImageCapability(t, task, "standard", handlerTestImageCapabilityRoute("image.standard", model.TierFree))
	}
}

func handlerTestImageCapabilityRoute(sku string, minTier model.Tier, sizes ...string) config.ImageGenerationRouteConfig {
	return config.ImageGenerationRouteConfig{
		Provider: "openai-test", Model: "image-test", BaseURL: "https://images.invalid/v1",
		APIKey: "test-secret", Timeout: time.Minute, Enabled: true, MinTier: string(minTier), BillingSKU: sku,
		GenerationFeatures: config.ImageGenerationFeatures{SizePresets: sizes},
	}
}

func freezeHandlerTaskImageCapability(t *testing.T, task *model.Task, key string, route config.ImageGenerationRouteConfig) {
	t.Helper()
	resolver := service.NewImageCapabilityResolver(nil, &config.Config{ModelRoutes: config.ModelRoutesConfig{
		ImageGeneration: config.ImageGenerationRoutesConfig{DefaultCapability: key, Capabilities: map[string]config.ImageGenerationRouteConfig{key: route}},
	}})
	snapshot, err := resolver.FreezeImageCapability(context.Background(), task.UserID, key)
	if err != nil {
		t.Fatalf("freeze handler task image capability: %v", err)
	}
	task.ImageCapabilityKey = snapshot.Key
	task.SetImageCapabilitySnapshot(snapshot)
}

func newHandlerTaskService(t *testing.T, repo repository.Repository, enqueuer service.TaskEnqueuer, store storage.Provider, logger *zerolog.Logger, taskLogDir string, pubsub *service.RedisPubSub, publishing *service.PublishingService) *service.TaskService {
	t.Helper()
	svc := service.NewTaskService(repo, enqueuer, store, logger, taskLogDir, pubsub, publishing)
	svc.SetAgentProfileRegistry(handlerTestAgentProfileRegistry(t))
	svc.SetImageCapabilityResolver(service.NewImageCapabilityResolver(repo, &config.Config{
		ModelRoutes: config.ModelRoutesConfig{ImageGeneration: config.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]config.ImageGenerationRouteConfig{
				"standard": handlerTestImageCapabilityRoute("image.standard", model.TierFree),
			},
		}},
	}))
	return svc
}

func setHandlerImageCapabilities(taskSvc *service.TaskService, handler *TaskHandler, repo repository.Repository, routes config.ImageGenerationRoutesConfig) {
	taskSvc.SetImageCapabilityResolver(service.NewImageCapabilityResolver(repo, &config.Config{
		ModelRoutes: config.ModelRoutesConfig{ImageGeneration: routes},
	}))
	handler.SetImageCapabilities(routes)
}

func newHandlerPlanService(t *testing.T, repo repository.Repository, logger *zerolog.Logger) *service.PlanService {
	t.Helper()
	svc := service.NewPlanService(repo, logger)
	svc.SetAgentProfileRegistry(handlerTestAgentProfileRegistry(t))
	return svc
}

func (availableRuntimeDispatcher) Scope() string { return "docker" }

func (availableRuntimeDispatcher) ResolveRuntime(taskType string) config.RuntimeImageSelection {
	return config.RuntimeImages{
		model.PlatformArticle:  "creator-agent-article:test",
		model.PlatformSeednote: "creator-agent-seednote:test",
		model.PlatformMontage:  "creator-agent-montage:test",
	}.ForTask(taskType)
}

func (availableRuntimeDispatcher) Prepare(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "docker", Workload: "test-runtime"}, nil
}

func (availableRuntimeDispatcher) ResolvePrepared(context.Context, *model.TaskExecution, *model.Task) (*model.RuntimeIdentity, error) {
	return &model.RuntimeIdentity{Scope: "docker", Workload: "test-runtime", InstanceID: "test-instance"}, nil
}

func (availableRuntimeDispatcher) Activate(context.Context, *model.TaskExecution) error { return nil }

func (availableRuntimeDispatcher) Inspect(context.Context, *model.TaskExecution) (*serveragent.RuntimeExecutionState, error) {
	return &serveragent.RuntimeExecutionState{Phase: serveragent.RuntimePhasePending}, nil
}

func (availableRuntimeDispatcher) Delete(context.Context, *model.TaskExecution) error { return nil }

func (noopTaskEnqueuer) Enqueue(string, []byte) error {
	return nil
}

func (noopTaskEnqueuer) EnqueueIn(string, []byte, time.Duration) error {
	return nil
}

func (noopTaskEnqueuer) EnqueueUnique(string, []byte, string) (bool, error) {
	return true, nil
}

func (e *capturingTaskEnqueuer) Enqueue(taskType string, payload []byte) error {
	e.items = append(e.items, capturedTaskEnqueue{taskType: taskType, payload: append([]byte(nil), payload...)})
	return nil
}

func (e *capturingTaskEnqueuer) EnqueueIn(taskType string, payload []byte, _ time.Duration) error {
	return e.Enqueue(taskType, payload)
}

func (e *capturingTaskEnqueuer) EnqueueUnique(taskType string, payload []byte, _ string) (bool, error) {
	return true, e.Enqueue(taskType, payload)
}

func setupTaskHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	})
	if err := model.AutoMigrate(db); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	return db
}

// seedHandlerFrozenExecution mirrors the production dispatch snapshot so file
// API tests exercise the same delivery-contract lookup as real tasks.
func seedHandlerFrozenExecution(t *testing.T, repo repository.Repository, taskID, taskType, terminalStatus string) string {
	t.Helper()
	ctx := context.Background()
	pack, ok := agentpack.Default().ForTaskType(taskType)
	if !ok {
		t.Fatalf("missing Agent Pack for %s", taskType)
	}
	contract, err := json.Marshal(pack.DeliveryForTaskType(taskType))
	if err != nil {
		t.Fatalf("marshal delivery contract: %v", err)
	}
	required, err := pack.RequiredArtifactsForTaskType(taskType)
	if err != nil {
		t.Fatalf("resolve required artifact contract: %v", err)
	}
	requiredContract, err := json.Marshal(required)
	if err != nil {
		t.Fatalf("marshal required artifact contract: %v", err)
	}
	executionID := uuid.NewString()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: pack.ID, AgentPackVersion: pack.Version, AgentPackDigest: pack.Digest,
		AgentPackDeliveryContract: contract, AgentPackRequiredArtifactContract: requiredContract,
		ExecutionProfile: "effective", Provider: "test",
		ProfileFingerprint: strings.Repeat("e", 64), Target: "docker",
	}); err != nil {
		t.Fatalf("create frozen execution: %v", err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("set current execution = %v, %v", ok, err)
	}
	if terminalStatus != model.TaskStatusRunning {
		if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, terminalStatus); err != nil {
			t.Fatalf("set terminal task status: %v", err)
		}
	}
	return executionID
}

func seedHandlerUploadSession(t *testing.T, repo repository.Repository, userID, uploadID, purpose, filename, contentType string) string {
	t.Helper()
	key := "uploads/pending/" + userID + "/" + uploadID + "/" + filename
	publicURL := "https://cdn.example.com/" + key
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID: uploadID, UserID: userID, Purpose: purpose, StagingKey: key,
		FileName: filename, ContentType: contentType, Size: 1,
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed upload session %s: %v", uploadID, err)
	}
	return publicURL
}

func postJSON(t *testing.T, app *fiber.App, path, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	return resp
}

func TestCreateTaskRejectsMissingExecutionProfile(t *testing.T) {
	logger := zerolog.New(io.Discard)
	handler := NewTaskHandler(nil, &logger)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", uuid.NewString())
		return handler.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{"project_id":"project-id","prompt":"write an article"}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "execution_profile is required") {
		t.Fatalf("body = %s, want execution_profile validation error", body)
	}
}

func TestTaskAndPlanCreateRejectLegacyExecutionProfileIDs(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID: userID, Email: "legacy-profile@example.com", Password: "hashed",
		InviteCode: "legacyprofile", Tier: model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle,
		Name: "Article", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard)
	taskHandler := NewTaskHandler(newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil), &logger)
	planHandler := NewPlanHandler(newHandlerPlanService(t, repo, &logger), &logger)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return taskHandler.Create(c)
	})
	app.Post("/plans", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return planHandler.Create(c)
	})

	for _, profileID := range []string{"cost_effective", "maximum_quality"} {
		for _, request := range []struct {
			name string
			path string
			body string
		}{
			{name: "task", path: "/tasks", body: `{"project_id":"` + projectID + `","prompt":"write","execution_profile":"` + profileID + `"}`},
			{name: "plan", path: "/plans", body: `{"project_id":"` + projectID + `","cron_expr":"0 9 * * *","execution_profile":"` + profileID + `"}`},
		} {
			t.Run(request.name+"/"+profileID, func(t *testing.T) {
				resp := postJSON(t, app, request.path, request.body)
				defer resp.Body.Close()
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					t.Fatalf("read response: %v", err)
				}
				if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(string(body), `"msg":"invalid_agent_execution_profile"`) {
					t.Fatalf("status/body = %d/%s, want 400 invalid_agent_execution_profile", resp.StatusCode, body)
				}
			})
		}
	}
}

func TestCreateTaskRejectsAgentInputWhenPackHasNoSchema(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "taskagentinput"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}

	logger := zerolog.New(io.Discard)
	h := NewTaskHandler(newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil), &logger)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })

	resp := postJSON(t, app, "/tasks", `{"project_id":"`+projectID+`","execution_profile":"effective","agent_input":{"tone":"concise"}}`)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != fiber.StatusBadRequest || !strings.Contains(string(body), "invalid_agent_input") {
		t.Fatalf("status/body = %d/%s, want 400 invalid_agent_input", resp.StatusCode, body)
	}
}

func TestCreateViralAnalysisTaskUsesStandardManagedLifecycle(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformSeednote, Name: "Viral analysis", Status: model.ProjectStatusActive}
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "viralmanaged"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	bundle, err := billing.LoadBundle("../billing")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	catalog := service.NewBillingCatalogService(repo, bundle, service.BillingCatalogOptions{Now: func() time.Time { return now }})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: userID, PaidCredits: 5_000}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Billing().CreateLot(ctx, &model.BillingCreditLot{
		ID: uuid.NewString(), UserID: userID, Kind: model.BillingCreditLotKindPaid,
		SourceType: "fixture", SourceID: "viral-managed", CatalogID: bundle.Products.CatalogID,
		OriginalCredits: 5_000, AvailableCredits: 5_000, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	enqueuer := &capturingTaskEnqueuer{}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, enqueuer, nil, &logger, "", nil, nil)
	taskSvc.SetBillingCatalogService(catalog)
	taskSvc.SetBillingWalletService(service.NewBillingWalletService(repo, bundle, service.BillingWalletOptions{Now: func() time.Time { return now }}))
	taskSvc.SetRuntimeDispatcher(availableRuntimeDispatcher{})
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })

	body := `{"project_id":"` + project.ID + `","type":"viral_analysis","prompt":"https://www.xiaohongshu.com/explore/note-1","execution_profile":"effective","quantity":1}`
	resp := postJSON(t, app, "/tasks", body)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, data)
	}
	data := decodeEnvelopeRawData(t, resp)
	var taskID string
	if err := json.Unmarshal(data["id"], &taskID); err != nil {
		t.Fatal(err)
	}
	task, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if task.Type != model.TaskTypeViralAnalysis || task.ExecutionProfile != "effective" || task.AgentProfileSnapshot.ProfileID != "effective" {
		t.Fatalf("task contract = %#v", task)
	}
	if task.BillingCatalogID != bundle.Products.CatalogID || task.BillingSKUID != "task.viral-analysis.effective" {
		t.Fatalf("task billing = %q/%q", task.BillingCatalogID, task.BillingSKUID)
	}
	quote, err := repo.Billing().FindQuoteByKey(ctx, "task-admission-quote", task.ID)
	if err != nil {
		t.Fatalf("find task admission quote: %v", err)
	}
	var frozenSKU billing.SKUConfig
	if err := json.Unmarshal(quote.SKUSnapshot, &frozenSKU); err != nil {
		t.Fatalf("decode task admission SKU: %v", err)
	}
	if frozenSKU.Operation != "task.viral_analysis" || frozenSKU.ExecutionProfile != "effective" || frozenSKU.ID != task.BillingSKUID {
		t.Fatalf("task admission SKU = %#v", frozenSKU)
	}
	if len(enqueuer.items) != 1 || enqueuer.items[0].taskType != service.TypeContentGenerate {
		t.Fatalf("queue = %#v, want one content generate", enqueuer.items)
	}
	var queued map[string]string
	if err := json.Unmarshal(enqueuer.items[0].payload, &queued); err != nil || queued["task_id"] != task.ID || queued["user_id"] != userID {
		t.Fatalf("queued payload = %#v, %v", queued, err)
	}
	if err := taskSvc.HandleExecutionFromPayload(ctx, task.ID, userID); err != nil {
		t.Fatalf("HandleExecutionFromPayload: %v", err)
	}
	if _, err := repo.TaskExecutions().FindCurrentByTaskID(ctx, task.ID); err != nil {
		t.Fatalf("find current task execution: %v", err)
	}
}

func TestCreateViralAnalysisTaskValidatesRequestedTypeAgainstProjectPlatform(t *testing.T) {
	for _, tt := range []struct {
		name     string
		platform string
		taskType string
	}{
		{name: "viral analysis requires seednote", platform: model.PlatformArticle, taskType: model.TaskTypeViralAnalysis},
		{name: "ordinary type must match project", platform: model.PlatformSeednote, taskType: model.PlatformArticle},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskHandlerTestDB(t)
			repo := repository.New(db)
			userID := uuid.NewString()
			projectID := uuid.NewString()
			if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "typeguard"}); err != nil {
				t.Fatal(err)
			}
			if err := repo.Projects().Create(t.Context(), &model.Project{ID: projectID, UserID: userID, Platform: tt.platform, Name: tt.name, Status: model.ProjectStatusActive}); err != nil {
				t.Fatal(err)
			}
			logger := zerolog.New(io.Discard)
			taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
			h := NewTaskHandler(taskSvc, &logger)
			app := fiber.New()
			app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })
			resp := postJSON(t, app, "/tasks", `{"project_id":"`+projectID+`","type":"`+tt.taskType+`","prompt":"source","execution_profile":"effective","quantity":1}`)
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusBadRequest {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, body)
			}
		})
	}
}

func decodeEnvelopeRawData(t *testing.T, resp *http.Response) map[string]json.RawMessage {
	t.Helper()
	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data == nil {
		t.Fatalf("response data is nil")
	}
	return env.Data
}

func mustMarshalTaskJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return raw
}

func TestDownloadAndPreviewRemainAvailableForCompletedTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	taskID := uuid.New().String()
	fileID := uuid.New().String()
	localStore, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	store := &readTrackingTaskStorage{LocalProvider: localStore}
	upload, err := store.Upload(ctx, "tasks/"+taskID+"/output/05-article.html", strings.NewReader("<main>ok</main>"), "text/html")
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "billlock",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: uuid.New().String(),
		Type: model.PlatformArticle, Status: model.TaskStatusRunning,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	executionID := seedHandlerFrozenExecution(t, repo, taskID, model.PlatformArticle, model.TaskStatusCompleted)
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:              fileID,
		TaskID:          taskID,
		ExecutionID:     executionID,
		Role:            model.FileRoleHTML,
		FilePath:        "output/05-article.html",
		FileName:        "article.html",
		MimeType:        "text/html",
		FileSize:        upload.Size,
		OSSKey:          upload.Key,
		OSSURL:          upload.URL,
		StorageProvider: store.Name(),
	}); err != nil {
		t.Fatalf("create task file: %v", err)
	}

	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, nil, store, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files/zip", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.DownloadZip(c)
	})
	app.Get("/tasks/:id/preview", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.PreviewHTML(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files/zip", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("zip status = %d, want 200 body=%s", resp.StatusCode, data)
	}

	preview, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/preview", nil))
	if err != nil {
		t.Fatalf("preview request: %v", err)
	}
	defer preview.Body.Close()
	previewBody, _ := io.ReadAll(preview.Body)
	if preview.StatusCode != fiber.StatusOK || !strings.Contains(string(previewBody), "<main>ok</main>") {
		t.Fatalf("preview = status %d body=%s, want accessible HTML", preview.StatusCode, previewBody)
	}
	previewCSP := preview.Header.Get("Content-Security-Policy")
	if !strings.Contains(previewCSP, "sandbox") || !strings.Contains(previewCSP, "default-src 'none'") || strings.Contains(previewCSP, "allow-scripts") {
		t.Fatalf("legacy HTML preview CSP = %q, want script-free restrictive sandbox", previewCSP)
	}
	if store.readCalls != 0 {
		t.Fatalf("storage Read calls = %d, want streaming downloads without full-object buffering", store.readCalls)
	}
}

func TestGetFilesPreservesDeliveryURLs(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	taskID := uuid.New().String()
	fileID := uuid.New().String()
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	upload, err := store.Upload(ctx, "tasks/"+taskID+"/output/image_01.png", strings.NewReader("png"), "image/png")
	if err != nil {
		t.Fatalf("upload file: %v", err)
	}
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "fileslock",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: uuid.New().String(),
		Type: model.PlatformSeednote, Status: model.TaskStatusRunning,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	executionID := seedHandlerFrozenExecution(t, repo, taskID, model.PlatformSeednote, model.TaskStatusCompleted)
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID:          fileID,
		TaskID:      taskID,
		ExecutionID: executionID,
		Role:        model.FileRoleImage,
		FilePath:    "output/image_01.png",
		FileName:    "image_01.png",
		MimeType:    "image/png",
		FileSize:    upload.Size,
		OSSKey:      upload.Key,
		OSSURL:      upload.URL,
		MediaID:     "wechat-media-1",
		WechatURL:   "https://mmbiz.qpic.cn/wechat-media-1",
	}); err != nil {
		t.Fatalf("create task file: %v", err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, nil, store, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetFiles(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, data)
	}
	var body struct {
		Code int              `json:"code"`
		Msg  string           `json:"msg"`
		Data []model.TaskFile `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data) != 1 {
		t.Fatalf("files = %d, want 1", len(body.Data))
	}
	got := body.Data[0]
	if got.ID != fileID || got.FileName != "image_01.png" || got.MimeType != "image/png" || got.FileSize != upload.Size {
		t.Fatalf("metadata = %+v, want file metadata preserved", got)
	}
	if got.URL == "" || got.MediaID != "wechat-media-1" || got.WechatURL != "https://mmbiz.qpic.cn/wechat-media-1" {
		t.Fatalf("delivery fields = url %q media_id %q wechat_url %q, want preserved", got.URL, got.MediaID, got.WechatURL)
	}
	if !got.IsDeliverable || got.DeliveryRole != "image" || got.PreviewURL == "" || got.DownloadURL == "" {
		t.Fatalf("delivery metadata = %+v, want matched image with preview and download URLs", got)
	}
}

func TestGetFilesReturnsPublishedAndCollectedFiles(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, taskID := uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "visiblefiles"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: uuid.NewString(), Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	executionID := seedHandlerFrozenExecution(t, repo, taskID, model.PlatformSeednote, model.TaskStatusFailed)
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown"},
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateRetained, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json"},
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewTaskHandler(newHandlerTaskService(t, repo, nil, nil, &logger, "", nil, nil), &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetFiles(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		Data []model.TaskFile `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Data) != 2 || body.Data[0].State != model.TaskFileStateDelivered || body.Data[1].State != model.TaskFileStateRetained {
		t.Fatalf("files = %#v", body.Data)
	}
	if !body.Data[0].IsDeliverable || body.Data[0].DownloadURL == "" || body.Data[0].PreviewURL == "" {
		t.Fatalf("published delivery metadata = %+v", body.Data[0])
	}
	if body.Data[1].IsDeliverable || body.Data[1].DownloadURL == "" || body.Data[1].URL != "" || body.Data[1].PreviewURL == "" {
		t.Fatalf("collected process metadata = %+v", body.Data[1])
	}
}

func TestProcessTaskFileCanBePreviewedButNotDownloaded(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, taskID, fileID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "previewonly"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: uuid.NewString(), Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	seedHandlerFrozenExecution(t, repo, taskID, model.PlatformSeednote, model.TaskStatusCompleted)
	upload, err := store.Upload(ctx, "tasks/"+taskID+"/output/review.html", strings.NewReader("<main>internal review</main>"), "text/html")
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: fileID, TaskID: taskID, State: model.TaskFileStateDelivered, Role: model.FileRoleHTML,
		FilePath: "output/review.html", FileName: `review".html`, MimeType: "text/html",
		FileSize: upload.Size, OSSKey: upload.Key, StorageProvider: store.Name(),
	}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	h := NewTaskHandler(newHandlerTaskService(t, repo, nil, store, &logger, "", nil, nil), &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files/:fileId/download", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.DownloadFile(c)
	})
	app.Get("/tasks/:id/files/:fileId/preview", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.PreviewFile(c)
	})

	downloadResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files/"+fileID+"/download", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer downloadResp.Body.Close()
	if downloadResp.StatusCode != fiber.StatusForbidden {
		body, _ := io.ReadAll(downloadResp.Body)
		t.Fatalf("process download status = %d body=%s, want 403", downloadResp.StatusCode, body)
	}

	previewResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files/"+fileID+"/preview", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer previewResp.Body.Close()
	previewBody, _ := io.ReadAll(previewResp.Body)
	if previewResp.StatusCode != fiber.StatusOK || string(previewBody) != "<main>internal review</main>" {
		t.Fatalf("process preview = status %d body=%q, want authenticated preview", previewResp.StatusCode, previewBody)
	}
	if got := previewResp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	csp := previewResp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || strings.Contains(csp, "allow-scripts") {
		t.Fatalf("Content-Security-Policy = %q, want script-free sandbox", csp)
	}
	if got := previewResp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "inline;") || strings.ContainsAny(got, "\r\n") {
		t.Fatalf("Content-Disposition = %q, want safe inline disposition", got)
	}
}

func TestTaskFilePreviewSandboxesXHTML(t *testing.T) {
	app := fiber.New()
	app.Get("/preview", func(c fiber.Ctx) error {
		setTaskFilePreviewHeaders(c, "application/xhtml+xml", "preview.xhtml")
		return c.SendString("<html xmlns=\"http://www.w3.org/1999/xhtml\"><script>alert(1)</script></html>")
	})
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/preview", nil))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	csp := resp.Header.Get("Content-Security-Policy")
	if !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "allow-scripts") {
		t.Fatalf("XHTML preview CSP = %q, want script-free restrictive sandbox", csp)
	}
	if got := resp.Header.Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("XHTML preview Referrer-Policy = %q", got)
	}
}

func TestTaskFilePreviewForcesUnknownAndPDFContentToAttachment(t *testing.T) {
	for _, contentType := range []string{"application/pdf", "application/octet-stream", "application/x-custom-active-content"} {
		t.Run(contentType, func(t *testing.T) {
			app := fiber.New()
			app.Get("/preview", func(c fiber.Ctx) error {
				setTaskFilePreviewHeaders(c, contentType, "untrusted.bin")
				return c.SendString("untrusted")
			})
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/preview", nil))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if got := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(got, "attachment;") {
				t.Fatalf("Content-Disposition = %q, want attachment", got)
			}
			if got := resp.Header.Get("Content-Type"); got != "application/octet-stream" {
				t.Fatalf("Content-Type = %q, want application/octet-stream", got)
			}
		})
	}
}

func TestTaskFileDownloadSurfacesStorageStreamFailure(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID, taskID, fileID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "streamfail"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: uuid.NewString(), Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	executionID := seedHandlerFrozenExecution(t, repo, taskID, model.PlatformSeednote, model.TaskStatusCompleted)
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: fileID, TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStateDelivered,
		Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md",
		MimeType: "text/markdown", FileSize: 32, OSSKey: "tasks/" + taskID + "/output/content.md",
	}); err != nil {
		t.Fatal(err)
	}
	localStore, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := &faultingTaskDownloadStorage{LocalProvider: localStore}
	logger := zerolog.New(io.Discard)
	h := NewTaskHandler(newHandlerTaskService(t, repo, nil, store, &logger, "", nil, nil), &logger)
	app := fiber.New()
	app.Get("/tasks/:id/files/:fileId/download", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.DownloadFile(c)
	})

	resp, requestErr := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID+"/files/"+fileID+"/download", nil))
	if resp != nil {
		_ = resp.Body.Close()
	}
	if requestErr == nil {
		t.Fatal("download returned a successful HTTP response after the storage stream failed")
	}
	if store.lastStream == nil || !store.lastStream.closed {
		t.Fatal("download did not close the failed storage stream")
	}
}

func TestTaskHandlerProductionDoesNotReferenceLegacyPaymentRequiredColumns(t *testing.T) {
	raw, err := os.ReadFile("task.go")
	if err != nil {
		t.Fatalf("read task.go: %v", err)
	}
	for _, forbidden := range []string{
		"TaskBillingStatusPaymentRequired", "BillingShortfallCredits", "taskBillingLocked", "ensureTaskBillingUnlocked",
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("task.go still contains legacy delivery gate %q", forbidden)
		}
	}
}

func TestTaskCreatePromptLengthLimit(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "prompt-limit@example.com",
		Password:   "hashed",
		InviteCode: "promptlimit",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	handler := NewTaskHandler(taskSvc, &logger)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handler.Create(c)
	})

	tests := []struct {
		name       string
		prompt     string
		wantStatus int
	}{
		{"allows 5120 characters", strings.Repeat("a", 5120), fiber.StatusOK},
		{"rejects more than 5120 characters", strings.Repeat("a", 5121), fiber.StatusBadRequest},
		{"counts unicode characters", strings.Repeat("汉", 5120), fiber.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := `{"execution_profile":"effective","project_id":"` + projectID + `","prompt":"` + tt.prompt + `"}`
			req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// TestCreateTask_ImageCapabilityKeyTierForbidden verifies that the handler returns
// 403 when a Free-tier user tries to create a task with a Pro-tier image preset,
// or with "custom" (Enterprise-only). Also covers the fail-closed path:
// Free users CAN still create tasks with Free-tier or empty keys.
func TestCreateTask_ImageCapabilityKeyTierForbidden(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "tier-forbidden@example.com",
		Password:   "hashed",
		InviteCode: "tierforbidden",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	capabilities := map[string]config.ImageGenerationRouteConfig{
		"volcengine-standard": handlerTestImageCapabilityRoute("image.standard", model.TierFree),
		"gemini-pro":          handlerTestImageCapabilityRoute("image.professional", model.TierPro),
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	setHandlerImageCapabilities(taskSvc, h, repo, config.ImageGenerationRoutesConfig{DefaultCapability: "volcengine-standard", Capabilities: capabilities})
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "free tier + free preset accepted",
			body:       `{"execution_profile":"effective","project_id":"` + projectID + `","image_capability_key":"volcengine-standard"}`,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "free tier + empty key accepted",
			body:       `{"execution_profile":"effective","project_id":"` + projectID + `"}`,
			wantStatus: fiber.StatusOK,
		},
		{
			name:       "free tier + pro preset rejected",
			body:       `{"execution_profile":"effective","project_id":"` + projectID + `","image_capability_key":"gemini-pro"}`,
			wantStatus: fiber.StatusForbidden,
		},
		{
			name:       "free tier + custom rejected",
			body:       `{"execution_profile":"effective","project_id":"` + projectID + `","image_capability_key":"custom"}`,
			wantStatus: fiber.StatusForbidden,
		},
		{
			name:       "free tier + unknown key rejected",
			body:       `{"execution_profile":"effective","project_id":"` + projectID + `","image_capability_key":"made-up"}`,
			wantStatus: fiber.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/tasks", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// TestCreateTask_ArticleImageTogglesPersist verifies the full
// handler→service→model→DB round-trip persists an explicit `false` for both
// article image toggles. This is a regression guard for the *bool /
// gorm:"default:true" mitigation: a plain bool with default:true silently
// coerces false→true at Create time (in-memory AND DB). It also guards against
// a handler wiring omission (forgetting to pass req.ArticleWithCover into
// CreateManualParams would silently default the user's choice to true). The
// value is re-read from the repo to assert the persisted state, not just the
// in-memory response.
func TestCreateTask_ArticleImageTogglesPersist(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "article-toggles@example.com",
		Password:   "hashed",
		InviteCode: "articletoggles",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)

	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	body := `{"execution_profile":"effective","project_id":"` + projectID + `","prompt":"文章开关持久化测试","article_with_cover":false,"article_with_content_images":false}`
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil {
		t.Fatalf("find tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	for label, ptr := range map[string]*bool{
		"ArticleWithCover":         got.ArticleWithCover,
		"ArticleWithContentImages": got.ArticleWithContentImages,
	} {
		switch {
		case ptr == nil:
			t.Errorf("%s = nil, want non-nil false", label)
		case *ptr:
			t.Errorf("%s = true, want false", label)
		}
	}
}

func TestCreateTaskMontageReturnsSingleTaskWhenQuantityIsClamped(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "omhandler",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformMontage,
		Name:     "Montage",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetRuntimeDispatcher(availableRuntimeDispatcher{})
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{
		"execution_profile":"effective","project_id": "`+projectID+`",
		"quantity": 3,
		"montage_input": {
			"brief": "做一条新品发布短片",
			"pipeline_key": "default"
		}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	data := decodeEnvelopeRawData(t, resp)
	if _, ok := data["id"]; !ok {
		t.Fatalf("response data should be a single task object, got keys %#v", data)
	}
	if _, ok := data["montage_input"]; !ok {
		t.Fatalf("response missing montage_input: keys %#v", data)
	}
}

func TestCreateTaskEcommerceKeepsArrayResponseWhenRequestQuantityExceedsOne(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "ecommercearray",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformEcommerce,
		Name:     "Ecommerce",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Create(c)
	})

	resp := postJSON(t, app, "/tasks", `{
		"execution_profile":"effective","project_id": "`+projectID+`",
		"quantity": 3,
		"selected_modules": {"main_images": 1}
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var env struct {
		Data []map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode array response: %v", err)
	}
	if len(env.Data) != 1 {
		t.Fatalf("response data len = %d, want one clamped ecommerce task", len(env.Data))
	}
}

func TestCreateTaskEcommerceFreezesUploadedProductPhotoAsInputAttachment(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "ecomfreeze"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformEcommerce, Name: "Ecommerce", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	uploadID := uuid.NewString()
	pendingKey := "uploads/pending/" + userID + "/" + uploadID + "/product.png"
	imageBytes := tinyPNG()
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: uploadID, UserID: userID, Purpose: service.DirectUploadPurposeEcommercePhoto,
		StagingKey: pendingKey, FileName: "product.png", ContentType: "image/png", Size: int64(len(imageBytes)),
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	store := uploadSessionStatStore(repo.UploadSessions())
	store.data = map[string][]byte{pendingKey: imageBytes}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })

	resp := postJSON(t, app, "/tasks", `{
		"execution_profile":"effective","project_id":"`+projectID+`",
		"selected_modules":{"main_images":1},
		"product_photos":["/api/v1/files/`+pendingKey+`"]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %#v err=%v", tasks, err)
	}
	attachments := tasks[0].InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].AssetID != uploadID || attachments[0].Role != "ecommerce_product" || attachments[0].ContentType != "image/png" {
		t.Fatalf("frozen ecommerce attachments = %#v", attachments)
	}
	if photos := tasks[0].Ecommerce.Data().ProductPhotos; len(photos) != 1 || !strings.Contains(photos[0], "/assets/users/"+userID+"/"+uploadID+"/") {
		t.Fatalf("finalized product photos = %#v", photos)
	}
}

func TestCreateTaskEcommerceValidatesAndFreezesInheritedProjectCapability(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "ecomcap", Tier: model.TierFree}); err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformEcommerce, Name: "Ecommerce", Status: model.ProjectStatusActive}
	project.SetEcommerceDefaults(model.EcommerceProjectDefaults{
		DefaultSelectedModules: map[string]int{"main_images": 1},
		ImageCapabilityKey:     "professional",
	})
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}

	routes := config.ImageGenerationRoutesConfig{DefaultCapability: "standard", Capabilities: map[string]config.ImageGenerationRouteConfig{
		"standard":     handlerTestImageCapabilityRoute("image.standard", model.TierFree, "1:1"),
		"professional": handlerTestImageCapabilityRoute("image.professional", model.TierFree, "21:9"),
	}}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	setHandlerImageCapabilities(taskSvc, h, repo, routes)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })

	resp := postJSON(t, app, "/tasks", `{"execution_profile":"effective","project_id":"`+project.ID+`","image_ratio":"3:4"}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, project.ID, "", 0, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("tasks = %#v, err=%v", tasks, err)
	}
	if tasks[0].ImageCapabilityKey != "professional" || tasks[0].ImageRatio != "3:4" {
		t.Fatalf("frozen image config = %q/%q", tasks[0].ImageCapabilityKey, tasks[0].ImageRatio)
	}
}

func TestCloneTask_AllowsCompletedTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "clone-completed@example.com",
		Password:   "hashed",
		InviteCode: "clonecompleted",
		Tier:       model.TierPro,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.New().String()
	source := &model.Task{
		ID:               taskID,
		UserID:           userID,
		ProjectID:        projectID,
		Type:             model.PlatformArticle,
		ExecutionProfile: "effective",
		Status:           model.TaskStatusCompleted,
		Prompt:           "clone this completed task",
	}
	freezeHandlerTaskImageCapability(t, source, "standard", handlerTestImageCapabilityRoute("image.standard", model.TierFree))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create task: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	setHandlerImageCapabilities(taskSvc, h, repo, config.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities: map[string]config.ImageGenerationRouteConfig{
			"standard": handlerTestImageCapabilityRoute("image.standard", model.TierFree),
		},
	})

	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Clone(c)
	})
	missingProfile := postJSON(t, app, "/tasks/"+taskID+"/clone", `{}`)
	if missingProfile.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(missingProfile.Body)
		missingProfile.Body.Close()
		t.Fatalf("missing execution profile status = %d, want 400 body=%s", missingProfile.StatusCode, body)
	}
	missingProfile.Body.Close()

	resp := postJSON(t, app, "/tasks/"+taskID+"/clone", `{
		"execution_profile":"balanced",
		"prompt":"edited exact clone prompt",
		"input_attachments":[{"type":"text","text":"edited exact attachment","file_name":"brief.txt"}]
	}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ID == "" || env.Data.ID == taskID {
		t.Fatalf("new task id = %q, want fresh id", env.Data.ID)
	}
	if env.Data.Status != model.TaskStatusPending {
		t.Fatalf("new task status = %q, want pending", env.Data.Status)
	}
	if env.Data.ExecutionProfile != "balanced" || env.Data.AgentProfileSnapshot.ProfileID != "balanced" {
		t.Fatalf("exact clone profile = %q snapshot=%#v, want balanced", env.Data.ExecutionProfile, env.Data.AgentProfileSnapshot)
	}
	if env.Data.ProjectID != projectID || env.Data.Type != model.PlatformArticle || env.Data.Prompt != "edited exact clone prompt" {
		t.Fatalf("exact clone destination/config = project %q type %q prompt %q", env.Data.ProjectID, env.Data.Type, env.Data.Prompt)
	}
	attachments := env.Data.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].Text != "edited exact attachment" {
		t.Fatalf("exact clone attachments = %#v", attachments)
	}
}

func TestCloneMontageTaskIgnoresLegacyUnavailableImageSettings(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: userID + "@example.com", Password: "hashed", InviteCode: "montageclone"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformMontage, Name: "Montage", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformMontage,
		ExecutionProfile: "effective", Status: model.TaskStatusFailed,
		ImageRatio: "16:9", ImageCapabilityKey: "standard",
	}
	source.SetMontageInput(model.MontageInput{Brief: "历史短片", PipelineKey: "default"})
	freezeHandlerTaskImageCapability(t, source, "standard", handlerTestImageCapabilityRoute("image.standard", model.TierFree))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetRuntimeDispatcher(availableRuntimeDispatcher{})
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	setHandlerImageCapabilities(taskSvc, h, repo, config.ImageGenerationRoutesConfig{Capabilities: map[string]config.ImageGenerationRouteConfig{
		"standard": handlerTestImageCapabilityRoute("image.standard", model.TierFree),
	}})
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })
	app.Post("/tasks/bulk-clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.BulkClone(c) })

	exact := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{"execution_profile":"effective"}`)
	defer exact.Body.Close()
	if exact.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(exact.Body)
		t.Fatalf("exact clone status = %d, want 200 body=%s", exact.StatusCode, body)
	}
	bulk := postJSON(t, app, "/tasks/bulk-clone", `{"task_ids":["`+source.ID+`"],"execution_profile":"effective"}`)
	defer bulk.Body.Close()
	if bulk.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(bulk.Body)
		t.Fatalf("bulk clone status = %d, want 200 body=%s", bulk.StatusCode, body)
	}
	var result struct {
		Data bulkTasksResponse `json:"data"`
	}
	if err := json.NewDecoder(bulk.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if result.Data.Succeeded != 1 {
		t.Fatalf("bulk clone result = %#v, want one success", result.Data)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("task count = %d, want source plus two clones", len(tasks))
	}
	for _, task := range tasks {
		if task.ID != source.ID && (task.ImageRatio != "16:9" || task.ImageCapabilityKey != "standard") {
			t.Fatalf("cloned Montage image settings = ratio %q, capability %q; want source values", task.ImageRatio, task.ImageCapabilityKey)
		}
	}
}

func TestCloneTask_FullEditableOverrides(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "editable-clone@example.com", Password: "hashed", InviteCode: "editableclone", Tier: model.TierPro}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformSeednote, Name: "Source seednote", Status: model.ProjectStatusActive}
	destinationProject := &model.Project{
		ID:           uuid.NewString(),
		UserID:       userID,
		Platform:     model.PlatformArticle,
		Name:         "Destination article",
		Status:       model.ProjectStatusActive,
		Instructions: "current destination instructions",
		VisualStyle:  "current destination visual style",
	}
	for _, project := range []*model.Project{sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project: %v", err)
		}
	}
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: model.PlatformSeednote, ExecutionProfile: "effective", Status: model.TaskStatusCompleted, Prompt: "source prompt"}
	source.SetProjectSnapshot(model.SnapshotProject(sourceProject))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create source task: %v", err)
	}
	referenceAsset := cutoverAsset(uuid.NewString(), userID, service.DirectUploadPurposeTaskReference, "clone-ref.png", "image/png")
	if err := repo.Assets().Create(ctx, referenceAsset); err != nil {
		t.Fatalf("create reference asset: %v", err)
	}

	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	setHandlerImageCapabilities(taskSvc, h, repo, config.ImageGenerationRoutesConfig{DefaultCapability: "free-image", Capabilities: map[string]config.ImageGenerationRouteConfig{"free-image": handlerTestImageCapabilityRoute("image.standard", model.TierFree, "1:1")}})
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

	resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{
		"execution_profile":"balanced","project_id":"`+destinationProject.ID+`",
		"quantity":2,
		"prompt":"edited full clone prompt",
		"image_ratio":"1:1",
		"image_capability_key":"free-image",
		"skip_reference_image":true,
		"reference_image":{"asset_id":"`+referenceAsset.ID+`"},
		"input_attachments":[{"type":"text","text":"validated attachment","file_name":"brief.txt"}],
		"watermark":true,
		"has_content_image":false,
		"has_tail_image":true,
		"article_with_cover":false,
			"article_with_content_images":false
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var env struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ProjectID != destinationProject.ID || env.Data.Type != model.PlatformArticle || env.Data.Prompt != "edited full clone prompt" {
		t.Fatalf("response task = project %q type %q prompt %q", env.Data.ProjectID, env.Data.Type, env.Data.Prompt)
	}

	tasks, err := repo.Tasks().FindByUserID(ctx, userID, destinationProject.ID, "", 0, 10)
	if err != nil {
		t.Fatalf("find destination tasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("destination tasks = %d, want 2", len(tasks))
	}
	for _, task := range tasks {
		if task.ExecutionProfile != "balanced" || task.AgentProfileSnapshot.ProfileID != "balanced" {
			t.Fatalf("clone profile = %q snapshot=%#v, want balanced", task.ExecutionProfile, task.AgentProfileSnapshot)
		}
		snapshot := task.ProjectSnapshot.Data()
		if task.Type != destinationProject.Platform || snapshot.Platform != destinationProject.Platform || snapshot.ProjectName != destinationProject.Name || snapshot.Instructions != destinationProject.Instructions || snapshot.VisualStyle != destinationProject.VisualStyle {
			t.Fatalf("destination snapshot = %#v task type=%q", snapshot, task.Type)
		}
		if task.ImageRatio != "1:1" || task.ImageCapabilityKey != "free-image" || !task.SkipReferenceImage || task.ReferenceImageAssetID != "" || !task.Watermark {
			t.Fatalf("shared overrides = %#v", task)
		}
		if task.HasContentImage || !task.HasTailImage || task.ArticleWithCover == nil || *task.ArticleWithCover || task.ArticleWithContentImages == nil || *task.ArticleWithContentImages {
			t.Fatalf("image overrides = %#v", task)
		}
		if task.ExecutionTarget != model.ExecutionTargetCloud || task.LocalClaimDeadline != nil {
			t.Fatalf("execution target = %q deadline=%v", task.ExecutionTarget, task.LocalClaimDeadline)
		}
		if got := task.InputAttachments.Data(); len(got) != 1 || got[0].Text != "validated attachment" {
			t.Fatalf("attachments = %#v", got)
		}
		if task.InputSourceTaskID != source.ID || task.InputSourceProjectID != source.ProjectID {
			t.Fatalf("source provenance = %q/%q", task.InputSourceTaskID, task.InputSourceProjectID)
		}
	}
}

func TestCloneTask_FullEditableRejectsProjectStyleReferenceAsArticlePortrait(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "inherited-reference@example.com", Password: "hashed", InviteCode: "inheritedreference", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	inherited := cutoverAsset(uuid.NewString(), userID, service.DirectUploadPurposeProjectReference, "inherited.png", "image/png")
	unrelated := cutoverAsset(uuid.NewString(), userID, service.DirectUploadPurposeProjectReference, "unrelated.png", "image/png")
	foreign := cutoverAsset(uuid.NewString(), uuid.NewString(), service.DirectUploadPurposeProjectReference, "foreign.png", "image/png")
	portrait := cutoverAsset(uuid.NewString(), userID, service.DirectUploadPurposeTaskReference, "portrait.png", "image/png")
	for _, asset := range []*model.Asset{inherited, unrelated, foreign, portrait} {
		if err := repo.Assets().Create(ctx, asset); err != nil {
			t.Fatalf("create asset %q: %v", asset.ID, err)
		}
	}

	sourceProject := &model.Project{
		ID:                    uuid.NewString(),
		UserID:                userID,
		Platform:              model.PlatformArticle,
		Name:                  "source",
		Status:                model.ProjectStatusActive,
		ReferenceImageAssetID: inherited.ID,
	}
	destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "destination", Status: model.ProjectStatusActive}
	for _, project := range []*model.Project{sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project: %v", err)
		}
	}
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: sourceProject.Platform, ExecutionProfile: "effective", Status: model.TaskStatusCompleted, Prompt: "source prompt"}
	source.SetProjectSnapshot(model.SnapshotProject(sourceProject))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}

	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	setHandlerImageCapabilities(taskSvc, h, repo, config.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities: map[string]config.ImageGenerationRouteConfig{
			"standard": handlerTestImageCapabilityRoute("image.standard", model.TierFree),
		},
	})
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })
	app.Post("/tasks/bulk-clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.BulkClone(c) })

	view, err := h.presentTaskReference(ctx, userID, source)
	if err != nil || view == nil || view.AssetID != inherited.ID {
		t.Fatalf("effective source reference = %#v, %v", view, err)
	}
	cloneBody := func(assetID string) string {
		return `{"execution_profile":"effective","project_id":"` + destinationProject.ID + `","quantity":1,"prompt":"editable clone","reference_image":{"asset_id":"` + assetID + `"}}`
	}
	taskCount := func() int {
		tasks, err := repo.Tasks().FindByUserID(ctx, userID, "", "", 0, 20)
		if err != nil {
			t.Fatalf("find tasks: %v", err)
		}
		return len(tasks)
	}

	resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", cloneBody(inherited.ID))
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("project style clone status = %d, want 400 body=%s", resp.StatusCode, body)
	}
	resp.Body.Close()

	resp = postJSON(t, app, "/tasks/"+source.ID+"/clone", cloneBody(portrait.ID))
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("portrait clone status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var firstEnvelope struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&firstEnvelope); err != nil {
		resp.Body.Close()
		t.Fatalf("decode portrait clone: %v", err)
	}
	resp.Body.Close()
	if firstEnvelope.Data.ReferenceImage == nil || firstEnvelope.Data.ReferenceImage.AssetID != portrait.ID {
		t.Fatalf("portrait clone reference = %#v, want %q", firstEnvelope.Data.ReferenceImage, portrait.ID)
	}

	firstClone, err := repo.Tasks().FindByID(ctx, firstEnvelope.Data.ID)
	if err != nil {
		t.Fatalf("find portrait clone: %v", err)
	}
	if firstClone.ReferenceImageAssetID != portrait.ID || len(firstClone.InputAttachments.Data()) != 0 {
		t.Fatalf("persisted portrait clone = reference %q attachments %#v", firstClone.ReferenceImageAssetID, firstClone.InputAttachments.Data())
	}
	firstClone.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(ctx, firstClone); err != nil {
		t.Fatalf("complete portrait clone: %v", err)
	}
	resp = postJSON(t, app, "/tasks/"+firstClone.ID+"/clone", `{"execution_profile":"effective"}`)
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("exact portrait clone status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var exactEnvelope struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&exactEnvelope); err != nil {
		resp.Body.Close()
		t.Fatalf("decode exact portrait clone: %v", err)
	}
	resp.Body.Close()
	if exactEnvelope.Data.ReferenceImage == nil || exactEnvelope.Data.ReferenceImage.AssetID != portrait.ID {
		t.Fatalf("exact portrait clone reference = %#v, want %q", exactEnvelope.Data.ReferenceImage, portrait.ID)
	}

	firstClone.Status = model.TaskStatusFailed
	if err := repo.Tasks().Update(ctx, firstClone); err != nil {
		t.Fatalf("fail portrait clone: %v", err)
	}
	resp = postJSON(t, app, "/tasks/bulk-clone", `{"task_ids":["`+firstClone.ID+`"],"execution_profile":"effective"}`)
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("bulk portrait clone status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var bulkEnvelope struct {
		Data bulkTasksResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&bulkEnvelope); err != nil {
		resp.Body.Close()
		t.Fatalf("decode bulk portrait clone: %v", err)
	}
	resp.Body.Close()
	if bulkEnvelope.Data.Succeeded != 1 || len(bulkEnvelope.Data.Results) != 1 || !bulkEnvelope.Data.Results[0].OK {
		t.Fatalf("bulk portrait clone result = %#v, want one success", bulkEnvelope.Data)
	}

	retiredCapabilityTask, err := repo.Tasks().FindByID(ctx, exactEnvelope.Data.ID)
	if err != nil {
		t.Fatalf("find task for retired capability bulk clone: %v", err)
	}
	retiredCapabilityTask.Status = model.TaskStatusFailed
	retiredCapabilityTask.ImageCapabilityKey = "retired-capability"
	if err := repo.Tasks().Update(ctx, retiredCapabilityTask); err != nil {
		t.Fatalf("mark retired capability task failed: %v", err)
	}
	resp = postJSON(t, app, "/tasks/bulk-clone", `{"task_ids":["`+retiredCapabilityTask.ID+`"],"execution_profile":"effective"}`)
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("retired capability bulk clone status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	bulkEnvelope = struct {
		Data bulkTasksResponse `json:"data"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&bulkEnvelope); err != nil {
		resp.Body.Close()
		t.Fatalf("decode retired capability bulk clone: %v", err)
	}
	resp.Body.Close()
	if bulkEnvelope.Data.Succeeded != 0 || len(bulkEnvelope.Data.Results) != 1 || bulkEnvelope.Data.Results[0].Reason != "image_capability_unavailable" {
		t.Fatalf("retired capability bulk clone result = %#v, want image_capability_unavailable", bulkEnvelope.Data)
	}

	for _, test := range []struct {
		name       string
		assetID    string
		wantStatus int
	}{
		{name: "unrelated same-user project reference", assetID: unrelated.ID, wantStatus: fiber.StatusBadRequest},
		{name: "foreign project reference", assetID: foreign.ID, wantStatus: fiber.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := taskCount()
			resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", cloneBody(test.assetID))
			defer resp.Body.Close()
			if resp.StatusCode != test.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, test.wantStatus, body)
			}
			if got := taskCount(); got != before {
				t.Fatalf("task count after rejected clone = %d, want %d", got, before)
			}
		})
	}

	beforeCreate := taskCount()
	resp = postJSON(t, app, "/tasks", cloneBody(inherited.ID))
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create project reference status = %d, want 400 body=%s", resp.StatusCode, body)
	}
	if got := taskCount(); got != beforeCreate {
		t.Fatalf("task count after rejected create = %d, want %d", got, beforeCreate)
	}
}

func TestCloneTask_FullEditableReusesOnlyExactDirectAIEntryReference(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "ai-entry-clone@example.com", Password: "hashed", InviteCode: "aientryclone", Tier: model.TierFree}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	trusted := cutoverAsset(uuid.NewString(), userID, service.DirectUploadPurposeAIEntryAttachment, "trusted.png", "image/png")
	unrelated := cutoverAsset(uuid.NewString(), userID, service.DirectUploadPurposeAIEntryAttachment, "unrelated.png", "image/png")
	foreign := cutoverAsset(uuid.NewString(), uuid.NewString(), service.DirectUploadPurposeAIEntryAttachment, "foreign.png", "image/png")
	for _, asset := range []*model.Asset{trusted, unrelated, foreign} {
		if err := repo.Assets().Create(ctx, asset); err != nil {
			t.Fatalf("create asset %q: %v", asset.ID, err)
		}
	}

	sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "source", Status: model.ProjectStatusActive}
	destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "destination", Status: model.ProjectStatusActive}
	for _, project := range []*model.Project{sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatalf("create project: %v", err)
		}
	}
	source := &model.Task{
		ID:                    uuid.NewString(),
		UserID:                userID,
		ProjectID:             sourceProject.ID,
		Type:                  sourceProject.Platform,
		ExecutionProfile:      "effective",
		Status:                model.TaskStatusCompleted,
		Prompt:                "AI entry source",
		ReferenceImageAssetID: trusted.ID,
	}
	source.SetProjectSnapshot(model.SnapshotProject(sourceProject))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}
	snapshotOnlySource := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: sourceProject.Platform, ExecutionProfile: "effective", Status: model.TaskStatusCompleted}
	snapshotOnlySource.SetProjectSnapshot(model.ProjectSnapshot{ReferenceImageAssetID: unrelated.ID})
	if err := repo.Tasks().Create(ctx, snapshotOnlySource); err != nil {
		t.Fatalf("create snapshot-only source: %v", err)
	}

	store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
	taskSvc.SetReferenceAssetService(referenceSvc)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	h.SetReferenceAssetService(referenceSvc)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Create(c) })
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

	view, err := h.presentTaskReference(ctx, userID, source)
	if err != nil || view == nil || view.AssetID != trusted.ID {
		t.Fatalf("source reference view = %#v, %v", view, err)
	}
	cloneBody := func(assetID string) string {
		return `{"execution_profile":"effective","project_id":"` + destinationProject.ID + `","quantity":1,"reference_image":{"asset_id":"` + assetID + `"}}`
	}
	taskCount := func() int64 {
		count, err := repo.Tasks().CountByUserID(ctx, userID, "", "")
		if err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return count
	}

	resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", cloneBody(trusted.ID))
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("trusted AI-entry clone status = %d, want 200 body=%s", resp.StatusCode, body)
	}
	var envelope struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		resp.Body.Close()
		t.Fatalf("decode trusted AI-entry clone: %v", err)
	}
	resp.Body.Close()
	if envelope.Data.ReferenceImage == nil || envelope.Data.ReferenceImage.AssetID != trusted.ID {
		t.Fatalf("clone reference view = %#v, want %q", envelope.Data.ReferenceImage, trusted.ID)
	}
	persisted, err := repo.Tasks().FindByID(ctx, envelope.Data.ID)
	if err != nil {
		t.Fatalf("find trusted AI-entry clone: %v", err)
	}
	if persisted.ReferenceImageAssetID != trusted.ID || len(persisted.InputAttachments.Data()) != 0 {
		t.Fatalf("persisted clone reference = %q attachments=%#v, want dedicated asset %q", persisted.ReferenceImageAssetID, persisted.InputAttachments.Data(), trusted.ID)
	}

	for _, test := range []struct {
		name       string
		sourceID   string
		assetID    string
		wantStatus int
	}{
		{name: "unrelated same-user direct asset", sourceID: source.ID, assetID: unrelated.ID, wantStatus: fiber.StatusBadRequest},
		{name: "foreign direct asset", sourceID: source.ID, assetID: foreign.ID, wantStatus: fiber.StatusForbidden},
		{name: "snapshot-only match", sourceID: snapshotOnlySource.ID, assetID: unrelated.ID, wantStatus: fiber.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := taskCount()
			resp := postJSON(t, app, "/tasks/"+test.sourceID+"/clone", cloneBody(test.assetID))
			defer resp.Body.Close()
			if resp.StatusCode != test.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, test.wantStatus, body)
			}
			if got := taskCount(); got != before {
				t.Fatalf("task count after rejected clone = %d, want %d", got, before)
			}
		})
	}

	beforeCreate := taskCount()
	resp = postJSON(t, app, "/tasks", cloneBody(trusted.ID))
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create AI-entry reference status = %d, want 400 body=%s", resp.StatusCode, body)
	}
	if got := taskCount(); got != beforeCreate {
		t.Fatalf("task count after rejected create = %d, want %d", got, beforeCreate)
	}
}

func TestCloneTask_FullEditableTypeSpecificFields(t *testing.T) {
	tests := []struct {
		name       string
		platform   string
		typeFields string
		assert     func(t *testing.T, task *model.Task)
	}{
		{
			name:       "ecommerce",
			platform:   model.PlatformEcommerce,
			typeFields: `"selected_modules":{"main_images":2},"target_platform":"amazon","selling_points":"durable","language":"en"`,
			assert: func(t *testing.T, task *model.Task) {
				got := task.Ecommerce.Data()
				if got.SelectedModules["main_images"] != 2 || got.TargetPlatform != "amazon" || got.SellingPoints != "durable" || got.Language != "en" || len(got.ProductPhotos) != 0 {
					t.Fatalf("ecommerce config = %#v", got)
				}
			},
		},
		{
			name:       "montage",
			platform:   model.PlatformMontage,
			typeFields: `"montage_input":{"brief":"edited montage brief","pipeline_key":"default","preferences":{"duration_seconds":30}}`,
			assert: func(t *testing.T, task *model.Task) {
				got := task.MontageInput.Data()
				if got.Brief != "edited montage brief" || got.PipelineKey != "default" || got.Preferences.DurationSeconds != 30 {
					t.Fatalf("montage input = %#v", got)
				}
			},
		},
		{
			name:       "ecommerce language only",
			platform:   model.PlatformEcommerce,
			typeFields: `"language":"zh-CN"`,
			assert: func(t *testing.T, task *model.Task) {
				got := task.Ecommerce.Data()
				if got.SelectedModules["main_images"] != 1 || got.Language != "zh-CN" {
					t.Fatalf("language-only ecommerce config = %#v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskHandlerTestDB(t)
			repo := repository.New(db)
			userID := uuid.NewString()
			if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Email: tt.name + "-clone@example.com", Password: "hashed", InviteCode: tt.name + "clone"}); err != nil {
				t.Fatal(err)
			}
			sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "source", Status: model.ProjectStatusActive}
			destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: tt.platform, Name: tt.name, Status: model.ProjectStatusActive}
			if tt.platform == model.PlatformEcommerce {
				destinationProject.SetEcommerceDefaults(model.EcommerceProjectDefaults{DefaultSelectedModules: map[string]int{"main_images": 1}})
			}
			for _, project := range []*model.Project{sourceProject, destinationProject} {
				if err := repo.Projects().Create(t.Context(), project); err != nil {
					t.Fatal(err)
				}
			}
			source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: sourceProject.Platform, ExecutionProfile: "effective", Status: model.TaskStatusCompleted}
			if err := repo.Tasks().Create(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			logger := zerolog.New(io.Discard)
			taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
			if tt.platform == model.PlatformMontage {
				taskSvc.SetRuntimeDispatcher(availableRuntimeDispatcher{})
			}
			h := NewTaskHandler(taskSvc, &logger)
			h.SetRepository(repo)
			app := fiber.New()
			app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

			resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{"execution_profile":"effective","project_id":"`+destinationProject.ID+`","quantity":2,`+tt.typeFields+`}`)
			defer resp.Body.Close()
			if resp.StatusCode != fiber.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d body=%s", resp.StatusCode, body)
			}
			tasks, err := repo.Tasks().FindByUserID(t.Context(), userID, destinationProject.ID, "", 0, 10)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("destination tasks = %d err=%v", len(tasks), err)
			}
			tt.assert(t, tasks[0])
		})
	}
}

type cloneSourceReuseFixture struct {
	repo               repository.Repository
	app                *fiber.App
	store              storage.Provider
	signingStore       *cloneSourceReuseStorage
	userID             string
	rootProjectID      string
	rootTaskID         string
	sourceAsset        *model.Asset
	source             *model.Task
	destinationProject *model.Project
}

type cloneSourceReuseStorage struct {
	*fakeStorageProvider
	signedKeys []string
	getURL     func(string) string
}

func (s *cloneSourceReuseStorage) GetURL(key string) string {
	if s.getURL != nil {
		return s.getURL(key)
	}
	return s.fakeStorageProvider.GetURL(key)
}

func (s *cloneSourceReuseStorage) DownloadURL(_ context.Context, key string, _ int) (string, error) {
	s.signedKeys = append(s.signedKeys, key)
	return "https://signed.example.com/" + key, nil
}

func setupCloneSourceReuseFixture(t *testing.T, destinationPlatform string) *cloneSourceReuseFixture {
	t.Helper()
	store := &cloneSourceReuseStorage{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
	return setupCloneSourceReuseFixtureWithStore(t, destinationPlatform, store)
}

func setupCloneSourceReuseFixtureWithStore(t *testing.T, destinationPlatform string, store storage.Provider) *cloneSourceReuseFixture {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: uuid.NewString() + "@source-reuse.test", Password: "hashed", InviteCode: strings.ReplaceAll(uuid.NewString(), "-", "")[:16]}); err != nil {
		t.Fatal(err)
	}
	rootProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "root source", Status: model.ProjectStatusActive}
	sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "intermediate source", Status: model.ProjectStatusActive}
	destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: destinationPlatform, Name: "destination", Status: model.ProjectStatusActive}
	if destinationPlatform == model.PlatformEcommerce {
		destinationProject.SetEcommerceDefaults(model.EcommerceProjectDefaults{DefaultSelectedModules: map[string]int{"main_images": 1}})
	}
	for _, project := range []*model.Project{rootProject, sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	rootTask := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: rootProject.ID, Type: rootProject.Platform, ExecutionProfile: "effective", Status: model.TaskStatusCompleted}
	sourceAsset := &model.Asset{
		ID: uuid.NewString(), UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment,
		StorageKey: "assets/users/" + userID + "/clone/source.png", FileName: "source.png",
		ContentType: "image/png", Size: 10, ETag: "source-etag",
	}
	if err := repo.Assets().Create(ctx, sourceAsset); err != nil {
		t.Fatal(err)
	}
	source := &model.Task{
		ID:                   uuid.NewString(),
		UserID:               userID,
		ProjectID:            sourceProject.ID,
		Type:                 sourceProject.Platform,
		ExecutionProfile:     "effective",
		Status:               model.TaskStatusCompleted,
		InputSourceTaskID:    rootTask.ID,
		InputSourceProjectID: rootProject.ID,
	}
	for _, task := range []*model.Task{rootTask, source} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	if destinationPlatform == model.PlatformMontage {
		taskSvc.SetRuntimeDispatcher(availableRuntimeDispatcher{})
	}
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	h.SetStore(store)
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })
	signingStore, _ := store.(*cloneSourceReuseStorage)
	return &cloneSourceReuseFixture{
		repo:               repo,
		app:                app,
		store:              store,
		signingStore:       signingStore,
		userID:             userID,
		rootProjectID:      rootProject.ID,
		rootTaskID:         rootTask.ID,
		sourceAsset:        sourceAsset,
		source:             source,
		destinationProject: destinationProject,
	}
}

func bootstrapClonedSourceAttachment(t *testing.T, fixture *cloneSourceReuseFixture, task *model.Task) {
	t.Helper()
	if fixture.signingStore == nil {
		t.Fatal("bootstrap fixture requires signing storage")
	}
	executionID := uuid.NewString()
	task.Status = model.TaskStatusRunning
	task.CurrentExecutionID = &executionID
	if err := fixture.repo.Tasks().Update(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	profiledExecution := model.NewTaskExecutionAgentProfile(task.AgentProfileSnapshot, task.AgentProfileFingerprint)
	profiledExecution.ID, profiledExecution.TaskID, profiledExecution.Attempt = executionID, task.ID, 1
	profiledExecution.Target, profiledExecution.Status = "kubernetes", model.TaskExecutionStarting
	profiledExecution.RuntimeScope, profiledExecution.RuntimeWorkload, profiledExecution.RuntimeInstanceID = "anban", "job-1", "pod-uid-1"
	if err := fixture.repo.TaskExecutions().Create(t.Context(), &profiledExecution); err != nil {
		t.Fatal(err)
	}
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := service.NewAgentBootstrapService(fixture.repo, tokens, service.AgentBootstrapConfig{
		TokenTTL: time.Hour, SignedURLTTL: 60, Store: fixture.store,
		Registry: handlerTestAgentProfileRegistry(t),
	}, zerolog.Nop())
	response, err := bootstrap.Bootstrap(t.Context(), &serveragent.WorkloadIdentity{
		Target: "kubernetes", RuntimeIdentity: model.RuntimeIdentity{Scope: "anban", Workload: "job-1", InstanceID: "pod-uid-1"},
		ExecutionID: executionID, TaskID: task.ID, ProjectID: task.ProjectID, UserID: task.UserID,
		Deadline: time.Now().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("bootstrap cloned attachment: %v", err)
	}
	for _, file := range response.Files {
		if strings.HasPrefix(file.Path, ".anban-creator/input-attachments/") && file.DownloadURL != "" {
			if len(fixture.signingStore.signedKeys) != 1 || fixture.signingStore.signedKeys[0] != fixture.sourceAsset.StorageKey {
				t.Fatalf("signed keys = %#v, attachment = %#v", fixture.signingStore.signedKeys, task.InputAttachments.Data()[0])
			}
			return
		}
	}
	t.Fatalf("bootstrap files have no signed attachment: %#v", response.Files)
}

func cloneSourceAttachmentAPIShape(t *testing.T, fixture *cloneSourceReuseFixture, attachment model.EntryAttachment) map[string]any {
	t.Helper()
	fixture.source.SetInputAttachments([]model.EntryAttachment{attachment})
	response := taskAPIResponse(fixture.source, fixture.store)
	attachments, ok := response["input_attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("task API input_attachments = %#v", response["input_attachments"])
	}
	result, ok := attachments[0].(map[string]any)
	if !ok {
		t.Fatalf("task API attachment = %#v", attachments[0])
	}
	return result
}

func cloneSourceAttachmentRequest(t *testing.T, projectID string, attachment map[string]any) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"execution_profile": "effective",
		"project_id":        projectID,
		"quantity":          1,
		"input_attachments": []any{attachment},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func assertCloneSourceReuseRejectedWithoutPersistence(t *testing.T, fixture *cloneSourceReuseFixture, requestBody string) {
	t.Helper()
	resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", requestBody)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, body)
	}
	count, err := fixture.repo.Tasks().CountByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "")
	if err != nil || count != 0 {
		t.Fatalf("destination task count = %d err=%v, want 0", count, err)
	}
	total, err := fixture.repo.Tasks().CountByUserID(t.Context(), fixture.userID, "", "")
	if err != nil || total != 2 {
		t.Fatalf("total task count = %d err=%v, want unchanged root+source tasks", total, err)
	}
}

func cloneSourceTaskURL(userID, projectID, taskID, fileName string) string {
	key := strings.Join([]string{"uploads", "users", userID, "projects", projectID, "tasks", taskID, "inputs", fileName}, "/")
	return "/api/v1/files/" + key
}

func cloneSourceReuseRequest(platform, projectID, rawURL string) string {
	switch platform {
	case model.PlatformEcommerce:
		return `{"execution_profile":"effective","project_id":"` + projectID + `","quantity":1,"product_photos":["` + rawURL + `"]}`
	case model.PlatformMontage:
		return `{"execution_profile":"effective","project_id":"` + projectID + `","quantity":1,"montage_input":{"brief":"reuse source","source_assets":[{"type":"image","url":"` + rawURL + `","file_name":"source.png","mime_type":"image/png"}]}}`
	default:
		return `{"execution_profile":"effective","project_id":"` + projectID + `","quantity":1,"input_attachments":[{"type":"image","url":"` + rawURL + `","file_name":"source.png","content_type":"image/png"}]}`
	}
}

func TestCloneTask_FullEditableEnforcesTrustedRootSourceReusePolicy(t *testing.T) {
	for _, tt := range []struct {
		platform   string
		wantStatus int
	}{
		{platform: model.PlatformArticle, wantStatus: fiber.StatusBadRequest},
		{platform: model.PlatformMontage, wantStatus: fiber.StatusOK},
	} {
		t.Run(tt.platform, func(t *testing.T) {
			fixture := setupCloneSourceReuseFixture(t, tt.platform)
			rawURL := cloneSourceTaskURL(fixture.userID, fixture.rootProjectID, fixture.rootTaskID, "source.png")
			resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", cloneSourceReuseRequest(tt.platform, fixture.destinationProject.ID, rawURL))
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, tt.wantStatus, body)
			}
			if tt.wantStatus != fiber.StatusOK {
				return
			}
			tasks, err := fixture.repo.Tasks().FindByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "", 0, 10)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("destination tasks = %d err=%v", len(tasks), err)
			}
			got := tasks[0]
			if got.InputSourceTaskID != fixture.rootTaskID || got.InputSourceProjectID != fixture.rootProjectID {
				t.Fatalf("root source = %q/%q", got.InputSourceTaskID, got.InputSourceProjectID)
			}
			switch tt.platform {
			case model.PlatformEcommerce:
				if photos := got.Ecommerce.Data().ProductPhotos; len(photos) != 1 || photos[0] != rawURL {
					t.Fatalf("product photos = %#v", photos)
				}
			case model.PlatformMontage:
				if assets := got.MontageInput.Data().SourceAssets; len(assets) != 1 || assets[0].URL != rawURL {
					t.Fatalf("montage assets = %#v", assets)
				}
			default:
				if attachments := got.InputAttachments.Data(); len(attachments) != 1 || attachments[0].URL != rawURL {
					t.Fatalf("attachments = %#v", attachments)
				}
			}
		})
	}
}

func TestCloneTask_FullEditableAllowsTaskAPIAttachmentSourceReuse(t *testing.T) {
	fixture := setupCloneSourceReuseFixture(t, model.PlatformArticle)
	apiAttachment := cloneSourceAttachmentAPIShape(t, fixture, model.EntryAttachment{
		AssetID: fixture.sourceAsset.ID,
	})
	if _, hasUploadID := apiAttachment["upload_id"]; hasUploadID {
		t.Fatalf("task API attachment unexpectedly has upload_id: %#v", apiAttachment)
	}
	if _, hasURL := apiAttachment["url"]; hasURL {
		t.Fatalf("task API attachment unexpectedly has url: %#v", apiAttachment)
	}
	if _, hasKey := apiAttachment["key"]; hasKey {
		t.Fatalf("task API attachment unexpectedly has key: %#v", apiAttachment)
	}
	resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", cloneSourceAttachmentRequest(t, fixture.destinationProject.ID, apiAttachment))
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 body=%s attachment=%#v", resp.StatusCode, body, apiAttachment)
	}
	tasks, err := fixture.repo.Tasks().FindByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "", 0, 10)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("destination tasks = %d err=%v", len(tasks), err)
	}
	got := tasks[0].InputAttachments.Data()
	if len(got) != 1 || got[0].AssetID != fixture.sourceAsset.ID || got[0].URL != "" || got[0].Key != "" || got[0].UploadID != "" {
		t.Fatalf("persisted attachment = %#v, want asset identity only", got)
	}
	bootstrapClonedSourceAttachment(t, fixture, tasks[0])
}

func TestCloneTask_FullEditableRejectsUntrustedTaskAPIAttachmentSourceReuse(t *testing.T) {
	mismatches := []struct {
		name string
		ids  func(*cloneSourceReuseFixture) (string, string, string)
	}{
		{name: "wrong user", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return uuid.NewString(), f.rootProjectID, f.rootTaskID
		}},
		{name: "wrong project", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return f.userID, uuid.NewString(), f.rootTaskID
		}},
		{name: "wrong task", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return f.userID, f.rootProjectID, uuid.NewString()
		}},
	}
	for _, mismatch := range mismatches {
		t.Run(mismatch.name, func(t *testing.T) {
			fixture := setupCloneSourceReuseFixture(t, model.PlatformArticle)
			userID, projectID, taskID := mismatch.ids(fixture)
			apiAttachment := cloneSourceAttachmentAPIShape(t, fixture, model.EntryAttachment{
				Type:        "image",
				URL:         cloneSourceTaskURL(userID, projectID, taskID, "source.png"),
				FileName:    "source.png",
				ContentType: "image/png",
			})
			assertCloneSourceReuseRejectedWithoutPersistence(t, fixture, cloneSourceAttachmentRequest(t, fixture.destinationProject.ID, apiAttachment))
		})
	}

	t.Run("URL and key identify different objects", func(t *testing.T) {
		fixture := setupCloneSourceReuseFixture(t, model.PlatformArticle)
		apiAttachment := cloneSourceAttachmentAPIShape(t, fixture, model.EntryAttachment{
			Type:        "image",
			URL:         cloneSourceTaskURL(fixture.userID, fixture.rootProjectID, fixture.rootTaskID, "source.png"),
			FileName:    "source.png",
			ContentType: "image/png",
		})
		apiAttachment["key"] = strings.TrimPrefix(cloneSourceTaskURL(fixture.userID, fixture.rootProjectID, fixture.rootTaskID, "other.png"), "/api/v1/files/")
		assertCloneSourceReuseRejectedWithoutPersistence(t, fixture, cloneSourceAttachmentRequest(t, fixture.destinationProject.ID, apiAttachment))
	})

	t.Run("untrusted key only", func(t *testing.T) {
		fixture := setupCloneSourceReuseFixture(t, model.PlatformArticle)
		apiAttachment := cloneSourceAttachmentAPIShape(t, fixture, model.EntryAttachment{
			Type:        "image",
			Key:         strings.TrimPrefix(cloneSourceTaskURL(uuid.NewString(), fixture.rootProjectID, fixture.rootTaskID, "source.png"), "/api/v1/files/"),
			FileName:    "source.png",
			ContentType: "image/png",
		})
		assertCloneSourceReuseRejectedWithoutPersistence(t, fixture, cloneSourceAttachmentRequest(t, fixture.destinationProject.ID, apiAttachment))
	})
}

func TestCloneTask_FullEditableRejectsKeyOnlyReuseWithoutOwnedProviderURL(t *testing.T) {
	tests := []struct {
		name  string
		store storage.Provider
	}{
		{name: "missing storage"},
		{
			name: "empty provider URL",
			store: &cloneSourceReuseStorage{
				fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}},
				getURL:              func(string) string { return "" },
			},
		},
		{
			name: "non-owned provider URL",
			store: &cloneSourceReuseStorage{
				fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}},
				getURL:              func(string) string { return "https://public.example.com/source.png" },
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := setupCloneSourceReuseFixtureWithStore(t, model.PlatformArticle, tt.store)
			apiAttachment := cloneSourceAttachmentAPIShape(t, fixture, model.EntryAttachment{
				Type:        "image",
				Key:         strings.TrimPrefix(cloneSourceTaskURL(fixture.userID, fixture.rootProjectID, fixture.rootTaskID, "source.png"), "/api/v1/files/"),
				FileName:    "source.png",
				ContentType: "image/png",
			})
			assertCloneSourceReuseRejectedWithoutPersistence(t, fixture, cloneSourceAttachmentRequest(t, fixture.destinationProject.ID, apiAttachment))
		})
	}
}

func TestCloneTask_FullEditableEnforcesPlatformExternalSourceURLPolicy(t *testing.T) {
	tests := []struct {
		platform   string
		wantStatus int
	}{
		{platform: model.PlatformEcommerce, wantStatus: fiber.StatusBadRequest},
		{platform: model.PlatformMontage, wantStatus: fiber.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.platform, func(t *testing.T) {
			fixture := setupCloneSourceReuseFixture(t, tt.platform)
			rawURL := "https://public.example.com/source.png"
			resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", cloneSourceReuseRequest(tt.platform, fixture.destinationProject.ID, rawURL))
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, tt.wantStatus, body)
			}
			tasks, err := fixture.repo.Tasks().FindByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "", 0, 10)
			if err != nil {
				t.Fatal(err)
			}
			wantTasks := 1
			if tt.wantStatus != fiber.StatusOK {
				wantTasks = 0
			}
			if len(tasks) != wantTasks {
				t.Fatalf("destination tasks = %d, want %d", len(tasks), wantTasks)
			}
		})
	}
}

func TestCloneTask_FullEditableRejectsUntrustedTaskScopedSourceURLs(t *testing.T) {
	mismatches := []struct {
		name string
		ids  func(*cloneSourceReuseFixture) (string, string, string)
	}{
		{name: "wrong user", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return uuid.NewString(), f.rootProjectID, f.rootTaskID
		}},
		{name: "wrong project", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return f.userID, uuid.NewString(), f.rootTaskID
		}},
		{name: "wrong task", ids: func(f *cloneSourceReuseFixture) (string, string, string) {
			return f.userID, f.rootProjectID, uuid.NewString()
		}},
	}
	for _, platform := range []string{model.PlatformArticle, model.PlatformEcommerce, model.PlatformMontage} {
		for _, mismatch := range mismatches {
			t.Run(platform+"/"+mismatch.name, func(t *testing.T) {
				fixture := setupCloneSourceReuseFixture(t, platform)
				userID, projectID, taskID := mismatch.ids(fixture)
				rawURL := cloneSourceTaskURL(userID, projectID, taskID, "source.png")
				resp := postJSON(t, fixture.app, "/tasks/"+fixture.source.ID+"/clone", cloneSourceReuseRequest(platform, fixture.destinationProject.ID, rawURL))
				defer resp.Body.Close()
				if resp.StatusCode != fiber.StatusBadRequest {
					body, _ := io.ReadAll(resp.Body)
					t.Fatalf("status = %d, want 400 body=%s", resp.StatusCode, body)
				}
				count, err := fixture.repo.Tasks().CountByUserID(t.Context(), fixture.userID, fixture.destinationProject.ID, "")
				if err != nil || count != 0 {
					t.Fatalf("destination task count = %d err=%v, want 0", count, err)
				}
				total, err := fixture.repo.Tasks().CountByUserID(t.Context(), fixture.userID, "", "")
				if err != nil || total != 2 {
					t.Fatalf("total task count = %d err=%v, want unchanged root+source tasks", total, err)
				}
			})
		}
	}
}

func TestCloneTask_FullEditableRejectsInvalidInputBeforePersistence(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, repo repository.Repository, userID string, destination *model.Project) string
		wantStatus int
	}{
		{
			name: "foreign project",
			prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
				destination.UserID = uuid.NewString()
				if err := repo.Projects().Create(t.Context(), destination); err != nil {
					t.Fatal(err)
				}
				return `{"execution_profile":"effective","project_id":"` + destination.ID + `","quantity":1}`
			},
			wantStatus: fiber.StatusForbidden,
		},
		{
			name: "inactive project",
			prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
				destination.Status = model.ProjectStatusArchived
				if err := repo.Projects().Create(t.Context(), destination); err != nil {
					t.Fatal(err)
				}
				return `{"execution_profile":"effective","project_id":"` + destination.ID + `","quantity":1}`
			},
			wantStatus: fiber.StatusBadRequest,
		},
		{name: "quantity zero", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"execution_profile":"effective","project_id":"` + destination.ID + `","quantity":0}`
		}, wantStatus: fiber.StatusBadRequest},
		{name: "quantity above five", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"execution_profile":"effective","project_id":"` + destination.ID + `","quantity":6}`
		}, wantStatus: fiber.StatusBadRequest},
		{name: "unavailable model", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"execution_profile":"effective","project_id":"` + destination.ID + `","quantity":1,"image_capability_key":"unknown"}`
		}, wantStatus: fiber.StatusForbidden},
		{name: "unsafe attachment", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			return `{"execution_profile":"effective","project_id":"` + destination.ID + `","quantity":1,"input_attachments":[{"type":"image","url":"file:///etc/passwd","file_name":"passwd.png","content_type":"image/png"}]}`
		}, wantStatus: fiber.StatusBadRequest},
		{name: "inaccessible reference", prepare: func(t *testing.T, repo repository.Repository, _ string, destination *model.Project) string {
			if err := repo.Projects().Create(t.Context(), destination); err != nil {
				t.Fatal(err)
			}
			asset := cutoverAsset(uuid.NewString(), uuid.NewString(), service.DirectUploadPurposeTaskReference, "foreign.png", "image/png")
			if err := repo.Assets().Create(t.Context(), asset); err != nil {
				t.Fatal(err)
			}
			return `{"execution_profile":"effective","project_id":"` + destination.ID + `","quantity":1,"reference_image":{"asset_id":"` + asset.ID + `"}}`
		}, wantStatus: fiber.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupTaskHandlerTestDB(t)
			repo := repository.New(db)
			userID := uuid.NewString()
			if err := repo.Users().Create(t.Context(), &model.User{ID: userID, Email: tt.name + "@example.com", Password: "hashed", InviteCode: strings.ReplaceAll(tt.name, " ", "")}); err != nil {
				t.Fatal(err)
			}
			sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "source", Status: model.ProjectStatusActive}
			if err := repo.Projects().Create(t.Context(), sourceProject); err != nil {
				t.Fatal(err)
			}
			source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: sourceProject.Platform, ExecutionProfile: "effective", Status: model.TaskStatusCompleted}
			if err := repo.Tasks().Create(t.Context(), source); err != nil {
				t.Fatal(err)
			}
			destination := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "destination", Status: model.ProjectStatusActive}
			body := tt.prepare(t, repo, userID, destination)

			store := &referencePresentationStore{fakeStorageProvider: &fakeStorageProvider{objects: map[string]*storage.ObjectInfo{}}}
			logger := zerolog.New(io.Discard)
			taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
			referenceSvc := service.NewReferenceAssetService(repo, store, time.Now)
			taskSvc.SetReferenceAssetService(referenceSvc)
			h := NewTaskHandler(taskSvc, &logger)
			h.SetRepository(repo)
			h.SetStore(store)
			h.SetReferenceAssetService(referenceSvc)
			setHandlerImageCapabilities(taskSvc, h, repo, config.ImageGenerationRoutesConfig{DefaultCapability: "free-image", Capabilities: map[string]config.ImageGenerationRouteConfig{"free-image": handlerTestImageCapabilityRoute("image.standard", model.TierFree, "1:1")}})
			app := fiber.New()
			app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

			resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", body)
			defer resp.Body.Close()
			if resp.StatusCode != tt.wantStatus {
				responseBody, _ := io.ReadAll(resp.Body)
				t.Fatalf("status = %d, want %d body=%s", resp.StatusCode, tt.wantStatus, responseBody)
			}
			tasks, err := repo.Tasks().FindByUserID(t.Context(), userID, "", "", 0, 20)
			if err != nil {
				t.Fatal(err)
			}
			if len(tasks) != 1 || tasks[0].ID != source.ID {
				t.Fatalf("tasks after rejection = %#v, want only source", tasks)
			}
		})
	}
}

func TestCloneTask_FullEditableRejectsInsufficientBalanceWithoutCreatingTask(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "clone-insufficient@example.com", Password: "hashed", InviteCode: "cloneinsufficient"}); err != nil {
		t.Fatal(err)
	}
	sourceProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "source", Status: model.ProjectStatusActive}
	destinationProject := &model.Project{ID: uuid.NewString(), UserID: userID, Platform: model.PlatformArticle, Name: "destination", Status: model.ProjectStatusActive}
	for _, project := range []*model.Project{sourceProject, destinationProject} {
		if err := repo.Projects().Create(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: sourceProject.ID, Type: model.PlatformArticle, ExecutionProfile: "effective", Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	bundle := billing.Bundle{
		Economics: billing.EconomicsConfig{CreditsPerCNY: 1_000},
		Policy: billing.PolicySnapshot{
			TaskAdmission: billing.TaskAdmissionPolicy{RequireZeroDebt: true, RequireFullPrice: true},
			AcceptedTask:  billing.AcceptedTaskPolicy{ContinueWhenBalanceNegative: true, OperationChargeMayCreateDebt: true},
			TopUp:         billing.TopUpPolicy{RepayDebtFirst: true},
		},
		Products: billing.ProductCatalog{
			CatalogID:        "clone-insufficient-v1",
			Currency:         "credits",
			TierRatesPercent: map[string]int64{"free": 100, "pro": 90, "enterprise": 80},
			SKUs: []billing.SKUConfig{{
				ID: "task.article.effective", Operation: "task.article", ExecutionProfile: "effective", ChargePolicy: "task_admission", PriceCredits: 500, Delivery: "article",
			}},
		},
	}
	now := time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)
	catalog := service.NewBillingCatalogService(repo, &bundle, service.BillingCatalogOptions{Now: func() time.Time { return now }})
	if _, err := catalog.Publish(ctx); err != nil {
		t.Fatalf("publish billing catalog: %v", err)
	}
	if err := repo.Billing().CreateAccount(ctx, &model.BillingWalletAccount{UserID: userID}); err != nil {
		t.Fatalf("create empty wallet: %v", err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetBillingCatalogService(catalog)
	taskSvc.SetBillingWalletService(service.NewBillingWalletService(repo, &bundle, service.BillingWalletOptions{Now: func() time.Time { return now }}))
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/clone", func(c fiber.Ctx) error { c.Locals("user_id", userID); return h.Clone(c) })

	resp := postJSON(t, app, "/tasks/"+source.ID+"/clone", `{"execution_profile":"effective","project_id":"`+destinationProject.ID+`","quantity":1,"prompt":"new task"}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusPaymentRequired {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 402 body=%s", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, "", "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != source.ID {
		t.Fatalf("tasks after insufficient balance = %#v, want only source", tasks)
	}
}

func TestResumeTask_ReusesCurrentTaskAndAcceptsPromptFilesAndLabels(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "resume@example.com",
		Password:   "hashed",
		InviteCode: "resume",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.New().String()
	task := &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
		Prompt:    "failed task",
	}
	freezeHandlerTaskProfile(t, task)
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	uploadID := "resume-upload"
	pendingKey := "uploads/pending/" + userID + "/" + uploadID + "/notes.md"
	if err := repo.UploadSessions().Create(ctx, &model.UploadSession{
		ID: uploadID, UserID: userID, Purpose: service.DirectUploadPurposeAIEntryAttachment,
		StagingKey: pendingKey, FileName: "notes.md", ContentType: "text/markdown", Size: int64(len("# notes")),
		Status: model.UploadSessionPending, ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	store := uploadSessionStatStore(repo.UploadSessions())
	store.data = map[string][]byte{pendingKey: []byte("# notes")}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, store, &logger, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/resume", `{
		"prompt":"继续写结论",
		"input_attachments":[{
			"type":"text","upload_id":"`+uploadID+`","key":"`+pendingKey+`",
			"file_name":"forged.exe","content_type":"application/x-msdownload","size":999999,
			"instruction":"修改意见"
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data model.Task `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Data.ID != taskID || env.Data.Status != model.TaskStatusPending {
		t.Fatalf("resume response = id %q status %q, want same pending task", env.Data.ID, env.Data.Status)
	}
	resumed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	var latest, file bool
	for _, attachment := range resumed.InputAttachments.Data() {
		switch attachment.Role {
		case model.EntryAttachmentRoleResumeLatest:
			latest = strings.Contains(attachment.Text, "继续写结论") && strings.Contains(attachment.Text, "修改意见") && strings.Contains(attachment.Text, "notes.md")
		case model.EntryAttachmentRoleResumeFile:
			file = attachment.Key != "" && attachment.FileName == "notes.md"
		}
	}
	if !latest || !file {
		t.Fatalf("resume attachments latest=%v file=%v: %#v", latest, file, resumed.InputAttachments.Data())
	}
	upload, err := repo.UploadSessions().FindByID(ctx, uploadID)
	if err != nil || upload.Status != model.UploadSessionFinalized || upload.AssetID == "" {
		t.Fatalf("upload session = %#v err=%v, want finalized", upload, err)
	}
}

func TestResumeTask_AcceptsJSONPromptOnly(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "resume-prompt@example.com", Password: "hashed", InviteCode: "resumeprompt"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.NewString()
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusFailed}
	freezeHandlerTaskProfile(t, task)
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/resume", `{"prompt":"继续","input_attachments":[]}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status/body = %d/%s, want 200", resp.StatusCode, body)
	}
	resumed, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("task status = %q, want pending", resumed.Status)
	}
	attachments := resumed.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].Role != model.EntryAttachmentRoleResumeLatest || !strings.Contains(attachments[0].Text, "继续") {
		t.Fatalf("resume attachments = %#v, want latest prompt", attachments)
	}
}

func TestResumeTask_RejectsCompletedTaskAndPreservesDeliveryState(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "resume-completed@example.com", Password: "hashed", InviteCode: "resume-completed"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "Seednote", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	completedAt := time.Now().Add(-time.Minute)
	result := `{"success":true}`
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote,
		Status: model.TaskStatusCompleted, Result: &result, CompletedAt: &completedAt,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+task.ID+"/resume", `{"prompt":"rewrite delivery"}`)
	defer resp.Body.Close()
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusConflict || body.Msg != "已完成任务不可继续执行，请克隆任务创建新版本" {
		t.Fatalf("status=%d body=%#v", resp.StatusCode, body)
	}
	persisted, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.TaskStatusCompleted || persisted.Result == nil || *persisted.Result != result || persisted.CompletedAt == nil {
		t.Fatalf("completed task was mutated: %#v", persisted)
	}
}

func TestResumeTask_MapsDeletedFrozenProviderError(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "resume-profile@example.com", Password: "hashed", InviteCode: "resumeprofile"}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatal(err)
	}
	frozen := handlerTestProfile("effective", "Cost effective", "", "deleted", "model-v1", model.TierFree)
	snapshot, fingerprint, err := frozen.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	taskID := uuid.NewString()
	task := &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed,
		ExecutionProfile: frozen.ID, AgentProfileSnapshot: snapshot, AgentProfileFingerprint: fingerprint, ImageCapabilityKey: "standard",
	}
	freezeHandlerTaskImageCapability(t, task, "standard", handlerTestImageCapabilityRoute("image.standard", model.TierFree))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/resume", `{"prompt":"continue"}`)
	defer resp.Body.Close()
	var body Response
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != fiber.StatusUnprocessableEntity || body.Code != 46006 || body.Msg != "agent_provider_unavailable" {
		t.Fatalf("status=%d body=%#v", resp.StatusCode, body)
	}
}

func TestResumeTask_Returns503WhenFileStorageUnavailable(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: "resume-storage@example.com", Password: "hashed", InviteCode: "resume-storage"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.NewString()
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	logger := zerolog.New(io.Discard)
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	resp := postJSON(t, app, "/tasks/"+taskID+"/resume", `{
		"input_attachments":[{
			"type":"text","upload_id":"resume-storage-upload",
			"key":"uploads/pending/`+userID+`/resume-storage-upload/notes.md"
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var env Response
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if env.Msg != "补充文件存储暂不可用，请稍后重试" {
		t.Fatalf("message = %q", env.Msg)
	}
}

func TestResumeTask_RejectsEmptyInput(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      "resume-empty@example.com",
		Password:   "hashed",
		InviteCode: "resumeempty",
		Tier:       model.TierFree,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformArticle,
		Name:     "Article",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	taskID := uuid.New().String()
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID:        taskID,
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	workspaceRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspaceRoot, taskID), 0o755); err != nil {
		t.Fatalf("create workdir: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	taskSvc.SetNASResumeEnabled(true)
	h := NewTaskHandler(taskSvc, &logger)
	h.SetRepository(repo)

	app := fiber.New()
	app.Post("/tasks/:id/resume", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.Resume(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/tasks/"+taskID+"/resume", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestTaskResponsesIncludeTotalBillingAndChargeDetails(t *testing.T) {
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	taskID := uuid.NewString()
	chargeID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID: userID, Email: "task-billing@example.com", Password: "hashed", InviteCode: "taskbilling",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformArticle, Name: "Article", Status: model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{
		ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, Prompt: "详情固定价格展示",
		BillingQuoteID: "quote-v1", BillingCatalogID: "retail-v1", BillingSKUID: "task.article.standard.v1",
		BillingChargeID: &chargeID, BillingPriceCredits: 6000,
	}); err != nil {
		t.Fatalf("create task: %v", err)
	}
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
	attemptID := uuid.NewString()
	toolCallID := "image:content-test"
	operationChargeID := uuid.NewString()
	analysisToolCallID := "analysis:content-test"
	analysisChargeID := uuid.NewString()
	if err := repo.WithTx(ctx, func(tx repository.Repository) error {
		charges := []*model.BillingCharge{
			{
				ID: chargeID, UserID: userID, CatalogID: "retail-v1", SKUID: "task.article.standard.v1",
				ResourceType: "task", ResourceID: taskID, Kind: model.BillingChargeKindTask,
				Policy: "task_admission", Status: model.BillingChargeStatusPosted,
				PriceCredits: 6000, PaidCredits: 6000, TaskID: &taskID,
				IdempotencyScope: "task-charge", IdempotencyKey: taskID,
				RequestFingerprint: strings.Repeat("a", 64), CreatedAt: now,
			},
			{
				ID: operationChargeID, UserID: userID, CatalogID: "retail-v1", SKUID: "image.seedream.content.v1",
				ResourceType: "image", ResourceID: uuid.NewString(), Kind: model.BillingChargeKindOperation,
				Policy: "accepted_task_operation", Status: model.BillingChargeStatusPosted,
				PriceCredits: 500, PaidCredits: 500, OperationTaskID: &taskID, AttemptID: &attemptID, ToolCallID: &toolCallID,
				IdempotencyScope: "operation-charge", IdempotencyKey: operationChargeID,
				RequestFingerprint: strings.Repeat("b", 64), CreatedAt: now.Add(time.Minute),
			},
			{
				ID: analysisChargeID, UserID: userID, CatalogID: "retail-v1", SKUID: "analysis.content.v1",
				ResourceType: "analysis", ResourceID: uuid.NewString(), Kind: model.BillingChargeKindOperation,
				Policy: "accepted_task_operation", Status: model.BillingChargeStatusPosted,
				PriceCredits: 300, PaidCredits: 300, OperationTaskID: &taskID, AttemptID: &attemptID, ToolCallID: &analysisToolCallID,
				IdempotencyScope: "operation-charge", IdempotencyKey: analysisChargeID,
				RequestFingerprint: strings.Repeat("c", 64), CreatedAt: now.Add(2 * time.Minute),
			},
		}
		for _, charge := range charges {
			if err := tx.Billing().CreateCharge(ctx, charge, nil); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create billing charges: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	h := NewTaskHandler(newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil), &logger)
	h.SetRepository(repo)
	app := fiber.New()
	app.Get("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.List(c)
	})
	app.Get("/tasks/:id", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return h.GetByID(c)
	})

	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks/"+taskID, nil))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Data struct {
			ID                   string  `json:"id"`
			BillingCatalogID     string  `json:"billing_catalog_id"`
			BillingSKUID         string  `json:"billing_sku_id"`
			BillingChargeID      *string `json:"billing_charge_id"`
			BillingPriceCredits  int64   `json:"billing_price_credits"`
			BillingTotalCredits  int64   `json:"billing_total_credits"`
			BillingChargeDetails []struct {
				ID         string  `json:"id"`
				ChargeKind string  `json:"charge_kind"`
				SKUID      string  `json:"sku_id"`
				Credits    int64   `json:"credits"`
				ToolCallID *string `json:"tool_call_id"`
			} `json:"billing_charge_details"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.ID != taskID || body.Data.BillingCatalogID != "retail-v1" ||
		body.Data.BillingSKUID != "task.article.standard.v1" || body.Data.BillingChargeID == nil ||
		*body.Data.BillingChargeID != chargeID || body.Data.BillingPriceCredits != 6000 ||
		body.Data.BillingTotalCredits != 6800 || len(body.Data.BillingChargeDetails) != 3 ||
		body.Data.BillingChargeDetails[1].SKUID != "image.seedream.content.v1" ||
		body.Data.BillingChargeDetails[1].Credits != 500 || body.Data.BillingChargeDetails[1].ToolCallID == nil ||
		*body.Data.BillingChargeDetails[1].ToolCallID != toolCallID ||
		body.Data.BillingChargeDetails[2].SKUID != "analysis.content.v1" ||
		body.Data.BillingChargeDetails[2].Credits != 300 {
		t.Fatalf("task billing identity = %#v", body.Data)
	}

	listResp, err := app.Test(httptest.NewRequest(http.MethodGet, "/tasks?offset=0&limit=20", nil))
	if err != nil {
		t.Fatalf("list request failed: %v", err)
	}
	if listResp.StatusCode != fiber.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
	var listBody struct {
		Data struct {
			Items []struct {
				ID                   string `json:"id"`
				BillingTotalCredits  int64  `json:"billing_total_credits"`
				BillingChargeDetails []struct {
					Credits int64 `json:"credits"`
				} `json:"billing_charge_details"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listBody); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listBody.Data.Items) != 1 || listBody.Data.Items[0].ID != taskID ||
		listBody.Data.Items[0].BillingTotalCredits != 6800 || len(listBody.Data.Items[0].BillingChargeDetails) != 0 {
		t.Fatalf("task list billing = %#v", listBody.Data.Items)
	}
}
func setupSeednoteTaskCreateHandler(t *testing.T) (*fiber.App, repository.Repository, context.Context, string, string) {
	t.Helper()
	db := setupTaskHandlerTestDB(t)
	repo := repository.New(db)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "seedrefs",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := repo.Projects().Create(ctx, &model.Project{
		ID:       projectID,
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "Seednote references",
		Status:   model.ProjectStatusActive,
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	taskSvc := newHandlerTaskService(t, repo, noopTaskEnqueuer{}, nil, &logger, "", nil, nil)
	handler := NewTaskHandler(taskSvc, &logger)
	handler.SetRepository(repo)
	app := fiber.New()
	app.Post("/tasks", func(c fiber.Ctx) error {
		c.Locals("user_id", userID)
		return handler.Create(c)
	})
	return app, repo, ctx, userID, projectID
}

func TestCreateTaskAcceptsSeednoteInputAttachments(t *testing.T) {
	app, repo, ctx, userID, projectID := setupSeednoteTaskCreateHandler(t)
	asset := &model.Asset{
		ID: uuid.NewString(), UserID: userID, Purpose: service.DirectUploadPurposeTaskReference,
		StorageKey: "assets/users/" + userID + "/seednote/product.png", FileName: "product.png",
		ContentType: "image/png", Size: 10, ETag: "product-etag",
	}
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatalf("create task attachment asset: %v", err)
	}
	resp := postJSON(t, app, "/tasks", `{
		"execution_profile":"effective","project_id":"`+projectID+`",
		"prompt":"生成新品种草图文",
		"input_attachments":[{
			"asset_id":"`+asset.ID+`",
			"instruction":"  保持包装、Logo 和瓶盖颜色  "
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200: %s", resp.StatusCode, body)
	}
	data := decodeEnvelopeRawData(t, resp)
	var taskID string
	if err := json.Unmarshal(data["id"], &taskID); err != nil {
		t.Fatalf("decode task id: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, taskID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 {
		t.Fatalf("input attachments = %#v, want one", attachments)
	}
	if attachments[0].AssetID != asset.ID || attachments[0].URL != "" || attachments[0].Key != "" || attachments[0].FileName != "product.png" || attachments[0].Instruction != "保持包装、Logo 和瓶盖颜色" {
		t.Fatalf("stored attachment = %#v", attachments[0])
	}
}

func TestCreateTaskRejectsNonImageSeednoteAttachment(t *testing.T) {
	app, repo, ctx, userID, projectID := setupSeednoteTaskCreateHandler(t)
	resp := postJSON(t, app, "/tasks", `{
		"execution_profile":"effective","project_id":"`+projectID+`",
		"prompt":"生成新品种草图文",
		"input_attachments":[{
			"type":"video",
			"url":"/api/v1/files/uploads/references/demo.mp4",
			"file_name":"demo.mp4",
			"content_type":"video/mp4"
		}]
	}`)
	defer resp.Body.Close()
	if resp.StatusCode != fiber.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 400: %s", resp.StatusCode, body)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil {
		t.Fatalf("find tasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("tasks = %#v, want none", tasks)
	}
}
