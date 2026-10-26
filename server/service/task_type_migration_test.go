package service

import (
	"context"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
)

func TestMigratePersistedTaskTypesCanonicalizesTypeWithoutChangingTaskKind(t *testing.T) {
	db := setupTaskTestDB(t)
	for _, pair := range []struct{ old, canonical string }{
		{model.TaskKindViralAnalysis, model.TaskTypeViralAnalysis},
		{model.TaskKindProfileAnalysis, model.TaskTypeProfileAnalysis},
		{model.TaskKindContentResearch, model.TaskTypeContentResearch},
		{model.TaskKindContentValidation, model.TaskTypeContentValidation},
		{model.TaskKindContentScript, model.TaskTypeContentScript},
		{model.TaskKindFeedbackAnalysis, model.TaskTypeFeedbackAnalysis},
	} {
		task := model.Task{ID: pair.old, Type: pair.old, TaskKind: pair.old}
		if err := db.Create(&task).Error; err != nil {
			t.Fatalf("create %q task: %v", pair.old, err)
		}
	}
	if err := MigratePersistedTaskTypes(context.Background(), db); err != nil {
		t.Fatalf("MigratePersistedTaskTypes(): %v", err)
	}
	for _, pair := range []struct{ old, canonical string }{
		{model.TaskKindViralAnalysis, model.TaskTypeViralAnalysis},
		{model.TaskKindProfileAnalysis, model.TaskTypeProfileAnalysis},
		{model.TaskKindContentResearch, model.TaskTypeContentResearch},
		{model.TaskKindContentValidation, model.TaskTypeContentValidation},
		{model.TaskKindContentScript, model.TaskTypeContentScript},
		{model.TaskKindFeedbackAnalysis, model.TaskTypeFeedbackAnalysis},
	} {
		var got model.Task
		if err := db.First(&got, "id = ?", pair.old).Error; err != nil {
			t.Fatal(err)
		}
		if got.Type != pair.canonical || got.TaskKind != pair.old {
			t.Errorf("migrated task = type %q kind %q; want type %q kind %q", got.Type, got.TaskKind, pair.canonical, pair.old)
		}
	}
	if err := MigratePersistedTaskTypes(context.Background(), db); err != nil {
		t.Fatalf("second MigratePersistedTaskTypes(): %v", err)
	}
}
