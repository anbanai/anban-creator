package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestResolveTaskIdentityRequiresExplicitIdentity(t *testing.T) {
	_, _, _, _, err := resolveTaskIdentity(&model.Project{Platform: model.PlatformWechat}, CreateManualParams{})
	if err == nil || !strings.Contains(err.Error(), "agent_id, channel, and task_kind are required") {
		t.Fatalf("resolveTaskIdentity error = %v, want explicit identity requirement", err)
	}
}

func TestResolveTaskIdentityRejectsAgentChannelMismatch(t *testing.T) {
	_, _, _, _, err := resolveTaskIdentity(nil, CreateManualParams{
		AgentID:  model.AgentIDArticle,
		Channel:  model.ChannelSeednote,
		TaskKind: model.TaskKindContentGeneration,
	})
	if err == nil || !strings.Contains(err.Error(), "bound to channel") {
		t.Fatalf("resolveTaskIdentity error = %v, want channel mismatch", err)
	}
}

func TestResolveTaskIdentityRejectsUnsupportedTaskKind(t *testing.T) {
	_, _, _, _, err := resolveTaskIdentity(nil, CreateManualParams{
		AgentID:  model.AgentIDArticle,
		Channel:  model.ChannelArticle,
		TaskKind: model.TaskKindViralAnalysis,
	})
	if err == nil || !strings.Contains(err.Error(), "does not support task kind") {
		t.Fatalf("resolveTaskIdentity error = %v, want unsupported task kind", err)
	}
}

// Test fixtures declare the output independently of the account project.
func testOutputIdentity(taskType string) (string, string, string) {
	switch taskType {
	case model.PlatformWechat, model.TaskTypeWechatArticle:
		return model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration
	case model.PlatformSeednote:
		return model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindContentGeneration
	case model.PlatformMontage:
		return model.AgentIDMontage, model.ChannelMontage, model.PlatformMontage
	case model.PlatformHypit:
		return model.AgentIDHypit, model.ChannelHypit, model.PlatformHypit
	case model.PlatformMoments:
		return "moments", model.ChannelMoments, model.PlatformMoments
	case model.PlatformEcommerce:
		return "ecommerce", model.ChannelEcommerce, model.PlatformEcommerce
	case model.TaskTypeViralAnalysis:
		return model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindViralAnalysis
	default:
		panic("unsupported test output: " + taskType)
	}
}

func TestManualTaskRequiresCompleteIdentityBeforeAdmission(t *testing.T) {
	for _, missing := range []string{"all", "agent", "channel", "kind"} {
		t.Run(missing, func(t *testing.T) {
			svc, repo := setupTaskServiceWithEnqueuer(t)
			projectID := createTestProject(t, repo, "identity-user", model.PlatformWechat)
			p := CreateManualParams{UserID: "identity-user", ProjectID: projectID, ExecutionProfile: "effective", RequestedTaskType: model.TaskTypeWechatArticle, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration}
			switch missing {
			case "all":
				p.AgentID, p.Channel, p.TaskKind = "", "", ""
			case "agent":
				p.AgentID = ""
			case "channel":
				p.Channel = ""
			case "kind":
				p.TaskKind = ""
			}
			if tasks, err := svc.CreateManual(t.Context(), p); err == nil || len(tasks) != 0 {
				t.Fatalf("incomplete identity admitted: tasks=%#v err=%v", tasks, err)
			}
			_, total, err := svc.List(t.Context(), "identity-user", 0, 10, "", "", "")
			if err != nil || total != 0 {
				t.Fatalf("task admission mutated state: total=%d err=%v", total, err)
			}
		})
	}
}

func TestPlanEntryIdentityIndependentOfProjectPlatform(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	projectID := createTestProject(t, repo, "entry-user", model.PlatformWechat)
	plan := &model.Plan{ID: "multi-output-plan", UserID: "entry-user", ProjectID: projectID, Prompt: "shared brief", ImageRatio: "auto"}
	for _, tc := range []struct{ agent, channel, kind, taskType string }{
		{model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration, model.TaskTypeWechatArticle},
		{model.AgentIDWechatPicture, model.ChannelWechatPicture, model.TaskKindContentGeneration, model.TaskTypeWechatPicture},
		{model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindContentGeneration, model.PlatformSeednote},
	} {
		t.Run(tc.agent, func(t *testing.T) {
			entry := &model.PlanEntry{PlanID: plan.ID, AgentID: tc.agent, Channel: tc.channel, TaskKind: tc.kind, ExecutionProfile: "effective"}
			task, err := svc.CreateFromPlanEntry(t.Context(), plan, entry)
			if err != nil {
				t.Fatal(err)
			}
			if task.AgentID != tc.agent || task.Channel != tc.channel || task.TaskKind != tc.kind || task.Type != tc.taskType || task.ProjectSnapshot.Data().Platform != model.PlatformWechat {
				t.Fatalf("entry identity or account context changed: %#v", task)
			}
			if plan.AgentID != "" || plan.Channel != "" || plan.TaskKind != "" {
				t.Fatal("plan was mutated with output identity")
			}
		})
	}
}

func TestPlanEntryPortraitCoverAndSnapshotUseSelectedPlanAsset(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	ctx := t.Context()
	userID := "plan-portrait-owner"
	projectID := createTestProject(t, repo, userID, model.PlatformWechat)
	projectDefault := referenceAssetFixture("plan-portrait-project-default", userID, DirectUploadPurposeProjectPortraitReference)
	selected := referenceAssetFixture("plan-portrait-selected", userID, DirectUploadPurposeProjectPortraitReference)
	seedReferenceAsset(t, repo, projectDefault)
	seedReferenceAsset(t, repo, selected)
	project, err := repo.Projects().FindByID(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	project.PortraitReferenceImageAssetID = projectDefault.ID
	if err := repo.Projects().Update(ctx, project); err != nil {
		t.Fatal(err)
	}
	svc.SetReferenceAssetService(NewReferenceAssetService(repo, nil, time.Now))
	plan := &model.Plan{
		ID: "plan-portrait-mixed", UserID: userID, ProjectID: projectID,
		Prompt: "scheduled topic", ImageRatio: model.ImageRatioAuto,
		PortraitReferenceImageAssetID: selected.ID, PortraitReferenceConfigured: true,
		CoverUsePortrait: true,
	}

	article, err := svc.CreateFromPlanEntry(ctx, plan, &model.PlanEntry{
		PlanID: plan.ID, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle,
		TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !article.CoverUsePortrait || article.ProjectSnapshot.Data().PortraitReferenceImageAssetID != selected.ID {
		t.Fatalf("article portrait settings = cover %v, frozen asset %q", article.CoverUsePortrait, article.ProjectSnapshot.Data().PortraitReferenceImageAssetID)
	}
	legacyPlan := &model.Plan{ID: "legacy-plan-portrait", UserID: userID, ProjectID: projectID, ExecutionProfile: "effective"}
	legacyTask, err := svc.CreateFromPlanEntry(ctx, legacyPlan, &model.PlanEntry{
		PlanID: legacyPlan.ID, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle,
		TaskKind: model.TaskKindContentGeneration, ExecutionProfile: "effective",
	})
	if err != nil {
		t.Fatal(err)
	}
	if legacyTask.CoverUsePortrait || legacyTask.ProjectSnapshot.Data().PortraitReferenceImageAssetID != projectDefault.ID {
		t.Fatalf("legacy plan should inherit the project image without enabling it as a cover: cover %v asset %q", legacyTask.CoverUsePortrait, legacyTask.ProjectSnapshot.Data().PortraitReferenceImageAssetID)
	}

	if !planEntryUsesPortraitCover(true, model.AgentIDArticle, model.ChannelArticle, model.TaskKindContentGeneration) {
		t.Fatal("supported entry should use the plan portrait cover")
	}
	if planEntryUsesPortraitCover(true, "ecommerce", model.ChannelEcommerce, model.PlatformEcommerce) {
		t.Fatal("unsupported entry should skip the plan portrait cover")
	}
	if !planEntryUsesPortraitCover(true, model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindContentGeneration) {
		t.Fatal("seednote cover support should be evaluated independently")
	}
}

func TestPlanTaskDoesNotInferIdentityFromProject(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	projectID := createTestProject(t, repo, "plan-user", model.PlatformWechat)
	plan := &model.Plan{ID: "missing-entry", UserID: "plan-user", ProjectID: projectID, ExecutionProfile: "effective"}
	if task, err := svc.CreateFromPlan(t.Context(), plan); err == nil || task != nil {
		t.Fatalf("missing identity admitted: %#v %v", task, err)
	}
}

func TestCloneAndResumeRejectIncompleteOrMismatchedIdentity(t *testing.T) {
	for _, mutation := range []string{"agent", "channel", "kind", "type", "mismatch"} {
		t.Run(mutation, func(t *testing.T) {
			svc, repo := setupTaskServiceWithEnqueuer(t)
			projectID := createTestProject(t, repo, "retry-user", model.PlatformWechat)
			task := &model.Task{ID: "invalid-identity", UserID: "retry-user", ProjectID: projectID, AgentID: model.AgentIDArticle, Channel: model.ChannelArticle, TaskKind: model.TaskKindContentGeneration, Type: model.TaskTypeWechatArticle, Status: model.TaskStatusFailed}
			switch mutation {
			case "agent":
				task.AgentID = ""
			case "channel":
				task.Channel = ""
			case "kind":
				task.TaskKind = ""
			case "type":
				task.Type = ""
			case "mismatch":
				task.Type = model.PlatformSeednote
			}
			if err := repo.Tasks().Create(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			if tasks, err := svc.Clone(t.Context(), task.ID, CloneTaskParams{ExecutionProfile: "effective"}); !errors.Is(err, ErrTaskIdentityIncompatible) || len(tasks) != 0 {
				t.Fatalf("clone accepted invalid identity: %#v %v", tasks, err)
			}
			if resumed, err := svc.Resume(t.Context(), task.UserID, task.ID, ResumeTaskParams{Prompt: "continue"}); !errors.Is(err, ErrTaskIdentityIncompatible) || resumed != nil {
				t.Fatalf("resume accepted invalid identity: %#v %v", resumed, err)
			}
			stored, err := repo.Tasks().FindByID(t.Context(), task.ID)
			if err != nil || stored.Status != model.TaskStatusFailed || len(stored.InputAttachments.Data()) != 0 {
				t.Fatalf("invalid retry mutated task: %#v %v", stored, err)
			}
		})
	}
}

func TestAIEntryRejectsMissingIdentityBeforeIntentParsing(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	projectID := createTestProject(t, repo, "ai-user", model.PlatformWechat)
	llm := &fakeAIEntryLLM{}
	entry := NewAIEntryService(repo, svc, llm, nil, AIEntryModelConfig{}, nil)
	result, err := entry.Submit(t.Context(), AIEntrySubmitRequest{UserID: "ai-user", ProjectID: projectID, ExecutionProfile: "effective", Text: "write an article"})
	if err != nil || result.Status != AIEntryStatusNeedsConfiguration || len(llm.calls) != 0 {
		t.Fatalf("missing identity reached intent parsing: %#v err=%v calls=%d", result, err, len(llm.calls))
	}
}

func TestPlanEntryRejectsAgentsThatRequireManualTaskInputs(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	projectID := createTestProject(t, repo, "entry-input-user", model.PlatformWechat)
	plan := &model.Plan{ID: "manual-only-output", UserID: "entry-input-user", ProjectID: projectID, Prompt: "brief"}
	for _, tc := range []struct{ agent, channel, kind string }{
		{model.AgentIDMontage, model.ChannelMontage, model.PlatformMontage},
		{model.AgentIDHypit, model.ChannelHypit, model.PlatformHypit},
		{model.AgentIDSeednote, model.ChannelSeednote, model.TaskKindViralAnalysis},
	} {
		entry := &model.PlanEntry{PlanID: plan.ID, AgentID: tc.agent, Channel: tc.channel, TaskKind: tc.kind, ExecutionProfile: "effective"}
		if task, err := svc.CreateFromPlanEntry(t.Context(), plan, entry); err == nil || task != nil {
			t.Fatalf("manual-only entry admitted: %#v %v", task, err)
		}
	}
}
