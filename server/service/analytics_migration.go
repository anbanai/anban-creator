package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// AnalyticsMigrationReport is persisted on the rebuild job. Unknown Seednote
// basis and missing identity are counted explicitly and never enter totals.
type AnalyticsMigrationReport struct {
	WechatImports     int64            `json:"wechat_imports"`
	WechatOfficial    int64            `json:"wechat_official"`
	SeednoteImports   int64            `json:"seednote_imports"`
	SeednoteOfficial  int64            `json:"seednote_official"`
	Inserted          int64            `json:"inserted"`
	Duplicates        int64            `json:"duplicates"`
	Revoked           int64            `json:"revoked"`
	BasisUnknown      int64            `json:"basis_unknown"`
	IdentityConflicts int64            `json:"identity_conflicts"`
	Skipped           int64            `json:"skipped"`
	Reasons           map[string]int64 `json:"reasons"`
}

func (r *AnalyticsMigrationReport) skip(reason string) {
	r.Skipped++
	if r.Reasons == nil {
		r.Reasons = map[string]int64{}
	}
	r.Reasons[reason]++
}

// AnalyticsRebuildManager converts legacy facts, rebuilds an unpublished
// generation, and publishes it atomically. Legacy rows remain for audit.
type AnalyticsRebuildManager struct{ repo repository.Repository }

func NewAnalyticsRebuildManager(repo repository.Repository) *AnalyticsRebuildManager {
	return &AnalyticsRebuildManager{repo: repo}
}

func (m *AnalyticsRebuildManager) Start(ctx context.Context, project string) (*model.AnalyticsRebuildJob, error) {
	if project == "" {
		return nil, errors.New("project required")
	}
	var job model.AnalyticsRebuildJob
	err := m.repo.WithTx(ctx, func(tx repository.Repository) error {
		state, e := tx.Analytics().BeginRebuild(ctx, project)
		if e != nil {
			return e
		}
		now := time.Now().UTC()
		job = model.AnalyticsRebuildJob{ID: uuid.NewString(), ProjectID: project, Generation: state.ActiveGeneration + 1, Status: "queued", CreatedAt: now, UpdatedAt: now}
		return tx.Analytics().DBCreateRebuildJob(ctx, &job)
	})
	return &job, err
}

func (m *AnalyticsRebuildManager) Run(ctx context.Context, jobID string) error {
	job, err := m.repo.Analytics().FindRebuildJob(ctx, jobID)
	if err != nil {
		return err
	}
	if job.Status == "published" {
		return nil
	}
	state, err := m.repo.Analytics().State(ctx, job.ProjectID)
	if err != nil {
		return err
	}
	// A worker may have published the generation before crashing while marking
	// the job complete. Do not rebuild an already active generation.
	if state.ActiveGeneration >= job.Generation && state.Status == "ready" {
		job.Status, job.UpdatedAt = "published", time.Now().UTC()
		return m.repo.Analytics().UpdateRebuildJob(ctx, job)
	}
	claimed, err := m.repo.Analytics().ClaimRebuildJob(ctx, jobID, time.Now().UTC().Add(-15*time.Minute))
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	// Once a job is claimed, every failure must release the project write gate
	// and persist a terminal job status. Use a detached context for cleanup so a
	// canceled worker request cannot strand the project in rebuilding state.
	fail := func(cause error) error {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		abortErr := m.repo.Analytics().AbortRebuild(cleanupCtx, job.ProjectID)
		job.Status, job.Error, job.UpdatedAt = "failed", cause.Error(), time.Now().UTC()
		jobErr := m.repo.Analytics().UpdateRebuildJob(cleanupCtx, job)
		return errors.Join(cause, abortErr, jobErr)
	}
	job.Status, job.UpdatedAt = "running", time.Now().UTC()
	if err = m.repo.Analytics().UpdateRebuildJob(ctx, job); err != nil {
		return fail(err)
	}
	report, err := m.migrateLegacy(ctx, job)
	if err != nil {
		return fail(err)
	}
	encodedReport, _ := json.Marshal(report)
	job.ReportJSON = string(encodedReport)
	if err = m.repo.Analytics().Rebuild(ctx, job.ProjectID, job.Generation); err != nil {
		return fail(err)
	}
	if err = m.repo.WithTx(ctx, func(tx repository.Repository) error {
		return tx.Analytics().PublishRebuild(ctx, job.ProjectID, job.Generation)
	}); err != nil {
		return fail(err)
	}
	job.Status, job.UpdatedAt, job.Processed = "published", time.Now().UTC(), report.Inserted
	if err := m.repo.Analytics().UpdateRebuildJob(ctx, job); err != nil {
		// Publication is already durable; fail only the job record update. The
		// next retry observes the active generation and repairs the job status.
		return err
	}
	return nil
}
func (m *AnalyticsRebuildManager) Status(ctx context.Context, jobID string) (*model.AnalyticsRebuildJob, error) {
	return m.repo.Analytics().FindRebuildJob(ctx, jobID)
}
func (m *AnalyticsRebuildManager) String() string { return fmt.Sprintf("analytics rebuild manager") }

type legacyMigrationInput struct {
	content model.AnalyticsContent
	obs     model.AnalyticsObservation
	raw     string
}

// migrateLegacy processes rows in 500-record transactions. The state remains
// rebuilding throughout, so normal imports are rejected until publication.
func (m *AnalyticsRebuildManager) migrateLegacy(ctx context.Context, job *model.AnalyticsRebuildJob) (AnalyticsMigrationReport, error) {
	report := AnalyticsMigrationReport{Reasons: map[string]int64{}}
	project, generation := job.ProjectID, job.Generation
	db := m.repo.Analytics().DB()
	pending := make([]legacyMigrationInput, 0, 500)
	flush := func() error {
		if len(pending) == 0 {
			return nil
		}
		batch := pending
		pending = make([]legacyMigrationInput, 0, 500)
		err := db.WithContext(ctx).Transaction(func(txdb *gorm.DB) error {
			r := repository.New(txdb).Analytics()
			// Refresh list metadata from authoritative operational tables while
			// importing legacy facts. Analytics rows must never become the source
			// of truth for titles, URLs, or lifecycle state.
			taskIDs, pubIDs, postIDs := make([]string, 0), make([]string, 0), make([]string, 0)
			for _, in := range batch {
				if strings.HasPrefix(in.content.ID, "task:") {
					taskIDs = append(taskIDs, strings.TrimPrefix(in.content.ID, "task:"))
				}
				if strings.HasPrefix(in.content.ID, "wechat_publication:") {
					pubIDs = append(pubIDs, strings.TrimPrefix(in.content.ID, "wechat_publication:"))
				}
				if strings.HasPrefix(in.content.ID, "seednote_post:") {
					postIDs = append(postIDs, strings.TrimPrefix(in.content.ID, "seednote_post:"))
				}
			}
			var tasks []model.Task
			var pubs []model.WechatPublication
			var posts []model.SeednotePost
			if len(taskIDs) > 0 {
				if e := txdb.Where("id IN ?", taskIDs).Find(&tasks).Error; e != nil {
					return e
				}
			}
			if len(pubIDs) > 0 {
				if e := txdb.Where("id IN ?", pubIDs).Find(&pubs).Error; e != nil {
					return e
				}
			}
			if len(postIDs) > 0 {
				if e := txdb.Where("id IN ?", postIDs).Find(&posts).Error; e != nil {
					return e
				}
			}
			taskByID, pubByID, postByID := map[string]model.Task{}, map[string]model.WechatPublication{}, map[string]model.SeednotePost{}
			for _, v := range tasks {
				taskByID[v.ID] = v
			}
			for _, v := range pubs {
				pubByID[v.ID] = v
			}
			for _, v := range posts {
				postByID[v.ID] = v
			}
			affected := make([]repository.AnalyticsAffectedDay, 0, len(batch))
			for _, in := range batch {
				if id := strings.TrimPrefix(in.content.ID, "task:"); id != in.content.ID {
					if v, ok := taskByID[id]; ok {
						in.content.Title = v.Title
						if in.content.Title == "" {
							in.content.Title = v.Prompt
						}
						in.content.ContentType = v.Type
						in.content.Status = v.Status
						in.content.Date = &v.CreatedAt
					}
				}
				if id := strings.TrimPrefix(in.content.ID, "wechat_publication:"); id != in.content.ID {
					if v, ok := pubByID[id]; ok {
						in.content.Title = v.DraftTitle
						in.content.ContentType = "article"
						in.content.Status = v.Status
						in.content.URL = v.ArticleURL
						in.content.Date = v.PublishedAt
					}
				}
				if id := strings.TrimPrefix(in.content.ID, "seednote_post:"); id != in.content.ID {
					if v, ok := postByID[id]; ok {
						in.content.Title = v.Title
						in.content.ContentType = v.Genre
						in.content.Status = "recorded"
						in.content.URL = v.NoteURL
						in.content.Date = v.FirstPublishedAt
					}
				}
				if err := r.UpsertContent(ctx, &in.content); err != nil {
					return err
				}
				var prior model.AnalyticsObservation
				err := txdb.Where("id = ?", in.obs.ID).First(&prior).Error
				if errors.Is(err, gorm.ErrRecordNotFound) {
					inserted, insertErr := r.InsertObservation(ctx, &in.obs, in.raw)
					if insertErr != nil {
						return insertErr
					}
					if inserted {
						report.Inserted++
						affected = append(affected, repository.AnalyticsAffectedDay{ContentID: in.obs.ContentID, MetricBasis: in.obs.MetricBasis, StatDate: in.obs.StatDate})
					} else {
						report.Duplicates++
					}
				} else if err != nil {
					return err
				} else if strings.HasPrefix(in.obs.ID, "legacy:") {
					// Legacy observations are deterministic projections of source rows.
					// Refresh them on reruns so identity resolution and revocation changes
					// are reflected without touching modern observations.
					if err := txdb.Model(&model.AnalyticsObservation{}).Where("id = ?", in.obs.ID).Select("*").Omit("sequence").Updates(&in.obs).Error; err != nil {
						return err
					}
					if err := txdb.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "observation_id"}}, DoUpdates: clause.AssignmentColumns([]string{"payload"})}).Create(&model.AnalyticsRawPayload{ObservationID: in.obs.ID, Payload: in.raw}).Error; err != nil {
						return err
					}
					report.Duplicates++
					affected = append(affected,
						repository.AnalyticsAffectedDay{ContentID: prior.ContentID, MetricBasis: prior.MetricBasis, StatDate: prior.StatDate},
						repository.AnalyticsAffectedDay{ContentID: in.obs.ContentID, MetricBasis: in.obs.MetricBasis, StatDate: in.obs.StatDate},
					)
				} else {
					// A non-legacy conflict is an existing modern fact. Preserve it.
					report.Duplicates++
				}
				if in.obs.RevokedAt != nil {
					report.Revoked++
				}
			}
			return r.RecomputeDays(ctx, project, generation, affected)
		})
		if err != nil {
			return err
		}
		job.Processed = report.Inserted + report.Duplicates
		job.UpdatedAt = time.Now().UTC()
		return m.repo.Analytics().UpdateRebuildJob(ctx, job)
	}
	queue := func(in legacyMigrationInput) error {
		pending = append(pending, in)
		if len(pending) >= 500 {
			return flush()
		}
		return nil
	}
	job.CursorSource = "wechat_import"
	if err := m.repo.Analytics().UpdateRebuildJob(ctx, job); err != nil {
		return report, err
	}
	if err := m.migrateWechatImports(ctx, project, &report, queue); err != nil {
		return report, err
	}
	job.CursorSource = "wechat_official"
	if err := m.repo.Analytics().UpdateRebuildJob(ctx, job); err != nil {
		return report, err
	}
	if err := m.migrateWechatOfficial(ctx, project, &report, queue); err != nil {
		return report, err
	}
	job.CursorSource = "seednote_import"
	if err := m.repo.Analytics().UpdateRebuildJob(ctx, job); err != nil {
		return report, err
	}
	if err := m.migrateSeednoteImports(ctx, project, &report, queue); err != nil {
		return report, err
	}
	job.CursorSource = "seednote_official"
	if err := m.repo.Analytics().UpdateRebuildJob(ctx, job); err != nil {
		return report, err
	}
	if err := m.migrateSeednoteOfficial(ctx, project, &report, queue); err != nil {
		return report, err
	}
	return report, flush()
}

func dateShanghai(t time.Time) string { return t.In(repository.AnalyticsLocation).Format("2006-01-02") }
func int64ptr(v int) *int64           { n := int64(v); return &n }
func decimalFromFloat(v float64) *model.AnalyticsDecimal {
	n := model.AnalyticsDecimal(strconv.FormatFloat(v, 'f', -1, 64))
	if n.Validate() != nil {
		return nil
	}
	return &n
}
func decimalFromFloatPtr(v *float64) *model.AnalyticsDecimal {
	if v == nil {
		return nil
	}
	return decimalFromFloat(*v)
}
func legacyRevoked(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	t := at.UTC()
	return &t
}

// Prefix and column are internal SQL constants used to identify observations
// that were already written by the modern import path.
func analyticsPrefixedIDSQL(dialect, prefix, column string) string {
	if dialect == "mysql" {
		return "CONCAT('" + prefix + "', " + column + ")"
	}
	return "('" + prefix + "' || " + column + ")"
}

func (m *AnalyticsRebuildManager) migrateWechatImports(ctx context.Context, project string, report *AnalyticsMigrationReport, queue func(legacyMigrationInput) error) error {
	type row struct {
		model.WechatAnalyticsSnapshot
		BatchRevokedAt *time.Time `gorm:"column:batch_revoked_at"`
	}
	db := m.repo.Analytics().DB()
	if !db.Migrator().HasTable("wechat_analytics_snapshots") {
		return nil
	}
	var rows []row
	query := db.WithContext(ctx).Table("wechat_analytics_snapshots s").Select("s.*, b.revoked_at AS batch_revoked_at").Joins("LEFT JOIN wechat_analytics_import_batches b ON b.id=s.batch_id AND b.project_id=s.project_id").Where("s.project_id = ?", project)
	// Modern imports already wrote the immutable fact in the same transaction.
	// Their retained legacy snapshots are audit copies, not additional versions.
	duplicateCondition := "EXISTS (SELECT 1 FROM analytics_observations o WHERE o.project_id = s.project_id AND o.id = " + analyticsPrefixedIDSQL(db.Dialector.Name(), "wechat_import:", "s.import_row_id") + ")"
	var duplicateCount int64
	if err := db.WithContext(ctx).Table("wechat_analytics_snapshots s").Where("s.project_id = ?", project).Where(duplicateCondition).Count(&duplicateCount).Error; err != nil {
		return err
	}
	report.Duplicates += duplicateCount
	report.WechatImports += duplicateCount
	var duplicateRevokedCount int64
	if err := db.WithContext(ctx).Table("wechat_analytics_snapshots s").Joins("LEFT JOIN wechat_analytics_import_batches b ON b.id=s.batch_id AND b.project_id=s.project_id").Where("s.project_id = ?", project).Where("b.revoked_at IS NOT NULL").Where(duplicateCondition).Count(&duplicateRevokedCount).Error; err != nil {
		return err
	}
	report.Revoked += duplicateRevokedCount
	query = query.Where("NOT " + duplicateCondition)
	if err := query.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for _, v := range rows {
			contentID := ""
			if v.TaskID != "" {
				contentID = "task:" + v.TaskID
			} else if v.PublicationID != "" {
				contentID = "wechat_publication:" + v.PublicationID
			}
			if contentID == "" {
				report.IdentityConflicts++
				report.skip("wechat_missing_identity")
				continue
			}
			metrics := model.AnalyticsMetrics{ReadUsers: v.ReadUsers, ShareUsers: v.ShareUsers, ReadToFollowUsers: v.ReadToFollowUsers, DeliveredUsers: v.DeliveredUsers, ReadCompletionRate: decimalFromFloatPtr(v.ReadCompletionRate), DeliveryCompletionRate: decimalFromFloatPtr(v.DeliveryCompletionRate)}
			in := legacyMigrationInput{content: model.AnalyticsContent{ID: contentID, ProjectID: project, Platform: model.PlatformArticle, TaskID: v.TaskID, PublicationID: v.PublicationID}, obs: model.AnalyticsObservation{ID: "legacy:wechat_import:" + v.ID, ProjectID: project, ContentID: contentID, BatchID: v.BatchID, MetricBasis: "cumulative", StatDate: dateShanghai(v.DataAsOfAt), Source: "wechat_import", SourcePriority: 100, EffectiveAt: v.DataAsOfAt.UTC(), ReceivedAt: v.ImportedAt.UTC(), RevokedAt: legacyRevoked(v.BatchRevokedAt), AnalyticsMetrics: metrics}, raw: v.RawData}
			if err := queue(in); err != nil {
				return err
			}
			report.WechatImports++
		}
		return nil
	}).Error; err != nil {
		return err
	}
	return nil
}

func (m *AnalyticsRebuildManager) migrateWechatOfficial(ctx context.Context, project string, report *AnalyticsMigrationReport, queue func(legacyMigrationInput) error) error {
	type row struct {
		model.WechatMetricSnapshot
		ProjectID      string `gorm:"column:project_id"`
		PublicationID  string `gorm:"column:publication_id"`
		TrackingTaskID string `gorm:"column:tracking_task_id"`
	}
	db := m.repo.Analytics().DB()
	if !db.Migrator().HasTable("wechat_metric_snapshots") {
		return nil
	}
	var rows []row
	query := db.WithContext(ctx).Table("wechat_metric_snapshots s").Select("s.*, t.project_id, t.publication_id AS publication_id, t.task_id AS tracking_task_id").Joins("JOIN wechat_article_trackings t ON t.id=s.tracking_id").Where("t.project_id = ?", project)
	duplicateCondition := "EXISTS (SELECT 1 FROM analytics_observations o WHERE o.project_id = t.project_id AND o.tracking_id = s.tracking_id AND o.source = 'wechat_api' AND o.metric_basis = 'cumulative' AND o.stat_date = s.stat_date AND o.id NOT LIKE 'legacy:wechat_official:%')"
	var duplicateCount int64
	if err := db.WithContext(ctx).Table("wechat_metric_snapshots s").Joins("JOIN wechat_article_trackings t ON t.id=s.tracking_id").Where("t.project_id = ?", project).Where(duplicateCondition).Count(&duplicateCount).Error; err != nil {
		return err
	}
	report.Duplicates += duplicateCount
	report.WechatOfficial += duplicateCount
	query = query.Where("NOT " + duplicateCondition)
	if err := query.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for _, v := range rows {
			contentID := ""
			publicationID := strings.TrimSpace(v.PublicationID)
			taskID := strings.TrimSpace(v.TaskID)
			if taskID == "" {
				taskID = strings.TrimSpace(v.TrackingTaskID)
			}
			if taskID != "" {
				contentID = "task:" + taskID
			} else if publicationID != "" {
				contentID = "wechat_publication:" + publicationID
			}
			if contentID == "" {
				report.IdentityConflicts++
				report.skip("wechat_official_missing_identity")
				continue
			}
			metrics := model.AnalyticsMetrics{ReadUsers: int64ptr(v.ReadUsers), ShareUsers: int64ptr(v.ShareUsers), CollectionUsers: int64ptr(v.CollectionUsers), LikeUsers: int64ptr(v.LikeUsers), ZaikanUsers: int64ptr(v.ZaikanUsers), CommentCount: int64ptr(v.CommentCount), ReadToFollowUsers: int64ptr(v.ReadToSubscribeUsers), ReadCompletionRate: decimalFromFloat(v.ReadFinishRate), AverageReadActiveTime: decimalFromFloat(v.AverageReadActiveTime)}
			in := legacyMigrationInput{content: model.AnalyticsContent{ID: contentID, ProjectID: project, Platform: model.PlatformArticle, TaskID: taskID, PublicationID: publicationID}, obs: model.AnalyticsObservation{ID: "legacy:wechat_official:" + v.ID, TrackingID: v.TrackingID, ProjectID: project, ContentID: contentID, MetricBasis: "cumulative", StatDate: v.StatDate, Source: "wechat_api", SourcePriority: 200, EffectiveAt: v.CapturedAt.UTC(), ReceivedAt: v.CapturedAt.UTC(), AnalyticsMetrics: metrics}, raw: string(v.RawResponse)}
			if err := queue(in); err != nil {
				return err
			}
			report.WechatOfficial++
		}
		return nil
	}).Error; err != nil {
		return err
	}
	return nil
}

func (m *AnalyticsRebuildManager) migrateSeednoteImports(ctx context.Context, project string, report *AnalyticsMigrationReport, queue func(legacyMigrationInput) error) error {
	type row struct {
		model.SeednoteMetricVersion
		ProjectID      string     `gorm:"column:project_id"`
		MetricBasis    string     `gorm:"column:metric_basis"`
		BatchRevokedAt *time.Time `gorm:"column:batch_revoked_at"`
	}
	db := m.repo.Analytics().DB()
	if !db.Migrator().HasTable("seednote_metric_versions") {
		return nil
	}
	var rows []row
	query := db.WithContext(ctx).Table("seednote_metric_versions v").Select("v.*, p.project_id, b.metric_basis, b.revoked_at AS batch_revoked_at").Joins("JOIN seednote_posts p ON p.id=v.post_id").Joins("LEFT JOIN seednote_import_batches b ON b.id=v.batch_id").Where("p.project_id = ?", project)
	duplicateCondition := "EXISTS (SELECT 1 FROM analytics_observations o WHERE o.project_id = p.project_id AND o.id = " + analyticsPrefixedIDSQL(db.Dialector.Name(), "seednote_import:", "v.import_row_id") + ")"
	var duplicateCount int64
	if err := db.WithContext(ctx).Table("seednote_metric_versions v").Joins("JOIN seednote_posts p ON p.id=v.post_id").Where("p.project_id = ?", project).Where(duplicateCondition).Count(&duplicateCount).Error; err != nil {
		return err
	}
	report.Duplicates += duplicateCount
	report.SeednoteImports += duplicateCount
	var duplicateRevokedCount int64
	if err := db.WithContext(ctx).Table("seednote_metric_versions v").Joins("JOIN seednote_posts p ON p.id=v.post_id").Joins("LEFT JOIN seednote_import_batches b ON b.id=v.batch_id").Where("p.project_id = ?", project).Where("b.revoked_at IS NOT NULL").Where(duplicateCondition).Count(&duplicateRevokedCount).Error; err != nil {
		return err
	}
	report.Revoked += duplicateRevokedCount
	query = query.Where("NOT " + duplicateCondition)
	if err := query.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for _, v := range rows {
			basis := strings.ToLower(strings.TrimSpace(v.MetricBasis))
			if basis != "daily" && basis != "cumulative" {
				report.BasisUnknown++
				report.skip("seednote_basis_unknown")
				continue
			}
			contentID := "seednote_post:" + v.PostID
			metrics := model.AnalyticsMetrics{ExposureCount: v.ExposureCount, ViewCount: v.ViewCount, LikeCount: v.LikeCount, CommentCount: v.CommentCount, CollectCount: v.CollectCount, FollowerGainCount: v.FollowerGainCount, ShareCount: v.ShareCount, BarrageCount: v.BarrageCount, CoverClickRate: decimalFromFloatPtr(v.CoverClickRate), AvgWatchDuration: decimalFromFloatPtr(v.AvgWatchDuration)}
			in := legacyMigrationInput{content: model.AnalyticsContent{ID: contentID, ProjectID: project, Platform: model.PlatformSeednote, PostID: v.PostID}, obs: model.AnalyticsObservation{ID: "legacy:seednote_import:" + v.ID, ProjectID: project, ContentID: contentID, BatchID: v.BatchID, MetricBasis: basis, StatDate: dateShanghai(v.DataAsOfAt), Source: "seednote_import", SourcePriority: 200, EffectiveAt: v.DataAsOfAt.UTC(), ReceivedAt: v.ImportedAt.UTC(), RevokedAt: legacyRevoked(v.BatchRevokedAt), AnalyticsMetrics: metrics}, raw: v.RawData}
			if err := queue(in); err != nil {
				return err
			}
			report.SeednoteImports++
		}
		return nil
	}).Error; err != nil {
		return err
	}
	return nil
}

func (m *AnalyticsRebuildManager) migrateSeednoteOfficial(ctx context.Context, project string, report *AnalyticsMigrationReport, queue func(legacyMigrationInput) error) error {
	type row struct {
		model.SeednoteMetricSnapshot
		ProjectID string `gorm:"column:project_id"`
		NoteID    string `gorm:"column:note_id"`
		NoteURL   string `gorm:"column:note_url"`
		TaskID    string `gorm:"column:task_id"`
	}
	db := m.repo.Analytics().DB()
	if !db.Migrator().HasTable("seednote_metric_snapshots") {
		return nil
	}
	var rows []row
	// Tracking stores the external note identity, while imported facts use the
	// stable SeednotePost UUID. Resolve the external identity once per project
	// so both sources land in the same analytics content series.
	var posts []model.SeednotePost
	if err := db.WithContext(ctx).Where("project_id = ?", project).Order("created_at DESC, id DESC").Find(&posts).Error; err != nil {
		return err
	}
	byNote, byURL, byTask := map[string]string{}, map[string]string{}, map[string]string{}
	noteCount, urlCount, taskCount := map[string]int{}, map[string]int{}, map[string]int{}
	for _, p := range posts {
		if p.NoteID != "" {
			noteCount[p.NoteID]++
			if _, ok := byNote[p.NoteID]; !ok {
				byNote[p.NoteID] = p.ID
			}
		}
		if p.NoteURL != "" {
			urlCount[p.NoteURL]++
			if _, ok := byURL[p.NoteURL]; !ok {
				byURL[p.NoteURL] = p.ID
			}
		}
		if p.TaskID != "" {
			taskCount[p.TaskID]++
			if _, ok := byTask[p.TaskID]; !ok {
				byTask[p.TaskID] = p.ID
			}
		}
	}
	query := db.WithContext(ctx).Table("seednote_metric_snapshots s").Select("s.*, t.project_id, t.note_id, t.note_url, t.task_id").Joins("JOIN seednote_post_trackings t ON t.id=s.tracking_id").Where("t.project_id = ?", project)
	duplicateCondition := "EXISTS (SELECT 1 FROM analytics_observations o WHERE o.project_id = t.project_id AND o.tracking_id = s.tracking_id AND o.source = 'seednote_public' AND o.metric_basis = 'cumulative' AND o.stat_date = s.captured_date AND o.id NOT LIKE 'legacy:seednote_official:%')"
	var duplicateCount int64
	if err := db.WithContext(ctx).Table("seednote_metric_snapshots s").Joins("JOIN seednote_post_trackings t ON t.id=s.tracking_id").Where("t.project_id = ?", project).Where(duplicateCondition).Count(&duplicateCount).Error; err != nil {
		return err
	}
	report.Duplicates += duplicateCount
	report.SeednoteOfficial += duplicateCount
	query = query.Where("NOT " + duplicateCondition)
	if err := query.FindInBatches(&rows, 500, func(_ *gorm.DB, _ int) error {
		for _, v := range rows {
			postID, ambiguous := resolveSeednotePostIdentity(v.NoteID, v.NoteURL, v.TaskID, byNote, noteCount, byURL, urlCount, byTask, taskCount)
			if postID == "" {
				if ambiguous {
					report.IdentityConflicts++
					report.skip("seednote_ambiguous_note_identity")
				} else {
					report.IdentityConflicts++
					report.skip("seednote_missing_note_identity")
				}
				continue
			}
			contentID := "seednote_post:" + postID
			metrics := model.AnalyticsMetrics{LikeCount: int64ptr(v.LikeCount), CollectCount: int64ptr(v.CollectCount), CommentCount: int64ptr(v.CommentCount), ShareCount: int64ptr(v.ShareCount)}
			in := legacyMigrationInput{content: model.AnalyticsContent{ID: contentID, ProjectID: project, Platform: model.PlatformSeednote, PostID: postID}, obs: model.AnalyticsObservation{ID: "legacy:seednote_official:" + v.ID, TrackingID: v.TrackingID, ProjectID: project, ContentID: contentID, MetricBasis: "cumulative", StatDate: v.CapturedDate, Source: "seednote_public", SourcePriority: 50, EffectiveAt: v.CapturedAt.UTC(), ReceivedAt: v.CapturedAt.UTC(), AnalyticsMetrics: metrics}, raw: v.RawData}
			if err := queue(in); err != nil {
				return err
			}
			report.SeednoteOfficial++
		}
		return nil
	}).Error; err != nil {
		return err
	}
	return nil
}

func resolveSeednotePostIdentity(noteID, noteURL, taskID string, postsByNoteID map[string]string, noteIDCounts map[string]int, postsByURL map[string]string, urlCounts map[string]int, postsByTask map[string]string, taskCounts map[string]int) (string, bool) {
	candidates := map[string]struct{}{}
	if noteID != "" {
		if noteIDCounts[noteID] > 1 {
			return "", true
		}
		if postID := postsByNoteID[noteID]; postID != "" {
			candidates[postID] = struct{}{}
		}
	}
	if noteURL != "" {
		if urlCounts[noteURL] > 1 {
			return "", true
		}
		if postID := postsByURL[noteURL]; postID != "" {
			candidates[postID] = struct{}{}
		}
	}
	if len(candidates) > 1 {
		return "", true
	}
	for postID := range candidates {
		return postID, false
	}
	if taskID != "" && taskCounts[taskID] == 1 {
		return postsByTask[taskID], false
	}
	return "", false
}
