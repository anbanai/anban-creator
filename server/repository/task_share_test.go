package repository

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestTaskShareRepositoryDoesNotLogOrSerializeToken(t *testing.T) {
	var logs bytes.Buffer
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{Logger: logger.New(log.New(&logs, "", 0), logger.Config{LogLevel: logger.Info})})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.TaskShare{}); err != nil {
		t.Fatal(err)
	}
	logs.Reset()
	repo := New(db)
	share := &model.TaskShare{TaskID: "task", UserID: "owner", Token: strings.Repeat("private-token", 4), ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.TaskShares().Save(t.Context(), share); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.TaskShares().FindByToken(t.Context(), share.Token); err != nil {
		t.Fatal(err)
	}
	duplicate := *share
	duplicate.TaskID = "another-task"
	if err := repo.TaskShares().Save(t.Context(), &duplicate); err == nil {
		t.Fatal("duplicate token accepted")
	}
	if strings.Contains(logs.String(), share.Token) {
		t.Fatalf("token leaked in SQL logs: %s", logs.String())
	}
	data, err := json.Marshal(share)
	if err != nil || string(data) != "{}" {
		t.Fatalf("model JSON = %s, %v", data, err)
	}
}
