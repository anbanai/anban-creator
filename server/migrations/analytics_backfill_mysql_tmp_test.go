package migrations

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func TestTmpAnalyticsBackfillMySQL(t *testing.T) {
	dsn := os.Getenv("ANALYTICS_BACKFILL_TEST_DSN")
	if dsn == "" {
		t.Skip("ANALYTICS_BACKFILL_TEST_DSN is unset")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := model.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	project := uuid.NewString()
	task := uuid.NewString()
	publication := uuid.NewString()
	wechatTrack := uuid.NewString()
	seedTrack := uuid.NewString()
	post := uuid.NewString()
	user := uuid.NewString()
	wechatSnapshot := uuid.NewString()
	seedSnapshot := uuid.NewString()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	queries := []string{
		"INSERT INTO tasks (id,user_id,project_id,type,status,topic,title,execution_profile,agent_profile_snapshot,agent_profile_fingerprint,created_at,updated_at) VALUES ('" + task + "','" + user + "','" + project + "','article','completed','topic','title','quality','{}','',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))",
		"INSERT INTO wechat_publications (id,task_id,user_id,project_id,draft_digest,source,status,last_error,created_at,updated_at) VALUES ('" + publication + "','" + task + "','" + user + "','" + project + "','','wechat_console','published','',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))",
		"INSERT INTO wechat_article_trackings (id,task_id,user_id,project_id,publication_id,source,status,published_at,expires_at,last_error,created_at,updated_at) VALUES ('" + wechatTrack + "','" + task + "','" + user + "','" + project + "','" + publication + "','wechat_console','tracking',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3),'',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))",
		"INSERT INTO wechat_metric_snapshots (id,tracking_id,task_id,stat_date,captured_at,read_users,share_users,collection_users,like_users,zaikan_users,comment_count,read_finish_rate,average_read_active_time,read_to_subscribe_users,raw_response,created_at,updated_at) VALUES ('" + wechatSnapshot + "','" + wechatTrack + "','" + task + "','2026-09-28','" + now.Format("2006-01-02 15:04:05") + "',42,2,3,4,5,6,0.25,3.5,7,'{}',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))",
		"INSERT INTO seednote_posts (id,user_id,project_id,title,normalized_title,note_id,note_url,task_id,created_at,updated_at) VALUES ('" + post + "','" + user + "','" + project + "','note','note','note-1','https://note/1','" + task + "',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))",
		"INSERT INTO seednote_post_trackings (id,task_id,user_id,project_id,status,note_id,note_url,created_at,updated_at) VALUES ('" + seedTrack + "','" + task + "','" + user + "','" + project + "','tracking','note-1','https://note/1',UTC_TIMESTAMP(3),UTC_TIMESTAMP(3))",
		"INSERT INTO seednote_metric_snapshots (id,tracking_id,task_id,captured_at,captured_date,like_count,collect_count,comment_count,share_count,view_count,raw_data,created_at) VALUES ('" + seedSnapshot + "','" + seedTrack + "','" + task + "','" + now.Format("2006-01-02 15:04:05") + "','2026-09-28',8,9,2,1,100,'{}',UTC_TIMESTAMP(3))",
	}
	for _, q := range queries {
		if err := db.Exec(q).Error; err != nil {
			t.Fatalf("seed: %v\n%s", err, q)
		}
	}
	raw, err := os.ReadFile("20260927_analytics_legacy_sql_backfill.sql")
	if err != nil {
		t.Fatal(err)
	}
	var executable strings.Builder
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		executable.WriteString(line)
		executable.WriteByte('\n')
	}
	runBackfill := func() {
		t.Helper()
		for _, statement := range strings.Split(executable.String(), ";") {
			statement = strings.TrimSpace(statement)
			if statement == "" {
				continue
			}
			if err := db.Exec(statement).Error; err != nil {
				t.Fatalf("backfill: %v\n%s", err, statement)
			}
		}
	}
	runBackfill()
	var observations, official int64
	db.Table("analytics_observations").Where("project_id = ?", project).Count(&observations)
	db.Table("analytics_observations").Where("project_id = ? AND source IN ('wechat_api','seednote_public')", project).Count(&official)
	if observations != 2 || official != 2 {
		t.Fatalf("observations=%d official=%d, want 2 each", observations, official)
	}
	runBackfill()
	if err := db.Table("analytics_observations").Where("project_id = ?", project).Count(&observations).Error; err != nil {
		t.Fatal(err)
	}
	if observations != 2 {
		t.Fatalf("observations after rerun=%d, want 2", observations)
	}
	for _, table := range []string{"analytics_observation_payloads", "analytics_buckets", "analytics_contents"} {
		var count int64
		db.Table(table).Count(&count)
		if count == 0 {
			t.Fatalf("%s was not populated", table)
		}
	}
}
