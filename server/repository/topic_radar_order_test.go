package repository

import (
	"strings"
	"testing"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// TestTopicSuggestionOrderQuotesReservedRank verifies the list query is valid
// on MySQL, where RANK is a reserved keyword and must be quoted.
func TestTopicSuggestionOrderQuotesReservedRank(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "user:pass@tcp(127.0.0.1:1)/anban?charset=utf8mb4",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	sql := db.ToSQL(func(tx *gorm.DB) *gorm.DB {
		var items []*model.TopicSuggestion
		return topicSuggestionListQuery(tx, "digest-1", &items)
	})
	if !strings.Contains(sql, "ORDER BY `rank`") {
		t.Fatalf("query = %q, want quoted reserved column", sql)
	}
}
