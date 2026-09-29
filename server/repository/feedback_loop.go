package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func isMissingFeedbackTable(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such table") || strings.Contains(message, "doesn't exist") || strings.Contains(message, "does not exist")
}

type feedbackLoopRepository struct{ db *gorm.DB }

func newFeedbackLoopRepository(db *gorm.DB) FeedbackLoopRepository {
	return &feedbackLoopRepository{db: db}
}

func (r *feedbackLoopRepository) CreateJob(ctx context.Context, job *model.FeedbackJob) (bool, error) {
	res := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "fingerprint"}}, DoNothing: true}).Create(job)
	if res.Error != nil {
		return false, res.Error
	}
	if res.RowsAffected == 0 {
		var existing model.FeedbackJob
		if err := r.db.WithContext(ctx).Where("fingerprint = ?", job.Fingerprint).First(&existing).Error; err != nil {
			return false, err
		}
		*job = existing
		return false, nil
	}
	return true, nil
}

func (r *feedbackLoopRepository) FindJobByFingerprint(ctx context.Context, fingerprint string) (*model.FeedbackJob, error) {
	var job model.FeedbackJob
	err := r.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).First(&job).Error
	return &job, err
}

func (r *feedbackLoopRepository) FindJobByIDOrFingerprint(ctx context.Context, identity string) (*model.FeedbackJob, error) {
	var job model.FeedbackJob
	err := r.db.WithContext(ctx).Where("id = ? OR fingerprint = ?", identity, identity).First(&job).Error
	return &job, err
}

func (r *feedbackLoopRepository) FindRunningJob(ctx context.Context, projectID, operation string) (*model.FeedbackJob, error) {
	var job model.FeedbackJob
	err := r.db.WithContext(ctx).
		Where("project_id = ? AND operation = ? AND status IN ?", projectID, operation, []string{model.FeedbackJobQueued, model.FeedbackJobRunning}).
		Order("created_at asc").First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &job, err
}

func (r *feedbackLoopRepository) ClaimQueuedJob(ctx context.Context, identity string, now time.Time) (*model.FeedbackJob, bool, error) {
	result := r.db.WithContext(ctx).Model(&model.FeedbackJob{}).
		Where("(id = ? OR fingerprint = ?) AND ((status = ?) OR (status = ? AND attempts < ?))", identity, identity, model.FeedbackJobQueued, model.FeedbackJobFailed, model.FeedbackMaxAttempts).
		Updates(map[string]any{"status": model.FeedbackJobRunning, "attempts": gorm.Expr("attempts + 1"), "started_at": now, "updated_at": now})
	if result.Error != nil {
		return nil, false, result.Error
	}
	job, err := r.FindJobByIDOrFingerprint(ctx, identity)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	return job, result.RowsAffected == 1, err
}

func (r *feedbackLoopRepository) AcquireFeedbackLease(ctx context.Context, scope, jobID string, now time.Time, ttl time.Duration) (bool, error) {
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(jobID) == "" || ttl <= 0 {
		return false, errors.New("feedback lease scope, job, and ttl are required")
	}
	won := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("scope = ? AND expires_at <= ?", scope, now).Delete(&model.FeedbackLease{}).Error; err != nil {
			return err
		}
		lease := &model.FeedbackLease{Scope: scope, JobID: jobID, AcquiredAt: now, ExpiresAt: now.Add(ttl)}
		result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "scope"}}, DoNothing: true}).Create(lease)
		if result.Error != nil {
			return result.Error
		}
		won = result.RowsAffected == 1
		return nil
	})
	return won, err
}

func (r *feedbackLoopRepository) ReleaseFeedbackLease(ctx context.Context, scope, jobID string) error {
	if strings.TrimSpace(scope) == "" || strings.TrimSpace(jobID) == "" {
		return nil
	}
	return r.db.WithContext(ctx).Where("scope = ? AND job_id = ?", scope, jobID).Delete(&model.FeedbackLease{}).Error
}

func (r *feedbackLoopRepository) UpdateJob(ctx context.Context, job *model.FeedbackJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *feedbackLoopRepository) ListJobs(ctx context.Context, projectID, cadence string, limit int) ([]*model.FeedbackJob, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var jobs []*model.FeedbackJob
	q := r.db.WithContext(ctx).Order("created_at asc").Limit(limit)
	if projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	if cadence != "" {
		q = q.Where("cadence = ?", cadence)
	}
	err := q.Find(&jobs).Error
	return jobs, err
}

func (r *feedbackLoopRepository) ListRunningByAccount(ctx context.Context, accountID, operation string, limit int) ([]*model.FeedbackJob, error) {
	if strings.TrimSpace(accountID) == "" {
		return nil, nil
	}
	return r.listRunning(ctx, "account_id", accountID, operation, limit)
}

func (r *feedbackLoopRepository) ListRunningByUser(ctx context.Context, userID string, limit int) ([]*model.FeedbackJob, error) {
	return r.listRunning(ctx, "user_id", userID, "", limit)
}

func (r *feedbackLoopRepository) ListRunningByProject(ctx context.Context, projectID string, limit int) ([]*model.FeedbackJob, error) {
	return r.listRunning(ctx, "project_id", projectID, "", limit)
}

func (r *feedbackLoopRepository) listRunning(ctx context.Context, field, value, operation string, limit int) ([]*model.FeedbackJob, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var jobs []*model.FeedbackJob
	q := r.db.WithContext(ctx).Where(field+" = ? AND status IN ?", value, []string{model.FeedbackJobQueued, model.FeedbackJobRunning}).Order("created_at asc").Limit(limit)
	if strings.TrimSpace(operation) != "" {
		q = q.Where("operation = ?", operation)
	}
	return jobs, q.Find(&jobs).Error
}

func (r *feedbackLoopRepository) CreateInsight(ctx context.Context, insight *model.FeedbackInsight) error {
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "job_id"}, {Name: "kind"}}, DoNothing: true}).Create(insight).Error
}
func (r *feedbackLoopRepository) CreateStrategy(ctx context.Context, snapshot *model.StrategySnapshot) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var previous model.StrategySnapshot
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("project_id = ? AND platform = ? AND status = ?", snapshot.ProjectID, snapshot.Platform, "active").
			Order("revision desc").First(&previous).Error; err == nil {
			previous.Status = "retired"
			now := time.Now().UTC()
			previous.RetiredAt = &now
			if err := tx.Save(&previous).Error; err != nil {
				return err
			}
			snapshot.SupersedesID = previous.ID
			if snapshot.Revision <= previous.Revision {
				snapshot.Revision = previous.Revision + 1
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if snapshot.ActivatedAt == nil {
			now := time.Now().UTC()
			snapshot.ActivatedAt = &now
		}
		return tx.Create(snapshot).Error
	})
}

func (r *feedbackLoopRepository) NextStrategyRevision(ctx context.Context, projectID, platform string) (int64, error) {
	var maxRevision int64
	if err := r.db.WithContext(ctx).Model(&model.StrategySnapshot{}).
		Where("project_id = ? AND platform = ?", projectID, platform).
		Select("COALESCE(MAX(revision), 0)").Scan(&maxRevision).Error; err != nil {
		return 0, err
	}
	return maxRevision + 1, nil
}

func (r *feedbackLoopRepository) FindActiveStrategy(ctx context.Context, projectID, platform string) (*model.StrategySnapshot, error) {
	var snapshot model.StrategySnapshot
	q := r.db.WithContext(ctx).Where("project_id = ? AND platform = ? AND status = ?", projectID, platform, "active").Order("revision desc").Limit(1)
	err := q.First(&snapshot).Error
	if isMissingFeedbackTable(err) {
		return nil, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &snapshot, err
}

func (r *feedbackLoopRepository) FindStrategyByID(ctx context.Context, id string) (*model.StrategySnapshot, error) {
	if id == "" {
		return nil, nil
	}
	var snapshot model.StrategySnapshot
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&snapshot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &snapshot, nil
}

func (r *feedbackLoopRepository) CreateGenerationContext(ctx context.Context, context *model.GenerationContext) error {
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "execution_id"}}, DoNothing: true}).Create(context).Error
	if isMissingFeedbackTable(err) {
		return nil
	}
	return err
}
func (r *feedbackLoopRepository) FindGenerationContext(ctx context.Context, executionID string) (*model.GenerationContext, error) {
	var context model.GenerationContext
	err := r.db.WithContext(ctx).Where("execution_id = ?", executionID).First(&context).Error
	if isMissingFeedbackTable(err) {
		return nil, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &context, err
}
