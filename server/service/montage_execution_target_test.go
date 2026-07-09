package service

import (
	"strings"
	"testing"

	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

func TestResolveMontageExecutionTargetDefaultsCloud(t *testing.T) {
	got, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
		Config: srvconfig.MontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"cloud", "local"},
			DefaultExecutionTarget: "cloud",
		},
		TaskType: model.PlatformMontage,
	})
	if err != nil {
		t.Fatalf("ResolveMontageExecutionTarget error = %v", err)
	}
	if got != model.ExecutionTargetCloud {
		t.Fatalf("target = %q, want cloud empty target", got)
	}
}

func TestResolveMontageExecutionTargetRejectsWhenDisabled(t *testing.T) {
	_, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
		Config:   srvconfig.MontageConfig{Enabled: false},
		TaskType: model.PlatformMontage,
	})
	if err == nil {
		t.Fatal("ResolveMontageExecutionTarget succeeded when disabled")
	}
}

func TestResolveMontageExecutionTargetKeepsLocalDisabledWithoutCapability(t *testing.T) {
	_, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
		Config: srvconfig.MontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"local"},
			DefaultExecutionTarget: "local",
		},
		TaskType:       model.PlatformMontage,
		LocalAvailable: false,
	})
	if err == nil {
		t.Fatal("ResolveMontageExecutionTarget succeeded without local capability")
	}
}

func TestResolveMontageExecutionTargetFallsBackFromLocalToCloud(t *testing.T) {
	got, err := ResolveMontageExecutionTarget(MontageExecutionTargetRequest{
		Config: srvconfig.MontageConfig{
			Enabled:                true,
			ExecutionTargets:       []string{"cloud", "local"},
			DefaultExecutionTarget: "local",
		},
		TaskType:       model.PlatformMontage,
		LocalAvailable: false,
	})
	if err != nil {
		t.Fatalf("ResolveMontageExecutionTarget error = %v", err)
	}
	if got != model.ExecutionTargetCloud {
		t.Fatalf("target = %q, want cloud fallback", got)
	}
}

func TestTaskServiceCreateManualMontageRejectsWhenDisabled(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	cfg := srvconfig.MontageConfig{Enabled: true}
	cfg.ApplyDefaults()
	cfg.Enabled = false
	svc.SetMontageConfig(cfg)

	userID := "user-om-disabled"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	_, err := svc.CreateManual(t.Context(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		MontageInput: &model.MontageInput{
			Brief: "应该被禁用拒绝",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "montage is disabled") {
		t.Fatalf("CreateManual error = %v, want disabled rejection", err)
	}
}

func TestTaskServiceCreateManualMontageUsesConfiguredLocalTarget(t *testing.T) {
	svc, repo := setupTaskServiceWithEnqueuer(t)
	cfg := srvconfig.MontageConfig{Enabled: true}
	cfg.ApplyDefaults()
	cfg.DefaultExecutionTarget = "local"
	cfg.ExecutionTargets = []string{"cloud", "local"}
	svc.SetMontageConfig(cfg)

	userID := "user-om-local"
	projectID := createTestProject(t, repo, userID, model.PlatformMontage)

	tasks, err := svc.CreateManual(t.Context(), CreateManualParams{
		UserID:    userID,
		ProjectID: projectID,
		MontageInput: &model.MontageInput{
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
