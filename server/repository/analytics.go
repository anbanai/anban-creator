package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrAnalyticsRebuilding = errors.New("analytics rebuilding")
var ErrAnalyticsIdempotencyConflict = errors.New("analytics idempotency key reused with changed request")
var ErrAnalyticsRebuildSuperseded = errors.New("analytics rebuild publication was superseded")

type AnalyticsRepository struct {
	db           *gorm.DB
	metricFamily model.AnalyticsMetricFamily
}

// WithMetricFamily scopes every content read in this snapshot to the selected channel family.
func (r *AnalyticsRepository) WithMetricFamily(family model.AnalyticsMetricFamily) *AnalyticsRepository {
	return &AnalyticsRepository{db: r.db, metricFamily: family}
}

func (r *AnalyticsRepository) scopeContent(q *gorm.DB, prefix string) *gorm.DB {
	if r.metricFamily == 0 {
		return q
	}
	// New content has an immutable channel; old projections retain a platform or
	// a publication/post identity. Never infer a channel from its parent project.
	expression := "CASE WHEN COALESCE(" + prefix + "channel,'') <> '' THEN CASE WHEN " + prefix + "channel IN ('wechat-article','wechat-picture') THEN 'wechat' ELSE " + prefix + "channel END WHEN COALESCE(" + prefix + "platform,'') <> '' THEN " + prefix + "platform WHEN COALESCE(" + prefix + "post_id,'') <> '' THEN 'seednote' WHEN COALESCE(" + prefix + "publication_id,'') <> '' THEN 'wechat' ELSE '' END"
	platform := ""
	switch r.metricFamily {
	case model.AnalyticsMetricsWechat:
		platform = model.PlatformWechat
	case model.AnalyticsMetricsSeednote:
		platform = model.PlatformSeednote
	}
	return q.Where(expression+" = ?", platform)
}

func (r *AnalyticsRepository) scopeBuckets(ctx context.Context, q *gorm.DB, project string) *gorm.DB {
	if r.metricFamily == 0 {
		return q
	}
	contents := r.scopeContent(r.db.WithContext(ctx).Model(&model.AnalyticsContent{}).Select("id").Where("project_id = ?", project), "")
	return q.Where("content_id IN (?)", contents)
}

// DB is intentionally limited to migration code. Runtime reads and writes use
// the typed methods below so analytics queries cannot accidentally depend on
// legacy tables.
func (r *AnalyticsRepository) DB() *gorm.DB { return r.db }

// Snapshot explicitly requests repeatable read: revision, page, totals and series
// must come from the same committed projection generation.
func (r *AnalyticsRepository) Snapshot(ctx context.Context, fn func(*AnalyticsRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return fn(&AnalyticsRepository{db: tx, metricFamily: r.metricFamily}) }, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
}
func (r *AnalyticsRepository) Project(ctx context.Context, id string) (*model.Project, error) {
	var p model.Project
	e := r.db.WithContext(ctx).Where("id = ?", id).First(&p).Error
	return &p, e
}
func (r *AnalyticsRepository) State(ctx context.Context, project string) (*model.AnalyticsState, error) {
	var s model.AnalyticsState
	e := r.db.WithContext(ctx).Where("project_id = ?", project).First(&s).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return &model.AnalyticsState{ProjectID: project, ActiveGeneration: 1, Status: "ready"}, nil
	}
	return &s, e
}

// LockProject serializes ingestion, revocation and publication of rebuilds.
// It must be called in the caller's transaction before any import mutation.
func (r *AnalyticsRepository) LockProject(ctx context.Context, project string) (*model.AnalyticsState, error) {
	s := model.AnalyticsState{ProjectID: project, ActiveGeneration: 1, Status: "ready"}
	if e := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&s).Error; e != nil {
		return nil, e
	}
	if e := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ?", project).First(&s).Error; e != nil {
		return nil, e
	}
	if s.Status != "ready" {
		return nil, ErrAnalyticsRebuilding
	}
	return &s, nil
}
func (r *AnalyticsRepository) AdvanceRevision(ctx context.Context, s *model.AnalyticsState) error {
	s.Revision++
	s.UpdatedAt = time.Now().UTC()
	return r.db.WithContext(ctx).Model(&model.AnalyticsState{}).Where("project_id = ?", s.ProjectID).Updates(map[string]any{"revision": s.Revision, "updated_at": s.UpdatedAt}).Error
}
func (r *AnalyticsRepository) ClaimIdempotency(ctx context.Context, project, key, hash, batch string) (string, int64, error) {
	s, e := r.LockProject(ctx, project)
	if e != nil {
		return "", 0, e
	}
	var row model.AnalyticsIdempotency
	e = r.db.WithContext(ctx).Where("project_id = ? AND idempotency_key = ?", project, key).First(&row).Error
	if e == nil {
		if row.RequestHash != hash {
			return "", s.Revision, ErrAnalyticsIdempotencyConflict
		}
		return row.BatchID, s.Revision, nil
	}
	if !errors.Is(e, gorm.ErrRecordNotFound) {
		return "", 0, e
	}
	e = r.db.WithContext(ctx).Create(&model.AnalyticsIdempotency{ProjectID: project, Key: key, RequestHash: hash, BatchID: batch, CreatedAt: time.Now().UTC()}).Error
	return "", s.Revision, e
}
func (r *AnalyticsRepository) UpsertContent(ctx context.Context, c *model.AnalyticsContent) error {
	// A content ID belongs permanently to its project. Never permit cross-account
	// writes to retarget an existing identity.
	var old model.AnalyticsContent
	e := r.db.WithContext(ctx).Where("id = ?", c.ID).First(&old).Error
	if e == nil && old.ProjectID != c.ProjectID {
		return errors.New("analytics content belongs to another project")
	}
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return e
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, DoUpdates: clause.AssignmentColumns([]string{"title", "content_type", "status", "url", "date", "task_id", "publication_id", "post_id", "updated_at"})}).Create(c).Error
}
func (r *AnalyticsRepository) InsertObservation(ctx context.Context, o *model.AnalyticsObservation, raw string) (bool, error) {
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(o)
	if result.Error != nil {
		return false, result.Error
	}
	if result.RowsAffected == 0 {
		return false, nil
	}
	if e := r.db.WithContext(ctx).Create(&model.AnalyticsRawPayload{ObservationID: o.ID, Payload: raw}).Error; e != nil {
		return false, e
	}
	return true, nil
}

type AnalyticsAffectedDay struct{ ContentID, MetricBasis, StatDate string }

func (r *AnalyticsRepository) RevokeBatch(ctx context.Context, project, batch string) ([]AnalyticsAffectedDay, error) {
	var rows []AnalyticsAffectedDay
	e := r.db.WithContext(ctx).Model(&model.AnalyticsObservation{}).Select("DISTINCT content_id, metric_basis, stat_date").Where("project_id = ? AND batch_id = ? AND revoked_at IS NULL", project, batch).Order("content_id, metric_basis, stat_date").Find(&rows).Error
	if e != nil {
		return nil, e
	}
	e = r.db.WithContext(ctx).Model(&model.AnalyticsObservation{}).Where("project_id = ? AND batch_id = ? AND revoked_at IS NULL", project, batch).Update("revoked_at", time.Now().UTC()).Error
	return rows, e
}
func analyticsSumColumns(prefix string) string {
	out := []string{}
	for _, c := range model.AnalyticsCountColumns() {
		out = append(out, "SUM("+prefix+c+") AS "+c)
	}
	for _, c := range model.AnalyticsDecimalColumns() {
		out = append(out, "NULL AS "+c)
	}
	return strings.Join(out, ",")
}
func analyticsMetricColumns(prefix string) string {
	out := []string{}
	for _, c := range model.AnalyticsMetricColumns() {
		out = append(out, prefix+c)
	}
	return strings.Join(out, ",")
}
func analyticsBucketWhere(db *gorm.DB, b *model.AnalyticsBucket) *gorm.DB {
	return db.Where("project_id = ? AND generation = ? AND content_id = ? AND metric_basis = ? AND granularity = ? AND bucket_start = ?", b.ProjectID, b.Generation, b.ContentID, b.MetricBasis, b.Granularity, b.BucketStart)
}
func (r *AnalyticsRepository) replaceBucket(ctx context.Context, b *model.AnalyticsBucket, exists bool) error {
	db := r.db.WithContext(ctx)
	if !exists {
		return analyticsBucketWhere(db, b).Delete(&model.AnalyticsBucket{}).Error
	}
	return db.Clauses(clause.OnConflict{UpdateAll: true}).Create(b).Error
}

var AnalyticsLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func AnalyticsBucketBounds(date, granularity string) (string, string) {
	d, _ := time.ParseInLocation("2006-01-02", date, AnalyticsLocation)
	switch granularity {
	case "week":
		days := (int(d.Weekday()) + 6) % 7
		d = d.AddDate(0, 0, -days)
		return d.Format("2006-01-02"), d.AddDate(0, 0, 6).Format("2006-01-02")
	case "month":
		d = time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, AnalyticsLocation)
		return d.Format("2006-01-02"), d.AddDate(0, 1, -1).Format("2006-01-02")
	}
	return date, date
}

// RecomputeDay touches one winning content/day and its week/month ancestors.
// Account projections aggregate per-content buckets, so cumulative values are
// never summed across dates. No raw payload is read by the projector.
func (r *AnalyticsRepository) RecomputeDay(ctx context.Context, project string, generation int64, content, basis, date string) error {
	var winner model.AnalyticsObservation
	e := r.db.WithContext(ctx).Where("project_id = ? AND content_id = ? AND metric_basis = ? AND stat_date = ? AND revoked_at IS NULL", project, content, basis, date).Order("source_priority DESC, effective_at DESC, received_at DESC, sequence DESC").First(&winner).Error
	if e != nil && !errors.Is(e, gorm.ErrRecordNotFound) {
		return e
	}
	day := model.AnalyticsBucket{ProjectID: project, Generation: generation, ContentID: content, MetricBasis: basis, Granularity: "day", BucketStart: date, LastStatDate: date, ObservationID: winner.ID, Coverage: 1, AnalyticsMetrics: winner.AnalyticsMetrics}
	if e = r.replaceBucket(ctx, &day, e == nil); e != nil {
		return e
	}
	for _, gran := range []string{"day", "week", "month"} {
		start, end := AnalyticsBucketBounds(date, gran)
		if gran != "day" {
			b := day
			b.Granularity = gran
			b.BucketStart = start
			q := r.db.WithContext(ctx).Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ? AND content_id = ? AND metric_basis = ? AND granularity = 'day' AND bucket_start BETWEEN ? AND ?", project, generation, content, basis, start, end)
			var n int64
			if e = q.Count(&n).Error; e != nil {
				return e
			}
			if n > 0 {
				if basis == "cumulative" {
					var last model.AnalyticsBucket
					if e = q.Order("bucket_start DESC").First(&last).Error; e != nil {
						return e
					}
					b.AnalyticsMetrics = last.AnalyticsMetrics
					b.LastStatDate = last.LastStatDate
					b.ObservationID = last.ObservationID
				} else {
					var aggregate model.AnalyticsBucket
					if e = q.Select(analyticsSumColumns("") + ", MAX(last_stat_date) AS last_stat_date").Scan(&aggregate).Error; e != nil {
						return e
					}
					b.AnalyticsMetrics = aggregate.AnalyticsMetrics
					b.LastStatDate = aggregate.LastStatDate
					b.ObservationID = ""
				}
			}
			if e = r.replaceBucket(ctx, &b, n > 0); e != nil {
				return e
			}
		}
		account := model.AnalyticsBucket{ProjectID: project, Generation: generation, MetricBasis: basis, Granularity: gran, BucketStart: start}
		q := r.db.WithContext(ctx).Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ? AND content_id <> '' AND metric_basis = ? AND granularity = ? AND bucket_start = ?", project, generation, basis, gran, start)
		var aggregate model.AnalyticsBucket
		if e = q.Select(analyticsSumColumns("") + ", COUNT(*) AS coverage, MAX(last_stat_date) AS last_stat_date").Scan(&aggregate).Error; e != nil {
			return e
		}
		account.AnalyticsMetrics = aggregate.AnalyticsMetrics
		account.Coverage = aggregate.Coverage
		account.LastStatDate = aggregate.LastStatDate
		if e = r.replaceBucket(ctx, &account, aggregate.Coverage > 0); e != nil {
			return e
		}
	}
	return nil
}

// Range selection covers complete months and at most 60 edge days. All reads
// below operate on projections, never observations or raw import documents.
func analyticsRangeCondition(from, to string) (string, []any) {
	start, _ := time.ParseInLocation("2006-01-02", from, AnalyticsLocation)
	finish, _ := time.ParseInLocation("2006-01-02", to, AnalyticsLocation)
	first := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, AnalyticsLocation)
	if start.Day() != 1 {
		first = first.AddDate(0, 1, 0)
	}
	lastExclusive := time.Date(finish.Year(), finish.Month(), 1, 0, 0, 0, 0, AnalyticsLocation)
	if finish.AddDate(0, 0, 1).Day() == 1 {
		lastExclusive = lastExclusive.AddDate(0, 1, 0)
	}
	if !first.Before(lastExclusive) {
		return "granularity = 'day' AND bucket_start BETWEEN ? AND ?", []any{from, to}
	}
	return "((granularity = 'month' AND bucket_start >= ? AND bucket_start < ?) OR (granularity = 'day' AND bucket_start BETWEEN ? AND ? AND (bucket_start < ? OR bucket_start >= ?)))", []any{first.Format("2006-01-02"), lastExclusive.Format("2006-01-02"), from, to, first.Format("2006-01-02"), lastExclusive.Format("2006-01-02")}
}
func (r *AnalyticsRepository) rangeQuery(ctx context.Context, project string, generation int64, basis, from, to, content string) *gorm.DB {
	condition, args := analyticsRangeCondition(from, to)
	q := r.db.WithContext(ctx).Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ? AND metric_basis = ? AND content_id <> ''", project, generation, basis).Where(condition, args...)
	if content != "" {
		q = q.Where("content_id = ?", content)
	}
	return r.scopeBuckets(ctx, q, project)
}
func (r *AnalyticsRepository) contentRange(ctx context.Context, project string, generation int64, basis, from, to, content string) *gorm.DB {
	q := r.rangeQuery(ctx, project, generation, basis, from, to, content)
	if basis == "cumulative" {
		ranked := q.Select("content_id,last_stat_date," + analyticsMetricColumns("") + ", ROW_NUMBER() OVER (PARTITION BY content_id ORDER BY last_stat_date DESC) AS rn")
		return r.db.WithContext(ctx).Table("(?) AS ranked", ranked).Select("content_id,last_stat_date," + analyticsMetricColumns("")).Where("rn = 1")
	}
	return q.Select("content_id,MAX(last_stat_date) AS last_stat_date," + analyticsSumColumns("")).Group("content_id")
}
func (r *AnalyticsRepository) RangeTotals(ctx context.Context, project string, generation int64, basis, from, to, content string, preserveRatios bool) (model.AnalyticsMetrics, int64, error) {
	sub := r.contentRange(ctx, project, generation, basis, from, to, content)
	var row struct {
		model.AnalyticsMetrics
		Coverage int64
	}
	selects := analyticsSumColumns("") + ", COUNT(*) AS coverage"
	if preserveRatios && basis == "cumulative" && content != "" {
		selects = analyticsMetricColumns("") + ", 1 AS coverage"
	}
	e := r.db.WithContext(ctx).Table("(?) AS ranged", sub).Select(selects).Scan(&row).Error
	return row.AnalyticsMetrics, row.Coverage, e
}
func (r *AnalyticsRepository) ResolveContent(ctx context.Context, project, id string) (*model.AnalyticsContent, error) {
	content, err := r.resolveContentIdentity(ctx, project, id)
	if err != nil {
		return content, err
	}
	metadata := r.contentMetadata(ctx, project)
	err = metadata.query.Select(metadata.columns()).Where("c.id = ?", content.ID).Take(content).Error
	return content, err
}

func (r *AnalyticsRepository) resolveContentIdentity(ctx context.Context, project, id string) (*model.AnalyticsContent, error) {
	q := r.scopeContent(r.db.WithContext(ctx).Where("project_id = ?", project), "")
	parts := strings.SplitN(id, ":", 2)
	if len(parts) == 2 {
		switch parts[0] {
		case "task":
			q = q.Where("id = ? OR task_id = ?", id, parts[1])
		case "wechat_publication":
			q = q.Where("id = ? OR publication_id = ?", id, parts[1])
		case "seednote_post":
			q = q.Where("id = ? OR post_id = ?", id, parts[1])
		default:
			q = q.Where("id = ?", id)
		}
	} else {
		q = q.Where("id = ?", id)
	}
	var c model.AnalyticsContent
	e := q.Order("date IS NULL ASC, date DESC, id ASC").First(&c).Error
	if !errors.Is(e, gorm.ErrRecordNotFound) || len(parts) != 2 || parts[0] != "task" {
		return &c, e
	}
	// Legacy imports could create a publication/post content row before the
	// task identity was copied into analytics_contents. Resolve the task deep
	// link through the authoritative operational identity tables as a fallback.
	if r.db.Migrator().HasTable("wechat_publications") {
		var alias model.AnalyticsContent
		e2 := r.scopeContent(r.db.WithContext(ctx).Table("analytics_contents AS c"), "c.").Select("c.*").
			Joins("JOIN wechat_publications AS p ON p.id = c.publication_id AND p.project_id = c.project_id").
			Where("c.project_id = ? AND p.task_id = ?", project, parts[1]).
			Order("c.date IS NULL ASC, c.date DESC, c.id ASC").First(&alias).Error
		if e2 == nil {
			return &alias, nil
		}
		if !errors.Is(e2, gorm.ErrRecordNotFound) {
			return &c, e2
		}
	}
	if r.db.Migrator().HasTable("seednote_posts") {
		var alias model.AnalyticsContent
		e2 := r.scopeContent(r.db.WithContext(ctx).Table("analytics_contents AS c"), "c.").Select("c.*").
			Joins("JOIN seednote_posts AS p ON p.id = c.post_id AND p.project_id = c.project_id").
			Where("c.project_id = ? AND p.task_id = ?", project, parts[1]).
			Order("c.date IS NULL ASC, c.date DESC, c.id ASC").First(&alias).Error
		if e2 == nil {
			return &alias, nil
		}
		if !errors.Is(e2, gorm.ErrRecordNotFound) {
			return &c, e2
		}
	}
	return &c, e
}

type AnalyticsContentRow struct {
	model.AnalyticsContent
	model.AnalyticsMetrics
	LastStatDate string `json:"last_stat_date,omitempty"`
}
type AnalyticsContentFilter struct {
	Search, ContentType, Sort, Direction string
	Offset, Limit                        int
}

// contentMetadata resolves operational metadata once for list, filter and detail.
// Stored analytics identity and observation dates remain unchanged.
type analyticsContentMetadata struct {
	query                           *gorm.DB
	title, contentType, status, url string
}

func (m analyticsContentMetadata) columns() string {
	return "c.*, " + m.title + " AS title, " + m.contentType + " AS content_type, " + m.status + " AS status, " + m.url + " AS url"
}

func (r *AnalyticsRepository) contentMetadata(ctx context.Context, project string) analyticsContentMetadata {
	q := r.scopeContent(r.db.WithContext(ctx).Table("analytics_contents AS c").Where("c.project_id = ?", project), "c.")
	liveTitle, liveType, liveStatus, liveURL := "c.title", analyticsContentTypeSQL("c.content_type"), "c.status", "c.url"
	if r.db.Migrator().HasTable("tasks") {
		q = q.Joins("LEFT JOIN tasks AS t ON t.id = c.task_id AND t.project_id = c.project_id")
		liveTitle = "COALESCE(NULLIF(t.title,''), c.title)"
		liveType = analyticsContentTypeSQL("COALESCE(NULLIF(t.channel,''),NULLIF(t.type,''), c.content_type)")
		liveStatus = "COALESCE(NULLIF(t.status,''), c.status)"
	}
	if r.db.Migrator().HasTable("wechat_publications") {
		q = q.Joins("LEFT JOIN wechat_publications AS p ON p.id = c.publication_id AND p.project_id = c.project_id")
		liveTitle = "COALESCE(NULLIF(p.draft_title,''), " + liveTitle + ")"
		liveStatus = "COALESCE(NULLIF(p.status,''), " + liveStatus + ")"
		liveURL = "COALESCE(NULLIF(p.article_url,''), " + liveURL + ")"
		liveType = "CASE WHEN p.id IS NOT NULL THEN " + analyticsWechatTypeSQL("p.draft_article_type") + " ELSE " + liveType + " END"
	}
	if r.db.Migrator().HasTable("seednote_posts") {
		q = q.Joins("LEFT JOIN seednote_posts AS sp ON sp.id = c.post_id AND sp.project_id = c.project_id")
		liveTitle = "COALESCE(NULLIF(sp.title,''), " + liveTitle + ")"
		liveType = "CASE WHEN sp.id IS NOT NULL THEN " + analyticsSeednoteTypeSQL("sp.genre") + " ELSE " + liveType + " END"
		liveURL = "COALESCE(NULLIF(sp.note_url,''), " + liveURL + ")"
	}
	return analyticsContentMetadata{q, liveTitle, liveType, liveStatus, liveURL}
}

func (r *AnalyticsRepository) Contents(ctx context.Context, project string, generation int64, basis, from, to string, f AnalyticsContentFilter) ([]AnalyticsContentRow, int64, error) {
	sub := r.contentRange(ctx, project, generation, basis, from, to, "")
	metadata := r.contentMetadata(ctx, project)
	q := metadata.query.Joins("LEFT JOIN (?) AS m ON m.content_id = c.id", sub)
	liveTitle, liveType := metadata.title, metadata.contentType
	if f.Search != "" {
		q = q.Where(liveTitle+" LIKE ?", "%"+f.Search+"%")
	}
	if f.ContentType != "" {
		q = q.Where(liveType+" = ?", f.ContentType)
	}
	var n int64
	if e := q.Count(&n).Error; e != nil {
		return nil, 0, e
	}
	column := "m.last_stat_date"
	switch f.Sort {
	case "title":
		column = liveTitle
	case "date", "":
	default:
		valid := false
		for _, c := range model.AnalyticsMetricColumns() {
			if c == f.Sort {
				column = "m." + c
				valid = true
				break
			}
		}
		if !valid {
			return nil, 0, fmt.Errorf("invalid analytics sort")
		}
	}
	direction := "DESC"
	if f.Direction == "asc" {
		direction = "ASC"
	}
	order := column + " IS NULL ASC, " + column + " " + direction + ", c.id ASC"
	for _, c := range model.AnalyticsDecimalColumns() {
		if f.Sort == c {
			order = column + " IS NULL ASC, CAST(" + column + " AS DECIMAL(38,18)) " + direction + ", c.id ASC"
		}
	}
	rows := []AnalyticsContentRow{}
	e := q.Select(metadata.columns() + ", COALESCE(m.last_stat_date, '') AS last_stat_date, " + analyticsMetricColumns("m.")).Order(order).Offset(f.Offset).Limit(f.Limit).Scan(&rows).Error
	return rows, n, e
}
func (r *AnalyticsRepository) ObservationPage(ctx context.Context, project, content, from, to, basis string, offset, limit int) ([]model.AnalyticsObservation, int64, error) {
	q := r.db.WithContext(ctx).Model(&model.AnalyticsObservation{}).Where("project_id = ? AND content_id = ? AND metric_basis = ? AND stat_date BETWEEN ? AND ?", project, content, basis, from, to)
	q = r.scopeBuckets(ctx, q, project)
	var count int64
	if e := q.Count(&count).Error; e != nil {
		return nil, 0, e
	}
	rows := []model.AnalyticsObservation{}
	e := q.Order("stat_date DESC, source_priority DESC, effective_at DESC, received_at DESC, sequence DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, count, e
}
func (r *AnalyticsRepository) Dates(ctx context.Context, project string, generation int64, basis string, year int) ([]string, error) {
	dates := []string{}
	q := r.db.WithContext(ctx).Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ? AND content_id <> '' AND metric_basis = ? AND granularity = 'day' AND bucket_start >= ? AND bucket_start < ?", project, generation, basis, fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-01-01", year+1))
	e := r.scopeBuckets(ctx, q, project).Distinct("bucket_start").Order("bucket_start").Pluck("bucket_start", &dates).Error
	return dates, e
}

func (r *AnalyticsRepository) Series(ctx context.Context, project string, generation int64, basis, from, to, granularity, content string) ([]model.AnalyticsBucket, error) {
	start, _ := AnalyticsBucketBounds(from, granularity)
	rows := []model.AnalyticsBucket{}
	q := r.db.WithContext(ctx).Model(&model.AnalyticsBucket{}).Where("project_id = ? AND generation = ? AND metric_basis = ? AND granularity = ? AND bucket_start BETWEEN ? AND ?", project, generation, basis, granularity, start, to)
	if content != "" || r.metricFamily == 0 {
		q = r.scopeBuckets(ctx, q.Where("content_id = ?", content), project)
	} else {
		q = r.scopeBuckets(ctx, q.Where("content_id <> ''"), project).Select("bucket_start," + analyticsSumColumns("") + ", COUNT(*) AS coverage, MAX(last_stat_date) AS last_stat_date").Group("bucket_start")
	}
	e := q.Order("bucket_start").Find(&rows).Error
	if e != nil {
		return nil, e
	}
	// A boundary bucket may contain out-of-range observations, including a latest
	// cumulative snapshot after 'to'. Re-evaluate just those (at most two) buckets.
	clipped := make([]model.AnalyticsBucket, 0, len(rows))
	for _, b := range rows {
		bs, be := AnalyticsBucketBounds(b.BucketStart, granularity)
		if bs < from || be > to {
			lo, hi := bs, be
			if lo < from {
				lo = from
			}
			if hi > to {
				hi = to
			}
			m, n, e := r.RangeTotals(ctx, project, generation, basis, lo, hi, content, content != "")
			if e != nil {
				return nil, e
			}
			if n == 0 {
				continue
			}
			b.AnalyticsMetrics = m
			b.Coverage = n
		}
		clipped = append(clipped, b)
	}
	return clipped, nil
}

// Rebuild projections into an unpublished generation in bounded day-key pages.
// The job owner holds the project write gate until it atomically publishes it.
func (r *AnalyticsRepository) Rebuild(ctx context.Context, project string, generation int64) error {
	if generation <= 0 {
		return errors.New("invalid analytics generation")
	}
	if e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var state model.AnalyticsState
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ?", project).First(&state).Error; err != nil {
			return err
		}
		if state.Status != "rebuilding" || state.ActiveGeneration >= generation {
			return ErrAnalyticsRebuildSuperseded
		}
		return tx.Where("project_id = ? AND generation = ?", project, generation).Delete(&model.AnalyticsBucket{}).Error
	}); e != nil {
		return e
	}
	var after AnalyticsAffectedDay
	for {
		rows := []AnalyticsAffectedDay{}
		q := r.db.WithContext(ctx).Model(&model.AnalyticsObservation{}).Select("DISTINCT content_id, metric_basis, stat_date").Where("project_id = ? AND revoked_at IS NULL AND metric_basis IN ?", project, []string{"daily", "cumulative"})
		if after.ContentID != "" {
			q = q.Where("content_id > ? OR (content_id = ? AND metric_basis > ?) OR (content_id = ? AND metric_basis = ? AND stat_date > ?)", after.ContentID, after.ContentID, after.MetricBasis, after.ContentID, after.MetricBasis, after.StatDate)
		}
		if e := q.Order("content_id, metric_basis, stat_date").Limit(500).Scan(&rows).Error; e != nil {
			return e
		}
		if e := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var state model.AnalyticsState
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ?", project).First(&state).Error; err != nil {
				return err
			}
			if state.Status != "rebuilding" || state.ActiveGeneration >= generation {
				return ErrAnalyticsRebuildSuperseded
			}
			return (&AnalyticsRepository{db: tx}).RecomputeDays(ctx, project, generation, rows)
		}); e != nil {
			return e
		}

		if len(rows) < 500 {
			return nil
		}
		after = rows[len(rows)-1]
	}
}

func (r *AnalyticsRepository) DBCreateRebuildJob(ctx context.Context, j *model.AnalyticsRebuildJob) error {
	// MySQL JSON rejects an empty string; legacy SQL-created jobs can also
	// load NULL into the Go string before their first progress update.
	if strings.TrimSpace(j.ReportJSON) == "" {
		j.ReportJSON = "{}"
	}
	return r.db.WithContext(ctx).Create(j).Error
}
func (r *AnalyticsRepository) FindRebuildJob(ctx context.Context, id string) (*model.AnalyticsRebuildJob, error) {
	var j model.AnalyticsRebuildJob
	e := r.db.WithContext(ctx).Where("id = ?", id).First(&j).Error
	return &j, e
}

// ClaimRebuildJob permits only one worker to process a rebuild. A stale
// running lease can be reclaimed after a process crash.
func (r *AnalyticsRepository) ClaimRebuildJob(ctx context.Context, id string, staleBefore time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.AnalyticsRebuildJob{}).
		Where("id = ? AND (status = ? OR (status = ? AND updated_at < ?))", id, "queued", "running", staleBefore).
		Updates(map[string]any{"status": "running", "updated_at": time.Now().UTC()})
	return result.RowsAffected == 1, result.Error
}

func (r *AnalyticsRepository) ListRebuildJobs(ctx context.Context, statuses []string, limit int) ([]*model.AnalyticsRebuildJob, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	var jobs []*model.AnalyticsRebuildJob
	q := r.db.WithContext(ctx).Order("updated_at ASC").Limit(limit)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	if err := q.Find(&jobs).Error; err != nil {
		return nil, err
	}
	return jobs, nil
}

func (r *AnalyticsRepository) UpdateRebuildJob(ctx context.Context, j *model.AnalyticsRebuildJob) error {
	if strings.TrimSpace(j.ReportJSON) == "" {
		j.ReportJSON = "{}"
	}
	return r.db.WithContext(ctx).Save(j).Error
}
func (r *AnalyticsRepository) BeginRebuild(ctx context.Context, project string) (*model.AnalyticsState, error) {
	s := model.AnalyticsState{ProjectID: project, ActiveGeneration: 1, Status: "ready"}
	if e := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&s).Error; e != nil {
		return nil, e
	}
	if e := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ?", project).First(&s).Error; e != nil {
		return nil, e
	}
	if s.Status != "ready" {
		return nil, ErrAnalyticsRebuilding
	}
	s.Status = "rebuilding"
	s.UpdatedAt = time.Now().UTC()
	if e := r.db.WithContext(ctx).Save(&s).Error; e != nil {
		return nil, e
	}
	return &s, nil
}
func (r *AnalyticsRepository) PublishRebuild(ctx context.Context, project string, generation int64) error {
	result := r.db.WithContext(ctx).Model(&model.AnalyticsState{}).Where("project_id = ? AND status = ? AND active_generation < ?", project, "rebuilding", generation).Updates(map[string]any{"active_generation": generation, "status": "ready", "revision": gorm.Expr("revision + 1"), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAnalyticsRebuildSuperseded
	}
	// Buckets are a replaceable read projection. Once the new generation is
	// published, remove every older projection for this project in the same
	// transaction so repeated rebuilds cannot grow storage without bound.
	return r.db.WithContext(ctx).Where("project_id = ? AND generation <> ?", project, generation).Delete(&model.AnalyticsBucket{}).Error
}

// AbortRebuild releases the project write gate after a failed migration or
// projection build. The previously published generation remains active and a
// later retry can start from the durable legacy facts/checkpoint.
func (r *AnalyticsRepository) AbortRebuild(ctx context.Context, project string) error {
	return r.db.WithContext(ctx).Model(&model.AnalyticsState{}).Where("project_id = ? AND status = ?", project, "rebuilding").Updates(map[string]any{"status": "ready", "updated_at": time.Now().UTC()}).Error
}

// Expressions are internal column references, never request values. Keep the
// selected value and filter expression identical so labels and filters agree.
func analyticsContentTypeSQL(expression string) string {
	return "CASE WHEN " + expression + " IN ('wechat-article','wechat-picture','image_text','video') THEN " + expression + " ELSE 'unknown' END"
}
func analyticsWechatTypeSQL(expression string) string {
	return "CASE " + expression + " WHEN 'news' THEN 'wechat-article' WHEN 'newspic' THEN 'wechat-picture' ELSE 'unknown' END"
}
func analyticsSeednoteTypeSQL(expression string) string {
	return "CASE WHEN " + expression + " IN ('image','image_text','image_note','图文','图文笔记') THEN 'image_text' WHEN " + expression + " IN ('video','video_note','视频') THEN 'video' ELSE 'unknown' END"
}
