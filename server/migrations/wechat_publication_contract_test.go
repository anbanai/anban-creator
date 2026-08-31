package migrations

import (
	"os"
	"strings"
	"testing"
)

func TestWechatPublicationContractMigrationIsACleanCutover(t *testing.T) {
	raw, err := os.ReadFile("20260831_wechat_publication_lifecycle_contract.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, required := range []string{
		"DROP COLUMN `published`", "DROP COLUMN `published_at`", "DROP COLUMN `publish_approval_state`", "DROP COLUMN `pending_draft_articles`",
		"CHANGE COLUMN `publishing_status` `draft_delivery_status`", "CHANGE COLUMN `publishing_result` `draft_delivery_result`",
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
