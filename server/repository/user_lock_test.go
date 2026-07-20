package repository

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserRepositoryLockByIDUsesForUpdate(t *testing.T) {
	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	var logs bytes.Buffer
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}), &gorm.Config{
		DryRun: true,
		Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info}),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, _ = newUserRepository(db).LockByID(context.Background(), "user-1")
	sql := logs.String()
	if !strings.Contains(sql, "FROM `users`") || !strings.Contains(sql, "FOR UPDATE") {
		t.Fatalf("user lock SQL contract invalid:\n%s", sql)
	}
}
