package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	serveragent "github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

func TestBootstrapTransitionsCurrentExecutionAndIsIdempotentForSamePod(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	t.Cleanup(func() { _ = repo.Close() })
	ctx := context.Background()
	userID, projectID, taskID, executionID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Users().Create(ctx, &model.User{ID: userID, Email: uuid.NewString() + "@example.com", Password: "x", InviteCode: uuid.NewString()[:12]}); err != nil {
		t.Fatal(err)
	}
	project := &model.Project{ID: projectID, UserID: userID, Platform: model.PlatformSeednote, Name: "P", Instructions: "Write precisely", Status: model.ProjectStatusActive}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning, Prompt: "topic", ReferenceImageURL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/reference.png"}
	task.SetInputAttachments([]model.EntryAttachment{
		{Type: "text", Text: "brief", FileName: "brief.txt"},
		{Type: "image", URL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/input.png", FileName: "input.png"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "resume context"},
	})
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, Target: "kubernetes", Status: model.TaskExecutionStarting, Namespace: "anban", JobName: "job-1"}); err != nil {
		t.Fatal(err)
	}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	store := &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Model: "claude-test", MaxTurns: map[string]int{model.PlatformSeednote: 12}, TokenTTL: 10 * time.Minute, ActiveDeadline: 5 * time.Minute, Store: store}, zerolog.Nop())
	identity := &serveragent.KubernetesWorkloadIdentity{Namespace: "anban", PodName: "pod-1", PodUID: "pod-uid-1", JobName: "job-1", ExecutionID: executionID, TaskID: taskID, ProjectID: projectID, UserID: userID, JobDeadline: time.Now().Add(4 * time.Minute)}
	first, err := svc.Bootstrap(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	if first.ExecutionToken == "" || first.TaskID != taskID || first.ProjectID != projectID || first.AgentFlag != "anban:seednote" || first.AutoMemoryDirectory != ".claude/memory" || first.MaxTurns != 12 {
		t.Fatalf("response = %#v", first)
	}
	if len(first.Files) < 2 {
		t.Fatalf("files = %#v", first.Files)
	}
	paths := map[string]BootstrapFile{}
	for _, file := range first.Files {
		paths[file.Path] = file
	}
	for _, path := range []string{".anban-creator/reference.png", ".anban-creator/input-attachments/01-brief.txt", ".anban-creator/input-attachments/02-input.png", ".anban-creator/input-attachments/index.json", ".anban-creator/resume/latest.md"} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("missing bootstrap path %q in %#v", path, first.Files)
		}
	}
	if paths[".anban-creator/reference.png"].DownloadURL == "" || paths[".anban-creator/input-attachments/02-input.png"].DownloadURL == "" {
		t.Fatal("private objects were not signed")
	}
	if !strings.Contains(paths[".anban-creator/settings.json"].Text, `"seednote"`) {
		t.Fatalf("settings did not use runtime app config: %s", paths[".anban-creator/settings.json"].Text)
	}
	claims, err := tokens.Validate(first.ExecutionToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.After(identity.JobDeadline) {
		t.Fatalf("token expiry = %v, exceeds Job deadline", claims.ExpiresAt)
	}
	second, err := svc.Bootstrap(ctx, identity)
	if err != nil || second.TaskID != taskID {
		t.Fatalf("idempotent retry = %#v, %v", second, err)
	}
	found, _ := repo.TaskExecutions().FindByID(ctx, executionID)
	if found.Status != model.TaskExecutionRunning || !found.Started || found.PodUID != "pod-uid-1" || found.StartedAt == nil || found.LastHeartbeatAt == nil {
		t.Fatalf("execution = %#v", found)
	}
	if _, err := svc.Bootstrap(ctx, &serveragent.KubernetesWorkloadIdentity{Namespace: "anban", PodName: "pod-2", PodUID: "pod-uid-2", JobName: "job-1", ExecutionID: executionID, TaskID: taskID, ProjectID: projectID, UserID: userID}); err == nil {
		t.Fatal("different Pod stole running execution")
	}
	taskSvc := NewTaskService(repo, nil, nil, nil, nil, nil, "", nil, "", nil, nil)
	if err := taskSvc.ValidateAgentExecutionAccess(ctx, userID, projectID, taskID, executionID); err != nil {
		t.Fatalf("current execution rejected: %v", err)
	}
	if err := taskSvc.ValidateAgentExecutionAccess(ctx, userID, projectID, taskID, uuid.NewString()); err == nil {
		t.Fatal("stale execution token accepted")
	}
}

func TestValidateBootstrapFilesRejectsUnsafeContracts(t *testing.T) {
	for _, files := range [][]BootstrapFile{
		{{Path: "/absolute", Text: "x", Mode: 0644}},
		{{Path: "../escape", Text: "x", Mode: 0644}},
		{{Path: "same", Text: "x", Mode: 0644}, {Path: "same", Text: "y", Mode: 0644}},
		{{Path: "both", Text: "x", DownloadURL: "https://example.com/x", Mode: 0644}},
		{{Path: "world", Text: "x", Mode: 0666}},
		{{Path: "setuid", Text: "x", Mode: 04644}},
		{{Path: "unexpected-executable", Text: "x", Mode: 0755}},
	} {
		if err := ValidateBootstrapFiles(files); err == nil {
			t.Fatalf("unsafe files accepted: %#v", files)
		}
	}
}

func TestBuildMontageBootstrapFiles(t *testing.T) {
	svc := &AgentBootstrapService{cfg: AgentBootstrapConfig{MontageToolPolicy: map[string]config.MontageToolCapabilityPolicy{}, MontagePipelineDefaults: map[string]map[string]any{}}}
	files, err := svc.buildMontageFiles(&model.Task{Type: model.PlatformMontage})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, file := range files {
		paths[file.Path] = true
	}
	for _, path := range []string{"montage-input.json", "montage-tool-policy.json", "montage-pipeline-defaults.json"} {
		if !paths[path] {
			t.Fatalf("missing %s", path)
		}
	}
}

func TestBuildEcommerceProductBootstrapFiles(t *testing.T) {
	svc := &AgentBootstrapService{cfg: AgentBootstrapConfig{Store: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}, SignedURLTTL: 60}}
	task := &model.Task{Type: model.PlatformEcommerce}
	task.SetEcommerce(model.EcommerceConfig{ProductPhotos: []string{"https://bucket.oss-cn-x.aliyuncs.com/uploads/product.png"}})
	files, err := svc.buildProductFiles(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != ".anban-creator/products/product_01.png" || files[0].DownloadURL == "" || files[1].Path != ".anban-creator/products/index.json" {
		t.Fatalf("product files = %#v", files)
	}
}
