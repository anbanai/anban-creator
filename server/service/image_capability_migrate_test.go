package service

import (
	"context"
	"testing"

	"github.com/rs/zerolog"
)

func TestMigrateImageCapabilitiesRenamesColumnsMapsValuesAndDropsBYOK(t *testing.T) {
	db := newMigrateTestDB(t)
	for _, statement := range []string{
		`CREATE TABLE tasks (id TEXT PRIMARY KEY, image_model_key TEXT, project_snapshot JSON)`,
		`CREATE TABLE plans (id TEXT PRIMARY KEY, image_model_key TEXT)`,
		`CREATE TABLE projects (id TEXT PRIMARY KEY, ecommerce_defaults JSON)`,
		`CREATE TABLE templates (id TEXT PRIMARY KEY, ecommerce JSON)`,
		`CREATE TABLE user_model_configs (id TEXT PRIMARY KEY, image_config_json TEXT)`,
		`INSERT INTO tasks VALUES
			('standard', 'standard_image', '{"ecommerce_defaults":{"image_model_key":"standard_image"}}'),
			('professional', 'professional_enhance', '{}'),
			('custom', 'custom', '{}'),
			('unknown', 'retired-route', '{}'),
			('empty', '', '{}')`,
		`INSERT INTO plans VALUES ('professional', 'gpt-image-2'), ('unknown', 'missing')`,
		`INSERT INTO projects VALUES ('p1', '{"image_model_key":"openai-gpt-image","keep":"value"}')`,
		`INSERT INTO templates VALUES ('t1', '{"image_model_key":"volcengine-standard"}')`,
		`INSERT INTO user_model_configs VALUES ('u1', '{"api_key":"secret"}')`,
	} {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("exec %q: %v", statement, err)
		}
	}

	logger := zerolog.Nop()
	stats, err := MigrateImageCapabilities(context.Background(), db, "standard", &logger)
	if err != nil {
		t.Fatalf("MigrateImageCapabilities: %v", err)
	}
	if stats.UnknownValues != 2 {
		t.Fatalf("unknown values = %d, want 2", stats.UnknownValues)
	}
	if db.Migrator().HasColumn("tasks", "image_model_key") || !db.Migrator().HasColumn("tasks", "image_capability_key") {
		t.Fatal("tasks image_model_key was not hard-renamed")
	}
	if db.Migrator().HasColumn("plans", "image_model_key") || !db.Migrator().HasColumn("plans", "image_capability_key") {
		t.Fatal("plans image_model_key was not hard-renamed")
	}
	if db.Migrator().HasTable("user_model_configs") {
		t.Fatal("user_model_configs table still exists")
	}

	assertCapability := func(table, id, want string) {
		t.Helper()
		var got string
		if err := db.Table(table).Select("image_capability_key").Where("id = ?", id).Scan(&got).Error; err != nil {
			t.Fatalf("read %s/%s: %v", table, id, err)
		}
		if got != want {
			t.Fatalf("%s/%s capability = %q, want %q", table, id, got, want)
		}
	}
	assertCapability("tasks", "standard", "standard")
	assertCapability("tasks", "professional", "professional")
	assertCapability("tasks", "custom", "professional")
	assertCapability("tasks", "unknown", "standard")
	assertCapability("tasks", "empty", "standard")
	assertCapability("plans", "professional", "professional")
	assertCapability("plans", "unknown", "standard")

	for table, column := range map[string]string{
		"tasks": "project_snapshot", "projects": "ecommerce_defaults", "templates": "ecommerce",
	} {
		var payload string
		if err := db.Table(table).Select(column).Limit(1).Scan(&payload).Error; err != nil {
			t.Fatalf("read %s.%s: %v", table, column, err)
		}
		if containsJSONKey(payload, "image_model_key") || !containsJSONKey(payload, "image_capability_key") {
			t.Fatalf("%s.%s was not migrated: %s", table, column, payload)
		}
	}

	second, err := MigrateImageCapabilities(context.Background(), db, "standard", &logger)
	if err != nil {
		t.Fatalf("second migration: %v", err)
	}
	if second.UnknownValues != 0 {
		t.Fatalf("second migration unknown values = %d, want 0", second.UnknownValues)
	}
}

func TestMigrateImageCapabilitiesPreservesExplicitRatio(t *testing.T) {
	db := newMigrateTestDB(t)
	if err := db.Exec(`CREATE TABLE tasks (id TEXT PRIMARY KEY, image_model_key TEXT, image_ratio TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO tasks VALUES ('professional-wide', 'professional_enhance', '16:9')`).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := MigrateImageCapabilities(context.Background(), db, "standard", nil); err != nil {
		t.Fatalf("MigrateImageCapabilities: %v", err)
	}
	var migrated struct {
		ImageCapabilityKey string
		ImageRatio         string
	}
	if readErr := db.Table("tasks").Select("image_capability_key, image_ratio").Where("id = ?", "professional-wide").Scan(&migrated).Error; readErr != nil {
		t.Fatal(readErr)
	}
	if migrated.ImageCapabilityKey != "professional" || migrated.ImageRatio != "16:9" {
		t.Fatalf("migrated task = %#v, want professional with original explicit ratio", migrated)
	}
}
