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
	Plans() PlanRepository
	Tasks() TaskRepository
	TaskFiles() TaskFileRepository
	Channels() ChannelRepository
	Credits() CreditRepository
	APIKeys() APIKeyRepository
	Feedbacks() FeedbackRepository
	ModelConfigs() ModelConfigRepository
	SeednoteTrackings() SeednoteTrackingRepository
	SeednoteMetricSnapshots() SeednoteMetricSnapshotRepository
	Templates() TemplateRepository
	ViralAnalyses() ViralAnalysisRepository
	PosterTasks() PosterTaskRepository
	WithTx(ctx context.Context, fn func(Repository) error) error
	Close() error
}

// UserRepository provides access to the users table.
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByOpenID(ctx context.Context, openID string) (*model.User, error)
	FindByInviteCode(ctx context.Context, code string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, user *model.User) error
	IncrementInviteCount(ctx context.Context, userID string, maxCount int) (bool, error)
	AdjustBalance(ctx context.Context, userID string, delta int) (int, error)
	DeductCredits(ctx context.Context, userID string, amount int) (int, bool, error)
}

// SessionRepository provides access to the login_sessions table.
type SessionRepository interface {
	Create(ctx context.Context, session *model.LoginSession) error
	FindByToken(ctx context.Context, token string) (*model.LoginSession, error)
	FindByRefreshToken(ctx context.Context, token string) (*model.LoginSession, error)
	Update(ctx context.Context, session *model.LoginSession) error
	Delete(ctx context.Context, token string) error
	DeleteExpired(ctx context.Context) error
	DeleteByUserID(ctx context.Context, userID string) error
}

// PlanRepository provides access to the plans table.
type PlanRepository interface {
	Create(ctx context.Context, plan *model.Plan) error
	FindByID(ctx context.Context, id string) (*model.Plan, error)
	FindByUserID(ctx context.Context, userID string, channelID string, offset, limit int) ([]*model.Plan, error)
	Update(ctx context.Context, plan *model.Plan) error
	Delete(ctx context.Context, id string) error
	ListActive(ctx context.Context) ([]*model.Plan, error)
	ListActiveByUserID(ctx context.Context, userID string, channelID string) ([]*model.Plan, error)
	ListDue(ctx context.Context, now time.Time) ([]*model.Plan, error)
	CountByUserID(ctx context.Context, userID string, channelID string) (int64, error)
}

// TaskRepository provides access to the tasks table.
type TaskRepository interface {
	Create(ctx context.Context, task *model.Task) error
	FindByID(ctx context.Context, id string) (*model.Task, error)
	FindByUserID(ctx context.Context, userID string, channelID string, offset, limit int) ([]*model.Task, error)
	FindByUserIDAndStatus(ctx context.Context, userID, status string, channelID string, offset, limit int) ([]*model.Task, error)
	FindByCreatedAtRange(ctx context.Context, from, to time.Time, offset, limit int) ([]*model.Task, error)
	FindByUserIDAndCreatedAtRange(ctx context.Context, userID string, from, to time.Time, offset, limit int) ([]*model.Task, error)
	FindRunning(ctx context.Context) ([]*model.Task, error)
	FindRunningByUser(ctx context.Context, userID string, channelID string) ([]*model.Task, error)
	FindCompletedOlderThan(ctx context.Context, before time.Time) ([]*model.Task, error)
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error
	UpdateProgressLog(ctx context.Context, id, log string) error
	AppendProgressLog(ctx context.Context, id, message string) error
	UpdateResult(ctx context.Context, id, result string) error
	Update(ctx context.Context, task *model.Task) error
	UpdateTitle(ctx context.Context, id string, title string) error
	UpdateCleanedUpAt(ctx context.Context, id string, t time.Time) error
	SetStartedAt(ctx context.Context, id string) error
	SetCompletedAt(ctx context.Context, id string) error
	UpdateHeartbeat(ctx context.Context, id string) error
	CountByUserID(ctx context.Context, userID string, channelID string) (int64, error)
	CountByUserIDAndStatus(ctx context.Context, userID, status string, channelID string) (int64, error)
	CountRunningByChannel(ctx context.Context, channelID string) (int64, error)
	FindPendingByChannel(ctx context.Context, channelID string, limit int) ([]*model.Task, error)
	CompareAndSwapStatus(ctx context.Context, taskID, expected, newStatus string) (bool, error)
	CompareAndSwapStatusAndStartedAt(ctx context.Context, taskID, expected, newStatus string) (bool, error)
	CompareAndSwapStatusAndError(ctx context.Context, taskID, expected, newStatus, errorMsg string) (bool, error)
	IncrementRetryAndSetPending(ctx context.Context, taskID string, field string) error
	FindTopicsByChannelID(ctx context.Context, channelID string) ([]string, error)
	SetPublished(ctx context.Context, id string, published bool) error
	UpdateWorkflowStatus(ctx context.Context, id string, workflowStatus string) error
	Delete(ctx context.Context, id string) error
	UpdateTokenUsage(ctx context.Context, id string, inputTokens, outputTokens, cacheReadTokens, cacheCreationTokens int64, costUSD float64) error
	AggregateUsageByUser(ctx context.Context, userID string, from, to time.Time, channelID string) (totalTasks int64, totalInput, totalOutput, totalCacheRead, totalCacheCreation int64, totalCost float64, err error)
	AggregateUsageByType(ctx context.Context, userID string, from, to time.Time, channelID string) ([]TypeUsageRow, error)
}

// TaskFileRepository provides access to the task_files table.
type TaskFileRepository interface {
	Create(ctx context.Context, file *model.TaskFile) error
	Upsert(ctx context.Context, file *model.TaskFile) (*model.TaskFile, error)
	FindExisting(ctx context.Context, taskID, filePath string) (*model.TaskFile, error)
	FindByID(ctx context.Context, id string) (*model.TaskFile, error)
	FindByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error)
	FindByTaskIDAndRole(ctx context.Context, taskID, role string) ([]*model.TaskFile, error)
	FindByTaskIDAndContentHash(ctx context.Context, taskID, contentHash string) (*model.TaskFile, error)
	BatchCreate(ctx context.Context, files []*model.TaskFile) error
	DeleteByTaskID(ctx context.Context, taskID string) error
	ExistsByTaskIDAndID(ctx context.Context, taskID, fileID string) (bool, error)
}

// FeedbackRepository provides access to the feedbacks table.
type FeedbackRepository interface {
	Create(ctx context.Context, feedback *model.Feedback) error
}

// SeednoteTrackingRepository provides access to Seednote post tracking records.
type SeednoteTrackingRepository interface {
	Create(ctx context.Context, tracking *model.SeednotePostTracking) error
	FindByTaskID(ctx context.Context, taskID string) (*model.SeednotePostTracking, error)
	FindByID(ctx context.Context, id string) (*model.SeednotePostTracking, error)
	FindDue(ctx context.Context, now time.Time, limit int) ([]*model.SeednotePostTracking, error)
	Update(ctx context.Context, tracking *model.SeednotePostTracking) error
	UpdateStatus(ctx context.Context, id, status string) error
}

// SeednoteMetricSnapshotRepository provides access to Seednote metric snapshots.
type SeednoteMetricSnapshotRepository interface {
	Create(ctx context.Context, snapshot *model.SeednoteMetricSnapshot) error
	UpsertByTrackingAndDate(ctx context.Context, snapshot *model.SeednoteMetricSnapshot) error
	FindByTaskID(ctx context.Context, taskID string) ([]*model.SeednoteMetricSnapshot, error)
	FindLatestByTrackingID(ctx context.Context, trackingID string) (*model.SeednoteMetricSnapshot, error)
	FindPreviousByTrackingID(ctx context.Context, trackingID string, capturedAt time.Time) (*model.SeednoteMetricSnapshot, error)
}

// -----------------------------------------------------------------------------
// Implementation
// -----------------------------------------------------------------------------

type repository struct {
	db                      *gorm.DB
	users                   UserRepository
	sessions                SessionRepository
	plans                   PlanRepository
	tasks                   TaskRepository
	files                   TaskFileRepository
	channels                ChannelRepository
	credits                 CreditRepository
	apiKeys                 APIKeyRepository
	feedbacks               FeedbackRepository
	modelConfigs            ModelConfigRepository
	seednoteTrackings       SeednoteTrackingRepository
	seednoteMetricSnapshots SeednoteMetricSnapshotRepository
	templates               TemplateRepository
	viralAnalyses           ViralAnalysisRepository
	posterTasks             PosterTaskRepository
}

// New creates a new Repository backed by the given *gorm.DB.
func New(db *gorm.DB) Repository {
	users := newUserRepository(db)
	sessions := newSessionRepository(db)
	plans := newPlanRepository(db)
	tasks := newTaskRepository(db)
	files := newTaskFileRepository(db)
	channels := newChannelRepository(db)
	credits := newCreditRepository(db)
	apiKeys := newAPIKeyRepository(db)
	feedbacks := newFeedbackRepository(db)
	modelConfigs := newModelConfigRepository(db)
	seednoteTrackings := newSeednoteTrackingRepository(db)
	seednoteMetricSnapshots := newSeednoteMetricSnapshotRepository(db)
	templates := newTemplateRepository(db)
	viralAnalyses := newViralAnalysisRepository(db)
	posterTasks := newPosterTaskRepository(db)

	return &repository{
		db:                      db,
		users:                   users,
		sessions:                sessions,
		plans:                   plans,
		tasks:                   tasks,
		files:                   files,
		channels:                channels,
		credits:                 credits,
		apiKeys:                 apiKeys,
		feedbacks:               feedbacks,
		modelConfigs:            modelConfigs,
		seednoteTrackings:       seednoteTrackings,
		seednoteMetricSnapshots: seednoteMetricSnapshots,
		templates:               templates,
		viralAnalyses:           viralAnalyses,
		posterTasks:             posterTasks,
	}
}

func (r *repository) Users() UserRepository                         { return r.users }
func (r *repository) Sessions() SessionRepository                   { return r.sessions }
func (r *repository) Plans() PlanRepository                         { return r.plans }
func (r *repository) Tasks() TaskRepository                         { return r.tasks }
func (r *repository) TaskFiles() TaskFileRepository                 { return r.files }
func (r *repository) Channels() ChannelRepository                   { return r.channels }
func (r *repository) Credits() CreditRepository                     { return r.credits }
func (r *repository) APIKeys() APIKeyRepository                     { return r.apiKeys }
func (r *repository) Feedbacks() FeedbackRepository                 { return r.feedbacks }
func (r *repository) ModelConfigs() ModelConfigRepository           { return r.modelConfigs }
func (r *repository) SeednoteTrackings() SeednoteTrackingRepository { return r.seednoteTrackings }
func (r *repository) SeednoteMetricSnapshots() SeednoteMetricSnapshotRepository {
	return r.seednoteMetricSnapshots
}
func (r *repository) Templates() TemplateRepository          { return r.templates }
func (r *repository) ViralAnalyses() ViralAnalysisRepository { return r.viralAnalyses }
func (r *repository) PosterTasks() PosterTaskRepository      { return r.posterTasks }

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
	db                      *gorm.DB
	users                   UserRepository
	sessions                SessionRepository
	plans                   PlanRepository
	tasks                   TaskRepository
	files                   TaskFileRepository
	channels                ChannelRepository
	credits                 CreditRepository
	apiKeys                 APIKeyRepository
	feedbacks               FeedbackRepository
	modelConfigs            ModelConfigRepository
	seednoteTrackings       SeednoteTrackingRepository
	seednoteMetricSnapshots SeednoteMetricSnapshotRepository
	templates               TemplateRepository
	viralAnalyses           ViralAnalysisRepository
	posterTasks             PosterTaskRepository
}

func newTxRepository(tx *gorm.DB) *txRepository {
	return &txRepository{
		db:                      tx,
		users:                   newUserRepository(tx),
		sessions:                newSessionRepository(tx),
		plans:                   newPlanRepository(tx),
		tasks:                   newTaskRepository(tx),
		files:                   newTaskFileRepository(tx),
		channels:                newChannelRepository(tx),
		credits:                 newCreditRepository(tx),
		apiKeys:                 newAPIKeyRepository(tx),
		feedbacks:               newFeedbackRepository(tx),
		modelConfigs:            newModelConfigRepository(tx),
		seednoteTrackings:       newSeednoteTrackingRepository(tx),
		seednoteMetricSnapshots: newSeednoteMetricSnapshotRepository(tx),
		templates:               newTemplateRepository(tx),
		viralAnalyses:           newViralAnalysisRepository(tx),
		posterTasks:             newPosterTaskRepository(tx),
	}
}

func (r *txRepository) Users() UserRepository                         { return r.users }
func (r *txRepository) Sessions() SessionRepository                   { return r.sessions }
func (r *txRepository) Plans() PlanRepository                         { return r.plans }
func (r *txRepository) Tasks() TaskRepository                         { return r.tasks }
func (r *txRepository) TaskFiles() TaskFileRepository                 { return r.files }
func (r *txRepository) Channels() ChannelRepository                   { return r.channels }
func (r *txRepository) Credits() CreditRepository                     { return r.credits }
func (r *txRepository) APIKeys() APIKeyRepository                     { return r.apiKeys }
func (r *txRepository) Feedbacks() FeedbackRepository                 { return r.feedbacks }
func (r *txRepository) ModelConfigs() ModelConfigRepository           { return r.modelConfigs }
func (r *txRepository) SeednoteTrackings() SeednoteTrackingRepository { return r.seednoteTrackings }
func (r *txRepository) SeednoteMetricSnapshots() SeednoteMetricSnapshotRepository {
	return r.seednoteMetricSnapshots
}
func (r *txRepository) Templates() TemplateRepository          { return r.templates }
func (r *txRepository) ViralAnalyses() ViralAnalysisRepository { return r.viralAnalyses }
func (r *txRepository) PosterTasks() PosterTaskRepository      { return r.posterTasks }

func (r *txRepository) WithTx(ctx context.Context, fn func(Repository) error) error {
	// Already in a transaction -- use a savepoint.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := newTxRepository(tx)
		return fn(txRepo)
	})
}

func (r *txRepository) Close() error { return nil }
