package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/anbanai/anban-creator/server/storage"
)

type taskCreationRepositoryOverride struct {
	repository.Repository
	projects   repository.ProjectRepository
	topicPools repository.TopicPoolRepository
}

func (r *taskCreationRepositoryOverride) Projects() repository.ProjectRepository {
	if r.projects != nil {
		return r.projects
	}
	return r.Repository.Projects()
}

func (r *taskCreationRepositoryOverride) TopicPools() repository.TopicPoolRepository {
	if r.topicPools != nil {
		return r.topicPools
	}
	return r.Repository.TopicPools()
}

type countingTopicPoolRepository struct {
	repository.TopicPoolRepository
	claimWithTaskCalls int
}

func (r *countingTopicPoolRepository) ClaimWithTask(ctx context.Context, userID, projectID, taskID string) (*model.TopicPool, error) {
	r.claimWithTaskCalls++
	return r.TopicPoolRepository.ClaimWithTask(ctx, userID, projectID, taskID)
}

func setupTaskTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Plan{}, &model.Task{}, &model.TaskExecution{}, &model.User{},
		&model.LoginSession{}, &model.TaskFile{}, &model.Project{},
		&model.TaskFileObjectCleanup{},
		&model.BillingSettlementOutbox{},
		&model.TopicPool{},
		&model.IlinkBinding{}, &model.IlinkNotification{}, &model.UploadSession{}, &model.Asset{},
		&model.WechatPublication{}, &model.WechatPublicationBinding{},
		&model.WechatArticleTracking{}, &model.WechatMetricSnapshot{},
	); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}
	return db
}

func newTestTaskService(
	repo repository.Repository,
	enqueuer TaskEnqueuer,
	store storage.Provider,
	logger *zerolog.Logger,
	taskLogDir string,
	pubsub *RedisPubSub,
	publishingSvc *PublishingService,
) *TaskService {
	svc := NewTaskService(repo, enqueuer, store, logger, taskLogDir, pubsub, publishingSvc)
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		panic(err)
	}
	svc.SetAgentProfileRegistry(registry)
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(nil, &serverconfig.Config{
		ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
				"standard": testImageCapabilityRoute("image.standard"),
			},
		}},
	}))
	return svc
}

func testImageCapabilityRoute(sku string) serverconfig.ImageGenerationRouteConfig {
	return serverconfig.ImageGenerationRouteConfig{
		Provider: "openai-test", Model: "image-test", BaseURL: "https://images.invalid/v1", APIKey: "test-secret",
		Timeout: time.Minute, MinTier: string(model.TierFree), BillingSKU: sku, Enabled: true,
		GenerationFeatures: serverconfig.ImageGenerationFeatures{MaxBatch: 1, MaxReferenceImages: 4, SupportsReference: true},
	}
}

func freezeTestTaskImageCapability(t *testing.T, task *model.Task, key string, route serverconfig.ImageGenerationRouteConfig) {
	t.Helper()
	snapshot, err := imageCapabilitySnapshot(key, route)
	if err != nil {
		t.Fatalf("freeze test task image capability: %v", err)
	}
	task.ImageCapabilityKey = snapshot.Key
	task.SetImageCapabilitySnapshot(snapshot)
}

func setupTaskServiceWithEnqueuer(t *testing.T) (*TaskService, repository.Repository) {
	t.Helper()
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	svc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	return svc, repo
}

func TestTaskServiceCreateManualRejectsMissingExecutionProfile(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID: userID, ProjectID: projectID, Prompt: "topic", Quantity: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "execution_profile is required") {
		t.Fatalf("CreateManual error = %v, want execution_profile is required", err)
	}
}

func TestTaskServiceCreateManualRejectsAgentInputWhenPackHasNoSchema(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	_, err := svc.CreateManual(context.Background(), CreateManualParams{
		UserID: userID, ProjectID: projectID, Prompt: "topic", Quantity: 1,
		ExecutionProfile: "effective", AgentInput: map[string]any{"tone": "concise"},
	})
	if !errors.Is(err, ErrInvalidAgentInput) {
		t.Fatalf("CreateManual error = %v, want ErrInvalidAgentInput", err)
	}
}

func TestTaskServiceCreateManualViralAnalysisRequiresSeednoteProject(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	seednoteProjectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	params := CreateManualParams{
		UserID: userID, ProjectID: seednoteProjectID, RequestedTaskType: model.TaskTypeViralAnalysis,
		ExecutionProfile: "effective", Prompt: "https://example.com/note/1", Quantity: 1,
	}
	tasks, err := svc.CreateManual(ctx, params)
	if err != nil {
		t.Fatalf("CreateManual viral analysis: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Type != model.TaskTypeViralAnalysis {
		t.Fatalf("tasks = %#v, want one viral_analysis task", tasks)
	}

	articleProjectID := createTestProject(t, repo, userID, model.PlatformArticle)
	params.ProjectID = articleProjectID
	_, err = svc.CreateManual(ctx, params)
	if !errors.Is(err, ErrViralAnalysisRequiresSeednoteProject) {
		t.Fatalf("article viral analysis error = %v, want ErrViralAnalysisRequiresSeednoteProject", err)
	}
}

func injectTestAgentProfiles(t *testing.T, svc *TaskService) {
	t.Helper()
	registry, err := NewAgentProfileRegistry(testAgentProfiles())
	if err != nil {
		t.Fatalf("NewAgentProfileRegistry: %v", err)
	}
	injector, ok := any(svc).(interface {
		SetAgentProfileRegistry(*AgentProfileRegistry)
	})
	if !ok {
		t.Fatal("TaskService does not expose AgentProfileRegistry injection")
	}
	injector.SetAgentProfileRegistry(registry)
}

func freezeTestTaskProfile(t *testing.T, task *model.Task) {
	t.Helper()
	profile := testAgentProfiles()[0]
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatalf("freeze test task profile: %v", err)
	}
	task.ExecutionProfile = profile.ID
	task.AgentProfileSnapshot = snapshot
	task.AgentProfileFingerprint = fingerprint
	if task.Type != model.TaskTypeViralAnalysis && task.ImageCapabilityKey == "" {
		task.ImageCapabilityKey = "standard"
	}
}

func TestTaskServiceCreateManualValidatesTierAndFreezesProfileSnapshot(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	injectTestAgentProfiles(t, svc)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Tier: model.TierPro}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		UserID: userID, ProjectID: projectID, Prompt: "topic", Quantity: 1,
		ExecutionProfile: "balanced",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks = %d, want 1", len(tasks))
	}
	task := tasks[0]
	if task.ExecutionProfile != "balanced" || task.AgentProfileSnapshot.ProfileID != "balanced" || task.AgentProfileSnapshot.Envs[model.ClaudeEnvModel] != "doubao-seed-evolving" || len(task.AgentProfileFingerprint) != 64 {
		t.Fatalf("task profile = %q, snapshot = %#v", task.ExecutionProfile, task.AgentProfileSnapshot)
	}

	if _, err := svc.CreateManual(ctx, CreateManualParams{
		UserID: userID, ProjectID: projectID, Prompt: "denied", Quantity: 1,
		ExecutionProfile: "quality",
	}); !errors.Is(err, ErrAgentProfileAccessDenied) {
		t.Fatalf("enterprise profile error = %v, want ErrAgentProfileAccessDenied", err)
	}
}

func TestTaskServiceCreateFromPlanInheritsAndFreezesExecutionProfile(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	injectTestAgentProfiles(t, svc)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Tier: model.TierPro}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	plan := &model.Plan{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		ExecutionProfile: "balanced", Status: model.PlanStatusActive, Prompt: "scheduled topic", ImageRatio: "4:3",
	}
	plan.SetAgentInput(map[string]any{})

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	if task == nil || task.ExecutionProfile != "balanced" || task.AgentProfileSnapshot.ProfileID != "balanced" || task.AgentProfileSnapshot.Envs[model.ClaudeEnvModel] != "doubao-seed-evolving" || len(task.AgentProfileFingerprint) != 64 {
		t.Fatalf("plan task profile = %#v", task)
	}
	if task.ImageRatio != "4:3" {
		t.Fatalf("plan task image ratio = %q, want 4:3", task.ImageRatio)
	}
	if task.AgentInput.Data() == nil {
		t.Fatal("plan agent_input was not copied to the task")
	}
}

func TestTaskServiceCreateFromPlanRevalidatesCapabilityBeforeAdmission(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Tier: model.TierFree}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	cfg := &serverconfig.Config{ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
			"standard": testImageCapabilityRoute("image.standard"),
			"professional": func() serverconfig.ImageGenerationRouteConfig {
				route := testImageCapabilityRoute("image.professional")
				route.MinTier = string(model.TierEnterprise)
				route.GenerationFeatures.SizePresets = []string{"1:1"}
				return route
			}(),
		},
	}}}
	setter, ok := any(svc).(interface {
		SetImageCapabilityResolver(*ImageCapabilityResolver)
	})
	if !ok {
		t.Fatal("TaskService does not expose image capability revalidation")
	}
	setter.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, cfg))
	plan := &model.Plan{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		ExecutionProfile: "effective", Status: model.PlanStatusActive,
		ImageCapabilityKey: "professional", ImageRatio: "1:1",
	}

	if task, err := svc.CreateFromPlan(ctx, plan); err == nil || task != nil || !strings.Contains(err.Error(), "requires enterprise tier") {
		t.Fatalf("CreateFromPlan = task %#v, err %v; want current-tier rejection", task, err)
	}
	tasks, err := repo.Tasks().FindByUserID(ctx, userID, projectID, "", 0, 10)
	if err != nil || len(tasks) != 0 {
		t.Fatalf("persisted tasks = %#v, err=%v; want none", tasks, err)
	}
}

func TestTaskServiceCreateFromPlanUsesBusinessRatioIndependentOfGenerationSpecs(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Tier: model.TierEnterprise}); err != nil {
		t.Fatal(err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	cfg := &serverconfig.Config{ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
		DefaultCapability: "standard",
		Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
			"standard": testImageCapabilityRoute("image.standard"),
			"professional": func() serverconfig.ImageGenerationRouteConfig {
				route := testImageCapabilityRoute("image.professional")
				route.MinTier = string(model.TierEnterprise)
				route.GenerationFeatures.SizePresets = []string{"1:1"}
				return route
			}(),
		},
	}}}
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(repo, cfg))
	plan := &model.Plan{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		ExecutionProfile: "effective", Status: model.PlanStatusActive,
		ImageCapabilityKey: "professional", ImageRatio: "16:9",
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil || task == nil || task.ImageRatio != "16:9" {
		t.Fatalf("CreateFromPlan = task %#v, err %#v; want business ratio independent of generation specs", task, err)
	}
}

func TestTaskService_ResumeRequiresNASCapability(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	svc := newTestTaskService(repo, &mockEnqueuer{}, nil, &logger, "", nil, nil)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeUnavailable) {
		t.Fatalf("Resume error = %v, want ErrTaskResumeUnavailable", err)
	}
}

// mockEnqueuer captures enqueued tasks without executing them.
type mockEnqueuer struct {
	enqueued  []string
	uniqueIDs map[string]struct{}
}

type cancelingFailTaskEnqueuer struct {
	cancel context.CancelFunc
	err    error
}

func (e cancelingFailTaskEnqueuer) Enqueue(string, []byte) error {
	e.cancel()
	return e.err
}

func (e cancelingFailTaskEnqueuer) EnqueueIn(string, []byte, time.Duration) error {
	e.cancel()
	return e.err
}

func (e cancelingFailTaskEnqueuer) EnqueueUnique(string, []byte, string) (bool, error) {
	e.cancel()
	return false, e.err
}

func (m *mockEnqueuer) Enqueue(taskType string, payload []byte) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

func (m *mockEnqueuer) EnqueueIn(taskType string, payload []byte, delay time.Duration) error {
	m.enqueued = append(m.enqueued, taskType)
	return nil
}

func (m *mockEnqueuer) EnqueueUnique(taskType string, payload []byte, uniqueKey string) (bool, error) {
	if m.uniqueIDs == nil {
		m.uniqueIDs = make(map[string]struct{})
	}
	if _, exists := m.uniqueIDs[uniqueKey]; exists {
		return false, nil
	}
	m.uniqueIDs[uniqueKey] = struct{}{}
	m.enqueued = append(m.enqueued, taskType)
	return true, nil
}

type resumeTestStorage struct {
	files        map[string][]byte
	deleted      []string
	uploadCount  int
	failUploadAt int
}

func (s *resumeTestStorage) Name() string { return "resume-test" }

func (s *resumeTestStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	s.uploadCount++
	if s.failUploadAt > 0 && s.uploadCount == s.failUploadAt {
		return nil, errors.New("object storage unavailable")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if s.files == nil {
		s.files = map[string][]byte{}
	}
	s.files[key] = data
	return &storage.UploadResult{Key: key, URL: "https://storage.test/" + key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (s *resumeTestStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}

func (s *resumeTestStorage) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *resumeTestStorage) GetURL(key string) string { return "https://storage.test/" + key }

func (s *resumeTestStorage) Read(_ context.Context, key string) ([]byte, error) {
	data, ok := s.files[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (s *resumeTestStorage) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	delete(s.files, key)
	return nil
}

func (s *resumeTestStorage) DownloadURL(context.Context, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *resumeTestStorage) HasCustomDomain() bool  { return true }
func (s *resumeTestStorage) IsOwnedURL(string) bool { return true }

type concurrentResumeStorage struct {
	mu      sync.Mutex
	files   map[string][]byte
	deleted []string
	uploads int
	release chan struct{}
}

func newConcurrentResumeStorage() *concurrentResumeStorage {
	return &concurrentResumeStorage{files: map[string][]byte{}, release: make(chan struct{})}
}

func (s *concurrentResumeStorage) Name() string { return "concurrent-resume-test" }

func (s *concurrentResumeStorage) Upload(_ context.Context, key string, reader io.Reader, contentType string) (*storage.UploadResult, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.files[key] = data
	s.uploads++
	if s.uploads == 2 {
		close(s.release)
	}
	s.mu.Unlock()
	<-s.release
	return &storage.UploadResult{Key: key, URL: "https://storage.test/" + key, Size: int64(len(data)), MimeType: contentType}, nil
}

func (s *concurrentResumeStorage) UploadFile(context.Context, string, string, string) (*storage.UploadResult, error) {
	return nil, errors.New("not implemented")
}

func (s *concurrentResumeStorage) UploadURL(context.Context, string, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *concurrentResumeStorage) GetURL(key string) string { return "https://storage.test/" + key }
func (s *concurrentResumeStorage) Read(context.Context, string) ([]byte, error) {
	return nil, errors.New("not implemented")
}

func (s *concurrentResumeStorage) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, key)
	delete(s.files, key)
	return nil
}

func (s *concurrentResumeStorage) DownloadURL(context.Context, string, int) (string, error) {
	return "", errors.New("not implemented")
}

func (s *concurrentResumeStorage) HasCustomDomain() bool  { return true }
func (s *concurrentResumeStorage) IsOwnedURL(string) bool { return true }

type blockingTaskDeleteStorage struct {
	*resumeTestStorage
	deleteEntered chan struct{}
	allowDelete   chan struct{}
	deleteOnce    sync.Once
}

func newBlockingTaskDeleteStorage(key string) *blockingTaskDeleteStorage {
	return &blockingTaskDeleteStorage{
		resumeTestStorage: &resumeTestStorage{files: map[string][]byte{key: []byte("artifact")}},
		deleteEntered:     make(chan struct{}),
		allowDelete:       make(chan struct{}),
	}
}

func (s *blockingTaskDeleteStorage) Name() string { return "blocking-task-delete" }

func (s *blockingTaskDeleteStorage) Delete(ctx context.Context, key string) error {
	s.deleteOnce.Do(func() { close(s.deleteEntered) })
	select {
	case <-s.allowDelete:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.resumeTestStorage.Delete(ctx, key)
}

func TestTaskService_FinalizeTitleUpdatesCanonicalTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	title, err := svc.FinalizeTitle(ctx, userID, task.ID, "  月薪5000和月薪5万的人，喝茶差距在哪  ")
	if err != nil {
		t.Fatalf("FinalizeTitle: %v", err)
	}
	if title != "月薪5000和月薪5万的人，喝茶差距在哪" {
		t.Fatalf("title = %q", title)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Title != title {
		t.Fatalf("stored title = %q, want %q", found.Title, title)
	}
}

func TestTaskService_FinalizeTitleRejectsInvalidTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"empty", "   ", "title is required"},
		{"too long", strings.Repeat("长", 201), "title must be <= 200 characters"},
		{"artifact", "图片内容规划", "artifact title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.FinalizeTitle(ctx, userID, task.ID, tt.title)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("FinalizeTitle error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestTaskService_FinalizeTitleRejectsForeignTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	ownerID := uuid.New().String()
	otherID := uuid.New().String()
	projectID := createTestProject(t, repo, ownerID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    ownerID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, otherID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsUserKeyForUnownedTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    "",
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.FinalizeTitle(ctx, userID, task.ID, "真实标题")
	if err == nil || !strings.Contains(err.Error(), "task not found") {
		t.Fatalf("FinalizeTitle error = %v, want task not found", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateWithinProject(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	otherProjectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	existing := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
		Title:     "新手咖啡豆怎么选",
		CreatedAt: time.Now().Add(-1 * time.Hour),
	}
	current := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	foreignProject := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: otherProjectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
	}
	for _, task := range []*model.Task{existing, current, foreignProject} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, current.ID, " 新手 咖啡豆怎么选 ")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}

	if _, err := svc.FinalizeTitle(ctx, userID, foreignProject.ID, "新手咖啡豆怎么选"); err != nil {
		t.Fatalf("other project duplicate should be allowed: %v", err)
	}
}

func TestTaskService_FinalizeTitleRejectsDuplicateEvenWhenCurrentTaskAlreadyHasTitle(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        "task-current-with-same-title",
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusRunning,
			Title:     "新手咖啡豆怎么选",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	_, err := svc.FinalizeTitle(ctx, userID, "task-current-with-same-title", "新手 咖啡豆怎么选")
	if err == nil || !strings.Contains(err.Error(), "duplicate title") {
		t.Fatalf("FinalizeTitle error = %v, want duplicate title", err)
	}
}

func TestTaskService_CreateManualSnapshotsProjectConfig(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	ensureTestUser(t, repo, userID)
	asset := &model.Asset{ID: uuid.NewString(), UserID: userID, Purpose: DirectUploadPurposeProjectReference, StorageKey: "assets/users/" + userID + "/reference/ref.png", FileName: "ref.png", ContentType: "image/png", Size: 3, ETag: "etag"}
	if err := repo.Assets().Create(ctx, asset); err != nil {
		t.Fatalf("create reference asset: %v", err)
	}
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))
	project := &model.Project{
		ID:                    uuid.New().String(),
		UserID:                userID,
		Platform:              model.PlatformArticle,
		Name:                  "旧项目名",
		Status:                model.ProjectStatusActive,
		Instructions:          "旧定位",
		Keywords:              "旧关键词",
		VisualStyle:           "旧视觉",
		ReferenceImageAssetID: asset.ID,
		ImageRatio:            "4:3",
		Writer:                "dan-koe",
		Theme:                 "autumn-warm",
		Author:                "旧署名",
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: project.ID,
		Prompt:    "topic",
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("created tasks = %d, want 1", len(tasks))
	}

	project.Name = "新项目名"
	project.Instructions = "新定位"
	project.VisualStyle = "新视觉"
	project.Author = "新署名"
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	snap := found.ProjectSnapshot.Data()
	if snap.ProjectName != "旧项目名" || snap.Instructions != "旧定位" ||
		snap.VisualStyle != "旧视觉" || snap.Author != "旧署名" {
		t.Fatalf("snapshot = %+v, want original project values", snap)
	}
	if found.ImageRatio != "4:3" {
		t.Fatalf("task image ratio = %q, want inherited project ratio 4:3", found.ImageRatio)
	}
	if snap.ReferenceImageAssetID != asset.ID || snap.ImageRatio != "4:3" {
		t.Fatalf("snapshot image fields = %q/%q", snap.ReferenceImageAssetID, snap.ImageRatio)
	}
}

func TestTaskService_CreateFromPlanSnapshotsProjectWithoutPlanStyleOverrides(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	ensureTestUser(t, repo, userID)
	project := &model.Project{
		ID:           uuid.New().String(),
		UserID:       userID,
		Platform:     model.PlatformSeednote,
		Name:         "项目",
		Status:       model.ProjectStatusActive,
		Instructions: "项目定位",
		VisualStyle:  "项目视觉",
		Writer:       "project-writer",
		Theme:        "project-theme",
		Author:       "项目署名",
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}
	plan := &model.Plan{ExecutionProfile: "effective",
		ID:          uuid.New().String(),
		UserID:      userID,
		ProjectID:   project.ID,
		Type:        model.PlatformSeednote,
		Status:      model.PlanStatusActive,
		Prompt:      "topic",
		VisualStyle: "计划视觉不应进入任务",
		Writer:      "plan-writer",
		Theme:       "plan-theme",
		Author:      "计划署名",
	}

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	snap := found.ProjectSnapshot.Data()
	if snap.VisualStyle != "项目视觉" || snap.Writer != "project-writer" ||
		snap.Theme != "project-theme" || snap.Author != "项目署名" {
		t.Fatalf("snapshot = %+v, want project values", snap)
	}
	if overrides := found.Overrides.Data(); overrides != (model.StyleOverrides{}) {
		t.Fatalf("overrides = %+v, want empty", overrides)
	}
	if found.PlanID == nil || *found.PlanID != plan.ID {
		t.Fatalf("plan_id = %v, want %q", found.PlanID, plan.ID)
	}
}

func TestTaskService_ListFiltersByPlanID(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-plan-filter"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	project := &model.Project{
		ID:       uuid.New().String(),
		UserID:   userID,
		Platform: model.PlatformSeednote,
		Name:     "项目",
		Status:   model.ProjectStatusActive,
	}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatalf("create project: %v", err)
	}

	planA := &model.Plan{ExecutionProfile: "effective",
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		Prompt:    "计划 A",
	}
	planB := &model.Plan{ExecutionProfile: "effective",
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: project.ID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		Prompt:    "计划 B",
	}
	taskA, err := svc.CreateFromPlan(ctx, planA)
	if err != nil {
		t.Fatalf("CreateFromPlan A: %v", err)
	}
	if _, err := svc.CreateFromPlan(ctx, planB); err != nil {
		t.Fatalf("CreateFromPlan B: %v", err)
	}

	tasks, total, err := svc.List(ctx, userID, 0, 20, "", "", planA.ID)
	if err != nil {
		t.Fatalf("List by plan_id: %v", err)
	}
	if total != 1 || len(tasks) != 1 {
		t.Fatalf("filtered tasks len/total = %d/%d, want 1/1", len(tasks), total)
	}
	if tasks[0].ID != taskA.ID || tasks[0].PlanID == nil || *tasks[0].PlanID != planA.ID {
		t.Fatalf("filtered task = %+v, want task %s for plan %s", tasks[0], taskA.ID, planA.ID)
	}
}

func TestTaskService_CreateManualEcommerceTaskForcesSinglePackageWithoutCreditService(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, OpenID: "openid-ecommerce-no-credit"}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformEcommerce)

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "做一组咖啡杯电商图",
		Quantity:  5,
		Ecommerce: &model.EcommerceConfig{
			SelectedModules: map[string]int{"main_images": 5},
			ProductPhotos:   []string{"https://cdn.example.com/cup.png"},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("created tasks = %d, want 1 ecommerce package task", len(tasks))
	}
}

func TestTaskServiceCreateManualMontageStoresInputAndClampsQuantity(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	userID := "user-om"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:             userID,
		ProjectID:          projectID,
		Quantity:           3,
		ImageRatio:         "16:9",
		ImageCapabilityKey: "standard",
		MontageInput: &model.MontageInput{
			Brief:       "做一条新品发布短片",
			PipelineKey: "default",
			Preferences: model.MontagePreferences{
				DurationSeconds: 30,
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	if tasks[0].ImageRatio != "16:9" || tasks[0].ImageCapabilityKey != "standard" {
		t.Fatalf("Montage image settings = ratio %q, capability %q", tasks[0].ImageRatio, tasks[0].ImageCapabilityKey)
	}
	got := tasks[0].MontageInput.Data()
	if got.Brief != "做一条新品发布短片" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.DurationSeconds != 30 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestTaskServiceCreateManualRejectsMontageInputForOtherPlatforms(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := "user-om-reject"
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)

	_, err := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "春季穿搭",
		MontageInput: &model.MontageInput{
			Brief: "错误平台",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "montage_input can only be set on montage tasks") {
		t.Fatalf("CreateManual error = %v, want montage input rejection", err)
	}
}

func TestTaskServiceCreateFromPlanMontageCopiesInput(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	plan := &model.Plan{ExecutionProfile: "effective",
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformMontage,
		Status:             model.PlanStatusActive,
		Prompt:             "计划提示",
		ImageRatio:         "16:9",
		ImageCapabilityKey: "standard",
	}
	plan.SetMontageInput(model.MontageInput{
		Brief:       "从计划生成发布会短片",
		PipelineKey: "default",
		Preferences: model.MontagePreferences{
			DurationSeconds: 45,
		},
	})

	task, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	if task.ImageRatio != "16:9" || task.ImageCapabilityKey != "standard" {
		t.Fatalf("Montage image settings = ratio %q, capability %q", task.ImageRatio, task.ImageCapabilityKey)
	}
	got := task.MontageInput.Data()
	if got.Brief != "从计划生成发布会短片" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.DurationSeconds != 45 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestTaskService_ClearArtifactTitles(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	for _, task := range []*model.Task{
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "图片内容规划",
			CreatedAt: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:        uuid.New().String(),
			UserID:    userID,
			ProjectID: projectID,
			Type:      model.PlatformSeednote,
			Status:    model.TaskStatusCompleted,
			Title:     "真实茶饮标题",
			CreatedAt: time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
		},
	} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	count, err := svc.ClearArtifactTitles(ctx)
	if err != nil {
		t.Fatalf("ClearArtifactTitles: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
	titles, err := svc.ListTitles(ctx, projectID)
	if err != nil {
		t.Fatalf("ListTitles: %v", err)
	}
	if len(titles) != 1 || titles[0] != "真实茶饮标题" {
		t.Fatalf("titles = %v, want [真实茶饮标题]", titles)
	}
}

func TestTaskService_CloneClonesCompletedTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	src := &model.Task{ExecutionProfile: "effective",
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformArticle,
		Status:             model.TaskStatusCompleted,
		Prompt:             "finished topic",
		ImageRatio:         "16:9",
		ImageCapabilityKey: "standard",
	}
	src.SetInputAttachments([]model.EntryAttachment{
		{Role: "brief", Text: "original input", FileName: "brief.txt"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "continue old workspace", FileName: "latest.md"},
		{Role: model.EntryAttachmentRoleResumeFile, Key: "resume/feedback.pdf", FileName: "feedback.pdf"},
	})
	src.SetAgentInput(map[string]any{})
	freezeTestTaskImageCapability(t, src, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatalf("create source task: %v", err)
	}

	clones, err := svc.Clone(ctx, src.ID, CloneTaskParams{ExecutionProfile: src.ExecutionProfile})
	if err != nil {
		t.Fatalf("Clone completed task: %v", err)
	}
	if len(clones) != 1 {
		t.Fatalf("clones = %d, want 1", len(clones))
	}
	clone := clones[0]
	if clone.ID == src.ID {
		t.Fatal("clone reused the original task id")
	}
	if clone.Status != model.TaskStatusPending {
		t.Fatalf("clone status = %q, want %q", clone.Status, model.TaskStatusPending)
	}
	if clone.Prompt != src.Prompt || clone.ImageRatio != src.ImageRatio {
		t.Fatalf("clone config = prompt %q ratio %q, want source config", clone.Prompt, clone.ImageRatio)
	}
	if clone.ExecutionProfile != src.ExecutionProfile || clone.AgentProfileSnapshot.ProfileID != src.ExecutionProfile {
		t.Fatalf("clone profile = %q snapshot=%#v, want source profile %q", clone.ExecutionProfile, clone.AgentProfileSnapshot, src.ExecutionProfile)
	}
	if clone.InputSourceTaskID != src.ID {
		t.Fatalf("clone input source = %q, want %q", clone.InputSourceTaskID, src.ID)
	}
	if clone.InputSourceProjectID != src.ProjectID {
		t.Fatalf("clone input source project = %q, want %q", clone.InputSourceProjectID, src.ProjectID)
	}
	if clone.AgentInput.Data() == nil {
		t.Fatal("clone did not preserve source agent_input")
	}
	cloneAttachments := clone.InputAttachments.Data()
	if len(cloneAttachments) != 1 || cloneAttachments[0].Role != "brief" || cloneAttachments[0].Text != "original input" {
		t.Fatalf("clone attachments = %#v, want only original task inputs", cloneAttachments)
	}
	foundSrc, err := repo.Tasks().FindByID(ctx, src.ID)
	if err != nil {
		t.Fatalf("find source task: %v", err)
	}
	if foundSrc.Status != model.TaskStatusCompleted {
		t.Fatalf("source status = %q, want completed", foundSrc.Status)
	}
	if len(foundSrc.InputAttachments.Data()) != 3 {
		t.Fatalf("source resume inputs were mutated: %#v", foundSrc.InputAttachments.Data())
	}
}

func TestCloneConvertsDirectReferenceToPrependedAttachment(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeTaskReference)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ReferenceImageAssetID: asset.ID, ImageCapabilityKey: "standard",
	}
	source.SetInputAttachments([]model.EntryAttachment{
		{Type: "document", Text: "brief", FileName: "brief.md"},
		{Type: "text", Text: "notes", FileName: "notes.txt"},
	})
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	clone := clones[0]
	if clone.ReferenceImageAssetID != "" {
		t.Fatalf("clone reference asset = %q, want empty", clone.ReferenceImageAssetID)
	}
	attachments := clone.InputAttachments.Data()
	if len(attachments) != 3 {
		t.Fatalf("clone attachments = %#v, want reference plus two materials", attachments)
	}
	if attachments[0].AssetID != asset.ID || attachments[0].Type != "image" || attachments[0].FileName != asset.FileName || attachments[0].ContentType != asset.ContentType || attachments[0].Size != asset.Size {
		t.Fatalf("first attachment = %#v, want canonical asset material", attachments[0])
	}
	if attachments[1].FileName != "brief.md" || attachments[2].FileName != "notes.txt" {
		t.Fatalf("existing material order changed: %#v", attachments)
	}
}

func TestCloneDoesNotDuplicateDirectReferenceAttachment(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeTaskReference)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ReferenceImageAssetID: asset.ID, ImageCapabilityKey: "standard",
	}
	source.SetInputAttachments([]model.EntryAttachment{
		{AssetID: asset.ID, Type: "document", FileName: "forged.pdf", ContentType: "application/pdf", Size: 1},
		{Type: "text", Text: "notes", FileName: "notes.txt"},
	})
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	attachments := clones[0].InputAttachments.Data()
	if clones[0].ReferenceImageAssetID != "" || len(attachments) != 2 || attachments[0].AssetID != asset.ID || attachments[0].Type != "image" || attachments[0].FileName != asset.FileName || attachments[0].ContentType != asset.ContentType || attachments[0].Size != asset.Size || attachments[1].FileName != "notes.txt" {
		t.Fatalf("clone = reference %q attachments %#v", clones[0].ReferenceImageAssetID, attachments)
	}
}

func TestCloneKeepsProjectSnapshotReferenceOutOfAttachments(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ImageCapabilityKey: "standard"}
	source.SetProjectSnapshot(model.ProjectSnapshot{Platform: model.PlatformArticle, ReferenceImageAssetID: asset.ID})
	source.SetInputAttachments([]model.EntryAttachment{{Type: "text", Text: "notes", FileName: "notes.txt"}})
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	attachments := clones[0].InputAttachments.Data()
	if clones[0].ReferenceImageAssetID != "" || clones[0].ProjectSnapshot.Data().ReferenceImageAssetID != asset.ID || len(attachments) != 1 || attachments[0].FileName != "notes.txt" {
		t.Fatalf("clone = reference %q snapshot %#v attachments %#v", clones[0].ReferenceImageAssetID, clones[0].ProjectSnapshot.Data(), attachments)
	}
}

func TestCloneExcludesDirectReferenceDuplicatedFromProjectSnapshot(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeProjectReference)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ReferenceImageAssetID: asset.ID, ImageCapabilityKey: "standard",
	}
	source.SetProjectSnapshot(model.ProjectSnapshot{Platform: model.PlatformArticle, ReferenceImageAssetID: asset.ID})
	source.SetInputAttachments([]model.EntryAttachment{{Type: "text", Text: "notes", FileName: "notes.txt"}})
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	attachments := clones[0].InputAttachments.Data()
	if clones[0].ReferenceImageAssetID != "" || clones[0].ProjectSnapshot.Data().ReferenceImageAssetID != asset.ID || len(attachments) != 1 || attachments[0].FileName != "notes.txt" {
		t.Fatalf("clone = reference %q snapshot %#v attachments %#v", clones[0].ReferenceImageAssetID, clones[0].ProjectSnapshot.Data(), attachments)
	}
}

func TestCloneMovesDuplicateReferenceAttachmentToFront(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeTaskReference)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ReferenceImageAssetID: asset.ID, ImageCapabilityKey: "standard",
	}
	source.SetInputAttachments([]model.EntryAttachment{
		{Type: "document", Text: "brief", FileName: "brief.pdf"},
		{AssetID: asset.ID, Type: "document", FileName: "forged.pdf", ContentType: "application/pdf", Size: 1, Instruction: "use as style"},
		{Type: "text", Text: "notes", FileName: "notes.txt"},
	})
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	attachments := clones[0].InputAttachments.Data()
	if len(attachments) != 3 || attachments[0].AssetID != asset.ID || attachments[0].Type != "image" || attachments[0].FileName != asset.FileName || attachments[0].ContentType != asset.ContentType || attachments[0].Size != asset.Size || attachments[0].Instruction != "use as style" {
		t.Fatalf("first attachment = %#v; all attachments %#v", attachments[0], attachments)
	}
	if attachments[1].FileName != "brief.pdf" || attachments[2].FileName != "notes.txt" {
		t.Fatalf("other attachment order changed: %#v", attachments)
	}
}

func TestCloneRemovesAllDuplicateReferenceAttachmentsPreservingFirstInstruction(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeAIEntryAttachment)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ReferenceImageAssetID: asset.ID, ImageCapabilityKey: "standard"}
	source.SetInputAttachments([]model.EntryAttachment{
		{AssetID: asset.ID, Type: "document", Instruction: "first instruction", FileName: "forged-a.pdf"},
		{Type: "text", FileName: "brief.txt"},
		{AssetID: asset.ID, Type: "image", Instruction: "second instruction", FileName: "forged-b.png"},
		{Type: "document", FileName: "last.pdf"},
	})
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile})
	if err != nil {
		t.Fatal(err)
	}
	attachments := clones[0].InputAttachments.Data()
	if len(attachments) != 3 || attachments[0].AssetID != asset.ID || attachments[0].Instruction != "first instruction" || attachments[1].FileName != "brief.txt" || attachments[2].FileName != "last.pdf" {
		t.Fatalf("attachments = %#v", attachments)
	}
}

func TestCloneAcceptsHistoricalAIEntryReferenceAsset(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeAIEntryAttachment)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))
	source := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ReferenceImageAssetID: asset.ID, ImageCapabilityKey: "standard"}
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile})
	if err != nil || len(clones[0].InputAttachments.Data()) != 1 || clones[0].InputAttachments.Data()[0].AssetID != asset.ID {
		t.Fatalf("clone err=%v attachments=%#v", err, clones[0].InputAttachments.Data())
	}
}

func TestCloneEditableOverridesConvertReferenceToPrependedAttachment(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	asset := referenceAssetFixture(uuid.NewString(), userID, DirectUploadPurposeTaskReference)
	seedReferenceAsset(t, repo, asset)
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, ExecutionProfile: "effective", ReferenceImageAssetID: asset.ID,
	}
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{
		ExecutionProfile: source.ExecutionProfile,
		Overrides: &CloneTaskOverrides{
			ProjectID: projectID, Quantity: 1, ReferenceImageAssetID: asset.ID,
			InputAttachments: []model.EntryAttachment{
				{Type: "document", Text: "brief", FileName: "brief.pdf"},
				{Type: "text", Text: "notes", FileName: "notes.txt"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Clone editable task: %v", err)
	}
	clone := clones[0]
	attachments := clone.InputAttachments.Data()
	if clone.ReferenceImageAssetID != "" {
		t.Fatalf("clone reference asset = %q, want empty", clone.ReferenceImageAssetID)
	}
	if len(attachments) != 3 || attachments[0].AssetID != asset.ID || attachments[0].Type != "image" || attachments[1].FileName != "brief.pdf" || attachments[2].FileName != "notes.txt" {
		t.Fatalf("clone attachments = %#v", attachments)
	}
}

func TestTaskServiceCloneRefreezesCurrentProfileConfiguration(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	user, err := repo.Users().FindByID(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	user.Tier = model.TierEnterprise
	if err := repo.Users().Update(ctx, user); err != nil {
		t.Fatal(err)
	}

	current, err := svc.agentProfiles.Resolve("quality")
	if err != nil {
		t.Fatal(err)
	}
	historical := current.Snapshot()
	for _, key := range []string{
		model.ClaudeEnvModel, "ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_HAIKU_MODEL", "CLAUDE_CODE_SUBAGENT_MODEL",
	} {
		historical.Envs[key] = "kimi-k2.7-code"
	}
	historical.ModelUsageAliases = map[string]string{"kimi-k2.7-code": "kimi-k2.7-code"}
	historicalFingerprint, err := model.AgentProfileFingerprint(historical)
	if err != nil {
		t.Fatal(err)
	}
	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, Prompt: "historical", ExecutionProfile: current.ID,
		AgentProfileSnapshot: historical, AgentProfileFingerprint: historicalFingerprint, ImageCapabilityKey: "standard",
	}
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: current.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(clones) != 1 || clones[0].AgentProfileSnapshot.Envs[model.ClaudeEnvModel] != "kimi-k3[1m]" || clones[0].AgentProfileFingerprint == historicalFingerprint {
		t.Fatalf("clone did not freeze current profile: %#v", clones)
	}
}

func TestTaskService_CloneAppliesFullEditableOverrides(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetImageCapabilityResolver(NewImageCapabilityResolver(nil, &serverconfig.Config{
		ModelRoutes: serverconfig.ModelRoutesConfig{ImageGeneration: serverconfig.ImageGenerationRoutesConfig{
			DefaultCapability: "standard",
			Capabilities: map[string]serverconfig.ImageGenerationRouteConfig{
				"standard":   testImageCapabilityRoute("image.standard"),
				"gemini-pro": testImageCapabilityRoute("image.gemini-pro"),
			},
		}},
	}))
	ctx := context.Background()
	userID := uuid.NewString()
	sourceProjectID := createTestProject(t, repo, userID, model.PlatformArticle)
	destinationProject := &model.Project{
		ID:           uuid.NewString(),
		UserID:       userID,
		Platform:     model.PlatformSeednote,
		Name:         "Editable clone destination",
		Status:       model.ProjectStatusActive,
		Instructions: "current destination instructions",
		VisualStyle:  "current destination style",
	}
	if err := repo.Projects().Create(ctx, destinationProject); err != nil {
		t.Fatalf("create destination project: %v", err)
	}
	referenceAsset := &model.Asset{
		ID:          uuid.NewString(),
		UserID:      userID,
		Purpose:     DirectUploadPurposeTaskReference,
		StorageKey:  "assets/users/" + userID + "/reference/editable-clone.png",
		FileName:    "editable-clone.png",
		ContentType: "image/png",
		Size:        128,
		ETag:        "editable-clone-etag",
	}
	if err := repo.Assets().Create(ctx, referenceAsset); err != nil {
		t.Fatalf("create reference asset: %v", err)
	}
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{ExecutionProfile: "effective",
		ID:                   uuid.NewString(),
		UserID:               userID,
		ProjectID:            sourceProjectID,
		Type:                 model.PlatformArticle,
		Status:               model.TaskStatusCompleted,
		Prompt:               "source prompt",
		InputSourceTaskID:    "root-source-task",
		InputSourceProjectID: "root-source-project",
	}
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create source task: %v", err)
	}

	skipReference := true
	watermark := true
	hasContent := false
	hasTail := true
	articleCover := false
	articleContent := false
	attachments := []model.EntryAttachment{{Role: "brief", Text: "edited attachment", FileName: "brief.txt"}}
	tasks, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile, Overrides: &CloneTaskOverrides{
		ProjectID:                destinationProject.ID,
		Quantity:                 2,
		Prompt:                   "edited prompt",
		ImageRatio:               "1:1",
		ImageCapabilityKey:       "gemini-pro",
		SkipRefImage:             &skipReference,
		ReferenceImageAssetID:    referenceAsset.ID,
		InputAttachments:         attachments,
		Watermark:                &watermark,
		HasContentImage:          &hasContent,
		HasTailImage:             &hasTail,
		ArticleWithCover:         &articleCover,
		ArticleWithContentImages: &articleContent,
	}})
	if err != nil {
		t.Fatalf("Clone with editable overrides: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("cloned tasks = %d, want 2", len(tasks))
	}
	for _, task := range tasks {
		if task.ProjectID != destinationProject.ID || task.Type != model.PlatformSeednote {
			t.Fatalf("destination = project %q type %q, want %q/%q", task.ProjectID, task.Type, destinationProject.ID, model.PlatformSeednote)
		}
		snapshot := task.ProjectSnapshot.Data()
		if snapshot.ProjectName != destinationProject.Name || snapshot.Platform != destinationProject.Platform || snapshot.Instructions != destinationProject.Instructions || snapshot.VisualStyle != destinationProject.VisualStyle {
			t.Fatalf("destination snapshot = %#v", snapshot)
		}
		if task.Prompt != "edited prompt" || task.ImageRatio != "1:1" || task.ImageCapabilityKey != "gemini-pro" {
			t.Fatalf("editable fields = prompt %q ratio %q model %q", task.Prompt, task.ImageRatio, task.ImageCapabilityKey)
		}
		if !task.SkipReferenceImage || task.ReferenceImageAssetID != "" || !task.Watermark {
			t.Fatalf("reference/watermark fields = skip %v asset %q watermark %v", task.SkipReferenceImage, task.ReferenceImageAssetID, task.Watermark)
		}
		if task.HasContentImage || !task.HasTailImage {
			t.Fatalf("seednote fields = content %v tail %v", task.HasContentImage, task.HasTailImage)
		}
		if task.ArticleWithCover == nil || *task.ArticleWithCover || task.ArticleWithContentImages == nil || *task.ArticleWithContentImages {
			t.Fatalf("article fields = cover %v content %v", task.ArticleWithCover, task.ArticleWithContentImages)
		}
		if task.ExecutionTarget != model.ExecutionTargetCloud || task.LocalClaimDeadline != nil {
			t.Fatalf("execution target = %q deadline %v", task.ExecutionTarget, task.LocalClaimDeadline)
		}
		if got := task.InputAttachments.Data(); len(got) != 2 || got[0].AssetID != referenceAsset.ID || got[1] != attachments[0] {
			t.Fatalf("attachments = %#v, want reference then %#v", got, attachments)
		}
		if task.InputSourceTaskID != "root-source-task" || task.InputSourceProjectID != "root-source-project" {
			t.Fatalf("root provenance = %q/%q", task.InputSourceTaskID, task.InputSourceProjectID)
		}
		persisted, err := repo.Tasks().FindByID(ctx, task.ID)
		if err != nil {
			t.Fatalf("reload editable clone: %v", err)
		}
		if persisted.HasContentImage {
			t.Fatal("persisted has_content_image = true, want explicit false")
		}
	}
}

func TestTaskService_CloneOnlyReusesTrustedInheritedProjectReference(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	sourceProjectID := createTestProject(t, repo, userID, model.PlatformArticle)
	destinationProjectID := createTestProject(t, repo, userID, model.PlatformArticle)
	inherited := &model.Asset{
		ID:          uuid.NewString(),
		UserID:      userID,
		Purpose:     DirectUploadPurposeProjectReference,
		StorageKey:  "assets/users/" + userID + "/reference/inherited.png",
		FileName:    "inherited.png",
		ContentType: "image/png",
		Size:        128,
		ETag:        "inherited-etag",
	}
	unrelated := &model.Asset{
		ID:          uuid.NewString(),
		UserID:      userID,
		Purpose:     DirectUploadPurposeProjectReference,
		StorageKey:  "assets/users/" + userID + "/reference/unrelated.png",
		FileName:    "unrelated.png",
		ContentType: "image/png",
		Size:        128,
		ETag:        "unrelated-etag",
	}
	foreign := &model.Asset{
		ID:          uuid.NewString(),
		UserID:      uuid.NewString(),
		Purpose:     DirectUploadPurposeProjectReference,
		StorageKey:  "assets/users/foreign/reference/foreign.png",
		FileName:    "foreign.png",
		ContentType: "image/png",
		Size:        128,
		ETag:        "foreign-etag",
	}
	taskReference := &model.Asset{
		ID:          uuid.NewString(),
		UserID:      userID,
		Purpose:     DirectUploadPurposeTaskReference,
		StorageKey:  "assets/users/" + userID + "/reference/task.png",
		FileName:    "task.png",
		ContentType: "image/png",
		Size:        128,
		ETag:        "task-etag",
	}
	for _, asset := range []*model.Asset{inherited, unrelated, foreign, taskReference} {
		seedReferenceAsset(t, repo, asset)
	}
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))

	source := &model.Task{ExecutionProfile: "effective", ID: uuid.NewString(), UserID: userID, ProjectID: sourceProjectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}
	source.SetProjectSnapshot(model.ProjectSnapshot{ReferenceImageAssetID: inherited.ID})
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatalf("create source: %v", err)
	}
	taskCount := func() int64 {
		count, err := repo.Tasks().CountByUserID(ctx, userID, "", "")
		if err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return count
	}

	before := taskCount()
	if _, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID: userID, ProjectID: destinationProjectID, Quantity: 1, ReferenceImageAssetID: inherited.ID,
	}); !errors.Is(err, ErrReferenceAssetPurposeMismatch) {
		t.Fatalf("direct CreateManual error = %v, want ErrReferenceAssetPurposeMismatch", err)
	}
	if got := taskCount(); got != before {
		t.Fatalf("task count after rejected CreateManual = %d, want %d", got, before)
	}

	clone := func(assetID string) ([]*model.Task, error) {
		return svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile, Overrides: &CloneTaskOverrides{
			ProjectID: destinationProjectID, Quantity: 1, ReferenceImageAssetID: assetID,
		}})
	}
	clones, err := clone(inherited.ID)
	if err != nil || len(clones) != 1 || clones[0].ReferenceImageAssetID != "" || len(clones[0].InputAttachments.Data()) != 0 {
		t.Fatalf("trusted inherited clone = %#v, %v", clones, err)
	}
	firstClone := clones[0]
	firstClone.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(ctx, firstClone); err != nil {
		t.Fatalf("complete first clone: %v", err)
	}
	if exactClones, err := svc.Clone(ctx, firstClone.ID, CloneTaskParams{ExecutionProfile: firstClone.ExecutionProfile}); err != nil || len(exactClones) != 1 || exactClones[0].ReferenceImageAssetID != "" || len(exactClones[0].InputAttachments.Data()) != 0 {
		t.Fatalf("exact clone-of-clone = %#v, %v", exactClones, err)
	}

	for _, test := range []struct {
		name    string
		assetID string
		wantErr error
	}{
		{name: "unrelated same-user project reference", assetID: unrelated.ID, wantErr: ErrReferenceAssetPurposeMismatch},
		{name: "foreign project reference", assetID: foreign.ID, wantErr: ErrReferenceAssetForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := taskCount()
			if _, err := clone(test.assetID); !errors.Is(err, test.wantErr) {
				t.Fatalf("clone error = %v, want %v", err, test.wantErr)
			}
			if got := taskCount(); got != before {
				t.Fatalf("task count after rejected clone = %d, want %d", got, before)
			}
		})
	}

	normalClones, err := clone(taskReference.ID)
	if err != nil || len(normalClones) != 1 || normalClones[0].ReferenceImageAssetID != "" || len(normalClones[0].InputAttachments.Data()) != 1 || normalClones[0].InputAttachments.Data()[0].AssetID != taskReference.ID {
		t.Fatalf("normal task-reference clone = %#v, %v", normalClones, err)
	}
}

func TestTaskService_CloneAppliesTypeSpecificEditableOverrides(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		override func(projectID string) *CloneTaskOverrides
		assert   func(t *testing.T, task *model.Task)
	}{
		{
			name:     "ecommerce",
			platform: model.PlatformEcommerce,
			override: func(projectID string) *CloneTaskOverrides {
				return &CloneTaskOverrides{
					ProjectID: projectID,
					Quantity:  2,
					Ecommerce: &model.EcommerceConfig{
						SelectedModules: map[string]int{"main_images": 2},
						ProductPhotos:   []string{"https://example.com/product.png"},
						TargetPlatform:  "amazon",
						SellingPoints:   "durable",
						Language:        "en",
					},
				}
			},
			assert: func(t *testing.T, task *model.Task) {
				got := task.Ecommerce.Data()
				if got.SelectedModules["main_images"] != 2 || got.TargetPlatform != "amazon" || got.SellingPoints != "durable" || got.Language != "en" || len(got.ProductPhotos) != 1 {
					t.Fatalf("ecommerce = %#v", got)
				}
			},
		},
		{
			name:     "montage",
			platform: model.PlatformMontage,
			override: func(projectID string) *CloneTaskOverrides {
				return &CloneTaskOverrides{
					ProjectID: projectID,
					Quantity:  2,
					MontageInput: &model.MontageInput{
						Brief:       "edited montage brief",
						PipelineKey: "default",
						Preferences: model.MontagePreferences{DurationSeconds: 30},
					},
				}
			},
			assert: func(t *testing.T, task *model.Task) {
				got := task.MontageInput.Data()
				if got.Brief != "edited montage brief" || got.PipelineKey != "default" || got.Preferences.DurationSeconds != 30 {
					t.Fatalf("montage input = %#v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := setupTaskServiceWithEnqueuer(t)
			svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
			ctx := context.Background()
			userID := uuid.NewString()
			sourceProjectID := createTestProject(t, repo, userID, model.PlatformArticle)
			destinationProjectID := createTestProject(t, repo, userID, tt.platform)
			source := &model.Task{ExecutionProfile: "effective", ID: uuid.NewString(), UserID: userID, ProjectID: sourceProjectID, Type: model.PlatformArticle, Status: model.TaskStatusCompleted}
			if err := repo.Tasks().Create(ctx, source); err != nil {
				t.Fatalf("create source task: %v", err)
			}

			tasks, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: source.ExecutionProfile, Overrides: tt.override(destinationProjectID)})
			if err != nil {
				t.Fatalf("Clone: %v", err)
			}
			if len(tasks) != 1 {
				t.Fatalf("type-specific cloned tasks = %d, want platform-clamped 1", len(tasks))
			}
			if tasks[0].ProjectID != destinationProjectID || tasks[0].Type != tt.platform || tasks[0].ProjectSnapshot.Data().Platform != tt.platform {
				t.Fatalf("destination task = %#v", tasks[0])
			}
			if tasks[0].InputSourceTaskID != source.ID || tasks[0].InputSourceProjectID != sourceProjectID {
				t.Fatalf("source provenance = %q/%q", tasks[0].InputSourceTaskID, tasks[0].InputSourceProjectID)
			}
			tt.assert(t, tasks[0])
		})
	}
}

func TestTaskServiceClonePreservesRootInputSource(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	src := &model.Task{ExecutionProfile: "effective",
		ID:                   uuid.NewString(),
		UserID:               userID,
		ProjectID:            projectID,
		Type:                 model.PlatformArticle,
		Status:               model.TaskStatusCompleted,
		Prompt:               "clone lineage",
		ImageCapabilityKey:   "standard",
		InputSourceTaskID:    "root-task-id",
		InputSourceProjectID: "root-project-id",
	}
	freezeTestTaskImageCapability(t, src, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, src.ID, CloneTaskParams{ExecutionProfile: src.ExecutionProfile})
	if err != nil {
		t.Fatal(err)
	}
	if len(clones) != 1 {
		t.Fatalf("clones = %d, want 1", len(clones))
	}
	clone := clones[0]
	if clone.InputSourceTaskID != "root-task-id" {
		t.Fatalf("clone input source = %q, want root-task-id", clone.InputSourceTaskID)
	}
	if clone.InputSourceProjectID != "root-project-id" {
		t.Fatalf("clone input source project = %q, want root-project-id", clone.InputSourceProjectID)
	}
}

func TestTaskServiceCloneRepairsPartialInputSource(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	src := &model.Task{ExecutionProfile: "effective",
		ID:                   uuid.NewString(),
		UserID:               userID,
		ProjectID:            projectID,
		Type:                 model.PlatformArticle,
		Status:               model.TaskStatusCompleted,
		Prompt:               "partial clone lineage",
		ImageCapabilityKey:   "standard",
		InputSourceProjectID: "stale-project-id",
	}
	freezeTestTaskImageCapability(t, src, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, src.ID, CloneTaskParams{ExecutionProfile: src.ExecutionProfile})
	if err != nil {
		t.Fatal(err)
	}
	if len(clones) != 1 {
		t.Fatalf("clones = %d, want 1", len(clones))
	}
	clone := clones[0]
	if clone.InputSourceTaskID != src.ID {
		t.Fatalf("clone input source = %q, want %q", clone.InputSourceTaskID, src.ID)
	}
	if clone.InputSourceProjectID != src.ProjectID {
		t.Fatalf("clone input source project = %q, want %q", clone.InputSourceProjectID, src.ProjectID)
	}
}

func TestTaskServiceClonePreservesMontageInput(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	src := &model.Task{ExecutionProfile: "effective",
		ID:                 uuid.New().String(),
		UserID:             userID,
		ProjectID:          projectID,
		Type:               model.PlatformMontage,
		Status:             model.TaskStatusCompleted,
		Prompt:             "source prompt",
		ImageCapabilityKey: "standard",
	}
	src.SetMontageInput(model.MontageInput{
		Brief:       "保留克隆输入",
		PipelineKey: "default",
		Preferences: model.MontagePreferences{
			DurationSeconds: 20,
		},
	})
	freezeTestTaskImageCapability(t, src, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, src); err != nil {
		t.Fatalf("create source task: %v", err)
	}

	clones, err := svc.Clone(ctx, src.ID, CloneTaskParams{ExecutionProfile: src.ExecutionProfile})
	if err != nil {
		t.Fatalf("Clone montage task: %v", err)
	}
	if len(clones) != 1 {
		t.Fatalf("clones = %d, want 1", len(clones))
	}
	clone := clones[0]
	got := clone.MontageInput.Data()
	if got.Brief != "保留克隆输入" || got.PipelineKey != "default" {
		t.Fatalf("montage input = %#v", got)
	}
	if got.Preferences.DurationSeconds != 20 {
		t.Fatalf("preferences = %#v", got.Preferences)
	}
}

func TestTaskService_ResumeReusesTaskAndPersistsPromptAndFiles(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	store := &resumeTestStorage{files: map[string][]byte{}}
	svc := newTestTaskService(repo, enqueuer, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	completedAt := time.Now().Add(-time.Minute)
	resultJSON := `{"success":true}`
	workflowStatus := `{"current_stage":"done"}`
	task := &model.Task{
		ID:             uuid.New().String(),
		UserID:         userID,
		ProjectID:      projectID,
		Type:           model.PlatformArticle,
		Status:         model.TaskStatusFailed,
		Prompt:         "finished topic",
		Progress:       100,
		Result:         &resultJSON,
		ErrorMessage:   "old error",
		CompletedAt:    &completedAt,
		WorkflowStatus: &workflowStatus,
	}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "请基于现有草稿补充案例",
		Files: []ResumeTaskFile{
			{
				OriginalName: "客户 反馈.txt",
				Label:        "客户反馈",
				Reader:       strings.NewReader("feedback"),
				Size:         int64(len("feedback")),
			},
			{
				OriginalName: "客户 反馈.txt",
				Label:        "第二份反馈",
				Reader:       strings.NewReader("feedback-2"),
				Size:         int64(len("feedback-2")),
			},
		},
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.ID != task.ID {
		t.Fatalf("resumed id = %q, want original %q", resumed.ID, task.ID)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", resumed.Status)
	}
	if resumed.CompletedAt != nil || resumed.Result != nil || resumed.ErrorMessage != "" || resumed.Progress != 0 || resumed.WorkflowStatus != nil {
		t.Fatalf("resume did not reset execution state: %+v", resumed)
	}
	if len(enqueuer.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(enqueuer.enqueued))
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	var latestText string
	var resumeFiles []model.EntryAttachment
	for _, attachment := range found.InputAttachments.Data() {
		switch attachment.Role {
		case model.EntryAttachmentRoleResumeLatest:
			latestText = attachment.Text
		case model.EntryAttachmentRoleResumeFile:
			resumeFiles = append(resumeFiles, attachment)
		}
	}
	for _, want := range []string{"请基于现有草稿补充案例", "客户反馈", "客户 反馈.txt", "attachments/客户_反馈.txt", "attachments/客户_反馈_2.txt"} {
		if !strings.Contains(latestText, want) {
			t.Fatalf("resume latest missing %q:\n%s", want, latestText)
		}
	}
	if len(resumeFiles) != 2 || resumeFiles[0].FileName != "客户_反馈.txt" || resumeFiles[1].FileName != "客户_反馈_2.txt" || string(store.files[resumeFiles[0].Key]) != "feedback" || string(store.files[resumeFiles[1].Key]) != "feedback-2" {
		t.Fatalf("resume files = %#v", resumeFiles)
	}
}

func TestTaskServiceResumeRejectsCompletedTaskWithoutMutation(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	enqueuer := &mockEnqueuer{}
	store := &resumeTestStorage{files: map[string][]byte{}}
	svc := newTestTaskService(repo, enqueuer, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	completedAt := time.Now().Add(-time.Minute)
	result := `{"success":true}`
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusCompleted, Progress: 100, Result: &result, CompletedAt: &completedAt,
	}
	freezeTestTaskProfile(t, task)
	task.SetInputAttachments([]model.EntryAttachment{{Role: "brief", Text: "keep"}})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "change completed delivery",
		Files:  []ResumeTaskFile{{OriginalName: "new.txt", Reader: strings.NewReader("new")}},
	})
	if resumed != nil || !errors.Is(err, ErrTaskResumeCompleted) {
		t.Fatalf("Resume completed task = %#v, %v; want nil/ErrTaskResumeCompleted", resumed, err)
	}
	if len(enqueuer.enqueued) != 0 || len(store.files) != 0 {
		t.Fatalf("completed resume side effects: enqueued=%v files=%v", enqueuer.enqueued, store.files)
	}
	found, findErr := repo.Tasks().FindByID(ctx, task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.Status != model.TaskStatusCompleted || found.Progress != 100 || found.Result == nil || *found.Result != result || found.CompletedAt == nil || len(found.InputAttachments.Data()) != 1 || found.InputAttachments.Data()[0].Text != "keep" {
		t.Fatalf("completed task mutated: %#v", found)
	}
}

func TestTaskServiceResumeRejectsUnfinishedCurrentFinalization(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	executionID := uuid.NewString()
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusFailed, CurrentExecutionID: &executionID,
	}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionFailed,
		FinalizationStatus: model.TaskExecutionFinalizationTask,
	}); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeConflict) || resumed != nil {
		t.Fatalf("Resume unfinished finalization = task %v err %v, want nil/ErrTaskResumeConflict", resumed, err)
	}
	found, findErr := repo.Tasks().FindByID(ctx, task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.Status != model.TaskStatusFailed || found.CurrentExecutionID == nil || *found.CurrentExecutionID != executionID {
		t.Fatalf("conflicting resume mutated task: status=%q current=%v", found.Status, found.CurrentExecutionID)
	}
}

func TestTaskServiceResumeRejectsDeletedFrozenProviderBeforeMutation(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	executionID := uuid.NewString()
	profile := testAgentProfiles()[0]
	snapshot, fingerprint, err := profile.Freeze()
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusFailed, CurrentExecutionID: &executionID, ExecutionProfile: profile.ID,
		AgentProfileSnapshot: snapshot, AgentProfileFingerprint: fingerprint, ImageCapabilityKey: "standard",
	}
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionFailed,
		FinalizationStatus: model.TaskExecutionFinalizationDone,
	}); err != nil {
		t.Fatal(err)
	}
	unavailableEnvs := model.CloneClaudeProfileEnvs(profile.Envs)
	delete(unavailableEnvs, model.ClaudeEnvAuthToken)
	registry, err := NewAgentProfileRegistry([]AgentExecutionProfile{{
		ID: profile.ID, DisplayName: profile.DisplayName, Provider: profile.Provider, Protocol: profile.Protocol,
		Envs: unavailableEnvs, ModelUsageAliases: profile.ModelUsageAliases, MinTier: profile.MinTier, Available: false,
		UnavailableReason: "agent_provider_unavailable",
	}})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetAgentProfileRegistry(registry)

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "continue"})
	if resumed != nil || !errors.Is(err, ErrAgentProviderUnavailable) {
		t.Fatalf("Resume = %#v, %v; want nil/ErrAgentProviderUnavailable", resumed, err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Status != model.TaskStatusFailed || found.CurrentExecutionID == nil || *found.CurrentExecutionID != executionID {
		t.Fatalf("task state mutated: status=%q current_execution_id=%v", found.Status, found.CurrentExecutionID)
	}
	if len(found.InputAttachments.Data()) != 0 {
		t.Fatalf("resume artifacts persisted: %#v", found.InputAttachments.Data())
	}
	current, err := repo.TaskExecutions().FindByID(ctx, executionID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != model.TaskExecutionFailed || current.FinalizationStatus != model.TaskExecutionFinalizationDone {
		t.Fatalf("current execution mutated: %#v", current)
	}
}

func TestTaskServiceResumeClearsPreviousTerminalEvidence(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	executionID := uuid.NewString()
	resultJSON := `{"success":false,"cost_status":"reconciled"}`
	task := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle,
		Status: model.TaskStatusFailed, CurrentExecutionID: &executionID, Result: &resultJSON,
		TerminalModelUsage: datatypes.NewJSONType([]model.ModelTokenUsage{{Provider: "provider", Model: "old", InputTokens: 9}}),
		CostStatus:         agent.CostStatusReconciled,
	}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionFailed,
		FinalizationStatus: model.TaskExecutionFinalizationDone,
	}); err != nil {
		t.Fatal(err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Result != nil || len(resumed.TerminalModelUsage.Data()) != 0 || resumed.CostStatus != "" {
		t.Fatalf("returned resumed task exposes old evidence: result=%v usage=%+v cost=%q", resumed.Result, resumed.TerminalModelUsage.Data(), resumed.CostStatus)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Result != nil || len(found.TerminalModelUsage.Data()) != 0 || found.CostStatus != "" {
		t.Fatalf("persisted resumed task exposes old evidence: result=%v usage=%+v cost=%q", found.Result, found.TerminalModelUsage.Data(), found.CostStatus)
	}
}

func TestTaskService_ResumePersistsFilesWithoutResultOrLocalWorkspace(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard).With().Timestamp().Logger()
	enqueuer := &mockEnqueuer{}
	store := &fakeTaskStorage{files: map[string][]byte{}}
	svc := newTestTaskService(repo, enqueuer, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
		Prompt:    "finished remotely",
	}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	task.SetInputAttachments([]model.EntryAttachment{
		{Role: "brief", Text: "keep me", FileName: "brief.txt"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "old resume", FileName: "latest.md"},
	})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "继续补充 ACK 方案",
		Files: []ResumeTaskFile{{
			OriginalName: "补充 材料.txt",
			Label:        "补充材料",
			Reader:       strings.NewReader("remote resume file"),
			Size:         int64(len("remote resume file")),
		}},
	})
	if err != nil {
		t.Fatalf("Resume remote: %v", err)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", resumed.Status)
	}
	if len(enqueuer.enqueued) != 1 {
		t.Fatalf("enqueued = %d, want 1", len(enqueuer.enqueued))
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find resumed: %v", err)
	}
	attachments := found.InputAttachments.Data()
	var latest, resumeFile, preserved bool
	for _, attachment := range attachments {
		switch attachment.Role {
		case model.EntryAttachmentRoleResumeLatest:
			latest = strings.Contains(attachment.Text, "继续补充 ACK 方案") &&
				strings.Contains(attachment.Text, "attachments/补充_材料.txt")
		case model.EntryAttachmentRoleResumeFile:
			resumeFile = attachment.Key != "" && string(store.files[attachment.Key]) == "remote resume file"
		case "brief":
			preserved = attachment.Text == "keep me"
		}
		if attachment.Role == model.EntryAttachmentRoleResumeLatest && attachment.Text == "old resume" {
			t.Fatal("old resume attachment was not replaced")
		}
	}
	if !latest || !resumeFile || !preserved {
		t.Fatalf("attachments latest=%v resumeFile=%v preserved=%v: %#v", latest, resumeFile, preserved, attachments)
	}
}

func TestTaskService_ResumeStorageFailureCleansPartialUploads(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &resumeTestStorage{files: map[string][]byte{}, failUploadAt: 2}
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
	}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
		Prompt: "继续",
		Files: []ResumeTaskFile{
			{OriginalName: "first.md", Reader: strings.NewReader("first")},
			{OriginalName: "second.md", Reader: strings.NewReader("second")},
		},
	})
	if !errors.Is(err, ErrTaskResumeStorageUnavailable) {
		t.Fatalf("Resume error = %v, want ErrTaskResumeStorageUnavailable", err)
	}
	if len(store.deleted) != 2 || len(store.files) != 0 {
		t.Fatalf("deleted = %v files = %v, want completed and failing upload keys cleaned", store.deleted, store.files)
	}
	if !strings.HasSuffix(store.deleted[0], "/first.md") || !strings.HasSuffix(store.deleted[1], "/second.md") {
		t.Fatalf("deleted keys = %v, want first.md and second.md", store.deleted)
	}
	found, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if found.Status != model.TaskStatusFailed || len(found.InputAttachments.Data()) != 0 {
		t.Fatalf("task changed after upload failure: status=%q attachments=%#v", found.Status, found.InputAttachments.Data())
	}
}

func TestTaskService_ResumeRejectsNonPortableFilenameAndCleansPartialUploads(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &resumeTestStorage{files: map[string][]byte{}}
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续", Files: []ResumeTaskFile{
		{OriginalName: "first.md", Reader: strings.NewReader("first")},
		{OriginalName: "CON.txt", Reader: strings.NewReader("reserved")},
	}})
	if err == nil {
		t.Fatal("Windows reserved resume filename accepted")
	}
	if len(store.files) != 0 || len(store.deleted) != 1 || !strings.HasSuffix(store.deleted[0], "/first.md") {
		t.Fatalf("partial portable-name failure cleanup files=%v deleted=%v", store.files, store.deleted)
	}
	found, findErr := repo.Tasks().FindByID(ctx, task.ID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	if found.Status != model.TaskStatusFailed || len(found.InputAttachments.Data()) != 0 {
		t.Fatalf("task changed after portable-name failure: status=%q attachments=%#v", found.Status, found.InputAttachments.Data())
	}
}

func TestTaskService_ResumeDeletesSupersededResumeFilesAfterCAS(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := &resumeTestStorage{files: map[string][]byte{"resume/old.txt": []byte("old")}}
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	task.SetInputAttachments([]model.EntryAttachment{
		{Role: "brief", Text: "keep"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "old latest"},
		{Role: model.EntryAttachmentRoleResumeFile, Key: "resume/old.txt", FileName: "old.txt"},
	})
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	if _, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "new prompt"}); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != "resume/old.txt" {
		t.Fatalf("deleted = %v, want superseded resume file", store.deleted)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 2 || attachments[0].Role != "brief" || attachments[1].Role != model.EntryAttachmentRoleResumeLatest {
		t.Fatalf("attachments = %#v, want brief plus new latest", attachments)
	}
}

func TestTaskService_DeletePreventsConcurrentResumeFromRestoringAuthority(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	const objectKey = "tasks/delete-race/final.md"
	store := newBlockingTaskDeleteStorage(objectKey)
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: uuid.NewString(), TaskID: task.ID, FileName: "final.md", FilePath: "output/final.md",
		OSSKey: objectKey, CleanupOSSKey: objectKey, StorageProvider: store.Name(),
	}); err != nil {
		t.Fatalf("create task file: %v", err)
	}

	deleteErr := make(chan error, 1)
	go func() { deleteErr <- svc.Delete(ctx, task.ID) }()
	select {
	case <-store.deleteEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("Delete did not reach object cleanup")
	}

	_, resumeErr := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	close(store.allowDelete)
	if err := <-deleteErr; err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !errors.Is(resumeErr, ErrTaskResumeNotTerminal) {
		t.Fatalf("concurrent Resume error = %v, want deleting task rejection", resumeErr)
	}
	if _, err := repo.Tasks().FindByID(ctx, task.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("task lookup after delete = %v, want record not found", err)
	}
}

func TestTaskService_ResumeEnqueueFailureReturnsTaskToFailed(t *testing.T) {
	db := setupTaskTestDB(t)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	ctx, cancel := context.WithCancel(context.Background())
	svc := newTestTaskService(repo, cancelingFailTaskEnqueuer{cancel: cancel, err: errors.New("redis unavailable")}, nil, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if err == nil || !strings.Contains(err.Error(), "enqueue resumed task") {
		t.Fatalf("Resume error = %v, want enqueue failure", err)
	}
	found, findErr := repo.Tasks().FindByID(context.Background(), task.ID)
	if findErr != nil {
		t.Fatalf("find task: %v", findErr)
	}
	if found.Status != model.TaskStatusFailed || found.CompletedAt == nil {
		t.Fatalf("status=%q completed_at=%v, want retryable failed terminal task", found.Status, found.CompletedAt)
	}
	if !strings.Contains(found.ErrorMessage, "redis unavailable") {
		t.Fatalf("error_message = %q", found.ErrorMessage)
	}
}

func TestTaskService_ConcurrentResumeKeepsOnlyWinningUpload(t *testing.T) {
	db := setupTaskTestDB(t)
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql db: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	repo := repository.New(db)
	logger := zerolog.New(io.Discard)
	store := newConcurrentResumeStorage()
	svc := newTestTaskService(repo, &mockEnqueuer{}, store, &logger, "", nil, nil)
	svc.SetNASResumeEnabled(true)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, Status: model.TaskStatusFailed}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	errs := make(chan error, 2)
	for _, prompt := range []string{"first", "second"} {
		prompt := prompt
		go func() {
			_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{
				Prompt: prompt,
				Files:  []ResumeTaskFile{{OriginalName: prompt + ".md", Reader: strings.NewReader(prompt)}},
			})
			errs <- err
		}()
	}
	var succeeded, conflicted int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrTaskResumeConflict):
			conflicted++
		default:
			t.Fatalf("unexpected Resume error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("succeeded=%d conflicted=%d, want one each", succeeded, conflicted)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.files) != 1 || len(store.deleted) != 1 {
		t.Fatalf("files=%v deleted=%v, want only winning upload", store.files, store.deleted)
	}
}

func TestTaskServiceLegacyProjectConcurrencyCapOverridesProjectLimit(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetProjectConcurrencyCap(1)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatalf("find project: %v", err)
	}
	project.MaxConcurrentTasks = 10
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatalf("update project: %v", err)
	}
	running := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusRunning,
		Prompt:    "already running",
	}
	if err := repo.Tasks().Create(ctx, running); err != nil {
		t.Fatalf("create running task: %v", err)
	}
	pending := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusPending,
		Prompt:    "next task",
	}
	if err := repo.Tasks().Create(ctx, pending); err != nil {
		t.Fatalf("create pending task: %v", err)
	}

	if err := svc.EnqueueExecution(ctx, pending, project); err != nil {
		t.Fatalf("EnqueueExecution: %v", err)
	}
	if got := len(svc.enqueuer.(*mockEnqueuer).enqueued); got != 0 {
		t.Fatalf("enqueued = %d, want 0 while legacy Kubernetes cap is 1", got)
	}
}

func TestTaskServiceRuntimeDispatcherHonorsConfiguredProjectConcurrencyCap(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetProjectConcurrencyCap(1)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.MaxConcurrentTasks = 10
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	var pending *model.Task
	for _, status := range []string{model.TaskStatusRunning, model.TaskStatusPending} {
		created := &model.Task{
			ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
			Type: model.PlatformArticle, Status: status,
		}
		if err := repo.Tasks().Create(ctx, created); err != nil {
			t.Fatal(err)
		}
		if status == model.TaskStatusPending {
			pending = created
		}
	}
	if err := svc.EnqueueExecution(ctx, pending, project); err != nil {
		t.Fatal(err)
	}
	if got := len(svc.enqueuer.(*mockEnqueuer).enqueued); got != 0 {
		t.Fatalf("enqueued = %d, want 0 while configured cap is reached", got)
	}
}

func TestTaskServiceProjectConcurrencyModes(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	project := &model.Project{MaxConcurrentTasks: 8}
	if got := svc.effectiveProjectMaxConcurrent(project); got != 8 {
		t.Fatalf("local/Docker configured limit = %d, want 8", got)
	}
	svc.SetProjectConcurrencyCap(1)
	if got := svc.effectiveProjectMaxConcurrent(project); got != 1 {
		t.Fatalf("configured runtime cap = %d, want 1", got)
	}
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	if got := svc.effectiveProjectMaxConcurrent(project); got != 1 {
		t.Fatalf("dispatcher configured cap = %d, want 1", got)
	}
	uncapped, _ := setupTaskServiceWithEnqueuer(t)
	uncapped.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	if got := uncapped.effectiveProjectMaxConcurrent(project); got != 8 {
		t.Fatalf("uncapped dispatcher limit = %d, want project limit 8", got)
	}
}

func TestTaskService_ResumeRejectsNoInputAndNonTerminal(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	_, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{})
	if !errors.Is(err, ErrTaskResumeNoInput) {
		t.Fatalf("Resume empty input error = %v, want ErrTaskResumeNoInput", err)
	}

	task.Status = model.TaskStatusRunning
	if err := repo.Tasks().Update(ctx, task); err != nil {
		t.Fatalf("update task: %v", err)
	}
	_, err = svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续"})
	if !errors.Is(err, ErrTaskResumeNotTerminal) {
		t.Fatalf("Resume running task error = %v, want ErrTaskResumeNotTerminal", err)
	}
}

func TestTaskRepository_ResetRetryableTaskForResumeOnlyOneStatusSwap(t *testing.T) {
	_, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:               uuid.New().String(),
		UserID:           userID,
		ProjectID:        projectID,
		Type:             model.PlatformArticle,
		Status:           model.TaskStatusFailed,
		Progress:         88,
		ProgressSequence: 10,
		LatestProgress: datatypes.NewJSONType(
			model.ProgressPayload{Stage: "delivery", State: "complete", Percent: 88},
		),
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	first, err := repo.Tasks().ResetRetryableTaskForResume(ctx, task.ID, nil)
	if err != nil {
		t.Fatalf("first ResetRetryableTaskForResume: %v", err)
	}
	if !first {
		t.Fatal("first ResetRetryableTaskForResume = false, want true")
	}
	second, err := repo.Tasks().ResetRetryableTaskForResume(ctx, task.ID, nil)
	if err != nil {
		t.Fatalf("second ResetRetryableTaskForResume: %v", err)
	}
	if second {
		t.Fatal("second ResetRetryableTaskForResume = true, want false after status changed")
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", found.Status)
	}
	if found.Progress != 0 || found.ProgressSequence != 0 || found.LatestProgress.Data().Stage != "" {
		t.Fatalf("progress reset = percent:%d sequence:%d latest:%#v", found.Progress, found.ProgressSequence, found.LatestProgress.Data())
	}
}

func TestTaskService_ResumePersistsPromptWithoutResultOrLocalWorkspace(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusFailed,
	}
	freezeTestTaskProfile(t, task)
	freezeTestTaskImageCapability(t, task, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	resumed, err := svc.Resume(ctx, userID, task.ID, ResumeTaskParams{Prompt: "继续完成原任务"})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if resumed.Status != model.TaskStatusPending {
		t.Fatalf("status = %q, want pending", resumed.Status)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find resumed task: %v", err)
	}
	attachments := found.InputAttachments.Data()
	if len(attachments) != 1 || attachments[0].Role != model.EntryAttachmentRoleResumeLatest {
		t.Fatalf("input attachments = %#v, want one resume latest attachment", attachments)
	}
	if !strings.Contains(attachments[0].Text, "继续完成原任务") {
		t.Fatalf("resume latest = %q, want prompt", attachments[0].Text)
	}
}

func TestTaskService_CreateManual(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	tasks, err := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Test topic",
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	task := tasks[0]
	if task.ID == "" {
		t.Error("task.ID should not be empty")
	}
	if task.UserID != userID {
		t.Errorf("UserID = %q, want %q", task.UserID, userID)
	}
	if task.ProjectID != projectID {
		t.Errorf("ProjectID = %q, want %q", task.ProjectID, projectID)
	}
	if task.Status != model.TaskStatusPending {
		t.Errorf("Status = %q, want %q", task.Status, model.TaskStatusPending)
	}
	if task.Prompt != "Test topic" {
		t.Errorf("Prompt = %q, want %q", task.Prompt, "Test topic")
	}
}

func TestTaskService_CreateManual_NoProject(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    "user1",
		ProjectID: "",
		Prompt:    "topic",
	})
	if err == nil {
		t.Error("expected error for empty project_id")
	}
}

func TestTaskService_CreateManual_WrongUser(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	_, err := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    "wrong-user",
		ProjectID: projectID,
		Prompt:    "topic",
	})
	if err == nil {
		t.Error("expected error for wrong user")
	}
}

func TestTaskService_GetByID(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Find me",
	})
	task := taskSlice[0]

	found, err := svc.GetByID(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.ID != task.ID {
		t.Errorf("ID = %q, want %q", found.ID, task.ID)
	}
}

func TestTaskService_GetByID_NotFound(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	_, err := svc.GetByID(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent task")
	}
}

func TestTaskService_Cancel(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Cancel me",
	})
	task := taskSlice[0]

	err := svc.Cancel(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	found, _ := svc.GetByID(context.Background(), task.ID)
	if found.Status != model.TaskStatusCancelled {
		t.Errorf("Status = %q, want %q", found.Status, model.TaskStatusCancelled)
	}
}

func TestTaskService_CancelEnqueuesIlinkNotification(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	if err := repo.IlinkBindings().Create(context.Background(), &model.IlinkBinding{
		ID:                uuid.NewString(),
		UserID:            userID,
		PlatformAccountID: stringPtr("platform-1"),
		ExternalUserID:    stringPtr("wx-user-1"),
		Status:            model.IlinkBindingStatusActive,
	}); err != nil {
		t.Fatalf("create ilink binding: %v", err)
	}
	log := zerolog.Nop()
	svc.SetIlinkNotifier(NewIlinkNotifier(repo, true, &log))

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Cancel me",
	})
	task := taskSlice[0]

	if err := svc.Cancel(context.Background(), task.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}

	items, err := repo.IlinkNotifications().ListDue(context.Background(), 10)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("outbox items = %d, want 1", len(items))
	}
	if items[0].TaskID != task.ID || items[0].TaskStatus != model.TaskStatusCancelled {
		t.Fatalf("unexpected notification: %+v", items[0])
	}
}

func TestTaskService_List(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Task 1",
	})
	svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Task 2",
	})

	tasks, total, err := svc.List(context.Background(), userID, 0, 10, "", "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total < 2 {
		t.Errorf("total = %d, want >= 2", total)
	}
	if len(tasks) < 2 {
		t.Errorf("len(tasks) = %d, want >= 2", len(tasks))
	}
}

func TestTaskService_List_ByStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)

	taskSlice, _ := svc.CreateManual(context.Background(), CreateManualParams{ExecutionProfile: "effective",
		UserID:    userID,
		ProjectID: projectID,
		Prompt:    "Pending task",
	})
	task := taskSlice[0]
	svc.Cancel(context.Background(), task.ID)

	// Filter by pending — should find none
	tasks, total, err := svc.List(context.Background(), userID, 0, 10, model.TaskStatusPending, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 0 {
		t.Errorf("pending total = %d, want 0", total)
	}
	if len(tasks) != 0 {
		t.Errorf("pending tasks = %d, want 0", len(tasks))
	}

	// Filter by cancelled
	tasks, total, err = svc.List(context.Background(), userID, 0, 10, model.TaskStatusCancelled, "", "")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 {
		t.Errorf("cancelled total = %d, want 1", total)
	}
}

func TestTaskService_GetFiles(t *testing.T) {
	svc, _ := setupTaskServiceWithEnqueuer(t)
	files, err := svc.GetFiles(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("GetFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
}

func TestTaskService_GetVisibleFilesIncludesCollectedAfterPublished(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	taskID := uuid.NewString()
	executionID := uuid.NewString()
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: strings.Repeat("a", 64),
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("SetCurrentExecution = %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md"},
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateCollected, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json"},
	}); err != nil {
		t.Fatal(err)
	}

	published, err := svc.GetFiles(ctx, taskID)
	if err != nil || len(published) != 1 || published[0].State != model.TaskFileStatePublished {
		t.Fatalf("published files = %#v, err=%v", published, err)
	}
	visible, err := svc.GetVisibleFiles(ctx, taskID)
	if err != nil {
		t.Fatalf("GetVisibleFiles: %v", err)
	}
	if len(visible) != 2 || visible[0].State != model.TaskFileStatePublished || visible[1].State != model.TaskFileStateCollected {
		t.Fatalf("visible files = %#v", visible)
	}
}

func TestTaskServiceCollectedFileIsDownloadableButExcludedFromZip(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	svc.store = store
	ctx := context.Background()
	taskID := uuid.NewString()
	executionID := uuid.NewString()
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: taskID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: strings.Repeat("b", 64),
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
	}); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, taskID, executionID); err != nil || !ok {
		t.Fatalf("SetCurrentExecution = %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, taskID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	publishedUpload, err := store.Upload(ctx, "tasks/"+taskID+"/content.md", strings.NewReader("published"), "text/markdown")
	if err != nil {
		t.Fatal(err)
	}
	collectedUpload, err := store.Upload(ctx, "tasks/"+taskID+"/failure-state.json", strings.NewReader("failure"), "application/json")
	if err != nil {
		t.Fatal(err)
	}
	collectedID := uuid.NewString()
	if err := repo.TaskFiles().BatchCreate(ctx, []*model.TaskFile{
		{ID: uuid.NewString(), TaskID: taskID, ExecutionID: executionID, State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown, FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown", OSSKey: publishedUpload.Key, FileSize: publishedUpload.Size, StorageProvider: store.Name()},
		{ID: collectedID, TaskID: taskID, ExecutionID: "failed", State: model.TaskFileStateCollected, Role: model.FileRoleOther, FilePath: "output/failure-state.json", FileName: "failure-state.json", OSSKey: collectedUpload.Key, FileSize: collectedUpload.Size, StorageProvider: store.Name()},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.VerifyFileBelongsToTask(ctx, taskID, collectedID); err != nil {
		t.Fatalf("VerifyFileBelongsToTask: %v", err)
	}
	stream, file, err := svc.GetFileStream(ctx, collectedID)
	if err != nil {
		t.Fatalf("GetFileStream: %v", err)
	}
	data, readErr := io.ReadAll(stream)
	stream.Close()
	if readErr != nil || string(data) != "failure" || file.State != model.TaskFileStateCollected {
		t.Fatalf("downloaded collected file=%#v data=%q err=%v", file, data, readErr)
	}

	stream, _, err = svc.DownloadZip(ctx, taskID)
	if err != nil {
		t.Fatalf("DownloadZip: %v", err)
	}
	zipData, err := io.ReadAll(stream)
	stream.Close()
	if err != nil {
		t.Fatalf("read DownloadZip stream: %v", err)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "content.md" {
		t.Fatalf("zip files = %#v, want only content.md", reader.File)
	}
}

func TestTaskService_DownloadTasksZip(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	localStore, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	store := &readTrackingLocalStorage{LocalProvider: localStore}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	otherUserID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	otherProjectID := createTestProject(t, repo, otherUserID, model.PlatformSeednote)

	completed := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusRunning,
		Prompt:    "A finished task",
		Title:     "Finished",
	}
	emptyCompleted := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "A completed task without delivery files",
		Title:     "Empty",
	}
	pending := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusPending,
		Prompt:    "A pending task",
	}
	foreign := &model.Task{
		ID:        uuid.New().String(),
		UserID:    otherUserID,
		ProjectID: otherProjectID,
		Type:      model.PlatformSeednote,
		Status:    model.TaskStatusCompleted,
		Prompt:    "Foreign task",
	}
	for _, task := range []*model.Task{completed, emptyCompleted, pending, foreign} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}
	completedExecutionID := uuid.NewString()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: completedExecutionID, TaskID: completed.ID, Attempt: 1, Status: model.TaskExecutionSucceeded,
		AgentPackID: "seednote", AgentPackVersion: "1.0.1", AgentPackDigest: strings.Repeat("c", 64),
		AgentPackDeliveryContract: datatypes.JSON(`[{"role":"content","path":"output/content.md","mime_type":"text/markdown"}]`),
	}); err != nil {
		t.Fatalf("create completed execution: %v", err)
	}
	if ok, err := repo.Tasks().SetCurrentExecution(ctx, completed.ID, completedExecutionID); err != nil || !ok {
		t.Fatalf("set completed current execution = %v, %v", ok, err)
	}
	if _, err := repo.Tasks().CompareAndSwapStatus(ctx, completed.ID, model.TaskStatusRunning, model.TaskStatusCompleted); err != nil {
		t.Fatalf("complete task: %v", err)
	}

	completedUpload, err := store.Upload(ctx, "tasks/"+completed.ID+"/output/content.md", strings.NewReader("# hello"), "text/markdown")
	if err != nil {
		t.Fatalf("upload completed task file: %v", err)
	}
	if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
		ID: uuid.NewString(), TaskID: completed.ID, ExecutionID: completedExecutionID,
		State: model.TaskFileStatePublished, Role: model.FileRoleMarkdown,
		FilePath: "output/content.md", FileName: "content.md", MimeType: "text/markdown",
		FileSize: completedUpload.Size, OSSKey: completedUpload.Key, OSSURL: completedUpload.URL, StorageProvider: store.Name(),
	}); err != nil {
		t.Fatalf("persist completed task file: %v", err)
	}
	buf, zipName, err := svc.DownloadTasksZip(ctx, userID, []string{completed.ID, emptyCompleted.ID, pending.ID, foreign.ID})
	if err != nil {
		t.Fatalf("DownloadTasksZip: %v", err)
	}
	if !strings.HasPrefix(zipName, "tasks_export_") || !strings.HasSuffix(zipName, ".zip") {
		t.Fatalf("unexpected zip name: %s", zipName)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}

	var hasManifest bool
	var hasCompletedFile bool
	for _, file := range reader.File {
		if file.Name == "manifest.json" {
			hasManifest = true
			rc, err := file.Open()
			if err != nil {
				t.Fatalf("open manifest: %v", err)
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			manifest := string(data)
			var parsedManifest BulkDownloadZipManifest
			if err := json.Unmarshal(data, &parsedManifest); err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			for _, want := range []string{completed.ID, emptyCompleted.ID, pending.ID, foreign.ID, "task_not_completed", "no_delivery_files", "unavailable"} {
				if !strings.Contains(manifest, want) {
					t.Fatalf("manifest missing %q: %s", want, manifest)
				}
			}
			if strings.Contains(manifest, "forbidden") || strings.Contains(manifest, "task_not_found") {
				t.Fatalf("manifest uses distinguishable unavailable reasons: %s", manifest)
			}
			for _, task := range parsedManifest.Tasks {
				if task.TaskID == foreign.ID && (task.Title != "" || task.Status != "") {
					t.Fatalf("manifest leaked foreign task metadata: %+v", task)
				}
			}
		}
		if strings.HasSuffix(file.Name, "output/content.md") {
			hasCompletedFile = true
		}
		if strings.Contains(file.Name, "secret.md") {
			t.Fatalf("foreign file leaked into zip: %s", file.Name)
		}
	}
	if !hasManifest {
		t.Fatal("manifest.json missing")
	}
	if !hasCompletedFile {
		t.Fatal("completed task file missing")
	}
	if store.readCalls != 0 {
		t.Fatalf("storage Read calls = %d, want bulk ZIP to use bounded streaming reads", store.readCalls)
	}
}

func TestTaskService_RebuildWorkflowStatus(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	store, err := storage.NewLocalProvider(t.TempDir())
	if err != nil {
		t.Fatalf("create local storage: %v", err)
	}
	svc.store = store

	ctx := context.Background()
	userID := uuid.New().String()
	projectID := createTestProject(t, repo, userID, model.PlatformArticle)
	task := &model.Task{
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformArticle,
		Status:    model.TaskStatusCompleted,
		Prompt:    "workflow task",
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	executionID := uuid.NewString()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{
		ID: executionID, TaskID: task.ID, Attempt: 1, Status: model.TaskExecutionSucceeded,
	}); err != nil {
		t.Fatalf("create task execution: %v", err)
	}

	files := []struct {
		path    string
		body    string
		mime    string
		size    int64
		wantErr bool
	}{
		{"output/03-draft.md", "# Draft", "text/markdown", 7, false},
		{"output/04-final.md", "# Final", "text/markdown", 7, false},
		{"output/review.json", `{"overall_score":91,"readiness":"ready","strengths":["清晰"],"risks":[],"next_actions":["发布"]}`, "application/json", 92, false},
	}
	for _, file := range files {
		key := "tests/tasks/" + task.ID + "/" + file.path
		uploaded, err := store.Upload(ctx, key, strings.NewReader(file.body), file.mime)
		if err != nil {
			t.Fatalf("upload %s: %v", file.path, err)
		}
		if err := repo.TaskFiles().Create(ctx, &model.TaskFile{
			ID: uuid.NewString(), TaskID: task.ID, ExecutionID: executionID,
			State: model.TaskFileStatePublished, FilePath: file.path, FileName: filepath.Base(file.path),
			MimeType: file.mime, FileSize: uploaded.Size, OSSKey: uploaded.Key, OSSURL: uploaded.URL,
			StorageProvider: store.Name(),
		}); err != nil {
			t.Fatalf("persist %s: %v", file.path, err)
		}
	}

	if err := svc.RebuildWorkflowStatus(ctx, task.ID); err != nil {
		t.Fatalf("RebuildWorkflowStatus: %v", err)
	}

	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.WorkflowStatus == nil || *found.WorkflowStatus == "" {
		t.Fatal("workflow_status was not persisted")
	}

	var status WorkflowStatus
	if err := json.Unmarshal([]byte(*found.WorkflowStatus), &status); err != nil {
		t.Fatalf("unmarshal workflow status: %v", err)
	}
	if status.Review == nil {
		t.Fatal("expected review summary")
	}
	if status.Review.OverallScore != 91 {
		t.Fatalf("review score = %d, want 91", status.Review.OverallScore)
	}
}

func TestCreateManualClonesInputAttachments(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	input := []model.EntryAttachment{{
		Type:        "image",
		URL:         "/api/v1/files/product.png",
		FileName:    "product.png",
		ContentType: "image/png",
		Instruction: "保持包装和 Logo",
	}}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{ExecutionProfile: "effective",
		UserID:           userID,
		ProjectID:        projectID,
		Prompt:           "生成种草图文",
		Quantity:         1,
		InputAttachments: input,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	input[0].Instruction = "调用方后续修改"
	input[0].FileName = "mutated.png"

	returned := tasks[0].InputAttachments.Data()
	if len(returned) != 1 || returned[0].Instruction != "保持包装和 Logo" || returned[0].FileName != "product.png" {
		t.Fatalf("returned attachment snapshot = %#v", returned)
	}
	found, err := repo.Tasks().FindByID(ctx, tasks[0].ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	stored := found.InputAttachments.Data()
	if len(stored) != 1 || stored[0].Instruction != "保持包装和 Logo" || stored[0].FileName != "product.png" {
		t.Fatalf("persisted attachment snapshot = %#v", stored)
	}
}

func TestCreateFromPlanClonesAttachmentSnapshotPerTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := context.Background()
	userID := uuid.New().String()
	if err := repo.Users().Create(ctx, &model.User{
		ID:         userID,
		Email:      userID + "@example.com",
		Password:   "hashed",
		InviteCode: "plantasksnapshot",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	projectID := createTestProject(t, repo, userID, model.PlatformSeednote)
	plan := &model.Plan{ExecutionProfile: "effective",
		ID:        uuid.New().String(),
		UserID:    userID,
		ProjectID: projectID,
		Type:      model.PlatformSeednote,
		Status:    model.PlanStatusActive,
		CronExpr:  "0 9 * * *",
		Prompt:    "根据产品参考图生成种草内容",
	}
	plan.SetInputAttachments([]model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/product.png", Instruction: "保留原始包装",
	}})
	if err := repo.Plans().Create(ctx, plan); err != nil {
		t.Fatalf("create plan: %v", err)
	}

	taskA, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan A: %v", err)
	}
	taskB, err := svc.CreateFromPlan(ctx, plan)
	if err != nil {
		t.Fatalf("CreateFromPlan B: %v", err)
	}

	plan.SetInputAttachments([]model.EntryAttachment{{
		Type: "image", URL: "https://cdn.example.com/product.png", Instruction: "计划后来修改",
	}})
	if err := repo.Plans().Update(ctx, plan); err != nil {
		t.Fatalf("update plan: %v", err)
	}

	for label, taskID := range map[string]string{"task A": taskA.ID, "task B": taskB.ID} {
		stored, err := repo.Tasks().FindByID(ctx, taskID)
		if err != nil {
			t.Fatalf("find %s: %v", label, err)
		}
		got := stored.InputAttachments.Data()
		if len(got) != 1 || got[0].Instruction != "保留原始包装" {
			t.Fatalf("%s attachments = %#v, want original plan snapshot", label, got)
		}
	}
}
