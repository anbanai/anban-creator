package service

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/anbanai/anban-creator/server/model"
)

func TestMigrateTaskLifecycleDropsLegacyPercentageColumnsIdempotently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskExecution{}); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE tasks ADD COLUMN progress integer",
		"ALTER TABLE tasks ADD COLUMN progress_sequence integer",
		"ALTER TABLE tasks ADD COLUMN latest_progress text",
		"ALTER TABLE task_executions ADD COLUMN agent_pack_progress_contract text",
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"progress", "progress_sequence", "latest_progress"} {
		if !db.Migrator().HasColumn("tasks", column) {
			t.Fatalf("legacy setup missing tasks.%s", column)
		}
	}
	if !db.Migrator().HasColumn("task_executions", "agent_pack_progress_contract") {
		t.Fatal("legacy setup missing task_executions.agent_pack_progress_contract")
	}
	logger := zerolog.Nop()
	for range 2 {
		if err := MigrateTaskLifecycle(context.Background(), db, &logger); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"progress", "progress_sequence", "latest_progress"} {
		if db.Migrator().HasColumn("tasks", column) {
			t.Fatalf("legacy tasks.%s still exists", column)
		}
	}
	if db.Migrator().HasColumn("task_executions", "agent_pack_progress_contract") {
		t.Fatal("legacy task_executions.agent_pack_progress_contract still exists")
	}
	if !db.Migrator().HasColumn("tasks", "lifecycle") {
		t.Fatal("tasks.lifecycle missing after migration")
	}
}
