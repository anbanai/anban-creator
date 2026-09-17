package service

import (
	"errors"
	"testing"

	serverconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func montageIntegrationConfig() serverconfig.MontageConfig {
	cfg := serverconfig.MontageConfig{}
	cfg.ApplyDefaults()
	cfg.Enabled = true
	cfg.DefaultPipeline = "cinematic"
	cfg.AllowedPipelines = []string{"cinematic", "talking-head", "screen-demo", "clip-factory"}
	cfg.MaxDurationSeconds = 600
	cfg.MaxAssets = 20
	return cfg
}

func TestTaskServiceNormalizesMontageInputFromProjectDefaults(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	project, err := repo.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.SetMontageDefaults(model.MontageDefaults{
		DefaultPipeline: "talking-head",
		Preferences: model.MontagePreferences{
			DurationSeconds: 90,
			Style:           "clean",
		},
		DeliveryTargets: []string{"final_video"},
	})
	if err := repo.Projects().Update(t.Context(), project); err != nil {
		t.Fatal(err)
	}

	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{
		ExecutionProfile: "effective",
		UserID:           userID,
		ProjectID:        projectID,
		MontageInput: &model.MontageInput{
			Brief:           "  精剪产品介绍  ",
			SourceAssets:    []model.MontageAsset{{Type: "video_url", URL: "https://example.com/source.mp4"}},
			DeliveryTargets: []string{},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	got := tasks[0].MontageInput.Data()
	if got.Brief != "精剪产品介绍" || got.PipelineKey != "talking-head" {
		t.Fatalf("normalized input = %#v", got)
	}
	if got.Preferences.DurationSeconds != 90 || got.Preferences.Style != "clean" {
		t.Fatalf("normalized preferences = %#v", got.Preferences)
	}
	if got.DeliveryTargets == nil || len(got.DeliveryTargets) != 0 {
		t.Fatalf("explicit empty delivery targets = %#v, want non-nil empty", got.DeliveryTargets)
	}
}

func TestTaskServiceRejectsRetiredMontagePipeline(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	_, err := svc.CreateManual(t.Context(), CreateManualParams{
		ExecutionProfile: "effective",
		UserID:           userID,
		ProjectID:        projectID,
		MontageInput: &model.MontageInput{
			Brief:       "使用已经停用的流程",
			PipelineKey: "retired-pipeline",
		},
	})
	if err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("CreateManual error = %v, want ErrMontageInput", err)
	}
}

func TestTaskServiceNormalizesMontagePlanWhenCreatingScheduledTask(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	plan := &model.Plan{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: model.PlatformMontage, Status: model.PlanStatusActive,
		ExecutionProfile: "effective", ImageCapabilityKey: "standard",
	}
	plan.SetMontageInput(model.MontageInput{Brief: "自动生成发布视频"})

	task, err := svc.CreateFromPlan(t.Context(), plan)
	if err != nil {
		t.Fatalf("CreateFromPlan: %v", err)
	}
	got := task.MontageInput.Data()
	if got.PipelineKey != "cinematic" || got.Preferences.DurationSeconds != 30 {
		t.Fatalf("scheduled task montage input = %#v", got)
	}
}

func TestTaskServiceExactClonePreservesRetiredMontagePipeline(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	source := &model.Task{
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: model.PlatformMontage, Status: model.TaskStatusCompleted,
		ExecutionProfile: "effective", ExecutionTarget: model.ExecutionTargetCloud,
		ImageRatio: "16:9", ImageCapabilityKey: "standard",
	}
	source.SetMontageInput(model.MontageInput{
		Brief: "历史项目成片", PipelineKey: "retired-pipeline",
		Preferences: model.MontagePreferences{DurationSeconds: 25},
	})
	source.SetProjectSnapshot(model.SnapshotProject(&model.Project{
		ID: projectID, UserID: userID, Platform: model.PlatformMontage,
	}))
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(t.Context(), source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(t.Context(), source.ID, CloneTaskParams{ExecutionProfile: "effective"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if got := clones[0].MontageInput.Data().PipelineKey; got != "retired-pipeline" {
		t.Fatalf("cloned pipeline = %q, want retired-pipeline", got)
	}
}

func TestPlanServiceNormalizesMontageInputAndValidatesUpdates(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	svc.SetMontageCapabilityService(NewMontageCapabilityService(montageIntegrationConfig()))
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	project, err := repo.Projects().FindByID(t.Context(), projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.SetMontageDefaults(model.MontageDefaults{DefaultPipeline: "screen-demo"})
	if err := repo.Projects().Update(t.Context(), project); err != nil {
		t.Fatal(err)
	}

	plan, err := svc.Create(t.Context(), CreatePlanParams{
		ExecutionProfile: "effective",
		UserID:           userID,
		ProjectID:        projectID,
		CronExpr:         "0 10 * * *",
		MontageInput:     &model.MontageInput{Brief: "  每天生成产品演示  "},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := plan.MontageInput.Data()
	if got.Brief != "每天生成产品演示" || got.PipelineKey != "screen-demo" || got.Preferences.DurationSeconds != 60 {
		t.Fatalf("normalized plan input = %#v", got)
	}

	_, err = svc.Update(t.Context(), UpdatePlanParams{
		ExecutionProfile: "effective",
		ID:               plan.ID,
		MontageInput: &model.MontageInput{
			Brief:       "切换到停用流程",
			PipelineKey: "retired-pipeline",
		},
	})
	if err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("Update error = %v, want ErrMontageInput", err)
	}
}

func TestPlanServiceRequiresSourcesForSelectedMontagePipeline(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	svc.SetMontageCapabilityService(NewMontageCapabilityService(montageIntegrationConfig()))
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	_, err := svc.Create(t.Context(), CreatePlanParams{
		ExecutionProfile: "effective",
		UserID:           userID,
		ProjectID:        projectID,
		CronExpr:         "0 10 * * *",
		MontageInput: &model.MontageInput{
			Brief:       "定期精剪口播",
			PipelineKey: "talking-head",
		},
	})
	if err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("Create error = %v, want ErrMontageInput", err)
	}
}

func TestProjectServiceValidatesMontageDefaultsAgainstCapabilityCatalog(t *testing.T) {
	svc, _, ctx, userID := setupProjectServiceTest(t)
	svc.SetMontageCapabilityService(NewMontageCapabilityService(montageIntegrationConfig()))

	valid := &model.Project{Platform: model.PlatformMontage, Name: "口播项目"}
	valid.SetMontageDefaults(model.MontageDefaults{
		DefaultPipeline: "talking-head",
		Preferences:     model.MontagePreferences{DurationSeconds: 120},
	})
	valid.MontageDefaultsSet = true
	if _, err := svc.Create(ctx, userID, valid); err != nil {
		t.Fatalf("Create valid project defaults: %v", err)
	}

	tests := []struct {
		name     string
		defaults model.MontageDefaults
	}{
		{name: "retired pipeline", defaults: model.MontageDefaults{DefaultPipeline: "retired-pipeline"}},
		{name: "duration above limit", defaults: model.MontageDefaults{DefaultPipeline: "cinematic", Preferences: model.MontagePreferences{DurationSeconds: 601}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			project := &model.Project{Platform: model.PlatformMontage, Name: tt.name}
			project.SetMontageDefaults(tt.defaults)
			project.MontageDefaultsSet = true
			if _, err := svc.Create(ctx, userID, project); err == nil || !errors.Is(err, ErrProjectMontageDefaults) {
				t.Fatalf("Create error = %v, want ErrProjectMontageDefaults", err)
			}
		})
	}
}
