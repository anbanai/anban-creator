package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anbanai/anban-creator/server/model"
	"github.com/anbanai/anban-creator/server/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	contentAutopilotPolicyBatch = 100
	contentAutopilotAssetBatch  = 100
	contentAutopilotFreshness   = 30 * 24 * time.Hour
)

// DiscoverPublishedContentAssets imports only the current, owned analytics
// projection as candidate evidence. It is idempotent by the canonical source
// link and analytics revision; it never copies platform metrics into a second
// fact store or marks a candidate as validated.
func (s *ContentAssetService) DiscoverPublishedContentAssets(ctx context.Context, userID, projectID string, expectedRevision int64) (int, error) {
	project, err := s.repo.Projects().FindByID(ctx, projectID)
	if err != nil || project.UserID != userID {
		return 0, fmt.Errorf("discovery project is not owned")
	}
	family, err := model.AnalyticsMetricFamilyForPlatform(project.Platform)
	if err != nil {
		return 0, nil
	}
	analytics := s.repo.Analytics().WithMetricFamily(family)
	state, err := analytics.State(ctx, projectID)
	if err != nil {
		return 0, err
	}
	if state.Status != "ready" || state.ActiveGeneration <= 0 || (expectedRevision > 0 && state.Revision != expectedRevision) {
		return 0, nil
	}
	created := 0
	for offset := 0; offset < 5000; offset += 100 {
		rows, total, err := analytics.Contents(ctx, projectID, state.ActiveGeneration, "cumulative", "1970-01-01", time.Now().UTC().Format("2006-01-02"), repository.AnalyticsContentFilter{Offset: offset, Limit: 100})
		if err != nil {
			return created, err
		}
		for _, row := range rows {
			kind, contentID := discoveredContentIdentity(row)
			if contentID == "" || !s.isPublishedAnalyticsContent(ctx, userID, projectID, row) {
				continue
			}
			title := strings.TrimSpace(row.Title)
			if title == "" {
				continue
			}
			asset, findErr := s.repo.ContentAssets().FindByTitle(ctx, userID, title)
			if findErr != nil {
				asset = model.NewContentAsset(userID, title)
				asset.CoreQuestion = title
				if err := s.repo.ContentAssets().Create(ctx, asset); err != nil {
					if existing, retryErr := s.repo.ContentAssets().FindByTitle(ctx, userID, title); retryErr == nil {
						asset = existing
					} else {
						return created, err
					}
				} else {
					created++
				}
			}
			if _, linkErr := s.repo.ContentAssets().FindLink(ctx, userID, asset.ID, kind, contentID, model.ContentAssetLinkOrigin); errors.Is(linkErr, gorm.ErrRecordNotFound) {
				if err := s.repo.ContentAssets().CreateLink(ctx, &model.ContentAssetLink{ID: uuid.NewString(), AssetID: asset.ID, UserID: userID, ProjectID: projectID, ContentKind: kind, ContentID: contentID, Platform: project.Platform, Relation: model.ContentAssetLinkOrigin, Angle: "internal published analytics"}); err != nil {
					return created, err
				}
			} else if linkErr != nil {
				return created, linkErr
			}
			evidence, evidenceErr := s.repo.ContentAssets().ListEvidence(ctx, userID, asset.ID)
			if evidenceErr != nil {
				return created, evidenceErr
			}
			known := false
			for _, item := range evidence {
				if item != nil && item.SourceType == model.ContentAssetEvidencePublished && item.ProjectID == projectID && item.SourceID == contentID && item.DataSource == fmt.Sprintf("analytics:%d", state.Revision) {
					known = true
					break
				}
			}
			if known {
				continue
			}
			var dataAsOf *time.Time
			if parsed, parseErr := time.Parse("2006-01-02", row.LastStatDate); parseErr == nil {
				dataAsOf = &parsed
			}
			revisionRef, _ := json.Marshal(map[string]int64{"analytics_revision": state.Revision})
			if err := s.repo.ContentAssets().CreateEvidence(ctx, &model.ContentAssetEvidence{ID: uuid.NewString(), AssetID: asset.ID, UserID: userID, SourceType: model.ContentAssetEvidencePublished, Platform: project.Platform, ProjectID: projectID, SourceKind: kind, SourceID: contentID, Title: title, Metrics: revisionRef, DataAsOf: dataAsOf, DataSource: fmt.Sprintf("analytics:%d", state.Revision), Quality: "verified"}); err != nil {
				return created, err
			}
		}
		if offset+len(rows) >= int(total) || len(rows) == 0 {
			break
		}
	}
	return created, nil
}

func (s *ContentAssetService) isPublishedAnalyticsContent(ctx context.Context, userID, projectID string, row repository.AnalyticsContentRow) bool {
	if row.PublicationID != "" {
		publication, err := s.repo.WechatPublications().FindByID(ctx, row.PublicationID)
		return err == nil && publication.UserID == userID && publication.ProjectID == projectID && publication.Status == model.WechatPublicationStatusPublished
	}
	if row.PostID != "" {
		post, err := s.repo.SeednotePosts().FindByID(ctx, projectID, row.PostID)
		return err == nil && post != nil && post.UserID == userID
	}
	if row.Status == "published" {
		if row.TaskID == "" {
			return false
		}
		task, err := s.repo.Tasks().FindByID(ctx, row.TaskID)
		return err == nil && task.UserID == userID && task.ProjectID == projectID && task.Status == model.TaskStatusCompleted
	}
	return false
}

func (s *ContentAssetService) dispatchDiscoveredResearch(ctx context.Context, userID, projectID string, revision int64, policyRevision int) error {
	if s.taskSvc == nil {
		return ErrContentPlanningUnavailable
	}
	if err := s.taskSvc.ContentPlanningAvailable(); err != nil {
		return err
	}
	assets, _, err := s.repo.ContentAssets().List(ctx, userID, "candidate", "", 0, 100)
	if err != nil {
		return err
	}
	for _, asset := range assets {
		if asset == nil || asset.Status != model.ContentAssetStatusCandidate {
			continue
		}
		evidence, err := s.repo.ContentAssets().ListEvidence(ctx, userID, asset.ID)
		if err != nil {
			return err
		}
		for _, item := range evidence {
			if item == nil || item.SourceType != model.ContentAssetEvidencePublished || item.ProjectID != projectID || item.Quality != "verified" || item.DataSource != fmt.Sprintf("analytics:%d", revision) {
				continue
			}
			fingerprint := researchDiscoveryFingerprint(userID, projectID, item.SourceID, revision, policyRevision)
			job, err := s.EnsureResearchJob(ctx, userID, projectID, asset.ID, fingerprint, revision, policyRevision, map[string]any{"project_id": projectID, "analytics_revision": revision, "source_kind": item.SourceKind, "source_id": item.SourceID})
			if err != nil {
				return err
			}
			if job.TaskID != nil && strings.TrimSpace(*job.TaskID) != "" {
				break
			}
			input := map[string]any{
				"schema_version":     "1.0",
				"task_kind":          model.TaskKindContentResearch,
				"candidates":         []map[string]any{{"asset_id": asset.ID, "title": asset.Title, "source_refs": []string{item.SourceID}}},
				"accepted_artifacts": []any{},
				"internal_sources":   []map[string]any{{"project_id": projectID, "content_id": item.SourceKind + ":" + item.SourceID, "analytics_revision": revision}},
			}
			tasks, err := s.taskSvc.CreateManual(ctx, CreateManualParams{UserID: userID, ProjectID: projectID, AgentID: model.AgentIDContentPlanning, Channel: model.ChannelContentPlanning, TaskKind: model.TaskKindContentResearch, RequestedTaskType: model.TaskTypeContentResearch, ExecutionProfile: "effective", Prompt: "research discovered published content", ContentAssetID: asset.ID, ResearchJobID: job.ID, AgentInput: input})
			if err != nil {
				return err
			}
			for _, task := range tasks {
				if err := s.taskSvc.EnqueueExecution(ctx, task, nil); err != nil {
					return err
				}
			}
			break
		}
	}
	return nil
}

func discoveredContentIdentity(row repository.AnalyticsContentRow) (string, string) {
	if row.PublicationID != "" {
		return "wechat_publication", row.PublicationID
	}
	if row.PostID != "" {
		return "seednote_post", row.PostID
	}
	if row.TaskID != "" {
		return "task", row.TaskID
	}
	return "", ""
}

func researchDiscoveryFingerprint(userID, projectID, contentID string, revision int64, policyRevision int) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%d:%d", userID, projectID, contentID, revision, policyRevision)))
	return hex.EncodeToString(digest[:])
}

// RunAutopilotTick admits eligible assets from the latest current-revision
// score snapshots. It is safe to invoke repeatedly; the daily run key and run
// repository uniqueness make overlapping ticks idempotent.
func (s *ContentAssetService) RunAutopilotTick(ctx context.Context, now time.Time) (int, error) {
	if s == nil || s.repo == nil {
		return 0, fmt.Errorf("content autopilot service unavailable")
	}
	if s.enqueuer == nil {
		return 0, fmt.Errorf("content reuse queue unavailable")
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	if err := s.repo.ContentAssets().RecoverReuseOperationBudgets(ctx, now); err != nil {
		return 0, err
	}
	if err := s.recoverReuseDispatch(ctx, now); err != nil {
		return 0, err
	}
	created := 0
	for afterUserID := ""; ; {
		policies, err := s.repo.ContentAssets().ListEnabledPoliciesAfter(ctx, afterUserID, contentAutopilotPolicyBatch)
		if err != nil {
			return created, fmt.Errorf("list autopilot policies: %w", err)
		}
		for _, policy := range policies {
			projectIDs, err := decodePolicyStrings(policy.ProjectIDs)
			if err != nil {
				return created, fmt.Errorf("decode policy project scope for user %s: %w", policy.UserID, err)
			}
			platforms, err := decodePolicyStrings(policy.Platforms)
			if err != nil {
				return created, fmt.Errorf("decode policy platform scope for user %s: %w", policy.UserID, err)
			}
			if len(projectIDs) == 0 || len(platforms) == 0 {
				continue
			}
			allowedPlatforms := stringSet(platforms)
			for _, projectID := range projectIDs {
				project, err := s.repo.Projects().FindByID(ctx, projectID)
				if err != nil || project.UserID != policy.UserID || !allowedPlatforms[project.Platform] {
					continue
				}
				family, err := model.AnalyticsMetricFamilyForPlatform(project.Platform)
				if err != nil {
					continue
				}
				state, err := s.repo.Analytics().WithMetricFamily(family).State(ctx, project.ID)
				if err != nil {
					return created, fmt.Errorf("read analytics revision for project %s: %w", project.ID, err)
				}
				if state.Status != "ready" || state.ActiveGeneration <= 0 {
					continue
				}
				if _, discoveryErr := s.DiscoverPublishedContentAssets(ctx, policy.UserID, project.ID, state.Revision); discoveryErr != nil {
					// A single project's provider/schema issue must not starve the
					// remaining enabled projects in this bounded scheduler pass.
					continue
				}
				if researchErr := s.dispatchDiscoveredResearch(ctx, policy.UserID, project.ID, state.Revision, policy.Revision); researchErr != nil && !errors.Is(researchErr, ErrContentPlanningUnavailable) {
					continue
				}
				for offset := 0; ; offset += contentAutopilotAssetBatch {
					assets, total, err := s.repo.ContentAssets().List(ctx, policy.UserID, "", "", offset, contentAutopilotAssetBatch)
					if err != nil {
						return created, fmt.Errorf("list autopilot assets for user %s: %w", policy.UserID, err)
					}
					for _, asset := range assets {
						if asset.Status != model.ContentAssetStatusValidated && asset.Status != model.ContentAssetStatusReady {
							continue
						}
						snapshots, err := s.repo.ContentAssets().ListScoreSnapshots(ctx, policy.UserID, asset.ID)
						if err != nil {
							return created, fmt.Errorf("list score snapshots for asset %s: %w", asset.ID, err)
						}
						var latest *model.ContentAssetScoreSnapshot
						for _, candidate := range snapshots {
							if candidate.ProjectID == project.ID && candidate.Platform == project.Platform {
								latest = candidate
								break
							}
						}
						refresh := latest == nil || latest.AnalyticsRevision != state.Revision || asset.UpdatedAt.After(latest.CreatedAt) || now.Sub(latest.CreatedAt) >= 24*time.Hour
						if !refresh {
							evidence, err := s.repo.ContentAssets().ListEvidence(ctx, policy.UserID, asset.ID)
							if err != nil {
								return created, fmt.Errorf("read score evidence changes: %w", err)
							}
							for _, item := range evidence {
								if item.CreatedAt.After(latest.CreatedAt) {
									refresh = true
									break
								}
							}
						}
						if refresh {
							latest, err = s.ScoreAssetForProject(ctx, policy.UserID, asset.ID, project.ID)
							if err != nil {
								return created, fmt.Errorf("refresh score for asset %s: %w", asset.ID, err)
							}
						}
						if !autopilotSnapshotEligible(latest, policy, project.Platform, state.Revision, now) {
							continue
						}
						var scoreSnapshot map[string]any
						if err := json.Unmarshal(latest.Snapshot, &scoreSnapshot); err != nil {
							return created, fmt.Errorf("decode score snapshot %s: %w", latest.ID, err)
						}
						key := fmt.Sprintf("autopilot-%s-%s-%s-r%d", now.Format("20060102"), shortIdentity(asset.ID), shortIdentity(project.ID), state.Revision)
						_, newlyCreated, err := s.createAutopilotReuseRun(ctx, CreateReuseRunParams{
							UserID: policy.UserID, AssetID: asset.ID, TargetProjectID: project.ID,
							expectedPolicyRevision: policy.Revision, scoredRecommendation: latest,
							TargetPlatform: project.Platform, IdempotencyKey: key, ScoreSnapshot: scoreSnapshot,
						})
						if err != nil {
							if errors.Is(err, ErrContentReuseConflict) {
								continue
							}
							return created, fmt.Errorf("create autopilot run for asset %s: %w", asset.ID, err)
						}
						if newlyCreated {
							created++
						}
					}
					if offset+len(assets) >= int(total) || len(assets) == 0 {
						break
					}
				}
			}
		}
		if len(policies) < contentAutopilotPolicyBatch {
			break
		}
		afterUserID = policies[len(policies)-1].UserID
	}
	return created, nil
}

func autopilotSnapshotEligible(snapshot *model.ContentAssetScoreSnapshot, policy *model.ContentAutopilotPolicy, platform string, revision int64, now time.Time) bool {
	if snapshot == nil || policy == nil || snapshot.Status != "scored" || snapshot.AnalyticsRevision != revision || snapshot.DataAsOf == nil || snapshot.DataAsOf.After(now) || now.Sub(*snapshot.DataAsOf) > contentAutopilotFreshness {
		return false
	}
	minimum := policy.MinRecommendationScore
	if overrides, err := decodePlatformScoreOverrides(policy.PlatformMinRecommendationScores); err != nil {
		return false
	} else if override, ok := overrides[platform]; ok {
		minimum = override
	}
	if snapshot.Score < float64(minimum) {
		return false
	}
	if isVideoProductionPlatform(platform) && confidencePercent(snapshot.Confidence) < policy.MinVideoConfidence {
		return false
	}
	return true
}

func isVideoProductionPlatform(platform string) bool {
	switch platform {
	case model.PlatformHypit, model.PlatformMontage, model.PlatformWhiteboardAnimation:
		return true
	default:
		return false
	}
}

func confidencePercent(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return 100
	case "medium":
		return 50
	default:
		return 0
	}
}

func decodePolicyStrings(raw []byte) ([]string, error) {
	if len(strings.TrimSpace(string(raw))) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result, nil
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func shortIdentity(value string) string {
	value = strings.ReplaceAll(value, "-", "")
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

// Recover dispatch after an enqueue failure or a Server crash. This does not
// advance content stages or perform external publication; workers reconcile the
// persisted task binding before creating any new work.
func (s *ContentAssetService) recoverReuseDispatch(ctx context.Context, now time.Time) error {
	runs, err := s.repo.ContentAssets().ListRecoverableReuseRuns(ctx, now, 100)
	if err != nil {
		return err
	}
	var failures []error
	for _, run := range runs {
		// Touch the dispatch timestamp before enqueue, clearing only an elapsed
		// retry delay. A crash at either side is recovered by the next aged scan;
		// worker/CAS/task uniqueness handles duplicate delivery.
		run.NextRunAt = nil
		if err := s.repo.ContentAssets().UpdateReuseRun(ctx, run); err != nil {
			continue
		}
		if err := s.enqueueReuseRun(ctx, run); err != nil {
			if run.TaskID == "" {
				_ = s.failReuseRun(ctx, run, "queue_unavailable", err)
			}
			failures = append(failures, fmt.Errorf("recover run %s: %w", run.ID, err))
		}
	}
	return errors.Join(failures...)
}
