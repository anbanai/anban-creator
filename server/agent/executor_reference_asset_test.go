package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestLocalExecutorReferenceAssetFailureStopsBeforeAgentStart(t *testing.T) {
	logger := zerolog.Nop()
	key := "assets/users/user-1/asset-1/reference.png"
	store := &fakeStore{readErr: context.Canceled}
	executor := NewLocalExecutor(&logger, nil, nil, "", false, "", nil, nil, t.TempDir(), "", store, nil)
	opts := referenceAssetExecutionOptions(key)

	result, err := executor.Execute(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "materialize reference asset") {
		t.Fatalf("Execute result=%#v err=%v, want materialization failure", result, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context cancellation chain", err)
	}
	if len(store.readKeys) != 1 || store.readKeys[0] != key {
		t.Fatalf("storage reads = %#v, want [%s]", store.readKeys, key)
	}
}

func TestDockerExecutorReferenceAssetFailureStopsBeforeContainerStart(t *testing.T) {
	logger := zerolog.Nop()
	key := "assets/users/user-1/asset-1/reference.png"
	store := &fakeStore{readErr: context.Canceled}
	executor := &DockerExecutor{
		logger:    &logger,
		store:     store,
		dockerCfg: srvconfig.DockerConfig{ContainerName: "agent-runtime", WorkspaceDir: t.TempDir()},
	}
	opts := referenceAssetExecutionOptions(key)

	result, err := executor.Execute(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "materialize reference asset") {
		t.Fatalf("Execute result=%#v err=%v, want materialization failure", result, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Execute error = %v, want context cancellation chain", err)
	}
	if len(store.readKeys) != 1 || store.readKeys[0] != key {
		t.Fatalf("storage reads = %#v, want [%s]", store.readKeys, key)
	}
}

func referenceAssetExecutionOptions(key string) *ExecutionOptions {
	task := &model.Task{ID: "task-1", UserID: "user-1", ProjectID: "project-1", Type: model.PlatformSeednote}
	project := &model.Project{ID: task.ProjectID, UserID: task.UserID, Platform: task.Type, Name: "project"}
	return &ExecutionOptions{
		Task: task, Project: project,
		ReferenceAsset: &model.Asset{ID: "asset-1", UserID: task.UserID, StorageKey: key, Size: 5},
	}
}
