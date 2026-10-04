package service

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
)

func TestHypitPortraitCoverFollowsVideoRatio(t *testing.T) {
	for _, tc := range []struct{ name, input, defaults, want string }{
		{"horizontal", "16:9", "", "16:9"}, {"source", "source", "16:9", "auto"}, {"project default", "", "16:9", "16:9"}, {"unspecified", "", "", "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tasks, repo := setupTaskServiceWithEnqueuer(t)
			tasks.SetHypitConfig(hypitTestConfig())
			tasks.SetRuntimeDispatcher(&dispatchTestDispatcher{runtimeSelection: config.RuntimeImageSelection{Profile: "hypit", Image: "image@sha256:pinned"}})
			user := "hypit-cover-owner"
			id := createTestProject(t, repo, user, model.PlatformHypit)
			portrait := referenceAssetFixture("portrait", user, DirectUploadPurposeProjectPortraitReference)
			seedReferenceAsset(t, repo, portrait)
			project, _ := repo.Projects().FindByID(t.Context(), id)
			project.PortraitReferenceImageAssetID = portrait.ID
			project.ImageRatio = "9:16"
			project.SetHypitDefaults(model.HypitDefaults{Preferences: model.HypitPreferences{AspectRatio: tc.defaults}})
			if err := repo.Projects().Update(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			tasks.SetReferenceAssetService(NewReferenceAssetService(repo, nil, nil))
			input := model.HypitInput{Brief: "replicate", Reference: &model.HypitAsset{Type: "video_url", URL: "https://example.com/watch"}, Preferences: model.HypitPreferences{AspectRatio: tc.input}}
			created, err := tasks.CreateManual(t.Context(), CreateManualParams{AgentID: model.AgentIDHypit, Channel: model.ChannelHypit, TaskKind: model.PlatformHypit, UserID: user, ProjectID: id, ExecutionProfile: "effective", CoverUsePortrait: true, ImageRatio: "9:16", HypitInput: &input})
			if err != nil {
				t.Fatal(err)
			}
			task := created[0]
			if task.ImageRatio != tc.want {
				t.Fatalf("task cover ratio=%q want %q", task.ImageRatio, tc.want)
			}
			task.Status = model.TaskStatusCompleted
			if err := repo.Tasks().Update(t.Context(), task); err != nil {
				t.Fatal(err)
			}
			project.SetHypitDefaults(model.HypitDefaults{Preferences: model.HypitPreferences{AspectRatio: "1:1"}})
			if err := repo.Projects().Update(t.Context(), project); err != nil {
				t.Fatal(err)
			}
			clones, err := tasks.Clone(t.Context(), task.ID, CloneTaskParams{ExecutionProfile: "effective"})
			if err != nil {
				t.Fatal(err)
			}
			if clones[0].ImageRatio != tc.want {
				t.Fatalf("clone cover ratio=%q want frozen %q", clones[0].ImageRatio, tc.want)
			}
			assertHypitImageCoverGeneration(t, task.ImageRatio)
		})
	}
}

// Exercise the real image service using the ratio frozen by task admission.
func assertHypitImageCoverGeneration(t *testing.T, frozenRatio string) {
	t.Helper()
	f := newTaskImageFixture(t)
	task, err := f.repo.Tasks().FindByID(t.Context(), f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	task.Type = model.PlatformHypit
	task.ImageRatio = frozenRatio
	if err := f.repo.Tasks().Update(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	actualRatio, width, height := "16:9", 16, 9
	if frozenRatio == model.ImageRatioAuto {
		actualRatio, width, height = "4:3", 4, 3
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.generator.result.LocalFilePath, buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	req := f.request()
	req.AspectRatio = actualRatio
	if _, err := f.service.Generate(t.Context(), req); err != nil {
		t.Fatalf("final-video cover ratio rejected: %v", err)
	}
	req.AspectRatio = "2:3"
	if _, err := f.service.Generate(t.Context(), req); err == nil {
		t.Fatal("unsupported video cover ratio accepted")
	}
}

func TestHypitProfileExposesFrozenCoverInputsWithoutImageResolver(t *testing.T) {
	tasks, repo := setupTaskServiceWithEnqueuer(t)
	id := createTestProject(t, repo, "profile-owner", model.PlatformHypit)
	task := &model.Task{AgentID: model.AgentIDHypit, Channel: model.ChannelHypit, TaskKind: model.PlatformHypit, ID: "hypit-profile-task", UserID: "profile-owner", ProjectID: id, Type: model.PlatformHypit, ImageRatio: "16:9", CoverUsePortrait: true, ImageCapabilityKey: "frozen-image-model", ReferenceImageAssetID: "task-reference"}
	task.SetProjectSnapshot(model.ProjectSnapshot{Platform: model.PlatformHypit, ProjectName: "frozen name", VisualStyle: "frozen style", ReferenceImageAssetID: "style-reference", PortraitReferenceImageAssetID: "frozen-portrait", HypitDefaults: model.HypitDefaults{Preferences: model.HypitPreferences{AspectRatio: "16:9"}}})
	if err := repo.Tasks().Create(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	logger := zerolog.Nop()
	profiles := NewAgentProjectProfileService(NewProjectService(repo, &logger), tasks, nil, config.MontageConfig{}, nil)
	profile, err := profiles.Get(t.Context(), AgentProjectProfileRequest{UserID: task.UserID, ProjectID: id, TaskID: task.ID})
	if err != nil {
		t.Fatal(err)
	}
	resolved, ok := (*profile)["resolved_profile"].(map[string]any)
	if !ok || resolved["project_portrait_reference_path"] != ".anban-creator/project-portrait-reference.png" || resolved["image_ratio"] != "16:9" || resolved["uses_project_snapshot"] != true {
		t.Fatalf("missing frozen cover profile: %+v", profile)
	}
	for key, want := range map[string]string{"visual_style": "frozen style", "task_reference_path": ".anban-creator/task-reference.png", "project_style_reference_path": ".anban-creator/project-style-reference.png", "image_capability_key": "frozen-image-model"} {
		if resolved[key] != want {
			t.Fatalf("%s=%v want %s", key, resolved[key], want)
		}
	}
	allowed, ok := resolved["allowed_image_ratios"].([]string)
	if !ok || len(allowed) != 5 {
		t.Fatalf("allowed ratios=%v", resolved["allowed_image_ratios"])
	}
	if (*profile)["hypit"] == nil {
		t.Fatal("Hypit runtime profile lost")
	}
	if _, err := profiles.Get(t.Context(), AgentProjectProfileRequest{UserID: task.UserID, ProjectID: id}); err != nil {
		t.Fatalf("ordinary Hypit profile requires image resolver: %v", err)
	}
}
