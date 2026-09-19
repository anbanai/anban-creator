package repository

import (
	"context"
	"errors"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type imageAnalysisRepository struct{ db *gorm.DB }

func newImageAnalysisRepository(db *gorm.DB) ImageAnalysisRepository {
	return &imageAnalysisRepository{db: db}
}

func (r *imageAnalysisRepository) Create(ctx context.Context, job *model.ImageAnalysisJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *imageAnalysisRepository) FindByID(ctx context.Context, id string) (*model.ImageAnalysisJob, error) {
	var job model.ImageAnalysisJob
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&job).Error
	return &job, err
}

func (r *imageAnalysisRepository) FindByIDForUpdate(ctx context.Context, id string) (*model.ImageAnalysisJob, error) {
	var job model.ImageAnalysisJob
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&job).Error
	return &job, err
}

func (r *imageAnalysisRepository) FindBySubject(ctx context.Context, subjectType, subjectID, kind string) (*model.ImageAnalysisJob, error) {
	var job model.ImageAnalysisJob
	err := r.db.WithContext(ctx).Where("subject_type = ? AND subject_id = ? AND kind = ?", subjectType, subjectID, kind).First(&job).Error
	return &job, err
}

func (r *imageAnalysisRepository) FindBySubjectForUpdate(ctx context.Context, subjectType, subjectID, kind string) (*model.ImageAnalysisJob, error) {
	var job model.ImageAnalysisJob
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("subject_type = ? AND subject_id = ? AND kind = ?", subjectType, subjectID, kind).First(&job).Error
	return &job, err
}

func (r *imageAnalysisRepository) Update(ctx context.Context, job *model.ImageAnalysisJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *imageAnalysisRepository) ListRecoverable(ctx context.Context, now, queuedBefore time.Time, limit int) ([]*model.ImageAnalysisJob, error) {
	var jobs []*model.ImageAnalysisJob
	q := r.db.WithContext(ctx).Where(
		"(status = ? AND (enqueued_at IS NULL OR enqueued_at < ?)) OR (status = ? AND lease_expires_at < ?)",
		model.ImageAnalysisStatusQueued, queuedBefore, model.ImageAnalysisStatusRunning, now,
	).Order("updated_at ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	return jobs, q.Find(&jobs).Error
}

func (r *imageAnalysisRepository) MarkEnqueued(ctx context.Context, id string, generation int64, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.ImageAnalysisJob{}).
		Where("id = ? AND generation = ? AND status = ?", id, generation, model.ImageAnalysisStatusQueued).
		Updates(map[string]any{"enqueued_at": at, "updated_at": at})
	return result.RowsAffected == 1, result.Error
}

func imageAnalysisNotFound(err error) bool { return errors.Is(err, gorm.ErrRecordNotFound) }
