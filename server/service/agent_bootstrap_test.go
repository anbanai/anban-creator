package service

import (
	"context"
	"fmt"
	"path"
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

type bootstrapSecurityStore struct {
	*signFakeStore
	signedKeys     []string
	signedTTLs     []int
	signAt         time.Time
	signedExpiries []time.Time
}

func (s *bootstrapSecurityStore) DownloadURL(_ context.Context, key string, ttl int) (string, error) {
	s.signedKeys = append(s.signedKeys, key)
	s.signedTTLs = append(s.signedTTLs, ttl)
	if !s.signAt.IsZero() {
		s.signedExpiries = append(s.signedExpiries, time.Unix(s.signAt.Unix()+int64(ttl), 0))
	}
	return "https://signed.example.com/" + key, nil
}

func TestBootstrapSignedDownloadDoesNotCrossAbsoluteDeadlineAtSignerBoundary(t *testing.T) {
	start := time.Unix(1_800_000_000, 0)
	deadline := start.Add(3 * time.Second)
	for _, tc := range []struct {
		name    string
		signAt  time.Time
		wantErr bool
	}{
		{name: "crosses one second boundary", signAt: start.Add(1100 * time.Millisecond)},
		{name: "signer stalls beyond safety margin", signAt: start.Add(2100 * time.Millisecond), wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.example.com/"}, signAt: tc.signAt}
			samples := []time.Time{start, tc.signAt}
			svc := &AgentBootstrapService{cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}, now: func() time.Time {
				sampled := samples[0]
				samples = samples[1:]
				return sampled
			}}
			task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
			key := "uploads/users/user-1/projects/project-1/tasks/task-1/inputs/reference.png"
			_, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: "https://bucket.example.com/" + key}, deadline)
			if tc.wantErr {
				if err == nil {
					t.Fatal("stalled signer returned a URL beyond the absolute deadline")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(store.signedExpiries) != 1 || store.signedExpiries[0].After(deadline) {
				t.Fatalf("signed expiry = %v, deadline = %v, ttl = %v", store.signedExpiries, deadline, store.signedTTLs)
			}
		})
	}
}

func TestBootstrapBoundsEverySignedDownloadToCredentialDeadline(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second).Add(200 * time.Millisecond)
	jobDeadline := now.Add(3700 * time.Millisecond)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := NewAgentBootstrapService(nil, tokens, AgentBootstrapConfig{TokenTTL: 10 * time.Minute, SignedURLTTL: 600, Store: store}, zerolog.Nop())
	svc.now = func() time.Time { return now }
	prefix := "uploads/users/user-1/projects/project-1/tasks/task-1"
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformEcommerce, Prompt: "topic", ReferenceImageURL: "https://bucket.oss-cn-x.aliyuncs.com/" + prefix + "/inputs/reference.png"}
	task.SetInputAttachments([]model.EntryAttachment{
		{Type: "image", URL: "https://bucket.oss-cn-x.aliyuncs.com/" + prefix + "/inputs/attachment.png", FileName: "attachment.png"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "read attachments/resume.pdf"},
		{Role: model.EntryAttachmentRoleResumeFile, URL: "https://bucket.oss-cn-x.aliyuncs.com/" + prefix + "/resume/run/attachments/resume.pdf", FileName: "resume.pdf"},
	})
	task.SetEcommerce(model.EcommerceConfig{ProductPhotos: []string{"https://bucket.oss-cn-x.aliyuncs.com/" + prefix + "/inputs/product.png"}})
	const resumeSessionID = "bba21f1d-70b8-4157-917b-f9802c2b1740"
	response, err := svc.buildResponse(context.Background(), &model.TaskExecution{ID: "execution-1", ResumeSessionID: resumeSessionID}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, jobDeadline)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := tokens.Validate(response.ExecutionToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.After(jobDeadline) {
		t.Fatalf("token expiry %v exceeds Job deadline %v", claims.ExpiresAt, jobDeadline)
	}
	if response.ResumeSessionID != resumeSessionID {
		t.Fatalf("resume session = %q, want %q", response.ResumeSessionID, resumeSessionID)
	}
	if len(store.signedTTLs) != 4 {
		t.Fatalf("signed TTLs = %v, want reference, attachment, resume, product", store.signedTTLs)
	}
	for i, ttl := range store.signedTTLs {
		if ttl <= 0 || now.Add(time.Duration(ttl)*time.Second).After(claims.ExpiresAt.Time) {
			t.Fatalf("signed TTL[%d]=%d exceeds credential deadline %v from %v", i, ttl, claims.ExpiresAt.Time, now)
		}
	}

	before := len(store.signedKeys)
	svc.now = func() time.Time { return jobDeadline.Add(-500 * time.Millisecond) }
	if _, err := svc.buildResponse(context.Background(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, jobDeadline); err == nil {
		t.Fatal("bootstrap accepted a deadline with no positive whole-second signing lifetime")
	}
	if len(store.signedKeys) != before {
		t.Fatalf("expired bootstrap reached signer: %v", store.signedKeys[before:])
	}
}

func TestBootstrapRejectsTextOnlyResponseWhenSafeLifetimeExpiresDuringBuild(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second).Add(200 * time.Millisecond)
	jobDeadline := now.Add(3700 * time.Millisecond)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	svc := NewAgentBootstrapService(nil, tokens, AgentBootstrapConfig{TokenTTL: 10 * time.Minute}, zerolog.Nop())
	clockSamples := []time.Time{now, jobDeadline.Add(-500 * time.Millisecond)}
	svc.now = func() time.Time {
		sampled := clockSamples[0]
		clockSamples = clockSamples[1:]
		return sampled
	}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle, Prompt: "topic", SkipReferenceImage: true}
	if _, err := svc.buildResponse(context.Background(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, jobDeadline); err == nil {
		t.Fatal("text-only bootstrap issued a token without a positive whole-second lifetime")
	}
}

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
	taskPrefix := fmt.Sprintf("uploads/users/%s/projects/%s/tasks/%s", userID, projectID, taskID)
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning, Prompt: "topic", ReferenceImageURL: "https://bucket.oss-cn-x.aliyuncs.com/" + taskPrefix + "/inputs/reference.png"}
	task.SetInputAttachments([]model.EntryAttachment{
		{Type: "text", Text: "brief", FileName: "brief.txt"},
		{Type: "image", URL: "https://bucket.oss-cn-x.aliyuncs.com/" + taskPrefix + "/inputs/input.png", FileName: "input.png"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "read attachments/foo.pdf"},
		{Role: model.EntryAttachmentRoleResumeFile, URL: "https://bucket.oss-cn-x.aliyuncs.com/" + taskPrefix + "/resume/run/attachments/foo.pdf", Key: taskPrefix + "/resume/run/attachments/foo.pdf", FileName: "foo.pdf"},
	})
	task.CurrentExecutionID = &executionID
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	resumeSessionID := uuid.NewString()
	if err := repo.TaskExecutions().Create(ctx, &model.TaskExecution{ID: executionID, TaskID: taskID, Attempt: 1, ResumeSessionID: resumeSessionID, Target: "kubernetes", Status: model.TaskExecutionStarting, Namespace: "anban", JobName: "job-1"}); err != nil {
		t.Fatal(err)
	}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	store := &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Model: "claude-test", MaxTurns: map[string]int{model.PlatformSeednote: 12}, TokenTTL: 10 * time.Minute, ActiveDeadline: 5 * time.Minute, Store: store, RuntimeEnv: map[string]string{
		"ANTHROPIC_AUTH_TOKEN": "bootstrap-secret",
		"ANTHROPIC_BASE_URL":   "https://anthropic.example.com",
		"PATH":                 "/untrusted/bin",
	}}, zerolog.Nop())
	identity := &serveragent.KubernetesWorkloadIdentity{Namespace: "anban", PodName: "pod-1", PodUID: "pod-uid-1", JobName: "job-1", ExecutionID: executionID, TaskID: taskID, ProjectID: projectID, UserID: userID, JobDeadline: time.Now().Add(4 * time.Minute)}
	first, err := svc.Bootstrap(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	resumeContextPath, _ := serveragent.ExecutionResumeContextPath(executionID)
	if first.ExecutionToken == "" || first.TaskID != taskID || first.ProjectID != projectID || first.AgentFlag != "anban:seednote" || first.AutoMemoryDirectory != ".claude/memory" || first.ResumeSessionID != resumeSessionID || first.ResumeContextPath != resumeContextPath || first.MaxTurns != 12 {
		t.Fatalf("response = %#v", first)
	}
	if first.RuntimeEnv["ANTHROPIC_AUTH_TOKEN"] != "bootstrap-secret" || first.RuntimeEnv["ANTHROPIC_BASE_URL"] != "https://anthropic.example.com" || len(first.RuntimeEnv) != 2 {
		t.Fatalf("runtime environment = %#v, want only allowlisted Claude values", first.RuntimeEnv)
	}
	if len(first.Files) < 2 {
		t.Fatalf("files = %#v", first.Files)
	}
	paths := map[string]BootstrapFile{}
	for _, file := range first.Files {
		paths[file.Path] = file
	}
	resumeAttachmentPath, _ := serveragent.ExecutionResumeAttachmentPath(executionID, "foo.pdf")
	for _, path := range []string{".anban-creator/reference.png", ".anban-creator/input-attachments/01-brief.txt", ".anban-creator/input-attachments/02-input.png", ".anban-creator/input-attachments/index.json", resumeContextPath, resumeAttachmentPath} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("missing bootstrap path %q in %#v", path, first.Files)
		}
	}
	if _, exists := paths[path.Join(path.Dir(resumeAttachmentPath), "04-foo.pdf")]; exists {
		t.Fatal("bootstrap renamed persisted resume attachment")
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
	if second.ExecutionToken == first.ExecutionToken {
		t.Fatal("idempotent retry reused JWT ID")
	}
	retryClaims, err := tokens.Validate(second.ExecutionToken)
	if err != nil {
		t.Fatal(err)
	}
	if retryClaims.ExecutionID != executionID || retryClaims.ExpiresAt == nil || retryClaims.ExpiresAt.Time.After(identity.JobDeadline) {
		t.Fatalf("retry claims = %#v", retryClaims)
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

func TestBootstrapRejectsExpiredJobDeadlineBeforeBuildingResponse(t *testing.T) {
	svc := &AgentBootstrapService{}
	if _, err := svc.buildResponse(context.Background(), nil, nil, nil, time.Now().Add(-time.Second)); err == nil {
		t.Fatal("expired Job deadline accepted")
	}
}

func TestValidateBootstrapFilesRejectsUnsafeContracts(t *testing.T) {
	for _, files := range [][]BootstrapFile{
		{{Path: "/absolute", Text: "x", Mode: 0644}},
		{{Path: "../escape", Text: "x", Mode: 0644}},
		{{Path: "same", Text: "x", Mode: 0644}, {Path: "same", Text: "y", Mode: 0644}},
		{{Path: "Foo.txt", Text: "x", Mode: 0644}, {Path: "foo.txt", Text: "y", Mode: 0644}},
		{{Path: "Straße.txt", Text: "x", Mode: 0644}, {Path: "STRASSE.txt", Text: "y", Mode: 0644}},
		{{Path: "Résumé.txt", Text: "x", Mode: 0644}, {Path: "Re\u0301sume\u0301.txt", Text: "y", Mode: 0644}},
		{{Path: strings.Repeat("a", 256), Text: "x", Mode: 0644}},
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
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformEcommerce}
	task.SetEcommerce(model.EcommerceConfig{ProductPhotos: []string{"https://bucket.oss-cn-x.aliyuncs.com/uploads/users/user-1/projects/project-1/tasks/task-1/inputs/product.png"}})
	files, err := svc.buildProductFiles(context.Background(), task, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != ".anban-creator/products/product_01.png" || files[0].DownloadURL == "" || files[1].Path != ".anban-creator/products/index.json" {
		t.Fatalf("product files = %#v", files)
	}
}

func TestBootstrapDownloadSigningRequiresCanonicalTaskOwnership(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	goodKey := "uploads/users/user-1/projects/project-1/tasks/task-1/inputs/good.png"
	goodURL := "https://bucket.oss-cn-x.aliyuncs.com/" + goodKey
	deadline := time.Now().Add(time.Hour)

	if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: goodURL, AssertedKey: goodKey}, deadline); err != nil {
		t.Fatalf("owned input rejected: %v", err)
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != goodKey {
		t.Fatalf("signed keys = %v", store.signedKeys)
	}

	attacks := []bootstrapDownloadSource{
		{URL: goodURL, AssertedKey: "uploads/users/victim/projects/victim-project/tasks/victim-task/secret.png"},
		{URL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/users/user-2/projects/project-2/tasks/task-2/secret.png", AssertedKey: "uploads/users/user-2/projects/project-2/tasks/task-2/secret.png"},
		{URL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/users/user-1/projects/project-2/tasks/task-2/secret.png", AssertedKey: "uploads/users/user-1/projects/project-2/tasks/task-2/secret.png"},
		{URL: "https://external.example.com/uploads/users/user-1/projects/project-1/tasks/task-1/secret.png"},
	}
	for _, attack := range attacks {
		if _, err := svc.signedDownloadURL(context.Background(), task, attack, deadline); err == nil {
			t.Fatalf("unsafe source accepted: %#v", attack)
		}
	}
	if len(store.signedKeys) != 1 {
		t.Fatalf("attack reached signer: %v", store.signedKeys)
	}
}

func TestBootstrapDownloadSigningAllowsExplicitCloneInputSource(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "clone-task", UserID: "user-1", ProjectID: "project-1", InputSourceTaskID: "source-task"}
	deadline := time.Now().Add(time.Hour)
	allowedKey := "uploads/users/user-1/projects/project-1/tasks/source-task/inputs/reference.png"
	allowedURL := "https://bucket.oss-cn-x.aliyuncs.com/" + allowedKey

	if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: allowedURL}, deadline); err != nil {
		t.Fatalf("explicit clone input source rejected: %v", err)
	}
	for _, key := range []string{
		"uploads/users/user-1/projects/project-1/tasks/other-task/inputs/reference.png",
		"uploads/users/user-1/projects/other-project/tasks/source-task/inputs/reference.png",
		"uploads/users/other-user/projects/project-1/tasks/source-task/inputs/reference.png",
	} {
		if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: "https://bucket.oss-cn-x.aliyuncs.com/" + key}, deadline); err == nil {
			t.Fatalf("unrelated clone source accepted: %s", key)
		}
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != allowedKey {
		t.Fatalf("signed keys = %v, want only explicit source", store.signedKeys)
	}
}

func TestBootstrapDownloadSigningAllowsUserOwnedChannelReference(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	deadline := time.Now().Add(time.Hour)
	ownedURL := "https://bucket.oss-cn-x.aliyuncs.com/uploads%2Fchannels%2Fuser-1%2Freference.jpg?Expires=1&Signature=redacted"

	if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: ownedURL}, deadline); err != nil {
		t.Fatalf("user-owned channel reference rejected: %v", err)
	}
	otherURL := "https://bucket.oss-cn-x.aliyuncs.com/uploads%2Fchannels%2Fuser-2%2Freference.jpg"
	if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: otherURL}, deadline); err == nil {
		t.Fatal("other user's channel reference accepted")
	}
}

func TestBootstrapDownloadSigningValidatesFinalizedPendingUpload(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	key := "uploads/pending/user-1/upload-1/input.png"
	url := "https://bucket.oss-cn-x.aliyuncs.com/" + key
	deadline := time.Now().Add(time.Hour)
	if err := repo.PendingUploads().CreatePendingUpload(context.Background(), &model.PendingUpload{ID: "upload-1", UserID: task.UserID, Purpose: DirectUploadPurposeAIEntryAttachment, Key: key, PublicURL: url, Status: model.PendingUploadStatusFinalized, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: url, AssertedKey: key, UploadID: "upload-1", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}}, deadline); err != nil {
		t.Fatalf("finalized pending input rejected: %v", err)
	}
	if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: url, AssertedKey: key, UploadID: "other", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}}, deadline); err == nil {
		t.Fatal("mismatched upload id accepted")
	}
	if len(store.signedKeys) != 1 {
		t.Fatalf("rejected upload reached signer: %v", store.signedKeys)
	}
	encodedKey := "uploads/pending/user-1/upload-encoded/reference.png"
	encodedPublicURL := "https://bucket.oss-cn-x.aliyuncs.com/" + encodedKey
	if err := repo.PendingUploads().CreatePendingUpload(context.Background(), &model.PendingUpload{
		ID:        "upload-encoded",
		UserID:    task.UserID,
		Purpose:   DirectUploadPurposeTaskReference,
		Key:       encodedKey,
		PublicURL: encodedPublicURL,
		Status:    model.PendingUploadStatusFinalized,
		ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	encodedURL := "https://bucket.oss-cn-x.aliyuncs.com/uploads%2Fpending%2Fuser-1%2Fupload-encoded%2Freference.png?Expires=1&Signature=redacted"
	if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{
		URL: encodedURL, AllowedPurposes: []string{DirectUploadPurposeTaskReference},
	}, deadline); err != nil {
		t.Fatalf("encoded finalized pending input rejected: %v", err)
	}

	attacks := []struct {
		name   string
		upload *model.PendingUpload
		source bootstrapDownloadSource
	}{
		{name: "guessed key without URL", source: bootstrapDownloadSource{AssertedKey: key, UploadID: "upload-1", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}}},
		{name: "other tenant URL", upload: &model.PendingUpload{ID: "victim-upload", UserID: "victim", Purpose: DirectUploadPurposeAIEntryAttachment, Key: "uploads/pending/victim/victim-upload/secret.png", PublicURL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/victim/victim-upload/secret.png", Status: model.PendingUploadStatusFinalized, ExpiresAt: time.Now().Add(time.Hour)}, source: bootstrapDownloadSource{URL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/victim/victim-upload/secret.png", AssertedKey: "uploads/pending/victim/victim-upload/secret.png", UploadID: "victim-upload", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}}},
		{name: "wrong purpose", upload: &model.PendingUpload{ID: "wrong-purpose", UserID: task.UserID, Purpose: DirectUploadPurposeVideoReference, Key: "uploads/pending/user-1/wrong-purpose/video.mp4", PublicURL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/user-1/wrong-purpose/video.mp4", Status: model.PendingUploadStatusFinalized, ExpiresAt: time.Now().Add(time.Hour)}, source: bootstrapDownloadSource{URL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/user-1/wrong-purpose/video.mp4", UploadID: "wrong-purpose", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}}},
		{name: "not finalized", upload: &model.PendingUpload{ID: "still-pending", UserID: task.UserID, Purpose: DirectUploadPurposeAIEntryAttachment, Key: "uploads/pending/user-1/still-pending/input.png", PublicURL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/user-1/still-pending/input.png", Status: model.PendingUploadStatusPending, ExpiresAt: time.Now().Add(time.Hour)}, source: bootstrapDownloadSource{URL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/user-1/still-pending/input.png", UploadID: "still-pending", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}}},
		{name: "record key mismatch", upload: &model.PendingUpload{ID: "key-mismatch", UserID: task.UserID, Purpose: DirectUploadPurposeAIEntryAttachment, Key: "uploads/pending/user-1/key-mismatch/server.png", PublicURL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/user-1/key-mismatch/server.png", Status: model.PendingUploadStatusFinalized, ExpiresAt: time.Now().Add(time.Hour)}, source: bootstrapDownloadSource{URL: "https://bucket.oss-cn-x.aliyuncs.com/uploads/pending/user-1/key-mismatch/client.png", UploadID: "key-mismatch", AllowedPurposes: []string{DirectUploadPurposeAIEntryAttachment}}},
	}
	for _, attack := range attacks {
		t.Run(attack.name, func(t *testing.T) {
			if attack.upload != nil {
				if err := repo.PendingUploads().CreatePendingUpload(context.Background(), attack.upload); err != nil {
					t.Fatal(err)
				}
			}
			before := len(store.signedKeys)
			if _, err := svc.signedDownloadURL(context.Background(), task, attack.source, deadline); err == nil {
				t.Fatalf("unsafe pending source accepted: %#v", attack.source)
			}
			if len(store.signedKeys) != before {
				t.Fatalf("unsafe pending source reached signer: %v", store.signedKeys)
			}
		})
	}
}

func TestBootstrapPendingPurposeMatrix(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	deadline := time.Now().Add(time.Hour)
	for _, purpose := range []string{DirectUploadPurposeTaskReference, DirectUploadPurposeProjectReference, DirectUploadPurposeAIEntryAttachment, DirectUploadPurposeEcommercePhoto} {
		t.Run(purpose, func(t *testing.T) {
			id := strings.ReplaceAll(purpose, "_", "-")
			key := "uploads/pending/user-1/" + id + "/input.png"
			url := "https://bucket.oss-cn-x.aliyuncs.com/" + key
			if err := repo.PendingUploads().CreatePendingUpload(context.Background(), &model.PendingUpload{ID: id, UserID: task.UserID, Purpose: purpose, Key: key, PublicURL: url, Status: model.PendingUploadStatusFinalized, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			if _, err := svc.signedDownloadURL(context.Background(), task, bootstrapDownloadSource{URL: url, AllowedPurposes: []string{purpose}}, deadline); err != nil {
				t.Fatalf("legitimate %s upload rejected: %v", purpose, err)
			}
		})
	}
}

func openBootstrapTestRepository(t *testing.T) repository.Repository {
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
	return repo
}
