package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Backfill only missing references. Content, use timestamps, task/execution IDs,
// publication records and legacy artifacts remain intact. Re-running is safe.
func migrateUnifiedTopicHistory(ctx context.Context, db *gorm.DB) error {
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := repository.New(tx)
		var suggestions []model.TopicSuggestion
		if err := tx.Where("content_asset_id IS NULL OR content_asset_id = ''").Find(&suggestions).Error; err != nil {
			return err
		}
		for i := range suggestions {
			sg := &suggestions[i]
			var digest model.TopicDigest
			if err := tx.Where("id = ?", sg.DigestID).First(&digest).Error; err != nil {
				return fmt.Errorf("migrate suggestion %s: %w", sg.ID, err)
			}
			asset, err := repo.ContentAssets().FindByTitle(ctx, digest.UserID, sg.Title)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				asset = model.NewContentAsset(digest.UserID, sg.Title)
				asset.CoreQuestion = sg.Introduction
				err = repo.ContentAssets().Create(ctx, asset)
			}
			if err != nil {
				return err
			}
			sg.ContentAssetID = asset.ID
			if err := tx.Model(sg).UpdateColumn("content_asset_id", asset.ID).Error; err != nil {
				return err
			}
			projectExists, err := topicHistoryProjectExists(ctx, tx, digest.UserID, digest.ProjectID)
			if err != nil {
				return err
			}
			if projectExists {
				row := &model.TopicPool{UserID: digest.UserID, ProjectID: digest.ProjectID, Topic: asset.Title, ContentAssetID: &asset.ID, SourceDigestID: &digest.ID, SourceSuggestionID: &sg.ID, SourceLabel: topicPoolSourceLabel(sg.SourceSummary), Status: model.TopicStatusUnused}
				if _, err := repo.TopicPools().AddWithSource(ctx, row); err != nil {
					return err
				}
				if row.ContentAssetID == nil {
					if err := repo.TopicPools().LinkContentAsset(ctx, row.ID, asset.ID); err != nil {
						return err
					}
				}
			}
			var refs []model.TopicSourceRef
			if err := json.Unmarshal(sg.SourceRefs, &refs); err != nil {
				return err
			}
			for _, ref := range refs {
				if err := repo.ContentAssets().CreateEvidence(ctx, &model.ContentAssetEvidence{ID: uuid.NewString(), UserID: digest.UserID, AssetID: asset.ID, ProjectID: digest.ProjectID, SourceType: model.ContentAssetEvidenceExternal, SourceKind: ref.Type, SourceID: ref.RefID, Title: ref.Title, URL: ref.URL, Notes: sg.SourceSummary, Quality: "unverified", DataSource: "migrated_topic_research"}); err != nil {
					return err
				}
			}
		}
		var rows []model.TopicPool
		if err := tx.Where("task_id IS NOT NULL AND content_asset_id IS NOT NULL AND EXISTS (SELECT 1 FROM tasks WHERE tasks.id = topic_pool.task_id AND (tasks.content_asset_id IS NULL OR tasks.content_asset_id = ''))").Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			if err := tx.Model(&model.Task{}).Where("id = ? AND user_id = ? AND project_id = ? AND (content_asset_id IS NULL OR content_asset_id = '')", *row.TaskID, row.UserID, row.ProjectID).UpdateColumn("content_asset_id", *row.ContentAssetID).Error; err != nil {
				return err
			}
		}
		var jobs []model.TopicScriptJob
		if err := tx.Where("content_asset_id IS NULL OR content_asset_id = '' OR input_snapshot IS NULL").Find(&jobs).Error; err != nil {
			return err
		}
		for i := range jobs {
			job := &jobs[i]
			var sg model.TopicSuggestion
			var digest model.TopicDigest
			if err := tx.Where("id = ?", job.SuggestionID).First(&sg).Error; err != nil {
				return fmt.Errorf("migrate script %s: %w", job.ID, err)
			}
			if err := tx.Where("id = ?", job.DigestID).First(&digest).Error; err != nil {
				return err
			}
			if digest.UserID != job.UserID || sg.ProjectID != job.ProjectID {
				return fmt.Errorf("script migration ownership mismatch: %s", job.ID)
			}
			job.ContentAssetID = sg.ContentAssetID
			if len(job.InputSnapshot) == 0 || string(job.InputSnapshot) == "null" {
				var refs []model.TopicSourceRef
				var scores map[string]float64
				if err := json.Unmarshal(sg.SourceRefs, &refs); err != nil {
					return err
				}
				if err := json.Unmarshal(sg.Scores, &scores); err != nil {
					return err
				}
				selected := map[string]any{"title": sg.Title, "introduction": sg.Introduction, "source_summary": sg.SourceSummary, "source_refs": refs, "scores": scores, "video_fit": sg.VideoFit}
				frozen, err := json.Marshal(map[string]any{"selected_topic": selected, "target": map[string]any{"platform": job.TargetPlatform, "duration_seconds": job.DurationSeconds, "aspect_ratio": job.AspectRatio}, "frozen_context": json.RawMessage(digest.InputSnapshot)})
				if err != nil {
					return err
				}
				hash := sha256.Sum256(frozen)
				var input map[string]any
				if err := json.Unmarshal(frozen, &input); err != nil {
					return err
				}
				input["schema_version"] = "1.0"
				input["task_id"] = ""
				input["execution_id"] = ""
				input["task_kind"] = model.TaskKindTopicScript
				input["snapshot"] = map[string]any{"snapshot_id": job.ID, "snapshot_hash": hex.EncodeToString(hash[:]), "captured_at": job.CreatedAt.UTC().Format(time.RFC3339Nano), "asset_id": job.ContentAssetID, "project_id": job.ProjectID}
				input["source_records"] = refs
				job.InputSnapshot, err = json.Marshal(input)
				if err != nil {
					return err
				}
			}
			// Use the new shared fingerprint, retaining old jobs as historical variants if necessary.
			fingerprintInput, _ := json.Marshal(struct {
				AssetID     string `json:"asset_id"`
				ProjectID   string `json:"project_id"`
				UserID      string `json:"user_id"`
				Platform    string `json:"platform"`
				Duration    int    `json:"duration"`
				AspectRatio string `json:"aspect_ratio"`
			}{job.ContentAssetID, job.ProjectID, job.UserID, job.TargetPlatform, job.DurationSeconds, job.AspectRatio})
			fingerprintHash := sha256.Sum256(fingerprintInput)
			fingerprint := hex.EncodeToString(fingerprintHash[:])
			var count int64
			if err := tx.Model(&model.TopicScriptJob{}).Where("fingerprint = ? AND id <> ?", fingerprint, job.ID).Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				job.Fingerprint = fingerprint
			}
			if err := tx.Save(job).Error; err != nil {
				return err
			}
			if job.TaskID != nil {
				if err := tx.Model(&model.Task{}).Where("id = ? AND user_id = ? AND (content_asset_id IS NULL OR content_asset_id = '')", *job.TaskID, job.UserID).UpdateColumn("content_asset_id", job.ContentAssetID).Error; err != nil {
					return err
				}
			}
		}
		// Reverse projection repairs assets imported before the unified queue existed.
		// References and planning work attach personas without consuming production.
		var links []model.ContentAssetLink
		if err := tx.Order("created_at ASC, id ASC").Find(&links).Error; err != nil {
			return err
		}
		for _, link := range links {
			if link.ProjectID == "" {
				var projectID string
				switch link.ContentKind {
				case "task", "video_task":
					if err := tx.Model(&model.Task{}).Select("project_id").Where("id = ? AND user_id = ?", link.ContentID, link.UserID).Scan(&projectID).Error; err != nil {
						return err
					}
				case "wechat_publication":
					if err := tx.Model(&model.WechatPublication{}).Select("project_id").Where("id = ? AND user_id = ?", link.ContentID, link.UserID).Scan(&projectID).Error; err != nil {
						return err
					}
				case "seednote_post":
					if err := tx.Model(&model.SeednotePost{}).Select("project_id").Where("id = ? AND user_id = ?", link.ContentID, link.UserID).Scan(&projectID).Error; err != nil {
						return err
					}
				}
				if projectID != "" {
					link.ProjectID = projectID
					if err := tx.Model(&model.ContentAssetLink{}).Where("id = ?", link.ID).UpdateColumn("project_id", projectID).Error; err != nil {
						return err
					}
				}
			}
			if exists, err := topicHistoryProjectExists(ctx, tx, link.UserID, link.ProjectID); err != nil {
				return err
			} else if !exists {
				continue
			}
			if link.Relation != model.ContentAssetLinkReference && link.ProjectID != "" {
				task, _, _, err := topicLinkedTask(ctx, repo, link.UserID, link.ProjectID, link.ContentKind, link.ContentID)
				if err != nil {
					return err
				}
				// Legacy links could reference another topic's task. Retain their history
				// as an association; the task's canonical topic remains authoritative.
				if task != nil && task.ContentAssetID != "" && task.ContentAssetID != link.AssetID {
					if err := repo.ContentAssets().ProjectTopicAssociation(ctx, link.UserID, link.ProjectID, link.AssetID, "", false, link.CreatedAt); err != nil {
						return err
					}
					continue
				}
			}
			if err := projectTopicLink(ctx, repo, &link); err != nil {
				return fmt.Errorf("migrate topic link %s: %w", link.ID, err)
			}
		}
		var associatedTasks []model.Task
		if err := tx.Where("content_asset_id IS NOT NULL AND content_asset_id <> ''").Order("created_at ASC, id ASC").Find(&associatedTasks).Error; err != nil {
			return err
		}
		for _, task := range associatedTasks {
			if exists, err := topicHistoryProjectExists(ctx, tx, task.UserID, task.ProjectID); err != nil {
				return err
			} else if !exists {
				continue
			}
			if _, err := repo.ContentAssets().FindLink(ctx, task.UserID, task.ContentAssetID, "task", task.ID, model.ContentAssetLinkDerivative); errors.Is(err, gorm.ErrRecordNotFound) {
				if err := repo.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: uuid.NewString(), UserID: task.UserID, ProjectID: task.ProjectID, AssetID: task.ContentAssetID, ContentKind: "task", ContentID: task.ID, Platform: task.ProjectSnapshot.Data().Platform, Relation: model.ContentAssetLinkDerivative, CreatedAt: task.CreatedAt}); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
			if err := repo.ContentAssets().ProjectTopicAssociation(ctx, task.UserID, task.ProjectID, task.ContentAssetID, task.ID, topicTaskIsProduction(&task), task.CreatedAt); err != nil {
				return err
			}
		}
		var evidence []model.ContentAssetEvidence
		if err := tx.Where("project_id IS NOT NULL AND project_id <> ''").Find(&evidence).Error; err != nil {
			return err
		}
		for _, item := range evidence {
			if exists, err := topicHistoryProjectExists(ctx, tx, item.UserID, item.ProjectID); err != nil {
				return err
			} else if !exists {
				continue
			}
			// Evidence is reference material; only authoritative output lineage consumes.
			if err := repo.ContentAssets().ProjectTopicAssociation(ctx, item.UserID, item.ProjectID, item.AssetID, "", false, item.CreatedAt); err != nil {
				return err
			}
		}
		var runs []model.ContentReuseRun
		if err := tx.Find(&runs).Error; err != nil {
			return err
		}
		for _, run := range runs {
			if exists, err := topicHistoryProjectExists(ctx, tx, run.UserID, run.TargetProjectID); err != nil {
				return err
			} else if !exists {
				continue
			}
			if err := repo.ContentAssets().ProjectTopicAssociation(ctx, run.UserID, run.TargetProjectID, run.AssetID, "", false, run.CreatedAt); err != nil {
				return err
			}
		}
		var researchJobs []model.ContentAssetResearchJob
		if err := tx.Where("candidate_asset_id IS NOT NULL AND candidate_asset_id <> ''").Find(&researchJobs).Error; err != nil {
			return err
		}
		for _, job := range researchJobs {
			if exists, err := topicHistoryProjectExists(ctx, tx, job.UserID, job.ProjectID); err != nil {
				return err
			} else if !exists {
				continue
			}
			assetID := job.CandidateAssetID
			if job.TaskID != nil && *job.TaskID != "" {
				if task, err := repo.Tasks().FindByID(ctx, *job.TaskID); err == nil && task.ContentAssetID != "" {
					assetID = task.ContentAssetID
				}
			}
			if err := repo.ContentAssets().ProjectTopicAssociation(ctx, job.UserID, job.ProjectID, assetID, "", false, job.CreatedAt); err != nil {
				return err
			}
		}
		return nil
	})
}

// Historical imports, reuse runs and research jobs can outlive a deleted persona.
// Only migration skips absent projects; live admission still requires ownership.
// Query by ID first so a surviving foreign project cannot masquerade as absent.
func topicHistoryProjectExists(ctx context.Context, tx *gorm.DB, userID, projectID string) (bool, error) {
	if projectID == "" {
		return false, nil
	}
	var project model.Project
	err := tx.WithContext(ctx).Where("id = ?", projectID).First(&project).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read historical topic project %s: %w", projectID, err)
	}
	if project.UserID != userID {
		return false, fmt.Errorf("historical topic project ownership mismatch: %s", projectID)
	}
	return true, nil
}
