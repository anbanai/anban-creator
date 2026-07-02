package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
)

// ProjectStats holds computed statistics for a project.
type ProjectStats struct {
	TotalTasks     int64      `json:"total_tasks"`
	CompletedTasks int64      `json:"completed_tasks"`
	FailedTasks    int64      `json:"failed_tasks"`
	RunningTasks   int64      `json:"running_tasks"`
	PendingTasks   int64      `json:"pending_tasks"`
	SuccessRate    float64    `json:"success_rate"`
	LastActivityAt *time.Time `json:"last_activity_at"`
}

// ProjectListOptions for filtering project list queries.
type ProjectListOptions struct {
	Status   string // filter by status (active, archived)
	Platform string // filter by platform (article, seednote)
}

// ProjectRepository defines the interface for project data access.
type ProjectRepository interface {
	Create(ctx context.Context, project *model.Project) error
	FindByID(ctx context.Context, id string) (*model.Project, error)
	ListByUserID(ctx context.Context, userID string, opts ProjectListOptions) ([]*model.Project, error)
	ListActiveProjects(ctx context.Context) ([]*model.Project, error)
	FindByUserAndPlatform(ctx context.Context, userID, platform string) ([]*model.Project, error)
	Update(ctx context.Context, project *model.Project) error
	UpdateStatus(ctx context.Context, id, status string) error
	Delete(ctx context.Context, id string) error
	GetStats(ctx context.Context, projectID string) (*ProjectStats, error)
	GetStatsByProjectIDs(ctx context.Context, projectIDs []string) (map[string]*ProjectStats, error)
}

// -----------------------------------------------------------------------------
// Implementation
// -----------------------------------------------------------------------------

type gormProjectRepository struct {
	db *gorm.DB
}

func newProjectRepository(db *gorm.DB) ProjectRepository {
	return &gormProjectRepository{db: db}
}

func (r *gormProjectRepository) Create(ctx context.Context, project *model.Project) error {
	return r.db.WithContext(ctx).Create(project).Error
}

func (r *gormProjectRepository) FindByID(ctx context.Context, id string) (*model.Project, error) {
	var project model.Project
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&project).Error; err != nil {
		return nil, err
	}
	return &project, nil
}

func (r *gormProjectRepository) ListByUserID(ctx context.Context, userID string, opts ProjectListOptions) ([]*model.Project, error) {
	q := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if opts.Status != "" {
		q = q.Where("status = ?", opts.Status)
	}
	if opts.Platform != "" {
		q = q.Where("platform = ?", opts.Platform)
	}

	var projects []*model.Project
	if err := q.Order("created_at DESC").Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}

func (r *gormProjectRepository) ListActiveProjects(ctx context.Context) ([]*model.Project, error) {
	var projects []*model.Project
	err := r.db.WithContext(ctx).
		Where("status = ?", model.ProjectStatusActive).
		Find(&projects).Error
	return projects, err
}

func (r *gormProjectRepository) FindByUserAndPlatform(ctx context.Context, userID, platform string) ([]*model.Project, error) {
	var projects []*model.Project
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND platform = ? AND status = ?", userID, platform, model.ProjectStatusActive).
		Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}

func (r *gormProjectRepository) Update(ctx context.Context, project *model.Project) error {
	return r.db.WithContext(ctx).Save(project).Error
}

func (r *gormProjectRepository) UpdateStatus(ctx context.Context, id, status string) error {
	return r.db.WithContext(ctx).
		Model(&model.Project{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":     status,
			"updated_at": time.Now(),
		}).Error
}

func (r *gormProjectRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.Project{}).Error
}

func (r *gormProjectRepository) GetStats(ctx context.Context, projectID string) (*ProjectStats, error) {
	var stats struct {
		TotalTasks     int64
		CompletedTasks int64
		FailedTasks    int64
		RunningTasks   int64
		PendingTasks   int64
		LastActivityAt *time.Time
	}

	err := r.db.WithContext(ctx).Raw(`
		SELECT
			COUNT(*) as total_tasks,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed_tasks,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_tasks,
			SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END) as running_tasks,
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending_tasks,
			MAX(completed_at) as last_activity_at
		FROM tasks WHERE project_id = ?
	`, projectID).Scan(&stats).Error

	if err != nil {
		return nil, err
	}

	result := &ProjectStats{
		TotalTasks:     stats.TotalTasks,
		CompletedTasks: stats.CompletedTasks,
		FailedTasks:    stats.FailedTasks,
		RunningTasks:   stats.RunningTasks,
		PendingTasks:   stats.PendingTasks,
		LastActivityAt: stats.LastActivityAt,
	}

	if stats.TotalTasks > 0 {
		result.SuccessRate = float64(stats.CompletedTasks) / float64(stats.TotalTasks)
	}

	return result, nil
}

func (r *gormProjectRepository) GetStatsByProjectIDs(ctx context.Context, projectIDs []string) (map[string]*ProjectStats, error) {
	if len(projectIDs) == 0 {
		return map[string]*ProjectStats{}, nil
	}

	type statsRow struct {
		ProjectID      string
		TotalTasks     int64
		CompletedTasks int64
		FailedTasks    int64
		RunningTasks   int64
		PendingTasks   int64
		LastActivityAt *time.Time
	}

	var rows []statsRow
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			project_id,
			COUNT(*) as total_tasks,
			SUM(CASE WHEN status = 'completed' THEN 1 ELSE 0 END) as completed_tasks,
			SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END) as failed_tasks,
			SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END) as running_tasks,
			SUM(CASE WHEN status = 'pending' THEN 1 ELSE 0 END) as pending_tasks,
			MAX(completed_at) as last_activity_at
		FROM tasks
		WHERE project_id IN ?
		GROUP BY project_id
	`, projectIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	statsMap := make(map[string]*ProjectStats, len(projectIDs))
	for _, projectID := range projectIDs {
		statsMap[projectID] = &ProjectStats{}
	}

	for _, row := range rows {
		stats := &ProjectStats{
			TotalTasks:     row.TotalTasks,
			CompletedTasks: row.CompletedTasks,
			FailedTasks:    row.FailedTasks,
			RunningTasks:   row.RunningTasks,
			PendingTasks:   row.PendingTasks,
			LastActivityAt: row.LastActivityAt,
		}
		if row.TotalTasks > 0 {
			stats.SuccessRate = float64(row.CompletedTasks) / float64(row.TotalTasks)
		}
		statsMap[row.ProjectID] = stats
	}

	return statsMap, nil
}
