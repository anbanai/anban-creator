package service

import (
	"encoding/json"
	"github.com/anbanai/anban-creator/server/agentpack"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/storage"
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"strings"
	"testing"
	"time"
)

func hypitTestConfig() config.HypitConfig {
	c := config.HypitConfig{Enabled: true, RuntimeProfile: map[string]any{"format": "hypit.runtime-local@1", "credentials": map[string]any{"env": map[string]any{"use": "@hypit/credential-store-env"}}, "endpoints": map[string]any{"local": map[string]any{"use": "@hypit/provider-media-local"}}}, Env: map[string]string{"HYPIT_PROVIDER_TOKEN": "private-secret"}}
	c.ApplyDefaults()
	return c
}
func TestHypitInputValidation(t *testing.T) {
	s := NewHypitCapabilityService(hypitTestConfig())
	for _, tc := range []struct {
		name         string
		input        model.HypitInput
		reuse, valid bool
	}{{"missing reference", model.HypitInput{Brief: "a"}, false, false}, {"reference", model.HypitInput{Brief: "a", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch?v=x"}}, false, true}, {"archive reuse", model.HypitInput{Brief: "a"}, true, true}, {"bad aspect", model.HypitInput{Brief: "a", Preferences: model.HypitPreferences{AspectRatio: "4:3"}}, true, false}, {"dual locator", model.HypitInput{Brief: "a", Reference: &model.HypitAsset{Type: "video", URL: "https://example.com/a.mp4", TaskFileID: "id"}}, false, false}, {"bad reference type", model.HypitInput{Brief: "a", Reference: &model.HypitAsset{Type: "image", URL: "https://example.com/a.png"}}, false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			err := s.NormalizeAndValidateInput(&tc.input, model.HypitDefaults{}, tc.reuse)
			if (err == nil) != tc.valid {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func TestHypitTaskAdmissionFreezesNativeProfileWithoutSecrets(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: "hypit", Image: "image@sha256:pinned"}})
	svc.SetHypitConfig(hypitTestConfig())
	user := uuid.NewString()
	project := createTestProject(t, repo, user, model.PlatformHypit)
	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{ExecutionProfile: "effective", UserID: user, ProjectID: project, Quantity: 4, HypitInput: &model.HypitInput{Brief: "replicate", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch?v=x"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks=%d", len(tasks))
	}
	snap := readHypitSnapshot(tasks[0])
	if snap.Image != "image@sha256:pinned" || snap.Profile["dataRoot"] != "/workspace/project/.hypit/execution" {
		t.Fatalf("bad snapshot: %+v", snap)
	}
	if strings.Contains(string(tasks[0].HypitRuntimeSnapshot), "private-secret") {
		t.Fatal("snapshot leaked secret")
	}
	b, _ := json.Marshal(tasks[0])
	if strings.Contains(string(b), "hypitRuntimeSnapshot") || strings.Contains(string(b), "image@sha256:pinned") {
		t.Fatal("private execution leaked into JSON")
	}
}
func TestHypitCompletionRequiresDurableExactRoles(t *testing.T) {
	files := []*model.TaskFile{}
	for p, r := range hypitRequired {
		files = append(files, &model.TaskFile{FilePath: p, Role: r, FileSize: 1, OSSKey: "immutable/" + p, ContentHash: strings.Repeat("a", 64), State: model.TaskFileStatePending})
	}
	if !validateHypitCompletionArtifacts(files).Valid {
		t.Fatal("complete evidence rejected")
	}
	files[0].ContentHash = ""
	if validateHypitCompletionArtifacts(files).Valid {
		t.Fatal("unverified artifact accepted")
	}
	files[0].ContentHash = strings.Repeat("a", 64)
	files[0].Role = "wrong"
	if validateHypitCompletionArtifacts(files).Valid {
		t.Fatal("wrong role accepted")
	}
}
func TestHypitLargerArtifactAndBootstrapLimitsScoped(t *testing.T) {
	cfg := hypitTestConfig()
	b, _ := json.Marshal(hypitRuntimeSnapshot{Limits: cfg.Limits})
	task := &model.Task{Type: model.PlatformHypit, HypitRuntimeSnapshot: datatypes.JSON(b)}
	if taskArtifactByteLimit(task, "output/project.zip") != 2<<30 || taskArtifactByteLimit(task, "output/other.zip") != 512<<20 {
		t.Fatal("incorrect scoped upload limits")
	}
	files := []BootstrapFile{{Path: ".anban-creator/project.zip", DownloadURL: "https://example.com/immutable.zip", ExpectedSize: 1 << 30, MaxBytes: 2 << 30, ContentSHA256: strings.Repeat("a", 64), Mode: 0644}}
	if err := validateBootstrapFilesForTask(files, task); err != nil {
		t.Fatal(err)
	}
	if ValidateBootstrapFiles(files) == nil {
		t.Fatal("large bootstrap accepted for unrelated task")
	}
}
func TestHypitBootstrapKeepsExternalVideoLinkForOfficialFetch(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetHypitConfig(hypitTestConfig())
	task := &model.Task{Type: model.PlatformHypit}
	task.SetHypitInput(model.HypitInput{Brief: "a", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch?v=x"}})
	if err := svc.freezeHypitTask(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	bootstrap := &AgentBootstrapService{repo: repo}
	files, err := bootstrap.buildHypitFiles(t.Context(), task, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("external URL materialized: %+v", files)
	}
	if !strings.Contains(files[0].Text, "https://example.com/watch?v=x") {
		t.Fatal("external reference lost")
	}
}

func TestHypitPlanCreationUpdateAndScheduledInput(t *testing.T) {
	svc, repo := setupTestPlanService(t)
	svc.SetHypitCapabilityService(NewHypitCapabilityService(hypitTestConfig()))
	user := uuid.NewString()
	project := createTestProject(t, repo, user, model.PlatformHypit)
	p, err := svc.Create(t.Context(), CreatePlanParams{UserID: user, ProjectID: project, ExecutionProfile: "effective", CronExpr: "0 9 * * *", HypitInput: &model.HypitInput{Brief: "first", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch?v=a"}}})
	if err != nil {
		t.Fatal(err)
	}
	input := p.HypitInput.Data()
	input.Brief = "updated"
	p, err = svc.Update(t.Context(), UpdatePlanParams{ID: p.ID, ExecutionProfile: "effective", HypitInput: &input})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Plans().FindByID(t.Context(), p.ID)
	if err != nil || stored.HypitInput.Data().Brief != "updated" {
		t.Fatalf("input not persisted: %v", err)
	}
}

func TestHypitCloneUsesVerifiedSourceArchiveAndFrozenProvider(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: "hypit", Image: "image@sha256:old"}})
	svc.SetHypitConfig(hypitTestConfig())
	user := uuid.NewString()
	project := createTestProject(t, repo, user, model.PlatformHypit)
	src := &model.Task{ID: uuid.NewString(), UserID: user, ProjectID: project, Type: model.PlatformHypit, Status: model.TaskStatusCompleted}
	src.SetHypitInput(model.HypitInput{Brief: "original", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch?v=a"}})
	if err := svc.freezeHypitTask(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	file := &model.TaskFile{ID: uuid.NewString(), TaskID: src.ID, FilePath: "output/project.zip", Role: "project_archive", State: model.TaskFileStateDelivered, OSSKey: "immutable/project.zip", ContentHash: strings.Repeat("a", 64), FileSize: 128, StorageProvider: "fake"}
	if err := repo.TaskFiles().Create(t.Context(), file); err != nil {
		t.Fatal(err)
	}
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: "hypit", Image: "image@sha256:new"}})
	next := &model.Task{ID: uuid.NewString(), UserID: user, ProjectID: project, Type: model.PlatformHypit, InputSourceTaskID: src.ID, InputSourceProjectID: project}
	next.SetHypitInput(src.HypitInput.Data())
	if err := svc.freezeHypitTask(t.Context(), next); err != nil {
		t.Fatal(err)
	}
	snap := readHypitSnapshot(next)
	if snap.Image != "image@sha256:old" || snap.SourceArchiveID != file.ID || snap.SourceExecution == nil || !hasCompleteAgentPackIdentity(snap.SourceExecution.execution()) {
		t.Fatalf("source identity not preserved: %+v", snap)
	}
	bootstrap := &AgentBootstrapService{repo: repo, cfg: AgentBootstrapConfig{Store: &signFakeStore{}, Hypit: hypitTestConfig()}}
	executionDeadline := time.Now().UTC().Add(7 * time.Minute).Truncate(time.Second)
	files, err := bootstrap.buildHypitFiles(t.Context(), next, time.Now().Add(time.Hour), executionDeadline)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("repeated media materialized: %+v", files)
	}
	for _, f := range files {
		if f.Path == "input.json" {
			var in map[string]any
			if json.Unmarshal([]byte(f.Text), &in) != nil || in["reference"] != nil || in["project_archive_path"] != ".anban-creator/project.zip" {
				t.Fatalf("unexpected clone input %s", f.Text)
			}
			if in["execution_deadline"] != executionDeadline.Format(time.RFC3339Nano) {
				t.Fatalf("workload deadline lost: %+v", in)
			}
			runtime, ok := in["runtime"].(map[string]any)
			if !ok || runtime["image"] != "image@sha256:old" || runtime["agent_pack_digest"] != snap.SourceExecution.Digest {
				t.Fatalf("clone runtime provenance lost: %+v", runtime)
			}
		}
	}
}

func TestHypitReportsAcceptUpgradeButRejectMixedRevisionsAndFailedChecks(t *testing.T) {
	cfg := hypitTestConfig()
	snap, _ := json.Marshal(hypitRuntimeSnapshot{Limits: cfg.Limits})
	task := &model.Task{Type: model.PlatformHypit, HypitRuntimeSnapshot: datatypes.JSON(snap)}
	revision := strings.Repeat("b", 40)
	upstream := map[string]any{"repository": "https://github.com/hypit-ai/hypit", "revision": revision}
	checks := map[string]bool{}
	for _, k := range []string{"semantic", "video_probe", "video_full_decode", "cover_decode", "project_references", "official_check", "official_plan", "project_archive"} {
		checks[k] = true
	}
	quality := map[string]any{"schema_version": 1, "passed": true, "upstream": upstream, "checks": checks, "media": map[string]any{"duration_seconds": 15, "width": 720, "height": 1280}}
	project := map[string]any{"schema_version": 1, "upstream": upstream, "project_root": "/workspace/project", "run_path": "productions/main/runs/main.svrun"}
	store := &fakeTaskArtifactStorage{objects: map[string][]byte{}}
	svc := &TaskService{store: store}
	files := []*model.TaskFile{{FilePath: "output/project.json", OSSKey: "project"}, {FilePath: "output/quality-report.json", OSSKey: "quality"}}
	write := func() {
		store.objects["project"], _ = json.Marshal(project)
		store.objects["quality"], _ = json.Marshal(quality)
	}
	write()
	if err := svc.validateHypitReports(t.Context(), task, files); err != nil {
		t.Fatalf("upgrade rejected: %v", err)
	}
	quality["upstream"] = map[string]any{"repository": "https://github.com/hypit-ai/hypit", "revision": strings.Repeat("c", 40)}
	write()
	if svc.validateHypitReports(t.Context(), task, files) == nil {
		t.Fatal("mixed revisions accepted")
	}
	quality["upstream"] = upstream
	checks["video_full_decode"] = false
	write()
	if svc.validateHypitReports(t.Context(), task, files) == nil {
		t.Fatal("failed full decode accepted")
	}
}

func TestHypitScheduledTaskFreezesInput(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: "hypit", Image: "image@sha256:pinned"}})
	svc.SetHypitConfig(hypitTestConfig())
	user := uuid.NewString()
	project := createTestProject(t, repo, user, model.PlatformHypit)
	p := &model.Plan{ID: uuid.NewString(), UserID: user, ProjectID: project, Type: model.PlatformHypit, Status: model.PlanStatusActive, ExecutionProfile: "effective"}
	p.SetHypitInput(model.HypitInput{Brief: "scheduled", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch"}})
	task, err := svc.CreateFromPlan(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	if task == nil || task.HypitInput.Data().Brief != "scheduled" || readHypitSnapshot(task).Image != "image@sha256:pinned" {
		t.Fatalf("incorrect scheduled task %+v", task)
	}
}

func TestHypitDurableDeliveryAcceptsArchiveAboveLegacyLimit(t *testing.T) {
	cfg := hypitTestConfig()
	b, _ := json.Marshal(hypitRuntimeSnapshot{Limits: cfg.Limits})
	task := &model.Task{ID: "hypit-task", UserID: "owner", ProjectID: "project", Type: model.PlatformHypit, HypitRuntimeSnapshot: datatypes.JSON(b)}
	file := &model.TaskFile{TaskID: task.ID, ExecutionID: "execution", FilePath: "output/project.zip", MimeType: "application/zip", FileSize: 1 << 30, ContentHash: strings.Repeat("a", 64), StorageProvider: "oss"}
	file.OSSKey = buildTaskArtifactFinalStorageKey(task, file.ExecutionID, file.ContentHash, file.FilePath)
	store := &fakeTaskArtifactStorage{name: "oss", stats: map[string]*storage.ObjectInfo{file.OSSKey: {Size: file.FileSize, ContentType: file.MimeType, SHA256: file.ContentHash}}}
	svc := &TaskService{store: store}
	spec := agentpack.DeliverySpec{Role: "project_archive", Path: file.FilePath, MIMEType: file.MimeType}
	if err := svc.validateStoredDeliveryObject(t.Context(), task, file.ExecutionID, file, spec); err != nil {
		t.Fatalf("valid 1GiB archive rejected: %v", err)
	}
	task.Type = model.PlatformArticle
	if svc.validateStoredDeliveryObject(t.Context(), task, file.ExecutionID, file, spec) == nil {
		t.Fatal("legacy task got expanded archive allowance")
	}
	task.Type = model.PlatformHypit
	file.FileSize = cfg.Limits.MaxProjectBytes + 1
	if svc.validateStoredDeliveryObject(t.Context(), task, file.ExecutionID, file, spec) == nil {
		t.Fatal("archive above frozen maximum accepted")
	}
}

func TestHypitRejectsUnownedInternalURLAdmission(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetHypitConfig(hypitTestConfig())
	project := createTestProject(t, repo, "review-user", model.PlatformHypit)
	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{UserID: "review-user", ProjectID: project, ExecutionProfile: "effective", HypitInput: &model.HypitInput{Brief: "replicate", Reference: &model.HypitAsset{Type: "video", URL: "/api/v1/files/tasks/another-user/private.mp4"}}})
	if err == nil {
		t.Fatalf("accepted unowned URL on tasks %v", tasks)
	}
}

func TestHypitCloneKeepsFrozenLimitsAfterConfigChange(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	cfg := hypitTestConfig()
	svc.SetHypitConfig(cfg)
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: "hypit", Image: "image@sha256:old"}})
	project := createTestProject(t, repo, "review-user", model.PlatformHypit)
	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{UserID: "review-user", ProjectID: project, ExecutionProfile: "effective", HypitInput: &model.HypitInput{Brief: "replicate", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch"}, Preferences: model.HypitPreferences{DurationSeconds: hypitDuration(120)}}})
	if err != nil {
		t.Fatal(err)
	}
	src := tasks[0]
	src.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	cfg.Limits.MaxDurationSeconds = 60
	svc.SetHypitConfig(cfg)
	_, err = svc.Clone(t.Context(), src.ID, CloneTaskParams{ExecutionProfile: "effective"})
	t.Logf("source frozen limit=%d, unchanged clone error=%v", readHypitSnapshot(src).Limits.MaxDurationSeconds, err)
	if err != nil {
		t.Fatalf("frozen clone rejected: %v", err)
	}
}

func TestHypitSourceLineageAuthorizesAncestorMediaOnly(t *testing.T) {
	_, repo := setupTaskServiceWithEnqueuer(t)
	root := &model.Task{ID: uuid.NewString(), UserID: "owner", ProjectID: "old-project", Type: model.PlatformHypit}
	middle := &model.Task{ID: uuid.NewString(), UserID: "owner", ProjectID: "middle-project", Type: model.PlatformHypit, InputSourceTaskID: root.ID}
	for _, task := range []*model.Task{root, middle} {
		if err := repo.Tasks().Create(t.Context(), task); err != nil {
			t.Fatal(err)
		}
	}
	trust, err := hypitSourceTrust(t.Context(), repo, "owner", middle.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !montageSourceTaskAuthorized(root, "destination", trust...) {
		t.Fatal("ancestor media rejected")
	}
	other := *root
	other.ID = "unrelated"
	if montageSourceTaskAuthorized(&other, "destination", trust...) {
		t.Fatal("unrelated media accepted")
	}
	if _, err := hypitSourceTrust(t.Context(), repo, "attacker", middle.ID); err == nil {
		t.Fatal("foreign lineage accepted")
	}
}

func TestHypitExplicitZeroDurationSurvivesNormalization(t *testing.T) {
	s := NewHypitCapabilityService(hypitTestConfig())
	for _, tc := range []struct {
		name, raw string
		want      int64
	}{{"omitted", `{}`, 30}, {"source", `{"duration_seconds":0}`, 0}, {"positive", `{"duration_seconds":12}`, 12}} {
		t.Run(tc.name, func(t *testing.T) {
			var in model.HypitInput
			var d model.HypitDefaults
			_ = json.Unmarshal([]byte(`{"preferences":{"duration_seconds":30}}`), &d)
			_ = json.Unmarshal([]byte(`{"brief":"a","preferences":`+tc.raw+`}`), &in)
			if err := s.NormalizeAndValidateInput(&in, d, true); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(in)
			var result struct {
				Preferences struct {
					Duration *int64 `json:"duration_seconds"`
				} `json:"preferences"`
			}
			_ = json.Unmarshal(raw, &result)
			if result.Preferences.Duration == nil || *result.Preferences.Duration != tc.want {
				t.Fatalf("duration roundtrip %s want %d", raw, tc.want)
			}
		})
	}
}

func hypitDuration(v int64) *int64 { return &v }

func TestHypitSourceCapabilitiesUseFrozenLimitsCurrentCredentialsAndOwnership(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	cfg := hypitTestConfig()
	cfg.RuntimeProfile["endpoints"] = map[string]any{"native": map[string]any{"use": "@hypit/provider-hiapi", "config": map[string]any{"apiKey": map[string]any{"store": "env", "key": "HYPIT_PROVIDER_TOKEN"}}}}
	svc.SetHypitConfig(cfg)
	src := &model.Task{ID: uuid.NewString(), UserID: "owner", Type: model.PlatformHypit}
	if err := svc.freezeHypitTask(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	cfg.Limits.MaxDurationSeconds = 60
	cfg.Env = map[string]string{}
	svc.SetHypitConfig(cfg)
	catalog, err := svc.HypitCapabilitiesForSource(t.Context(), "owner", src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Limits.MaxDurationSeconds != 180 || catalog.Configured || !catalog.Enabled {
		t.Fatalf("catalog=%+v", catalog)
	}
	cfg.Env["HYPIT_PROVIDER_TOKEN"] = "refreshed-secret"
	cfg.Enabled = false
	svc.SetHypitConfig(cfg)
	catalog, err = svc.HypitCapabilitiesForSource(t.Context(), "owner", src.ID)
	if err != nil || !catalog.Configured || catalog.Enabled {
		t.Fatalf("current config not refreshed: %+v %v", catalog, err)
	}
	if _, err = svc.HypitCapabilitiesForSource(t.Context(), "foreign", src.ID); err == nil {
		t.Fatal("foreign source capabilities accepted")
	}
}

func TestHypitExplicitZeroDurationPersistsAndClones(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetHypitConfig(hypitTestConfig())
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: "hypit", Image: "frozen-image"}})
	user := uuid.NewString()
	id := createTestProject(t, repo, user, model.PlatformHypit)
	project, _ := repo.Projects().FindByID(t.Context(), id)
	project.SetHypitDefaults(model.HypitDefaults{Preferences: model.HypitPreferences{DurationSeconds: hypitDuration(30)}})
	if err := repo.Projects().Update(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{UserID: user, ProjectID: id, ExecutionProfile: "effective", HypitInput: &model.HypitInput{Brief: "source", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch"}, Preferences: model.HypitPreferences{DurationSeconds: hypitDuration(0)}}})
	if err != nil {
		t.Fatal(err)
	}
	src, _ := repo.Tasks().FindByID(t.Context(), tasks[0].ID)
	duration := src.HypitInput.Data().Preferences.DurationSeconds
	if duration == nil || *duration != 0 {
		t.Fatal("zero lost in persistence")
	}
	src.Status = model.TaskStatusCompleted
	if err := repo.Tasks().Update(t.Context(), src); err != nil {
		t.Fatal(err)
	}
	clones, err := svc.Clone(t.Context(), src.ID, CloneTaskParams{ExecutionProfile: "effective"})
	if err != nil {
		t.Fatal(err)
	}
	duration = clones[0].HypitInput.Data().Preferences.DurationSeconds
	if duration == nil || *duration != 0 {
		t.Fatal("zero lost in clone")
	}
}
