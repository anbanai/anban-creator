package service

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// MigrateWechatIdentity performs the one-time logical rename from the legacy
// article identity. It is deliberately idempotent so startup can safely run
// it on fresh databases and during rolling deployments.
func MigrateWechatIdentity(ctx context.Context, db *gorm.DB, log *zerolog.Logger) error {
	if db == nil {
		return nil
	}
	updates := []struct{ table, column, from, to string }{
		{"projects", "platform", "article", "wechat"},
		{"tasks", "type", "article", "wechat-article"},
		{"plans", "type", "article", "wechat-article"},
		{"strategy_snapshots", "applicable_tasks", "article", "wechat-article"},
		{"billing_skus", "operation", "task.article", "task.wechat_article"},
		{"billing_skus", "sku_id", "task.article.", "task.wechat_article."},
		{"billing_quotes", "sku_id", "task.article.", "task.wechat_article."},
		{"billing_charges", "operation", "task.article", "task.wechat_article"},
		{"task_executions", "runtime_profile", "article", "wechat"},
	}
	for _, update := range updates {
		if !db.Migrator().HasTable(update.table) || !db.Migrator().HasColumn(update.table, update.column) {
			continue
		}
		query := fmt.Sprintf("UPDATE %s SET %s = REPLACE(%s, ?, ?) WHERE %s LIKE ?", update.table, update.column, update.column, update.column)
		if err := db.WithContext(ctx).Exec(query, update.from, update.to, "%"+update.from+"%").Error; err != nil {
			return fmt.Errorf("migrate %s.%s identity: %w", update.table, update.column, err)
		}
	}
	if log != nil {
		log.Info().Msg("migrated legacy article identities to wechat identities")
	}
	return nil
}
