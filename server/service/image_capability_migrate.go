package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type ImageCapabilityMigrationStats struct {
	UnknownValues int64
}

// MigrateImageCapabilities performs the hard database cutover before AutoMigrate.
// It is intentionally idempotent so a process restart can resume after DDL that
// was already committed by MySQL.
func MigrateImageCapabilities(ctx context.Context, db *gorm.DB, defaultCapability string, logger *zerolog.Logger) (ImageCapabilityMigrationStats, error) {
	var stats ImageCapabilityMigrationStats
	if db == nil {
		return stats, nil
	}
	defaultCapability = strings.TrimSpace(defaultCapability)
	if defaultCapability == "" {
		return stats, fmt.Errorf("default image capability is required for migration")
	}

	for _, table := range []string{"tasks", "plans"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		hasOld := db.Migrator().HasColumn(table, "image_model_key")
		hasNew := db.Migrator().HasColumn(table, "image_capability_key")
		if hasOld && hasNew {
			return stats, fmt.Errorf("%s contains both image_model_key and image_capability_key", table)
		}
		if hasOld {
			if err := db.Migrator().RenameColumn(table, "image_model_key", "image_capability_key"); err != nil {
				return stats, fmt.Errorf("rename %s.image_model_key: %w", table, err)
			}
			hasNew = true
		}
		if hasNew {
			unknown, err := migrateCapabilityColumn(ctx, db, table, defaultCapability)
			if err != nil {
				return stats, err
			}
			stats.UnknownValues += unknown
		}
	}

	for _, target := range []struct{ table, column string }{
		{table: "tasks", column: "project_snapshot"},
		{table: "projects", column: "ecommerce_defaults"},
		{table: "templates", column: "ecommerce"},
	} {
		if !db.Migrator().HasTable(target.table) || !db.Migrator().HasColumn(target.table, target.column) {
			continue
		}
		unknown, err := migrateCapabilityJSON(ctx, db, target.table, target.column, defaultCapability)
		if err != nil {
			return stats, err
		}
		stats.UnknownValues += unknown
	}
	if err := migrateBusinessImageRatios(ctx, db); err != nil {
		return stats, err
	}

	if db.Migrator().HasTable("user_model_configs") {
		if err := db.Migrator().DropTable("user_model_configs"); err != nil {
			return stats, fmt.Errorf("drop user_model_configs: %w", err)
		}
	}
	if logger != nil {
		logger.Info().Int64("unknown_values", stats.UnknownValues).Msg("image capability database migration completed")
	}
	return stats, nil
}

func migrateBusinessImageRatios(ctx context.Context, db *gorm.DB) error {
	projectRatios := make(map[string]string)
	if db.Migrator().HasTable("projects") &&
		db.Migrator().HasColumn("projects", "platform") &&
		db.Migrator().HasColumn("projects", "image_ratio") {
		type projectRow struct {
			ID, Platform, ImageRatio string
		}
		var projects []projectRow
		if err := db.WithContext(ctx).Table("projects").Select("id, platform, image_ratio").Scan(&projects).Error; err != nil {
			return fmt.Errorf("read project image ratios: %w", err)
		}
		for _, project := range projects {
			ratio := normalizePersistedBusinessImageRatio(project.Platform, project.ImageRatio)
			if ratio != strings.TrimSpace(project.ImageRatio) {
				if err := db.WithContext(ctx).Table("projects").Where("id = ?", project.ID).Update("image_ratio", ratio).Error; err != nil {
					return fmt.Errorf("normalize project %s image ratio: %w", project.ID, err)
				}
			}
			projectRatios[project.ID] = ratio
		}
	}

	if db.Migrator().HasTable("tasks") &&
		db.Migrator().HasColumn("tasks", "type") &&
		db.Migrator().HasColumn("tasks", "image_ratio") {
		type taskRow struct {
			ID, ProjectID, Type, ImageRatio string
			ProjectSnapshot                 sql.NullString
		}
		selectColumns := "id, type, image_ratio"
		if db.Migrator().HasColumn("tasks", "project_id") {
			selectColumns += ", project_id"
		}
		if db.Migrator().HasColumn("tasks", "project_snapshot") {
			selectColumns += ", project_snapshot"
		}
		var tasks []taskRow
		if err := db.WithContext(ctx).Table("tasks").Select(selectColumns).Scan(&tasks).Error; err != nil {
			return fmt.Errorf("read task image ratios: %w", err)
		}
		for _, task := range tasks {
			var snapshot map[string]any
			var snapshotValid bool
			if task.ProjectSnapshot.Valid && strings.TrimSpace(task.ProjectSnapshot.String) != "" {
				if err := json.Unmarshal([]byte(task.ProjectSnapshot.String), &snapshot); err == nil && snapshot != nil {
					snapshotValid = true
				}
			}
			platform := strings.TrimSpace(stringMapValue(snapshot, "platform"))
			if platform == "" {
				platform = task.Type
			}
			ratio := strings.TrimSpace(task.ImageRatio)
			if ratio == "" {
				ratio = stringMapValue(snapshot, "image_ratio")
			}
			ratio = normalizePersistedBusinessImageRatio(platform, ratio)
			updates := make(map[string]any)
			if ratio != strings.TrimSpace(task.ImageRatio) {
				updates["image_ratio"] = ratio
			}
			if snapshotValid && ratio != "" && stringMapValue(snapshot, "image_ratio") != ratio {
				snapshot["image_ratio"] = ratio
				encoded, err := json.Marshal(snapshot)
				if err != nil {
					return fmt.Errorf("encode task %s project snapshot: %w", task.ID, err)
				}
				updates["project_snapshot"] = encoded
			}
			if len(updates) > 0 {
				if err := db.WithContext(ctx).Table("tasks").Where("id = ?", task.ID).Updates(updates).Error; err != nil {
					return fmt.Errorf("normalize task %s image ratio: %w", task.ID, err)
				}
			}
		}
	}

	if db.Migrator().HasTable("plans") &&
		db.Migrator().HasColumn("plans", "type") &&
		db.Migrator().HasColumn("plans", "image_ratio") {
		type planRow struct {
			ID, ProjectID, Type, ImageRatio string
		}
		selectColumns := "id, type, image_ratio"
		if db.Migrator().HasColumn("plans", "project_id") {
			selectColumns += ", project_id"
		}
		var plans []planRow
		if err := db.WithContext(ctx).Table("plans").Select(selectColumns).Scan(&plans).Error; err != nil {
			return fmt.Errorf("read plan image ratios: %w", err)
		}
		for _, plan := range plans {
			ratio := strings.TrimSpace(plan.ImageRatio)
			if ratio == "" {
				ratio = projectRatios[plan.ProjectID]
			}
			ratio = normalizePersistedBusinessImageRatio(plan.Type, ratio)
			if ratio != strings.TrimSpace(plan.ImageRatio) {
				if err := db.WithContext(ctx).Table("plans").Where("id = ?", plan.ID).Update("image_ratio", ratio).Error; err != nil {
					return fmt.Errorf("normalize plan %s image ratio: %w", plan.ID, err)
				}
			}
		}
	}
	return nil
}

func normalizePersistedBusinessImageRatio(platform, ratio string) string {
	ratio = strings.TrimSpace(ratio)
	if len(model.SupportedImageRatios(platform)) == 0 {
		return ratio
	}
	if ratio != "" && model.IsBusinessImageRatioAllowed(platform, ratio) {
		return ratio
	}
	return model.DefaultImageRatio(platform)
}

func stringMapValue(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	text, _ := value[key].(string)
	return strings.TrimSpace(text)
}

func migrateCapabilityColumn(ctx context.Context, db *gorm.DB, table, defaultCapability string) (int64, error) {
	rows, err := db.WithContext(ctx).Table(table).Select("id, image_capability_key").Rows()
	if err != nil {
		return 0, fmt.Errorf("read %s image capabilities: %w", table, err)
	}
	defer rows.Close()
	type update struct{ id, value string }
	var updates []update
	var unknown int64
	for rows.Next() {
		var id string
		var value sql.NullString
		if err := rows.Scan(&id, &value); err != nil {
			return 0, fmt.Errorf("scan %s image capability: %w", table, err)
		}
		mapped, known := migrateLegacyCapabilityKey(value.String, defaultCapability)
		if !known {
			unknown++
		}
		if !value.Valid || value.String != mapped {
			updates = append(updates, update{id: id, value: mapped})
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate %s image capabilities: %w", table, err)
	}
	for _, item := range updates {
		if err := db.WithContext(ctx).Table(table).Where("id = ?", item.id).Update("image_capability_key", item.value).Error; err != nil {
			return 0, fmt.Errorf("update %s image capability: %w", table, err)
		}
	}
	return unknown, nil
}

func migrateCapabilityJSON(ctx context.Context, db *gorm.DB, table, column, defaultCapability string) (int64, error) {
	rows, err := db.WithContext(ctx).Table(table).Select("id, " + column).Rows()
	if err != nil {
		return 0, fmt.Errorf("read %s.%s: %w", table, column, err)
	}
	defer rows.Close()
	type update struct {
		id      string
		payload []byte
	}
	var updates []update
	var unknown int64
	for rows.Next() {
		var id string
		var raw sql.NullString
		if err := rows.Scan(&id, &raw); err != nil {
			return 0, fmt.Errorf("scan %s.%s: %w", table, column, err)
		}
		if !raw.Valid || strings.TrimSpace(raw.String) == "" || strings.TrimSpace(raw.String) == "null" {
			continue
		}
		var value any
		if err := json.Unmarshal([]byte(raw.String), &value); err != nil {
			return 0, fmt.Errorf("decode %s.%s row %s: %w", table, column, id, err)
		}
		changed, count := migrateCapabilityJSONValue(value, defaultCapability)
		unknown += count
		if !changed {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return 0, fmt.Errorf("encode %s.%s row %s: %w", table, column, id, err)
		}
		updates = append(updates, update{id: id, payload: encoded})
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate %s.%s: %w", table, column, err)
	}
	for _, item := range updates {
		if err := db.WithContext(ctx).Table(table).Where("id = ?", item.id).Update(column, item.payload).Error; err != nil {
			return 0, fmt.Errorf("update %s.%s row %s: %w", table, column, item.id, err)
		}
	}
	return unknown, nil
}

func migrateCapabilityJSONValue(value any, defaultCapability string) (bool, int64) {
	changed := false
	var unknown int64
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if strings.EqualFold(key, "provider_strategy_override") {
				delete(typed, key)
				changed = true
				continue
			}
			if key == "image_model_key" {
				raw, _ := child.(string)
				mapped, known := migrateLegacyCapabilityKey(raw, defaultCapability)
				delete(typed, key)
				typed["image_capability_key"] = mapped
				changed = true
				if !known {
					unknown++
				}
				continue
			}
			childChanged, childUnknown := migrateCapabilityJSONValue(child, defaultCapability)
			changed = changed || childChanged
			unknown += childUnknown
		}
	case []any:
		for _, child := range typed {
			childChanged, childUnknown := migrateCapabilityJSONValue(child, defaultCapability)
			changed = changed || childChanged
			unknown += childUnknown
		}
	}
	return changed, unknown
}

func migrateLegacyCapabilityKey(value, defaultCapability string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "standard", "standard_image", "volcengine-standard", "openai-standard":
		return "standard", true
	case "professional", "professional_enhance", "custom", "gpt-image-2", "openai-gpt-image", "gemini-pro":
		return "professional", true
	default:
		return defaultCapability, false
	}
}

func containsJSONKey(payload, key string) bool {
	var value any
	if json.Unmarshal([]byte(payload), &value) != nil {
		return false
	}
	return jsonValueContainsKey(value, key)
}

func jsonValueContainsKey(value any, key string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for candidate, child := range typed {
			if candidate == key || jsonValueContainsKey(child, key) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if jsonValueContainsKey(child, key) {
				return true
			}
		}
	}
	return false
}
