package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

var wechatArticleNumber = regexp.MustCompile(`^[0-9]+$`)
var wechatArticleToken = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// The official article URL is an identity, never the (possibly truncated) title.
// Signature, tracking parameters, query order and protocol do not change it.
func wechatRecoveryArticleKey(raw string) string {
	u, err := url.Parse(html.UnescapeString(strings.TrimSpace(raw)))
	if err != nil || u.User != nil || u.Port() != "" || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Hostname(), "mp.weixin.qq.com") {
		return ""
	}
	if u.Path == "/s" {
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["__biz"]) != 1 || len(q["mid"]) != 1 || len(q["idx"]) != 1 || q.Get("__biz") == "" || !wechatArticleNumber.MatchString(q.Get("mid")) || !wechatArticleNumber.MatchString(q.Get("idx")) {
			return ""
		}
		return "mp.weixin.qq.com/s?" + url.Values{"__biz": {q.Get("__biz")}, "mid": {q.Get("mid")}, "idx": {q.Get("idx")}}.Encode()
	}
	if token := strings.TrimPrefix(u.Path, "/s/"); token != u.Path && wechatArticleToken.MatchString(token) {
		return "mp.weixin.qq.com/s/" + token
	}
	return ""
}

func wechatRecoveryContent(project, key string) model.AnalyticsContent {
	sum := sha256.Sum256([]byte(project + "\x00" + key))
	return model.AnalyticsContent{ID: "wechat_article:" + hex.EncodeToString(sum[:]), ProjectID: project, Platform: model.PlatformArticle, ContentType: "article", Status: "recorded"}
}

// Only existing, explicit row bindings and exact URLs may attach a recovered
// row to a task. A title match, even a unique one, is insufficient evidence.
func (m *AnalyticsRebuildManager) wechatRecoveryIdentities(ctx context.Context, project string) (map[string]model.AnalyticsContent, map[string]bool, error) {
	db := m.repo.Analytics().DB().WithContext(ctx)
	byURL, conflicts := map[string]model.AnalyticsContent{}, map[string]bool{}
	add := func(raw string, c model.AnalyticsContent) {
		key := wechatRecoveryArticleKey(raw)
		if key == "" || c.ID == "" {
			return
		}
		if old, ok := byURL[key]; ok && old.ID != c.ID {
			conflicts[key] = true
			return
		}
		byURL[key] = c
	}
	var contents []model.AnalyticsContent
	if err := db.Where("project_id = ? AND platform = ?", project, model.PlatformArticle).Find(&contents).Error; err != nil {
		return nil, nil, err
	}
	byID := map[string]model.AnalyticsContent{}
	for _, c := range contents {
		byID[c.ID] = c
		add(c.URL, c)
	}
	type binding struct {
		model.WechatAnalyticsImportRow
		SnapshotID            string
		SnapshotTaskID        string
		SnapshotPublicationID string
		LiveContentID         string
	}
	var rows []binding
	q := db.Table("wechat_analytics_import_rows r").Select("r.*, s.id AS snapshot_id, s.task_id AS snapshot_task_id, s.publication_id AS snapshot_publication_id, COALESCE(o.content_id, legacy.content_id) AS live_content_id").
		Joins("LEFT JOIN wechat_analytics_snapshots s ON s.import_row_id = r.id AND s.project_id = r.project_id").
		Joins("LEFT JOIN analytics_observations o ON o.id = "+analyticsPrefixedIDSQL(db.Dialector.Name(), "wechat_import:", "r.id")+" AND o.project_id = r.project_id").
		Joins("LEFT JOIN analytics_observations legacy ON legacy.id = "+analyticsPrefixedIDSQL(db.Dialector.Name(), "legacy:wechat_import:", "s.id")+" AND legacy.project_id = r.project_id").
		Where("r.project_id = ? AND (s.id IS NOT NULL OR o.id IS NOT NULL)", project)
	err := q.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for _, r := range rows {
			id := r.LiveContentID
			if id == "" {
				if r.SnapshotTaskID != "" {
					id = "task:" + r.SnapshotTaskID
				} else if r.SnapshotPublicationID != "" {
					id = "wechat_publication:" + r.SnapshotPublicationID
				}
			}
			c, ok := byID[id]
			if !ok {
				c = model.AnalyticsContent{ID: id, ProjectID: project, Platform: model.PlatformArticle, TaskID: r.SnapshotTaskID, PublicationID: r.SnapshotPublicationID, ContentType: "article"}
			}
			add(r.ArticleURL, c)
		}
		return nil
	}).Error
	return byURL, conflicts, err
}

// Prefix and column are internal SQL constants, never request values.
func analyticsPrefixedIDSQL(dialect, prefix, column string) string {
	if dialect == "mysql" {
		return "CONCAT('" + prefix + "', " + column + ")"
	}
	return "('" + prefix + "' || " + column + ")"
}

// Old imports persisted unmatched rows without snapshots. Recover those rows
// from their original batches; re-uploading a file cannot recreate these dates.
// No operational task/publication is invented or relabelled in this process.
func (m *AnalyticsRebuildManager) recoverWechatImportRows(ctx context.Context, project string, report *AnalyticsMigrationReport, queue func(legacyMigrationInput) error) error {
	db := m.repo.Analytics().DB().WithContext(ctx)
	if !db.Migrator().HasTable("wechat_analytics_import_rows") || !db.Migrator().HasTable("wechat_analytics_import_batches") {
		return nil
	}
	identities, conflicts, err := m.wechatRecoveryIdentities(ctx, project)
	if err != nil {
		return err
	}
	type historicalRow struct {
		model.WechatAnalyticsImportRow
		BatchFound      string
		BatchStatus     string
		BatchDataAsOfAt time.Time
		BatchReceivedAt time.Time
		BatchRevokedAt  *time.Time
	}
	var rows []historicalRow
	q := db.Table("wechat_analytics_import_rows r").
		Select("r.*, b.id AS batch_found, b.status AS batch_status, b.data_as_of_at AS batch_data_as_of_at, b.received_at AS batch_received_at, b.revoked_at AS batch_revoked_at").
		Joins("LEFT JOIN wechat_analytics_import_batches b ON b.id = r.batch_id AND b.project_id = r.project_id").
		Where("r.project_id = ?", project).
		Where("NOT EXISTS (SELECT 1 FROM wechat_analytics_snapshots s WHERE s.import_row_id = r.id AND s.project_id = r.project_id)")
	return q.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for _, r := range rows {
			if r.BatchFound == "" || r.BatchDataAsOfAt.IsZero() || r.BatchReceivedAt.IsZero() {
				report.skip("wechat_import_missing_batch_date")
				continue
			}
			if r.BatchStatus != model.WechatAnalyticsImportBatchCompleted && r.BatchStatus != model.WechatAnalyticsImportBatchNeedsReview && !(r.BatchStatus == "revoked" && r.BatchRevokedAt != nil) {
				report.skip("wechat_import_incomplete_batch")
				continue
			}
			if r.ParseError != "" || r.MatchStatus == model.WechatAnalyticsImportRowInvalid {
				report.skip("wechat_import_invalid_row")
				continue
			}
			key := wechatRecoveryArticleKey(r.ArticleURL)
			if key == "" || conflicts[key] {
				report.IdentityConflicts++
				reason := "wechat_import_missing_article_identity"
				if conflicts[key] {
					reason = "wechat_import_conflicting_article_identity"
				}
				report.skip(reason)
				continue
			}
			c, ok := identities[key]
			if !ok {
				c = wechatRecoveryContent(project, key)
			}
			if c.Title == "" {
				c.Title = r.Title
			}
			if c.URL == "" {
				c.URL = r.ArticleURL
			}
			if c.Date == nil {
				c.Date = r.PublishedDate
			}
			metrics, err := wechatRecoveryMetrics(&r.WechatAnalyticsImportRow)
			if err != nil {
				report.skip("wechat_import_invalid_metrics")
				continue
			}
			o := model.AnalyticsObservation{ID: "wechat_import:" + r.ID, ProjectID: project, ContentID: c.ID, BatchID: r.BatchID, MetricBasis: "cumulative", StatDate: dateShanghai(r.BatchDataAsOfAt), Source: "wechat_import", SourcePriority: 100, EffectiveAt: r.BatchDataAsOfAt.UTC(), ReceivedAt: r.BatchReceivedAt.UTC(), RevokedAt: legacyRevoked(r.BatchRevokedAt), AnalyticsMetrics: metrics}
			if err := queue(legacyMigrationInput{content: c, obs: o, raw: r.RawData}); err != nil {
				return err
			}
			report.WechatRecoveredRows++
		}
		return nil
	}).Error
}

func wechatRecoveryMetrics(r *model.WechatAnalyticsImportRow) (model.AnalyticsMetrics, error) {
	m := model.AnalyticsMetrics{ReadUsers: r.ReadUsers, ShareUsers: r.ShareUsers, ReadToFollowUsers: r.ReadToFollowUsers, DeliveredUsers: r.DeliveredUsers}
	var raw map[string]string
	_ = json.Unmarshal([]byte(r.RawData), &raw)
	var err error
	if m.ReadCompletionRate, err = analyticsDecimal(raw["阅读完成率"], r.ReadCompletionRate, true); err != nil {
		return m, err
	}
	if m.DeliveryCompletionRate, err = analyticsDecimal(raw["送达完成率"], r.DeliveryCompletionRate, true); err != nil {
		return m, err
	}
	return m, m.Validate()
}
