package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type contentMetadataRepository struct{ db *gorm.DB }

func newContentMetadataRepository(db *gorm.DB) ContentMetadataRepository {
	return &contentMetadataRepository{db: db}
}

func (r *contentMetadataRepository) CreateOrUpdate(ctx context.Context, report *model.ContentMetadataReport) error {
	db := r.db.WithContext(ctx)
	if err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "task_id"}, {Name: "execution_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "tagging_status", "feedback_status", "taxonomy_version", "source_digest", "raw_metadata", "error_message", "attempts", "updated_at"}),
	}).Create(report).Error; err != nil {
		return err
	}
	return db.Where("task_id = ? AND execution_id = ?", report.TaskID, report.ExecutionID).First(report).Error
}

func (r *contentMetadataRepository) ListVocabulary(ctx context.Context, dimension, taxonomyVersion string) ([]*model.ContentTagVocabulary, error) {
	var values []*model.ContentTagVocabulary
	db := r.db.WithContext(ctx).Where("dimension = ?", dimension)
	if taxonomyVersion != "" {
		db = db.Where("taxonomy_version = ?", taxonomyVersion)
	}
	if err := db.Order("status ASC, value ASC").Find(&values).Error; err != nil {
		return nil, err
	}
	return values, nil
}

func (r *contentMetadataRepository) UpsertVocabulary(ctx context.Context, vocabulary *model.ContentTagVocabulary) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "dimension"}, {Name: "value"}, {Name: "taxonomy_version"}},
		DoUpdates: clause.AssignmentColumns([]string{"display_name", "status", "aliases", "taxonomy_version", "updated_at"}),
	}).Create(vocabulary).Error
}

func (r *contentMetadataRepository) FindByTaskExecution(ctx context.Context, taskID, executionID string) (*model.ContentMetadataReport, error) {
	var report model.ContentMetadataReport
	if err := r.db.WithContext(ctx).Where("task_id = ? AND execution_id = ?", taskID, executionID).First(&report).Error; err != nil {
		return nil, err
	}
	return &report, nil
}

func (r *contentMetadataRepository) ListTagsByTaskIDs(ctx context.Context, taskIDs []string) (map[string][]*model.ContentTagAssignment, error) {
	result := make(map[string][]*model.ContentTagAssignment)
	if len(taskIDs) == 0 {
		return result, nil
	}
	if r.db.Migrator().HasTable((&model.ContentMetadataReport{}).TableName()) && r.db.Migrator().HasTable((&model.ContentTagAssignment{}).TableName()) {
		var reports []model.ContentMetadataReport
		if err := r.db.WithContext(ctx).Where("task_id IN ?", taskIDs).Order("updated_at DESC, created_at DESC, id DESC").Find(&reports).Error; err != nil {
			return nil, err
		}
		latest := make(map[string]model.ContentMetadataReport, len(reports))
		for _, report := range reports {
			if _, exists := latest[report.TaskID]; exists {
				continue
			}
			// A failed or still-pending retry must not hide the last completed
			// tagging result from analytics readers.
			if report.TaggingStatus == model.ContentMetadataSucceeded || (report.TaggingStatus == "" && report.Status == model.ContentMetadataSucceeded) {
				latest[report.TaskID] = report
			}
		}
		if len(latest) > 0 {
			reportIDs := make([]string, 0, len(latest))
			taskByReport := make(map[string]string, len(latest))
			for taskID, report := range latest {
				reportIDs = append(reportIDs, report.ID)
				taskByReport[report.ID] = taskID
			}
			var assignments []*model.ContentTagAssignment
			if err := r.db.WithContext(ctx).Where("report_id IN ?", reportIDs).Order("dimension ASC, display_name ASC, id ASC").Find(&assignments).Error; err != nil {
				return nil, err
			}
			for _, assignment := range assignments {
				if taskID, ok := taskByReport[assignment.ReportID]; ok {
					result[taskID] = append(result[taskID], assignment)
				}
			}
		}
	}
	// Content origin is a server-owned fact. Keep the derived label visible for
	// historical content whose metadata hook ran before tagging was introduced.
	if r.db.Migrator().HasTable((&model.Task{}).TableName()) && r.db.Migrator().HasColumn((&model.Task{}).TableName(), "content_origin") {
		var tasks []model.Task
		if err := r.db.WithContext(ctx).Select("id, content_origin").Where("id IN ?", taskIDs).Find(&tasks).Error; err != nil {
			return nil, err
		}
		for _, task := range tasks {
			if task.ContentOrigin != model.ContentOriginHotSearch {
				continue
			}
			originTag := &model.ContentTagAssignment{
				Dimension: "source_relation", CanonicalValue: "hot_search", DisplayName: "热搜",
				LabelStatus: model.ContentTagCanonical, Confidence: 1, Primary: true,
			}
			filtered := make([]*model.ContentTagAssignment, 0, len(result[task.ID])+1)
			filtered = append(filtered, originTag)
			for _, tag := range result[task.ID] {
				if tag == nil || tag.Dimension != "source_relation" {
					filtered = append(filtered, tag)
				}
			}
			result[task.ID] = filtered
		}
	}
	return result, nil
}

func (r *contentMetadataRepository) ReplaceTags(ctx context.Context, reportID string, tags []*model.ContentTagAssignment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("report_id = ?", reportID).Delete(&model.ContentTagAssignment{}).Error; err != nil {
			return err
		}
		if len(tags) == 0 {
			return nil
		}
		return tx.Create(&tags).Error
	})
}
