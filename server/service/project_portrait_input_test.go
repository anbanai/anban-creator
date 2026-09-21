package service

import (
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/auth"
	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

func TestArticleProjectPortraitIsAnIndependentFrozenInput(t *testing.T) {
	for _, entry := range []string{"manual", "plan", "ai_entry"} {
		t.Run(entry, func(t *testing.T) {
			tasks, repo := setupTaskServiceWithEnqueuer(t)
			ctx := t.Context()
			userID := uuid.NewString()
			ensureTestUser(t, repo, userID)
			projectID := createTestProject(t, repo, userID, model.PlatformArticle)
			portrait := referenceAssetFixture("portrait", userID, DirectUploadPurposeProjectPortraitReference)
			seedReferenceAsset(t, repo, portrait)
			direct := referenceAssetFixture("product", userID, DirectUploadPurposeTaskReference)
			seedReferenceAsset(t, repo, direct)
			project, err := repo.Projects().FindByID(ctx, projectID)
			if err != nil {
				t.Fatal(err)
			}
			project.PortraitReferenceImageAssetID = portrait.ID
			if err := repo.Projects().Update(ctx, project); err != nil {
				t.Fatal(err)
			}
			store := &bootstrapSecurityStore{signFakeStore: &signFakeStore{}}
			tasks.SetReferenceAssetService(NewReferenceAssetService(repo, store, time.Now))
			var task *model.Task
			noCover := false
			switch entry {
			case "manual":
				created, createErr := tasks.CreateManual(ctx, CreateManualParams{UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Prompt: "article", ReferenceImageAssetID: direct.ID, ArticleWithCover: &noCover})
				if createErr != nil {
					t.Fatal(createErr)
				}
				task = created[0]
			case "plan":
				task, err = tasks.CreateFromPlan(ctx, &model.Plan{ID: uuid.NewString(), UserID: userID, ProjectID: projectID, Type: model.PlatformArticle, ExecutionProfile: "effective", Prompt: "article", ReferenceImageAssetID: direct.ID, ArticleWithCover: &noCover})
				if err != nil {
					t.Fatal(err)
				}
			case "ai_entry":
				entrySvc := NewAIEntryService(repo, tasks, &fakeAIEntryLLM{}, nil, AIEntryModelConfig{}, nil)
				entrySvc.SetReferenceAssetService(NewReferenceAssetService(repo, store, time.Now))
				result, submitErr := entrySvc.Submit(ctx, AIEntrySubmitRequest{UserID: userID, ProjectID: projectID, ExecutionProfile: "effective", Text: "article"})
				if submitErr != nil {
					t.Fatal(submitErr)
				}
				if result.Status != AIEntryStatusCreated {
					t.Fatalf("result = %#v", result)
				}
				task = result.Tasks[0]
			}
			// Later project edits cannot change the portrait already supplied to a task.
			project.PortraitReferenceImageAssetID = ""
			if err := repo.Projects().Update(ctx, project); err != nil {
				t.Fatal(err)
			}
			logger := zerolog.Nop()
			profiles := NewAgentProjectProfileService(NewProjectService(repo, &logger), tasks, nil, config.MontageConfig{}, tasks.imageCapabilities)
			profile, err := profiles.Get(ctx, AgentProjectProfileRequest{UserID: userID, ProjectID: projectID, TaskID: task.ID})
			if err != nil {
				t.Fatal(err)
			}
			resolved := (*profile)["resolved_profile"].(map[string]any)
			if resolved["project_portrait_reference_path"] != ".anban-creator/project-portrait-reference.png" {
				t.Fatalf("missing independent project portrait input: %#v", resolved)
			}
			if entry != "ai_entry" && task.ReferenceImageAssetID != direct.ID {
				t.Fatalf("direct product reference overwritten: %q", task.ReferenceImageAssetID)
			}
			tokens, _ := auth.NewExecutionTokenService("0123456789abcdef0123456789abcdef")
			bootstrap := NewAgentBootstrapService(repo, tokens, AgentBootstrapConfig{Store: store, TokenTTL: time.Hour, SignedURLTTL: 60}, logger)
			response, err := buildBootstrapTestResponse(t, bootstrap, ctx, &model.TaskExecution{ID: uuid.NewString()}, task, project, time.Now().Add(time.Hour))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, file := range response.Files {
				if file.Path == ".anban-creator/project-portrait-reference.png" {
					found = file.DownloadURL != ""
				}
				if file.Path == ".anban-creator/settings.json" && strings.Contains(file.Text, "project-portrait-reference.png") {
					t.Fatal("optional portrait forced into image generation settings")
				}
			}
			if !found {
				t.Fatal("project portrait not materialized")
			}
		})
	}
}
