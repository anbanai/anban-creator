package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
)

type beforeTerminalTaskTxRepository struct {
	repository.Repository
	before func() error
}

func (r *beforeTerminalTaskTxRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	if r.before != nil {
		if err := r.before(); err != nil {
			return err
		}
		r.before = nil
	}
	return r.Repository.WithTx(ctx, fn)
}

type failingTerminalBillingRepository struct {
	repository.Repository
	err error
}

func (r *failingTerminalBillingRepository) WithTx(ctx context.Context, fn func(repository.Repository) error) error {
	return r.Repository.WithTx(ctx, func(tx repository.Repository) error {
		return fn(&failingTerminalBillingTx{Repository: tx, billing: &failingTerminalBilling{
			BillingRepository: tx.Billing(),
			err:               r.err,
		}})
	})
}

type failingTerminalBillingTx struct {
	repository.Repository
	billing repository.BillingRepository
}

func (r *failingTerminalBillingTx) Billing() repository.BillingRepository { return r.billing }

type failingTerminalBilling struct {
	repository.BillingRepository
	err error
}

func (r *failingTerminalBilling) EnqueueSettlement(context.Context, *model.BillingSettlementOutbox) error {
	return r.err
}

func TestFailStaleRunningTaskForInfrastructureRevalidatesCurrentState(t *testing.T) {
	for _, race := range []string{"execution attached", "heartbeat refreshed"} {
		t.Run(race, func(t *testing.T) {
			svc, repo := setupTaskServiceWithEnqueuer(t)
			ctx := context.Background()
			stale := time.Now().Add(-10 * time.Minute)
			task := &model.Task{
				ID: uuid.NewString(), UserID: uuid.NewString(), Type: model.PlatformSeednote,
				Status: model.TaskStatusRunning, StartedAt: &stale,
			}
			if err := repo.Tasks().Create(ctx, task); err != nil {
				t.Fatal(err)
			}
			wrapped := &beforeTerminalTaskTxRepository{Repository: repo}
			wrapped.before = func() error {
				switch race {
				case "execution attached":
					executionID := uuid.NewString()
					execution := &model.TaskExecution{
						ID: executionID, TaskID: task.ID, Attempt: 1, Target: "kubernetes",
						Status: model.TaskExecutionRunning, Started: true, StartedAt: &stale,
					}
					if err := repo.TaskExecutions().Create(ctx, execution); err != nil {
						return err
					}
					won, err := repo.Tasks().SetCurrentExecution(ctx, task.ID, executionID)
					if err != nil || !won {
						return errors.New("failed to attach execution during reaper race")
					}
				case "heartbeat refreshed":
					return repo.Tasks().UpdateHeartbeat(ctx, task.ID)
				}
				return nil
			}
			svc.repo = wrapped

			won, err := svc.FailStaleRunningTaskForInfrastructure(ctx, task.ID, time.Now().Add(-5*time.Minute), "stale")
			if err != nil {
				t.Fatalf("FailStaleRunningTaskForInfrastructure: %v", err)
			}
			if won {
				t.Fatal("stale reaper won after task state changed")
			}
			persisted, err := repo.Tasks().FindByID(ctx, task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Status != model.TaskStatusRunning || persisted.CompletedAt != nil || persisted.BillingTerminalReason != "" {
				t.Fatalf("race-lost reaper changed task: %#v", persisted)
			}
		})
	}
}

func TestFailStaleRunningTaskForInfrastructureRollsBackWhenReversalEnqueueFails(t *testing.T) {
	ctx := context.Background()
	svc, fixture, _ := newFixedTaskBillingFixture(t, 1_000, 0)
	projectID := createTestProject(t, fixture.repo, billingWalletUserID, model.PlatformArticle)
	tasks, err := svc.CreateManual(ctx, CreateManualParams{
		ExecutionProfile: "effective", UserID: billingWalletUserID, ProjectID: projectID,
		Prompt: "stale billed task", Quantity: 1,
	})
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	task := tasks[0]
	stale := time.Now().Add(-10 * time.Minute)
	task.Status = model.TaskStatusRunning
	task.StartedAt = &stale
	if err := fixture.repo.Tasks().Update(ctx, task); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("settlement enqueue unavailable")
	svc.repo = &failingTerminalBillingRepository{Repository: fixture.repo, err: wantErr}

	won, err := svc.FailStaleRunningTaskForInfrastructure(ctx, task.ID, time.Now().Add(-5*time.Minute), "stale")
	if won || !errors.Is(err, wantErr) {
		t.Fatalf("FailStaleRunningTaskForInfrastructure = %v, %v; want false, root cause", won, err)
	}
	persisted, err := fixture.repo.Tasks().FindByID(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != model.TaskStatusRunning || persisted.CompletedAt != nil || persisted.BillingTerminalReason != "" {
		t.Fatalf("failed reversal enqueue left partial terminal task: %#v", persisted)
	}
}
