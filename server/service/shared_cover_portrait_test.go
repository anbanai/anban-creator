package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestCreateManualSharedCoverPortrait(t *testing.T) {
	for _, platform := range []string{model.PlatformWechat, model.PlatformSeednote, model.PlatformMontage, model.PlatformHypit} {
		for _, selected := range []bool{false, true} {
			t.Run(platform+map[bool]string{false: "/disabled", true: "/selected"}[selected], func(t *testing.T) {
				svc, repo := setupTaskServiceWithEnqueuer(t)
				svc.SetMontageConfig(montageIntegrationConfig())
				svc.SetHypitConfig(hypitTestConfig())
				svc.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: platform, Image: "image@sha256:pinned"}})
				userID := uuid.NewString()
				ensureTestUser(t, repo, userID)
				projectID := createTestProject(t, repo, userID, platform)
				portrait := referenceAssetFixture("portrait", userID, DirectUploadPurposeProjectPortraitReference)
				product := referenceAssetFixture("product", userID, DirectUploadPurposeTaskReference)
				style := referenceAssetFixture("style", userID, DirectUploadPurposeProjectReference)
				for _, asset := range []*model.Asset{portrait, product, style} {
					seedReferenceAsset(t, repo, asset)
				}
				project, err := repo.Projects().FindByID(t.Context(), projectID)
				if err != nil {
					t.Fatal(err)
				}
				project.PortraitReferenceImageAssetID, project.ReferenceImageAssetID = portrait.ID, style.ID
				if err := repo.Projects().Update(t.Context(), project); err != nil {
					t.Fatal(err)
				}
				svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, nil))
				params := CreateManualParams{UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Prompt: "create cover", ReferenceImageAssetID: product.ID, CoverUsePortrait: selected}
				if platform != model.PlatformWechat {
					off := false
					params.ArticleWithCover = &off // Article-only controls must not disable other covers.
				}
				if platform == model.PlatformMontage {
					params.MontageInput = &model.MontageInput{Brief: "create video"}
				}
				if platform == model.PlatformHypit {
					params.HypitInput = &model.HypitInput{Brief: "replicate", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch?v=x"}}
				}
				tasks, err := svc.CreateManual(t.Context(), params)
				if err != nil {
					t.Fatal(err)
				}
				task := tasks[0]
				if task.CoverUsePortrait != selected {
					t.Fatalf("cover selection = %t", task.CoverUsePortrait)
				}
				if task.ReferenceImageAssetID != product.ID || task.ProjectSnapshot.Data().ReferenceImageAssetID != style.ID || projectPortraitReferenceAssetID(task) != portrait.ID {
					t.Fatalf("independent references lost: task=%q style=%q portrait=%q", task.ReferenceImageAssetID, task.ProjectSnapshot.Data().ReferenceImageAssetID, projectPortraitReferenceAssetID(task))
				}
				project.PortraitReferenceImageAssetID = ""
				if err := repo.Projects().Update(t.Context(), project); err != nil {
					t.Fatal(err)
				}
				if projectPortraitReferenceAssetID(task) != portrait.ID {
					t.Fatal("snapshot portrait changed after project edit")
				}
				store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
				tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
				bootstrap := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60, Hypit: hypitTestConfig()}, zerolog.Nop())
				response, err := buildBootstrapTestResponse(t, bootstrap, t.Context(), &model.TaskExecution{ID: uuid.NewString()}, task, project, time.Now().Add(time.Hour))
				if err != nil {
					t.Fatal(err)
				}
				paths := map[string]string{}
				for _, file := range response.Files {
					paths[file.Path] = file.DownloadURL
				}
				for _, path := range []string{".anban-creator/project-portrait-reference.png", ".anban-creator/project-style-reference.png", ".anban-creator/task-reference.png"} {
					if paths[path] == "" {
						t.Fatalf("missing reference %s", path)
					}
				}
				wantControl := "cover_portrait=disabled"
				if selected {
					wantControl = "cover_portrait=required_project_portrait"
				}
				if !strings.Contains(response.Prompt, wantControl) {
					t.Fatalf("missing runtime control: %s", response.Prompt)
				}
				if platform == model.PlatformMontage && !strings.Contains(response.Prompt, "Video material reference: .anban-creator/task-reference.png") {
					t.Fatal("cover selection changed Montage video material")
				}
				// Exact clones keep the frozen identity even after the project portrait is removed.
				task.Status = model.TaskStatusCompleted
				if err := repo.Tasks().Update(t.Context(), task); err != nil {
					t.Fatal(err)
				}
				clones, err := svc.Clone(t.Context(), task.ID, CloneTaskParams{ExecutionProfile: "effective"})
				if err != nil {
					t.Fatal(err)
				}
				if len(clones) != 1 || clones[0].CoverUsePortrait != selected || projectPortraitReferenceAssetID(clones[0]) != portrait.ID || clones[0].Prompt != params.Prompt {
					t.Fatalf("clone lost cover choice, frozen portrait or user prompt: %+v", clones)
				}

			})
		}
	}
}

func TestCreateManualRejectsUnavailableCoverPortrait(t *testing.T) {
	for _, tc := range []struct {
		name, platform, taskType string
		portrait, coverOff       bool
	}{
		{name: "missing", platform: model.PlatformSeednote},
		{name: "article cover off", platform: model.PlatformWechat, portrait: true, coverOff: true},
		{name: "viral analysis", platform: model.PlatformSeednote, taskType: model.TaskTypeViralAnalysis, portrait: true},
		{name: "unsupported", platform: model.PlatformMoments, portrait: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := setupTaskServiceWithEnqueuer(t)
			userID := uuid.NewString()
			ensureTestUser(t, repo, userID)
			projectID := createTestProject(t, repo, userID, tc.platform)
			if tc.portrait {
				portrait := referenceAssetFixture("portrait", userID, DirectUploadPurposeProjectPortraitReference)
				seedReferenceAsset(t, repo, portrait)
				project, _ := repo.Projects().FindByID(t.Context(), projectID)
				project.PortraitReferenceImageAssetID = portrait.ID
				if err := repo.Projects().Update(t.Context(), project); err != nil {
					t.Fatal(err)
				}
			}
			svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, nil))
			p := CreateManualParams{UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", CoverUsePortrait: true, RequestedTaskType: tc.taskType}
			if tc.coverOff {
				off := false
				p.ArticleWithCover = &off
			}
			if _, err := svc.CreateManual(t.Context(), p); !errors.Is(err, ErrCoverPortraitUnavailable) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSelectedCoverPortraitRequiresFrozenPortraitAtRuntime(t *testing.T) {
	_, repo := setupTaskServiceWithEnqueuer(t)
	for _, platform := range []string{model.PlatformWechat, model.PlatformSeednote, model.PlatformMontage, model.PlatformHypit} {
		task := &model.Task{Type: platform, CoverUsePortrait: true}
		if _, err := resolveProjectPortraitReferenceAsset(t.Context(), repo, task); !errors.Is(err, ErrCoverPortraitUnavailable) {
			t.Fatalf("%s missing frozen portrait error=%v", platform, err)
		}
	}
}

func TestSharedCoverPortraitPlanLifecycle(t *testing.T) {
	for _, platform := range []string{model.PlatformWechat, model.PlatformSeednote, model.PlatformMontage, model.PlatformHypit} {
		t.Run(platform, func(t *testing.T) {
			tasks, repo := setupTaskServiceWithEnqueuer(t)
			tasks.SetMontageConfig(montageIntegrationConfig())
			tasks.SetHypitConfig(hypitTestConfig())
			tasks.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: platform, Image: "image@sha256:pinned"}})
			plans := newTestPlanService(t, repo)
			plans.SetMontageCapabilityService(NewMontageCapabilityService(montageIntegrationConfig()))
			plans.SetHypitCapabilityService(NewHypitCapabilityService(hypitTestConfig()))
			userID := uuid.NewString()
			projectID := createTestProject(t, repo, userID, platform)
			portrait := referenceAssetFixture("portrait", userID, DirectUploadPurposeProjectPortraitReference)
			seedReferenceAsset(t, repo, portrait)
			project, _ := repo.Projects().FindByID(t.Context(), projectID)
			project.PortraitReferenceImageAssetID = portrait.ID
			if err := repo.Projects().Update(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			tasks.SetReferenceAssetService(NewReferenceAssetService(repo, nil, nil))
			params := CreatePlanParams{UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", CronExpr: "0 9 * * *", Prompt: "create", CoverUsePortrait: true}
			if platform != model.PlatformWechat {
				off := false
				params.ArticleWithCover = &off
			}
			if platform == model.PlatformMontage {
				params.MontageInput = &model.MontageInput{Brief: "create video"}
			}
			if platform == model.PlatformHypit {
				params.HypitInput = &model.HypitInput{Brief: "replicate", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch?v=x"}}
			}
			plan, err := plans.Create(t.Context(), params)
			if err != nil {
				t.Fatal(err)
			}
			for _, selected := range []bool{false, true} {
				plan, err = plans.Update(t.Context(), UpdatePlanParams{ID: plan.ID, ExecutionProfile: "effective", CoverUsePortrait: &selected})
				if err != nil {
					t.Fatal(err)
				}
				stored, err := repo.Plans().FindByID(t.Context(), plan.ID)
				if err != nil || stored.CoverUsePortrait != selected {
					t.Fatalf("persisted selection=%+v err=%v", stored, err)
				}
			}
			task, err := tasks.CreateFromPlan(t.Context(), plan)
			if err != nil {
				t.Fatal(err)
			}
			if !task.CoverUsePortrait || projectPortraitReferenceAssetID(task) != portrait.ID {
				t.Fatal("scheduled task lost selected frozen portrait")
			}
			project.PortraitReferenceImageAssetID = ""
			if err := repo.Projects().Update(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			if _, err := tasks.CreateFromPlan(t.Context(), plan); !errors.Is(err, ErrCoverPortraitUnavailable) {
				t.Fatalf("spawn with removed portrait error=%v", err)
			}
			if _, err := plans.Update(t.Context(), UpdatePlanParams{ID: plan.ID, ExecutionProfile: "effective"}); !errors.Is(err, ErrCoverPortraitUnavailable) {
				t.Fatalf("update with removed portrait error=%v", err)
			}
			off := false
			if _, err := plans.Update(t.Context(), UpdatePlanParams{ID: plan.ID, ExecutionProfile: "effective", CoverUsePortrait: &off}); err != nil {
				t.Fatalf("opt out after portrait removal: %v", err)
			}
		})
	}
}
