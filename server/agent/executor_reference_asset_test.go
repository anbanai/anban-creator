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
	executor := NewLocalExecutor(&logger, nil, nil, "", false, "", nil, nil, nil, t.TempDir(), "", store, nil)
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
		dockerCfg: srvconfig.DockerConfig{},
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

func TestExecutorsRejectReferenceAssetSizeMismatchBeforeStartup(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*fakeStore, *ExecutionOptions) error
	}{
		{name: "local", run: func(store *fakeStore, opts *ExecutionOptions) error {
			logger := zerolog.Nop()
			executor := NewLocalExecutor(&logger, nil, nil, "", false, "", nil, nil, nil, t.TempDir(), "", store, nil)
			_, err := executor.Execute(t.Context(), opts)
			return err
		}},
		{name: "docker", run: func(store *fakeStore, opts *ExecutionOptions) error {
			logger := zerolog.Nop()
			executor := &DockerExecutor{logger: &logger, store: store, dockerCfg: srvconfig.DockerConfig{}}
			_, err := executor.Execute(t.Context(), opts)
			return err
		}},
	} {
		for _, mismatch := range []struct {
			name string
			data []byte
			size int64
		}{
			{name: "short", data: []byte("short"), size: 6},
			{name: "long", data: []byte("longer"), size: 5},
		} {
			t.Run(tc.name+"/"+mismatch.name, func(t *testing.T) {
				key := "assets/users/user-1/asset-1/reference.png"
				store := &fakeStore{readData: map[string][]byte{key: mismatch.data}}
				opts := referenceAssetExecutionOptions(key)
				opts.ReferenceAsset.Size = mismatch.size
				opts.Project = nil
				err := tc.run(store, opts)
				if err == nil || !strings.Contains(err.Error(), "materialize reference asset") || !strings.Contains(err.Error(), "size") {
					t.Fatalf("Execute error = %v, want pre-start size mismatch", err)
				}
			})
		}
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
