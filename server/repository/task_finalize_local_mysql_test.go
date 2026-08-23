package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/anbanai/anban-creator/server/model"
)

func TestFinalizeLocalTaskMySQLLocksExecutionBeforeTask(t *testing.T) {
	db, mock := openTaskEvidenceMySQLMockDB(t)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL expectations: %v", err)
		}
	})

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("execution-1"))
	mock.ExpectExec("UPDATE `tasks` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE `task_executions` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	won, err := newTaskRepository(db).FinalizeLocalTask(
		context.Background(),
		"task-1",
		"execution-1",
		model.TaskStatusCompleted,
		"",
		`{"success":true}`,
		nil,
		"",
	)
	if err != nil || !won {
		t.Fatalf("FinalizeLocalTask = %v, %v, want true, nil", won, err)
	}
}

func TestFinalizeLocalTaskMySQLTaskGuardMissRollsBackAsCASLost(t *testing.T) {
	db, mock := openTaskEvidenceMySQLMockDB(t)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL expectations: %v", err)
		}
	})

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("execution-1"))
	mock.ExpectExec("UPDATE `tasks` SET").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	won, err := newTaskRepository(db).FinalizeLocalTask(
		context.Background(),
		"task-1",
		"execution-1",
		model.TaskStatusCompleted,
		"",
		`{"success":true}`,
		nil,
		"",
	)
	if won || !errors.Is(err, ErrLocalTaskExecutionCASLost) {
		t.Fatalf("FinalizeLocalTask = %v, %v, want false, ErrLocalTaskExecutionCASLost", won, err)
	}
}

func TestFinalizeLocalTaskMySQLExecutionGuardMissRollsBackAsCASLost(t *testing.T) {
	db, mock := openTaskEvidenceMySQLMockDB(t)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL expectations: %v", err)
		}
	})

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()

	won, err := newTaskRepository(db).FinalizeLocalTask(
		context.Background(), "task-1", "execution-1", model.TaskStatusCompleted,
		"", `{"success":true}`, nil, "",
	)
	if won || !errors.Is(err, ErrLocalTaskExecutionCASLost) {
		t.Fatalf("FinalizeLocalTask = %v, %v, want false, ErrLocalTaskExecutionCASLost", won, err)
	}
}

func TestFinalizeLocalTaskMySQLExecutionCASLossRollsBackTaskUpdate(t *testing.T) {
	db, mock := openTaskEvidenceMySQLMockDB(t)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL expectations: %v", err)
		}
	})

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("execution-1"))
	mock.ExpectExec("UPDATE `tasks` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE `task_executions` SET").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	won, err := newTaskRepository(db).FinalizeLocalTask(
		context.Background(), "task-1", "execution-1", model.TaskStatusCompleted,
		"", `{"success":true}`, nil, "",
	)
	if won || !errors.Is(err, ErrLocalTaskExecutionCASLost) {
		t.Fatalf("FinalizeLocalTask = %v, %v, want false, ErrLocalTaskExecutionCASLost", won, err)
	}
}

func TestFinalizeLocalTaskMySQLLockErrorPreservesCause(t *testing.T) {
	db, mock := openTaskEvidenceMySQLMockDB(t)
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Errorf("SQL expectations: %v", err)
		}
	})

	lockErr := fmt.Errorf("lock wait interrupted")
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT .* FROM `task_executions` .*FOR UPDATE").WillReturnError(lockErr)
	mock.ExpectRollback()

	won, err := newTaskRepository(db).FinalizeLocalTask(
		context.Background(), "task-1", "execution-1", model.TaskStatusCompleted,
		"", `{"success":true}`, nil, "",
	)
	if won || !errors.Is(err, lockErr) || errors.Is(err, ErrLocalTaskExecutionCASLost) || !strings.Contains(err.Error(), "lock local task execution") {
		t.Fatalf("FinalizeLocalTask = %v, %v, want wrapped lock cause and not CAS-lost", won, err)
	}
}
