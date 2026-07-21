package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
)

func openTaskEvidenceMySQLMockDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return db, mock
}

func TestTaskEvidenceMySQLZeroChangedRowsReadback(t *testing.T) {
	const (
		taskID      = "task-1"
		executionID = "execution-1"
		resultJSON  = `{"success":true,"cost_status":"reconciled"}`
		usageJSON   = `[{"provider":"provider","model":"model-1","input_tokens":7,"output_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}]`
	)
	usage := []model.ModelTokenUsage{{Provider: "provider", Model: "model-1", InputTokens: 7}}

	tests := []struct {
		name         string
		fenced       bool
		row          []driver.Value
		wantMatched  bool
		wantConflict bool
	}{
		{name: "generic missing"},
		{name: "generic identical", row: []driver.Value{taskID, nil, resultJSON, usageJSON, "reconciled"}, wantMatched: true},
		{name: "generic conflicting evidence", row: []driver.Value{taskID, nil, `{"success":false}`, usageJSON, "reconciled"}, wantConflict: true},
		{name: "cloud stale authority", fenced: true, row: []driver.Value{taskID, "execution-2", resultJSON, usageJSON, "reconciled"}},
		{name: "cloud identical", fenced: true, row: []driver.Value{taskID, executionID, resultJSON, usageJSON, "reconciled"}, wantMatched: true},
		{name: "cloud conflicting evidence", fenced: true, row: []driver.Value{taskID, executionID, resultJSON, `[]`, "reconciled"}, wantConflict: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db, mock := openTaskEvidenceMySQLMockDB(t)
			t.Cleanup(func() {
				if err := mock.ExpectationsWereMet(); err != nil {
					t.Errorf("SQL expectations: %v", err)
				}
			})
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `tasks` SET").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectCommit()
			rows := sqlmock.NewRows([]string{"id", "current_execution_id", "result", "terminal_model_usage", "cost_status"})
			if tc.row != nil {
				rows.AddRow(tc.row...)
			}
			mock.ExpectQuery("SELECT .* FROM `tasks` WHERE id = \\?").WithArgs(taskID, 1).WillReturnRows(rows)

			repo := newTaskRepository(db)
			var matched bool
			var err error
			if tc.fenced {
				matched, err = repo.UpdateExecutionEvidenceForExecution(context.Background(), taskID, executionID, resultJSON, usage, "reconciled")
			} else {
				matched, err = repo.UpdateExecutionEvidence(context.Background(), taskID, resultJSON, usage, "reconciled")
			}
			if tc.wantConflict {
				if !errors.Is(err, ErrTaskExecutionEvidenceConflict) || !strings.Contains(strings.ToLower(err.Error()), "conflict") {
					t.Fatalf("error = %v, want explicit evidence conflict", err)
				}
				return
			}
			if err != nil || matched != tc.wantMatched {
				t.Fatalf("matched/error = %v/%v, want %v/nil", matched, err, tc.wantMatched)
			}
		})
	}
}

func TestJSONValuesEqualRejectsTrailingValue(t *testing.T) {
	if jsonValuesEqual(`{"success":true}`, `{"success":true} {"extra":true}`) {
		t.Fatal("JSON comparison accepted a trailing value")
	}
}
