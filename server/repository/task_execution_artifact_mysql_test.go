package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/anbanai/anban-creator/server/model"
)

func TestLockCurrentForArtifactMutationMySQLLocksRunningExecution(t *testing.T) {
	db, mock := openTaskEvidenceMySQLMockDB(t)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL expectations: %v", err)
		}
	})

	mock.ExpectQuery("SELECT `id` FROM `task_executions` .*status = .*started = .*completed_at IS NULL.*FOR UPDATE").
		WithArgs("execution-1", "task-1", model.TaskExecutionRunning, true, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("execution-1"))

	locked, err := newTaskExecutionRepository(db).LockCurrentForArtifactMutation(context.Background(), "execution-1", "task-1")
	if err != nil || !locked {
		t.Fatalf("LockCurrentForArtifactMutation = %v, %v, want true, nil", locked, err)
	}
}

func TestLockCurrentForArtifactMutationMySQLGuardMissAndError(t *testing.T) {
	t.Run("guard miss", func(t *testing.T) {
		db, mock := openTaskEvidenceMySQLMockDB(t)
		t.Cleanup(func() {
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("SQL expectations: %v", err)
			}
		})

		mock.ExpectQuery("SELECT `id` FROM `task_executions` .*FOR UPDATE").
			WithArgs("execution-1", "task-1", model.TaskExecutionRunning, true, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		locked, err := newTaskExecutionRepository(db).LockCurrentForArtifactMutation(context.Background(), "execution-1", "task-1")
		if err != nil || locked {
			t.Fatalf("LockCurrentForArtifactMutation = %v, %v, want false, nil", locked, err)
		}
	})

	t.Run("lock error", func(t *testing.T) {
		db, mock := openTaskEvidenceMySQLMockDB(t)
		t.Cleanup(func() {
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("SQL expectations: %v", err)
			}
		})
		wantErr := errors.New("lock wait interrupted")

		mock.ExpectQuery("SELECT `id` FROM `task_executions` .*FOR UPDATE").
			WithArgs("execution-1", "task-1", model.TaskExecutionRunning, true, 1).
			WillReturnError(wantErr)

		locked, err := newTaskExecutionRepository(db).LockCurrentForArtifactMutation(context.Background(), "execution-1", "task-1")
		if locked || !errors.Is(err, wantErr) {
			t.Fatalf("LockCurrentForArtifactMutation = %v, %v, want false and cause %v", locked, err, wantErr)
		}
	})
}
