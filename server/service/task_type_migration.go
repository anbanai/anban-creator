package service

import (
	"context"
	"fmt"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// MigratePersistedTaskTypes canonicalizes historical public Task.Type values.
// TaskKind and execution identities deliberately remain unchanged.
func MigratePersistedTaskTypes(ctx context.Context, db *gorm.DB) error {
	if db == nil || !db.Migrator().HasTable("tasks") || !db.Migrator().HasColumn("tasks", "type") {
		return nil
	}
	for _, pair := range []struct{ old, canonical string }{
		{model.TaskKindViralAnalysis, model.TaskTypeViralAnalysis},
		{model.TaskKindProfileAnalysis, model.TaskTypeProfileAnalysis},
		{model.TaskKindContentResearch, model.TaskTypeContentResearch},
		{model.TaskKindContentValidation, model.TaskTypeContentValidation},
		{model.TaskKindContentScript, model.TaskTypeContentScript},
		{model.TaskKindFeedbackAnalysis, model.TaskTypeFeedbackAnalysis},
	} {
		if err := db.WithContext(ctx).Table("tasks").Where("type = ?", pair.old).Update("type", pair.canonical).Error; err != nil {
			return fmt.Errorf("migrate task type %q to %q: %w", pair.old, pair.canonical, err)
		}
	}
	return nil
}
