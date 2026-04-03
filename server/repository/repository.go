package repository

import (
	"context"
	"time"

	"github.com/royalrick/anbanwriter/server/model"

	"gorm.io/gorm"
)

// Repository aggregates all sub-repositories and supports transactions.
type Repository interface {
	Users() UserRepository
	Sessions() SessionRepository
	UserConfigs() UserConfigRepository
	Plans() PlanRepository
	Tasks() TaskRepository
	TaskFiles() TaskFileRepository
	Channels() ChannelRepository
	WithTx(ctx context.Context, fn func(Repository) error) error
	Close() error
}

// UserRepository provides access to the users table.
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByPhone(ctx context.Context, phone string) (*model.User, error)
	FindByOpenID(ctx context.Context, openID string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, user *model.User) error
}

// SessionRepository provides access to the login_sessions table.
type SessionRepository interface {
	Create(ctx context.Context, session *model.LoginSession) error
	FindByToken(ctx context.Context, token string) (*model.LoginSession, error)
	FindByRefreshToken(ctx context.Context, token string) (*model.LoginSession, error)
	Update(ctx context.Context, session *model.LoginSession) error
	Delete(ctx context.Context, token string) error
	DeleteExpired(ctx context.Context) error
}

// UserConfigRepository provides access to the user_configs table.
type UserConfigRepository interface {
	FindByUserAndScope(ctx context.Context, userID, scope string) (*model.UserConfig, error)
	Upsert(ctx context.Context, config *model.UserConfig) error
	ListByUserID(ctx context.Context, userID string) ([]*model.UserConfig, error)
	Delete(ctx context.Context, id uint) error
}

// PlanRepository provides access to the plans table.
type PlanRepository interface {
	Create(ctx context.Context, plan *model.Plan) error
	FindByID(ctx context.Context, id string) (*model.Plan, error)
	FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.Plan, error)
	Update(ctx context.Context, plan *model.Plan) error
	Delete(ctx context.Context, id string) error
	ListActive(ctx context.Context) ([]*model.Plan, error)
	ListActiveByUserID(ctx context.Context, userID string) ([]*model.Plan, error)
	ListDue(ctx context.Context, now time.Time) ([]*model.Plan, error)
	CountByUserID(ctx context.Context, userID string) (int64, error)
}

// TaskRepository provides access to the tasks table.
type TaskRepository interface {
	Create(ctx context.Context, task *model.Task) error
	FindByID(ctx context.Context, id string) (*model.Task, error)
	FindByUserID(ctx context.Context, userID string, offset, limit int) ([]*model.Task, error)
	FindByUserIDAndStatus(ctx context.Context, userID, status string, offset, limit int) ([]*model.Task, error)
	FindByCreatedAtRange(ctx context.Context, from, to time.Time, offset, limit int) ([]*model.Task, error)
	FindRunning(ctx context.Context) ([]*model.Task, error)
	FindRunningByUser(ctx context.Context, userID string) ([]*model.Task, error)
	FindCompletedOlderThan(ctx context.Context, before time.Time) ([]*model.Task, error)
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error
	UpdateProgressLog(ctx context.Context, id, log string) error
	UpdateResult(ctx context.Context, id, result string) error
	Update(ctx context.Context, task *model.Task) error
	UpdateCleanedUpAt(ctx context.Context, id string, t time.Time) error
	SetStartedAt(ctx context.Context, id string) error
	SetCompletedAt(ctx context.Context, id string) error
	CountByUserID(ctx context.Context, userID string) (int64, error)
	CountByUserIDAndStatus(ctx context.Context, userID, status string) (int64, error)
}

// TaskFileRepository provides access to the task_files table.
type TaskFileRepository interface {
	Create(ctx context.Context, file *model.TaskFile) error
	FindByID(ctx context.Context, id string) (*model.TaskFile, error)
	FindByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error)
	FindByTaskIDAndRole(ctx context.Context, taskID, role string) ([]*model.TaskFile, error)
	BatchCreate(ctx context.Context, files []*model.TaskFile) error
}

// -----------------------------------------------------------------------------
// Implementation
// -----------------------------------------------------------------------------

type repository struct {
	db       *gorm.DB
	users    UserRepository
	sessions SessionRepository
	configs  UserConfigRepository
	plans    PlanRepository
	tasks    TaskRepository
	files    TaskFileRepository
	channels ChannelRepository
}

// New creates a new Repository backed by the given *gorm.DB.
func New(db *gorm.DB) Repository {
	users := newUserRepository(db)
	sessions := newSessionRepository(db)
	configs := newUserConfigRepository(db)
	plans := newPlanRepository(db)
	tasks := newTaskRepository(db)
	files := newTaskFileRepository(db)
	channels := newChannelRepository(db)

	return &repository{
		db:       db,
		users:    users,
		sessions: sessions,
		configs:  configs,
		plans:    plans,
		tasks:    tasks,
		files:    files,
		channels: channels,
	}
}

func (r *repository) Users() UserRepository        { return r.users }
func (r *repository) Sessions() SessionRepository   { return r.sessions }
func (r *repository) UserConfigs() UserConfigRepository { return r.configs }
func (r *repository) Plans() PlanRepository         { return r.plans }
func (r *repository) Tasks() TaskRepository         { return r.tasks }
func (r *repository) TaskFiles() TaskFileRepository { return r.files }
func (r *repository) Channels() ChannelRepository    { return r.channels }

// WithTx executes fn inside a database transaction. If fn returns an error the
// transaction is rolled back; otherwise it is committed. The txRepo passed to fn
// uses the same sub-repository interfaces but operates on the transaction.
func (r *repository) WithTx(ctx context.Context, fn func(Repository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := newTxRepository(tx)
		return fn(txRepo)
	})
}

// Close releases the underlying database connection pool.
func (r *repository) Close() error {
	sqlDB, err := r.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// -----------------------------------------------------------------------------
// Transaction-backed repository
// -----------------------------------------------------------------------------

type txRepository struct {
	db       *gorm.DB
	users    UserRepository
	sessions SessionRepository
	configs  UserConfigRepository
	plans    PlanRepository
	tasks    TaskRepository
	files    TaskFileRepository
	channels ChannelRepository
}

func newTxRepository(tx *gorm.DB) *txRepository {
	return &txRepository{
		db:       tx,
		users:    newUserRepository(tx),
		sessions: newSessionRepository(tx),
		configs:  newUserConfigRepository(tx),
		plans:    newPlanRepository(tx),
		tasks:    newTaskRepository(tx),
		files:    newTaskFileRepository(tx),
		channels: newChannelRepository(tx),
	}
}

func (r *txRepository) Users() UserRepository        { return r.users }
func (r *txRepository) Sessions() SessionRepository   { return r.sessions }
func (r *txRepository) UserConfigs() UserConfigRepository { return r.configs }
func (r *txRepository) Plans() PlanRepository         { return r.plans }
func (r *txRepository) Tasks() TaskRepository         { return r.tasks }
func (r *txRepository) TaskFiles() TaskFileRepository { return r.files }
func (r *txRepository) Channels() ChannelRepository    { return r.channels }

func (r *txRepository) WithTx(ctx context.Context, fn func(Repository) error) error {
	// Already in a transaction -- use a savepoint.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := newTxRepository(tx)
		return fn(txRepo)
	})
}

func (r *txRepository) Close() error { return nil }
