package migrations

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestWechatPublicationContractMigrationIsACleanCutover(t *testing.T) {
	raw, err := os.ReadFile("20260831_wechat_publication_lifecycle_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"DROP COLUMN `published`", "DROP COLUMN `published_at`", "DROP COLUMN `publish_approval_state`", "DROP COLUMN `pending_draft_articles`",
		"DROP INDEX `idx_task_executions_publishing_status`", "CHANGE COLUMN `publishing_status` `draft_delivery_status`", "CHANGE COLUMN `publishing_result` `draft_delivery_result`", "ADD INDEX `idx_task_executions_draft_delivery_status` (`draft_delivery_status`)",
		"CREATE TABLE `wechat_publications`", "UNIQUE KEY `idx_wechat_publications_task_id` (`task_id`)",
		"CONSTRAINT `chk_wechat_publication_source` CHECK (`source` IN ('anban_api','wechat_console'))",
		"CONSTRAINT `chk_wechat_publication_status` CHECK (`status` IN ('drafting','drafted','publish_submitting','publishing','published','needs_selection','publish_failed','unsupported'))",
		"`draft_author`", "`msg_id`", "`article_url`", "`article_index` int NOT NULL DEFAULT 1", "`wechat_status_code`", "`draft_created_at`", "`published_at`",
	} {
		if !strings.Contains(sql, required) {
			t.Errorf("migration missing %q", required)
		}
	}
	for _, forbidden := range []string{"UPDATE `tasks`", "INSERT INTO `wechat_publications`", "enable_publishing", "require_publish_approval"} {
		if strings.Contains(strings.ToLower(sql), strings.ToLower(forbidden)) {
			t.Errorf("migration contains obsolete compatibility path %q", forbidden)
		}
	}
}

func TestWechatPublicationMigrationMatchesModelNullabilityAndDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.WechatPublication{}); err != nil {
		t.Fatal(err)
	}
	type column struct {
		notNull      bool
		defaultValue string
	}
	columns := map[string]column{}
	rows, err := db.Raw("PRAGMA table_info(wechat_publications)").Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, typ string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = column{notNull: notNull == 1, defaultValue: defaultValue.String}
	}
	for _, tt := range []struct {
		name, defaultValue, ddl string
		notNull                 bool
	}{
		{"draft_media_id", "''", "`draft_media_id` varchar(191) NOT NULL DEFAULT ''", true},
		{"draft_title", "''", "`draft_title` varchar(500) NOT NULL DEFAULT ''", true},
		{"draft_author", "''", "`draft_author` varchar(500) NOT NULL DEFAULT ''", true},
		{"draft_digest", "", "`draft_digest` text NOT NULL", true},
		{"draft_thumb_media_id", "''", "`draft_thumb_media_id` varchar(191) NOT NULL DEFAULT ''", true},
		{"draft_content_fingerprint", "''", "`draft_content_fingerprint` char(64) NOT NULL DEFAULT ''", true},
		{"source", "", "`source` varchar(32) NOT NULL", true},
		{"status", "", "`status` varchar(32) NOT NULL", true},
		{"publish_id", "''", "`publish_id` varchar(191) NOT NULL DEFAULT ''", true},
		{"msg_data_id", "''", "`msg_data_id` varchar(191) NOT NULL DEFAULT ''", true},
		{"msg_id", "''", "`msg_id` varchar(191) NOT NULL DEFAULT ''", true},
		{"article_id", "''", "`article_id` varchar(191) NOT NULL DEFAULT ''", true},
		{"article_url", "''", "`article_url` varchar(1000) NOT NULL DEFAULT ''", true},
		{"article_index", "1", "`article_index` int NOT NULL DEFAULT 1", true},
		{"wechat_status_code", "0", "`wechat_status_code` int NOT NULL DEFAULT 0", true},
		{"last_error", "", "`last_error` text NOT NULL", true},
		{"claim_token", "''", "`claim_token` char(36) NOT NULL DEFAULT ''", true},
	} {
		got, ok := columns[tt.name]
		if !ok || got.notNull != tt.notNull || normalizeDefault(got.defaultValue) != normalizeDefault(tt.defaultValue) {
			t.Errorf("model column %s = %#v, want notNull=%v default=%q", tt.name, got, tt.notNull, tt.defaultValue)
		}
		if raw, readErr := os.ReadFile("20260831_wechat_publication_lifecycle_contract.sql"); readErr != nil || !strings.Contains(string(raw), tt.ddl) {
			t.Errorf("migration missing model-equivalent declaration %q", tt.ddl)
		}
	}
}

func normalizeDefault(value string) string {
	return strings.Trim(value, "'\"")
}
