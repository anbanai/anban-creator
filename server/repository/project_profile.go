package repository

import (
	"context"

	"github.com/anbanai/anban-creator/server/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ProjectProfileRevisionRepository stores immutable profile history.
type ProjectProfileRevisionRepository interface {
	Create(ctx context.Context, revision *model.ProjectProfileRevision) error
	ListByProject(ctx context.Context, projectID string) ([]*model.ProjectProfileRevision, error)
	FindByProjectRevision(ctx context.Context, projectID string, revision int64) (*model.ProjectProfileRevision, error)
}

type ProjectProfileStateRepository interface {
	Create(ctx context.Context, state *model.ProjectProfileState) error
	FindByProjectID(ctx context.Context, projectID string) (*model.ProjectProfileState, error)
	FindByProjectIDForUpdate(ctx context.Context, projectID string) (*model.ProjectProfileState, error)
	Save(ctx context.Context, state *model.ProjectProfileState) error
}

type projectProfileStateRepository struct{ db *gorm.DB }

func newProjectProfileStateRepository(db *gorm.DB) ProjectProfileStateRepository {
	return &projectProfileStateRepository{db: db}
}

func (r *projectProfileStateRepository) Create(ctx context.Context, state *model.ProjectProfileState) error {
	return r.db.WithContext(ctx).Create(state).Error
}

func (r *projectProfileStateRepository) FindByProjectID(ctx context.Context, projectID string) (*model.ProjectProfileState, error) {
	var state model.ProjectProfileState
	if err := r.db.WithContext(ctx).Where("project_id = ?", projectID).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *projectProfileStateRepository) FindByProjectIDForUpdate(ctx context.Context, projectID string) (*model.ProjectProfileState, error) {
	var state model.ProjectProfileState
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ?", projectID).First(&state).Error; err != nil {
		return nil, err
	}
	return &state, nil
}

func (r *projectProfileStateRepository) Save(ctx context.Context, state *model.ProjectProfileState) error {
	return r.db.WithContext(ctx).Save(state).Error
}

type projectProfileRevisionRepository struct{ db *gorm.DB }

func newProjectProfileRevisionRepository(db *gorm.DB) ProjectProfileRevisionRepository {
	return &projectProfileRevisionRepository{db: db}
}

func (r *projectProfileRevisionRepository) Create(ctx context.Context, revision *model.ProjectProfileRevision) error {
	return r.db.WithContext(ctx).Create(revision).Error
}

func (r *projectProfileRevisionRepository) ListByProject(ctx context.Context, projectID string) ([]*model.ProjectProfileRevision, error) {
	var rows []*model.ProjectProfileRevision
	err := r.db.WithContext(ctx).Where("project_id = ?", projectID).Order("revision DESC").Find(&rows).Error
	return rows, err
}

func (r *projectProfileRevisionRepository) FindByProjectRevision(ctx context.Context, projectID string, revision int64) (*model.ProjectProfileRevision, error) {
	var row model.ProjectProfileRevision
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("project_id = ? AND revision = ?", projectID, revision).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}
