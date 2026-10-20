package service

import (
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
)

func TestTaskServiceCreatesLocalLifeTaskWithValidatedInput(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	svc.SetLocalLifeCapabilityService(NewLocalLifeCapabilityService(true))
	svc.SetRuntimeDispatcher(&dispatchTestDispatcher{})
	userID := uuid.NewString()
	projectID := createTestProject(t, repo, userID, model.PlatformLocalLife)
	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{
		AgentID: model.AgentIDLocalLife, Channel: model.ChannelLocalLife, TaskKind: model.TaskKindContentGeneration,
		ExecutionProfile: "effective", UserID: userID, ProjectID: projectID,
		LocalLifeInput: &model.LocalLifeInput{
			MixType: "restaurant", Brief: "真实火锅店介绍",
			SourceAssets: []model.LocalLifeAsset{{Type: "image", URL: "https://cdn.example/assets/hotpot.jpg"}},
		},
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks", len(tasks))
	}
	input := tasks[0].LocalLifeInput.Data()
	if input.VariantCount != 3 || input.AspectRatio != "9:16" || tasks[0].Type != model.PlatformLocalLife {
		t.Fatalf("task type/input = %q %#v", tasks[0].Type, input)
	}
}
