package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
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

func bootstrapTestProfile(t *testing.T) (AgentExecutionProfile, *AgentProfileRegistry) {
	t.Helper()
	profile := AgentExecutionProfile{
		ID: "effective", DisplayName: "Cost effective",
		Provider: "deepseek", Protocol: "anthropic", Envs: map[string]string{
			model.ClaudeEnvBaseURL: "https://anthropic.example.com", model.ClaudeEnvAuthToken: "test-token",
			model.ClaudeEnvModel: "claude-test", "ANTHROPIC_DEFAULT_OPUS_MODEL": "claude-test",
			"ANTHROPIC_DEFAULT_FABLE_MODEL": "claude-test", "ANTHROPIC_DEFAULT_SONNET_MODEL": "claude-test",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL": "claude-test",
		},
		ModelUsageAliases: map[string]string{"claude-test": "claude-test"},
		MinTier:           model.TierFree, Available: true,
	}
	registry, err := NewAgentProfileRegistry([]AgentExecutionProfile{profile})
	if err != nil {
		t.Fatalf("NewAgentProfileRegistry: %v", err)
	}
	return profile, registry
}

func applyBootstrapTestProfile(t *testing.T, svc *AgentBootstrapService, task *model.Task, execution *model.TaskExecution) {
	t.Helper()
	profile, registry := bootstrapTestProfile(t)
	snapshot := profile.Snapshot()
	fingerprint, err := model.AgentProfileFingerprint(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	svc.cfg.Registry = registry
	task.ExecutionProfile = profile.ID
	task.AgentProfileSnapshot = snapshot
	task.AgentProfileFingerprint = fingerprint
	profiled := model.NewTaskExecutionAgentProfile(snapshot, fingerprint)
	execution.ExecutionProfile = profiled.ExecutionProfile
	execution.Provider = profiled.Provider
	execution.ProfileEnvs = profiled.ProfileEnvs
	execution.ProfileFingerprint = profiled.ProfileFingerprint
}

func buildBootstrapTestResponse(t *testing.T, svc *AgentBootstrapService, ctx context.Context, execution *model.TaskExecution, task *model.Task, project *model.Project, deadline time.Time) (*AgentBootstrapResponse, error) {
	t.Helper()
	applyBootstrapTestProfile(t, svc, task, execution)
	return svc.buildResponse(ctx, execution, task, project, deadline)
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
			key := "assets/users/user-1/asset-1/reference.png"
			_, err := svc.signedBootstrapObjectKey(context.Background(), key, deadline)
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

func TestBootstrapSignsOwnedReferenceAsset(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	asset := referenceAssetFixture("asset-bootstrap", "user-1", DirectUploadPurposeTaskReference)
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformSeednote, ReferenceImageAssetID: asset.ID}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60}, zerolog.Nop())

	response, err := buildBootstrapTestResponse(t, svc, context.Background(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("buildResponse: %v", err)
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
		t.Fatalf("signed keys = %#v, want [%s]", store.signedKeys, asset.StorageKey)
	}
	found := false
	for _, file := range response.Files {
		if file.Path == ".anban-creator/task-reference.png" && file.DownloadURL != "" {
			if file.ExpectedSize != asset.Size || file.MaxBytes != 10<<20 {
				t.Fatalf("reference limits = expected:%d max:%d, want %d and %d", file.ExpectedSize, file.MaxBytes, asset.Size, 10<<20)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("bootstrap files = %#v, want materialized reference", response.Files)
	}
	raw, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "storage_key") {
		t.Fatalf("bootstrap response exposed storage key: %s", raw)
	}
}

func TestBootstrapBuildsMontagePromptFromFrozenTaskImageSettings(t *testing.T) {
	for _, tc := range []struct {
		name              string
		imageRatio        string
		hasReferenceImage bool
		wantLines         []string
	}{
		{
			name:              "task portrait",
			imageRatio:        "9:16",
			hasReferenceImage: true,
			wantLines: []string{
				"Video aspect ratio: 9:16",
				"Portrait reference: use the system-provided portrait at .anban-creator/task-reference.png",
			},
		},
		{
			name:       "no portrait",
			imageRatio: "16:9",
			wantLines: []string{
				"Video aspect ratio: 16:9",
				"Portrait reference: no system portrait selected",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := openBootstrapTestRepository(t)
			tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
			if err != nil {
				t.Fatal(err)
			}
			task := &model.Task{
				ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformMontage,
				Prompt: "turn this webinar into a launch video", ImageRatio: tc.imageRatio,
			}
			if tc.hasReferenceImage {
				asset := referenceAssetFixture("asset-bootstrap", task.UserID, DirectUploadPurposeTaskReference)
				if err := repo.Assets().Create(t.Context(), asset); err != nil {
					t.Fatal(err)
				}
				task.ReferenceImageAssetID = asset.ID
			}
			svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: &signFakeStore{}, TokenTTL: time.Hour, SignedURLTTL: 60}, zerolog.Nop())

			response, err := buildBootstrapTestResponse(t, svc, t.Context(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatalf("buildResponse: %v", err)
			}
			for _, line := range tc.wantLines {
				if !strings.Contains(response.Prompt, line) {
					t.Errorf("bootstrap prompt = %q, want line %q", response.Prompt, line)
				}
			}
		})
	}
}

func TestBootstrapDoesNotMaterializeLegacyTaskContext(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle, SkipReferenceImage: true}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: &signFakeStore{}, TokenTTL: time.Hour}, zerolog.Nop())

	response, err := buildBootstrapTestResponse(t, svc, t.Context(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("buildResponse: %v", err)
	}
	for _, file := range response.Files {
		if file.Path == ".task-context" {
			t.Fatalf("bootstrap files retain legacy task context: %#v", response.Files)
		}
	}
}

func TestBootstrapRejectsReferenceAssetOwnershipAndPurpose(t *testing.T) {
	for _, tc := range []struct {
		name    string
		owner   string
		purpose string
	}{
		{name: "foreign", owner: "user-2", purpose: DirectUploadPurposeTaskReference},
		{name: "wrong purpose", owner: "user-1", purpose: DirectUploadPurposeProjectReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := openBootstrapTestRepository(t)
			store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
			tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
			asset := referenceAssetFixture("asset-bootstrap", tc.owner, tc.purpose)
			if err := repo.Assets().Create(t.Context(), asset); err != nil {
				t.Fatal(err)
			}
			task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformSeednote, ReferenceImageAssetID: asset.ID}
			svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60}, zerolog.Nop())
			if _, err := buildBootstrapTestResponse(t, svc, context.Background(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour)); err == nil {
				t.Fatal("invalid reference asset was accepted")
			}
			if len(store.signedKeys) != 0 {
				t.Fatalf("invalid reference reached signer: %#v", store.signedKeys)
			}
		})
	}
}

func TestBootstrapSignsInheritedProjectReferenceAsset(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	asset := referenceAssetFixture("asset-project", "user-1", DirectUploadPurposeProjectReference)
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task-1", UserID: asset.UserID, ProjectID: "project-1", Type: model.PlatformSeednote}
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: task.Type, ReferenceImageAssetID: asset.ID})
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60}, zerolog.Nop())

	response, err := buildBootstrapTestResponse(t, svc, t.Context(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
		t.Fatalf("signed keys = %#v, want [%s]", store.signedKeys, asset.StorageKey)
	}
	for _, file := range response.Files {
		if file.Path == ".anban-creator/project-style-reference.png" {
			return
		}
	}
	t.Fatalf("reference file missing from %#v", response.Files)
}

func TestBootstrapKeepsInheritedProjectStyleReferenceOutOfGenerationSettings(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	asset := referenceAssetFixture("asset-project-style", "user-1", DirectUploadPurposeProjectReference)
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	task := &model.Task{ID: "task-1", UserID: asset.UserID, ProjectID: "project-1", Type: model.PlatformSeednote}
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: task.Type, ReferenceImageAssetID: asset.ID})
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60}, zerolog.Nop())

	response, err := buildBootstrapTestResponse(t, svc, t.Context(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var settingsText string
	foundReference := false
	for _, file := range response.Files {
		switch file.Path {
		case ".anban-creator/project-style-reference.png":
			foundReference = true
		case ".anban-creator/settings.json":
			settingsText = file.Text
		}
	}
	if !foundReference {
		t.Fatal("project style reference was not provided for prompt analysis")
	}
	var settings struct {
		Seednote struct {
			Cover struct {
				Image struct {
					Refer string `json:"refer"`
				} `json:"image"`
			} `json:"cover"`
			Content struct {
				Image struct {
					Refer string `json:"refer"`
				} `json:"image"`
			} `json:"content"`
		} `json:"seednote"`
	}
	if err := json.Unmarshal([]byte(settingsText), &settings); err != nil {
		t.Fatalf("decode runtime settings: %v", err)
	}
	if settings.Seednote.Cover.Image.Refer != "" || settings.Seednote.Content.Image.Refer != "" {
		t.Fatalf("project style reference leaked into generation settings: %#v", settings.Seednote)
	}
}

func TestBootstrapKeepsTaskAndProjectStyleReferencesSeparateWhenBothExist(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	taskAsset := referenceAssetFixture("asset-task", "user-1", DirectUploadPurposeTaskReference)
	styleAsset := referenceAssetFixture("asset-style", "user-1", DirectUploadPurposeProjectReference)
	for _, asset := range []*model.Asset{taskAsset, styleAsset} {
		if err := repo.Assets().Create(t.Context(), asset); err != nil {
			t.Fatal(err)
		}
	}
	task := &model.Task{
		ID: "task-1", UserID: taskAsset.UserID, ProjectID: "project-1", Type: model.PlatformSeednote,
		ReferenceImageAssetID: taskAsset.ID,
	}
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: task.Type, ReferenceImageAssetID: styleAsset.ID})
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60}, zerolog.Nop())

	response, err := buildBootstrapTestResponse(t, svc, t.Context(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	var settingsText string
	for _, file := range response.Files {
		paths[file.Path] = true
		if file.Path == ".anban-creator/settings.json" {
			settingsText = file.Text
		}
	}
	for _, want := range []string{serveragent.TaskReferenceImagePath, serveragent.ProjectStyleReferenceImagePath} {
		if !paths[want] {
			t.Fatalf("bootstrap files = %#v, want %q", response.Files, want)
		}
	}
	if !strings.Contains(settingsText, `"refer":"`+serveragent.TaskReferenceImagePath+`"`) {
		t.Fatalf("settings do not bind direct task reference to generation: %s", settingsText)
	}
	if strings.Contains(settingsText, serveragent.ProjectStyleReferenceImagePath) {
		t.Fatalf("settings bind project style reference to generation: %s", settingsText)
	}
}

func TestBootstrapPreservesReferenceRepositoryFailureBeforeSigning(t *testing.T) {
	baseRepo := openBootstrapTestRepository(t)
	rootCause := errors.New("asset database unavailable")
	repo := &referenceAssetRepositoryOverride{
		Repository: baseRepo,
		assets:     &failingReferenceAssetRepository{AssetRepository: baseRepo.Assets(), err: rootCause},
	}
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformSeednote, ReferenceImageAssetID: "asset-1"}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60}, zerolog.Nop())

	_, err := buildBootstrapTestResponse(t, svc, t.Context(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, time.Now().Add(time.Hour))
	if !errors.Is(err, ErrReferenceAssetUnavailable) || !errors.Is(err, rootCause) {
		t.Fatalf("buildResponse error = %v, want unavailable and root cause", err)
	}
	if len(store.signedKeys) != 0 {
		t.Fatalf("repository failure reached signer: %#v", store.signedKeys)
	}
}

func TestBootstrapBoundsEverySignedDownloadToCredentialDeadline(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second).Add(200 * time.Millisecond)
	jobDeadline := now.Add(3700 * time.Millisecond)
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{TokenTTL: 10 * time.Minute, SignedURLTTL: 600, Store: store}, zerolog.Nop())
	svc.now = func() time.Time { return now }
	prefix := "uploads/users/user-1/projects/project-1/tasks/task-1"
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformEcommerce, Prompt: "topic", ReferenceImageAssetID: "asset-bootstrap"}
	createBootstrapAsset(t, repo, task.ReferenceImageAssetID, task.UserID, DirectUploadPurposeTaskReference, "reference.png", 10)
	createBootstrapAsset(t, repo, "attachment-bootstrap", task.UserID, DirectUploadPurposeAIEntryAttachment, "attachment.png", 10)
	task.SetInputAttachments([]model.EntryAttachment{
		{AssetID: "attachment-bootstrap", Type: "image", FileName: "attachment.png", ContentType: "image/png", Size: 10},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "read attachments/resume.pdf"},
		{Role: model.EntryAttachmentRoleResumeFile, Key: prefix + "/resume/run/attachments/resume.pdf", FileName: "resume.pdf"},
	})
	task.SetEcommerce(model.EcommerceConfig{ProductPhotos: []string{"https://bucket.oss-cn-x.aliyuncs.com/" + prefix + "/inputs/product.png"}})
	const resumeSessionID = "bba21f1d-70b8-4157-917b-f9802c2b1740"
	response, err := buildBootstrapTestResponse(t, svc, context.Background(), &model.TaskExecution{ID: "execution-1", ResumeSessionID: resumeSessionID}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, jobDeadline)
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
	if len(store.signedTTLs) != 3 {
		t.Fatalf("signed TTLs = %v, want reference plus unified input attachment and resume", store.signedTTLs)
	}
	for i, ttl := range store.signedTTLs {
		if ttl <= 0 || now.Add(time.Duration(ttl)*time.Second).After(claims.ExpiresAt.Time) {
			t.Fatalf("signed TTL[%d]=%d exceeds credential deadline %v from %v", i, ttl, claims.ExpiresAt.Time, now)
		}
	}

	before := len(store.signedKeys)
	svc.now = func() time.Time { return jobDeadline.Add(-500 * time.Millisecond) }
	if _, err := buildBootstrapTestResponse(t, svc, context.Background(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, jobDeadline); err == nil {
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
	if _, err := buildBootstrapTestResponse(t, svc, context.Background(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, jobDeadline); err == nil {
		t.Fatal("text-only bootstrap issued a token without a positive whole-second lifetime")
	}
}

func TestBootstrapAcceptsGenericDockerWorkloadIdentity(t *testing.T) {
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
	inputAssetID := uuid.NewString()
	keyFirstUploadID := uuid.NewString()
	createBootstrapAsset(t, repo, inputAssetID, userID, DirectUploadPurposeAIEntryAttachment, "input.png", 321)
	createBootstrapAsset(t, repo, keyFirstUploadID, userID, DirectUploadPurposeAIEntryAttachment, "key-first.png", 321)
	task := &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.PlatformSeednote, Status: model.TaskStatusRunning, Prompt: "topic"}
	task.SetInputAttachments([]model.EntryAttachment{
		{Type: "text", Text: "brief", FileName: "brief.txt"},
		{AssetID: inputAssetID, Type: "image", FileName: "input.png", ContentType: "image/png", Size: 321},
		{AssetID: keyFirstUploadID, Type: "image", FileName: "key-first.png", ContentType: "image/png", Size: 321},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "read attachments/foo.pdf"},
		{Role: model.EntryAttachmentRoleResumeFile, Key: taskPrefix + "/resume/run/attachments/foo.pdf", FileName: "foo.pdf"},
	})
	task.CurrentExecutionID = &executionID
	profile, registry := bootstrapTestProfile(t)
	task.ExecutionProfile = profile.ID
	task.AgentProfileSnapshot = profile.Snapshot()
	task.AgentProfileFingerprint, err = model.AgentProfileFingerprint(task.AgentProfileSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatal(err)
	}
	resumeSessionID := uuid.NewString()
	profiledExecution := model.NewTaskExecutionAgentProfile(task.AgentProfileSnapshot, task.AgentProfileFingerprint)
	profiledExecution.ID, profiledExecution.TaskID, profiledExecution.Attempt = executionID, taskID, 1
	profiledExecution.ResumeSessionID, profiledExecution.Target, profiledExecution.Status = resumeSessionID, "docker", model.TaskExecutionStarting
	profiledExecution.RuntimeScope, profiledExecution.RuntimeWorkload = "docker", "exec-1"
	if err := applyAgentPackIdentity(&profiledExecution, task.Type); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskExecutions().Create(ctx, &profiledExecution); err != nil {
		t.Fatal(err)
	}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	store := &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{MaxTurns: map[string]int{model.PlatformSeednote: 12}, TokenTTL: 10 * time.Minute, ActiveDeadline: 5 * time.Minute, Store: store, Registry: registry}, zerolog.Nop())
	identity := &serveragent.WorkloadIdentity{Target: "docker", RuntimeIdentity: model.RuntimeIdentity{Scope: "docker", Workload: "exec-1", InstanceID: "container-id"}, ExecutionID: executionID, TaskID: taskID, ProjectID: projectID, UserID: userID, Deadline: time.Now().Add(4 * time.Minute)}
	first, err := svc.Bootstrap(ctx, identity)
	if err != nil {
		t.Fatal(err)
	}
	resumeContextPath, _ := serveragent.ExecutionResumeContextPath(executionID)
	if first.ExecutionToken == "" || first.ExecutionID != executionID || first.TaskID != taskID || first.ProjectID != projectID || first.AgentFlag != "anban:seednote" || first.AutoMemoryDirectory != ".claude/memory" || first.ResumeSessionID != resumeSessionID || first.ResumeContextPath != resumeContextPath || first.MaxTurns != 12 {
		t.Fatalf("response = %#v", first)
	}
	if first.AgentPackID != "seednote" || first.AgentPackVersion != "1.0.1" || len(first.AgentPackDigest) != 64 || first.RuntimeAdapter != "standard" || first.RuntimeProfile != "seednote" {
		t.Fatalf("response Agent Pack identity = %#v", first)
	}
	if first.ExecutionProfile.Envs["ANTHROPIC_AUTH_TOKEN"] != "test-token" || first.ExecutionProfile.Envs["ANTHROPIC_BASE_URL"] != "https://anthropic.example.com" || first.ExecutionProfile.Envs["ANTHROPIC_MODEL"] != "claude-test" || len(first.ExecutionProfile.Envs) != 7 {
		t.Fatalf("runtime environment = %#v, want only allowlisted Claude values", first.ExecutionProfile.Envs)
	}
	if len(first.Files) < 2 {
		t.Fatalf("files = %#v", first.Files)
	}
	paths := map[string]BootstrapFile{}
	for _, file := range first.Files {
		paths[file.Path] = file
	}
	settings := paths[".anban-creator/settings.json"]
	if !settings.ReplaceExisting {
		t.Fatal("runtime settings must be replaceable across resumed executions")
	}
	for bootstrapPath, file := range paths {
		if bootstrapPath != ".anban-creator/settings.json" && file.ReplaceExisting {
			t.Fatalf("bootstrap path %q unexpectedly permits replacement", bootstrapPath)
		}
	}
	resumeAttachmentPath, _ := serveragent.ExecutionResumeAttachmentPath(executionID, "foo.pdf")
	for _, path := range []string{".anban-creator/input-attachments/attachment_01_brief.txt", ".anban-creator/input-attachments/attachment_02_input.png", ".anban-creator/input-attachments/attachment_03_key-first.png", ".anban-creator/input-attachments/index.json", resumeContextPath, resumeAttachmentPath} {
		if _, ok := paths[path]; !ok {
			t.Fatalf("missing bootstrap path %q in %#v", path, first.Files)
		}
	}
	if _, exists := paths[path.Join(path.Dir(resumeAttachmentPath), "05-foo.pdf")]; exists {
		t.Fatal("bootstrap renamed persisted resume attachment")
	}
	if _, exists := paths[serveragent.TaskReferenceImagePath]; exists {
		t.Fatal("legacy reference_image_url produced a task reference file")
	}
	for _, rawURL := range store.ownedChecks {
		if strings.Contains(rawURL, "/inputs/reference.png") {
			t.Fatalf("legacy reference_image_url reached storage URL ownership parsing: %q", rawURL)
		}
	}
	if paths[".anban-creator/input-attachments/attachment_02_input.png"].DownloadURL == "" || paths[".anban-creator/input-attachments/attachment_03_key-first.png"].DownloadURL == "" {
		t.Fatal("private objects were not signed")
	}
	if !strings.Contains(paths[".anban-creator/settings.json"].Text, `"seednote"`) {
		t.Fatalf("settings did not use runtime app config: %s", paths[".anban-creator/settings.json"].Text)
	}
	claims, err := tokens.Validate(first.ExecutionToken)
	if err != nil {
		t.Fatal(err)
	}
	if claims.ExpiresAt == nil || claims.ExpiresAt.Time.After(identity.Deadline) {
		t.Fatalf("token expiry = %v, exceeds workload deadline", claims.ExpiresAt)
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
	if retryClaims.ExecutionID != executionID || retryClaims.ExpiresAt == nil || retryClaims.ExpiresAt.Time.After(identity.Deadline) {
		t.Fatalf("retry claims = %#v", retryClaims)
	}
	found, _ := repo.TaskExecutions().FindByID(ctx, executionID)
	if found.Status != model.TaskExecutionRunning || !found.Started || found.RuntimeInstanceID != "container-id" || found.StartedAt == nil || found.LastHeartbeatAt == nil {
		t.Fatalf("execution = %#v", found)
	}
	if _, err := svc.Bootstrap(ctx, &serveragent.WorkloadIdentity{Target: "docker", RuntimeIdentity: model.RuntimeIdentity{Scope: "docker", Workload: "exec-1", InstanceID: "replacement-id"}, ExecutionID: executionID, TaskID: taskID, ProjectID: projectID, UserID: userID, Deadline: time.Now().Add(time.Minute)}); err == nil {
		t.Fatal("different runtime instance stole running execution")
	}
	crossProvider := *identity
	crossProvider.Target = "kubernetes"
	if _, err := svc.Bootstrap(ctx, &crossProvider); err == nil {
		t.Fatal("cross-provider workload with identical runtime identity accepted")
	}
	taskSvc := newTestTaskService(repo, nil, nil, nil, "", nil, nil)
	if err := taskSvc.ValidateAgentExecutionAccess(ctx, userID, projectID, taskID, executionID); err != nil {
		t.Fatalf("current execution rejected: %v", err)
	}
	if err := taskSvc.ValidateAgentExecutionAccess(ctx, userID, projectID, taskID, uuid.NewString()); err == nil {
		t.Fatal("stale execution token accepted")
	}
}

func TestBootstrapRejectsExpiredWorkloadDeadlineBeforeBuildingResponse(t *testing.T) {
	svc := &AgentBootstrapService{}
	if _, err := svc.buildResponse(context.Background(), nil, nil, nil, time.Now().Add(-time.Second)); err == nil {
		t.Fatal("expired workload deadline accepted")
	}
}

type bootstrapReadTrackingRepository struct {
	repository.Repository
	reads int
}

type bootstrapReadTrackingExecutions struct {
	repository.TaskExecutionRepository
	owner *bootstrapReadTrackingRepository
}

func (r *bootstrapReadTrackingRepository) TaskExecutions() repository.TaskExecutionRepository {
	return &bootstrapReadTrackingExecutions{owner: r}
}

func (r *bootstrapReadTrackingExecutions) FindByID(context.Context, string) (*model.TaskExecution, error) {
	r.owner.reads++
	return nil, errors.New("unexpected execution read")
}

func TestBootstrapRejectsIncompleteNonCanonicalOrExpiredIdentityBeforeRepositoryAccess(t *testing.T) {
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	base := serveragent.WorkloadIdentity{
		Target:          "docker",
		RuntimeIdentity: model.RuntimeIdentity{Scope: "daemon-a", Workload: "exec-1", InstanceID: "container-id"},
		ExecutionID:     "execution-1",
		TaskID:          "task-1",
		ProjectID:       "project-1",
		UserID:          "user-1",
		Deadline:        time.Now().Add(time.Minute),
	}
	for _, tc := range []struct {
		name   string
		mutate func(*serveragent.WorkloadIdentity)
	}{
		{name: "target", mutate: func(i *serveragent.WorkloadIdentity) { i.Target = "" }},
		{name: "scope", mutate: func(i *serveragent.WorkloadIdentity) { i.Scope = "" }},
		{name: "workload", mutate: func(i *serveragent.WorkloadIdentity) { i.Workload = "" }},
		{name: "instance", mutate: func(i *serveragent.WorkloadIdentity) { i.InstanceID = "" }},
		{name: "execution", mutate: func(i *serveragent.WorkloadIdentity) { i.ExecutionID = "" }},
		{name: "task", mutate: func(i *serveragent.WorkloadIdentity) { i.TaskID = "" }},
		{name: "project", mutate: func(i *serveragent.WorkloadIdentity) { i.ProjectID = "" }},
		{name: "user", mutate: func(i *serveragent.WorkloadIdentity) { i.UserID = "" }},
		{name: "expired deadline", mutate: func(i *serveragent.WorkloadIdentity) { i.Deadline = time.Now().Add(-time.Second) }},
		{name: "non-canonical target", mutate: func(i *serveragent.WorkloadIdentity) { i.Target = " docker" }},
		{name: "non-canonical instance", mutate: func(i *serveragent.WorkloadIdentity) { i.InstanceID = "container-id " }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := base
			tc.mutate(&identity)
			repo := &bootstrapReadTrackingRepository{}
			svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{TokenTTL: time.Minute}, zerolog.Nop())
			response, err := svc.Bootstrap(t.Context(), &identity)
			if !errors.Is(err, ErrAgentBootstrapConflict) || response != nil {
				t.Fatalf("Bootstrap response/error = %#v/%v, want nil conflict", response, err)
			}
			if repo.reads != 0 {
				t.Fatalf("invalid identity caused %d repository reads", repo.reads)
			}
		})
	}
}

type bootstrapRaceState struct {
	mu                sync.Mutex
	execution         model.TaskExecution
	task              model.Task
	project           model.Project
	user              model.User
	transitionEntered chan struct{}
	transitionRelease <-chan struct{}
	txFindOnce        sync.Once
	txFindEntered     chan struct{}
	txFindRelease     <-chan struct{}
}

type bootstrapRaceRepository struct {
	repository.Repository
	state *bootstrapRaceState
	inTx  bool
}

type bootstrapRaceUsers struct {
	repository.UserRepository
	state *bootstrapRaceState
}

type bootstrapRaceProjects struct {
	repository.ProjectRepository
	state *bootstrapRaceState
}

type bootstrapRaceTasks struct {
	repository.TaskRepository
	state *bootstrapRaceState
}

type bootstrapRaceExecutions struct {
	repository.TaskExecutionRepository
	state *bootstrapRaceState
	inTx  bool
}

func (r *bootstrapRaceRepository) Users() repository.UserRepository {
	return &bootstrapRaceUsers{state: r.state}
}

func (r *bootstrapRaceRepository) Projects() repository.ProjectRepository {
	return &bootstrapRaceProjects{state: r.state}
}

func (r *bootstrapRaceRepository) Tasks() repository.TaskRepository {
	return &bootstrapRaceTasks{state: r.state}
}

func (r *bootstrapRaceRepository) TaskExecutions() repository.TaskExecutionRepository {
	return &bootstrapRaceExecutions{state: r.state, inTx: r.inTx}
}

func (r *bootstrapRaceRepository) WithTx(_ context.Context, fn func(repository.Repository) error) error {
	return fn(&bootstrapRaceRepository{state: r.state, inTx: true})
}

func (r *bootstrapRaceUsers) FindByID(_ context.Context, id string) (*model.User, error) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if id != r.state.user.ID {
		return nil, gorm.ErrRecordNotFound
	}
	copy := r.state.user
	return &copy, nil
}

func (r *bootstrapRaceProjects) FindByID(_ context.Context, id string) (*model.Project, error) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if id != r.state.project.ID {
		return nil, gorm.ErrRecordNotFound
	}
	copy := r.state.project
	return &copy, nil
}

func (r *bootstrapRaceTasks) FindByID(_ context.Context, id string) (*model.Task, error) {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if id != r.state.task.ID {
		return nil, gorm.ErrRecordNotFound
	}
	copy := r.state.task
	return &copy, nil
}

func (r *bootstrapRaceTasks) UpdateHeartbeat(context.Context, string) error { return nil }

func (r *bootstrapRaceExecutions) FindByID(_ context.Context, id string) (*model.TaskExecution, error) {
	if r.inTx && r.state.txFindEntered != nil {
		r.state.txFindOnce.Do(func() {
			close(r.state.txFindEntered)
			<-r.state.txFindRelease
		})
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if id != r.state.execution.ID {
		return nil, gorm.ErrRecordNotFound
	}
	copy := r.state.execution
	return &copy, nil
}

func (r *bootstrapRaceExecutions) SetRuntimeIdentity(_ context.Context, id string, identity model.RuntimeIdentity) error {
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if id != r.state.execution.ID {
		return gorm.ErrRecordNotFound
	}
	if r.state.execution.Status != model.TaskExecutionStarting && r.state.execution.Status != model.TaskExecutionRunning {
		return repository.ErrRuntimeIdentityInactive
	}
	for _, pair := range [][2]string{{r.state.execution.RuntimeScope, identity.Scope}, {r.state.execution.RuntimeWorkload, identity.Workload}, {r.state.execution.RuntimeInstanceID, identity.InstanceID}} {
		if pair[0] != "" && pair[1] != "" && pair[0] != pair[1] {
			return repository.ErrRuntimeIdentityConflict
		}
	}
	if identity.Scope != "" {
		r.state.execution.RuntimeScope = identity.Scope
	}
	if identity.Workload != "" {
		r.state.execution.RuntimeWorkload = identity.Workload
	}
	if identity.InstanceID != "" {
		r.state.execution.RuntimeInstanceID = identity.InstanceID
	}
	return nil
}

func (r *bootstrapRaceExecutions) Transition(_ context.Context, id string, from []string, to string, change model.ExecutionTransition) (bool, error) {
	if r.state.transitionEntered != nil {
		r.state.transitionEntered <- struct{}{}
		<-r.state.transitionRelease
	}
	r.state.mu.Lock()
	defer r.state.mu.Unlock()
	if id != r.state.execution.ID || !slices.Contains(from, r.state.execution.Status) {
		return false, nil
	}
	r.state.execution.Status = to
	r.state.execution.Started = change.Started
	if change.RuntimeInstanceID != "" {
		r.state.execution.RuntimeInstanceID = change.RuntimeInstanceID
	}
	return true, nil
}

func (r *bootstrapRaceExecutions) UpdateHeartbeat(context.Context, string, time.Time) error {
	return nil
}

func newBootstrapRaceFixture(t *testing.T) (*AgentBootstrapService, *bootstrapRaceState, *serveragent.WorkloadIdentity) {
	t.Helper()
	executionID := "execution-1"
	profile, registry := bootstrapTestProfile(t)
	snapshot := profile.Snapshot()
	fingerprint, err := model.AgentProfileFingerprint(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	profiledExecution := model.NewTaskExecutionAgentProfile(snapshot, fingerprint)
	profiledExecution.ID, profiledExecution.TaskID = executionID, "task-1"
	profiledExecution.Target, profiledExecution.Status = "docker", model.TaskExecutionStarting
	profiledExecution.RuntimeScope, profiledExecution.RuntimeWorkload = "daemon-a", "exec-1"
	state := &bootstrapRaceState{
		execution: profiledExecution,
		task:      model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle, ExecutionProfile: profile.ID, AgentProfileSnapshot: snapshot, AgentProfileFingerprint: fingerprint, Status: model.TaskStatusRunning, Prompt: "topic", SkipReferenceImage: true, CurrentExecutionID: &executionID},
		project:   model.Project{ID: "project-1", UserID: "user-1", Platform: model.PlatformArticle, Name: "project", Status: model.ProjectStatusActive},
		user:      model.User{ID: "user-1"},
	}
	repo := &bootstrapRaceRepository{state: state}
	tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{TokenTTL: time.Minute, Registry: registry}, zerolog.Nop())
	identity := &serveragent.WorkloadIdentity{Target: "docker", RuntimeIdentity: model.RuntimeIdentity{Scope: "daemon-a", Workload: "exec-1", InstanceID: "container-a"}, ExecutionID: executionID, TaskID: "task-1", ProjectID: "project-1", UserID: "user-1", Deadline: time.Now().Add(time.Minute)}
	return svc, state, identity
}

func TestBootstrapConcurrentSameInstanceIsIdempotent(t *testing.T) {
	svc, state, identity := newBootstrapRaceFixture(t)
	state.transitionEntered = make(chan struct{}, 2)
	release := make(chan struct{})
	state.transitionRelease = release
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := svc.Bootstrap(t.Context(), identity)
			errs <- err
		}()
	}
	<-state.transitionEntered
	<-state.transitionEntered
	close(release)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("same-instance bootstrap failed: %v", err)
		}
	}
}

func TestBootstrapConcurrentDifferentInstancesHasOneWinnerWithoutOverwrite(t *testing.T) {
	svc, state, first := newBootstrapRaceFixture(t)
	second := *first
	second.InstanceID = "container-b"
	start := make(chan struct{})
	type result struct {
		instance string
		err      error
	}
	results := make(chan result, 2)
	for _, identity := range []*serveragent.WorkloadIdentity{first, &second} {
		go func(identity *serveragent.WorkloadIdentity) {
			<-start
			_, err := svc.Bootstrap(t.Context(), identity)
			results <- result{instance: identity.InstanceID, err: err}
		}(identity)
	}
	close(start)
	successes := 0
	winner := ""
	for range 2 {
		result := <-results
		if result.err == nil {
			successes++
			winner = result.instance
		}
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if successes != 1 || state.execution.RuntimeInstanceID != winner {
		t.Fatalf("successes/winner/persisted = %d/%q/%q", successes, winner, state.execution.RuntimeInstanceID)
	}
}

func TestBootstrapAndReconcilerInstanceRaceNeverOverwrite(t *testing.T) {
	svc, state, identity := newBootstrapRaceFixture(t)
	state.txFindEntered = make(chan struct{})
	release := make(chan struct{})
	state.txFindRelease = release
	bootstrapErr := make(chan error, 1)
	go func() {
		_, err := svc.Bootstrap(t.Context(), identity)
		bootstrapErr <- err
	}()
	<-state.txFindEntered
	reconcileErr := (&TaskService{repo: svc.repo}).RecordExecutionInstance(t.Context(), identity.ExecutionID, "reconciler-container")
	close(release)
	bootErr := <-bootstrapErr
	state.mu.Lock()
	persisted := state.execution.RuntimeInstanceID
	state.mu.Unlock()
	successes := 0
	if bootErr == nil {
		successes++
		if persisted != identity.InstanceID {
			t.Fatalf("bootstrap won but persisted instance = %q", persisted)
		}
	}
	if reconcileErr == nil {
		successes++
		if persisted != "reconciler-container" {
			t.Fatalf("reconciler won but persisted instance = %q", persisted)
		}
	}
	if successes != 1 {
		t.Fatalf("bootstrap/reconciler errors = %v/%v, want exactly one winner", bootErr, reconcileErr)
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
		{{Path: "attachments/input.txt", Text: "input", Mode: 0644, ReplaceExisting: true}},
	} {
		if err := ValidateBootstrapFiles(files); err == nil {
			t.Fatalf("unsafe files accepted: %#v", files)
		}
	}
}

func TestBuildMontageBootstrapFiles(t *testing.T) {
	svc := &AgentBootstrapService{cfg: AgentBootstrapConfig{
		MontageEnv:              map[string]string{"NEW_PROVIDER_TOKEN": "future-secret"},
		MontageToolPolicy:       map[string]config.MontageToolCapabilityPolicy{},
		MontagePipelineDefaults: map[string]map[string]any{},
	}}
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
	if got := svc.montageEnv(&model.Task{Type: model.PlatformMontage}); got["NEW_PROVIDER_TOKEN"] != "future-secret" {
		t.Fatalf("Montage env = %#v, want future provider key", got)
	}
	if got := svc.montageEnv(&model.Task{Type: model.PlatformArticle}); len(got) != 0 {
		t.Fatalf("article env = %#v, want empty", got)
	}
}

func TestBuildEcommerceProductBootstrapFilesUsesUnifiedAttachmentIndex(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformEcommerce}
	createBootstrapAsset(t, repo, "product-upload", task.UserID, DirectUploadPurposeEcommercePhoto, "product.png", 1)
	attachment := model.EntryAttachment{AssetID: "product-upload", Type: "image", Role: model.EntryAttachmentRoleEcommerceProduct, FileName: "product.png", ContentType: "image/png", Size: 1}

	files, err := svc.buildAttachmentFiles(context.Background(), "execution-1", task, []model.EntryAttachment{attachment}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0].Path != ".anban-creator/input-attachments/attachment_01_product.png" || files[0].DownloadURL == "" || files[1].Path != ".anban-creator/input-attachments/index.json" {
		t.Fatalf("product attachment files = %#v", files)
	}
	if strings.Contains(files[0].Path+files[1].Path, ".anban-creator/products") {
		t.Fatalf("legacy product directory remained in %#v", files)
	}
}

func TestAgentBootstrapAttachmentTypeIndexPreservesSourceOrder(t *testing.T) {
	svc := &AgentBootstrapService{}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	attachments := []model.EntryAttachment{
		{Type: "document", Text: "document", FileName: "brief.pdf"},
		{Type: "image", Text: "image one", FileName: "first.png"},
		{Type: "image", Text: "image two", FileName: "second.png"},
		{Type: "text", Text: "notes", FileName: "notes.txt"},
	}

	files, err := svc.buildAttachmentFiles(t.Context(), "execution-1", task, attachments, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("files = %#v", files)
	}
	for i, want := range []string{
		".anban-creator/input-attachments/attachment_01_brief.pdf",
		".anban-creator/input-attachments/attachment_02_first.png",
		".anban-creator/input-attachments/attachment_03_second.png",
		".anban-creator/input-attachments/attachment_04_notes.txt",
	} {
		if files[i].Path != want {
			t.Fatalf("file %d path = %q, want %q", i, files[i].Path, want)
		}
	}
	var index []struct {
		Index     int    `json:"index"`
		Type      string `json:"type"`
		TypeIndex int    `json:"type_index"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(files[4].Text), &index); err != nil {
		t.Fatal(err)
	}
	wantTypes := []string{"document", "image", "image", "text"}
	wantTypeIndexes := []int{1, 1, 2, 1}
	for i := range index {
		if index[i].Index != i+1 || index[i].Type != wantTypes[i] || index[i].TypeIndex != wantTypeIndexes[i] || index[i].Path != files[i].Path {
			t.Fatalf("index[%d] = %#v", i, index[i])
		}
	}
}

func TestAgentBootstrapAttachmentIndexIsCompactAroundResumeEntries(t *testing.T) {
	svc := &AgentBootstrapService{}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	attachments := []model.EntryAttachment{
		{Type: "text", Text: "first", FileName: "first.txt"},
		{Role: model.EntryAttachmentRoleResumeLatest, Text: "resume"},
		{Type: "image", Text: "second", FileName: "second.png"},
	}
	files, err := svc.buildAttachmentFiles(t.Context(), "execution-1", task, attachments, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var index []struct {
		Index     int    `json:"index"`
		TypeIndex int    `json:"type_index"`
		Path      string `json:"path"`
	}
	if err := json.Unmarshal([]byte(files[len(files)-1].Text), &index); err != nil {
		t.Fatal(err)
	}
	if len(index) != 2 || index[0].Index != 1 || index[1].Index != 2 || index[0].TypeIndex != 1 || index[1].TypeIndex != 1 {
		t.Fatalf("index=%#v", index)
	}
}

func TestAgentBootstrapRejectsAmbiguousAssetAttachmentIdentity(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	asset := referenceAssetFixture("asset-historical", task.UserID, DirectUploadPurposeAIEntryAttachment)
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.buildAttachmentFiles(t.Context(), "execution-1", task, []model.EntryAttachment{{AssetID: asset.ID, URL: "https://attacker.invalid/x", Key: "victim/key"}}, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("ambiguous asset attachment identity was accepted")
	}
	if len(store.signedKeys) != 0 {
		t.Fatalf("ambiguous asset attachment reached signer: %#v", store.signedKeys)
	}
}

func TestAgentBootstrapAssetAttachmentUsesRepositoryIdentity(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	asset := referenceAssetFixture("asset-material", task.UserID, DirectUploadPurposeTaskReference)
	if err := repo.Assets().Create(t.Context(), asset); err != nil {
		t.Fatal(err)
	}
	attachment := model.EntryAttachment{AssetID: asset.ID, Type: "document", FileName: "forged.pdf", ContentType: "application/pdf", Size: 1}

	files, err := svc.buildAttachmentFiles(t.Context(), "execution-1", task, []model.EntryAttachment{attachment}, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("buildAttachmentFiles: %v", err)
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != asset.StorageKey {
		t.Fatalf("signed keys = %#v, want repository key %q", store.signedKeys, asset.StorageKey)
	}
	if len(files) != 2 || files[0].DownloadURL == "" || files[0].ExpectedSize != asset.Size || files[0].Path != ".anban-creator/input-attachments/attachment_01_"+asset.FileName {
		t.Fatalf("files = %#v", files)
	}
}

func TestAgentBootstrapAssetAttachmentRejectsUntrustedAsset(t *testing.T) {
	for _, tc := range []struct {
		name    string
		owner   string
		purpose string
	}{
		{name: "foreign owner", owner: "user-2", purpose: DirectUploadPurposeTaskReference},
		{name: "wrong purpose", owner: "user-1", purpose: DirectUploadPurposeProjectReference},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := openBootstrapTestRepository(t)
			store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
			svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
			task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
			asset := referenceAssetFixture("asset-material", tc.owner, tc.purpose)
			if err := repo.Assets().Create(t.Context(), asset); err != nil {
				t.Fatal(err)
			}
			attachment := model.EntryAttachment{AssetID: asset.ID, Type: "image", FileName: asset.FileName, ContentType: asset.ContentType, Size: asset.Size}

			if _, err := svc.buildAttachmentFiles(t.Context(), "execution-1", task, []model.EntryAttachment{attachment}, time.Now().Add(time.Hour)); err == nil {
				t.Fatal("untrusted asset attachment was accepted")
			}
			if len(store.signedKeys) != 0 {
				t.Fatalf("untrusted asset reached signer: %#v", store.signedKeys)
			}
		})
	}
}

func TestAgentBootstrapRejectsNonResumeStorageAttachmentWithoutAssetID(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	svc := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	key := "assets/users/user-1/upload-1/input.png"
	createBootstrapAsset(t, repo, "upload-1", task.UserID, DirectUploadPurposeAIEntryAttachment, "input.png", 1)

	for _, attachment := range []model.EntryAttachment{
		{Type: "image", URL: "https://bucket.oss-cn-x.aliyuncs.com/" + key, FileName: "input.png", ContentType: "image/png", Size: 1},
		{Type: "image", UploadID: "upload-1", Key: key, FileName: "input.png", ContentType: "image/png", Size: 1},
	} {
		before := len(store.signedKeys)
		if _, err := svc.buildAttachmentFiles(t.Context(), "execution-1", task, []model.EntryAttachment{attachment}, time.Now().Add(time.Hour)); err == nil {
			t.Fatalf("attachment without immutable asset identity was accepted: %#v", attachment)
		}
		if len(store.signedKeys) != before {
			t.Fatalf("attachment without asset identity reached signer: %#v", store.signedKeys)
		}
	}
}

func TestBuildResponseSignsAssetBackedReferenceImage(t *testing.T) {
	repo := openBootstrapTestRepository(t)
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{ownedPrefix: "https://bucket.oss-cn-x.aliyuncs.com/"}}
	tokens, err := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	svc := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: 10 * time.Minute, SignedURLTTL: 60}, zerolog.Nop())
	svc.now = func() time.Time { return now }
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformArticle, Prompt: "write", Status: model.TaskStatusRunning}
	attachment := model.EntryAttachment{AssetID: "reference-upload", Type: "image", FileName: "reference.png", ContentType: "image/png", Size: 1}
	task.ReferenceImageAssetID = attachment.AssetID
	task.SetInputAttachments([]model.EntryAttachment{attachment})
	createBootstrapAsset(t, repo, attachment.AssetID, task.UserID, DirectUploadPurposeAIEntryAttachment, attachment.FileName, 1)

	response, err := buildBootstrapTestResponse(t, svc, context.Background(), &model.TaskExecution{ID: "execution-1"}, task, &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type}, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("build response: %v", err)
	}
	paths := map[string]BootstrapFile{}
	for _, file := range response.Files {
		paths[file.Path] = file
	}
	if paths[serveragent.TaskReferenceImagePath].DownloadURL == "" || len(store.signedKeys) != 2 {
		t.Fatalf("files = %#v, signed keys = %#v", response.Files, store.signedKeys)
	}
}

func TestBootstrapResumeAttachmentSigningRequiresCurrentTaskNamespace(t *testing.T) {
	store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
	svc := &AgentBootstrapService{cfg: AgentBootstrapConfig{Store: store, SignedURLTTL: 60}}
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1"}
	deadline := time.Now().Add(time.Hour)
	allowed := "uploads/users/user-1/projects/project-1/tasks/task-1/resume/run/attachments/input.png"
	if _, err := svc.signedResumeAttachmentURL(t.Context(), task, allowed, deadline); err != nil {
		t.Fatalf("current task resume attachment rejected: %v", err)
	}
	for _, key := range []string{
		"uploads/users/user-1/projects/project-1/tasks/task-1/inputs/input.png",
		"uploads/users/user-1/projects/project-1/tasks/other/resume/run/attachments/input.png",
		"uploads/users/user-2/projects/project-1/tasks/task-1/resume/run/attachments/input.png",
		"uploads/users/user-1/projects/project-1/tasks/task-1/resume/../secret.png",
	} {
		if _, err := svc.signedResumeAttachmentURL(t.Context(), task, key, deadline); err == nil {
			t.Fatalf("out-of-scope resume attachment accepted: %q", key)
		}
	}
	if len(store.signedKeys) != 1 || store.signedKeys[0] != allowed {
		t.Fatalf("signed keys = %#v, want only %q", store.signedKeys, allowed)
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

func createBootstrapAsset(t *testing.T, repo repository.Repository, id, userID, purpose, fileName string, size int64) {
	t.Helper()
	now := time.Now()
	stagingKey := path.Join("uploads/pending", userID, id, fileName)
	if err := repo.UploadSessions().Create(t.Context(), &model.UploadSession{
		ID: id, UserID: userID, Purpose: purpose, StagingKey: stagingKey,
		FileName: fileName, ContentType: "image/png", Size: size,
		Status: model.UploadSessionFinalized, ExpiresAt: now.Add(time.Hour), FinalizationETag: "etag-" + id, AssetID: id, FinalizedAt: &now,
	}); err != nil {
		t.Fatalf("create upload session: %v", err)
	}
	if err := repo.Assets().Create(t.Context(), &model.Asset{
		ID: id, UserID: userID, Purpose: purpose, StorageKey: path.Join("assets/users", userID, id, fileName),
		FileName: fileName, ContentType: "image/png", Size: size, ETag: "etag-" + id,
	}); err != nil {
		t.Fatalf("create asset: %v", err)
	}
}
