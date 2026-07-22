package repository

import (
	"context"
	"time"

	"github.com/anbanai/anban-creator/server/model"

	"gorm.io/gorm"
)

// Repository aggregates all sub-repositories and supports transactions.
type Repository interface {
	Users() UserRepository
	Sessions() SessionRepository
	Plans() PlanRepository
	Tasks() TaskRepository
	TaskExecutions() TaskExecutionRepository
	TaskFiles() TaskFileRepository
	UploadSessions() UploadSessionRepository
	Assets() AssetRepository
	Projects() ProjectRepository
	APIKeys() APIKeyRepository
	Feedbacks() FeedbackRepository
	ModelConfigs() ModelConfigRepository
	SeednoteTrackings() SeednoteTrackingRepository
	SeednoteMetricSnapshots() SeednoteMetricSnapshotRepository
	Templates() TemplateRepository
	ViralAnalyses() ViralAnalysisRepository
	PosterTasks() PosterTaskRepository
	TopicPools() TopicPoolRepository
	ImageGenerations() ImageGenerationRepository
	AgentFeedbacks() AgentFeedbackRepository
	IlinkBindings() IlinkBindingRepository
	IlinkNotifications() IlinkNotificationRepository
	Billing() BillingRepository
	WithTx(ctx context.Context, fn func(Repository) error) error
	Close() error
}

// UserRepository provides access to the users table.
type UserRepository interface {
	FindByID(ctx context.Context, id string) (*model.User, error)
	LockByID(ctx context.Context, id string) (*model.User, error)
	FindByEmail(ctx context.Context, email string) (*model.User, error)
	FindByOpenID(ctx context.Context, openID string) (*model.User, error)
	FindByInviteCode(ctx context.Context, code string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, user *model.User) error
	IncrementInviteCount(ctx context.Context, userID string, maxCount int) (bool, error)
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
	FindByUserID(ctx context.Context, userID string, projectID string, offset, limit int) ([]*model.Plan, error)
	Update(ctx context.Context, plan *model.Plan) error
	UpdateEditable(ctx context.Context, plan *model.Plan, scheduleChanged bool) error
	UpdateEditableIfReferenceImageAssetID(ctx context.Context, plan *model.Plan, expectedID string, scheduleChanged bool) (bool, error)
	UpdateStatusAndNextRunAt(ctx context.Context, id, status string, nextRunAt *time.Time) error
	UpdateNextRunAtIf(ctx context.Context, id string, nextRunAt, expectedNextRunAt *time.Time) (bool, error)
	Delete(ctx context.Context, id string) error
	ListActive(ctx context.Context) ([]*model.Plan, error)
	ListActiveByUserID(ctx context.Context, userID string, projectID string) ([]*model.Plan, error)
	ListDue(ctx context.Context, now time.Time) ([]*model.Plan, error)
	CountByUserID(ctx context.Context, userID string, projectID string) (int64, error)
}

// TaskRepository provides access to the tasks table.
type TaskRepository interface {
	Create(ctx context.Context, task *model.Task) error
	FindByID(ctx context.Context, id string) (*model.Task, error)
	FindByUserID(ctx context.Context, userID string, projectID string, planID string, offset, limit int) ([]*model.Task, error)
	FindByUserIDAndStatus(ctx context.Context, userID, status string, projectID string, planID string, offset, limit int) ([]*model.Task, error)
	FindByCreatedAtRange(ctx context.Context, from, to time.Time, offset, limit int) ([]*model.Task, error)
	FindByUserIDAndCreatedAtRange(ctx context.Context, userID string, from, to time.Time, offset, limit int) ([]*model.Task, error)
	FindRunning(ctx context.Context) ([]*model.Task, error)
	FindRunningByUser(ctx context.Context, userID string, projectID string) ([]*model.Task, error)
	UpdateStatus(ctx context.Context, id, status string) error
	UpdateStatusAndError(ctx context.Context, id, status, errorMsg string) error
	UpdateProgressLog(ctx context.Context, id, log string) error
	AppendProgressLog(ctx context.Context, id, message string) error
	UpdateProgressColumn(ctx context.Context, id string, percent int) error
	// UpdateLatestProgress writes the latest structured progress payload to the
	// dedicated latest_progress JSON column. Called by TaskService.UpdateProgress
	// so Studio can render the current stage without parsing progress_log.
	UpdateLatestProgress(ctx context.Context, id string, payload model.ProgressPayload) error
	// GetTypeAndProgress loads only the type and progress columns for a task.
	// Used on hot paths (e.g. UpdateProgress) where loading the full row —
	// including the longtext progress_log — would be wasteful.
	GetTypeAndProgress(ctx context.Context, id string) (taskType string, progress int, err error)
	UpdateExecutionEvidence(ctx context.Context, id, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error)
	UpdateExecutionEvidenceForExecution(ctx context.Context, id, executionID, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error)
	FinalizeLocalTask(ctx context.Context, id, executionID, status, errorMsg, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error)
	FinalizeLocalTaskInTx(ctx context.Context, id, executionID, status, errorMsg, result string, usage []model.ModelTokenUsage, costStatus string) (bool, error)
	FinalizeTaskForExecution(ctx context.Context, id, executionID, status, errorMsg string) (bool, error)
	UpdateBillingTerminalReason(ctx context.Context, id, reason string) error
	Update(ctx context.Context, task *model.Task) error
	UpdateInputAttachments(ctx context.Context, id string, attachments []model.EntryAttachment) error
	UpdateTitle(ctx context.Context, id string, title string) error
	SetStartedAt(ctx context.Context, id string) error
	SetCompletedAt(ctx context.Context, id string) error
	UpdateHeartbeat(ctx context.Context, id string) error
	CountByUserID(ctx context.Context, userID string, projectID string, planID string) (int64, error)
	CountByUserIDAndStatus(ctx context.Context, userID, status string, projectID string, planID string) (int64, error)
	CountRunningByProject(ctx context.Context, projectID string) (int64, error)
	FindPendingByProject(ctx context.Context, projectID string, limit int) ([]*model.Task, error)
	// ClaimNextLocalTask atomically claims the oldest pending local-target task
	// owned by userID: CAS status pending→running, set execution_target=
	// local_claimed + executor_info + started_at, all inside one transaction so
	// concurrent claimers cannot double-claim. Returns the claimed task, or
	// (nil, nil) when no task is claimable (none pending, none local-target, or
	// deadline already expired). executorInfo is an opaque JSON blob.
	ClaimNextLocalTask(ctx context.Context, userID string, executorInfo []byte) (*model.Task, error)
	// FindExpiredLocalTasks returns IDs of pending local-target tasks whose
	// claim deadline has passed — candidates for cloud fallback. A nil/zero
	// deadline never expires (treated as unclaimed indefinitely, which should
	// not happen since creation always sets a deadline).
	FindExpiredLocalTasks(ctx context.Context, now time.Time) ([]string, error)
	// ResetLocalTarget atomically clears the local-execution markers
	// (execution_target back to cloud, deadline cleared) ONLY while the task is
	// still pending + local-target — a guarded CAS. Returns reset=true when the
	// CAS matched and the task is now eligible for cloud dispatch; reset=false
	// when the task was claimed (running+local_claimed) or otherwise changed
	// since the fallback selected it. In the false case the caller MUST NOT
	// re-enqueue, or the task would run twice (cloud + the desktop that just
	// claimed it). Used by the fallback worker before re-enqueueing an
	// unclaimed local task.
	ResetLocalTarget(ctx context.Context, taskID string) (bool, error)
	CompareAndSwapStatus(ctx context.Context, taskID, expected, newStatus string) (bool, error)
	CompareAndSwapStatusForUser(ctx context.Context, taskID, userID, expected, newStatus string) (bool, error)
	CompareAndSwapStatusAndStartedAt(ctx context.Context, taskID, expected, newStatus string) (bool, error)
	CompareAndSwapStatusAndError(ctx context.Context, taskID, expected, newStatus, errorMsg string) (bool, error)
	SetCurrentExecution(ctx context.Context, taskID, executionID string) (bool, error)
	FailRunningTask(ctx context.Context, taskID, errorMsg string) (bool, error)
	FailPendingTask(ctx context.Context, taskID, errorMsg string) (bool, error)
	ResetTerminalTaskForResume(ctx context.Context, taskID string, attachments []model.EntryAttachment) (bool, error)
	FindTitlesByProjectID(ctx context.Context, projectID string) ([]string, error)
	FindTitleTasksByProjectID(ctx context.Context, projectID string) ([]*model.Task, error)
	ClearTitles(ctx context.Context, titles []string) (int64, error)
	SetPublished(ctx context.Context, id string, published bool) error
	// UpdatePublishApproval writes the publish-approval state and the frozen
	// pending-draft-articles blob for a task (Batch 4A). Used by holdPublishForApproval
	// to enter the pending state. pendingArticles may be nil only if state is non-pending.
	UpdatePublishApproval(ctx context.Context, id, state string, pendingArticles []byte) error
	// CompareAndSwapPublishApproval atomically transitions publish_approval_state
	// from expected to newState, clearing the frozen pending-draft-articles blob
	// in the same update when clearArticles is true (spec: approve/reject clear
	// the blob). Returns true only if the task was in the expected state and is
	// now newState — the single atomic winner among concurrent approve/reject
	// calls. Used by ApprovePublish/RejectPublish to prevent a double-publish
	// race: only the goroutine whose CAS wins actually publishes.
	CompareAndSwapPublishApproval(ctx context.Context, id, expected, newState string, clearArticles bool) (bool, error)
	UpdateWorkflowStatus(ctx context.Context, id string, workflowStatus string) error
	Delete(ctx context.Context, id string) error
	AggregateUsageByUser(ctx context.Context, userID string, from, to time.Time, projectID string) (totalTasks int64, totalInput, totalOutput, totalCacheRead, totalCacheCreation int64, err error)
	AggregateUsageByType(ctx context.Context, userID string, from, to time.Time, projectID string) ([]TypeUsageRow, error)
}

// TaskFileRepository provides access to the task_files table.
type TaskFileRepository interface {
	Create(ctx context.Context, file *model.TaskFile) error
	Upsert(ctx context.Context, file *model.TaskFile) (*model.TaskFile, error)
	FindExisting(ctx context.Context, taskID, filePath string) (*model.TaskFile, error)
	FindByID(ctx context.Context, id string) (*model.TaskFile, error)
	FindByIDForExecution(ctx context.Context, id, taskID, executionID string) (*model.TaskFile, error)
	FindByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error)
	FindCollectedByTaskID(ctx context.Context, taskID string) ([]*model.TaskFile, error)
	FindByExecutionID(ctx context.Context, executionID string) ([]*model.TaskFile, error)
	FindByTaskIDAndRole(ctx context.Context, taskID, role string) ([]*model.TaskFile, error)
	FindByTaskIDAndContentHash(ctx context.Context, taskID, contentHash string) (*model.TaskFile, error)
	BatchCreate(ctx context.Context, files []*model.TaskFile) error
	DeleteByTaskID(ctx context.Context, taskID string) error
	ExistsByTaskIDAndID(ctx context.Context, taskID, fileID string) (bool, error)
	PublishCurrentExecution(ctx context.Context, taskID, executionID string) error
	CollectCurrentExecution(ctx context.Context, taskID, executionID string) error
	DiscardCurrentExecution(ctx context.Context, taskID, executionID string) error
	UpsertPendingCurrentExecution(ctx context.Context, taskID, executionID string, file *model.TaskFile) (*model.TaskFile, error)
	UpdatePendingCurrentExecutionMetadata(ctx context.Context, original *model.TaskFile, role, mediaID, wechatURL string) (*model.TaskFile, error)
	ReplacePendingCurrentExecution(ctx context.Context, taskID, executionID string, files []*model.TaskFile) error
	ReplacePendingCurrentExecutionPreservingMCPArtifacts(ctx context.Context, taskID, executionID string, files []*model.TaskFile) error
}

// TaskExecutionRepository provides durable execution-attempt persistence.
type TaskExecutionRepository interface {
	Create(ctx context.Context, execution *model.TaskExecution) error
	NextAttempt(ctx context.Context, taskID string) (int, error)
	ClaimDispatch(ctx context.Context, id, token string, leaseDuration time.Duration) (bool, error)
	AbandonDispatch(ctx context.Context, id, token string) (bool, error)
	CompleteDispatch(ctx context.Context, id, token, namespace, jobName string) (bool, error)
	FailDispatch(ctx context.Context, id, token, reason string, diagnostics, result []byte) (bool, error)
	FindByID(ctx context.Context, id string) (*model.TaskExecution, error)
	FindCurrentByTaskID(ctx context.Context, taskID string) (*model.TaskExecution, error)
	FindReconcilable(ctx context.Context, before time.Time, limit int) ([]*model.TaskExecution, error)
	SetRuntimeIdentity(ctx context.Context, id, namespace, jobName, podUID string) error
	UpdateHeartbeat(ctx context.Context, id string, now time.Time) error
	Transition(ctx context.Context, id string, from []string, to string, change model.ExecutionTransition) (bool, error)
	ClaimFinalization(ctx context.Context, id, token string, lease time.Duration) (bool, error)
	AdvanceFinalization(ctx context.Context, id, token, from, to string) (bool, error)
	RenewFinalizationClaim(ctx context.Context, id, token string) (bool, error)
	ReleaseFinalization(ctx context.Context, id, token string) error
	TransitionPublishing(ctx context.Context, id, from, to string, result []byte) (bool, error)
	ClaimCleanup(ctx context.Context, id, token string, lease time.Duration) (bool, error)
	CompleteCleanup(ctx context.Context, id, token string) (bool, error)
	FailCleanup(ctx context.Context, id, token string, backoff time.Duration) (bool, error)
	ReleaseCleanup(ctx context.Context, id, token string) error
}

// UploadSessionRepository tracks browser-direct uploads until finalization or expiration.
type UploadSessionRepository interface {
	Create(ctx context.Context, session *model.UploadSession) error
	FindByID(ctx context.Context, id string) (*model.UploadSession, error)
	ClaimFinalization(ctx context.Context, id, token string, claimedAt, claimStaleBefore time.Time) (bool, error)
	RecordPromotionSourceETag(ctx context.Context, id, token, etag string) (bool, error)
	RecordFinalizationETag(ctx context.Context, id, token, etag string) (bool, error)
	ClaimFinalizationRecovery(ctx context.Context, id, token string, claimedAt, claimStaleBefore time.Time) (bool, error)
	CompleteFinalization(ctx context.Context, id, token, assetID string, finalizedAt time.Time) (bool, error)
	ReleaseFinalization(ctx context.Context, id, token string) (bool, error)
	FindForCleanup(ctx context.Context, expiredBefore, claimStaleBefore time.Time, limit int) ([]*model.UploadSession, error)
	ClaimExpiration(ctx context.Context, id, claimID string, claimedAt, claimStaleBefore time.Time) (bool, error)
	CompleteExpiration(ctx context.Context, id, claimID string, expiredAt time.Time) (bool, error)
	ReopenExpiration(ctx context.Context, id, claimID string) (bool, error)
}

// AssetRepository provides access to immutable finalized upload assets.
type AssetRepository interface {
	Create(ctx context.Context, asset *model.Asset) error
	FindByID(ctx context.Context, id string) (*model.Asset, error)
	FindOwnedByID(ctx context.Context, id, userID string) (*model.Asset, error)
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

// TopicPoolRepository provides access to the topic_pool table.
type TopicPoolRepository interface {
	Create(ctx context.Context, topic *model.TopicPool) error
	CreateBatch(ctx context.Context, topics []*model.TopicPool) error
	FindByID(ctx context.Context, id uint) (*model.TopicPool, error)
	FindByProject(ctx context.Context, projectID, status string, offset, limit int) ([]*model.TopicPool, int64, error)
	ClaimOne(ctx context.Context, userID, projectID string) (*model.TopicPool, error)
	ClaimWithTask(ctx context.Context, userID, projectID, taskID string) (*model.TopicPool, error)
	MarkUsed(ctx context.Context, id uint, taskID string) error
	ResetStatus(ctx context.Context, id uint) error
	ResetByTask(ctx context.Context, taskID string) error
	Delete(ctx context.Context, id uint) error
}

// AgentFeedbackRepository provides access to the agent_feedbacks table.
type AgentFeedbackRepository interface {
	Create(ctx context.Context, feedback *model.AgentFeedback) error
	FindByTaskID(ctx context.Context, taskID string) ([]*model.AgentFeedback, error)
}

type ImageGenerationRepository interface {
	Create(ctx context.Context, generation *model.ImageGeneration) error
	FindByID(ctx context.Context, id string) (*model.ImageGeneration, error)
	MarkFailed(ctx context.Context, id, message string, completedAt time.Time) error
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
	taskExecutions          TaskExecutionRepository
	files                   TaskFileRepository
	uploadSessions          UploadSessionRepository
	assets                  AssetRepository
	projects                ProjectRepository
	apiKeys                 APIKeyRepository
	feedbacks               FeedbackRepository
	modelConfigs            ModelConfigRepository
	seednoteTrackings       SeednoteTrackingRepository
	seednoteMetricSnapshots SeednoteMetricSnapshotRepository
	templates               TemplateRepository
	viralAnalyses           ViralAnalysisRepository
	posterTasks             PosterTaskRepository
	topicPools              TopicPoolRepository
	imageGenerations        ImageGenerationRepository
	agentFeedbacks          AgentFeedbackRepository
	ilinkBindings           IlinkBindingRepository
	ilinkNotifications      IlinkNotificationRepository
	billing                 BillingRepository
}

// New creates a new Repository backed by the given *gorm.DB.
func New(db *gorm.DB) Repository {
	users := newUserRepository(db)
	sessions := newSessionRepository(db)
	plans := newPlanRepository(db)
	tasks := newTaskRepository(db)
	taskExecutions := newTaskExecutionRepository(db)
	files := newTaskFileRepository(db)
	uploadSessions := newUploadSessionRepository(db)
	assets := newAssetRepository(db)
	projects := newProjectRepository(db)
	apiKeys := newAPIKeyRepository(db)
	feedbacks := newFeedbackRepository(db)
	modelConfigs := newModelConfigRepository(db)
	seednoteTrackings := newSeednoteTrackingRepository(db)
	seednoteMetricSnapshots := newSeednoteMetricSnapshotRepository(db)
	templates := newTemplateRepository(db)
	viralAnalyses := newViralAnalysisRepository(db)
	posterTasks := newPosterTaskRepository(db)
	topicPools := newTopicPoolRepository(db)
	imageGenerations := newImageGenerationRepository(db)
	agentFeedbacks := newAgentFeedbackRepository(db)
	ilinkBindings := newIlinkBindingRepository(db)
	ilinkNotifications := newIlinkNotificationRepository(db)
	billing := newBillingRepository(db)

	return &repository{
		db:                      db,
		users:                   users,
		sessions:                sessions,
		plans:                   plans,
		tasks:                   tasks,
		taskExecutions:          taskExecutions,
		files:                   files,
		uploadSessions:          uploadSessions,
		assets:                  assets,
		projects:                projects,
		apiKeys:                 apiKeys,
		feedbacks:               feedbacks,
		modelConfigs:            modelConfigs,
		seednoteTrackings:       seednoteTrackings,
		seednoteMetricSnapshots: seednoteMetricSnapshots,
		templates:               templates,
		viralAnalyses:           viralAnalyses,
		posterTasks:             posterTasks,
		topicPools:              topicPools,
		imageGenerations:        imageGenerations,
		agentFeedbacks:          agentFeedbacks,
		ilinkBindings:           ilinkBindings,
		ilinkNotifications:      ilinkNotifications,
		billing:                 billing,
	}
}

func (r *repository) Users() UserRepository                         { return r.users }
func (r *repository) Sessions() SessionRepository                   { return r.sessions }
func (r *repository) Plans() PlanRepository                         { return r.plans }
func (r *repository) Tasks() TaskRepository                         { return r.tasks }
func (r *repository) TaskExecutions() TaskExecutionRepository       { return r.taskExecutions }
func (r *repository) TaskFiles() TaskFileRepository                 { return r.files }
func (r *repository) UploadSessions() UploadSessionRepository       { return r.uploadSessions }
func (r *repository) Assets() AssetRepository                       { return r.assets }
func (r *repository) Projects() ProjectRepository                   { return r.projects }
func (r *repository) APIKeys() APIKeyRepository                     { return r.apiKeys }
func (r *repository) Feedbacks() FeedbackRepository                 { return r.feedbacks }
func (r *repository) ModelConfigs() ModelConfigRepository           { return r.modelConfigs }
func (r *repository) SeednoteTrackings() SeednoteTrackingRepository { return r.seednoteTrackings }
func (r *repository) SeednoteMetricSnapshots() SeednoteMetricSnapshotRepository {
	return r.seednoteMetricSnapshots
}
func (r *repository) Templates() TemplateRepository           { return r.templates }
func (r *repository) ViralAnalyses() ViralAnalysisRepository  { return r.viralAnalyses }
func (r *repository) PosterTasks() PosterTaskRepository       { return r.posterTasks }
func (r *repository) AgentFeedbacks() AgentFeedbackRepository { return r.agentFeedbacks }

func (r *repository) TopicPools() TopicPoolRepository             { return r.topicPools }
func (r *repository) ImageGenerations() ImageGenerationRepository { return r.imageGenerations }

func (r *repository) IlinkBindings() IlinkBindingRepository {
	return r.ilinkBindings
}
func (r *repository) IlinkNotifications() IlinkNotificationRepository {
	return r.ilinkNotifications
}
func (r *repository) Billing() BillingRepository { return r.billing }

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
	taskExecutions          TaskExecutionRepository
	files                   TaskFileRepository
	uploadSessions          UploadSessionRepository
	assets                  AssetRepository
	projects                ProjectRepository
	apiKeys                 APIKeyRepository
	feedbacks               FeedbackRepository
	modelConfigs            ModelConfigRepository
	seednoteTrackings       SeednoteTrackingRepository
	seednoteMetricSnapshots SeednoteMetricSnapshotRepository
	templates               TemplateRepository
	viralAnalyses           ViralAnalysisRepository
	posterTasks             PosterTaskRepository
	topicPools              TopicPoolRepository
	imageGenerations        ImageGenerationRepository
	agentFeedbacks          AgentFeedbackRepository
	ilinkBindings           IlinkBindingRepository
	ilinkNotifications      IlinkNotificationRepository
	billing                 BillingRepository
}

func newTxRepository(tx *gorm.DB) *txRepository {
	return &txRepository{
		db:                      tx,
		users:                   newUserRepository(tx),
		sessions:                newSessionRepository(tx),
		plans:                   newPlanRepository(tx),
		tasks:                   newTaskRepository(tx),
		taskExecutions:          newTaskExecutionRepository(tx),
		files:                   newTaskFileRepository(tx),
		uploadSessions:          newUploadSessionRepository(tx),
		assets:                  newAssetRepository(tx),
		projects:                newProjectRepository(tx),
		apiKeys:                 newAPIKeyRepository(tx),
		feedbacks:               newFeedbackRepository(tx),
		modelConfigs:            newModelConfigRepository(tx),
		seednoteTrackings:       newSeednoteTrackingRepository(tx),
		seednoteMetricSnapshots: newSeednoteMetricSnapshotRepository(tx),
		templates:               newTemplateRepository(tx),
		viralAnalyses:           newViralAnalysisRepository(tx),
		posterTasks:             newPosterTaskRepository(tx),
		topicPools:              newTopicPoolRepository(tx),
		imageGenerations:        newImageGenerationRepository(tx),
		agentFeedbacks:          newAgentFeedbackRepository(tx),
		ilinkBindings:           newIlinkBindingRepository(tx),
		ilinkNotifications:      newIlinkNotificationRepository(tx),
		billing:                 newTxBillingRepository(tx),
	}
}

func (r *txRepository) Users() UserRepository                         { return r.users }
func (r *txRepository) Sessions() SessionRepository                   { return r.sessions }
func (r *txRepository) Plans() PlanRepository                         { return r.plans }
func (r *txRepository) Tasks() TaskRepository                         { return r.tasks }
func (r *txRepository) TaskExecutions() TaskExecutionRepository       { return r.taskExecutions }
func (r *txRepository) TaskFiles() TaskFileRepository                 { return r.files }
func (r *txRepository) UploadSessions() UploadSessionRepository       { return r.uploadSessions }
func (r *txRepository) Assets() AssetRepository                       { return r.assets }
func (r *txRepository) Projects() ProjectRepository                   { return r.projects }
func (r *txRepository) APIKeys() APIKeyRepository                     { return r.apiKeys }
func (r *txRepository) Feedbacks() FeedbackRepository                 { return r.feedbacks }
func (r *txRepository) ModelConfigs() ModelConfigRepository           { return r.modelConfigs }
func (r *txRepository) SeednoteTrackings() SeednoteTrackingRepository { return r.seednoteTrackings }
func (r *txRepository) SeednoteMetricSnapshots() SeednoteMetricSnapshotRepository {
	return r.seednoteMetricSnapshots
}
func (r *txRepository) Templates() TemplateRepository           { return r.templates }
func (r *txRepository) ViralAnalyses() ViralAnalysisRepository  { return r.viralAnalyses }
func (r *txRepository) PosterTasks() PosterTaskRepository       { return r.posterTasks }
func (r *txRepository) AgentFeedbacks() AgentFeedbackRepository { return r.agentFeedbacks }

func (r *txRepository) TopicPools() TopicPoolRepository             { return r.topicPools }
func (r *txRepository) ImageGenerations() ImageGenerationRepository { return r.imageGenerations }

func (r *txRepository) IlinkBindings() IlinkBindingRepository {
	return r.ilinkBindings
}
func (r *txRepository) IlinkNotifications() IlinkNotificationRepository {
	return r.ilinkNotifications
}
func (r *txRepository) Billing() BillingRepository { return r.billing }

func (r *txRepository) WithTx(ctx context.Context, fn func(Repository) error) error {
	// Already in a transaction -- use a savepoint.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txRepo := newTxRepository(tx)
		return fn(txRepo)
	})
}

func (r *txRepository) Close() error { return nil }
