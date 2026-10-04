package service

import (
	"errors"
	"strings"
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

	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
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

	_, err := svc.CreateManual(t.Context(), CreateManualParams{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
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

func TestTaskServiceValidatesMontageTaskFileSources(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	svc.store = &signFakeStore{}
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	foreignUserID := uuid.NewString()
	foreignProjectID := createTestProject(t, repo, foreignUserID, model.PlatformMontage)

	ownerTask := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage, ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformMontage, Status: model.TaskStatusCompleted}
	foreignTask := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage, ID: uuid.NewString(), UserID: foreignUserID, ProjectID: foreignProjectID, Type: model.PlatformMontage, Status: model.TaskStatusCompleted}
	for _, task := range []*model.Task{ownerTask, foreignTask} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatalf("create source task: %v", err)
		}
	}
	validVideo := &model.TaskFile{
		ID: uuid.NewString(), TaskID: ownerTask.ID, State: model.TaskFileStateDelivered,
		Role: model.FileRoleVideo, FilePath: "output/source.mp4", FileName: "source.mp4", MimeType: "video/mp4",
		FileSize: 1024, ContentHash: strings.Repeat("a", 64), OSSKey: "tasks/source/source.mp4", StorageProvider: "fake",
	}
	imageFile := &model.TaskFile{ID: uuid.NewString(), TaskID: ownerTask.ID, State: model.TaskFileStateDelivered, Role: model.FileRoleImage, FilePath: "output/source.png", FileName: "source.png", MimeType: "image/png"}
	foreignVideo := &model.TaskFile{ID: uuid.NewString(), TaskID: foreignTask.ID, State: model.TaskFileStateDelivered, Role: model.FileRoleVideo, FilePath: "output/foreign.mp4", FileName: "foreign.mp4", MimeType: "video/mp4"}
	for _, file := range []*model.TaskFile{validVideo, imageFile, foreignVideo} {
		if err := repo.TaskFiles().Create(ctx, file); err != nil {
			t.Fatalf("create source file: %v", err)
		}
	}

	tests := []struct {
		name      string
		fileID    string
		assetType string
		wantErr   bool
	}{
		{name: "missing file", fileID: uuid.NewString(), assetType: "video", wantErr: true},
		{name: "foreign file", fileID: foreignVideo.ID, assetType: "video", wantErr: true},
		{name: "mime mismatch", fileID: imageFile.ID, assetType: "video", wantErr: true},
		{name: "owned matching file", fileID: validVideo.ID, assetType: "video", wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateManual(ctx, CreateManualParams{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
				ExecutionProfile: "effective",
				UserID:           userID,
				ProjectID:        projectID,
				MontageInput: &model.MontageInput{
					Brief:        "校验素材来源",
					PipelineKey:  "talking-head",
					SourceAssets: []model.MontageAsset{{Type: tt.assetType, TaskFileID: tt.fileID}},
				},
			})
			if tt.wantErr {
				if err == nil || !errors.Is(err, ErrMontageInput) {
					t.Fatalf("CreateManual error = %v, want ErrMontageInput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("CreateManual error = %v, want success", err)
			}
		})
	}
}

func TestTaskServiceReservesBootstrapBudgetBeyondMontageTaskFileSources(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	svc.store = &signFakeStore{}
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	sourceTask := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage, ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformMontage, Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(ctx, sourceTask); err != nil {
		t.Fatal(err)
	}
	assets := make([]model.MontageAsset, 0, 4)
	for i := 0; i < 4; i++ {
		file := &model.TaskFile{
			ID: uuid.NewString(), TaskID: sourceTask.ID, State: model.TaskFileStateDelivered,
			Role: model.FileRoleVideo, FilePath: "output/source-" + string(rune('a'+i)) + ".mp4",
			FileName: "source.mp4", MimeType: "video/mp4", FileSize: 64 << 20,
			ContentHash: strings.Repeat("a", 64), OSSKey: "tasks/source/" + uuid.NewString() + ".mp4", StorageProvider: "fake",
		}
		if err := repo.TaskFiles().Create(ctx, file); err != nil {
			t.Fatal(err)
		}
		assets = append(assets, model.MontageAsset{Type: "video", TaskFileID: file.ID})
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
		ExecutionProfile: "effective", UserID: userID, ProjectID: projectID,
		MontageInput: &model.MontageInput{Brief: "too much bootstrap media", PipelineKey: "cinematic", SourceAssets: assets},
	})
	if err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("CreateManual error = %v, want ErrMontageInput", err)
	}
	if tasks != nil {
		t.Fatalf("CreateManual tasks = %#v, want nil", tasks)
	}
}

func TestTaskServiceRejectsMontageInlineBootstrapBudgetAtSourceLimit(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	svc.store = &signFakeStore{}
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	sourceTask := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage, ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformMontage, Status: model.TaskStatusCompleted}
	if err := repo.Tasks().Create(ctx, sourceTask); err != nil {
		t.Fatal(err)
	}

	sizes := []int64{64 << 20, 64 << 20, 64 << 20, montageSourceTotalMaxBytes - 3*(64<<20)}
	assets := make([]model.MontageAsset, 0, len(sizes))
	for i, size := range sizes {
		file := &model.TaskFile{
			ID: uuid.NewString(), TaskID: sourceTask.ID, State: model.TaskFileStateDelivered,
			Role: model.FileRoleVideo, FilePath: "output/source-" + string(rune('a'+i)) + ".mp4",
			FileName: "source.mp4", MimeType: "video/mp4", FileSize: size,
			ContentHash: strings.Repeat("a", 64), OSSKey: "tasks/source/" + uuid.NewString() + ".mp4", StorageProvider: "fake",
		}
		if err := repo.TaskFiles().Create(ctx, file); err != nil {
			t.Fatal(err)
		}
		assets = append(assets, model.MontageAsset{Type: "video", TaskFileID: file.ID})
	}

	tasks, err := svc.CreateManual(ctx, CreateManualParams{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
		ExecutionProfile: "effective", UserID: userID, ProjectID: projectID,
		MontageInput: &model.MontageInput{
			Brief: "inline metadata must fit the remaining bootstrap budget", PipelineKey: "cinematic",
			SourceAssets: assets,
			Advanced:     map[string]any{"oversized": strings.Repeat("x", montageBootstrapInlineReserveBytes)},
		},
	})
	if err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("CreateManual error = %v, want ErrMontageInput", err)
	}
	if tasks != nil {
		t.Fatalf("CreateManual tasks = %#v, want nil", tasks)
	}
}

func TestTaskServiceExactCloneAcceptsIntermediateMontageSourceFromRootLineage(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	svc.store = &signFakeStore{}
	ctx := t.Context()
	userID := uuid.NewString()
	rootProjectID := createTestProject(t, repo, userID, model.PlatformMontage)
	intermediateProjectID := createTestProject(t, repo, userID, model.PlatformMontage)
	destinationProjectID := createTestProject(t, repo, userID, model.PlatformMontage)

	root := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
		ID: uuid.NewString(), UserID: userID, ProjectID: rootProjectID,
		Type: model.PlatformMontage, Status: model.TaskStatusCompleted,
	}
	intermediate := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
		ID: uuid.NewString(), UserID: userID, ProjectID: intermediateProjectID,
		Type: model.PlatformMontage, Status: model.TaskStatusCompleted,
		InputSourceTaskID: root.ID, InputSourceProjectID: root.ProjectID,
	}
	for _, task := range []*model.Task{root, intermediate} {
		if err := repo.Tasks().Create(ctx, task); err != nil {
			t.Fatal(err)
		}
	}
	intermediateFile := &model.TaskFile{
		ID: uuid.NewString(), TaskID: intermediate.ID, State: model.TaskFileStateDelivered,
		Role: model.FileRoleVideo, FilePath: "output/intermediate.mp4", FileName: "intermediate.mp4",
		MimeType: "video/mp4", FileSize: 1024, ContentHash: strings.Repeat("a", 64),
		OSSKey: "tasks/intermediate/output/intermediate.mp4", StorageProvider: "fake",
	}
	if err := repo.TaskFiles().Create(ctx, intermediateFile); err != nil {
		t.Fatal(err)
	}

	source := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
		ID: uuid.NewString(), UserID: userID, ProjectID: destinationProjectID,
		Type: model.PlatformMontage, Status: model.TaskStatusCompleted,
		ExecutionProfile: "effective",
		ImageRatio:       "16:9", ImageCapabilityKey: "standard",
		InputSourceTaskID: root.ID, InputSourceProjectID: root.ProjectID,
	}
	source.SetMontageInput(model.MontageInput{
		Brief: "reuse an intermediate lineage output", PipelineKey: "talking-head",
		Preferences:  model.MontagePreferences{DurationSeconds: 60},
		SourceAssets: []model.MontageAsset{{Type: "video", TaskFileID: intermediateFile.ID}},
	})
	source.SetProjectSnapshot(model.SnapshotProject(&model.Project{
		ID: destinationProjectID, UserID: userID, Platform: model.PlatformMontage,
	}))
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: "effective"})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if len(clones) != 1 || clones[0].MontageInput.Data().SourceAssets[0].TaskFileID != intermediateFile.ID {
		t.Fatalf("clones = %#v, want one clone preserving intermediate source", clones)
	}
}

func TestTaskServiceExactCloneRejectsUnmaterializableMontageSource(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	svc.store = &signFakeStore{}
	ctx := t.Context()
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	source := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: model.PlatformMontage, Status: model.TaskStatusCompleted,
		ExecutionProfile: "effective",
		ImageRatio:       "16:9", ImageCapabilityKey: "standard",
	}
	sourceFile := &model.TaskFile{
		ID: uuid.NewString(), TaskID: source.ID, State: model.TaskFileStateDelivered,
		Role: model.FileRoleVideo, FilePath: "output/source.mp4", FileName: "source.mp4", MimeType: "video/mp4", StorageProvider: "fake",
	}
	source.SetMontageInput(model.MontageInput{
		Brief: "historical source", PipelineKey: "retired-pipeline",
		Preferences:  model.MontagePreferences{DurationSeconds: 25},
		SourceAssets: []model.MontageAsset{{Type: "video", TaskFileID: sourceFile.ID}},
	})
	source.SetProjectSnapshot(model.SnapshotProject(&model.Project{ID: projectID, UserID: userID, Platform: model.PlatformMontage}))
	freezeTestTaskImageCapability(t, source, "standard", testImageCapabilityRoute("image.standard"))
	if err := repo.Tasks().Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := repo.TaskFiles().Create(ctx, sourceFile); err != nil {
		t.Fatal(err)
	}

	clones, err := svc.Clone(ctx, source.ID, CloneTaskParams{ExecutionProfile: "effective"})
	if err == nil || !errors.Is(err, ErrMontageInput) {
		t.Fatalf("Clone error = %v, want ErrMontageInput", err)
	}
	if clones != nil {
		t.Fatalf("Clone tasks = %#v, want nil", clones)
	}
}

func TestTaskServiceExactClonePreservesRetiredMontagePipeline(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	svc.SetMontageConfig(montageIntegrationConfig())
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)
	source := &model.Task{AgentID: model.AgentIDMontage, Channel: model.ChannelMontage, TaskKind: model.PlatformMontage,
		ID: uuid.NewString(), UserID: userID, ProjectID: projectID,
		Type: model.PlatformMontage, Status: model.TaskStatusCompleted,
		ExecutionProfile: "effective",
		ImageRatio:       "16:9", ImageCapabilityKey: "standard",
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

	editedPrompt := "沿用历史流程但更新 brief"
	clones, err := svc.Clone(t.Context(), source.ID, CloneTaskParams{ExecutionProfile: "effective", Prompt: &editedPrompt})
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	gotInput := clones[0].MontageInput.Data()
	if gotInput.PipelineKey != "retired-pipeline" {
		t.Fatalf("cloned pipeline = %q, want retired-pipeline", gotInput.PipelineKey)
	}
	if gotInput.Brief != editedPrompt {
		t.Fatalf("cloned brief = %q, want %q", gotInput.Brief, editedPrompt)
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

func TestProjectServiceRevalidatesPersistedMontageDefaultsOnPartialUpdate(t *testing.T) {
	svc, _, ctx, userID := setupProjectServiceTest(t)
	legacy := &model.Project{Platform: model.PlatformMontage, Name: "历史视频项目"}
	legacy.SetMontageDefaults(model.MontageDefaults{DefaultPipeline: "retired-pipeline"})
	legacy.MontageDefaultsSet = true
	created, err := svc.Create(ctx, userID, legacy)
	if err != nil {
		t.Fatalf("Create legacy project: %v", err)
	}
	svc.SetMontageCapabilityService(NewMontageCapabilityService(montageIntegrationConfig()))

	_, err = svc.Update(ctx, userID, created.ID, &model.Project{Name: "更新后的项目名称"})
	if err == nil || !errors.Is(err, ErrProjectMontageDefaults) {
		t.Fatalf("Update error = %v, want ErrProjectMontageDefaults for persisted retired pipeline", err)
	}
}
