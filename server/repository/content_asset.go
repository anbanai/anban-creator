package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrContentReuseRunConflict = errors.New("content reuse run changed concurrently")

func reusePolicyRevision(raw []byte) int {
	var value struct {
		Revision int `json:"revision"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return 0
	}
	return value.Revision
}

func reuseTaskMatchesRun(ctx context.Context, db *gorm.DB, task *model.Task, run *model.ContentReuseRun) bool {
	if task == nil || run == nil || task.UserID != run.UserID || task.ProjectID != run.TargetProjectID || task.ReuseRunID != run.ID {
		return false
	}
	if task.ReuseStageID == "" {
		return run.TaskID == task.ID
	}
	var stage model.ContentReuseStageExecution
	return db.WithContext(ctx).Where("id = ? AND run_id = ? AND task_id = ?", task.ReuseStageID, run.ID, task.ID).First(&stage).Error == nil
}

type contentAssetRepository struct{ db *gorm.DB }

func newContentAssetRepository(db *gorm.DB) ContentAssetRepository {
	return &contentAssetRepository{db: db}
}

func (r *contentAssetRepository) Create(ctx context.Context, asset *model.ContentAsset) error {
	return r.db.WithContext(ctx).Create(asset).Error
}

func (r *contentAssetRepository) FindByID(ctx context.Context, userID, id string) (*model.ContentAsset, error) {
	var asset model.ContentAsset
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&asset).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *contentAssetRepository) FindByTitle(ctx context.Context, userID, title string) (*model.ContentAsset, error) {
	var asset model.ContentAsset
	if err := r.db.WithContext(ctx).Where("user_id = ? AND title = ?", userID, title).Order("created_at ASC").First(&asset).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *contentAssetRepository) List(ctx context.Context, userID, status, query string, offset, limit int) ([]*model.ContentAsset, int64, error) {
	var rows []*model.ContentAsset
	var total int64
	q := r.db.WithContext(ctx).Model(&model.ContentAsset{}).Where("user_id = ?", userID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if query != "" {
		q = q.Where("title LIKE ? OR core_question LIKE ?", "%"+query+"%", "%"+query+"%")
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("updated_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *contentAssetRepository) ListWithFilters(ctx context.Context, filter ContentAssetListFilter) ([]*model.ContentAsset, int64, error) {
	var rows []*model.ContentAsset
	var total int64
	q := r.db.WithContext(ctx).Model(&model.ContentAsset{}).Where("content_assets.user_id = ?", filter.UserID)
	if filter.Status != "" {
		q = q.Where("content_assets.status = ?", filter.Status)
	}
	if filter.Lifecycle != "" {
		q = q.Where("content_assets.lifecycle = ?", filter.Lifecycle)
	}
	if filter.Query != "" {
		q = q.Where("(content_assets.title LIKE ? OR content_assets.core_question LIKE ?)", "%"+filter.Query+"%", "%"+filter.Query+"%")
	}
	if filter.Source != "" {
		q = q.Where("EXISTS (SELECT 1 FROM content_asset_evidence e WHERE e.asset_id = content_assets.id AND e.user_id = content_assets.user_id AND e.source_type = ?)", filter.Source)
	}
	if filter.Platform != "" {
		q = q.Where(`(EXISTS (SELECT 1 FROM content_asset_links l WHERE l.asset_id = content_assets.id AND l.user_id = content_assets.user_id AND l.platform = ?)
			OR EXISTS (SELECT 1 FROM content_asset_evidence e WHERE e.asset_id = content_assets.id AND e.user_id = content_assets.user_id AND e.platform = ?)
			OR EXISTS (SELECT 1 FROM content_reuse_runs r WHERE r.asset_id = content_assets.id AND r.user_id = content_assets.user_id AND r.target_platform = ?))`, filter.Platform, filter.Platform, filter.Platform)
	}
	if filter.ProjectID != "" {
		q = q.Where(`(EXISTS (SELECT 1 FROM content_asset_links l WHERE l.asset_id = content_assets.id AND l.user_id = content_assets.user_id AND l.project_id = ?)
			OR EXISTS (SELECT 1 FROM content_asset_evidence e WHERE e.asset_id = content_assets.id AND e.user_id = content_assets.user_id AND e.project_id = ?)
			OR EXISTS (SELECT 1 FROM content_reuse_runs r WHERE r.asset_id = content_assets.id AND r.user_id = content_assets.user_id AND r.target_project_id = ?))`, filter.ProjectID, filter.ProjectID, filter.ProjectID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("content_assets.updated_at DESC, content_assets.id DESC").Offset(filter.Offset).Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

func (r *contentAssetRepository) Update(ctx context.Context, asset *model.ContentAsset) error {
	return r.db.WithContext(ctx).Save(asset).Error
}
func (r *contentAssetRepository) CreateEvidence(ctx context.Context, evidence *model.ContentAssetEvidence) error {
	return r.db.WithContext(ctx).Create(evidence).Error
}

// MergeAsset moves the source asset's durable lineage into the canonical
// asset, de-duplicates links under the unique lineage key, preserves evidence,
// and archives the source. The operation is idempotent for retries.
func (r *contentAssetRepository) MergeAsset(ctx context.Context, userID, canonicalID, sourceID string) error {
	if canonicalID == sourceID || strings.TrimSpace(canonicalID) == "" || strings.TrimSpace(sourceID) == "" {
		return nil
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var canonical, source model.ContentAsset
		if err := tx.Where("id = ? AND user_id = ?", canonicalID, userID).First(&canonical).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ? AND user_id = ?", sourceID, userID).First(&source).Error; err != nil {
			return err
		}
		var links []model.ContentAssetLink
		if err := tx.Where("asset_id = ? AND user_id = ?", sourceID, userID).Find(&links).Error; err != nil {
			return err
		}
		for _, link := range links {
			var existing model.ContentAssetLink
			err := tx.Where("asset_id = ? AND content_kind = ? AND content_id = ? AND relation = ?", canonicalID, link.ContentKind, link.ContentID, link.Relation).First(&existing).Error
			if err == nil {
				if err := tx.Delete(&model.ContentAssetLink{}, "id = ?", link.ID).Error; err != nil {
					return err
				}
				continue
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Model(&model.ContentAssetLink{}).Where("id = ? AND asset_id = ? AND user_id = ?", link.ID, sourceID, userID).Updates(map[string]any{"asset_id": canonicalID}).Error; err != nil {
				return err
			}
		}
		var evidence []model.ContentAssetEvidence
		if err := tx.Where("asset_id = ? AND user_id = ?", sourceID, userID).Find(&evidence).Error; err != nil {
			return err
		}
		for _, item := range evidence {
			var duplicate model.ContentAssetEvidence
			err := tx.Where("asset_id = ? AND user_id = ? AND source_type = ? AND source_id = ? AND url = ?", canonicalID, userID, item.SourceType, item.SourceID, item.URL).First(&duplicate).Error
			if err == nil && item.SourceID != "" {
				if err := tx.Delete(&model.ContentAssetEvidence{}, "id = ?", item.ID).Error; err != nil {
					return err
				}
				continue
			}
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Model(&model.ContentAssetEvidence{}).Where("id = ? AND asset_id = ? AND user_id = ?", item.ID, sourceID, userID).Updates(map[string]any{"asset_id": canonicalID}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&model.ContentAsset{}).Where("id = ? AND user_id = ?", sourceID, userID).Updates(map[string]any{"status": model.ContentAssetStatusArchived, "updated_at": time.Now().UTC()}).Error
	})
}

func (r *contentAssetRepository) ListEvidence(ctx context.Context, userID, assetID string) ([]*model.ContentAssetEvidence, error) {
	var rows []*model.ContentAssetEvidence
	if err := r.db.WithContext(ctx).Where("user_id = ? AND asset_id = ?", userID, assetID).Order("created_at DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *contentAssetRepository) CreateLink(ctx context.Context, link *model.ContentAssetLink) error {
	var existing model.ContentAssetLink
	err := r.db.WithContext(ctx).Where("asset_id = ? AND content_kind = ? AND content_id = ? AND relation = ?", link.AssetID, link.ContentKind, link.ContentID, link.Relation).First(&existing).Error
	if err == nil {
		*link = existing
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := r.db.WithContext(ctx).Create(link).Error; err != nil {
		if lookupErr := r.db.WithContext(ctx).Where("asset_id = ? AND content_kind = ? AND content_id = ? AND relation = ?", link.AssetID, link.ContentKind, link.ContentID, link.Relation).First(&existing).Error; lookupErr == nil {
			*link = existing
			return nil
		}
		return err
	}
	return nil
}

func (r *contentAssetRepository) FindLink(ctx context.Context, userID, assetID, contentKind, contentID, relation string) (*model.ContentAssetLink, error) {
	var link model.ContentAssetLink
	if err := r.db.WithContext(ctx).Where("user_id = ? AND asset_id = ? AND content_kind = ? AND content_id = ? AND relation = ?", userID, assetID, contentKind, contentID, relation).First(&link).Error; err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *contentAssetRepository) ListLinks(ctx context.Context, userID, assetID string) ([]*model.ContentAssetLink, error) {
	var rows []*model.ContentAssetLink
	if err := r.db.WithContext(ctx).Where("user_id = ? AND asset_id = ?", userID, assetID).Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *contentAssetRepository) ListLinksByContent(ctx context.Context, userID, projectID, contentKind, contentID string) ([]*model.ContentAssetLink, error) {
	var rows []*model.ContentAssetLink
	query := r.db.WithContext(ctx).Where("user_id = ? AND content_kind = ? AND content_id = ?", userID, contentKind, contentID)
	if strings.TrimSpace(projectID) != "" {
		query = query.Where("project_id = ?", projectID)
	}
	if err := query.Order("created_at ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *contentAssetRepository) CreateScoreSnapshot(ctx context.Context, snapshot *model.ContentAssetScoreSnapshot) error {
	return r.db.WithContext(ctx).Create(snapshot).Error
}

func (r *contentAssetRepository) ListScoreSnapshots(ctx context.Context, userID, assetID string) ([]*model.ContentAssetScoreSnapshot, error) {
	var rows []*model.ContentAssetScoreSnapshot
	if err := r.db.WithContext(ctx).Where("user_id = ? AND asset_id = ?", userID, assetID).Order("created_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *contentAssetRepository) CreateReuseRun(ctx context.Context, run *model.ContentReuseRun) error {
	return r.db.WithContext(ctx).Create(run).Error
}
func (r *contentAssetRepository) FindReuseRun(ctx context.Context, userID, id string) (*model.ContentReuseRun, error) {
	var run model.ContentReuseRun
	if err := r.db.WithContext(ctx).Where("id = ? AND user_id = ?", id, userID).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *contentAssetRepository) FindReuseRunByID(ctx context.Context, id string) (*model.ContentReuseRun, error) {
	var run model.ContentReuseRun
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *contentAssetRepository) FindLatestReuseRun(ctx context.Context, userID, assetID string) (*model.ContentReuseRun, error) {
	var run model.ContentReuseRun
	if err := r.db.WithContext(ctx).
		Where("user_id = ? AND asset_id = ?", userID, assetID).
		Order("updated_at DESC, created_at DESC, id DESC").
		First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *contentAssetRepository) FindReuseRunByIdempotency(ctx context.Context, userID, key string) (*model.ContentReuseRun, error) {
	var run model.ContentReuseRun
	if err := r.db.WithContext(ctx).Where("user_id = ? AND idempotency_key = ?", userID, key).First(&run).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (r *contentAssetRepository) ListReuseRuns(ctx context.Context, filter ContentReuseRunListFilter) ([]*model.ContentReuseRun, int64, error) {
	var rows []*model.ContentReuseRun
	var total int64
	q := r.db.WithContext(ctx).Model(&model.ContentReuseRun{}).Where("user_id = ?", filter.UserID)
	if filter.AssetID != "" {
		q = q.Where("asset_id = ?", filter.AssetID)
	}
	if filter.TargetProjectID != "" {
		q = q.Where("target_project_id = ?", filter.TargetProjectID)
	}
	if filter.TargetPlatform != "" {
		q = q.Where("target_platform = ?", filter.TargetPlatform)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.Stage != "" {
		q = q.Where("stage = ?", filter.Stage)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := q.Order("updated_at DESC, id DESC").Offset(filter.Offset).Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// Persisted queued/running rows are also the dispatch recovery intent. Stopped
// runs never enter this scan. Bound tasks remain the production idempotency key.
func (r *contentAssetRepository) ListRecoverableReuseRuns(ctx context.Context, now time.Time, limit int) ([]*model.ContentReuseRun, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var runs []*model.ContentReuseRun
	err := r.db.WithContext(ctx).Where("status IN ?", []string{model.ContentReuseRunStatusQueued, model.ContentReuseRunStatusRunning}).
		Where("next_run_at <= ? OR (next_run_at IS NULL AND updated_at <= ?)", now, now.Add(-10*time.Minute)).
		Order("updated_at ASC, id ASC").Limit(limit).Find(&runs).Error
	return runs, err
}

func (r *contentAssetRepository) ClaimReuseRun(ctx context.Context, id string) (bool, error) {
	result := r.db.WithContext(ctx).Model(&model.ContentReuseRun{}).
		Where("id = ? AND status = ?", id, model.ContentReuseRunStatusQueued).
		Updates(map[string]any{"status": model.ContentReuseRunStatusRunning, "version": gorm.Expr("version + 1")})
	return result.RowsAffected == 1, result.Error
}

// BindReuseTask runs inside task admission's transaction, serializing creation
// and its charge against pause/cancel. A losing admission rolls back entirely.
func (r *contentAssetRepository) BindReuseTask(ctx context.Context, task *model.Task) error {
	var run model.ContentReuseRun
	if err := r.db.WithContext(ctx).Where("id = ?", task.ReuseRunID).First(&run).Error; err != nil {
		return err
	}
	if run.TaskID == "" && run.BudgetReserved > 0 {
		var policy model.ContentAutopilotPolicy
		if err := json.Unmarshal(run.PolicySnapshot, &policy); err != nil {
			return err
		}
		if policy.Enabled {
			reserved, err := r.ReserveReuseRunBudget(ctx, run.ID, run.BudgetReserved, policy.DailyBudgetCredits, time.Now().UTC())
			if err != nil {
				return err
			}
			if !reserved {
				return ErrContentReuseBudgetExceeded
			}
		}
	}
	result := r.db.WithContext(ctx).Model(&model.ContentReuseRun{}).
		Where("id = ? AND user_id = ? AND asset_id = ? AND target_project_id = ? AND status = ? AND task_id = ?",
			task.ReuseRunID, task.UserID, task.ContentAssetID, task.ProjectID, model.ContentReuseRunStatusRunning, "").
		Updates(map[string]any{"task_id": task.ID, "version": gorm.Expr("version + 1")})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrContentReuseRunConflict
	}
	return nil
}

func (r *contentAssetRepository) BindReuseStageTask(ctx context.Context, task *model.Task) error {
	var stage model.ContentReuseStageExecution
	if err := r.db.WithContext(ctx).Where("id = ?", task.ReuseStageID).First(&stage).Error; err != nil {
		return err
	}
	var run model.ContentReuseRun
	if err := r.db.WithContext(ctx).Where("id = ?", stage.RunID).First(&run).Error; err != nil {
		return err
	}
	if task.ReuseRunID != run.ID || task.UserID != run.UserID || task.ContentAssetID != run.AssetID || task.ProjectID != run.TargetProjectID || run.Status != model.ContentReuseRunStatusRunning || stage.Status != model.ContentStageQueued || stage.TaskID != nil {
		return ErrContentReuseRunConflict
	}
	stage.TaskID = &task.ID
	stage.Status = model.ContentStageRunning
	stage.QuoteID = task.BillingQuoteID
	if task.BillingChargeID != nil {
		stage.ChargeID = *task.BillingChargeID
	}
	stage.ReservedCredits = task.BillingPriceCredits
	now := time.Now().UTC()
	event := &model.ContentReuseAuditEvent{ID: uuid.NewString(), UserID: run.UserID, RunID: run.ID, StageExecutionID: stage.ID, EntityKind: "stage", EntityID: stage.ID, FromState: model.ContentStageQueued, ToState: model.ContentStageRunning, TaskID: task.ID, PolicyRevision: reusePolicyRevision(run.PolicySnapshot), ReasonCode: "task_admitted", CreatedAt: now}
	return r.TransitionReuseStage(ctx, &stage, event)
}

func (r *contentAssetRepository) CreateResearchJob(ctx context.Context, job *model.ContentAssetResearchJob) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		err := tx.Create(&model.ContentReuseAuditEvent{ID: uuid.NewString(), UserID: job.UserID, RunID: job.ID, EntityKind: "research_job", EntityID: job.ID, ToState: job.Status, PolicyRevision: job.PolicyRevision, ReasonCode: "research_job_created", CreatedAt: time.Now().UTC()}).Error
		if strings.Contains(strings.ToLower(fmt.Sprint(err)), "no such table") {
			return nil
		}
		return err
	})
}

func (r *contentAssetRepository) FindResearchJobByFingerprint(ctx context.Context, userID, fingerprint string) (*model.ContentAssetResearchJob, error) {
	var job model.ContentAssetResearchJob
	if err := r.db.WithContext(ctx).Where("user_id = ? AND fingerprint = ?", userID, fingerprint).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *contentAssetRepository) FindResearchJobByID(ctx context.Context, userID, id string) (*model.ContentAssetResearchJob, error) {
	var job model.ContentAssetResearchJob
	if err := r.db.WithContext(ctx).Where("user_id = ? AND id = ?", userID, id).First(&job).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *contentAssetRepository) BindResearchJobTask(ctx context.Context, task *model.Task) error {
	var job model.ContentAssetResearchJob
	if err := r.db.WithContext(ctx).Where("id = ?", task.ResearchJobID).First(&job).Error; err != nil {
		return err
	}
	if task.Type != model.TaskTypeContentResearch || task.TaskKind != model.TaskTypeContentResearch || task.UserID != job.UserID || task.ProjectID != job.ProjectID || job.Status != model.ContentStageQueued || job.TaskID != nil {
		return ErrContentReuseRunConflict
	}
	previous := job.Version
	result := r.db.WithContext(ctx).Model(&model.ContentAssetResearchJob{}).Where("id = ? AND user_id = ? AND version = ? AND task_id IS NULL AND status = ?", job.ID, job.UserID, previous, model.ContentStageQueued).Updates(map[string]any{"task_id": task.ID, "version": gorm.Expr("version + 1"), "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrContentReuseRunConflict
	}
	return nil
}

func (r *contentAssetRepository) UpdateResearchJob(ctx context.Context, job *model.ContentAssetResearchJob) error {
	previous := job.Version
	job.Version++
	result := r.db.WithContext(ctx).Model(&model.ContentAssetResearchJob{}).Where("id = ? AND user_id = ? AND version = ?", job.ID, job.UserID, previous).Updates(map[string]any{
		"status": job.Status, "retry_count": job.RetryCount, "error_code": job.ErrorCode,
		"error_message": job.ErrorMessage, "next_run_at": job.NextRunAt,
		"task_id": job.TaskID, "version": job.Version, "updated_at": time.Now().UTC(),
	})
	if result.Error != nil || result.RowsAffected != 1 {
		job.Version = previous
		if result.Error != nil {
			return result.Error
		}
		return ErrContentReuseRunConflict
	}
	return nil
}

func (r *contentAssetRepository) TransitionResearchJob(ctx context.Context, job *model.ContentAssetResearchJob, event *model.ContentReuseAuditEvent) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		previous := job.Version
		job.Version++
		job.UpdatedAt = time.Now().UTC()
		result := tx.Model(&model.ContentAssetResearchJob{}).Where("id = ? AND user_id = ? AND version = ?", job.ID, job.UserID, previous).Updates(map[string]any{
			"status": job.Status, "retry_count": job.RetryCount, "error_code": job.ErrorCode,
			"error_message": job.ErrorMessage, "next_run_at": job.NextRunAt, "task_id": job.TaskID,
			"accepted_execution_id": job.AcceptedExecutionID, "artifact_refs": job.ArtifactRefs,
			"version": job.Version, "updated_at": job.UpdatedAt,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			job.Version = previous
			return ErrContentReuseRunConflict
		}
		event.PreviousVersion, event.NewVersion = previous, job.Version
		if event.ID == "" {
			event.ID = uuid.NewString()
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		return tx.Create(event).Error
	})
}

func (r *contentAssetRepository) CreateReuseStage(ctx context.Context, stage *model.ContentReuseStageExecution) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(stage).Error; err != nil {
			return err
		}
		var run model.ContentReuseRun
		if err := tx.Select("id", "user_id", "policy_snapshot").Where("id = ?", stage.RunID).First(&run).Error; err != nil {
			return err
		}
		err := tx.Create(&model.ContentReuseAuditEvent{ID: uuid.NewString(), UserID: run.UserID, RunID: run.ID, StageExecutionID: stage.ID, EntityKind: "stage", EntityID: stage.ID, ToState: stage.Status, PolicyRevision: reusePolicyRevision(run.PolicySnapshot), ReasonCode: "stage_created", CreatedAt: time.Now().UTC()}).Error
		if strings.Contains(strings.ToLower(fmt.Sprint(err)), "no such table") {
			return nil
		}
		return err
	})
}

func (r *contentAssetRepository) FindReuseStage(ctx context.Context, runID, name string) (*model.ContentReuseStageExecution, error) {
	var stage model.ContentReuseStageExecution
	if err := r.db.WithContext(ctx).Where("run_id = ? AND stage = ?", runID, name).First(&stage).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return &stage, nil
}

func (r *contentAssetRepository) FindReuseStageByID(ctx context.Context, id string) (*model.ContentReuseStageExecution, error) {
	var stage model.ContentReuseStageExecution
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&stage).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, gorm.ErrRecordNotFound
		}
		return nil, err
	}
	return &stage, nil
}

func (r *contentAssetRepository) UpdateReuseStage(ctx context.Context, stage *model.ContentReuseStageExecution) error {
	previous := stage.Version
	stage.Version++
	stage.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Model(&model.ContentReuseStageExecution{}).Where("id = ? AND version = ?", stage.ID, previous).Updates(map[string]any{
		"status": stage.Status, "task_id": stage.TaskID, "accepted_execution_id": stage.AcceptedExecutionID,
		"artifact_refs": stage.ArtifactRefs, "quote_id": stage.QuoteID, "charge_id": stage.ChargeID,
		"reserved_credits": stage.ReservedCredits, "settled_credits": stage.SettledCredits,
		"retry_count": stage.RetryCount, "error_code": stage.ErrorCode, "error_message": stage.ErrorMessage,
		"recovery_point": stage.RecoveryPoint, "next_run_at": stage.NextRunAt,
		"version": stage.Version, "updated_at": stage.UpdatedAt,
	})
	if result.Error != nil || result.RowsAffected != 1 {
		stage.Version = previous
		if result.Error != nil {
			return result.Error
		}
		return ErrContentReuseRunConflict
	}
	return nil
}

func (r *contentAssetRepository) TransitionReuseStage(ctx context.Context, stage *model.ContentReuseStageExecution, event *model.ContentReuseAuditEvent) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.ContentReuseRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", stage.RunID).First(&run).Error; err != nil {
			return err
		}
		if run.Status != model.ContentReuseRunStatusRunning {
			return ErrContentReuseRunConflict
		}
		runCAS := tx.Model(&model.ContentReuseRun{}).Where("id = ? AND user_id = ? AND version = ? AND status = ?", run.ID, run.UserID, run.Version, model.ContentReuseRunStatusRunning).Update("version", gorm.Expr("version + 1"))
		if runCAS.Error != nil {
			return runCAS.Error
		}
		if runCAS.RowsAffected != 1 {
			return ErrContentReuseRunConflict
		}
		previous := stage.Version
		stage.Version++
		stage.UpdatedAt = time.Now().UTC()
		result := tx.Model(&model.ContentReuseStageExecution{}).Where("id = ? AND run_id = ? AND version = ?", stage.ID, stage.RunID, previous).Updates(map[string]any{
			"status": stage.Status, "task_id": stage.TaskID, "accepted_execution_id": stage.AcceptedExecutionID,
			"artifact_refs": stage.ArtifactRefs, "quote_id": stage.QuoteID, "charge_id": stage.ChargeID,
			"reserved_credits": stage.ReservedCredits, "settled_credits": stage.SettledCredits,
			"retry_count": stage.RetryCount, "error_code": stage.ErrorCode, "error_message": stage.ErrorMessage,
			"recovery_point": stage.RecoveryPoint, "next_run_at": stage.NextRunAt,
			"version": stage.Version, "updated_at": stage.UpdatedAt,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			stage.Version = previous
			return ErrContentReuseRunConflict
		}
		event.PreviousVersion, event.NewVersion = previous, stage.Version
		if event.ID == "" {
			event.ID = uuid.NewString()
		}
		if event.CreatedAt.IsZero() {
			event.CreatedAt = time.Now().UTC()
		}
		return tx.Create(event).Error
	})
}

func (r *contentAssetRepository) AppendReuseAudit(ctx context.Context, event *model.ContentReuseAuditEvent) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	return r.db.WithContext(ctx).Create(event).Error
}

func (r *contentAssetRepository) ListReuseAudit(ctx context.Context, userID, runID string) ([]*model.ContentReuseAuditEvent, error) {
	var events []*model.ContentReuseAuditEvent
	err := r.db.WithContext(ctx).Where("user_id = ? AND run_id = ?", userID, runID).Order("created_at ASC, id ASC").Find(&events).Error
	return events, err
}

func (r *contentAssetRepository) ReserveReuseRunBudget(ctx context.Context, runID string, amount, dailyLimit int64, now time.Time) (bool, error) {
	if amount <= 0 || dailyLimit <= 0 || amount > dailyLimit {
		return false, nil
	}
	reserved := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run model.ContentReuseRun
		if err := tx.Where("id = ?", runID).First(&run).Error; err != nil {
			return err
		}
		var policy model.ContentAutopilotPolicy
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", run.UserID).First(&policy).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", runID).First(&run).Error; err != nil {
			return err
		}
		if run.Status != model.ContentReuseRunStatusRunning {
			return nil
		}
		dayStart := now.UTC().Truncate(24 * time.Hour)
		sameDay := run.BudgetDate != nil && run.BudgetDate.UTC().Equal(dayStart)
		if run.BudgetReserved > 0 && (sameDay || run.TaskID != "") {
			reserved = run.BudgetReserved == amount
			return nil
		}
		// Serialize admission against the user's policy row so concurrent workers
		// cannot both observe the same remaining daily budget.
		spent, err := reuseDailyBudgetUsage(ctx, tx, run.UserID, run.ID, dayStart)
		if err != nil {
			return err
		}
		if amount > dailyLimit-spent {
			return nil
		}
		result := tx.Model(&model.ContentReuseRun{}).Where("id = ? AND version = ?", run.ID, run.Version).Updates(map[string]any{"budget_reserved": amount, "budget_date": dayStart, "version": gorm.Expr("version + 1")})
		if result.Error != nil {
			return result.Error
		}
		reserved = result.RowsAffected == 1
		return nil
	})
	return reserved, err
}
func (r *contentAssetRepository) UpdateReuseRun(ctx context.Context, run *model.ContentReuseRun) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		previousVersion := run.Version
		var previous model.ContentReuseRun
		if err := tx.Where("id = ? AND user_id = ? AND version = ?", run.ID, run.UserID, previousVersion).First(&previous).Error; err != nil {
			return ErrContentReuseRunConflict
		}
		run.Version++
		result := tx.Model(&model.ContentReuseRun{}).
			Where("id = ? AND user_id = ? AND version = ?", run.ID, run.UserID, previousVersion).
			Select("*").Omit("id", "created_at").Updates(run)
		if result.Error != nil || result.RowsAffected != 1 {
			run.Version = previousVersion
			if result.Error != nil {
				return result.Error
			}
			return ErrContentReuseRunConflict
		}
		if previous.Status != run.Status || previous.Stage != run.Stage {
			details, _ := json.Marshal(map[string]string{"from_stage": previous.Stage, "to_stage": run.Stage})
			event := &model.ContentReuseAuditEvent{ID: uuid.NewString(), UserID: run.UserID, RunID: run.ID, EntityKind: "run", EntityID: run.ID, FromState: previous.Status + ":" + previous.Stage, ToState: run.Status + ":" + run.Stage, PreviousVersion: previousVersion, NewVersion: run.Version, PolicyRevision: reusePolicyRevision(run.PolicySnapshot), ReasonCode: "run_state_updated", Details: details, CreatedAt: time.Now().UTC()}
			if err := tx.Create(event).Error; err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
				run.Version = previousVersion
				return err
			}
		}
		return nil
	})
}

func (r *contentAssetRepository) GetPolicy(ctx context.Context, userID string) (*model.ContentAutopilotPolicy, error) {
	var policy model.ContentAutopilotPolicy
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.NewContentAutopilotPolicy(userID), nil
		}
		return nil, err
	}
	return &policy, nil
}

func (r *contentAssetRepository) LockPolicy(ctx context.Context, userID string) (*model.ContentAutopilotPolicy, error) {
	var policy model.ContentAutopilotPolicy
	err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&policy).Error
	return &policy, err
}

func (r *contentAssetRepository) ListEnabledPolicies(ctx context.Context, limit int) ([]*model.ContentAutopilotPolicy, error) {
	return r.ListEnabledPoliciesAfter(ctx, "", limit)
}

func (r *contentAssetRepository) ListEnabledPoliciesAfter(ctx context.Context, afterUserID string, limit int) ([]*model.ContentAutopilotPolicy, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var rows []*model.ContentAutopilotPolicy
	err := r.db.WithContext(ctx).Where("enabled = ? AND user_id > ?", true, afterUserID).Order("user_id ASC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (r *contentAssetRepository) UpsertPolicy(ctx context.Context, policy *model.ContentAutopilotPolicy) error {
	var existing model.ContentAutopilotPolicy
	if err := r.db.WithContext(ctx).Where("user_id = ?", policy.UserID).First(&existing).Error; err == nil {
		policy.Revision = existing.Revision + 1
		return r.db.WithContext(ctx).Model(&existing).Updates(map[string]any{
			"enabled": policy.Enabled, "project_ids": policy.ProjectIDs, "platforms": policy.Platforms,
			"daily_budget_credits": policy.DailyBudgetCredits, "max_run_cost_credits": policy.MaxRunCostCredits,
			"min_recommendation_score": policy.MinRecommendationScore, "min_video_confidence": policy.MinVideoConfidence,
			"platform_min_recommendation_scores": policy.PlatformMinRecommendationScores, "video_first": policy.VideoFirst,
			"publish_windows": policy.PublishWindows, "draft_enabled": policy.DraftEnabled, "publish_enabled": policy.PublishEnabled,
			"compliance_version": policy.ComplianceVersion, "failure_mode": policy.FailureMode, "revision": policy.Revision,
		}).Error
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return r.db.WithContext(ctx).Create(policy).Error
}
