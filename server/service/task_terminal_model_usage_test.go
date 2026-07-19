package service

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/anbanai/anban-creator/server/agent"
	"github.com/anbanai/anban-creator/server/model"
)

func TestUpdateExecutionResultPersistsTerminalModelUsageForEveryOutcome(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(map[bool]string{true: "success", false: "failure"}[success], func(t *testing.T) {
			ctx := context.Background()
			repo := setupCreditTestRepo(t)
			task := &model.Task{
				ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformArticle,
				Status: model.TaskStatusRunning,
			}
			if err := repo.Tasks().Create(ctx, task); err != nil {
				t.Fatalf("create task: %v", err)
			}
			logger := zerolog.New(io.Discard)
			svc := NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
			usage := []agent.ModelTokenUsage{
				{
					Provider: "volcengine_ark", Model: "doubao-seed-2-1-turbo-260628",
					InputTokens: 7, OutputTokens: 11, CacheReadInputTokens: 13, CacheCreationInputTokens: 17,
				},
				{
					Provider: "volcengine_ark", Model: "doubao-seed-evolving",
					InputTokens: 19, OutputTokens: 23, CacheReadInputTokens: 29, CacheCreationInputTokens: 31,
				},
			}
			result := &agent.ExecutionResult{
				Success: success, ModelUsage: usage, CostStatus: agent.CostStatusReconciled,
			}

			if err := svc.UpdateExecutionResult(ctx, task.ID, result); err != nil {
				t.Fatalf("UpdateExecutionResult: %v", err)
			}
			found, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatalf("find task: %v", err)
			}
			if found.CostStatus != agent.CostStatusReconciled {
				t.Fatalf("CostStatus = %q, want reconciled", found.CostStatus)
			}
			if !reflect.DeepEqual(found.TerminalModelUsage.Data(), []model.ModelTokenUsage(usage)) {
				t.Fatalf("TerminalModelUsage = %#v, want %#v", found.TerminalModelUsage.Data(), usage)
			}
			if found.InputTokens != nil || found.OutputTokens != nil || found.CacheReadTokens != nil || found.CacheCreationTokens != nil || found.TotalCostUSD != nil {
				t.Fatalf("legacy usage columns were written: input=%v output=%v cache_read=%v cache_creation=%v cost=%v",
					found.InputTokens, found.OutputTokens, found.CacheReadTokens, found.CacheCreationTokens, found.TotalCostUSD)
			}
			if found.Result == nil {
				t.Fatal("execution result JSON was not persisted")
			}
			var persisted agent.ExecutionResult
			if err := json.Unmarshal([]byte(*found.Result), &persisted); err != nil {
				t.Fatalf("decode persisted result: %v", err)
			}
			if !reflect.DeepEqual(persisted.ModelUsage, usage) || persisted.CostStatus != agent.CostStatusReconciled {
				t.Fatalf("persisted execution result = %+v, want terminal usage", persisted)
			}
		})
	}
}

func TestUpdateExecutionResultPersistsUnreconciledTerminalStatus(t *testing.T) {
	ctx := context.Background()
	repo := setupCreditTestRepo(t)
	task := &model.Task{
		ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformArticle,
		Status: model.TaskStatusFailed,
	}
	if err := repo.Tasks().Create(ctx, task); err != nil {
		t.Fatalf("create task: %v", err)
	}
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)
	result := &agent.ExecutionResult{
		Success: false, CostStatus: agent.CostStatusUnreconciled,
		CostDiagnostics: []agent.CostDiagnostic{{Code: agent.CostDiagnosticMissingTerminalModelUsage}},
	}

	if err := svc.UpdateExecutionResult(ctx, task.ID, result); err != nil {
		t.Fatalf("UpdateExecutionResult: %v", err)
	}
	found, err := repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatalf("find task: %v", err)
	}
	if found.CostStatus != agent.CostStatusUnreconciled || len(found.TerminalModelUsage.Data()) != 0 {
		t.Fatalf("terminal cost evidence = status %q usage %+v, want unreconciled/empty", found.CostStatus, found.TerminalModelUsage.Data())
	}
}

func TestUpdateExecutionResultRejectsNilInsteadOfSilentlySkippingPersistence(t *testing.T) {
	repo := setupCreditTestRepo(t)
	logger := zerolog.New(io.Discard)
	svc := NewTaskService(repo, nil, nil, nil, nil, &logger, "", nil, "", nil, nil)

	if err := svc.UpdateExecutionResult(context.Background(), uuid.NewString(), nil); err == nil {
		t.Fatal("UpdateExecutionResult(nil) = nil, want explicit error")
	}
}
