package service

import (
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestResolveOpenMontageExecutionTargetDefaultsCloud(t *testing.T) {
	got, err := ResolveOpenMontageExecutionTarget(OpenMontageExecutionTargetRequest{
		Config: srvconfig.OpenMontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"cloud", "local"},
			DefaultExecutionTarget: "cloud",
		},
		TaskType: model.PlatformOpenMontage,
	})
	if err != nil {
		t.Fatalf("ResolveOpenMontageExecutionTarget error = %v", err)
	}
	if got != model.ExecutionTargetCloud {
		t.Fatalf("target = %q, want cloud empty target", got)
	}
}

func TestResolveOpenMontageExecutionTargetRejectsWhenDisabled(t *testing.T) {
	_, err := ResolveOpenMontageExecutionTarget(OpenMontageExecutionTargetRequest{
		Config:   srvconfig.OpenMontageConfig{Enabled: false},
		TaskType: model.PlatformOpenMontage,
	})
	if err == nil {
		t.Fatal("ResolveOpenMontageExecutionTarget succeeded when disabled")
	}
}

func TestResolveOpenMontageExecutionTargetKeepsLocalDisabledWithoutCapability(t *testing.T) {
	_, err := ResolveOpenMontageExecutionTarget(OpenMontageExecutionTargetRequest{
		Config: srvconfig.OpenMontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"local"},
			DefaultExecutionTarget: "local",
		},
		TaskType:       model.PlatformOpenMontage,
		LocalAvailable: false,
	})
	if err == nil {
		t.Fatal("ResolveOpenMontageExecutionTarget succeeded without local capability")
	}
}

func TestResolveOpenMontageExecutionTargetFallsBackFromLocalToCloud(t *testing.T) {
	got, err := ResolveOpenMontageExecutionTarget(OpenMontageExecutionTargetRequest{
		Config: srvconfig.OpenMontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"cloud", "local"},
			DefaultExecutionTarget: "local",
		},
		TaskType:       model.PlatformOpenMontage,
		LocalAvailable: false,
	})
	if err != nil {
		t.Fatalf("ResolveOpenMontageExecutionTarget error = %v", err)
	}
	if got != model.ExecutionTargetCloud {
		t.Fatalf("target = %q, want cloud fallback", got)
	}
}

func TestTaskServiceCreateManualOpenMontageRejectsWhenDisabled(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	cfg := srvconfig.OpenMontageConfig{Enabled: true}
	cfg.ApplyDefaults()
	cfg.Enabled = false
	svc.SetOpenMontageConfig(cfg)

	userID := "user-om-disabled"
	projectID := createTestProject(t, repo, userID, model.PlatformOpenMontage)

	_, err := svc.CreateManual(t.Context(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		OpenMontageInput: &model.OpenMontageInput{
			Brief: "应该被禁用拒绝",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "openmontage is disabled") {
		t.Fatalf("CreateManual error = %v, want disabled rejection", err)
	}
}

func TestTaskServiceCreateManualOpenMontageUsesConfiguredLocalTarget(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	cfg := srvconfig.OpenMontageConfig{Enabled: true}
	cfg.ApplyDefaults()
	cfg.DefaultExecutionTarget = "local"
	cfg.ExecutionTargets = []string{"cloud", "local"}
	svc.SetOpenMontageConfig(cfg)

	userID := "user-om-local"
	projectID := createTestProject(t, repo, userID, model.PlatformOpenMontage)

	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		OpenMontageInput: &model.OpenMontageInput{
			Brief: "本机执行短片",
		},
	})
	if err != nil {
		t.Fatalf("CreateManual error = %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	if tasks[0].ExecutionTarget != model.ExecutionTargetLocal {
		t.Fatalf("execution_target = %q, want local", tasks[0].ExecutionTarget)
	}
	if tasks[0].LocalClaimDeadline == nil {
		t.Fatal("LocalClaimDeadline = nil, want local claim deadline")
	}
}
