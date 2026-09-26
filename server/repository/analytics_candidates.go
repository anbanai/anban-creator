package repository

import (
	"context"
	"github.com/anbanai/anban-creator/server/model"
	"strings"
)

// AnalyticsCandidateRow is a lightweight business identity, never a metric history.
type AnalyticsCandidateRow struct {
	Kind          string
	ID            string
	Title         string
	ContentType   string
	Status        string
	Date          string
	URL           string
	TaskID        string
	PublicationID string
	PostID        string
}

func (r *AnalyticsRepository) CandidatePage(ctx context.Context, userID, projectID, platform, search string, offset, limit int) ([]AnalyticsCandidateRow, int64, error) {
	var sql string
	if platform == model.PlatformArticle {
		sql = `SELECT 'task' AS kind,t.id,COALESCE(NULLIF(p.draft_title,''),NULLIF(t.title,''),t.topic) AS title,t.type AS content_type,t.status,COALESCE(p.published_at,t.created_at) AS date,COALESCE(p.article_url,'') AS url,t.id AS task_id,COALESCE(p.id,'') AS publication_id,'' AS post_id FROM tasks t LEFT JOIN wechat_publications p ON p.task_id=t.id AND p.project_id=t.project_id WHERE t.project_id=? AND t.user_id=?`
	} else {
		sql = `SELECT 'seednote_post' AS kind,p.id,p.title,COALESCE(NULLIF(p.genre,''),'unknown') AS content_type,'recorded' AS status,COALESCE(p.first_published_at,p.created_at) AS date,p.note_url AS url,p.task_id,'' AS publication_id,p.id AS post_id FROM seednote_posts p WHERE p.project_id=? AND p.user_id=? UNION ALL SELECT 'task' AS kind,t.id,COALESCE(NULLIF(t.title,''),t.topic) AS title,'unknown' AS content_type,t.status,t.created_at AS date,'' AS url,t.id AS task_id,'' AS publication_id,'' AS post_id FROM tasks t WHERE t.project_id=? AND t.user_id=? AND NOT EXISTS (SELECT 1 FROM seednote_posts p WHERE p.task_id=t.id AND p.project_id=t.project_id)`
	}
	args := []any{projectID, userID}
	if platform != model.PlatformArticle {
		args = append(args, projectID, userID)
	}
	query := r.db.WithContext(ctx).Table("("+sql+") AS candidates", args...)
	if search = strings.TrimSpace(search); search != "" {
		query = query.Where("LOWER(title) LIKE ?", "%"+strings.ToLower(search)+"%")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	var items []AnalyticsCandidateRow
	err := query.Order("date DESC, kind ASC, id ASC").Offset(offset).Limit(limit).Scan(&items).Error
	return items, total, err
}
