package migrations

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

const WechatPublicationStatusMigration = "20261004_wechat_publication_status_constraint.sql"

var publicationInClause = regexp.MustCompile("(?i)^statusin\\((.*)\\)$")
var publicationStatusLiteral = regexp.MustCompile(`^(?:_[a-zA-Z0-9]+)?'([a-z_]+)'$`)

// RequireWechatPublicationStatusConstraint validates the actual MySQL check,
// including enforcement, without relying on its name or the Go model tag.
func RequireWechatPublicationStatusConstraint(ctx context.Context, db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "mysql" {
		return nil
	}
	var clause, enforced string
	err := db.WithContext(ctx).Raw(`SELECT cc.CHECK_CLAUSE, tc.ENFORCED
 FROM information_schema.CHECK_CONSTRAINTS cc
 JOIN information_schema.TABLE_CONSTRAINTS tc
 ON tc.CONSTRAINT_SCHEMA = cc.CONSTRAINT_SCHEMA AND tc.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
 WHERE cc.CONSTRAINT_SCHEMA = DATABASE() AND tc.TABLE_NAME = ? AND cc.CONSTRAINT_NAME = ?`, "wechat_publications", "chk_wechat_publication_status").Row().Scan(&clause, &enforced)
	if err != nil {
		return fmt.Errorf("apply server/migrations/%s: publication status constraint unavailable: %w", WechatPublicationStatusMigration, err)
	}
	if enforced != "YES" || !currentPublicationStatusClause(clause) {
		return fmt.Errorf("apply server/migrations/%s: publication status constraint is stale, unexpected, or not enforced", WechatPublicationStatusMigration)
	}
	return nil
}

func currentPublicationStatusClause(clause string) bool {
	clause = strings.ReplaceAll(clause, `\'`, "'")
	clause = strings.ReplaceAll(clause, "`", "")
	clause = strings.Join(strings.Fields(clause), "")
	// MySQL wraps the entire expression in one or more pairs of parentheses.
	for strings.HasPrefix(clause, "(") && strings.HasSuffix(clause, ")") {
		clause = clause[1 : len(clause)-1]
	}
	match := publicationInClause.FindStringSubmatch(clause)
	if len(match) != 2 {
		return false
	}
	var actual []string
	for _, part := range strings.Split(match[1], ",") {
		m := publicationStatusLiteral.FindStringSubmatch(part)
		if len(m) != 2 {
			return false
		}
		actual = append(actual, m[1])
	}
	expected := []string{model.WechatPublicationStatusDrafting, model.WechatPublicationStatusDrafted, model.WechatPublicationStatusAwaitingManual, model.WechatPublicationStatusAmbiguous, model.WechatPublicationStatusPublishing, model.WechatPublicationStatusPublished, model.WechatPublicationStatusNeedsSelection, model.WechatPublicationStatusPublishFailed, model.WechatPublicationStatusUnsupported}
	sort.Strings(actual)
	sort.Strings(expected)
	return strings.Join(actual, ",") == strings.Join(expected, ",")
}
