package service

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/datatypes"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
)

type profileProjectionMemory struct {
	files map[string]string
}

func (m *profileProjectionMemory) EnsureProject(context.Context, string) error { return nil }

func (m *profileProjectionMemory) WriteMarkdownFile(_ context.Context, _, path, content string) error {
	if m.files == nil {
		m.files = map[string]string{}
	}
	m.files[path] = content
	return nil
}

type failingProfileProjectionMemory struct {
	profileProjectionMemory
	err error
}

func (m *failingProfileProjectionMemory) WriteMarkdownFile(ctx context.Context, projectID, path, content string) error {
	if m.err != nil {
		return m.err
	}
	return m.profileProjectionMemory.WriteMarkdownFile(ctx, projectID, path, content)
}

type profileLifecycleFixture struct {
	repo      repository.Repository
	service   *ProjectService
	userID    string
	projectID string
	taskID    string
	memory    *profileProjectionMemory
}

func setupProfileLifecycle(t *testing.T) *profileLifecycleFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "profile-lifecycle.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Project{}, &model.Task{}, &model.ProjectProfileState{}, &model.ProjectProfileRevision{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := repository.New(db)
	ctx := context.Background()
	userID, projectID, taskID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	profile := model.NewProjectProfile()
	profile.AnalysisTaskID = taskID
	profile.InitializationStatus = model.ProfileInitializationRunning
	project := &model.Project{ID: projectID, UserID: userID, Name: "profile lifecycle", Platform: model.PlatformWechat, Profile: datatypes.NewJSONType(profile)}
	if err := repo.Projects().Create(ctx, project); err != nil {
		t.Fatal(err)
	}
	if err := repo.Tasks().Create(ctx, &model.Task{ID: taskID, UserID: userID, ProjectID: projectID, Type: model.TaskTypeProfileAnalysis, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	memory := &profileProjectionMemory{}
	svc := NewProjectService(repo, &logger)
	svc.SetProjectProfileMemory(memory)
	return &profileLifecycleFixture{repo: repo, service: svc, userID: userID, projectID: projectID, taskID: taskID, memory: memory}
}

func validProfileDimensions() model.ProjectProfileDimensions {
	newDimension := func(summary string) model.ProfileDimension {
		return model.ProfileDimension{
			Content: map[string]any{"summary": summary}, Sources: []string{"[链接分析]"},
			Evidence: []string{"用户提供的公开主页"}, MissingFields: []string{},
		}
	}
	return model.ProjectProfileDimensions{
		Identity: newDimension("账号定位"), Style: newDimension("表达风格"), Audience: newDimension("目标受众"),
		Platforms: newDimension("平台规则"), Preferences: newDimension("内容偏好"), Memory: newDimension("复盘经验"),
	}
}

func (f *profileLifecycleFixture) currentProfile(t *testing.T) model.ProjectProfile {
	t.Helper()
	project, err := f.repo.Projects().FindByID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	return project.Profile.Data()
}

func (f *profileLifecycleFixture) currentTask(t *testing.T) *model.Task {
	t.Helper()
	task, err := f.repo.Tasks().FindByID(context.Background(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func (f *profileLifecycleFixture) revisions(t *testing.T) []*model.ProjectProfileRevision {
	t.Helper()
	rows, err := f.repo.ProjectProfileRevisions().ListByProject(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestApplyAgentProfileResultStaleRevisionLeavesAllStateUnchanged(t *testing.T) {
	f := setupProfileLifecycle(t)
	before := f.currentProfile(t)
	_, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 1, validProfileDimensions(), nil, nil)
	if !errors.Is(err, ErrProjectProfileVersionConflict) {
		t.Fatalf("ApplyAgentProfileResult error = %v, want revision conflict", err)
	}
	if got := f.currentProfile(t); got.Version != before.Version || got.InitializationStatus != before.InitializationStatus || got.AnalysisTaskID != before.AnalysisTaskID {
		t.Fatalf("profile changed after stale submission: before=%+v after=%+v", before, got)
	}
	if got := f.revisions(t); len(got) != 0 {
		t.Fatalf("revision rows = %d, want none", len(got))
	}
	if got := f.currentTask(t).Result; got != nil {
		t.Fatalf("task result = %v, want untouched", *got)
	}
	if len(f.memory.files) != 0 {
		t.Fatalf("projection writes = %#v, want none", f.memory.files)
	}
}

func TestApplyAgentProfileResultPersistsRevisionTaskResultAndProjection(t *testing.T) {
	f := setupProfileLifecycle(t)
	got, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 0, validProfileDimensions(), []string{"主页样本有限"}, []string{"代表作品"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || got.InitializationStatus != model.ProfileInitializationReady || got.AnalysisTaskID != f.taskID {
		t.Fatalf("profile metadata = version %d status %q task %q", got.Version, got.InitializationStatus, got.AnalysisTaskID)
	}
	if got.Status != model.ProfileStatusDraft {
		t.Fatalf("profile status = %q, want an unconfirmed first-result draft", got.Status)
	}
	rows := f.revisions(t)
	if len(rows) != 1 || rows[0].Revision != 1 || rows[0].SourceTaskID != f.taskID {
		t.Fatalf("revisions = %#v, want one revision 1 sourced from task", rows)
	}
	var dimensions model.ProjectProfileDimensions
	if err := json.Unmarshal(rows[0].SixDimensions, &dimensions); err != nil {
		t.Fatal(err)
	}
	if dimensions.Identity.Content["summary"] != "账号定位" {
		t.Fatalf("revision identity = %#v", dimensions.Identity)
	}
	result := f.currentTask(t).Result
	if result == nil || !strings.Contains(*result, `"profile_revision":1`) {
		t.Fatalf("task result = %v, want revision summary", result)
	}
	if len(f.memory.files) != 0 {
		t.Fatalf("unconfirmed draft was projected into project memory: %#v", f.memory.files)
	}
	confirmed, err := f.service.ConfirmProfile(context.Background(), f.userID, f.projectID, got.Version, got)
	if err != nil {
		t.Fatalf("ConfirmProfile: %v", err)
	}
	if confirmed.Status != model.ProfileStatusConfirmed || confirmed.Version != 2 {
		t.Fatalf("confirmed profile metadata = status %q version %d", confirmed.Status, confirmed.Version)
	}
	if len(f.memory.files) != 7 {
		t.Fatalf("projected files = %d, want six dimensions and AGENTS.md after confirmation", len(f.memory.files))
	}
	if !strings.Contains(f.memory.files["AGENTS.md"], "profile/preferences.md") || !strings.Contains(f.memory.files["profile/memory.md"], "复盘经验") {
		t.Fatalf("projection missing expected AGENTS/profile content: %#v", f.memory.files)
	}
	state, err := f.repo.ProjectProfileStates().FindByProjectID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != model.ProfileInitializationReady || state.Revision != 2 || state.ActiveTaskID != f.taskID {
		t.Fatalf("profile state = %+v, want ready revision 2 linked to task", state)
	}
}

func TestConfirmedProfileRemainsAvailableDuringRefreshAndRefreshReplacesIt(t *testing.T) {
	f := setupProfileLifecycle(t)
	firstDimensions := validProfileDimensions()
	first, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 0, firstDimensions, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := f.service.ConfirmProfile(context.Background(), f.userID, f.projectID, first.Version, first)
	if err != nil {
		t.Fatal(err)
	}

	refreshTaskID := uuid.NewString()
	if err := f.repo.Tasks().Create(context.Background(), &model.Task{ID: refreshTaskID, UserID: f.userID, ProjectID: f.projectID, Type: model.TaskTypeProfileAnalysis, Status: model.TaskStatusRunning}); err != nil {
		t.Fatal(err)
	}
	refreshing, err := f.service.SetProfileAnalysisTaskID(context.Background(), f.userID, f.projectID, confirmed.Version, refreshTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if refreshing.Status != model.ProfileStatusConfirmed || refreshing.Dimensions.Identity.Content["summary"] != "账号定位" {
		t.Fatalf("confirmed profile was hidden during refresh: status=%q dimensions=%#v", refreshing.Status, refreshing.Dimensions.Identity.Content)
	}

	updated := validProfileDimensions()
	updated.Identity.Content["summary"] = "更新后的账号定位"
	refreshed, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, refreshTaskID, refreshing.Version, updated, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.Status != model.ProfileStatusConfirmed || refreshed.Dimensions.Identity.Content["summary"] != "更新后的账号定位" {
		t.Fatalf("refresh did not replace the confirmed profile: status=%q identity=%#v", refreshed.Status, refreshed.Dimensions.Identity.Content)
	}
}

func TestUpdateProfileLifecycleSynchronizesStateReadModel(t *testing.T) {
	f := setupProfileLifecycle(t)
	if err := UpdateProfileLifecycle(context.Background(), f.repo, f.projectID, f.taskID, model.ProfileInitializationFailed, "runtime failed"); err != nil {
		t.Fatal(err)
	}
	profile := f.currentProfile(t)
	if profile.InitializationStatus != model.ProfileInitializationFailed || profile.LastError != "runtime failed" {
		t.Fatalf("profile lifecycle = %+v, want failed state and error", profile)
	}
	state, err := f.repo.ProjectProfileStates().FindByProjectID(context.Background(), f.projectID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != model.ProfileInitializationFailed || state.Revision != profile.Version || state.ActiveTaskID != f.taskID || state.LastError != "runtime failed" {
		t.Fatalf("profile state = %+v, want synchronized failure marker", state)
	}
}

func TestProfileCloudOutcomeCarriesAcceptedRevision(t *testing.T) {
	f := setupProfileLifecycle(t)
	if _, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 0, validProfileDimensions(), nil, nil); err != nil {
		t.Fatal(err)
	}
	task := f.currentTask(t)
	execution := &model.TaskExecution{ID: uuid.NewString(), TaskID: task.ID, ManifestSealed: false}
	logger := zerolog.Nop()
	taskService := NewTaskService(f.repo, nil, nil, &logger, "", nil, nil)
	_, _, normalized, err := taskService.cloudTerminalOutcome(context.Background(), task, execution, &agent.ExecutionResult{Success: true})
	if err != nil {
		t.Fatal(err)
	}
	if !normalized.Success || normalized.ResultSubtype != "profile_result" || normalized.ProfileRevision != 1 {
		t.Fatalf("normalized profile result = %+v, want subtype profile_result revision 1", normalized)
	}
}

func TestApplyAgentProfileResultRejectsInvalidDimensionsWithoutChangingState(t *testing.T) {
	invalidCases := []struct {
		name   string
		mutate func(*model.ProjectProfileDimensions)
	}{
		{name: "nil content", mutate: func(d *model.ProjectProfileDimensions) { d.Identity.Content = nil }},
		{name: "nil sources", mutate: func(d *model.ProjectProfileDimensions) { d.Style.Sources = nil }},
		{name: "nil evidence", mutate: func(d *model.ProjectProfileDimensions) { d.Audience.Evidence = nil }},
		{name: "nil missing fields", mutate: func(d *model.ProjectProfileDimensions) { d.Platforms.MissingFields = nil }},
		{name: "unsupported source", mutate: func(d *model.ProjectProfileDimensions) { d.Preferences.Sources = []string{"[fabricated]"} }},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			f := setupProfileLifecycle(t)
			beforeProfile := f.currentProfile(t)
			beforeTask := f.currentTask(t).Result
			beforeFiles := map[string]string{}
			for path, content := range f.memory.files {
				beforeFiles[path] = content
			}
			dimensions := validProfileDimensions()
			tc.mutate(&dimensions)
			if _, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 0, dimensions, nil, nil); err == nil {
				t.Fatal("ApplyAgentProfileResult succeeded with invalid dimensions")
			}
			if got := f.currentProfile(t); got.Version != beforeProfile.Version || got.InitializationStatus != beforeProfile.InitializationStatus {
				t.Fatalf("profile changed after invalid submission: %+v", got)
			}
			if got := f.revisions(t); len(got) != 0 {
				t.Fatalf("revision rows = %d, want none", len(got))
			}
			if got := f.currentTask(t).Result; (got == nil) != (beforeTask == nil) {
				t.Fatalf("task result changed: before=%v after=%v", beforeTask, got)
			}
			if len(f.memory.files) != len(beforeFiles) {
				t.Fatalf("projection changed: before=%#v after=%#v", beforeFiles, f.memory.files)
			}
		})
	}
}

func TestApplyAgentProfileResultAllowsOnlyOneRevisionPerTask(t *testing.T) {
	f := setupProfileLifecycle(t)
	if _, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 0, validProfileDimensions(), nil, nil); err != nil {
		t.Fatal(err)
	}
	updated := validProfileDimensions()
	updated.Identity.Content["summary"] = "第二次提交"
	if _, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 1, updated, nil, nil); err == nil {
		t.Fatal("second submission for same task succeeded")
	}
	profile := f.currentProfile(t)
	if profile.Version != 1 || profile.Dimensions.Identity.Content["summary"] != "账号定位" {
		t.Fatalf("duplicate submission changed profile: %+v", profile)
	}
	if got := f.revisions(t); len(got) != 1 {
		t.Fatalf("revision rows = %d, want one", len(got))
	}
}

func TestApplyAgentProfileResultKeepsDatabaseSuccessWhenProjectionFails(t *testing.T) {
	f := setupProfileLifecycle(t)
	failing := &failingProfileProjectionMemory{err: errors.New("memory temporarily unavailable")}
	f.service.SetProjectProfileMemory(failing)

	got, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 0, validProfileDimensions(), nil, nil)
	if err != nil {
		t.Fatalf("ApplyAgentProfileResult error = %v, want database success despite projection failure", err)
	}
	if got.Version != 1 || got.InitializationStatus != model.ProfileInitializationReady {
		t.Fatalf("profile = %+v, want ready revision 1", got)
	}
	if len(f.revisions(t)) != 1 || f.currentTask(t).Result == nil {
		t.Fatal("database result was not committed before projection failure")
	}
}

func TestApplyAgentProfileResultRejectsTerminalTask(t *testing.T) {
	f := setupProfileLifecycle(t)
	if err := f.repo.Tasks().UpdateStatus(context.Background(), f.taskID, model.TaskStatusCompleted); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ApplyAgentProfileResult(context.Background(), f.userID, f.projectID, f.taskID, 0, validProfileDimensions(), nil, nil); err == nil {
		t.Fatal("ApplyAgentProfileResult accepted a terminal profile task")
	}
	if len(f.revisions(t)) != 0 || f.currentTask(t).Result != nil {
		t.Fatal("terminal task submission changed profile state")
	}
}
