package repository

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestIsDuplicateKeyErrorRecognizesDatabaseDrivers(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "mysql duplicate", err: &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry"}, want: true},
		{name: "wrapped mysql duplicate", err: fmt.Errorf("create user: %w", &mysqlDriver.MySQLError{Number: 1062, Message: "Duplicate entry"}), want: true},
		{name: "gorm duplicate", err: gorm.ErrDuplicatedKey, want: true},
		{name: "mysql other", err: &mysqlDriver.MySQLError{Number: 1452, Message: "foreign key constraint fails"}, want: false},
		{name: "unrelated", err: errors.New("database unavailable"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDuplicateKeyError(tt.err); got != tt.want {
				t.Fatalf("IsDuplicateKeyError(%T) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestIsDuplicateKeyErrorRecognizesSQLiteUniqueConstraint(t *testing.T) {
	repo := New(setupTestDB(t))
	ctx := context.Background()
	first := &model.User{ID: "duplicate-user-1", Email: "duplicate@example.com", Password: "x", InviteCode: "DUPKEY01"}
	second := &model.User{ID: "duplicate-user-2", Email: first.Email, Password: "x", InviteCode: "DUPKEY02"}
	if err := repo.Users().Create(ctx, first); err != nil {
		t.Fatalf("create first user: %v", err)
	}
	err := repo.Users().Create(ctx, second)
	if err == nil {
		t.Fatal("create duplicate user error = nil")
	}
	if !IsDuplicateKeyError(err) {
		t.Fatalf("IsDuplicateKeyError(%T: %v) = false, want true", err, err)
	}
}
